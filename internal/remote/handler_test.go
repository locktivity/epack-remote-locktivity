package remote

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"errors"
	"github.com/locktivity/epack-remote-locktivity/internal/auth"
	"github.com/locktivity/epack-remote-locktivity/internal/locktivity"
	"github.com/locktivity/epack/componentsdk"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
)

func silenceTestLogs(t *testing.T) {
	t.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() {
		slog.SetDefault(prev)
	})
}

func TestPushPrepare(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	keychain := auth.NewMemoryKeychain()
	_ = keychain.SetToken("test-token")

	handler := &Handler{
		client:   mockClient,
		keychain: keychain,
		endpoint: "https://api.locktivity.com",
	}

	req := PushPrepareRequest{
		RequestID: "req_123",
		Target: RemoteTarget{
			Environment: "prod",
		},
		Pack: PackInfo{
			Digest:    "sha256:abc123",
			SizeBytes: 1024,
		},
	}

	resp, err := handler.PushPrepare(req)
	if err != nil {
		t.Fatalf("PushPrepare failed: %v", err)
	}

	if resp.Upload.URL != "https://storage.example.com/upload" {
		t.Errorf("expected upload URL 'https://storage.example.com/upload', got '%s'", resp.Upload.URL)
	}

	if resp.FinalizeToken == "" {
		t.Error("expected finalize token to be set")
	}
	var tokenData PushFinalizeTokenData
	if err := handler.decodeSignedToken(resp.FinalizeToken, &tokenData); err != nil {
		t.Fatalf("failed to decode finalize token: %v", err)
	}
	if tokenData.FinalizeToken == "" {
		t.Fatal("expected finalize_token in push finalize token")
	}

	if len(mockClient.CreatePackCalls) != 1 {
		t.Errorf("expected 1 CreatePack call, got %d", len(mockClient.CreatePackCalls))
	}

}

func TestPushFinalize(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	keychain := auth.NewMemoryKeychain()
	_ = keychain.SetToken("test-token")

	handler := &Handler{
		client:   mockClient,
		keychain: keychain,
		endpoint: "https://api.locktivity.com",
	}

	token, err := handler.encodeSignedToken(PushFinalizeTokenData{
		FinalizeToken: "fin_123",
		PackID:        "pack_123",
		PackDigest:    "sha256:abc123",
		UploadToken:   "upload_token_123",
		ExpiresAt:     4102444800, // 2100-01-01
		Nonce:         "nonce-test",
	})
	if err != nil {
		t.Fatalf("failed to encode token: %v", err)
	}

	req := PushFinalizeRequest{
		RequestID:     "req_123",
		FinalizeToken: token,
		Release: ReleaseInfo{
			LockProvenance: &LockProvenance{
				LockfileSHA256: "lock-sha",
				TriggerKind:    "frozen_check",
				Outcome:        "success",
			},
		},
	}

	resp, err := handler.PushFinalize(req)
	if err != nil {
		t.Fatalf("PushFinalize failed: %v", err)
	}

	if resp.Release.ReleaseID != "rel_123" {
		t.Errorf("expected release ID 'rel_123', got '%s'", resp.Release.ReleaseID)
	}

	if len(mockClient.CreateReleaseCalls) != 1 {
		t.Errorf("expected 1 CreateRelease call, got %d", len(mockClient.CreateReleaseCalls))
	}
	call := mockClient.CreateReleaseCalls[0]
	if call.FinalizeToken != "fin_123" {
		t.Fatalf("expected finalize_token fin_123, got %q", call.FinalizeToken)
	}
	if provenance, ok := call.LockProvenance.(*LockProvenance); !ok || provenance.LockfileSHA256 != "lock-sha" {
		t.Fatalf("expected lock provenance on release request, got %#v", call.LockProvenance)
	}
}

func TestLockReport(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	handler := &Handler{client: mockClient}

	resp, err := handler.LockReport(LockReportRequest{
		RequestID: "req_123",
		LockProvenance: LockProvenance{
			Lockfile:       "schema_version: 1\n",
			LockfileSHA256: "lock-sha",
			TriggerKind:    "bootstrap",
			Outcome:        "success",
			ReportedAt:     "2026-05-25T12:00:00Z",
			Summary:        map[string]any{"schema_version": float64(1)},
			RuntimeContext: map[string]any{
				"pipeline_id": "pipeline-123",
				"github": map[string]any{
					"repository": "acme/evidence",
					"ref":        "refs/heads/locktivity/setup",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("LockReport failed: %v", err)
	}

	if resp.Status != "accepted" {
		t.Fatalf("expected accepted status, got %q", resp.Status)
	}
	if len(mockClient.ReportLockCalls) != 1 {
		t.Fatalf("expected one ReportLock call, got %d", len(mockClient.ReportLockCalls))
	}
	call := mockClient.ReportLockCalls[0]
	if call.PipelineID != "pipeline-123" || call.RepoOwner != "acme" || call.RepoName != "evidence" {
		t.Fatalf("unexpected repo context: %#v", call)
	}
	if call.Branch != "locktivity/setup" {
		t.Fatalf("expected branch locktivity/setup, got %q", call.Branch)
	}
	if call.LockfileSHA256 != "lock-sha" {
		t.Fatalf("expected lock sha, got %q", call.LockfileSHA256)
	}
	if wire, _ := json.Marshal(resp); resp.PipelineURL != "" || strings.Contains(string(wire), "pipeline_url") {
		t.Fatalf("expected no pipeline_url when the API sends none, got %s", wire)
	}

	page := "https://app.locktivity.com/evidence_packs/pipelines/pipeline-123"
	mockClient.ReportLockResponse.PipelineURL = page
	resp, err = handler.LockReport(LockReportRequest{RequestID: "req_124", LockProvenance: LockProvenance{TriggerKind: "check", Outcome: "success"}})
	if err != nil {
		t.Fatalf("LockReport failed: %v", err)
	}
	if wire, _ := json.Marshal(resp); resp.PipelineURL != page || !strings.Contains(string(wire), `"pipeline_url":"`+page+`"`) {
		t.Fatalf("expected the API's pipeline_url passed through, got %s", wire)
	}
}

func TestPushPrepare_ExistingPackIncludesFinalizeToken(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	mockClient.CreatePackResponse.Exists = true
	mockClient.CreatePackResponse.Upload = &locktivity.UploadInfo{
		FinalizeToken: "fin_existing_123",
	}

	handler := &Handler{
		client: mockClient,
	}

	req := PushPrepareRequest{
		RequestID: "req_123",
		Target: RemoteTarget{
			Environment: "prod",
		},
		Pack: PackInfo{
			Digest:    "sha256:abc123",
			SizeBytes: 1024,
		},
	}

	resp, err := handler.PushPrepare(req)
	if err != nil {
		t.Fatalf("PushPrepare failed: %v", err)
	}
	if resp.Upload.Method != "skip" {
		t.Fatalf("expected upload method skip, got %q", resp.Upload.Method)
	}
	var tokenData PushFinalizeTokenData
	if err := handler.decodeSignedToken(resp.FinalizeToken, &tokenData); err != nil {
		t.Fatalf("failed to decode finalize token: %v", err)
	}
	if tokenData.FinalizeToken != "fin_existing_123" {
		t.Fatalf("expected finalize_token fin_existing_123, got %q", tokenData.FinalizeToken)
	}
}

func TestPullPrepare(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	keychain := auth.NewMemoryKeychain()
	_ = keychain.SetToken("test-token")

	handler := &Handler{
		client:   mockClient,
		keychain: keychain,
		endpoint: "https://api.locktivity.com",
	}

	req := componentsdk.PullPrepareRequest{
		RequestID: "req_123",
		Target: componentsdk.RemoteTarget{
			Environment: "prod",
		},
		Ref: componentsdk.PullRef{
			Latest: true,
		},
	}

	resp, err := handler.PullPrepare(req)
	if err != nil {
		t.Fatalf("PullPrepare failed: %v", err)
	}

	if resp.Download.URL != "https://storage.example.com/download" {
		t.Errorf("expected download URL 'https://storage.example.com/download', got '%s'", resp.Download.URL)
	}

	if resp.Pack.Digest != "sha256:abc123" {
		t.Errorf("expected digest 'sha256:abc123', got '%s'", resp.Pack.Digest)
	}
	var tokenData PullFinalizeTokenData
	if err := handler.decodeSignedToken(resp.FinalizeToken, &tokenData); err != nil {
		t.Fatalf("failed to decode pull finalize token: %v", err)
	}
	if tokenData.FinalizeToken == "" {
		t.Fatal("expected finalize_token in pull finalize token")
	}

	if len(mockClient.GetLatestReleaseCalls) != 1 {
		t.Errorf("expected 1 GetLatestRelease call, got %d", len(mockClient.GetLatestReleaseCalls))
	}
	if mockClient.GetLatestReleaseCalls[0].Environment != "prod" {
		t.Errorf("expected environment 'prod', got '%s'", mockClient.GetLatestReleaseCalls[0].Environment)
	}

	if len(mockClient.GetPackCalls) != 1 {
		t.Errorf("expected 1 GetPack call, got %d", len(mockClient.GetPackCalls))
	}
	if len(mockClient.CreateFinalizeIntentCalls) != 1 {
		t.Fatalf("expected 1 CreateFinalizeIntent call, got %d", len(mockClient.CreateFinalizeIntentCalls))
	}
	if mockClient.CreateFinalizeIntentCalls[0].PackID != "pack_123" {
		t.Fatalf("expected finalize intent pack_id pack_123, got %q", mockClient.CreateFinalizeIntentCalls[0].PackID)
	}
}

func TestPullPrepare_ByVersion(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	keychain := auth.NewMemoryKeychain()
	_ = keychain.SetToken("test-token")

	handler := &Handler{
		client:   mockClient,
		keychain: keychain,
		endpoint: "https://api.locktivity.com",
	}

	req := componentsdk.PullPrepareRequest{
		RequestID: "req_123",
		Target: componentsdk.RemoteTarget{
			Environment: "prod",
		},
		Ref: componentsdk.PullRef{
			Version: "v1.0.0",
		},
	}

	resp, err := handler.PullPrepare(req)
	if err != nil {
		t.Fatalf("PullPrepare failed: %v", err)
	}

	if resp.Download.URL != "https://storage.example.com/download" {
		t.Errorf("expected download URL 'https://storage.example.com/download', got '%s'", resp.Download.URL)
	}

	if len(mockClient.GetVersionReleaseCalls) != 1 {
		t.Fatalf("expected 1 GetReleaseByVersion call, got %d", len(mockClient.GetVersionReleaseCalls))
	}

	call := mockClient.GetVersionReleaseCalls[0]
	if call.Version != "v1.0.0" {
		t.Errorf("expected version 'v1.0.0', got '%s'", call.Version)
	}
	if call.Environment != "prod" {
		t.Errorf("expected environment 'prod', got '%s'", call.Environment)
	}
}

func TestPullPrepare_ByDigest(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	keychain := auth.NewMemoryKeychain()
	_ = keychain.SetToken("test-token")

	handler := &Handler{
		client:   mockClient,
		keychain: keychain,
		endpoint: "https://api.locktivity.com",
	}

	req := componentsdk.PullPrepareRequest{
		RequestID: "req_123",
		Target: componentsdk.RemoteTarget{
			Environment: "prod",
		},
		Ref: componentsdk.PullRef{
			Digest: "sha256:abc123",
		},
	}

	resp, err := handler.PullPrepare(req)
	if err != nil {
		t.Fatalf("PullPrepare failed: %v", err)
	}

	if resp.Download.URL != "https://storage.example.com/download" {
		t.Errorf("expected download URL 'https://storage.example.com/download', got '%s'", resp.Download.URL)
	}

	if len(mockClient.GetDigestReleaseCalls) != 1 {
		t.Fatalf("expected 1 GetReleaseByDigest call, got %d", len(mockClient.GetDigestReleaseCalls))
	}

	call := mockClient.GetDigestReleaseCalls[0]
	if call.Digest != "sha256:abc123" {
		t.Errorf("expected digest 'sha256:abc123', got '%s'", call.Digest)
	}
	if call.Environment != "prod" {
		t.Errorf("expected environment 'prod', got '%s'", call.Environment)
	}
}

func TestPullPrepare_RateLimited(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	handler := &Handler{
		client: mockClient,
	}

	req := componentsdk.PullPrepareRequest{
		RequestID: "req_123",
		Target: componentsdk.RemoteTarget{
			Environment: "prod",
		},
		Ref: componentsdk.PullRef{
			Latest: true,
		},
	}

	if _, err := handler.PullPrepare(req); err != nil {
		t.Fatalf("first PullPrepare failed: %v", err)
	}
	if _, err := handler.PullPrepare(req); err == nil {
		t.Fatal("expected rate-limited error on second PullPrepare")
	}
}

func TestPullFinalize(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	keychain := auth.NewMemoryKeychain()
	_ = keychain.SetToken("test-token")

	handler := &Handler{
		client:   mockClient,
		keychain: keychain,
		endpoint: "https://api.locktivity.com",
	}

	token, err := handler.encodeSignedToken(PullFinalizeTokenData{
		FinalizeToken: "fin_pull_123",
		PackID:        "pack_123",
		FileDigest:    "sha256:abc123",
		ReleaseID:     "rel_123",
		ExpiresAt:     4102444800, // 2100-01-01
		Nonce:         "nonce-test",
	})
	if err != nil {
		t.Fatalf("failed to encode token: %v", err)
	}

	req := componentsdk.PullFinalizeRequest{
		RequestID:     "req_123",
		FinalizeToken: token,
		PackDigest:    "sha256:abc123",
	}

	resp, err := handler.PullFinalize(req)
	if err != nil {
		t.Fatalf("PullFinalize failed: %v", err)
	}

	if !resp.Confirmed {
		t.Error("expected confirmed to be true")
	}
	if len(mockClient.ConsumeFinalizeIntentCalls) != 1 {
		t.Fatalf("expected 1 ConsumeFinalizeIntent call, got %d", len(mockClient.ConsumeFinalizeIntentCalls))
	}
	if mockClient.ConsumeFinalizeIntentCalls[0].FinalizeIntentID != "fin_pull_123" {
		t.Fatalf("expected consume finalize_intent_id fin_pull_123, got %q", mockClient.ConsumeFinalizeIntentCalls[0].FinalizeIntentID)
	}
}

func TestPullFinalize_RejectsDigestMismatch(t *testing.T) {
	handler := &Handler{client: locktivity.NewMockClient()}
	token, err := handler.encodeSignedToken(PullFinalizeTokenData{
		FinalizeToken: "fin_pull_123",
		PackID:        "pack_123",
		FileDigest:    "sha256:abc123",
		ReleaseID:     "rel_123",
		ExpiresAt:     4102444800,
		Nonce:         "nonce-test",
	})
	if err != nil {
		t.Fatalf("failed to encode token: %v", err)
	}

	_, err = handler.PullFinalize(componentsdk.PullFinalizeRequest{
		RequestID:     "req_123",
		FinalizeToken: token,
		PackDigest:    "sha256:different",
	})
	if err == nil {
		t.Fatal("expected digest mismatch error")
	}
}

func TestPullFinalize_RejectsReplay(t *testing.T) {
	handler := &Handler{client: locktivity.NewMockClient()}
	token, err := handler.encodeSignedToken(PullFinalizeTokenData{
		FinalizeToken: "fin_pull_123",
		PackID:        "pack_123",
		FileDigest:    "sha256:abc123",
		ReleaseID:     "rel_123",
		ExpiresAt:     4102444800,
		Nonce:         "nonce-replay",
	})
	if err != nil {
		t.Fatalf("failed to encode token: %v", err)
	}

	req := componentsdk.PullFinalizeRequest{
		RequestID:     "req_123",
		FinalizeToken: token,
		PackDigest:    "sha256:abc123",
	}

	if _, err := handler.PullFinalize(req); err != nil {
		t.Fatalf("first PullFinalize failed: %v", err)
	}
	if _, err := handler.PullFinalize(req); err == nil {
		t.Fatal("expected replay error on second PullFinalize")
	}
}

func TestPushFinalize_RejectsTamperedToken(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	handler := &Handler{client: mockClient}

	token, err := handler.encodeSignedToken(PushFinalizeTokenData{
		PackID:      "pack_123",
		PackDigest:  "sha256:abc123",
		UploadToken: "upload_token_123",
		ExpiresAt:   4102444800,
		Nonce:       "nonce-test",
	})
	if err != nil {
		t.Fatalf("failed to encode token: %v", err)
	}
	tampered := token[:len(token)-1] + "x"

	_, err = handler.PushFinalize(PushFinalizeRequest{
		RequestID:     "req_123",
		FinalizeToken: tampered,
	})
	if err == nil {
		t.Fatal("expected tampered token error")
	}
}

func TestPushFinalize_RejectsReplay(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	handler := &Handler{client: mockClient}

	token, err := handler.encodeSignedToken(PushFinalizeTokenData{
		PackID:      "pack_123",
		PackDigest:  "sha256:abc123",
		UploadToken: "upload_token_123",
		ExpiresAt:   4102444800,
		Nonce:       "nonce-replay",
	})
	if err != nil {
		t.Fatalf("failed to encode token: %v", err)
	}

	req := PushFinalizeRequest{
		RequestID:     "req_123",
		FinalizeToken: token,
	}

	if _, err := handler.PushFinalize(req); err != nil {
		t.Fatalf("first PushFinalize failed: %v", err)
	}
	if _, err := handler.PushFinalize(req); err == nil {
		t.Fatal("expected replay error on second PushFinalize")
	}
}

func TestFinalizeTokens_RoundTripAcrossHandlersWithAccessToken(t *testing.T) {
	t.Setenv(locktivity.EnvAccessToken, "access_123")

	handlerA := NewHandlerWithClient(locktivity.NewMockClient(), nil)
	handlerB := NewHandlerWithClient(locktivity.NewMockClient(), nil)

	token, err := handlerA.encodeSignedToken(PushFinalizeTokenData{
		PackID:      "pack_123",
		PackDigest:  "sha256:abc123",
		UploadToken: "upload_token_123",
		ExpiresAt:   4102444800,
		Nonce:       "nonce-test",
	})
	if err != nil {
		t.Fatalf("failed to encode token: %v", err)
	}

	var tokenData PushFinalizeTokenData
	if err := handlerB.decodeSignedToken(token, &tokenData); err != nil {
		t.Fatalf("failed to decode token across handlers: %v", err)
	}
	if tokenData.UploadToken != "upload_token_123" {
		t.Fatalf("expected upload token upload_token_123, got %q", tokenData.UploadToken)
	}
}

func TestFinalizeTokens_RoundTripAcrossHandlersWithStoredRefreshToken(t *testing.T) {
	keychain := auth.NewMemoryKeychain()
	_ = keychain.SetRefreshToken("refresh_123")

	handlerA := &Handler{keychain: keychain, tokenKey: generateTokenKey(keychain)}
	handlerB := &Handler{keychain: keychain, tokenKey: generateTokenKey(keychain)}

	token, err := handlerA.encodeSignedToken(PushFinalizeTokenData{
		PackID:      "pack_123",
		PackDigest:  "sha256:abc123",
		UploadToken: "upload_token_123",
		ExpiresAt:   4102444800,
		Nonce:       "nonce-test",
	})
	if err != nil {
		t.Fatalf("failed to encode token: %v", err)
	}

	var tokenData PushFinalizeTokenData
	if err := handlerB.decodeSignedToken(token, &tokenData); err != nil {
		t.Fatalf("failed to decode token across handlers: %v", err)
	}
	if tokenData.UploadToken != "upload_token_123" {
		t.Fatalf("expected upload token upload_token_123, got %q", tokenData.UploadToken)
	}
}

func TestRunsSync(t *testing.T) {
	silenceTestLogs(t)

	mockClient := locktivity.NewMockClient()
	keychain := auth.NewMemoryKeychain()
	_ = keychain.SetToken("test-token")

	handler := &Handler{
		client:   mockClient,
		keychain: keychain,
		endpoint: "https://api.locktivity.com",
	}

	dir := t.TempDir()
	resultPath := filepath.Join(dir, "result.json")
	resultJSON := `{"tool":{"name":"tool","version":"1.0.0"},"status":"ok","outputs":[]}`
	if err := os.WriteFile(resultPath, []byte(resultJSON), 0o600); err != nil {
		t.Fatalf("failed to write result.json: %v", err)
	}

	req := RunsSyncRequest{
		RequestID: "req_123",
		Target: RemoteTarget{
			Stream: "acme",
		},
		FileDigest: "sha256:abc123",
		Runs: []RunInfo{
			{RunID: "run_123", ResultPath: resultPath, ResultDigest: "sha256:def456"},
		},
	}

	resp, err := handler.RunsSync(req)
	if err != nil {
		t.Fatalf("RunsSync failed: %v", err)
	}

	if resp.Accepted != 1 {
		t.Errorf("expected 1 accepted, got %d", resp.Accepted)
	}

	if len(mockClient.SyncRunsCalls) != 1 {
		t.Errorf("expected 1 SyncRuns call, got %d", len(mockClient.SyncRunsCalls))
	}

	// Verify metadata was extracted
	runInfo := mockClient.SyncRunsCalls[0].Runs[0]
	if runInfo.ToolName != "tool" {
		t.Errorf("expected tool name 'tool', got '%s'", runInfo.ToolName)
	}
	if runInfo.ToolVersion != "1.0.0" {
		t.Errorf("expected tool version '1.0.0', got '%s'", runInfo.ToolVersion)
	}
}

func TestRunsSync_RejectsOutputPathTraversal(t *testing.T) {
	silenceTestLogs(t)

	mockClient := locktivity.NewMockClient()
	handler := &Handler{
		client: mockClient,
	}

	dir := t.TempDir()
	resultPath := filepath.Join(dir, "result.json")
	resultJSON := `{
		"tool":{"name":"tool","version":"1.0.0"},
		"status":"ok",
		"outputs":[{"path":"../secret.txt","media_type":"text/plain","digest":"sha256:x","bytes":10}]
	}`
	if err := os.WriteFile(resultPath, []byte(resultJSON), 0o600); err != nil {
		t.Fatalf("failed to write result.json: %v", err)
	}

	req := RunsSyncRequest{
		RequestID:  "req_123",
		FileDigest: "sha256:abc123",
		Runs: []RunInfo{
			{RunID: "run_123", ResultPath: resultPath, ResultDigest: "sha256:def456"},
		},
	}

	if _, err := handler.RunsSync(req); err != nil {
		t.Fatalf("RunsSync failed: %v", err)
	}

	if len(mockClient.SyncRunsCalls) != 1 {
		t.Fatalf("expected 1 SyncRuns call, got %d", len(mockClient.SyncRunsCalls))
	}
	if got := len(mockClient.SyncRunsCalls[0].Runs[0].Outputs); got != 0 {
		t.Fatalf("expected traversal output to be rejected, got %d outputs", got)
	}
}

func TestRunsSync_RejectsOutputSymlinkEscape(t *testing.T) {
	silenceTestLogs(t)

	mockClient := locktivity.NewMockClient()
	handler := &Handler{
		client: mockClient,
	}

	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatalf("failed to write outside file: %v", err)
	}

	linkPath := filepath.Join(dir, "linked-secret.txt")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}

	resultPath := filepath.Join(dir, "result.json")
	resultJSON := `{
		"tool":{"name":"tool","version":"1.0.0"},
		"status":"ok",
		"outputs":[{"path":"linked-secret.txt","media_type":"text/plain","digest":"sha256:x","bytes":6}]
	}`
	if err := os.WriteFile(resultPath, []byte(resultJSON), 0o600); err != nil {
		t.Fatalf("failed to write result.json: %v", err)
	}

	req := RunsSyncRequest{
		RequestID:  "req_123",
		FileDigest: "sha256:abc123",
		Runs: []RunInfo{
			{RunID: "run_123", ResultPath: resultPath, ResultDigest: "sha256:def456"},
		},
	}

	if _, err := handler.RunsSync(req); err != nil {
		t.Fatalf("RunsSync failed: %v", err)
	}

	if len(mockClient.SyncRunsCalls) != 1 {
		t.Fatalf("expected 1 SyncRuns call, got %d", len(mockClient.SyncRunsCalls))
	}
	if got := len(mockClient.SyncRunsCalls[0].Runs[0].Outputs); got != 0 {
		t.Fatalf("expected symlink escape output to be rejected, got %d outputs", got)
	}
}

func TestRunsSync_UploadsToPresignedURLs(t *testing.T) {
	silenceTestLogs(t)

	mockClient := locktivity.NewMockClient()
	// Configure mock to return presigned URLs for outputs
	mockClient.SyncRunsResponse = &locktivity.PackRunsResponse{
		Accepted: 1,
		Rejected: 0,
		Runs: []locktivity.PackRunInfo{
			{
				ID:     "run_db_123",
				RunID:  "run_123",
				Status: "accepted",
				Outputs: []locktivity.OutputUploadInfo{
					{
						ID:        "output_db_123",
						Path:      "output.txt",
						UploadURL: "https://storage.example.com/upload/output.txt",
						UploadHeaders: map[string]string{
							"Content-Type": "text/plain",
						},
					},
				},
			},
		},
	}

	handler := &Handler{
		client: mockClient,
	}

	dir := t.TempDir()
	resultPath := filepath.Join(dir, "result.json")
	outputPath := filepath.Join(dir, "output.txt")
	outputContent := "hello world"
	if err := os.WriteFile(outputPath, []byte(outputContent), 0o600); err != nil {
		t.Fatalf("failed to write output file: %v", err)
	}
	resultJSON := `{
		"tool":{"name":"tool","version":"1.0.0"},
		"status":"ok",
		"outputs":[{"path":"output.txt","media_type":"text/plain","digest":"sha256:abc","bytes":11}]
	}`
	if err := os.WriteFile(resultPath, []byte(resultJSON), 0o600); err != nil {
		t.Fatalf("failed to write result.json: %v", err)
	}

	req := RunsSyncRequest{
		RequestID:  "req_123",
		FileDigest: "sha256:abc123",
		Runs: []RunInfo{
			{RunID: "run_123", ResultPath: resultPath, ResultDigest: "sha256:def456"},
		},
	}

	resp, err := handler.RunsSync(req)
	if err != nil {
		t.Fatalf("RunsSync failed: %v", err)
	}

	if resp.Accepted != 1 {
		t.Errorf("expected 1 accepted, got %d", resp.Accepted)
	}

	// Verify metadata was sent without content
	if len(mockClient.SyncRunsCalls) != 1 {
		t.Fatalf("expected 1 SyncRuns call, got %d", len(mockClient.SyncRunsCalls))
	}
	outputs := mockClient.SyncRunsCalls[0].Runs[0].Outputs
	if len(outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(outputs))
	}
	if outputs[0].Path != "output.txt" {
		t.Errorf("expected output path 'output.txt', got '%s'", outputs[0].Path)
	}

	// Verify file was uploaded to presigned URL
	if len(mockClient.UploadCalls) != 1 {
		t.Fatalf("expected 1 upload call, got %d", len(mockClient.UploadCalls))
	}
	upload := mockClient.UploadCalls[0]
	if upload.URL != "https://storage.example.com/upload/output.txt" {
		t.Errorf("expected upload URL 'https://storage.example.com/upload/output.txt', got '%s'", upload.URL)
	}
	if string(upload.Content) != outputContent {
		t.Errorf("expected upload content '%s', got '%s'", outputContent, string(upload.Content))
	}
	if upload.Headers["Content-Type"] != "text/plain" {
		t.Errorf("expected Content-Type header 'text/plain', got '%s'", upload.Headers["Content-Type"])
	}
}

func TestAuthWhoami(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	keychain := auth.NewMemoryKeychain()
	_ = keychain.SetToken("test-token")

	handler := &Handler{
		client:   mockClient,
		keychain: keychain,
		endpoint: "https://api.locktivity.com",
	}

	req := AuthWhoamiRequest{
		RequestID: "req_123",
	}

	resp, err := handler.AuthWhoami(req)
	if err != nil {
		t.Fatalf("AuthWhoami failed: %v", err)
	}

	if !resp.Identity.Authenticated {
		t.Error("expected authenticated to be true")
	}

	if resp.Identity.Subject != "user@example.com" {
		t.Errorf("expected subject 'user@example.com', got '%s'", resp.Identity.Subject)
	}
}

func TestAuthWhoami_NotAuthenticated(t *testing.T) {
	keychain := auth.NewMemoryKeychain()
	// No token set, no client set

	handler := &Handler{
		keychain: keychain,
		endpoint: "https://api.locktivity.com",
	}

	req := AuthWhoamiRequest{
		RequestID: "req_123",
	}

	resp, err := handler.AuthWhoami(req)
	if err != nil {
		t.Fatalf("AuthWhoami failed: %v", err)
	}

	if resp.Identity.Authenticated {
		t.Error("expected authenticated to be false")
	}
}

const testRedirectURI = "http://127.0.0.1:53682/callback"

// browserSignInHandler points the handler's sign-in at a TLS test server
// configured as the auth endpoint override, the only kind of non-default
// endpoint a release build talks to. API endpoint overrides left in the shell
// are cleared, since a plain HTTP one fails that check too.
func browserSignInHandler(t *testing.T, token http.HandlerFunc) (*Handler, *auth.MemoryKeychain) {
	t.Helper()
	srv := httptest.NewTLSServer(token)
	t.Cleanup(srv.Close)
	t.Setenv(locktivity.EnvAuthMode, "")
	t.Setenv(locktivity.EnvRemoteAuthEndpoint, srv.URL)
	t.Setenv(locktivity.EnvRemoteEndpoint, "")
	t.Setenv(locktivity.EnvEndpoint, "")

	keychain := auth.NewMemoryKeychain()
	oauth := auth.NewOAuth(srv.URL, keychain)
	oauth.SetHTTPClient(srv.Client())
	return &Handler{oauth: oauth, keychain: keychain}, keychain
}

func TestAuthLogin_ReturnsTheAuthorizationURLStateAndSession(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, "")
	handler := &Handler{oauth: auth.NewOAuth(locktivity.DefaultAuthBaseURL, auth.NewMemoryKeychain())}

	resp, err := handler.AuthLogin(AuthLoginRequest{RequestID: "req_1", RedirectURI: testRedirectURI})
	if err != nil {
		t.Fatalf("AuthLogin failed: %v", err)
	}
	if resp.Type != "auth.login.result" || resp.RequestID != "req_1" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	instructions := resp.Instructions
	if !strings.HasPrefix(instructions.AuthorizationURL, "https://app.locktivity.com/epack/oauth2/authorize?") {
		t.Fatalf("unexpected authorization URL: %q", instructions.AuthorizationURL)
	}
	authURL, err := url.Parse(instructions.AuthorizationURL)
	if err != nil {
		t.Fatalf("authorization URL does not parse: %v", err)
	}
	if instructions.State == "" || authURL.Query().Get("state") != instructions.State {
		t.Fatalf("state %q is not the one in the authorization URL", instructions.State)
	}
	if authURL.Query().Get("redirect_uri") != testRedirectURI {
		t.Fatalf("unexpected redirect_uri: %q", authURL.Query().Get("redirect_uri"))
	}
	if instructions.Session == "" || instructions.ExpiresInSecs != 600 {
		t.Fatalf("unexpected instructions: %#v", instructions)
	}

	_, err = handler.AuthLogin(AuthLoginRequest{RequestID: "req_2", RedirectURI: testRedirectURI})
	var remoteErr componentsdk.RemoteError
	if !errors.As(err, &remoteErr) || remoteErr.Code != "rate_limited" {
		t.Fatalf("expected a second auth.login right away to be rate limited, got %v", err)
	}
}

func TestAuthLogin_RefusesARedirectURIOutsideTheLoopbackCallback(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, "")

	for _, redirect := range []string{"", "https://127.0.0.1:53682/callback", "http://localhost:53682/callback", "http://127.0.0.1:53682/callback?next=1"} {
		handler := &Handler{oauth: auth.NewOAuth(locktivity.DefaultAuthBaseURL, auth.NewMemoryKeychain())}
		_, err := handler.AuthLogin(AuthLoginRequest{RequestID: "req_1", RedirectURI: redirect})
		var remoteErr componentsdk.RemoteError
		if !errors.As(err, &remoteErr) || remoteErr.Code != "invalid_request" || !strings.Contains(remoteErr.Message, "http://127.0.0.1:<port>/callback") {
			t.Errorf("redirect_uri %q: expected invalid_request naming the loopback form, got %v", redirect, err)
		}
	}
}

func TestAuthLogin_DisabledInClientCredentialsOnlyMode(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, "client_credentials_only")
	handler := &Handler{oauth: auth.NewOAuth(locktivity.DefaultAuthBaseURL, auth.NewMemoryKeychain())}

	_, err := handler.AuthLogin(AuthLoginRequest{RequestID: "req_1", RedirectURI: testRedirectURI})
	if err == nil || !strings.Contains(err.Error(), "disables browser sign-in") {
		t.Fatalf("expected browser sign-in to be refused, got %v", err)
	}
}

func TestAuthComplete_StoresTheSessionAndConfirmsTheIdentity(t *testing.T) {
	handler, keychain := browserSignInHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok_person","token_type":"Bearer","expires_in":3600}`))
	})
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != locktivity.APIPathPrefix+"/identity" || r.Header.Get("Authorization") != "Bearer tok_person" {
			t.Errorf("unexpected identity request: %s with %q", r.URL.Path, r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"authenticated":true,"subject":"dana@northwind.com"}`))
	}))
	defer api.Close()
	handler.endpoint = api.URL

	login, err := handler.AuthLogin(AuthLoginRequest{RequestID: "req_1", RedirectURI: testRedirectURI})
	if err != nil {
		t.Fatalf("AuthLogin failed: %v", err)
	}
	resp, err := handler.AuthComplete(AuthCompleteRequest{
		RequestID: "req_2",
		Session:   login.Instructions.Session,
		Code:      "code_1",
		State:     login.Instructions.State,
	})
	if err != nil {
		t.Fatalf("AuthComplete failed: %v", err)
	}
	if resp.Type != "auth.complete.result" || resp.RequestID != "req_2" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if !resp.Identity.Authenticated || resp.Identity.Subject != "dana@northwind.com" {
		t.Fatalf("expected the confirmed identity, got %#v", resp.Identity)
	}
	if token, _ := keychain.GetToken(); token != "tok_person" {
		t.Fatalf("expected the session to be stored, got %q", token)
	}
}

func TestAuthComplete_FinishesWhenTheIdentityLookupFails(t *testing.T) {
	handler, keychain := browserSignInHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok_person","token_type":"Bearer","expires_in":3600}`))
	})
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer api.Close()
	handler.endpoint = api.URL

	login, err := handler.AuthLogin(AuthLoginRequest{RequestID: "req_1", RedirectURI: testRedirectURI})
	if err != nil {
		t.Fatalf("AuthLogin failed: %v", err)
	}
	resp, err := handler.AuthComplete(AuthCompleteRequest{
		RequestID: "req_2",
		Session:   login.Instructions.Session,
		Code:      "code_1",
		State:     login.Instructions.State,
	})
	if err != nil {
		t.Fatalf("AuthComplete failed: %v", err)
	}
	if !resp.Identity.Authenticated || resp.Identity.Subject != "" {
		t.Fatalf("expected an authenticated identity without a subject, got %#v", resp.Identity)
	}
	if token, _ := keychain.GetToken(); token != "tok_person" {
		t.Fatalf("expected the session to be stored, got %q", token)
	}
}

func TestAuthComplete_RefusesMissingFieldsAndUnusableSessions(t *testing.T) {
	handler, keychain := browserSignInHandler(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no code should be exchanged, got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	})
	login, err := handler.AuthLogin(AuthLoginRequest{RequestID: "req_1", RedirectURI: testRedirectURI})
	if err != nil {
		t.Fatalf("AuthLogin failed: %v", err)
	}
	session, state := login.Instructions.Session, login.Instructions.State

	for _, tc := range []struct {
		name string
		req  AuthCompleteRequest
		want string
	}{
		{"no session", AuthCompleteRequest{Code: "code_1", State: state}, "auth.complete needs"},
		{"no code", AuthCompleteRequest{Session: session, State: state}, "auth.complete needs"},
		{"no state", AuthCompleteRequest{Session: session, Code: "code_1"}, "auth.complete needs"},
		{"malformed session", AuthCompleteRequest{Session: "not a session", Code: "code_1", State: state}, "run 'epack remote login' again"},
		{"another sign-in's state", AuthCompleteRequest{Session: session, Code: "code_1", State: "state_other"}, "does not match"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := handler.AuthComplete(tc.req)
			var remoteErr componentsdk.RemoteError
			if !errors.As(err, &remoteErr) || remoteErr.Code != "invalid_request" || !strings.Contains(remoteErr.Message, tc.want) {
				t.Fatalf("expected invalid_request saying %q, got %v", tc.want, err)
			}
		})
	}
	if token, _ := keychain.GetToken(); token != "" {
		t.Fatalf("nothing should be stored, got %q", token)
	}
}

func TestAuthComplete_ReportsTheServersErrorDescription(t *testing.T) {
	handler, _ := browserSignInHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"The authorization code has expired."}`))
	})
	login, err := handler.AuthLogin(AuthLoginRequest{RequestID: "req_1", RedirectURI: testRedirectURI})
	if err != nil {
		t.Fatalf("AuthLogin failed: %v", err)
	}

	_, err = handler.AuthComplete(AuthCompleteRequest{
		RequestID: "req_2",
		Session:   login.Instructions.Session,
		Code:      "code_1",
		State:     login.Instructions.State,
	})
	var remoteErr componentsdk.RemoteError
	if !errors.As(err, &remoteErr) || remoteErr.Code != "auth_required" || remoteErr.Retryable {
		t.Fatalf("expected a non-retryable auth_required, got %#v", err)
	}
	if !strings.Contains(remoteErr.Message, "invalid_grant: The authorization code has expired.") {
		t.Fatalf("expected the server's error_description, got %q", remoteErr.Message)
	}
}

func TestConfigPull_ReturnsFilesAndRevision(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	mockClient.GetPipelineBundleResponse = &locktivity.PipelineBundleResponse{
		ID: "pipe_1", Name: "Northwind production", ConfigName: "northwind-production", Stream: "northwind/production", RunsIn: "My laptop",
		Revision: 2,
		Files:    map[string]string{"northwind/production/epack.yaml": "stream: northwind/production\n"},
		Shas:     map[string]string{"northwind/production/epack.yaml": "abc"},
		Lockfile: "schema_version: 1\n",
	}
	handler := &Handler{client: mockClient}

	resp, err := handler.ConfigPull(ConfigPullRequest{RequestID: "req_1", Config: ConfigPullTarget{Name: " northwind-production "}})
	if err != nil {
		t.Fatalf("ConfigPull failed: %v", err)
	}
	if mockClient.GetPipelineBundleCalls[0] != "northwind-production" {
		t.Fatalf("expected the trimmed name to be requested, got %q", mockClient.GetPipelineBundleCalls[0])
	}
	if resp.Config.ID != "pipe_1" || resp.Config.Name != "northwind-production" || resp.Config.Title != "Northwind production" || resp.Config.Revision != 2 {
		t.Fatalf("unexpected config: %#v", resp.Config)
	}
	if resp.Config.Files["northwind/production/epack.yaml"] == "" || resp.Config.Shas["northwind/production/epack.yaml"] != "abc" || resp.Config.Lockfile == "" {
		t.Fatalf("expected files, shas, and lockfile: %#v", resp.Config)
	}
}

func TestConfigPull_NotFoundExplainsTheName(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	mockClient.GetPipelineBundleError = errors.New("API error [NOT_FOUND]: Resource not found")
	handler := &Handler{client: mockClient}

	_, err := handler.ConfigPull(ConfigPullRequest{RequestID: "req_1", Config: ConfigPullTarget{Name: "nope"}})
	var remoteErr componentsdk.RemoteError
	if !errors.As(err, &remoteErr) || remoteErr.Code != "not_found" {
		t.Fatalf("expected not_found, got %v", err)
	}
	if !strings.Contains(remoteErr.Message, `"nope"`) || !strings.Contains(remoteErr.Message, "epack run <name>") {
		t.Fatalf("expected a message naming the pipeline and the command, got %q", remoteErr.Message)
	}
}

func TestConfigPull_RequiresName(t *testing.T) {
	handler := &Handler{client: locktivity.NewMockClient()}

	_, err := handler.ConfigPull(ConfigPullRequest{RequestID: "req_1"})
	if err == nil || !strings.Contains(err.Error(), "pipeline name") {
		t.Fatalf("expected a name error, got %v", err)
	}
}

func TestKeyRegister_PassesTheKeyThroughAndSaysWhetherItWasNew(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	mockClient.RegisterSigningKeyResponse = &locktivity.SigningKeyResponse{
		ID: "key_1", Name: "Michaels-MacBook-Pro-2", Fingerprint: "9f14", Algorithm: "ecdsa", Status: "usable",
		RegisteredBy: "michael@example.com", ExpiresAt: "2027-10-01T00:00:00Z", Created: true,
	}
	handler := &Handler{client: mockClient}

	resp, err := handler.KeyRegister(KeyRegisterRequest{
		RequestID: "req_1", Config: "northwind-production", PublicKeyPEM: "-----BEGIN PUBLIC KEY-----\nabc\n-----END PUBLIC KEY-----\n",
		Name: "Michaels-MacBook-Pro-2", ExpiresInDays: 365,
	})
	if err != nil {
		t.Fatalf("KeyRegister: %v", err)
	}
	if !resp.Created || resp.Key.ID != "key_1" || resp.Key.Name != "Michaels-MacBook-Pro-2" || resp.Key.ExpiresAt != "2027-10-01T00:00:00Z" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if wire, _ := json.Marshal(resp); resp.PipelineURL != "" || strings.Contains(string(wire), "pipeline_url") {
		t.Fatalf("expected no pipeline_url when the API sends none, got %s", wire)
	}
	if len(mockClient.RegisterSigningKeyCalls) != 1 {
		t.Fatalf("expected one RegisterSigningKey call, got %d", len(mockClient.RegisterSigningKeyCalls))
	}
	call := mockClient.RegisterSigningKeyCalls[0]
	if call.Pipeline != "northwind-production" || call.Request.Name != "Michaels-MacBook-Pro-2" || call.Request.LifetimeDays != 365 {
		t.Fatalf("unexpected call: %+v", call)
	}

	t.Setenv(ProjectRootEnvVar, t.TempDir())
	if _, err := handler.KeyRegister(KeyRegisterRequest{RequestID: "req_2", PublicKeyPEM: "x"}); err == nil {
		t.Fatal("a request without a configuration should be refused")
	}
}

func TestKeyListAndRevoke(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	mockClient.ListSigningKeysResponse = &locktivity.SigningKeysResponse{SigningKeys: []locktivity.SigningKeyResponse{
		{ID: "key_1", Fingerprint: "9f14", Status: "usable"},
		{ID: "key_0", Fingerprint: "0a0a", Status: "revoked", RevokedAt: "2026-09-01T00:00:00Z"},
	}}
	mockClient.RevokeSigningKeyResponse = &locktivity.SigningKeyResponse{ID: "key_1", Fingerprint: "9f14", Status: "revoked"}
	handler := &Handler{client: mockClient}

	list, err := handler.KeyList(KeyListRequest{RequestID: "req_1", Config: "northwind-production"})
	if err != nil {
		t.Fatalf("KeyList: %v", err)
	}
	if len(list.Keys) != 2 || list.Keys[0].ID != "key_1" || list.Keys[1].Status != "revoked" {
		t.Fatalf("unexpected list: %+v", list.Keys)
	}

	revoked, err := handler.KeyRevoke(KeyRevokeRequest{RequestID: "req_2", Config: "northwind-production", ID: "key_1"})
	if err != nil {
		t.Fatalf("KeyRevoke: %v", err)
	}
	if revoked.Key.Status != "revoked" || mockClient.RevokeSigningKeyCalls[0].ID != "key_1" {
		t.Fatalf("unexpected revoke: %+v", revoked)
	}
}

func TestKeyRetire(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	mockClient.RetireSigningKeyResponse = &locktivity.SigningKeyResponse{ID: "key_1", Fingerprint: "9f14", Status: "retired", RetiredAt: "2026-10-07T18:05:00Z"}
	handler := &Handler{client: mockClient}

	retired, err := handler.KeyRetire(KeyRetireRequest{RequestID: "req_1", Config: "northwind-production", ID: "key_1"})
	if err != nil {
		t.Fatalf("KeyRetire: %v", err)
	}
	if retired.Type != "key.retire.result" || retired.Key.Status != "retired" || retired.Key.RetiredAt != "2026-10-07T18:05:00Z" {
		t.Fatalf("unexpected retire: %+v", retired)
	}
	if call := mockClient.RetireSigningKeyCalls[0]; call.Pipeline != "northwind-production" || call.ID != "key_1" {
		t.Fatalf("unexpected call: %+v", call)
	}

	if _, err := handler.KeyRetire(KeyRetireRequest{RequestID: "req_2", Config: "northwind-production"}); err == nil {
		t.Fatal("a request without a key id should be refused")
	}
}

func TestKeyRegister_PassesThePendingApprovalAndMachineThrough(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	mockClient.RegisterSigningKeyResponse = &locktivity.SigningKeyResponse{
		ID: "key_1", Fingerprint: "9f14", Status: "pending", Machine: "Michaels-MacBook-Pro-2.local", Created: true,
		Approval: &locktivity.SigningKeyApproval{
			Code: "WDJB-MJHT", URL: "https://app.locktivity.com/evidence_packs/pipelines/pipe_1/signing_keys/key_1/approval",
			ExpiresAt: "2026-10-07T18:15:00Z", Interval: 5,
		},
		PipelineURL: "https://app.locktivity.com/evidence_packs/pipelines/pipe_1",
	}
	handler := &Handler{client: mockClient}

	resp, err := handler.KeyRegister(KeyRegisterRequest{RequestID: "req_1", Config: "northwind-production", PublicKeyPEM: "pem"})
	if err != nil {
		t.Fatalf("KeyRegister: %v", err)
	}
	if resp.Key.Status != "pending" || resp.Key.Machine != "Michaels-MacBook-Pro-2.local" {
		t.Fatalf("unexpected key: %+v", resp.Key)
	}
	if result, _ := json.Marshal(resp); resp.PipelineURL != "https://app.locktivity.com/evidence_packs/pipelines/pipe_1" ||
		!strings.Contains(string(result), `"created":true,"pipeline_url":"https://app.locktivity.com/evidence_packs/pipelines/pipe_1"`) {
		t.Fatalf("expected the API's pipeline_url next to created, got %s", result)
	}
	want := KeyApproval{
		Code: "WDJB-MJHT", URL: "https://app.locktivity.com/evidence_packs/pipelines/pipe_1/signing_keys/key_1/approval",
		ExpiresAt: "2026-10-07T18:15:00Z", Interval: 5,
	}
	if resp.Key.Approval == nil || *resp.Key.Approval != want {
		t.Fatalf("expected approval %+v, got %+v", want, resp.Key.Approval)
	}

	wire, err := json.Marshal(resp.Key)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		`"machine":"Michaels-MacBook-Pro-2.local"`,
		`"approval":{"code":"WDJB-MJHT","url":"https://app.locktivity.com/evidence_packs/pipelines/pipe_1/signing_keys/key_1/approval","expires_at":"2026-10-07T18:15:00Z","interval":5}`,
	} {
		if !strings.Contains(string(wire), field) {
			t.Fatalf("expected %s in %s", field, wire)
		}
	}
}

func TestKeyList_PassesTheMachineThroughButNeverAnApproval(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	mockClient.ListSigningKeysResponse = &locktivity.SigningKeysResponse{SigningKeys: []locktivity.SigningKeyResponse{
		{ID: "key_3", Fingerprint: "3c3c", Status: "pending", Machine: "Michaels-MacBook-Pro-2.local",
			Approval: &locktivity.SigningKeyApproval{Code: "WDJB-MJHT", URL: "https://app.locktivity.com/approval", ExpiresAt: "2026-10-07T18:15:00Z", Interval: 5}},
		{ID: "key_2", Fingerprint: "2b2b", Status: "lapsed", Machine: "build-box"},
		{ID: "key_1", Fingerprint: "1a1a", Status: "denied"},
	}}
	handler := &Handler{client: mockClient}

	list, err := handler.KeyList(KeyListRequest{RequestID: "req_1", Config: "northwind-production"})
	if err != nil {
		t.Fatalf("KeyList: %v", err)
	}
	want := []SigningKey{
		{ID: "key_3", Fingerprint: "3c3c", Status: "pending", Machine: "Michaels-MacBook-Pro-2.local"},
		{ID: "key_2", Fingerprint: "2b2b", Status: "lapsed", Machine: "build-box"},
		{ID: "key_1", Fingerprint: "1a1a", Status: "denied"},
	}
	if len(list.Keys) != len(want) {
		t.Fatalf("expected %d keys, got %+v", len(want), list.Keys)
	}
	for i := range want {
		if list.Keys[i] != want[i] {
			t.Fatalf("key %d = %+v, want %+v", i, list.Keys[i], want[i])
		}
	}
}

func TestABundleLaidOutByHandNamesItsPipelineThroughTheManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".locktivity"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".locktivity", "manifest.json"), []byte(`{"schema_version":1,"pipeline_id":"pipe_9"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ProjectRootEnvVar, root)

	mockClient := locktivity.NewMockClient()
	mockClient.ListSigningKeysResponse = &locktivity.SigningKeysResponse{}
	mockClient.ReportLockResponse = &locktivity.LockfileReportResponse{Status: "accepted"}
	handler := &Handler{client: mockClient}

	if _, err := handler.KeyList(KeyListRequest{RequestID: "req_1"}); err != nil {
		t.Fatalf("KeyList: %v", err)
	}
	if mockClient.ListSigningKeysCalls[0] != "pipe_9" {
		t.Fatalf("KeyList used %q, want the manifest's pipeline", mockClient.ListSigningKeysCalls[0])
	}

	if _, err := handler.LockReport(LockReportRequest{RequestID: "req_2", LockProvenance: LockProvenance{TriggerKind: "check", Outcome: "failure", ReportedAt: "2026-10-01T12:00:00Z"}}); err != nil {
		t.Fatalf("LockReport: %v", err)
	}
	if got := mockClient.ReportLockCalls[0].PipelineID; got != "pipe_9" {
		t.Fatalf("LockReport pipeline %q, want the manifest's", got)
	}

	if _, err := handler.KeyList(KeyListRequest{RequestID: "req_3", Config: "northwind-production"}); err != nil {
		t.Fatalf("KeyList: %v", err)
	}
	if mockClient.ListSigningKeysCalls[1] != "northwind-production" {
		t.Fatal("a named configuration must win over the manifest")
	}
}
