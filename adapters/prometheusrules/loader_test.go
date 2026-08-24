package prometheusrules

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/graph"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/impact"
)

func TestLoadStandardRulesAndTransitiveImpact(t *testing.T) {
	t.Parallel()

	discovery, err := (Loader{Required: true}).LoadFile(context.Background(), "testdata/standard.yaml")
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if got, want := len(discovery.Consumers), 2; got != want {
		t.Fatalf("len(Consumers) = %d, want %d", got, want)
	}
	if got, want := len(discovery.Productions), 1; got != want {
		t.Fatalf("len(Productions) = %d, want %d", got, want)
	}
	if discovery.Consumers[1].Criticality != domain.CriticalityCritical {
		t.Errorf("alert criticality = %q, want critical", discovery.Consumers[1].Criticality)
	}
	if discovery.Consumers[0].Source.Line == 0 {
		t.Error("recording rule source line was not preserved")
	}

	target, err := impact.BuildGraph(discovery)
	if err != nil {
		t.Fatalf("BuildGraph() error = %v", err)
	}
	start := graph.SymbolNodeID(domain.Symbol{
		Domain: domain.DomainPrometheus,
		Kind:   domain.SymbolKindMetric,
		Name:   "checkout_request_duration_seconds_bucket",
	})
	alertID := graph.ConsumerNodeID(discovery.Consumers[1].ID)
	if !hasPathTo(target.ImpactPaths(start), alertID) {
		t.Fatalf("no transitive path from raw metric to alert; paths = %+v", target.ImpactPaths(start))
	}
}

func TestLoadPrometheusRuleCRD(t *testing.T) {
	t.Parallel()

	discovery, err := (Loader{Required: true}).LoadFile(context.Background(), "testdata/prometheusrule.yaml")
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if got, want := len(discovery.Consumers), 2; got != want {
		t.Fatalf("len(Consumers) = %d, want %d", got, want)
	}
	if got, want := discovery.Productions[0].Symbol.Name, "payment:error_rate"; got != want {
		t.Errorf("production symbol = %q, want %q", got, want)
	}
	if discovery.Consumers[1].Criticality != domain.CriticalityHigh {
		t.Errorf("warning alert criticality = %q, want high", discovery.Consumers[1].Criticality)
	}
}

func TestInvalidPromQLBecomesRequiredDiagnostic(t *testing.T) {
	t.Parallel()

	discovery, err := (Loader{Required: true}).LoadFile(context.Background(), "testdata/unresolved.yaml")
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if got, want := len(discovery.Diagnostics), 1; got != want {
		t.Fatalf("len(Diagnostics) = %d, want %d", got, want)
	}
	if !discovery.Diagnostics[0].Required {
		t.Error("diagnostic Required = false, want true")
	}
	if !discovery.Consumers[0].Unresolved {
		t.Error("consumer Unresolved = false, want true")
	}
}

func TestParseRejectsMalformedRule(t *testing.T) {
	t.Parallel()

	_, err := (Loader{Required: true}).Parse(
		context.Background(),
		"malformed.yaml",
		strings.NewReader("groups:\n  - name: checkout\n    rules:\n      - expr: up\n"),
	)
	if err == nil {
		t.Fatal("Parse() error = nil, want malformed rule error")
	}
}

func TestParseSupportsMultipleDocuments(t *testing.T) {
	t.Parallel()

	standard, err := os.ReadFile("testdata/standard.yaml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	crd, err := os.ReadFile("testdata/prometheusrule.yaml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	combined := string(standard) + "\n---\n" + string(crd)
	discovery, err := (Loader{Required: true}).Parse(context.Background(), "combined.yaml", strings.NewReader(combined))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(discovery.Consumers), 4; got != want {
		t.Fatalf("len(Consumers) = %d, want %d", got, want)
	}
}

func hasPathTo(paths []graph.Path, nodeID string) bool {
	for _, path := range paths {
		if path.Nodes[len(path.Nodes)-1] == nodeID {
			return true
		}
	}
	return false
}
