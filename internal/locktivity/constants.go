package locktivity

import "time"

// API constants.
const (
	DefaultBaseURL     = "https://api.locktivity.com"
	DefaultAuthBaseURL = "https://app.locktivity.com"
	APIVersion         = "v1"
	APIPathPrefix      = "/management/v1/evidence_packs"
)

// HTTP constants.
const (
	HTTPTimeout         = 30 * time.Second
	MaxRetries          = 3
	RetryBackoff        = time.Second
	MaxRetryBackoff     = 30 * time.Second
	AcceptHeader        = "application/json"
	ContentTypeHeader   = "application/json"
	AuthorizationHeader = "Authorization"
)

// OAuth constants.
const (
	OAuthTokenEndpoint      = "/oauth2/token"
	OAuthDeviceCodeEndpoint = "/oauth2/device/code"
	DeviceCodePollInterval  = 5 * time.Second
	DeviceCodeMaxPollTime   = 10 * time.Minute
)

// Environment variable names.
const (
	EnvClientID     = "LOCKTIVITY_CLIENT_ID"
	EnvClientSecret = "LOCKTIVITY_CLIENT_SECRET"
	EnvOIDCToken    = "LOCKTIVITY_OIDC_TOKEN"
	EnvEndpoint     = "LOCKTIVITY_ENDPOINT"
	EnvAuthEndpoint = "LOCKTIVITY_AUTH_ENDPOINT"
	EnvAuthMode     = "LOCKTIVITY_AUTH_MODE"
)

// Keychain constants.
const (
	KeychainService = "epack-remote-locktivity"
	KeychainAccount = "default"
)

// Size limits.
const (
	// MaxRunOutputSize is the maximum size for individual run output files (50MB).
	MaxRunOutputSize = 50_000_000
)
