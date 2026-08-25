package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/version"
)

func TestReleaseLinkerMetadataAndTrimpath(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binaryName := "telemetry-change-guard"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(t.TempDir(), binaryName)
	commit := "0123456789abcdef0123456789abcdef01234567"
	buildDate := "2026-08-25T00:00:00Z"
	packagePath := "github.com/tadurisaikiran/telemetry-change-guard/internal/version"
	ldflags := strings.Join([]string{
		"-X", packagePath + ".Version=0.1.0-alpha.1",
		"-X", packagePath + ".Commit=" + commit,
		"-X", packagePath + ".Date=" + buildDate,
		"-X", packagePath + ".Dirty=false",
	}, " ")

	goBinary := filepath.Join(runtime.GOROOT(), "bin", "go")
	build := exec.Command(goBinary, "build", "-buildvcs=false", "-trimpath", "-ldflags", ldflags, "-o", binaryPath, "./cmd/telemetry-change-guard")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build release-shaped binary: %v\n%s", err, output)
	}

	machine := exec.Command(binaryPath, "version", "--format", "json")
	output, err := machine.Output()
	if err != nil {
		t.Fatalf("run version JSON: %v", err)
	}
	var info version.Info
	if err := json.Unmarshal(output, &info); err != nil {
		t.Fatalf("decode version JSON: %v\n%s", err, output)
	}
	if info.SchemaVersion != version.SchemaVersion || info.Version != "0.1.0-alpha.1" ||
		info.Commit != commit || info.BuildDate != buildDate || info.Dirty == nil || *info.Dirty ||
		info.GoVersion == "" || info.OS != runtime.GOOS || info.Arch != runtime.GOARCH {
		t.Fatalf("release identity = %#v", info)
	}

	human := exec.Command(binaryPath, "--version")
	humanOutput, err := human.Output()
	if err != nil {
		t.Fatalf("run --version: %v", err)
	}
	for _, expected := range []string{
		"Version: 0.1.0-alpha.1",
		"Commit: " + commit,
		"Build date: " + buildDate,
		"Dirty: false",
	} {
		if !bytes.Contains(humanOutput, []byte(expected)) {
			t.Errorf("human version missing %q:\n%s", expected, humanOutput)
		}
	}

	binaryContents, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(binaryContents, []byte(root)) {
		t.Fatalf("release-shaped binary contains local build path %q", root)
	}
}
