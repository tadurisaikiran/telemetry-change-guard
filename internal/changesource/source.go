// Package changesource normalizes supported deterministic change inputs into
// the canonical ChangeSet consumed by impact analysis.
package changesource

import (
	"context"
	"errors"
	"fmt"

	"github.com/tadurisaikiran/telemetry-change-guard/adapters/weaver"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/config"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/snapshot"
)

// ChangeSource detects a ChangeSet and any deterministic uncertainty attached
// to the source. Runtime and malformed-input failures remain errors so callers
// can distinguish ERROR from INCOMPLETE.
type ChangeSource interface {
	Detect(context.Context) (domain.ChangeSet, []domain.Diagnostic, error)
}

// Explicit loads the native tcg/v1alpha1 ChangeSet manifest.
type Explicit struct {
	Path string
}

// Detect implements ChangeSource.
func (source Explicit) Detect(ctx context.Context) (domain.ChangeSet, []domain.Diagnostic, error) {
	changeSet, err := config.LoadChangeSet(ctx, source.Path)
	if err != nil {
		return domain.ChangeSet{}, nil, err
	}
	return changeSet, nil, nil
}

// Weaver imports a structured registry diff through explicit backend mapping
// and ignore decisions while retaining unmapped changes as uncertainty.
type Weaver struct {
	DiffPath    string
	MappingPath string
}

// Detect implements ChangeSource.
func (source Weaver) Detect(ctx context.Context) (domain.ChangeSet, []domain.Diagnostic, error) {
	result, err := weaver.LoadImportResult(ctx, source.DiffPath, source.MappingPath)
	if err != nil {
		var unsupported *weaver.UnsupportedChangeError
		if errors.As(err, &unsupported) {
			return source.unsupportedChangeSet(ctx, unsupported)
		}
		return domain.ChangeSet{}, nil, err
	}
	changeSet, diagnostics, err := result.ChangeSet()
	if err != nil {
		return domain.ChangeSet{}, diagnostics, fmt.Errorf("normalize Weaver ChangeSet: %w", err)
	}
	for index := range diagnostics {
		diagnostics[index].Source = domain.SourceLocation{File: source.DiffPath}
	}
	return changeSet, diagnostics, nil
}

func (source Weaver) unsupportedChangeSet(
	ctx context.Context,
	unsupported *weaver.UnsupportedChangeError,
) (domain.ChangeSet, []domain.Diagnostic, error) {
	mapping, err := weaver.LoadMapping(ctx, source.MappingPath)
	if err != nil {
		return domain.ChangeSet{}, nil, err
	}
	changeSet := domain.ChangeSet{
		APIVersion:  domain.ChangeSetAPIVersion,
		Kind:        domain.ChangeSetKind,
		Metadata:    domain.ChangeSetMetadata{Name: mapping.Name},
		Description: "Weaver import contains a change without deterministic field-level mapping information.",
		Changes:     []domain.Change{},
	}
	if err := config.ValidateChangeSet(changeSet); err != nil {
		return domain.ChangeSet{}, nil, fmt.Errorf("validate incomplete Weaver ChangeSet: %w", err)
	}
	diagnostic := domain.Diagnostic{
		Adapter:  "weaver",
		Source:   domain.SourceLocation{File: source.DiffPath},
		Message:  unsupported.Error(),
		Required: true,
	}
	return changeSet, []domain.Diagnostic{diagnostic}, nil
}

// SnapshotPair compares two deterministic telemetry-contract snapshots.
type SnapshotPair struct {
	BaselinePath  string
	CandidatePath string
	Name          string
}

// Detect implements ChangeSource.
func (source SnapshotPair) Detect(ctx context.Context) (domain.ChangeSet, []domain.Diagnostic, error) {
	result, err := snapshot.CompareFiles(ctx, source.BaselinePath, source.CandidatePath, source.Name)
	if err != nil {
		return domain.ChangeSet{}, nil, err
	}
	return result.ChangeSet, result.Diagnostics, nil
}
