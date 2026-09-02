package config

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds process-wide settings loaded from environment.
type Config struct {
	Port             string
	DBBackend        string // memory | firestore
	GCPProjectID     string
	AdminAppID       string
	AdminEmails      []string
	JWTIssuer        string
	AccessTokenTTL   time.Duration
	RefreshTokenTTL  time.Duration
	EncryptionKey    string
	BcryptCost       int
	AppCacheTTLSec   int
	RSAPrivateKeyPEM string
	RSAPublicKeyPEM  string
	BaseURL          string
}

// Load reads configuration from environment variables with documented defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Port:             getenv("PORT", "8080"),
		DBBackend:        strings.ToLower(getenv("DB_BACKEND", "memory")),
		GCPProjectID:     os.Getenv("GCP_PROJECT_ID"),
		AdminAppID:       getenv("ADMIN_APP_ID", "harborAdmin"),
		AdminEmails:      parseAdminEmails(os.Getenv("ADMIN_EMAILS")),
		JWTIssuer:        getenv("JWT_ISSUER", "harbor-services"),
		AccessTokenTTL:   time.Duration(getenvInt("ACCESS_TOKEN_TTL", 7200)) * time.Second,
		RefreshTokenTTL:  time.Duration(getenvInt("REFRESH_TOKEN_TTL", 2592000)) * time.Second,
		EncryptionKey:    getenv("ENCRYPTION_KEY", "dev-encryption-key-change-me!!"),
		BcryptCost:       getenvInt("BCRYPT_COST", 12),
		AppCacheTTLSec:   getenvInt("APP_CACHE_TTL_SEC", 300),
		RSAPrivateKeyPEM: os.Getenv("RSA_PRIVATE_KEY_PEM"),
		RSAPublicKeyPEM:  os.Getenv("RSA_PUBLIC_KEY_PEM"),
		BaseURL:          getenv("BASE_URL", "http://localhost:8080"),
	}
	return cfg, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func parseAdminEmails(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	var emails []string
	if err := json.Unmarshal([]byte(raw), &emails); err != nil {
		// allow comma-separated fallback for local convenience
		parts := strings.Split(raw, ",")
		emails = make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.ToLower(strings.TrimSpace(p))
			if p != "" {
				emails = append(emails, p)
			}
		}
		return emails
	}
	out := make([]string, 0, len(emails))
	for _, e := range emails {
		e = strings.ToLower(strings.TrimSpace(e))
		if e != "" {
			out = append(out, e)
		}
	}
	return out
}
