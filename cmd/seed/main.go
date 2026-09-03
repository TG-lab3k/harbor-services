package main

import (
	"context"
	"log"
	"time"

	authapp "github.com/okok/harbor-services/internal/auth/application"
	authdomain "github.com/okok/harbor-services/internal/auth/domain"
	authinfra "github.com/okok/harbor-services/internal/auth/infrastructure"
	"github.com/okok/harbor-services/internal/platform/config"
	"github.com/okok/harbor-services/internal/platform/firestorex"
	"github.com/okok/harbor-services/internal/platform/seed"
	"github.com/okok/harbor-services/internal/shared/cache"
	"github.com/okok/harbor-services/internal/shared/crypto"
	sharedjwt "github.com/okok/harbor-services/internal/shared/jwt"
	tenantapp "github.com/okok/harbor-services/internal/tenant/application"
	tenantdomain "github.com/okok/harbor-services/internal/tenant/domain"
	tenantinfra "github.com/okok/harbor-services/internal/tenant/infrastructure"
)

type tenantGate struct {
	svc *tenantapp.AppService
}

func (g *tenantGate) RequireActiveApp(ctx context.Context, appID string) (*tenantdomain.App, error) {
	return g.svc.RequireActiveApp(ctx, appID)
}

func (g *tenantGate) GetApp(ctx context.Context, appID string) (*tenantdomain.App, error) {
	return g.svc.GetApp(ctx, appID)
}

func (g *tenantGate) VerifyAppSecret(ctx context.Context, appID, secret string) (*tenantdomain.App, error) {
	return g.svc.VerifyAppSecret(ctx, appID, secret)
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.DBBackend == "memory" {
		log.Println("WARNING: seed with DB_BACKEND=memory does not persist; use SEED_ON_START=true on the API process instead")
	}

	hasher := crypto.NewBcryptHasher(cfg.BcryptCost)
	encryptor, err := crypto.NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		log.Fatalf("encryptor: %v", err)
	}
	jwtSvc, err := sharedjwt.NewService(sharedjwt.Options{
		Issuer:            cfg.JWTIssuer,
		AccessTTL:         cfg.AccessTokenTTL,
		RefreshTTL:        cfg.RefreshTokenTTL,
		PrivateKeyPEM:     cfg.RSAPrivateKeyPEM,
		PublicKeyPEM:      cfg.RSAPublicKeyPEM,
		AllowEphemeralKey: cfg.DBBackend == "memory",
	})
	if err != nil {
		log.Fatalf("jwt: %v", err)
	}

	var (
		appRepo        tenantdomain.AppRepository
		authConfigRepo authdomain.AuthConfigRepository
		userRepo       authdomain.UserRepository
		oauthRepo      authdomain.OAuthAccountRepository
		refreshRepo    authdomain.RefreshTokenRepository
		verifyRepo     authdomain.VerificationTokenRepository
	)

	switch cfg.DBBackend {
	case "memory":
		appRepo = tenantinfra.NewMemoryAppRepository()
		store := authinfra.NewMemoryStore()
		authConfigRepo = store.AuthConfigRepo()
		userRepo = store.UserRepo()
		oauthRepo = store.OAuthRepo()
		refreshRepo = store.RefreshRepo()
		verifyRepo = store.VerifyRepo()
	case "firestore":
		if cfg.GCPProjectID == "" {
			log.Fatal("GCP_PROJECT_ID is required when DB_BACKEND=firestore")
		}
		fsClient, err := firestorex.NewClient(context.Background(), cfg.GCPProjectID)
		if err != nil {
			log.Fatalf("firestore: %v", err)
		}
		defer fsClient.Close()
		appRepo = tenantinfra.NewFirestoreAppRepository(fsClient)
		store := authinfra.NewFirestoreStore(fsClient)
		authConfigRepo = store.AuthConfigRepo()
		userRepo = store.UserRepo()
		oauthRepo = store.OAuthRepo()
		refreshRepo = store.RefreshRepo()
		verifyRepo = store.VerifyRepo()
	default:
		log.Fatalf("unknown DB_BACKEND %q (want memory or firestore)", cfg.DBBackend)
	}

	cachedApps := tenantinfra.NewCachedAppRepository(appRepo, cache.NewInMemoryCache(), cfg.AppCacheTTLSec)

	gate := &tenantGate{}
	authSvc := authapp.NewService(authapp.Deps{
		Apps:           gate,
		AuthConfigRepo: authConfigRepo,
		UserRepo:       userRepo,
		OAuthRepo:      oauthRepo,
		RefreshRepo:    refreshRepo,
		VerifyRepo:     verifyRepo,
		Hasher:         hasher,
		Encryptor:      encryptor,
		JWT:            jwtSvc,
	})
	tenantSvc := tenantapp.NewAppService(cachedApps, hasher, authSvc)
	gate.svc = tenantSvc

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := seed.Run(ctx, cfg, tenantSvc, authSvc, userRepo, hasher, cfg.DBBackend); err != nil {
		log.Fatalf("seed: %v", err)
	}
	log.Println("seed complete")
}
