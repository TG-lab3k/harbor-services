package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/okok/harbor-services/internal/auth/domain"
	"github.com/okok/harbor-services/internal/platform/apperr"
	"github.com/okok/harbor-services/internal/shared/crypto"
	"github.com/okok/harbor-services/internal/shared/idgen"
	sharedjwt "github.com/okok/harbor-services/internal/shared/jwt"
	tenantdomain "github.com/okok/harbor-services/internal/tenant/domain"
)

// AppGate is the tenant contract Auth needs (RequireActiveApp / GetApp / VerifyAppSecret).
type AppGate interface {
	RequireActiveApp(ctx context.Context, appID string) (*tenantdomain.App, error)
	GetApp(ctx context.Context, appID string) (*tenantdomain.App, error)
	VerifyAppSecret(ctx context.Context, appID, secret string) (*tenantdomain.App, error)
}

// EmailSender sends transactional emails (MVP: log stub ok).
type EmailSender interface {
	SendVerification(ctx context.Context, to, token, appID string) error
	SendPasswordReset(ctx context.Context, to, token, appID string) error
}

// LogEmailSender logs emails instead of sending.
type LogEmailSender struct{}

func (LogEmailSender) SendVerification(_ context.Context, to, token, appID string) error {
	log.Printf("[email] verification to=%s app_id=%s token=%s", to, appID, token)
	return nil
}

func (LogEmailSender) SendPasswordReset(_ context.Context, to, token, appID string) error {
	log.Printf("[email] password_reset to=%s app_id=%s token=%s", to, appID, token)
	return nil
}

// RateLimiter is a simple allow/deny interface.
type RateLimiter interface {
	Allow(key string, limit int, window time.Duration) bool
}

// MemoryRateLimiter is a process-local sliding counter.
type MemoryRateLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func NewMemoryRateLimiter() *MemoryRateLimiter {
	return &MemoryRateLimiter{hits: make(map[string][]time.Time)}
}

func (r *MemoryRateLimiter) Allow(key string, limit int, window time.Duration) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-window)
	arr := r.hits[key]
	kept := arr[:0]
	for _, t := range arr {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= limit {
		r.hits[key] = kept
		return false
	}
	kept = append(kept, now)
	r.hits[key] = kept
	return true
}

// OAuthProfile is the normalized identity from a provider.
type OAuthProfile struct {
	ProviderUserID string
	Email          string
	EmailVerified  bool
	Name           string
}

// OAuthProvider exchanges codes / verifies id_tokens.
type OAuthProvider interface {
	Name() string
	AuthorizeURL(redirectURI, state string) string
	Exchange(ctx context.Context, code, redirectURI string) (*OAuthProfile, error)
	VerifyIDToken(ctx context.Context, idToken string) (*OAuthProfile, error)
}

// OAuthStateStore holds CSRF state for authorize → callback.
type OAuthStateStore interface {
	Put(state string, data OAuthStateData, ttl time.Duration)
	Get(state string) (OAuthStateData, bool)
	Delete(state string)
}

// OAuthStateData is stored against the authorize state.
type OAuthStateData struct {
	AppID       string
	Provider    string
	RedirectURI string
}

// MemoryOAuthStateStore is an in-memory TTL map.
type MemoryOAuthStateStore struct {
	mu   sync.Mutex
	data map[string]stateEntry
}

type stateEntry struct {
	data      OAuthStateData
	expiresAt time.Time
}

func NewMemoryOAuthStateStore() *MemoryOAuthStateStore {
	return &MemoryOAuthStateStore{data: make(map[string]stateEntry)}
}

func (s *MemoryOAuthStateStore) Put(state string, data OAuthStateData, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[state] = stateEntry{data: data, expiresAt: time.Now().Add(ttl)}
}

func (s *MemoryOAuthStateStore) Get(state string) (OAuthStateData, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.data[state]
	if !ok || time.Now().After(e.expiresAt) {
		delete(s.data, state)
		return OAuthStateData{}, false
	}
	return e.data, true
}

func (s *MemoryOAuthStateStore) Delete(state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, state)
}

// ProviderFactory builds a provider from decrypted auth config.
type ProviderFactory func(cfg *domain.AppAuthConfig, decryptedGoogleSecret, decryptedAppleKey string) (OAuthProvider, error)

// Service is the Auth application service.
type Service struct {
	apps           AppGate
	authConfigRepo domain.AuthConfigRepository
	userRepo       domain.UserRepository
	oauthRepo      domain.OAuthAccountRepository
	refreshRepo    domain.RefreshTokenRepository
	verifyRepo     domain.VerificationTokenRepository
	hasher         crypto.PasswordHasher
	encryptor      *crypto.Encryptor
	jwtSvc         *sharedjwt.Service
	emailSender    EmailSender
	rateLimiter    RateLimiter
	stateStore     OAuthStateStore
	providerFactory ProviderFactory
	baseURL        string
}

// Deps wires Auth Service dependencies.
type Deps struct {
	Apps            AppGate
	AuthConfigRepo  domain.AuthConfigRepository
	UserRepo        domain.UserRepository
	OAuthRepo       domain.OAuthAccountRepository
	RefreshRepo     domain.RefreshTokenRepository
	VerifyRepo      domain.VerificationTokenRepository
	Hasher          crypto.PasswordHasher
	Encryptor       *crypto.Encryptor
	JWT             *sharedjwt.Service
	EmailSender     EmailSender
	RateLimiter     RateLimiter
	StateStore      OAuthStateStore
	ProviderFactory ProviderFactory
	BaseURL         string
}

func NewService(d Deps) *Service {
	if d.EmailSender == nil {
		d.EmailSender = LogEmailSender{}
	}
	if d.RateLimiter == nil {
		d.RateLimiter = NewMemoryRateLimiter()
	}
	if d.StateStore == nil {
		d.StateStore = NewMemoryOAuthStateStore()
	}
	if d.ProviderFactory == nil {
		d.ProviderFactory = DefaultProviderFactory
	}
	return &Service{
		apps:            d.Apps,
		authConfigRepo:  d.AuthConfigRepo,
		userRepo:        d.UserRepo,
		oauthRepo:       d.OAuthRepo,
		refreshRepo:     d.RefreshRepo,
		verifyRepo:      d.VerifyRepo,
		hasher:          d.Hasher,
		encryptor:       d.Encryptor,
		jwtSvc:          d.JWT,
		emailSender:     d.EmailSender,
		rateLimiter:     d.RateLimiter,
		stateStore:      d.StateStore,
		providerFactory: d.ProviderFactory,
		baseURL:         d.BaseURL,
	}
}

// TokenHash returns sha256 hex of a JWT string.
func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

var emailRegexp = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

func ValidateEmail(email string) error {
	email = NormalizeEmail(email)
	if email == "" || !emailRegexp.MatchString(email) {
		return apperr.Validation("invalid email")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return apperr.Validation("invalid email")
	}
	return nil
}

func ValidatePassword(password string) error {
	if len(password) < 8 {
		return apperr.Validation("password must be at least 8 characters")
	}
	var upper, lower, digit bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsLower(r):
			lower = true
		case unicode.IsDigit(r):
			digit = true
		}
	}
	if !upper || !lower || !digit {
		return apperr.Validation("password must contain upper, lower, and digit")
	}
	return nil
}

func redirectURIAllowed(app *tenantdomain.App, redirectURI string) bool {
	for _, u := range app.RedirectURIs {
		if u == redirectURI {
			return true
		}
	}
	return false
}

func allowRegister(app *tenantdomain.App) bool {
	if app.Settings == nil {
		return true
	}
	v, ok := app.Settings["allow_register"]
	if !ok {
		return true
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true") || t == "1"
	default:
		return true
	}
}

// --- Register / Login / Tokens ---

type RegisterResult struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Status string `json:"status"`
}

func (s *Service) Register(ctx context.Context, appID, email, password, nickname string) (*RegisterResult, error) {
	app, err := s.apps.RequireActiveApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	if !allowRegister(app) {
		return nil, apperr.Forbidden("registration disabled")
	}
	email = NormalizeEmail(email)
	if err := ValidateEmail(email); err != nil {
		return nil, err
	}
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}
	if !s.rateLimiter.Allow("register:"+appID+":"+email, 5, time.Hour) {
		return nil, apperr.RateLimited("")
	}
	existing, err := s.userRepo.GetByEmail(ctx, appID, email)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.Status != domain.UserStatusDeleted {
		return nil, apperr.EmailExists("")
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return nil, apperr.Internal("failed to hash password")
	}
	now := time.Now().UTC()
	user := &domain.User{
		UserID:        idgen.NewUserID(),
		AppID:         appID,
		Email:         email,
		EmailVerified: false,
		PasswordHash:  hash,
		Nickname:      strings.TrimSpace(nickname),
		Status:        domain.UserStatusUnverified,
		TokenVersion:  1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, err
	}
	rawToken := idgen.NewSecret(24)
	vt := &domain.VerificationToken{
		TokenID:   idgen.NewTokenID(),
		UserID:    user.UserID,
		AppID:     appID,
		TokenType: domain.VerifyTypeEmailVerification,
		TokenHash: TokenHash(rawToken),
		ExpiresAt: now.Add(24 * time.Hour),
		Used:      false,
		CreatedAt: now,
	}
	if err := s.verifyRepo.Create(ctx, vt); err != nil {
		return nil, err
	}
	_ = s.emailSender.SendVerification(ctx, email, rawToken, appID)
	return &RegisterResult{
		UserID: user.UserID,
		Email:  user.Email,
		Status: string(user.Status),
	}, nil
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

type LoginResult struct {
	*TokenPair
	User *UserPublic `json:"user"`
}

type UserPublic struct {
	UserID        string     `json:"user_id"`
	AppID         string     `json:"app_id"`
	Email         string     `json:"email"`
	EmailVerified bool       `json:"email_verified"`
	Nickname      string     `json:"nickname,omitempty"`
	AvatarURL     string     `json:"avatar_url,omitempty"`
	Phone         string     `json:"phone,omitempty"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	LastLoginAt   *time.Time `json:"last_login_at,omitempty"`
}

func toUserPublic(u *domain.User) *UserPublic {
	if u == nil {
		return nil
	}
	return &UserPublic{
		UserID:        u.UserID,
		AppID:         u.AppID,
		Email:         u.Email,
		EmailVerified: u.EmailVerified,
		Nickname:      u.Nickname,
		AvatarURL:     u.AvatarURL,
		Phone:         u.Phone,
		Status:        string(u.Status),
		CreatedAt:     u.CreatedAt,
		UpdatedAt:     u.UpdatedAt,
		LastLoginAt:   u.LastLoginAt,
	}
}

func (s *Service) issueTokens(ctx context.Context, user *domain.User, familyID string) (*TokenPair, error) {
	if familyID == "" {
		familyID = idgen.NewFamilyID()
	}
	access, err := s.jwtSvc.IssueAccess(user.UserID, user.AppID, user.Email, "user", user.TokenVersion)
	if err != nil {
		return nil, apperr.Internal("failed to issue access token")
	}
	refresh, err := s.jwtSvc.IssueRefresh(user.UserID, user.AppID, familyID)
	if err != nil {
		return nil, apperr.Internal("failed to issue refresh token")
	}
	claims, err := s.jwtSvc.Verify(refresh)
	if err != nil {
		return nil, apperr.Internal("failed to parse refresh token")
	}
	now := time.Now().UTC()
	rec := &domain.RefreshTokenRecord{
		TokenID:   claims.ID,
		UserID:    user.UserID,
		AppID:     user.AppID,
		TokenHash: TokenHash(refresh),
		FamilyID:  familyID,
		ExpiresAt: claims.ExpiresAt.Time,
		Revoked:   false,
		CreatedAt: now,
	}
	if err := s.refreshRepo.Create(ctx, rec); err != nil {
		return nil, err
	}
	expiresIn := int64(0)
	if ac, err := s.jwtSvc.Verify(access); err == nil && ac.ExpiresAt != nil {
		expiresIn = int64(ac.ExpiresAt.Time.Sub(now).Seconds())
	}
	return &TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    expiresIn,
	}, nil
}

func (s *Service) Login(ctx context.Context, appID, email, password, clientIP string) (*LoginResult, error) {
	if _, err := s.apps.RequireActiveApp(ctx, appID); err != nil {
		return nil, err
	}
	email = NormalizeEmail(email)
	if err := ValidateEmail(email); err != nil {
		return nil, err
	}
	if password == "" {
		return nil, apperr.Validation("password is required")
	}
	ipKey := clientIP
	if ipKey == "" {
		ipKey = "unknown"
	}
	if !s.rateLimiter.Allow("login:"+ipKey, 20, time.Minute) {
		return nil, apperr.RateLimited("")
	}
	user, err := s.userRepo.GetByEmail(ctx, appID, email)
	if err != nil {
		return nil, err
	}
	if user == nil || user.Status == domain.UserStatusDeleted {
		return nil, apperr.BadCredentials("")
	}
	now := time.Now().UTC()
	if user.IsLocked(now) {
		return nil, apperr.Locked("")
	}
	switch user.Status {
	case domain.UserStatusUnverified:
		return nil, apperr.Unverified("")
	case domain.UserStatusDisabled:
		return nil, apperr.UserDisabled("")
	case domain.UserStatusActive:
		// ok
	default:
		return nil, apperr.BadCredentials("")
	}
	if !user.HasPassword() || !s.hasher.Verify(user.PasswordHash, password) {
		user.LoginFailCount++
		if user.LoginFailCount >= 5 {
			until := now.Add(15 * time.Minute)
			user.LockedUntil = &until
			user.LoginFailCount = 0
		}
		user.UpdatedAt = now
		_ = s.userRepo.Update(ctx, user)
		if user.IsLocked(now) || (user.LockedUntil != nil && user.LockedUntil.After(now)) {
			return nil, apperr.Locked("")
		}
		return nil, apperr.BadCredentials("")
	}
	user.LoginFailCount = 0
	user.LockedUntil = nil
	user.LastLoginAt = &now
	user.UpdatedAt = now
	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, err
	}
	pair, err := s.issueTokens(ctx, user, "")
	if err != nil {
		return nil, err
	}
	return &LoginResult{TokenPair: pair, User: toUserPublic(user)}, nil
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	if refreshToken == "" {
		return nil, apperr.Validation("refresh_token is required")
	}
	claims, err := s.jwtSvc.Verify(refreshToken)
	if err != nil || claims.Type != sharedjwt.TypeRefresh {
		return nil, apperr.TokenInvalid("")
	}
	if _, err := s.apps.RequireActiveApp(ctx, claims.AppID); err != nil {
		return nil, err
	}
	hash := TokenHash(refreshToken)
	rec, err := s.refreshRepo.GetByHash(ctx, hash)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if rec == nil || !rec.IsValid(now) {
		if rec != nil && rec.Revoked {
			_ = s.refreshRepo.RevokeFamily(ctx, rec.FamilyID)
		} else if claims.Family != "" {
			_ = s.refreshRepo.RevokeFamily(ctx, claims.Family)
		}
		return nil, apperr.TokenInvalid("refresh token reuse detected")
	}
	_ = s.refreshRepo.Revoke(ctx, rec.TokenID)
	user, err := s.userRepo.GetByID(ctx, claims.Subject)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive() {
		return nil, apperr.TokenInvalid("")
	}
	return s.issueTokens(ctx, user, rec.FamilyID)
}

func (s *Service) Logout(ctx context.Context, user *domain.User) error {
	if user == nil {
		return apperr.Unauthorized("")
	}
	_ = s.refreshRepo.RevokeAllForUser(ctx, user.UserID)
	user.TokenVersion++
	user.UpdatedAt = time.Now().UTC()
	return s.userRepo.Update(ctx, user)
}

func (s *Service) VerifyEmail(ctx context.Context, token, appID string) error {
	if token == "" {
		return apperr.Validation("token is required")
	}
	vt, err := s.verifyRepo.GetByHash(ctx, TokenHash(token))
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if vt == nil || !vt.IsValid(now) || vt.TokenType != domain.VerifyTypeEmailVerification {
		return apperr.TokenInvalid("")
	}
	user, err := s.userRepo.GetByID(ctx, vt.UserID)
	if err != nil {
		return err
	}
	if user == nil {
		return apperr.TokenInvalid("")
	}
	if appID != "" && user.AppID != appID {
		return apperr.TokenInvalid("")
	}
	if _, err := s.apps.RequireActiveApp(ctx, user.AppID); err != nil {
		return err
	}
	user.EmailVerified = true
	if user.Status == domain.UserStatusUnverified {
		user.Status = domain.UserStatusActive
	}
	user.UpdatedAt = now
	if err := s.userRepo.Update(ctx, user); err != nil {
		return err
	}
	return s.verifyRepo.MarkUsed(ctx, vt.TokenID)
}

func (s *Service) ForgotPassword(ctx context.Context, appID, email string) error {
	if _, err := s.apps.RequireActiveApp(ctx, appID); err != nil {
		return err
	}
	email = NormalizeEmail(email)
	if err := ValidateEmail(email); err != nil {
		return err
	}
	if !s.rateLimiter.Allow("forgot:"+appID+":"+email, 3, time.Hour) {
		return apperr.RateLimited("")
	}
	user, err := s.userRepo.GetByEmail(ctx, appID, email)
	if err != nil {
		return err
	}
	// Always succeed to avoid email enumeration.
	if user == nil || user.Status == domain.UserStatusDeleted || !user.HasPassword() {
		return nil
	}
	raw := idgen.NewSecret(24)
	now := time.Now().UTC()
	vt := &domain.VerificationToken{
		TokenID:   idgen.NewTokenID(),
		UserID:    user.UserID,
		AppID:     appID,
		TokenType: domain.VerifyTypePasswordReset,
		TokenHash: TokenHash(raw),
		ExpiresAt: now.Add(1 * time.Hour),
		Used:      false,
		CreatedAt: now,
	}
	if err := s.verifyRepo.Create(ctx, vt); err != nil {
		return err
	}
	_ = s.emailSender.SendPasswordReset(ctx, email, raw, appID)
	return nil
}

func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	if token == "" {
		return apperr.Validation("token is required")
	}
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	vt, err := s.verifyRepo.GetByHash(ctx, TokenHash(token))
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if vt == nil || !vt.IsValid(now) || vt.TokenType != domain.VerifyTypePasswordReset {
		return apperr.TokenInvalid("")
	}
	user, err := s.userRepo.GetByID(ctx, vt.UserID)
	if err != nil {
		return err
	}
	if user == nil {
		return apperr.TokenInvalid("")
	}
	if _, err := s.apps.RequireActiveApp(ctx, user.AppID); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return apperr.Internal("failed to hash password")
	}
	user.PasswordHash = hash
	user.TokenVersion++
	user.LoginFailCount = 0
	user.LockedUntil = nil
	user.UpdatedAt = now
	if err := s.userRepo.Update(ctx, user); err != nil {
		return err
	}
	_ = s.refreshRepo.RevokeAllForUser(ctx, user.UserID)
	return s.verifyRepo.MarkUsed(ctx, vt.TokenID)
}

// --- OAuth ---

type AuthorizeResult struct {
	AuthorizeURL string `json:"authorize_url"`
	State        string `json:"state"`
}

func (s *Service) loadProvider(ctx context.Context, appID, provider string) (OAuthProvider, *domain.AppAuthConfig, error) {
	cfg, err := s.authConfigRepo.Get(ctx, appID)
	if err != nil {
		return nil, nil, err
	}
	if cfg == nil {
		return nil, nil, apperr.ProviderNotConfigured("")
	}
	var googleSecret, appleKey string
	if cfg.GoogleClientSecretEncrypted != nil && *cfg.GoogleClientSecretEncrypted != "" {
		googleSecret, err = s.encryptor.Decrypt(*cfg.GoogleClientSecretEncrypted)
		if err != nil {
			return nil, nil, apperr.Internal("failed to decrypt google secret")
		}
	}
	if cfg.ApplePrivateKeyEncrypted != nil && *cfg.ApplePrivateKeyEncrypted != "" {
		appleKey, err = s.encryptor.Decrypt(*cfg.ApplePrivateKeyEncrypted)
		if err != nil {
			return nil, nil, apperr.Internal("failed to decrypt apple key")
		}
	}
	p, err := s.providerFactory(cfg, googleSecret, appleKey)
	if err != nil {
		return nil, nil, err
	}
	if p == nil || p.Name() != provider {
		// factory returns specific provider; ask again with name filter
		p, err = buildNamedProvider(provider, cfg, googleSecret, appleKey)
		if err != nil {
			return nil, nil, err
		}
	}
	return p, cfg, nil
}

func (s *Service) OAuthAuthorize(ctx context.Context, appID, provider, redirectURI string) (*AuthorizeResult, error) {
	provider = strings.ToLower(provider)
	if provider != domain.ProviderGoogle && provider != domain.ProviderApple {
		return nil, apperr.Validation("unsupported provider")
	}
	app, err := s.apps.RequireActiveApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	if redirectURI == "" {
		return nil, apperr.Validation("redirect_uri is required")
	}
	if !redirectURIAllowed(app, redirectURI) {
		return nil, apperr.RedirectURINotAllowed("")
	}
	p, cfg, err := s.loadProvider(ctx, appID, provider)
	if err != nil {
		return nil, err
	}
	_ = cfg
	if provider == domain.ProviderGoogle && !cfg.GooglePublicConfigured() {
		return nil, apperr.ProviderNotConfigured("")
	}
	if provider == domain.ProviderApple && !cfg.ApplePublicConfigured() {
		return nil, apperr.ProviderNotConfigured("")
	}
	state := idgen.NewSecret(16)
	s.stateStore.Put(state, OAuthStateData{AppID: appID, Provider: provider, RedirectURI: redirectURI}, 10*time.Minute)
	url := p.AuthorizeURL(redirectURI, state)
	return &AuthorizeResult{AuthorizeURL: url, State: state}, nil
}

type OAuthCallbackInput struct {
	AppID       string
	Provider    string
	Code        string
	IDToken     string
	RedirectURI string
	State       string
}

func (s *Service) OAuthCallback(ctx context.Context, in OAuthCallbackInput) (*LoginResult, error) {
	provider := strings.ToLower(in.Provider)
	if provider != domain.ProviderGoogle && provider != domain.ProviderApple {
		return nil, apperr.Validation("unsupported provider")
	}
	app, err := s.apps.RequireActiveApp(ctx, in.AppID)
	if err != nil {
		return nil, err
	}
	redirectURI := in.RedirectURI
	if in.State != "" {
		st, ok := s.stateStore.Get(in.State)
		if !ok || st.AppID != in.AppID || st.Provider != provider {
			return nil, apperr.OAuthFailed("invalid state")
		}
		if redirectURI == "" {
			redirectURI = st.RedirectURI
		}
		s.stateStore.Delete(in.State)
	}
	if redirectURI != "" && !redirectURIAllowed(app, redirectURI) {
		return nil, apperr.RedirectURINotAllowed("")
	}
	p, _, err := s.loadProvider(ctx, in.AppID, provider)
	if err != nil {
		return nil, err
	}
	var profile *OAuthProfile
	switch {
	case in.IDToken != "":
		profile, err = p.VerifyIDToken(ctx, in.IDToken)
	case in.Code != "":
		profile, err = p.Exchange(ctx, in.Code, redirectURI)
	default:
		return nil, apperr.Validation("code or id_token is required")
	}
	if err != nil || profile == nil || profile.ProviderUserID == "" {
		if he, ok := apperr.AsHarborError(err); ok {
			return nil, he
		}
		return nil, apperr.OAuthFailed(fmt.Sprintf("%v", err))
	}
	profile.Email = NormalizeEmail(profile.Email)
	return s.loginOrCreateOAuthUser(ctx, in.AppID, provider, profile)
}

func (s *Service) loginOrCreateOAuthUser(ctx context.Context, appID, provider string, profile *OAuthProfile) (*LoginResult, error) {
	acct, err := s.oauthRepo.GetByProvider(ctx, appID, provider, profile.ProviderUserID)
	if err != nil {
		return nil, err
	}
	var user *domain.User
	now := time.Now().UTC()
	if acct != nil {
		user, err = s.userRepo.GetByID(ctx, acct.UserID)
		if err != nil {
			return nil, err
		}
		if user == nil || user.Status == domain.UserStatusDeleted {
			return nil, apperr.OAuthFailed("linked user missing")
		}
		if user.Status == domain.UserStatusDisabled {
			return nil, apperr.UserDisabled("")
		}
	} else if profile.Email != "" {
		existing, err := s.userRepo.GetByEmail(ctx, appID, profile.Email)
		if err != nil {
			return nil, err
		}
		if existing != nil && existing.Status == domain.UserStatusActive {
			user = existing
			link := &domain.OAuthAccount{
				AccountID:      idgen.NewAccountID(),
				AppID:          appID,
				UserID:         user.UserID,
				Provider:       provider,
				ProviderUserID: profile.ProviderUserID,
				Email:          profile.Email,
				CreatedAt:      now,
			}
			if err := s.oauthRepo.Create(ctx, link); err != nil {
				return nil, err
			}
		}
	}
	if user == nil {
		user = &domain.User{
			UserID:        idgen.NewUserID(),
			AppID:         appID,
			Email:         profile.Email,
			EmailVerified: profile.EmailVerified || profile.Email != "",
			Nickname:      profile.Name,
			Status:        domain.UserStatusActive,
			TokenVersion:  1,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := s.userRepo.Create(ctx, user); err != nil {
			return nil, err
		}
		link := &domain.OAuthAccount{
			AccountID:      idgen.NewAccountID(),
			AppID:          appID,
			UserID:         user.UserID,
			Provider:       provider,
			ProviderUserID: profile.ProviderUserID,
			Email:          profile.Email,
			CreatedAt:      now,
		}
		if err := s.oauthRepo.Create(ctx, link); err != nil {
			return nil, err
		}
	}
	if user.Status != domain.UserStatusActive {
		if user.Status == domain.UserStatusUnverified {
			user.Status = domain.UserStatusActive
			user.EmailVerified = true
		} else {
			return nil, apperr.UserDisabled("")
		}
	}
	user.LastLoginAt = &now
	user.UpdatedAt = now
	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, err
	}
	pair, err := s.issueTokens(ctx, user, "")
	if err != nil {
		return nil, err
	}
	return &LoginResult{TokenPair: pair, User: toUserPublic(user)}, nil
}

func (s *Service) OAuthLink(ctx context.Context, user *domain.User, provider, code, idToken, redirectURI string) error {
	if user == nil {
		return apperr.Unauthorized("")
	}
	provider = strings.ToLower(provider)
	p, _, err := s.loadProvider(ctx, user.AppID, provider)
	if err != nil {
		return err
	}
	var profile *OAuthProfile
	switch {
	case idToken != "":
		profile, err = p.VerifyIDToken(ctx, idToken)
	case code != "":
		profile, err = p.Exchange(ctx, code, redirectURI)
	default:
		return apperr.Validation("code or id_token is required")
	}
	if err != nil || profile == nil || profile.ProviderUserID == "" {
		return apperr.OAuthFailed("")
	}
	existing, err := s.oauthRepo.GetByProvider(ctx, user.AppID, provider, profile.ProviderUserID)
	if err != nil {
		return err
	}
	if existing != nil && existing.UserID != user.UserID {
		return apperr.OAuthLinked("")
	}
	if existing != nil {
		return nil
	}
	return s.oauthRepo.Create(ctx, &domain.OAuthAccount{
		AccountID:      idgen.NewAccountID(),
		AppID:          user.AppID,
		UserID:         user.UserID,
		Provider:       provider,
		ProviderUserID: profile.ProviderUserID,
		Email:          NormalizeEmail(profile.Email),
		CreatedAt:      time.Now().UTC(),
	})
}

func (s *Service) OAuthUnlink(ctx context.Context, user *domain.User, provider string) error {
	if user == nil {
		return apperr.Unauthorized("")
	}
	provider = strings.ToLower(provider)
	links, err := s.oauthRepo.ListByUser(ctx, user.UserID)
	if err != nil {
		return err
	}
	var target *domain.OAuthAccount
	for _, l := range links {
		if l.Provider == provider {
			target = l
			break
		}
	}
	if target == nil {
		return apperr.Validation("provider not linked")
	}
	remaining := len(links) - 1
	if remaining == 0 && !user.HasPassword() {
		return apperr.CannotUnlink("")
	}
	return s.oauthRepo.Delete(ctx, target.AccountID)
}

// --- Introspect / Me ---

type IntrospectResult struct {
	Active    bool   `json:"active"`
	UserID    string `json:"user_id,omitempty"`
	AppID     string `json:"app_id,omitempty"`
	Email     string `json:"email,omitempty"`
	TokenType string `json:"token_type,omitempty"`
	Exp       int64  `json:"exp,omitempty"`
	TV        int    `json:"tv,omitempty"`
}

func (s *Service) Introspect(ctx context.Context, appID, appSecret, token string) (*IntrospectResult, error) {
	if !s.rateLimiter.Allow("introspect:"+appID, 100, time.Minute) {
		return nil, apperr.RateLimited("")
	}
	if _, err := s.apps.VerifyAppSecret(ctx, appID, appSecret); err != nil {
		return nil, err
	}
	inactive := &IntrospectResult{Active: false}
	claims, err := s.jwtSvc.Verify(token)
	if err != nil || claims.Type != sharedjwt.TypeAccess {
		return inactive, nil
	}
	if claims.AppID != appID {
		return inactive, nil
	}
	user, err := s.userRepo.GetByID(ctx, claims.Subject)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive() || user.TokenVersion != claims.TV {
		return inactive, nil
	}
	if _, err := s.apps.RequireActiveApp(ctx, appID); err != nil {
		return inactive, nil
	}
	exp := int64(0)
	if claims.ExpiresAt != nil {
		exp = claims.ExpiresAt.Unix()
	}
	return &IntrospectResult{
		Active:    true,
		UserID:    user.UserID,
		AppID:     user.AppID,
		Email:     user.Email,
		TokenType: sharedjwt.TypeAccess,
		Exp:       exp,
		TV:        user.TokenVersion,
	}, nil
}

func (s *Service) GetMe(ctx context.Context, user *domain.User) (*UserPublic, error) {
	_ = ctx
	if user == nil {
		return nil, apperr.Unauthorized("")
	}
	return toUserPublic(user), nil
}

type UpdateMeInput struct {
	Nickname  *string `json:"nickname"`
	AvatarURL *string `json:"avatar_url"`
	Phone     *string `json:"phone"`
}

func (s *Service) UpdateMe(ctx context.Context, user *domain.User, in UpdateMeInput) (*UserPublic, error) {
	if user == nil {
		return nil, apperr.Unauthorized("")
	}
	if in.Nickname != nil {
		user.Nickname = strings.TrimSpace(*in.Nickname)
	}
	if in.AvatarURL != nil {
		user.AvatarURL = strings.TrimSpace(*in.AvatarURL)
	}
	if in.Phone != nil {
		user.Phone = strings.TrimSpace(*in.Phone)
	}
	user.UpdatedAt = time.Now().UTC()
	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, err
	}
	return toUserPublic(user), nil
}

func (s *Service) ChangePassword(ctx context.Context, user *domain.User, oldPassword, newPassword string) error {
	if user == nil {
		return apperr.Unauthorized("")
	}
	if !user.HasPassword() || !s.hasher.Verify(user.PasswordHash, oldPassword) {
		return apperr.BadCredentials("")
	}
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return apperr.Internal("failed to hash password")
	}
	user.PasswordHash = hash
	user.TokenVersion++
	user.UpdatedAt = time.Now().UTC()
	if err := s.userRepo.Update(ctx, user); err != nil {
		return err
	}
	_ = s.refreshRepo.RevokeAllForUser(ctx, user.UserID)
	return nil
}

func (s *Service) ChangeEmail(ctx context.Context, user *domain.User, newEmail, password string) error {
	if user == nil {
		return apperr.Unauthorized("")
	}
	newEmail = NormalizeEmail(newEmail)
	if err := ValidateEmail(newEmail); err != nil {
		return err
	}
	if !user.HasPassword() || !s.hasher.Verify(user.PasswordHash, password) {
		return apperr.BadCredentials("")
	}
	existing, err := s.userRepo.GetByEmail(ctx, user.AppID, newEmail)
	if err != nil {
		return err
	}
	if existing != nil && existing.UserID != user.UserID && existing.Status != domain.UserStatusDeleted {
		return apperr.EmailExists("")
	}
	user.Email = newEmail
	user.EmailVerified = false
	user.Status = domain.UserStatusUnverified
	user.TokenVersion++
	user.UpdatedAt = time.Now().UTC()
	if err := s.userRepo.Update(ctx, user); err != nil {
		return err
	}
	_ = s.refreshRepo.RevokeAllForUser(ctx, user.UserID)
	raw := idgen.NewSecret(24)
	now := time.Now().UTC()
	vt := &domain.VerificationToken{
		TokenID:   idgen.NewTokenID(),
		UserID:    user.UserID,
		AppID:     user.AppID,
		TokenType: domain.VerifyTypeEmailVerification,
		TokenHash: TokenHash(raw),
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	}
	if err := s.verifyRepo.Create(ctx, vt); err != nil {
		return err
	}
	_ = s.emailSender.SendVerification(ctx, newEmail, raw, user.AppID)
	return nil
}

func (s *Service) ListAccountLinks(ctx context.Context, user *domain.User) ([]*domain.OAuthAccount, error) {
	if user == nil {
		return nil, apperr.Unauthorized("")
	}
	return s.oauthRepo.ListByUser(ctx, user.UserID)
}

func (s *Service) DeleteAccount(ctx context.Context, user *domain.User, password string) error {
	if user == nil {
		return apperr.Unauthorized("")
	}
	if user.HasPassword() {
		if !s.hasher.Verify(user.PasswordHash, password) {
			return apperr.BadCredentials("")
		}
	}
	user.Status = domain.UserStatusDeleted
	user.Email = ""
	user.PasswordHash = ""
	user.TokenVersion++
	user.UpdatedAt = time.Now().UTC()
	if err := s.userRepo.Update(ctx, user); err != nil {
		return err
	}
	_ = s.refreshRepo.RevokeAllForUser(ctx, user.UserID)
	links, _ := s.oauthRepo.ListByUser(ctx, user.UserID)
	for _, l := range links {
		_ = s.oauthRepo.Delete(ctx, l.AccountID)
	}
	return nil
}

// --- AuthConfig ---

type AuthConfigPublic struct {
	AppID            string     `json:"app_id"`
	GoogleClientID   *string    `json:"google_client_id"`
	GoogleConfigured bool       `json:"google_configured"`
	AppleClientID    *string    `json:"apple_client_id"`
	AppleTeamID      *string    `json:"apple_team_id"`
	AppleKeyID       *string    `json:"apple_key_id"`
	AppleConfigured  bool       `json:"apple_configured"`
	UpdatedAt        *time.Time `json:"updated_at"`
}

func toAuthConfigPublic(cfg *domain.AppAuthConfig) *AuthConfigPublic {
	if cfg == nil {
		return &AuthConfigPublic{}
	}
	out := &AuthConfigPublic{
		AppID:            cfg.AppID,
		GoogleClientID:   cfg.GoogleClientID,
		GoogleConfigured: cfg.GoogleConfigured() || cfg.GooglePublicConfigured(),
		AppleClientID:    cfg.AppleClientID,
		AppleTeamID:      cfg.AppleTeamID,
		AppleKeyID:       cfg.AppleKeyID,
		AppleConfigured:  cfg.AppleConfigured() || cfg.ApplePublicConfigured(),
	}
	if !cfg.UpdatedAt.IsZero() {
		t := cfg.UpdatedAt
		out.UpdatedAt = &t
	}
	return out
}

func (s *Service) GetAuthConfig(ctx context.Context, appID string) (*UpdateAuthConfigResult, error) {
	if _, err := s.apps.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	cfg, err := s.authConfigRepo.Get(ctx, appID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return &UpdateAuthConfigResult{
			AuthConfigPublic: &AuthConfigPublic{
				AppID:            appID,
				GoogleConfigured: false,
				AppleConfigured:  false,
			},
		}, nil
	}
	res := &UpdateAuthConfigResult{AuthConfigPublic: toAuthConfigPublic(cfg)}
	if s.encryptor != nil &&
		cfg.GoogleClientSecretEncrypted != nil &&
		*cfg.GoogleClientSecretEncrypted != "" {
		plain, err := s.encryptor.Decrypt(*cfg.GoogleClientSecretEncrypted)
		if err != nil {
			return nil, apperr.Internal("failed to decrypt google secret")
		}
		res.GoogleClientSecret = &plain
	}
	if s.encryptor != nil &&
		cfg.ApplePrivateKeyEncrypted != nil &&
		*cfg.ApplePrivateKeyEncrypted != "" {
		plain, err := s.encryptor.Decrypt(*cfg.ApplePrivateKeyEncrypted)
		if err != nil {
			return nil, apperr.Internal("failed to decrypt apple key")
		}
		res.ApplePrivateKey = &plain
	}
	return res, nil
}

type UpdateAuthConfigInput struct {
	GoogleClientID     *string `json:"google_client_id"`
	GoogleClientSecret *string `json:"google_client_secret"`
	AppleClientID      *string `json:"apple_client_id"`
	AppleTeamID        *string `json:"apple_team_id"`
	AppleKeyID         *string `json:"apple_key_id"`
	ApplePrivateKey    *string `json:"apple_private_key"`
	ClearGoogleSecret  bool    `json:"clear_google_secret"`
	ClearAppleKey      bool    `json:"clear_apple_key"`
}

type UpdateAuthConfigResult struct {
	*AuthConfigPublic
	GoogleClientSecret *string `json:"google_client_secret,omitempty"`
	ApplePrivateKey    *string `json:"apple_private_key,omitempty"`
}

func (s *Service) UpdateAuthConfig(ctx context.Context, appID string, in UpdateAuthConfigInput) (*UpdateAuthConfigResult, error) {
	if _, err := s.apps.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	cfg, err := s.authConfigRepo.Get(ctx, appID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg = &domain.AppAuthConfig{AppID: appID}
	}
	if in.GoogleClientID != nil {
		cfg.GoogleClientID = in.GoogleClientID
	}
	if in.ClearGoogleSecret {
		cfg.GoogleClientSecretEncrypted = nil
	} else if in.GoogleClientSecret != nil && *in.GoogleClientSecret != "" {
		enc, err := s.encryptor.Encrypt(*in.GoogleClientSecret)
		if err != nil {
			return nil, apperr.Internal("encrypt google secret failed")
		}
		cfg.GoogleClientSecretEncrypted = &enc
	}
	if in.AppleClientID != nil {
		cfg.AppleClientID = in.AppleClientID
	}
	if in.AppleTeamID != nil {
		cfg.AppleTeamID = in.AppleTeamID
	}
	if in.AppleKeyID != nil {
		cfg.AppleKeyID = in.AppleKeyID
	}
	if in.ClearAppleKey {
		cfg.ApplePrivateKeyEncrypted = nil
	} else if in.ApplePrivateKey != nil && *in.ApplePrivateKey != "" {
		enc, err := s.encryptor.Encrypt(*in.ApplePrivateKey)
		if err != nil {
			return nil, apperr.Internal("encrypt apple key failed")
		}
		cfg.ApplePrivateKeyEncrypted = &enc
	}
	cfg.UpdatedAt = time.Now().UTC()
	if err := s.authConfigRepo.Upsert(ctx, cfg); err != nil {
		return nil, err
	}
	res := &UpdateAuthConfigResult{AuthConfigPublic: toAuthConfigPublic(cfg)}
	if in.GoogleClientSecret != nil && *in.GoogleClientSecret != "" {
		res.GoogleClientSecret = in.GoogleClientSecret
	}
	if in.ApplePrivateKey != nil && *in.ApplePrivateKey != "" {
		res.ApplePrivateKey = in.ApplePrivateKey
	}
	return res, nil
}

func (s *Service) EnsureAuthConfig(ctx context.Context, appID string) error {
	cfg, err := s.authConfigRepo.Get(ctx, appID)
	if err != nil {
		return err
	}
	if cfg != nil {
		return nil
	}
	now := time.Now().UTC()
	return s.authConfigRepo.Upsert(ctx, &domain.AppAuthConfig{
		AppID:     appID,
		UpdatedAt: now,
	})
}

// OnAppCreated implements tenant.AppLifecycleHook.
func (s *Service) OnAppCreated(ctx context.Context, app *tenantdomain.App) error {
	if app == nil {
		return nil
	}
	return s.EnsureAuthConfig(ctx, app.AppID)
}

// OnAppDisabled implements tenant.AppLifecycleHook (no-op for Auth P0).
func (s *Service) OnAppDisabled(ctx context.Context, appID string) error {
	_ = ctx
	_ = appID
	return nil
}

// LoadUserForAccess validates an access token and returns the user.
func (s *Service) LoadUserForAccess(ctx context.Context, accessToken string) (*domain.User, *sharedjwt.Claims, error) {
	claims, err := s.jwtSvc.Verify(accessToken)
	if err != nil || claims.Type != sharedjwt.TypeAccess {
		return nil, nil, apperr.Unauthorized("")
	}
	user, err := s.userRepo.GetByID(ctx, claims.Subject)
	if err != nil {
		return nil, nil, err
	}
	if user == nil || !user.IsActive() || user.TokenVersion != claims.TV {
		return nil, nil, apperr.Unauthorized("")
	}
	if _, err := s.apps.RequireActiveApp(ctx, claims.AppID); err != nil {
		return nil, nil, err
	}
	return user, claims, nil
}

// JWKS exposes the JWT service JWKS.
func (s *Service) JWKS() map[string]interface{} {
	return s.jwtSvc.JWKS()
}

// ParseUnverifiedJWTPayload parses a JWT payload without signature verify (MVP id_token helper).
func ParseUnverifiedJWTPayload(idToken string) (map[string]interface{}, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid jwt")
	}
	payload, err := decodeSegment(parts[1])
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(payload, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func decodeSegment(seg string) ([]byte, error) {
	return decodeB64URL(seg)
}
