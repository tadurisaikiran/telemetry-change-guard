package impact

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
)

func TestAnalyzeProducesStableDirectTransitiveAndUncertainFindings(t *testing.T) {
	t.Parallel()

	changeSet := metricChangeSet()
	discovery := domain.Discovery{
		Consumers: []domain.Consumer{
			{ID: "unresolved", Kind: domain.ConsumerKindDashboard, Name: "Dynamic dashboard", Criticality: domain.CriticalityCritical, Unresolved: true},
			{ID: "migrated", Kind: domain.ConsumerKindDashboardPanel, Name: "Migrated panel", Criticality: domain.CriticalityHigh},
			{ID: "alert", Kind: domain.ConsumerKindAlertRule, Name: "TrafficStopped", Criticality: domain.CriticalityCritical},
			{ID: "recording", Kind: domain.ConsumerKindRecordingRule, Name: "requests:rate", Criticality: domain.CriticalityHigh},
		},
		References: []domain.Reference{
			{ConsumerID: "recording", Symbol: findingMetric("old_metric")},
			{ConsumerID: "alert", Symbol: findingMetric("requests_rate")},
			{ConsumerID: "migrated", Symbol: findingMetric("new_metric")},
		},
		Productions: []domain.Production{{ConsumerID: "recording", Symbol: findingMetric("requests_rate")}},
	}
	target, err := BuildGraph(discovery)
	if err != nil {
		t.Fatal(err)
	}

	findings, err := Analyze(changeSet, discovery, target, true)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(findings), 3; got != want {
		t.Fatalf("findings = %d, want %d: %#v", got, want, findings)
	}
	if findings[0].Consumer.ID != "alert" || findings[0].Impact != TypeAlertingRisk || len(findings[0].Paths) != 1 {
		t.Fatalf("alert finding = %#v", findings[0])
	}
	if got := len(findings[0].Paths[0].Edges); got != 3 {
		t.Fatalf("alert path edges = %d, want 3", got)
	}
	if findings[1].Consumer.ID != "recording" || findings[1].Impact != TypeSemanticRisk {
		t.Fatalf("recording finding = %#v", findings[1])
	}
	if findings[2].Consumer.ID != "unresolved" || !findings[2].Uncertain || findings[2].Impact != TypeVisibilityLoss {
		t.Fatalf("unresolved finding = %#v", findings[2])
	}
	for _, finding := range findings {
		if finding.Consumer.ID == "migrated" {
			t.Fatal("destination-only consumer produced a finding")
		}
	}

	direct, err := Analyze(changeSet, discovery, target, false)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(direct), 2; got != want {
		t.Fatalf("direct findings = %d, want %d", got, want)
	}
	if direct[0].Consumer.ID != "recording" || direct[1].Consumer.ID != "unresolved" {
		t.Fatalf("direct finding order = %#v", direct)
	}
}

func TestAnalyzeNeverMatchesSameNameAcrossDomains(t *testing.T) {
	t.Parallel()

	destination := domain.Symbol{
		Domain: domain.DomainOpenTelemetry, Kind: domain.SymbolKindSpanAttribute, Name: "http.request.method",
	}
	changeSet := domain.ChangeSet{
		APIVersion: domain.ChangeSetAPIVersion,
		Kind:       domain.ChangeSetKind,
		Metadata:   domain.ChangeSetMetadata{Name: "trace-attribute"},
		Changes: []domain.Change{{
			ID: "trace-change", Kind: domain.ChangeKindSpanAttributeRename, Domain: domain.DomainOpenTelemetry,
			From: domain.Symbol{Domain: domain.DomainOpenTelemetry, Kind: domain.SymbolKindSpanAttribute, Name: "http.method"},
			To:   &destination,
		}},
	}
	discovery := domain.Discovery{
		Consumers: []domain.Consumer{{
			ID: "trace-query", Kind: domain.ConsumerKindQuery, Name: "Trace query", Criticality: domain.CriticalityHigh,
		}},
		References: []domain.Reference{{
			ConsumerID: "trace-query",
			Symbol: domain.Symbol{
				Domain: domain.DomainTempo, Kind: domain.SymbolKindSpanAttribute, Name: "http.method",
			},
		}},
	}
	target, err := BuildGraph(discovery)
	if err != nil {
		t.Fatal(err)
	}
	findings, err := Analyze(changeSet, discovery, target, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("cross-domain findings = %#v, want none", findings)
	}
}

func TestAnalyzeMatchesPrometheusMetricFamiliesOnly(t *testing.T) {
	t.Parallel()

	discovery := domain.Discovery{
		Consumers: []domain.Consumer{{
			ID: "histogram", Kind: domain.ConsumerKindDashboardPanel, Name: "Histogram", Criticality: domain.CriticalityHigh,
		}},
		References: []domain.Reference{{ConsumerID: "histogram", Symbol: findingMetric("old_metric_count")}},
	}
	target, err := BuildGraph(discovery)
	if err != nil {
		t.Fatal(err)
	}
	findings, err := Analyze(metricChangeSet(), discovery, target, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Consumer.ID != "histogram" {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestTypeForConsumerCoversEverySupportedOperationalClass(t *testing.T) {
	t.Parallel()

	tests := map[domain.ConsumerKind]Type{
		domain.ConsumerKindDashboard:      TypeVisibilityLoss,
		domain.ConsumerKindDashboardPanel: TypeVisibilityLoss,
		domain.ConsumerKindQuery:          TypeVisibilityLoss,
		domain.ConsumerKindRunbook:        TypeVisibilityLoss,
		domain.ConsumerKindAlertRule:      TypeAlertingRisk,
		domain.ConsumerKindSLO:            TypeSLORisk,
		domain.ConsumerKindAutoscaler:     TypeScalingRisk,
		domain.ConsumerKindDeploymentGate: TypeDeploymentGateRisk,
		domain.ConsumerKindAutomation:     TypeAutomationRisk,
		domain.ConsumerKindRecordingRule:  TypeSemanticRisk,
		domain.ConsumerKindCollector:      TypeSemanticRisk,
		domain.ConsumerKindSourceCode:     TypeSemanticRisk,
	}
	for kind, expected := range tests {
		kind, expected := kind, expected
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			got, err := TypeForConsumer(kind)
			if err != nil || got != expected {
				t.Fatalf("TypeForConsumer(%q) = %q, %v; want %q", kind, got, err, expected)
			}
		})
	}
	if _, err := TypeForConsumer(domain.ConsumerKind("unknown")); err == nil {
		t.Fatal("unknown consumer kind did not fail")
	}
}

func TestAnalyzeIsDeterministicAndDoesNotMutateInputs(t *testing.T) {
	t.Parallel()

	changeSet := metricChangeSet()
	discovery := domain.Discovery{
		Consumers: []domain.Consumer{
			{ID: "z", Kind: domain.ConsumerKindAlertRule, Name: "Z", Criticality: domain.CriticalityCritical},
			{ID: "a", Kind: domain.ConsumerKindSLO, Name: "A", Criticality: domain.CriticalityHigh},
		},
		References: []domain.Reference{
			{ConsumerID: "z", Symbol: findingMetric("old_metric")},
			{ConsumerID: "a", Symbol: findingMetric("old_metric")},
		},
	}
	target, err := BuildGraph(discovery)
	if err != nil {
		t.Fatal(err)
	}
	beforeChangeSet, _ := json.Marshal(changeSet)
	beforeDiscovery, _ := json.Marshal(discovery)
	first, err := Analyze(changeSet, discovery, target, true)
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		next, err := Analyze(changeSet, discovery, target, true)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(next, first) {
			t.Fatalf("findings are nondeterministic\nfirst: %#v\nnext: %#v", first, next)
		}
	}
	afterChangeSet, _ := json.Marshal(changeSet)
	afterDiscovery, _ := json.Marshal(discovery)
	if string(beforeChangeSet) != string(afterChangeSet) || string(beforeDiscovery) != string(afterDiscovery) {
		t.Fatal("Analyze mutated its inputs")
	}

	reordered := discovery
	reordered.Consumers = []domain.Consumer{discovery.Consumers[1], discovery.Consumers[0]}
	reordered.References = []domain.Reference{discovery.References[1], discovery.References[0]}
	reorderedGraph, err := BuildGraph(reordered)
	if err != nil {
		t.Fatal(err)
	}
	reorderedFindings, err := Analyze(changeSet, reordered, reorderedGraph, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reorderedFindings, first) {
		t.Fatalf("equivalent reordered evidence changed findings\nfirst: %#v\nreordered: %#v", first, reorderedFindings)
	}
}

func TestAnalyzeRejectsInvalidGraphOrConsumerIdentity(t *testing.T) {
	t.Parallel()

	if _, err := Analyze(metricChangeSet(), domain.Discovery{}, nil, true); err == nil {
		t.Fatal("nil graph did not fail")
	}
	discovery := domain.Discovery{Consumers: []domain.Consumer{{Kind: domain.ConsumerKindDashboard}}}
	target, err := BuildGraph(discovery)
	if err == nil {
		_, err = Analyze(metricChangeSet(), discovery, target, true)
	}
	if err == nil {
		t.Fatal("empty consumer ID did not fail")
	}
}

func TestAnalyzeReturnsProvenFindingsBeforeRelevantConsumerError(t *testing.T) {
	t.Parallel()

	discovery := domain.Discovery{
		Consumers: []domain.Consumer{
			{ID: "a-known", Kind: domain.ConsumerKindAlertRule, Criticality: domain.CriticalityCritical},
			{ID: "z-unsupported", Kind: domain.ConsumerKind("unknown"), Criticality: domain.CriticalityCritical},
		},
		References: []domain.Reference{
			{ConsumerID: "a-known", Symbol: findingMetric("old_metric")},
			{ConsumerID: "z-unsupported", Symbol: findingMetric("old_metric")},
		},
	}
	target, err := BuildGraph(discovery)
	if err != nil {
		t.Fatal(err)
	}
	findings, err := Analyze(metricChangeSet(), discovery, target, true)
	if err == nil || len(findings) != 1 || findings[0].Consumer.ID != "a-known" {
		t.Fatalf("findings = %#v, error = %v; want one preserved finding and an error", findings, err)
	}
}

func metricChangeSet() domain.ChangeSet {
	destination := findingMetric("new_metric")
	return domain.ChangeSet{
		APIVersion: domain.ChangeSetAPIVersion,
		Kind:       domain.ChangeSetKind,
		Metadata:   domain.ChangeSetMetadata{Name: "metrics"},
		Changes: []domain.Change{{
			ID: "metric-change", Kind: domain.ChangeKindMetricRename, Domain: domain.DomainPrometheus,
			From: findingMetric("old_metric"), To: &destination,
		}},
	}
}

func findingMetric(name string) domain.Symbol {
	return domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: name}
}
