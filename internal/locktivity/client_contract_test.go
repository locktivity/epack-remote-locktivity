package locktivity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/locktivity/epack-remote-locktivity/internal/contracttest"
)

func TestCreatePack_UsesPackEnvelope(t *testing.T) {
	server := contracttest.NewServer(t, contracttest.Step{
		Method: http.MethodPost,
		Path:   APIPathPrefix + "/packs",
		Status: http.StatusCreated,
		Check: func(t *testing.T, r *http.Request, body []byte) {
			t.Helper()

			var payload map[string]map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("failed to decode request body: %v", err)
			}

			pack, ok := payload["pack"]
			if !ok {
				t.Fatalf("expected top-level pack envelope, got %v", payload)
			}

			if pack["file_digest"] != "sha256:file" {
				t.Fatalf("expected file_digest sha256:file, got %v", pack["file_digest"])
			}
			if pack["size_bytes"] != float64(123) {
				t.Fatalf("expected size_bytes 123, got %v", pack["size_bytes"])
			}
		},
		JSONBody: CreatePackResponse{
			ID:         "pack_123",
			FileDigest: "sha256:file",
			SizeBytes:  123,
		},
	})

	client := NewClientWithHTTP(server.Client(), server.URL())
	resp, err := client.CreatePack(context.Background(), CreatePackRequest{
		FileDigest: "sha256:file",
		SizeBytes:  123,
	})
	if err != nil {
		t.Fatalf("CreatePack failed: %v", err)
	}
	if resp.ID != "pack_123" {
		t.Fatalf("expected pack ID pack_123, got %q", resp.ID)
	}
}

func TestReleaseLookups_UseSupportedQueryParams(t *testing.T) {
	tests := []struct {
		name  string
		query url.Values
		call  func(client *APIClient) (*ReleaseResponse, error)
	}{
		{
			name:  "latest",
			query: url.Values{"environment": {"prod"}},
			call: func(client *APIClient) (*ReleaseResponse, error) {
				return client.GetLatestRelease(context.Background(), "prod")
			},
		},
		{
			name:  "version",
			query: url.Values{"environment": {"prod"}, "version": {"1.2.3"}},
			call: func(client *APIClient) (*ReleaseResponse, error) {
				return client.GetReleaseByVersion(context.Background(), "1.2.3", "prod")
			},
		},
		{
			name:  "digest",
			query: url.Values{"environment": {"prod"}, "pack_digest": {"sha256:pack"}},
			call: func(client *APIClient) (*ReleaseResponse, error) {
				return client.GetReleaseByDigest(context.Background(), "sha256:pack", "prod")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := contracttest.NewServer(t, contracttest.Step{
				Method:   http.MethodGet,
				Path:     APIPathPrefix + "/releases/latest",
				Query:    tc.query,
				Status:   http.StatusOK,
				JSONBody: ReleaseResponse{ID: "rel_123", Pack: &PackInfo{ID: "pack_123"}},
			})

			client := NewClientWithHTTP(server.Client(), server.URL())
			resp, err := tc.call(client)
			if err != nil {
				t.Fatalf("release lookup failed: %v", err)
			}
			if resp.ID != "rel_123" {
				t.Fatalf("expected release ID rel_123, got %q", resp.ID)
			}
		})
	}
}

func TestCreateRelease_ReturnsErrAccepted(t *testing.T) {
	server := contracttest.NewServer(t, contracttest.Step{
		Method: http.MethodPost,
		Path:   APIPathPrefix + "/releases",
		Status: http.StatusAccepted,
		Check: func(t *testing.T, r *http.Request, body []byte) {
			t.Helper()

			var payload map[string]map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("failed to decode request body: %v", err)
			}

			release, ok := payload["release"]
			if !ok {
				t.Fatalf("expected top-level release envelope, got %v", payload)
			}
			if release["finalize_token"] != "fin_123" {
				t.Fatalf("expected finalize_token fin_123, got %v", release["finalize_token"])
			}
		},
		JSONBody: APIError{Errors: []string{"processing"}},
	})

	client := NewClientWithHTTP(server.Client(), server.URL())
	_, err := client.CreateRelease(context.Background(), CreateReleaseRequest{FinalizeToken: "fin_123"})
	var acceptedErr ErrAccepted
	if !errors.As(err, &acceptedErr) {
		t.Fatalf("expected ErrAccepted, got %v", err)
	}
	if acceptedErr.Message != "processing" {
		t.Fatalf("expected accepted message processing, got %q", acceptedErr.Message)
	}
}

func TestCreateRelease_ReturnsErrRateLimited(t *testing.T) {
	server := contracttest.NewServer(t, contracttest.Step{
		Method: http.MethodPost,
		Path:   APIPathPrefix + "/releases",
		Status: http.StatusTooManyRequests,
		Headers: map[string]string{
			"Retry-After":           "7",
			"X-RateLimit-Limit":     "50",
			"X-RateLimit-Remaining": "0",
			"X-Request-Id":          "req-123",
		},
		JSONBody: APIError{Errors: []string{"too many requests"}},
	})

	client := NewClientWithHTTP(server.Client(), server.URL())
	_, err := client.CreateRelease(context.Background(), CreateReleaseRequest{FinalizeToken: "fin_123"})
	var rateLimitErr ErrRateLimited
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
	if rateLimitErr.RetryAfter != "7" {
		t.Fatalf("expected Retry-After 7, got %q", rateLimitErr.RetryAfter)
	}
	if rateLimitErr.Method != http.MethodPost {
		t.Fatalf("expected method POST, got %q", rateLimitErr.Method)
	}
	if rateLimitErr.Endpoint != APIPathPrefix+"/releases" {
		t.Fatalf("expected endpoint %q, got %q", APIPathPrefix+"/releases", rateLimitErr.Endpoint)
	}
	if rateLimitErr.Limit != "50" {
		t.Fatalf("expected limit 50, got %q", rateLimitErr.Limit)
	}
	if rateLimitErr.Remaining != "0" {
		t.Fatalf("expected remaining 0, got %q", rateLimitErr.Remaining)
	}
	if rateLimitErr.RequestID != "req-123" {
		t.Fatalf("expected request ID req-123, got %q", rateLimitErr.RequestID)
	}
	if !strings.Contains(rateLimitErr.Error(), "POST "+APIPathPrefix+"/releases") {
		t.Fatalf("expected error to include request path, got %q", rateLimitErr.Error())
	}
}

func TestCreatePack_ReturnsDecodeErrorOnMalformedJSON(t *testing.T) {
	server := contracttest.NewServer(t, contracttest.Step{
		Method: http.MethodPost,
		Path:   APIPathPrefix + "/packs",
		Status: http.StatusCreated,
		Body:   "{",
	})

	client := NewClientWithHTTP(server.Client(), server.URL())
	_, err := client.CreatePack(context.Background(), CreatePackRequest{
		FileDigest: "sha256:file",
		SizeBytes:  123,
	})
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to decode response") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestGetPack_UsesPackPath(t *testing.T) {
	server := contracttest.NewServer(t, contracttest.Step{
		Method: http.MethodGet,
		Path:   APIPathPrefix + "/packs/pack_123",
		Status: http.StatusOK,
		JSONBody: GetPackResponse{
			ID:         "pack_123",
			FileDigest: "sha256:file",
			PackDigest: "sha256:pack",
			SizeBytes:  123,
			Download:   &DownloadInfo{URL: "https://storage.example.com/download"},
		},
	})

	client := NewClientWithHTTP(server.Client(), server.URL())
	resp, err := client.GetPack(context.Background(), "pack_123")
	if err != nil {
		t.Fatalf("GetPack failed: %v", err)
	}
	if resp.Download == nil || resp.Download.URL != "https://storage.example.com/download" {
		t.Fatalf("expected download URL, got %#v", resp.Download)
	}
}

func TestCreateFinalizeIntent_SendsPackID(t *testing.T) {
	server := contracttest.NewServer(t, contracttest.Step{
		Method: http.MethodPost,
		Path:   APIPathPrefix + "/finalize_intents",
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
		JSONBody: CreateFinalizeIntentResponse{
			FinalizeToken: "fin_123",
			PackID:        "pack_123",
		},
	})

	client := NewClientWithHTTP(server.Client(), server.URL())
	resp, err := client.CreateFinalizeIntent(context.Background(), CreateFinalizeIntentRequest{PackID: "pack_123"})
	if err != nil {
		t.Fatalf("CreateFinalizeIntent failed: %v", err)
	}
	if resp.FinalizeToken != "fin_123" {
		t.Fatalf("expected finalize token fin_123, got %q", resp.FinalizeToken)
	}
}

func TestConsumeFinalizeIntent_UsesPatchPath(t *testing.T) {
	server := contracttest.NewServer(t, contracttest.Step{
		Method:   http.MethodPatch,
		Path:     APIPathPrefix + "/finalize_intents/fin_123",
		Status:   http.StatusOK,
		JSONBody: ConsumeFinalizeIntentResponse{},
	})

	client := NewClientWithHTTP(server.Client(), server.URL())
	if _, err := client.ConsumeFinalizeIntent(context.Background(), ConsumeFinalizeIntentRequest{FinalizeIntentID: "fin_123"}); err != nil {
		t.Fatalf("ConsumeFinalizeIntent failed: %v", err)
	}
}

func TestSyncRuns_SendsRunMetadata(t *testing.T) {
	server := contracttest.NewServer(t, contracttest.Step{
		Method: http.MethodPost,
		Path:   APIPathPrefix + "/pack_runs",
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
		},
		JSONBody: PackRunsResponse{Accepted: 1, Rejected: 0},
	})

	client := NewClientWithHTTP(server.Client(), server.URL())
	resp, err := client.SyncRuns(context.Background(), CreatePackRunsRequest{
		FileDigest: "sha256:file",
		Runs: []RunInfo{
			{RunID: "run_123", ResultPath: "/tmp/result.json", ResultDigest: "sha256:result"},
		},
	})
	if err != nil {
		t.Fatalf("SyncRuns failed: %v", err)
	}
	if resp.Accepted != 1 {
		t.Fatalf("expected accepted 1, got %d", resp.Accepted)
	}
}

func TestConfirmRunOutputUpload_UsesPatchPathAndBody(t *testing.T) {
	server := contracttest.NewServer(t, contracttest.Step{
		Method: http.MethodPatch,
		Path:   APIPathPrefix + "/pack_run_outputs/output_123",
		Status: http.StatusOK,
		Check: func(t *testing.T, r *http.Request, body []byte) {
			t.Helper()

			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("failed to decode request body: %v", err)
			}
			if payload["upload_token"] != "upload_123" {
				t.Fatalf("expected upload_token upload_123, got %v", payload["upload_token"])
			}
		},
		JSONBody: ConfirmRunOutputUploadResponse{
			ID:         "output_123",
			Path:       "output.txt",
			ScanStatus: "clean",
		},
	})

	client := NewClientWithHTTP(server.Client(), server.URL())
	resp, err := client.ConfirmRunOutputUpload(context.Background(), "output_123", ConfirmRunOutputUploadRequest{
		UploadToken: "upload_123",
	})
	if err != nil {
		t.Fatalf("ConfirmRunOutputUpload failed: %v", err)
	}
	if resp.ScanStatus != "clean" {
		t.Fatalf("expected scan status clean, got %q", resp.ScanStatus)
	}
}

func TestGetIdentity_ReturnsIdentity(t *testing.T) {
	server := contracttest.NewServer(t, contracttest.Step{
		Method: http.MethodGet,
		Path:   APIPathPrefix + "/identity",
		Status: http.StatusOK,
		JSONBody: IdentityResponse{
			Authenticated: true,
			Subject:       "user@example.com",
		},
	})

	client := NewClientWithHTTP(server.Client(), server.URL())
	resp, err := client.GetIdentity(context.Background())
	if err != nil {
		t.Fatalf("GetIdentity failed: %v", err)
	}
	if !resp.Authenticated || resp.Subject != "user@example.com" {
		t.Fatalf("unexpected identity response: %#v", resp)
	}
}

func TestUploadToPresignedURL_UsesHeadersAndBody(t *testing.T) {
	server := contracttest.NewServer(t, contracttest.Step{
		Method: http.MethodPut,
		Path:   "/upload/output.txt",
		Status: http.StatusOK,
		Check: func(t *testing.T, r *http.Request, body []byte) {
			t.Helper()

			if r.Header.Get("Content-Type") != "text/plain" {
				t.Fatalf("expected Content-Type text/plain, got %q", r.Header.Get("Content-Type"))
			}
			if string(body) != "hello world" {
				t.Fatalf("expected upload body hello world, got %q", string(body))
			}
		},
	})

	client := NewClientWithHTTP(server.Client(), server.URL())
	if err := client.UploadToPresignedURL(
		context.Background(),
		server.URL()+"/upload/output.txt",
		map[string]string{"Content-Type": "text/plain"},
		bytes.NewBufferString("hello world"),
	); err != nil {
		t.Fatalf("UploadToPresignedURL failed: %v", err)
	}
}

func TestUploadToPresignedURL_ReturnsStatusError(t *testing.T) {
	server := contracttest.NewServer(t, contracttest.Step{
		Method: http.MethodPut,
		Path:   "/upload/output.txt",
		Status: http.StatusBadGateway,
		Body:   "upstream unavailable",
	})

	client := NewClientWithHTTP(server.Client(), server.URL())
	err := client.UploadToPresignedURL(
		context.Background(),
		server.URL()+"/upload/output.txt",
		nil,
		bytes.NewBufferString("hello world"),
	)
	if err == nil {
		t.Fatal("expected upload error, got nil")
	}
	if !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("expected status error, got %v", err)
	}
}

func TestHandleErrorResponse_UsesAPICode(t *testing.T) {
	server := contracttest.NewServer(t, contracttest.Step{
		Method: http.MethodGet,
		Path:   APIPathPrefix + "/identity",
		Status: http.StatusForbidden,
		JSONBody: APIError{
			Code:   "PRODUCT_NOT_ENABLED",
			Errors: []string{"not enabled"},
		},
	})

	client := NewClientWithHTTP(server.Client(), server.URL())
	_, err := client.GetIdentity(context.Background())
	if err == nil {
		t.Fatal("expected API error, got nil")
	}
	if !strings.Contains(err.Error(), "API error [PRODUCT_NOT_ENABLED]") {
		t.Fatalf("expected coded API error, got %v", err)
	}
}
