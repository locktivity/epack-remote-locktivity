package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/locktivity/epack-remote-locktivity/internal/auth"
	"github.com/locktivity/epack-remote-locktivity/internal/remote"
	"github.com/locktivity/epack/componentsdk"
)

const baseVersion = "1.0.0"

var version = baseVersion + versionSuffix

type requestHandler interface {
	PushPrepare(req componentsdk.PushPrepareRequest) (*componentsdk.PushPrepareResponse, error)
	PushFinalize(req componentsdk.PushFinalizeRequest) (*componentsdk.PushFinalizeResponse, error)
	PullPrepare(req componentsdk.PullPrepareRequest) (*componentsdk.PullPrepareResponse, error)
	PullFinalize(req componentsdk.PullFinalizeRequest) (*componentsdk.PullFinalizeResponse, error)
	RunsSync(req remote.RunsSyncRequest) (*remote.RunsSyncResponse, error)
	AuthLogin(req remote.AuthLoginRequest) (*remote.AuthLoginResponse, error)
	AuthWhoami(req remote.AuthWhoamiRequest) (*remote.AuthWhoamiResponse, error)
}

func main() {
	os.Exit(run())
}

func run() int {
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--capabilities":
			return outputCapabilities()
		case "--version":
			fmt.Println(version)
			return 0
		}
	}

	// Create handler
	handler := remote.NewHandler()

	// Process requests from stdin
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		response := processRequest(line, handler)

		enc := json.NewEncoder(os.Stdout)
		if err := enc.Encode(response); err != nil {
			fmt.Fprintf(os.Stderr, "error encoding response: %v\n", err)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "error reading stdin: %v\n", err)
		return 1
	}

	return 0
}

func outputCapabilities() int {
	caps, err := buildCapabilities()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error building capabilities: %v\n", err)
		return 1
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(caps); err != nil {
		fmt.Fprintf(os.Stderr, "error encoding capabilities: %v\n", err)
		return 1
	}
	return 0
}

func buildCapabilities() (map[string]any, error) {
	authMode, err := auth.EffectiveAuthMode()
	if err != nil {
		return nil, err
	}

	authLoginEnabled := true
	authModes := []string{"device_code", "oidc_token", "client_credentials"}
	if authMode == auth.AuthModeClientCredentialsOnly {
		authLoginEnabled = false
		authModes = []string{"client_credentials"}
	}

	return map[string]any{
		"name":                    "locktivity",
		"kind":                    "remote_adapter",
		"deploy_protocol_version": 1,
		"version":                 version,
		"description":             "Locktivity registry remote adapter for epack push/pull operations",
		"features": map[string]bool{
			"prepare_finalize": true,
			"pull":             true,
			"runs_sync":        true,
			"auth_login":       authLoginEnabled,
			"whoami":           true,
		},
		"auth": map[string]any{
			"modes": authModes,
		},
		"limits": map[string]any{
			"max_pack_bytes": 100 * 1024 * 1024,
		},
	}, nil
}

func processRequest(data []byte, handler requestHandler) map[string]any {
	base, err := parseBaseRequest(data)
	if err != nil {
		return errorResponse("", "invalid_request", "failed to parse request JSON")
	}
	if err := validateProtocolVersion(base.ProtocolVersion); err != nil {
		return errorResponse(base.RequestID, "unsupported_protocol", err.Error())
	}
	return dispatchRequest(base, data, handler)
}

type baseRequest struct {
	Type            string `json:"type"`
	RequestID       string `json:"request_id"`
	ProtocolVersion int    `json:"protocol_version,omitempty"`
}

func parseBaseRequest(data []byte) (baseRequest, error) {
	var base baseRequest
	if err := json.Unmarshal(data, &base); err != nil {
		return baseRequest{}, err
	}
	return base, nil
}

func validateProtocolVersion(protocolVersion int) error {
	if protocolVersion != 1 {
		return fmt.Errorf("unsupported protocol_version: %d", protocolVersion)
	}
	return nil
}

func dispatchRequest(base baseRequest, data []byte, handler requestHandler) map[string]any {
	dispatch := map[string]func(string, []byte, requestHandler) map[string]any{
		"push.prepare":  handlePushPrepare,
		"push.finalize": handlePushFinalize,
		"pull.prepare":  handlePullPrepare,
		"pull.finalize": handlePullFinalize,
		"runs.sync":     handleRunsSync,
		"auth.login":    handleAuthLogin,
		"auth.whoami":   handleAuthWhoami,
	}
	fn, ok := dispatch[base.Type]
	if !ok {
		return errorResponse(base.RequestID, "unsupported_protocol", fmt.Sprintf("unknown request type: %s", base.Type))
	}
	return fn(base.RequestID, data, handler)
}

func handlePushPrepare(requestID string, data []byte, handler requestHandler) map[string]any {
	var req componentsdk.PushPrepareRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return errorResponse(requestID, "invalid_request", "failed to parse push.prepare request")
	}
	resp, err := handler.PushPrepare(req)
	if err != nil {
		return remoteErrorResponse(requestID, err)
	}
	return successResponse(requestID, "push.prepare.result", resp)
}

func handlePushFinalize(requestID string, data []byte, handler requestHandler) map[string]any {
	var req componentsdk.PushFinalizeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return errorResponse(requestID, "invalid_request", "failed to parse push.finalize request")
	}
	resp, err := handler.PushFinalize(req)
	if err != nil {
		return remoteErrorResponse(requestID, err)
	}
	return successResponse(requestID, "push.finalize.result", resp)
}

func handlePullPrepare(requestID string, data []byte, handler requestHandler) map[string]any {
	var req componentsdk.PullPrepareRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return errorResponse(requestID, "invalid_request", "failed to parse pull.prepare request")
	}
	resp, err := handler.PullPrepare(req)
	if err != nil {
		return remoteErrorResponse(requestID, err)
	}
	return successResponse(requestID, "pull.prepare.result", resp)
}

func handlePullFinalize(requestID string, data []byte, handler requestHandler) map[string]any {
	var req componentsdk.PullFinalizeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return errorResponse(requestID, "invalid_request", "failed to parse pull.finalize request")
	}
	resp, err := handler.PullFinalize(req)
	if err != nil {
		return remoteErrorResponse(requestID, err)
	}
	return successResponse(requestID, "pull.finalize.result", resp)
}

func handleRunsSync(requestID string, data []byte, handler requestHandler) map[string]any {
	var req remote.RunsSyncRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return errorResponse(requestID, "invalid_request", "failed to parse runs.sync request")
	}
	resp, err := handler.RunsSync(req)
	if err != nil {
		return errorResponse(requestID, "server_error", err.Error())
	}
	return map[string]any{
		"ok":         resp.OK,
		"type":       resp.Type,
		"request_id": resp.RequestID,
		"accepted":   resp.Accepted,
		"rejected":   resp.Rejected,
		"items":      resp.Items,
	}
}

func handleAuthLogin(requestID string, data []byte, handler requestHandler) map[string]any {
	var req remote.AuthLoginRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return errorResponse(requestID, "invalid_request", "failed to parse auth.login request")
	}
	resp, err := handler.AuthLogin(req)
	if err != nil {
		return remoteErrorResponse(requestID, err)
	}
	return map[string]any{
		"ok":           resp.OK,
		"type":         resp.Type,
		"request_id":   resp.RequestID,
		"instructions": resp.Instructions,
	}
}

func handleAuthWhoami(requestID string, data []byte, handler requestHandler) map[string]any {
	var req remote.AuthWhoamiRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return errorResponse(requestID, "invalid_request", "failed to parse auth.whoami request")
	}
	resp, err := handler.AuthWhoami(req)
	if err != nil {
		return errorResponse(requestID, "server_error", err.Error())
	}
	return map[string]any{
		"ok":         resp.OK,
		"type":       resp.Type,
		"request_id": resp.RequestID,
		"identity":   resp.Identity,
	}
}

func successResponse(requestID, responseType string, data any) map[string]any {
	result := map[string]any{
		"type":       responseType,
		"ok":         true,
		"request_id": requestID,
	}

	// Merge in the response fields
	if data != nil {
		dataBytes, _ := json.Marshal(data)
		var dataMap map[string]any
		_ = json.Unmarshal(dataBytes, &dataMap)
		for k, v := range dataMap {
			result[k] = v
		}
	}

	return result
}

func errorResponse(requestID, code, message string) map[string]any {
	return map[string]any{
		"type":       "error",
		"ok":         false,
		"request_id": requestID,
		"error": map[string]any{
			"code":      code,
			"message":   message,
			"retryable": false,
		},
	}
}

func remoteErrorResponse(requestID string, err error) map[string]any {
	if re, ok := err.(componentsdk.RemoteError); ok {
		return map[string]any{
			"type":       "error",
			"ok":         false,
			"request_id": requestID,
			"error": map[string]any{
				"code":      re.Code,
				"message":   re.Message,
				"retryable": re.Retryable,
			},
		}
	}
	return errorResponse(requestID, "server_error", err.Error())
}
