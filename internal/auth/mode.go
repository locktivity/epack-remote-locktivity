package auth

import (
	"fmt"
	"os"
	"strings"

	"github.com/locktivity/epack-remote-locktivity/internal/locktivity"
)

const (
	// AuthModeAuto is the default: environment credentials first, then the
	// session a person stored by signing in. Browser sign-in only starts when
	// asked for explicitly, never from a push.
	AuthModeAuto                  = "auto"
	AuthModeAll                   = "all"
	AuthModeClientCredentialsOnly = "client_credentials_only"
)

// EffectiveAuthMode resolves the active auth mode from environment.
func EffectiveAuthMode() (string, error) {
	mode := strings.TrimSpace(os.Getenv(locktivity.EnvAuthMode))
	if mode == "" {
		return AuthModeAuto, nil
	}

	switch mode {
	case AuthModeAuto, AuthModeAll, AuthModeClientCredentialsOnly:
		return mode, nil
	default:
		return "", fmt.Errorf(
			"invalid %s=%q (expected %q, %q, or %q)",
			locktivity.EnvAuthMode,
			mode,
			AuthModeAuto,
			AuthModeAll,
			AuthModeClientCredentialsOnly,
		)
	}
}
