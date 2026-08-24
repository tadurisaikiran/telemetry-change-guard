package hpa

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMappingRequiresExplicitBackendDecisions(t *testing.T) {
	t.Parallel()

	mapping, err := ParseMapping(strings.NewReader(validMapping()))
	if err != nil {
		t.Fatal(err)
	}
	if mapping.EntryCount() != 2 {
		t.Fatalf("entries = %d, want 2", mapping.EntryCount())
	}
	external, exists := mapping.lookup(MetricTypeExternal, "queue_messages_ready")
	if !exists || external.Prometheus == nil || external.Ignore != "" ||
		external.Prometheus.Metric != "rabbitmq_queue_messages_ready" ||
		external.Prometheus.Labels["queue"] != "rabbitmq_queue" ||
		external.Prometheus.IgnoredLabels["tenant"] == "" {
		t.Fatalf("external mapping = %#v", external)
	}
	pods, exists := mapping.lookup(MetricTypePods, "requests_per_second")
	if !exists || pods.Prometheus != nil || pods.Ignore == "" {
		t.Fatalf("Pods mapping = %#v", pods)
	}
}

func TestParseMappingRejectsUnsafeOrAmbiguousMappings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "empty", content: "", want: "mapping is empty"},
		{name: "unknown field", content: strings.Replace(validMapping(), "spec:\n", "future: true\nspec:\n", 1), want: "field future not found"},
		{name: "version", content: strings.Replace(validMapping(), MappingAPIVersion, "tcg.hpa/v2", 1), want: "apiVersion"},
		{name: "kind", content: strings.Replace(validMapping(), MappingKind, "Other", 1), want: "kind"},
		{name: "additional document", content: validMapping() + "---\n{}\n", want: "exactly one"},
		{name: "empty entries", content: mappingWithEntries("[]"), want: "at least one"},
		{name: "unknown type", content: mappingWithEntry("Unknown", "queue", "    ignore: elsewhere\n"), want: "External, Object, or Pods"},
		{name: "missing metric", content: mappingWithEntry("External", "", "    ignore: elsewhere\n"), want: "kubernetes.metric"},
		{name: "metric whitespace", content: mappingWithEntry("External", " queue ", "    ignore: elsewhere\n"), want: "surrounding whitespace"},
		{name: "missing decision", content: mappingWithEntry("External", "queue", ""), want: "exactly one"},
		{name: "two decisions", content: mappingWithEntry("External", "queue", "    ignore: elsewhere\n    prometheus:\n      metric: queue_total\n"), want: "exactly one"},
		{name: "empty prometheus metric", content: mappingWithEntry("External", "queue", "    prometheus: {}\n"), want: "prometheus.metric"},
		{name: "prometheus metric whitespace", content: mappingWithEntry("External", "queue", "    prometheus: {metric: \" queue_total \"}\n"), want: "surrounding whitespace"},
		{name: "metric identity label", content: mappingWithEntry("External", "queue", "    prometheus:\n      metric: queue_total\n      labels: {queue: __name__}\n"), want: "metric identity"},
		{name: "overlap", content: mappingWithEntry("External", "queue", "    prometheus:\n      metric: queue_total\n      labels: {tenant: tenant}\n      ignoredLabels: {tenant: routing}\n"), want: "must not also appear"},
		{name: "empty ignore reason", content: mappingWithEntry("External", "queue", "    prometheus:\n      metric: queue_total\n      ignoredLabels: {tenant: \"\"}\n"), want: "reason must be non-empty"},
		{name: "whitespace ignore reason", content: mappingWithEntry("External", "queue", "    prometheus:\n      metric: queue_total\n      ignoredLabels: {tenant: \"   \"}\n"), want: "reason must be non-empty"},
		{name: "duplicate prometheus label", content: mappingWithEntry("External", "queue", "    prometheus:\n      metric: queue_total\n      labels: {queue: target, namespace: target}\n"), want: "same Prometheus label"},
		{name: "whitespace key", content: mappingWithEntry("External", "queue", "    prometheus:\n      metric: queue_total\n      labels: {\" \" : target}\n"), want: "label names must be non-empty"},
		{name: "duplicate selector", content: strings.Replace(validMapping(), "    - kubernetes:\n        type: Pods", "    - kubernetes:\n        type: External\n        metric: queue_messages_ready\n      ignore: duplicate\n    - kubernetes:\n        type: Pods", 1), want: "duplicates spec.mappings[0]"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseMapping(strings.NewReader(test.content))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestParseMappingEnforcesSizeLimit(t *testing.T) {
	t.Parallel()

	_, err := ParseMapping(strings.NewReader(strings.Repeat("x", maxMappingBytes+1)))
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("error = %v, want size-limit error", err)
	}
}

func TestLoadMappingObservesCancellation(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "mapping.yaml")
	if err := os.WriteFile(path, []byte(validMapping()), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := LoadMapping(ctx, path)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("error = %v, want cancellation", err)
	}
}

func FuzzParseMappingDoesNotPanic(f *testing.F) {
	f.Add([]byte(validMapping()))
	f.Add([]byte("apiVersion: ["))
	f.Fuzz(func(t *testing.T, contents []byte) {
		_, _ = ParseMapping(strings.NewReader(string(contents)))
	})
}

func validMapping() string {
	return `apiVersion: tcg.hpa/v1alpha1
kind: HPAMetricMappings
spec:
  mappings:
    - kubernetes:
        type: External
        metric: queue_messages_ready
      prometheus:
        metric: rabbitmq_queue_messages_ready
        labels:
          queue: rabbitmq_queue
        ignoredLabels:
          tenant: adapter routing label
    - kubernetes:
        type: Pods
        metric: requests_per_second
      ignore: supplied by a non-Prometheus adapter
`
}

func mappingWithEntries(entries string) string {
	return "apiVersion: tcg.hpa/v1alpha1\nkind: HPAMetricMappings\nspec:\n  mappings: " + entries + "\n"
}

func mappingWithEntry(metricType, metric, decision string) string {
	if decision != "" {
		decision = "  " + strings.ReplaceAll(strings.TrimSuffix(decision, "\n"), "\n", "\n  ") + "\n"
	}
	return "apiVersion: tcg.hpa/v1alpha1\nkind: HPAMetricMappings\nspec:\n  mappings:\n" +
		"    - kubernetes:\n        type: " + metricType + "\n        metric: \"" + metric + "\"\n" + decision
}
