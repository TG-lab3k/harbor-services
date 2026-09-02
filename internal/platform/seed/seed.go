package seed

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	authapp "github.com/okok/harbor-services/internal/auth/application"
	"github.com/okok/harbor-services/internal/auth/domain"
	"github.com/okok/harbor-services/internal/platform/apperr"
	"github.com/okok/harbor-services/internal/platform/config"
	"github.com/okok/harbor-services/internal/shared/crypto"
	"github.com/okok/harbor-services/internal/shared/idgen"
	tenantapp "github.com/okok/harbor-services/internal/tenant/application"
)

// Run creates harborAdmin app + admin users. Idempotent.
func Run(
	ctx context.Context,
	cfg *config.Config,
	tenant *tenantapp.AppService,
	auth *authapp.Service,
	userRepo domain.UserRepository,
	hasher crypto.PasswordHasher,
	backend string,
) error {
	if backend == "memory" {
		log.Println("[seed] NOTE: memory backend — seed data lives only in this process")
	}
	password := os.Getenv("ADMIN_PASSWORD")
	if password == "" {
		password = "AdminPass123"
		log.Println("[seed] ADMIN_PASSWORD not set; using default AdminPass123 (dev only)")
	}
	if err := authapp.ValidatePassword(password); err != nil {
		return fmt.Errorf("ADMIN_PASSWORD: %w", err)
	}
	if len(cfg.AdminEmails) == 0 {
		log.Println("[seed] ADMIN_EMAILS empty; creating app only (Admin API remains fail-closed)")
	}

	existing, err := tenant.GetApp(ctx, cfg.AdminAppID)
	if err != nil {
		if he, ok := apperr.AsHarborError(err); !ok || he.Code != apperr.CodeAppNotFound {
			return err
		}
		existing = nil
	}
	if existing == nil {
		_, err := tenant.CreateApp(ctx, tenantapp.CreateAppInput{
			AppName:      "Harbor Admin",
			FixedAppID:   cfg.AdminAppID,
			RedirectURIs: []string{},
			Settings:     map[string]any{"allow_register": false},
		})
		if err != nil {
			return fmt.Errorf("create admin app: %w", err)
		}
		log.Printf("[seed] created app %s", cfg.AdminAppID)
	} else {
		log.Printf("[seed] app %s already exists", cfg.AdminAppID)
	}

	if err := auth.EnsureAuthConfig(ctx, cfg.AdminAppID); err != nil {
		return fmt.Errorf("ensure auth config: %w", err)
	}

	now := time.Now().UTC()
	for _, email := range cfg.AdminEmails {
		email = strings.ToLower(strings.TrimSpace(email))
		if email == "" {
			continue
		}
		u, err := userRepo.GetByEmail(ctx, cfg.AdminAppID, email)
		if err != nil {
			return err
		}
		if u != nil {
			log.Printf("[seed] admin user %s already exists", email)
			continue
		}
		hash, err := hasher.Hash(password)
		if err != nil {
			return err
		}
		user := &domain.User{
			UserID:        idgen.NewUserID(),
			AppID:         cfg.AdminAppID,
			Email:         email,
			EmailVerified: true,
			PasswordHash:  hash,
			Nickname:      "Admin",
			Status:        domain.UserStatusActive,
			TokenVersion:  1,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := userRepo.Create(ctx, user); err != nil {
			return fmt.Errorf("create admin user %s: %w", email, err)
		}
		log.Printf("[seed] created admin user %s", email)
	}
	return nil
}
