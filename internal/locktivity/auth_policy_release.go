//go:build !dev

package locktivity

import (
	"net/url"
	"strings"
)

// IsAllowedAllModeAuthURL returns true when an auth URL is permitted for
// LOCKTIVITY_AUTH_MODE=all in release builds.
func IsAllowedAllModeAuthURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if strings.ToLower(u.Scheme) != "https" {
		return false
	}
	if port := u.Port(); port != "" && port != "443" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "app.locktivity.com"
}
