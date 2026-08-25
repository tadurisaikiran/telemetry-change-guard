package agentic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type recordingRunner struct {
	results []processResult
	errors  []error
	specs   []processSpec
}

func (runner *recordingRunner) Run(_ context.Context, spec processSpec) (processResult, error) {
	runner.specs = append(runner.specs, spec)
	index := len(runner.specs) - 1
	var result processResult
	if index < len(runner.results) {
		result = runner.results[index]
	}
	var err error
	if index < len(runner.errors) {
		err = runner.errors[index]
	}
	return result, err
}

func TestContainerSandboxUsesHardenedImmutableInvocation(t *testing.T) {
	t.Parallel()
	runner := &recordingRunner{results: []processResult{
		{Stdout: []byte("sha256:" + strings.Repeat("a", 64) + "\n")},
		{Stdout: []byte(`{"schemaVersion":"tcg-agent-response/v1alpha1","summary":"drafted","changedFiles":["rules.yaml"]}`)},
	}}
	sandbox, err := NewContainerSandbox(ContainerOptions{
		RuntimeCommand: executableForTest(t),
		Image:          "example-agent:local",
		AgentCommand:   "/adapter",
		Network:        "none",
		Memory:         "512m",
		CPUs:           "0.5",
		PIDs:           64,
		Process:        runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	request := AgentRequest{SchemaVersion: AgentRequestSchemaVersion, Task: AgentTask{ID: "t", Description: "d"}, Attempt: 1, Workspace: "/workspace"}
	execution, err := sandbox.Run(context.Background(), workspace, request)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if execution.Response.Summary != "drafted" || len(runner.specs) != 2 {
		t.Fatalf("unexpected execution: %#v; calls=%d", execution, len(runner.specs))
	}
	arguments := runner.specs[1].Args
	for _, required := range []string{
		"--read-only", "--network", "none", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges", "--pull", "never",
		"--pids-limit", "64", "--memory", "512m", "--cpus", "0.5",
		"--user", "--workdir", "/workspace", "--tmpfs",
		"sha256:" + strings.Repeat("a", 64), "/adapter",
	} {
		if !slices.Contains(arguments, required) {
			t.Errorf("container arguments missing %q: %v", required, arguments)
		}
	}
	mountIndex := slices.Index(arguments, "--mount")
	if mountIndex < 0 || mountIndex+1 >= len(arguments) || !strings.Contains(arguments[mountIndex+1], "dst=/workspace") {
		t.Errorf("single workspace mount missing: %v", arguments)
	}
	if strings.Count(strings.Join(arguments, " "), "type=bind") != 1 {
		t.Errorf("want exactly one bind mount: %v", arguments)
	}
	if !strings.Contains(string(runner.specs[1].Stdin), `"guardrails":null`) {
		t.Errorf("request was not passed over stdin: %s", runner.specs[1].Stdin)
	}
}

func TestContainerSandboxFailsClosedOnMalformedOrTimedOutAgent(t *testing.T) {
	t.Parallel()
	for name, scenario := range map[string]struct {
		result processResult
		err    error
	}{
		"malformed": {result: processResult{Stdout: []byte(`{"schemaVersion":"tcg-agent-response/v1alpha1","summary":"ok","status":"PASS"}`)}},
		"timeout":   {result: processResult{ExitCode: 1, TimedOut: true}, err: context.DeadlineExceeded},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runner := &recordingRunner{
				results: []processResult{{Stdout: []byte("sha256:" + strings.Repeat("b", 64))}, scenario.result},
				errors:  []error{nil, scenario.err},
			}
			sandbox, err := NewContainerSandbox(ContainerOptions{RuntimeCommand: executableForTest(t), Image: "agent", AgentCommand: "/agent", Process: runner})
			if err != nil {
				t.Fatal(err)
			}
			execution, err := sandbox.Run(context.Background(), t.TempDir(), AgentRequest{SchemaVersion: AgentRequestSchemaVersion, Task: AgentTask{ID: "t", Description: "d"}, Attempt: 1, Workspace: "/workspace"})
			if err == nil {
				t.Fatal("expected sandbox to fail closed")
			}
			if name == "timeout" && (!execution.TimedOut || !errors.Is(scenario.err, context.DeadlineExceeded)) {
				t.Fatalf("timeout metadata lost: %#v", execution)
			}
		})
	}
}

func executableForTest(t *testing.T) string {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
