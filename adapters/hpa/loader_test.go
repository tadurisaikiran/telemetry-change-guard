package hpa

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
)

func TestParseMapsExternalMetricAndSelectorLabels(t *testing.T) {
	t.Parallel()

	loader := testLoader(t, validMapping())
	discovery, err := loader.Parse(context.Background(), "deploy/hpa.yaml", strings.NewReader(`
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: checkout-worker
  namespace: commerce
  labels:
    environment: production
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: checkout-worker
  metrics:
    - type: External
      external:
        metric:
          name: queue_messages_ready
          selector:
            matchLabels:
              queue: secret-queue-value
            matchExpressions:
              - key: tenant
                operator: Exists
        target:
          type: AverageValue
          averageValue: "5"
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Consumers) != 1 || len(discovery.References) != 2 || len(discovery.Diagnostics) != 0 {
		t.Fatalf("discovery = %#v", discovery)
	}
	consumer := discovery.Consumers[0]
	if consumer.Kind != domain.ConsumerKindAutoscaler || consumer.Name != "checkout-worker" ||
		consumer.Criticality != domain.CriticalityCritical || consumer.Unresolved ||
		consumer.Metadata["prometheus_metric"] != "rabbitmq_queue_messages_ready" || consumer.Source.Line == 0 {
		t.Fatalf("consumer = %#v", consumer)
	}
	metric := discovery.References[0]
	label := discovery.References[1]
	if metric.Symbol.Kind != domain.SymbolKindMetric || metric.Symbol.Name != "rabbitmq_queue_messages_ready" ||
		metric.Evidence.Method != domain.EvidenceMethodExplicitMapping ||
		metric.Evidence.Confidence != domain.ConfidenceConfirmed || metric.Evidence.Source.File != "config/hpa-mapping.yaml" {
		t.Fatalf("metric reference = %#v", metric)
	}
	if label.Symbol.Kind != domain.SymbolKindLabel || label.Symbol.Name != "rabbitmq_queue" ||
		label.Symbol.Parent != "rabbitmq_queue_messages_ready" || label.Usage != domain.UsageFilter {
		t.Fatalf("label reference = %#v", label)
	}
	encoded, err := json.Marshal(discovery)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-queue-value", "adapter routing label"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("discovery retained selector value or ignore reason %q: %s", secret, encoded)
		}
	}
}

func TestParseSupportsObjectAndPodsWithoutTreatingBuiltinsAsPrometheus(t *testing.T) {
	t.Parallel()

	mapping := `apiVersion: tcg.hpa/v1alpha1
kind: HPAMetricMappings
spec:
  mappings:
    - kubernetes: {type: Object, metric: requests_per_second}
      prometheus: {metric: http_requests_total}
    - kubernetes: {type: Pods, metric: worker_lag}
      prometheus: {metric: worker_lag_seconds}
`
	manifest := `apiVersion: v1
kind: ConfigMap
metadata: {name: unrelated}
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata: {name: api}
spec:
  scaleTargetRef: {name: api}
  metrics:
    - type: Object
      object:
        describedObject: {apiVersion: networking.k8s.io/v1, kind: Ingress, name: api}
        metric: {name: requests_per_second}
        target: {type: Value, value: "10"}
    - type: Pods
      pods:
        metric: {name: worker_lag}
        target: {type: AverageValue, averageValue: "2"}
    - type: Resource
      resource:
        name: cpu
        target: {type: Utilization, averageUtilization: 80}
    - type: ContainerResource
      containerResource:
        name: memory
        container: api
        target: {type: AverageValue, averageValue: 256Mi}
`
	discovery, err := testLoader(t, mapping).Parse(context.Background(), "hpa.yaml", strings.NewReader(manifest))
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Consumers) != 2 || len(discovery.References) != 2 || len(discovery.Diagnostics) != 0 {
		t.Fatalf("discovery = %#v", discovery)
	}
	if discovery.References[0].Symbol.Name != "http_requests_total" ||
		discovery.References[1].Symbol.Name != "worker_lag_seconds" ||
		discovery.Consumers[0].Criticality != domain.CriticalityHigh {
		t.Fatalf("discovery = %#v", discovery)
	}
}

func TestParseRequiresMappingEvenWhenMetricNamesMatch(t *testing.T) {
	t.Parallel()

	mapping := `apiVersion: tcg.hpa/v1alpha1
kind: HPAMetricMappings
spec:
  mappings:
    - kubernetes: {type: External, metric: other_metric}
      prometheus: {metric: same_name_metric}
`
	discovery, err := testLoader(t, mapping).Parse(context.Background(), "hpa.yaml", strings.NewReader(validHPA("same_name_metric")))
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Consumers) != 1 || !discovery.Consumers[0].Unresolved || len(discovery.References) != 0 ||
		len(discovery.Diagnostics) != 1 || !discovery.Diagnostics[0].Required ||
		!strings.Contains(discovery.Diagnostics[0].Message, "explicit backend mapping") {
		t.Fatalf("discovery = %#v", discovery)
	}
}

func TestParseHonorsExplicitNonPrometheusDecision(t *testing.T) {
	t.Parallel()

	mapping := `apiVersion: tcg.hpa/v1alpha1
kind: HPAMetricMappings
spec:
  mappings:
    - kubernetes: {type: External, metric: cloud_queue_depth}
      ignore: supplied by the CloudWatch metrics adapter
`
	discovery, err := testLoader(t, mapping).Parse(context.Background(), "hpa.yaml", strings.NewReader(validHPA("cloud_queue_depth")))
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Consumers) != 0 || len(discovery.References) != 0 || len(discovery.Diagnostics) != 0 {
		t.Fatalf("discovery = %#v", discovery)
	}
}

func TestParseFailsClosedForUnmappedSelectorLabel(t *testing.T) {
	t.Parallel()

	mapping := `apiVersion: tcg.hpa/v1alpha1
kind: HPAMetricMappings
spec:
  mappings:
    - kubernetes: {type: External, metric: queue_depth}
      prometheus: {metric: queue_depth_total}
`
	manifest := strings.Replace(
		validHPA("queue_depth"),
		"          name: queue_depth\n",
		"          name: queue_depth\n          selector:\n            matchLabels: {queue: checkout}\n",
		1,
	)
	discovery, err := testLoader(t, mapping).Parse(context.Background(), "hpa.yaml", strings.NewReader(manifest))
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Consumers) != 1 || discovery.Consumers[0].Unresolved || len(discovery.References) != 2 ||
		len(discovery.Diagnostics) != 1 || !discovery.Diagnostics[0].Required {
		t.Fatalf("discovery = %#v", discovery)
	}
	unresolved := discovery.References[1]
	if !unresolved.RequiresResolution || unresolved.ResolutionScope != domain.ResolutionScopeLabels ||
		unresolved.Pattern != "queue" || unresolved.Symbol.Name != "queue_depth_total" {
		t.Fatalf("unresolved reference = %#v", unresolved)
	}
}

func TestParseAcceptsHPAWithOnlyBuiltInOrDefaultMetrics(t *testing.T) {
	t.Parallel()

	loader := testLoader(t, validMapping())
	for _, manifest := range []string{
		`apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata: {name: api}
spec:
  scaleTargetRef: {name: api}
`,
		`apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata: {name: api}
spec:
  scaleTargetRef: {name: api}
  metrics:
    - type: Resource
      resource:
        name: cpu
        target: {type: Utilization, averageUtilization: 80}
`,
	} {
		discovery, err := loader.Parse(context.Background(), "hpa.yaml", strings.NewReader(manifest))
		if err != nil {
			t.Fatal(err)
		}
		if len(discovery.Consumers) != 0 || len(discovery.Diagnostics) != 0 {
			t.Fatalf("discovery = %#v", discovery)
		}
	}
}

func TestParseRejectsMalformedHPA(t *testing.T) {
	t.Parallel()

	valid := validHPA("queue_depth")
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "empty", content: "", want: "manifest is empty"},
		{name: "no HPA", content: "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x}\n", want: "contains no HorizontalPodAutoscaler"},
		{name: "unsupported version", content: strings.Replace(valid, "autoscaling/v2", "autoscaling/v1", 1), want: "apiVersion must be"},
		{name: "missing name", content: strings.Replace(valid, "  name: worker\n", "  name: \"\"\n", 1), want: "metadata.name"},
		{name: "missing target", content: strings.Replace(valid, "    name: worker\n", "    name: \"\"\n", 1), want: "scaleTargetRef.name"},
		{name: "missing type", content: strings.Replace(valid, "    - type: External\n", "    - future: true\n", 1), want: "type must be"},
		{name: "type whitespace", content: strings.Replace(valid, "type: External", "type: \" External \"", 1), want: "surrounding whitespace"},
		{name: "unknown type", content: strings.Replace(valid, "type: External", "type: Future", 1), want: "type must be"},
		{name: "duplicate type", content: strings.Replace(valid, "    - type: External\n", "    - type: External\n      type: External\n", 1), want: "field \"type\" is duplicated"},
		{name: "duplicate provider", content: strings.Replace(valid, "      external:\n", "      external: {}\n      external:\n", 1), want: "field \"external\" is duplicated"},
		{name: "missing provider", content: strings.Replace(valid, "      external:\n", "      object:\n", 1), want: "external is required"},
		{name: "multiple providers", content: strings.Replace(valid, "      external:\n", "      object: {}\n      external:\n", 1), want: "exactly one metric source"},
		{name: "null provider", content: strings.Replace(valid, "      external:\n        metric:\n          name: queue_depth\n        target: {type: Value, value: \"1\"}\n", "      external: null\n", 1), want: "must be a mapping"},
		{name: "missing metric name", content: strings.Replace(valid, "          name: queue_depth\n", "          name: \"\"\n", 1), want: "metric.name is required"},
		{name: "metric name whitespace", content: strings.Replace(valid, "          name: queue_depth\n", "          name: \" queue_depth \"\n", 1), want: "surrounding whitespace"},
		{name: "bad selector operator", content: strings.Replace(valid, "          name: queue_depth\n", "          name: queue_depth\n          selector:\n            matchExpressions:\n              - {key: queue, operator: Future}\n", 1), want: "operator must be"},
		{name: "In needs values", content: strings.Replace(valid, "          name: queue_depth\n", "          name: queue_depth\n          selector:\n            matchExpressions:\n              - {key: queue, operator: In}\n", 1), want: "values must not be empty"},
		{name: "Exists rejects values", content: strings.Replace(valid, "          name: queue_depth\n", "          name: queue_depth\n          selector:\n            matchExpressions:\n              - {key: queue, operator: Exists, values: [x]}\n", 1), want: "values must be empty"},
	}
	loader := testLoader(t, `apiVersion: tcg.hpa/v1alpha1
kind: HPAMetricMappings
spec:
  mappings:
    - kubernetes: {type: External, metric: queue_depth}
      prometheus: {metric: queue_depth_total}
`)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := loader.Parse(context.Background(), "hpa.yaml", strings.NewReader(test.content))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestParseEnforcesBoundsAndCancellation(t *testing.T) {
	t.Parallel()

	loader := testLoader(t, validMapping())
	_, err := loader.Parse(context.Background(), "hpa.yaml", strings.NewReader(strings.Repeat("x", maxManifestBytes+1)))
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("size error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = loader.Parse(ctx, "hpa.yaml", strings.NewReader(validHPA("queue_messages_ready")))
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestParseRequiresMapping(t *testing.T) {
	t.Parallel()

	_, err := (Loader{Required: true}).Parse(context.Background(), "hpa.yaml", strings.NewReader(validHPA("queue")))
	if err == nil || !strings.Contains(err.Error(), "mapping is required") {
		t.Fatalf("error = %v", err)
	}
}

func FuzzParseManifestDoesNotPanic(f *testing.F) {
	mapping, err := ParseMapping(strings.NewReader(validMapping()))
	if err != nil {
		f.Fatal(err)
	}
	loader := Loader{Required: true, Mapping: mapping, MappingSource: "mapping.yaml"}
	f.Add([]byte(validHPA("queue_messages_ready")))
	f.Add([]byte("apiVersion: ["))
	f.Fuzz(func(t *testing.T, contents []byte) {
		_, _ = loader.Parse(context.Background(), "fuzz.yaml", strings.NewReader(string(contents)))
	})
}

func testLoader(t *testing.T, mappingDocument string) Loader {
	t.Helper()
	mapping, err := ParseMapping(strings.NewReader(mappingDocument))
	if err != nil {
		t.Fatal(err)
	}
	return Loader{Required: true, Mapping: mapping, MappingSource: "config/hpa-mapping.yaml"}
}

func validHPA(metric string) string {
	return `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: worker
spec:
  scaleTargetRef:
    name: worker
  metrics:
    - type: External
      external:
        metric:
          name: ` + metric + `
        target: {type: Value, value: "1"}
`
}
