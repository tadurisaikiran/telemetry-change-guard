package agentic

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type fakeSandbox struct {
	runs   int
	mutate func(int, string) error
	fail   error
}

func (sandbox *fakeSandbox) Identity(context.Context) (SandboxIdentity, error) {
	return SandboxIdentity{RuntimeCommand: "/runtime", ImageReference: "fixture", ImageID: "sha256:fixture", AgentCommand: "/agent", Network: "none"}, nil
}

func (sandbox *fakeSandbox) Run(_ context.Context, workspace string, request AgentRequest) (AgentExecution, error) {
	sandbox.runs++
	if sandbox.mutate != nil {
		if err := sandbox.mutate(sandbox.runs, workspace); err != nil {
			return AgentExecution{}, err
		}
	}
	response := AgentResponse{SchemaVersion: AgentResponseSchemaVersion, Summary: fmt.Sprintf("attempt %d", request.Attempt)}
	raw, _ := json.Marshal(response)
	execution := AgentExecution{Response: response, RawResponse: raw, Started: true, Duration: time.Millisecond}
	if sandbox.fail != nil {
		return execution, sandbox.fail
	}
	return execution, nil
}

type fakeEvaluator struct {
	statuses []string
	runs     int
}

func (evaluator *fakeEvaluator) Identity() ToolIdentity {
	return ToolIdentity{Command: "/telemetry-change-guard", SHA256: "fixture"}
}

func (evaluator *fakeEvaluator) Evaluate(_ context.Context, _, _, _, attemptDirectory string, _ time.Duration) (Evaluation, error) {
	index := evaluator.runs
	evaluator.runs++
	if index >= len(evaluator.statuses) {
		return Evaluation{}, fmt.Errorf("unexpected evaluation %d", index+1)
	}
	status := evaluator.statuses[index]
	resultPath := filepath.Join(attemptDirectory, "tcg-result.json")
	statusPath := filepath.Join(attemptDirectory, "tcg-status.txt")
	stderrPath := filepath.Join(attemptDirectory, "tcg-stderr.txt")
	if err := os.WriteFile(resultPath, []byte(`{"schemaVersion":"tcg-result/v1alpha1"}`), 0o600); err != nil {
		return Evaluation{}, err
	}
	if err := os.WriteFile(statusPath, []byte(status+"\n"), 0o600); err != nil {
		return Evaluation{}, err
	}
	if err := os.WriteFile(stderrPath, nil, 0o600); err != nil {
		return Evaluation{}, err
	}
	return Evaluation{
		Status: status, ExitCode: tcgExitCode(status),
		Started: true, Duration: time.Millisecond, Command: []string{"/telemetry-change-guard", "check"},
		ResultArtifact: resultPath, StatusArtifact: statusPath, StderrArtifact: stderrPath,
		Result: tcgMachineResult{SchemaVersion: TCGResultSchemaVersion, Status: status},
	}, nil
}

func TestControllerRetriesBlockThenProducesReviewOnlyDiff(t *testing.T) {
	t.Parallel()
	task, output := controllerFixture(t, 3)
	sandbox := &fakeSandbox{mutate: func(attempt int, workspace string) error {
		if attempt == 2 {
			return os.WriteFile(filepath.Join(workspace, "rules.yaml"), []byte("metric: new\n"), 0o600)
		}
		return nil
	}}
	evaluator := &fakeEvaluator{statuses: []string{"BLOCK", "PASS"}}
	result, err := (Controller{Sandbox: sandbox, TCG: evaluator}).Run(context.Background(), task, output)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Outcome != OutcomeReviewReady || result.AuthoritativeStatus != "PASS" || len(result.Attempts) != 2 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if ExitCode(result, err) != 0 || len(result.FinalChangedFiles) != 1 || result.FinalDiffArtifact != "final.diff" {
		t.Fatalf("review result is incomplete: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(output, "run.json")); err != nil {
		t.Fatalf("run artifact missing: %v", err)
	}
	diff, err := os.ReadFile(filepath.Join(output, result.FinalDiffArtifact))
	if err != nil || len(diff) == 0 {
		t.Fatalf("review diff missing: bytes=%d err=%v", len(diff), err)
	}
	original, err := os.ReadFile(filepath.Join(task.RepositoryPath, "workspace", "rules.yaml"))
	if err != nil || string(original) != "metric: old\n" {
		t.Fatalf("source repository was mutated: %q err=%v", original, err)
	}
}

func TestControllerTerminalStatuses(t *testing.T) {
	t.Parallel()
	scenarios := []struct {
		name     string
		statuses []string
		outcome  Outcome
		exit     int
	}{
		{name: "blocked", statuses: []string{"BLOCK", "BLOCK"}, outcome: OutcomeBlocked, exit: 2},
		{name: "incomplete", statuses: []string{"INCOMPLETE"}, outcome: OutcomeIncomplete, exit: 3},
	}
	for _, scenario := range scenarios {
		scenario := scenario
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			task, output := controllerFixture(t, 2)
			result, err := (Controller{Sandbox: &fakeSandbox{}, TCG: &fakeEvaluator{statuses: scenario.statuses}}).Run(context.Background(), task, output)
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if result.Outcome != scenario.outcome || ExitCode(result, err) != scenario.exit || len(result.Attempts) != len(scenario.statuses) {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestControllerDetectsControlTamperingAndWorkspaceEscape(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name   string
		mutate func(ResolvedTask) func(int, string) error
	}{
		{
			name: "control tampering",
			mutate: func(task ResolvedTask) func(int, string) error {
				return func(_ int, _ string) error { return os.WriteFile(task.ConfigPath, []byte("tampered\n"), 0o600) }
			},
		},
		{
			name: "outside workspace",
			mutate: func(_ ResolvedTask) func(int, string) error {
				return func(_ int, workspace string) error {
					return os.WriteFile(filepath.Join(filepath.Dir(workspace), "outside.txt"), []byte("escape\n"), 0o600)
				}
			},
		},
	} {
		scenario := scenario
		t.Run(scenario.name, func(t *testing.T) {
			task, output := controllerFixture(t, 1)
			result, err := (Controller{
				Sandbox: &fakeSandbox{mutate: scenario.mutate(task)},
				TCG:     &fakeEvaluator{statuses: []string{"PASS"}},
			}).Run(context.Background(), task, output)
			if err == nil || result.Outcome != OutcomeIntegrityFailed || ExitCode(result, err) != 1 {
				t.Fatalf("expected integrity failure, got result=%#v err=%v", result, err)
			}
			if _, statErr := os.Stat(filepath.Join(output, "run.json")); statErr != nil {
				t.Fatalf("failure evidence missing: %v", statErr)
			}
		})
	}
}

func TestControllerStopsOnTCGError(t *testing.T) {
	t.Parallel()
	task, output := controllerFixture(t, 3)
	result, err := (Controller{Sandbox: &fakeSandbox{}, TCG: &fakeEvaluator{statuses: []string{"ERROR"}}}).Run(context.Background(), task, output)
	if err == nil || result.Outcome != OutcomeError || len(result.Attempts) != 1 || ExitCode(result, err) != 1 {
		t.Fatalf("expected immediate ERROR stop, got result=%#v err=%v", result, err)
	}
}

func TestValidateWorkspaceTreeRejectsGitMetadata(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	if err := validateWorkspaceTree(root); err == nil {
		t.Fatal("workspace Git metadata was accepted")
	}
}

func controllerFixture(t *testing.T, attempts int) (ResolvedTask, string) {
	t.Helper()
	root := t.TempDir()
	repository := filepath.Join(root, "repository")
	mustMkdir(t, filepath.Join(repository, "workspace"))
	mustWrite(t, filepath.Join(repository, "workspace", "rules.yaml"), "metric: old\n")
	mustRunGit(t, repository, "init", "--quiet")
	mustRunGit(t, repository, "add", ".")
	mustRunGit(t, repository, "-c", "user.name=TCG Test", "-c", "user.email=tcg-test@example.invalid", "commit", "--quiet", "-m", "fixture")
	config := filepath.Join(root, "tcg.yaml")
	changes := filepath.Join(root, "changes.yaml")
	mustWrite(t, config, "config\n")
	mustWrite(t, changes, "changes\n")
	output, err := PrepareOutputDirectory(filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	return ResolvedTask{
		Task: Task{
			ID: "controller-test", Description: "repair", Repository: RepositorySpec{Revision: "HEAD"}, AgentWorkspace: "workspace",
		},
		RepositoryPath: repository, ConfigPath: config, ChangesPath: changes,
		IntegrityAbsPaths: []string{config, changes}, AgentTimeout: time.Minute, TCGTimeout: time.Minute,
		TotalTimeout: 2 * time.Minute, MaxAttempts: attempts, MaxChangedFiles: 10, MaxDiffBytes: 1 << 20,
	}, output
}

func mustRunGit(t *testing.T, repository string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
