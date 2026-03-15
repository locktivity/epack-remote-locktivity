package remote

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
