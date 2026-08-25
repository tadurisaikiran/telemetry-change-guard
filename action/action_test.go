package action

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
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
		"migration", "comment", "artifact-name", "remote-evidence", "allowed-remote-origins",
		"allow-insecure-loopback", "remote-bearer-token",
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
		"action/build-action.sh",
		"Telemetry Change Guard build identity",
		"telemetry-change-guard\" version",
		"uses: actions/setup-go@",
		"uses: actions/upload-artifact@",
		"uses: actions/github-script@",
		"cache: false",
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

func TestBuildActionSelectsOnlyAuthoritativeCommitIdentity(t *testing.T) {
	t.Parallel()

	const actionCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const workflowCommit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	tests := []struct {
		name               string
		actionRef          string
		actionRepository   string
		workflowRepository string
		workflowSHA        string
		wantCommit         string
	}{
		{
			name:               "immutable external Action ref",
			actionRef:          actionCommit,
			actionRepository:   "owner/telemetry-change-guard",
			workflowRepository: "consumer/service",
			workflowSHA:        workflowCommit,
			wantCommit:         actionCommit,
		},
		{
			name:               "local Action uses workflow commit",
			actionRepository:   "owner/telemetry-change-guard",
			workflowRepository: "owner/telemetry-change-guard",
			workflowSHA:        workflowCommit,
			wantCommit:         workflowCommit,
		},
		{
			name:               "local Action without repository context uses workflow commit",
			workflowRepository: "owner/telemetry-change-guard",
			workflowSHA:        workflowCommit,
			wantCommit:         workflowCommit,
		},
		{
			name:               "movable external ref remains unknown",
			actionRef:          "v0.1.0-alpha.1",
			actionRepository:   "owner/telemetry-change-guard",
			workflowRepository: "consumer/service",
			workflowSHA:        workflowCommit,
			wantCommit:         "unknown",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := t.TempDir()
			argumentsPath := filepath.Join(runner, "go-arguments")
			fakeGoPath := filepath.Join(runner, "go")
			fakeGo := fmt.Sprintf("#!/bin/bash\nprintf '%%s\\n' \"$@\" > %q\n", argumentsPath)
			if err := os.WriteFile(fakeGoPath, []byte(fakeGo), 0o755); err != nil {
				t.Fatal(err)
			}

			command := exec.Command("bash", "build-action.sh")
			command.Env = []string{
				"GO=" + fakeGoPath,
				"PATH=" + os.Getenv("PATH"),
				"RUNNER_TEMP=" + runner,
				"TCG_ACTION_REF=" + test.actionRef,
				"TCG_ACTION_REPOSITORY=" + test.actionRepository,
				"TCG_WORKFLOW_REPOSITORY=" + test.workflowRepository,
				"TCG_WORKFLOW_SHA=" + test.workflowSHA,
			}
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("build-action.sh error = %v\n%s", err, output)
			}
			arguments := readFile(t, argumentsPath)
			for _, expected := range []string{
				"-buildvcs=false",
				"-trimpath",
				".Version=dev",
				".Commit=" + test.wantCommit,
				".Date=unknown",
				".Dirty=unknown",
			} {
				if !strings.Contains(arguments, expected) {
					t.Errorf("build arguments missing %q:\n%s", expected, arguments)
				}
			}
		})
	}
}

func TestResolveActionPathsCanonicalizesLocalAndExternalConsumption(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		actionPath func(t *testing.T) string
	}{
		{
			name: "local path containing dot segment",
			actionPath: func(t *testing.T) string {
				return filepath.Join(root, ".") + string(filepath.Separator) + "."
			},
		},
		{
			name: "external checkout path through symlink",
			actionPath: func(t *testing.T) string {
				external := filepath.Join(t.TempDir(), "telemetry-change-guard")
				if err := os.Symlink(root, external); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				return external
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outputPath := filepath.Join(t.TempDir(), "github-output")
			command := exec.Command("bash", "resolve-action-paths.sh")
			command.Env = []string{
				"GITHUB_ACTION_PATH=" + test.actionPath(t),
				"GITHUB_OUTPUT=" + outputPath,
				"PATH=" + os.Getenv("PATH"),
			}
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("resolve-action-paths.sh error = %v\n%s", err, output)
			}
			outputs := readActionOutputs(t, outputPath)
			if outputs["go-mod"] != filepath.Join(root, "go.mod") {
				t.Errorf("go-mod = %q", outputs["go-mod"])
			}
			if len(outputs) != 1 {
				t.Errorf("outputs = %v; want only canonical go-mod", outputs)
			}
			for name, path := range outputs {
				if path != filepath.Clean(path) {
					t.Errorf("%s path is not canonical: %q", name, path)
				}
			}
		})
	}
}

func TestWorkflowDependenciesAreImmutableAndVersioned(t *testing.T) {
	root := filepath.Clean("..")
	files := []string{filepath.Join(root, "action.yml")}
	for _, pattern := range []string{
		filepath.Join(root, ".github", "workflows", "*.yml"),
		filepath.Join(root, ".github", "workflows", "*.yaml"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}

	usesPattern := regexp.MustCompile(`(?m)^\s*uses:\s*([^\s#]+)([^\n]*)$`)
	immutablePattern := regexp.MustCompile(`^[^@]+@[0-9a-f]{40}$`)
	versionPattern := regexp.MustCompile(`#\s*v[0-9]+(?:\.[0-9]+)*(?:[-.][0-9A-Za-z.-]+)?\s*$`)
	for _, file := range files {
		contents, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range usesPattern.FindAllStringSubmatch(string(contents), -1) {
			reference := match[1]
			if strings.HasPrefix(reference, "./") {
				continue
			}
			if !immutablePattern.MatchString(reference) {
				t.Errorf("%s has movable Action reference %q", file, reference)
			}
			if !versionPattern.MatchString(match[2]) {
				t.Errorf("%s reference %q lacks an exact version comment", file, reference)
			}
		}
	}
}

func TestWorkflowDefaultPermissionsAreReadOnly(t *testing.T) {
	patterns := []string{
		filepath.Join("..", ".github", "workflows", "*.yml"),
		filepath.Join("..", ".github", "workflows", "*.yaml"),
	}
	for _, pattern := range patterns {
		files, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			contents, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var document struct {
				Permissions map[string]string `yaml:"permissions"`
			}
			if err := yaml.Unmarshal(contents, &document); err != nil {
				t.Fatalf("decode %s: %v", file, err)
			}
			want := map[string]string{"contents": "read"}
			if !reflect.DeepEqual(document.Permissions, want) {
				t.Errorf("%s default permissions = %#v, want %#v", file, document.Permissions, want)
			}
		}
	}
}

func TestDocumentedActionCoordinatesMatchCandidateMetadata(t *testing.T) {
	root := filepath.Clean("..")
	metadata := readEnvironmentFile(t, filepath.Join(root, "release", "metadata.env"))
	repository := metadata["TCG_ACTION_REPOSITORY"]
	reference := metadata["TCG_ACTION_REF"]
	if repository == "" || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(reference) {
		t.Fatalf("invalid release/metadata.env Action coordinate: %#v", metadata)
	}

	coordinatePatterns := []*regexp.Regexp{
		regexp.MustCompile(regexp.QuoteMeta(repository) + `@([0-9a-f]{40})`),
		regexp.MustCompile(`(?m)^git checkout ([0-9a-f]{40})$`),
	}
	files := []string{filepath.Join(root, "README.md")}
	err := filepath.WalkDir(filepath.Join(root, "docs"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && filepath.Ext(path) == ".md" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	found := 0
	for _, file := range files {
		contents, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, pattern := range coordinatePatterns {
			for _, match := range pattern.FindAllStringSubmatch(string(contents), -1) {
				found++
				if match[1] != reference {
					t.Errorf("%s documents stale installation ref %s; want %s", file, match[1], reference)
				}
			}
		}
	}
	if found == 0 {
		t.Fatal("no immutable Action installation coordinate is documented")
	}
}

func TestRunActionRemoteEvidenceIsDisabledByDefaultAndExplicitlyAllowlisted(t *testing.T) {
	t.Parallel()

	defaultResult := runActionScript(t, map[string]string{
		"TCG_CONFIG":      "tcg.yaml",
		"TCG_CHANGES":     "changes.yaml",
		"TCG_TEST_SCHEMA": "tcg-result/v1alpha1",
		"TCG_TEST_STATUS": "PASS",
		"TCG_TEST_EXIT":   "0",
	}, true)
	defaultArguments := strings.Split(strings.TrimSpace(defaultResult.arguments), "\n")
	for _, expected := range []string{"--remote-evidence", "disabled"} {
		if !contains(defaultArguments, expected) {
			t.Fatalf("default arguments missing %q: %#v", expected, defaultArguments)
		}
	}

	denied := runActionScript(t, map[string]string{
		"TCG_CONFIG":          "tcg.yaml",
		"TCG_CHANGES":         "changes.yaml",
		"TCG_REMOTE_EVIDENCE": "enabled",
	}, false)
	if denied.outputs["status"] != "ERROR" || !strings.Contains(denied.markdown, "trusted allowed-remote-origin") {
		t.Fatalf("denied result = %#v", denied)
	}

	allowed := runActionScript(t, map[string]string{
		"TCG_CONFIG":                 "tcg.yaml",
		"TCG_CHANGES":                "changes.yaml",
		"TCG_REMOTE_EVIDENCE":        "enabled",
		"TCG_ALLOWED_REMOTE_ORIGINS": "https://tempo.example.test\nhttps://perses.example.test",
		"TCG_TEST_SCHEMA":            "tcg-result/v1alpha1",
		"TCG_TEST_STATUS":            "PASS",
		"TCG_TEST_EXIT":              "0",
	}, true)
	allowedArguments := strings.Split(strings.TrimSpace(allowed.arguments), "\n")
	for _, expected := range []string{
		"enabled", "--allowed-remote-origin", "https://tempo.example.test", "https://perses.example.test",
	} {
		if !contains(allowedArguments, expected) {
			t.Fatalf("allowed arguments missing %q: %#v", expected, allowedArguments)
		}
	}
}

func TestRunActionClearsJobEnvironmentBeforeAnalysis(t *testing.T) {
	t.Parallel()

	result := runActionScript(t, map[string]string{
		"TCG_CONFIG":                 "tcg.yaml",
		"TCG_CHANGES":                "changes.yaml",
		"TCG_REMOTE_EVIDENCE":        "enabled",
		"TCG_ALLOWED_REMOTE_ORIGINS": "https://tempo.example.test",
		"TCG_REMOTE_BEARER_TOKEN":    "dedicated-token",
		"UNTRUSTED_JOB_SECRET":       "must-not-reach-analysis",
		"TCG_TEST_SCHEMA":            "tcg-result/v1alpha1",
		"TCG_TEST_STATUS":            "PASS",
		"TCG_TEST_EXIT":              "0",
	}, true)
	if !strings.Contains(result.arguments, "environment:unset") || !strings.Contains(result.arguments, "remote-token:set") {
		t.Fatalf("sanitized environment evidence missing: %q", result.arguments)
	}
	combined := result.arguments + result.markdown + result.json + result.summary
	for _, secret := range []string{"must-not-reach-analysis", "dedicated-token"} {
		if strings.Contains(combined, secret) {
			t.Fatalf("Action output leaked %q: %s", secret, combined)
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
		fake := fmt.Sprintf(`#!/bin/bash
set -euo pipefail
test_arguments=%q
test_schema=%q
test_status=%q
test_exit=%q
printf '%%s\n' "$@" > "${test_arguments}"
printf 'environment:%%s\n' "${UNTRUSTED_JOB_SECRET-unset}" >> "${test_arguments}"
printf 'remote-token:%%s\n' "${TCG_REMOTE_BEARER_TOKEN+set}" >> "${test_arguments}"
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
printf '# Telemetry Change Guard\n\n**Status:** **%%s**\n' "${test_status}" > "${report}"
printf '{\n  "schemaVersion": "%%s",\n  "status": "%%s"\n}\n' "${test_schema}" "${test_status}" > "${json_report}"
printf '%%s\n' "${test_status}" > "${status_output}"
exit "${test_exit}"
`, argumentsPath, values["TCG_TEST_SCHEMA"], values["TCG_TEST_STATUS"], values["TCG_TEST_EXIT"])
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

func readEnvironmentFile(t *testing.T, path string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, line := range strings.Split(readFile(t, path), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			t.Fatalf("invalid metadata line %q in %s", line, path)
		}
		result[parts[0]] = parts[1]
	}
	return result
}
