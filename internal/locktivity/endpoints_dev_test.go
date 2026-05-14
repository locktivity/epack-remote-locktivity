//go:build dev

package locktivity

import (
	"testing"

	"github.com/locktivity/epack-remote-locktivity/internal/securityaudit"
)

func TestValidateCustomEndpoint_AllowsHTTPInDevBuilds(t *testing.T) {
	got, err := validateCustomEndpoint("endpoint", "http://api.localhost:3000")
	if err != nil {
		t.Fatalf("expected http:// to be allowed in dev builds, got error: %v", err)
	}
	if got != "http://api.localhost:3000" {
		t.Fatalf("unexpected normalized URL: %q", got)
	}
}

func TestResolveEndpointConfig_AllowsHTTPInDevBuildsAndEmitsAudit(t *testing.T) {
	sink := &auditSink{}
	securityaudit.SetSink(sink)
	t.Cleanup(func() { securityaudit.SetSink(nil) })

	cfg, err := ResolveEndpointConfig(func(name string) string {
		switch name {
		case EnvRemoteEndpoint:
			return "http://api.localhost:3000"
		case EnvRemoteAuthEndpoint:
			return "http://localhost:3000"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("ResolveEndpointConfig() error = %v", err)
	}
	if cfg.APIURL != "http://api.localhost:3000" {
		t.Fatalf("APIURL = %q", cfg.APIURL)
	}
	if !cfg.Custom {
		t.Fatal("expected Custom=true")
	}

	events := sink.Snapshot()
	if len(events) == 0 {
		t.Fatal("expected insecure bypass event for http custom endpoint")
	}
	got := events[0]
	if got.Attrs["remote_endpoint_scheme"] != "http" || got.Attrs["remote_auth_endpoint_scheme"] != "http" {
		t.Fatalf("expected http scheme in audit attrs, got: %+v", got.Attrs)
	}
}
