package cli

import (
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/config"
	remoteurl "github.com/tadurisaikiran/telemetry-change-guard/internal/remote"
)

type remoteEvidenceFlags struct {
	mode                  *string
	allowedOrigins        stringListFlag
	allowInsecureLoopback *bool
}

func addRemoteEvidenceFlags(flags *flag.FlagSet) *remoteEvidenceFlags {
	values := &remoteEvidenceFlags{}
	values.mode = flags.String(
		"remote-evidence",
		config.RemoteEvidenceEnabled,
		"remote evidence policy: disabled or enabled",
	)
	flags.Var(
		&values.allowedOrigins,
		"allowed-remote-origin",
		"exact trusted remote origin, for example https://tempo.example.com (repeatable; required for credentials)",
	)
	values.allowInsecureLoopback = flags.Bool(
		"allow-insecure-loopback",
		false,
		"allow bearer authentication over HTTP only to an exact loopback development endpoint",
	)
	return values
}

func (values *remoteEvidenceFlags) apply(configuration *config.Config) error {
	mode := strings.TrimSpace(*values.mode)
	if mode != config.RemoteEvidenceEnabled && mode != config.RemoteEvidenceDisabled {
		return fmt.Errorf("--remote-evidence must be %q or %q", config.RemoteEvidenceDisabled, config.RemoteEvidenceEnabled)
	}
	if mode == config.RemoteEvidenceDisabled && (len(values.allowedOrigins) != 0 || *values.allowInsecureLoopback) {
		return fmt.Errorf("--allowed-remote-origin and --allow-insecure-loopback require --remote-evidence enabled")
	}
	origins := make([]string, 0, len(values.allowedOrigins))
	seen := make(map[string]struct{}, len(values.allowedOrigins))
	for _, configured := range values.allowedOrigins {
		origin, err := remoteurl.ParseAllowedOrigin(configured)
		if err != nil {
			return fmt.Errorf("invalid --allowed-remote-origin %q: %w", configured, err)
		}
		if _, exists := seen[origin]; exists {
			continue
		}
		seen[origin] = struct{}{}
		origins = append(origins, origin)
	}
	sort.Strings(origins)
	configuration.RemoteEvidence = config.RemoteEvidencePolicy{
		Mode:                  mode,
		AllowedOrigins:        origins,
		AllowInsecureLoopback: *values.allowInsecureLoopback,
	}
	return nil
}
