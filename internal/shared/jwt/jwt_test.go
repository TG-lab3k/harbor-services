package jwt

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func encodePKCS1PrivatePEM(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	b := x509.MarshalPKCS1PrivateKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: b}))
}

func TestKidStableAcrossRestarts(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemStr := encodePKCS1PrivatePEM(t, key)

	s1, err := NewService(Options{Issuer: "test", PrivateKeyPEM: pemStr})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := NewService(Options{Issuer: "test", PrivateKeyPEM: pemStr})
	if err != nil {
		t.Fatal(err)
	}
	if s1.Kid() == "" || s2.Kid() == "" {
		t.Fatal("empty kid")
	}
	if s1.Kid() != s2.Kid() {
		t.Fatalf("kid not stable: %q vs %q", s1.Kid(), s2.Kid())
	}

	jwks := s1.JWKS()
	keys, _ := jwks["keys"].([]map[string]interface{})
	if len(keys) != 1 || keys[0]["kid"] != s1.Kid() {
		t.Fatalf("jwks kid mismatch: %+v", jwks)
	}
}

func TestRequirePEMWhenEphemeralDisabled(t *testing.T) {
	_, err := NewService(Options{Issuer: "test", AllowEphemeralKey: false})
	if err == nil {
		t.Fatal("expected error without PEM")
	}
}

func TestAllowEphemeralKey(t *testing.T) {
	s, err := NewService(Options{Issuer: "test", AllowEphemeralKey: true})
	if err != nil {
		t.Fatal(err)
	}
	if s.Kid() == "" {
		t.Fatal("empty kid")
	}
}
