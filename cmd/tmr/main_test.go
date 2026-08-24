package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunValidateSuccess(t *testing.T) {
	t.Parallel()

	manifest := filepath.Join("..", "..", "internal", "config", "testdata", "valid", "all-change-kinds.yaml")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run(context.Background(), []string{"validate", "--migration", manifest}, &stdout, &stderr)
	if got, want := exitCode, 0; got != want {
		t.Fatalf("exit code = %d, want %d; stderr = %q", got, want, stderr.String())
	}
	if got, want := stdout.String(), "Migration manifest is valid.\nChanges: 4\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if got := stderr.String(); got != "" {
		t.Errorf("stderr = %q, want empty", got)
	}
}

func TestRunValidateFailure(t *testing.T) {
	t.Parallel()

	manifest := filepath.Join("..", "..", "internal", "config", "testdata", "invalid", "validation-errors.yaml")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run(context.Background(), []string{"validate", "--migration", manifest}, &stdout, &stderr)
	if got, want := exitCode, 1; got != want {
		t.Fatalf("exit code = %d, want %d", got, want)
	}
	if got := stdout.String(); got != "" {
		t.Errorf("stdout = %q, want empty", got)
	}
	if got := stderr.String(); !strings.Contains(got, "metadata.name: is required") {
		t.Errorf("stderr = %q, want validation error", got)
	}
}

func TestRunValidateRequiresMigrationFlag(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(context.Background(), []string{"validate"}, &stdout, &stderr)

	if got, want := exitCode, 1; got != want {
		t.Fatalf("exit code = %d, want %d", got, want)
	}
	if got, want := stderr.String(), "--migration is required\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(context.Background(), []string{"analyze"}, &stdout, &stderr)

	if got, want := exitCode, 1; got != want {
		t.Fatalf("exit code = %d, want %d", got, want)
	}
	if got := stderr.String(); !strings.Contains(got, `unknown command "analyze"`) {
		t.Errorf("stderr = %q, want unknown-command error", got)
	}
}
