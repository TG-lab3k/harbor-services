package jwt

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"

	"github.com/okok/harbor-services/internal/shared/idgen"
)

const (
	TypeAccess  = "access"
	TypeRefresh = "refresh"
)

// Claims holds access/refresh JWT claims.
type Claims struct {
	Type   string `json:"type"`
	AppID  string `json:"app_id"`
	Email  string `json:"email,omitempty"`
	Role   string `json:"role,omitempty"`
	TV     int    `json:"tv,omitempty"`
	Family string `json:"family,omitempty"`
	jwtlib.RegisteredClaims
}

// Service issues and verifies RS256 JWTs and exposes JWKS.
type Service struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	kid        string
	issuer     string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

type Options struct {
	Issuer         string
	AccessTTL      time.Duration
	RefreshTTL     time.Duration
	PrivateKeyPEM  string
	PublicKeyPEM   string
}

func NewService(opts Options) (*Service, error) {
	if opts.Issuer == "" {
		opts.Issuer = "harbor-services"
	}
	if opts.AccessTTL <= 0 {
		opts.AccessTTL = 2 * time.Hour
	}
	if opts.RefreshTTL <= 0 {
		opts.RefreshTTL = 30 * 24 * time.Hour
	}

	var priv *rsa.PrivateKey
	var pub *rsa.PublicKey
	var err error

	if opts.PrivateKeyPEM != "" {
		priv, err = parsePrivateKeyPEM(opts.PrivateKeyPEM)
		if err != nil {
			return nil, err
		}
		pub = &priv.PublicKey
		if opts.PublicKeyPEM != "" {
			pub, err = parsePublicKeyPEM(opts.PublicKeyPEM)
			if err != nil {
				return nil, err
			}
		}
	} else {
		priv, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, fmt.Errorf("generate rsa: %w", err)
		}
		pub = &priv.PublicKey
	}

	kid := idgen.RandomURLSafe(8)
	return &Service{
		privateKey: priv,
		publicKey:  pub,
		kid:        kid,
		issuer:     opts.Issuer,
		accessTTL:  opts.AccessTTL,
		refreshTTL: opts.RefreshTTL,
	}, nil
}

func (s *Service) IssueAccess(userID, appID, email, role string, tv int) (string, error) {
	now := time.Now()
	claims := Claims{
		Type:  TypeAccess,
		AppID: appID,
		Email: email,
		Role:  role,
		TV:    tv,
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   userID,
			Audience:  jwtlib.ClaimStrings{appID},
			ID:        idgen.NewJTI(),
			IssuedAt:  jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(now.Add(s.accessTTL)),
		},
	}
	return s.sign(claims)
}

func (s *Service) IssueRefresh(userID, appID, familyID string) (string, error) {
	now := time.Now()
	claims := Claims{
		Type:   TypeRefresh,
		AppID:  appID,
		Family: familyID,
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   userID,
			ID:        idgen.NewJTI(),
			IssuedAt:  jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(now.Add(s.refreshTTL)),
		},
	}
	return s.sign(claims)
}

func (s *Service) sign(claims Claims) (string, error) {
	token := jwtlib.NewWithClaims(jwtlib.SigningMethodRS256, claims)
	token.Header["kid"] = s.kid
	return token.SignedString(s.privateKey)
}

func (s *Service) Verify(tokenString string) (*Claims, error) {
	token, err := jwtlib.ParseWithClaims(tokenString, &Claims{}, func(t *jwtlib.Token) (interface{}, error) {
		if t.Method != jwtlib.SigningMethodRS256 {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.publicKey, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// JWKS returns a JWKS map suitable for JSON serialization.
func (s *Service) JWKS() map[string]interface{} {
	n := base64.RawURLEncoding.EncodeToString(s.publicKey.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(s.publicKey.E)).Bytes())
	return map[string]interface{}{
		"keys": []map[string]interface{}{
			{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": s.kid,
				"n":   n,
				"e":   e,
			},
		},
	}
}

func parsePrivateKeyPEM(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("failed to decode private key PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("not an RSA private key")
	}
	return key, nil
}

func parsePublicKeyPEM(pemStr string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("failed to decode public key PEM")
	}
	if key, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("not an RSA public key")
	}
	return key, nil
}
