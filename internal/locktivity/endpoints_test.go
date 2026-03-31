package locktivity

import (
	"strings"
	"sync"
	"testing"

	"github.com/locktivity/epack-remote-locktivity/internal/securityaudit"
	"github.com/locktivity/epack-remote-locktivity/internal/securitypolicy"
)

type auditSink struct {
	mu     sync.Mutex
	events []securityaudit.Event
}

func (s *auditSink) HandleSecurityEvent(evt securityaudit.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, evt)
}

func (s *auditSink) Snapshot() []securityaudit.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]securityaudit.Event, len(s.events))
	copy(out, s.events)
	return out
}

func TestResolveEndpointConfig_Defaults(t *testing.T) {
	cfg, err := ResolveEndpointConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("ResolveEndpointConfig() error = %v", err)
	}
	if cfg.APIURL != DefaultBaseURL {
		t.Fatalf("APIURL = %q, want %q", cfg.APIURL, DefaultBaseURL)
	}
	if cfg.AuthURL != DefaultAuthBaseURL {
		t.Fatalf("AuthURL = %q, want %q", cfg.AuthURL, DefaultAuthBaseURL)
	}
	if cfg.Custom {
		t.Fatal("expected Custom=false")
	}
}

func TestResolveEndpointConfig_UsesGenericOverrides(t *testing.T) {
	cfg, err := ResolveEndpointConfig(func(name string) string {
		switch name {
		case EnvRemoteEndpoint:
			return "https://api.dev.example"
		case EnvRemoteAuthEndpoint:
			return "https://auth.dev.example"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("ResolveEndpointConfig() error = %v", err)
	}
	if cfg.APIURL != "https://api.dev.example" {
		t.Fatalf("APIURL = %q", cfg.APIURL)
	}
	if cfg.AuthURL != "https://auth.dev.example" {
		t.Fatalf("AuthURL = %q", cfg.AuthURL)
	}
	if !cfg.Custom {
		t.Fatal("expected Custom=true")
	}
}

func TestResolveEndpointConfig_UsesLegacyFallback(t *testing.T) {
	cfg, err := ResolveEndpointConfig(func(name string) string {
		switch name {
		case EnvEndpoint:
			return "https://api.dev.example"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("ResolveEndpointConfig() error = %v", err)
	}
	if cfg.APIURL != "https://api.dev.example" {
		t.Fatalf("APIURL = %q", cfg.APIURL)
	}
	if cfg.AuthURL != DefaultAuthBaseURL {
		t.Fatalf("AuthURL = %q", cfg.AuthURL)
	}
}

func TestResolveEndpointConfig_StrictProductionBlocksCustomEndpoints(t *testing.T) {
	t.Setenv(securitypolicy.StrictProductionEnvVar, "1")

	_, err := ResolveEndpointConfig(func(name string) string {
		switch name {
		case EnvRemoteEndpoint:
			return "https://api.dev.example"
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatal("expected strict production error")
	}
	if !strings.Contains(err.Error(), "strict production mode forbids insecure execution overrides") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveEndpointConfig_EmitsAuditEvent(t *testing.T) {
	sink := &auditSink{}
	securityaudit.SetSink(sink)
	t.Cleanup(func() { securityaudit.SetSink(nil) })

	_, err := ResolveEndpointConfig(func(name string) string {
		switch name {
		case EnvRemoteEndpoint:
			return "https://api.dev.example"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("ResolveEndpointConfig() error = %v", err)
	}

	events := sink.Snapshot()
	for _, evt := range events {
		if evt.Type == securityaudit.EventInsecureBypass && evt.Component == "locktivity_remote" && evt.Attrs["remote_endpoint_host"] == "api.dev.example" {
			return
		}
	}
	t.Fatalf("expected insecure bypass event, got: %+v", events)
}

func TestIsAllowedAllModeAuthURL(t *testing.T) {
	t.Setenv(EnvRemoteAuthEndpoint, "https://auth.dev.example")

	tests := []struct {
		url  string
		want bool
	}{
		{url: "https://app.locktivity.com", want: true},
		{url: "https://app.locktivity.com:443", want: true},
		{url: "https://auth.dev.example", want: true},
		{url: "https://evil.example.com", want: false},
		{url: "http://auth.dev.example", want: false},
	}

	for _, tc := range tests {
		if got := IsAllowedAllModeAuthURL(tc.url); got != tc.want {
			t.Fatalf("IsAllowedAllModeAuthURL(%q)=%v want %v", tc.url, got, tc.want)
		}
	}
}

func TestIsAllowedAllModeAuthURL_DoesNotEmitAuditEvent(t *testing.T) {
	sink := &auditSink{}
	securityaudit.SetSink(sink)
	t.Cleanup(func() { securityaudit.SetSink(nil) })

	t.Setenv(EnvRemoteAuthEndpoint, "https://auth.dev.example")

	if !IsAllowedAllModeAuthURL("https://auth.dev.example") {
		t.Fatal("expected custom auth endpoint to be allowed")
	}

	if events := sink.Snapshot(); len(events) != 0 {
		t.Fatalf("expected auth URL validation to avoid audit side effects, got: %+v", events)
	}
}
