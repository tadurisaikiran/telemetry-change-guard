// Command tcg-agent-fixture is a deterministic test adapter. It intentionally
// waits for one authoritative BLOCK result before applying the fixture repair,
// which exercises the complete agent -> TCG -> feedback -> agent loop.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	requestSchema  = "tcg-agent-request/v1alpha1"
	responseSchema = "tcg-agent-response/v1alpha1"
	maxInput       = 1 << 20
)

type request struct {
	SchemaVersion string            `json:"schemaVersion"`
	Task          task              `json:"task"`
	Attempt       int               `json:"attempt"`
	Workspace     string            `json:"workspace"`
	Guardrails    []string          `json:"guardrails"`
	Feedback      *feedback         `json:"feedback,omitempty"`
	Context       map[string]string `json:"context,omitempty"`
}

type task struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type feedback struct {
	AuthoritativeStatus string          `json:"authoritativeStatus"`
	Findings            json.RawMessage `json:"findings,omitempty"`
	Diagnostics         json.RawMessage `json:"diagnostics,omitempty"`
	Truncated           bool            `json:"truncated,omitempty"`
}

type response struct {
	SchemaVersion string   `json:"schemaVersion"`
	Summary       string   `json:"summary"`
	ChangedFiles  []string `json:"changedFiles,omitempty"`
	Limitations   []string `json:"limitations,omitempty"`
}

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(input io.Reader, output io.Writer) error {
	contents, err := io.ReadAll(io.LimitReader(input, maxInput+1))
	if err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	if len(contents) > maxInput {
		return fmt.Errorf("request exceeds %d bytes", maxInput)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var value request
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return err
	}
	if value.SchemaVersion != requestSchema || value.Workspace != "/workspace" || value.Attempt < 1 {
		return fmt.Errorf("invalid request envelope")
	}

	result := response{SchemaVersion: responseSchema}
	if value.Feedback == nil {
		result.Summary = "No deterministic TCG finding was provided yet; left the workspace unchanged."
		result.Limitations = []string{"Waiting for authoritative TCG feedback before editing the fixture."}
		return json.NewEncoder(output).Encode(result)
	}
	if value.Feedback.AuthoritativeStatus != "BLOCK" {
		return fmt.Errorf("adapter only accepts BLOCK feedback, got %q", value.Feedback.AuthoritativeStatus)
	}
	path := filepath.Join(value.Workspace, "prometheus", "rules.yaml")
	if err := replaceExactlyOnce(path, "checkout_requests_total", "checkout_server_requests_total"); err != nil {
		return err
	}
	result.Summary = "Updated the fixture rule to consume the replacement metric; TCG must independently re-verify it."
	result.ChangedFiles = []string{"prometheus/rules.yaml"}
	return json.NewEncoder(output).Encode(result)
}

func requireEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing data: %w", err)
	}
	return fmt.Errorf("request must contain exactly one JSON value")
}

func replaceExactlyOnce(path, old, replacement string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read fixture: %w", err)
	}
	if bytes.Count(contents, []byte(old)) != 1 {
		return fmt.Errorf("fixture must contain exactly one %q occurrence", old)
	}
	updated := bytes.Replace(contents, []byte(old), []byte(replacement), 1)
	temporary, err := os.CreateTemp(filepath.Dir(path), ".tcg-agent-fixture-*")
	if err != nil {
		return fmt.Errorf("create temporary fixture: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(updated); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish fixture: %w", err)
	}
	return nil
}
