package domain

import "time"

// UserStatus is the lifecycle state of an auth user.
type UserStatus string

const (
	UserStatusUnverified UserStatus = "unverified"
	UserStatusActive     UserStatus = "active"
	UserStatusDisabled   UserStatus = "disabled"
	UserStatusDeleted    UserStatus = "deleted"
)

// User is the Auth primary entity, scoped by app_id.
type User struct {
	UserID        string     `json:"user_id"`
	AppID         string     `json:"app_id"`
	Email         string     `json:"email"`
	EmailVerified bool       `json:"email_verified"`
	PasswordHash  string     `json:"-"`
	Nickname      string     `json:"nickname,omitempty"`
	AvatarURL     string     `json:"avatar_url,omitempty"`
	Phone         string     `json:"phone,omitempty"`
	Status        UserStatus `json:"status"`
	GlobalUserID  string     `json:"global_user_id,omitempty"`
	LoginFailCount int       `json:"-"`
	LockedUntil   *time.Time `json:"-"`
	TokenVersion  int        `json:"-"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	LastLoginAt   *time.Time `json:"last_login_at,omitempty"`
}

func (u *User) IsActive() bool {
	return u != nil && u.Status == UserStatusActive
}

func (u *User) IsLocked(now time.Time) bool {
	return u != nil && u.LockedUntil != nil && u.LockedUntil.After(now)
}

func (u *User) HasPassword() bool {
	return u != nil && u.PasswordHash != ""
}

func (u *User) Clone() *User {
	if u == nil {
		return nil
	}
	out := *u
	if u.LockedUntil != nil {
		t := *u.LockedUntil
		out.LockedUntil = &t
	}
	if u.LastLoginAt != nil {
		t := *u.LastLoginAt
		out.LastLoginAt = &t
	}
	return &out
}

// OAuthProvider names.
const (
	ProviderGoogle = "google"
	ProviderApple  = "apple"
)

// OAuthAccount links an external identity to a user.
type OAuthAccount struct {
	AccountID      string    `json:"account_id"`
	AppID          string    `json:"app_id"`
	UserID         string    `json:"user_id"`
	Provider       string    `json:"provider"`
	ProviderUserID string    `json:"provider_user_id"`
	Email          string    `json:"email,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

func (a *OAuthAccount) Clone() *OAuthAccount {
	if a == nil {
		return nil
	}
	out := *a
	return &out
}

// RefreshTokenRecord stores a hashed refresh JWT with family rotation support.
type RefreshTokenRecord struct {
	TokenID   string    `json:"token_id"`
	UserID    string    `json:"user_id"`
	AppID     string    `json:"app_id"`
	TokenHash string    `json:"-"`
	FamilyID  string    `json:"family_id"`
	ExpiresAt time.Time `json:"expires_at"`
	Revoked   bool      `json:"revoked"`
	CreatedAt time.Time `json:"created_at"`
}

func (r *RefreshTokenRecord) Clone() *RefreshTokenRecord {
	if r == nil {
		return nil
	}
	out := *r
	return &out
}

func (r *RefreshTokenRecord) IsValid(now time.Time) bool {
	return r != nil && !r.Revoked && r.ExpiresAt.After(now)
}

// Verification token types.
const (
	VerifyTypeEmailVerification = "email_verification"
	VerifyTypePasswordReset     = "password_reset"
)

// VerificationToken is a one-time email verification or password-reset token.
type VerificationToken struct {
	TokenID   string    `json:"token_id"`
	UserID    string    `json:"user_id"`
	AppID     string    `json:"app_id"`
	TokenType string    `json:"token_type"`
	TokenHash string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	Used      bool      `json:"used"`
	CreatedAt time.Time `json:"created_at"`
}

func (t *VerificationToken) Clone() *VerificationToken {
	if t == nil {
		return nil
	}
	out := *t
	return &out
}

func (t *VerificationToken) IsValid(now time.Time) bool {
	return t != nil && !t.Used && t.ExpiresAt.After(now)
}

// AppAuthConfig holds per-app OAuth credentials (encrypted at rest).
type AppAuthConfig struct {
	AppID                       string    `json:"app_id"`
	GoogleClientID              *string   `json:"google_client_id,omitempty"`
	GoogleClientSecretEncrypted *string   `json:"-"`
	AppleClientID               *string   `json:"apple_client_id,omitempty"`
	AppleTeamID                 *string   `json:"apple_team_id,omitempty"`
	AppleKeyID                  *string   `json:"apple_key_id,omitempty"`
	ApplePrivateKeyEncrypted    *string   `json:"-"`
	UpdatedAt                   time.Time `json:"updated_at"`
}

func (c *AppAuthConfig) GoogleConfigured() bool {
	return c != nil &&
		c.GoogleClientID != nil && *c.GoogleClientID != "" &&
		c.GoogleClientSecretEncrypted != nil && *c.GoogleClientSecretEncrypted != ""
}

func (c *AppAuthConfig) AppleConfigured() bool {
	return c != nil &&
		c.AppleClientID != nil && *c.AppleClientID != "" &&
		c.AppleTeamID != nil && *c.AppleTeamID != "" &&
		c.AppleKeyID != nil && *c.AppleKeyID != "" &&
		c.ApplePrivateKeyEncrypted != nil && *c.ApplePrivateKeyEncrypted != ""
}

// GooglePublicConfigured is true when only client_id is set (id_token MVP path).
func (c *AppAuthConfig) GooglePublicConfigured() bool {
	return c != nil && c.GoogleClientID != nil && *c.GoogleClientID != ""
}

// ApplePublicConfigured is true when Apple client_id is set (id_token aud check).
func (c *AppAuthConfig) ApplePublicConfigured() bool {
	return c != nil && c.AppleClientID != nil && *c.AppleClientID != ""
}

func (c *AppAuthConfig) Clone() *AppAuthConfig {
	if c == nil {
		return nil
	}
	out := *c
	out.GoogleClientID = cloneStrPtr(c.GoogleClientID)
	out.GoogleClientSecretEncrypted = cloneStrPtr(c.GoogleClientSecretEncrypted)
	out.AppleClientID = cloneStrPtr(c.AppleClientID)
	out.AppleTeamID = cloneStrPtr(c.AppleTeamID)
	out.AppleKeyID = cloneStrPtr(c.AppleKeyID)
	out.ApplePrivateKeyEncrypted = cloneStrPtr(c.ApplePrivateKeyEncrypted)
	return &out
}

func cloneStrPtr(p *string) *string {
	if p == nil {
		return nil
	}
	s := *p
	return &s
}
