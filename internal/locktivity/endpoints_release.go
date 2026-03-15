//go:build !dev

package locktivity

// Endpoints returns release-safe API and auth endpoints.
func Endpoints() (apiURL, authURL string) {
	return DefaultBaseURL, DefaultAuthBaseURL
}
