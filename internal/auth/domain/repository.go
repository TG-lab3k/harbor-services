package domain

import "context"

// UserRepository persists users.
type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, userID string) (*User, error)
	GetByEmail(ctx context.Context, appID, email string) (*User, error)
	Update(ctx context.Context, user *User) error
}

// OAuthAccountRepository persists OAuth account links.
type OAuthAccountRepository interface {
	Create(ctx context.Context, account *OAuthAccount) error
	GetByProvider(ctx context.Context, appID, provider, providerUserID string) (*OAuthAccount, error)
	ListByUser(ctx context.Context, userID string) ([]*OAuthAccount, error)
	Delete(ctx context.Context, accountID string) error
}

// RefreshTokenRepository persists refresh token records.
type RefreshTokenRepository interface {
	Create(ctx context.Context, record *RefreshTokenRecord) error
	GetByHash(ctx context.Context, tokenHash string) (*RefreshTokenRecord, error)
	Revoke(ctx context.Context, tokenID string) error
	RevokeFamily(ctx context.Context, familyID string) error
	RevokeAllForUser(ctx context.Context, userID string) error
}

// VerificationTokenRepository persists one-time verification tokens.
type VerificationTokenRepository interface {
	Create(ctx context.Context, token *VerificationToken) error
	GetByHash(ctx context.Context, tokenHash string) (*VerificationToken, error)
	MarkUsed(ctx context.Context, tokenID string) error
}

// AuthConfigRepository persists per-app auth OAuth config.
type AuthConfigRepository interface {
	Get(ctx context.Context, appID string) (*AppAuthConfig, error)
	Upsert(ctx context.Context, cfg *AppAuthConfig) error
}
