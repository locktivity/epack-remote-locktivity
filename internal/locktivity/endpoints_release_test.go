//go:build !dev

package locktivity

import (
	"testing"
)

func TestEndpoints_ReleaseAlwaysUsesDefaults(t *testing.T) {
	t.Setenv(EnvEndpoint, "http://api.localhost:3000")
	t.Setenv(EnvAuthEndpoint, "http://app.localhost:3000")

	apiURL, authURL := Endpoints()

	if apiURL != DefaultBaseURL {
		t.Fatalf("expected default api URL %q, got %q", DefaultBaseURL, apiURL)
	}
	if authURL != DefaultAuthBaseURL {
		t.Fatalf("expected default auth URL %q, got %q", DefaultAuthBaseURL, authURL)
	}
}
