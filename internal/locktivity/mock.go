package locktivity

import (
	"context"
	"io"
	"time"
)

// MockClient implements Client for testing.
type MockClient struct {
	// Response stubs
	CreatePackResponse             *CreatePackResponse
	CreatePackError                error
	GetPackResponse                *GetPackResponse
	GetPackError                   error
	CreateFinalizeIntentResponse   *CreateFinalizeIntentResponse
	CreateFinalizeIntentError      error
	ConsumeFinalizeIntentResponse  *ConsumeFinalizeIntentResponse
	ConsumeFinalizeIntentError     error
	CreateReleaseResponse          *ReleaseResponse
	CreateReleaseError             error
	GetReleaseResponse             *ReleaseResponse
	GetReleaseError                error
	GetLatestReleaseResponse       *ReleaseResponse
	GetLatestReleaseError          error
	GetVersionReleaseResponse      *ReleaseResponse
	GetVersionReleaseError         error
	GetDigestReleaseResponse       *ReleaseResponse
	GetDigestReleaseError          error
	SyncRunsResponse               *PackRunsResponse
	SyncRunsError                  error
	ConfirmRunOutputUploadResponse *ConfirmRunOutputUploadResponse
	ConfirmRunOutputUploadError    error
	GetIdentityResponse            *IdentityResponse
	GetIdentityError               error

	// Call tracking
	CreatePackCalls             []CreatePackRequest
	GetPackCalls                []string
	CreateFinalizeIntentCalls   []CreateFinalizeIntentRequest
	ConsumeFinalizeIntentCalls  []ConsumeFinalizeIntentRequest
	CreateReleaseCalls          []CreateReleaseRequest
	GetReleaseCalls             []string
	GetLatestReleaseCalls       []GetLatestReleaseCall
	GetVersionReleaseCalls      []GetVersionReleaseCall
	GetDigestReleaseCalls       []GetDigestReleaseCall
	SyncRunsCalls               []CreatePackRunsRequest
	ConfirmRunOutputUploadCalls []ConfirmRunOutputUploadCall
	GetIdentityCalls            int
	UploadCalls                 []UploadCall
}

type ConfirmRunOutputUploadCall struct {
	OutputID string
	Request  ConfirmRunOutputUploadRequest
}

type UploadCall struct {
	URL     string
	Headers map[string]string
	Content []byte
}

type GetLatestReleaseCall struct {
	Environment string
}

type GetVersionReleaseCall struct {
	Version     string
	Environment string
}

type GetDigestReleaseCall struct {
	Digest      string
	Environment string
}

// Ensure MockClient implements Client.
var _ Client = (*MockClient)(nil)

// NewMockClient creates a new mock client with default responses.
func NewMockClient() *MockClient {
	return &MockClient{
		CreatePackResponse: &CreatePackResponse{
			ID:         "pack_123",
			FileDigest: "sha256:abc123",
			SizeBytes:  1024,
			Exists:     false,
			Upload: &UploadInfo{
				URL:           "https://storage.example.com/upload",
				UploadToken:   "upload_token_123",
				FinalizeToken: "fin_123",
			},
		},
		GetPackResponse: &GetPackResponse{
			ID:         "pack_123",
			PackDigest: "sha256:abc123",
			SizeBytes:  1024,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
			Download: &DownloadInfo{
				URL: "https://storage.example.com/download",
			},
		},
		CreateFinalizeIntentResponse: &CreateFinalizeIntentResponse{
			FinalizeToken: "fin_pull_123",
			PackID:        "pack_123",
			ExpiresAt:     time.Now().Add(15 * time.Minute),
		},
		ConsumeFinalizeIntentResponse: &ConsumeFinalizeIntentResponse{},
		CreateReleaseResponse: &ReleaseResponse{
			ID: "rel_123",
			Pack: &PackInfo{
				ID: "pack_123",
			},
		},
		GetLatestReleaseResponse: &ReleaseResponse{
			ID: "rel_123",
			Pack: &PackInfo{
				ID: "pack_123",
			},
		},
		GetVersionReleaseResponse: &ReleaseResponse{
			ID:      "rel_123",
			Version: "v1.0.0",
			Pack: &PackInfo{
				ID: "pack_123",
			},
		},
		GetDigestReleaseResponse: &ReleaseResponse{
			ID: "rel_123",
			Pack: &PackInfo{
				ID: "pack_123",
			},
		},
		SyncRunsResponse: &PackRunsResponse{
			Accepted: 1,
			Rejected: 0,
			Runs: []PackRunInfo{
				{ID: "run_123", RunID: "run_123", Status: "accepted", CreatedAt: time.Now()},
			},
		},
		GetIdentityResponse: &IdentityResponse{
			Authenticated: true,
			Subject:       "user@example.com",
			Scopes:        []string{"read:evidence_packs", "write:evidence_packs"},
		},
	}
}

func (m *MockClient) CreatePack(ctx context.Context, req CreatePackRequest) (*CreatePackResponse, error) {
	m.CreatePackCalls = append(m.CreatePackCalls, req)
	if m.CreatePackError != nil {
		return nil, m.CreatePackError
	}
	return m.CreatePackResponse, nil
}

func (m *MockClient) GetPack(ctx context.Context, packID string) (*GetPackResponse, error) {
	m.GetPackCalls = append(m.GetPackCalls, packID)
	if m.GetPackError != nil {
		return nil, m.GetPackError
	}
	return m.GetPackResponse, nil
}

func (m *MockClient) CreateFinalizeIntent(ctx context.Context, req CreateFinalizeIntentRequest) (*CreateFinalizeIntentResponse, error) {
	m.CreateFinalizeIntentCalls = append(m.CreateFinalizeIntentCalls, req)
	if m.CreateFinalizeIntentError != nil {
		return nil, m.CreateFinalizeIntentError
	}
	return m.CreateFinalizeIntentResponse, nil
}

func (m *MockClient) ConsumeFinalizeIntent(ctx context.Context, req ConsumeFinalizeIntentRequest) (*ConsumeFinalizeIntentResponse, error) {
	m.ConsumeFinalizeIntentCalls = append(m.ConsumeFinalizeIntentCalls, req)
	if m.ConsumeFinalizeIntentError != nil {
		return nil, m.ConsumeFinalizeIntentError
	}
	return m.ConsumeFinalizeIntentResponse, nil
}

func (m *MockClient) CreateRelease(ctx context.Context, req CreateReleaseRequest) (*ReleaseResponse, error) {
	m.CreateReleaseCalls = append(m.CreateReleaseCalls, req)
	if m.CreateReleaseError != nil {
		return nil, m.CreateReleaseError
	}
	return m.CreateReleaseResponse, nil
}

func (m *MockClient) GetRelease(ctx context.Context, releaseID string) (*ReleaseResponse, error) {
	m.GetReleaseCalls = append(m.GetReleaseCalls, releaseID)
	if m.GetReleaseError != nil {
		return nil, m.GetReleaseError
	}
	return m.GetReleaseResponse, nil
}

func (m *MockClient) GetLatestRelease(ctx context.Context, environment string) (*ReleaseResponse, error) {
	m.GetLatestReleaseCalls = append(m.GetLatestReleaseCalls, GetLatestReleaseCall{
		Environment: environment,
	})
	if m.GetLatestReleaseError != nil {
		return nil, m.GetLatestReleaseError
	}
	return m.GetLatestReleaseResponse, nil
}

func (m *MockClient) GetReleaseByVersion(ctx context.Context, version, environment string) (*ReleaseResponse, error) {
	m.GetVersionReleaseCalls = append(m.GetVersionReleaseCalls, GetVersionReleaseCall{
		Version:     version,
		Environment: environment,
	})
	if m.GetVersionReleaseError != nil {
		return nil, m.GetVersionReleaseError
	}
	return m.GetVersionReleaseResponse, nil
}

func (m *MockClient) GetReleaseByDigest(ctx context.Context, digest, environment string) (*ReleaseResponse, error) {
	m.GetDigestReleaseCalls = append(m.GetDigestReleaseCalls, GetDigestReleaseCall{
		Digest:      digest,
		Environment: environment,
	})
	if m.GetDigestReleaseError != nil {
		return nil, m.GetDigestReleaseError
	}
	return m.GetDigestReleaseResponse, nil
}

func (m *MockClient) SyncRuns(ctx context.Context, req CreatePackRunsRequest) (*PackRunsResponse, error) {
	m.SyncRunsCalls = append(m.SyncRunsCalls, req)
	if m.SyncRunsError != nil {
		return nil, m.SyncRunsError
	}
	return m.SyncRunsResponse, nil
}

func (m *MockClient) GetIdentity(ctx context.Context) (*IdentityResponse, error) {
	m.GetIdentityCalls++
	if m.GetIdentityError != nil {
		return nil, m.GetIdentityError
	}
	return m.GetIdentityResponse, nil
}

func (m *MockClient) UploadToPresignedURL(ctx context.Context, uploadURL string, headers map[string]string, content io.Reader) error {
	data, _ := io.ReadAll(content)
	m.UploadCalls = append(m.UploadCalls, UploadCall{
		URL:     uploadURL,
		Headers: headers,
		Content: data,
	})
	return nil
}

func (m *MockClient) ConfirmRunOutputUpload(ctx context.Context, outputID string, req ConfirmRunOutputUploadRequest) (*ConfirmRunOutputUploadResponse, error) {
	m.ConfirmRunOutputUploadCalls = append(m.ConfirmRunOutputUploadCalls, ConfirmRunOutputUploadCall{
		OutputID: outputID,
		Request:  req,
	})
	if m.ConfirmRunOutputUploadError != nil {
		return nil, m.ConfirmRunOutputUploadError
	}
	if m.ConfirmRunOutputUploadResponse != nil {
		return m.ConfirmRunOutputUploadResponse, nil
	}
	return &ConfirmRunOutputUploadResponse{
		ID:         outputID,
		Path:       "output.json",
		ScanStatus: "clean",
	}, nil
}
