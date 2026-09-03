package infrastructure

import (
	"context"
	"sort"
	"sync"

	"github.com/okok/harbor-services/internal/billing/domain"
)

// MemoryStore holds in-process billing data and exposes typed repositories.
type MemoryStore struct {
	mu               sync.RWMutex
	configs          map[string]*domain.BillingConfig
	products         map[string]*domain.Product
	orders           map[string]*domain.Order
	idempotency      map[string]string
	providerCheckout map[string]string
	webhooks         map[string]*domain.WebhookEvent
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		configs:          make(map[string]*domain.BillingConfig),
		products:         make(map[string]*domain.Product),
		orders:           make(map[string]*domain.Order),
		idempotency:      make(map[string]string),
		providerCheckout: make(map[string]string),
		webhooks:         make(map[string]*domain.WebhookEvent),
	}
}

func (s *MemoryStore) ConfigRepo() domain.ConfigRepository           { return &memoryConfigRepo{s} }
func (s *MemoryStore) ProductRepo() domain.ProductRepository         { return &memoryProductRepo{s} }
func (s *MemoryStore) OrderRepo() domain.OrderRepository             { return &memoryOrderRepo{s} }
func (s *MemoryStore) WebhookRepo() domain.WebhookEventRepository     { return &memoryWebhookRepo{s} }

type memoryConfigRepo struct{ s *MemoryStore }

func (r *memoryConfigRepo) Get(_ context.Context, appID string) (*domain.BillingConfig, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	cfg, ok := r.s.configs[appID]
	if !ok {
		return nil, nil
	}
	return cfg.Clone(), nil
}

func (r *memoryConfigRepo) Upsert(_ context.Context, cfg *domain.BillingConfig) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.configs[cfg.AppID] = cfg.Clone()
	return nil
}

type memoryProductRepo struct{ s *MemoryStore }

func (r *memoryProductRepo) Create(_ context.Context, p *domain.Product) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.products[p.ProductID] = p.Clone()
	return nil
}

func (r *memoryProductRepo) GetByID(_ context.Context, productID string) (*domain.Product, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	p, ok := r.s.products[productID]
	if !ok {
		return nil, nil
	}
	return p.Clone(), nil
}

func (r *memoryProductRepo) ListByApp(_ context.Context, appID string, includeArchived bool) ([]*domain.Product, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	var out []*domain.Product
	for _, p := range r.s.products {
		if p.AppID != appID {
			continue
		}
		if !includeArchived && p.Status == domain.ProductStatusArchived {
			continue
		}
		out = append(out, p.Clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (r *memoryProductRepo) Update(_ context.Context, p *domain.Product) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.products[p.ProductID] = p.Clone()
	return nil
}

type memoryOrderRepo struct{ s *MemoryStore }

func (r *memoryOrderRepo) Create(_ context.Context, o *domain.Order) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.orders[o.OrderID] = o.Clone()
	if o.IdempotencyKey != "" {
		r.s.idempotency[o.AppID+"|"+o.IdempotencyKey] = o.OrderID
	}
	if o.ProviderCheckoutID != "" {
		r.s.providerCheckout[o.Provider+"|"+o.ProviderCheckoutID] = o.OrderID
	}
	return nil
}

func (r *memoryOrderRepo) GetByID(_ context.Context, orderID string) (*domain.Order, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	o, ok := r.s.orders[orderID]
	if !ok {
		return nil, nil
	}
	return o.Clone(), nil
}

func (r *memoryOrderRepo) GetByIdempotencyKey(_ context.Context, appID, key string) (*domain.Order, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	id, ok := r.s.idempotency[appID+"|"+key]
	if !ok {
		return nil, nil
	}
	o, ok := r.s.orders[id]
	if !ok {
		return nil, nil
	}
	return o.Clone(), nil
}

func (r *memoryOrderRepo) GetByProviderCheckoutID(_ context.Context, provider, providerCheckoutID string) (*domain.Order, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	id, ok := r.s.providerCheckout[provider+"|"+providerCheckoutID]
	if !ok {
		return nil, nil
	}
	o, ok := r.s.orders[id]
	if !ok {
		return nil, nil
	}
	return o.Clone(), nil
}

func (r *memoryOrderRepo) Update(_ context.Context, o *domain.Order) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.orders[o.OrderID] = o.Clone()
	if o.ProviderCheckoutID != "" {
		r.s.providerCheckout[o.Provider+"|"+o.ProviderCheckoutID] = o.OrderID
	}
	return nil
}

func (r *memoryOrderRepo) ListByApp(_ context.Context, filter domain.OrderListFilter) ([]*domain.Order, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	var out []*domain.Order
	for _, o := range r.s.orders {
		if o.AppID != filter.AppID {
			continue
		}
		if filter.UserID != "" && o.UserID != filter.UserID {
			continue
		}
		if filter.Status != "" && o.Status != filter.Status {
			continue
		}
		out = append(out, o.Clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type memoryWebhookRepo struct{ s *MemoryStore }

func (r *memoryWebhookRepo) Create(_ context.Context, e *domain.WebhookEvent) (bool, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	id := e.EventDocID
	if id == "" {
		id = domain.WebhookEventDocID(e.Provider, e.EventID)
		e.EventDocID = id
	}
	if _, ok := r.s.webhooks[id]; ok {
		return false, nil
	}
	r.s.webhooks[id] = e.Clone()
	return true, nil
}

func (r *memoryWebhookRepo) Get(_ context.Context, provider, eventID string) (*domain.WebhookEvent, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	e, ok := r.s.webhooks[domain.WebhookEventDocID(provider, eventID)]
	if !ok {
		return nil, nil
	}
	return e.Clone(), nil
}
