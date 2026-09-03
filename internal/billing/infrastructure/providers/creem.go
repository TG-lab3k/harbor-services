package providers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/okok/harbor-services/internal/billing/domain"
	"github.com/okok/harbor-services/internal/platform/apperr"
)

const (
	creemLiveAPI = "https://api.creem.io/v1"
	creemTestAPI = "https://test-api.creem.io/v1"
)

// CreemProvider adapts Creem MoR REST API.
type CreemProvider struct {
	httpClient *http.Client
}

func NewCreem(httpClient *http.Client) *CreemProvider {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &CreemProvider{httpClient: httpClient}
}

func (p *CreemProvider) Name() string { return domain.ProviderCreem }

func (p *CreemProvider) baseURL(testMode bool) string {
	if testMode {
		return creemTestAPI
	}
	return creemLiveAPI
}

func (p *CreemProvider) CreateCheckout(ctx context.Context, creds domain.ProviderCredentialsPlain, in domain.CreateCheckoutInput) (*domain.CreateCheckoutResult, error) {
	if creds.APIKey == "" {
		return nil, apperr.BillingProvider("creem api_key required")
	}
	body := map[string]any{
		"product_id":  in.ProviderProductID,
		"request_id":  in.RequestID,
		"success_url": in.SuccessURL,
	}
	if in.CustomerEmail != "" {
		body["customer"] = map[string]any{"email": in.CustomerEmail}
	}
	if in.Metadata != nil {
		body["metadata"] = in.Metadata
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL(creds.TestMode)+"/checkouts", bytes.NewReader(raw))
	if err != nil {
		return nil, apperr.BillingUpstream(err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", creds.APIKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, apperr.BillingUpstream(err.Error())
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, apperr.BillingUpstream(fmt.Sprintf("creem checkout failed: %s", truncate(string(respBody), 200)))
	}
	var parsed struct {
		ID          string `json:"id"`
		CheckoutURL string `json:"checkout_url"`
		Status      string `json:"status"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, apperr.BillingUpstream("invalid creem response")
	}
	if parsed.CheckoutURL == "" {
		return nil, apperr.BillingUpstream("creem response missing checkout_url")
	}
	var rawMap map[string]any
	_ = json.Unmarshal(respBody, &rawMap)
	return &domain.CreateCheckoutResult{
		ProviderCheckoutID: parsed.ID,
		CheckoutURL:        parsed.CheckoutURL,
		Raw:                rawMap,
	}, nil
}

func (p *CreemProvider) VerifyAndParseWebhook(creds domain.ProviderCredentialsPlain, headers map[string]string, body []byte) (*domain.ParsedWebhook, error) {
	sig := headerGet(headers, "creem-signature")
	if creds.WebhookSecret == "" {
		return nil, apperr.BillingWebhookSig("webhook_secret not configured")
	}
	mac := hmac.New(sha256.New, []byte(creds.WebhookSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(strings.ToLower(sig)), []byte(strings.ToLower(expected))) {
		return nil, apperr.BillingWebhookSig("")
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, apperr.BillingWebhookSig("invalid json")
	}

	eventType, _ := payload["eventType"].(string)
	if eventType == "" {
		eventType, _ = payload["event_type"].(string)
	}
	eventID := extractEventID(payload)
	obj, _ := payload["object"].(map[string]any)
	if obj == nil {
		obj = payload
	}

	providerCheckoutID := asString(obj["id"])
	if providerCheckoutID == "" {
		providerCheckoutID = asString(obj["checkout_id"])
	}
	providerOrderID := ""
	if order, ok := obj["order"].(map[string]any); ok {
		providerOrderID = asString(order["id"])
	}
	if providerOrderID == "" {
		providerOrderID = asString(obj["order_id"])
	}
	requestID := asString(obj["request_id"])
	if requestID == "" {
		requestID = asString(payload["request_id"])
	}
	email := ""
	if cust, ok := obj["customer"].(map[string]any); ok {
		email = asString(cust["email"])
	}

	status := domain.OrderStatus("")
	switch eventType {
	case "checkout.completed":
		status = domain.OrderStatusPaid
	case "refund.created":
		status = domain.OrderStatusRefunded
	}

	raw := map[string]any{}
	for k, v := range payload {
		raw[k] = v
	}
	if requestID != "" {
		raw["request_id"] = requestID
	}

	return &domain.ParsedWebhook{
		EventID:            eventID,
		EventType:          eventType,
		ProviderCheckoutID: providerCheckoutID,
		ProviderOrderID:    providerOrderID,
		Status:             status,
		CustomerEmail:      email,
		Raw:                raw,
	}, nil
}

func extractEventID(payload map[string]any) string {
	if id := asString(payload["id"]); id != "" {
		return id
	}
	if id := asString(payload["event_id"]); id != "" {
		return id
	}
	// fallback: hash body uniqueness is handled by caller requiring non-empty —
	// use eventType + object id
	et := asString(payload["eventType"])
	if et == "" {
		et = asString(payload["event_type"])
	}
	obj, _ := payload["object"].(map[string]any)
	oid := ""
	if obj != nil {
		oid = asString(obj["id"])
	}
	if et != "" || oid != "" {
		return et + "_" + oid
	}
	sum := sha256.Sum256(mustJSON(payload))
	return hex.EncodeToString(sum[:16])
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return ""
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
