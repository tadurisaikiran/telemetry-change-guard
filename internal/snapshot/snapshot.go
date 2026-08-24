// Package snapshot owns the deterministic telemetry-contract snapshot format
// and baseline/candidate comparison. Collection from external systems remains
// in adapters; this package performs no network access.
package snapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
)

const (
	APIVersion         = "tcg/v1alpha1"
	Kind               = "TelemetrySnapshot"
	DiffSchemaVersion  = "tcg-snapshot-diff/v1alpha1"
	maxSnapshotBytes   = 16 << 20
	maxMetrics         = 100_000
	maxLabelsPerMetric = 256
	maxNameBytes       = 1024
)

// Metadata identifies a snapshot without embedding collection time. Omitting
// generated timestamps keeps identical telemetry contracts byte-stable.
type Metadata struct {
	Name string `json:"name"`
}

// Metric is the normalized observable contract for one metric family. Labels
// are names only; values are deliberately excluded to avoid secrets and high
// cardinality data in contract artifacts.
type Metric struct {
	Name   string   `json:"name"`
	Type   string   `json:"type"`
	Unit   string   `json:"unit,omitempty"`
	Labels []string `json:"labels"`
}

// Spec contains the domain-specific contract. Domain identity remains
// explicit even though the initial collector supports Prometheus only.
type Spec struct {
	Domain  domain.Domain `json:"domain"`
	Metrics []Metric      `json:"metrics"`
}

// Snapshot is the versioned, deterministic telemetry-contract artifact.
type Snapshot struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
}

// DifferenceKind classifies one observed baseline/candidate delta without
// claiming unsupported changes are safe.
type DifferenceKind string

const (
	DifferenceMetricAdded         DifferenceKind = "metric_added"
	DifferenceMetricRemoved       DifferenceKind = "metric_removed"
	DifferenceLabelAdded          DifferenceKind = "label_added"
	DifferenceLabelRemoved        DifferenceKind = "label_removed"
	DifferenceMetricTypeChanged   DifferenceKind = "metric_type_changed"
	DifferenceMetricUnitChanged   DifferenceKind = "metric_unit_changed"
	DifferenceMetadataUnavailable DifferenceKind = "metric_metadata_unavailable"
)

// Difference is one stable snapshot delta. Before and After carry type or unit
// values for semantic changes; Label is set only for label changes.
type Difference struct {
	Kind   DifferenceKind `json:"kind"`
	Metric string         `json:"metric"`
	Label  string         `json:"label,omitempty"`
	Field  string         `json:"field,omitempty"`
	Before string         `json:"before,omitempty"`
	After  string         `json:"after,omitempty"`
}

// Diff is the inspectable full delta plus its actionable ChangeSet. Additions
// remain visible in Differences but are not treated as breaking changes.
type Diff struct {
	SchemaVersion string              `json:"schemaVersion"`
	Baseline      string              `json:"baseline"`
	Candidate     string              `json:"candidate"`
	Differences   []Difference        `json:"differences"`
	ChangeSet     domain.ChangeSet    `json:"changeSet"`
	Diagnostics   []domain.Diagnostic `json:"diagnostics,omitempty"`
}

// Load reads one bounded snapshot file.
func Load(ctx context.Context, path string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("load telemetry snapshot %q: %w", path, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("open telemetry snapshot %q: %w", path, err)
	}
	result, parseErr := Parse(file)
	closeErr := file.Close()
	if parseErr != nil {
		return Snapshot{}, fmt.Errorf("load telemetry snapshot %q: %w", path, parseErr)
	}
	if closeErr != nil {
		return Snapshot{}, fmt.Errorf("close telemetry snapshot %q: %w", path, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("load telemetry snapshot %q: %w", path, err)
	}
	return result, nil
}

// Parse strictly decodes, validates, and normalizes one snapshot.
func Parse(reader io.Reader) (Snapshot, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maxSnapshotBytes+1))
	if err != nil {
		return Snapshot{}, fmt.Errorf("read telemetry snapshot: %w", err)
	}
	if len(contents) > maxSnapshotBytes {
		return Snapshot{}, fmt.Errorf("telemetry snapshot exceeds the %d-byte size limit", maxSnapshotBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var result Snapshot
	if err := decoder.Decode(&result); err != nil {
		if errors.Is(err, io.EOF) {
			return Snapshot{}, errors.New("telemetry snapshot is empty")
		}
		return Snapshot{}, fmt.Errorf("decode telemetry snapshot: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return Snapshot{}, errors.New("telemetry snapshot must contain exactly one JSON value")
	} else if !errors.Is(err, io.EOF) {
		return Snapshot{}, fmt.Errorf("decode trailing telemetry snapshot data: %w", err)
	}
	if err := Normalize(&result); err != nil {
		return Snapshot{}, err
	}
	return result, nil
}

// Normalize validates an in-memory snapshot and establishes stable ordering.
func Normalize(target *Snapshot) error {
	if target == nil {
		return errors.New("telemetry snapshot is required")
	}
	var issues []string
	if target.APIVersion != APIVersion {
		issues = append(issues, fmt.Sprintf("apiVersion must be %q", APIVersion))
	}
	if target.Kind != Kind {
		issues = append(issues, fmt.Sprintf("kind must be %q", Kind))
	}
	if strings.TrimSpace(target.Metadata.Name) == "" {
		issues = append(issues, "metadata.name is required")
	} else if len(target.Metadata.Name) > maxNameBytes {
		issues = append(issues, fmt.Sprintf("metadata.name exceeds %d bytes", maxNameBytes))
	}
	if target.Spec.Domain != domain.DomainPrometheus {
		issues = append(issues, fmt.Sprintf("spec.domain must be %q", domain.DomainPrometheus))
	}
	if len(target.Spec.Metrics) > maxMetrics {
		issues = append(issues, fmt.Sprintf("spec.metrics contains more than %d metrics", maxMetrics))
	}

	seenMetrics := make(map[string]int, len(target.Spec.Metrics))
	for index := range target.Spec.Metrics {
		metric := &target.Spec.Metrics[index]
		path := fmt.Sprintf("spec.metrics[%d]", index)
		if strings.TrimSpace(metric.Name) == "" {
			issues = append(issues, path+".name is required")
		} else if len(metric.Name) > maxNameBytes {
			issues = append(issues, fmt.Sprintf("%s.name exceeds %d bytes", path, maxNameBytes))
		} else if previous, exists := seenMetrics[metric.Name]; exists {
			issues = append(issues, fmt.Sprintf("%s.name duplicates spec.metrics[%d].name", path, previous))
		} else {
			seenMetrics[metric.Name] = index
		}
		if !supportedMetricType(metric.Type) {
			issues = append(issues, fmt.Sprintf("%s.type %q is unsupported", path, metric.Type))
		}
		if len(metric.Unit) > maxNameBytes {
			issues = append(issues, fmt.Sprintf("%s.unit exceeds %d bytes", path, maxNameBytes))
		}
		if len(metric.Labels) > maxLabelsPerMetric {
			issues = append(issues, fmt.Sprintf("%s.labels contains more than %d labels", path, maxLabelsPerMetric))
		}
		seenLabels := make(map[string]int, len(metric.Labels))
		for labelIndex, label := range metric.Labels {
			labelPath := fmt.Sprintf("%s.labels[%d]", path, labelIndex)
			if strings.TrimSpace(label) == "" {
				issues = append(issues, labelPath+" is required")
			} else if label == "__name__" {
				issues = append(issues, labelPath+" must not contain the metric-name label")
			} else if len(label) > maxNameBytes {
				issues = append(issues, fmt.Sprintf("%s exceeds %d bytes", labelPath, maxNameBytes))
			} else if previous, exists := seenLabels[label]; exists {
				issues = append(issues, fmt.Sprintf("%s duplicates %s.labels[%d]", labelPath, path, previous))
			} else {
				seenLabels[label] = labelIndex
			}
		}
		sort.Strings(metric.Labels)
		if metric.Labels == nil {
			metric.Labels = []string{}
		}
	}
	sort.Slice(target.Spec.Metrics, func(i, j int) bool {
		return target.Spec.Metrics[i].Name < target.Spec.Metrics[j].Name
	})
	if target.Spec.Metrics == nil {
		target.Spec.Metrics = []Metric{}
	}
	if len(issues) != 0 {
		return fmt.Errorf("telemetry snapshot is invalid:\n  - %s", strings.Join(issues, "\n  - "))
	}
	return nil
}

// Marshal emits byte-stable, indented JSON after validating a defensive copy.
func Marshal(value Snapshot) ([]byte, error) {
	copyValue := cloneSnapshot(value)
	if err := Normalize(&copyValue); err != nil {
		return nil, err
	}
	contents, err := json.MarshalIndent(copyValue, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode telemetry snapshot: %w", err)
	}
	return append(contents, '\n'), nil
}

func supportedMetricType(value string) bool {
	switch value {
	case "counter", "gauge", "histogram", "gaugehistogram", "summary", "info", "stateset", "unknown", "untyped":
		return true
	default:
		return false
	}
}

func cloneSnapshot(source Snapshot) Snapshot {
	result := source
	result.Spec.Metrics = make([]Metric, len(source.Spec.Metrics))
	for index, metric := range source.Spec.Metrics {
		result.Spec.Metrics[index] = metric
		result.Spec.Metrics[index].Labels = append([]string(nil), metric.Labels...)
	}
	return result
}
