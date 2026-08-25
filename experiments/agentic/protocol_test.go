package agentic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeAgentResponseRejectsUntrustedControlFields(t *testing.T) {
	t.Parallel()
	for name, document := range map[string]string{
		"unknown status":  `{"schemaVersion":"tcg-agent-response/v1alpha1","summary":"done","status":"PASS"}`,
		"unknown command": `{"schemaVersion":"tcg-agent-response/v1alpha1","summary":"done","command":"git push"}`,
		"trailing JSON":   `{"schemaVersion":"tcg-agent-response/v1alpha1","summary":"done"}{}`,
		"escaped path":    `{"schemaVersion":"tcg-agent-response/v1alpha1","summary":"done","changedFiles":["../policy.yaml"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeAgentResponse([]byte(document)); err == nil {
				t.Fatal("expected strict response validation to fail")
			}
		})
	}
}

func TestLoadTaskStrictAndResolved(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repository := filepath.Join(root, "repo")
	mustMkdir(t, filepath.Join(repository, "workspace"))
	mustWrite(t, filepath.Join(root, "tcg.yaml"), "policy: test\n")
	mustWrite(t, filepath.Join(root, "changes.yaml"), "changes: []\n")
	taskPath := filepath.Join(root, "task.json")
	mustWrite(t, taskPath, `{
  "schemaVersion":"tcg-agent-task/v1alpha1",
  "id":"repair-1",
  "description":"Repair a telemetry consumer",
  "repository":{"path":"repo","revision":"HEAD"},
  "agentWorkspace":"workspace",
  "tcg":{"config":"tcg.yaml","changes":"changes.yaml"},
  "limits":{"maxAttempts":2,"agentTimeout":"30s","totalTimeout":"2m"}
}`)

	task, err := LoadTask(taskPath)
	if err != nil {
		t.Fatalf("LoadTask() error = %v", err)
	}
	repository, err = filepath.EvalSymlinks(repository)
	if err != nil {
		t.Fatal(err)
	}
	if task.MaxAttempts != 2 || task.RepositoryPath != repository {
		t.Fatalf("unexpected resolved task: %#v", task)
	}
	if len(task.IntegrityAbsPaths) != 3 {
		t.Fatalf("integrity paths = %v, want task/config/changes", task.IntegrityAbsPaths)
	}

	contents, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	invalid := strings.Replace(string(contents), `"id":"repair-1"`, `"id":"repair-1","approval":true`, 1)
	mustWrite(t, taskPath, invalid)
	if _, err := LoadTask(taskPath); err == nil {
		t.Fatal("LoadTask accepted unknown field")
	}

	mustWrite(t, taskPath, strings.Replace(string(contents), `"agentWorkspace":"workspace"`, `"agentWorkspace":"."`, 1))
	if _, err := LoadTask(taskPath); err == nil {
		t.Fatal("LoadTask accepted the repository root as the agent workspace")
	}
}

func TestVerifyDigestsDetectsIntegrityDirectoryMembershipChanges(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "policy.yaml"), "policy\n")
	expected, err := digestPaths([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "injected.yaml"), "injected\n")
	if err := verifyDigests([]string{root}, expected); err == nil {
		t.Fatal("new file inside integrity directory was not detected")
	}
}

func FuzzDecodeAgentResponse(f *testing.F) {
	f.Add([]byte(`{"schemaVersion":"tcg-agent-response/v1alpha1","summary":"done"}`))
	f.Add([]byte(`{"status":"PASS"}`))
	f.Fuzz(func(t *testing.T, contents []byte) {
		_, _ = DecodeAgentResponse(contents)
	})
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
