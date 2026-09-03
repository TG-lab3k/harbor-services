package idgen

import (
	"crypto/rand"
	"encoding/base64"
)

// RandomURLSafe returns a URL-safe base64 string from n random bytes (no padding).
func RandomURLSafe(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("idgen: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// NewAppID returns app_ + 12 URL-safe random bytes.
func NewAppID() string {
	return "app_" + RandomURLSafe(12)
}

// NewUserID returns a 16-byte URL-safe id.
func NewUserID() string {
	return RandomURLSafe(16)
}

// NewTokenID returns a token document id (16 URL-safe bytes).
func NewTokenID() string {
	return RandomURLSafe(16)
}

// NewFamilyID returns a refresh-token family id.
func NewFamilyID() string {
	return RandomURLSafe(16)
}

// NewAccountID returns an OAuth account id.
func NewAccountID() string {
	return RandomURLSafe(16)
}

// NewSecret returns an n-byte URL-safe secret (app_secret uses 32).
func NewSecret(n int) string {
	if n <= 0 {
		n = 32
	}
	return RandomURLSafe(n)
}

// NewJTI returns a JWT id claim.
func NewJTI() string {
	return RandomURLSafe(16)
}

// NewOrderID returns a billing order id.
func NewOrderID() string {
	return "ord_" + RandomURLSafe(12)
}

// NewCheckoutID returns a billing checkout session id.
func NewCheckoutID() string {
	return "chk_" + RandomURLSafe(12)
}

// NewProductID returns a billing product id.
func NewProductID() string {
	return "prod_" + RandomURLSafe(12)
}

// NewWebhookEventID returns a webhook event document id.
func NewWebhookEventID() string {
	return "whe_" + RandomURLSafe(12)
}
