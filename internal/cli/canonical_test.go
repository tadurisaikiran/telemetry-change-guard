package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/config"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/safety"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/snapshot"
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

func TestCanonicalCheckKEDAControlPlaneContract(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", ".."))
	output, exitCode := runCLIHelper(t, root, "canonical",
		"check",
		"--config", "examples/keda/tcg.yaml",
		"--changes", "examples/keda/changes.yaml",
		"--format", "json",
	)
	if exitCode != safety.ExitCode(safety.StatusBlock) {
		t.Fatalf("exit = %d, want BLOCK exit 2; output = %s", exitCode, output)
	}
	var result safety.Result
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode result: %v\n%s", err, output)
	}
	if result.Status != safety.StatusBlock || len(result.Findings) != 1 ||
		result.Findings[0].Impact != "SCALING_RISK" ||
		result.Findings[0].Consumer.Kind != domain.ConsumerKindAutoscaler ||
		result.Findings[0].Consumer.Name != "orders-worker" ||
		result.Findings[0].Criticality != domain.CriticalityCritical {
		t.Fatalf("result = %#v", result)
	}
}

func TestSourceCountIncludesKEDA(t *testing.T) {
	t.Parallel()

	configuration := config.Config{Sources: config.Sources{KEDA: []config.SourcePattern{{Pattern: "scaledobject.yaml"}}}}
	if got, want := sourceCount(configuration), 1; got != want {
		t.Fatalf("sourceCount() = %d, want %d", got, want)
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

func TestCanonicalCheckRequiresExactlyOneCompleteChangeSource(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "missing", args: nil, want: "exactly one change source"},
		{name: "partial Weaver", args: []string{"--weaver-diff", "diff.json"}, want: "must be provided together"},
		{name: "partial snapshot", args: []string{"--baseline", "baseline.json"}, want: "must be provided together"},
		{name: "multiple", args: []string{"--changes", "changes.yaml", "--baseline", "baseline.json", "--candidate", "candidate.json"}, want: "exactly one change source"},
		{name: "orphan name", args: []string{"--changes", "changes.yaml", "--change-set-name", "generated"}, want: "requires --baseline"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			args := []string{"check", "--config", "missing-config.yaml"}
			args = append(args, test.args...)
			var stdout, stderr bytes.Buffer
			exitCode := Run(context.Background(), args, &stdout, &stderr)
			if exitCode != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), test.want) ||
				strings.Contains(stderr.String(), "missing-config.yaml") {
				t.Fatalf("exit = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
			}
		})
	}
}

func TestCanonicalDiffWritesFullReportAndActionableChangeSet(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	baselinePath := filepath.Join(root, "baseline.json")
	candidatePath := filepath.Join(root, "candidate.json")
	writeSnapshotFixture(t, baselinePath, []snapshot.Metric{
		{Name: "removed_metric", Type: "counter", Labels: []string{"job"}},
		{Name: "stable_metric", Type: "gauge", Labels: []string{"job", "zone"}},
	})
	writeSnapshotFixture(t, candidatePath, []snapshot.Metric{
		{Name: "new_metric", Type: "counter", Labels: []string{}},
		{Name: "stable_metric", Type: "gauge", Labels: []string{"job"}},
	})
	diffPath := filepath.Join(root, "diff.json")
	changesPath := filepath.Join(root, "changes.yaml")
	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{
		"diff", "--baseline", baselinePath, "--candidate", candidatePath,
		"--name", "detected-contract", "--output", diffPath, "--changes-output", changesPath,
	}, &stdout, &stderr)
	if exitCode != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	diffContents, err := os.ReadFile(diffPath)
	if err != nil {
		t.Fatal(err)
	}
	var result snapshot.Diff
	if err := json.Unmarshal(diffContents, &result); err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != snapshot.DiffSchemaVersion || len(result.Differences) != 3 || len(result.ChangeSet.Changes) != 2 {
		t.Fatalf("diff = %#v", result)
	}
	changeSet, err := config.LoadChangeSet(context.Background(), changesPath)
	if err != nil {
		t.Fatal(err)
	}
	if changeSet.Metadata.Name != "detected-contract" || len(changeSet.Changes) != 2 ||
		changeSet.Changes[0].Metadata["source.candidate.file"] != candidatePath {
		t.Fatalf("changeSet = %#v", changeSet)
	}

	var validateOut, validateErr bytes.Buffer
	if got := Run(context.Background(), []string{"validate", "--snapshot", candidatePath}, &validateOut, &validateErr); got != 0 ||
		validateOut.String() != "TelemetrySnapshot is valid.\nMetrics: 2\n" || validateErr.Len() != 0 {
		t.Fatalf("validate exit = %d, stdout = %q, stderr = %q", got, validateOut.String(), validateErr.String())
	}
}

func TestCanonicalCheckSnapshotSemanticDriftFailsIncomplete(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	rulesPath := filepath.Join(root, "rules.yaml")
	writeCLIFixture(t, rulesPath, `groups:
  - name: requests
    rules:
      - alert: RequestsMissing
        expr: requests_total == 0
        labels: {severity: critical}
`)
	configPath := filepath.Join(root, "tcg.yaml")
	writeCLIFixture(t, configPath, fmt.Sprintf(`apiVersion: tcg/v1alpha1
kind: Config
sources:
  prometheusRules:
    - path: %q
      required: true
output:
  formats: [json]
`, rulesPath))
	baselinePath := filepath.Join(root, "baseline.json")
	candidatePath := filepath.Join(root, "candidate.json")
	writeSnapshotFixture(t, baselinePath, []snapshot.Metric{{Name: "requests_total", Type: "counter", Labels: []string{"job"}}})
	writeSnapshotFixture(t, candidatePath, []snapshot.Metric{{Name: "requests_total", Type: "gauge", Labels: []string{"job"}}})

	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{
		"check", "--config", configPath, "--baseline", baselinePath, "--candidate", candidatePath, "--format", "json",
	}, &stdout, &stderr)
	if exitCode != safety.ExitCode(safety.StatusIncomplete) || stderr.Len() != 0 {
		t.Fatalf("exit = %d, stderr = %q", exitCode, stderr.String())
	}
	var result safety.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != safety.StatusIncomplete || len(result.ChangeSet.Changes) != 0 ||
		len(result.Diagnostics) != 1 || !result.Diagnostics[0].Required {
		t.Fatalf("result = %#v", result)
	}
}

func TestCanonicalDiffSemanticDriftWritesEvidenceAndReturnsIncomplete(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	baselinePath := filepath.Join(root, "baseline.json")
	candidatePath := filepath.Join(root, "candidate.json")
	writeSnapshotFixture(t, baselinePath, []snapshot.Metric{{Name: "requests", Type: "counter", Labels: []string{}}})
	writeSnapshotFixture(t, candidatePath, []snapshot.Metric{{Name: "requests", Type: "gauge", Labels: []string{}}})
	diffPath := filepath.Join(root, "diff.json")
	changesPath := filepath.Join(root, "changes.yaml")
	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{
		"diff", "--baseline", baselinePath, "--candidate", candidatePath,
		"--output", diffPath, "--changes-output", changesPath,
	}, &stdout, &stderr)
	if exitCode != safety.ExitCode(safety.StatusIncomplete) || stdout.Len() != 0 ||
		!strings.Contains(stderr.String(), "Diagnostic [telemetry_snapshot/required]") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	contents, err := os.ReadFile(diffPath)
	if err != nil {
		t.Fatal(err)
	}
	var result snapshot.Diff
	if err := json.Unmarshal(contents, &result); err != nil {
		t.Fatal(err)
	}
	changeSet, err := config.LoadChangeSet(context.Background(), changesPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || len(changeSet.Changes) != 0 {
		t.Fatalf("diff = %#v, changeSet = %#v", result, changeSet)
	}
}

func TestCanonicalCheckWeaverMissingMappingFailsIncompleteWithKnownChanges(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "tcg.yaml")
	writeCLIFixture(t, configPath, `apiVersion: tcg/v1alpha1
kind: Config
sources:
  prometheusRules:
    - path: /definitely/missing/tcg-weaver-optional/*.yaml
      required: false
output:
  formats: [json]
`)
	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{
		"check",
		"--config", configPath,
		"--weaver-diff", filepath.Join(root, "adapters", "weaver", "testdata", "diff-v2.json"),
		"--weaver-mapping", filepath.Join(root, "adapters", "weaver", "testdata", "mapping-incomplete.yaml"),
		"--format", "json",
	}, &stdout, &stderr)
	if exitCode != safety.ExitCode(safety.StatusIncomplete) || stderr.Len() != 0 {
		t.Fatalf("exit = %d, stderr = %q", exitCode, stderr.String())
	}
	var result safety.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != safety.StatusIncomplete || len(result.ChangeSet.Changes) != 2 {
		t.Fatalf("result = %#v", result)
	}
	foundRequiredWeaver := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Adapter == "weaver" && diagnostic.Required {
			foundRequiredWeaver = true
		}
	}
	if !foundRequiredWeaver {
		t.Fatalf("missing required Weaver diagnostic: %#v", result.Diagnostics)
	}
}

func TestCanonicalValidateWeaverMissingMappingReturnsIncomplete(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", ".."))
	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{
		"validate",
		"--weaver-diff", filepath.Join(root, "adapters", "weaver", "testdata", "diff-v2.json"),
		"--weaver-mapping", filepath.Join(root, "adapters", "weaver", "testdata", "mapping-incomplete.yaml"),
	}, &stdout, &stderr)
	if exitCode != safety.ExitCode(safety.StatusIncomplete) || stdout.Len() != 0 ||
		!strings.Contains(stderr.String(), "Diagnostic [weaver/required]") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
}

func TestCanonicalSnapshotRejectsUnboundedOptionsBeforeNetwork(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "zero metrics", args: []string{"--max-metrics", "0"}, want: "must be positive"},
		{name: "negative series", args: []string{"--max-series", "-1"}, want: "must be positive"},
		{name: "long timeout", args: []string{"--timeout", "11m"}, want: "no greater than 10m"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			args := []string{"snapshot", "--prometheus", "http://127.0.0.1:1"}
			args = append(args, test.args...)
			var stdout, stderr bytes.Buffer
			exitCode := Run(context.Background(), args, &stdout, &stderr)
			if exitCode != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), test.want) ||
				strings.Contains(stderr.String(), "connection refused") {
				t.Fatalf("exit = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
			}
		})
	}
}

func TestCanonicalSnapshotCollectsPrometheusContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer snapshot-secret" {
			t.Errorf("missing bearer authorization")
		}
		writer.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(request.URL.Path, "/metadata") {
			fmt.Fprint(writer, `{"status":"success","data":{"requests_total":[{"type":"counter","unit":"requests"}]}}`)
			return
		}
		fmt.Fprint(writer, `{"status":"success","data":[{"__name__":"requests_total","job":"api"}]}`)
	}))
	defer server.Close()
	t.Setenv("TCG_SNAPSHOT_TEST_TOKEN", "snapshot-secret")

	outputPath := filepath.Join(t.TempDir(), "snapshot.json")
	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{
		"snapshot", "--prometheus", server.URL, "--name", "checkout", "--output", outputPath,
		"--bearer-token-env", "TCG_SNAPSHOT_TEST_TOKEN", "--max-metrics", "10", "--max-series", "10",
	}, &stdout, &stderr)
	if exitCode != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	contract, err := snapshot.Load(context.Background(), outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if contract.Metadata.Name != "checkout" || len(contract.Spec.Metrics) != 1 ||
		!reflect.DeepEqual(contract.Spec.Metrics[0].Labels, []string{"job"}) {
		t.Fatalf("snapshot = %#v", contract)
	}
	contents, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "snapshot-secret") {
		t.Fatal("snapshot leaked bearer token")
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

func writeSnapshotFixture(t *testing.T, path string, metrics []snapshot.Metric) {
	t.Helper()
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	contents, err := snapshot.Marshal(snapshot.Snapshot{
		APIVersion: snapshot.APIVersion,
		Kind:       snapshot.Kind,
		Metadata:   snapshot.Metadata{Name: name},
		Spec:       snapshot.Spec{Domain: domain.DomainPrometheus, Metrics: metrics},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}
