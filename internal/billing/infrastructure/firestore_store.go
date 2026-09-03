package infrastructure

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/okok/harbor-services/internal/billing/domain"
)

const (
	colConfigs  = "app_billing_configs"
	colProducts = "billing_products"
	colOrders   = "billing_orders"
	colWebhooks = "billing_webhook_events"
)

// FirestoreStore exposes Billing repositories backed by Firestore.
type FirestoreStore struct {
	client *firestore.Client
}

func NewFirestoreStore(client *firestore.Client) *FirestoreStore {
	return &FirestoreStore{client: client}
}

func (s *FirestoreStore) ConfigRepo() domain.ConfigRepository       { return &fsConfigRepo{s.client} }
func (s *FirestoreStore) ProductRepo() domain.ProductRepository     { return &fsProductRepo{s.client} }
func (s *FirestoreStore) OrderRepo() domain.OrderRepository         { return &fsOrderRepo{s.client} }
func (s *FirestoreStore) WebhookRepo() domain.WebhookEventRepository { return &fsWebhookRepo{s.client} }

// --- Config ---

type fsConfigRepo struct{ c *firestore.Client }

type billingConfigDoc struct {
	AppID           string                     `firestore:"app_id"`
	Enabled         bool                       `firestore:"enabled"`
	DefaultProvider *string                    `firestore:"default_provider"`
	TestMode        bool                       `firestore:"test_mode"`
	Providers       map[string]providerCredDoc `firestore:"providers"`
	UpdatedAt       *time.Time                 `firestore:"updated_at"`
}

type providerCredDoc struct {
	Enabled                bool           `firestore:"enabled"`
	AccountID              string         `firestore:"account_id"`
	APIKeyEncrypted        string         `firestore:"api_key_encrypted"`
	WebhookSecretEncrypted string         `firestore:"webhook_secret_encrypted"`
	Extra                  map[string]any `firestore:"extra"`
}

func (r *fsConfigRepo) Get(ctx context.Context, appID string) (*domain.BillingConfig, error) {
	snap, err := r.c.Collection(colConfigs).Doc(appID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}
	var d billingConfigDoc
	if err := snap.DataTo(&d); err != nil {
		return nil, err
	}
	return configFromDoc(&d), nil
}

func (r *fsConfigRepo) Upsert(ctx context.Context, cfg *domain.BillingConfig) error {
	_, err := r.c.Collection(colConfigs).Doc(cfg.AppID).Set(ctx, configToDoc(cfg))
	return err
}

func configToDoc(c *domain.BillingConfig) *billingConfigDoc {
	d := &billingConfigDoc{
		AppID:           c.AppID,
		Enabled:         c.Enabled,
		DefaultProvider: c.DefaultProvider,
		TestMode:        c.TestMode,
		UpdatedAt:       c.UpdatedAt,
		Providers:       map[string]providerCredDoc{},
	}
	for name, p := range c.Providers {
		if p == nil {
			continue
		}
		d.Providers[name] = providerCredDoc{
			Enabled:                p.Enabled,
			AccountID:              p.AccountID,
			APIKeyEncrypted:        p.APIKeyEncrypted,
			WebhookSecretEncrypted: p.WebhookSecretEncrypted,
			Extra:                  p.Extra,
		}
	}
	return d
}

func configFromDoc(d *billingConfigDoc) *domain.BillingConfig {
	c := &domain.BillingConfig{
		AppID:           d.AppID,
		Enabled:         d.Enabled,
		DefaultProvider: d.DefaultProvider,
		TestMode:        d.TestMode,
		UpdatedAt:       d.UpdatedAt,
		Providers:       map[string]*domain.ProviderCredentials{},
	}
	for name, p := range d.Providers {
		c.Providers[name] = &domain.ProviderCredentials{
			Enabled:                p.Enabled,
			AccountID:              p.AccountID,
			APIKeyEncrypted:        p.APIKeyEncrypted,
			WebhookSecretEncrypted: p.WebhookSecretEncrypted,
			Extra:                  p.Extra,
		}
	}
	return c
}

// --- Products ---

type fsProductRepo struct{ c *firestore.Client }

type productDoc struct {
	ProductID        string            `firestore:"product_id"`
	AppID            string            `firestore:"app_id"`
	Name             string            `firestore:"name"`
	Description      string            `firestore:"description"`
	Type             string            `firestore:"type"`
	ProviderPriceIDs map[string]string `firestore:"provider_price_ids"`
	Status           string            `firestore:"status"`
	CreatedAt        time.Time         `firestore:"created_at"`
	UpdatedAt        time.Time         `firestore:"updated_at"`
}

func (r *fsProductRepo) Create(ctx context.Context, p *domain.Product) error {
	_, err := r.c.Collection(colProducts).Doc(p.ProductID).Create(ctx, productToDoc(p))
	return err
}

func (r *fsProductRepo) GetByID(ctx context.Context, productID string) (*domain.Product, error) {
	snap, err := r.c.Collection(colProducts).Doc(productID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}
	var d productDoc
	if err := snap.DataTo(&d); err != nil {
		return nil, err
	}
	return productFromDoc(&d), nil
}

func (r *fsProductRepo) ListByApp(ctx context.Context, appID string, includeArchived bool) ([]*domain.Product, error) {
	q := r.c.Collection(colProducts).Where("app_id", "==", appID)
	snaps, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var out []*domain.Product
	for _, snap := range snaps {
		var d productDoc
		if err := snap.DataTo(&d); err != nil {
			return nil, err
		}
		p := productFromDoc(&d)
		if !includeArchived && p.Status == domain.ProductStatusArchived {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func (r *fsProductRepo) Update(ctx context.Context, p *domain.Product) error {
	_, err := r.c.Collection(colProducts).Doc(p.ProductID).Set(ctx, productToDoc(p))
	return err
}

func productToDoc(p *domain.Product) *productDoc {
	return &productDoc{
		ProductID:        p.ProductID,
		AppID:            p.AppID,
		Name:             p.Name,
		Description:      p.Description,
		Type:             string(p.Type),
		ProviderPriceIDs: p.ProviderPriceIDs,
		Status:           string(p.Status),
		CreatedAt:        p.CreatedAt,
		UpdatedAt:        p.UpdatedAt,
	}
}

func productFromDoc(d *productDoc) *domain.Product {
	return &domain.Product{
		ProductID:        d.ProductID,
		AppID:            d.AppID,
		Name:             d.Name,
		Description:      d.Description,
		Type:             domain.ProductType(d.Type),
		ProviderPriceIDs: d.ProviderPriceIDs,
		Status:           domain.ProductStatus(d.Status),
		CreatedAt:        d.CreatedAt,
		UpdatedAt:        d.UpdatedAt,
	}
}

// --- Orders ---

type fsOrderRepo struct{ c *firestore.Client }

type orderDoc struct {
	OrderID            string         `firestore:"order_id"`
	CheckoutID         string         `firestore:"checkout_id"`
	AppID              string         `firestore:"app_id"`
	UserID             string         `firestore:"user_id"`
	CustomerEmail      string         `firestore:"customer_email"`
	ProductID          string         `firestore:"product_id"`
	Provider           string         `firestore:"provider"`
	ProviderProductID  string         `firestore:"provider_product_id"`
	ProviderOrderID    string         `firestore:"provider_order_id"`
	ProviderCheckoutID string         `firestore:"provider_checkout_id"`
	CheckoutURL        string         `firestore:"checkout_url"`
	Status             string         `firestore:"status"`
	Currency           string         `firestore:"currency"`
	AmountCents        int64          `firestore:"amount_cents"`
	IdempotencyKey     string         `firestore:"idempotency_key"`
	Metadata           map[string]any `firestore:"metadata"`
	SuccessURL         string         `firestore:"success_url"`
	CancelURL          string         `firestore:"cancel_url"`
	TestMode           bool           `firestore:"test_mode"`
	CreatedAt          time.Time      `firestore:"created_at"`
	UpdatedAt          time.Time      `firestore:"updated_at"`
	PaidAt             *time.Time     `firestore:"paid_at"`
}

func (r *fsOrderRepo) Create(ctx context.Context, o *domain.Order) error {
	_, err := r.c.Collection(colOrders).Doc(o.OrderID).Create(ctx, orderToDoc(o))
	return err
}

func (r *fsOrderRepo) GetByID(ctx context.Context, orderID string) (*domain.Order, error) {
	snap, err := r.c.Collection(colOrders).Doc(orderID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}
	var d orderDoc
	if err := snap.DataTo(&d); err != nil {
		return nil, err
	}
	return orderFromDoc(&d), nil
}

func (r *fsOrderRepo) GetByIdempotencyKey(ctx context.Context, appID, key string) (*domain.Order, error) {
	snaps, err := r.c.Collection(colOrders).
		Where("app_id", "==", appID).
		Where("idempotency_key", "==", key).
		Limit(1).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(snaps) == 0 {
		return nil, nil
	}
	var d orderDoc
	if err := snaps[0].DataTo(&d); err != nil {
		return nil, err
	}
	return orderFromDoc(&d), nil
}

func (r *fsOrderRepo) GetByProviderCheckoutID(ctx context.Context, provider, providerCheckoutID string) (*domain.Order, error) {
	snaps, err := r.c.Collection(colOrders).
		Where("provider", "==", provider).
		Where("provider_checkout_id", "==", providerCheckoutID).
		Limit(1).
		Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(snaps) == 0 {
		return nil, nil
	}
	var d orderDoc
	if err := snaps[0].DataTo(&d); err != nil {
		return nil, err
	}
	return orderFromDoc(&d), nil
}

func (r *fsOrderRepo) Update(ctx context.Context, o *domain.Order) error {
	_, err := r.c.Collection(colOrders).Doc(o.OrderID).Set(ctx, orderToDoc(o))
	return err
}

func (r *fsOrderRepo) ListByApp(ctx context.Context, filter domain.OrderListFilter) ([]*domain.Order, error) {
	q := r.c.Collection(colOrders).Where("app_id", "==", filter.AppID)
	if filter.UserID != "" {
		q = q.Where("user_id", "==", filter.UserID)
	}
	if filter.Status != "" {
		q = q.Where("status", "==", string(filter.Status))
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	q = q.OrderBy("created_at", firestore.Desc).Limit(limit)
	snaps, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Order, 0, len(snaps))
	for _, snap := range snaps {
		var d orderDoc
		if err := snap.DataTo(&d); err != nil {
			return nil, err
		}
		out = append(out, orderFromDoc(&d))
	}
	return out, nil
}

func orderToDoc(o *domain.Order) *orderDoc {
	return &orderDoc{
		OrderID:            o.OrderID,
		CheckoutID:         o.CheckoutID,
		AppID:              o.AppID,
		UserID:             o.UserID,
		CustomerEmail:      o.CustomerEmail,
		ProductID:          o.ProductID,
		Provider:           o.Provider,
		ProviderProductID:  o.ProviderProductID,
		ProviderOrderID:    o.ProviderOrderID,
		ProviderCheckoutID: o.ProviderCheckoutID,
		CheckoutURL:        o.CheckoutURL,
		Status:             string(o.Status),
		Currency:           o.Currency,
		AmountCents:        o.AmountCents,
		IdempotencyKey:     o.IdempotencyKey,
		Metadata:           o.Metadata,
		SuccessURL:         o.SuccessURL,
		CancelURL:          o.CancelURL,
		TestMode:           o.TestMode,
		CreatedAt:          o.CreatedAt,
		UpdatedAt:          o.UpdatedAt,
		PaidAt:             o.PaidAt,
	}
}

func orderFromDoc(d *orderDoc) *domain.Order {
	return &domain.Order{
		OrderID:            d.OrderID,
		CheckoutID:         d.CheckoutID,
		AppID:              d.AppID,
		UserID:             d.UserID,
		CustomerEmail:      d.CustomerEmail,
		ProductID:          d.ProductID,
		Provider:           d.Provider,
		ProviderProductID:  d.ProviderProductID,
		ProviderOrderID:    d.ProviderOrderID,
		ProviderCheckoutID: d.ProviderCheckoutID,
		CheckoutURL:        d.CheckoutURL,
		Status:             domain.OrderStatus(d.Status),
		Currency:           d.Currency,
		AmountCents:        d.AmountCents,
		IdempotencyKey:     d.IdempotencyKey,
		Metadata:           d.Metadata,
		SuccessURL:         d.SuccessURL,
		CancelURL:          d.CancelURL,
		TestMode:           d.TestMode,
		CreatedAt:          d.CreatedAt,
		UpdatedAt:          d.UpdatedAt,
		PaidAt:             d.PaidAt,
	}
}

// --- Webhooks ---

type fsWebhookRepo struct{ c *firestore.Client }

type webhookDoc struct {
	EventDocID string         `firestore:"event_doc_id"`
	Provider   string         `firestore:"provider"`
	EventID    string         `firestore:"event_id"`
	AppID      string         `firestore:"app_id"`
	EventType  string         `firestore:"event_type"`
	OrderID    string         `firestore:"order_id"`
	Payload    map[string]any `firestore:"payload"`
	CreatedAt  time.Time      `firestore:"created_at"`
}

func (r *fsWebhookRepo) Create(ctx context.Context, e *domain.WebhookEvent) (bool, error) {
	id := e.EventDocID
	if id == "" {
		id = domain.WebhookEventDocID(e.Provider, e.EventID)
		e.EventDocID = id
	}
	_, err := r.c.Collection(colWebhooks).Doc(id).Create(ctx, &webhookDoc{
		EventDocID: id,
		Provider:   e.Provider,
		EventID:    e.EventID,
		AppID:      e.AppID,
		EventType:  e.EventType,
		OrderID:    e.OrderID,
		Payload:    e.Payload,
		CreatedAt:  e.CreatedAt,
	})
	if err != nil {
		if status.Code(err) == codes.AlreadyExists {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *fsWebhookRepo) Get(ctx context.Context, provider, eventID string) (*domain.WebhookEvent, error) {
	id := domain.WebhookEventDocID(provider, eventID)
	snap, err := r.c.Collection(colWebhooks).Doc(id).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}
	var d webhookDoc
	if err := snap.DataTo(&d); err != nil {
		return nil, err
	}
	return &domain.WebhookEvent{
		EventDocID: d.EventDocID,
		Provider:   d.Provider,
		EventID:    d.EventID,
		AppID:      d.AppID,
		EventType:  d.EventType,
		OrderID:    d.OrderID,
		Payload:    d.Payload,
		CreatedAt:  d.CreatedAt,
	}, nil
}
