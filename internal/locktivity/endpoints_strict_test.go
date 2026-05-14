//go:build !dev

package locktivity

import (
	"strings"
	"testing"
)

func TestValidateCustomEndpoint_RejectsHTTPInReleaseBuilds(t *testing.T) {
	_, err := validateCustomEndpoint("endpoint", "http://localhost:3000")
	if err == nil {
		t.Fatal("expected http:// to be rejected in release builds, got nil error")
	}
	if !strings.Contains(err.Error(), "must use HTTPS") {
		t.Fatalf("unexpected error: %v", err)
	}
}
