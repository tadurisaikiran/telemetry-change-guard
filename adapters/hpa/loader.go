package hpa

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
)

const maxManifestBytes = 8 << 20

// Loader requires an explicit backend mapping for every external/custom HPA
// metric. Kubernetes and Prometheus names are never treated as equivalent.
type Loader struct {
	Required      bool
	Mapping       Mapping
	MappingSource string
}

// LoadFile reads one local Kubernetes manifest containing autoscaling/v2 HPA
// resources.
func (loader Loader) LoadFile(ctx context.Context, path string) (domain.Discovery, error) {
	if err := ctx.Err(); err != nil {
		return domain.Discovery{}, fmt.Errorf("load HPA manifest %q: %w", path, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("open HPA manifest %q: %w", path, err)
	}
	discovery, parseErr := loader.Parse(ctx, filepath.Clean(path), file)
	closeErr := file.Close()
	if parseErr != nil {
		return domain.Discovery{}, fmt.Errorf("load HPA manifest %q: %w", path, parseErr)
	}
	if closeErr != nil {
		return domain.Discovery{}, fmt.Errorf("close HPA manifest %q: %w", path, closeErr)
	}
	return discovery, nil
}

// Parse discovers External, Object, and Pods metric dependencies in every
// HorizontalPodAutoscaler document. Resource and ContainerResource metrics
// are Kubernetes built-ins and do not create Prometheus evidence.
func (loader Loader) Parse(ctx context.Context, source string, reader io.Reader) (domain.Discovery, error) {
	if loader.Mapping.EntryCount() == 0 {
		return domain.Discovery{}, errors.New("an explicit HPA backend mapping is required")
	}
	contents, err := io.ReadAll(io.LimitReader(reader, maxManifestBytes+1))
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("read HPA manifest: %w", err)
	}
	if len(contents) > maxManifestBytes {
		return domain.Discovery{}, fmt.Errorf("HPA manifest exceeds the %d-byte size limit", maxManifestBytes)
	}

	decoder := yaml.NewDecoder(strings.NewReader(string(contents)))
	var discovery domain.Discovery
	documentIndex := 0
	hpaCount := 0
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
		if document.empty() || document.Kind != "HorizontalPodAutoscaler" {
			continue
		}
		hpaCount++
		additional, err := loader.discoverHPA(source, documentIndex, document)
		if err != nil {
			return domain.Discovery{}, err
		}
		discovery.Append(additional)
	}

	if documentIndex == 0 {
		return domain.Discovery{}, errors.New("HPA manifest is empty")
	}
	if hpaCount == 0 {
		return domain.Discovery{}, errors.New("HPA manifest contains no HorizontalPodAutoscaler resources")
	}
	return discovery, nil
}

func (loader Loader) discoverHPA(source string, documentIndex int, document manifest) (domain.Discovery, error) {
	prefix := fmt.Sprintf("document %d HorizontalPodAutoscaler", documentIndex)
	if document.APIVersion != "autoscaling/v2" {
		return domain.Discovery{}, fmt.Errorf("%s apiVersion must be autoscaling/v2", prefix)
	}
	hpaName := strings.TrimSpace(document.Metadata.Name)
	if hpaName == "" {
		return domain.Discovery{}, fmt.Errorf("%s metadata.name is required", prefix)
	}
	target := strings.TrimSpace(document.Spec.ScaleTargetRef.Name)
	if target == "" {
		return domain.Discovery{}, fmt.Errorf("%s spec.scaleTargetRef.name is required", prefix)
	}

	namespace := strings.TrimSpace(document.Metadata.Namespace)
	if namespace == "" {
		namespace = "default"
	}
	criticality := autoscalerCriticality(document.Metadata.Labels)
	var discovery domain.Discovery
	for metricIndex, metric := range document.Spec.Metrics {
		metricType, identifier, err := metric.identifier(prefix, metricIndex)
		if err != nil {
			return domain.Discovery{}, err
		}
		if identifier == nil {
			continue
		}

		kubernetesMetric := strings.TrimSpace(identifier.Name)
		if kubernetesMetric == "" {
			return domain.Discovery{}, fmt.Errorf(
				"%s spec.metrics[%d].%s.metric.name is required",
				prefix,
				metricIndex,
				providerField(metricType),
			)
		}
		if kubernetesMetric != identifier.Name {
			return domain.Discovery{}, fmt.Errorf(
				"%s spec.metrics[%d].%s.metric.name must not have surrounding whitespace",
				prefix,
				metricIndex,
				providerField(metricType),
			)
		}
		selectorLabels, err := identifier.selectorLabels(prefix, metricIndex, metricType)
		if err != nil {
			return domain.Discovery{}, err
		}
		location := domain.SourceLocation{File: source, Line: metric.line, Column: metric.column}
		consumer := newConsumer(
			source,
			documentIndex,
			metricIndex,
			document.APIVersion,
			namespace,
			hpaName,
			target,
			metricType,
			kubernetesMetric,
			criticality,
			location,
		)

		entry, exists := loader.Mapping.lookup(metricType, kubernetesMetric)
		if !exists {
			consumer.Unresolved = true
			discovery.Consumers = append(discovery.Consumers, consumer)
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter: "hpa",
				Source:  location,
				Message: fmt.Sprintf(
					"%s spec.metrics[%d] %s metric %q requires an explicit backend mapping or ignore decision",
					prefix,
					metricIndex,
					metricType,
					kubernetesMetric,
				),
				Required: loader.Required,
			})
			continue
		}
		if entry.Ignore != "" {
			continue
		}

		consumer.Metadata["prometheus_metric"] = entry.Prometheus.Metric
		discovery.Consumers = append(discovery.Consumers, consumer)
		discovery.References = append(discovery.References, mappedMetricReference(
			consumer.ID,
			metricType,
			kubernetesMetric,
			entry.Prometheus.Metric,
			loader.MappingSource,
		))
		for _, kubernetesLabel := range selectorLabels {
			if prometheusLabel, mapped := entry.Prometheus.Labels[kubernetesLabel]; mapped {
				discovery.References = append(discovery.References, mappedLabelReference(
					consumer.ID,
					metricType,
					kubernetesMetric,
					kubernetesLabel,
					entry.Prometheus.Metric,
					prometheusLabel,
					loader.MappingSource,
				))
				continue
			}
			if _, ignored := entry.Prometheus.IgnoredLabels[kubernetesLabel]; ignored {
				continue
			}
			discovery.References = append(discovery.References, unresolvedSelectorReference(
				consumer.ID,
				entry.Prometheus.Metric,
				kubernetesLabel,
				location,
			))
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter: "hpa",
				Source:  location,
				Message: fmt.Sprintf(
					"%s spec.metrics[%d] selector label %q requires an explicit Prometheus label mapping or ignore reason",
					prefix,
					metricIndex,
					kubernetesLabel,
				),
				Required: loader.Required,
			})
		}
	}
	return discovery, nil
}

func newConsumer(
	source string,
	documentIndex, metricIndex int,
	apiVersion, namespace, hpaName, target string,
	metricType MetricType,
	kubernetesMetric string,
	criticality domain.Criticality,
	location domain.SourceLocation,
) domain.Consumer {
	return domain.Consumer{
		ID: fmt.Sprintf(
			"hpa:%s:%d:%s:%s:%s:%d",
			source,
			documentIndex,
			namespace,
			hpaName,
			metricType,
			metricIndex,
		),
		Kind:        domain.ConsumerKindAutoscaler,
		Name:        target,
		Source:      location,
		Criticality: criticality,
		Expression:  kubernetesMetric,
		Metadata: map[string]string{
			"api_version":       apiVersion,
			"hpa":               hpaName,
			"kubernetes_metric": kubernetesMetric,
			"metric_index":      fmt.Sprint(metricIndex),
			"metric_type":       string(metricType),
			"namespace":         namespace,
			"scale_target":      target,
		},
	}
}

func mappedMetricReference(
	consumerID string,
	metricType MetricType,
	kubernetesMetric, prometheusMetric, mappingSource string,
) domain.Reference {
	return domain.Reference{
		ConsumerID: consumerID,
		Symbol: domain.Symbol{
			Domain: domain.DomainPrometheus,
			Kind:   domain.SymbolKindMetric,
			Name:   prometheusMetric,
		},
		Usage: domain.UsageSelector,
		Evidence: domain.Evidence{
			Method:      domain.EvidenceMethodExplicitMapping,
			Confidence:  domain.ConfidenceConfirmed,
			Source:      domain.SourceLocation{File: cleanMappingSource(mappingSource)},
			Expression:  fmt.Sprintf("%s/%s -> %s", metricType, kubernetesMetric, prometheusMetric),
			Explanation: "explicit HPA metrics-adapter mapping",
		},
	}
}

func mappedLabelReference(
	consumerID string,
	metricType MetricType,
	kubernetesMetric, kubernetesLabel, prometheusMetric, prometheusLabel, mappingSource string,
) domain.Reference {
	return domain.Reference{
		ConsumerID: consumerID,
		Symbol: domain.Symbol{
			Domain: domain.DomainPrometheus,
			Kind:   domain.SymbolKindLabel,
			Name:   prometheusLabel,
			Parent: prometheusMetric,
		},
		Usage: domain.UsageFilter,
		Evidence: domain.Evidence{
			Method:     domain.EvidenceMethodExplicitMapping,
			Confidence: domain.ConfidenceConfirmed,
			Source:     domain.SourceLocation{File: cleanMappingSource(mappingSource)},
			Expression: fmt.Sprintf(
				"%s/%s label %s -> %s/%s",
				metricType,
				kubernetesMetric,
				kubernetesLabel,
				prometheusMetric,
				prometheusLabel,
			),
			Explanation: "explicit HPA selector-label mapping",
		},
	}
}

func unresolvedSelectorReference(
	consumerID, prometheusMetric, kubernetesLabel string,
	location domain.SourceLocation,
) domain.Reference {
	return domain.Reference{
		ConsumerID: consumerID,
		Symbol: domain.Symbol{
			Domain: domain.DomainPrometheus,
			Kind:   domain.SymbolKindMetric,
			Name:   prometheusMetric,
		},
		Usage:              domain.UsageUnknown,
		Pattern:            kubernetesLabel,
		RequiresResolution: true,
		ResolutionScope:    domain.ResolutionScopeLabels,
		Evidence: domain.Evidence{
			Method:      domain.EvidenceMethodStaticConfig,
			Confidence:  domain.ConfidenceUnknown,
			Source:      location,
			Explanation: "HPA selector label has no explicit Prometheus mapping",
		},
	}
}

func autoscalerCriticality(labels map[string]string) domain.Criticality {
	for _, key := range []string{"environment", "env", "app.kubernetes.io/environment"} {
		switch strings.ToLower(strings.TrimSpace(labels[key])) {
		case "prod", "production":
			return domain.CriticalityCritical
		}
	}
	return domain.CriticalityHigh
}

type manifest struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   metadata `yaml:"metadata"`
	Spec       spec     `yaml:"spec"`
}

func (document manifest) empty() bool {
	return document.APIVersion == "" && document.Kind == "" && document.Metadata.Name == "" &&
		document.Spec.ScaleTargetRef.Name == "" && len(document.Spec.Metrics) == 0
}

type metadata struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace"`
	Labels    map[string]string `yaml:"labels"`
}

type spec struct {
	ScaleTargetRef scaleTargetRef `yaml:"scaleTargetRef"`
	Metrics        []metricSpec   `yaml:"metrics"`
}

type scaleTargetRef struct {
	Name string `yaml:"name"`
}

type metricSpec struct {
	Type      string
	providers map[string]*yaml.Node
	line      int
	column    int
}

func (metric *metricSpec) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return errors.New("HPA metric must be a mapping")
	}
	*metric = metricSpec{
		providers: make(map[string]*yaml.Node),
		line:      node.Line,
		column:    node.Column,
	}
	seen := make(map[string]struct{})
	for index := 0; index < len(node.Content); index += 2 {
		key := node.Content[index]
		value := node.Content[index+1]
		if key.Kind != yaml.ScalarNode {
			return errors.New("HPA metric field name must be a string")
		}
		switch key.Value {
		case "type":
			if _, exists := seen[key.Value]; exists {
				return fmt.Errorf("HPA metric field %q is duplicated", key.Value)
			}
			seen[key.Value] = struct{}{}
			if err := value.Decode(&metric.Type); err != nil {
				return fmt.Errorf("decode HPA metric type: %w", err)
			}
		case "external", "object", "pods", "resource", "containerResource":
			if _, exists := seen[key.Value]; exists {
				return fmt.Errorf("HPA metric field %q is duplicated", key.Value)
			}
			seen[key.Value] = struct{}{}
			metric.providers[key.Value] = value
		}
	}
	return nil
}

func (metric metricSpec) identifier(prefix string, metricIndex int) (MetricType, *metricIdentifier, error) {
	metricType := MetricType(strings.TrimSpace(metric.Type))
	if string(metricType) != metric.Type {
		return "", nil, fmt.Errorf("%s spec.metrics[%d].type must not have surrounding whitespace", prefix, metricIndex)
	}
	expected := providerField(metricType)
	if expected == "" {
		return "", nil, fmt.Errorf(
			"%s spec.metrics[%d].type must be ContainerResource, External, Object, Pods, or Resource",
			prefix,
			metricIndex,
		)
	}
	if len(metric.providers) != 1 {
		return "", nil, fmt.Errorf(
			"%s spec.metrics[%d] must contain exactly one metric source matching type %s",
			prefix,
			metricIndex,
			metricType,
		)
	}
	provider, exists := metric.providers[expected]
	if !exists {
		return "", nil, fmt.Errorf(
			"%s spec.metrics[%d].%s is required and must match type %s",
			prefix,
			metricIndex,
			expected,
			metricType,
		)
	}
	if provider.Kind != yaml.MappingNode {
		return "", nil, fmt.Errorf("%s spec.metrics[%d].%s must be a mapping", prefix, metricIndex, expected)
	}
	if metricType == "Resource" || metricType == "ContainerResource" {
		return metricType, nil, nil
	}
	var source metricSource
	if err := provider.Decode(&source); err != nil {
		return "", nil, fmt.Errorf("%s spec.metrics[%d].%s: %w", prefix, metricIndex, expected, err)
	}
	return metricType, &source.Metric, nil
}

func providerField(metricType MetricType) string {
	switch metricType {
	case MetricTypeExternal:
		return "external"
	case MetricTypeObject:
		return "object"
	case MetricTypePods:
		return "pods"
	case "Resource":
		return "resource"
	case "ContainerResource":
		return "containerResource"
	default:
		return ""
	}
}

type metricSource struct {
	Metric metricIdentifier `yaml:"metric"`
}

type metricIdentifier struct {
	Name     string         `yaml:"name"`
	Selector *labelSelector `yaml:"selector"`
}

func (identifier metricIdentifier) selectorLabels(
	prefix string,
	metricIndex int,
	metricType MetricType,
) ([]string, error) {
	if identifier.Selector == nil {
		return nil, nil
	}
	labels := make(map[string]struct{}, len(identifier.Selector.MatchLabels)+len(identifier.Selector.MatchExpressions))
	for label := range identifier.Selector.MatchLabels {
		normalized := strings.TrimSpace(label)
		if normalized == "" {
			return nil, fmt.Errorf(
				"%s spec.metrics[%d].%s.metric.selector.matchLabels contains an empty label name",
				prefix,
				metricIndex,
				providerField(metricType),
			)
		}
		if label != normalized {
			return nil, fmt.Errorf(
				"%s spec.metrics[%d].%s.metric.selector.matchLabels label %q must not have surrounding whitespace",
				prefix,
				metricIndex,
				providerField(metricType),
				label,
			)
		}
		labels[label] = struct{}{}
	}
	for expressionIndex, expression := range identifier.Selector.MatchExpressions {
		label := strings.TrimSpace(expression.Key)
		path := fmt.Sprintf(
			"%s spec.metrics[%d].%s.metric.selector.matchExpressions[%d]",
			prefix,
			metricIndex,
			providerField(metricType),
			expressionIndex,
		)
		if label == "" {
			return nil, fmt.Errorf("%s.key is required", path)
		}
		if label != expression.Key {
			return nil, fmt.Errorf("%s.key must not have surrounding whitespace", path)
		}
		switch expression.Operator {
		case "In", "NotIn":
			if len(expression.Values) == 0 {
				return nil, fmt.Errorf("%s.values must not be empty for operator %s", path, expression.Operator)
			}
		case "Exists", "DoesNotExist":
			if len(expression.Values) != 0 {
				return nil, fmt.Errorf("%s.values must be empty for operator %s", path, expression.Operator)
			}
		default:
			return nil, fmt.Errorf("%s.operator must be In, NotIn, Exists, or DoesNotExist", path)
		}
		labels[label] = struct{}{}
	}
	result := make([]string, 0, len(labels))
	for label := range labels {
		result = append(result, label)
	}
	sort.Strings(result)
	return result, nil
}

type labelSelector struct {
	MatchLabels      map[string]string          `yaml:"matchLabels"`
	MatchExpressions []labelSelectorRequirement `yaml:"matchExpressions"`
}

type labelSelectorRequirement struct {
	Key      string   `yaml:"key"`
	Operator string   `yaml:"operator"`
	Values   []string `yaml:"values"`
}
