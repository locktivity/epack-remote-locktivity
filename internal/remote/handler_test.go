package remote

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/locktivity/epack-remote-locktivity/internal/auth"
	"github.com/locktivity/epack-remote-locktivity/internal/locktivity"
	"github.com/locktivity/epack/componentsdk"
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

	req := componentsdk.PushPrepareRequest{
		RequestID: "req_123",
		Target: componentsdk.RemoteTarget{
			Environment: "prod",
		},
		Pack: componentsdk.PackInfo{
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

	req := componentsdk.PushFinalizeRequest{
		RequestID:     "req_123",
		FinalizeToken: token,
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

	req := componentsdk.PushPrepareRequest{
		RequestID: "req_123",
		Target: componentsdk.RemoteTarget{
			Environment: "prod",
		},
		Pack: componentsdk.PackInfo{
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

	_, err = handler.PushFinalize(componentsdk.PushFinalizeRequest{
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

	req := componentsdk.PushFinalizeRequest{
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
