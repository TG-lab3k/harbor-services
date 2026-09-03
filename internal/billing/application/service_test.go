package application_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	billingapp "github.com/okok/harbor-services/internal/billing/application"
	"github.com/okok/harbor-services/internal/billing/domain"
	billinginfra "github.com/okok/harbor-services/internal/billing/infrastructure"
	"github.com/okok/harbor-services/internal/billing/infrastructure/providers"
	"github.com/okok/harbor-services/internal/billing"
	"github.com/okok/harbor-services/internal/platform/apperr"
	"github.com/okok/harbor-services/internal/shared/crypto"
	tenantdomain "github.com/okok/harbor-services/internal/tenant/domain"
)

type fakeApps struct {
	app *tenantdomain.App
}

func (f *fakeApps) RequireActiveApp(_ context.Context, appID string) (*tenantdomain.App, error) {
	if f.app == nil || f.app.AppID != appID || !f.app.IsActive() {
		return nil, apperr.AppNotFound("")
	}
	return f.app, nil
}

func (f *fakeApps) GetApp(_ context.Context, appID string) (*tenantdomain.App, error) {
	if f.app == nil || f.app.AppID != appID {
		return nil, apperr.AppNotFound("")
	}
	return f.app, nil
}

func (f *fakeApps) VerifyAppSecret(_ context.Context, appID, secret string) (*tenantdomain.App, error) {
	if secret != "secret" {
		return nil, apperr.InvalidAppSecret("")
	}
	return f.RequireActiveApp(context.Background(), appID)
}

func newTestService(t *testing.T) (*billingapp.Service, *fakeApps) {
	t.Helper()
	enc, err := crypto.NewEncryptor("test-encryption-key-32bytes!!")
	if err != nil {
		t.Fatal(err)
	}
	store := billinginfra.NewMemoryStore()
	apps := &fakeApps{app: &tenantdomain.App{
		AppID:  "app_test",
		Status: tenantdomain.AppStatusActive,
	}}
	svc := billingapp.NewService(billingapp.Deps{
		Apps:      apps,
		Configs:   store.ConfigRepo(),
		Products:  store.ProductRepo(),
		Orders:    store.OrderRepo(),
		Webhooks:  store.WebhookRepo(),
		Registry:  providers.NewRegistry(providers.NewMock(), providers.NewCreem(nil)),
		Encryptor: enc,
	})
	return svc, apps
}

func TestBillingConfigMaskAndCheckoutMock(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	dp := domain.ProviderMock
	_, err := svc.Put(ctx, "app_test", &billing.Config{
		Enabled:         true,
		DefaultProvider: &dp,
		TestMode:        true,
		Providers: map[string]billing.ProviderPublic{
			domain.ProviderMock: {
				Enabled:       true,
				APIKey:        "mock-key",
				WebhookSecret: "whsec",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, "app_test")
	if err != nil {
		t.Fatal(err)
	}
	if got.Providers[domain.ProviderMock].APIKey != "" {
		t.Fatal("api_key must be masked on GET")
	}
	if !got.Providers[domain.ProviderMock].APIKeySet {
		t.Fatal("api_key_set should be true")
	}

	prod, err := svc.CreateProduct(ctx, "app_test", billingapp.CreateProductInput{
		Name: "Pro",
		ProviderPriceIDs: map[string]string{
			domain.ProviderMock: "mock_price_1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	checkout, err := svc.CreateCheckout(ctx, "app_test", billingapp.CreateCheckoutInput{
		ProductID:      prod.ProductID,
		SuccessURL:     "https://example.com/ok",
		CancelURL:      "https://example.com/cancel",
		UserID:         "user_1",
		IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if checkout.CheckoutURL == "" || checkout.Status != domain.OrderStatusPending {
		t.Fatalf("unexpected checkout: %+v", checkout)
	}

	// idempotent replay
	again, err := svc.CreateCheckout(ctx, "app_test", billingapp.CreateCheckoutInput{
		ProductID:      prod.ProductID,
		SuccessURL:     "https://example.com/ok",
		CancelURL:      "https://example.com/cancel",
		UserID:         "user_1",
		IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.OrderID != checkout.OrderID {
		t.Fatal("idempotency should return same order")
	}

	order, err := svc.GetOrder(ctx, "app_test", checkout.OrderID, "")
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{
		"event_id":             "evt_1",
		"event_type":           "checkout.completed",
		"provider_checkout_id": order.ProviderCheckoutID,
		"request_id":           order.OrderID,
		"status":               "paid",
	})
	mac := hmac.New(sha256.New, []byte("whsec"))
	mac.Write(payload)
	sig := hex.EncodeToString(mac.Sum(nil))
	err = svc.HandleWebhook(ctx, domain.ProviderMock, "app_test", map[string]string{
		"X-Harbor-Mock-Signature": sig,
	}, payload)
	if err != nil {
		t.Fatal(err)
	}
	paid, err := svc.GetOrder(ctx, "app_test", checkout.OrderID, "")
	if err != nil {
		t.Fatal(err)
	}
	if paid.Status != domain.OrderStatusPaid {
		t.Fatalf("want paid, got %s", paid.Status)
	}
	// idempotent webhook
	err = svc.HandleWebhook(ctx, domain.ProviderMock, "app_test", map[string]string{
		"X-Harbor-Mock-Signature": sig,
	}, payload)
	if err != nil {
		t.Fatal(err)
	}

	// JWT user cannot read others' orders
	_, err = svc.GetOrder(ctx, "app_test", checkout.OrderID, "other_user")
	if err == nil {
		t.Fatal("expected forbidden")
	}
	if he, ok := apperr.AsHarborError(err); !ok || he.Code != apperr.CodeForbidden {
		t.Fatalf("want forbidden, got %v", err)
	}
}

func TestCheckoutRejectsInactiveApp(t *testing.T) {
	svc, apps := newTestService(t)
	ctx := context.Background()
	dp := domain.ProviderMock
	_, _ = svc.Put(ctx, "app_test", &billing.Config{
		Enabled:         true,
		DefaultProvider: &dp,
		Providers: map[string]billing.ProviderPublic{
			domain.ProviderMock: {Enabled: true},
		},
	})
	apps.app.Status = tenantdomain.AppStatusDisabled
	_, err := svc.CreateCheckout(ctx, "app_test", billingapp.CreateCheckoutInput{
		ProviderProductID: "x",
		SuccessURL:        "https://example.com/ok",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if he, ok := apperr.AsHarborError(err); !ok || he.Code != apperr.CodeBillingAppInactive {
		t.Fatalf("want 3010, got %v", err)
	}
}

func TestDefaultProviderMustBeEnabled(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	dp := domain.ProviderCreem
	_, err := svc.Put(ctx, "app_test", &billing.Config{
		Enabled:         true,
		DefaultProvider: &dp,
		Providers: map[string]billing.ProviderPublic{
			domain.ProviderCreem: {Enabled: false, APIKey: "k"},
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if he, ok := apperr.AsHarborError(err); !ok || he.Code != apperr.CodeBillingProvider {
		t.Fatalf("want billing provider error, got %v", err)
	}
}
