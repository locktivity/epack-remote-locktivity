//go:build dev

package locktivity

import (
	"net"
	"net/url"
	"strings"
)

// IsAllowedAllModeAuthURL returns true when an auth URL is permitted for
// LOCKTIVITY_AUTH_MODE=all in dev builds.
func IsAllowedAllModeAuthURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return false
	}

	host := strings.ToLower(u.Hostname())

	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}

	return isAllowedLocktivityHost(host, scheme, u.Port())
}

func isAllowedLocktivityHost(host, scheme, port string) bool {
	if host != "app.locktivity.com" {
		return false
	}
	if scheme != "https" {
		return false
	}
	return port == "" || port == "443"
}
