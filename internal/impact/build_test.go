package impact

import (
	"testing"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/graph"
)

func TestBuildGraphPropagatesRecordingRuleImpact(t *testing.T) {
	t.Parallel()

	raw := metric("checkout_request_duration_seconds")
	derived := metric("checkout:p95_latency")
	discovery := domain.Discovery{
		Consumers: []domain.Consumer{
			{ID: "recording", Kind: domain.ConsumerKindRecordingRule, Name: "checkout:p95_latency"},
			{ID: "alert", Kind: domain.ConsumerKindAlertRule, Name: "CheckoutLatencyHigh"},
		},
		References: []domain.Reference{
			{ConsumerID: "recording", Symbol: raw},
			{ConsumerID: "alert", Symbol: derived},
		},
		Productions: []domain.Production{
			{ConsumerID: "recording", Symbol: derived},
		},
	}

	target, err := BuildGraph(discovery)
	if err != nil {
		t.Fatalf("BuildGraph() error = %v", err)
	}
	paths := target.ImpactPaths(graph.SymbolNodeID(raw))
	alertID := graph.ConsumerNodeID("alert")
	var alertPath graph.Path
	for _, path := range paths {
		if path.Nodes[len(path.Nodes)-1] == alertID {
			alertPath = path
			break
		}
	}
	if len(alertPath.Nodes) == 0 {
		t.Fatalf("no transitive alert path in %+v", paths)
	}
	want := []string{
		graph.SymbolNodeID(raw),
		graph.ConsumerNodeID("recording"),
		graph.SymbolNodeID(derived),
		graph.ConsumerNodeID("alert"),
	}
	if !equalPath(alertPath.Nodes, want) {
		t.Errorf("alert path = %v, want %v", alertPath.Nodes, want)
	}
}

func TestBuildGraphRejectsReferenceWithoutConsumer(t *testing.T) {
	t.Parallel()

	_, err := BuildGraph(domain.Discovery{
		References: []domain.Reference{{ConsumerID: "missing", Symbol: metric("raw")}},
	})
	if err == nil {
		t.Fatal("BuildGraph() error = nil, want error")
	}
}

func metric(name string) domain.Symbol {
	return domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: name}
}

func equalPath(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
