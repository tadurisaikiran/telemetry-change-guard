package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyAcceptsCleanCodeQLSARIF(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSARIF(t, filepath.Join(dir, "go.sarif"), `{
		"runs": [{
			"tool": {"driver": {"name": "CodeQL"}},
			"invocations": [{"executionSuccessful": true}],
			"results": []
		}]
	}`)
	if err := verify(dir); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsExtractionDiagnostic(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "go.sarif")
	writeSARIF(t, file, `{
		"runs": [{
			"tool": {"driver": {"name": "CodeQL CLI"}},
			"results": [{
				"ruleId": "go/diagnostics/extraction-errors",
				"message": {"text": "file requires newer Go version"}
			}]
		}]
	}`)
	err := verify(file)
	if err == nil || !strings.Contains(err.Error(), "extraction diagnostic") {
		t.Fatalf("error = %v; want extraction diagnostic failure", err)
	}
}

func TestVerifyRejectsFailedInvocation(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "go.sarif.json")
	writeSARIF(t, file, `{
		"runs": [{
			"tool": {"driver": {"name": "CodeQL"}},
			"invocations": [{"executionSuccessful": false}]
		}]
	}`)
	err := verify(file)
	if err == nil || !strings.Contains(err.Error(), "executionSuccessful=false") {
		t.Fatalf("error = %v; want failed invocation", err)
	}
}

func TestVerifyRejectsToolExecutionError(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "go.sarif")
	writeSARIF(t, file, `{
		"runs": [{
			"tool": {"driver": {"name": "CodeQL"}},
			"invocations": [{
				"executionSuccessful": true,
				"toolExecutionNotifications": [{"level": "error", "message": {"text": "extractor failed"}}]
			}]
		}]
	}`)
	err := verify(file)
	if err == nil || !strings.Contains(err.Error(), "invocation error") {
		t.Fatalf("error = %v; want tool execution error", err)
	}
}

func TestVerifyRequiresCodeQLRun(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "other.sarif")
	writeSARIF(t, file, `{"runs":[{"tool":{"driver":{"name":"Other analyzer"}}}]}`)
	err := verify(file)
	if err == nil || !strings.Contains(err.Error(), "CodeQL run") {
		t.Fatalf("error = %v; want missing CodeQL run", err)
	}
}

func TestVerifyRejectsEmptyDirectory(t *testing.T) {
	t.Parallel()

	err := verify(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no SARIF files") {
		t.Fatalf("error = %v; want no SARIF files", err)
	}
}

func TestVerifyRejectsMalformedSARIF(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "broken.sarif")
	writeSARIF(t, file, `{`)
	if err := verify(file); err == nil {
		t.Fatal("verify() succeeded for malformed SARIF")
	}
}

func writeSARIF(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
