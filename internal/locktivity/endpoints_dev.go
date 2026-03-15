//go:build dev

package locktivity

import "os"

// Endpoints returns API and auth endpoints, allowing env overrides for dev builds.
func Endpoints() (apiURL, authURL string) {
	apiURL = DefaultBaseURL
	authURL = DefaultAuthBaseURL

	if endpoint := os.Getenv(EnvEndpoint); endpoint != "" {
		apiURL = endpoint
	}
	if authEndpoint := os.Getenv(EnvAuthEndpoint); authEndpoint != "" {
		authURL = authEndpoint
	}

	return apiURL, authURL
}
