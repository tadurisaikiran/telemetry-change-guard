package promql

import (
	"fmt"
	"testing"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
)

func TestAnalyzeExtractsPromQLReferences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		expression string
		want       []referenceExpectation
	}{
		{
			name:       "metric selector",
			expression: `http_requests_total`,
			want: []referenceExpectation{
				{kind: domain.SymbolKindMetric, name: "http_requests_total", usage: domain.UsageSelector},
			},
		},
		{
			name:       "label matchers",
			expression: `rate(http_requests_total{status=~"5..", method="GET"}[5m])`,
			want: []referenceExpectation{
				{kind: domain.SymbolKindMetric, name: "http_requests_total", usage: domain.UsageSelector},
				{kind: domain.SymbolKindLabel, name: "status", parent: "http_requests_total", usage: domain.UsageFilter},
				{kind: domain.SymbolKindLabel, name: "method", parent: "http_requests_total", usage: domain.UsageFilter},
			},
		},
		{
			name:       "by grouping",
			expression: `sum by(service, route) (http_requests_total)`,
			want: []referenceExpectation{
				{kind: domain.SymbolKindLabel, name: "service", parent: "http_requests_total", usage: domain.UsageGrouping},
				{kind: domain.SymbolKindLabel, name: "route", parent: "http_requests_total", usage: domain.UsageGrouping},
			},
		},
		{
			name:       "without grouping",
			expression: `sum without(instance) (http_requests_total)`,
			want: []referenceExpectation{
				{kind: domain.SymbolKindLabel, name: "instance", parent: "http_requests_total", usage: domain.UsageGrouping},
			},
		},
		{
			name:       "on vector matching",
			expression: `requests_total / on(service) request_limit`,
			want: []referenceExpectation{
				{kind: domain.SymbolKindLabel, name: "service", parent: "requests_total", usage: domain.UsageVectorMatching},
				{kind: domain.SymbolKindLabel, name: "service", parent: "request_limit", usage: domain.UsageVectorMatching},
			},
		},
		{
			name:       "ignoring vector matching",
			expression: `requests_total / ignoring(instance) request_limit`,
			want: []referenceExpectation{
				{kind: domain.SymbolKindLabel, name: "instance", parent: "requests_total", usage: domain.UsageVectorMatching},
				{kind: domain.SymbolKindLabel, name: "instance", parent: "request_limit", usage: domain.UsageVectorMatching},
			},
		},
		{
			name:       "group left include",
			expression: `requests_total / on(service) group_left(team) request_limit`,
			want: []referenceExpectation{
				{kind: domain.SymbolKindLabel, name: "team", parent: "requests_total", usage: domain.UsageVectorMatching},
				{kind: domain.SymbolKindLabel, name: "team", parent: "request_limit", usage: domain.UsageVectorMatching},
			},
		},
		{
			name:       "group right include",
			expression: `requests_total / on(service) group_right(region) request_limit`,
			want: []referenceExpectation{
				{kind: domain.SymbolKindLabel, name: "region", parent: "requests_total", usage: domain.UsageVectorMatching},
				{kind: domain.SymbolKindLabel, name: "region", parent: "request_limit", usage: domain.UsageVectorMatching},
			},
		},
		{
			name:       "label replace",
			expression: `label_replace(http_requests_total, "service", "$1", "job", "(.*)")`,
			want: []referenceExpectation{
				{kind: domain.SymbolKindLabel, name: "service", parent: "http_requests_total", usage: domain.UsageGeneratedName},
				{kind: domain.SymbolKindLabel, name: "job", parent: "http_requests_total", usage: domain.UsageFilter},
			},
		},
		{
			name:       "label join",
			expression: `label_join(http_requests_total, "endpoint", "/", "service", "route")`,
			want: []referenceExpectation{
				{kind: domain.SymbolKindLabel, name: "endpoint", parent: "http_requests_total", usage: domain.UsageGeneratedName},
				{kind: domain.SymbolKindLabel, name: "service", parent: "http_requests_total", usage: domain.UsageFilter},
				{kind: domain.SymbolKindLabel, name: "route", parent: "http_requests_total", usage: domain.UsageFilter},
			},
		},
		{
			name:       "exact name matcher",
			expression: `{__name__="checkout_requests_total"}`,
			want: []referenceExpectation{
				{kind: domain.SymbolKindMetric, name: "checkout_requests_total", usage: domain.UsageSelector},
			},
		},
		{
			name:       "metric name pattern",
			expression: `{__name__=~"http_.+_requests_total"}`,
			want: []referenceExpectation{
				{kind: domain.SymbolKindMetric, name: `__name__=~"http_.+_requests_total"`, usage: domain.UsagePattern, unresolved: true},
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			analysis, err := Analyze(test.expression)
			if err != nil {
				t.Fatalf("Analyze() error = %v", err)
			}
			if len(analysis.Unresolved) != 0 {
				t.Fatalf("Analyze() unresolved = %v", analysis.Unresolved)
			}
			for _, expectation := range test.want {
				if !containsReference(analysis.References, expectation) {
					t.Errorf("references do not contain %+v; got:\n%s", expectation, formatReferences(analysis.References))
				}
			}
		})
	}
}

func TestAnalyzeDoesNotSubstringMatchMetricNames(t *testing.T) {
	t.Parallel()

	analysis, err := Analyze(`checkout_requests_total_backup`)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	for _, reference := range analysis.References {
		if reference.Symbol.Name == "checkout_requests_total" {
			t.Fatalf("found false positive reference: %+v", reference)
		}
	}
}

func TestAnalyzeTreatsTemplatesAsUnresolved(t *testing.T) {
	t.Parallel()

	analysis, err := Analyze(`rate(${service}_request_duration_seconds[5m])`)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if got, want := len(analysis.Unresolved), 1; got != want {
		t.Fatalf("len(Unresolved) = %d, want %d", got, want)
	}
	if len(analysis.References) != 0 {
		t.Fatalf("References = %v, want none for unresolved template", analysis.References)
	}
}

func TestAnalyzeRejectsInvalidPromQL(t *testing.T) {
	t.Parallel()

	if _, err := Analyze(`rate(http_requests_total[)`); err == nil {
		t.Fatal("Analyze() error = nil, want parse error")
	}
}

func TestValidateRejectsUnresolvedTemplate(t *testing.T) {
	t.Parallel()

	if err := Validate(`rate($${service}[5m])`); err == nil {
		t.Fatal("Validate() error = nil, want unresolved error")
	}
}

func FuzzAnalyzeNeverPanics(f *testing.F) {
	for _, seed := range []string{
		`http_requests_total`,
		`sum by(service) (rate(http_requests_total{status=~"5.."}[5m]))`,
		`left / on(service) group_left(team) right`,
		`label_replace(metric, "dst", "$1", "src", "(.*)")`,
		`{__name__=~"http_.+"}`,
		`rate(http_requests_total[)`,
		`${service}_requests_total`,
	} {
		f.Add(seed)
	}

	f.Fuzz(func(_ *testing.T, expression string) {
		_, _ = Analyze(expression)
	})
}

type referenceExpectation struct {
	kind       domain.SymbolKind
	name       string
	parent     string
	usage      domain.UsageType
	unresolved bool
}

func containsReference(references []domain.Reference, expectation referenceExpectation) bool {
	for _, reference := range references {
		if reference.Symbol.Kind == expectation.kind &&
			reference.Symbol.Name == expectation.name &&
			reference.Symbol.Parent == expectation.parent &&
			reference.Usage == expectation.usage &&
			reference.RequiresResolution == expectation.unresolved {
			return true
		}
	}
	return false
}

func formatReferences(references []domain.Reference) string {
	formatted := ""
	for _, reference := range references {
		formatted += fmt.Sprintf("%s %s parent=%s usage=%s unresolved=%t\n",
			reference.Symbol.Kind,
			reference.Symbol.Name,
			reference.Symbol.Parent,
			reference.Usage,
			reference.RequiresResolution,
		)
	}
	return formatted
}
