package securitypolicy

import (
	"fmt"
	"os"
	"strings"
)

// StrictProductionEnvVar blocks insecure runtime overrides when set.
const StrictProductionEnvVar = "EPACK_STRICT_PRODUCTION"

// EnforceStrictProduction rejects insecure runtime overrides in strict production mode.
func EnforceStrictProduction(component string, hasUnsafeOverride bool) error {
	if !hasUnsafeOverride {
		return nil
	}
	if !strictProductionEnabled() {
		return nil
	}
	return fmt.Errorf("strict production mode forbids insecure execution overrides (%s)", component)
}

func strictProductionEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(StrictProductionEnvVar))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
