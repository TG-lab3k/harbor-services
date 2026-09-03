package providers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/okok/harbor-services/internal/billing/domain"
	"github.com/okok/harbor-services/internal/platform/apperr"
)

// MockProvider is a local sandbox adapter (no external MoR).
type MockProvider struct{}

func NewMock() *MockProvider { return &MockProvider{} }

func (p *MockProvider) Name() string { return domain.ProviderMock }

func (p *MockProvider) CreateCheckout(_ context.Context, _ domain.ProviderCredentialsPlain, in domain.CreateCheckoutInput) (*domain.CreateCheckoutResult, error) {
	id := "mock_ch_" + in.CheckoutID
	return &domain.CreateCheckoutResult{
		ProviderCheckoutID: id,
		CheckoutURL:        fmt.Sprintf("https://mock.harbor.local/checkout/%s?order_id=%s", id, in.OrderID),
	}, nil
}

func (p *MockProvider) VerifyAndParseWebhook(creds domain.ProviderCredentialsPlain, headers map[string]string, body []byte) (*domain.ParsedWebhook, error) {
	sig := headerGet(headers, "X-Harbor-Mock-Signature")
	if creds.WebhookSecret == "" {
		// allow unsigned in mock when secret not set
	} else {
		mac := hmac.New(sha256.New, []byte(creds.WebhookSecret))
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(strings.ToLower(sig)), []byte(strings.ToLower(expected))) {
			return nil, apperr.BillingWebhookSig("")
		}
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, apperr.BillingWebhookSig("invalid json")
	}
	eventID, _ := payload["event_id"].(string)
	if eventID == "" {
		eventID = fmt.Sprintf("mock_%d", time.Now().UnixNano())
	}
	eventType, _ := payload["event_type"].(string)
	if eventType == "" {
		eventType = "checkout.completed"
	}
	providerCheckoutID, _ := payload["provider_checkout_id"].(string)
	providerOrderID, _ := payload["provider_order_id"].(string)
	requestID, _ := payload["request_id"].(string)
	status := domain.OrderStatusPaid
	if s, ok := payload["status"].(string); ok && s != "" {
		status = domain.OrderStatus(s)
	}
	raw := payload
	if requestID != "" {
		raw["request_id"] = requestID
	}
	return &domain.ParsedWebhook{
		EventID:            eventID,
		EventType:          eventType,
		ProviderCheckoutID: providerCheckoutID,
		ProviderOrderID:    providerOrderID,
		Status:             status,
		Raw:                raw,
	}, nil
}

func headerGet(h map[string]string, key string) string {
	if h == nil {
		return ""
	}
	if v, ok := h[key]; ok {
		return v
	}
	// case-insensitive
	lk := strings.ToLower(key)
	for k, v := range h {
		if strings.ToLower(k) == lk {
			return v
		}
	}
	return ""
}
