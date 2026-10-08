package locktivity

import "time"

// API constants.
const (
	DefaultBaseURL     = "https://api.locktivity.com"
	DefaultAuthBaseURL = "https://app.locktivity.com"
	APIVersion         = "v1"
	APIPathPrefix      = "/management/v1/evidence_packs"

	// SigningKeysPerPage is the largest page the API serves.
	SigningKeysPerPage = 100
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
	OAuthTokenEndpoint     = "/oauth2/token"
	OAuthAuthorizeEndpoint = "/epack/oauth2/authorize"
	// OAuthSignInTokenEndpoint is where a browser sign-in's code is exchanged.
	// The general token endpoint refuses epack's codes.
	OAuthSignInTokenEndpoint = "/epack/oauth2/token"
	BrowserSignInLifetime    = 10 * time.Minute

	// CredentialBrokerPath resolves a pipeline's Locktivity-managed
	// credentials. It is on the API host, outside APIPathPrefix.
	CredentialBrokerPath = "/oidc/v1/credential_sets/resolve"

	// PublicClientID is the public OAuth client every epack install signs in through.
	PublicClientID = "epack"
)

// Environment variable names.
const (
	EnvAccessToken  = "LOCKTIVITY_ACCESS_TOKEN"
	EnvClientID     = "LOCKTIVITY_CLIENT_ID"
	EnvClientSecret = "LOCKTIVITY_CLIENT_SECRET"
	EnvOIDCToken    = "LOCKTIVITY_OIDC_TOKEN"
	EnvEndpoint     = "LOCKTIVITY_ENDPOINT"
	EnvAuthEndpoint = "LOCKTIVITY_AUTH_ENDPOINT"
	EnvAuthMode     = "LOCKTIVITY_AUTH_MODE"

	EnvRemoteEndpoint     = "EPACK_REMOTE_ENDPOINT"
	EnvRemoteAuthEndpoint = "EPACK_REMOTE_AUTH_ENDPOINT"
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
