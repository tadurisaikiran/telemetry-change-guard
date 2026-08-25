package agentic

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var errOutputLimit = errors.New("process output exceeded its configured limit")

type processSpec struct {
	Command     string
	Args        []string
	Dir         string
	Env         []string
	Stdin       []byte
	StdoutLimit int64
	StderrLimit int64
}

type processResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	TimedOut bool
	Duration time.Duration
}

type processRunner interface {
	Run(context.Context, processSpec) (processResult, error)
}

type execProcessRunner struct{}

func (execProcessRunner) Run(ctx context.Context, spec processSpec) (processResult, error) {
	if strings.TrimSpace(spec.Command) == "" {
		return processResult{}, fmt.Errorf("process command is required")
	}
	stdout := &boundedBuffer{limit: spec.StdoutLimit}
	stderr := &boundedBuffer{limit: spec.StderrLimit}
	command := exec.CommandContext(ctx, spec.Command, spec.Args...)
	command.Dir = spec.Dir
	command.Env = append([]string(nil), spec.Env...)
	command.Stdin = bytes.NewReader(spec.Stdin)
	command.Stdout = stdout
	command.Stderr = stderr
	command.WaitDelay = 2 * time.Second

	started := time.Now()
	err := command.Run()
	result := processResult{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: 0,
		TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded),
		Duration: time.Since(started),
	}
	if stdout.Exceeded() || stderr.Exceeded() {
		return result, errOutputLimit
	}
	if err == nil {
		return result, nil
	}
	if ctx.Err() != nil {
		result.ExitCode = 1
		return result, ctx.Err()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		if result.ExitCode < 0 {
			result.ExitCode = 1
		}
		return result, nil
	}
	return result, err
}

type boundedBuffer struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	limit    int64
	exceeded bool
}

func (buffer *boundedBuffer) Write(contents []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	if buffer.limit <= 0 {
		buffer.exceeded = true
		return 0, errOutputLimit
	}
	remaining := buffer.limit - int64(buffer.buffer.Len())
	if remaining <= 0 {
		buffer.exceeded = true
		return 0, errOutputLimit
	}
	if int64(len(contents)) > remaining {
		_, _ = buffer.buffer.Write(contents[:remaining])
		buffer.exceeded = true
		return int(remaining), errOutputLimit
	}
	return buffer.buffer.Write(contents)
}

func (buffer *boundedBuffer) Bytes() []byte {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return append([]byte(nil), buffer.buffer.Bytes()...)
}

func (buffer *boundedBuffer) Exceeded() bool {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.exceeded
}

func filteredEnvironment(allowed []string) ([]string, error) {
	names := append([]string{
		"PATH",
		"HOME",
		"TMPDIR",
		"XDG_RUNTIME_DIR",
		"DOCKER_HOST",
		"DOCKER_CONTEXT",
		"DOCKER_CONFIG",
		"DOCKER_CERT_PATH",
		"DOCKER_TLS_VERIFY",
		"CONTAINER_HOST",
	}, allowed...)
	seen := make(map[string]struct{}, len(names))
	var environment []string
	for _, name := range names {
		if err := validateEnvironmentName(name); err != nil {
			return nil, err
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		if value, exists := os.LookupEnv(name); exists {
			environment = append(environment, name+"="+value)
		}
	}
	sort.Strings(environment)
	return environment, nil
}

func executablePath(command string) (string, error) {
	path := command
	var err error
	if !filepath.IsAbs(path) {
		path, err = exec.LookPath(path)
		if err != nil {
			return "", fmt.Errorf("locate executable %q: %w", command, err)
		}
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve executable %q: %w", command, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("inspect executable %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		real, resolveErr := filepath.EvalSymlinks(path)
		if resolveErr != nil {
			return "", fmt.Errorf("resolve executable symlink %q: %w", path, resolveErr)
		}
		path = real
		info, err = os.Lstat(path)
		if err != nil {
			return "", err
		}
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("executable %q must be an executable regular file", path)
	}
	return path, nil
}
