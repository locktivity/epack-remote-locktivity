package locktivity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client defines the interface for Locktivity API operations.
// This interface allows for easy mocking in tests.
type Client interface {
	// Pack operations
	CreatePack(ctx context.Context, req CreatePackRequest) (*CreatePackResponse, error)
	GetPack(ctx context.Context, packID string) (*GetPackResponse, error)
	CreateFinalizeIntent(ctx context.Context, req CreateFinalizeIntentRequest) (*CreateFinalizeIntentResponse, error)
	ConsumeFinalizeIntent(ctx context.Context, req ConsumeFinalizeIntentRequest) (*ConsumeFinalizeIntentResponse, error)

	// Release operations
	CreateRelease(ctx context.Context, req CreateReleaseRequest) (*ReleaseResponse, error)
	GetRelease(ctx context.Context, releaseID string) (*ReleaseResponse, error)
	GetLatestRelease(ctx context.Context, environment string) (*ReleaseResponse, error)
	GetReleaseByVersion(ctx context.Context, version, environment string) (*ReleaseResponse, error)
	GetReleaseByDigest(ctx context.Context, digest, environment string) (*ReleaseResponse, error)

	// Run operations
	SyncRuns(ctx context.Context, req CreatePackRunsRequest) (*PackRunsResponse, error)
	ConfirmRunOutputUpload(ctx context.Context, outputID string, req ConfirmRunOutputUploadRequest) (*ConfirmRunOutputUploadResponse, error)

	// Upload operations
	UploadToPresignedURL(ctx context.Context, uploadURL string, headers map[string]string, content io.Reader) error

	// Identity operations
	GetIdentity(ctx context.Context) (*IdentityResponse, error)
}

// APIClient implements the Client interface.
type APIClient struct {
	httpClient  *http.Client
	baseURL     string
	accessToken string
}

// Ensure APIClient implements Client.
var _ Client = (*APIClient)(nil)

// NewClient creates a new Locktivity API client.
func NewClient(baseURL, accessToken string) *APIClient {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	return &APIClient{
		httpClient: &http.Client{
			Timeout:   HTTPTimeout,
			Transport: newHTTPTransport(),
		},
		baseURL:     baseURL,
		accessToken: accessToken,
	}
}

// NewClientWithHTTP creates a client with a custom HTTP client (for testing).
func NewClientWithHTTP(httpClient *http.Client, baseURL string) *APIClient {
	return &APIClient{
		httpClient: httpClient,
		baseURL:    strings.TrimSuffix(baseURL, "/"),
	}
}

// SetToken sets the access token.
func (c *APIClient) SetToken(token string) {
	c.accessToken = token
}

// createPackWrapper wraps CreatePackRequest for Rails strong params.
type createPackWrapper struct {
	Pack CreatePackRequest `json:"pack"`
}

// CreatePack creates a pack and returns upload URL.
func (c *APIClient) CreatePack(ctx context.Context, req CreatePackRequest) (*CreatePackResponse, error) {
	path := fmt.Sprintf("%s/packs", APIPathPrefix)

	var resp CreatePackResponse
	if err := c.doJSON(ctx, http.MethodPost, path, createPackWrapper{Pack: req}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetPack fetches a pack by ID (returns download URL).
func (c *APIClient) GetPack(ctx context.Context, packID string) (*GetPackResponse, error) {
	path := fmt.Sprintf("%s/packs/%s", APIPathPrefix, url.PathEscape(packID))

	var resp GetPackResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CreateFinalizeIntent creates a one-time finalize intent for a pack.
func (c *APIClient) CreateFinalizeIntent(ctx context.Context, req CreateFinalizeIntentRequest) (*CreateFinalizeIntentResponse, error) {
	path := fmt.Sprintf("%s/finalize_intents", APIPathPrefix)

	var resp CreateFinalizeIntentResponse
	if err := c.doJSON(ctx, http.MethodPost, path, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ConsumeFinalizeIntent consumes a one-time finalize intent.
func (c *APIClient) ConsumeFinalizeIntent(ctx context.Context, req ConsumeFinalizeIntentRequest) (*ConsumeFinalizeIntentResponse, error) {
	path := fmt.Sprintf("%s/finalize_intents/%s", APIPathPrefix, url.PathEscape(req.FinalizeIntentID))

	var resp ConsumeFinalizeIntentResponse
	if err := c.doJSON(ctx, http.MethodPatch, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// createReleaseWrapper wraps CreateReleaseRequest for Rails strong params.
type createReleaseWrapper struct {
	Release CreateReleaseRequest `json:"release"`
}

// CreateRelease creates a release after upload completes.
func (c *APIClient) CreateRelease(ctx context.Context, req CreateReleaseRequest) (*ReleaseResponse, error) {
	path := fmt.Sprintf("%s/releases", APIPathPrefix)

	var resp ReleaseResponse
	if err := c.doJSON(ctx, http.MethodPost, path, createReleaseWrapper{Release: req}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetRelease fetches a release by ID.
func (c *APIClient) GetRelease(ctx context.Context, releaseID string) (*ReleaseResponse, error) {
	path := fmt.Sprintf("%s/releases/%s", APIPathPrefix, url.PathEscape(releaseID))

	var resp ReleaseResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetLatestRelease fetches the latest release for a target environment.
func (c *APIClient) GetLatestRelease(ctx context.Context, environment string) (*ReleaseResponse, error) {
	path := fmt.Sprintf("%s/releases/latest", APIPathPrefix)
	if environment != "" {
		path += "?environment=" + url.QueryEscape(environment)
	}

	var resp ReleaseResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetReleaseByVersion fetches a release by version for a target environment.
func (c *APIClient) GetReleaseByVersion(ctx context.Context, version, environment string) (*ReleaseResponse, error) {
	path := fmt.Sprintf("%s/releases/latest?version=%s", APIPathPrefix, url.QueryEscape(version))
	if environment != "" {
		path += "&environment=" + url.QueryEscape(environment)
	}

	var resp ReleaseResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetReleaseByDigest fetches a release by pack digest for a target environment.
func (c *APIClient) GetReleaseByDigest(ctx context.Context, digest, environment string) (*ReleaseResponse, error) {
	path := fmt.Sprintf("%s/releases/latest?pack_digest=%s", APIPathPrefix, url.QueryEscape(digest))
	if environment != "" {
		path += "&environment=" + url.QueryEscape(environment)
	}

	var resp ReleaseResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SyncRuns syncs run ledgers to the remote.
func (c *APIClient) SyncRuns(ctx context.Context, req CreatePackRunsRequest) (*PackRunsResponse, error) {
	path := fmt.Sprintf("%s/pack_runs", APIPathPrefix)

	var resp PackRunsResponse
	if err := c.doJSON(ctx, http.MethodPost, path, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ConfirmRunOutputUpload confirms a run output file upload.
func (c *APIClient) ConfirmRunOutputUpload(ctx context.Context, outputID string, req ConfirmRunOutputUploadRequest) (*ConfirmRunOutputUploadResponse, error) {
	path := fmt.Sprintf("%s/pack_run_outputs/%s", APIPathPrefix, url.PathEscape(outputID))

	var resp ConfirmRunOutputUploadResponse
	if err := c.doJSON(ctx, http.MethodPatch, path, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetIdentity fetches the current authenticated identity.
func (c *APIClient) GetIdentity(ctx context.Context) (*IdentityResponse, error) {
	path := fmt.Sprintf("%s/identity", APIPathPrefix)

	var resp IdentityResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ErrAccepted is returned when the server responds with 202 Accepted,
// indicating the request was accepted but processing is not complete.
// Callers should retry the request.
type ErrAccepted struct {
	Message string
}

func (e ErrAccepted) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "request accepted but processing not complete, retry later"
}

// doJSON performs an HTTP request with JSON body and response.
func (c *APIClient) doJSON(ctx context.Context, method, path string, body, result any) error {
	req, err := c.newJSONRequest(ctx, method, path, body)
	if err != nil {
		return err
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusAccepted {
		// 202 Accepted - request was accepted but processing is not complete
		var apiErr APIError
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err == nil && apiErr.ErrorString() != "" {
			return ErrAccepted{Message: apiErr.ErrorString()}
		}
		return ErrAccepted{}
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return c.handleRateLimitResponse(resp)
	}

	if resp.StatusCode >= 400 {
		return c.handleErrorResponse(resp)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

func (c *APIClient) newJSONRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	reqURL := fmt.Sprintf("%s%s", c.baseURL, path)
	if body == nil {
		return http.NewRequestWithContext(ctx, method, reqURL, nil)
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	return http.NewRequestWithContext(ctx, method, reqURL, bytes.NewReader(data))
}

// setHeaders sets standard headers on a request.
func (c *APIClient) setHeaders(req *http.Request) {
	req.Header.Set("Accept", AcceptHeader)
	req.Header.Set("Content-Type", ContentTypeHeader)
	if c.accessToken != "" {
		req.Header.Set(AuthorizationHeader, "Bearer "+c.accessToken)
	}
}

// ErrRateLimited is returned when the API returns a 429 response.
type ErrRateLimited struct {
	Message    string
	RetryAfter string
}

func (e ErrRateLimited) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("rate_limited: %s", e.Message)
	}
	return "rate_limited: too many requests"
}

// handleRateLimitResponse parses a 429 response.
func (c *APIClient) handleRateLimitResponse(resp *http.Response) error {
	retryAfter := resp.Header.Get("Retry-After")
	var apiErr APIError
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err == nil && apiErr.ErrorString() != "" {
		return ErrRateLimited{Message: apiErr.ErrorString(), RetryAfter: retryAfter}
	}
	return ErrRateLimited{RetryAfter: retryAfter}
}

// handleErrorResponse parses an error response.
func (c *APIClient) handleErrorResponse(resp *http.Response) error {
	var apiErr APIError
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err == nil {
		if apiErr.Code != "" {
			return fmt.Errorf("API error [%s]: %s", apiErr.Code, apiErr.ErrorString())
		}
		if apiErr.ErrorString() != "" {
			return fmt.Errorf("API error: %s", apiErr.ErrorString())
		}
	}
	return fmt.Errorf("API returned status %d", resp.StatusCode)
}

// UploadToPresignedURL uploads content to a presigned URL.
func (c *APIClient) UploadToPresignedURL(ctx context.Context, uploadURL string, headers map[string]string, content io.Reader) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, content)
	if err != nil {
		return fmt.Errorf("failed to create upload request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
