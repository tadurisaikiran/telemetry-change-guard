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

// Pattern is one configured local source pattern and its fail-closed
// requirement. It is deliberately adapter-neutral.
type Pattern struct {
	Path     string
	Required bool
}

// Match is one canonical file matched by one or more patterns. Required is the
// logical OR of every matching pattern. Patterns retains normalized provenance
// for diagnostics and review.
type Match struct {
	Path     string
	Required bool
	Patterns []string
}

// Failure records one unique pattern that could not be expanded. Duplicate or
// normalized-equivalent patterns merge Required with logical OR.
type Failure struct {
	Pattern  string
	Required bool
	Err      error
}

// ExpandPatterns expands every pattern before any file is loaded, groups files
// by canonical identity, merges required semantics with logical OR, and returns
// stable order independent of configuration order.
func ExpandPatterns(patterns []Pattern) ([]Match, []Failure) {
	type matchState struct {
		path     string
		required bool
		patterns map[string]struct{}
	}
	type failureState struct {
		pattern  string
		required bool
		err      error
	}

	configured := append([]Pattern(nil), patterns...)
	sort.Slice(configured, func(i, j int) bool {
		left := canonicalPattern(configured[i].Path)
		right := canonicalPattern(configured[j].Path)
		if left != right {
			return left < right
		}
		if configured[i].Required != configured[j].Required {
			return configured[i].Required
		}
		return filepath.Clean(configured[i].Path) < filepath.Clean(configured[j].Path)
	})

	matches := make(map[string]*matchState)
	failures := make(map[string]*failureState)
	for _, pattern := range configured {
		patternKey := canonicalPattern(pattern.Path)
		patternDisplay := displayPath(patternKey)
		files, err := Expand(pattern.Path)
		if err != nil || len(files) == 0 {
			if err == nil {
				err = fmt.Errorf("source pattern matched no files")
			}
			failure := failures[patternKey]
			if failure == nil {
				failures[patternKey] = &failureState{
					pattern:  patternDisplay,
					required: pattern.Required,
					err:      err,
				}
			} else {
				failure.required = failure.required || pattern.Required
			}
			continue
		}
		for _, file := range files {
			key, display, canonicalErr := CanonicalFile(file)
			if canonicalErr != nil {
				failureKey := patternKey + "\x00" + filepath.Clean(file)
				failures[failureKey] = &failureState{
					pattern:  patternDisplay,
					required: pattern.Required,
					err:      canonicalErr,
				}
				continue
			}
			state := matches[key]
			if state == nil {
				state = &matchState{path: display, patterns: make(map[string]struct{})}
				matches[key] = state
			}
			state.required = state.required || pattern.Required
			state.patterns[patternDisplay] = struct{}{}
		}
	}

	matchKeys := make([]string, 0, len(matches))
	for key := range matches {
		matchKeys = append(matchKeys, key)
	}
	sort.Strings(matchKeys)
	result := make([]Match, 0, len(matchKeys))
	for _, key := range matchKeys {
		state := matches[key]
		provenance := make([]string, 0, len(state.patterns))
		for pattern := range state.patterns {
			provenance = append(provenance, pattern)
		}
		sort.Strings(provenance)
		result = append(result, Match{Path: state.path, Required: state.required, Patterns: provenance})
	}

	failureKeys := make([]string, 0, len(failures))
	for key := range failures {
		failureKeys = append(failureKeys, key)
	}
	sort.Strings(failureKeys)
	failureResult := make([]Failure, 0, len(failureKeys))
	for _, key := range failureKeys {
		state := failures[key]
		failureResult = append(failureResult, Failure{
			Pattern: state.pattern, Required: state.required, Err: state.err,
		})
	}
	return result, failureResult
}

// CanonicalFile returns a stable normalized absolute identity and a useful
// display path. It intentionally does not resolve symlinks so user-facing
// source paths remain stable across host-specific aliases such as /var and
// /private/var on macOS.
func CanonicalFile(path string) (identity string, display string, err error) {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", "", fmt.Errorf("canonicalize source file %q: %w", path, err)
	}
	absolute = filepath.Clean(absolute)
	return absolute, displayPath(absolute), nil
}

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

func canonicalPattern(pattern string) string {
	cleaned := filepath.Clean(pattern)
	absolute, err := filepath.Abs(cleaned)
	if err != nil {
		return cleaned
	}
	return filepath.Clean(absolute)
}

func displayPath(absolute string) string {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return filepath.Clean(absolute)
	}
	relative, err := filepath.Rel(workingDirectory, absolute)
	if err == nil && relative != "." && relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return filepath.Clean(relative)
	}
	return filepath.Clean(absolute)
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
