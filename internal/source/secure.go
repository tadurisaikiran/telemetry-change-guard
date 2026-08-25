package source

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var errPolicyViolation = errors.New("local evidence policy violation")

// IsPolicyViolation reports whether an error represents a trusted execution
// boundary or aggregate-limit violation rather than ordinary missing evidence.
func IsPolicyViolation(err error) bool {
	return errors.Is(err, errPolicyViolation)
}

// Budget confines repository-controlled paths and applies one aggregate file
// budget across every local evidence adapter in an analysis.
type Budget struct {
	lexicalRoot  string
	resolvedRoot string
	root         *os.Root
	maxFiles     int
	maxBytes     int64
	files        map[string]int64
	totalBytes   int64
}

// NewBudget opens a trusted filesystem root. The root itself may be reached
// through a host alias, but repository-controlled paths below it may not use
// symbolic links or escape through lexical parent traversal.
func NewBudget(repositoryRoot string, maxFiles int, maxBytes int64) (*Budget, error) {
	if strings.TrimSpace(repositoryRoot) == "" {
		return nil, fmt.Errorf("repository root is required")
	}
	if maxFiles <= 0 || maxBytes <= 0 {
		return nil, fmt.Errorf("source file and byte limits must be positive")
	}
	lexicalRoot, err := filepath.Abs(filepath.Clean(repositoryRoot))
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(lexicalRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	info, err := os.Stat(resolvedRoot)
	if err != nil {
		return nil, fmt.Errorf("stat repository root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("repository root is not a directory")
	}
	root, err := os.OpenRoot(lexicalRoot)
	if err != nil {
		return nil, fmt.Errorf("open repository root: %w", err)
	}
	return &Budget{
		lexicalRoot:  lexicalRoot,
		resolvedRoot: filepath.Clean(resolvedRoot),
		root:         root,
		maxFiles:     maxFiles,
		maxBytes:     maxBytes,
		files:        make(map[string]int64),
	}, nil
}

// Close releases the rooted filesystem handle.
func (budget *Budget) Close() error {
	if budget == nil || budget.root == nil {
		return nil
	}
	return budget.root.Close()
}

// Root returns the normalized trusted root spelling supplied by the caller.
// Keeping that spelling preserves containment for later glob patterns on hosts
// where an absolute prefix is an alias (for example /var and /private/var).
func (budget *Budget) Root() string {
	if budget == nil {
		return ""
	}
	return budget.lexicalRoot
}

// Expand expands a repository-contained pattern, rejects symlinked matches,
// and accounts for every unique regular file before any adapter opens it.
func (budget *Budget) Expand(pattern string) ([]string, error) {
	if budget == nil {
		return Expand(pattern)
	}
	absolutePattern, err := filepath.Abs(filepath.Clean(pattern))
	if err != nil {
		return nil, fmt.Errorf("resolve source pattern: %w", err)
	}
	if _, err := budget.relative(absolutePattern); err != nil {
		return nil, fmt.Errorf("%w: source pattern %q: %v", errPolicyViolation, pattern, err)
	}
	files, err := Expand(absolutePattern)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(files))
	for _, path := range files {
		_, display, err := budget.Register(path)
		if err != nil {
			return nil, err
		}
		result = append(result, display)
	}
	sort.Strings(result)
	return result, nil
}

// ExpandPatterns is the rooted equivalent of ExpandPatterns. Policy
// violations are returned as errors because optional repository configuration
// may not downgrade a trusted execution boundary.
func (budget *Budget) ExpandPatterns(patterns []Pattern) ([]Match, []Failure, error) {
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
		files, err := budget.Expand(pattern.Path)
		if IsPolicyViolation(err) {
			return nil, nil, err
		}
		if err != nil || len(files) == 0 {
			if err == nil {
				err = fmt.Errorf("source pattern matched no files")
			}
			failure := failures[patternKey]
			if failure == nil {
				failures[patternKey] = &failureState{pattern: patternDisplay, required: pattern.Required, err: err}
			} else {
				failure.required = failure.required || pattern.Required
			}
			continue
		}
		for _, file := range files {
			identity, display, err := budget.Register(file)
			if err != nil {
				return nil, nil, err
			}
			state := matches[identity]
			if state == nil {
				state = &matchState{path: display, patterns: make(map[string]struct{})}
				matches[identity] = state
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
		failureResult = append(failureResult, Failure{Pattern: state.pattern, Required: state.required, Err: state.err})
	}
	return result, failureResult, nil
}

// Register validates and accounts for one existing regular file.
func (budget *Budget) Register(path string) (identity string, display string, err error) {
	if budget == nil {
		return CanonicalFile(path)
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", "", fmt.Errorf("resolve source file %q: %w", path, err)
	}
	relative, err := budget.relative(absolute)
	if err != nil {
		return "", "", fmt.Errorf("%w: source file %q: %v", errPolicyViolation, path, err)
	}
	info, err := budget.lstatNoSymlinks(relative)
	if err != nil {
		return "", "", fmt.Errorf("validate source file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("%w: source file %q is not a regular file", errPolicyViolation, path)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", "", fmt.Errorf("resolve source file %q: %w", path, err)
	}
	if err := contained(budget.resolvedRoot, resolved); err != nil {
		return "", "", fmt.Errorf("%w: source file %q: %v", errPolicyViolation, path, err)
	}
	identity = filepath.Clean(resolved)
	if _, exists := budget.files[identity]; !exists {
		if len(budget.files) >= budget.maxFiles {
			return "", "", fmt.Errorf("%w: source file count exceeds the execution limit of %d", errPolicyViolation, budget.maxFiles)
		}
		if info.Size() < 0 || budget.totalBytes > budget.maxBytes-info.Size() {
			return "", "", fmt.Errorf("%w: total source bytes exceed the execution limit of %d", errPolicyViolation, budget.maxBytes)
		}
		budget.files[identity] = info.Size()
		budget.totalBytes += info.Size()
	}
	return identity, displayPath(identity), nil
}

// Open revalidates and opens a registered file through os.Root. Comparing the
// opened descriptor with Lstat closes the common symlink-swap race between
// validation and parser access.
func (budget *Budget) Open(path string) (*os.File, error) {
	if budget == nil {
		return os.Open(path)
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	relative, err := budget.relative(absolute)
	if err != nil {
		return nil, err
	}
	before, err := budget.lstatNoSymlinks(relative)
	if err != nil {
		return nil, err
	}
	file, err := budget.root.Open(relative)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	identity := filepath.Clean(filepath.Join(budget.resolvedRoot, relative))
	expectedSize, registered := budget.files[identity]
	if !registered || !after.Mode().IsRegular() || !os.SameFile(before, after) ||
		before.Size() != expectedSize || after.Size() != expectedSize {
		file.Close()
		return nil, fmt.Errorf("%w: source file changed after validation", errPolicyViolation)
	}
	return file, nil
}

// ValidateDirectory confines a repository-controlled directory without
// accounting for it as evidence.
func (budget *Budget) ValidateDirectory(path string) (string, error) {
	if budget == nil {
		return filepath.Abs(filepath.Clean(path))
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	relative, err := budget.relative(absolute)
	if err != nil {
		return "", err
	}
	info, err := budget.lstatNoSymlinks(relative)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a directory")
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	if err := contained(budget.resolvedRoot, resolved); err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func (budget *Budget) relative(absolute string) (string, error) {
	relative, err := filepath.Rel(budget.lexicalRoot, filepath.Clean(absolute))
	if err == nil && validRelative(relative) == nil {
		return relative, nil
	}
	resolvedRelative, resolvedErr := filepath.Rel(budget.resolvedRoot, filepath.Clean(absolute))
	if resolvedErr == nil && validRelative(resolvedRelative) == nil {
		return resolvedRelative, nil
	}
	if err != nil {
		return "", fmt.Errorf("resolve repository-relative path: %w", err)
	}
	return "", fmt.Errorf("path resolves outside repository root")
}

func (budget *Budget) lstatNoSymlinks(relative string) (fs.FileInfo, error) {
	current := ""
	for _, component := range strings.Split(filepath.Clean(relative), string(filepath.Separator)) {
		if component == "." || component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := budget.root.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: symbolic links are not allowed: %s", errPolicyViolation, current)
		}
	}
	return budget.root.Lstat(relative)
}

func validRelative(relative string) error {
	if filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path resolves outside repository root")
	}
	return nil
}

func contained(root, path string) error {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("resolve canonical repository path: %w", err)
	}
	return validRelative(relative)
}
