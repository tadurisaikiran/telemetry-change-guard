package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
)

func TestParseChangeSetStrictRoundTrip(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("testdata/valid/changeset.yaml")
	if err != nil {
		t.Fatal(err)
	}
	changeSet, err := ParseChangeSet(strings.NewReader(string(contents)))
	if err != nil {
		t.Fatalf("ParseChangeSet() error = %v", err)
	}
	if changeSet.APIVersion != domain.ChangeSetAPIVersion || changeSet.Kind != domain.ChangeSetKind {
		t.Fatalf("identity = %s/%s", changeSet.APIVersion, changeSet.Kind)
	}
	if got, want := len(changeSet.Changes), 2; got != want {
		t.Fatalf("changes = %d, want %d", got, want)
	}
	if got := changeSet.Changes[0].Metadata["ticket"]; got != "OBS-142" {
		t.Fatalf("ticket metadata = %q", got)
	}

	encoded, err := json.Marshal(changeSet)
	if err != nil {
		t.Fatal(err)
	}
	var decoded domain.ChangeSet
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, changeSet) {
		t.Fatalf("JSON round trip changed ChangeSet\noriginal: %#v\ndecoded:  %#v", changeSet, decoded)
	}
}

func TestParseChangeSetRejectsUnknownFieldsAndMultipleDocuments(t *testing.T) {
	t.Parallel()

	valid, err := os.ReadFile("testdata/valid/changeset.yaml")
	if err != nil {
		t.Fatal(err)
	}
	unknown := strings.Replace(string(valid), "kind: ChangeSet", "kind: ChangeSet\nunsafeDefault: true", 1)
	_, err = ParseChangeSet(strings.NewReader(unknown))
	assertErrorContains(t, err, "field unsafeDefault not found")

	_, err = ParseChangeSet(strings.NewReader(string(valid) + "\n---\n" + string(valid)))
	assertErrorContains(t, err, "must contain exactly one YAML document")
}

func TestParseChangeSetRejectsEmptyAndOversizedInput(t *testing.T) {
	t.Parallel()

	_, err := ParseChangeSet(strings.NewReader(""))
	assertErrorContains(t, err, "change set manifest is empty")

	_, err = ParseChangeSet(strings.NewReader(strings.Repeat("x", maxChangeSetManifestBytes+1)))
	assertErrorContains(t, err, "exceeds the 1048576-byte size limit")
}

func TestLoadChangeSetHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := LoadChangeSet(ctx, "testdata/valid/changeset.yaml")
	assertErrorContains(t, err, "context canceled")
}

func TestValidateChangeSetFailsClosedAndOrdersMetadataDiagnostics(t *testing.T) {
	t.Parallel()

	changeSet := validChangeSet()
	changeSet.APIVersion = "tcg/v9"
	changeSet.Kind = "UnsafeChange"
	changeSet.Metadata.Name = " "
	changeSet.Changes[0].Metadata = map[string]string{
		" ":                      "blank",
		strings.Repeat("k", 129): "long key",
		"bounded-key":            strings.Repeat("v", 4097),
	}
	err := ValidateChangeSet(changeSet)
	for _, expected := range []string{
		`apiVersion: must be "tcg/v1alpha1"`,
		`kind: must be "ChangeSet"`,
		"metadata.name: is required",
		"metadata: keys must not be blank",
		"metadata.bounded-key: value exceeds 4096 bytes",
		"key exceeds 128 bytes",
	} {
		assertErrorContains(t, err, expected)
	}
	first := err.Error()
	for range 20 {
		if got := ValidateChangeSet(changeSet).Error(); got != first {
			t.Fatalf("validation diagnostics are nondeterministic\nfirst: %s\ngot: %s", first, got)
		}
	}
}

func TestValidateChangeSetRejectsAmbiguousOrUnsupportedChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*domain.ChangeSet)
		wantErr string
	}{
		{
			name: "duplicate ID",
			mutate: func(changeSet *domain.ChangeSet) {
				changeSet.Changes = append(changeSet.Changes, changeSet.Changes[0])
			},
			wantErr: "duplicates spec.changes[0].id",
		},
		{
			name: "unsupported kind",
			mutate: func(changeSet *domain.ChangeSet) {
				changeSet.Changes[0].Kind = domain.ChangeKind("metric_guess")
			},
			wantErr: `unsupported change kind "metric_guess"`,
		},
		{
			name: "cross-domain destination",
			mutate: func(changeSet *domain.ChangeSet) {
				changeSet.Changes[0].To.Domain = domain.DomainTempo
			},
			wantErr: "to.domain: must match the change domain",
		},
		{
			name: "rename without destination",
			mutate: func(changeSet *domain.ChangeSet) {
				changeSet.Changes[0].To = nil
			},
			wantErr: "to: is required for a rename",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			changeSet := validChangeSet()
			test.mutate(&changeSet)
			assertErrorContains(t, ValidateChangeSet(changeSet), test.wantErr)
		})
	}
}

func TestValidateChangeSetBoundsMetadataEntryCount(t *testing.T) {
	t.Parallel()

	changeSet := validChangeSet()
	changeSet.Changes[0].Metadata = make(map[string]string, maxChangeMetadataEntries+1)
	for index := range maxChangeMetadataEntries + 1 {
		changeSet.Changes[0].Metadata[fmt.Sprintf("key-%02d", index)] = "value"
	}
	assertErrorContains(
		t,
		ValidateChangeSet(changeSet),
		"metadata: must contain no more than 64 entries",
	)
}

func TestNormalizeMigrationAllLegacyChangeKindsGolden(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("testdata/valid/all-domain-change-kinds.yaml")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := ParseMigration(strings.NewReader(string(contents)))
	if err != nil {
		t.Fatal(err)
	}
	changeSet, err := NormalizeMigration(migration)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.MarshalIndent(changeSet, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	want, err := os.ReadFile("testdata/valid/all-domain-change-kinds.changeset.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != string(want) {
		t.Fatalf("normalized ChangeSet mismatch\n--- got ---\n%s--- want ---\n%s", encoded, want)
	}
}

func TestNormalizeMigrationDoesNotAliasMutableInput(t *testing.T) {
	t.Parallel()

	migration := domain.Migration{
		APIVersion: domain.MigrationAPIVersion,
		Kind:       domain.MigrationKind,
		Metadata:   domain.MigrationMetadata{Name: "deep-copy"},
		Changes:    validChangeSet().Changes,
	}
	migration.Changes[0].Metadata = map[string]string{"source.adapter": "fixture"}
	changeSet, err := NormalizeMigration(migration)
	if err != nil {
		t.Fatal(err)
	}
	migration.Changes[0].Metadata["source.adapter"] = "mutated"
	migration.Changes[0].To.Name = "mutated"
	if got := changeSet.Changes[0].Metadata["source.adapter"]; got != "fixture" {
		t.Fatalf("normalized metadata aliased input: %q", got)
	}
	if got := changeSet.Changes[0].To.Name; got != "new_metric" {
		t.Fatalf("normalized destination aliased input: %q", got)
	}
	changeSet.Changes[0].Metadata["source.adapter"] = "changed-set"
	if got := migration.Changes[0].Metadata["source.adapter"]; got != "mutated" {
		t.Fatalf("migration metadata aliased normalized output: %q", got)
	}
}

func TestNormalizeMigrationRejectsInvalidLegacyInput(t *testing.T) {
	t.Parallel()

	migration := domain.Migration{APIVersion: domain.MigrationAPIVersion, Kind: domain.MigrationKind}
	_, err := NormalizeMigration(migration)
	assertErrorContains(t, err, "metadata.name: is required")
	assertErrorContains(t, err, "spec.changes: must contain at least one change")
}

func TestMarshalChangeSetRoundTripsEmptyDetectedSet(t *testing.T) {
	t.Parallel()

	changeSet := domain.ChangeSet{
		APIVersion: domain.ChangeSetAPIVersion,
		Kind:       domain.ChangeSetKind,
		Metadata:   domain.ChangeSetMetadata{Name: "no-breaking-changes"},
		Changes:    []domain.Change{},
	}
	contents, err := MarshalChangeSet(changeSet)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "to: null") || strings.Contains(string(contents), "metadata: null") {
		t.Fatalf("manifest contains noisy null fields:\n%s", contents)
	}
	parsed, err := ParseChangeSet(strings.NewReader(string(contents)))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed, changeSet) {
		t.Fatalf("round trip mismatch\nparsed: %#v\nwant:   %#v", parsed, changeSet)
	}
}

func FuzzParseChangeSetDoesNotPanic(f *testing.F) {
	seed, err := os.ReadFile("testdata/valid/changeset.yaml")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte(""))
	f.Add([]byte("apiVersion: tcg/v1alpha1\nkind: ChangeSet\n"))
	f.Fuzz(func(t *testing.T, input []byte) {
		_, _ = ParseChangeSet(strings.NewReader(string(input)))
	})
}

func validChangeSet() domain.ChangeSet {
	destination := domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: "new_metric"}
	return domain.ChangeSet{
		APIVersion: domain.ChangeSetAPIVersion,
		Kind:       domain.ChangeSetKind,
		Metadata:   domain.ChangeSetMetadata{Name: "fixture"},
		Changes: []domain.Change{{
			ID:     "metric-rename",
			Kind:   domain.ChangeKindMetricRename,
			Domain: domain.DomainPrometheus,
			From:   domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: "old_metric"},
			To:     &destination,
		}},
	}
}
