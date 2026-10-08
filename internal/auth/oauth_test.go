package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/locktivity/epack-remote-locktivity/internal/locktivity"
)

const testRedirectURI = "http://127.0.0.1:53682/callback"

// browserSignInOAuth points browser sign-in at a TLS test server configured as
// the auth endpoint override, the only kind of non-default endpoint a release
// build talks to. API endpoint overrides left in the shell are cleared, since
// a plain HTTP one fails that check too.
func browserSignInOAuth(t *testing.T, keychain Keychain, token http.HandlerFunc) *OAuth {
	t.Helper()
	srv := httptest.NewTLSServer(token)
	t.Cleanup(srv.Close)
	t.Setenv(locktivity.EnvAuthMode, "")
	t.Setenv(locktivity.EnvRemoteAuthEndpoint, srv.URL)
	t.Setenv(locktivity.EnvRemoteEndpoint, "")
	t.Setenv(locktivity.EnvEndpoint, "")

	o := NewOAuth(srv.URL, keychain)
	o.SetHTTPClient(srv.Client())
	return o
}

func mustStartBrowserSignIn(t *testing.T, o *OAuth) *BrowserSignIn {
	t.Helper()
	signIn, err := o.StartBrowserSignIn(testRedirectURI)
	if err != nil {
		t.Fatalf("StartBrowserSignIn returned error: %v", err)
	}
	return signIn
}

func noExchangeExpected(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no code should be exchanged, got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func TestStartBrowserSignIn_BuildsTheAuthorizationURLAndSession(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, "")
	o := NewOAuth(locktivity.DefaultAuthBaseURL, NewMemoryKeychain())

	signIn := mustStartBrowserSignIn(t, o)

	authURL, err := url.Parse(signIn.AuthorizationURL)
	if err != nil {
		t.Fatalf("authorization URL does not parse: %v", err)
	}
	if authURL.Scheme != "https" || authURL.Host != "app.locktivity.com" || authURL.Path != "/epack/oauth2/authorize" {
		t.Fatalf("unexpected authorization endpoint: %s", signIn.AuthorizationURL)
	}
	query := authURL.Query()
	assertFormValue(t, query, "response_type", "code")
	assertFormValue(t, query, "client_id", "epack")
	assertFormValue(t, query, "redirect_uri", testRedirectURI)
	assertFormValue(t, query, "scope", "read:evidence_packs write:evidence_packs")
	assertFormValue(t, query, "code_challenge_method", "S256")
	assertFormValue(t, query, "state", signIn.State)
	if len(query) != 7 {
		t.Fatalf("expected exactly the seven authorize parameters, got %v", query)
	}

	if state, err := base64.RawURLEncoding.DecodeString(signIn.State); err != nil || len(state) < 16 {
		t.Fatalf("state is not at least 128 bits of unpadded base64url: %q", signIn.State)
	}
	if strings.ContainsAny(signIn.Session, "=+/") {
		t.Fatalf("session is not unpadded base64url: %q", signIn.Session)
	}
	if signIn.ExpiresIn != 10*time.Minute {
		t.Fatalf("expected a ten minute sign-in, got %s", signIn.ExpiresIn)
	}

	session, err := openBrowserSession(signIn.Session, signIn.State)
	if err != nil {
		t.Fatalf("the session does not open with its own state: %v", err)
	}
	if session.RedirectURI != testRedirectURI {
		t.Fatalf("session redirect_uri = %q", session.RedirectURI)
	}
	if left := time.Until(time.Unix(session.ExpiresAt, 0)); left < 9*time.Minute || left > 10*time.Minute {
		t.Fatalf("session expires in %s, want ten minutes", left)
	}
	if len(session.CodeVerifier) < 43 {
		t.Fatalf("code_verifier is shorter than PKCE allows: %d characters", len(session.CodeVerifier))
	}

	again := mustStartBrowserSignIn(t, o)
	if again.State == signIn.State || again.Session == signIn.Session {
		t.Fatal("two sign-ins share a state or a session")
	}
}

func TestBrowserSignIn_ExchangesTheCodeWithTheVerifierBehindTheChallenge(t *testing.T) {
	var form url.Values
	keychain := NewMemoryKeychain()
	o := browserSignInOAuth(t, keychain, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != locktivity.OAuthSignInTokenEndpoint {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("unexpected Content-Type %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm failed: %v", err)
		}
		form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok_person","token_type":"Bearer","expires_in":3600,"scope":"read:evidence_packs write:evidence_packs","created_at":1790000000}`))
	})

	signIn := mustStartBrowserSignIn(t, o)
	authURL, err := url.Parse(signIn.AuthorizationURL)
	if err != nil {
		t.Fatalf("authorization URL does not parse: %v", err)
	}
	challenge := authURL.Query().Get("code_challenge")

	if err := o.CompleteBrowserSignIn(context.Background(), signIn.Session, "code_1", signIn.State); err != nil {
		t.Fatalf("CompleteBrowserSignIn returned error: %v", err)
	}

	assertFormValue(t, form, "grant_type", "authorization_code")
	assertFormValue(t, form, "code", "code_1")
	assertFormValue(t, form, "redirect_uri", testRedirectURI)
	assertFormValue(t, form, "client_id", "epack")
	if len(form) != 5 {
		t.Fatalf("expected exactly the five exchange fields, got %v", form)
	}
	sum := sha256.Sum256([]byte(form.Get("code_verifier")))
	if got := base64.RawURLEncoding.EncodeToString(sum[:]); got != challenge {
		t.Fatalf("SHA-256 of the code_verifier is %q, the code_challenge was %q", got, challenge)
	}

	if token, _ := keychain.GetToken(); token != "tok_person" {
		t.Fatalf("expected the token to be stored, got %q", token)
	}
	if expiry, _ := keychain.GetTokenExpiry(); expiry < time.Now().Unix()+3500 {
		t.Fatalf("expected an expiry about an hour out, got %d", expiry)
	}
	if refresh, _ := keychain.GetRefreshToken(); refresh != "" {
		t.Fatalf("no refresh token was sent, yet %q is stored", refresh)
	}
}

func TestCompleteBrowserSignIn_KeepsARefreshTokenWhenOneIsSent(t *testing.T) {
	keychain := NewMemoryKeychain()
	o := browserSignInOAuth(t, keychain, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"tok_person","refresh_token":"refresh_person","token_type":"Bearer","expires_in":3600}`))
	})
	signIn := mustStartBrowserSignIn(t, o)

	if err := o.CompleteBrowserSignIn(context.Background(), signIn.Session, "code_1", signIn.State); err != nil {
		t.Fatalf("CompleteBrowserSignIn returned error: %v", err)
	}
	if refresh, _ := keychain.GetRefreshToken(); refresh != "refresh_person" {
		t.Fatalf("expected the refresh token to be stored, got %q", refresh)
	}
}

func TestCompleteBrowserSignIn_RefusesAStateFromAnotherSignIn(t *testing.T) {
	keychain := NewMemoryKeychain()
	o := browserSignInOAuth(t, keychain, noExchangeExpected(t))
	signIn := mustStartBrowserSignIn(t, o)
	other := mustStartBrowserSignIn(t, o)

	err := o.CompleteBrowserSignIn(context.Background(), signIn.Session, "code_1", other.State)
	var requestErr *SignInRequestError
	if !errors.As(err, &requestErr) || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected a state mismatch, got %v", err)
	}
	if token, _ := keychain.GetToken(); token != "" {
		t.Fatalf("nothing should be stored, got %q", token)
	}
}

func TestCompleteBrowserSignIn_RefusesMalformedAndExpiredSessions(t *testing.T) {
	o := browserSignInOAuth(t, NewMemoryKeychain(), noExchangeExpected(t))
	valid := browserSession{CodeVerifier: "verifier", State: "state_1", RedirectURI: testRedirectURI, ExpiresAt: time.Now().Add(time.Minute).Unix()}
	expired := valid
	expired.ExpiresAt = time.Now().Add(-time.Second).Unix()
	noVerifier := valid
	noVerifier.CodeVerifier = ""
	elsewhere := valid
	elsewhere.RedirectURI = "https://example.com/callback"

	for _, tc := range []struct {
		name    string
		session string
		want    string
	}{
		{"not base64url", "not a session", "not valid"},
		{"not JSON", base64.RawURLEncoding.EncodeToString([]byte("not json")), "not valid"},
		{"no verifier", noVerifier.encode(), "not valid"},
		{"another redirect address", elsewhere.encode(), "not valid"},
		{"expired", expired.encode(), "expired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := o.CompleteBrowserSignIn(context.Background(), tc.session, "code_1", "state_1")
			var requestErr *SignInRequestError
			if !errors.As(err, &requestErr) || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "run 'epack remote login' again") {
				t.Fatalf("expected a sign-in request error saying %q, got %v", tc.want, err)
			}
		})
	}
}

func TestStartBrowserSignIn_RefusesRedirectURIsOutsideTheLoopbackCallback(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, "")
	o := NewOAuth(locktivity.DefaultAuthBaseURL, NewMemoryKeychain())

	for _, redirect := range []string{
		"",
		"://127.0.0.1:53682/callback",
		"https://127.0.0.1:53682/callback",
		"HTTP://127.0.0.1:53682/callback",
		"http://localhost:53682/callback",
		"http://[::1]:53682/callback",
		"http://127.0.0.2:53682/callback",
		"http://127.0.0.1/callback",
		"http://127.0.0.1:/callback",
		"http://127.0.0.1:0/callback",
		"http://127.0.0.1:65536/callback",
		"http://127.0.0.1:053682/callback",
		"http://127.0.0.1:53682",
		"http://127.0.0.1:53682/",
		"http://127.0.0.1:53682/callback/",
		"http://127.0.0.1:53682/Callback",
		"http://127.0.0.1:53682/call%62ack",
		"http://user@127.0.0.1:53682/callback",
		"http://127.0.0.1:53682/callback?next=1",
		"http://127.0.0.1:53682/callback?",
		"http://127.0.0.1:53682/callback#top",
		"http://127.0.0.1:53682/callback#",
		" http://127.0.0.1:53682/callback",
	} {
		_, err := o.StartBrowserSignIn(redirect)
		var requestErr *SignInRequestError
		if !errors.As(err, &requestErr) {
			t.Errorf("redirect_uri %q: expected a sign-in request error, got %v", redirect, err)
		}
	}

	for _, redirect := range []string{"http://127.0.0.1:1/callback", "http://127.0.0.1:65535/callback"} {
		if _, err := o.StartBrowserSignIn(redirect); err != nil {
			t.Errorf("redirect_uri %q: unexpected error %v", redirect, err)
		}
	}
}

func TestCompleteBrowserSignIn_StoresNothingWhenTheExchangeFails(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"invalid_grant", http.StatusBadRequest, `{"error":"invalid_grant","error_description":"The authorization code has expired."}`, "invalid_grant: The authorization code has expired."},
		{"no access token", http.StatusOK, `{"token_type":"Bearer","expires_in":3600}`, "no access token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keychain := NewMemoryKeychain()
			o := browserSignInOAuth(t, keychain, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			signIn := mustStartBrowserSignIn(t, o)

			err := o.CompleteBrowserSignIn(context.Background(), signIn.Session, "code_1", signIn.State)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected an error carrying %q, got %v", tc.want, err)
			}
			var requestErr *SignInRequestError
			if errors.As(err, &requestErr) {
				t.Fatalf("a refused exchange is not a malformed request: %v", err)
			}
			if token, _ := keychain.GetToken(); token != "" {
				t.Fatalf("nothing should be stored, got %q", token)
			}
		})
	}
}

func TestCompleteBrowserSignIn_ReportsATokenItCouldNotStore(t *testing.T) {
	o := browserSignInOAuth(t, &failingKeychain{setTokenErr: errors.New("set token failed")}, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"tok_person","token_type":"Bearer","expires_in":3600}`))
	})
	signIn := mustStartBrowserSignIn(t, o)

	err := o.CompleteBrowserSignIn(context.Background(), signIn.Session, "code_1", signIn.State)
	if err == nil || !strings.Contains(err.Error(), "failed to store token") {
		t.Fatalf("expected a wrapped store token error, got %v", err)
	}
}

func TestCompleteBrowserSignIn_DropsTheExpiryOfAnEarlierSession(t *testing.T) {
	keychain := NewMemoryKeychain()
	_ = keychain.SetTokenExpiry(time.Now().Add(time.Hour).Unix())
	o := browserSignInOAuth(t, keychain, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok_person","token_type":"Bearer"}`))
	})
	signIn := mustStartBrowserSignIn(t, o)

	if err := o.CompleteBrowserSignIn(context.Background(), signIn.Session, "code_1", signIn.State); err != nil {
		t.Fatalf("CompleteBrowserSignIn returned error: %v", err)
	}
	if expiry, _ := keychain.GetTokenExpiry(); expiry != 0 {
		t.Fatalf("the earlier expiry survived the sign-in: %d", expiry)
	}
}

func TestCompleteBrowserSignIn_DropsTheClientIDAndRefreshTokenOfAnEarlierSession(t *testing.T) {
	keychain := NewMemoryKeychain()
	_ = keychain.SetToken("tok_client_A")
	_ = keychain.SetClientID("client_A")
	_ = keychain.SetRefreshToken("refresh_earlier")

	clientGrants := 0
	o := browserSignInOAuth(t, keychain, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm failed: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			_, _ = w.Write([]byte(`{"access_token":"tok_person","token_type":"Bearer","expires_in":3600}`))
		case "client_credentials":
			clientGrants++
			_, _ = w.Write([]byte(`{"access_token":"tok_client_A_new","token_type":"Bearer","expires_in":3600}`))
		default:
			t.Errorf("unexpected grant_type %q", r.PostForm.Get("grant_type"))
			w.WriteHeader(http.StatusBadRequest)
		}
	})
	signIn := mustStartBrowserSignIn(t, o)

	if err := o.CompleteBrowserSignIn(context.Background(), signIn.Session, "code_1", signIn.State); err != nil {
		t.Fatalf("CompleteBrowserSignIn returned error: %v", err)
	}
	if token, _ := keychain.GetToken(); token != "tok_person" {
		t.Fatalf("expected the person's token to be stored, got %q", token)
	}
	if clientID, _ := keychain.GetClientID(); clientID != "" {
		t.Fatalf("the earlier client ID survived the sign-in: %q", clientID)
	}
	if refresh, _ := keychain.GetRefreshToken(); refresh != "" {
		t.Fatalf("the earlier refresh token survived the sign-in: %q", refresh)
	}

	t.Setenv(locktivity.EnvAccessToken, "")
	t.Setenv(locktivity.EnvOIDCToken, "")
	t.Setenv(locktivity.EnvClientID, "client_A")
	t.Setenv(locktivity.EnvClientSecret, "secret_A")
	token, err := o.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken returned error: %v", err)
	}
	if token != "tok_client_A_new" || clientGrants != 1 {
		t.Fatalf("client credentials for client_A got %q after %d grants, want a newly fetched token", token, clientGrants)
	}
}

func TestCompleteBrowserSignIn_StoresNothingWhenTheClientIDCannotBeCleared(t *testing.T) {
	keychain := failingClientIDKeychain{NewMemoryKeychain()}
	_ = keychain.MemoryKeychain.SetClientID("client_A")
	_ = keychain.SetRefreshToken("refresh_earlier")
	o := browserSignInOAuth(t, keychain, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"tok_person","token_type":"Bearer","expires_in":3600}`))
	})
	signIn := mustStartBrowserSignIn(t, o)

	err := o.CompleteBrowserSignIn(context.Background(), signIn.Session, "code_1", signIn.State)
	if err == nil || !strings.Contains(err.Error(), "failed to clear the stored client ID") {
		t.Fatalf("expected the sign-in to fail on the client ID, got %v", err)
	}
	if token, _ := keychain.GetToken(); token != "" {
		t.Fatalf("no token should be stored, got %q", token)
	}
	if refresh, _ := keychain.GetRefreshToken(); refresh != "refresh_earlier" {
		t.Fatalf("nothing should be written, yet the refresh token is %q", refresh)
	}
}

func TestBrowserSignIn_DisabledInClientCredentialsOnlyMode(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, AuthModeClientCredentialsOnly)
	o := NewOAuth(locktivity.DefaultAuthBaseURL, NewMemoryKeychain())

	if _, err := o.StartBrowserSignIn(testRedirectURI); err == nil || !strings.Contains(err.Error(), "disables browser sign-in") {
		t.Fatalf("expected StartBrowserSignIn to be refused, got %v", err)
	}
	if err := o.CompleteBrowserSignIn(context.Background(), "session", "code_1", "state_1"); err == nil || !strings.Contains(err.Error(), "disables browser sign-in") {
		t.Fatalf("expected CompleteBrowserSignIn to be refused, got %v", err)
	}
}

func TestStartBrowserSignIn_RefusesADisallowedAuthEndpoint(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, "")

	o := NewOAuth("https://evil.example.com", NewMemoryKeychain())
	if _, err := o.StartBrowserSignIn(testRedirectURI); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected endpoint policy error, got %v", err)
	}
}

func TestEffectiveAuthMode_DefaultAuto(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, "")

	mode, err := EffectiveAuthMode()
	if err != nil {
		t.Fatalf("EffectiveAuthMode returned error: %v", err)
	}
	if mode != AuthModeAuto {
		t.Fatalf("expected mode %q, got %q", AuthModeAuto, mode)
	}
}

func TestGetToken_AutoMode_UsesStoredSessionWithoutEnvCredentials(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, "")
	t.Setenv(locktivity.EnvAccessToken, "")
	t.Setenv(locktivity.EnvOIDCToken, "")
	t.Setenv(locktivity.EnvClientID, "")
	t.Setenv(locktivity.EnvClientSecret, "")

	keychain := NewMemoryKeychain()
	_ = keychain.SetToken("tok_stored")

	o := NewOAuth(locktivity.DefaultAuthBaseURL, keychain)
	token, err := o.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken returned error: %v", err)
	}
	if token != "tok_stored" {
		t.Fatalf("expected the stored session, got %q", token)
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
		if _, sent := r.Form["scope"]; sent {
			t.Fatalf("did not expect a scope, got %q", r.Form.Get("scope"))
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

func TestGetToken_UsesAccessTokenEnvFirst(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, AuthModeClientCredentialsOnly)
	t.Setenv(locktivity.EnvAccessToken, "access_123")
	t.Setenv(locktivity.EnvClientID, "client_123")
	t.Setenv(locktivity.EnvClientSecret, "secret_123")

	o := NewOAuth("https://app.locktivity.com", NewMemoryKeychain())
	token, err := o.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken returned error: %v", err)
	}
	if token != "access_123" {
		t.Fatalf("expected access_123, got %q", token)
	}
}

func TestGetToken_AccessTokenEnvBypassesAllModeEndpointValidation(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, AuthModeAll)
	t.Setenv(locktivity.EnvAccessToken, "access_123")

	o := NewOAuth("https://evil.example.com", NewMemoryKeychain())
	token, err := o.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken returned error: %v", err)
	}
	if token != "access_123" {
		t.Fatalf("expected access_123, got %q", token)
	}
}

func TestGetToken_ClientCredentials_DoesNotReuseCachedTokenForDifferentClientID(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, AuthModeClientCredentialsOnly)

	tokenRequestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenRequestCount++
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm failed: %v", err)
		}
		clientID := r.Form.Get("client_id")
		w.WriteHeader(http.StatusOK)
		// Return different tokens for different clients
		_, _ = w.Write([]byte(`{"access_token":"tok_` + clientID + `","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	keychain := NewMemoryKeychain()
	o := NewOAuth(srv.URL, keychain)

	// First request with client_A
	t.Setenv(locktivity.EnvClientID, "client_A")
	t.Setenv(locktivity.EnvClientSecret, "secret_A")
	token1, err := o.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken returned error: %v", err)
	}
	if token1 != "tok_client_A" {
		t.Fatalf("expected tok_client_A, got %q", token1)
	}
	if tokenRequestCount != 1 {
		t.Fatalf("expected 1 token request, got %d", tokenRequestCount)
	}

	// Second request with client_B - should NOT reuse cached token
	t.Setenv(locktivity.EnvClientID, "client_B")
	t.Setenv(locktivity.EnvClientSecret, "secret_B")
	token2, err := o.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken returned error: %v", err)
	}
	if token2 != "tok_client_B" {
		t.Fatalf("expected tok_client_B, got %q", token2)
	}
	if tokenRequestCount != 2 {
		t.Fatalf("expected 2 token requests (different client_id), got %d", tokenRequestCount)
	}

	// Third request with client_B again - should reuse cached token
	token3, err := o.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken returned error: %v", err)
	}
	if token3 != "tok_client_B" {
		t.Fatalf("expected tok_client_B, got %q", token3)
	}
	if tokenRequestCount != 2 {
		t.Fatalf("expected still 2 token requests (reused cache), got %d", tokenRequestCount)
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

func (k *failingKeychain) GetClientID() (string, error) {
	return "", nil
}

func (k *failingKeychain) SetClientID(clientID string) error {
	return nil
}

func (k *failingKeychain) Clear() error {
	return nil
}

type failingClientIDKeychain struct {
	*MemoryKeychain
}

func (k failingClientIDKeychain) SetClientID(string) error {
	return errors.New("keychain locked")
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
