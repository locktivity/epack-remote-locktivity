package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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

// SetClientCredentials sets the client ID and secret for client credentials flow.
func (o *OAuth) SetClientCredentials(clientID, clientSecret string) {
	o.clientID = clientID
	o.clientSecret = clientSecret
}

// GetToken returns a valid access token using the best available method.
// Token sources are tried in order: client_credentials_only mode, OIDC exchange,
// env-based client credentials, and finally stored tokens.
func (o *OAuth) GetToken(ctx context.Context) (string, error) {
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
func (o *OAuth) doClientCredentialsGrant(ctx context.Context, clientID, clientSecret string) (*locktivity.TokenResponse, error) {
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"scope":         {"read:evidence_packs write:evidence_packs"},
	}
	return o.doTokenRequest(ctx, data)
}

// exchangeOIDCToken exchanges an OIDC token for a Locktivity access token.
func (o *OAuth) exchangeOIDCToken(ctx context.Context, oidcToken string) (string, error) {
	data := url.Values{
		"grant_type":         {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token":      {oidcToken},
		"subject_token_type": {"urn:ietf:params:oauth:token-type:id_token"},
		"scope":              {"read:evidence_packs write:evidence_packs"},
	}

	tokenResp, err := o.doTokenRequest(ctx, data)
	if err != nil {
		return "", err
	}
	return tokenResp.AccessToken, nil
}

// doTokenRequest performs a token endpoint request and returns the full response.
func (o *OAuth) doTokenRequest(ctx context.Context, data url.Values) (*locktivity.TokenResponse, error) {
	tokenURL := o.baseURL + locktivity.OAuthTokenEndpoint

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

// StartDeviceCodeFlow initiates the device code flow and returns the user code and URI.
func (o *OAuth) StartDeviceCodeFlow(ctx context.Context) (*locktivity.DeviceCodeResponse, error) {
	mode, err := EffectiveAuthMode()
	if err != nil {
		return nil, err
	}

	if mode == AuthModeClientCredentialsOnly {
		return nil, fmt.Errorf(
			"%s=%s disables device code login",
			locktivity.EnvAuthMode,
			AuthModeClientCredentialsOnly,
		)
	}

	if err := o.validateAllModeAuthEndpoint(); err != nil {
		return nil, err
	}

	return o.doDeviceCodeRequest(ctx)
}

func (o *OAuth) doDeviceCodeRequest(ctx context.Context) (*locktivity.DeviceCodeResponse, error) {
	deviceURL := o.baseURL + locktivity.OAuthDeviceCodeEndpoint

	data := url.Values{"scope": {"read:evidence_packs write:evidence_packs"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceURL, strings.NewReader(data.Encode()))
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
		return nil, fmt.Errorf("device code request failed with status %d", resp.StatusCode)
	}

	var deviceResp locktivity.DeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&deviceResp); err != nil {
		return nil, err
	}

	return &deviceResp, nil
}

// PollDeviceCodeToken polls the token endpoint until the user completes authentication.
func (o *OAuth) PollDeviceCodeToken(ctx context.Context, deviceCode string, interval int) (*locktivity.TokenResponse, error) {
	pollInterval := time.Duration(interval) * time.Second
	if pollInterval < locktivity.DeviceCodePollInterval {
		pollInterval = locktivity.DeviceCodePollInterval
	}

	deadline := time.Now().Add(locktivity.DeviceCodeMaxPollTime)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}

		result, err := o.pollDeviceCodeOnce(ctx, deviceCode)
		if err != nil {
			return nil, err
		}
		if result.token != nil {
			return result.token, nil
		}
		if result.slowDown {
			pollInterval += time.Second
		}
	}

	return nil, fmt.Errorf("device code flow timed out")
}

type deviceCodePollResult struct {
	token    *locktivity.TokenResponse
	slowDown bool
}

func (o *OAuth) pollDeviceCodeOnce(ctx context.Context, deviceCode string) (deviceCodePollResult, error) {
	req, err := o.newDeviceCodeTokenRequest(ctx, deviceCode)
	if err != nil {
		return deviceCodePollResult{}, err
	}

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return deviceCodePollResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	decoder := json.NewDecoder(resp.Body)
	if resp.StatusCode == http.StatusOK {
		return decodeDeviceCodeSuccess(decoder)
	}
	return decodeDeviceCodeError(decoder, resp.StatusCode)
}

func (o *OAuth) newDeviceCodeTokenRequest(ctx context.Context, deviceCode string) (*http.Request, error) {
	data := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
	}

	tokenURL := o.baseURL + locktivity.OAuthTokenEndpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req, nil
}

// CompleteDeviceCodeFlow performs the full device code flow including storing the token.
func (o *OAuth) CompleteDeviceCodeFlow(ctx context.Context, deviceCode string, interval int) error {
	return o.completeDeviceCodeFlow(ctx, deviceCode, interval, o.PollDeviceCodeToken)
}

func (o *OAuth) completeDeviceCodeFlow(
	ctx context.Context,
	deviceCode string,
	interval int,
	pollFn func(context.Context, string, int) (*locktivity.TokenResponse, error),
) error {
	tokenResp, err := pollFn(ctx, deviceCode, interval)
	if err != nil {
		return err
	}

	return o.storeDeviceCodeToken(tokenResp)
}

func (o *OAuth) storeDeviceCodeToken(tokenResp *locktivity.TokenResponse) error {
	if o.keychain == nil {
		return nil
	}

	if err := o.keychain.SetToken(tokenResp.AccessToken); err != nil {
		return fmt.Errorf("failed to store token: %w", err)
	}

	if tokenResp.ExpiresIn > 0 {
		_ = o.keychain.SetTokenExpiry(time.Now().Unix() + int64(tokenResp.ExpiresIn))
	}

	if tokenResp.RefreshToken != "" {
		if err := o.keychain.SetRefreshToken(tokenResp.RefreshToken); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to store refresh token: %v\n", err)
		}
	}

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

func decodeDeviceCodeSuccess(decoder *json.Decoder) (deviceCodePollResult, error) {
	var tokenResp locktivity.TokenResponse
	if err := decoder.Decode(&tokenResp); err != nil {
		return deviceCodePollResult{}, err
	}
	return deviceCodePollResult{token: &tokenResp}, nil
}

func decodeDeviceCodeError(decoder *json.Decoder, statusCode int) (deviceCodePollResult, error) {
	var errResp struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := decoder.Decode(&errResp); err != nil {
		return deviceCodePollResult{}, fmt.Errorf("device code polling failed with status %d and non-JSON error body", statusCode)
	}
	return mapDeviceCodePollError(errResp.Error, errResp.ErrorDescription)
}

func mapDeviceCodePollError(errCode, errDescription string) (deviceCodePollResult, error) {
	switch errCode {
	case "authorization_pending":
		return deviceCodePollResult{}, nil
	case "slow_down":
		return deviceCodePollResult{slowDown: true}, nil
	case "expired_token":
		return deviceCodePollResult{}, fmt.Errorf("device code expired")
	case "access_denied":
		return deviceCodePollResult{}, fmt.Errorf("access denied by user")
	default:
		return deviceCodePollResult{}, fmt.Errorf("OAuth error: %s - %s", errCode, errDescription)
	}
}
