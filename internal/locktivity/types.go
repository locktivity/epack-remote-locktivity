package locktivity

import "time"

// CreatePackRequest is sent to POST /packs.
// Only file_digest, size_bytes, checksum are required.
// pack_digest, manifest_digest, stream are extracted from the file during ingestion.
type CreatePackRequest struct {
	FileDigest string `json:"file_digest"` // SHA256 of .epack file (unique identifier)
	SizeBytes  int64  `json:"size_bytes"`
	Checksum   string `json:"checksum,omitempty"` // Base64-encoded MD5 for S3 upload
}

// CreatePackResponse is returned from POST /packs.
type CreatePackResponse struct {
	ID         string      `json:"id"`
	FileDigest string      `json:"file_digest"` // SHA256 of .epack file
	SizeBytes  int64       `json:"size_bytes"`
	Exists     bool        `json:"exists,omitempty"`
	Upload     *UploadInfo `json:"upload,omitempty"`
}

// UploadInfo contains presigned upload URL info.
type UploadInfo struct {
	URL           string            `json:"url"`
	Headers       map[string]string `json:"headers,omitempty"`
	UploadToken   string            `json:"upload_token"`
	FinalizeToken string            `json:"finalize_token,omitempty"`
}

// GetPackResponse is returned from GET /packs/:id.
type GetPackResponse struct {
	ID         string        `json:"id"`
	PackDigest string        `json:"pack_digest"`           // SHA256 of artifacts (logical pack identity)
	FileDigest string        `json:"file_digest,omitempty"` // SHA256 of .epack file
	SizeBytes  int64         `json:"size_bytes"`
	Stream     string        `json:"stream,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
	Download   *DownloadInfo `json:"download,omitempty"`
}

// DownloadInfo contains presigned download URL info.
type DownloadInfo struct {
	URL string `json:"url"`
}

// CreateReleaseRequest is sent to POST /releases.
type CreateReleaseRequest struct {
	FinalizeToken  string            `json:"finalize_token"`
	UploadToken    string            `json:"upload_token,omitempty"`
	Environment    string            `json:"environment,omitempty"`
	Version        string            `json:"version,omitempty"`
	Notes          string            `json:"notes,omitempty"`
	Labels         []string          `json:"labels,omitempty"`
	BuildContext   map[string]string `json:"build_context,omitempty"`
	LockProvenance any               `json:"lock_provenance,omitempty"`
}

// CreateLockfileReportRequest is sent to POST /lockfile_reports.
type CreateLockfileReportRequest struct {
	PipelineID     string         `json:"pipeline_id,omitempty"`
	RepoOwner      string         `json:"repo_owner,omitempty"`
	RepoName       string         `json:"repo_name,omitempty"`
	Branch         string         `json:"branch,omitempty"`
	Ref            string         `json:"ref,omitempty"`
	TriggerKind    string         `json:"trigger_kind,omitempty"`
	Outcome        string         `json:"outcome,omitempty"`
	Lockfile       string         `json:"lockfile,omitempty"`
	LockfileSHA256 string         `json:"lockfile_sha256,omitempty"`
	ReportedAt     string         `json:"reported_at,omitempty"`
	FailureCode    string         `json:"failure_code,omitempty"`
	FailureMessage string         `json:"failure_message,omitempty"`
	Summary        any            `json:"summary,omitempty"`
	RuntimeContext map[string]any `json:"runtime_context,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// LockfileReportResponse is returned from POST /lockfile_reports.
type LockfileReportResponse struct {
	ID             string `json:"id,omitempty"`
	PipelineID     string `json:"pipeline_id,omitempty"`
	RevisionID     string `json:"revision_id,omitempty"`
	Outcome        string `json:"outcome,omitempty"`
	LockfileSHA256 string `json:"lockfile_sha256,omitempty"`
	Status         string `json:"status,omitempty"`
}

type CreateFinalizeIntentRequest struct {
	PackID string `json:"pack_id"`
}

type CreateFinalizeIntentResponse struct {
	FinalizeToken string    `json:"finalize_token"`
	PackID        string    `json:"pack_id"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type ConsumeFinalizeIntentRequest struct {
	FinalizeIntentID string `json:"finalize_intent_id"`
}

// ConsumeFinalizeIntentResponse is returned from consuming a finalize intent.
// Response is currently ignored (only error is checked).
type ConsumeFinalizeIntentResponse struct{}

// ReleaseResponse is returned from POST /releases and GET /releases/:id.
// Only includes fields that are actually used by the handler.
type ReleaseResponse struct {
	ID      string    `json:"id"`
	Version string    `json:"version,omitempty"`
	Pack    *PackInfo `json:"pack,omitempty"`
}

// PackInfo contains pack details in a release response.
// Only includes fields that are actually used by the handler.
type PackInfo struct {
	ID string `json:"id"`
}

// CreatePackRunsRequest is sent to POST /pack_runs.
type CreatePackRunsRequest struct {
	FileDigest string    `json:"file_digest"` // SHA256 of .epack file (unique pack identifier)
	Runs       []RunInfo `json:"runs"`
}

// RunInfo contains metadata about a single run.
type RunInfo struct {
	RunID        string       `json:"run_id"`
	ResultPath   string       `json:"result_path"`
	ResultDigest string       `json:"result_digest"`
	ToolName     string       `json:"tool_name,omitempty"`
	ToolVersion  string       `json:"tool_version,omitempty"`
	Status       string       `json:"status,omitempty"`
	StartedAt    string       `json:"started_at,omitempty"`
	CompletedAt  string       `json:"completed_at,omitempty"`
	DurationMs   int64        `json:"duration_ms,omitempty"`
	ExitCode     int          `json:"exit_code,omitempty"`
	ToolExitCode int          `json:"tool_exit_code,omitempty"`
	ErrorMessage string       `json:"error_message,omitempty"`
	Outputs      []OutputInfo `json:"outputs,omitempty"`
}

// OutputInfo contains metadata about a tool output file (request).
type OutputInfo struct {
	Path      string `json:"path"`
	Digest    string `json:"digest,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
	Checksum  string `json:"checksum"` // Base64-encoded MD5 for S3 upload (required by Rails)
}

// PackRunsResponse is returned from POST /pack_runs.
type PackRunsResponse struct {
	Accepted int           `json:"accepted"`
	Rejected int           `json:"rejected"`
	Runs     []PackRunInfo `json:"runs,omitempty"`
}

// PackRunInfo contains info about a synced run.
type PackRunInfo struct {
	ID           string             `json:"id"`
	RunID        string             `json:"run_id"`
	ResultPath   string             `json:"result_path"`
	ResultDigest string             `json:"result_digest"`
	Status       string             `json:"status"`
	CreatedAt    time.Time          `json:"created_at"`
	Outputs      []OutputUploadInfo `json:"outputs,omitempty"`
}

// OutputUploadInfo contains presigned upload info for an output file.
type OutputUploadInfo struct {
	ID            string            `json:"id"`
	Path          string            `json:"path"`
	UploadURL     string            `json:"upload_url,omitempty"`
	UploadHeaders map[string]string `json:"upload_headers,omitempty"`
	UploadToken   string            `json:"upload_token,omitempty"`
}

// ConfirmRunOutputUploadRequest is sent to PATCH /pack_run_outputs/:id.
type ConfirmRunOutputUploadRequest struct {
	UploadToken string `json:"upload_token"`
}

// ConfirmRunOutputUploadResponse is returned from PATCH /pack_run_outputs/:id.
type ConfirmRunOutputUploadResponse struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	ScanStatus string `json:"scan_status"`
}

// IdentityResponse is returned from GET /identity.
type IdentityResponse struct {
	Authenticated bool     `json:"authenticated"`
	Subject       string   `json:"subject"`
	Scopes        []string `json:"scopes,omitempty"`
}

// TokenResponse is returned from OAuth token endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// DeviceCodeResponse is returned from OAuth device code endpoint.
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// APIError represents an error response from the API.
// Handles both old format {error: "..."} and new format {errors: [...]}.
type APIError struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Error   string   `json:"error"`  // Old format (singular, also used by Rack middleware)
	Errors  []string `json:"errors"` // New format (array)
}

func (e APIError) ErrorString() string {
	if e.Message != "" {
		return e.Message
	}
	if len(e.Errors) > 0 {
		return e.Errors[0]
	}
	return e.Error
}
