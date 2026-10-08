package main

import (
	"encoding/json"
	"errors"
	"reflect"
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

func TestProcessRequest_LockReportSuccess(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	handler := remote.NewHandlerWithClient(mockClient, nil)

	resp := processRequest([]byte(`{
		"type":"lock.report",
		"request_id":"req_1",
		"protocol_version":1,
		"lock_provenance":{
			"lockfile":"schema_version: 1\n",
			"lockfile_sha256":"lock-sha",
			"trigger_kind":"bootstrap",
			"outcome":"success",
			"runtime_context":{
				"pipeline_id":"pipeline-123",
				"github":{"repository":"acme/evidence","ref":"refs/heads/locktivity/setup"}
			}
		}
	}`), handler)

	if got := resp["ok"]; got != true {
		t.Fatalf("expected ok=true, got %v", got)
	}
	if got := resp["type"]; got != "lock.report.result" {
		t.Fatalf("expected lock.report.result, got %v", got)
	}
	if len(mockClient.ReportLockCalls) != 1 {
		t.Fatalf("expected one ReportLock call, got %d", len(mockClient.ReportLockCalls))
	}
}

func TestProcessRequest_CredentialsResolveReturnsTheEnv(t *testing.T) {
	mockClient := locktivity.NewMockClient()
	mockClient.ResolveCredentialSetsResponse = &locktivity.ResolvedCredentials{Env: map[string]string{"LOCKTIVITY_DOCUMENTS_TOKEN": "tok_docs"}}
	handler := remote.NewHandlerWithClient(mockClient, nil)

	resp := processRequest([]byte(`{"type":"credentials.resolve","request_id":"req_1","protocol_version":1,"config":"pipe_1","credential_sets":["credset_docs"]}`), handler)

	if got := resp["ok"]; got != true {
		t.Fatalf("expected ok=true, got %v", resp)
	}
	if got := resp["type"]; got != "credentials.resolve.result" {
		t.Fatalf("expected credentials.resolve.result, got %v", got)
	}
	if env, _ := resp["env"].(map[string]any); env["LOCKTIVITY_DOCUMENTS_TOKEN"] != "tok_docs" {
		t.Fatalf("unexpected env: %v", resp["env"])
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
		{
			name:    "lock.report",
			payload: `{"type":"lock.report","request_id":"req_1","protocol_version":1,"lock_provenance":"oops"}`,
			wantMsg: "failed to parse lock.report request",
		},
		{
			name:    "auth.login",
			payload: `{"type":"auth.login","request_id":"req_1","protocol_version":1,"redirect_uri":123}`,
			wantMsg: "failed to parse auth.login request",
		},
		{
			name:    "auth.complete",
			payload: `{"type":"auth.complete","request_id":"req_1","protocol_version":1,"code":123}`,
			wantMsg: "failed to parse auth.complete request",
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

func TestBuildCapabilities_DefaultAutoMode(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, "")

	caps, err := buildCapabilities()
	if err != nil {
		t.Fatalf("buildCapabilities returned error: %v", err)
	}

	auth := caps["auth"].(map[string]any)
	modes := auth["modes"].([]string)
	want := []string{"access_token", "browser", "client_credentials"}
	if !reflect.DeepEqual(modes, want) {
		t.Fatalf("unexpected auth modes: %#v", modes)
	}

	if caps["files_dir"] != ".locktivity" {
		t.Fatalf("the adapter must declare its bookkeeping folder, got %v", caps["files_dir"])
	}

	features := caps["features"].(map[string]bool)
	for _, feature := range []string{"auth_login", "auth_browser", "config_pull", "whoami", "keys", "credentials_resolve"} {
		if !features[feature] {
			t.Fatalf("expected %s=true", feature)
		}
	}
	if _, ok := features["auth_wait"]; ok {
		t.Fatal("auth_wait must not be advertised")
	}
}

func TestProcessRequest_DispatchesBrowserSignInAndConfigPull(t *testing.T) {
	handler := fakeRequestHandler{
		authLogin: func(req remote.AuthLoginRequest) (*remote.AuthLoginResponse, error) {
			if req.RedirectURI != "http://127.0.0.1:53682/callback" {
				t.Fatalf("unexpected redirect_uri: %q", req.RedirectURI)
			}
			return &remote.AuthLoginResponse{OK: true, Type: "auth.login.result", RequestID: req.RequestID,
				Instructions: remote.AuthLoginInstructions{AuthorizationURL: "https://app.locktivity.com/epack/oauth2/authorize?state=state_1", State: "state_1", Session: "session_1", ExpiresInSecs: 600}}, nil
		},
		authComplete: func(req remote.AuthCompleteRequest) (*remote.AuthCompleteResponse, error) {
			if req.Session != "session_1" || req.Code != "code_1" || req.State != "state_1" {
				t.Fatalf("unexpected auth.complete request: %#v", req)
			}
			return &remote.AuthCompleteResponse{OK: true, Type: "auth.complete.result", RequestID: req.RequestID,
				Identity: remote.IdentityResult{Authenticated: true, Subject: "dana@northwind.com"}}, nil
		},
		configPull: func(req remote.ConfigPullRequest) (*remote.ConfigPullResponse, error) {
			if req.Config.Name != "northwind-production" {
				t.Fatalf("unexpected config name: %q", req.Config.Name)
			}
			return &remote.ConfigPullResponse{OK: true, Type: "config.pull.result", RequestID: req.RequestID,
				Config: remote.ConfigPullResult{Name: req.Config.Name, Revision: 3, Files: map[string]string{"a/epack.yaml": "stream: a\n"}}}, nil
		},
	}

	login := processRequest([]byte(`{"type":"auth.login","request_id":"req_1","protocol_version":1,"redirect_uri":"http://127.0.0.1:53682/callback"}`), handler)
	if login["ok"] != true || login["type"] != "auth.login.result" || login["request_id"] != "req_1" {
		t.Fatalf("unexpected auth.login response: %#v", login)
	}
	if instructions := login["instructions"].(remote.AuthLoginInstructions); instructions.State != "state_1" || instructions.Session != "session_1" {
		t.Fatalf("unexpected auth.login instructions: %#v", instructions)
	}

	complete := processRequest([]byte(`{"type":"auth.complete","request_id":"req_2","protocol_version":1,"session":"session_1","code":"code_1","state":"state_1"}`), handler)
	if complete["ok"] != true || complete["type"] != "auth.complete.result" || complete["request_id"] != "req_2" {
		t.Fatalf("unexpected auth.complete response: %#v", complete)
	}
	if identity := complete["identity"].(remote.IdentityResult); !identity.Authenticated || identity.Subject != "dana@northwind.com" {
		t.Fatalf("unexpected auth.complete identity: %#v", identity)
	}

	pull := processRequest([]byte(`{"type":"config.pull","request_id":"req_3","protocol_version":1,"config":{"name":"northwind-production"}}`), handler)
	if pull["type"] != "config.pull.result" {
		t.Fatalf("unexpected config.pull response: %#v", pull)
	}
	cfg := pull["config"].(remote.ConfigPullResult)
	if cfg.Revision != 3 || cfg.Files["a/epack.yaml"] == "" {
		t.Fatalf("unexpected config payload: %#v", cfg)
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
	want := []string{"access_token", "client_credentials"}
	if !reflect.DeepEqual(modes, want) {
		t.Fatalf("unexpected auth modes: %#v", modes)
	}

	features := caps["features"].(map[string]bool)
	if features["auth_login"] || features["auth_browser"] {
		t.Fatal("expected auth_login=false and auth_browser=false")
	}
	if features["credentials_resolve"] {
		t.Fatal("the broker resolves credentials only for a person's sign-in")
	}
	if _, ok := features["auth_wait"]; ok {
		t.Fatal("auth_wait must not be advertised")
	}
	if !features["config_pull"] {
		t.Fatal("expected config_pull=true")
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
	want := []string{"access_token", "browser", "client_credentials"}
	if !reflect.DeepEqual(modes, want) {
		t.Fatalf("unexpected auth modes: %#v", modes)
	}

	features := caps["features"].(map[string]bool)
	if !features["auth_login"] || !features["auth_browser"] {
		t.Fatal("expected auth_login=true and auth_browser=true")
	}
	if !features["lock_report"] {
		t.Fatal("expected lock_report=true")
	}
}

func TestBuildCapabilities_InvalidAuthMode(t *testing.T) {
	t.Setenv(locktivity.EnvAuthMode, "bad_mode")

	if _, err := buildCapabilities(); err == nil {
		t.Fatal("expected error")
	}
}

type fakeRequestHandler struct {
	pushPrepare  func(req remote.PushPrepareRequest) (*componentsdk.PushPrepareResponse, error)
	pushFinalize func(req remote.PushFinalizeRequest) (*componentsdk.PushFinalizeResponse, error)
	pullPrepare  func(req componentsdk.PullPrepareRequest) (*componentsdk.PullPrepareResponse, error)
	pullFinalize func(req componentsdk.PullFinalizeRequest) (*componentsdk.PullFinalizeResponse, error)
	lockReport   func(req remote.LockReportRequest) (*remote.LockReportResponse, error)
	runsSync     func(req remote.RunsSyncRequest) (*remote.RunsSyncResponse, error)
	authLogin    func(req remote.AuthLoginRequest) (*remote.AuthLoginResponse, error)
	authComplete func(req remote.AuthCompleteRequest) (*remote.AuthCompleteResponse, error)
	authWhoami   func(req remote.AuthWhoamiRequest) (*remote.AuthWhoamiResponse, error)
	configPull   func(req remote.ConfigPullRequest) (*remote.ConfigPullResponse, error)
}

func (f fakeRequestHandler) AuthComplete(req remote.AuthCompleteRequest) (*remote.AuthCompleteResponse, error) {
	if f.authComplete != nil {
		return f.authComplete(req)
	}
	return &remote.AuthCompleteResponse{}, nil
}

func (f fakeRequestHandler) KeyRegister(req remote.KeyRegisterRequest) (*remote.KeyRegisterResponse, error) {
	return nil, componentsdk.ErrServerError("not implemented")
}

func (f fakeRequestHandler) KeyList(req remote.KeyListRequest) (*remote.KeyListResponse, error) {
	return nil, componentsdk.ErrServerError("not implemented")
}

func (f fakeRequestHandler) CredentialsResolve(req remote.CredentialsResolveRequest) (*remote.CredentialsResolveResponse, error) {
	return nil, componentsdk.ErrServerError("not implemented")
}

func (f fakeRequestHandler) KeyRevoke(req remote.KeyRevokeRequest) (*remote.KeyRevokeResponse, error) {
	return nil, componentsdk.ErrServerError("not implemented")
}

func (f fakeRequestHandler) KeyRetire(req remote.KeyRetireRequest) (*remote.KeyRetireResponse, error) {
	return nil, componentsdk.ErrServerError("not implemented")
}

func (f fakeRequestHandler) ConfigPull(req remote.ConfigPullRequest) (*remote.ConfigPullResponse, error) {
	if f.configPull != nil {
		return f.configPull(req)
	}
	return &remote.ConfigPullResponse{}, nil
}

func (f fakeRequestHandler) PushPrepare(req remote.PushPrepareRequest) (*componentsdk.PushPrepareResponse, error) {
	if f.pushPrepare != nil {
		return f.pushPrepare(req)
	}
	return &componentsdk.PushPrepareResponse{}, nil
}

func (f fakeRequestHandler) PushFinalize(req remote.PushFinalizeRequest) (*componentsdk.PushFinalizeResponse, error) {
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

func (f fakeRequestHandler) LockReport(req remote.LockReportRequest) (*remote.LockReportResponse, error) {
	if f.lockReport != nil {
		return f.lockReport(req)
	}
	return &remote.LockReportResponse{}, nil
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
