package agentic

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxWorkspaceFiles = 100_000
	maxWorkspaceBytes = 512 << 20
)

type worktree struct {
	repository string
	root       string
	path       string
	commit     string
	process    processRunner
}

func createWorktree(ctx context.Context, repository, revision string, process processRunner) (*worktree, error) {
	if process == nil {
		process = execProcessRunner{}
	}
	gitPath, err := executablePath("git")
	if err != nil {
		return nil, err
	}
	environment, err := filteredEnvironment(nil)
	if err != nil {
		return nil, err
	}
	resolveContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resolved, err := process.Run(resolveContext, processSpec{
		Command: gitPath,
		Args: []string{
			"-c", "core.hooksPath=/dev/null",
			"-c", "core.fsmonitor=false",
			"-C", repository,
			"rev-parse", "--verify", revision + "^{commit}",
		},
		Env:         environment,
		StdoutLimit: 256,
		StderrLimit: 64 << 10,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve repository revision: %w", err)
	}
	if resolved.ExitCode != 0 {
		return nil, fmt.Errorf("resolve repository revision exited %d: %s", resolved.ExitCode, safeMessage(resolved.Stderr, 2048))
	}
	commit := strings.TrimSpace(string(resolved.Stdout))
	if len(commit) < 40 || len(commit) > 64 {
		return nil, fmt.Errorf("resolved repository commit is invalid")
	}
	if _, err := hex.DecodeString(commit); err != nil {
		return nil, fmt.Errorf("resolved repository commit is invalid")
	}

	temporaryRoot, err := os.MkdirTemp("", "tcg-agentic-worktree-")
	if err != nil {
		return nil, fmt.Errorf("create temporary worktree root: %w", err)
	}
	if err := os.Chmod(temporaryRoot, 0o700); err != nil {
		_ = os.RemoveAll(temporaryRoot)
		return nil, fmt.Errorf("secure temporary worktree root: %w", err)
	}
	path := filepath.Join(temporaryRoot, "repository")
	addContext, addCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer addCancel()
	added, err := process.Run(addContext, processSpec{
		Command: gitPath,
		Args: []string{
			"-c", "core.hooksPath=/dev/null",
			"-c", "core.fsmonitor=false",
			"-C", repository,
			"worktree", "add", "--detach", "--no-checkout", path, commit,
		},
		Env:         environment,
		StdoutLimit: 64 << 10,
		StderrLimit: 64 << 10,
	})
	if err != nil || added.ExitCode != 0 {
		_ = os.RemoveAll(temporaryRoot)
		if err != nil {
			return nil, fmt.Errorf("create Git worktree: %w", err)
		}
		return nil, fmt.Errorf("create Git worktree exited %d: %s", added.ExitCode, safeMessage(added.Stderr, 2048))
	}

	checkoutContext, checkoutCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer checkoutCancel()
	checkedOut, err := process.Run(checkoutContext, processSpec{
		Command: gitPath,
		Args: []string{
			"-c", "core.hooksPath=/dev/null",
			"-c", "core.fsmonitor=false",
			"-C", path,
			"checkout", "--detach", "--no-recurse-submodules", commit, "--",
		},
		Env:         environment,
		StdoutLimit: 64 << 10,
		StderrLimit: 64 << 10,
	})
	if err != nil || checkedOut.ExitCode != 0 {
		candidate := &worktree{repository: repository, root: temporaryRoot, path: path, commit: commit, process: process}
		_ = candidate.Close(context.Background())
		if err != nil {
			return nil, fmt.Errorf("checkout Git worktree: %w", err)
		}
		return nil, fmt.Errorf("checkout Git worktree exited %d: %s", checkedOut.ExitCode, safeMessage(checkedOut.Stderr, 2048))
	}
	return &worktree{repository: repository, root: temporaryRoot, path: path, commit: commit, process: process}, nil
}

func (tree *worktree) AgentPath(relative string) (string, error) {
	if err := validateRelativePath(relative, "agentWorkspace"); err != nil {
		return "", err
	}
	candidate := filepath.Join(tree.path, filepath.Clean(relative))
	if err := ensureWithin(tree.path, candidate); err != nil {
		return "", err
	}
	info, err := os.Lstat(candidate)
	if err != nil {
		return "", fmt.Errorf("inspect agent workspace: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("agent workspace must be a non-symlink directory")
	}
	real, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve agent workspace: %w", err)
	}
	if err := ensureWithin(tree.path, real); err != nil {
		return "", err
	}
	if err := validateWorkspaceTree(real); err != nil {
		return "", err
	}
	return real, nil
}

func (tree *worktree) ChangedFiles(ctx context.Context, agentWorkspace string, maximum int) ([]string, error) {
	tracked, err := tree.gitNames(ctx, []string{"diff", "--name-only", "-z", "--no-renames", "HEAD", "--"})
	if err != nil {
		return nil, err
	}
	staged, err := tree.gitNames(ctx, []string{"diff", "--cached", "--name-only", "-z", "--no-renames", "HEAD", "--"})
	if err != nil {
		return nil, err
	}
	untracked, err := tree.gitNames(ctx, []string{"ls-files", "--others", "--exclude-standard", "-z", "--"})
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(tracked)+len(staged)+len(untracked))
	for _, group := range [][]string{tracked, staged, untracked} {
		for _, path := range group {
			clean := filepath.Clean(path)
			if err := validateRelativePath(clean, "changed file"); err != nil || clean == "." {
				return nil, fmt.Errorf("Git returned invalid changed path %q", path)
			}
			seen[clean] = struct{}{}
		}
	}
	if len(seen) > maximum {
		return nil, fmt.Errorf("changed file count %d exceeds limit %d", len(seen), maximum)
	}
	workspace := filepath.Clean(agentWorkspace)
	result := make([]string, 0, len(seen))
	for path := range seen {
		if workspace != "." && path != workspace && !strings.HasPrefix(path, workspace+string(filepath.Separator)) {
			return nil, fmt.Errorf("agent changed %q outside declared workspace %q", path, workspace)
		}
		result = append(result, filepath.ToSlash(path))
	}
	sort.Strings(result)
	return result, nil
}

func (tree *worktree) Diff(ctx context.Context, changedFiles []string, limit int64) ([]byte, error) {
	gitPath, err := executablePath("git")
	if err != nil {
		return nil, err
	}
	environment, err := filteredEnvironment(nil)
	if err != nil {
		return nil, err
	}
	remaining := limit
	var output bytes.Buffer
	runDiff := func(arguments []string, allowedExitOne bool) error {
		if remaining <= 0 {
			return fmt.Errorf("diff exceeds the %d-byte limit", limit)
		}
		result, runErr := tree.process.Run(ctx, processSpec{
			Command:     gitPath,
			Args:        append([]string{"-c", "core.hooksPath=/dev/null", "-C", tree.path}, arguments...),
			Env:         environment,
			StdoutLimit: remaining,
			StderrLimit: 64 << 10,
		})
		if runErr != nil {
			return runErr
		}
		if result.ExitCode != 0 && !(allowedExitOne && result.ExitCode == 1) {
			return fmt.Errorf("git diff exited %d: %s", result.ExitCode, safeMessage(result.Stderr, 2048))
		}
		output.Write(result.Stdout)
		remaining -= int64(len(result.Stdout))
		return nil
	}
	if err := runDiff([]string{"diff", "--binary", "--no-ext-diff", "--no-textconv", "--full-index", "HEAD", "--"}, false); err != nil {
		return nil, err
	}
	untracked, err := tree.gitNames(ctx, []string{"ls-files", "--others", "--exclude-standard", "-z", "--"})
	if err != nil {
		return nil, err
	}
	for _, path := range untracked {
		if err := runDiff([]string{"diff", "--no-index", "--binary", "--no-ext-diff", "--no-textconv", "--full-index", "--", "/dev/null", path}, true); err != nil {
			return nil, err
		}
	}
	if len(changedFiles) != 0 && output.Len() == 0 {
		return nil, fmt.Errorf("changed files exist but no review diff was produced")
	}
	return output.Bytes(), nil
}

func (tree *worktree) gitNames(ctx context.Context, arguments []string) ([]string, error) {
	gitPath, err := executablePath("git")
	if err != nil {
		return nil, err
	}
	environment, err := filteredEnvironment(nil)
	if err != nil {
		return nil, err
	}
	commandContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, err := tree.process.Run(commandContext, processSpec{
		Command:     gitPath,
		Args:        append([]string{"-c", "core.hooksPath=/dev/null", "-C", tree.path}, arguments...),
		Env:         environment,
		StdoutLimit: 4 << 20,
		StderrLimit: 64 << 10,
	})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("git command exited %d: %s", result.ExitCode, safeMessage(result.Stderr, 2048))
	}
	parts := bytes.Split(result.Stdout, []byte{0})
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		paths = append(paths, string(part))
	}
	return paths, nil
}

func (tree *worktree) Close(ctx context.Context) error {
	gitPath, err := executablePath("git")
	if err != nil {
		return err
	}
	environment, err := filteredEnvironment(nil)
	if err != nil {
		return err
	}
	removeContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, runErr := tree.process.Run(removeContext, processSpec{
		Command: gitPath,
		Args: []string{
			"-c", "core.hooksPath=/dev/null",
			"-C", tree.repository,
			"worktree", "remove", "--force", tree.path,
		},
		Env:         environment,
		StdoutLimit: 64 << 10,
		StderrLimit: 64 << 10,
	})
	removeErr := os.RemoveAll(tree.root)
	if runErr != nil {
		return fmt.Errorf("remove Git worktree: %w", runErr)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("remove Git worktree exited %d: %s", result.ExitCode, safeMessage(result.Stderr, 2048))
	}
	if removeErr != nil {
		return fmt.Errorf("remove temporary worktree root: %w", removeErr)
	}
	return nil
}

func validateWorkspaceTree(root string) error {
	count := 0
	var total int64
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("agent workspace contains symlink %q", path)
		}
		if path != root && entry.Name() == ".git" {
			return fmt.Errorf("agent workspace contains forbidden Git metadata %q", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("agent workspace contains unsupported file type %q", path)
		}
		count++
		total += info.Size()
		if count > maxWorkspaceFiles {
			return fmt.Errorf("agent workspace exceeds %d files", maxWorkspaceFiles)
		}
		if total > maxWorkspaceBytes {
			return fmt.Errorf("agent workspace exceeds %d bytes", maxWorkspaceBytes)
		}
		return nil
	})
}
