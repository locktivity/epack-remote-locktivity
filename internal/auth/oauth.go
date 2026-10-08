package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/locktivity/epack-remote-locktivity/internal/locktivity"
)

// TokenProvider provides access tokens for API calls.
type TokenProvider interface {
	// GetToken returns a valid access token.
	// It may refresh the token if needed.
	GetToken(ctx context.Context) (string, error)
}

// OAuth handles OAuth 2.0 authentication flows.
type OAuth struct {
	httpClient   *http.Client
	baseURL      string
	clientID     string
	clientSecret string
	keychain     Keychain
}

const tokenExpiryLeewaySeconds = int64(30)

// NewOAuth creates a new OAuth handler.
func NewOAuth(authURL string, keychain Keychain) *OAuth {
	if authURL == "" {
		authURL = locktivity.DefaultAuthBaseURL
	}

	return &OAuth{
		httpClient: &http.Client{
			Timeout:   locktivity.HTTPTimeout,
			Transport: newHTTPTransport(),
		},
		baseURL:  strings.TrimSuffix(authURL, "/"),
		keychain: keychain,
	}
}

// SetHTTPClient replaces the HTTP client, which tests use to fake the auth server.
func (o *OAuth) SetHTTPClient(client *http.Client) {
	o.httpClient = client
}

// SetClientCredentials sets the client ID and secret for client credentials flow.
func (o *OAuth) SetClientCredentials(clientID, clientSecret string) {
	o.clientID = clientID
	o.clientSecret = clientSecret
}

// GetToken returns a valid access token using the best available method.
// Token sources are tried in order: a pre-resolved access token, client_credentials_only
// mode, OIDC exchange, env-based client credentials, and finally stored tokens.
func (o *OAuth) GetToken(ctx context.Context) (string, error) {
	if token := accessTokenFromEnv(); token != "" {
		return token, nil
	}

	if err := o.validateAuthMode(); err != nil {
		return "", err
	}

	// Try each token source in priority order
	sources := []func(context.Context) (string, bool, error){
		o.tryClientCredentialsOnlyMode,
		o.tryOIDCExchange,
		o.tryEnvClientCredentials,
		o.tryStoredToken,
	}

	for _, source := range sources {
		token, decided, err := source(ctx)
		if err != nil {
			return "", err
		}
		if decided {
			return token, nil
		}
	}

	return "", fmt.Errorf("no authentication available: run 'epack remote login locktivity' or set environment variables")
}

func (o *OAuth) validateAuthMode() error {
	mode, err := EffectiveAuthMode()
	if err != nil {
		return err
	}
	if mode == AuthModeAll {
		return o.validateAllModeAuthEndpoint()
	}
	return nil
}

// tryClientCredentialsOnlyMode handles the strict client_credentials_only mode.
func (o *OAuth) tryClientCredentialsOnlyMode(ctx context.Context) (string, bool, error) {
	mode, _ := EffectiveAuthMode()
	if mode != AuthModeClientCredentialsOnly {
		return "", false, nil
	}

	creds, ok := envClientCredentials()
	if !ok {
		return "", true, fmt.Errorf(
			"%s=%s requires %s and %s",
			locktivity.EnvAuthMode,
			AuthModeClientCredentialsOnly,
			locktivity.EnvClientID,
			locktivity.EnvClientSecret,
		)
	}

	token, err := o.getOrFetchClientCredentials(ctx, creds)
	return token, true, err
}

// tryOIDCExchange attempts OIDC token exchange if configured.
func (o *OAuth) tryOIDCExchange(ctx context.Context) (string, bool, error) {
	oidcToken := oidcTokenFromEnv()
	if oidcToken == "" {
		return "", false, nil
	}

	token, err := o.exchangeOIDCToken(ctx, oidcToken)
	return token, true, err
}

// tryEnvClientCredentials attempts client credentials from environment.
func (o *OAuth) tryEnvClientCredentials(ctx context.Context) (string, bool, error) {
	creds, ok := envClientCredentials()
	if !ok {
		return "", false, nil
	}

	token, err := o.getOrFetchClientCredentials(ctx, creds)
	return token, true, err
}

// tryStoredToken attempts to use a cached token from the keychain.
func (o *OAuth) tryStoredToken(ctx context.Context) (string, bool, error) {
	token, ok := o.getUsableStoredToken(ctx)
	if !ok {
		return "", false, nil
	}
	return token, true, nil
}

// getOrFetchClientCredentials returns a cached token or fetches a new one.
// Only reuses cached tokens if they were obtained with the same client_id.
func (o *OAuth) getOrFetchClientCredentials(ctx context.Context, creds clientCredentials) (string, error) {
	// Try cached token first, but only if it matches the current client_id
	if token, ok := o.getUsableStoredTokenForClient(ctx, creds.clientID); ok {
		return token, nil
	}

	// Fetch fresh token and cache it
	tokenResp, err := o.doClientCredentialsGrant(ctx, creds.clientID, creds.clientSecret)
	if err != nil {
		return "", err
	}

	o.cacheTokenWithClientID(tokenResp, creds.clientID)
	return tokenResp.AccessToken, nil
}

// cacheTokenWithClientID stores a token response with associated client_id.
func (o *OAuth) cacheTokenWithClientID(tokenResp *locktivity.TokenResponse, clientID string) {
	if o.keychain == nil {
		return
	}

	if err := o.keychain.SetToken(tokenResp.AccessToken); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to cache token: %v\n", err)
		return
	}

	if tokenResp.ExpiresIn > 0 {
		expiry := time.Now().Unix() + int64(tokenResp.ExpiresIn)
		_ = o.keychain.SetTokenExpiry(expiry)
	}

	// Store client_id for validation on subsequent requests
	_ = o.keychain.SetClientID(clientID)

	// Client credentials don't have refresh tokens; clear any stale one
	_ = o.keychain.SetRefreshToken("")
}

// getUsableStoredTokenForClient returns a cached token only if it was obtained
// with the specified client_id. This prevents reusing tokens from different OAuth apps.
func (o *OAuth) getUsableStoredTokenForClient(ctx context.Context, clientID string) (string, bool) {
	if o.keychain == nil {
		return "", false
	}

	// Check if stored client_id matches
	storedClientID, err := o.keychain.GetClientID()
	if err != nil || storedClientID != clientID {
		return "", false
	}

	return o.getUsableStoredToken(ctx)
}

type clientCredentials struct {
	clientID     string
	clientSecret string
}

func envClientCredentials() (clientCredentials, bool) {
	creds := clientCredentials{
		clientID:     os.Getenv(locktivity.EnvClientID),
		clientSecret: os.Getenv(locktivity.EnvClientSecret),
	}
	return creds, creds.clientID != "" && creds.clientSecret != ""
}

func accessTokenFromEnv() string {
	return strings.TrimSpace(os.Getenv(locktivity.EnvAccessToken))
}

func oidcTokenFromEnv() string {
	return os.Getenv(locktivity.EnvOIDCToken)
}

func (o *OAuth) getUsableStoredToken(ctx context.Context) (string, bool) {
	if o.keychain == nil {
		return "", false
	}

	token, err := o.keychain.GetToken()
	if err != nil || token == "" {
		return "", false
	}

	if !o.isStoredTokenExpired() {
		return token, true
	}

	// Try to refresh expired token
	if refreshed, err := o.refreshStoredToken(ctx); err == nil {
		return refreshed, true
	}

	return "", false
}

// doClientCredentialsGrant performs OAuth 2.0 client credentials grant.
// No scope is requested: the token carries what the credential was granted,
// and asking for a scope the application lacks fails the grant.
func (o *OAuth) doClientCredentialsGrant(ctx context.Context, clientID, clientSecret string) (*locktivity.TokenResponse, error) {
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}
	return o.doTokenRequest(ctx, locktivity.OAuthTokenEndpoint, data)
}

// exchangeOIDCToken exchanges an OIDC token for a Locktivity access token.
func (o *OAuth) exchangeOIDCToken(ctx context.Context, oidcToken string) (string, error) {
	data := url.Values{
		"grant_type":         {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token":      {oidcToken},
		"subject_token_type": {"urn:ietf:params:oauth:token-type:id_token"},
		"scope":              {"read:evidence_packs write:evidence_packs"},
	}

	tokenResp, err := o.doTokenRequest(ctx, locktivity.OAuthTokenEndpoint, data)
	if err != nil {
		return "", err
	}
	return tokenResp.AccessToken, nil
}

// doTokenRequest performs a token endpoint request and returns the full response.
func (o *OAuth) doTokenRequest(ctx context.Context, endpoint string, data url.Values) (*locktivity.TokenResponse, error) {
	tokenURL := o.baseURL + endpoint

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, o.parseTokenError(resp)
	}

	var tokenResp locktivity.TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	return &tokenResp, nil
}

func (o *OAuth) parseTokenError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	var errResp struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}

	if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
		return fmt.Errorf("%s: %s", errResp.Error, errResp.ErrorDescription)
	}

	if len(body) > 0 && len(body) < 500 {
		return fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return fmt.Errorf("token request failed with status %d", resp.StatusCode)
}

func (o *OAuth) refreshStoredToken(ctx context.Context) (string, error) {
	refreshToken, err := o.getStoredRefreshToken()
	if err != nil {
		return "", err
	}

	resp, err := o.doRefreshTokenRequest(ctx, refreshToken)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	tokenResp, err := decodeRefreshTokenResponse(resp)
	if err != nil {
		return "", err
	}

	if err := o.storeRefreshedToken(tokenResp); err != nil {
		return "", err
	}

	return tokenResp.AccessToken, nil
}

func (o *OAuth) isStoredTokenExpired() bool {
	if o.keychain == nil {
		return false
	}

	exp, err := o.keychain.GetTokenExpiry()
	if err != nil || exp == 0 {
		// Unknown expiry: assume usable to preserve existing behavior
		return false
	}

	return time.Now().Unix() >= exp-tokenExpiryLeewaySeconds
}

// SignInRequestError is a browser sign-in that cannot go ahead with what
// epack sent: a redirect address other than the loopback callback, or a
// session that is malformed, expired, or for a different sign-in.
type SignInRequestError struct {
	msg string
}

func (e *SignInRequestError) Error() string {
	return e.msg
}

// BrowserSignIn is a started browser sign-in: the address the person opens,
// the state the browser carries back, and the session that finishes it.
type BrowserSignIn struct {
	AuthorizationURL string
	State            string
	Session          string
	ExpiresIn        time.Duration
}

// browserSession is what finishing a sign-in needs. Each adapter process
// serves one command, so it travels through epack instead of staying here.
// It is not signed because there is no stable key before sign-in; the server
// ties the code to the challenge and redirect address, so an altered session
// only fails the exchange.
type browserSession struct {
	CodeVerifier string `json:"code_verifier"`
	State        string `json:"state"`
	RedirectURI  string `json:"redirect_uri"`
	ExpiresAt    int64  `json:"expires_at"`
}

// StartBrowserSignIn begins an authorization code sign-in with PKCE that
// returns to epack on a loopback address.
func (o *OAuth) StartBrowserSignIn(redirectURI string) (*BrowserSignIn, error) {
	if err := o.allowBrowserSignIn(); err != nil {
		return nil, err
	}
	if !isLoopbackCallback(redirectURI) {
		return nil, &SignInRequestError{msg: "redirect_uri must be http://127.0.0.1:<port>/callback"}
	}

	session := browserSession{
		CodeVerifier: randomURLSafe(),
		State:        randomURLSafe(),
		RedirectURI:  redirectURI,
		ExpiresAt:    time.Now().Add(locktivity.BrowserSignInLifetime).Unix(),
	}
	challenge := sha256.Sum256([]byte(session.CodeVerifier))
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {locktivity.PublicClientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {"read:evidence_packs write:evidence_packs"},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
		"state":                 {session.State},
	}

	return &BrowserSignIn{
		AuthorizationURL: o.baseURL + locktivity.OAuthAuthorizeEndpoint + "?" + query.Encode(),
		State:            session.State,
		Session:          session.encode(),
		ExpiresIn:        locktivity.BrowserSignInLifetime,
	}, nil
}

// CompleteBrowserSignIn exchanges the code the browser brought back for a
// token and stores it, with the verifier and redirect address the session
// carries.
func (o *OAuth) CompleteBrowserSignIn(ctx context.Context, session, code, state string) error {
	if err := o.allowBrowserSignIn(); err != nil {
		return err
	}
	s, err := openBrowserSession(session, state)
	if err != nil {
		return err
	}

	tokenResp, err := o.doTokenRequest(ctx, locktivity.OAuthSignInTokenEndpoint, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {s.RedirectURI},
		"client_id":     {locktivity.PublicClientID},
		"code_verifier": {s.CodeVerifier},
	})
	if err != nil {
		return fmt.Errorf("could not finish the sign-in: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return errors.New("could not finish the sign-in: the token response has no access token")
	}

	return o.storeSignInToken(tokenResp)
}

func (o *OAuth) allowBrowserSignIn() error {
	mode, err := EffectiveAuthMode()
	if err != nil {
		return err
	}

	if mode == AuthModeClientCredentialsOnly {
		return fmt.Errorf(
			"%s=%s disables browser sign-in",
			locktivity.EnvAuthMode,
			AuthModeClientCredentialsOnly,
		)
	}

	return o.validateAllModeAuthEndpoint()
}

func (s browserSession) encode() string {
	data, _ := json.Marshal(s)
	return base64.RawURLEncoding.EncodeToString(data)
}

func openBrowserSession(raw, state string) (browserSession, error) {
	var session browserSession
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(data, &session) != nil ||
		session.CodeVerifier == "" || session.State == "" || session.ExpiresAt == 0 ||
		!isLoopbackCallback(session.RedirectURI) {
		return browserSession{}, &SignInRequestError{msg: "this sign-in session is not valid: run 'epack remote login' again"}
	}
	if time.Now().Unix() >= session.ExpiresAt {
		return browserSession{}, &SignInRequestError{msg: "this sign-in expired: run 'epack remote login' again"}
	}
	if subtle.ConstantTimeCompare([]byte(state), []byte(session.State)) != 1 {
		return browserSession{}, &SignInRequestError{msg: "the state from the browser does not match this sign-in: run 'epack remote login' again"}
	}
	return session, nil
}

// isLoopbackCallback compares against the address rebuilt from the port,
// which rules out any other scheme, host, or path, a missing or zero-padded
// port, and userinfo, a query, or a fragment.
func isLoopbackCallback(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	return err == nil && port >= 1 && port <= 65535 &&
		raw == "http://127.0.0.1:"+strconv.Itoa(port)+"/callback"
}

func randomURLSafe() string {
	b := make([]byte, 32)
	// crypto/rand.Read never returns an error; it crashes the program instead.
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (o *OAuth) storeSignInToken(tokenResp *locktivity.TokenResponse) error {
	if o.keychain == nil {
		return nil
	}

	// A stored client ID would let client credentials for that client reuse
	// the person's token, and an older refresh token could bring back an
	// earlier session, so both are replaced before the token is written. An
	// empty refresh token clears the old one.
	if err := o.keychain.SetClientID(""); err != nil {
		return fmt.Errorf("failed to clear the stored client ID: %w", err)
	}
	if err := o.keychain.SetRefreshToken(tokenResp.RefreshToken); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to replace the stored refresh token: %v\n", err)
	}

	if err := o.keychain.SetToken(tokenResp.AccessToken); err != nil {
		return fmt.Errorf("failed to store token: %w", err)
	}

	expiry := int64(0)
	if tokenResp.ExpiresIn > 0 {
		expiry = time.Now().Unix() + int64(tokenResp.ExpiresIn)
	}
	_ = o.keychain.SetTokenExpiry(expiry)

	return nil
}

func (o *OAuth) validateAllModeAuthEndpoint() error {
	if locktivity.IsAllowedAllModeAuthURL(o.baseURL) {
		return nil
	}
	return fmt.Errorf("auth endpoint %q is not allowed when %s=%s", o.baseURL, locktivity.EnvAuthMode, AuthModeAll)
}

func (o *OAuth) getStoredRefreshToken() (string, error) {
	if o.keychain == nil {
		return "", fmt.Errorf("no keychain available")
	}
	refreshToken, err := o.keychain.GetRefreshToken()
	if err != nil || refreshToken == "" {
		return "", fmt.Errorf("no refresh token available")
	}
	return refreshToken, nil
}

func (o *OAuth) doRefreshTokenRequest(ctx context.Context, refreshToken string) (*http.Response, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {locktivity.PublicClientID},
		"scope":         {"read:evidence_packs write:evidence_packs"},
	}

	tokenURL := o.baseURL + locktivity.OAuthTokenEndpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return o.httpClient.Do(req)
}

func decodeRefreshTokenResponse(resp *http.Response) (*locktivity.TokenResponse, error) {
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh token request failed with status %d", resp.StatusCode)
	}

	var tokenResp locktivity.TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("refresh token response missing access token")
	}
	return &tokenResp, nil
}

func (o *OAuth) storeRefreshedToken(tokenResp *locktivity.TokenResponse) error {
	if err := o.keychain.SetToken(tokenResp.AccessToken); err != nil {
		return err
	}
	if tokenResp.RefreshToken != "" {
		_ = o.keychain.SetRefreshToken(tokenResp.RefreshToken)
	}
	if tokenResp.ExpiresIn > 0 {
		_ = o.keychain.SetTokenExpiry(time.Now().Unix() + int64(tokenResp.ExpiresIn))
	}
	return nil
}
