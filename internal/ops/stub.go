package ops

import (
	"context"
	"time"
)

// Config is the stub ops configuration document.
type Config struct {
	AppID     string                 `json:"app_id"`
	Enabled   bool                   `json:"enabled"`
	Entries   map[string]interface{} `json:"entries"`
	UpdatedAt *time.Time             `json:"updated_at"`
}

// ConfigService is the Admin-facing ops config contract.
type ConfigService interface {
	Get(ctx context.Context, appID string) (*Config, error)
	Put(ctx context.Context, appID string, cfg *Config) (*Config, error)
}

// StubConfigService returns enabled:false skeletons.
type StubConfigService struct{}

func NewStubConfigService() *StubConfigService {
	return &StubConfigService{}
}

func (s *StubConfigService) Get(_ context.Context, appID string) (*Config, error) {
	return &Config{
		AppID:     appID,
		Enabled:   false,
		Entries:   map[string]interface{}{},
		UpdatedAt: nil,
	}, nil
}

func (s *StubConfigService) Put(_ context.Context, appID string, _ *Config) (*Config, error) {
	return s.Get(context.Background(), appID)
}
