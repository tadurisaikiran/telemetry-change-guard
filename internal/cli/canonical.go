package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/analysis"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/config"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/report"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/safety"
)

const canonicalUsage = `Telemetry Change Guard

Usage:
  telemetry-change-guard check --config <path> --changes <path> [--mode audit|warn|enforce] [--format console|json|markdown]
  telemetry-change-guard validate [--changes <path>] [--config <path>]
  telemetry-change-guard impact --config <path> --symbol <metric>
  telemetry-change-guard graph --config <path> [--output <path>]
  telemetry-change-guard migration check --config <path> (--plan <path> | --weaver-diff <path> --weaver-mapping <path>)
  telemetry-change-guard migration validate --plan <path>
  telemetry-change-guard migration advise --config <path> --plan <path> --question <text> --ai-command <executable>
  telemetry-change-guard migration remediate --config <path> --plan <path> --ai-command <executable>

Commands:
  check       Evaluate the operational safety of a ChangeSet
  validate    Validate generic change and analysis inputs
  impact      Explain dependency paths for one Prometheus metric
  graph       Export the dependency graph as JSON
  migration   Run the legacy migration-readiness workflow through shared code

The temporary tmr binary remains available for existing migration automation.
`

const migrationUsage = `Telemetry Change Guard migration workflow

Usage:
  telemetry-change-guard migration check --config <path> --plan <path>
  telemetry-change-guard migration validate --plan <path>
  telemetry-change-guard migration advise --config <path> --plan <path> --question <text> --ai-command <executable>
  telemetry-change-guard migration remediate --config <path> --plan <path> --ai-command <executable>
`

// Run executes the canonical Telemetry Change Guard command surface.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, canonicalUsage)
		return 1
	}

	switch args[0] {
	case "check":
		return runCheck(ctx, args[1:], stdout, stderr)
	case "validate":
		return runCanonicalValidate(ctx, args[1:], stdout, stderr)
	case "impact":
		return runSymbolImpact(ctx, "impact", true, args[1:], stdout, stderr)
	case "graph":
		return runGraphCommand(ctx, true, args[1:], stdout, stderr)
	case "migration":
		return runMigration(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, canonicalUsage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], canonicalUsage)
		return 1
	}
}

func runCheck(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: telemetry-change-guard check --config <path> --changes <path> [--mode audit|warn|enforce] [--format console|json|markdown]")
		flags.PrintDefaults()
	}
	configPath := flags.String("config", "", "path to an analysis configuration")
	changeSetPath := flags.String("changes", "", "path to a tcg/v1alpha1 ChangeSet manifest")
	rollout := flags.String("mode", string(safety.RolloutEnforce), "policy rollout mode: audit, warn, or enforce")
	format := flags.String("format", "", "report format: console, json, or markdown")
	output := flags.String("output", "", "optional report output path")
	if err := flags.Parse(args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 || *configPath == "" || *changeSetPath == "" {
		fmt.Fprintln(stderr, "check requires --config and --changes and accepts no positional arguments")
		return 1
	}
	mode, err := parseRolloutMode(*rollout)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	configuration, err := config.LoadConfig(ctx, *configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	changeSet, err := config.LoadChangeSet(ctx, *changeSetPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	policy := safety.DefaultPolicy()
	policy.Mode = mode
	result, _, _, analysisErr := analysis.RunSafety(ctx, configuration, changeSet, policy)

	selectedFormat := *format
	if selectedFormat == "" {
		selectedFormat = configuration.Output.Formats[0]
	}
	contents, err := renderSafetyResult(selectedFormat, result)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if err := writeOutput(*output, contents, stdout); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if analysisErr != nil {
		fmt.Fprintf(stderr, "Error: %v\n", analysisErr)
	}
	return safety.ExitCode(result.Status)
}

func runCanonicalValidate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: telemetry-change-guard validate [--changes <path>] [--config <path>]")
		flags.PrintDefaults()
	}
	changeSetPath := flags.String("changes", "", "path to a tcg/v1alpha1 ChangeSet manifest")
	configPath := flags.String("config", "", "path to an analysis configuration")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "validate does not accept positional arguments: %v\n", flags.Args())
		return 1
	}
	if *changeSetPath == "" && *configPath == "" {
		fmt.Fprintln(stderr, "--changes or --config is required")
		return 1
	}
	if *changeSetPath != "" {
		changeSet, err := config.LoadChangeSet(ctx, *changeSetPath)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "ChangeSet manifest is valid.")
		fmt.Fprintf(stdout, "Changes: %d\n", len(changeSet.Changes))
	}
	if *configPath != "" {
		configuration, err := config.LoadConfig(ctx, *configPath)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "Telemetry Change Guard configuration is valid.")
		fmt.Fprintf(stdout, "Sources: %d\n", sourceCount(configuration))
	}
	return 0
}

func runMigration(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, migrationUsage)
		return 1
	}
	switch args[0] {
	case "check":
		return runMigrationCheck(ctx, "migration check", "--plan", args[1:], stdout, stderr)
	case "validate":
		return runMigrationValidate(ctx, "telemetry-change-guard migration validate", "--plan", args[1:], stdout, stderr)
	case "advise":
		return runMigrationAdvise(ctx, "migration advise", "--plan", args[1:], stdout, stderr)
	case "remediate":
		return runMigrationRemediate(ctx, "migration remediate", "--plan", args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, migrationUsage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown migration command %q\n\n%s", args[0], migrationUsage)
		return 1
	}
}

func renderSafetyResult(format string, result safety.Result) ([]byte, error) {
	if format == "json" {
		return report.SafetyJSON(result)
	}
	var output bytes.Buffer
	switch format {
	case "console":
		if err := report.SafetyConsole(&output, result); err != nil {
			return nil, err
		}
	case "markdown":
		if err := report.SafetyMarkdown(&output, result); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported report format %q", format)
	}
	return output.Bytes(), nil
}

func parseRolloutMode(value string) (safety.RolloutMode, error) {
	mode := safety.RolloutMode(strings.ToLower(strings.TrimSpace(value)))
	switch mode {
	case safety.RolloutAudit, safety.RolloutWarn, safety.RolloutEnforce:
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported policy rollout mode %q", value)
	}
}
