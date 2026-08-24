package analysis

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/config"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/readiness"
)

func TestCheckoutFixtureIsBlockedAndTransitive(t *testing.T) {
	t.Parallel()

	repositoryRoot := filepath.Clean(filepath.Join("..", ".."))
	configuration, err := config.LoadConfig(context.Background(), filepath.Join(repositoryRoot, "examples", "checkout-migration", "tmr.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	absolutizePatterns(&configuration, repositoryRoot)
	migration, err := config.LoadMigration(context.Background(), filepath.Join(repositoryRoot, "examples", "checkout-migration", "migration.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	result, _, _, err := Run(context.Background(), configuration, migration)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Status != readiness.StatusBlocked {
		t.Fatalf("status = %s, want BLOCKED", result.Summary.Status)
	}
	if result.Summary.LegacyOnly == 0 || result.Summary.Uncertain == 0 {
		t.Fatalf("summary = %+v, want legacy and uncertain consumers", result.Summary)
	}

	var transitivePath bool
	for _, change := range result.Changes {
		for _, consumer := range change.Consumers {
			if consumer.Consumer.Name != "CheckoutLatencyHigh" {
				continue
			}
			for _, path := range consumer.Paths {
				joined := strings.Join(path.Nodes, " ")
				if strings.Contains(joined, "checkout:p95_latency") && len(path.Edges) > 1 {
					transitivePath = true
				}
			}
		}
	}
	if !transitivePath {
		t.Fatal("missing raw metric -> recording rule -> alert transitive path")
	}
}

func TestRequiredMissingSourceIsIncomplete(t *testing.T) {
	t.Parallel()

	configuration := config.Config{
		APIVersion: config.ConfigAPIVersion,
		Sources: config.Sources{PrometheusRules: []config.SourcePattern{{
			Pattern:  filepath.Join(t.TempDir(), "missing", "*.yaml"),
			Required: true,
		}}},
		Analysis: config.AnalysisConfig{IncludeTransitiveDependencies: true, UnresolvedReferencePolicy: "error"},
		Policy:   config.PolicyConfig{FailOnCriticalLegacyConsumer: true, FailOnCriticalUnknown: true, MinimumBlockingCriticality: "high"},
		Output:   config.OutputConfig{Formats: []string{"json"}},
	}
	migration, err := config.ParseMigration(strings.NewReader(`
apiVersion: telemetry-migration/v1alpha1
kind: Migration
metadata: {name: missing-source}
spec:
  changes:
    - id: remove
      kind: metric_remove
      domain: prometheus
      from: {metric: old_metric}
`))
	if err != nil {
		t.Fatal(err)
	}
	result, _, _, err := Run(context.Background(), configuration, migration)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Status != readiness.StatusIncomplete {
		t.Fatalf("status = %s, want INCOMPLETE", result.Summary.Status)
	}
}

func absolutizePatterns(configuration *config.Config, root string) {
	groups := [][]config.SourcePattern{
		configuration.Sources.PrometheusRules,
		configuration.Sources.Grafana,
		configuration.Sources.Sloth,
		configuration.Sources.Pyrra,
	}
	for _, group := range groups {
		for index := range group {
			group[index].Pattern = filepath.Join(root, strings.TrimPrefix(group[index].Pattern, "./"))
		}
	}
}
