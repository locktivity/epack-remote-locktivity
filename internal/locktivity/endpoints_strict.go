//go:build !dev

package locktivity

// allowInsecureHTTPInDevBuilds gates whether plain http:// custom endpoints
// are accepted by validateCustomEndpoint. False in release.
const allowInsecureHTTPInDevBuilds = false
