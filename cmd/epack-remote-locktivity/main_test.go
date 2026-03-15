package main

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/locktivity/epack-remote-locktivity/internal/locktivity"
	"github.com/locktivity/epack-remote-locktivity/internal/remote"
	"github.com/locktivity/epack/componentsdk"
)

func TestProcessRequest_InvalidJSON(t *testing.T) {
	handler := remote.NewHandlerWithClient(locktivity.NewMockClient(), nil)

	resp := processRequest([]byte("{"), handler)

	if got := resp["ok"]; got != false {
		t.Fatalf("expected ok=false, got %v", got)
	}
	errMap, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %T", resp["error"])
	}
	if got := errMap["code"]; got != "invalid_request" {
		t.Fatalf("expected invalid_request, got %v", got)
	}
}

func TestProcessRequest_UnsupportedType(t *testing.T) {
	handler := remote.NewHandlerWithClient(locktivity.NewMockClient(), nil)

	resp := processRequest([]byte(`{"type":"nope","request_id":"req_1","protocol_version":1}`), handler)

	if got := resp["ok"]; got != false {
		t.Fatalf("expected ok=false, got %v", got)
	}
	errMap := resp["error"].(map[string]any)
	if got := errMap["code"]; got != "unsupported_protocol" {
		t.Fatalf("expected unsupported_protocol, got %v", got)
	}
}

func TestProcessRequest_UnsupportedProtocolVersion(t *testing.T) {
	handler := remote.NewHandlerWithClient(locktivity.NewMockClient(), nil)

	resp := processRequest([]byte(`{"type":"auth.whoami","request_id":"req_1","protocol_version":2}`), handler)

	if got := resp["ok"]; got != false {
		t.Fatalf("expected ok=false, got %v", got)
	}
	errMap := resp["error"].(map[string]any)
	if got := errMap["code"]; got != "unsupported_protocol" {
		t.Fatalf("expected unsupported_protocol, got %v", got)
	}
}

func TestProcessRequest_RunsSyncSuccess(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	handler := remote.NewHandlerWithClient(mockClient, nil)

	req := map[string]any{
		"type":             "runs.sync",
		"request_id":       "req_123",
		"protocol_version": 1,
		"target": map[string]any{
			"workspace":   "acme",
			"environment": "prod",
		},
		"pack_digest": "sha256:abc123",
		"runs": []map[string]any{
			{
				"run_id":        "run_1",
				"result_path":   "result.json",
				"result_digest": "sha256:def456",
			},
		},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	resp := processRequest(data, handler)

	if got := resp["ok"]; got != true {
		t.Fatalf("expected ok=true, got %v", got)
	}
	if got := resp["type"]; got != "runs.sync.result" {
		t.Fatalf("expected runs.sync.result, got %v", got)
	}
	if got := resp["accepted"]; got != 1 {
		t.Fatalf("expected accepted=1, got %v", got)
	}
	if len(mockClient.SyncRunsCalls) != 1 {
		t.Fatalf("expected one SyncRuns call, got %d", len(mockClient.SyncRunsCalls))
	}
}

func TestProcessRequest_AuthWhoamiSuccess(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	handler := remote.NewHandlerWithClient(mockClient, nil)

	resp := processRequest([]byte(`{"type":"auth.whoami","request_id":"req_1","protocol_version":1}`), handler)

	if got := resp["ok"]; got != true {
		t.Fatalf("expected ok=true, got %v", got)
	}
	if got := resp["type"]; got != "auth.whoami.result" {
		t.Fatalf("expected auth.whoami.result, got %v", got)
	}
	if mockClient.GetIdentityCalls != 1 {
		t.Fatalf("expected one identity call, got %d", mockClient.GetIdentityCalls)
	}
}

func TestProcessRequest_ParseErrorsByOperation(t *testing.T) {
	handler := remote.NewHandlerWithClient(locktivity.NewMockClient(), nil)

	tests := []struct {
		name    string
		payload string
		wantMsg string
	}{
		{
			name:    "push.prepare",
			payload: `{"type":"push.prepare","request_id":"req_1","protocol_version":1,"pack":"oops"}`,
			wantMsg: "failed to parse push.prepare request",
		},
		{
			name:    "push.finalize",
			payload: `{"type":"push.finalize","request_id":"req_1","protocol_version":1,"finalize_token":123}`,
			wantMsg: "failed to parse push.finalize request",
		},
		{
			name:    "pull.prepare",
			payload: `{"type":"pull.prepare","request_id":"req_1","protocol_version":1,"ref":"oops"}`,
			wantMsg: "failed to parse pull.prepare request",
		},
		{
			name:    "pull.finalize",
			payload: `{"type":"pull.finalize","request_id":"req_1","protocol_version":1,"pack_digest":123}`,
			wantMsg: "failed to parse pull.finalize request",
		},
		{
			name:    "runs.sync",
			payload: `{"type":"runs.sync","request_id":"req_1","protocol_version":1,"pack_digest":"sha256:abc","runs":"oops"}`,
			wantMsg: "failed to parse runs.sync request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := processRequest([]byte(tt.payload), handler)

			if got := resp["ok"]; got != false {
				t.Fatalf("expected ok=false, got %v", got)
			}
			errMap, ok := resp["error"].(map[string]any)
			if !ok {
				t.Fatalf("expected error map, got %T", resp["error"])
			}
			if got := errMap["code"]; got != "invalid_request" {
				t.Fatalf("expected invalid_request, got %v", got)
			}
			if got := errMap["message"]; got != tt.wantMsg {
				t.Fatalf("expected message %q, got %v", tt.wantMsg, got)
			}
		})
	}
}

func TestProcessRequest_AuthLoginPayloadFailsBaseParse(t *testing.T) {
	handler := remote.NewHandlerWithClient(locktivity.NewMockClient(), nil)

	resp := processRequest([]byte(`{"type":"auth.login","request_id":123}`), handler)

	if got := resp["ok"]; got != false {
		t.Fatalf("expected ok=false, got %v", got)
	}
	errMap := resp["error"].(map[string]any)
	if got := errMap["code"]; got != "invalid_request" {
		t.Fatalf("expected invalid_request, got %v", got)
	}
	if got := errMap["message"]; got != "failed to parse request JSON" {
		t.Fatalf("unexpected message: %v", got)
	}
}

func TestProcessRequest_AuthLoginServerError(t *testing.T) {
	handler := fakeRequestHandler{
		authLogin: func(req remote.AuthLoginRequest) (*remote.AuthLoginResponse, error) {
			return nil, errors.New("login failed")
		},
	}

	resp := processRequest([]byte(`{"type":"auth.login","request_id":"req_1","protocol_version":1}`), handler)

	if got := resp["ok"]; got != false {
		t.Fatalf("expected ok=false, got %v", got)
	}
	errMap := resp["error"].(map[string]any)
	if got := errMap["code"]; got != "server_error" {
		t.Fatalf("expected server_error, got %v", got)
	}
	if got := errMap["message"]; got != "login failed" {
		t.Fatalf("expected login failed message, got %v", got)
	}
}

func TestProcessRequest_AuthLoginRateLimited(t *testing.T) {
	handler := fakeRequestHandler{
		authLogin: func(req remote.AuthLoginRequest) (*remote.AuthLoginResponse, error) {
			return nil, componentsdk.ErrRateLimited("too many login attempts")
		},
	}

	resp := processRequest([]byte(`{"type":"auth.login","request_id":"req_1","protocol_version":1}`), handler)

	if got := resp["ok"]; got != false {
		t.Fatalf("expected ok=false, got %v", got)
	}
	errMap := resp["error"].(map[string]any)
	if got := errMap["code"]; got != "rate_limited" {
		t.Fatalf("expected rate_limited, got %v", got)
	}
}

func TestSuccessResponse_MergesPayload(t *testing.T) {
	resp := successResponse("req_1", "push.prepare.result", map[string]any{
		"upload": map[string]any{"method": "PUT"},
	})

	if got := resp["ok"]; got != true {
		t.Fatalf("expected ok=true, got %v", got)
	}
	if got := resp["type"]; got != "push.prepare.result" {
		t.Fatalf("expected push.prepare.result, got %v", got)
	}
	if got := resp["request_id"]; got != "req_1" {
		t.Fatalf("expected req_1, got %v", got)
	}
	if _, ok := resp["upload"]; !ok {
		t.Fatal("expected merged upload field")
	}
}

func TestErrorResponse_Shape(t *testing.T) {
	resp := errorResponse("req_1", "invalid_request", "bad input")

	if got := resp["ok"]; got != false {
		t.Fatalf("expected ok=false, got %v", got)
	}
	if got := resp["type"]; got != "error" {
		t.Fatalf("expected error type, got %v", got)
	}
	errMap, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error map, got %T", resp["error"])
	}
	if got := errMap["code"]; got != "invalid_request" {
		t.Fatalf("expected invalid_request code, got %v", got)
	}
	if got := errMap["retryable"]; got != false {
		t.Fatalf("expected retryable=false, got %v", got)
	}
}

func TestRemoteErrorResponse_UsesRemoteErrorFields(t *testing.T) {
	resp := remoteErrorResponse("req_1", componentsdk.RemoteError{
		Code:      "rate_limited",
		Message:   "slow down",
		Retryable: true,
	})

	if got := resp["ok"]; got != false {
		t.Fatalf("expected ok=false, got %v", got)
	}
	errMap := resp["error"].(map[string]any)
	if got := errMap["code"]; got != "rate_limited" {
		t.Fatalf("expected rate_limited, got %v", got)
	}
	if got := errMap["message"]; got != "slow down" {
		t.Fatalf("expected message slow down, got %v", got)
	}
	if got := errMap["retryable"]; got != true {
		t.Fatalf("expected retryable=true, got %v", got)
	}
}

func TestRemoteErrorResponse_GenericErrorFallsBackToServerError(t *testing.T) {
	resp := remoteErrorResponse("req_1", errors.New("boom"))

	if got := resp["ok"]; got != false {
		t.Fatalf("expected ok=false, got %v", got)
	}
	errMap := resp["error"].(map[string]any)
	if got := errMap["code"]; got != "server_error" {
		t.Fatalf("expected server_error, got %v", got)
	}
	if got := errMap["message"]; got != "boom" {
		t.Fatalf("expected message boom, got %v", got)
	}
}

func TestBuildCapabilities_DefaultClientCredentialsOnlyMode(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, "")

	caps, err := buildCapabilities()
	if err != nil {
		t.Fatalf("buildCapabilities returned error: %v", err)
	}

	auth := caps["auth"].(map[string]any)
	modes := auth["modes"].([]string)
	if len(modes) != 1 || modes[0] != "client_credentials" {
		t.Fatalf("unexpected auth modes: %#v", modes)
	}

	features := caps["features"].(map[string]bool)
	if features["auth_login"] {
		t.Fatal("expected auth_login=false")
	}
}

func TestBuildCapabilities_ClientCredentialsOnlyMode(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, "client_credentials_only")

	caps, err := buildCapabilities()
	if err != nil {
		t.Fatalf("buildCapabilities returned error: %v", err)
	}

	auth := caps["auth"].(map[string]any)
	modes := auth["modes"].([]string)
	if len(modes) != 1 || modes[0] != "client_credentials" {
		t.Fatalf("unexpected auth modes: %#v", modes)
	}

	features := caps["features"].(map[string]bool)
	if features["auth_login"] {
		t.Fatal("expected auth_login=false")
	}
}

func TestBuildCapabilities_AllMode(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, "all")

	caps, err := buildCapabilities()
	if err != nil {
		t.Fatalf("buildCapabilities returned error: %v", err)
	}

	auth := caps["auth"].(map[string]any)
	modes := auth["modes"].([]string)
	if len(modes) != 3 {
		t.Fatalf("expected 3 auth modes, got %d", len(modes))
	}

	features := caps["features"].(map[string]bool)
	if !features["auth_login"] {
		t.Fatal("expected auth_login=true")
	}
}

func TestBuildCapabilities_InvalidAuthMode(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, "bad_mode")

	if _, err := buildCapabilities(); err == nil {
		t.Fatal("expected error")
	}
}

type fakeRequestHandler struct {
	pushPrepare  func(req componentsdk.PushPrepareRequest) (*componentsdk.PushPrepareResponse, error)
	pushFinalize func(req componentsdk.PushFinalizeRequest) (*componentsdk.PushFinalizeResponse, error)
	pullPrepare  func(req componentsdk.PullPrepareRequest) (*componentsdk.PullPrepareResponse, error)
	pullFinalize func(req componentsdk.PullFinalizeRequest) (*componentsdk.PullFinalizeResponse, error)
	runsSync     func(req remote.RunsSyncRequest) (*remote.RunsSyncResponse, error)
	authLogin    func(req remote.AuthLoginRequest) (*remote.AuthLoginResponse, error)
	authWhoami   func(req remote.AuthWhoamiRequest) (*remote.AuthWhoamiResponse, error)
}

func (f fakeRequestHandler) PushPrepare(req componentsdk.PushPrepareRequest) (*componentsdk.PushPrepareResponse, error) {
	if f.pushPrepare != nil {
		return f.pushPrepare(req)
	}
	return &componentsdk.PushPrepareResponse{}, nil
}

func (f fakeRequestHandler) PushFinalize(req componentsdk.PushFinalizeRequest) (*componentsdk.PushFinalizeResponse, error) {
	if f.pushFinalize != nil {
		return f.pushFinalize(req)
	}
	return &componentsdk.PushFinalizeResponse{}, nil
}

func (f fakeRequestHandler) PullPrepare(req componentsdk.PullPrepareRequest) (*componentsdk.PullPrepareResponse, error) {
	if f.pullPrepare != nil {
		return f.pullPrepare(req)
	}
	return &componentsdk.PullPrepareResponse{}, nil
}

func (f fakeRequestHandler) PullFinalize(req componentsdk.PullFinalizeRequest) (*componentsdk.PullFinalizeResponse, error) {
	if f.pullFinalize != nil {
		return f.pullFinalize(req)
	}
	return &componentsdk.PullFinalizeResponse{}, nil
}

func (f fakeRequestHandler) RunsSync(req remote.RunsSyncRequest) (*remote.RunsSyncResponse, error) {
	if f.runsSync != nil {
		return f.runsSync(req)
	}
	return &remote.RunsSyncResponse{}, nil
}

func (f fakeRequestHandler) AuthLogin(req remote.AuthLoginRequest) (*remote.AuthLoginResponse, error) {
	if f.authLogin != nil {
		return f.authLogin(req)
	}
	return &remote.AuthLoginResponse{}, nil
}

func (f fakeRequestHandler) AuthWhoami(req remote.AuthWhoamiRequest) (*remote.AuthWhoamiResponse, error) {
	if f.authWhoami != nil {
		return f.authWhoami(req)
	}
	return &remote.AuthWhoamiResponse{}, nil
}
