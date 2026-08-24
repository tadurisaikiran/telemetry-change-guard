package snapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
)

func TestMarshalParseSnapshotIsDeterministic(t *testing.T) {
	t.Parallel()

	value := Snapshot{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "candidate"},
		Spec: Spec{
			Domain: domain.DomainPrometheus,
			Metrics: []Metric{
				{Name: "z_metric", Type: "gauge", Labels: []string{"zone", "job"}},
				{Name: "a_metric", Type: "counter", Unit: "requests", Labels: nil},
			},
		},
	}
	first, err := Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("marshal is nondeterministic\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if strings.Index(string(first), "a_metric") > strings.Index(string(first), "z_metric") {
		t.Fatalf("metrics are not sorted:\n%s", first)
	}
	parsed, err := Parse(bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Spec.Metrics[0].Labels == nil {
		t.Fatal("empty label list normalized to nil")
	}
	roundTrip, err := Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(roundTrip, first) {
		t.Fatalf("round trip changed bytes\nfirst:\n%s\nsecond:\n%s", first, roundTrip)
	}
}

func TestParseSnapshotStrictBoundsAndValidation(t *testing.T) {
	t.Parallel()

	valid := `{"apiVersion":"tcg/v1alpha1","kind":"TelemetrySnapshot","metadata":{"name":"fixture"},"spec":{"domain":"prometheus","metrics":[]}}`
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: "telemetry snapshot is empty"},
		{name: "unknown field", input: strings.Replace(valid, `"kind"`, `"unsafe":true,"kind"`, 1), want: "unknown field"},
		{name: "trailing value", input: valid + `{}`, want: "exactly one JSON value"},
		{name: "wrong domain", input: strings.Replace(valid, `"prometheus"`, `"tempo"`, 1), want: `spec.domain must be "prometheus"`},
		{name: "duplicate metric", input: strings.Replace(valid, `"metrics":[]`, `"metrics":[{"name":"a","type":"gauge","labels":[]},{"name":"a","type":"gauge","labels":[]}]`, 1), want: "duplicates spec.metrics[0].name"},
		{name: "name label", input: strings.Replace(valid, `"metrics":[]`, `"metrics":[{"name":"a","type":"gauge","labels":["__name__"]}]`, 1), want: "must not contain the metric-name label"},
		{name: "unsupported type", input: strings.Replace(valid, `"metrics":[]`, `"metrics":[{"name":"a","type":"magic","labels":[]}]`, 1), want: `type "magic" is unsupported`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse(strings.NewReader(test.input))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}

	_, err := Parse(strings.NewReader(strings.Repeat("x", maxSnapshotBytes+1)))
	if err == nil || !strings.Contains(err.Error(), "16777216-byte size limit") {
		t.Fatalf("oversized error = %v", err)
	}
}

func TestLoadSnapshotHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Load(ctx, filepath.Join(t.TempDir(), "missing.json"))
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("error = %v", err)
	}
}

func TestCompareProducesFullDeltaAndActionableRemovals(t *testing.T) {
	t.Parallel()

	baseline := fixtureSnapshot("baseline", []Metric{
		{Name: "changed", Type: "counter", Unit: "requests", Labels: []string{"keep", "removed"}},
		{Name: "gone", Type: "gauge", Labels: []string{"ignored_with_parent"}},
		{Name: "metadata_lost", Type: "counter", Unit: "seconds", Labels: []string{}},
	})
	candidate := fixtureSnapshot("candidate", []Metric{
		{Name: "added", Type: "gauge", Labels: []string{"new_label"}},
		{Name: "changed", Type: "gauge", Unit: "items", Labels: []string{"added", "keep"}},
		{Name: "metadata_lost", Type: "unknown", Labels: []string{}},
	})

	result, err := Compare(baseline, candidate, "contract-diff")
	if err != nil {
		t.Fatal(err)
	}
	gotKinds := make([]DifferenceKind, 0, len(result.Differences))
	for _, difference := range result.Differences {
		gotKinds = append(gotKinds, difference.Kind)
	}
	// The comparison is ordered by metric first, then breaking priority.
	wantKinds := []DifferenceKind{
		DifferenceMetricAdded,
		DifferenceLabelRemoved,
		DifferenceMetricTypeChanged,
		DifferenceMetricUnitChanged,
		DifferenceLabelAdded,
		DifferenceMetricRemoved,
		DifferenceMetadataUnavailable,
		DifferenceMetadataUnavailable,
	}
	if !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Fatalf("difference kinds = %v, want %v\n%#v", gotKinds, wantKinds, result.Differences)
	}
	if got, want := len(result.ChangeSet.Changes), 2; got != want {
		t.Fatalf("actionable changes = %d, want %d: %#v", got, want, result.ChangeSet.Changes)
	}
	if result.ChangeSet.Changes[0].Kind != domain.ChangeKindLabelRemove ||
		result.ChangeSet.Changes[0].From.Parent != "changed" ||
		result.ChangeSet.Changes[1].Kind != domain.ChangeKindMetricRemove {
		t.Fatalf("unexpected ChangeSet: %#v", result.ChangeSet)
	}
	if got, want := len(result.Diagnostics), 4; got != want {
		t.Fatalf("diagnostics = %d, want %d: %#v", got, want, result.Diagnostics)
	}
	for _, diagnostic := range result.Diagnostics {
		if !diagnostic.Required {
			t.Fatalf("semantic diagnostic is not required: %#v", diagnostic)
		}
	}
	contents, err := MarshalDiff(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Diff
	if err := json.Unmarshal(contents, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, result) {
		t.Fatalf("diff JSON round trip mismatch\ndecoded: %#v\nresult:  %#v", decoded, result)
	}
}

func TestCompareNoBreakingChangesProducesValidEmptyChangeSet(t *testing.T) {
	t.Parallel()

	baseline := fixtureSnapshot("baseline", []Metric{{Name: "stable", Type: "gauge", Labels: []string{"job"}}})
	candidate := fixtureSnapshot("candidate", []Metric{
		{Name: "stable", Type: "gauge", Labels: []string{"job", "zone"}},
		{Name: "new_metric", Type: "counter", Labels: []string{}},
	})
	result, err := Compare(baseline, candidate, "safe-additions")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ChangeSet.Changes) != 0 || len(result.Diagnostics) != 0 || len(result.Differences) != 2 {
		t.Fatalf("result = %#v", result)
	}
}

func TestCompareFailsClosedOnAsymmetricSemanticMetadata(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		baseline  Metric
		candidate Metric
	}{
		{
			name:      "candidate metadata unavailable",
			baseline:  Metric{Name: "requests", Type: "counter", Unit: "requests", Labels: []string{}},
			candidate: Metric{Name: "requests", Type: "unknown", Labels: []string{}},
		},
		{
			name:      "baseline metadata unavailable",
			baseline:  Metric{Name: "requests", Type: "unknown", Labels: []string{}},
			candidate: Metric{Name: "requests", Type: "counter", Unit: "requests", Labels: []string{}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, err := Compare(
				fixtureSnapshot("baseline", []Metric{test.baseline}),
				fixtureSnapshot("candidate", []Metric{test.candidate}),
				"semantic-evidence",
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Differences) != 2 || len(result.Diagnostics) != 2 {
				t.Fatalf("result = %#v", result)
			}
			for _, difference := range result.Differences {
				if difference.Kind != DifferenceMetadataUnavailable {
					t.Fatalf("difference = %#v", difference)
				}
			}
		})
	}
}

func TestCompareTreatsUntypedAsKnownMetricType(t *testing.T) {
	t.Parallel()

	result, err := Compare(
		fixtureSnapshot("baseline", []Metric{{Name: "value", Type: "untyped", Labels: []string{}}}),
		fixtureSnapshot("candidate", []Metric{{Name: "value", Type: "gauge", Labels: []string{}}}),
		"type-change",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Differences) != 1 || result.Differences[0].Kind != DifferenceMetricTypeChanged ||
		len(result.Diagnostics) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestLoadSnapshotClosesAndReturnsNormalizedFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "snapshot.json")
	contents, err := Marshal(fixtureSnapshot("fixture", []Metric{{Name: "z", Type: "gauge", Labels: []string{"b", "a"}}}))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Spec.Metrics[0].Labels; !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("labels = %v", got)
	}
}

func FuzzParseSnapshotDoesNotPanic(f *testing.F) {
	seed, err := Marshal(fixtureSnapshot("fuzz", []Metric{{Name: "requests_total", Type: "counter", Labels: []string{"job"}}}))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte(""))
	f.Add([]byte(`{"apiVersion":"tcg/v1alpha1"}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		_, _ = Parse(bytes.NewReader(input))
	})
}

func fixtureSnapshot(name string, metrics []Metric) Snapshot {
	return Snapshot{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: name},
		Spec:       Spec{Domain: domain.DomainPrometheus, Metrics: metrics},
	}
}
