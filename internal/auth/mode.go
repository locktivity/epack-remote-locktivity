package auth

import (
	"fmt"
	"os"
	"strings"

	"github.com/locktivity/epack-remote-locktivity/internal/locktivity"
)

const (
	AuthModeAll                   = "all"
	AuthModeClientCredentialsOnly = "client_credentials_only"
)

// EffectiveAuthMode resolves the active auth mode from environment.
func EffectiveAuthMode() (string, error) {
	mode := strings.TrimSpace(os.Getenv(locktivity.EnvAuthMode))
	if mode == "" {
		// Default to the safest/most constrained production behavior.
		return AuthModeClientCredentialsOnly, nil
	}

	switch mode {
	case AuthModeAll, AuthModeClientCredentialsOnly:
		return mode, nil
	default:
		return "", fmt.Errorf(
			"invalid %s=%q (expected %q or %q)",
			locktivity.EnvAuthMode,
			mode,
			AuthModeAll,
			AuthModeClientCredentialsOnly,
		)
	}
}
