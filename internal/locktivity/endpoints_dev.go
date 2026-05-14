//go:build dev

package locktivity

// allowInsecureHTTPInDevBuilds gates whether plain http:// custom endpoints
// are accepted by validateCustomEndpoint. True in dev-tagged builds.
const allowInsecureHTTPInDevBuilds = true
