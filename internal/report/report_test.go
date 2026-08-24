package report

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/graph"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/impact"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/readiness"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/safety"
)

func TestRenderersPreserveStatusEvidenceAndPaths(t *testing.T) {
	t.Parallel()

	result := fixtureResult()
	contents, err := JSON(result)
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	var decoded readiness.Result
	if err := json.Unmarshal(contents, &decoded); err != nil {
		t.Fatalf("JSON output is invalid: %v", err)
	}
	if decoded.SchemaVersion != readiness.ResultSchemaVersion {
		t.Errorf("schemaVersion = %q", decoded.SchemaVersion)
	}

	for name, render := range map[string]func(*bytes.Buffer, readiness.Result) error{
		"console":  func(output *bytes.Buffer, result readiness.Result) error { return Console(output, result) },
		"markdown": func(output *bytes.Buffer, result readiness.Result) error { return Markdown(output, result) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			if err := render(&output, result); err != nil {
				t.Fatalf("render error = %v", err)
			}
			for _, expected := range []string{"BLOCKED", "CheckoutLatencyHigh", "Checkout Platform", "Runtime-observed", "12 execution(s)", "checkout_request_duration_seconds", "checkout:p95_latency"} {
				if !strings.Contains(output.String(), expected) {
					t.Errorf("output does not contain %q:\n%s", expected, output.String())
				}
			}
			if !strings.Contains(output.String(), "Telemetry Change Guard") {
				t.Errorf("output does not contain product branding:\n%s", output.String())
			}
		})
	}
}

func TestRuntimeConsumerCountIsUniqueAcrossAllChanges(t *testing.T) {
	t.Parallel()

	runtimeConsumer := domain.Consumer{ID: "runtime", Runtime: &domain.RuntimeEvidence{ExecutionCount: 1}}
	result := readiness.Result{Changes: []readiness.ChangeResult{
		{Consumers: []readiness.ConsumerResult{{Consumer: runtimeConsumer}}},
		{Consumers: []readiness.ConsumerResult{
			{Consumer: runtimeConsumer},
			{Consumer: domain.Consumer{ID: "second-runtime", Runtime: &domain.RuntimeEvidence{ExecutionCount: 1}}},
		}},
	}}
	if got, want := runtimeConsumerCount(result), 2; got != want {
		t.Fatalf("runtimeConsumerCount() = %d, want %d", got, want)
	}
}

func TestGraphJSONIsStable(t *testing.T) {
	t.Parallel()

	target := graph.New()
	symbol := domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: "requests_total"}
	consumer := domain.Consumer{ID: "alert:a", Kind: domain.ConsumerKindAlertRule, Name: "A"}
	if err := target.AddNode(graph.Node{ID: "consumer:alert:a", Kind: graph.NodeKindConsumer, Name: "A", Consumer: &consumer}); err != nil {
		t.Fatal(err)
	}
	if err := target.AddNode(graph.Node{ID: graph.SymbolNodeID(symbol), Kind: graph.NodeKindSymbol, Name: symbol.Name, Symbol: &symbol}); err != nil {
		t.Fatal(err)
	}
	if err := target.AddEdge(graph.Edge{From: graph.SymbolNodeID(symbol), To: "consumer:alert:a", Kind: graph.EdgeReferences}); err != nil {
		t.Fatal(err)
	}

	first, err := GraphJSON(target)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GraphJSON(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("GraphJSON output is not deterministic")
	}
}

func TestSafetyRenderersPreserveStatusImpactAndEvidence(t *testing.T) {
	t.Parallel()

	change := domain.Change{
		ID: "remove-requests", Kind: domain.ChangeKindMetricRemove, Domain: domain.DomainPrometheus,
		From: domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: "requests_total"},
	}
	changeSet := domain.ChangeSet{
		APIVersion: domain.ChangeSetAPIVersion,
		Kind:       domain.ChangeSetKind,
		Metadata:   domain.ChangeSetMetadata{Name: "requests-contract"},
		Changes:    []domain.Change{change},
	}
	finding := impact.Finding{
		Change: change,
		Consumer: impact.Consumer{
			ID: "alert:traffic", Kind: domain.ConsumerKindAlertRule, Name: "TrafficStopped",
			Criticality: domain.CriticalityCritical,
			Source:      domain.SourceLocation{File: "monitoring/alerts.yaml", Line: 14},
		},
		Impact: impact.TypeAlertingRisk, Criticality: domain.CriticalityCritical,
		Paths: []impact.Path{{
			Nodes: []string{"symbol:prometheus:metric::requests_total", "consumer:alert:traffic"},
			Edges: []graph.EdgeKind{graph.EdgeReferences},
		}},
	}
	result, err := safety.Evaluate(changeSet, []impact.Finding{finding}, nil, safety.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}

	jsonContents, err := SafetyJSON(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded safety.Result
	if err := json.Unmarshal(jsonContents, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != safety.ResultSchemaVersion || decoded.Status != safety.StatusBlock {
		t.Fatalf("decoded safety result = %#v", decoded)
	}

	for name, render := range map[string]func(io.Writer, safety.Result) error{
		"console":  SafetyConsole,
		"markdown": SafetyMarkdown,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			if err := render(&output, result); err != nil {
				t.Fatal(err)
			}
			for _, expected := range []string{
				"Telemetry Change Guard", "BLOCK", "ALERTING_RISK", "TrafficStopped",
				"monitoring/alerts.yaml:14", "requests_total", "alert:traffic",
			} {
				if !strings.Contains(output.String(), expected) {
					t.Errorf("output missing %q:\n%s", expected, output.String())
				}
			}
		})
	}
}

func TestSafetyErrorReportMarksPreservedFindingsUndecided(t *testing.T) {
	t.Parallel()

	result := safety.Result{
		SchemaVersion: safety.ResultSchemaVersion,
		Status:        safety.StatusError,
		Findings: []impact.Finding{{
			Change: domain.Change{ID: "known"},
			Consumer: impact.Consumer{
				ID: "consumer", Kind: domain.ConsumerKindDashboard, Name: "Known dashboard",
			},
			Impact: impact.TypeVisibilityLoss,
		}},
		Errors: []string{"analysis failed"},
	}
	var output bytes.Buffer
	if err := SafetyConsole(&output, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "[UNDECIDED]") || !strings.Contains(output.String(), "analysis failed") {
		t.Fatalf("error report = %s", output.String())
	}
}

func fixtureResult() readiness.Result {
	consumer := domain.Consumer{
		ID:          "alert:checkout",
		Kind:        domain.ConsumerKindAlertRule,
		Name:        "CheckoutLatencyHigh",
		Source:      domain.SourceLocation{File: "rules/checkout.yaml", Line: 12},
		Criticality: domain.CriticalityCritical,
		Owner:       &domain.Owner{Name: "Checkout Platform", Email: "checkout@example.com"},
		Runtime: &domain.RuntimeEvidence{
			Format: "prometheus_query_log", ExecutionCount: 12,
			FirstSeen: "2026-08-24T10:00:00Z", LastSeen: "2026-08-24T12:00:00Z",
			Window: "24h0m0s", WindowStart: "2026-08-23T12:00:00Z", WindowAnchor: "2026-08-24T12:00:00Z",
			Origins: []string{"prometheus_api"},
		},
	}
	oldSymbol := domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: "checkout_request_duration_seconds"}
	newSymbol := domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: "checkout_server_request_duration_seconds"}
	change := domain.Change{ID: "duration", Kind: domain.ChangeKindMetricRename, Domain: domain.DomainPrometheus, From: oldSymbol, To: &newSymbol}
	return readiness.Result{
		SchemaVersion: readiness.ResultSchemaVersion,
		Migration: domain.Migration{
			APIVersion: domain.MigrationAPIVersion,
			Kind:       domain.MigrationKind,
			Metadata:   domain.MigrationMetadata{Name: "checkout"},
			Changes:    []domain.Change{change},
		},
		Summary: readiness.Summary{Status: readiness.StatusBlocked, TotalConsumers: 1, LegacyOnly: 1},
		Changes: []readiness.ChangeResult{{
			Change: change,
			Status: readiness.StatusBlocked,
			Consumers: []readiness.ConsumerResult{{
				Consumer:       consumer,
				Classification: readiness.ClassificationLegacyOnly,
				References: []domain.Reference{{
					ConsumerID: consumer.ID,
					Symbol:     oldSymbol,
					Evidence: domain.Evidence{
						Method:     domain.EvidenceMethodPromQLAST,
						Confidence: domain.ConfidenceConfirmed,
						Source:     consumer.Source,
					},
				}},
				Paths: []graph.Path{{Nodes: []string{"checkout_request_duration_seconds", "checkout:p95_latency", "CheckoutLatencyHigh"}}},
			}},
		}},
	}
}
