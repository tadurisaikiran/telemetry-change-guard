package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/tadurisaikiran/telemetry-change-guard/adapters/prometheussnapshot"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/analysis"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/changesource"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/config"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/report"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/safety"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/snapshot"
)

const canonicalUsage = `Telemetry Change Guard

Usage:
  telemetry-change-guard check --config <path> (--changes <path> | --weaver-diff <path> --weaver-mapping <path> | --baseline <snapshot> --candidate <snapshot>) [--mode audit|warn|enforce]
  telemetry-change-guard snapshot --prometheus <url> --output <path>
  telemetry-change-guard diff --baseline <snapshot> --candidate <snapshot> [--output <path>] [--changes-output <path>]
  telemetry-change-guard validate [--changes <path> | --snapshot <path> | --weaver-diff <path> --weaver-mapping <path>] [--config <path>]
  telemetry-change-guard impact --config <path> --symbol <metric>
  telemetry-change-guard graph --config <path> [--output <path>]
  telemetry-change-guard migration check --config <path> (--plan <path> | --weaver-diff <path> --weaver-mapping <path>) [--json-output <path>] [--status-output <path>]
  telemetry-change-guard migration validate --plan <path>
  telemetry-change-guard migration advise --config <path> --plan <path> --question <text> --ai-command <executable>
  telemetry-change-guard migration remediate --config <path> --plan <path> --ai-command <executable>

Commands:
  check       Evaluate the operational safety of a ChangeSet
  snapshot    Capture a bounded deterministic Prometheus telemetry contract
  diff        Compare baseline and candidate telemetry snapshots
  validate    Validate generic change and analysis inputs
  impact      Explain dependency paths for one Prometheus metric
  graph       Export the dependency graph as JSON
  migration   Run the legacy migration-readiness workflow through shared code

The temporary tmr binary remains available for existing migration automation.
`

const migrationUsage = `Telemetry Change Guard migration workflow

Usage:
  telemetry-change-guard migration check --config <path> --plan <path> [--json-output <path>] [--status-output <path>]
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
	case "snapshot":
		return runSnapshotCommand(ctx, args[1:], stdout, stderr)
	case "diff":
		return runDiffCommand(ctx, args[1:], stdout, stderr)
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
		fmt.Fprintln(stderr, "Usage: telemetry-change-guard check --config <path> (--changes <path> | --weaver-diff <path> --weaver-mapping <path> | --baseline <snapshot> --candidate <snapshot>) [--mode audit|warn|enforce]")
		flags.PrintDefaults()
	}
	configPath := flags.String("config", "", "path to an analysis configuration")
	changeSetPath := flags.String("changes", "", "path to a tcg/v1alpha1 ChangeSet manifest")
	weaverDiffPath := flags.String("weaver-diff", "", "path to a Weaver registry diff JSON document")
	weaverMappingPath := flags.String("weaver-mapping", "", "path to an explicit Weaver backend mapping")
	baselinePath := flags.String("baseline", "", "path to a baseline telemetry snapshot")
	candidatePath := flags.String("candidate", "", "path to a candidate telemetry snapshot")
	changeSetName := flags.String("change-set-name", "", "optional name for a snapshot-derived ChangeSet")
	rollout := flags.String("mode", string(safety.RolloutEnforce), "policy rollout mode: audit, warn, or enforce")
	format := flags.String("format", "", "report format: console, json, or markdown")
	output := flags.String("output", "", "optional report output path")
	jsonOutput := flags.String("json-output", "", "optional companion JSON report path")
	statusOutput := flags.String("status-output", "", "optional authoritative status output path")
	if err := flags.Parse(args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 || *configPath == "" {
		fmt.Fprintln(stderr, "check requires --config and exactly one change source and accepts no positional arguments")
		return 1
	}
	if err := validateOutputPaths(*output, *jsonOutput, *statusOutput); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	mode, err := parseRolloutMode(*rollout)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	changeSource, err := selectChangeSource(
		*changeSetPath,
		*weaverDiffPath,
		*weaverMappingPath,
		*baselinePath,
		*candidatePath,
		*changeSetName,
	)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	configuration, err := config.LoadConfig(ctx, *configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	changeSet, sourceDiagnostics, err := changeSource.Detect(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	policy := safety.DefaultPolicy()
	policy.Mode = mode
	result, _, _, analysisErr := analysis.RunSafetyWithDiagnostics(
		ctx,
		configuration,
		changeSet,
		policy,
		sourceDiagnostics,
	)

	selectedFormat := *format
	if selectedFormat == "" {
		selectedFormat = configuration.Output.Formats[0]
	}
	contents, err := renderSafetyResult(selectedFormat, result)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	var jsonContents []byte
	if *jsonOutput != "" {
		jsonContents, err = renderSafetyResult("json", result)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
	}
	if err := writeOutput(*output, contents, stdout); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if *jsonOutput != "" {
		if err := writeOutput(*jsonOutput, jsonContents, io.Discard); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
	}
	if *statusOutput != "" {
		if err := writeOutput(*statusOutput, []byte(result.Status+"\n"), io.Discard); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
	}
	if analysisErr != nil {
		fmt.Fprintf(stderr, "Error: %v\n", analysisErr)
	}
	return safety.ExitCode(result.Status)
}

func selectChangeSource(
	changeSetPath string,
	weaverDiffPath string,
	weaverMappingPath string,
	baselinePath string,
	candidatePath string,
	changeSetName string,
) (changesource.ChangeSource, error) {
	if (weaverDiffPath == "") != (weaverMappingPath == "") {
		return nil, fmt.Errorf("--weaver-diff and --weaver-mapping must be provided together")
	}
	if (baselinePath == "") != (candidatePath == "") {
		return nil, fmt.Errorf("--baseline and --candidate must be provided together")
	}
	if changeSetName != "" && baselinePath == "" {
		return nil, fmt.Errorf("--change-set-name requires --baseline and --candidate")
	}
	sourceCount := 0
	if changeSetPath != "" {
		sourceCount++
	}
	if weaverDiffPath != "" {
		sourceCount++
	}
	if baselinePath != "" {
		sourceCount++
	}
	if sourceCount != 1 {
		return nil, fmt.Errorf(
			"exactly one change source is required: --changes, --weaver-diff with --weaver-mapping, or --baseline with --candidate",
		)
	}
	if changeSetPath != "" {
		return changesource.Explicit{Path: changeSetPath}, nil
	}
	if weaverDiffPath != "" {
		return changesource.Weaver{DiffPath: weaverDiffPath, MappingPath: weaverMappingPath}, nil
	}
	return changesource.SnapshotPair{
		BaselinePath:  baselinePath,
		CandidatePath: candidatePath,
		Name:          changeSetName,
	}, nil
}

func runSnapshotCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: telemetry-change-guard snapshot --prometheus <url> --output <path>")
		flags.PrintDefaults()
	}
	prometheusURL := flags.String("prometheus", "", "Prometheus base URL")
	name := flags.String("name", "prometheus", "deterministic snapshot name")
	output := flags.String("output", "", "snapshot output path; defaults to stdout")
	timeoutValue := flags.String("timeout", "60s", "total Prometheus collection timeout")
	maxMetrics := flags.Int("max-metrics", 50_000, "maximum collected metric families")
	maxSeries := flags.Int("max-series", 100_000, "maximum inspected Prometheus series")
	bearerTokenEnv := flags.String("bearer-token-env", "", "environment variable containing an optional Prometheus bearer token")
	if err := flags.Parse(args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 || *prometheusURL == "" {
		fmt.Fprintln(stderr, "snapshot requires --prometheus and accepts no positional arguments")
		return 1
	}
	timeout, err := time.ParseDuration(*timeoutValue)
	if err != nil || timeout <= 0 || timeout > 10*time.Minute {
		fmt.Fprintln(stderr, "Error: --timeout must be a positive Go duration no greater than 10m")
		return 1
	}
	if *maxMetrics <= 0 || *maxSeries <= 0 {
		fmt.Fprintln(stderr, "Error: --max-metrics and --max-series must be positive")
		return 1
	}
	var token string
	if *bearerTokenEnv != "" {
		var exists bool
		token, exists = os.LookupEnv(*bearerTokenEnv)
		if !exists || token == "" {
			fmt.Fprintf(stderr, "Error: bearer token environment variable %q is unset or empty\n", *bearerTokenEnv)
			return 1
		}
	}
	contract, err := (prometheussnapshot.Client{
		BaseURL:     *prometheusURL,
		Timeout:     timeout,
		MaxMetrics:  *maxMetrics,
		MaxSeries:   *maxSeries,
		BearerToken: token,
	}).Collect(ctx, *name)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	contents, err := snapshot.Marshal(contract)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if err := writeOutput(*output, contents, stdout); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func runDiffCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("diff", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: telemetry-change-guard diff --baseline <snapshot> --candidate <snapshot> [--output <path>] [--changes-output <path>]")
		flags.PrintDefaults()
	}
	baselinePath := flags.String("baseline", "", "path to a baseline telemetry snapshot")
	candidatePath := flags.String("candidate", "", "path to a candidate telemetry snapshot")
	name := flags.String("name", "", "optional generated ChangeSet name")
	output := flags.String("output", "", "snapshot-diff JSON output path; defaults to stdout")
	changesOutput := flags.String("changes-output", "", "optional actionable ChangeSet YAML output path")
	if err := flags.Parse(args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 || *baselinePath == "" || *candidatePath == "" {
		fmt.Fprintln(stderr, "diff requires --baseline and --candidate and accepts no positional arguments")
		return 1
	}
	if err := validateOutputPaths(*output, *changesOutput, ""); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	result, err := snapshot.CompareFiles(ctx, *baselinePath, *candidatePath, *name)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	diffContents, err := snapshot.MarshalDiff(result)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	var changeContents []byte
	if *changesOutput != "" {
		changeContents, err = config.MarshalChangeSet(result.ChangeSet)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
	}
	if err := writeOutput(*output, diffContents, stdout); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if *changesOutput != "" {
		if err := writeOutput(*changesOutput, changeContents, io.Discard); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
	}
	if writeDiscoveryDiagnostics(stderr, result.Diagnostics) {
		return safety.ExitCode(safety.StatusIncomplete)
	}
	return 0
}

func runCanonicalValidate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: telemetry-change-guard validate [--changes <path> | --snapshot <path> | --weaver-diff <path> --weaver-mapping <path>] [--config <path>]")
		flags.PrintDefaults()
	}
	changeSetPath := flags.String("changes", "", "path to a tcg/v1alpha1 ChangeSet manifest")
	snapshotPath := flags.String("snapshot", "", "path to a tcg/v1alpha1 TelemetrySnapshot")
	weaverDiffPath := flags.String("weaver-diff", "", "path to a Weaver registry diff JSON document")
	weaverMappingPath := flags.String("weaver-mapping", "", "path to an explicit Weaver backend mapping")
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
	if (*weaverDiffPath == "") != (*weaverMappingPath == "") {
		fmt.Fprintln(stderr, "--weaver-diff and --weaver-mapping must be provided together")
		return 1
	}
	changeInputCount := 0
	for _, present := range []bool{*changeSetPath != "", *snapshotPath != "", *weaverDiffPath != ""} {
		if present {
			changeInputCount++
		}
	}
	if changeInputCount > 1 {
		fmt.Fprintln(stderr, "--changes, --snapshot, and --weaver-diff/--weaver-mapping are mutually exclusive")
		return 1
	}
	if changeInputCount == 0 && *configPath == "" {
		fmt.Fprintln(stderr, "a change input or --config is required")
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
	if *snapshotPath != "" {
		contract, err := snapshot.Load(ctx, *snapshotPath)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "TelemetrySnapshot is valid.")
		fmt.Fprintf(stdout, "Metrics: %d\n", len(contract.Spec.Metrics))
	}
	if *weaverDiffPath != "" {
		changeSet, diagnostics, err := (changesource.Weaver{
			DiffPath: *weaverDiffPath, MappingPath: *weaverMappingPath,
		}).Detect(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		if len(diagnostics) != 0 {
			writeDiscoveryDiagnostics(stderr, diagnostics)
			return safety.ExitCode(safety.StatusIncomplete)
		}
		fmt.Fprintln(stdout, "Weaver diff and mapping are valid.")
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
