package safety

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/graph"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/impact"
)

func TestEvaluateStatusPrecedenceTruthTable(t *testing.T) {
	t.Parallel()

	blocking := finding("alert", impact.TypeAlertingRisk, domain.CriticalityCritical, false)
	uncertain := finding("unknown", impact.TypeVisibilityLoss, domain.CriticalityCritical, true)
	required := domain.Diagnostic{Adapter: "fixture", Message: "required evidence failed", Required: true}
	optional := domain.Diagnostic{Adapter: "fixture", Message: "optional evidence failed"}

	tests := []struct {
		name        string
		findings    []impact.Finding
		diagnostics []domain.Diagnostic
		policy      Policy
		want        Status
		wantErr     bool
	}{
		{name: "pass", policy: DefaultPolicy(), want: StatusPass},
		{name: "warn on finding", findings: []impact.Finding{finding("dashboard", impact.TypeVisibilityLoss, domain.CriticalityHigh, false)}, policy: DefaultPolicy(), want: StatusWarn},
		{name: "warn on optional diagnostic", diagnostics: []domain.Diagnostic{optional}, policy: DefaultPolicy(), want: StatusWarn},
		{name: "block", findings: []impact.Finding{blocking}, policy: DefaultPolicy(), want: StatusBlock},
		{name: "incomplete beats block", findings: []impact.Finding{blocking, uncertain}, policy: DefaultPolicy(), want: StatusIncomplete},
		{name: "required diagnostic beats block", findings: []impact.Finding{blocking}, diagnostics: []domain.Diagnostic{required}, policy: DefaultPolicy(), want: StatusIncomplete},
		{name: "error", findings: []impact.Finding{blocking}, policy: Policy{Mode: "unsafe"}, want: StatusError, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, err := Evaluate(safetyChangeSet(), test.findings, test.diagnostics, test.policy)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %t", err, test.wantErr)
			}
			if result.Status != test.want {
				t.Fatalf("status = %s, want %s; result = %#v", result.Status, test.want, result)
			}
			if len(result.Findings) != len(test.findings) {
				t.Fatalf("findings were suppressed: got %d, want %d", len(result.Findings), len(test.findings))
			}
		})
	}
}

func TestRolloutModesNeverEraseFindings(t *testing.T) {
	t.Parallel()

	for _, mode := range []RolloutMode{RolloutAudit, RolloutWarn, RolloutEnforce} {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			policy := DefaultPolicy()
			policy.Mode = mode
			result, err := Evaluate(
				safetyChangeSet(),
				[]impact.Finding{finding("alert", impact.TypeAlertingRisk, domain.CriticalityCritical, false)},
				nil,
				policy,
			)
			if err != nil {
				t.Fatal(err)
			}
			want := StatusWarn
			wantAction := ActionWarn
			if mode == RolloutEnforce {
				want = StatusBlock
				wantAction = ActionBlock
			}
			if result.Status != want || len(result.Findings) != 1 ||
				len(result.Decisions) != 1 || result.Decisions[0].EffectiveAction != wantAction {
				t.Fatalf("result = %#v, want status %s and action %s", result, want, wantAction)
			}
		})
	}
}

func TestCriticalityThresholdCannotTurnFindingIntoPass(t *testing.T) {
	t.Parallel()

	result, err := Evaluate(
		safetyChangeSet(),
		[]impact.Finding{finding("low-alert", impact.TypeAlertingRisk, domain.CriticalityLow, false)},
		nil,
		DefaultPolicy(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusWarn || result.Decisions[0].EffectiveAction != ActionWarn {
		t.Fatalf("result = %#v", result)
	}
}

func TestMissingImpactRuleDefaultsToWarning(t *testing.T) {
	t.Parallel()

	result, err := Evaluate(
		safetyChangeSet(),
		[]impact.Finding{finding("automation", impact.TypeAutomationRisk, domain.CriticalityCritical, false)},
		nil,
		Policy{Mode: RolloutEnforce},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusWarn || result.Decisions[0].EffectiveAction != ActionWarn {
		t.Fatalf("missing rule result = %#v", result)
	}
}

func TestInvalidPolicyAndFindingsFailClosedWithEvidence(t *testing.T) {
	t.Parallel()

	mismatchedImpact := finding("mismatched-impact", impact.TypeVisibilityLoss, domain.CriticalityHigh, false)
	mismatchedImpact.Consumer.Kind = domain.ConsumerKindAlertRule
	mismatchedCriticality := finding("mismatched-criticality", impact.TypeAlertingRisk, domain.CriticalityHigh, false)
	mismatchedCriticality.Criticality = domain.CriticalityCritical
	unknownConsumer := finding("unknown-consumer", impact.TypeVisibilityLoss, domain.CriticalityHigh, false)
	unknownConsumer.Consumer.Kind = domain.ConsumerKind("unknown")
	unknownCriticality := finding("unknown-criticality", impact.TypeAlertingRisk, domain.Criticality("unknown"), false)

	tests := []struct {
		name     string
		findings []impact.Finding
		policy   Policy
		wantErr  string
	}{
		{name: "mode", findings: []impact.Finding{finding("a", impact.TypeAlertingRisk, domain.CriticalityHigh, false)}, policy: Policy{Mode: "off"}, wantErr: "unsupported rollout mode"},
		{name: "impact", policy: Policy{Mode: RolloutEnforce, Impacts: map[impact.Type]ImpactRule{"UNKNOWN": {MinimumCriticality: domain.CriticalityHigh, Action: ActionBlock}}}, wantErr: "unsupported impact type"},
		{name: "action", policy: Policy{Mode: RolloutEnforce, Impacts: map[impact.Type]ImpactRule{impact.TypeAlertingRisk: {MinimumCriticality: domain.CriticalityHigh, Action: "allow"}}}, wantErr: "unsupported action"},
		{name: "criticality", policy: Policy{Mode: RolloutEnforce, Impacts: map[impact.Type]ImpactRule{impact.TypeAlertingRisk: {MinimumCriticality: "unknown", Action: ActionBlock}}}, wantErr: "unsupported minimum criticality"},
		{name: "duplicate finding", findings: []impact.Finding{finding("a", impact.TypeAlertingRisk, domain.CriticalityHigh, false), finding("a", impact.TypeAlertingRisk, domain.CriticalityHigh, false)}, policy: DefaultPolicy(), wantErr: "duplicate finding"},
		{name: "mismatched impact", findings: []impact.Finding{mismatchedImpact}, policy: DefaultPolicy(), wantErr: "maps to ALERTING_RISK"},
		{name: "mismatched criticality", findings: []impact.Finding{mismatchedCriticality}, policy: DefaultPolicy(), wantErr: "criticality does not match"},
		{name: "unknown consumer", findings: []impact.Finding{unknownConsumer}, policy: DefaultPolicy(), wantErr: "no deterministic impact mapping"},
		{name: "unknown finding criticality", findings: []impact.Finding{unknownCriticality}, policy: DefaultPolicy(), wantErr: "unsupported criticality"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, err := Evaluate(safetyChangeSet(), test.findings, nil, test.policy)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want %q", err, test.wantErr)
			}
			if result.Status != StatusError || len(result.Errors) != 1 || len(result.Findings) != len(test.findings) {
				t.Fatalf("error result lost evidence: %#v", result)
			}
		})
	}
}

func TestEvaluateIsDeterministicAndDoesNotAliasInputs(t *testing.T) {
	t.Parallel()

	changeSet := safetyChangeSet()
	findings := []impact.Finding{
		finding("z", impact.TypeSLORisk, domain.CriticalityHigh, false),
		finding("a", impact.TypeVisibilityLoss, domain.CriticalityMedium, false),
	}
	findings[0].Change.Metadata = map[string]string{"source.adapter": "fixture"}
	diagnostics := []domain.Diagnostic{
		{Adapter: "z", Message: "second"},
		{Adapter: "a", Message: "first"},
	}
	first, err := Evaluate(changeSet, findings, diagnostics, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		next, err := Evaluate(changeSet, findings, diagnostics, DefaultPolicy())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(next, first) {
			t.Fatalf("results are nondeterministic\nfirst: %#v\nnext: %#v", first, next)
		}
	}
	if first.Findings[0].Consumer.ID != "a" || first.Diagnostics[0].Adapter != "a" {
		t.Fatalf("result order is not stable: %#v", first)
	}
	first.ChangeSet.Changes[0].To.Name = "result-mutated"
	first.Findings[1].Change.Metadata["source.adapter"] = "result-mutated"
	if changeSet.Changes[0].To.Name != "new_metric" || findings[0].Change.Metadata["source.adapter"] != "fixture" {
		t.Fatal("result aliases caller inputs")
	}
}

func TestMachineResultUsesVersionedLowerCamelSchema(t *testing.T) {
	t.Parallel()

	finding := finding("dashboard", impact.TypeVisibilityLoss, domain.CriticalityHigh, false)
	finding.Paths = []impact.Path{{Nodes: []string{"symbol", "consumer"}, Edges: []graph.EdgeKind{graph.EdgeReferences}}}
	result, err := Evaluate(safetyChangeSet(), []impact.Finding{finding}, nil, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	contents, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"schemaVersion":"tcg-result/v1alpha1"`,
		`"changeSet"`,
		`"findings"`,
		`"decisions"`,
		`"nodes"`,
		`"edges"`,
	} {
		if !strings.Contains(string(contents), expected) {
			t.Errorf("machine result does not contain %s: %s", expected, contents)
		}
	}
	if strings.Contains(string(contents), `"Nodes"`) || strings.Contains(string(contents), `"Edges"`) {
		t.Fatalf("generic path schema leaked legacy field casing: %s", contents)
	}
}

func TestMachineResultGolden(t *testing.T) {
	t.Parallel()

	value := finding("checkout-alert", impact.TypeAlertingRisk, domain.CriticalityCritical, false)
	value.Consumer.Name = "Checkout failures"
	value.Consumer.Source = domain.SourceLocation{File: "monitoring/alerts.yaml", Line: 17}
	value.References = []domain.Reference{{
		ConsumerID: value.Consumer.ID,
		Symbol:     value.Change.From,
		Usage:      domain.UsageSelector,
		Evidence: domain.Evidence{
			Method:     domain.EvidenceMethodPromQLAST,
			Confidence: domain.ConfidenceConfirmed,
			Source:     value.Consumer.Source,
			Expression: "old_metric > 0",
		},
	}}
	value.Paths = []impact.Path{{
		Nodes: []string{"symbol:prometheus:metric::old_metric", "consumer:checkout-alert"},
		Edges: []graph.EdgeKind{graph.EdgeReferences},
	}}
	result, err := Evaluate(safetyChangeSet(), []impact.Finding{value}, nil, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	contents, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	contents = append(contents, '\n')
	want, err := os.ReadFile("testdata/result.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != string(want) {
		t.Fatalf("machine result changed\nwant:\n%s\ngot:\n%s", want, contents)
	}
}

func TestExitCodeContract(t *testing.T) {
	t.Parallel()

	tests := map[Status]int{
		StatusPass:        0,
		StatusWarn:        0,
		StatusError:       1,
		StatusBlock:       2,
		StatusIncomplete:  3,
		Status("UNKNOWN"): 1,
	}
	for status, want := range tests {
		if got := ExitCode(status); got != want {
			t.Errorf("ExitCode(%q) = %d, want %d", status, got, want)
		}
	}
}

func TestErrorResultIsVersionedAndFailClosed(t *testing.T) {
	t.Parallel()

	result := ErrorResult(
		safetyChangeSet(),
		[]impact.Finding{finding("known", impact.TypeVisibilityLoss, domain.CriticalityHigh, false)},
		[]domain.Diagnostic{{Adapter: "fixture", Message: "partial evidence"}},
		errors.New("failure"),
	)
	if result.SchemaVersion != ResultSchemaVersion || result.Status != StatusError || len(result.Errors) != 1 ||
		len(result.Findings) != 1 || len(result.Diagnostics) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if ExitCode(result.Status) != 1 {
		t.Fatalf("error exit code = %d", ExitCode(result.Status))
	}
}

func finding(id string, impactType impact.Type, criticality domain.Criticality, uncertain bool) impact.Finding {
	change := safetyChangeSet().Changes[0]
	kind := domain.ConsumerKindDashboard
	switch impactType {
	case impact.TypeAlertingRisk:
		kind = domain.ConsumerKindAlertRule
	case impact.TypeSLORisk:
		kind = domain.ConsumerKindSLO
	case impact.TypeScalingRisk:
		kind = domain.ConsumerKindAutoscaler
	case impact.TypeDeploymentGateRisk:
		kind = domain.ConsumerKindDeploymentGate
	case impact.TypeAutomationRisk:
		kind = domain.ConsumerKindAutomation
	case impact.TypeSemanticRisk:
		kind = domain.ConsumerKindRecordingRule
	}
	return impact.Finding{
		Change: change,
		Consumer: impact.Consumer{
			ID: id, Kind: kind, Name: id, Criticality: criticality,
		},
		Impact: impactType, Criticality: criticality, Uncertain: uncertain,
	}
}

func safetyChangeSet() domain.ChangeSet {
	destination := domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: "new_metric"}
	return domain.ChangeSet{
		APIVersion: domain.ChangeSetAPIVersion,
		Kind:       domain.ChangeSetKind,
		Metadata:   domain.ChangeSetMetadata{Name: "safety"},
		Changes: []domain.Change{{
			ID: "metric-change", Kind: domain.ChangeKindMetricRename, Domain: domain.DomainPrometheus,
			From: domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: "old_metric"},
			To:   &destination,
		}},
	}
}
