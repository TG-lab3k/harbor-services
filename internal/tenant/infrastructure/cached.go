package infrastructure

import (
	"context"
	"fmt"
	"time"

	"github.com/okok/harbor-services/internal/shared/cache"
	"github.com/okok/harbor-services/internal/tenant/domain"
)

// CachedAppRepository caches active apps by app:{id} with TTL.
// Disabled / miss are not cached so disable takes effect quickly.
type CachedAppRepository struct {
	inner domain.AppRepository
	cache *cache.InMemoryCache
	ttl   time.Duration
}

func NewCachedAppRepository(inner domain.AppRepository, c *cache.InMemoryCache, ttlSec int) *CachedAppRepository {
	if ttlSec <= 0 {
		ttlSec = 300
	}
	if c == nil {
		c = cache.NewInMemoryCache()
	}
	return &CachedAppRepository{
		inner: inner,
		cache: c,
		ttl:   time.Duration(ttlSec) * time.Second,
	}
}

func cacheKey(appID string) string {
	return fmt.Sprintf("app:%s", appID)
}

func (r *CachedAppRepository) Create(ctx context.Context, app *domain.App) error {
	if err := r.inner.Create(ctx, app); err != nil {
		return err
	}
	if app.IsActive() {
		r.cache.Set(cacheKey(app.AppID), app.Clone(), r.ttl)
	}
	return nil
}

func (r *CachedAppRepository) GetByID(ctx context.Context, appID string) (*domain.App, error) {
	key := cacheKey(appID)
	if v, ok := r.cache.Get(key); ok {
		if app, ok := v.(*domain.App); ok && app.IsActive() {
			return app.Clone(), nil
		}
		r.cache.Delete(key)
	}
	app, err := r.inner.GetByID(ctx, appID)
	if err != nil {
		return nil, err
	}
	if app != nil && app.IsActive() {
		r.cache.Set(key, app.Clone(), r.ttl)
	}
	return app, nil
}

func (r *CachedAppRepository) List(ctx context.Context, filter domain.ListAppsFilter) ([]*domain.App, error) {
	return r.inner.List(ctx, filter)
}

func (r *CachedAppRepository) Update(ctx context.Context, app *domain.App) error {
	if err := r.inner.Update(ctx, app); err != nil {
		return err
	}
	r.cache.Delete(cacheKey(app.AppID))
	if app.IsActive() {
		r.cache.Set(cacheKey(app.AppID), app.Clone(), r.ttl)
	}
	return nil
}

func (r *CachedAppRepository) SoftDisable(ctx context.Context, appID string) error {
	if err := r.inner.SoftDisable(ctx, appID); err != nil {
		return err
	}
	r.cache.Delete(cacheKey(appID))
	return nil
}

// Invalidate drops a cache entry (e.g. after secret rotate via Update).
func (r *CachedAppRepository) Invalidate(appID string) {
	r.cache.Delete(cacheKey(appID))
}
