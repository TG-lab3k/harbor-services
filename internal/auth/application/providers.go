package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/okok/harbor-services/internal/auth/domain"
	"github.com/okok/harbor-services/internal/platform/apperr"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

func decodeB64URL(seg string) ([]byte, error) {
	switch len(seg) % 4 {
	case 2:
		seg += "=="
	case 3:
		seg += "="
	}
	return base64.URLEncoding.DecodeString(seg)
}

// DefaultProviderFactory is a placeholder; callers should use buildNamedProvider.
func DefaultProviderFactory(cfg *domain.AppAuthConfig, googleSecret, appleKey string) (OAuthProvider, error) {
	_ = cfg
	_ = googleSecret
	_ = appleKey
	return nil, nil
}

func buildNamedProvider(provider string, cfg *domain.AppAuthConfig, googleSecret, appleKey string) (OAuthProvider, error) {
	switch provider {
	case domain.ProviderGoogle:
		if cfg == nil || !cfg.GooglePublicConfigured() {
			return nil, apperr.ProviderNotConfigured("")
		}
		return &GoogleProvider{
			ClientID:     *cfg.GoogleClientID,
			ClientSecret: googleSecret,
		}, nil
	case domain.ProviderApple:
		if cfg == nil || !cfg.ApplePublicConfigured() {
			return nil, apperr.ProviderNotConfigured("")
		}
		return &AppleProvider{
			ClientID:   *cfg.AppleClientID,
			TeamID:     strOrEmpty(cfg.AppleTeamID),
			KeyID:      strOrEmpty(cfg.AppleKeyID),
			PrivateKey: appleKey,
		}, nil
	default:
		return nil, apperr.Validation("unsupported provider")
	}
}

func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// GoogleProvider implements Google OAuth (code exchange + id_token).
type GoogleProvider struct {
	ClientID     string
	ClientSecret string
	HTTPClient   *http.Client
}

func (p *GoogleProvider) Name() string { return domain.ProviderGoogle }

func (p *GoogleProvider) client() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return http.DefaultClient
}

func (p *GoogleProvider) oauthConfig(redirectURI string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		RedirectURL:  redirectURI,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
}

func (p *GoogleProvider) AuthorizeURL(redirectURI, state string) string {
	cfg := p.oauthConfig(redirectURI)
	return cfg.AuthCodeURL(state, oauth2.AccessTypeOnline, oauth2.SetAuthURLParam("prompt", "select_account"))
}

func (p *GoogleProvider) Exchange(ctx context.Context, code, redirectURI string) (*OAuthProfile, error) {
	if p.ClientSecret == "" {
		return nil, apperr.OAuthFailed("google client secret required for code exchange")
	}
	cfg := p.oauthConfig(redirectURI)
	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return nil, apperr.OAuthFailed(err.Error())
	}
	rawID, ok := tok.Extra("id_token").(string)
	if ok && rawID != "" {
		return p.VerifyIDToken(ctx, rawID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/oauth2/v3/userinfo", nil)
	if err != nil {
		return nil, apperr.OAuthFailed(err.Error())
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp, err := p.client().Do(req)
	if err != nil {
		return nil, apperr.OAuthFailed(err.Error())
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, apperr.OAuthFailed(string(body))
	}
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, apperr.OAuthFailed("invalid userinfo")
	}
	return profileFromClaims(m), nil
}

func (p *GoogleProvider) VerifyIDToken(ctx context.Context, idToken string) (*OAuthProfile, error) {
	u := "https://oauth2.googleapis.com/tokeninfo?id_token=" + url.QueryEscape(idToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err == nil {
		resp, err := p.client().Do(req)
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode < 300 {
				var m map[string]interface{}
				if json.Unmarshal(body, &m) == nil {
					aud, _ := m["aud"].(string)
					if aud != "" && aud != p.ClientID {
						return nil, apperr.OAuthFailed("invalid audience")
					}
					return profileFromClaims(m), nil
				}
			}
		}
	}
	m, err := ParseUnverifiedJWTPayload(idToken)
	if err != nil {
		return nil, apperr.OAuthFailed("invalid id_token")
	}
	if !audienceMatches(m["aud"], p.ClientID) {
		return nil, apperr.OAuthFailed("invalid audience")
	}
	return profileFromClaims(m), nil
}

// AppleProvider verifies Apple id_tokens (basic aud check) for MVP.
type AppleProvider struct {
	ClientID   string
	TeamID     string
	KeyID      string
	PrivateKey string
}

func (p *AppleProvider) Name() string { return domain.ProviderApple }

func (p *AppleProvider) AuthorizeURL(redirectURI, state string) string {
	v := url.Values{}
	v.Set("response_type", "code id_token")
	v.Set("response_mode", "form_post")
	v.Set("client_id", p.ClientID)
	v.Set("redirect_uri", redirectURI)
	v.Set("scope", "name email")
	v.Set("state", state)
	return "https://appleid.apple.com/auth/authorize?" + v.Encode()
}

func (p *AppleProvider) Exchange(ctx context.Context, code, redirectURI string) (*OAuthProfile, error) {
	_ = ctx
	_ = code
	_ = redirectURI
	return nil, apperr.OAuthFailed("apple code exchange requires id_token on callback for MVP")
}

func (p *AppleProvider) VerifyIDToken(ctx context.Context, idToken string) (*OAuthProfile, error) {
	_ = ctx
	m, err := ParseUnverifiedJWTPayload(idToken)
	if err != nil {
		return nil, apperr.OAuthFailed("invalid id_token")
	}
	if !audienceMatches(m["aud"], p.ClientID) {
		return nil, apperr.OAuthFailed("invalid audience")
	}
	iss, _ := m["iss"].(string)
	if iss != "" && iss != "https://appleid.apple.com" {
		return nil, apperr.OAuthFailed("invalid issuer")
	}
	return profileFromClaims(m), nil
}

func audienceMatches(aud interface{}, clientID string) bool {
	switch v := aud.(type) {
	case string:
		return v == clientID
	case []interface{}:
		for _, a := range v {
			if s, ok := a.(string); ok && s == clientID {
				return true
			}
		}
	}
	return false
}

func profileFromClaims(m map[string]interface{}) *OAuthProfile {
	sub, _ := m["sub"].(string)
	email, _ := m["email"].(string)
	name, _ := m["name"].(string)
	if name == "" {
		if given, _ := m["given_name"].(string); given != "" {
			name = given
		}
	}
	verified := false
	switch v := m["email_verified"].(type) {
	case bool:
		verified = v
	case string:
		verified = strings.EqualFold(v, "true")
	}
	return &OAuthProfile{
		ProviderUserID: sub,
		Email:          email,
		EmailVerified:  verified,
		Name:           name,
	}
}
