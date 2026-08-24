package config

import (
	"strings"
	"testing"
)

func TestParseConfigSupportsScalarAndMappedSources(t *testing.T) {
	t.Parallel()

	configuration, err := ParseConfig(strings.NewReader(`apiVersion: tmr/v1alpha1
sources:
  prometheusRules:
    - ./monitoring/**/*.yaml
  grafana:
    - path: ./grafana/*.json
      required: false
`))
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if !configuration.Sources.PrometheusRules[0].Required {
		t.Error("scalar source Required = false, want default true")
	}
	if configuration.Sources.Grafana[0].Required {
		t.Error("mapped source Required = true, want false")
	}
	if !configuration.Analysis.IncludeTransitiveDependencies {
		t.Error("IncludeTransitiveDependencies = false, want default true")
	}
	if got, want := configuration.Policy.MinimumBlockingCriticality, "high"; got != want {
		t.Errorf("MinimumBlockingCriticality = %q, want %q", got, want)
	}
}

func TestParseConfigRejectsUnknownSourceField(t *testing.T) {
	t.Parallel()

	_, err := ParseConfig(strings.NewReader(`apiVersion: tmr/v1alpha1
sources:
  grafana:
    - path: ./grafana/*.json
      surprise: true
`))
	if err == nil {
		t.Fatal("ParseConfig() error = nil, want unknown-field error")
	}
}
