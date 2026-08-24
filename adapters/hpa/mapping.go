// Package hpa discovers Prometheus dependencies in Kubernetes
// autoscaling/v2 HorizontalPodAutoscaler resources without inferring backend
// identity from Kubernetes metric names.
package hpa

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// MappingAPIVersion is the schema version for explicit HPA backend mappings.
	MappingAPIVersion = "tcg.hpa/v1alpha1"
	// MappingKind is the required mapping document kind.
	MappingKind     = "HPAMetricMappings"
	maxMappingBytes = 1 << 20
)

// MetricType is one Kubernetes metric-source type whose backend may be
// provided by a metrics adapter.
type MetricType string

const (
	MetricTypeExternal MetricType = "External"
	MetricTypeObject   MetricType = "Object"
	MetricTypePods     MetricType = "Pods"
)

// Mapping is an immutable exact lookup from Kubernetes metric identity to a
// documented Prometheus identity or non-Prometheus decision.
type Mapping struct {
	entries map[string]MappingEntry
}

// MappingEntry resolves one exact Kubernetes metric identifier.
type MappingEntry struct {
	Kubernetes KubernetesMetric
	Prometheus *PrometheusMetric
	Ignore     string
}

// KubernetesMetric identifies one external/custom metric exposed through the
// Kubernetes metrics APIs.
type KubernetesMetric struct {
	Type   MetricType
	Metric string
}

// PrometheusMetric describes the exact backend metric and selector-label
// translations established by an operator-supplied adapter mapping.
type PrometheusMetric struct {
	Metric        string
	Labels        map[string]string
	IgnoredLabels map[string]string
}

type mappingDocument struct {
	APIVersion string              `yaml:"apiVersion"`
	Kind       string              `yaml:"kind"`
	Spec       mappingSpecDocument `yaml:"spec"`
}

type mappingSpecDocument struct {
	Mappings []mappingEntryDocument `yaml:"mappings"`
}

type mappingEntryDocument struct {
	Kubernetes kubernetesMetricDocument  `yaml:"kubernetes"`
	Prometheus *prometheusMetricDocument `yaml:"prometheus"`
	Ignore     string                    `yaml:"ignore"`
}

type kubernetesMetricDocument struct {
	Type   MetricType `yaml:"type"`
	Metric string     `yaml:"metric"`
}

type prometheusMetricDocument struct {
	Metric        string            `yaml:"metric"`
	Labels        map[string]string `yaml:"labels"`
	IgnoredLabels map[string]string `yaml:"ignoredLabels"`
}

// LoadMapping reads and validates one bounded local mapping document.
func LoadMapping(ctx context.Context, path string) (Mapping, error) {
	if err := ctx.Err(); err != nil {
		return Mapping{}, fmt.Errorf("load HPA mapping %q: %w", path, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return Mapping{}, fmt.Errorf("open HPA mapping %q: %w", path, err)
	}
	mapping, parseErr := ParseMapping(file)
	closeErr := file.Close()
	if parseErr != nil {
		return Mapping{}, fmt.Errorf("load HPA mapping %q: %w", path, parseErr)
	}
	if closeErr != nil {
		return Mapping{}, fmt.Errorf("close HPA mapping %q: %w", path, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return Mapping{}, fmt.Errorf("load HPA mapping %q: %w", path, err)
	}
	return mapping, nil
}

// ParseMapping strictly decodes an explicit Kubernetes-to-Prometheus mapping.
func ParseMapping(reader io.Reader) (Mapping, error) {
	contents, err := readBounded(reader, maxMappingBytes, "HPA mapping")
	if err != nil {
		return Mapping{}, err
	}

	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	var document mappingDocument
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return Mapping{}, errors.New("HPA mapping is empty")
		}
		return Mapping{}, fmt.Errorf("decode HPA mapping: %w", err)
	}
	var additional any
	if err := decoder.Decode(&additional); err == nil {
		return Mapping{}, errors.New("HPA mapping must contain exactly one YAML document")
	} else if !errors.Is(err, io.EOF) {
		return Mapping{}, fmt.Errorf("decode trailing HPA mapping document: %w", err)
	}

	entries := make([]MappingEntry, 0, len(document.Spec.Mappings))
	for _, entry := range document.Spec.Mappings {
		converted := MappingEntry{
			Kubernetes: KubernetesMetric{
				Type:   entry.Kubernetes.Type,
				Metric: entry.Kubernetes.Metric,
			},
			Ignore: strings.TrimSpace(entry.Ignore),
		}
		if entry.Prometheus != nil {
			converted.Prometheus = &PrometheusMetric{
				Metric:        entry.Prometheus.Metric,
				Labels:        normalizeStringMap(entry.Prometheus.Labels),
				IgnoredLabels: normalizeStringMap(entry.Prometheus.IgnoredLabels),
			}
		}
		entries = append(entries, converted)
	}
	if err := validateMapping(document.APIVersion, document.Kind, entries); err != nil {
		return Mapping{}, err
	}

	result := Mapping{entries: make(map[string]MappingEntry, len(entries))}
	for _, entry := range entries {
		result.entries[mappingKey(entry.Kubernetes.Type, entry.Kubernetes.Metric)] = entry
	}
	return result, nil
}

// EntryCount returns the number of explicit decisions in the mapping.
func (mapping Mapping) EntryCount() int {
	return len(mapping.entries)
}

func (mapping Mapping) lookup(metricType MetricType, metric string) (MappingEntry, bool) {
	entry, exists := mapping.entries[mappingKey(metricType, metric)]
	return entry, exists
}

func validateMapping(apiVersion, kind string, entries []MappingEntry) error {
	var issues []string
	add := func(path, message string) {
		issues = append(issues, path+": "+message)
	}
	if apiVersion != MappingAPIVersion {
		add("apiVersion", fmt.Sprintf("must be %q", MappingAPIVersion))
	}
	if kind != MappingKind {
		add("kind", fmt.Sprintf("must be %q", MappingKind))
	}
	if len(entries) == 0 {
		add("spec.mappings", "must contain at least one mapping")
	}

	seen := make(map[string]int, len(entries))
	for index, entry := range entries {
		path := fmt.Sprintf("spec.mappings[%d]", index)
		switch entry.Kubernetes.Type {
		case MetricTypeExternal, MetricTypeObject, MetricTypePods:
		default:
			add(path+".kubernetes.type", "must be External, Object, or Pods")
		}
		if strings.TrimSpace(entry.Kubernetes.Metric) == "" {
			add(path+".kubernetes.metric", "is required")
		} else if entry.Kubernetes.Metric != strings.TrimSpace(entry.Kubernetes.Metric) {
			add(path+".kubernetes.metric", "must not have surrounding whitespace")
		}
		key := mappingKey(entry.Kubernetes.Type, entry.Kubernetes.Metric)
		if previous, exists := seen[key]; exists {
			add(path+".kubernetes", fmt.Sprintf("duplicates spec.mappings[%d].kubernetes", previous))
		} else {
			seen[key] = index
		}

		hasPrometheus := entry.Prometheus != nil
		hasIgnore := entry.Ignore != ""
		if hasPrometheus == hasIgnore {
			add(path, "must set exactly one of prometheus or ignore")
		}
		if !hasPrometheus {
			continue
		}
		if strings.TrimSpace(entry.Prometheus.Metric) == "" {
			add(path+".prometheus.metric", "is required")
		} else if entry.Prometheus.Metric != strings.TrimSpace(entry.Prometheus.Metric) {
			add(path+".prometheus.metric", "must not have surrounding whitespace")
		}

		labelNames := make([]string, 0, len(entry.Prometheus.Labels))
		for kubernetesLabel := range entry.Prometheus.Labels {
			labelNames = append(labelNames, kubernetesLabel)
		}
		sort.Strings(labelNames)
		seenPrometheusLabels := make(map[string]string, len(labelNames))
		for _, kubernetesLabel := range labelNames {
			prometheusLabel := entry.Prometheus.Labels[kubernetesLabel]
			labelPath := path + ".prometheus.labels[" + kubernetesLabel + "]"
			if strings.TrimSpace(kubernetesLabel) == "" {
				add(path+".prometheus.labels", "Kubernetes label names must be non-empty")
			} else if kubernetesLabel != strings.TrimSpace(kubernetesLabel) {
				add(labelPath, "Kubernetes label name must not have surrounding whitespace")
			}
			if strings.TrimSpace(prometheusLabel) == "" {
				add(labelPath, "Prometheus label name must be non-empty")
			} else if prometheusLabel != strings.TrimSpace(prometheusLabel) {
				add(labelPath, "Prometheus label name must not have surrounding whitespace")
			} else if prometheusLabel == "__name__" {
				add(labelPath, "must not map a selector label to Prometheus metric identity __name__")
			} else if previous, exists := seenPrometheusLabels[prometheusLabel]; exists {
				add(labelPath, fmt.Sprintf("maps to the same Prometheus label as %q", previous))
			} else {
				seenPrometheusLabels[prometheusLabel] = kubernetesLabel
			}
			if _, ignored := entry.Prometheus.IgnoredLabels[kubernetesLabel]; ignored {
				add(labelPath, "must not also appear in ignoredLabels")
			}
		}

		ignoredNames := make([]string, 0, len(entry.Prometheus.IgnoredLabels))
		for kubernetesLabel := range entry.Prometheus.IgnoredLabels {
			ignoredNames = append(ignoredNames, kubernetesLabel)
		}
		sort.Strings(ignoredNames)
		for _, kubernetesLabel := range ignoredNames {
			ignoredPath := path + ".prometheus.ignoredLabels[" + kubernetesLabel + "]"
			if strings.TrimSpace(kubernetesLabel) == "" {
				add(path+".prometheus.ignoredLabels", "Kubernetes label names must be non-empty")
			} else if kubernetesLabel != strings.TrimSpace(kubernetesLabel) {
				add(ignoredPath, "Kubernetes label name must not have surrounding whitespace")
			}
			if strings.TrimSpace(entry.Prometheus.IgnoredLabels[kubernetesLabel]) == "" {
				add(ignoredPath, "reason must be non-empty")
			}
		}
	}

	if len(issues) != 0 {
		return fmt.Errorf("HPA mapping is invalid:\n  - %s", strings.Join(issues, "\n  - "))
	}
	return nil
}

func normalizeStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	normalized := make(map[string]string, len(values))
	for key, value := range values {
		normalized[key] = value
	}
	return normalized
}

func mappingKey(metricType MetricType, metric string) string {
	return string(metricType) + "\x00" + metric
}

func readBounded(reader io.Reader, maximum int64, description string) ([]byte, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", description, err)
	}
	if int64(len(contents)) > maximum {
		return nil, fmt.Errorf("%s exceeds the %d-byte size limit", description, maximum)
	}
	return contents, nil
}

func cleanMappingSource(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return filepath.Clean(path)
}
