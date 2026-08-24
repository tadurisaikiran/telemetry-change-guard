// Package source expands deterministic local filesystem source patterns.
package source

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Expand returns files matching a path pattern. In addition to filepath.Match
// syntax it supports ** as a whole path segment for recursive matching.
func Expand(pattern string) ([]string, error) {
	pattern = filepath.Clean(pattern)
	if !hasMeta(pattern) {
		info, err := os.Stat(pattern)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return []string{pattern}, nil
		}
		return filesUnder(pattern)
	}

	root := patternRoot(pattern)
	var matches []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		matched, err := matchPath(pattern, path)
		if err != nil {
			return err
		}
		if matched {
			matches = append(matches, filepath.Clean(path))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("expand %q: %w", pattern, err)
	}
	sort.Strings(matches)
	return matches, nil
}

func filesUnder(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			files = append(files, filepath.Clean(path))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func hasMeta(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}

func patternRoot(pattern string) string {
	volume := filepath.VolumeName(pattern)
	remainder := strings.TrimPrefix(pattern, volume)
	segments := strings.Split(filepath.ToSlash(remainder), "/")
	var rootSegments []string
	for _, segment := range segments {
		if strings.ContainsAny(segment, "*?[") {
			break
		}
		rootSegments = append(rootSegments, segment)
	}
	root := filepath.FromSlash(strings.Join(rootSegments, "/"))
	if volume != "" {
		root = volume + root
	}
	if root == "" {
		return "."
	}
	return root
}

func matchPath(pattern, candidate string) (bool, error) {
	patternSegments := splitPath(pattern)
	candidateSegments := splitPath(candidate)
	return matchSegments(patternSegments, candidateSegments)
}

func splitPath(value string) []string {
	normalized := filepath.ToSlash(filepath.Clean(value))
	normalized = strings.TrimPrefix(normalized, "./")
	return strings.Split(normalized, "/")
}

func matchSegments(pattern, candidate []string) (bool, error) {
	if len(pattern) == 0 {
		return len(candidate) == 0, nil
	}
	if pattern[0] == "**" {
		matched, err := matchSegments(pattern[1:], candidate)
		if err != nil || matched {
			return matched, err
		}
		if len(candidate) == 0 {
			return false, nil
		}
		return matchSegments(pattern, candidate[1:])
	}
	if len(candidate) == 0 {
		return false, nil
	}
	matched, err := filepath.Match(pattern[0], candidate[0])
	if err != nil || !matched {
		return false, err
	}
	return matchSegments(pattern[1:], candidate[1:])
}
