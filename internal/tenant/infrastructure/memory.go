package infrastructure

import (
	"context"
	"sync"
	"time"

	"github.com/okok/harbor-services/internal/platform/apperr"
	"github.com/okok/harbor-services/internal/tenant/domain"
)

// MemoryAppRepository is an in-memory AppRepository for DB_BACKEND=memory.
type MemoryAppRepository struct {
	mu   sync.RWMutex
	apps map[string]*domain.App
}

func NewMemoryAppRepository() *MemoryAppRepository {
	return &MemoryAppRepository{apps: make(map[string]*domain.App)}
}

func (r *MemoryAppRepository) Create(ctx context.Context, app *domain.App) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.apps[app.AppID]; exists {
		return apperr.Validation("app already exists")
	}
	r.apps[app.AppID] = app.Clone()
	return nil
}

func (r *MemoryAppRepository) GetByID(ctx context.Context, appID string) (*domain.App, error) {
	_ = ctx
	r.mu.RLock()
	defer r.mu.RUnlock()
	app, ok := r.apps[appID]
	if !ok {
		return nil, nil
	}
	return app.Clone(), nil
}

func (r *MemoryAppRepository) List(ctx context.Context, filter domain.ListAppsFilter) ([]*domain.App, error) {
	_ = ctx
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*domain.App, 0, len(r.apps))
	for _, app := range r.apps {
		if !filter.IncludeDisabled && app.Status != domain.AppStatusActive {
			continue
		}
		out = append(out, app.Clone())
	}
	return out, nil
}

func (r *MemoryAppRepository) Update(ctx context.Context, app *domain.App) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.apps[app.AppID]; !ok {
		return apperr.AppNotFound("")
	}
	r.apps[app.AppID] = app.Clone()
	return nil
}

func (r *MemoryAppRepository) SoftDisable(ctx context.Context, appID string) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	app, ok := r.apps[appID]
	if !ok {
		return apperr.AppNotFound("")
	}
	cloned := app.Clone()
	cloned.Status = domain.AppStatusDisabled
	cloned.UpdatedAt = time.Now().UTC()
	r.apps[appID] = cloned
	return nil
}
