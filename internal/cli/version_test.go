package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/version"
)

func TestCanonicalVersionHumanAndJSONContracts(t *testing.T) {
	t.Parallel()

	var human, stderr bytes.Buffer
	if exitCode := Run(context.Background(), []string{"version"}, &human, &stderr); exitCode != 0 {
		t.Fatalf("version exit = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, expected := range []string{
		"Telemetry Change Guard\n",
		"Version: dev\n",
		"Commit: unknown\n",
		"Build date: unknown\n",
		"Dirty: unknown\n",
		"Go version: " + runtime.Version() + "\n",
		"Platform: " + runtime.GOOS + "/" + runtime.GOARCH + "\n",
	} {
		if !strings.Contains(human.String(), expected) {
			t.Errorf("human version missing %q:\n%s", expected, human.String())
		}
	}

	var machine bytes.Buffer
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"version", "--format", "json"}, &machine, &stderr); exitCode != 0 {
		t.Fatalf("JSON version exit = %d, stderr = %q", exitCode, stderr.String())
	}
	var info version.Info
	if err := json.Unmarshal(machine.Bytes(), &info); err != nil {
		t.Fatalf("decode version JSON: %v\n%s", err, machine.String())
	}
	if info.SchemaVersion != version.SchemaVersion || info.Version != "dev" || info.Commit != "unknown" ||
		info.BuildDate != "unknown" || info.Dirty != nil || info.GoVersion != runtime.Version() ||
		info.OS != runtime.GOOS || info.Arch != runtime.GOARCH {
		t.Fatalf("version JSON = %#v", info)
	}
}

func TestCanonicalVersionFlagMatchesSubcommand(t *testing.T) {
	t.Parallel()

	var fromCommand, fromFlag, stderr bytes.Buffer
	if exitCode := Run(context.Background(), []string{"version"}, &fromCommand, &stderr); exitCode != 0 {
		t.Fatalf("version exit = %d, stderr = %q", exitCode, stderr.String())
	}
	stderr.Reset()
	if exitCode := Run(context.Background(), []string{"--version"}, &fromFlag, &stderr); exitCode != 0 {
		t.Fatalf("--version exit = %d, stderr = %q", exitCode, stderr.String())
	}
	if fromCommand.String() != fromFlag.String() {
		t.Fatalf("version output differs:\ncommand: %s\nflag: %s", fromCommand.String(), fromFlag.String())
	}
}

func TestVersionRejectsInvalidArguments(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"version", "--format", "yaml"},
		{"version", "unexpected"},
		{"--version", "unexpected"},
	} {
		var stdout, stderr bytes.Buffer
		if exitCode := Run(context.Background(), args, &stdout, &stderr); exitCode != 1 || stdout.Len() != 0 {
			t.Errorf("Run(%q) exit/stdout = %d/%q, stderr = %q", args, exitCode, stdout.String(), stderr.String())
		}
	}
}

func TestCompatibilityVersionUsesSharedIdentity(t *testing.T) {
	t.Parallel()

	var canonical, compatibility, stderr bytes.Buffer
	if exitCode := Run(context.Background(), []string{"version", "--format", "json"}, &canonical, &stderr); exitCode != 0 {
		t.Fatalf("canonical version exit = %d, stderr = %q", exitCode, stderr.String())
	}
	stderr.Reset()
	if exitCode := RunCompatibility(context.Background(), []string{"version", "--format", "json"}, &compatibility, &stderr); exitCode != 0 {
		t.Fatalf("compatibility version exit = %d, stderr = %q", exitCode, stderr.String())
	}
	if canonical.String() != compatibility.String() {
		t.Fatalf("version identities differ:\ncanonical: %s\ncompatibility: %s", canonical.String(), compatibility.String())
	}
}

func TestVersionRenderersHaveStableGoldenOutput(t *testing.T) {
	t.Parallel()

	dirty := false
	info := version.Info{
		SchemaVersion: version.SchemaVersion,
		Version:       "0.1.0-alpha.1",
		Commit:        "0123456789abcdef0123456789abcdef01234567",
		BuildDate:     "2026-08-25T00:00:00Z",
		Dirty:         &dirty,
		GoVersion:     "go1.27.0",
		OS:            "linux",
		Arch:          "amd64",
	}

	var human bytes.Buffer
	renderVersionText(&human, info)
	wantHuman := `Telemetry Change Guard
Version: 0.1.0-alpha.1
Commit: 0123456789abcdef0123456789abcdef01234567
Build date: 2026-08-25T00:00:00Z
Dirty: false
Go version: go1.27.0
Platform: linux/amd64
`
	if human.String() != wantHuman {
		t.Fatalf("human version:\n%s\nwant:\n%s", human.String(), wantHuman)
	}

	var machine bytes.Buffer
	if err := renderVersionJSON(&machine, info); err != nil {
		t.Fatal(err)
	}
	wantJSON := `{
  "schemaVersion": "tcg-version/v1alpha1",
  "version": "0.1.0-alpha.1",
  "commit": "0123456789abcdef0123456789abcdef01234567",
  "buildDate": "2026-08-25T00:00:00Z",
  "dirty": false,
  "goVersion": "go1.27.0",
  "os": "linux",
  "arch": "amd64"
}
`
	if machine.String() != wantJSON {
		t.Fatalf("JSON version:\n%s\nwant:\n%s", machine.String(), wantJSON)
	}
}
