package remote

import "github.com/locktivity/epack/componentsdk"

// PushPrepareRequest is the request for push.prepare operations.
type PushPrepareRequest struct {
	RequestID string       `json:"request_id"`
	Remote    string       `json:"remote"`
	Target    RemoteTarget `json:"target"`
	Pack      PackInfo     `json:"pack"`
	Release   ReleaseInfo  `json:"release"`
	Identity  *AuthHints   `json:"identity,omitempty"`
}

type PushPrepareResponse = componentsdk.PushPrepareResponse
type PushFinalizeResponse = componentsdk.PushFinalizeResponse
type PullPrepareRequest = componentsdk.PullPrepareRequest
type PullPrepareResponse = componentsdk.PullPrepareResponse
type PullFinalizeRequest = componentsdk.PullFinalizeRequest
type PullFinalizeResponse = componentsdk.PullFinalizeResponse
type UploadInfo = componentsdk.UploadInfo
type DownloadInfo = componentsdk.DownloadInfo
type PackResult = componentsdk.PackResult
type ReleaseResult = componentsdk.ReleaseResult
type AuthHints = componentsdk.AuthHints

// PushFinalizeRequest is the request for push.finalize operations.
type PushFinalizeRequest struct {
	RequestID     string       `json:"request_id"`
	Remote        string       `json:"remote"`
	Target        RemoteTarget `json:"target"`
	Pack          PackInfo     `json:"pack"`
	Release       ReleaseInfo  `json:"release,omitempty"`
	FinalizeToken string       `json:"finalize_token"`
}

type PackInfo struct {
	Path           string `json:"path,omitempty"`
	Digest         string `json:"digest"`
	ManifestDigest string `json:"manifest_digest,omitempty"`
	FileDigest     string `json:"file_digest,omitempty"`
	SizeBytes      int64  `json:"size_bytes"`
	Checksum       string `json:"checksum,omitempty"`
}

type ReleaseInfo struct {
	Version        string            `json:"version,omitempty"`
	Notes          string            `json:"notes,omitempty"`
	Labels         []string          `json:"labels,omitempty"`
	BuildContext   map[string]string `json:"build_context,omitempty"`
	LockProvenance *LockProvenance   `json:"lock_provenance,omitempty"`
}

// LockReportRequest is the request for lock.report operations.
type LockReportRequest struct {
	Type           string         `json:"type"`
	RequestID      string         `json:"request_id"`
	Remote         string         `json:"remote"`
	Target         RemoteTarget   `json:"target"`
	LockProvenance LockProvenance `json:"lock_provenance"`
	Identity       *AuthHints     `json:"identity,omitempty"`
}

// LockReportResponse is the response for lock.report operations.
type LockReportResponse struct {
	OK             bool   `json:"ok"`
	Type           string `json:"type"`
	RequestID      string `json:"request_id"`
	Status         string `json:"status"`
	Outcome        string `json:"outcome,omitempty"`
	LockfileSHA256 string `json:"lockfile_sha256,omitempty"`
	RevisionID     string `json:"revision_id,omitempty"`
}

type LockProvenance struct {
	Lockfile       string         `json:"lockfile,omitempty"`
	LockfileSHA256 string         `json:"lockfile_sha256,omitempty"`
	LockfilePath   string         `json:"lockfile_path,omitempty"`
	Summary        any            `json:"summary,omitempty"`
	RuntimeContext map[string]any `json:"runtime_context,omitempty"`
	TriggerKind    string         `json:"trigger_kind"`
	Outcome        string         `json:"outcome"`
	FailureCode    string         `json:"failure_code,omitempty"`
	FailureMessage string         `json:"failure_message,omitempty"`
	ReportedAt     string         `json:"reported_at,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// RunsSyncRequest is the request for runs.sync operations.
type RunsSyncRequest struct {
	Type       string       `json:"type"`
	RequestID  string       `json:"request_id"`
	Target     RemoteTarget `json:"target"`
	FileDigest string       `json:"file_digest"` // SHA256 of .epack file (unique pack identifier)
	Runs       []RunInfo    `json:"runs"`
}

// RemoteTarget contains caller-provided target selectors.
// Locktivity currently uses environment for release lookups.
type RemoteTarget struct {
	Workspace   string `json:"workspace,omitempty"`
	Stream      string `json:"stream"`
	Environment string `json:"environment,omitempty"`
}

// RunInfo contains run metadata.
type RunInfo struct {
	RunID        string `json:"run_id"`
	ResultPath   string `json:"result_path"`
	ResultDigest string `json:"result_digest"`
}

// RunsSyncResponse is the response for runs.sync operations.
type RunsSyncResponse struct {
	OK            bool           `json:"ok"`
	Type          string         `json:"type"`
	RequestID     string         `json:"request_id"`
	Accepted      int            `json:"accepted"`
	Rejected      int            `json:"rejected"`
	Items         []RunSyncItem  `json:"items"`
	FailedOutputs []FailedOutput `json:"failed_outputs,omitempty"`
}

// FailedOutput describes an output file that failed to upload or confirm.
type FailedOutput struct {
	RunID  string `json:"run_id"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// RunSyncItem is the result of syncing a single run.
type RunSyncItem struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

// AuthLoginRequest is the request for auth.login operations.
type AuthLoginRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
}

// AuthLoginResponse is the response for auth.login operations.
type AuthLoginResponse struct {
	OK           bool                  `json:"ok"`
	Type         string                `json:"type"`
	RequestID    string                `json:"request_id"`
	Instructions AuthLoginInstructions `json:"instructions"`
}

// AuthLoginInstructions provides device code flow instructions.
type AuthLoginInstructions struct {
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresInSecs   int    `json:"expires_in_seconds"`
}

// AuthWhoamiRequest is the request for auth.whoami operations.
type AuthWhoamiRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
}

// AuthWhoamiResponse is the response for auth.whoami operations.
type AuthWhoamiResponse struct {
	OK        bool           `json:"ok"`
	Type      string         `json:"type"`
	RequestID string         `json:"request_id"`
	Identity  IdentityResult `json:"identity"`
}

// IdentityResult contains authentication identity info.
type IdentityResult struct {
	Authenticated bool   `json:"authenticated"`
	Subject       string `json:"subject,omitempty"`
}

// ErrorResponse is the error response format.
type ErrorResponse struct {
	OK        bool      `json:"ok"`
	Type      string    `json:"type"`
	RequestID string    `json:"request_id"`
	Error     ErrorInfo `json:"error"`
}

// ErrorInfo contains error details.
type ErrorInfo struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}
