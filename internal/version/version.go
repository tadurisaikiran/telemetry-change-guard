// Package version exposes deterministic build identity for the CLI and
// release-verification tooling.
package version

import (
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

const SchemaVersion = "tcg-version/v1alpha1"

// These values are intentionally plain strings so release builds can inject
// them with -ldflags -X. Development builds remain explicit and honest.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
	Dirty   = "unknown"
)

var (
	semanticVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	commitPattern          = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// Info is the stable machine-readable build identity contract.
// Dirty is null only when the build process did not provide an authoritative
// clean/dirty state.
type Info struct {
	SchemaVersion string `json:"schemaVersion"`
	Version       string `json:"version"`
	Commit        string `json:"commit"`
	BuildDate     string `json:"buildDate"`
	Dirty         *bool  `json:"dirty"`
	GoVersion     string `json:"goVersion"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
}

// Current returns the identity embedded in the running executable.
func Current() Info {
	return newInfo(effectiveVersion(Version, moduleVersion()), Commit, Date, Dirty, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// effectiveVersion retains authoritative linker metadata for release builds.
// A binary installed with `go install module/cmd@version` has no release
// linker flags, so its immutable module version is the next-best identity.
func effectiveVersion(linked, module string) string {
	if strings.TrimSpace(linked) != "dev" {
		return linked
	}
	module = strings.TrimPrefix(strings.TrimSpace(module), "v")
	if semanticVersionPattern.MatchString(module) {
		return module
	}
	return linked
}

func moduleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return info.Main.Version
}

// DirtyText renders the tri-state dirty value for human-readable output.
func (info Info) DirtyText() string {
	if info.Dirty == nil {
		return "unknown"
	}
	return strconv.FormatBool(*info.Dirty)
}

func newInfo(version, commit, date, dirty, goVersion, operatingSystem, architecture string) Info {
	return Info{
		SchemaVersion: SchemaVersion,
		Version:       normalizeVersion(version),
		Commit:        normalizeCommit(commit),
		BuildDate:     normalizeDate(date),
		Dirty:         parseDirty(dirty),
		GoVersion:     valueOrUnknown(goVersion, "unknown"),
		OS:            valueOrUnknown(operatingSystem, "unknown"),
		Arch:          valueOrUnknown(architecture, "unknown"),
	}
}

func normalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "dev" || semanticVersionPattern.MatchString(value) {
		return value
	}
	return "dev"
}

func normalizeCommit(value string) string {
	value = strings.TrimSpace(value)
	if commitPattern.MatchString(value) {
		return value
	}
	return "unknown"
}

func normalizeDate(value string) string {
	value = strings.TrimSpace(value)
	if _, err := time.Parse(time.RFC3339, value); err == nil {
		return value
	}
	return "unknown"
}

func valueOrUnknown(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func parseDirty(value string) *bool {
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	return &parsed
}
