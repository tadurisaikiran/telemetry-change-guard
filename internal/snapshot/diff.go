package snapshot

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/config"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
)

// CompareFiles loads and compares two local snapshots while attaching file
// provenance to generated changes and diagnostics.
func CompareFiles(ctx context.Context, baselinePath, candidatePath, changeSetName string) (Diff, error) {
	baseline, err := Load(ctx, baselinePath)
	if err != nil {
		return Diff{}, err
	}
	candidate, err := Load(ctx, candidatePath)
	if err != nil {
		return Diff{}, err
	}
	result, err := Compare(baseline, candidate, changeSetName)
	if err != nil {
		return Diff{}, err
	}
	if err := ctx.Err(); err != nil {
		return Diff{}, fmt.Errorf("compare telemetry snapshots: %w", err)
	}
	for index := range result.ChangeSet.Changes {
		metadata := result.ChangeSet.Changes[index].Metadata
		metadata["source.baseline.file"] = baselinePath
		metadata["source.candidate.file"] = candidatePath
	}
	for index := range result.Diagnostics {
		result.Diagnostics[index].Source = domain.SourceLocation{File: candidatePath}
	}
	if err := config.ValidateChangeSet(result.ChangeSet); err != nil {
		return Diff{}, fmt.Errorf("validate snapshot ChangeSet with file provenance: %w", err)
	}
	return result, nil
}

// Compare produces a stable full delta and converts supported removals into a
// native ChangeSet. Semantic changes that the current domain model cannot
// represent remain required diagnostics, so safety evaluation fails closed.
func Compare(baseline, candidate Snapshot, changeSetName string) (Diff, error) {
	baseline = cloneSnapshot(baseline)
	candidate = cloneSnapshot(candidate)
	if err := Normalize(&baseline); err != nil {
		return Diff{}, fmt.Errorf("validate baseline snapshot: %w", err)
	}
	if err := Normalize(&candidate); err != nil {
		return Diff{}, fmt.Errorf("validate candidate snapshot: %w", err)
	}
	if baseline.Spec.Domain != candidate.Spec.Domain {
		return Diff{}, fmt.Errorf("snapshot domains differ: baseline %q, candidate %q", baseline.Spec.Domain, candidate.Spec.Domain)
	}
	if changeSetName == "" {
		changeSetName = baseline.Metadata.Name + "-to-" + candidate.Metadata.Name
	}

	baselineMetrics := metricIndex(baseline.Spec.Metrics)
	candidateMetrics := metricIndex(candidate.Spec.Metrics)
	metricNames := unionKeys(baselineMetrics, candidateMetrics)
	var differences []Difference
	for _, name := range metricNames {
		before, hadBefore := baselineMetrics[name]
		after, hasAfter := candidateMetrics[name]
		switch {
		case !hadBefore:
			differences = append(differences, Difference{Kind: DifferenceMetricAdded, Metric: name})
		case !hasAfter:
			differences = append(differences, Difference{Kind: DifferenceMetricRemoved, Metric: name})
		default:
			differences = append(differences, compareMetric(before, after)...)
		}
	}
	sortDifferences(differences)

	changeSet := domain.ChangeSet{
		APIVersion: domain.ChangeSetAPIVersion,
		Kind:       domain.ChangeSetKind,
		Metadata:   domain.ChangeSetMetadata{Name: changeSetName},
		Description: fmt.Sprintf(
			"Detected from telemetry snapshots %s to %s.",
			baseline.Metadata.Name,
			candidate.Metadata.Name,
		),
		Changes: []domain.Change{},
	}
	var diagnostics []domain.Diagnostic
	for _, difference := range differences {
		switch difference.Kind {
		case DifferenceMetricRemoved:
			changeSet.Changes = append(changeSet.Changes, removalChange(
				domain.ChangeKindMetricRemove,
				difference.Metric,
				"",
				baseline.Metadata.Name,
				candidate.Metadata.Name,
			))
		case DifferenceLabelRemoved:
			changeSet.Changes = append(changeSet.Changes, removalChange(
				domain.ChangeKindLabelRemove,
				difference.Label,
				difference.Metric,
				baseline.Metadata.Name,
				candidate.Metadata.Name,
			))
		case DifferenceMetricTypeChanged, DifferenceMetricUnitChanged, DifferenceMetadataUnavailable:
			diagnostics = append(diagnostics, unsupportedSemanticDiagnostic(difference))
		}
	}
	if err := config.ValidateChangeSet(changeSet); err != nil {
		return Diff{}, fmt.Errorf("validate snapshot ChangeSet: %w", err)
	}
	return Diff{
		SchemaVersion: DiffSchemaVersion,
		Baseline:      baseline.Metadata.Name,
		Candidate:     candidate.Metadata.Name,
		Differences:   differences,
		ChangeSet:     changeSet,
		Diagnostics:   diagnostics,
	}, nil
}

// MarshalDiff serializes the versioned, inspectable comparison report.
func MarshalDiff(result Diff) ([]byte, error) {
	contents, err := jsonMarshalIndent(result)
	if err != nil {
		return nil, fmt.Errorf("encode telemetry snapshot diff: %w", err)
	}
	return contents, nil
}

func compareMetric(before, after Metric) []Difference {
	var result []Difference
	if knownType(before.Type) && knownType(after.Type) && before.Type != after.Type {
		result = append(result, Difference{
			Kind: DifferenceMetricTypeChanged, Metric: before.Name,
			Field: "type", Before: before.Type, After: after.Type,
		})
	} else if knownType(before.Type) != knownType(after.Type) {
		result = append(result, Difference{
			Kind: DifferenceMetadataUnavailable, Metric: before.Name,
			Field: "type", Before: before.Type, After: after.Type,
		})
	}
	if before.Unit != "" && after.Unit != "" && before.Unit != after.Unit {
		result = append(result, Difference{
			Kind: DifferenceMetricUnitChanged, Metric: before.Name,
			Field: "unit", Before: before.Unit, After: after.Unit,
		})
	} else if (before.Unit != "") != (after.Unit != "") {
		result = append(result, Difference{
			Kind: DifferenceMetadataUnavailable, Metric: before.Name,
			Field: "unit", Before: before.Unit, After: after.Unit,
		})
	}

	beforeLabels := stringSet(before.Labels)
	afterLabels := stringSet(after.Labels)
	for _, label := range unionKeys(beforeLabels, afterLabels) {
		_, inBefore := beforeLabels[label]
		_, inAfter := afterLabels[label]
		if !inBefore {
			result = append(result, Difference{Kind: DifferenceLabelAdded, Metric: before.Name, Label: label})
		} else if !inAfter {
			result = append(result, Difference{Kind: DifferenceLabelRemoved, Metric: before.Name, Label: label})
		}
	}
	return result
}

func removalChange(
	kind domain.ChangeKind,
	name string,
	parent string,
	baseline string,
	candidate string,
) domain.Change {
	symbolKind := domain.SymbolKindMetric
	if kind == domain.ChangeKindLabelRemove {
		symbolKind = domain.SymbolKindLabel
	}
	key := string(kind) + "\x00" + parent + "\x00" + name
	digest := sha256.Sum256([]byte(key))
	return domain.Change{
		ID:     fmt.Sprintf("snapshot-%s-%x", kind, digest),
		Kind:   kind,
		Domain: domain.DomainPrometheus,
		From: domain.Symbol{
			Domain: domain.DomainPrometheus,
			Kind:   symbolKind,
			Name:   name,
			Parent: parent,
		},
		Metadata: map[string]string{
			"source.adapter":   "telemetry_snapshot",
			"source.baseline":  baseline,
			"source.candidate": candidate,
		},
	}
}

func unsupportedSemanticDiagnostic(difference Difference) domain.Diagnostic {
	message := fmt.Sprintf(
		"metric %q changed %s from %q to %q; this semantic change is not yet representable in tcg/v1alpha1",
		difference.Metric,
		difference.Field,
		difference.Before,
		difference.After,
	)
	if difference.Kind == DifferenceMetadataUnavailable {
		message = fmt.Sprintf(
			"metric %q has incomparable %s metadata (baseline %q, candidate %q); semantic compatibility cannot be proven",
			difference.Metric,
			difference.Field,
			difference.Before,
			difference.After,
		)
	}
	return domain.Diagnostic{Adapter: "telemetry_snapshot", Message: message, Required: true}
}

func metricIndex(metrics []Metric) map[string]Metric {
	result := make(map[string]Metric, len(metrics))
	for _, metric := range metrics {
		result[metric.Name] = metric
	}
	return result
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func unionKeys[V any](left, right map[string]V) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		seen[key] = struct{}{}
	}
	for key := range right {
		seen[key] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for key := range seen {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func knownType(value string) bool {
	return value != "" && value != "unknown"
}

func sortDifferences(differences []Difference) {
	rank := map[DifferenceKind]int{
		DifferenceMetricRemoved:       0,
		DifferenceLabelRemoved:        1,
		DifferenceMetricTypeChanged:   2,
		DifferenceMetricUnitChanged:   3,
		DifferenceMetadataUnavailable: 4,
		DifferenceMetricAdded:         5,
		DifferenceLabelAdded:          6,
	}
	sort.Slice(differences, func(i, j int) bool {
		left, right := differences[i], differences[j]
		if left.Metric != right.Metric {
			return left.Metric < right.Metric
		}
		if rank[left.Kind] != rank[right.Kind] {
			return rank[left.Kind] < rank[right.Kind]
		}
		if left.Label != right.Label {
			return left.Label < right.Label
		}
		return left.Field < right.Field
	})
}
