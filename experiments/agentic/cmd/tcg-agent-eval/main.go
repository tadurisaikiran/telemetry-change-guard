// Command tcg-agent-eval runs the explicitly experimental, bounded agentic
// feedback loop. It is intentionally a separate binary and is not part of the
// supported telemetry-change-guard CLI surface.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/tadurisaikiran/telemetry-change-guard/experiments/agentic"
)

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }

func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("tcg-agent-eval", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var (
		taskPath       string
		outputPath     string
		tcgCommand     string
		runtimeCommand string
		agentImage     string
		agentCommand   string
		agentNetwork   string
		memory         string
		cpus           string
		pids           int
		acknowledged   bool
		agentArgs      stringList
		agentEnv       stringList
		tcgEnv         stringList
	)
	flags.StringVar(&taskPath, "task", "", "path to a strict tcg-agent-task/v1alpha1 document")
	flags.StringVar(&outputPath, "output", "", "new directory for immutable run artifacts")
	flags.StringVar(&tcgCommand, "tcg-command", "telemetry-change-guard", "public TCG executable")
	flags.StringVar(&runtimeCommand, "container-runtime", "docker", "OCI-compatible container runtime executable")
	flags.StringVar(&agentImage, "agent-image", "", "local agent-adapter image reference (resolved to an immutable image ID)")
	flags.StringVar(&agentCommand, "agent-command", "", "absolute command path inside the agent image")
	flags.Var(&agentArgs, "agent-arg", "one agent command argument; repeat for multiple values")
	flags.Var(&agentEnv, "agent-env", "one host environment variable name to pass to the agent; repeat as needed")
	flags.Var(&tcgEnv, "tcg-env", "one host environment variable name to pass to TCG; repeat as needed")
	flags.StringVar(&agentNetwork, "agent-network", "none", "agent network: none (default) or bridge (explicit opt-in)")
	flags.StringVar(&memory, "agent-memory", "1g", "container memory limit")
	flags.StringVar(&cpus, "agent-cpus", "1", "container CPU limit")
	flags.IntVar(&pids, "agent-pids", 128, "container process limit (16-1024)")
	flags.BoolVar(&acknowledged, "acknowledge-experimental", false, "acknowledge that this MVP is experimental and requires human review")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: tcg-agent-eval --acknowledge-experimental --task TASK.json --output NEW_DIR --agent-image IMAGE --agent-command /path")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "The agent can draft workspace changes only. It cannot approve, commit, push, or merge them.")
		fmt.Fprintln(stderr)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 64
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "error: positional arguments are not supported")
		return 64
	}
	if !acknowledged {
		fmt.Fprintln(stderr, "error: --acknowledge-experimental is required")
		return 64
	}
	if taskPath == "" || outputPath == "" || agentImage == "" || agentCommand == "" {
		fmt.Fprintln(stderr, "error: --task, --output, --agent-image, and --agent-command are required")
		return 64
	}

	task, err := agentic.LoadTask(taskPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: load task: %v\n", err)
		return 1
	}
	sandbox, err := agentic.NewContainerSandbox(agentic.ContainerOptions{
		RuntimeCommand: runtimeCommand,
		Image:          agentImage,
		AgentCommand:   agentCommand,
		AgentArgs:      agentArgs,
		Environment:    agentEnv,
		Network:        agentNetwork,
		Memory:         memory,
		CPUs:           cpus,
		PIDs:           pids,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: configure sandbox: %v\n", err)
		return 1
	}
	evaluator, err := agentic.NewTCGEvaluator(tcgCommand, tcgEnv, nil)
	if err != nil {
		fmt.Fprintf(stderr, "error: configure TCG: %v\n", err)
		return 1
	}
	outputDirectory, err := agentic.PrepareOutputDirectory(outputPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: prepare output: %v\n", err)
		return 1
	}
	result, runErr := (agentic.Controller{Sandbox: sandbox, TCG: evaluator}).Run(ctx, task, outputDirectory)
	summary := struct {
		Outcome             agentic.Outcome `json:"outcome"`
		AuthoritativeStatus string          `json:"authoritativeStatus,omitempty"`
		Attempts            int             `json:"attempts"`
		Result              string          `json:"result"`
	}{
		Outcome:             result.Outcome,
		AuthoritativeStatus: result.AuthoritativeStatus,
		Attempts:            len(result.Attempts),
		Result:              outputDirectory + string(os.PathSeparator) + "run.json",
	}
	if err := json.NewEncoder(stdout).Encode(summary); err != nil {
		fmt.Fprintf(stderr, "error: write summary: %v\n", err)
		return 1
	}
	if runErr != nil {
		fmt.Fprintf(stderr, "error: %v\n", runErr)
	}
	return agentic.ExitCode(result, runErr)
}
