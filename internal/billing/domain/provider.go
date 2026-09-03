package domain

import (
	"context"
)

// CreateCheckoutInput is provider-agnostic checkout request.
type CreateCheckoutInput struct {
	OrderID           string
	CheckoutID        string
	ProviderProductID string
	SuccessURL        string
	CancelURL         string
	CustomerEmail     string
	Metadata          map[string]any
	RequestID         string // maps to Creem request_id for correlation
	TestMode          bool
}

// CreateCheckoutResult is the provider checkout session.
type CreateCheckoutResult struct {
	ProviderCheckoutID string
	CheckoutURL        string
	ProviderOrderID    string
	Raw                map[string]any
}

// ParsedWebhook is a normalized inbound event.
type ParsedWebhook struct {
	EventID            string
	EventType          string
	ProviderCheckoutID string
	ProviderOrderID    string
	Status             OrderStatus // mapped harbor status when applicable
	CustomerEmail      string
	Raw                map[string]any
}

// ProviderCredentialsPlain is decrypted credentials for adapter use.
type ProviderCredentialsPlain struct {
	AccountID     string
	APIKey        string
	WebhookSecret string
	Extra         map[string]any
	TestMode      bool
}

// Provider adapts a MoR / payment gateway.
type Provider interface {
	Name() string
	CreateCheckout(ctx context.Context, creds ProviderCredentialsPlain, in CreateCheckoutInput) (*CreateCheckoutResult, error)
	// VerifyAndParseWebhook verifies signature and parses the payload.
	VerifyAndParseWebhook(creds ProviderCredentialsPlain, headers map[string]string, body []byte) (*ParsedWebhook, error)
}

// ProviderRegistry resolves providers by name.
type ProviderRegistry interface {
	Get(name string) (Provider, bool)
}
