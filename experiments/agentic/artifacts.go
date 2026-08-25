package agentic

import (
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxIntegrityFiles = 4096
	maxIntegrityBytes = 64 << 20
	maxArtifactBytes  = 16 << 20
)

func PrepareOutputDirectory(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve output directory: %w", err)
	}
	if _, err := os.Lstat(absPath); err == nil {
		return "", fmt.Errorf("output directory %q already exists", absPath)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect output directory: %w", err)
	}
	if err := os.MkdirAll(absPath, 0o700); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}
	return absPath, nil
}

func writeArtifact(root, relative string, contents []byte) (string, error) {
	if err := validateRelativePath(relative, "artifact path"); err != nil || relative == "." {
		return "", fmt.Errorf("invalid artifact path %q", relative)
	}
	if len(contents) > maxArtifactBytes {
		return "", fmt.Errorf("artifact %q exceeds the %d-byte limit", relative, maxArtifactBytes)
	}
	path := filepath.Join(root, relative)
	if err := ensureWithin(root, path); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create artifact directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create artifact %q: %w", relative, err)
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("write artifact %q: %w", relative, err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("sync artifact %q: %w", relative, err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close artifact %q: %w", relative, err)
	}
	return filepath.ToSlash(relative), nil
}

func writeRunResult(root string, result RunResult) error {
	contents, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encode run result: %w", err)
	}
	contents = append(contents, '\n')
	temporary := filepath.Join(root, ".run.json.tmp")
	final := filepath.Join(root, "run.json")
	if err := os.WriteFile(temporary, contents, 0o600); err != nil {
		return fmt.Errorf("write temporary run result: %w", err)
	}
	if err := os.Rename(temporary, final); err != nil {
		return fmt.Errorf("publish run result: %w", err)
	}
	return nil
}

func digestPaths(paths []string) ([]FileDigest, error) {
	files := make(map[string]struct{})
	for _, root := range paths {
		info, err := os.Lstat(root)
		if err != nil {
			return nil, fmt.Errorf("inspect integrity path %q: %w", root, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("integrity path %q is a symlink", root)
		}
		if info.Mode().IsRegular() {
			files[root] = struct{}{}
			continue
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("integrity path %q has unsupported type", root)
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("integrity path %q contains symlink %q", root, path)
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("integrity path %q contains unsupported file %q", root, path)
			}
			files[path] = struct{}{}
			if len(files) > maxIntegrityFiles {
				return fmt.Errorf("integrity file count exceeds %d", maxIntegrityFiles)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	ordered := make([]string, 0, len(files))
	for path := range files {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	var total int64
	digests := make([]FileDigest, 0, len(ordered))
	for _, path := range ordered {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("integrity file %q changed type", path)
		}
		total += info.Size()
		if total > maxIntegrityBytes {
			return nil, fmt.Errorf("integrity contents exceed %d bytes", maxIntegrityBytes)
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, io.LimitReader(file, info.Size()+1))
		closeErr := file.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		digests = append(digests, FileDigest{Path: path, SHA256: hex.EncodeToString(hash.Sum(nil)), Bytes: info.Size()})
	}
	return digests, nil
}

func verifyDigests(roots []string, expected []FileDigest) error {
	// Rewalk the declared roots instead of only rehashing the original files.
	// That makes adding or removing a file inside an integrity directory an
	// integrity violation too.
	actual, err := digestPaths(roots)
	if err != nil {
		return err
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("control-file set changed: got %d files, expected %d", len(actual), len(expected))
	}
	for index := range expected {
		if actual[index] != expected[index] {
			return fmt.Errorf("control-file integrity changed at %q", expected[index].Path)
		}
	}
	return nil
}

func toolIdentity(command string) (ToolIdentity, error) {
	path, err := executablePath(command)
	if err != nil {
		return ToolIdentity{}, err
	}
	digests, err := digestPaths([]string{path})
	if err != nil {
		return ToolIdentity{}, fmt.Errorf("hash TCG executable: %w", err)
	}
	identity := ToolIdentity{Command: path, SHA256: digests[0].SHA256}
	if info, readErr := buildinfo.ReadFile(path); readErr == nil {
		identity.ModulePath = info.Main.Path
		identity.Module = info.Main.Version
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				identity.VCSRevision = setting.Value
			case "vcs.modified":
				identity.VCSModified = setting.Value == "true"
			}
		}
	}
	return identity, nil
}

func safeMessage(contents []byte, limit int) string {
	if limit <= 0 {
		return ""
	}
	value := strings.ToValidUTF8(string(contents), "�")
	var builder strings.Builder
	for _, character := range value {
		if character == '\n' || character == '\t' || (!unicode.IsControl(character) && character != '\u007f') {
			builder.WriteRune(character)
		} else {
			builder.WriteRune(' ')
		}
		if builder.Len() >= limit {
			break
		}
	}
	result := builder.String()
	for len(result) > limit || !utf8.ValidString(result) {
		result = result[:len(result)-1]
	}
	return strings.TrimSpace(result)
}

func ensureWithin(root, candidate string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	rootAbs, err = filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return fmt.Errorf("resolve root %q: %w", root, err)
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return err
	}
	candidateAbs, err = evalSymlinksWithMissingLeaf(candidateAbs)
	if err != nil {
		return fmt.Errorf("resolve candidate %q: %w", candidate, err)
	}
	relative, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q escapes root %q", candidate, root)
	}
	return nil
}

// evalSymlinksWithMissingLeaf resolves every existing ancestor while retaining
// a not-yet-created suffix. This is needed for safe artifact creation and also
// normalizes platform aliases such as macOS /var -> /private/var.
func evalSymlinksWithMissingLeaf(path string) (string, error) {
	current := filepath.Clean(path)
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}
