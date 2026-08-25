package cli

import (
	"context"
	"flag"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/config"
	filesource "github.com/tadurisaikiran/telemetry-change-guard/internal/source"
)

const (
	defaultAnalysisTimeout = 5 * time.Minute
	maxAnalysisTimeout     = 30 * time.Minute
)

type executionFlags struct {
	repositoryRoot *string
	timeout        *time.Duration
	maxSourceFiles *int
	maxSourceBytes *int64
	maxConsumers   *int
	maxReferences  *int
	maxProductions *int
	maxGraphNodes  *int
	maxGraphEdges  *int
	maxFindings    *int
	resolvedRoot   string
}

func addExecutionFlags(flags *flag.FlagSet) *executionFlags {
	values := &executionFlags{}
	values.repositoryRoot = flags.String("repository-root", ".", "trusted root containing every local input and evidence source")
	values.timeout = flags.Duration("analysis-timeout", defaultAnalysisTimeout, "total local analysis timeout (maximum 30m)")
	values.maxSourceFiles = flags.Int("max-source-files", config.DefaultMaxSourceFiles, "maximum unique local evidence files")
	values.maxSourceBytes = flags.Int64("max-source-bytes", config.DefaultMaxSourceBytes, "maximum aggregate local evidence bytes")
	values.maxConsumers = flags.Int("max-consumers", config.DefaultMaxConsumers, "maximum normalized consumers")
	values.maxReferences = flags.Int("max-references", config.DefaultMaxReferences, "maximum normalized references")
	values.maxProductions = flags.Int("max-productions", config.DefaultMaxProductions, "maximum normalized productions")
	values.maxGraphNodes = flags.Int("max-graph-nodes", config.DefaultMaxGraphNodes, "maximum dependency graph nodes")
	values.maxGraphEdges = flags.Int("max-graph-edges", config.DefaultMaxGraphEdges, "maximum dependency graph edges")
	values.maxFindings = flags.Int("max-findings", config.DefaultMaxFindings, "maximum deterministic findings")
	return values
}

func (values *executionFlags) prepare(ctx context.Context, paths ...*string) (context.Context, context.CancelFunc, error) {
	if values == nil {
		return ctx, func() {}, nil
	}
	if *values.timeout <= 0 || *values.timeout > maxAnalysisTimeout {
		return nil, nil, fmt.Errorf("--analysis-timeout must be positive and no greater than 30m")
	}
	for name, value := range map[string]int64{
		"--max-source-files": int64(*values.maxSourceFiles),
		"--max-source-bytes": *values.maxSourceBytes,
		"--max-consumers":    int64(*values.maxConsumers),
		"--max-references":   int64(*values.maxReferences),
		"--max-productions":  int64(*values.maxProductions),
		"--max-graph-nodes":  int64(*values.maxGraphNodes),
		"--max-graph-edges":  int64(*values.maxGraphEdges),
		"--max-findings":     int64(*values.maxFindings),
	} {
		if value <= 0 {
			return nil, nil, fmt.Errorf("%s must be positive", name)
		}
	}
	budget, err := filesource.NewBudget(*values.repositoryRoot, math.MaxInt, math.MaxInt64)
	if err != nil {
		return nil, nil, fmt.Errorf("--repository-root: %w", err)
	}
	defer budget.Close()
	values.resolvedRoot = budget.Root()
	for _, target := range paths {
		if target == nil || strings.TrimSpace(*target) == "" {
			continue
		}
		identity, _, err := budget.Register(*target)
		if err != nil {
			return nil, nil, fmt.Errorf("local input %q: %w", *target, err)
		}
		*target = filepath.Clean(identity)
	}
	analysisContext, cancel := context.WithTimeout(ctx, *values.timeout)
	return analysisContext, cancel, nil
}

func (values *executionFlags) apply(configuration *config.Config) {
	configuration.Execution = config.ExecutionPolicy{
		RepositoryRoot: values.resolvedRoot,
		MaxSourceFiles: *values.maxSourceFiles,
		MaxSourceBytes: *values.maxSourceBytes,
		MaxConsumers:   *values.maxConsumers,
		MaxReferences:  *values.maxReferences,
		MaxProductions: *values.maxProductions,
		MaxGraphNodes:  *values.maxGraphNodes,
		MaxGraphEdges:  *values.maxGraphEdges,
		MaxFindings:    *values.maxFindings,
	}
}
