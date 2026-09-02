package application

import (
	"context"
	"time"

	"github.com/okok/harbor-services/internal/platform/apperr"
	"github.com/okok/harbor-services/internal/shared/crypto"
	"github.com/okok/harbor-services/internal/shared/idgen"
	"github.com/okok/harbor-services/internal/tenant/domain"
)

// AppService provides tenant use cases shared across Auth/Admin/Billing.
type AppService struct {
	repo   domain.AppRepository
	hasher crypto.PasswordHasher
	hooks  []domain.AppLifecycleHook
}

func NewAppService(repo domain.AppRepository, hasher crypto.PasswordHasher, hooks ...domain.AppLifecycleHook) *AppService {
	return &AppService{repo: repo, hasher: hasher, hooks: hooks}
}

// RequireActiveApp returns the app if it exists and is active; otherwise AppNotFound (2001).
func (s *AppService) RequireActiveApp(ctx context.Context, appID string) (*domain.App, error) {
	return RequireActiveApp(ctx, s.repo, appID)
}

// RequireActiveApp is a package-level helper for callers that only have a repository.
func RequireActiveApp(ctx context.Context, repo domain.AppRepository, appID string) (*domain.App, error) {
	app, err := repo.GetByID(ctx, appID)
	if err != nil {
		return nil, err
	}
	if app == nil || !app.IsActive() {
		return nil, apperr.AppNotFound("")
	}
	return app, nil
}

// VerifyAppSecret checks active status and bcrypt secret; failure → InvalidAppSecret (2002).
func (s *AppService) VerifyAppSecret(ctx context.Context, appID, secret string) (*domain.App, error) {
	return VerifyAppSecret(ctx, s.repo, s.hasher, appID, secret)
}

func VerifyAppSecret(ctx context.Context, repo domain.AppRepository, hasher crypto.PasswordHasher, appID, secret string) (*domain.App, error) {
	app, err := RequireActiveApp(ctx, repo, appID)
	if err != nil {
		// map not-found to invalid secret for timing/side-channel friendliness on secret path
		if he, ok := apperr.AsHarborError(err); ok && he.Code == apperr.CodeAppNotFound {
			return nil, apperr.InvalidAppSecret("")
		}
		return nil, err
	}
	if !hasher.Verify(app.AppSecretHash, secret) {
		return nil, apperr.InvalidAppSecret("")
	}
	return app, nil
}

// CreateAppInput is the Admin create payload.
type CreateAppInput struct {
	AppName      string
	RedirectURIs []string
	Settings     map[string]any
	// FixedAppID optionally forces app_id (e.g. harborAdmin seed).
	FixedAppID string
}

// CreateAppResult includes the one-time plaintext secret.
type CreateAppResult struct {
	App       *domain.App
	AppSecret string
}

func (s *AppService) CreateApp(ctx context.Context, in CreateAppInput) (*CreateAppResult, error) {
	if in.AppName == "" {
		return nil, apperr.Validation("app_name is required")
	}
	appID := in.FixedAppID
	if appID == "" {
		appID = idgen.NewAppID()
	}
	secret := idgen.NewSecret(32)
	hash, err := s.hasher.Hash(secret)
	if err != nil {
		return nil, apperr.Internal("failed to hash app secret")
	}
	now := time.Now().UTC()
	settings := in.Settings
	if settings == nil {
		settings = map[string]any{}
	}
	uris := in.RedirectURIs
	if uris == nil {
		uris = []string{}
	}
	app := &domain.App{
		AppID:         appID,
		AppSecretHash: hash,
		AppName:       in.AppName,
		RedirectURIs:  uris,
		Status:        domain.AppStatusActive,
		Settings:      settings,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.repo.Create(ctx, app); err != nil {
		return nil, err
	}
	for _, h := range s.hooks {
		if err := h.OnAppCreated(ctx, app); err != nil {
			return nil, err
		}
	}
	return &CreateAppResult{App: app.Clone(), AppSecret: secret}, nil
}

func (s *AppService) ListApps(ctx context.Context, filter domain.ListAppsFilter) ([]*domain.App, error) {
	return s.repo.List(ctx, filter)
}

func (s *AppService) GetApp(ctx context.Context, appID string) (*domain.App, error) {
	app, err := s.repo.GetByID(ctx, appID)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, apperr.AppNotFound("")
	}
	return app, nil
}

// UpdateAppInput updates mutable App fields. Settings is whole-object replace when non-nil.
type UpdateAppInput struct {
	AppName      *string
	RedirectURIs []string
	Settings     map[string]any
	Status       *domain.AppStatus
	SetURIs      bool // true when RedirectURIs should replace (even if empty)
	SetSettings  bool
}

func (s *AppService) UpdateApp(ctx context.Context, appID string, in UpdateAppInput) (*domain.App, error) {
	app, err := s.repo.GetByID(ctx, appID)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, apperr.AppNotFound("")
	}
	if in.AppName != nil {
		if *in.AppName == "" {
			return nil, apperr.Validation("app_name is required")
		}
		app.AppName = *in.AppName
	}
	if in.SetURIs {
		if in.RedirectURIs == nil {
			app.RedirectURIs = []string{}
		} else {
			app.RedirectURIs = append([]string(nil), in.RedirectURIs...)
		}
	}
	if in.SetSettings {
		if in.Settings == nil {
			app.Settings = map[string]any{}
		} else {
			app.Settings = in.Settings
		}
	}
	if in.Status != nil {
		app.Status = *in.Status
	}
	app.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, app); err != nil {
		return nil, err
	}
	return app.Clone(), nil
}

type RotateSecretResult struct {
	App       *domain.App
	AppSecret string
}

func (s *AppService) RotateAppSecret(ctx context.Context, appID string) (*RotateSecretResult, error) {
	app, err := s.repo.GetByID(ctx, appID)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, apperr.AppNotFound("")
	}
	secret := idgen.NewSecret(32)
	hash, err := s.hasher.Hash(secret)
	if err != nil {
		return nil, apperr.Internal("failed to hash app secret")
	}
	app.AppSecretHash = hash
	app.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, app); err != nil {
		return nil, err
	}
	return &RotateSecretResult{App: app.Clone(), AppSecret: secret}, nil
}

func (s *AppService) DisableApp(ctx context.Context, appID string) error {
	app, err := s.repo.GetByID(ctx, appID)
	if err != nil {
		return err
	}
	if app == nil {
		return apperr.AppNotFound("")
	}
	if err := s.repo.SoftDisable(ctx, appID); err != nil {
		return err
	}
	for _, h := range s.hooks {
		if err := h.OnAppDisabled(ctx, appID); err != nil {
			return err
		}
	}
	return nil
}
