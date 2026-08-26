package agentic

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type tcgArtifactRunner struct {
	status     string
	jsonStatus string
	exit       int
	unknown    bool
	spec       processSpec
}

func (runner *tcgArtifactRunner) Run(_ context.Context, spec processSpec) (processResult, error) {
	runner.spec = spec
	resultIndex := slices.Index(spec.Args, "--output")
	statusIndex := slices.Index(spec.Args, "--status-output")
	if resultIndex < 0 || statusIndex < 0 {
		return processResult{ExitCode: 1}, nil
	}
	machine := tcgMachineResult{
		SchemaVersion: TCGResultSchemaVersion,
		ChangeSet:     tcgChangeSet{Changes: []tcgChange{}},
		Status:        runner.jsonStatus,
		Findings:      []tcgFinding{},
	}
	contents, _ := json.Marshal(machine)
	if runner.unknown {
		contents = []byte(`{"schemaVersion":"tcg-result/v1alpha1","changeSet":{"apiVersion":"","kind":"","metadata":{"name":""},"changes":[]},"status":"PASS","findings":[],"agentApproval":true}`)
	}
	if err := os.WriteFile(spec.Args[resultIndex+1], contents, 0o600); err != nil {
		return processResult{}, err
	}
	if err := os.WriteFile(spec.Args[statusIndex+1], []byte(runner.status+"\n"), 0o600); err != nil {
		return processResult{}, err
	}
	return processResult{ExitCode: runner.exit}, nil
}

func TestTCGEvaluatorEnforcesMachineContract(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name       string
		status     string
		jsonStatus string
		exit       int
		unknown    bool
		wantError  bool
	}{
		{name: "consistent pass", status: "PASS", jsonStatus: "PASS", exit: 0},
		{name: "status disagreement", status: "BLOCK", jsonStatus: "PASS", exit: 2, wantError: true},
		{name: "exit disagreement", status: "PASS", jsonStatus: "PASS", exit: 2, wantError: true},
		{name: "unknown machine field", status: "PASS", jsonStatus: "PASS", exit: 0, unknown: true, wantError: true},
	} {
		scenario := scenario
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			runner := &tcgArtifactRunner{status: scenario.status, jsonStatus: scenario.jsonStatus, exit: scenario.exit, unknown: scenario.unknown}
			evaluator, err := NewTCGEvaluator(executableForTest(t), nil, runner)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			config := filepath.Join(root, "tcg.yaml")
			changes := filepath.Join(root, "changes.yaml")
			mustWrite(t, config, "config\n")
			mustWrite(t, changes, "changes\n")
			attempt := filepath.Join(root, "attempt")
			evaluation, err := evaluator.Evaluate(context.Background(), root, root, config, changes, attempt, time.Second)
			if (err != nil) != scenario.wantError {
				t.Fatalf("Evaluate() error = %v, wantError=%v", err, scenario.wantError)
			}
			if !scenario.wantError && (evaluation.Status != "PASS" || evaluation.ExitCode != 0) {
				t.Fatalf("unexpected evaluation: %#v", evaluation)
			}
			joined := strings.Join(runner.spec.Args, " ")
			for _, required := range []string{"check", "--repository-root " + root, "--mode enforce", "--format json", "--output", "--status-output"} {
				if !strings.Contains(joined, required) {
					t.Errorf("public CLI invocation missing %q: %s", required, joined)
				}
			}
		})
	}
}

func TestEvaluationControlsStayOutsideRepositoryAndDetectMutation(t *testing.T) {
	t.Parallel()
	evaluationRoot := t.TempDir()
	repository := filepath.Join(evaluationRoot, "repository")
	mustMkdir(t, repository)
	sources := t.TempDir()
	config := filepath.Join(sources, "tcg.yaml")
	changes := filepath.Join(sources, "changes.yaml")
	mustWrite(t, config, "config\n")
	mustWrite(t, changes, "changes\n")

	controls, err := materializeEvaluationControls(evaluationRoot, config, changes)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(controls.directory) != evaluationRoot || strings.HasPrefix(controls.directory, repository+string(filepath.Separator)) {
		t.Fatalf("controls are not an isolated repository sibling: %s", controls.directory)
	}
	if err := controls.Verify(); err != nil {
		t.Fatalf("fresh controls failed integrity verification: %v", err)
	}
	if err := os.WriteFile(controls.ChangesPath, []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := controls.Verify(); err == nil || !strings.Contains(err.Error(), "integrity changed") {
		t.Fatalf("control mutation was not detected: %v", err)
	}
	if err := controls.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(controls.directory); !os.IsNotExist(err) {
		t.Fatalf("private controls still exist: %v", err)
	}
}

func TestFeedbackIsBoundedAndRedactsCommonSecrets(t *testing.T) {
	t.Parallel()
	evaluation := Evaluation{Status: "BLOCK", Result: tcgMachineResult{
		Findings: []tcgFinding{{
			Change: tcgChange{ID: "change"}, Consumer: tcgConsumer{ID: "consumer", Source: SourceLocation{URL: "https://example.invalid?token=secret-value"}},
			Impact: "breaking", Criticality: "high",
		}},
		Diagnostics: []tcgDiagnostic{{Message: "authorization: very-secret", Required: true}},
	}}
	feedback := feedbackFromEvaluation(evaluation)
	encoded, _ := json.Marshal(feedback)
	if strings.Contains(string(encoded), "very-secret") || strings.Contains(string(encoded), "secret-value") {
		t.Fatalf("feedback leaked a common secret: %s", encoded)
	}
}
