package remote

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/locktivity/epack-remote-locktivity/internal/auth"
	"github.com/locktivity/epack-remote-locktivity/internal/locktivity"
	"github.com/locktivity/epack/componentsdk"
)

// Handler implements componentsdk.RemoteHandler for Locktivity.
type Handler struct {
	client   locktivity.Client
	oauth    *auth.OAuth
	keychain auth.Keychain
	endpoint string
	sleep    func(time.Duration)

	tokenKey []byte

	loginMu         sync.Mutex
	loginInProgress bool
	loginCancel     context.CancelFunc
	loginSeq        uint64

	nonceMu    sync.Mutex
	usedNonces map[string]int64

	rateMu            sync.Mutex
	lastAuthLoginAt   time.Time
	lastPullPrepareAt time.Time
}

// Ensure Handler implements RemoteHandler.
var _ componentsdk.RemoteHandler = (*Handler)(nil)

// NewHandler creates a new Locktivity remote handler.
func NewHandler() *Handler {
	endpoint, authEndpoint := locktivity.Endpoints()

	keychain := auth.NewOSKeychain(authEndpoint)
	oauth := auth.NewOAuth(authEndpoint, keychain)

	return &Handler{
		oauth:    oauth,
		keychain: keychain,
		endpoint: endpoint,
		tokenKey: generateTokenKey(keychain),
		sleep:    time.Sleep,
	}
}

// NewHandlerWithClient creates a handler with a custom client (for testing).
func NewHandlerWithClient(client locktivity.Client, oauth *auth.OAuth) *Handler {
	return &Handler{
		client:   client,
		oauth:    oauth,
		tokenKey: generateTokenKey(nil),
		sleep:    time.Sleep,
	}
}

// getClient returns an authenticated client.
func (h *Handler) getClient(ctx context.Context) (locktivity.Client, error) {
	if h.client != nil {
		return h.client, nil
	}

	token, err := h.oauth.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	return locktivity.NewClient(h.endpoint, token), nil
}

// PushFinalizeTokenData holds the data encoded in the finalize token.
type PushFinalizeTokenData struct {
	FinalizeToken string            `json:"finalize_token,omitempty"`
	PackID        string            `json:"pack_id"`
	FileDigest    string            `json:"file_digest"`           // SHA256 of .epack file (unique per upload)
	PackDigest    string            `json:"pack_digest,omitempty"` // SHA256 of artifacts (for receipt display)
	UploadToken   string            `json:"upload_token,omitempty"`
	Exists        bool              `json:"exists,omitempty"`
	Environment   string            `json:"environment,omitempty"`
	Version       string            `json:"version,omitempty"`
	Notes         string            `json:"notes,omitempty"`
	Labels        []string          `json:"labels,omitempty"`
	BuildContext  map[string]string `json:"build_context,omitempty"`
	ExpiresAt     int64             `json:"exp"`
	Nonce         string            `json:"nonce"`
}

type PullFinalizeTokenData struct {
	FinalizeToken string `json:"finalize_token,omitempty"`
	PackID        string `json:"pack_id"`
	FileDigest    string `json:"file_digest"` // SHA256 of .epack file (for download verification)
	ReleaseID     string `json:"release_id"`
	ExpiresAt     int64  `json:"exp"`
	Nonce         string `json:"nonce"`
}

type signedToken struct {
	Payload string `json:"payload"`
	Sig     string `json:"sig"`
}

const finalizeTokenTTL = 15 * time.Minute
const (
	authLoginMinInterval   = 3 * time.Second
	pullPrepareMinInterval = 100 * time.Millisecond
)

// Retry configuration for rate limiting and async processing
const (
	maxRetries          = 30
	initialRetryWait    = time.Second
	maxRetryWait        = 5 * time.Second
	rateLimitRetryWait  = 2 * time.Second
	maxRateLimitRetries = 5
)

func generateTokenKey(keychain auth.Keychain) []byte {
	// Derive a deterministic key from the auth material that will be available
	// across separate prepare/finalize invocations.
	if material, ok := tokenKeyMaterial(keychain); ok {
		h := hmac.New(sha256.New, []byte("epack-remote-locktivity-token-key"))
		h.Write([]byte(material))
		return h.Sum(nil)
	}

	// Fallback to random key when no stable auth material is available.
	// Note: prepare/finalize flows will fail without deterministic key.
	key := make([]byte, 32)
	if _, err := rand.Read(key); err == nil {
		return key
	}
	// Fallback for extremely rare entropy failures.
	sum := sha256.Sum256([]byte(fmt.Sprintf("fallback-%d", time.Now().UnixNano())))
	return sum[:]
}

func tokenKeyMaterial(keychain auth.Keychain) (string, bool) {
	if token := strings.TrimSpace(os.Getenv(locktivity.EnvAccessToken)); token != "" {
		return "access_token:" + token, true
	}

	clientID := os.Getenv(locktivity.EnvClientID)
	clientSecret := os.Getenv(locktivity.EnvClientSecret)
	if clientID != "" && clientSecret != "" {
		return "client_credentials:" + clientID + ":" + clientSecret, true
	}

	if oidcToken := strings.TrimSpace(os.Getenv(locktivity.EnvOIDCToken)); oidcToken != "" {
		return "oidc_token:" + oidcToken, true
	}

	if keychain == nil {
		return "", false
	}

	if refreshToken, err := keychain.GetRefreshToken(); err == nil && refreshToken != "" {
		return "refresh_token:" + refreshToken, true
	}

	if accessToken, err := keychain.GetToken(); err == nil && accessToken != "" {
		return "access_token:" + accessToken, true
	}

	return "", false
}

func randomNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err == nil {
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return fmt.Sprintf("nonce-%d", time.Now().UnixNano())
}

func (h *Handler) sign(payloadB64 string) string {
	mac := hmac.New(sha256.New, h.tokenKey)
	_, _ = mac.Write([]byte(payloadB64))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (h *Handler) encodeSignedToken(payload any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(data)
	token := signedToken{
		Payload: payloadB64,
		Sig:     h.sign(payloadB64),
	}
	tokenBytes, err := json.Marshal(token)
	if err != nil {
		return "", err
	}
	return string(tokenBytes), nil
}

func (h *Handler) decodeSignedToken(tokenStr string, out any) error {
	var token signedToken
	if err := json.Unmarshal([]byte(tokenStr), &token); err != nil {
		return fmt.Errorf("invalid token format")
	}
	if token.Payload == "" || token.Sig == "" {
		return fmt.Errorf("missing token fields")
	}
	expectedSig := h.sign(token.Payload)
	if !hmac.Equal([]byte(token.Sig), []byte(expectedSig)) {
		return errors.New("invalid token signature")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(token.Payload)
	if err != nil {
		return fmt.Errorf("invalid token payload encoding")
	}
	if err := json.Unmarshal(payloadBytes, out); err != nil {
		return fmt.Errorf("invalid token payload")
	}
	return nil
}

func (h *Handler) consumeFinalizeNonce(nonce string, expiresAt int64) bool {
	if nonce == "" {
		return false
	}
	now := time.Now().Unix()

	h.nonceMu.Lock()
	defer h.nonceMu.Unlock()

	if h.usedNonces == nil {
		h.usedNonces = make(map[string]int64)
	}

	// Opportunistic cleanup of expired nonce entries.
	for n, exp := range h.usedNonces {
		if exp <= now {
			delete(h.usedNonces, n)
		}
	}

	if _, exists := h.usedNonces[nonce]; exists {
		return false
	}

	exp := expiresAt
	if exp <= now {
		exp = now + int64(finalizeTokenTTL.Seconds())
	}
	h.usedNonces[nonce] = exp
	return true
}

func (h *Handler) allowRateLimited(op string, minInterval time.Duration) bool {
	now := time.Now()
	h.rateMu.Lock()
	defer h.rateMu.Unlock()

	var last *time.Time
	switch op {
	case "auth.login":
		last = &h.lastAuthLoginAt
	case "pull.prepare":
		last = &h.lastPullPrepareAt
	default:
		return true
	}

	if !last.IsZero() && now.Sub(*last) < minInterval {
		return false
	}
	*last = now
	return true
}

// PushPrepare handles push.prepare requests.
func (h *Handler) PushPrepare(req componentsdk.PushPrepareRequest) (*componentsdk.PushPrepareResponse, error) {
	ctx := context.Background()

	client, err := h.getClient(ctx)
	if err != nil {
		return nil, componentsdk.ErrAuthRequired(err.Error())
	}

	createReq := locktivity.CreatePackRequest{
		FileDigest: req.Pack.FileDigest,
		SizeBytes:  req.Pack.SizeBytes,
		Checksum:   req.Pack.Checksum,
	}

	packResp, err := client.CreatePack(ctx, createReq)
	if err != nil {
		return nil, toRemoteError(err)
	}

	finalizeToken := ""
	if packResp.Upload != nil {
		finalizeToken = packResp.Upload.FinalizeToken
	}

	tokenData := PushFinalizeTokenData{
		FinalizeToken: finalizeToken,
		PackID:        packResp.ID,
		FileDigest:    packResp.FileDigest, // unique identifier (from API)
		PackDigest:    req.Pack.Digest,     // artifact hash (from client, for receipt)
		Environment:   req.Target.Environment,
		Version:       req.Release.Version,
		Notes:         req.Release.Notes,
		Labels:        req.Release.Labels,
		BuildContext:  req.Release.BuildContext,
		ExpiresAt:     time.Now().Add(finalizeTokenTTL).Unix(),
		Nonce:         randomNonce(),
	}

	if packResp.Exists {
		tokenData.Exists = true
		tokenStr, err := h.encodeSignedToken(tokenData)
		if err != nil {
			return nil, componentsdk.ErrServerError(fmt.Sprintf("failed to create finalize token: %v", err))
		}
		return &componentsdk.PushPrepareResponse{
			Upload: componentsdk.UploadInfo{
				Method: "skip",
				URL:    "",
			},
			FinalizeToken: tokenStr,
		}, nil
	}

	tokenData.UploadToken = packResp.Upload.UploadToken
	tokenStr, err := h.encodeSignedToken(tokenData)
	if err != nil {
		return nil, componentsdk.ErrServerError(fmt.Sprintf("failed to create finalize token: %v", err))
	}

	return &componentsdk.PushPrepareResponse{
		Upload: componentsdk.UploadInfo{
			Method:  "PUT",
			URL:     packResp.Upload.URL,
			Headers: packResp.Upload.Headers,
		},
		FinalizeToken: tokenStr,
	}, nil
}

// PushFinalize handles push.finalize requests.
func (h *Handler) PushFinalize(req componentsdk.PushFinalizeRequest) (*componentsdk.PushFinalizeResponse, error) {
	ctx := context.Background()

	client, err := h.getClient(ctx)
	if err != nil {
		return nil, componentsdk.ErrAuthRequired(err.Error())
	}

	var tokenData PushFinalizeTokenData
	if err := h.decodeSignedToken(req.FinalizeToken, &tokenData); err != nil {
		return nil, componentsdk.ErrServerError(fmt.Sprintf("invalid finalize token: %v", err))
	}
	if tokenData.ExpiresAt > 0 && time.Now().Unix() > tokenData.ExpiresAt {
		return nil, componentsdk.ErrServerError("finalize token expired")
	}
	if !h.consumeFinalizeNonce(tokenData.Nonce, tokenData.ExpiresAt) {
		return nil, componentsdk.ErrServerError("finalize token replay detected")
	}

	releaseReq := locktivity.CreateReleaseRequest{
		FinalizeToken: tokenData.FinalizeToken,
		UploadToken:   tokenData.UploadToken,
		Environment:   tokenData.Environment,
		Version:       tokenData.Version,
		Notes:         tokenData.Notes,
		Labels:        tokenData.Labels,
		BuildContext:  tokenData.BuildContext,
	}

	// Retry loop for 202 Accepted (processing) and 429 (rate limit) responses
	var releaseResp *locktivity.ReleaseResponse
	retryInterval := initialRetryWait
	rateLimitAttempts := 0

	for attempt := 0; attempt <= maxRetries; attempt++ {
		releaseResp, err = client.CreateRelease(ctx, releaseReq)
		if err == nil {
			break
		}

		// Check for retryable errors
		var acceptedErr locktivity.ErrAccepted
		var rateLimitErr locktivity.ErrRateLimited

		switch {
		case errors.As(err, &acceptedErr):
			// 202 Accepted - pack processing in progress
			if attempt == maxRetries {
				return nil, componentsdk.ErrServerError("pack processing timed out, please retry")
			}
			slog.Debug("pack processing in progress, retrying",
				"attempt", attempt+1,
				"max_retries", maxRetries,
				"wait", retryInterval)
			h.sleepFor(retryInterval)
			// Exponential backoff with cap
			retryInterval = retryInterval * 2
			if retryInterval > maxRetryWait {
				retryInterval = maxRetryWait
			}

		case errors.As(err, &rateLimitErr):
			// 429 Rate Limited - wait and retry
			rateLimitAttempts++
			if rateLimitAttempts > maxRateLimitRetries {
				return nil, toRemoteError(err)
			}
			wait := parseRetryAfter(rateLimitErr.RetryAfter, rateLimitRetryWait)
			slog.Debug("rate limited, retrying",
				"attempt", rateLimitAttempts,
				"max_attempts", maxRateLimitRetries,
				"wait", wait)
			h.sleepFor(wait)

		default:
			// Non-retryable error
			return nil, toRemoteError(err)
		}
	}

	return &componentsdk.PushFinalizeResponse{
		Release: componentsdk.ReleaseResult{
			ReleaseID:  releaseResp.ID,
			PackDigest: tokenData.PackDigest,
			Version:    releaseResp.Version,
		},
	}, nil
}

// PullPrepare handles pull.prepare requests.
func (h *Handler) PullPrepare(req componentsdk.PullPrepareRequest) (*componentsdk.PullPrepareResponse, error) {
	if !h.allowRateLimited("pull.prepare", pullPrepareMinInterval) {
		return nil, componentsdk.ErrRateLimited("pull.prepare rate limit exceeded")
	}

	ctx := context.Background()

	client, err := h.getClient(ctx)
	if err != nil {
		return nil, componentsdk.ErrAuthRequired(err.Error())
	}

	release, err := h.resolvePullRelease(ctx, client, req)
	if err != nil {
		return nil, toRemoteError(err)
	}

	packResp, err := client.GetPack(ctx, release.Pack.ID)
	if err != nil {
		return nil, toRemoteError(err)
	}

	if packResp.Download == nil {
		return nil, componentsdk.ErrNotFound("pack has no download URL available")
	}

	finalizeIntent, err := client.CreateFinalizeIntent(ctx, locktivity.CreateFinalizeIntentRequest{
		PackID: packResp.ID,
	})
	if err != nil {
		return nil, toRemoteError(err)
	}

	// Use file_digest for download verification (file SHA256, not artifact hash)
	verifyDigest := packResp.FileDigest
	if verifyDigest == "" {
		verifyDigest = packResp.PackDigest // fallback to pack_digest (artifact hash)
	}

	finalizeToken, err := h.encodeSignedToken(PullFinalizeTokenData{
		FinalizeToken: finalizeIntent.FinalizeToken,
		PackID:        packResp.ID,
		FileDigest:    verifyDigest,
		ReleaseID:     release.ID,
		ExpiresAt:     time.Now().Add(finalizeTokenTTL).Unix(),
		Nonce:         randomNonce(),
	})
	if err != nil {
		return nil, componentsdk.ErrServerError(fmt.Sprintf("failed to create finalize token: %v", err))
	}

	return &componentsdk.PullPrepareResponse{
		Download: componentsdk.DownloadInfo{
			URL: packResp.Download.URL,
		},
		Pack: componentsdk.PackResult{
			Digest:    verifyDigest,
			SizeBytes: packResp.SizeBytes,
		},
		FinalizeToken: finalizeToken,
	}, nil
}

func (h *Handler) resolvePullRelease(
	ctx context.Context,
	client locktivity.Client,
	req componentsdk.PullPrepareRequest,
) (*locktivity.ReleaseResponse, error) {
	release, err := h.fetchReleaseForPull(ctx, client, req)
	if err != nil {
		return nil, err
	}
	if release.Pack == nil {
		return nil, componentsdk.ErrServerError("release has no associated pack")
	}
	return release, nil
}

func (h *Handler) fetchReleaseForPull(
	ctx context.Context,
	client locktivity.Client,
	req componentsdk.PullPrepareRequest,
) (*locktivity.ReleaseResponse, error) {
	switch {
	case req.Ref.ReleaseID != "":
		return client.GetRelease(ctx, req.Ref.ReleaseID)
	case req.Ref.Digest != "":
		return client.GetReleaseByDigest(ctx, req.Ref.Digest, req.Target.Environment)
	case req.Ref.Version != "":
		return client.GetReleaseByVersion(ctx, req.Ref.Version, req.Target.Environment)
	default:
		return client.GetLatestRelease(ctx, req.Target.Environment)
	}
}

// PullFinalize handles pull.finalize requests.
func (h *Handler) PullFinalize(req componentsdk.PullFinalizeRequest) (*componentsdk.PullFinalizeResponse, error) {
	var tokenData PullFinalizeTokenData
	if err := h.decodeSignedToken(req.FinalizeToken, &tokenData); err != nil {
		return nil, componentsdk.ErrServerError(fmt.Sprintf("invalid finalize token: %v", err))
	}
	if tokenData.ExpiresAt > 0 && time.Now().Unix() > tokenData.ExpiresAt {
		return nil, componentsdk.ErrServerError("finalize token expired")
	}
	if !h.consumeFinalizeNonce(tokenData.Nonce, tokenData.ExpiresAt) {
		return nil, componentsdk.ErrServerError("finalize token replay detected")
	}
	if req.PackDigest == "" || req.PackDigest != tokenData.FileDigest {
		return nil, componentsdk.ErrServerError("pull finalize digest mismatch")
	}
	if tokenData.FinalizeToken == "" {
		return nil, componentsdk.ErrServerError("pull finalize missing finalize_token")
	}

	ctx := context.Background()
	client, err := h.getClient(ctx)
	if err != nil {
		return nil, componentsdk.ErrAuthRequired(err.Error())
	}
	if _, err := client.ConsumeFinalizeIntent(ctx, locktivity.ConsumeFinalizeIntentRequest{
		FinalizeIntentID: tokenData.FinalizeToken,
	}); err != nil {
		return nil, toRemoteError(err)
	}

	// Download confirmation is enforced by server-side consume semantics.
	return &componentsdk.PullFinalizeResponse{
		Confirmed: true,
	}, nil
}

// RunsSync handles runs.sync requests.
func (h *Handler) RunsSync(req RunsSyncRequest) (*RunsSyncResponse, error) {
	ctx := context.Background()

	client, err := h.getClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth_required: %s", err.Error())
	}

	// Build request with metadata only (no file content)
	syncReq := locktivity.CreatePackRunsRequest{
		FileDigest: req.FileDigest,
		Runs:       make([]locktivity.RunInfo, len(req.Runs)),
	}

	// Track output paths for uploading after we get presigned URLs
	type outputPathInfo struct {
		runIndex    int
		outputIndex int
		localPath   string
		runID       string
		outputPath  string
	}
	var outputPaths []outputPathInfo

	// Track failed outputs to report back
	var failedOutputs []FailedOutput

	for i, run := range req.Runs {
		runInfo := locktivity.RunInfo{
			RunID:        run.RunID,
			ResultPath:   run.ResultPath,
			ResultDigest: run.ResultDigest,
		}

		// Read the result.json file to extract metadata
		fileContent, err := os.ReadFile(run.ResultPath)
		if err != nil {
			slog.Warn("failed to read result.json",
				"result_path", run.ResultPath,
				"run_id", run.RunID,
				"error", err)
			syncReq.Runs[i] = runInfo
			continue
		}

		// Parse the JSON to extract metadata
		var resultData resultJSON
		if err := json.Unmarshal(fileContent, &resultData); err != nil {
			slog.Warn("failed to parse result.json",
				"result_path", run.ResultPath,
				"run_id", run.RunID,
				"error", err)
			syncReq.Runs[i] = runInfo
			continue
		}

		runInfo.ToolName = resultData.Tool.Name
		runInfo.ToolVersion = resultData.Tool.Version
		runInfo.Status = resultData.Status
		runInfo.StartedAt = resultData.StartedAt
		runInfo.CompletedAt = resultData.CompletedAt
		runInfo.DurationMs = resultData.DurationMs
		runInfo.ExitCode = resultData.ExitCode
		runInfo.ToolExitCode = resultData.ToolExitCode
		if len(resultData.Errors) > 0 {
			runInfo.ErrorMessage = resultData.Errors[0].Message
		}

		// Process outputs - send metadata only, no content
		if len(resultData.Outputs) > 0 {
			resultDir := filepath.Dir(run.ResultPath)
			for _, output := range resultData.Outputs {
				localPath, err := safeJoinWithinBase(resultDir, output.Path)
				if err != nil {
					slog.Warn("rejected output path outside result directory",
						"output_rel_path", output.Path,
						"result_dir", resultDir,
						"run_id", run.RunID,
						"error", err)
					failedOutputs = append(failedOutputs, FailedOutput{
						RunID:  run.RunID,
						Path:   output.Path,
						Reason: "path outside result directory",
					})
					continue
				}

				// Check file size before attempting upload
				fileInfo, err := os.Stat(localPath)
				if err != nil {
					slog.Warn("failed to stat output file",
						"path", localPath,
						"error", err)
					failedOutputs = append(failedOutputs, FailedOutput{
						RunID:  run.RunID,
						Path:   output.Path,
						Reason: fmt.Sprintf("failed to read file: %v", err),
					})
					continue
				}
				if fileInfo.Size() > locktivity.MaxRunOutputSize {
					slog.Warn("output file exceeds maximum size",
						"path", localPath,
						"size", fileInfo.Size(),
						"max_size", locktivity.MaxRunOutputSize,
						"run_id", run.RunID)
					failedOutputs = append(failedOutputs, FailedOutput{
						RunID:  run.RunID,
						Path:   output.Path,
						Reason: fmt.Sprintf("file size %d exceeds maximum %d bytes", fileInfo.Size(), locktivity.MaxRunOutputSize),
					})
					continue
				}

				// Compute MD5 checksum for S3 upload verification
				checksum, err := computeMD5Checksum(localPath)
				if err != nil {
					slog.Warn("failed to compute checksum for output",
						"path", localPath,
						"error", err)
					failedOutputs = append(failedOutputs, FailedOutput{
						RunID:  run.RunID,
						Path:   output.Path,
						Reason: fmt.Sprintf("failed to compute checksum: %v", err),
					})
					continue
				}

				outputInfo := locktivity.OutputInfo{
					Path:      output.Path,
					Digest:    output.Digest,
					MediaType: output.MediaType,
					SizeBytes: output.Bytes,
					Checksum:  checksum,
				}
				runInfo.Outputs = append(runInfo.Outputs, outputInfo)

				// Track for later upload
				outputPaths = append(outputPaths, outputPathInfo{
					runIndex:    i,
					outputIndex: len(runInfo.Outputs) - 1,
					localPath:   localPath,
					runID:       run.RunID,
					outputPath:  output.Path,
				})
			}
		}

		syncReq.Runs[i] = runInfo
	}

	// Send metadata to API, get back presigned URLs
	// Retry on rate limiting
	var syncResp *locktivity.PackRunsResponse
	for attempt := 0; attempt <= maxRateLimitRetries; attempt++ {
		syncResp, err = client.SyncRuns(ctx, syncReq)
		if err == nil {
			break
		}

		var rateLimitErr locktivity.ErrRateLimited
		if !errors.As(err, &rateLimitErr) {
			return nil, err
		}

		if attempt == maxRateLimitRetries {
			return nil, err
		}

		wait := parseRetryAfter(rateLimitErr.RetryAfter, rateLimitRetryWait)
		slog.Debug("runs sync rate limited, retrying",
			"attempt", attempt+1,
			"max_attempts", maxRateLimitRetries,
			"wait", wait)
		h.sleepFor(wait)
	}

	// Upload files to presigned URLs and confirm uploads
	for _, pathInfo := range outputPaths {
		if pathInfo.runIndex >= len(syncResp.Runs) {
			continue
		}
		runResp := syncResp.Runs[pathInfo.runIndex]
		if pathInfo.outputIndex >= len(runResp.Outputs) {
			continue
		}
		outputResp := runResp.Outputs[pathInfo.outputIndex]

		if outputResp.UploadURL == "" {
			continue
		}

		f, err := os.Open(pathInfo.localPath)
		if err != nil {
			slog.Warn("failed to open output file for upload",
				"path", pathInfo.localPath,
				"error", err)
			failedOutputs = append(failedOutputs, FailedOutput{
				RunID:  pathInfo.runID,
				Path:   pathInfo.outputPath,
				Reason: fmt.Sprintf("failed to open file: %v", err),
			})
			continue
		}

		if err := client.UploadToPresignedURL(ctx, outputResp.UploadURL, outputResp.UploadHeaders, f); err != nil {
			slog.Warn("failed to upload output file",
				"path", pathInfo.localPath,
				"upload_url", outputResp.UploadURL,
				"error", err)
			_ = f.Close()
			failedOutputs = append(failedOutputs, FailedOutput{
				RunID:  pathInfo.runID,
				Path:   pathInfo.outputPath,
				Reason: fmt.Sprintf("upload failed: %v", err),
			})
			continue
		}
		_ = f.Close()

		// Confirm the upload if we have a token
		if outputResp.UploadToken != "" {
			_, err := client.ConfirmRunOutputUpload(ctx, outputResp.ID, locktivity.ConfirmRunOutputUploadRequest{
				UploadToken: outputResp.UploadToken,
			})
			if err != nil {
				slog.Warn("failed to confirm output upload",
					"output_id", outputResp.ID,
					"path", pathInfo.localPath,
					"error", err)
				failedOutputs = append(failedOutputs, FailedOutput{
					RunID:  pathInfo.runID,
					Path:   pathInfo.outputPath,
					Reason: fmt.Sprintf("confirmation failed: %v", err),
				})
			}
		}
	}

	items := make([]RunSyncItem, len(syncResp.Runs))
	for i, run := range syncResp.Runs {
		items[i] = RunSyncItem{
			RunID:  run.RunID,
			Status: run.Status,
		}
	}

	return &RunsSyncResponse{
		OK:            true,
		Type:          "runs.sync.result",
		RequestID:     req.RequestID,
		Accepted:      syncResp.Accepted,
		Rejected:      syncResp.Rejected,
		Items:         items,
		FailedOutputs: failedOutputs,
	}, nil
}

func safeJoinWithinBase(baseDir, relPath string) (string, error) {
	if relPath == "" {
		return "", fmt.Errorf("empty output path")
	}
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	cleanRel := filepath.Clean(relPath)
	if cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path traversal is not allowed")
	}

	baseClean := filepath.Clean(baseDir)
	joined := filepath.Join(baseClean, cleanRel)
	relToBase, err := filepath.Rel(baseClean, joined)
	if err != nil {
		return "", fmt.Errorf("failed to evaluate output path: %w", err)
	}
	if relToBase == ".." || strings.HasPrefix(relToBase, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes result directory")
	}

	// Resolve symlinks on both sides to prevent escaping baseDir via symlink hops.
	baseResolved, err := filepath.EvalSymlinks(baseClean)
	if err != nil {
		return "", fmt.Errorf("failed to resolve result directory: %w", err)
	}
	joinedResolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", fmt.Errorf("failed to resolve output path: %w", err)
	}

	relResolved, err := filepath.Rel(baseResolved, joinedResolved)
	if err != nil {
		return "", fmt.Errorf("failed to evaluate resolved output path: %w", err)
	}
	if relResolved == ".." || strings.HasPrefix(relResolved, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("resolved path escapes result directory")
	}
	return joinedResolved, nil
}

// resultJSON represents the structure of a result.json file.
type resultJSON struct {
	Tool struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"tool"`
	Status       string `json:"status"`
	StartedAt    string `json:"started_at"`
	CompletedAt  string `json:"completed_at"`
	DurationMs   int64  `json:"duration_ms"`
	ExitCode     int    `json:"exit_code"`
	ToolExitCode int    `json:"tool_exit_code"`
	Errors       []struct {
		Message string `json:"message"`
	} `json:"errors"`
	Outputs []resultOutput `json:"outputs"`
}

// resultOutput represents an output entry in result.json.
type resultOutput struct {
	Path      string `json:"path"`
	MediaType string `json:"media_type"`
	Digest    string `json:"digest"`
	Bytes     int64  `json:"bytes"`
}

// computeMD5Checksum computes the base64-encoded MD5 checksum of a file.
// This is the format required by S3's Content-MD5 header.
func computeMD5Checksum(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := md5.Sum(data)
	return base64.StdEncoding.EncodeToString(sum[:]), nil
}

// AuthLogin handles auth.login requests.
func (h *Handler) AuthLogin(req AuthLoginRequest) (*AuthLoginResponse, error) {
	if !h.allowRateLimited("auth.login", authLoginMinInterval) {
		return nil, componentsdk.ErrRateLimited("auth.login rate limit exceeded")
	}

	ctx := context.Background()

	h.loginMu.Lock()
	if h.loginCancel != nil {
		h.loginCancel()
		h.loginCancel = nil
	}
	h.loginInProgress = false
	h.loginMu.Unlock()

	deviceResp, err := h.oauth.StartDeviceCodeFlow(ctx)
	if err != nil {
		return nil, err
	}

	loginCtx, cancel := context.WithTimeout(context.Background(), time.Duration(deviceResp.ExpiresIn)*time.Second)
	h.loginMu.Lock()
	h.loginSeq++
	seq := h.loginSeq
	h.loginCancel = cancel
	h.loginInProgress = true
	h.loginMu.Unlock()

	go func(deviceCode string, interval int, loginSeq uint64, cancelFn context.CancelFunc) {
		defer cancelFn()
		_ = h.oauth.CompleteDeviceCodeFlow(loginCtx, deviceCode, interval)
		h.loginMu.Lock()
		if h.loginSeq == loginSeq {
			h.loginCancel = nil
		}
		h.loginInProgress = false
		h.loginMu.Unlock()
	}(deviceResp.DeviceCode, deviceResp.Interval, seq, cancel)

	return &AuthLoginResponse{
		OK:        true,
		Type:      "auth.login.result",
		RequestID: req.RequestID,
		Instructions: AuthLoginInstructions{
			UserCode:        deviceResp.UserCode,
			VerificationURI: deviceResp.VerificationURI,
			ExpiresInSecs:   deviceResp.ExpiresIn,
		},
	}, nil
}

// AuthWhoami handles auth.whoami requests.
func (h *Handler) AuthWhoami(req AuthWhoamiRequest) (*AuthWhoamiResponse, error) {
	ctx := context.Background()

	var client locktivity.Client
	if h.client != nil {
		client = h.client
	} else {
		var token string
		var err error

		if h.keychain != nil {
			token, _ = h.keychain.GetToken()
		}

		if token == "" && h.oauth != nil {
			token, err = h.oauth.GetToken(ctx)
		}

		if err != nil || token == "" {
			return &AuthWhoamiResponse{
				OK:        true,
				Type:      "auth.whoami.result",
				RequestID: req.RequestID,
				Identity: IdentityResult{
					Authenticated: false,
				},
			}, nil
		}

		client = locktivity.NewClient(h.endpoint, token)
	}

	identity, err := client.GetIdentity(ctx)
	if err != nil {
		return &AuthWhoamiResponse{
			OK:        true,
			Type:      "auth.whoami.result",
			RequestID: req.RequestID,
			Identity: IdentityResult{
				Authenticated: false,
			},
		}, nil
	}

	return &AuthWhoamiResponse{
		OK:        true,
		Type:      "auth.whoami.result",
		RequestID: req.RequestID,
		Identity: IdentityResult{
			Authenticated: identity.Authenticated,
			Subject:       identity.Subject,
		},
	}, nil
}

// toRemoteError converts an error to a componentsdk.RemoteError.
func toRemoteError(err error) componentsdk.RemoteError {
	// Check for typed errors first
	var rateLimitErr locktivity.ErrRateLimited
	if errors.As(err, &rateLimitErr) {
		return componentsdk.ErrRateLimited(rateLimitErr.Error())
	}

	errStr := err.Error()

	// Check for common error patterns (case-insensitive for error codes)
	errLower := strings.ToLower(errStr)
	switch {
	case containsAny(errLower, "unauthorized", "auth_required"):
		return componentsdk.ErrAuthRequired(errStr)
	case containsAny(errLower, "forbidden", "product_not_enabled", "quarantine", "pack_quarantined", "finalize_token_redemption_denied"):
		return componentsdk.ErrForbidden(errStr)
	case containsAny(errLower, "not_found", "not found", "no releases found"):
		return componentsdk.ErrNotFound(errStr)
	case containsAny(errLower, "conflict", "finalize_token_consumed"):
		return componentsdk.ErrConflict(errStr)
	case containsAny(errLower, "rate_limit"):
		return componentsdk.ErrRateLimited(errStr)
	case containsAny(errLower, "finalize_token_expired"):
		return componentsdk.ErrServerError(errStr) // Treat as retriable error
	case containsAny(errLower, "finalize_token_invalid"):
		return componentsdk.ErrServerError(errStr) // Invalid token, likely client bug
	default:
		return componentsdk.ErrServerError(errStr)
	}
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// parseRetryAfter parses the Retry-After header value (seconds) and returns a duration.
// Falls back to defaultWait if parsing fails or the value is invalid.
func parseRetryAfter(retryAfter string, defaultWait time.Duration) time.Duration {
	if retryAfter == "" {
		return defaultWait
	}
	// Try parsing as seconds (most common for rate limiting)
	var seconds int
	if _, err := fmt.Sscanf(retryAfter, "%d", &seconds); err == nil && seconds > 0 {
		wait := time.Duration(seconds) * time.Second
		// Cap at reasonable maximum
		if wait > 60*time.Second {
			wait = 60 * time.Second
		}
		return wait
	}
	return defaultWait
}

func (h *Handler) sleepFor(wait time.Duration) {
	if h.sleep != nil {
		h.sleep(wait)
		return
	}
	time.Sleep(wait)
}
