// Command verifier exercises the canonical CLI across the combined
// control-plane lifecycle and validates its complete machine result.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/impact"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/safety"
)

const scenarioTimeout = 30 * time.Second

type consumerExpectation struct {
	kind   domain.ConsumerKind
	impact impact.Type
	method domain.EvidenceMethod
}

var expectedConsumers = map[string]consumerExpectation{
	"checkout-worker": {
		kind:   domain.ConsumerKindAutoscaler,
		impact: impact.TypeScalingRisk,
		method: domain.EvidenceMethodPromQLAST,
	},
	"checkout-rollout-gate / success-rate": {
		kind:   domain.ConsumerKindDeploymentGate,
		impact: impact.TypeDeploymentGateRisk,
		method: domain.EvidenceMethodPromQLAST,
	},
	"checkout-api": {
		kind:   domain.ConsumerKindAutoscaler,
		impact: impact.TypeScalingRisk,
		method: domain.EvidenceMethodExplicitMapping,
	},
}

var expectedChanges = []string{
	"remove-legacy-request-metric",
	"remove-legacy-route-label",
}

func main() {
	binary := flag.String("binary", "", "absolute path to telemetry-change-guard")
	repository := flag.String("repository", "", "absolute repository root")
	flag.Parse()
	if flag.NArg() != 0 || *binary == "" || *repository == "" {
		fmt.Fprintln(os.Stderr, "verifier requires --binary and --repository")
		os.Exit(1)
	}
	if !filepath.IsAbs(*binary) || !filepath.IsAbs(*repository) {
		fmt.Fprintln(os.Stderr, "--binary and --repository must be absolute paths")
		os.Exit(1)
	}
	if err := verifyLifecycle(*binary, *repository); err != nil {
		fmt.Fprintf(os.Stderr, "control-plane lifecycle failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Telemetry Change Guard combined control-plane lifecycle passed.")
}

func verifyLifecycle(binary, repository string) error {
	legacy, err := runScenario(binary, repository, "legacy", safety.StatusBlock)
	if err != nil {
		return err
	}
	if err := verifyLegacy(legacy); err != nil {
		return fmt.Errorf("legacy scenario: %w", err)
	}

	migrated, err := runScenario(binary, repository, "migrated", safety.StatusPass)
	if err != nil {
		return err
	}
	if len(migrated.Findings) != 0 || len(migrated.Decisions) != 0 || len(migrated.Diagnostics) != 0 {
		return fmt.Errorf("migrated scenario retained findings, decisions, or diagnostics: %#v", migrated)
	}

	incomplete, err := runScenario(binary, repository, "incomplete", safety.StatusIncomplete)
	if err != nil {
		return err
	}
	if err := verifyIncomplete(incomplete); err != nil {
		return fmt.Errorf("incomplete scenario: %w", err)
	}
	return nil
}

func runScenario(binary, repository, scenario string, expected safety.Status) (safety.Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), scenarioTimeout)
	defer cancel()
	configuration := filepath.Join("e2e", "controlplane", "scenarios", scenario, "config.yaml")
	command := exec.CommandContext(
		ctx,
		binary,
		"check",
		"--config", configuration,
		"--changes", filepath.Join("e2e", "controlplane", "changes.yaml"),
		"--format", "json",
	)
	command.Dir = repository
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	if ctx.Err() != nil {
		return safety.Result{}, fmt.Errorf("%s scenario exceeded %s: %w", scenario, scenarioTimeout, ctx.Err())
	}
	actualExit := 0
	if runErr != nil {
		var exitError *exec.ExitError
		if !errors.As(runErr, &exitError) {
			return safety.Result{}, fmt.Errorf("run %s scenario: %w", scenario, runErr)
		}
		actualExit = exitError.ExitCode()
	}
	expectedExit := safety.ExitCode(expected)
	if actualExit != expectedExit {
		return safety.Result{}, fmt.Errorf(
			"%s scenario exited %d, want %d; stderr=%q stdout=%q",
			scenario,
			actualExit,
			expectedExit,
			stderr.String(),
			stdout.String(),
		)
	}
	if strings.TrimSpace(stderr.String()) != "" {
		return safety.Result{}, fmt.Errorf("%s scenario wrote unexpected stderr: %q", scenario, stderr.String())
	}
	if bytes.Contains(stdout.Bytes(), []byte("prometheus.example.test")) {
		return safety.Result{}, fmt.Errorf("%s scenario retained a provider address in its report", scenario)
	}
	var result safety.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return safety.Result{}, fmt.Errorf("decode %s result: %w; output=%q", scenario, err, stdout.String())
	}
	if result.SchemaVersion != safety.ResultSchemaVersion || result.Status != expected || len(result.Errors) != 0 {
		return safety.Result{}, fmt.Errorf("%s scenario returned unexpected envelope: %#v", scenario, result)
	}
	return result, nil
}

func verifyLegacy(result safety.Result) error {
	expectedFindingCount := len(expectedConsumers) * len(expectedChanges)
	if len(result.Findings) != expectedFindingCount || len(result.Decisions) != expectedFindingCount || len(result.Diagnostics) != 0 {
		return fmt.Errorf("result counts are findings=%d decisions=%d diagnostics=%d, want %d/%d/0",
			len(result.Findings), len(result.Decisions), len(result.Diagnostics), expectedFindingCount, expectedFindingCount)
	}
	seen := make(map[string]int, expectedFindingCount)
	for _, finding := range result.Findings {
		expectedConsumer, exists := expectedConsumers[finding.Consumer.Name]
		if !exists {
			return fmt.Errorf("unexpected consumer %q", finding.Consumer.Name)
		}
		if finding.Consumer.Kind != expectedConsumer.kind || finding.Impact != expectedConsumer.impact ||
			finding.Criticality != domain.CriticalityCritical || finding.Uncertain {
			return fmt.Errorf("unexpected finding identity or severity: %#v", finding)
		}
		if len(finding.References) == 0 {
			return fmt.Errorf("finding has no evidence: %#v", finding)
		}
		for _, reference := range finding.References {
			if reference.Evidence.Method != expectedConsumer.method || reference.Evidence.Confidence != domain.ConfidenceConfirmed {
				return fmt.Errorf("unexpected evidence for %q: %#v", finding.Consumer.Name, reference.Evidence)
			}
		}
		seen[finding.Change.ID+"\x00"+finding.Consumer.Name]++
	}
	for _, change := range expectedChanges {
		for consumer := range expectedConsumers {
			if seen[change+"\x00"+consumer] != 1 {
				return fmt.Errorf("missing exact finding for change %q and consumer %q", change, consumer)
			}
		}
	}
	if err := verifyBlockingDecisions(result.Findings, result.Decisions); err != nil {
		return err
	}
	return nil
}

func verifyIncomplete(result safety.Result) error {
	expectedFindingCount := len(expectedConsumers) * len(expectedChanges)
	if len(result.Findings) != expectedFindingCount || len(result.Decisions) != expectedFindingCount || len(result.Diagnostics) != 3 {
		return fmt.Errorf("result counts are findings=%d decisions=%d diagnostics=%d, want %d/%d/3",
			len(result.Findings), len(result.Decisions), len(result.Diagnostics), expectedFindingCount, expectedFindingCount)
	}
	seenFindings := make(map[string]int, expectedFindingCount)
	for _, finding := range result.Findings {
		expectedConsumer, exists := expectedConsumers[finding.Consumer.Name]
		if !exists || finding.Consumer.Kind != expectedConsumer.kind || finding.Impact != expectedConsumer.impact ||
			finding.Criticality != domain.CriticalityCritical || !finding.Uncertain || len(finding.References) != 0 {
			return fmt.Errorf("unexpected unresolved finding: %#v", finding)
		}
		seenFindings[finding.Change.ID+"\x00"+finding.Consumer.Name]++
	}
	for _, change := range expectedChanges {
		for consumer := range expectedConsumers {
			if seenFindings[change+"\x00"+consumer] != 1 {
				return fmt.Errorf("missing unresolved finding for change %q and consumer %q", change, consumer)
			}
		}
	}
	expectedDiagnostics := map[string]int{"keda": 1, "argo_rollouts": 1, "hpa": 1}
	for _, diagnostic := range result.Diagnostics {
		if !diagnostic.Required {
			return fmt.Errorf("diagnostic is not required: %#v", diagnostic)
		}
		expectedDiagnostics[diagnostic.Adapter]--
	}
	for adapter, remaining := range expectedDiagnostics {
		if remaining != 0 {
			return fmt.Errorf("diagnostic count for %s differs by %d", adapter, remaining)
		}
	}
	if err := verifyBlockingDecisions(result.Findings, result.Decisions); err != nil {
		return err
	}
	return nil
}

func verifyBlockingDecisions(findings []impact.Finding, decisions []safety.Decision) error {
	expected := make(map[string]struct{}, len(findings))
	for _, finding := range findings {
		expected[decisionKey(finding.Change.ID, finding.Consumer.ID, finding.Impact)] = struct{}{}
	}
	for _, decision := range decisions {
		key := decisionKey(decision.ChangeID, decision.ConsumerID, decision.Impact)
		if _, exists := expected[key]; !exists {
			return fmt.Errorf("decision does not match exactly one finding: %#v", decision)
		}
		if decision.ConfiguredAction != safety.ActionBlock || decision.EffectiveAction != safety.ActionBlock {
			return fmt.Errorf("control-plane finding was not blocked: %#v", decision)
		}
		delete(expected, key)
	}
	if len(expected) != 0 {
		return fmt.Errorf("%d findings have no matching policy decision", len(expected))
	}
	return nil
}

func decisionKey(changeID, consumerID string, impactType impact.Type) string {
	return changeID + "\x00" + consumerID + "\x00" + string(impactType)
}
