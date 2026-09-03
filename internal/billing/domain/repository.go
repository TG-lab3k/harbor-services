package domain

import "context"

// ConfigRepository persists per-app billing configuration.
type ConfigRepository interface {
	Get(ctx context.Context, appID string) (*BillingConfig, error)
	Upsert(ctx context.Context, cfg *BillingConfig) error
}

// ProductRepository persists catalog products.
type ProductRepository interface {
	Create(ctx context.Context, p *Product) error
	GetByID(ctx context.Context, productID string) (*Product, error)
	ListByApp(ctx context.Context, appID string, includeArchived bool) ([]*Product, error)
	Update(ctx context.Context, p *Product) error
}

// OrderRepository persists orders.
type OrderRepository interface {
	Create(ctx context.Context, o *Order) error
	GetByID(ctx context.Context, orderID string) (*Order, error)
	GetByIdempotencyKey(ctx context.Context, appID, key string) (*Order, error)
	GetByProviderCheckoutID(ctx context.Context, provider, providerCheckoutID string) (*Order, error)
	Update(ctx context.Context, o *Order) error
	ListByApp(ctx context.Context, filter OrderListFilter) ([]*Order, error)
}

// OrderListFilter controls order listing.
type OrderListFilter struct {
	AppID  string
	UserID string
	Status OrderStatus
	Limit  int
}

// WebhookEventRepository stores idempotent webhook receipts.
type WebhookEventRepository interface {
	// Create returns false if (provider, event_id) already exists.
	Create(ctx context.Context, e *WebhookEvent) (created bool, err error)
	Get(ctx context.Context, provider, eventID string) (*WebhookEvent, error)
}
