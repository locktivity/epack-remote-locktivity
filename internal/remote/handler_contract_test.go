package remote

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/locktivity/epack-remote-locktivity/internal/contracttest"
	"github.com/locktivity/epack-remote-locktivity/internal/locktivity"
	"github.com/locktivity/epack/componentsdk"
)

func mustEncodePushFinalizeToken(t *testing.T, handler *Handler, nonce string) string {
	t.Helper()

	token, err := handler.encodeSignedToken(PushFinalizeTokenData{
		FinalizeToken: "fin_123",
		PackID:        "pack_123",
		PackDigest:    "sha256:pack",
		UploadToken:   "upload_123",
		ExpiresAt:     time.Now().Add(time.Hour).Unix(),
		Nonce:         nonce,
	})
	if err != nil {
		t.Fatalf("failed to encode finalize token: %v", err)
	}

	return token
}

func mustDecodePushFinalizeToken(t *testing.T, handler *Handler, token string) PushFinalizeTokenData {
	t.Helper()

	var tokenData PushFinalizeTokenData
	if err := handler.decodeSignedToken(token, &tokenData); err != nil {
		t.Fatalf("failed to decode push finalize token: %v", err)
	}

	return tokenData
}

func mustEncodePullFinalizeToken(t *testing.T, handler *Handler, nonce string) string {
	t.Helper()

	token, err := handler.encodeSignedToken(PullFinalizeTokenData{
		FinalizeToken: "fin_pull_123",
		PackID:        "pack_123",
		FileDigest:    "sha256:file",
		ReleaseID:     "rel_123",
		ExpiresAt:     time.Now().Add(time.Hour).Unix(),
		Nonce:         nonce,
	})
	if err != nil {
		t.Fatalf("failed to encode pull finalize token: %v", err)
	}

	return token
}

func TestPullPrepare_UsesEnvironmentOnlyForLatestLookup(t *testing.T) {
	silenceTestLogs(t)

	server := contracttest.NewServer(t,
		contracttest.Step{
			Method:   http.MethodGet,
			Path:     locktivity.APIPathPrefix + "/releases/latest",
			Query:    url.Values{"environment": {"prod"}},
			Status:   http.StatusOK,
			JSONBody: locktivity.ReleaseResponse{ID: "rel_123", Pack: &locktivity.PackInfo{ID: "pack_123"}},
		},
		contracttest.Step{
			Method: http.MethodGet,
			Path:   locktivity.APIPathPrefix + "/packs/pack_123",
			Status: http.StatusOK,
			JSONBody: locktivity.GetPackResponse{
				ID:         "pack_123",
				FileDigest: "sha256:file",
				PackDigest: "sha256:pack",
				SizeBytes:  123,
				Download:   &locktivity.DownloadInfo{URL: "https://storage.example.com/download"},
			},
		},
		contracttest.Step{
			Method: http.MethodPost,
			Path:   locktivity.APIPathPrefix + "/finalize_intents",
			Status: http.StatusCreated,
			Check: func(t *testing.T, r *http.Request, body []byte) {
				t.Helper()

				var payload map[string]any
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("failed to decode request body: %v", err)
				}
				if payload["pack_id"] != "pack_123" {
					t.Fatalf("expected pack_id pack_123, got %v", payload["pack_id"])
				}
			},
			JSONBody: locktivity.CreateFinalizeIntentResponse{
				FinalizeToken: "fin_pull_123",
				PackID:        "pack_123",
				ExpiresAt:     time.Now().Add(15 * time.Minute),
			},
		},
	)

	client := locktivity.NewClientWithHTTP(server.Client(), server.URL())
	handler := NewHandlerWithClient(client, nil)

	resp, err := handler.PullPrepare(componentsdk.PullPrepareRequest{
		RequestID: "req_123",
		Target: componentsdk.RemoteTarget{
			Stream:      "acme",
			Environment: "prod",
		},
		Ref: componentsdk.PullRef{Latest: true},
	})
	if err != nil {
		t.Fatalf("PullPrepare failed: %v", err)
	}
	if resp.Download.URL != "https://storage.example.com/download" {
		t.Fatalf("expected download URL https://storage.example.com/download, got %q", resp.Download.URL)
	}
}

func TestPullPrepare_ByVersionUsesVersionAndEnvironment(t *testing.T) {
	silenceTestLogs(t)

	server := contracttest.NewServer(t,
		contracttest.Step{
			Method:   http.MethodGet,
			Path:     locktivity.APIPathPrefix + "/releases/latest",
			Query:    url.Values{"version": {"1.2.3"}, "environment": {"prod"}},
			Status:   http.StatusOK,
			JSONBody: locktivity.ReleaseResponse{ID: "rel_123", Version: "1.2.3", Pack: &locktivity.PackInfo{ID: "pack_123"}},
		},
		contracttest.Step{
			Method: http.MethodGet,
			Path:   locktivity.APIPathPrefix + "/packs/pack_123",
			Status: http.StatusOK,
			JSONBody: locktivity.GetPackResponse{
				ID:         "pack_123",
				FileDigest: "sha256:file",
				PackDigest: "sha256:pack",
				SizeBytes:  123,
				Download:   &locktivity.DownloadInfo{URL: "https://storage.example.com/download"},
			},
		},
		contracttest.Step{
			Method:   http.MethodPost,
			Path:     locktivity.APIPathPrefix + "/finalize_intents",
			Status:   http.StatusCreated,
			JSONBody: locktivity.CreateFinalizeIntentResponse{FinalizeToken: "fin_pull_123", PackID: "pack_123", ExpiresAt: time.Now().Add(15 * time.Minute)},
		},
	)

	client := locktivity.NewClientWithHTTP(server.Client(), server.URL())
	handler := NewHandlerWithClient(client, nil)

	resp, err := handler.PullPrepare(componentsdk.PullPrepareRequest{
		RequestID: "req_123",
		Target: componentsdk.RemoteTarget{
			Environment: "prod",
		},
		Ref: componentsdk.PullRef{Version: "1.2.3"},
	})
	if err != nil {
		t.Fatalf("PullPrepare failed: %v", err)
	}
	if resp.Pack.Digest != "sha256:file" {
		t.Fatalf("expected pack digest sha256:file, got %q", resp.Pack.Digest)
	}
}

func TestPullPrepare_ByDigestUsesDigestAndEnvironment(t *testing.T) {
	silenceTestLogs(t)

	server := contracttest.NewServer(t,
		contracttest.Step{
			Method:   http.MethodGet,
			Path:     locktivity.APIPathPrefix + "/releases/latest",
			Query:    url.Values{"pack_digest": {"sha256:pack"}, "environment": {"prod"}},
			Status:   http.StatusOK,
			JSONBody: locktivity.ReleaseResponse{ID: "rel_123", Pack: &locktivity.PackInfo{ID: "pack_123"}},
		},
		contracttest.Step{
			Method: http.MethodGet,
			Path:   locktivity.APIPathPrefix + "/packs/pack_123",
			Status: http.StatusOK,
			JSONBody: locktivity.GetPackResponse{
				ID:         "pack_123",
				FileDigest: "sha256:file",
				PackDigest: "sha256:pack",
				SizeBytes:  123,
				Download:   &locktivity.DownloadInfo{URL: "https://storage.example.com/download"},
			},
		},
		contracttest.Step{
			Method:   http.MethodPost,
			Path:     locktivity.APIPathPrefix + "/finalize_intents",
			Status:   http.StatusCreated,
			JSONBody: locktivity.CreateFinalizeIntentResponse{FinalizeToken: "fin_pull_123", PackID: "pack_123", ExpiresAt: time.Now().Add(15 * time.Minute)},
		},
	)

	client := locktivity.NewClientWithHTTP(server.Client(), server.URL())
	handler := NewHandlerWithClient(client, nil)

	resp, err := handler.PullPrepare(componentsdk.PullPrepareRequest{
		RequestID: "req_123",
		Target: componentsdk.RemoteTarget{
			Environment: "prod",
		},
		Ref: componentsdk.PullRef{Digest: "sha256:pack"},
	})
	if err != nil {
		t.Fatalf("PullPrepare failed: %v", err)
	}
	if resp.Pack.Digest != "sha256:file" {
		t.Fatalf("expected pack digest sha256:file, got %q", resp.Pack.Digest)
	}
}

func TestPullPrepare_ByReleaseIDUsesReleasePath(t *testing.T) {
	silenceTestLogs(t)

	server := contracttest.NewServer(t,
		contracttest.Step{
			Method:   http.MethodGet,
			Path:     locktivity.APIPathPrefix + "/releases/rel_123",
			Status:   http.StatusOK,
			JSONBody: locktivity.ReleaseResponse{ID: "rel_123", Pack: &locktivity.PackInfo{ID: "pack_123"}},
		},
		contracttest.Step{
			Method: http.MethodGet,
			Path:   locktivity.APIPathPrefix + "/packs/pack_123",
			Status: http.StatusOK,
			JSONBody: locktivity.GetPackResponse{
				ID:         "pack_123",
				FileDigest: "sha256:file",
				PackDigest: "sha256:pack",
				SizeBytes:  123,
				Download:   &locktivity.DownloadInfo{URL: "https://storage.example.com/download"},
			},
		},
		contracttest.Step{
			Method:   http.MethodPost,
			Path:     locktivity.APIPathPrefix + "/finalize_intents",
			Status:   http.StatusCreated,
			JSONBody: locktivity.CreateFinalizeIntentResponse{FinalizeToken: "fin_pull_123", PackID: "pack_123", ExpiresAt: time.Now().Add(15 * time.Minute)},
		},
	)

	client := locktivity.NewClientWithHTTP(server.Client(), server.URL())
	handler := NewHandlerWithClient(client, nil)

	resp, err := handler.PullPrepare(componentsdk.PullPrepareRequest{
		RequestID: "req_123",
		Target: componentsdk.RemoteTarget{
			Environment: "prod",
		},
		Ref: componentsdk.PullRef{ReleaseID: "rel_123"},
	})
	if err != nil {
		t.Fatalf("PullPrepare failed: %v", err)
	}
	if resp.Download.URL != "https://storage.example.com/download" {
		t.Fatalf("expected download URL https://storage.example.com/download, got %q", resp.Download.URL)
	}
}

func TestPushPrepare_ExistingPackSkipsUploadAndPreservesMetadata(t *testing.T) {
	silenceTestLogs(t)

	server := contracttest.NewServer(t, contracttest.Step{
		Method: http.MethodPost,
		Path:   locktivity.APIPathPrefix + "/packs",
		Status: http.StatusCreated,
		JSONBody: locktivity.CreatePackResponse{
			ID:         "pack_123",
			FileDigest: "sha256:file",
			SizeBytes:  123,
			Exists:     true,
			Upload: &locktivity.UploadInfo{
				FinalizeToken: "fin_existing_123",
			},
		},
	})

	client := locktivity.NewClientWithHTTP(server.Client(), server.URL())
	handler := NewHandlerWithClient(client, nil)

	resp, err := handler.PushPrepare(componentsdk.PushPrepareRequest{
		RequestID: "req_123",
		Target: componentsdk.RemoteTarget{
			Environment: "prod",
		},
		Pack: componentsdk.PackInfo{
			Digest:     "sha256:pack",
			FileDigest: "sha256:file",
			SizeBytes:  123,
		},
		Release: componentsdk.ReleaseInfo{
			Version:      "1.2.3",
			Notes:        "release notes",
			Labels:       []string{"prod", "monthly"},
			BuildContext: map[string]string{"sha": "abc123"},
		},
	})
	if err != nil {
		t.Fatalf("PushPrepare failed: %v", err)
	}
	if resp.Upload.Method != "skip" {
		t.Fatalf("expected upload method skip, got %q", resp.Upload.Method)
	}

	tokenData := mustDecodePushFinalizeToken(t, handler, resp.FinalizeToken)
	if tokenData.FinalizeToken != "fin_existing_123" {
		t.Fatalf("expected finalize token fin_existing_123, got %q", tokenData.FinalizeToken)
	}
	if tokenData.Version != "1.2.3" || tokenData.Environment != "prod" || tokenData.Notes != "release notes" {
		t.Fatalf("unexpected token metadata: %#v", tokenData)
	}
}

func TestPushFinalize_RetriesAcceptedAndSucceeds(t *testing.T) {
	silenceTestLogs(t)

	server := contracttest.NewServer(t,
		contracttest.Step{
			Method: http.MethodPost,
			Path:   locktivity.APIPathPrefix + "/releases",
			Status: http.StatusAccepted,
			Check: func(t *testing.T, r *http.Request, body []byte) {
				t.Helper()

				var payload map[string]map[string]any
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("failed to decode request body: %v", err)
				}
				if payload["release"]["finalize_token"] != "fin_123" {
					t.Fatalf("expected finalize_token fin_123, got %v", payload["release"]["finalize_token"])
				}
			},
			JSONBody: locktivity.APIError{Errors: []string{"processing"}},
		},
		contracttest.Step{
			Method:   http.MethodPost,
			Path:     locktivity.APIPathPrefix + "/releases",
			Status:   http.StatusCreated,
			JSONBody: locktivity.ReleaseResponse{ID: "rel_123", Version: "1.0.0", Pack: &locktivity.PackInfo{ID: "pack_123"}},
		},
	)

	client := locktivity.NewClientWithHTTP(server.Client(), server.URL())
	handler := NewHandlerWithClient(client, nil)

	var waits []time.Duration
	handler.sleep = func(wait time.Duration) {
		waits = append(waits, wait)
	}

	token := mustEncodePushFinalizeToken(t, handler, "nonce-accepted")

	resp, err := handler.PushFinalize(componentsdk.PushFinalizeRequest{
		RequestID:     "req_123",
		FinalizeToken: token,
	})
	if err != nil {
		t.Fatalf("PushFinalize failed: %v", err)
	}
	if resp.Release.ReleaseID != "rel_123" {
		t.Fatalf("expected release ID rel_123, got %q", resp.Release.ReleaseID)
	}
	if len(waits) != 1 || waits[0] != initialRetryWait {
		t.Fatalf("expected one wait of %s, got %v", initialRetryWait, waits)
	}
}

func TestPullFinalize_ConsumesFinalizeIntent(t *testing.T) {
	silenceTestLogs(t)

	server := contracttest.NewServer(t, contracttest.Step{
		Method:   http.MethodPatch,
		Path:     locktivity.APIPathPrefix + "/finalize_intents/fin_pull_123",
		Status:   http.StatusOK,
		JSONBody: locktivity.ConsumeFinalizeIntentResponse{},
	})

	client := locktivity.NewClientWithHTTP(server.Client(), server.URL())
	handler := NewHandlerWithClient(client, nil)

	resp, err := handler.PullFinalize(componentsdk.PullFinalizeRequest{
		RequestID:     "req_123",
		FinalizeToken: mustEncodePullFinalizeToken(t, handler, "nonce-pull"),
		PackDigest:    "sha256:file",
	})
	if err != nil {
		t.Fatalf("PullFinalize failed: %v", err)
	}
	if !resp.Confirmed {
		t.Fatal("expected confirmed true")
	}
}

func TestPullFinalize_MapsConsumedIntentToConflict(t *testing.T) {
	silenceTestLogs(t)

	server := contracttest.NewServer(t, contracttest.Step{
		Method: http.MethodPatch,
		Path:   locktivity.APIPathPrefix + "/finalize_intents/fin_pull_123",
		Status: http.StatusConflict,
		JSONBody: locktivity.APIError{
			Code:   "FINALIZE_TOKEN_CONSUMED",
			Errors: []string{"already used"},
		},
	})

	client := locktivity.NewClientWithHTTP(server.Client(), server.URL())
	handler := NewHandlerWithClient(client, nil)

	_, err := handler.PullFinalize(componentsdk.PullFinalizeRequest{
		RequestID:     "req_123",
		FinalizeToken: mustEncodePullFinalizeToken(t, handler, "nonce-pull-conflict"),
		PackDigest:    "sha256:file",
	})
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}

	remoteErr, ok := err.(componentsdk.RemoteError)
	if !ok {
		t.Fatalf("expected componentsdk.RemoteError, got %T", err)
	}
	if remoteErr.Code != "conflict" {
		t.Fatalf("expected conflict, got %q (%s)", remoteErr.Code, remoteErr.Message)
	}
}

func TestPushFinalize_RetriesRateLimitedAndSucceeds(t *testing.T) {
	silenceTestLogs(t)

	server := contracttest.NewServer(t,
		contracttest.Step{
			Method: http.MethodPost,
			Path:   locktivity.APIPathPrefix + "/releases",
			Status: http.StatusTooManyRequests,
			Headers: map[string]string{
				"Retry-After": "7",
			},
			JSONBody: locktivity.APIError{Errors: []string{"too many requests"}},
		},
		contracttest.Step{
			Method:   http.MethodPost,
			Path:     locktivity.APIPathPrefix + "/releases",
			Status:   http.StatusCreated,
			JSONBody: locktivity.ReleaseResponse{ID: "rel_123", Version: "1.0.0", Pack: &locktivity.PackInfo{ID: "pack_123"}},
		},
	)

	client := locktivity.NewClientWithHTTP(server.Client(), server.URL())
	handler := NewHandlerWithClient(client, nil)

	var waits []time.Duration
	handler.sleep = func(wait time.Duration) {
		waits = append(waits, wait)
	}

	token := mustEncodePushFinalizeToken(t, handler, "nonce-rate-limit")

	resp, err := handler.PushFinalize(componentsdk.PushFinalizeRequest{
		RequestID:     "req_123",
		FinalizeToken: token,
	})
	if err != nil {
		t.Fatalf("PushFinalize failed: %v", err)
	}
	if resp.Release.ReleaseID != "rel_123" {
		t.Fatalf("expected release ID rel_123, got %q", resp.Release.ReleaseID)
	}
	if len(waits) != 1 || waits[0] != 7*time.Second {
		t.Fatalf("expected one wait of 7s, got %v", waits)
	}
}

func TestPushPrepare_MapsUnauthorizedToAuthRequired(t *testing.T) {
	silenceTestLogs(t)

	server := contracttest.NewServer(t, contracttest.Step{
		Method: http.MethodPost,
		Path:   locktivity.APIPathPrefix + "/packs",
		Status: http.StatusUnauthorized,
		JSONBody: locktivity.APIError{
			Code:   "unauthorized",
			Errors: []string{"token expired"},
		},
	})

	client := locktivity.NewClientWithHTTP(server.Client(), server.URL())
	handler := NewHandlerWithClient(client, nil)

	_, err := handler.PushPrepare(componentsdk.PushPrepareRequest{
		RequestID: "req_123",
		Target: componentsdk.RemoteTarget{
			Environment: "prod",
		},
		Pack: componentsdk.PackInfo{
			Digest:     "sha256:pack",
			FileDigest: "sha256:file",
			SizeBytes:  123,
		},
	})
	if err == nil {
		t.Fatal("expected auth error, got nil")
	}

	remoteErr, ok := err.(componentsdk.RemoteError)
	if !ok {
		t.Fatalf("expected componentsdk.RemoteError, got %T", err)
	}
	if remoteErr.Code != "auth_required" {
		t.Fatalf("expected auth_required, got %q (%s)", remoteErr.Code, remoteErr.Message)
	}
}

func TestPushFinalize_MapsFinalizeRedemptionDeniedToForbidden(t *testing.T) {
	silenceTestLogs(t)

	server := contracttest.NewServer(t, contracttest.Step{
		Method: http.MethodPost,
		Path:   locktivity.APIPathPrefix + "/releases",
		Status: http.StatusForbidden,
		JSONBody: locktivity.APIError{
			Code:   "FINALIZE_TOKEN_REDEMPTION_DENIED",
			Errors: []string{"wrong oauth app"},
		},
	})

	client := locktivity.NewClientWithHTTP(server.Client(), server.URL())
	handler := NewHandlerWithClient(client, nil)

	_, err := handler.PushFinalize(componentsdk.PushFinalizeRequest{
		RequestID:     "req_123",
		FinalizeToken: mustEncodePushFinalizeToken(t, handler, "nonce-denied"),
	})
	if err == nil {
		t.Fatal("expected forbidden error, got nil")
	}

	remoteErr, ok := err.(componentsdk.RemoteError)
	if !ok {
		t.Fatalf("expected componentsdk.RemoteError, got %T", err)
	}
	if remoteErr.Code != "forbidden" {
		t.Fatalf("expected forbidden, got %q (%s)", remoteErr.Code, remoteErr.Message)
	}
}

func TestPushFinalize_MapsFinalizeConflictToConflict(t *testing.T) {
	silenceTestLogs(t)

	server := contracttest.NewServer(t, contracttest.Step{
		Method: http.MethodPost,
		Path:   locktivity.APIPathPrefix + "/releases",
		Status: http.StatusConflict,
		JSONBody: locktivity.APIError{
			Code:   "FINALIZE_TOKEN_CONSUMED",
			Errors: []string{"already used"},
		},
	})

	client := locktivity.NewClientWithHTTP(server.Client(), server.URL())
	handler := NewHandlerWithClient(client, nil)

	_, err := handler.PushFinalize(componentsdk.PushFinalizeRequest{
		RequestID:     "req_123",
		FinalizeToken: mustEncodePushFinalizeToken(t, handler, "nonce-conflict"),
	})
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}

	remoteErr, ok := err.(componentsdk.RemoteError)
	if !ok {
		t.Fatalf("expected componentsdk.RemoteError, got %T", err)
	}
	if remoteErr.Code != "conflict" {
		t.Fatalf("expected conflict, got %q (%s)", remoteErr.Code, remoteErr.Message)
	}
}

func TestPushFinalize_ReturnsServerErrorAfterAcceptedRetriesExhausted(t *testing.T) {
	silenceTestLogs(t)

	steps := make([]contracttest.Step, 0, maxRetries+1)
	for i := 0; i <= maxRetries; i++ {
		steps = append(steps, contracttest.Step{
			Method: http.MethodPost,
			Path:   locktivity.APIPathPrefix + "/releases",
			Status: http.StatusAccepted,
			JSONBody: locktivity.APIError{
				Errors: []string{"processing"},
			},
		})
	}

	server := contracttest.NewServer(t, steps...)
	client := locktivity.NewClientWithHTTP(server.Client(), server.URL())
	handler := NewHandlerWithClient(client, nil)

	var waits []time.Duration
	handler.sleep = func(wait time.Duration) {
		waits = append(waits, wait)
	}

	_, err := handler.PushFinalize(componentsdk.PushFinalizeRequest{
		RequestID:     "req_123",
		FinalizeToken: mustEncodePushFinalizeToken(t, handler, "nonce-accepted-timeout"),
	})
	if err == nil {
		t.Fatal("expected server error, got nil")
	}

	remoteErr, ok := err.(componentsdk.RemoteError)
	if !ok {
		t.Fatalf("expected componentsdk.RemoteError, got %T", err)
	}
	if remoteErr.Code != "server_error" {
		t.Fatalf("expected server_error, got %q (%s)", remoteErr.Code, remoteErr.Message)
	}
	if remoteErr.Message != "pack processing timed out, please retry" {
		t.Fatalf("unexpected message: %q", remoteErr.Message)
	}
	if len(waits) != maxRetries {
		t.Fatalf("expected %d waits, got %d", maxRetries, len(waits))
	}
}

func TestPushFinalize_ReturnsRateLimitedAfterRetryBudgetExhausted(t *testing.T) {
	silenceTestLogs(t)

	steps := make([]contracttest.Step, 0, maxRateLimitRetries+1)
	for i := 0; i <= maxRateLimitRetries; i++ {
		steps = append(steps, contracttest.Step{
			Method: http.MethodPost,
			Path:   locktivity.APIPathPrefix + "/releases",
			Status: http.StatusTooManyRequests,
			Headers: map[string]string{
				"Retry-After": "7",
			},
			JSONBody: locktivity.APIError{
				Errors: []string{"too many requests"},
			},
		})
	}

	server := contracttest.NewServer(t, steps...)
	client := locktivity.NewClientWithHTTP(server.Client(), server.URL())
	handler := NewHandlerWithClient(client, nil)

	var waits []time.Duration
	handler.sleep = func(wait time.Duration) {
		waits = append(waits, wait)
	}

	_, err := handler.PushFinalize(componentsdk.PushFinalizeRequest{
		RequestID:     "req_123",
		FinalizeToken: mustEncodePushFinalizeToken(t, handler, "nonce-rate-limit-budget"),
	})
	if err == nil {
		t.Fatal("expected rate limited error, got nil")
	}

	remoteErr, ok := err.(componentsdk.RemoteError)
	if !ok {
		t.Fatalf("expected componentsdk.RemoteError, got %T", err)
	}
	if remoteErr.Code != "rate_limited" {
		t.Fatalf("expected rate_limited, got %q (%s)", remoteErr.Code, remoteErr.Message)
	}
	if len(waits) != maxRateLimitRetries {
		t.Fatalf("expected %d waits, got %d", maxRateLimitRetries, len(waits))
	}
	if waits[0] != 7*time.Second {
		t.Fatalf("expected first wait 7s, got %v", waits[0])
	}
}

func TestRunsSync_RetriesRateLimitedAndSucceeds(t *testing.T) {
	silenceTestLogs(t)

	dir := t.TempDir()
	resultPath := filepath.Join(dir, "result.json")
	resultJSON := `{"tool":{"name":"tool","version":"1.0.0"},"status":"ok","outputs":[]}`
	if err := os.WriteFile(resultPath, []byte(resultJSON), 0o600); err != nil {
		t.Fatalf("failed to write result.json: %v", err)
	}

	server := contracttest.NewServer(t,
		contracttest.Step{
			Method: http.MethodPost,
			Path:   locktivity.APIPathPrefix + "/pack_runs",
			Status: http.StatusTooManyRequests,
			Headers: map[string]string{
				"Retry-After": "9",
			},
			JSONBody: locktivity.APIError{Errors: []string{"too many requests"}},
		},
		contracttest.Step{
			Method:   http.MethodPost,
			Path:     locktivity.APIPathPrefix + "/pack_runs",
			Status:   http.StatusCreated,
			JSONBody: locktivity.PackRunsResponse{Accepted: 1, Rejected: 0, Runs: []locktivity.PackRunInfo{{RunID: "run_123", Status: "accepted"}}},
		},
	)

	client := locktivity.NewClientWithHTTP(server.Client(), server.URL())
	handler := NewHandlerWithClient(client, nil)

	var waits []time.Duration
	handler.sleep = func(wait time.Duration) {
		waits = append(waits, wait)
	}

	resp, err := handler.RunsSync(RunsSyncRequest{
		RequestID:  "req_123",
		FileDigest: "sha256:file",
		Runs: []RunInfo{
			{RunID: "run_123", ResultPath: resultPath, ResultDigest: "sha256:result"},
		},
	})
	if err != nil {
		t.Fatalf("RunsSync failed: %v", err)
	}
	if resp.Accepted != 1 {
		t.Fatalf("expected accepted 1, got %d", resp.Accepted)
	}
	if len(waits) != 1 || waits[0] != 9*time.Second {
		t.Fatalf("expected one wait of 9s, got %v", waits)
	}
}

func TestRunsSync_UploadsAndConfirmsOutputs(t *testing.T) {
	silenceTestLogs(t)

	dir := t.TempDir()
	resultPath := filepath.Join(dir, "result.json")
	outputPath := filepath.Join(dir, "output.txt")

	if err := os.WriteFile(outputPath, []byte("hello world"), 0o600); err != nil {
		t.Fatalf("failed to write output file: %v", err)
	}

	resultJSON := `{
		"tool":{"name":"tool","version":"1.0.0"},
		"status":"ok",
		"started_at":"2026-03-13T10:00:00Z",
		"completed_at":"2026-03-13T10:00:01Z",
		"duration_ms":1000,
		"exit_code":0,
		"tool_exit_code":0,
		"outputs":[{"path":"output.txt","media_type":"text/plain","digest":"sha256:output","bytes":11}]
	}`
	if err := os.WriteFile(resultPath, []byte(resultJSON), 0o600); err != nil {
		t.Fatalf("failed to write result.json: %v", err)
	}

	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("expected PUT upload, got %s", r.Method)
		}
		if r.URL.Path != "/upload/output.txt" {
			t.Fatalf("expected upload path /upload/output.txt, got %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "text/plain" {
			t.Fatalf("expected Content-Type text/plain, got %q", r.Header.Get("Content-Type"))
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read upload body: %v", err)
		}
		_ = r.Body.Close()
		if string(body) != "hello world" {
			t.Fatalf("expected uploaded body hello world, got %q", string(body))
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer uploadServer.Close()

	server := contracttest.NewServer(t,
		contracttest.Step{
			Method: http.MethodPost,
			Path:   locktivity.APIPathPrefix + "/pack_runs",
			Status: http.StatusCreated,
			Check: func(t *testing.T, r *http.Request, body []byte) {
				t.Helper()

				var payload map[string]any
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("failed to decode request body: %v", err)
				}
				if payload["file_digest"] != "sha256:file" {
					t.Fatalf("expected file_digest sha256:file, got %v", payload["file_digest"])
				}

				runs, ok := payload["runs"].([]any)
				if !ok || len(runs) != 1 {
					t.Fatalf("expected one run, got %v", payload["runs"])
				}
				run, ok := runs[0].(map[string]any)
				if !ok {
					t.Fatalf("expected run map, got %T", runs[0])
				}
				if run["tool_name"] != "tool" || run["tool_version"] != "1.0.0" {
					t.Fatalf("unexpected tool metadata: %v", run)
				}

				outputs, ok := run["outputs"].([]any)
				if !ok || len(outputs) != 1 {
					t.Fatalf("expected one output, got %v", run["outputs"])
				}
				output, ok := outputs[0].(map[string]any)
				if !ok {
					t.Fatalf("expected output map, got %T", outputs[0])
				}
				if output["path"] != "output.txt" {
					t.Fatalf("expected output path output.txt, got %v", output["path"])
				}
				if output["checksum"] != "XrY7u+Ae7tCTyyK7j1rNww==" {
					t.Fatalf("unexpected checksum: %v", output["checksum"])
				}
			},
			JSONBody: locktivity.PackRunsResponse{
				Accepted: 1,
				Rejected: 0,
				Runs: []locktivity.PackRunInfo{
					{
						ID:     "run_db_123",
						RunID:  "run_123",
						Status: "accepted",
						Outputs: []locktivity.OutputUploadInfo{
							{
								ID:        "output_123",
								Path:      "output.txt",
								UploadURL: uploadServer.URL + "/upload/output.txt",
								UploadHeaders: map[string]string{
									"Content-Type": "text/plain",
								},
								UploadToken: "upload_123",
							},
						},
					},
				},
			},
		},
		contracttest.Step{
			Method: http.MethodPatch,
			Path:   locktivity.APIPathPrefix + "/pack_run_outputs/output_123",
			Status: http.StatusOK,
			Check: func(t *testing.T, r *http.Request, body []byte) {
				t.Helper()

				var payload map[string]any
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("failed to decode confirm body: %v", err)
				}
				if payload["upload_token"] != "upload_123" {
					t.Fatalf("expected upload_token upload_123, got %v", payload["upload_token"])
				}
			},
			JSONBody: locktivity.ConfirmRunOutputUploadResponse{
				ID:         "output_123",
				Path:       "output.txt",
				ScanStatus: "clean",
			},
		},
	)

	client := locktivity.NewClientWithHTTP(server.Client(), server.URL())
	handler := NewHandlerWithClient(client, nil)

	resp, err := handler.RunsSync(RunsSyncRequest{
		RequestID:  "req_123",
		FileDigest: "sha256:file",
		Runs: []RunInfo{
			{RunID: "run_123", ResultPath: resultPath, ResultDigest: "sha256:result"},
		},
	})
	if err != nil {
		t.Fatalf("RunsSync failed: %v", err)
	}
	if resp.Accepted != 1 || len(resp.Items) != 1 || resp.Items[0].RunID != "run_123" {
		t.Fatalf("unexpected runs.sync response: %#v", resp)
	}
}
