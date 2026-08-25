package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testTarEntry struct {
	name     string
	typeflag byte
	body     string
}

func TestExpectedArtifacts(t *testing.T) {
	t.Parallel()

	artifacts := expectedArtifacts("0.1.0-alpha.1")
	if len(artifacts) != 21 {
		t.Fatalf("expected 21 artifacts, got %d", len(artifacts))
	}
	for index := 1; index < len(artifacts); index++ {
		if artifacts[index-1].File >= artifacts[index].File {
			t.Fatalf("artifacts are not sorted at %q", artifacts[index].File)
		}
	}
	for _, required := range []string{
		"telemetry-change-guard_0.1.0-alpha.1_linux_amd64.tar.gz",
		"telemetry-change-guard_0.1.0-alpha.1_windows_arm64.zip.cdx.json",
		"telemetry-change-guard_0.1.0-alpha.1_source.tar.gz.spdx.json",
	} {
		found := false
		for _, artifact := range artifacts {
			found = found || artifact.File == required
		}
		if !found {
			t.Errorf("missing expected artifact %s", required)
		}
	}
}

func TestNormalizeSPDX(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "sbom.json")
	writeTestJSON(t, file, map[string]any{
		"spdxVersion":       "SPDX-2.3",
		"documentNamespace": "urn:uuid:random",
		"creationInfo": map[string]any{
			"created": "2020-01-01T00:00:00Z",
		},
	})
	if err := normalizeSPDX(file, "subject.tar.gz", strings.Repeat("a", 64), "0.1.0-alpha.1", "2026-08-25T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	document, err := decodeJSONObject(file)
	if err != nil {
		t.Fatal(err)
	}
	creation := document["creationInfo"].(map[string]any)
	if creation["created"] != "2026-08-25T12:00:00Z" {
		t.Fatalf("created = %v", creation["created"])
	}
	if strings.Contains(document["documentNamespace"].(string), "random") {
		t.Fatal("random namespace was not replaced")
	}
}

func TestNormalizeCycloneDX(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "sbom.json")
	writeTestJSON(t, file, map[string]any{
		"bomFormat":    "CycloneDX",
		"serialNumber": "urn:uuid:random",
		"metadata": map[string]any{
			"timestamp": "2020-01-01T00:00:00Z",
		},
	})
	if err := normalizeCycloneDX(file, "2026-08-25T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	document, err := decodeJSONObject(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := document["serialNumber"]; exists {
		t.Fatal("random serial number was not removed")
	}
	metadata := document["metadata"].(map[string]any)
	if metadata["timestamp"] != "2026-08-25T12:00:00Z" {
		t.Fatalf("timestamp = %v", metadata["timestamp"])
	}
}

func TestParseChecksumsRejectsUnsafeAndUnsortedInput(t *testing.T) {
	t.Parallel()

	a := strings.Repeat("a", 64)
	b := strings.Repeat("b", 64)
	tests := []string{
		a + "  ../escape\n",
		b + "  z\n" + a + "  a\n",
		a + "  duplicate\n" + b + "  duplicate\n",
		a + " *single-space\n",
	}
	for _, input := range tests {
		if _, _, err := parseChecksums([]byte(input)); err == nil {
			t.Errorf("parseChecksums accepted %q", input)
		}
	}
}

func TestValidateArchivePath(t *testing.T) {
	t.Parallel()

	for _, valid := range []string{"release/", "release/tmr", "release/README.md"} {
		if err := validateArchivePath(valid); err != nil {
			t.Errorf("valid path %q rejected: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "/absolute", "../escape", "a/../escape", `a\\b`} {
		if err := validateArchivePath(invalid); err == nil {
			t.Errorf("unsafe path %q accepted", invalid)
		}
	}
}

func TestReadTarGzipRejectsSymlink(t *testing.T) {
	t.Parallel()

	invalid := filepath.Join(t.TempDir(), "invalid.tar.gz")
	writeTestTarGzip(t, invalid, []testTarEntry{
		{name: "release/link", typeflag: tar.TypeSymlink},
	})
	if _, err := readTarGzip(invalid); err == nil {
		t.Fatal("symlink archive entry was accepted")
	}
}

func writeTestTarGzip(t *testing.T, file string, entries []testTarEntry) {
	t.Helper()
	output, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(output)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		header := &tar.Header{
			Name:     entry.name,
			Mode:     0o644,
			Size:     int64(len(entry.body)),
			Typeflag: entry.typeflag,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if entry.body != "" {
			if _, err := tarWriter.Write([]byte(entry.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeTestJSON(t *testing.T, file string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
