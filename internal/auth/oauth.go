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
func (o *OAuth) GetToken(ctx context.Context) (string, error) {
	mode, err := EffectiveAuthMode()
	if err != nil {
		return "", err
	}
	if err := o.validateAuthEndpointForMode(mode); err != nil {
		return "", err
	}

	token, decided, err := o.getClientCredentialsOnlyToken(ctx, mode)
	if decided {
		return token, err
	}

	if token := oidcTokenFromEnv(); token != "" {
		return o.exchangeOIDCToken(ctx, token)
	}

	if creds, ok := envClientCredentials(); ok {
		return o.clientCredentialsGrant(ctx, creds.clientID, creds.clientSecret)
	}

	if token, ok := o.getUsableStoredToken(ctx); ok {
		return token, nil
	}

	return "", fmt.Errorf("no authentication available: run 'epack remote login locktivity' or set environment variables")
}

func (o *OAuth) validateAuthEndpointForMode(mode string) error {
	if mode != AuthModeAll {
		return nil
	}
	return o.validateAllModeAuthEndpoint()
}

func (o *OAuth) getClientCredentialsOnlyToken(ctx context.Context, mode string) (string, bool, error) {
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

	token, err := o.clientCredentialsGrant(ctx, creds.clientID, creds.clientSecret)
	return token, true, err
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
	if refreshed, refreshErr := o.refreshStoredToken(ctx); refreshErr == nil {
		return refreshed, true
	}
	return "", false
}

// clientCredentialsGrant performs OAuth 2.0 client credentials grant.
func (o *OAuth) clientCredentialsGrant(ctx context.Context, clientID, clientSecret string) (string, error) {
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	data.Set("scope", "read:evidence_packs write:evidence_packs")

	return o.tokenRequest(ctx, data)
}

// exchangeOIDCToken exchanges an OIDC token for a Locktivity access token.
func (o *OAuth) exchangeOIDCToken(ctx context.Context, oidcToken string) (string, error) {
	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	data.Set("subject_token", oidcToken)
	data.Set("subject_token_type", "urn:ietf:params:oauth:token-type:id_token")
	data.Set("scope", "read:evidence_packs write:evidence_packs")

	return o.tokenRequest(ctx, data)
}

// tokenRequest performs a token endpoint request.
func (o *OAuth) tokenRequest(ctx context.Context, data url.Values) (string, error) {
	tokenURL := fmt.Sprintf("%s%s", o.baseURL, locktivity.OAuthTokenEndpoint)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		var errResp struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return "", fmt.Errorf("OAuth error: %s - %s", errResp.Error, errResp.ErrorDescription)
		}
		// Include body in error for debugging
		if len(body) > 0 && len(body) < 500 {
			return "", fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
		}
		return "", fmt.Errorf("token request failed with status %d", resp.StatusCode)
	}

	var tokenResp locktivity.TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}

	return tokenResp.AccessToken, nil
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
		// Unknown expiry: preserve previous behavior and assume usable.
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

	deviceURL := fmt.Sprintf("%s%s", o.baseURL, locktivity.OAuthDeviceCodeEndpoint)

	data := url.Values{}
	data.Set("scope", "read:evidence_packs write:evidence_packs")

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
	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	data.Set("device_code", deviceCode)

	tokenURL := fmt.Sprintf("%s%s", o.baseURL, locktivity.OAuthTokenEndpoint)
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

	// Store the token in keychain
	if o.keychain != nil {
		if err := o.keychain.SetToken(tokenResp.AccessToken); err != nil {
			return fmt.Errorf("failed to store token: %w", err)
		}
		if tokenResp.ExpiresIn > 0 {
			_ = o.keychain.SetTokenExpiry(time.Now().Unix() + int64(tokenResp.ExpiresIn))
		}
		if tokenResp.RefreshToken != "" {
			if err := o.keychain.SetRefreshToken(tokenResp.RefreshToken); err != nil {
				// Non-fatal, just log
				fmt.Fprintf(os.Stderr, "warning: failed to store refresh token: %v\n", err)
			}
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
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("scope", "read:evidence_packs write:evidence_packs")

	tokenURL := fmt.Sprintf("%s%s", o.baseURL, locktivity.OAuthTokenEndpoint)
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
