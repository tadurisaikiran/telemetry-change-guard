package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunRequiresExplicitExperimentalAcknowledgement(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	exit := run(context.Background(), []string{
		"--task", "task.json", "--output", "output", "--agent-image", "agent", "--agent-command", "/agent",
	}, &stdout, &stderr)
	if exit != 64 || !strings.Contains(stderr.String(), "--acknowledge-experimental is required") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestRunHelpIsSuccessful(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if exit := run(context.Background(), []string{"--help"}, &stdout, &stderr); exit != 0 {
		t.Fatalf("help exit=%d stderr=%q", exit, stderr.String())
	}
	if !strings.Contains(stderr.String(), "The agent can draft workspace changes only") {
		t.Fatalf("help did not state the authority boundary: %q", stderr.String())
	}
}
