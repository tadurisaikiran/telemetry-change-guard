package source

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExpandSupportsRecursiveDoubleStar(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	nested := filepath.Join(root, "one", "two")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	for _, path := range []string{
		filepath.Join(root, "root.yaml"),
		filepath.Join(root, "one", "one.yaml"),
		filepath.Join(nested, "two.yaml"),
		filepath.Join(nested, "ignored.json"),
	} {
		if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}

	matches, err := Expand(filepath.Join(root, "**", "*.yaml"))
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}
	if got, want := len(matches), 3; got != want {
		t.Fatalf("len(matches) = %d, want %d; matches = %v", got, want, matches)
	}
}

func TestExpandPatternsMergesRequiredIndependentOfOrder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	critical := filepath.Join(root, "monitoring", "critical")
	if err := os.MkdirAll(critical, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(critical, "rules.yaml")
	if err := os.WriteFile(file, []byte("malformed"), 0o644); err != nil {
		t.Fatal(err)
	}
	broad := Pattern{Path: filepath.Join(root, "monitoring", "**", "*.yaml"), Required: false}
	narrow := Pattern{Path: filepath.Join(critical, "*.yaml"), Required: true}

	var previous []Match
	for _, patterns := range [][]Pattern{{broad, narrow}, {narrow, broad}} {
		matches, failures := ExpandPatterns(patterns)
		if len(failures) != 0 {
			t.Fatalf("failures = %#v", failures)
		}
		if len(matches) != 1 || !matches[0].Required || len(matches[0].Patterns) != 2 {
			t.Fatalf("matches = %#v", matches)
		}
		if previous != nil && !reflect.DeepEqual(matches, previous) {
			t.Fatalf("configuration order changed matches:\nfirst: %#v\nnext:  %#v", previous, matches)
		}
		previous = matches
	}
}

func TestExpandPatternsDeduplicatesExactAndNormalizedPaths(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "rules.yaml")
	if err := os.WriteFile(file, []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(workingDirectory, file)
	if err != nil {
		t.Fatal(err)
	}
	patterns := []Pattern{
		{Path: file, Required: false},
		{Path: filepath.Join(root, ".", "rules.yaml"), Required: false},
		{Path: relative, Required: true},
	}
	matches, failures := ExpandPatterns(patterns)
	if len(failures) != 0 || len(matches) != 1 || !matches[0].Required {
		t.Fatalf("matches = %#v, failures = %#v", matches, failures)
	}
}

func TestExpandPatternsMergesMissingDuplicateRequirement(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing.yaml")
	matches, failures := ExpandPatterns([]Pattern{
		{Path: missing, Required: false},
		{Path: filepath.Join(filepath.Dir(missing), ".", filepath.Base(missing)), Required: true},
	})
	if len(matches) != 0 || len(failures) != 1 || !failures[0].Required {
		t.Fatalf("matches = %#v, failures = %#v", matches, failures)
	}
	if got := failures[0].Err.Error(); got == "" {
		t.Fatal("failure message is empty")
	}
}

func TestExpandPatternsReturnsStableFileOrder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, name := range []string{"z.yaml", "a.yaml", "m.yaml"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	matches, failures := ExpandPatterns([]Pattern{{Path: filepath.Join(root, "*.yaml"), Required: true}})
	if len(failures) != 0 {
		t.Fatal(failures)
	}
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, filepath.Base(match.Path))
	}
	want := []string{"a.yaml", "m.yaml", "z.yaml"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v (%s)", got, want, fmt.Sprint(matches))
	}
}
