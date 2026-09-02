package infrastructure

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/okok/harbor-services/internal/auth/domain"
	"github.com/okok/harbor-services/internal/platform/apperr"
)

const (
	usersCollection              = "users"
	oauthAccountsCollection      = "oauth_accounts"
	refreshTokensCollection      = "refresh_tokens"
	verificationTokensCollection = "verification_tokens"
	appAuthConfigsCollection     = "app_auth_configs"
)

// FirestoreStore implements all Auth repositories against Firestore.
type FirestoreStore struct {
	client *firestore.Client
}

func NewFirestoreStore(client *firestore.Client) *FirestoreStore {
	return &FirestoreStore{client: client}
}

func (s *FirestoreStore) UserRepo() domain.UserRepository {
	return &firestoreUserRepo{client: s.client}
}

func (s *FirestoreStore) OAuthRepo() domain.OAuthAccountRepository {
	return &firestoreOAuthRepo{client: s.client}
}

func (s *FirestoreStore) RefreshRepo() domain.RefreshTokenRepository {
	return &firestoreRefreshRepo{client: s.client}
}

func (s *FirestoreStore) VerifyRepo() domain.VerificationTokenRepository {
	return &firestoreVerifyRepo{client: s.client}
}

func (s *FirestoreStore) AuthConfigRepo() domain.AuthConfigRepository {
	return &firestoreAuthConfigRepo{client: s.client}
}

// --- document shapes ---

type userDoc struct {
	UserID         string     `firestore:"user_id"`
	AppID          string     `firestore:"app_id"`
	Email          string     `firestore:"email"`
	EmailVerified  bool       `firestore:"email_verified"`
	PasswordHash   string     `firestore:"password_hash"`
	Nickname       string     `firestore:"nickname"`
	AvatarURL      string     `firestore:"avatar_url"`
	Phone          string     `firestore:"phone"`
	Status         string     `firestore:"status"`
	GlobalUserID   string     `firestore:"global_user_id"`
	LoginFailCount int        `firestore:"login_fail_count"`
	LockedUntil    *time.Time `firestore:"locked_until"`
	TokenVersion   int        `firestore:"token_version"`
	CreatedAt      time.Time  `firestore:"created_at"`
	UpdatedAt      time.Time  `firestore:"updated_at"`
	LastLoginAt    *time.Time `firestore:"last_login_at"`
}

func userToDoc(u *domain.User) userDoc {
	return userDoc{
		UserID:         u.UserID,
		AppID:          u.AppID,
		Email:          u.Email,
		EmailVerified:  u.EmailVerified,
		PasswordHash:   u.PasswordHash,
		Nickname:       u.Nickname,
		AvatarURL:      u.AvatarURL,
		Phone:          u.Phone,
		Status:         string(u.Status),
		GlobalUserID:   u.GlobalUserID,
		LoginFailCount: u.LoginFailCount,
		LockedUntil:    u.LockedUntil,
		TokenVersion:   u.TokenVersion,
		CreatedAt:      u.CreatedAt,
		UpdatedAt:      u.UpdatedAt,
		LastLoginAt:    u.LastLoginAt,
	}
}

func docToUser(d userDoc) *domain.User {
	return &domain.User{
		UserID:         d.UserID,
		AppID:          d.AppID,
		Email:          d.Email,
		EmailVerified:  d.EmailVerified,
		PasswordHash:   d.PasswordHash,
		Nickname:       d.Nickname,
		AvatarURL:      d.AvatarURL,
		Phone:          d.Phone,
		Status:         domain.UserStatus(d.Status),
		GlobalUserID:   d.GlobalUserID,
		LoginFailCount: d.LoginFailCount,
		LockedUntil:    d.LockedUntil,
		TokenVersion:   d.TokenVersion,
		CreatedAt:      d.CreatedAt,
		UpdatedAt:      d.UpdatedAt,
		LastLoginAt:    d.LastLoginAt,
	}
}

type oauthDoc struct {
	AccountID      string    `firestore:"account_id"`
	AppID          string    `firestore:"app_id"`
	UserID         string    `firestore:"user_id"`
	Provider       string    `firestore:"provider"`
	ProviderUserID string    `firestore:"provider_user_id"`
	Email          string    `firestore:"email"`
	CreatedAt      time.Time `firestore:"created_at"`
}

func oauthToDoc(a *domain.OAuthAccount) oauthDoc {
	return oauthDoc{
		AccountID:      a.AccountID,
		AppID:          a.AppID,
		UserID:         a.UserID,
		Provider:       a.Provider,
		ProviderUserID: a.ProviderUserID,
		Email:          a.Email,
		CreatedAt:      a.CreatedAt,
	}
}

func docToOAuth(d oauthDoc) *domain.OAuthAccount {
	return &domain.OAuthAccount{
		AccountID:      d.AccountID,
		AppID:          d.AppID,
		UserID:         d.UserID,
		Provider:       d.Provider,
		ProviderUserID: d.ProviderUserID,
		Email:          d.Email,
		CreatedAt:      d.CreatedAt,
	}
}

type refreshDoc struct {
	TokenID   string    `firestore:"token_id"`
	UserID    string    `firestore:"user_id"`
	AppID     string    `firestore:"app_id"`
	TokenHash string    `firestore:"token_hash"`
	FamilyID  string    `firestore:"family_id"`
	ExpiresAt time.Time `firestore:"expires_at"`
	Revoked   bool      `firestore:"revoked"`
	CreatedAt time.Time `firestore:"created_at"`
}

func refreshToDoc(r *domain.RefreshTokenRecord) refreshDoc {
	return refreshDoc{
		TokenID:   r.TokenID,
		UserID:    r.UserID,
		AppID:     r.AppID,
		TokenHash: r.TokenHash,
		FamilyID:  r.FamilyID,
		ExpiresAt: r.ExpiresAt,
		Revoked:   r.Revoked,
		CreatedAt: r.CreatedAt,
	}
}

func docToRefresh(d refreshDoc) *domain.RefreshTokenRecord {
	return &domain.RefreshTokenRecord{
		TokenID:   d.TokenID,
		UserID:    d.UserID,
		AppID:     d.AppID,
		TokenHash: d.TokenHash,
		FamilyID:  d.FamilyID,
		ExpiresAt: d.ExpiresAt,
		Revoked:   d.Revoked,
		CreatedAt: d.CreatedAt,
	}
}

type verifyDoc struct {
	TokenID   string    `firestore:"token_id"`
	UserID    string    `firestore:"user_id"`
	AppID     string    `firestore:"app_id"`
	TokenType string    `firestore:"token_type"`
	TokenHash string    `firestore:"token_hash"`
	ExpiresAt time.Time `firestore:"expires_at"`
	Used      bool      `firestore:"used"`
	CreatedAt time.Time `firestore:"created_at"`
}

func verifyToDoc(t *domain.VerificationToken) verifyDoc {
	return verifyDoc{
		TokenID:   t.TokenID,
		UserID:    t.UserID,
		AppID:     t.AppID,
		TokenType: t.TokenType,
		TokenHash: t.TokenHash,
		ExpiresAt: t.ExpiresAt,
		Used:      t.Used,
		CreatedAt: t.CreatedAt,
	}
}

func docToVerify(d verifyDoc) *domain.VerificationToken {
	return &domain.VerificationToken{
		TokenID:   d.TokenID,
		UserID:    d.UserID,
		AppID:     d.AppID,
		TokenType: d.TokenType,
		TokenHash: d.TokenHash,
		ExpiresAt: d.ExpiresAt,
		Used:      d.Used,
		CreatedAt: d.CreatedAt,
	}
}

type authConfigDoc struct {
	AppID                       string    `firestore:"app_id"`
	GoogleClientID              *string   `firestore:"google_client_id"`
	GoogleClientSecretEncrypted *string   `firestore:"google_client_secret_encrypted"`
	AppleClientID               *string   `firestore:"apple_client_id"`
	AppleTeamID                 *string   `firestore:"apple_team_id"`
	AppleKeyID                  *string   `firestore:"apple_key_id"`
	ApplePrivateKeyEncrypted    *string   `firestore:"apple_private_key_encrypted"`
	UpdatedAt                   time.Time `firestore:"updated_at"`
}

func authConfigToDoc(c *domain.AppAuthConfig) authConfigDoc {
	return authConfigDoc{
		AppID:                       c.AppID,
		GoogleClientID:              c.GoogleClientID,
		GoogleClientSecretEncrypted: c.GoogleClientSecretEncrypted,
		AppleClientID:               c.AppleClientID,
		AppleTeamID:                 c.AppleTeamID,
		AppleKeyID:                  c.AppleKeyID,
		ApplePrivateKeyEncrypted:    c.ApplePrivateKeyEncrypted,
		UpdatedAt:                   c.UpdatedAt,
	}
}

func docToAuthConfig(d authConfigDoc) *domain.AppAuthConfig {
	return &domain.AppAuthConfig{
		AppID:                       d.AppID,
		GoogleClientID:              d.GoogleClientID,
		GoogleClientSecretEncrypted: d.GoogleClientSecretEncrypted,
		AppleClientID:               d.AppleClientID,
		AppleTeamID:                 d.AppleTeamID,
		AppleKeyID:                  d.AppleKeyID,
		ApplePrivateKeyEncrypted:    d.ApplePrivateKeyEncrypted,
		UpdatedAt:                   d.UpdatedAt,
	}
}

// --- UserRepository ---

type firestoreUserRepo struct {
	client *firestore.Client
}

func (r *firestoreUserRepo) col() *firestore.CollectionRef {
	return r.client.Collection(usersCollection)
}

func (r *firestoreUserRepo) Create(ctx context.Context, user *domain.User) error {
	if user.Email != "" {
		existing, err := r.GetByEmail(ctx, user.AppID, user.Email)
		if err != nil {
			return err
		}
		if existing != nil {
			return apperr.EmailExists("")
		}
	}
	_, err := r.col().Doc(user.UserID).Create(ctx, userToDoc(user))
	if status.Code(err) == codes.AlreadyExists {
		return apperr.Validation("user already exists")
	}
	return err
}

func (r *firestoreUserRepo) GetByID(ctx context.Context, userID string) (*domain.User, error) {
	snap, err := r.col().Doc(userID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}
	var d userDoc
	if err := snap.DataTo(&d); err != nil {
		return nil, err
	}
	return docToUser(d), nil
}

func (r *firestoreUserRepo) GetByEmail(ctx context.Context, appID, email string) (*domain.User, error) {
	iter := r.col().Where("app_id", "==", appID).Where("email", "==", email).Limit(1).Documents(ctx)
	defer iter.Stop()
	snap, err := iter.Next()
	if err == iterator.Done {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d userDoc
	if err := snap.DataTo(&d); err != nil {
		return nil, err
	}
	return docToUser(d), nil
}

func (r *firestoreUserRepo) Update(ctx context.Context, user *domain.User) error {
	old, err := r.GetByID(ctx, user.UserID)
	if err != nil {
		return err
	}
	if old == nil {
		return apperr.Validation("user not found")
	}
	if user.Email != "" && (old.Email != user.Email || old.AppID != user.AppID) {
		existing, err := r.GetByEmail(ctx, user.AppID, user.Email)
		if err != nil {
			return err
		}
		if existing != nil && existing.UserID != user.UserID {
			return apperr.EmailExists("")
		}
	}
	_, err = r.col().Doc(user.UserID).Set(ctx, userToDoc(user))
	return err
}

// --- OAuthAccountRepository ---

type firestoreOAuthRepo struct {
	client *firestore.Client
}

func (r *firestoreOAuthRepo) col() *firestore.CollectionRef {
	return r.client.Collection(oauthAccountsCollection)
}

func (r *firestoreOAuthRepo) Create(ctx context.Context, account *domain.OAuthAccount) error {
	existing, err := r.GetByProvider(ctx, account.AppID, account.Provider, account.ProviderUserID)
	if err != nil {
		return err
	}
	if existing != nil {
		return apperr.OAuthLinked("")
	}
	_, err = r.col().Doc(account.AccountID).Create(ctx, oauthToDoc(account))
	if status.Code(err) == codes.AlreadyExists {
		return apperr.OAuthLinked("")
	}
	return err
}

func (r *firestoreOAuthRepo) GetByProvider(ctx context.Context, appID, provider, providerUserID string) (*domain.OAuthAccount, error) {
	iter := r.col().
		Where("app_id", "==", appID).
		Where("provider", "==", provider).
		Where("provider_user_id", "==", providerUserID).
		Limit(1).Documents(ctx)
	defer iter.Stop()
	snap, err := iter.Next()
	if err == iterator.Done {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d oauthDoc
	if err := snap.DataTo(&d); err != nil {
		return nil, err
	}
	return docToOAuth(d), nil
}

func (r *firestoreOAuthRepo) ListByUser(ctx context.Context, userID string) ([]*domain.OAuthAccount, error) {
	iter := r.col().Where("user_id", "==", userID).Documents(ctx)
	defer iter.Stop()
	out := make([]*domain.OAuthAccount, 0)
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var d oauthDoc
		if err := snap.DataTo(&d); err != nil {
			return nil, err
		}
		out = append(out, docToOAuth(d))
	}
	return out, nil
}

func (r *firestoreOAuthRepo) Delete(ctx context.Context, accountID string) error {
	_, err := r.col().Doc(accountID).Delete(ctx)
	if status.Code(err) == codes.NotFound {
		return nil
	}
	return err
}

// --- RefreshTokenRepository ---

type firestoreRefreshRepo struct {
	client *firestore.Client
}

func (r *firestoreRefreshRepo) col() *firestore.CollectionRef {
	return r.client.Collection(refreshTokensCollection)
}

func (r *firestoreRefreshRepo) Create(ctx context.Context, record *domain.RefreshTokenRecord) error {
	_, err := r.col().Doc(record.TokenID).Create(ctx, refreshToDoc(record))
	return err
}

func (r *firestoreRefreshRepo) GetByHash(ctx context.Context, tokenHash string) (*domain.RefreshTokenRecord, error) {
	iter := r.col().Where("token_hash", "==", tokenHash).Limit(1).Documents(ctx)
	defer iter.Stop()
	snap, err := iter.Next()
	if err == iterator.Done {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d refreshDoc
	if err := snap.DataTo(&d); err != nil {
		return nil, err
	}
	return docToRefresh(d), nil
}

func (r *firestoreRefreshRepo) Revoke(ctx context.Context, tokenID string) error {
	ref := r.col().Doc(tokenID)
	_, err := ref.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil
		}
		return err
	}
	_, err = ref.Update(ctx, []firestore.Update{
		{Path: "revoked", Value: true},
	})
	return err
}

func (r *firestoreRefreshRepo) revokeMatching(ctx context.Context, field, value string) error {
	iter := r.col().Where(field, "==", value).Where("revoked", "==", false).Documents(ctx)
	defer iter.Stop()
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		if _, err := snap.Ref.Update(ctx, []firestore.Update{
			{Path: "revoked", Value: true},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *firestoreRefreshRepo) RevokeFamily(ctx context.Context, familyID string) error {
	return r.revokeMatching(ctx, "family_id", familyID)
}

func (r *firestoreRefreshRepo) RevokeAllForUser(ctx context.Context, userID string) error {
	return r.revokeMatching(ctx, "user_id", userID)
}

// --- VerificationTokenRepository ---

type firestoreVerifyRepo struct {
	client *firestore.Client
}

func (r *firestoreVerifyRepo) col() *firestore.CollectionRef {
	return r.client.Collection(verificationTokensCollection)
}

func (r *firestoreVerifyRepo) Create(ctx context.Context, token *domain.VerificationToken) error {
	_, err := r.col().Doc(token.TokenID).Create(ctx, verifyToDoc(token))
	return err
}

func (r *firestoreVerifyRepo) GetByHash(ctx context.Context, tokenHash string) (*domain.VerificationToken, error) {
	iter := r.col().Where("token_hash", "==", tokenHash).Limit(1).Documents(ctx)
	defer iter.Stop()
	snap, err := iter.Next()
	if err == iterator.Done {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d verifyDoc
	if err := snap.DataTo(&d); err != nil {
		return nil, err
	}
	return docToVerify(d), nil
}

func (r *firestoreVerifyRepo) MarkUsed(ctx context.Context, tokenID string) error {
	ref := r.col().Doc(tokenID)
	_, err := ref.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil
		}
		return err
	}
	_, err = ref.Update(ctx, []firestore.Update{
		{Path: "used", Value: true},
	})
	return err
}

// --- AuthConfigRepository ---

type firestoreAuthConfigRepo struct {
	client *firestore.Client
}

func (r *firestoreAuthConfigRepo) col() *firestore.CollectionRef {
	return r.client.Collection(appAuthConfigsCollection)
}

func (r *firestoreAuthConfigRepo) Get(ctx context.Context, appID string) (*domain.AppAuthConfig, error) {
	snap, err := r.col().Doc(appID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, err
	}
	var d authConfigDoc
	if err := snap.DataTo(&d); err != nil {
		return nil, err
	}
	return docToAuthConfig(d), nil
}

func (r *firestoreAuthConfigRepo) Upsert(ctx context.Context, cfg *domain.AppAuthConfig) error {
	_, err := r.col().Doc(cfg.AppID).Set(ctx, authConfigToDoc(cfg))
	return err
}
