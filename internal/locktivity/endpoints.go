package locktivity

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/locktivity/epack-remote-locktivity/internal/securityaudit"
	"github.com/locktivity/epack-remote-locktivity/internal/securitypolicy"
)

// EndpointConfig is the resolved runtime endpoint configuration.
type EndpointConfig struct {
	APIURL     string
	AuthURL    string
	Custom     bool
	CustomAPI  string
	CustomAuth string
}

// ResolveEndpointConfig resolves runtime endpoint overrides from environment.
// EPACK_REMOTE_ENDPOINT and EPACK_REMOTE_AUTH_ENDPOINT are trusted as coming from
// epack's validated insecure_endpoint config.
func ResolveEndpointConfig(getenv func(string) string) (EndpointConfig, error) {
	cfg, err := resolveEndpointConfig(getenv)
	if err != nil {
		return EndpointConfig{}, err
	}

	if err := securitypolicy.EnforceStrictProduction("locktivity_remote_handler", cfg.Custom); err != nil {
		return EndpointConfig{}, err
	}
	if cfg.Custom {
		securityaudit.Emit(securityaudit.Event{
			Type:        securityaudit.EventInsecureBypass,
			Component:   "locktivity_remote",
			Name:        "handler",
			Description: "locktivity remote running with custom endpoints",
			Attrs:       cfg.AuditAttrs(),
		})
	}

	return cfg, nil
}

func resolveEndpointConfig(getenv func(string) string) (EndpointConfig, error) {
	cfg := EndpointConfig{
		APIURL:  DefaultBaseURL,
		AuthURL: DefaultAuthBaseURL,
	}

	customAPI := firstNonEmpty(strings.TrimSpace(getenv(EnvRemoteEndpoint)), strings.TrimSpace(getenv(EnvEndpoint)))
	customAuth := firstNonEmpty(strings.TrimSpace(getenv(EnvRemoteAuthEndpoint)), strings.TrimSpace(getenv(EnvAuthEndpoint)))

	if customAPI == "" && customAuth == "" {
		return cfg, nil
	}

	var err error
	if customAPI != "" {
		customAPI, err = validateCustomEndpoint("endpoint", customAPI)
		if err != nil {
			return EndpointConfig{}, err
		}
		cfg.APIURL = customAPI
		cfg.CustomAPI = customAPI
		cfg.Custom = true
	}
	if customAuth != "" {
		customAuth, err = validateCustomEndpoint("auth endpoint", customAuth)
		if err != nil {
			return EndpointConfig{}, err
		}
		cfg.AuthURL = customAuth
		cfg.CustomAuth = customAuth
		cfg.Custom = true
	}
	return cfg, nil
}

// Endpoints resolves runtime endpoints using process environment.
func Endpoints() (string, string, error) {
	cfg, err := ResolveEndpointConfig(os.Getenv)
	if err != nil {
		return "", "", err
	}
	return cfg.APIURL, cfg.AuthURL, nil
}

// AuditAttrs returns structured metadata for custom endpoint overrides.
func (c EndpointConfig) AuditAttrs() map[string]string {
	if !c.Custom {
		return nil
	}
	attrs := map[string]string{
		"insecure_allow_custom_endpoints": "true",
	}
	if c.CustomAPI != "" {
		attrs["remote_endpoint_host"] = endpointHost(c.CustomAPI)
		attrs["remote_endpoint_has_path"] = boolString(endpointHasPath(c.CustomAPI))
	}
	if c.CustomAuth != "" {
		attrs["remote_auth_endpoint_host"] = endpointHost(c.CustomAuth)
		attrs["remote_auth_endpoint_has_path"] = boolString(endpointHasPath(c.CustomAuth))
	}
	return attrs
}

// WarnCustomEndpoints writes operator-visible warnings when custom endpoints are active.
func (c EndpointConfig) WarnCustomEndpoints() {
	if !c.Custom {
		return
	}
	if c.CustomAPI != "" {
		_, _ = fmt.Fprintf(os.Stderr, "WARNING: Running with custom remote endpoint %s.\n", c.CustomAPI)
	}
	if c.CustomAuth != "" {
		_, _ = fmt.Fprintf(os.Stderr, "WARNING: Running with custom remote auth endpoint %s.\n", c.CustomAuth)
	}
}

// IsAllowedAllModeAuthURL returns true when an auth URL is permitted for LOCKTIVITY_AUTH_MODE=all.
func IsAllowedAllModeAuthURL(raw string) bool {
	validated, err := validateCustomEndpoint("auth endpoint", raw)
	if err != nil {
		return false
	}
	if isDefaultAllowedAuthEndpoint(validated) {
		return true
	}

	cfg, err := resolveEndpointConfig(os.Getenv)
	if err != nil {
		return false
	}
	return cfg.CustomAuth != "" && validated == cfg.CustomAuth
}

func isDefaultAllowedAuthEndpoint(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if strings.ToLower(parsed.Scheme) != "https" {
		return false
	}
	if strings.ToLower(parsed.Hostname()) != "app.locktivity.com" {
		return false
	}
	port := parsed.Port()
	return port == "" || port == "443"
}

func validateCustomEndpoint(field, raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("%s: invalid URL: %w", field, err)
	}
	if strings.ToLower(parsed.Scheme) != "https" {
		return "", fmt.Errorf("%s: must use HTTPS (got %q)", field, parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("%s: missing host", field)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("%s: userinfo is not allowed", field)
	}
	if parsed.RawQuery != "" {
		return "", fmt.Errorf("%s: query parameters are not allowed", field)
	}
	if parsed.Fragment != "" {
		return "", fmt.Errorf("%s: fragments are not allowed", field)
	}
	return parsed.String(), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func endpointHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Host
}

func endpointHasPath(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return parsed.Path != "" && parsed.Path != "/"
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
