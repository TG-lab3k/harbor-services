package domain

import "context"

// ListAppsFilter controls App listing.
type ListAppsFilter struct {
	IncludeDisabled bool
}

// AppRepository persists App primary data.
type AppRepository interface {
	Create(ctx context.Context, app *App) error
	GetByID(ctx context.Context, appID string) (*App, error)
	List(ctx context.Context, filter ListAppsFilter) ([]*App, error)
	Update(ctx context.Context, app *App) error
	// SoftDisable sets status=disabled.
	SoftDisable(ctx context.Context, appID string) error
}

// AppLifecycleHook lets Auth/Billing/Ops react to App lifecycle.
type AppLifecycleHook interface {
	OnAppCreated(ctx context.Context, app *App) error
	OnAppDisabled(ctx context.Context, appID string) error
}
