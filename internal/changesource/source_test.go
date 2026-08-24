package changesource

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/snapshot"
)

func TestSupportedSourcesImplementContract(t *testing.T) {
	t.Parallel()

	var _ ChangeSource = Explicit{}
	var _ ChangeSource = Weaver{}
	var _ ChangeSource = SnapshotPair{}
}

func TestExplicitDetectsNativeChangeSet(t *testing.T) {
	t.Parallel()

	changeSet, diagnostics, err := (Explicit{Path: filepath.Join("..", "config", "testdata", "valid", "changeset.yaml")}).Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 || changeSet.Kind != domain.ChangeSetKind || len(changeSet.Changes) != 2 {
		t.Fatalf("changeSet = %#v, diagnostics = %#v", changeSet, diagnostics)
	}
}

func TestWeaverDetectsNormalizedChangeSet(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "adapters", "weaver", "testdata")
	changeSet, diagnostics, err := (Weaver{
		DiffPath: filepath.Join(root, "diff-v2.json"), MappingPath: filepath.Join(root, "mapping.yaml"),
	}).Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 || changeSet.Kind != domain.ChangeSetKind || len(changeSet.Changes) == 0 {
		t.Fatalf("changeSet = %#v, diagnostics = %#v", changeSet, diagnostics)
	}
	if got := changeSet.Changes[0].Metadata["source.adapter"]; got != "weaver" {
		t.Fatalf("source adapter = %q", got)
	}
}

func TestWeaverPreservesIncompleteMappingsAsRequiredDiagnostics(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "adapters", "weaver", "testdata")
	diffPath := filepath.Join(root, "diff-v2.json")
	changeSet, diagnostics, err := (Weaver{
		DiffPath: diffPath, MappingPath: filepath.Join(root, "mapping-incomplete.yaml"),
	}).Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(changeSet.Changes) != 2 || len(diagnostics) != 1 || !diagnostics[0].Required ||
		diagnostics[0].Source.File != diffPath {
		t.Fatalf("changeSet = %#v, diagnostics = %#v", changeSet, diagnostics)
	}
}

func TestWeaverTreatsFieldLevelUnsupportedChangeAsIncomplete(t *testing.T) {
	t.Parallel()

	diffPath := filepath.Join(t.TempDir(), "updated.json")
	if err := os.WriteFile(diffPath, []byte(`{
  "head_schema_url":{"url":"https://example.test/2"},
  "baseline_schema_url":{"url":"https://example.test/1"},
  "registry":{
    "attribute_changes":[],"attribute_group_changes":[],"entity_changes":[],
    "event_changes":[],"metric_changes":[{"type":"updated","name":"requests"}],
    "span_changes":[]
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mappingPath := filepath.Join("..", "..", "adapters", "weaver", "testdata", "mapping.yaml")
	changeSet, diagnostics, err := (Weaver{DiffPath: diffPath, MappingPath: mappingPath}).Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(changeSet.Changes) != 0 || len(diagnostics) != 1 || !diagnostics[0].Required ||
		!strings.Contains(diagnostics[0].Message, "field-level mapping information") {
		t.Fatalf("changeSet = %#v, diagnostics = %#v", changeSet, diagnostics)
	}
}

func TestSnapshotPairDetectsChangesAndUncertaintyWithFileProvenance(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	baselinePath := filepath.Join(root, "baseline.json")
	candidatePath := filepath.Join(root, "candidate.json")
	writeSnapshot(t, baselinePath, snapshot.Snapshot{
		APIVersion: snapshot.APIVersion,
		Kind:       snapshot.Kind,
		Metadata:   snapshot.Metadata{Name: "baseline"},
		Spec: snapshot.Spec{Domain: domain.DomainPrometheus, Metrics: []snapshot.Metric{
			{Name: "removed", Type: "gauge", Labels: []string{}},
			{Name: "semantic", Type: "counter", Labels: []string{}},
		}},
	})
	writeSnapshot(t, candidatePath, snapshot.Snapshot{
		APIVersion: snapshot.APIVersion,
		Kind:       snapshot.Kind,
		Metadata:   snapshot.Metadata{Name: "candidate"},
		Spec: snapshot.Spec{Domain: domain.DomainPrometheus, Metrics: []snapshot.Metric{
			{Name: "semantic", Type: "gauge", Labels: []string{}},
		}},
	})
	changeSet, diagnostics, err := (SnapshotPair{
		BaselinePath: baselinePath, CandidatePath: candidatePath, Name: "detected",
	}).Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(changeSet.Changes) != 1 || len(diagnostics) != 1 || !diagnostics[0].Required {
		t.Fatalf("changeSet = %#v, diagnostics = %#v", changeSet, diagnostics)
	}
	if got := changeSet.Changes[0].Metadata["source.baseline.file"]; got != baselinePath {
		t.Fatalf("baseline provenance = %q", got)
	}
	if got := diagnostics[0].Source.File; got != candidatePath {
		t.Fatalf("diagnostic provenance = %q", got)
	}
}

func writeSnapshot(t *testing.T, path string, value snapshot.Snapshot) {
	t.Helper()
	contents, err := snapshot.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}
