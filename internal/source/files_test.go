package source

import (
	"os"
	"path/filepath"
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
