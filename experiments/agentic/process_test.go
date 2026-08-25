package agentic

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExecProcessRunnerBoundsOutputAndHonorsDeadline(t *testing.T) {
	t.Parallel()
	executable := executableForTest(t)
	for _, scenario := range []struct {
		name      string
		mode      string
		deadline  time.Duration
		limit     int64
		wantError error
	}{
		{name: "output limit", mode: "output", deadline: time.Second, limit: 32, wantError: errOutputLimit},
		{name: "deadline", mode: "sleep", deadline: 50 * time.Millisecond, limit: 1024, wantError: context.DeadlineExceeded},
	} {
		scenario := scenario
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), scenario.deadline)
			defer cancel()
			result, err := (execProcessRunner{}).Run(ctx, processSpec{
				Command: executable,
				Args:    []string{"-test.run=TestAgenticProcessHelper", "--", scenario.mode},
				Env:     []string{"TCG_AGENTIC_PROCESS_HELPER=1"}, StdoutLimit: scenario.limit, StderrLimit: 1024,
			})
			if !errors.Is(err, scenario.wantError) {
				t.Fatalf("Run() error = %v, want %v", err, scenario.wantError)
			}
			if scenario.mode == "sleep" && !result.TimedOut {
				t.Fatalf("timeout metadata missing: %#v", result)
			}
		})
	}
}

func TestAgenticProcessHelper(t *testing.T) {
	if os.Getenv("TCG_AGENTIC_PROCESS_HELPER") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	switch mode {
	case "output":
		fmt.Print(strings.Repeat("x", 4096))
	case "sleep":
		time.Sleep(5 * time.Second)
	default:
		os.Exit(2)
	}
	os.Exit(0)
}
