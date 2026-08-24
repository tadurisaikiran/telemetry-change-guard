package keda

import (
	"context"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
)

func TestParseDiscoversProductionPrometheusScaler(t *testing.T) {
	t.Parallel()

	discovery, err := (Loader{Required: true}).Parse(context.Background(), "deploy/keda.yaml", strings.NewReader(`
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: orders-worker-scaler
  namespace: commerce
  labels:
    app.kubernetes.io/environment: production
spec:
  scaleTargetRef:
    name: orders-worker
  minReplicaCount: 2
  maxReplicaCount: 40
  triggers:
    - type: prometheus
      name: request-rate
      metadata:
        serverAddress: https://prometheus.example.test
        customHeaders: Authorization=secret-value
        threshold: "100"
        query: sum by (service) (rate(checkout_requests_total{route="orders"}[2m]))
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(discovery.Consumers), 1; got != want {
		t.Fatalf("len(Consumers) = %d, want %d", got, want)
	}
	consumer := discovery.Consumers[0]
	if consumer.Kind != domain.ConsumerKindAutoscaler || consumer.Name != "orders-worker" {
		t.Fatalf("consumer = %#v", consumer)
	}
	if consumer.Criticality != domain.CriticalityCritical {
		t.Errorf("criticality = %q, want critical", consumer.Criticality)
	}
	if consumer.Source.Line == 0 || consumer.Source.Column == 0 {
		t.Errorf("source location = %#v, want line and column", consumer.Source)
	}
	if consumer.Metadata["scaled_object"] != "orders-worker-scaler" || consumer.Metadata["trigger_name"] != "request-rate" {
		t.Errorf("metadata = %#v", consumer.Metadata)
	}
	for key, value := range consumer.Metadata {
		if strings.Contains(strings.ToLower(key), "server") || strings.Contains(value, "secret-value") || strings.Contains(value, "prometheus.example.test") {
			t.Fatalf("sensitive scaler metadata was retained: %q=%q", key, value)
		}
	}
	if got, want := len(discovery.References), 3; got != want {
		t.Fatalf("len(References) = %d, want %d: %#v", got, want, discovery.References)
	}
	assertReference(t, discovery, domain.SymbolKindMetric, "checkout_requests_total", "")
	assertReference(t, discovery, domain.SymbolKindLabel, "route", "checkout_requests_total")
	assertReference(t, discovery, domain.SymbolKindLabel, "service", "checkout_requests_total")
	if len(discovery.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %#v, want none", discovery.Diagnostics)
	}
}

func TestParseCreatesOneConsumerPerPrometheusTriggerAndIgnoresOtherTriggers(t *testing.T) {
	t.Parallel()

	discovery, err := (Loader{Required: true}).Parse(context.Background(), "scaledobject.yaml", strings.NewReader(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: deployment-settings
---
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: worker
spec:
  scaleTargetRef:
    name: worker
  triggers:
    - type: kafka
      metadata:
        topic: jobs
    - type: prometheus
      metadata:
        query: jobs_waiting_total
    - type: prometheus
      metadata:
        query: jobs_running_total
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(discovery.Consumers), 2; got != want {
		t.Fatalf("len(Consumers) = %d, want %d", got, want)
	}
	if discovery.Consumers[0].ID == discovery.Consumers[1].ID {
		t.Fatalf("consumer IDs are not unique: %q", discovery.Consumers[0].ID)
	}
	for _, consumer := range discovery.Consumers {
		if consumer.Criticality != domain.CriticalityHigh {
			t.Errorf("criticality = %q, want high", consumer.Criticality)
		}
	}
	if got, want := len(discovery.References), 2; got != want {
		t.Fatalf("len(References) = %d, want %d", got, want)
	}
}

func TestParseNonPrometheusScaledObjectHasNoMetricConsumers(t *testing.T) {
	t.Parallel()

	discovery, err := (Loader{Required: true}).Parse(context.Background(), "scaledobject.yaml", strings.NewReader(`
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: queue-worker
spec:
  scaleTargetRef:
    name: queue-worker
  triggers:
    - type: kafka
      metadata:
        topic: jobs
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(discovery.Consumers) != 0 || len(discovery.References) != 0 || len(discovery.Diagnostics) != 0 {
		t.Fatalf("discovery = %#v, want no Prometheus evidence", discovery)
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
		{name: "env prod case insensitive", key: "env", value: "PROD", want: domain.CriticalityCritical},
		{name: "application environment", key: "app.kubernetes.io/environment", value: "prod", want: domain.CriticalityCritical},
		{name: "staging", key: "environment", value: "staging", want: domain.CriticalityHigh},
		{name: "unrecognized key", key: "stage", value: "production", want: domain.CriticalityHigh},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := autoscalerCriticality(map[string]string{test.key: test.value}); got != test.want {
				t.Fatalf("autoscalerCriticality() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestParseMalformedPromQLFailsClosed(t *testing.T) {
	t.Parallel()

	discovery, err := (Loader{Required: true}).Parse(context.Background(), "scaledobject.yaml", strings.NewReader(validManifest(`rate(checkout_requests_total[)`)))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(discovery.Consumers), 1; got != want {
		t.Fatalf("len(Consumers) = %d, want %d", got, want)
	}
	if !discovery.Consumers[0].Unresolved {
		t.Error("consumer Unresolved = false, want true")
	}
	if got, want := len(discovery.Diagnostics), 1; got != want {
		t.Fatalf("len(Diagnostics) = %d, want %d", got, want)
	}
	if !discovery.Diagnostics[0].Required || discovery.Diagnostics[0].Adapter != "keda" {
		t.Fatalf("diagnostic = %#v", discovery.Diagnostics[0])
	}
	if len(discovery.References) != 0 {
		t.Fatalf("References = %#v, want none", discovery.References)
	}
}

func TestParseMissingQueryFailsClosed(t *testing.T) {
	t.Parallel()

	discovery, err := (Loader{Required: false}).Parse(context.Background(), "scaledobject.yaml", strings.NewReader(validManifest("")))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(discovery.Consumers) != 1 || !discovery.Consumers[0].Unresolved {
		t.Fatalf("Consumers = %#v", discovery.Consumers)
	}
	if len(discovery.Diagnostics) != 1 || discovery.Diagnostics[0].Required {
		t.Fatalf("Diagnostics = %#v", discovery.Diagnostics)
	}
}

func TestParseRejectsInvalidScaledObjects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "empty", content: "", want: "empty"},
		{name: "no scaled object", content: "apiVersion: v1\nkind: ConfigMap\n", want: "contains no ScaledObject"},
		{name: "unsupported version", content: strings.Replace(validManifest("up"), "keda.sh/v1alpha1", "keda.sh/v2", 1), want: "apiVersion must be"},
		{name: "missing name", content: strings.Replace(validManifest("up"), "name: autoscaler", "name: ''", 1), want: "metadata.name"},
		{name: "missing target", content: strings.Replace(validManifest("up"), "name: worker", "name: ''", 1), want: "scaleTargetRef.name"},
		{name: "no triggers", content: strings.Split(validManifest("up"), "  triggers:")[0], want: "spec.triggers"},
		{name: "missing trigger type", content: strings.Replace(validManifest("up"), "type: prometheus", "type: ''", 1), want: "triggers[0].type"},
		{name: "missing trigger metadata", content: strings.Replace(validManifest("up"), "      metadata:\n        query: up", "", 1), want: "triggers[0].metadata"},
		{name: "malformed yaml", content: "kind: ScaledObject\nspec: [", want: "decode YAML"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := (Loader{Required: true}).Parse(context.Background(), "scaledobject.yaml", strings.NewReader(test.content))
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
	_, err := (Loader{Required: true}).Parse(ctx, "scaledobject.yaml", strings.NewReader(validManifest("up")))
	if !strings.Contains(errorString(err), context.Canceled.Error()) {
		t.Fatalf("Parse() error = %v, want cancellation", err)
	}
}

func TestParseRejectsOversizedManifest(t *testing.T) {
	t.Parallel()

	_, err := (Loader{Required: true}).Parse(context.Background(), "scaledobject.yaml", strings.NewReader(strings.Repeat("x", maxManifestBytes+1)))
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("Parse() error = %v, want size-limit error", err)
	}
}

func FuzzParseDoesNotPanic(f *testing.F) {
	f.Add(validManifest("up"))
	f.Add("apiVersion: v1\nkind: ConfigMap\n")
	f.Add("kind: ScaledObject\nspec: [")
	f.Fuzz(func(t *testing.T, content string) {
		_, _ = (Loader{Required: true}).Parse(context.Background(), "fuzz.yaml", strings.NewReader(content))
	})
}

func validManifest(query string) string {
	return `apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: autoscaler
spec:
  scaleTargetRef:
    name: worker
  triggers:
    - type: prometheus
      metadata:
        query: ` + query + "\n"
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
