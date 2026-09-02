package infrastructure

import (
	"context"
	"sync"

	"github.com/okok/harbor-services/internal/auth/domain"
	"github.com/okok/harbor-services/internal/platform/apperr"
)

// MemoryStore implements all Auth repositories in-process.
type MemoryStore struct {
	mu            sync.RWMutex
	users         map[string]*domain.User
	emailIndex    map[string]string // appID|email -> userID
	oauth         map[string]*domain.OAuthAccount
	oauthIndex    map[string]string // appID|provider|providerUserID -> accountID
	oauthByUser   map[string][]string
	refresh       map[string]*domain.RefreshTokenRecord
	refreshByHash map[string]string
	verify        map[string]*domain.VerificationToken
	verifyByHash  map[string]string
	authConfigs   map[string]*domain.AppAuthConfig
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		users:         make(map[string]*domain.User),
		emailIndex:    make(map[string]string),
		oauth:         make(map[string]*domain.OAuthAccount),
		oauthIndex:    make(map[string]string),
		oauthByUser:   make(map[string][]string),
		refresh:       make(map[string]*domain.RefreshTokenRecord),
		refreshByHash: make(map[string]string),
		verify:        make(map[string]*domain.VerificationToken),
		verifyByHash:  make(map[string]string),
		authConfigs:   make(map[string]*domain.AppAuthConfig),
	}
}

func emailKey(appID, email string) string {
	return appID + "|" + email
}

func oauthKey(appID, provider, providerUserID string) string {
	return appID + "|" + provider + "|" + providerUserID
}

// --- UserRepository ---

func (s *MemoryStore) Create(ctx context.Context, user *domain.User) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[user.UserID]; ok {
		return apperr.Validation("user already exists")
	}
	if user.Email != "" {
		if _, ok := s.emailIndex[emailKey(user.AppID, user.Email)]; ok {
			return apperr.EmailExists("")
		}
		s.emailIndex[emailKey(user.AppID, user.Email)] = user.UserID
	}
	s.users[user.UserID] = user.Clone()
	return nil
}

func (s *MemoryStore) GetByID(ctx context.Context, userID string) (*domain.User, error) {
	_ = ctx
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[userID]
	if !ok {
		return nil, nil
	}
	return u.Clone(), nil
}

func (s *MemoryStore) GetByEmail(ctx context.Context, appID, email string) (*domain.User, error) {
	_ = ctx
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.emailIndex[emailKey(appID, email)]
	if !ok {
		return nil, nil
	}
	u, ok := s.users[id]
	if !ok {
		return nil, nil
	}
	return u.Clone(), nil
}

func (s *MemoryStore) Update(ctx context.Context, user *domain.User) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.users[user.UserID]
	if !ok {
		return apperr.Validation("user not found")
	}
	if old.Email != "" && old.Email != user.Email {
		delete(s.emailIndex, emailKey(old.AppID, old.Email))
	}
	if user.Email != "" {
		if existing, ok := s.emailIndex[emailKey(user.AppID, user.Email)]; ok && existing != user.UserID {
			return apperr.EmailExists("")
		}
		s.emailIndex[emailKey(user.AppID, user.Email)] = user.UserID
	}
	s.users[user.UserID] = user.Clone()
	return nil
}

// --- OAuthAccountRepository ---

func (s *MemoryStore) createOAuth(ctx context.Context, account *domain.OAuthAccount) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	key := oauthKey(account.AppID, account.Provider, account.ProviderUserID)
	if _, ok := s.oauthIndex[key]; ok {
		return apperr.OAuthLinked("")
	}
	s.oauth[account.AccountID] = account.Clone()
	s.oauthIndex[key] = account.AccountID
	s.oauthByUser[account.UserID] = append(s.oauthByUser[account.UserID], account.AccountID)
	return nil
}

// OAuthAccountRepository methods with interface-compatible names via adapters below.

type oauthRepoAdapter struct{ *MemoryStore }

func (a oauthRepoAdapter) Create(ctx context.Context, account *domain.OAuthAccount) error {
	return a.createOAuth(ctx, account)
}

func (a oauthRepoAdapter) GetByProvider(ctx context.Context, appID, provider, providerUserID string) (*domain.OAuthAccount, error) {
	_ = ctx
	a.mu.RLock()
	defer a.mu.RUnlock()
	id, ok := a.oauthIndex[oauthKey(appID, provider, providerUserID)]
	if !ok {
		return nil, nil
	}
	acct, ok := a.oauth[id]
	if !ok {
		return nil, nil
	}
	return acct.Clone(), nil
}

func (a oauthRepoAdapter) ListByUser(ctx context.Context, userID string) ([]*domain.OAuthAccount, error) {
	_ = ctx
	a.mu.RLock()
	defer a.mu.RUnlock()
	ids := a.oauthByUser[userID]
	out := make([]*domain.OAuthAccount, 0, len(ids))
	for _, id := range ids {
		if acct, ok := a.oauth[id]; ok {
			out = append(out, acct.Clone())
		}
	}
	return out, nil
}

func (a oauthRepoAdapter) Delete(ctx context.Context, accountID string) error {
	_ = ctx
	a.mu.Lock()
	defer a.mu.Unlock()
	acct, ok := a.oauth[accountID]
	if !ok {
		return nil
	}
	delete(a.oauth, accountID)
	delete(a.oauthIndex, oauthKey(acct.AppID, acct.Provider, acct.ProviderUserID))
	ids := a.oauthByUser[acct.UserID]
	kept := ids[:0]
	for _, id := range ids {
		if id != accountID {
			kept = append(kept, id)
		}
	}
	a.oauthByUser[acct.UserID] = kept
	return nil
}

func (s *MemoryStore) OAuthRepo() domain.OAuthAccountRepository {
	return oauthRepoAdapter{s}
}

// --- RefreshTokenRepository ---

type refreshRepoAdapter struct{ *MemoryStore }

func (a refreshRepoAdapter) Create(ctx context.Context, record *domain.RefreshTokenRecord) error {
	_ = ctx
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refresh[record.TokenID] = record.Clone()
	a.refreshByHash[record.TokenHash] = record.TokenID
	return nil
}

func (a refreshRepoAdapter) GetByHash(ctx context.Context, tokenHash string) (*domain.RefreshTokenRecord, error) {
	_ = ctx
	a.mu.RLock()
	defer a.mu.RUnlock()
	id, ok := a.refreshByHash[tokenHash]
	if !ok {
		return nil, nil
	}
	r, ok := a.refresh[id]
	if !ok {
		return nil, nil
	}
	return r.Clone(), nil
}

func (a refreshRepoAdapter) Revoke(ctx context.Context, tokenID string) error {
	_ = ctx
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.refresh[tokenID]
	if !ok {
		return nil
	}
	cloned := r.Clone()
	cloned.Revoked = true
	a.refresh[tokenID] = cloned
	return nil
}

func (a refreshRepoAdapter) RevokeFamily(ctx context.Context, familyID string) error {
	_ = ctx
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, r := range a.refresh {
		if r.FamilyID == familyID && !r.Revoked {
			cloned := r.Clone()
			cloned.Revoked = true
			a.refresh[id] = cloned
		}
	}
	return nil
}

func (a refreshRepoAdapter) RevokeAllForUser(ctx context.Context, userID string) error {
	_ = ctx
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, r := range a.refresh {
		if r.UserID == userID && !r.Revoked {
			cloned := r.Clone()
			cloned.Revoked = true
			a.refresh[id] = cloned
		}
	}
	return nil
}

func (s *MemoryStore) RefreshRepo() domain.RefreshTokenRepository {
	return refreshRepoAdapter{s}
}

// --- VerificationTokenRepository ---

type verifyRepoAdapter struct{ *MemoryStore }

func (a verifyRepoAdapter) Create(ctx context.Context, token *domain.VerificationToken) error {
	_ = ctx
	a.mu.Lock()
	defer a.mu.Unlock()
	a.verify[token.TokenID] = token.Clone()
	a.verifyByHash[token.TokenHash] = token.TokenID
	return nil
}

func (a verifyRepoAdapter) GetByHash(ctx context.Context, tokenHash string) (*domain.VerificationToken, error) {
	_ = ctx
	a.mu.RLock()
	defer a.mu.RUnlock()
	id, ok := a.verifyByHash[tokenHash]
	if !ok {
		return nil, nil
	}
	t, ok := a.verify[id]
	if !ok {
		return nil, nil
	}
	return t.Clone(), nil
}

func (a verifyRepoAdapter) MarkUsed(ctx context.Context, tokenID string) error {
	_ = ctx
	a.mu.Lock()
	defer a.mu.Unlock()
	t, ok := a.verify[tokenID]
	if !ok {
		return nil
	}
	cloned := t.Clone()
	cloned.Used = true
	a.verify[tokenID] = cloned
	return nil
}

func (s *MemoryStore) VerifyRepo() domain.VerificationTokenRepository {
	return verifyRepoAdapter{s}
}

// --- AuthConfigRepository ---

type authConfigRepoAdapter struct{ *MemoryStore }

func (a authConfigRepoAdapter) Get(ctx context.Context, appID string) (*domain.AppAuthConfig, error) {
	_ = ctx
	a.mu.RLock()
	defer a.mu.RUnlock()
	cfg, ok := a.authConfigs[appID]
	if !ok {
		return nil, nil
	}
	return cfg.Clone(), nil
}

func (a authConfigRepoAdapter) Upsert(ctx context.Context, cfg *domain.AppAuthConfig) error {
	_ = ctx
	a.mu.Lock()
	defer a.mu.Unlock()
	a.authConfigs[cfg.AppID] = cfg.Clone()
	return nil
}

func (s *MemoryStore) AuthConfigRepo() domain.AuthConfigRepository {
	return authConfigRepoAdapter{s}
}

// UserRepo returns the store as UserRepository (methods already match).
func (s *MemoryStore) UserRepo() domain.UserRepository {
	return s
}
