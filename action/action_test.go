package action

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestActionMetadataAndScript(t *testing.T) {
	t.Parallel()

	root := filepath.Clean("..")
	contents, err := os.ReadFile(filepath.Join(root, "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Name        string                 `yaml:"name"`
		Description string                 `yaml:"description"`
		Inputs      map[string]any         `yaml:"inputs"`
		Outputs     map[string]any         `yaml:"outputs"`
		Runs        map[string]interface{} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(contents, &document); err != nil {
		t.Fatalf("decode action.yml: %v", err)
	}
	if document.Name != "Telemetry Change Guard" ||
		document.Description != "Detect downstream impact before telemetry changes reach production" ||
		document.Runs["using"] != "composite" {
		t.Fatalf("invalid action metadata: %+v", document)
	}
	for _, input := range []string{
		"config", "changes", "baseline", "candidate", "weaver-diff", "weaver-mapping",
		"migration", "comment", "artifact-name",
	} {
		if _, exists := document.Inputs[input]; !exists {
			t.Errorf("missing %q input", input)
		}
	}
	for _, output := range []string{"status", "exit-code", "report", "json-report", "mode"} {
		if _, exists := document.Outputs[output]; !exists {
			t.Errorf("missing %q output", output)
		}
	}
	for _, expected := range []string{
		"./cmd/telemetry-change-guard",
		"actions/upload-artifact@v6",
		"const marker = '<!-- telemetry-change-guard -->'",
		"const legacyMarker = '<!-- telemetry-migration-readiness -->'",
		"comment.body?.includes(marker) || comment.body?.includes(legacyMarker)",
		"const limit = 65536",
		"continue-on-error: true",
	} {
		if !strings.Contains(string(contents), expected) {
			t.Errorf("action.yml does not contain %q", expected)
		}
	}

	script, err := os.ReadFile(filepath.Join("run-action.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"set -euo pipefail",
		"generic change sources and migration are mutually exclusive",
		"--json-output",
		"--status-output",
		"status and process exit code disagreed",
		"GITHUB_STEP_SUMMARY",
		"GITHUB_OUTPUT",
		"json-report=",
	} {
		if !strings.Contains(string(script), expected) {
			t.Errorf("run-action.sh does not contain %q", expected)
		}
	}
}

func TestRunActionSupportsGenericAndMigrationModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		changes    string
		baseline   string
		candidate  string
		weaverDiff string
		weaverMap  string
		migration  string
		status     string
		wantMode   string
		wantArgs   []string
		unwantArgs []string
	}{
		{
			name: "generic", changes: "changes.yaml", status: "WARN", wantMode: "generic",
			wantArgs:   []string{"check", "--config", "tcg.yaml", "--changes", "changes.yaml"},
			unwantArgs: []string{"migration", "--plan"},
		},
		{
			name: "snapshot", baseline: "baseline.json", candidate: "candidate.json", status: "WARN", wantMode: "generic",
			wantArgs:   []string{"check", "--config", "tcg.yaml", "--baseline", "baseline.json", "--candidate", "candidate.json"},
			unwantArgs: []string{"migration", "--changes", "--weaver-diff"},
		},
		{
			name: "Weaver", weaverDiff: "diff.json", weaverMap: "mapping.yaml", status: "WARN", wantMode: "generic",
			wantArgs:   []string{"check", "--config", "tcg.yaml", "--weaver-diff", "diff.json", "--weaver-mapping", "mapping.yaml"},
			unwantArgs: []string{"migration", "--changes", "--baseline"},
		},
		{
			name: "migration", migration: "migration.yaml", status: "READY", wantMode: "migration",
			wantArgs:   []string{"migration", "check", "--config", "tcg.yaml", "--plan", "migration.yaml"},
			unwantArgs: []string{"--changes"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result := runActionScript(t, map[string]string{
				"TCG_CONFIG":         "tcg.yaml",
				"TCG_CHANGES":        test.changes,
				"TCG_BASELINE":       test.baseline,
				"TCG_CANDIDATE":      test.candidate,
				"TCG_WEAVER_DIFF":    test.weaverDiff,
				"TCG_WEAVER_MAPPING": test.weaverMap,
				"TCG_MIGRATION":      test.migration,
				"TCG_TEST_SCHEMA":    map[string]string{"generic": "tcg-result/v1alpha1", "migration": "tmr-result/v1alpha1"}[test.wantMode],
				"TCG_TEST_STATUS":    test.status,
				"TCG_TEST_EXIT":      "0",
			}, true)
			if result.outputs["status"] != test.status || result.outputs["exit-code"] != "0" ||
				result.outputs["mode"] != test.wantMode {
				t.Fatalf("outputs = %#v", result.outputs)
			}
			arguments := strings.Split(strings.TrimSpace(result.arguments), "\n")
			for _, expected := range test.wantArgs {
				if !contains(arguments, expected) {
					t.Errorf("arguments missing %q: %#v", expected, arguments)
				}
			}
			for _, unexpected := range test.unwantArgs {
				if contains(arguments, unexpected) {
					t.Errorf("arguments contain %q: %#v", unexpected, arguments)
				}
			}
			assertActionArtifacts(t, result, test.status)
		})
	}
}

func TestRunActionRejectsInvalidModeSelectionWithoutInvokingCLI(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		changes    string
		baseline   string
		candidate  string
		weaverDiff string
		weaverMap  string
		migration  string
		message    string
	}{
		{name: "both", changes: "changes.yaml", migration: "migration.yaml", message: "mutually exclusive"},
		{name: "neither", message: "exactly one"},
		{name: "multiple generic", changes: "changes.yaml", baseline: "baseline.json", candidate: "candidate.json", message: "exactly one"},
		{name: "partial snapshot", baseline: "baseline.json", message: "provided together"},
		{name: "partial Weaver", weaverDiff: "diff.json", message: "provided together"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result := runActionScript(t, map[string]string{
				"TCG_CONFIG":         "tcg.yaml",
				"TCG_CHANGES":        test.changes,
				"TCG_BASELINE":       test.baseline,
				"TCG_CANDIDATE":      test.candidate,
				"TCG_WEAVER_DIFF":    test.weaverDiff,
				"TCG_WEAVER_MAPPING": test.weaverMap,
				"TCG_MIGRATION":      test.migration,
			}, false)
			if result.outputs["status"] != "ERROR" || result.outputs["exit-code"] != "1" ||
				result.outputs["mode"] != "invalid" || result.arguments != "" {
				t.Fatalf("result = %#v", result)
			}
			if !strings.Contains(result.markdown, test.message) {
				t.Fatalf("report = %q, want %q", result.markdown, test.message)
			}
			assertActionArtifacts(t, result, "ERROR")
		})
	}
}

func TestRunActionFailsClosedWhenStatusAndExitCodeDisagree(t *testing.T) {
	t.Parallel()

	result := runActionScript(t, map[string]string{
		"TCG_CONFIG":      "tcg.yaml",
		"TCG_CHANGES":     "changes.yaml",
		"TCG_TEST_SCHEMA": "tcg-result/v1alpha1",
		"TCG_TEST_STATUS": "BLOCK",
		"TCG_TEST_EXIT":   "0",
	}, true)
	if result.outputs["status"] != "ERROR" || result.outputs["exit-code"] != "1" ||
		!strings.Contains(result.markdown, "failed closed") {
		t.Fatalf("result = %#v", result)
	}
	assertActionArtifacts(t, result, "ERROR")
}

func TestRunActionRejectsModeIncompatibleStatus(t *testing.T) {
	t.Parallel()

	result := runActionScript(t, map[string]string{
		"TCG_CONFIG":      "tcg.yaml",
		"TCG_CHANGES":     "changes.yaml",
		"TCG_TEST_SCHEMA": "tmr-result/v1alpha1",
		"TCG_TEST_STATUS": "READY",
		"TCG_TEST_EXIT":   "0",
	}, true)
	if result.outputs["status"] != "ERROR" || result.outputs["exit-code"] != "1" ||
		!strings.Contains(result.markdown, "mode-incompatible") {
		t.Fatalf("result = %#v", result)
	}
	assertActionArtifacts(t, result, "ERROR")
}

type actionRunResult struct {
	outputs   map[string]string
	arguments string
	markdown  string
	json      string
	summary   string
}

func runActionScript(t *testing.T, values map[string]string, installFake bool) actionRunResult {
	t.Helper()
	runner := t.TempDir()
	outputPath := filepath.Join(runner, "github-output")
	summaryPath := filepath.Join(runner, "github-summary")
	argumentsPath := filepath.Join(runner, "arguments")
	if installFake {
		fake := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" > "${TCG_TEST_ARGUMENTS}"
report=""
json_report=""
status_output=""
while (($#)); do
  case "$1" in
    --output) report=$2; shift 2 ;;
    --json-output) json_report=$2; shift 2 ;;
    --status-output) status_output=$2; shift 2 ;;
    *) shift ;;
  esac
done
printf '# Telemetry Change Guard\n\n**Status:** **%s**\n' "${TCG_TEST_STATUS}" > "${report}"
printf '{\n  "schemaVersion": "%s",\n  "status": "%s"\n}\n' "${TCG_TEST_SCHEMA}" "${TCG_TEST_STATUS}" > "${json_report}"
printf '%s\n' "${TCG_TEST_STATUS}" > "${status_output}"
exit "${TCG_TEST_EXIT}"
`
		binary := filepath.Join(runner, "telemetry-change-guard")
		if err := os.WriteFile(binary, []byte(fake), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	command := exec.Command("bash", "run-action.sh")
	command.Env = append(os.Environ(),
		"RUNNER_TEMP="+runner,
		"GITHUB_OUTPUT="+outputPath,
		"GITHUB_STEP_SUMMARY="+summaryPath,
		"TCG_TEST_ARGUMENTS="+argumentsPath,
		"TCG_CHANGES=",
		"TCG_BASELINE=",
		"TCG_CANDIDATE=",
		"TCG_WEAVER_DIFF=",
		"TCG_WEAVER_MAPPING=",
		"TCG_MIGRATION=",
	)
	for name, value := range values {
		command.Env = append(command.Env, name+"="+value)
	}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run-action.sh error = %v\n%s", err, output)
	}

	result := actionRunResult{outputs: readActionOutputs(t, outputPath)}
	result.arguments = readOptionalFile(t, argumentsPath)
	result.markdown = readFile(t, result.outputs["report"])
	result.json = readFile(t, result.outputs["json-report"])
	result.summary = readFile(t, summaryPath)
	return result
}

func assertActionArtifacts(t *testing.T, result actionRunResult, status string) {
	t.Helper()
	if !strings.Contains(result.markdown, "**Status:** **"+status+"**") || result.summary != result.markdown {
		t.Fatalf("markdown = %q, summary = %q", result.markdown, result.summary)
	}
	var document struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(result.json), &document); err != nil {
		t.Fatalf("decode JSON artifact: %v\n%s", err, result.json)
	}
	if document.Status != status {
		t.Fatalf("JSON status = %q, want %q", document.Status, status)
	}
}

func readActionOutputs(t *testing.T, path string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(readFile(t, path)), "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func readOptionalFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
