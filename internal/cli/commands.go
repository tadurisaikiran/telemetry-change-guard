package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	weaveradapter "github.com/tadurisaikiran/telemetry-change-guard/adapters/weaver"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/analysis"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/config"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/explanation"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/graph"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/impact"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/readiness"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/remediation"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/report"
)

type stringListFlag []string

func (values *stringListFlag) String() string {
	return strings.Join(*values, ",")
}

func (values *stringListFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func runAnalyze(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runMigrationCheck(ctx, "analyze", "--migration", args, stdout, stderr)
}

func runMigrationCheck(
	ctx context.Context,
	commandName string,
	migrationFlag string,
	args []string,
	stdout, stderr io.Writer,
) int {
	flags := flag.NewFlagSet(commandName, flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "path to a tmr YAML configuration")
	migrationPath := flags.String(strings.TrimPrefix(migrationFlag, "--"), "", "path to a migration YAML manifest")
	weaverDiffPath := flags.String("weaver-diff", "", "path to a Weaver registry diff JSON document")
	weaverMappingPath := flags.String("weaver-mapping", "", "path to an explicit Weaver backend mapping")
	format := flags.String("format", "", "report format: console, json, or markdown")
	output := flags.String("output", "", "optional report output path")
	jsonOutput := flags.String("json-output", "", "optional companion JSON report path")
	statusOutput := flags.String("status-output", "", "optional authoritative status output path")
	remoteFlags := addRemoteEvidenceFlags(flags)
	execution := addExecutionFlags(flags)
	if err := flags.Parse(args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 || *configPath == "" {
		fmt.Fprintf(stderr, "%s requires --config and one change source and accepts no positional arguments\n", commandName)
		return 1
	}
	if err := validateOutputPaths(*output, *jsonOutput, *statusOutput); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if *migrationPath != "" && (*weaverDiffPath != "" || *weaverMappingPath != "") {
		fmt.Fprintf(stderr, "%s and --weaver-diff/--weaver-mapping are mutually exclusive\n", migrationFlag)
		return 1
	}
	if *migrationPath == "" && (*weaverDiffPath == "" || *weaverMappingPath == "") {
		fmt.Fprintf(stderr, "%s requires %s or both --weaver-diff and --weaver-mapping\n", commandName, migrationFlag)
		return 1
	}
	analysisContext, cancel, err := execution.prepare(ctx, configPath, migrationPath, weaverDiffPath, weaverMappingPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	defer cancel()
	ctx = analysisContext

	configuration, err := config.LoadConfig(ctx, *configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if err := remoteFlags.apply(&configuration); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	execution.apply(&configuration)
	migration, err := loadSelectedMigration(ctx, *migrationPath, *weaverDiffPath, *weaverMappingPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		if isWeaverIncomplete(err) {
			return 3
		}
		return 1
	}
	result, _, _, err := analysis.Run(ctx, configuration, migration)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	selectedFormat := *format
	if selectedFormat == "" {
		selectedFormat = configuration.Output.Formats[0]
	}
	contents, err := renderResult(selectedFormat, result)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	var jsonContents []byte
	if *jsonOutput != "" {
		jsonContents, err = renderResult("json", result)
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
		if err := writeOutput(*statusOutput, []byte(result.Summary.Status+"\n"), io.Discard); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
	}
	return ReadinessExitCode(result.Summary.Status)
}

func runAdvise(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runMigrationAdvise(ctx, "advise", "--migration", args, stdout, stderr)
}

func runMigrationAdvise(
	ctx context.Context,
	commandName string,
	migrationFlag string,
	args []string,
	stdout, stderr io.Writer,
) int {
	flags := flag.NewFlagSet(commandName, flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "path to a tmr YAML configuration")
	migrationPath := flags.String(strings.TrimPrefix(migrationFlag, "--"), "", "path to a migration YAML manifest")
	weaverDiffPath := flags.String("weaver-diff", "", "path to a Weaver registry diff JSON document")
	weaverMappingPath := flags.String("weaver-mapping", "", "path to an explicit Weaver backend mapping")
	question := flags.String("question", "", "read-only migration question for the AI provider")
	providerCommand := flags.String("ai-command", "", "local AI provider executable")
	providerTimeout := flags.Duration("ai-timeout", 30*time.Second, "AI provider timeout (maximum 2m)")
	var providerArgs stringListFlag
	flags.Var(&providerArgs, "ai-arg", "argument passed directly to the AI provider executable (repeatable)")
	remoteFlags := addRemoteEvidenceFlags(flags)
	execution := addExecutionFlags(flags)
	if err := flags.Parse(args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 || *configPath == "" || *question == "" || *providerCommand == "" {
		fmt.Fprintf(stderr, "%s requires --config, one change source, --question, and --ai-command and accepts no positional arguments\n", commandName)
		return 1
	}
	if *providerTimeout <= 0 || *providerTimeout > 2*time.Minute {
		fmt.Fprintln(stderr, "--ai-timeout must be positive and no greater than 2m")
		return 1
	}
	if *migrationPath != "" && (*weaverDiffPath != "" || *weaverMappingPath != "") {
		fmt.Fprintf(stderr, "%s and --weaver-diff/--weaver-mapping are mutually exclusive\n", migrationFlag)
		return 1
	}
	if *migrationPath == "" && (*weaverDiffPath == "" || *weaverMappingPath == "") {
		fmt.Fprintf(stderr, "%s requires %s or both --weaver-diff and --weaver-mapping\n", commandName, migrationFlag)
		return 1
	}
	analysisContext, cancel, err := execution.prepare(ctx, configPath, migrationPath, weaverDiffPath, weaverMappingPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	defer cancel()
	ctx = analysisContext

	configuration, err := config.LoadConfig(ctx, *configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if err := remoteFlags.apply(&configuration); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	execution.apply(&configuration)
	migration, err := loadSelectedMigration(ctx, *migrationPath, *weaverDiffPath, *weaverMappingPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		if isWeaverIncomplete(err) {
			return 3
		}
		return 1
	}
	result, target, _, err := analysis.Run(ctx, configuration, migration)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	request, err := explanation.BuildRequest(*question, result, target)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	response, err := (explanation.CommandClient{
		Path:    *providerCommand,
		Args:    providerArgs,
		Timeout: *providerTimeout,
	}).Explain(ctx, request)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if err := explanation.Render(stdout, request, response); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return ReadinessExitCode(result.Summary.Status)
}

func runRemediate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runMigrationRemediate(ctx, "remediate", "--migration", args, stdout, stderr)
}

func runMigrationRemediate(
	ctx context.Context,
	commandName string,
	migrationFlag string,
	args []string,
	stdout, stderr io.Writer,
) int {
	flags := flag.NewFlagSet(commandName, flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "path to a tmr YAML configuration")
	migrationPath := flags.String(strings.TrimPrefix(migrationFlag, "--"), "", "path to a migration YAML manifest")
	weaverDiffPath := flags.String("weaver-diff", "", "path to a Weaver registry diff JSON document")
	weaverMappingPath := flags.String("weaver-mapping", "", "path to an explicit Weaver backend mapping")
	providerCommand := flags.String("ai-command", "", "local AI provider executable")
	providerTimeout := flags.Duration("ai-timeout", 30*time.Second, "AI provider timeout (maximum 2m)")
	var providerArgs stringListFlag
	flags.Var(&providerArgs, "ai-arg", "argument passed directly to the AI provider executable (repeatable)")
	remoteFlags := addRemoteEvidenceFlags(flags)
	execution := addExecutionFlags(flags)
	if err := flags.Parse(args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 || *configPath == "" || *providerCommand == "" {
		fmt.Fprintf(stderr, "%s requires --config, one change source, and --ai-command and accepts no positional arguments\n", commandName)
		return 1
	}
	if *providerTimeout <= 0 || *providerTimeout > 2*time.Minute {
		fmt.Fprintln(stderr, "--ai-timeout must be positive and no greater than 2m")
		return 1
	}
	if *migrationPath != "" && (*weaverDiffPath != "" || *weaverMappingPath != "") {
		fmt.Fprintf(stderr, "%s and --weaver-diff/--weaver-mapping are mutually exclusive\n", migrationFlag)
		return 1
	}
	if *migrationPath == "" && (*weaverDiffPath == "" || *weaverMappingPath == "") {
		fmt.Fprintf(stderr, "%s requires %s or both --weaver-diff and --weaver-mapping\n", commandName, migrationFlag)
		return 1
	}
	analysisContext, cancel, err := execution.prepare(ctx, configPath, migrationPath, weaverDiffPath, weaverMappingPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	defer cancel()
	ctx = analysisContext

	configuration, err := config.LoadConfig(ctx, *configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if err := remoteFlags.apply(&configuration); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	execution.apply(&configuration)
	migration, err := loadSelectedMigration(ctx, *migrationPath, *weaverDiffPath, *weaverMappingPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		if isWeaverIncomplete(err) {
			return 3
		}
		return 1
	}
	result, _, discovery, err := analysis.Run(ctx, configuration, migration)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	request, err := remediation.BuildRequest(result)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if len(request.Targets) == 0 {
		fmt.Fprintln(stdout, "No confirmed direct local Prometheus-rule or Grafana expression is patchable for this migration.")
		fmt.Fprintf(stdout, "Current authoritative status remains: %s\n", result.Summary.Status)
		return ReadinessExitCode(result.Summary.Status)
	}
	response, err := (remediation.CommandClient{
		Path:    *providerCommand,
		Args:    providerArgs,
		Timeout: *providerTimeout,
	}).Propose(ctx, request)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	candidates, err := remediation.Validate(
		ctx,
		request,
		response,
		migration,
		discovery,
		analysis.ReadinessPolicy(configuration),
	)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if err := remediation.Render(stdout, request, candidates); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return ReadinessExitCode(result.Summary.Status)
}

func loadSelectedMigration(ctx context.Context, migrationPath, weaverDiffPath, weaverMappingPath string) (domain.Migration, error) {
	if migrationPath != "" {
		return config.LoadMigration(ctx, migrationPath)
	}
	return loadWeaverMigration(ctx, weaverDiffPath, weaverMappingPath)
}

func loadWeaverMigration(ctx context.Context, diffPath, mappingPath string) (domain.Migration, error) {
	migration, _, err := weaveradapter.LoadMigration(ctx, diffPath, mappingPath)
	return migration, err
}

func isWeaverIncomplete(err error) bool {
	var target *weaveradapter.MappingRequiredError
	if errors.As(err, &target) {
		return true
	}
	var unsupported *weaveradapter.UnsupportedChangeError
	return errors.As(err, &unsupported)
}

func runGraph(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runGraphCommand(ctx, false, args, stdout, stderr)
}

func runGraphCommand(ctx context.Context, requireComplete bool, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("graph", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "path to a tmr YAML configuration")
	format := flags.String("format", "json", "graph format (json)")
	output := flags.String("output", "", "optional graph output path")
	remoteFlags := addRemoteEvidenceFlags(flags)
	execution := addExecutionFlags(flags)
	if err := flags.Parse(args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 || *configPath == "" {
		fmt.Fprintln(stderr, "graph requires --config and accepts no positional arguments")
		return 1
	}
	if *format != "json" {
		fmt.Fprintln(stderr, "graph --format currently supports only json")
		return 1
	}
	analysisContext, cancel, err := execution.prepare(ctx, configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	defer cancel()
	ctx = analysisContext
	configuration, err := config.LoadConfig(ctx, *configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if err := remoteFlags.apply(&configuration); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	execution.apply(&configuration)
	discovery, target, err := analysis.Discover(ctx, configuration)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	contents, err := report.GraphJSON(target)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if err := writeOutput(*output, contents, stdout); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if requireComplete && writeDiscoveryDiagnostics(stderr, discovery.Diagnostics) {
		return 3
	}
	return 0
}

func runExplain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runSymbolImpact(ctx, "explain", false, args, stdout, stderr)
}

func runSymbolImpact(
	ctx context.Context,
	commandName string,
	includeMetricFamily bool,
	args []string,
	stdout, stderr io.Writer,
) int {
	flags := flag.NewFlagSet(commandName, flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "path to a tmr YAML configuration")
	symbolName := flags.String("symbol", "", "Prometheus metric name")
	remoteFlags := addRemoteEvidenceFlags(flags)
	execution := addExecutionFlags(flags)
	if err := flags.Parse(args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 || *configPath == "" || *symbolName == "" {
		fmt.Fprintf(stderr, "%s requires --config and --symbol and accepts no positional arguments\n", commandName)
		return 1
	}
	analysisContext, cancel, err := execution.prepare(ctx, configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	defer cancel()
	ctx = analysisContext
	configuration, err := config.LoadConfig(ctx, *configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if err := remoteFlags.apply(&configuration); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	execution.apply(&configuration)
	discovery, target, err := analysis.Discover(ctx, configuration)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	symbol := domain.Symbol{
		Domain: domain.DomainPrometheus,
		Kind:   domain.SymbolKindMetric,
		Name:   *symbolName,
	}
	fmt.Fprintln(stdout, *symbolName)
	if includeMetricFamily {
		if err := renderMetricFamilyImpact(stdout, target, symbol); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		if writeDiscoveryDiagnostics(stderr, discovery.Diagnostics) {
			return 3
		}
		return 0
	}
	paths := target.ImpactPaths(graph.SymbolNodeID(symbol))
	if len(paths) == 0 {
		fmt.Fprintln(stdout, "\nNo confirmed dependents found.")
		return 0
	}
	fmt.Fprintln(stdout, "\nDependency paths:")
	for _, path := range paths {
		end, exists := target.Node(path.Nodes[len(path.Nodes)-1])
		if !exists || end.Consumer == nil {
			continue
		}
		fmt.Fprintf(stdout, "  %s\n", readablePath(target, path))
	}
	return 0
}

func renderMetricFamilyImpact(stdout io.Writer, target *graph.Graph, symbol domain.Symbol) error {
	byConsumer := impact.ImpactedConsumers(target, symbol, true)
	if len(byConsumer) == 0 {
		fmt.Fprintln(stdout, "\nNo confirmed dependents found.")
		return nil
	}

	consumerIDs := make([]string, 0, len(byConsumer))
	for consumerID := range byConsumer {
		consumerIDs = append(consumerIDs, consumerID)
	}
	sort.Strings(consumerIDs)
	direct, transitive := 0, 0
	var paths []string
	for _, consumerID := range consumerIDs {
		consumerDirect := false
		for _, path := range byConsumer[consumerID] {
			consumerDirect = consumerDirect || len(path.Edges) == 1
			paths = append(paths, readablePath(target, path))
		}
		if consumerDirect {
			direct++
		} else {
			transitive++
		}
	}
	sort.Strings(paths)
	fmt.Fprintf(stdout, "\nDirect consumers:     %d\n", direct)
	fmt.Fprintf(stdout, "Transitive consumers: %d\n", transitive)
	fmt.Fprintln(stdout, "\nAffected consumers:")
	for _, consumerID := range consumerIDs {
		node, exists := target.Node(graph.ConsumerNodeID(consumerID))
		if !exists || node.Consumer == nil {
			return fmt.Errorf("impacted consumer %q is missing from the dependency graph", consumerID)
		}
		impactType, err := impact.TypeForConsumer(node.Consumer.Kind)
		if err != nil {
			return err
		}
		fmt.Fprintf(
			stdout,
			"  [%s] %s — %s (%s)\n",
			strings.ToUpper(string(node.Consumer.Criticality)),
			impactType,
			node.Consumer.Name,
			node.Consumer.Kind,
		)
	}
	fmt.Fprintln(stdout, "\nDependency paths:")
	for _, path := range paths {
		fmt.Fprintf(stdout, "  %s\n", path)
	}
	return nil
}

func writeDiscoveryDiagnostics(stderr io.Writer, diagnostics []domain.Diagnostic) bool {
	required := false
	for _, diagnostic := range diagnostics {
		requirement := "optional"
		if diagnostic.Required {
			requirement = "required"
			required = true
		}
		location := diagnostic.Source.File
		if location == "" {
			location = diagnostic.Source.URL
		}
		if location == "" {
			location = "unknown source"
		}
		fmt.Fprintf(stderr, "Diagnostic [%s/%s] %s: %s\n", diagnostic.Adapter, requirement, location, diagnostic.Message)
	}
	return required
}

func renderResult(format string, result readiness.Result) ([]byte, error) {
	if format == "json" {
		return report.JSON(result)
	}
	var output bytes.Buffer
	switch format {
	case "console":
		if err := report.Console(&output, result); err != nil {
			return nil, err
		}
	case "markdown":
		if err := report.Markdown(&output, result); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported report format %q", format)
	}
	return output.Bytes(), nil
}

func writeOutput(path string, contents []byte, stdout io.Writer) error {
	if path == "" {
		_, err := stdout.Write(contents)
		return err
	}
	if err := writeFileAtomic(path, contents); err != nil {
		return fmt.Errorf("write output %q: %w", path, err)
	}
	return nil
}

func writeFileAtomic(path string, contents []byte) error {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return err
	}
	if info, statErr := os.Lstat(absolute); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to replace symbolic link")
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to replace non-regular file")
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	directory := filepath.Dir(absolute)
	temporary, err := os.CreateTemp(directory, ".tcg-output-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, absolute); err != nil {
		return err
	}
	return nil
}

func validateOutputPaths(outputPath, jsonOutputPath, statusOutputPath string) error {
	paths := []struct {
		flag string
		path string
	}{
		{flag: "--output", path: outputPath},
		{flag: "--json-output", path: jsonOutputPath},
		{flag: "--status-output", path: statusOutputPath},
	}
	resolved := make(map[string]string, len(paths))
	for _, candidate := range paths {
		if candidate.path == "" {
			continue
		}
		absolute, err := secureOutputIdentity(candidate.path)
		if err != nil {
			return fmt.Errorf("resolve %s path %q: %w", candidate.flag, candidate.path, err)
		}
		if previous, exists := resolved[absolute]; exists {
			return fmt.Errorf("%s and %s must identify different files", previous, candidate.flag)
		}
		resolved[absolute] = candidate.flag
	}
	return nil
}

func secureOutputIdentity(path string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", fmt.Errorf("resolve parent directory: %w", err)
	}
	if info, statErr := os.Lstat(absolute); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("output path must not be a symbolic link")
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("output path must be a regular file")
		}
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}
	return filepath.Join(parent, filepath.Base(absolute)), nil
}

func readablePath(target *graph.Graph, path graph.Path) string {
	names := make([]string, 0, len(path.Nodes))
	for _, nodeID := range path.Nodes {
		node, exists := target.Node(nodeID)
		if !exists {
			names = append(names, nodeID)
			continue
		}
		names = append(names, node.Name)
	}
	return strings.Join(names, " -> ")
}

func flagExitCode(err error) int {
	if err == flag.ErrHelp {
		return 0
	}
	return 1
}

// ReadinessExitCode preserves the permanent tmr process contract.
func ReadinessExitCode(status readiness.Status) int {
	switch status {
	case readiness.StatusReady:
		return 0
	case readiness.StatusBlocked:
		return 2
	case readiness.StatusIncomplete:
		return 3
	default:
		return 1
	}
}
