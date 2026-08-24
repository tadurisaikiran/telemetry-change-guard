// Package argorollouts discovers Prometheus dependencies in Argo Rollouts
// AnalysisTemplate and ClusterAnalysisTemplate resources.
package argorollouts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
)

const maxManifestBytes = 8 << 20

// Loader controls whether unresolved deployment-gate evidence is required.
type Loader struct {
	Required bool
}

// LoadFile reads one local Kubernetes manifest containing Argo Rollouts
// analysis templates.
func (loader Loader) LoadFile(ctx context.Context, path string) (domain.Discovery, error) {
	if err := ctx.Err(); err != nil {
		return domain.Discovery{}, fmt.Errorf("load Argo Rollouts manifest %q: %w", path, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("open Argo Rollouts manifest %q: %w", path, err)
	}
	defer file.Close()

	discovery, err := loader.Parse(ctx, filepath.Clean(path), file)
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("load Argo Rollouts manifest %q: %w", path, err)
	}
	return discovery, nil
}

// Parse discovers every Prometheus measurement in supported Argo Rollouts
// analysis templates. Unrelated Kubernetes resources in the same bundle are
// ignored.
func (loader Loader) Parse(ctx context.Context, source string, reader io.Reader) (domain.Discovery, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maxManifestBytes+1))
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("read Argo Rollouts manifest: %w", err)
	}
	if len(contents) > maxManifestBytes {
		return domain.Discovery{}, fmt.Errorf("Argo Rollouts manifest exceeds the %d-byte size limit", maxManifestBytes)
	}

	decoder := yaml.NewDecoder(strings.NewReader(string(contents)))
	var discovery domain.Discovery
	documentIndex := 0
	templateCount := 0
	for {
		if err := ctx.Err(); err != nil {
			return domain.Discovery{}, err
		}
		var document manifest
		if err := decoder.Decode(&document); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return domain.Discovery{}, fmt.Errorf("decode YAML document %d: %w", documentIndex+1, err)
		}
		documentIndex++
		if document.empty() || !supportedKind(document.Kind) {
			continue
		}
		templateCount++
		additional, err := loader.discoverTemplate(source, documentIndex, document)
		if err != nil {
			return domain.Discovery{}, err
		}
		discovery.Append(additional)
	}

	if documentIndex == 0 {
		return domain.Discovery{}, fmt.Errorf("Argo Rollouts manifest is empty")
	}
	if templateCount == 0 {
		return domain.Discovery{}, fmt.Errorf("Argo Rollouts manifest contains no AnalysisTemplate or ClusterAnalysisTemplate resources")
	}
	return discovery, nil
}

func (loader Loader) discoverTemplate(source string, documentIndex int, document manifest) (domain.Discovery, error) {
	prefix := fmt.Sprintf("document %d %s", documentIndex, document.Kind)
	if document.APIVersion != "argoproj.io/v1alpha1" {
		return domain.Discovery{}, fmt.Errorf("%s apiVersion must be argoproj.io/v1alpha1", prefix)
	}
	name := strings.TrimSpace(document.Metadata.Name)
	if name == "" {
		return domain.Discovery{}, fmt.Errorf("%s metadata.name is required", prefix)
	}
	if len(document.Spec.Metrics) == 0 && len(document.Spec.Templates) == 0 {
		return domain.Discovery{}, fmt.Errorf("%s spec must contain at least one metric or template reference", prefix)
	}
	for templateIndex, reference := range document.Spec.Templates {
		if strings.TrimSpace(reference.TemplateName) == "" {
			return domain.Discovery{}, fmt.Errorf("%s spec.templates[%d].templateName is required", prefix, templateIndex)
		}
	}

	namespace := templateNamespace(document.Kind, document.Metadata.Namespace)
	criticality := deploymentGateCriticality(document.Metadata.Labels)
	seenMetrics := make(map[string]int, len(document.Spec.Metrics))
	var discovery domain.Discovery
	for metricIndex, metric := range document.Spec.Metrics {
		metricName := strings.TrimSpace(metric.Name)
		if metricName == "" {
			return domain.Discovery{}, fmt.Errorf("%s spec.metrics[%d].name is required", prefix, metricIndex)
		}
		if previous, exists := seenMetrics[metricName]; exists {
			return domain.Discovery{}, fmt.Errorf(
				"%s spec.metrics[%d].name duplicates spec.metrics[%d]",
				prefix,
				metricIndex,
				previous,
			)
		}
		seenMetrics[metricName] = metricIndex
		if !metric.Provider.present || metric.Provider.entryCount != 1 {
			return domain.Discovery{}, fmt.Errorf("%s spec.metrics[%d].provider must contain exactly one provider", prefix, metricIndex)
		}
		if !metric.Provider.prometheusPresent {
			continue
		}

		query := strings.TrimSpace(metric.Provider.Prometheus.Query)
		location := domain.SourceLocation{File: source, Line: metric.line, Column: metric.column}
		consumer := domain.Consumer{
			ID: fmt.Sprintf(
				"argo_rollouts:%s:%d:%s:%s:%s:prometheus:%d",
				source,
				documentIndex,
				document.Kind,
				namespace,
				name,
				metricIndex,
			),
			Kind:        domain.ConsumerKindDeploymentGate,
			Name:        fmt.Sprintf("%s / %s", name, metricName),
			Source:      location,
			Criticality: criticality,
			Expression:  query,
			Metadata: map[string]string{
				"api_version":   document.APIVersion,
				"metric":        metricName,
				"namespace":     namespace,
				"query_type":    queryType(metric.Provider.Prometheus.RangeQuery),
				"template":      name,
				"template_kind": document.Kind,
			},
		}
		if query == "" {
			consumer.Unresolved = true
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter:  "argo_rollouts",
				Source:   location,
				Message:  fmt.Sprintf("%s spec.metrics[%d].provider.prometheus.query is required", prefix, metricIndex),
				Required: loader.Required,
			})
			discovery.Consumers = append(discovery.Consumers, consumer)
			continue
		}

		analysis, analysisErr := analyzePromQL(query)
		if analysisErr != nil || len(analysis.Unresolved) != 0 {
			consumer.Unresolved = true
			message := "PromQL expression is unresolved"
			if analysisErr != nil {
				message = analysisErr.Error()
			} else {
				message = analysis.Unresolved[0].Reason
			}
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter:  "argo_rollouts",
				Source:   location,
				Message:  message,
				Required: loader.Required,
			})
		} else {
			for _, reference := range analysis.References {
				reference.ConsumerID = consumer.ID
				reference.Evidence.Source = location
				discovery.References = append(discovery.References, reference)
			}
		}
		discovery.Consumers = append(discovery.Consumers, consumer)
	}
	return discovery, nil
}

func supportedKind(kind string) bool {
	return kind == "AnalysisTemplate" || kind == "ClusterAnalysisTemplate"
}

func templateNamespace(kind, namespace string) string {
	if kind == "ClusterAnalysisTemplate" {
		return "cluster"
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return "default"
	}
	return namespace
}

func deploymentGateCriticality(labels map[string]string) domain.Criticality {
	for _, key := range []string{"environment", "env", "app.kubernetes.io/environment"} {
		switch strings.ToLower(strings.TrimSpace(labels[key])) {
		case "prod", "production":
			return domain.CriticalityCritical
		}
	}
	return domain.CriticalityHigh
}

func queryType(rangeQuery *rangeQuery) string {
	if rangeQuery != nil {
		return "range"
	}
	return "instant"
}

type manifest struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   metadata `yaml:"metadata"`
	Spec       spec     `yaml:"spec"`
}

func (document manifest) empty() bool {
	return document.APIVersion == "" && document.Kind == "" && document.Metadata.Name == "" &&
		len(document.Spec.Metrics) == 0 && len(document.Spec.Templates) == 0
}

type metadata struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace"`
	Labels    map[string]string `yaml:"labels"`
}

type spec struct {
	Metrics   []metric            `yaml:"metrics"`
	Templates []templateReference `yaml:"templates"`
}

type templateReference struct {
	TemplateName string `yaml:"templateName"`
}

type metric struct {
	Name     string         `yaml:"name"`
	Provider metricProvider `yaml:"provider"`
	line     int
	column   int
}

func (value *metric) UnmarshalYAML(node *yaml.Node) error {
	type plainMetric metric
	var decoded plainMetric
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*value = metric(decoded)
	value.line = node.Line
	value.column = node.Column
	return nil
}

type metricProvider struct {
	Prometheus        prometheusProvider
	present           bool
	prometheusPresent bool
	entryCount        int
}

func (provider *metricProvider) UnmarshalYAML(node *yaml.Node) error {
	*provider = metricProvider{present: true}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("provider must be a mapping")
	}
	provider.entryCount = len(node.Content) / 2
	for index := 0; index < len(node.Content); index += 2 {
		key := node.Content[index]
		if key.Kind != yaml.ScalarNode || strings.TrimSpace(key.Value) == "" {
			return fmt.Errorf("provider name must be a non-empty string")
		}
		if key.Value != "prometheus" {
			continue
		}
		provider.prometheusPresent = true
		value := node.Content[index+1]
		if value.Kind != yaml.MappingNode {
			return fmt.Errorf("prometheus provider must be a mapping")
		}
		if err := value.Decode(&provider.Prometheus); err != nil {
			return err
		}
	}
	return nil
}

type prometheusProvider struct {
	Query      string      `yaml:"query"`
	RangeQuery *rangeQuery `yaml:"rangeQuery"`
}

type rangeQuery struct {
	Start string `yaml:"start"`
	End   string `yaml:"end"`
	Step  string `yaml:"step"`
}
