package billing

import (
	"context"
	"time"
)

// Config is the Admin-facing billing configuration DTO (secrets masked on GET).
type Config struct {
	AppID           string                    `json:"app_id"`
	Enabled         bool                      `json:"enabled"`
	DefaultProvider *string                   `json:"default_provider"`
	TestMode        bool                      `json:"test_mode"`
	Providers       map[string]ProviderPublic `json:"providers"`
	UpdatedAt       *time.Time                `json:"updated_at"`
}

// ProviderPublic is the masked provider entry for Admin responses / PUT input.
type ProviderPublic struct {
	Enabled           bool           `json:"enabled"`
	AccountID         string         `json:"account_id,omitempty"`
	APIKey            string         `json:"api_key,omitempty"`            // write-only; never returned on GET
	WebhookSecret     string         `json:"webhook_secret,omitempty"`     // write-only
	APIKeySet         bool           `json:"api_key_set"`
	WebhookSecretSet  bool           `json:"webhook_secret_set"`
	Extra             map[string]any `json:"extra,omitempty"`
}

// ConfigService is the Admin-facing billing config contract.
type ConfigService interface {
	Get(ctx context.Context, appID string) (*Config, error)
	Put(ctx context.Context, appID string, cfg *Config) (*Config, error)
}
