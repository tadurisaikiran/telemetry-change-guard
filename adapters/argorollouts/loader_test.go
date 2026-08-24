package argorollouts

import (
	"context"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
)

func TestParseDiscoversParameterizedPrometheusDeploymentGate(t *testing.T) {
	t.Parallel()

	discovery, err := (Loader{Required: true}).Parse(context.Background(), "deploy/analysis.yaml", strings.NewReader(`
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: checkout-success
  namespace: commerce
  labels:
    environment: production
spec:
  args:
    - name: service-name
  metrics:
    - name: success-rate
      successCondition: result[0] >= 0.99
      provider:
        prometheus:
          address: https://prometheus.example.test
          headers:
            - key: Authorization
              value: secret-value
          authentication:
            oauth2:
              clientSecret: another-secret
          query: |
            sum(rate(checkout_requests_total{service="{{ args.service-name }}",status=~"2.."}[5m]))
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(discovery.Consumers), 1; got != want {
		t.Fatalf("len(Consumers) = %d, want %d", got, want)
	}
	consumer := discovery.Consumers[0]
	if consumer.Kind != domain.ConsumerKindDeploymentGate || consumer.Name != "checkout-success / success-rate" {
		t.Fatalf("consumer = %#v", consumer)
	}
	if consumer.Criticality != domain.CriticalityCritical {
		t.Errorf("criticality = %q, want critical", consumer.Criticality)
	}
	if consumer.Source.Line == 0 || consumer.Source.Column == 0 {
		t.Errorf("source = %#v, want line and column", consumer.Source)
	}
	if consumer.Metadata["template_kind"] != "AnalysisTemplate" || consumer.Metadata["query_type"] != "instant" {
		t.Errorf("metadata = %#v", consumer.Metadata)
	}
	for key, value := range consumer.Metadata {
		combined := strings.ToLower(key + "=" + value)
		for _, forbidden := range []string{"address", "header", "auth", "secret", "prometheus.example.test"} {
			if strings.Contains(combined, forbidden) {
				t.Fatalf("sensitive provider metadata was retained: %q=%q", key, value)
			}
		}
	}
	if got, want := len(discovery.References), 3; got != want {
		t.Fatalf("len(References) = %d, want %d: %#v", got, want, discovery.References)
	}
	assertReference(t, discovery, domain.SymbolKindMetric, "checkout_requests_total", "")
	assertReference(t, discovery, domain.SymbolKindLabel, "service", "checkout_requests_total")
	assertReference(t, discovery, domain.SymbolKindLabel, "status", "checkout_requests_total")
	for _, reference := range discovery.References {
		if !strings.Contains(reference.Evidence.Expression, "{{ args.service-name }}") {
			t.Errorf("evidence expression did not preserve the original query: %q", reference.Evidence.Expression)
		}
	}
	if len(discovery.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %#v, want none", discovery.Diagnostics)
	}
}

func TestParseSupportsClusterTemplateRangeQueryAndMultipleDocuments(t *testing.T) {
	t.Parallel()

	discovery, err := (Loader{Required: true}).Parse(context.Background(), "analysis.yaml", strings.NewReader(`
apiVersion: v1
kind: ConfigMap
metadata: {name: settings}
---
apiVersion: argoproj.io/v1alpha1
kind: ClusterAnalysisTemplate
metadata:
  name: latency-gate
  namespace: ignored-for-cluster-scope
spec:
  metrics:
    - name: latency
      provider:
        prometheus:
          address: http://prometheus.monitoring:9090
          query: http_request_duration_seconds
          rangeQuery:
            start: now() - duration("5m")
            end: now()
            step: 1m
---
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: error-gate
spec:
  metrics:
    - name: errors
      provider:
        prometheus:
          query: http_requests_total
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(discovery.Consumers), 2; got != want {
		t.Fatalf("len(Consumers) = %d, want %d", got, want)
	}
	if discovery.Consumers[0].Metadata["namespace"] != "cluster" || discovery.Consumers[0].Metadata["query_type"] != "range" {
		t.Errorf("cluster metadata = %#v", discovery.Consumers[0].Metadata)
	}
	if discovery.Consumers[1].Metadata["namespace"] != "default" || discovery.Consumers[1].Metadata["query_type"] != "instant" {
		t.Errorf("namespaced metadata = %#v", discovery.Consumers[1].Metadata)
	}
	if discovery.Consumers[0].ID == discovery.Consumers[1].ID {
		t.Fatalf("consumer IDs are not unique: %q", discovery.Consumers[0].ID)
	}
}

func TestParseTemplateReferencesAndNonPrometheusProvidersHaveNoMetricConsumers(t *testing.T) {
	t.Parallel()

	discovery, err := (Loader{Required: true}).Parse(context.Background(), "analysis.yaml", strings.NewReader(`
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: combined-gate
spec:
  templates:
    - templateName: shared-gate
      clusterScope: true
  metrics:
    - name: smoke-test
      provider:
        job:
          spec: {}
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(discovery.Consumers) != 0 || len(discovery.References) != 0 || len(discovery.Diagnostics) != 0 {
		t.Fatalf("discovery = %#v, want no Prometheus evidence", discovery)
	}
}

func TestParseRejectsDynamicTelemetryIdentity(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		query string
	}{
		{name: "metric name", query: `{{args.metric-name}}`},
		{name: "metric name matcher", query: `{__name__="{{args.metric-name}}"}`},
		{name: "label function name", query: `label_replace(up, "{{args.destination-label}}", "$1", "job", "(.*)")`},
		{name: "range duration", query: `rate(requests_total[{{args.lookback}}])`},
		{name: "unsupported template", query: `requests_total{service="{{workflow.name}}"}`},
		{name: "marker collision", query: `requests_total{service="{{args.service}}tcg_argo_argument_value_98f1a6c4_"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			discovery, err := (Loader{Required: true}).Parse(
				context.Background(),
				"analysis.yaml",
				strings.NewReader(validManifest(test.query)),
			)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if len(discovery.Consumers) != 1 || !discovery.Consumers[0].Unresolved {
				t.Fatalf("Consumers = %#v", discovery.Consumers)
			}
			if len(discovery.Diagnostics) != 1 || !discovery.Diagnostics[0].Required {
				t.Fatalf("Diagnostics = %#v", discovery.Diagnostics)
			}
			if len(discovery.References) != 0 {
				t.Fatalf("References = %#v, want none", discovery.References)
			}
		})
	}
}

func TestAnalyzePromQLSupportsMultipleMatcherArgumentsWithoutChangingEvidence(t *testing.T) {
	t.Parallel()

	query := `requests_total{service="{{args.service}}123",instance=~"prefix-{{ args.instance }}"}`
	analysis, err := analyzePromQL(query)
	if err != nil {
		t.Fatalf("analyzePromQL() error = %v", err)
	}
	if len(analysis.Unresolved) != 0 || len(analysis.References) != 3 {
		t.Fatalf("analysis = %#v", analysis)
	}
	for _, reference := range analysis.References {
		if reference.Evidence.Expression != query {
			t.Errorf("evidence expression = %q, want original query", reference.Evidence.Expression)
		}
	}
}

func TestParseMissingAndMalformedPromQLFailClosed(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		query string
	}{
		{name: "missing", query: ""},
		{name: "malformed", query: `rate(requests_total[)`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			discovery, err := (Loader{Required: true}).Parse(
				context.Background(),
				"analysis.yaml",
				strings.NewReader(validManifest(test.query)),
			)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if len(discovery.Consumers) != 1 || !discovery.Consumers[0].Unresolved ||
				len(discovery.Diagnostics) != 1 || !discovery.Diagnostics[0].Required {
				t.Fatalf("discovery = %#v", discovery)
			}
		})
	}
}

func TestParseProductionLabels(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		key   string
		value string
		want  domain.Criticality
	}{
		{name: "environment production", key: "environment", value: "production", want: domain.CriticalityCritical},
		{name: "env prod", key: "env", value: "PROD", want: domain.CriticalityCritical},
		{name: "application environment", key: "app.kubernetes.io/environment", value: "prod", want: domain.CriticalityCritical},
		{name: "staging", key: "environment", value: "staging", want: domain.CriticalityHigh},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := deploymentGateCriticality(map[string]string{test.key: test.value}); got != test.want {
				t.Fatalf("deploymentGateCriticality() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestParseRejectsInvalidTemplates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "empty", content: "", want: "empty"},
		{name: "no template", content: "apiVersion: v1\nkind: ConfigMap\n", want: "contains no AnalysisTemplate"},
		{name: "unsupported version", content: strings.Replace(validManifest("up"), "argoproj.io/v1alpha1", "argoproj.io/v2", 1), want: "apiVersion must be"},
		{name: "missing name", content: strings.Replace(validManifest("up"), "name: rollout-gate", "name: ''", 1), want: "metadata.name"},
		{name: "empty spec", content: "apiVersion: argoproj.io/v1alpha1\nkind: AnalysisTemplate\nmetadata: {name: gate}\nspec: {}\n", want: "at least one metric"},
		{name: "missing template ref", content: "apiVersion: argoproj.io/v1alpha1\nkind: AnalysisTemplate\nmetadata: {name: gate}\nspec: {templates: [{}]}\n", want: "templateName"},
		{name: "missing metric name", content: strings.Replace(validManifest("up"), "name: error-rate", "name: ''", 1), want: "metrics[0].name"},
		{name: "duplicate metric", content: strings.Replace(validManifest("up"), "  metrics:", "  metrics:\n    - name: error-rate\n      provider: {web: {url: https://example.test}}", 1), want: "duplicates"},
		{name: "missing provider", content: strings.Replace(validManifest("up"), "      provider:\n        prometheus:\n          query: |-\n            up", "", 1), want: "exactly one provider"},
		{name: "empty provider", content: strings.Replace(validManifest("up"), "      provider:\n        prometheus:\n          query: |-\n            up", "      provider: {}", 1), want: "exactly one provider"},
		{name: "multiple providers", content: strings.Replace(validManifest("up"), "        prometheus:", "        web: {}\n        prometheus:", 1), want: "exactly one provider"},
		{name: "null prometheus", content: strings.Replace(validManifest("up"), "        prometheus:\n          query: |-\n            up", "        prometheus: null", 1), want: "prometheus provider must be a mapping"},
		{name: "malformed yaml", content: "kind: AnalysisTemplate\nspec: [", want: "decode YAML"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := (Loader{Required: true}).Parse(context.Background(), "analysis.yaml", strings.NewReader(test.content))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Parse() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestParseHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (Loader{Required: true}).Parse(ctx, "analysis.yaml", strings.NewReader(validManifest("up")))
	if !strings.Contains(errorString(err), context.Canceled.Error()) {
		t.Fatalf("Parse() error = %v, want cancellation", err)
	}
}

func TestParseRejectsOversizedManifest(t *testing.T) {
	t.Parallel()

	_, err := (Loader{Required: true}).Parse(context.Background(), "analysis.yaml", strings.NewReader(strings.Repeat("x", maxManifestBytes+1)))
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("Parse() error = %v, want size-limit error", err)
	}
}

func FuzzParseDoesNotPanic(f *testing.F) {
	f.Add(validManifest("up"))
	f.Add(validManifest(`requests_total{service="{{args.service}}"}`))
	f.Add("apiVersion: v1\nkind: ConfigMap\n")
	f.Add("kind: AnalysisTemplate\nspec: [")
	f.Fuzz(func(t *testing.T, content string) {
		_, _ = (Loader{Required: true}).Parse(context.Background(), "fuzz.yaml", strings.NewReader(content))
	})
}

func validManifest(query string) string {
	return `apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: rollout-gate
spec:
  metrics:
    - name: error-rate
      provider:
        prometheus:
          query: |-
            ` + strings.ReplaceAll(query, "\n", "\n            ") + "\n"
}

func assertReference(t *testing.T, discovery domain.Discovery, kind domain.SymbolKind, name, parent string) {
	t.Helper()
	for _, reference := range discovery.References {
		if reference.Symbol.Kind == kind && reference.Symbol.Name == name && reference.Symbol.Parent == parent {
			if reference.ConsumerID != discovery.Consumers[0].ID {
				t.Errorf("reference consumer = %q, want %q", reference.ConsumerID, discovery.Consumers[0].ID)
			}
			return
		}
	}
	t.Errorf("reference %s/%s parent %q not found in %#v", kind, name, parent, discovery.References)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
