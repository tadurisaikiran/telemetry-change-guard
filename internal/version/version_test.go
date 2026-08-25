package version

import (
	"runtime"
	"testing"
)

func TestCurrentDevelopmentIdentityIsExplicit(t *testing.T) {
	t.Parallel()

	info := Current()
	if info.SchemaVersion != SchemaVersion || info.Version != "dev" || info.Commit != "unknown" ||
		info.BuildDate != "unknown" || info.Dirty != nil {
		t.Fatalf("development info = %#v", info)
	}
	if info.GoVersion != runtime.Version() || info.OS != runtime.GOOS || info.Arch != runtime.GOARCH {
		t.Fatalf("runtime identity = %#v", info)
	}
}

func TestNewInfoNormalizesLinkerValues(t *testing.T) {
	t.Parallel()

	info := newInfo(
		" 0.1.0-alpha.1 ",
		" 0123456789abcdef0123456789abcdef01234567 ",
		" 2026-08-25T00:00:00Z ",
		" false ",
		" go1.27.0 ",
		" linux ",
		" arm64 ",
	)
	if info.Version != "0.1.0-alpha.1" || info.Commit != "0123456789abcdef0123456789abcdef01234567" ||
		info.BuildDate != "2026-08-25T00:00:00Z" || info.Dirty == nil || *info.Dirty ||
		info.GoVersion != "go1.27.0" || info.OS != "linux" || info.Arch != "arm64" {
		t.Fatalf("normalized info = %#v", info)
	}
	if info.DirtyText() != "false" {
		t.Fatalf("dirty text = %q", info.DirtyText())
	}
}

func TestNewInfoPreservesUnknownDirtyState(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "unknown", "not-a-boolean"} {
		info := newInfo("", "", "", value, "", "", "")
		if info.Version != "dev" || info.Commit != "unknown" || info.BuildDate != "unknown" ||
			info.Dirty != nil || info.DirtyText() != "unknown" {
			t.Errorf("newInfo dirty %q = %#v", value, info)
		}
	}
}

func TestNewInfoRejectsMalformedReleaseIdentity(t *testing.T) {
	t.Parallel()

	info := newInfo("main", "short-commit", "yesterday", "false", "go1.27.0", "linux", "amd64")
	if info.Version != "dev" || info.Commit != "unknown" || info.BuildDate != "unknown" ||
		info.Dirty == nil || *info.Dirty {
		t.Fatalf("malformed linker identity = %#v", info)
	}
}

func TestEffectiveVersionUsesImmutableModuleVersionForGoInstall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		linked string
		module string
		want   string
	}{
		{name: "release linker metadata wins", linked: "0.1.0-alpha.1", module: "v9.9.9", want: "0.1.0-alpha.1"},
		{name: "tagged module", linked: "dev", module: "v0.1.0-alpha.1", want: "0.1.0-alpha.1"},
		{name: "pseudo version", linked: "dev", module: "v0.0.0-20260825120000-0123456789ab", want: "0.0.0-20260825120000-0123456789ab"},
		{name: "local checkout", linked: "dev", module: "(devel)", want: "dev"},
		{name: "malformed module", linked: "dev", module: "latest", want: "dev"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := effectiveVersion(test.linked, test.module); got != test.want {
				t.Fatalf("effectiveVersion(%q, %q) = %q; want %q", test.linked, test.module, got, test.want)
			}
		})
	}
}
