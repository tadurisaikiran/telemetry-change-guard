package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHeadingAnchorsMatchGitHubStyleAndDeduplicate(t *testing.T) {
	t.Parallel()

	anchors := headingAnchors("# Hello, World!\n## Hello, World!\n```\n# ignored\n```\n")
	for _, want := range []string{"hello-world", "hello-world-1"} {
		if _, found := anchors[want]; !found {
			t.Fatalf("missing anchor %q in %#v", want, anchors)
		}
	}
}

func TestCheckLinksFindsMissingTargetAndAnchor(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	readme := filepath.Join(root, "README.md")
	doc := filepath.Join(root, "guide.md")
	if err := os.WriteFile(readme, []byte("[ok](guide.md#usage) [bad](missing.md) [anchor](guide.md#absent)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(doc, []byte("# Usage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	failures, err := checkLinks(root, readme)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 2 {
		t.Fatalf("failures = %v", failures)
	}
}

func TestCheckClaimsRejectsUnboundedCoverage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	readme := filepath.Join(root, "README.md")
	if err := os.WriteFile(readme, []byte("TCG finds your complete blast radius.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	failures, err := checkClaims(root, readme)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 1 {
		t.Fatalf("failures = %v", failures)
	}
}
