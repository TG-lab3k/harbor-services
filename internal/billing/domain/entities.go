package domain

import "time"

// Supported MoR / payment providers.
const (
	ProviderCreem         = "creem"
	ProviderPaddle        = "paddle"
	ProviderWaffoPancake  = "waffo_pancake"
	ProviderMock          = "mock" // local/sandbox without real MoR
)

// KnownProviders lists configurable provider keys.
var KnownProviders = []string{
	ProviderCreem,
	ProviderPaddle,
	ProviderWaffoPancake,
	ProviderMock,
}

func IsKnownProvider(name string) bool {
	for _, p := range KnownProviders {
		if p == name {
			return true
		}
	}
	return false
}

// ProviderCredentials holds per-provider settings (secrets encrypted at rest).
type ProviderCredentials struct {
	Enabled                bool           `json:"enabled"`
	AccountID              string         `json:"account_id,omitempty"`
	APIKeyEncrypted        string         `json:"-"`
	WebhookSecretEncrypted string         `json:"-"`
	Extra                  map[string]any `json:"extra,omitempty"`
}

func (p *ProviderCredentials) Clone() *ProviderCredentials {
	if p == nil {
		return nil
	}
	out := *p
	if p.Extra != nil {
		out.Extra = make(map[string]any, len(p.Extra))
		for k, v := range p.Extra {
			out.Extra[k] = v
		}
	}
	return &out
}

func (p *ProviderCredentials) APIKeySet() bool {
	return p != nil && p.APIKeyEncrypted != ""
}

func (p *ProviderCredentials) WebhookSecretSet() bool {
	return p != nil && p.WebhookSecretEncrypted != ""
}

// BillingConfig is the per-app billing configuration document.
type BillingConfig struct {
	AppID           string
	Enabled         bool
	DefaultProvider *string
	TestMode        bool
	Providers       map[string]*ProviderCredentials
	UpdatedAt       *time.Time
}

func (c *BillingConfig) Clone() *BillingConfig {
	if c == nil {
		return nil
	}
	out := *c
	if c.DefaultProvider != nil {
		v := *c.DefaultProvider
		out.DefaultProvider = &v
	}
	if c.UpdatedAt != nil {
		t := *c.UpdatedAt
		out.UpdatedAt = &t
	}
	if c.Providers != nil {
		out.Providers = make(map[string]*ProviderCredentials, len(c.Providers))
		for k, v := range c.Providers {
			out.Providers[k] = v.Clone()
		}
	} else {
		out.Providers = map[string]*ProviderCredentials{}
	}
	return &out
}

func (c *BillingConfig) Provider(name string) *ProviderCredentials {
	if c == nil || c.Providers == nil {
		return nil
	}
	return c.Providers[name]
}

// ProductType is the catalog product kind.
type ProductType string

const (
	ProductTypeOneTime      ProductType = "one_time"
	ProductTypeSubscription ProductType = "subscription"
)

// ProductStatus is catalog lifecycle.
type ProductStatus string

const (
	ProductStatusActive   ProductStatus = "active"
	ProductStatusArchived ProductStatus = "archived"
)

// Product maps a harbor catalog item to provider price/product ids.
type Product struct {
	ProductID        string            `json:"product_id"`
	AppID            string            `json:"app_id"`
	Name             string            `json:"name"`
	Description      string            `json:"description,omitempty"`
	Type             ProductType       `json:"type"`
	ProviderPriceIDs map[string]string `json:"provider_price_ids"`
	Status           ProductStatus     `json:"status"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

func (p *Product) Clone() *Product {
	if p == nil {
		return nil
	}
	out := *p
	if p.ProviderPriceIDs != nil {
		out.ProviderPriceIDs = make(map[string]string, len(p.ProviderPriceIDs))
		for k, v := range p.ProviderPriceIDs {
			out.ProviderPriceIDs[k] = v
		}
	}
	return &out
}

func (p *Product) PriceIDFor(provider string) (string, bool) {
	if p == nil || p.ProviderPriceIDs == nil {
		return "", false
	}
	id, ok := p.ProviderPriceIDs[provider]
	return id, ok && id != ""
}

// OrderStatus is the standardized order state.
type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "pending"
	OrderStatusPaid      OrderStatus = "paid"
	OrderStatusFailed    OrderStatus = "failed"
	OrderStatusCanceled  OrderStatus = "canceled"
	OrderStatusRefunded  OrderStatus = "refunded"
	OrderStatusExpired   OrderStatus = "expired"
)

// Order is the harbor billing order.
type Order struct {
	OrderID          string         `json:"order_id"`
	CheckoutID       string         `json:"checkout_id"`
	AppID            string         `json:"app_id"`
	UserID           string         `json:"user_id,omitempty"`
	CustomerEmail    string         `json:"customer_email,omitempty"`
	ProductID        string         `json:"product_id,omitempty"`
	Provider         string         `json:"provider"`
	ProviderProductID string        `json:"provider_product_id,omitempty"`
	ProviderOrderID  string         `json:"provider_order_id,omitempty"`
	ProviderCheckoutID string       `json:"provider_checkout_id,omitempty"`
	CheckoutURL      string         `json:"checkout_url,omitempty"`
	Status           OrderStatus    `json:"status"`
	Currency         string         `json:"currency,omitempty"`
	AmountCents      int64          `json:"amount_cents,omitempty"`
	IdempotencyKey   string         `json:"idempotency_key,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	SuccessURL       string         `json:"success_url,omitempty"`
	CancelURL        string         `json:"cancel_url,omitempty"`
	TestMode         bool           `json:"test_mode"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	PaidAt           *time.Time     `json:"paid_at,omitempty"`
}

func (o *Order) Clone() *Order {
	if o == nil {
		return nil
	}
	out := *o
	if o.Metadata != nil {
		out.Metadata = make(map[string]any, len(o.Metadata))
		for k, v := range o.Metadata {
			out.Metadata[k] = v
		}
	}
	if o.PaidAt != nil {
		t := *o.PaidAt
		out.PaidAt = &t
	}
	return &out
}

// WebhookEvent is an idempotent inbound provider event.
type WebhookEvent struct {
	EventDocID string
	Provider   string
	EventID    string
	AppID      string
	EventType  string
	OrderID    string
	Payload    map[string]any
	CreatedAt  time.Time
}

func (e *WebhookEvent) Clone() *WebhookEvent {
	if e == nil {
		return nil
	}
	out := *e
	if e.Payload != nil {
		out.Payload = make(map[string]any, len(e.Payload))
		for k, v := range e.Payload {
			out.Payload[k] = v
		}
	}
	return &out
}

// WebhookEventDocID builds a unique doc id from provider + event id.
func WebhookEventDocID(provider, eventID string) string {
	return provider + "_" + eventID
}
