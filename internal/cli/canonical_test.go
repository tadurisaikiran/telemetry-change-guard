package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/config"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/safety"
)

func TestCanonicalHelpUsesCollisionFreeExecutableName(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if exitCode := Run(context.Background(), []string{"help"}, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	for _, expected := range []string{
		"Telemetry Change Guard",
		"telemetry-change-guard check",
		"telemetry-change-guard migration check",
		"temporary tmr binary",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("help missing %q:\n%s", expected, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "\n  tcg ") {
		t.Fatalf("help advertises the rejected colliding tcg binary:\n%s", stdout.String())
	}
}

func TestCanonicalCheckCheckoutContract(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", ".."))
	command := exec.Command(os.Args[0], "-test.run=TestCanonicalCLIHelperProcess", "--",
		"canonical",
		"check",
		"--config", "examples/checkout-migration/tcg.yaml",
		"--changes", "examples/checkout-migration/changes.yaml",
		"--format", "json",
	)
	command.Dir = root
	command.Env = append(os.Environ(), "TCG_CLI_HELPER=1")
	output, err := command.Output()
	exitError, ok := err.(*exec.ExitError)
	if !ok || exitError.ExitCode() != safety.ExitCode(safety.StatusIncomplete) {
		t.Fatalf("check error = %v, want INCOMPLETE exit 3", err)
	}
	var result safety.Result
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode report: %v\n%s", err, output)
	}
	if result.SchemaVersion != safety.ResultSchemaVersion || result.Status != safety.StatusIncomplete ||
		len(result.Findings) == 0 || len(result.Decisions) != len(result.Findings) {
		t.Fatalf("result = %#v", result)
	}
}

func TestCanonicalCheckRejectsConflictingEnvironmentWithoutSecretDisclosure(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "tcg.yaml")
	writeCLIFixture(t, configPath, `apiVersion: tcg/v1alpha1
kind: Config
sources:
  persesUsage:
    - url: https://usage.example.test
      bearerTokenEnv: TCG_CLI_CONFLICT_TOKEN
output:
  formats: [json]
`)
	changesPath := filepath.Join(root, "changes.yaml")
	writeCLIFixture(t, changesPath, `apiVersion: tcg/v1alpha1
kind: ChangeSet
metadata: {name: conflict}
spec:
  changes:
    - id: remove-old
      kind: metric_remove
      domain: prometheus
      from: {domain: prometheus, kind: metric, name: old_metric}
`)
	t.Setenv("TCG_CLI_CONFLICT_TOKEN", "canonical-secret")
	t.Setenv("TMR_CLI_CONFLICT_TOKEN", "legacy-secret")

	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{
		"check", "--config", configPath, "--changes", changesPath, "--format", "json",
	}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want ERROR exit 1; stderr = %q", exitCode, stderr.String())
	}
	var result safety.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v\n%s", err, stdout.String())
	}
	if result.Status != safety.StatusError || len(result.Errors) != 1 {
		t.Fatalf("result = %#v", result)
	}
	combined := stdout.String() + stderr.String()
	for _, name := range []string{"TCG_CLI_CONFLICT_TOKEN", "TMR_CLI_CONFLICT_TOKEN"} {
		if !strings.Contains(combined, name) {
			t.Errorf("output missing environment name %q: %s", name, combined)
		}
	}
	for _, secret := range []string{"canonical-secret", "legacy-secret"} {
		if strings.Contains(combined, secret) {
			t.Errorf("output leaked secret %q: %s", secret, combined)
		}
	}
}

func TestCanonicalCheckRolloutAndExitContracts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	rulesPath := filepath.Join(root, "rules.yaml")
	writeCLIFixture(t, rulesPath, `groups:
  - name: traffic
    rules:
      - alert: TrafficStopped
        expr: old_metric == 0
        labels: {severity: critical}
`)
	configPath := filepath.Join(root, "tmr.yaml")
	writeCLIFixture(t, configPath, fmt.Sprintf(`apiVersion: tmr/v1alpha1
sources:
  prometheusRules:
    - path: %q
      required: true
analysis:
  includeTransitiveDependencies: true
  unresolvedReferencePolicy: error
policy:
  failOnCriticalLegacyConsumer: true
  failOnCriticalUnknown: true
  minimumBlockingCriticality: high
output:
  formats: [json]
`, rulesPath))
	changesPath := filepath.Join(root, "changes.yaml")
	writeCLIFixture(t, changesPath, `apiVersion: tcg/v1alpha1
kind: ChangeSet
metadata: {name: remove-old-metric}
spec:
  changes:
    - id: remove-old
      kind: metric_remove
      domain: prometheus
      from: {domain: prometheus, kind: metric, name: old_metric}
`)

	for _, test := range []struct {
		mode       string
		wantStatus safety.Status
		wantExit   int
	}{
		{mode: "enforce", wantStatus: safety.StatusBlock, wantExit: 2},
		{mode: "warn", wantStatus: safety.StatusWarn, wantExit: 0},
		{mode: "audit", wantStatus: safety.StatusWarn, wantExit: 0},
	} {
		t.Run(test.mode, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exitCode := Run(context.Background(), []string{
				"check", "--config", configPath, "--changes", changesPath,
				"--mode", test.mode, "--format", "json",
			}, &stdout, &stderr)
			if exitCode != test.wantExit {
				t.Fatalf("exit = %d, want %d; stderr = %q", exitCode, test.wantExit, stderr.String())
			}
			var result safety.Result
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Status != test.wantStatus || len(result.Findings) != 1 || len(result.Decisions) != 1 {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestCanonicalMigrationCheckIsBytewiseCompatibleWithTMR(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", ".."))
	canonicalOutput, canonicalExit := runCLIHelper(t, root, "canonical",
		"migration", "check",
		"--config", "examples/checkout-migration/tmr.yaml",
		"--plan", "examples/checkout-migration/migration.yaml",
		"--format", "json",
	)
	compatibilityOutput, compatibilityExit := runCLIHelper(t, root, "compatibility",
		"analyze",
		"--config", "examples/checkout-migration/tmr.yaml",
		"--migration", "examples/checkout-migration/migration.yaml",
		"--format", "json",
	)
	if canonicalExit != 2 || compatibilityExit != 2 {
		t.Fatalf("canonical exit = %d, compatibility exit = %d; want 2", canonicalExit, compatibilityExit)
	}
	if !bytes.Equal(canonicalOutput, compatibilityOutput) {
		t.Fatal("canonical migration output differs from tmr compatibility output")
	}
}

func TestCanonicalCheckWritesCompanionJSONFromSameEvaluation(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", ".."))
	reportPath := filepath.Join(t.TempDir(), "report.md")
	jsonPath := filepath.Join(t.TempDir(), "report.json")
	statusPath := filepath.Join(t.TempDir(), "status")
	output, exitCode := runCLIHelper(t, root, "canonical",
		"check",
		"--config", "examples/checkout-migration/tcg.yaml",
		"--changes", "examples/checkout-migration/changes.yaml",
		"--format", "markdown",
		"--output", reportPath,
		"--json-output", jsonPath,
		"--status-output", statusPath,
	)
	if exitCode != safety.ExitCode(safety.StatusIncomplete) || len(output) != 0 {
		t.Fatalf("exit = %d, stdout = %q", exitCode, output)
	}
	markdown, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	status, err := os.ReadFile(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	var result safety.Result
	if err := json.Unmarshal(contents, &result); err != nil {
		t.Fatalf("decode JSON companion: %v\n%s", err, contents)
	}
	if result.Status != safety.StatusIncomplete || string(status) != "INCOMPLETE\n" ||
		!strings.Contains(string(markdown), "**Status:** **INCOMPLETE**") {
		t.Fatalf("markdown and JSON disagree\nmarkdown:\n%s\nJSON: %#v", markdown, result)
	}
}

func TestCanonicalCheckRejectsCollidingOutputPathsBeforeInputLoading(t *testing.T) {
	t.Parallel()

	for _, flags := range [][]string{
		{"--output", "report", "--json-output", "./report"},
		{"--output", "report", "--status-output", "./report"},
		{"--json-output", "report", "--status-output", "./report"},
	} {
		var stdout, stderr bytes.Buffer
		args := []string{"check", "--config", "missing-config.yaml", "--changes", "missing-changes.yaml"}
		args = append(args, flags...)
		exitCode := Run(context.Background(), args, &stdout, &stderr)
		if exitCode != 1 || stdout.Len() != 0 ||
			!strings.Contains(stderr.String(), "must identify different files") ||
			strings.Contains(stderr.String(), "missing-config.yaml") {
			t.Fatalf("flags = %v, exit = %d, stdout = %q, stderr = %q", flags, exitCode, stdout.String(), stderr.String())
		}
	}
}

func TestCanonicalImpactIncludesPrometheusMetricFamilyDependencies(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", ".."))
	canonicalOutput, canonicalExit := runCLIHelper(t, root, "canonical",
		"impact",
		"--config", "examples/checkout-migration/tmr.yaml",
		"--symbol", "checkout_request_duration_seconds",
	)
	if canonicalExit != 3 {
		t.Fatalf("canonical exit = %d, want incomplete exit 3", canonicalExit)
	}
	for _, expected := range []string{
		"Direct consumers:", "Transitive consumers:", "Affected consumers:", "ALERTING_RISK", "Dependency paths:",
		"checkout_request_duration_seconds_bucket", "CheckoutLatencyHigh",
	} {
		if !strings.Contains(string(canonicalOutput), expected) {
			t.Fatalf("canonical impact output missing %q:\n%s", expected, canonicalOutput)
		}
	}
}

func TestCanonicalImpactFailsClosedOnRequiredMissingEvidence(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "tmr.yaml")
	writeCLIFixture(t, configPath, `apiVersion: tmr/v1alpha1
sources:
  prometheusRules:
    - path: /definitely/missing/tcg-r5/*.yaml
      required: true
analysis:
  includeTransitiveDependencies: true
  unresolvedReferencePolicy: error
policy:
  failOnCriticalLegacyConsumer: true
  failOnCriticalUnknown: true
  minimumBlockingCriticality: high
output:
  formats: [json]
`)
	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{
		"impact", "--config", configPath, "--symbol", "old_metric",
	}, &stdout, &stderr)
	if exitCode != 3 || !strings.Contains(stdout.String(), "No confirmed dependents found") ||
		!strings.Contains(stderr.String(), "Diagnostic [prometheus_rules/required]") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
}

func TestCanonicalValidateChangeSet(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "examples", "checkout-migration", "changes.yaml")
	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{"validate", "--changes", path}, &stdout, &stderr)
	if exitCode != 0 || stdout.String() != "ChangeSet manifest is valid.\nChanges: 2\n" || stderr.Len() != 0 {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
}

func TestCheckoutChangeSetMatchesLegacyNormalization(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", ".."))
	changeSet, err := config.LoadChangeSet(context.Background(), filepath.Join(root, "examples", "checkout-migration", "changes.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	migration, err := config.LoadMigration(context.Background(), filepath.Join(root, "examples", "checkout-migration", "migration.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := config.NormalizeMigration(migration)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changeSet, normalized) {
		t.Fatalf("native ChangeSet and normalized migration differ\nnative: %#v\nnormalized: %#v", changeSet, normalized)
	}
}

func TestCanonicalRejectsInvalidRolloutBeforeLoadingInputs(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{
		"check", "--config", "missing-config.yaml", "--changes", "missing-changes.yaml", "--mode", "disabled",
	}, &stdout, &stderr)
	if exitCode != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "unsupported policy rollout mode") ||
		strings.Contains(stderr.String(), "missing-config.yaml") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
}

func TestCanonicalCLIHelperProcess(t *testing.T) {
	if os.Getenv("TCG_CLI_HELPER") != "1" {
		return
	}
	separator := 0
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index + 1
			break
		}
	}
	args := os.Args[separator:]
	if len(args) == 0 {
		os.Exit(1)
	}
	var exitCode int
	switch args[0] {
	case "canonical":
		exitCode = Run(context.Background(), args[1:], os.Stdout, os.Stderr)
	case "compatibility":
		exitCode = RunCompatibility(context.Background(), args[1:], os.Stdout, os.Stderr)
	default:
		exitCode = 1
	}
	os.Exit(exitCode)
}

func runCLIHelper(t *testing.T, root string, args ...string) ([]byte, int) {
	t.Helper()
	commandArgs := []string{"-test.run=TestCanonicalCLIHelperProcess", "--"}
	commandArgs = append(commandArgs, args...)
	command := exec.Command(os.Args[0], commandArgs...)
	command.Dir = root
	command.Env = append(os.Environ(), "TCG_CLI_HELPER=1")
	output, err := command.Output()
	if err == nil {
		return output, 0
	}
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("run helper: %v", err)
	}
	return output, exitError.ExitCode()
}

func writeCLIFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
