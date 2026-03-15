package auth

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestMemoryKeychain_AccessTokenLifecycle(t *testing.T) {
	k := NewMemoryKeychain()

	_, err := k.GetToken()
	if !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty token, got %v", err)
	}

	if err := k.SetToken("access_123"); err != nil {
		t.Fatalf("SetToken failed: %v", err)
	}

	token, err := k.GetToken()
	if err != nil {
		t.Fatalf("GetToken failed: %v", err)
	}
	if token != "access_123" {
		t.Fatalf("expected token access_123, got %q", token)
	}
}

func TestMemoryKeychain_RefreshTokenLifecycle(t *testing.T) {
	k := NewMemoryKeychain()

	_, err := k.GetRefreshToken()
	if !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty refresh token, got %v", err)
	}

	if err := k.SetRefreshToken("refresh_123"); err != nil {
		t.Fatalf("SetRefreshToken failed: %v", err)
	}

	token, err := k.GetRefreshToken()
	if err != nil {
		t.Fatalf("GetRefreshToken failed: %v", err)
	}
	if token != "refresh_123" {
		t.Fatalf("expected token refresh_123, got %q", token)
	}
}

func TestMemoryKeychain_Clear(t *testing.T) {
	k := NewMemoryKeychain()
	_ = k.SetToken("access_123")
	_ = k.SetRefreshToken("refresh_123")

	if err := k.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	_, err := k.GetToken()
	if !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cleared access token, got %v", err)
	}

	_, err = k.GetRefreshToken()
	if !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cleared refresh token, got %v", err)
	}
}

func TestKeyNamespace_FromAuthEndpoint(t *testing.T) {
	ns := keyNamespace("https://app.locktivity.com")
	if ns != "https|app.locktivity.com|443" {
		t.Fatalf("expected https|app.locktivity.com|443, got %q", ns)
	}
}

func TestKeyNamespace_InvalidURLDefaults(t *testing.T) {
	ns := keyNamespace("://bad-url")
	if ns != "default" {
		t.Fatalf("expected default namespace, got %q", ns)
	}
}

func TestKeyNamespace_IncludesPort(t *testing.T) {
	nsA := keyNamespace("https://app.locktivity.com:8443")
	nsB := keyNamespace("https://app.locktivity.com")
	if nsA == nsB {
		t.Fatalf("expected distinct namespaces for distinct ports, got %q", nsA)
	}
}
