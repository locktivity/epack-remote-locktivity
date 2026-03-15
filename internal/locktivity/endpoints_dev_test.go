//go:build dev

package locktivity

import (
	"testing"
)

func TestEndpoints_DevUsesDefaultsWhenUnset(t *testing.T) {
	t.Setenv(EnvEndpoint, "")
	t.Setenv(EnvAuthEndpoint, "")

	apiURL, authURL := Endpoints()

	if apiURL != DefaultBaseURL {
		t.Fatalf("expected default api URL %q, got %q", DefaultBaseURL, apiURL)
	}
	if authURL != DefaultAuthBaseURL {
		t.Fatalf("expected default auth URL %q, got %q", DefaultAuthBaseURL, authURL)
	}
}

func TestEndpoints_DevUsesOverridesWhenSet(t *testing.T) {
	t.Setenv(EnvEndpoint, "http://api.localhost:3000")
	t.Setenv(EnvAuthEndpoint, "http://app.localhost:3000")

	apiURL, authURL := Endpoints()

	if apiURL != "http://api.localhost:3000" {
		t.Fatalf("expected api override, got %q", apiURL)
	}
	if authURL != "http://app.localhost:3000" {
		t.Fatalf("expected auth override, got %q", authURL)
	}
}
