package auth

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/locktivity/epack-remote-locktivity/internal/locktivity"
	"github.com/zalando/go-keyring"
)

// Keychain provides secure token storage.
type Keychain interface {
	// GetToken retrieves the stored access token.
	GetToken() (string, error)

	// SetToken stores an access token.
	SetToken(token string) error

	// GetRefreshToken retrieves the stored refresh token.
	GetRefreshToken() (string, error)

	// SetRefreshToken stores a refresh token.
	SetRefreshToken(token string) error

	// GetTokenExpiry returns Unix time when access token expires (0 if unknown).
	GetTokenExpiry() (int64, error)

	// SetTokenExpiry stores access token expiry as Unix time.
	SetTokenExpiry(unix int64) error

	// GetClientID retrieves the client_id that was used to obtain the stored token.
	// Returns empty string if no client_id was stored (e.g., device code flow).
	GetClientID() (string, error)

	// SetClientID stores the client_id associated with the stored token.
	SetClientID(clientID string) error

	// Clear removes all stored tokens.
	Clear() error
}

// OSKeychain implements Keychain using the OS keychain.
type OSKeychain struct {
	service   string
	namespace string
}

// Ensure OSKeychain implements Keychain.
var _ Keychain = (*OSKeychain)(nil)

// NewOSKeychain creates a new OS keychain accessor scoped to an auth endpoint.
func NewOSKeychain(authEndpoint string) *OSKeychain {
	return &OSKeychain{
		service:   locktivity.KeychainService,
		namespace: keyNamespace(authEndpoint),
	}
}

func keyNamespace(authEndpoint string) string {
	u, err := url.Parse(authEndpoint)
	if err != nil || u.Host == "" {
		return "default"
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "" {
		scheme = "https"
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "default"
	}
	port := u.Port()
	if port == "" {
		switch scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return fmt.Sprintf("%s|%s|%s", scheme, host, port)
}

func (k *OSKeychain) keyName(base string) string {
	return k.namespace + ":" + base
}

// GetToken retrieves the stored access token.
func (k *OSKeychain) GetToken() (string, error) {
	return keyring.Get(k.service, k.keyName("access_token"))
}

// SetToken stores an access token.
func (k *OSKeychain) SetToken(token string) error {
	return keyring.Set(k.service, k.keyName("access_token"), token)
}

// GetRefreshToken retrieves the stored refresh token.
func (k *OSKeychain) GetRefreshToken() (string, error) {
	return keyring.Get(k.service, k.keyName("refresh_token"))
}

// SetRefreshToken stores a refresh token.
func (k *OSKeychain) SetRefreshToken(token string) error {
	return keyring.Set(k.service, k.keyName("refresh_token"), token)
}

// GetTokenExpiry retrieves the stored access token expiry unix timestamp.
func (k *OSKeychain) GetTokenExpiry() (int64, error) {
	raw, err := keyring.Get(k.service, k.keyName("access_token_expiry"))
	if err != nil {
		return 0, err
	}
	var unix int64
	if _, err := fmt.Sscanf(raw, "%d", &unix); err != nil {
		return 0, err
	}
	return unix, nil
}

// SetTokenExpiry stores an access token expiry unix timestamp.
func (k *OSKeychain) SetTokenExpiry(unix int64) error {
	return keyring.Set(k.service, k.keyName("access_token_expiry"), fmt.Sprintf("%d", unix))
}

// GetClientID retrieves the client_id that was used to obtain the stored token.
func (k *OSKeychain) GetClientID() (string, error) {
	return keyring.Get(k.service, k.keyName("client_id"))
}

// SetClientID stores the client_id associated with the stored token.
func (k *OSKeychain) SetClientID(clientID string) error {
	if clientID == "" {
		// Clear client_id when empty (e.g., device code flow)
		_ = keyring.Delete(k.service, k.keyName("client_id"))
		return nil
	}
	return keyring.Set(k.service, k.keyName("client_id"), clientID)
}

// Clear removes all stored tokens.
func (k *OSKeychain) Clear() error {
	// Delete access token (ignore errors if not found)
	_ = keyring.Delete(k.service, k.keyName("access_token"))
	_ = keyring.Delete(k.service, k.keyName("refresh_token"))
	_ = keyring.Delete(k.service, k.keyName("access_token_expiry"))
	_ = keyring.Delete(k.service, k.keyName("client_id"))
	return nil
}

// MemoryKeychain implements Keychain in memory (for testing).
type MemoryKeychain struct {
	accessToken  string
	refreshToken string
	tokenExpiry  int64
	clientID     string
}

// Ensure MemoryKeychain implements Keychain.
var _ Keychain = (*MemoryKeychain)(nil)

// NewMemoryKeychain creates a new in-memory keychain.
func NewMemoryKeychain() *MemoryKeychain {
	return &MemoryKeychain{}
}

// GetToken retrieves the stored access token.
func (k *MemoryKeychain) GetToken() (string, error) {
	if k.accessToken == "" {
		return "", keyring.ErrNotFound
	}
	return k.accessToken, nil
}

// SetToken stores an access token.
func (k *MemoryKeychain) SetToken(token string) error {
	k.accessToken = token
	return nil
}

// GetRefreshToken retrieves the stored refresh token.
func (k *MemoryKeychain) GetRefreshToken() (string, error) {
	if k.refreshToken == "" {
		return "", keyring.ErrNotFound
	}
	return k.refreshToken, nil
}

// SetRefreshToken stores a refresh token.
func (k *MemoryKeychain) SetRefreshToken(token string) error {
	k.refreshToken = token
	return nil
}

// GetTokenExpiry retrieves the stored access token expiry unix timestamp.
func (k *MemoryKeychain) GetTokenExpiry() (int64, error) {
	if k.tokenExpiry == 0 {
		return 0, keyring.ErrNotFound
	}
	return k.tokenExpiry, nil
}

// SetTokenExpiry stores an access token expiry unix timestamp.
func (k *MemoryKeychain) SetTokenExpiry(unix int64) error {
	k.tokenExpiry = unix
	return nil
}

// GetClientID retrieves the client_id that was used to obtain the stored token.
func (k *MemoryKeychain) GetClientID() (string, error) {
	if k.clientID == "" {
		return "", keyring.ErrNotFound
	}
	return k.clientID, nil
}

// SetClientID stores the client_id associated with the stored token.
func (k *MemoryKeychain) SetClientID(clientID string) error {
	k.clientID = clientID
	return nil
}

// Clear removes all stored tokens.
func (k *MemoryKeychain) Clear() error {
	k.accessToken = ""
	k.refreshToken = ""
	k.tokenExpiry = 0
	k.clientID = ""
	return nil
}
