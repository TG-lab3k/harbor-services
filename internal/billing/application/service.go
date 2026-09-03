package application

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/okok/harbor-services/internal/billing"
	"github.com/okok/harbor-services/internal/billing/domain"
	"github.com/okok/harbor-services/internal/platform/apperr"
	"github.com/okok/harbor-services/internal/shared/crypto"
	"github.com/okok/harbor-services/internal/shared/idgen"
	tenantdomain "github.com/okok/harbor-services/internal/tenant/domain"
)

// AppGate is the Tenant surface Billing depends on.
type AppGate interface {
	RequireActiveApp(ctx context.Context, appID string) (*tenantdomain.App, error)
	GetApp(ctx context.Context, appID string) (*tenantdomain.App, error)
	VerifyAppSecret(ctx context.Context, appID, secret string) (*tenantdomain.App, error)
}

// Service orchestrates billing use cases.
type Service struct {
	apps     AppGate
	configs  domain.ConfigRepository
	products domain.ProductRepository
	orders   domain.OrderRepository
	webhooks domain.WebhookEventRepository
	registry domain.ProviderRegistry
	encryptor *crypto.Encryptor
}

// Deps wires Service dependencies.
type Deps struct {
	Apps      AppGate
	Configs   domain.ConfigRepository
	Products  domain.ProductRepository
	Orders    domain.OrderRepository
	Webhooks  domain.WebhookEventRepository
	Registry  domain.ProviderRegistry
	Encryptor *crypto.Encryptor
}

func NewService(d Deps) *Service {
	return &Service{
		apps:      d.Apps,
		configs:   d.Configs,
		products:  d.Products,
		orders:    d.Orders,
		webhooks:  d.Webhooks,
		registry:  d.Registry,
		encryptor: d.Encryptor,
	}
}

// Ensure ConfigService + AppLifecycleHook.
var (
	_ billing.ConfigService          = (*Service)(nil)
	_ tenantdomain.AppLifecycleHook  = (*Service)(nil)
)

func (s *Service) OnAppCreated(ctx context.Context, app *tenantdomain.App) error {
	if app == nil {
		return nil
	}
	existing, err := s.configs.Get(ctx, app.AppID)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	cfg := &domain.BillingConfig{
		AppID:     app.AppID,
		Enabled:   false,
		TestMode:  true,
		Providers: map[string]*domain.ProviderCredentials{},
	}
	return s.configs.Upsert(ctx, cfg)
}

func (s *Service) OnAppDisabled(ctx context.Context, appID string) error {
	_ = ctx
	_ = appID
	return nil
}

// --- Admin Config ---

func (s *Service) Get(ctx context.Context, appID string) (*billing.Config, error) {
	if _, err := s.apps.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	cfg, err := s.configs.Get(ctx, appID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return emptyPublicConfig(appID), nil
	}
	return toPublicConfig(cfg), nil
}

func (s *Service) Put(ctx context.Context, appID string, in *billing.Config) (*billing.Config, error) {
	if _, err := s.apps.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	if in == nil {
		return nil, apperr.Validation("body required")
	}

	existing, err := s.configs.Get(ctx, appID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		existing = &domain.BillingConfig{
			AppID:     appID,
			Providers: map[string]*domain.ProviderCredentials{},
		}
	}

	existing.Enabled = in.Enabled
	existing.TestMode = in.TestMode

	if in.DefaultProvider != nil {
		dp := strings.TrimSpace(*in.DefaultProvider)
		if dp == "" {
			existing.DefaultProvider = nil
		} else {
			if !domain.IsKnownProvider(dp) {
				return nil, apperr.BillingProvider("unknown default_provider")
			}
			existing.DefaultProvider = &dp
		}
	}

	if in.Providers != nil {
		if existing.Providers == nil {
			existing.Providers = map[string]*domain.ProviderCredentials{}
		}
		for name, pub := range in.Providers {
			name = strings.TrimSpace(name)
			if !domain.IsKnownProvider(name) {
				return nil, apperr.BillingProvider("unknown provider: " + name)
			}
			cur := existing.Providers[name]
			if cur == nil {
				cur = &domain.ProviderCredentials{}
			}
			cur.Enabled = pub.Enabled
			if pub.AccountID != "" {
				cur.AccountID = pub.AccountID
			}
			if pub.Extra != nil {
				cur.Extra = pub.Extra
			}
			if pub.APIKey != "" {
				enc, err := s.encryptor.Encrypt(pub.APIKey)
				if err != nil {
					return nil, apperr.Internal("failed to encrypt api_key")
				}
				cur.APIKeyEncrypted = enc
			}
			if pub.WebhookSecret != "" {
				enc, err := s.encryptor.Encrypt(pub.WebhookSecret)
				if err != nil {
					return nil, apperr.Internal("failed to encrypt webhook_secret")
				}
				cur.WebhookSecretEncrypted = enc
			}
			existing.Providers[name] = cur
		}
	}

	if existing.DefaultProvider != nil {
		dp := *existing.DefaultProvider
		pc := existing.Provider(dp)
		if pc == nil || !pc.Enabled {
			return nil, apperr.BillingProvider("default_provider must be configured and enabled")
		}
		if dp != domain.ProviderMock && !pc.APIKeySet() {
			return nil, apperr.BillingProvider("default_provider api_key is required")
		}
	}

	now := time.Now().UTC()
	existing.UpdatedAt = &now
	existing.AppID = appID
	if err := s.configs.Upsert(ctx, existing); err != nil {
		return nil, err
	}
	return toPublicConfig(existing), nil
}

func emptyPublicConfig(appID string) *billing.Config {
	return &billing.Config{
		AppID:           appID,
		Enabled:         false,
		DefaultProvider: nil,
		TestMode:        true,
		Providers:       map[string]billing.ProviderPublic{},
		UpdatedAt:       nil,
	}
}

func toPublicConfig(cfg *domain.BillingConfig) *billing.Config {
	out := &billing.Config{
		AppID:           cfg.AppID,
		Enabled:         cfg.Enabled,
		DefaultProvider: cfg.DefaultProvider,
		TestMode:        cfg.TestMode,
		UpdatedAt:       cfg.UpdatedAt,
		Providers:       map[string]billing.ProviderPublic{},
	}
	for name, p := range cfg.Providers {
		if p == nil {
			continue
		}
		out.Providers[name] = billing.ProviderPublic{
			Enabled:          p.Enabled,
			AccountID:        p.AccountID,
			APIKeySet:        p.APIKeySet(),
			WebhookSecretSet: p.WebhookSecretSet(),
			Extra:            p.Extra,
		}
	}
	return out
}

func (s *Service) loadConfig(ctx context.Context, appID string) (*domain.BillingConfig, error) {
	cfg, err := s.configs.Get(ctx, appID)
	if err != nil {
		return nil, err
	}
	if cfg == nil || !cfg.Enabled || cfg.DefaultProvider == nil || *cfg.DefaultProvider == "" {
		return nil, apperr.BillingNotEnabled("")
	}
	return cfg, nil
}

func (s *Service) decryptCreds(cfg *domain.BillingConfig, provider string) (domain.ProviderCredentialsPlain, error) {
	pc := cfg.Provider(provider)
	if pc == nil || !pc.Enabled {
		return domain.ProviderCredentialsPlain{}, apperr.BillingProvider("")
	}
	plain := domain.ProviderCredentialsPlain{
		AccountID: pc.AccountID,
		Extra:     pc.Extra,
		TestMode:  cfg.TestMode,
	}
	if pc.APIKeyEncrypted != "" {
		k, err := s.encryptor.Decrypt(pc.APIKeyEncrypted)
		if err != nil {
			return plain, apperr.Internal("failed to decrypt api_key")
		}
		plain.APIKey = k
	}
	if pc.WebhookSecretEncrypted != "" {
		k, err := s.encryptor.Decrypt(pc.WebhookSecretEncrypted)
		if err != nil {
			return plain, apperr.Internal("failed to decrypt webhook_secret")
		}
		plain.WebhookSecret = k
	}
	return plain, nil
}

// --- Products ---

type CreateProductInput struct {
	Name             string
	Description      string
	Type             domain.ProductType
	ProviderPriceIDs map[string]string
}

func (s *Service) CreateProduct(ctx context.Context, appID string, in CreateProductInput) (*domain.Product, error) {
	if _, err := s.apps.RequireActiveApp(ctx, appID); err != nil {
		return nil, mapAppInactive(err)
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, apperr.Validation("name is required")
	}
	typ := in.Type
	if typ == "" {
		typ = domain.ProductTypeOneTime
	}
	if typ != domain.ProductTypeOneTime && typ != domain.ProductTypeSubscription {
		return nil, apperr.Validation("invalid product type")
	}
	now := time.Now().UTC()
	p := &domain.Product{
		ProductID:        idgen.NewProductID(),
		AppID:            appID,
		Name:             strings.TrimSpace(in.Name),
		Description:      in.Description,
		Type:             typ,
		ProviderPriceIDs: in.ProviderPriceIDs,
		Status:           domain.ProductStatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if p.ProviderPriceIDs == nil {
		p.ProviderPriceIDs = map[string]string{}
	}
	if err := s.products.Create(ctx, p); err != nil {
		return nil, err
	}
	return p.Clone(), nil
}

type UpdateProductInput struct {
	Name             *string
	Description      *string
	Type             *domain.ProductType
	ProviderPriceIDs map[string]string
	Status           *domain.ProductStatus
}

func (s *Service) UpdateProduct(ctx context.Context, appID, productID string, in UpdateProductInput) (*domain.Product, error) {
	if _, err := s.apps.RequireActiveApp(ctx, appID); err != nil {
		return nil, mapAppInactive(err)
	}
	p, err := s.products.GetByID(ctx, productID)
	if err != nil {
		return nil, err
	}
	if p == nil || p.AppID != appID {
		return nil, apperr.BillingProduct("")
	}
	if in.Name != nil {
		p.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		p.Description = *in.Description
	}
	if in.Type != nil {
		p.Type = *in.Type
	}
	if in.ProviderPriceIDs != nil {
		p.ProviderPriceIDs = in.ProviderPriceIDs
	}
	if in.Status != nil {
		p.Status = *in.Status
	}
	p.UpdatedAt = time.Now().UTC()
	if err := s.products.Update(ctx, p); err != nil {
		return nil, err
	}
	return p.Clone(), nil
}

func (s *Service) GetProduct(ctx context.Context, appID, productID string) (*domain.Product, error) {
	p, err := s.products.GetByID(ctx, productID)
	if err != nil {
		return nil, err
	}
	if p == nil || p.AppID != appID {
		return nil, apperr.BillingProduct("")
	}
	return p.Clone(), nil
}

func (s *Service) ListProducts(ctx context.Context, appID string, includeArchived bool) ([]*domain.Product, error) {
	return s.products.ListByApp(ctx, appID, includeArchived)
}

// --- Checkout / Orders ---

type CreateCheckoutInput struct {
	ProductID         string
	ProviderProductID string
	SuccessURL        string
	CancelURL         string
	UserID            string
	CustomerEmail     string
	Provider          string
	Metadata          map[string]any
	IdempotencyKey    string
}

type CreateCheckoutResult struct {
	CheckoutID  string             `json:"checkout_id"`
	OrderID     string             `json:"order_id"`
	CheckoutURL string             `json:"checkout_url"`
	Provider    string             `json:"provider"`
	Status      domain.OrderStatus `json:"status"`
}

func (s *Service) CreateCheckout(ctx context.Context, appID string, in CreateCheckoutInput) (*CreateCheckoutResult, error) {
	if _, err := s.apps.RequireActiveApp(ctx, appID); err != nil {
		return nil, mapAppInactive(err)
	}
	if strings.TrimSpace(in.SuccessURL) == "" {
		return nil, apperr.BillingCheckoutParams("success_url is required")
	}
	if in.Metadata != nil {
		b, _ := json.Marshal(in.Metadata)
		if len(b) > 4096 {
			return nil, apperr.BillingCheckoutParams("metadata too large")
		}
	}

	if in.IdempotencyKey != "" {
		existing, err := s.orders.GetByIdempotencyKey(ctx, appID, in.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			if !checkoutParamsMatch(existing, in) {
				return nil, apperr.BillingIdempotency("")
			}
			return &CreateCheckoutResult{
				CheckoutID:  existing.CheckoutID,
				OrderID:     existing.OrderID,
				CheckoutURL: existing.CheckoutURL,
				Provider:    existing.Provider,
				Status:      existing.Status,
			}, nil
		}
	}

	cfg, err := s.loadConfig(ctx, appID)
	if err != nil {
		return nil, err
	}

	providerName := strings.TrimSpace(in.Provider)
	if providerName == "" {
		providerName = *cfg.DefaultProvider
	}
	if !domain.IsKnownProvider(providerName) {
		return nil, apperr.BillingProvider("")
	}
	prov, ok := s.registry.Get(providerName)
	if !ok {
		return nil, apperr.BillingProvider("provider adapter not registered")
	}

	providerProductID := strings.TrimSpace(in.ProviderProductID)
	productID := strings.TrimSpace(in.ProductID)
	if productID == "" && providerProductID == "" {
		return nil, apperr.BillingCheckoutParams("product_id or provider_product_id is required")
	}
	if productID != "" {
		prod, err := s.products.GetByID(ctx, productID)
		if err != nil {
			return nil, err
		}
		if prod == nil || prod.AppID != appID || prod.Status != domain.ProductStatusActive {
			return nil, apperr.BillingProduct("")
		}
		mapped, ok := prod.PriceIDFor(providerName)
		if !ok {
			return nil, apperr.BillingProduct("product not mapped to provider")
		}
		providerProductID = mapped
	}

	creds, err := s.decryptCreds(cfg, providerName)
	if err != nil {
		return nil, err
	}

	orderID := idgen.NewOrderID()
	checkoutID := idgen.NewCheckoutID()
	now := time.Now().UTC()
	order := &domain.Order{
		OrderID:           orderID,
		CheckoutID:        checkoutID,
		AppID:             appID,
		UserID:            in.UserID,
		CustomerEmail:     in.CustomerEmail,
		ProductID:         productID,
		Provider:          providerName,
		ProviderProductID: providerProductID,
		Status:            domain.OrderStatusPending,
		IdempotencyKey:    in.IdempotencyKey,
		Metadata:          in.Metadata,
		SuccessURL:        in.SuccessURL,
		CancelURL:         in.CancelURL,
		TestMode:          cfg.TestMode,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	result, err := prov.CreateCheckout(ctx, creds, domain.CreateCheckoutInput{
		OrderID:           orderID,
		CheckoutID:        checkoutID,
		ProviderProductID: providerProductID,
		SuccessURL:        in.SuccessURL,
		CancelURL:         in.CancelURL,
		CustomerEmail:     in.CustomerEmail,
		Metadata:          in.Metadata,
		RequestID:         orderID,
		TestMode:          cfg.TestMode,
	})
	if err != nil {
		return nil, err
	}
	order.CheckoutURL = result.CheckoutURL
	order.ProviderCheckoutID = result.ProviderCheckoutID
	order.ProviderOrderID = result.ProviderOrderID

	if err := s.orders.Create(ctx, order); err != nil {
		return nil, err
	}
	return &CreateCheckoutResult{
		CheckoutID:  order.CheckoutID,
		OrderID:     order.OrderID,
		CheckoutURL: order.CheckoutURL,
		Provider:    order.Provider,
		Status:      order.Status,
	}, nil
}

func checkoutParamsMatch(o *domain.Order, in CreateCheckoutInput) bool {
	if o.ProductID != strings.TrimSpace(in.ProductID) {
		return false
	}
	if in.ProviderProductID != "" && o.ProviderProductID != in.ProviderProductID {
		return false
	}
	if o.SuccessURL != in.SuccessURL {
		return false
	}
	if in.Provider != "" && o.Provider != in.Provider {
		return false
	}
	if in.UserID != "" && o.UserID != in.UserID {
		return false
	}
	return true
}

func (s *Service) GetOrder(ctx context.Context, appID, orderID string, jwtUserID string) (*domain.Order, error) {
	o, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if o == nil || o.AppID != appID {
		return nil, apperr.BillingOrderNotFound("")
	}
	if jwtUserID != "" {
		if o.UserID == "" || o.UserID != jwtUserID {
			return nil, apperr.Forbidden("order does not belong to user")
		}
	}
	return o.Clone(), nil
}

func (s *Service) ListOrders(ctx context.Context, appID string, jwtUserID string, userIDFilter string, status domain.OrderStatus, limit int) ([]*domain.Order, error) {
	filter := domain.OrderListFilter{AppID: appID, Status: status, Limit: limit}
	if jwtUserID != "" {
		filter.UserID = jwtUserID
	} else if userIDFilter != "" {
		filter.UserID = userIDFilter
	}
	return s.orders.ListByApp(ctx, filter)
}

// HandleWebhook processes an inbound provider webhook for a specific app.
func (s *Service) HandleWebhook(ctx context.Context, providerName, appID string, headers map[string]string, body []byte) error {
	cfg, err := s.configs.Get(ctx, appID)
	if err != nil {
		return err
	}
	if cfg == nil {
		return apperr.BillingProvider("billing config not found")
	}
	prov, ok := s.registry.Get(providerName)
	if !ok {
		return apperr.BillingProvider("provider adapter not registered")
	}
	creds, err := s.decryptCreds(cfg, providerName)
	if err != nil {
		return err
	}
	parsed, err := prov.VerifyAndParseWebhook(creds, headers, body)
	if err != nil {
		return err
	}
	if parsed.EventID == "" {
		return apperr.BillingWebhookSig("missing event id")
	}

	ev := &domain.WebhookEvent{
		EventDocID: domain.WebhookEventDocID(providerName, parsed.EventID),
		Provider:   providerName,
		EventID:    parsed.EventID,
		AppID:      appID,
		EventType:  parsed.EventType,
		Payload:    parsed.Raw,
		CreatedAt:  time.Now().UTC(),
	}
	created, err := s.webhooks.Create(ctx, ev)
	if err != nil {
		return err
	}
	if !created {
		return nil // idempotent no-op
	}

	var order *domain.Order
	if parsed.ProviderCheckoutID != "" {
		order, err = s.orders.GetByProviderCheckoutID(ctx, providerName, parsed.ProviderCheckoutID)
		if err != nil {
			return err
		}
	}
	if order == nil && parsed.Raw != nil {
		if rid, ok := parsed.Raw["request_id"].(string); ok && rid != "" {
			order, err = s.orders.GetByID(ctx, rid)
			if err != nil {
				return err
			}
			if order != nil && order.AppID != appID {
				order = nil
			}
		}
	}
	if order == nil {
		return nil
	}
	ev.OrderID = order.OrderID
	_ = ev

	if parsed.Status == "" {
		return nil
	}
	if order.Status == domain.OrderStatusPaid && parsed.Status == domain.OrderStatusPaid {
		return nil
	}
	order.Status = parsed.Status
	if parsed.ProviderOrderID != "" {
		order.ProviderOrderID = parsed.ProviderOrderID
	}
	if parsed.CustomerEmail != "" && order.CustomerEmail == "" {
		order.CustomerEmail = parsed.CustomerEmail
	}
	now := time.Now().UTC()
	order.UpdatedAt = now
	if parsed.Status == domain.OrderStatusPaid {
		order.PaidAt = &now
	}
	return s.orders.Update(ctx, order)
}

func mapAppInactive(err error) error {
	if he, ok := apperr.AsHarborError(err); ok && he.Code == apperr.CodeAppNotFound {
		return apperr.BillingAppInactive("")
	}
	return err
}

// VerifySecret exposes Tenant App Secret verification for HTTP middleware.
func (s *Service) VerifySecret(ctx context.Context, appID, secret string) (*tenantdomain.App, error) {
	return s.apps.VerifyAppSecret(ctx, appID, secret)
}
