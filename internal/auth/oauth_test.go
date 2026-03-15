package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/locktivity/epack-remote-locktivity/internal/locktivity"
)

func TestPollDeviceCodeOnce_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2/token" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"tok_123","token_type":"Bearer"}`))
	}))
	defer srv.Close()

	o := NewOAuth(srv.URL, NewMemoryKeychain())
	res, err := o.pollDeviceCodeOnce(context.Background(), "device_123")
	if err != nil {
		t.Fatalf("pollDeviceCodeOnce returned error: %v", err)
	}
	if res.token == nil {
		t.Fatal("expected token in result")
	}
	if res.token.AccessToken != "tok_123" {
		t.Fatalf("expected access token tok_123, got %q", res.token.AccessToken)
	}
	if res.slowDown {
		t.Fatal("did not expect slowDown")
	}
}

func TestPollDeviceCodeOnce_SlowDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"slow_down","error_description":"wait longer"}`))
	}))
	defer srv.Close()

	o := NewOAuth(srv.URL, NewMemoryKeychain())
	res, err := o.pollDeviceCodeOnce(context.Background(), "device_123")
	if err != nil {
		t.Fatalf("pollDeviceCodeOnce returned error: %v", err)
	}
	if !res.slowDown {
		t.Fatal("expected slowDown=true")
	}
	if res.token != nil {
		t.Fatal("did not expect token")
	}
}

func TestPollDeviceCodeOnce_AccessDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"access_denied","error_description":"user denied"}`))
	}))
	defer srv.Close()

	o := NewOAuth(srv.URL, NewMemoryKeychain())
	_, err := o.pollDeviceCodeOnce(context.Background(), "device_123")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "access denied by user") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPollDeviceCodeOnce_NonJSONErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()

	o := NewOAuth(srv.URL, NewMemoryKeychain())
	_, err := o.pollDeviceCodeOnce(context.Background(), "device_123")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "non-JSON error body") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPollDeviceCodeToken_ContextCanceled(t *testing.T) {
	o := NewOAuth("https://app.locktivity.com", NewMemoryKeychain())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := o.PollDeviceCodeToken(ctx, "device_123", 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestCompleteDeviceCodeFlow_StoresAccessAndRefreshToken(t *testing.T) {
	keychain := NewMemoryKeychain()
	o := NewOAuth("https://app.locktivity.com", keychain)

	err := o.completeDeviceCodeFlow(
		context.Background(),
		"device_123",
		5,
		func(context.Context, string, int) (*locktivity.TokenResponse, error) {
			return &locktivity.TokenResponse{
				AccessToken:  "access_123",
				RefreshToken: "refresh_123",
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("completeDeviceCodeFlow failed: %v", err)
	}

	accessToken, err := keychain.GetToken()
	if err != nil {
		t.Fatalf("GetToken failed: %v", err)
	}
	if accessToken != "access_123" {
		t.Fatalf("expected access_123, got %q", accessToken)
	}

	refreshToken, err := keychain.GetRefreshToken()
	if err != nil {
		t.Fatalf("GetRefreshToken failed: %v", err)
	}
	if refreshToken != "refresh_123" {
		t.Fatalf("expected refresh_123, got %q", refreshToken)
	}
}

func TestCompleteDeviceCodeFlow_SetTokenFailure(t *testing.T) {
	keychain := &failingKeychain{
		setTokenErr: errors.New("set token failed"),
	}
	o := NewOAuth("https://app.locktivity.com", keychain)

	err := o.completeDeviceCodeFlow(
		context.Background(),
		"device_123",
		5,
		func(context.Context, string, int) (*locktivity.TokenResponse, error) {
			return &locktivity.TokenResponse{
				AccessToken: "access_123",
			}, nil
		},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to store token") {
		t.Fatalf("expected wrapped store token error, got: %v", err)
	}
}

func TestEffectiveAuthMode_DefaultClientCredentialsOnly(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, "")

	mode, err := EffectiveAuthMode()
	if err != nil {
		t.Fatalf("EffectiveAuthMode returned error: %v", err)
	}
	if mode != AuthModeClientCredentialsOnly {
		t.Fatalf("expected mode %q, got %q", AuthModeClientCredentialsOnly, mode)
	}
}

func TestEffectiveAuthMode_Invalid(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, "nope")

	_, err := EffectiveAuthMode()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), locktivity.EnvAuthMode) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetToken_ClientCredentialsOnly_RequiresCredentials(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, AuthModeClientCredentialsOnly)
	t.Setenv(locktivity.EnvClientID, "")
	t.Setenv(locktivity.EnvClientSecret, "")
	t.Setenv(locktivity.EnvOIDCToken, "ignored")

	o := NewOAuth("https://app.locktivity.com", NewMemoryKeychain())
	_, err := o.GetToken(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), locktivity.EnvClientID) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetToken_ClientCredentialsOnly_UsesClientCredentialsGrant(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, AuthModeClientCredentialsOnly)
	t.Setenv(locktivity.EnvClientID, "client_123")
	t.Setenv(locktivity.EnvClientSecret, "secret_123")
	t.Setenv(locktivity.EnvOIDCToken, "ignored")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2/token" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm failed: %v", err)
		}

		assertFormValue(t, r.Form, "grant_type", "client_credentials")
		assertFormValue(t, r.Form, "client_id", "client_123")
		assertFormValue(t, r.Form, "client_secret", "secret_123")
		if got := r.Form.Get("subject_token"); got != "" {
			t.Fatalf("did not expect subject_token, got %q", got)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"tok_cc","token_type":"Bearer"}`))
	}))
	defer srv.Close()

	o := NewOAuth(srv.URL, NewMemoryKeychain())
	token, err := o.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken returned error: %v", err)
	}
	if token != "tok_cc" {
		t.Fatalf("expected tok_cc, got %q", token)
	}
}

func TestStartDeviceCodeFlow_DisabledInClientCredentialsOnlyMode(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, AuthModeClientCredentialsOnly)

	o := NewOAuth("https://app.locktivity.com", NewMemoryKeychain())
	_, err := o.StartDeviceCodeFlow(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "disables device code login") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetToken_AllMode_RefreshesExpiredStoredToken(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, AuthModeAll)
	t.Setenv(locktivity.EnvOIDCToken, "")
	t.Setenv(locktivity.EnvClientID, "")
	t.Setenv(locktivity.EnvClientSecret, "")

	keychain := NewMemoryKeychain()
	_ = keychain.SetToken("expired_token")
	_ = keychain.SetRefreshToken("refresh_123")
	_ = keychain.SetTokenExpiry(1) // clearly expired

	o := NewOAuth(locktivity.DefaultAuthBaseURL, keychain)
	o.httpClient = &http.Client{
		Timeout: locktivity.HTTPTimeout,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host != "app.locktivity.com" {
				t.Fatalf("unexpected host: %s", req.URL.Host)
			}
			if req.URL.Path != "/oauth2/token" {
				t.Fatalf("unexpected path: %s", req.URL.Path)
			}
			if err := req.ParseForm(); err != nil {
				t.Fatalf("ParseForm failed: %v", err)
			}
			assertFormValue(t, req.Form, "grant_type", "refresh_token")
			assertFormValue(t, req.Form, "refresh_token", "refresh_123")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"access_token":"tok_refreshed","refresh_token":"refresh_new","token_type":"Bearer","expires_in":3600}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}
	token, err := o.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken returned error: %v", err)
	}
	if token != "tok_refreshed" {
		t.Fatalf("expected tok_refreshed, got %q", token)
	}
}

func TestGetToken_AllMode_RejectsDisallowedAuthEndpoint(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, AuthModeAll)
	t.Setenv(locktivity.EnvOIDCToken, "oidc_123")

	o := NewOAuth("https://evil.example.com", NewMemoryKeychain())
	_, err := o.GetToken(context.Background())
	if err == nil {
		t.Fatal("expected endpoint policy error")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetToken_AllMode_RejectsNonHTTPSAuthEndpoint(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, AuthModeAll)
	t.Setenv(locktivity.EnvOIDCToken, "oidc_123")

	o := NewOAuth("http://app.locktivity.com", NewMemoryKeychain())
	_, err := o.GetToken(context.Background())
	if err == nil {
		t.Fatal("expected endpoint policy error")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertFormValue(t *testing.T, form url.Values, key, want string) {
	t.Helper()
	if got := form.Get(key); got != want {
		t.Fatalf("expected %s=%q, got %q", key, want, got)
	}
}

type failingKeychain struct {
	setTokenErr        error
	setRefreshTokenErr error
}

func (k *failingKeychain) GetToken() (string, error) {
	return "", nil
}

func (k *failingKeychain) SetToken(token string) error {
	return k.setTokenErr
}

func (k *failingKeychain) GetRefreshToken() (string, error) {
	return "", nil
}

func (k *failingKeychain) SetRefreshToken(token string) error {
	return k.setRefreshTokenErr
}

func (k *failingKeychain) GetTokenExpiry() (int64, error) {
	return 0, nil
}

func (k *failingKeychain) SetTokenExpiry(unix int64) error {
	return nil
}

func (k *failingKeychain) Clear() error {
	return nil
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
