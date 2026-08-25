package agentic

import (
	"context"
	"fmt"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	maxAgentStdout = maxAgentResponse
	maxAgentStderr = 1 << 20
)

var imageIDPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type AgentExecution struct {
	Response    AgentResponse
	RawResponse []byte
	Stderr      []byte
	ExitCode    int
	TimedOut    bool
	Duration    time.Duration
	Started     bool
}

type AgentSandbox interface {
	Identity(context.Context) (SandboxIdentity, error)
	Run(context.Context, string, AgentRequest) (AgentExecution, error)
}

type ContainerOptions struct {
	RuntimeCommand string
	Image          string
	AgentCommand   string
	AgentArgs      []string
	Environment    []string
	Network        string
	Memory         string
	CPUs           string
	PIDs           int
	Process        processRunner
}

// ContainerSandbox runs the adapter with a read-only root filesystem, no
// capabilities, no-new-privileges, bounded resources, and exactly one writable
// bind mount. The container runtime is trusted infrastructure; the image and
// agent process are untrusted.
type ContainerSandbox struct {
	options  ContainerOptions
	identity *SandboxIdentity
}

func NewContainerSandbox(options ContainerOptions) (*ContainerSandbox, error) {
	if strings.TrimSpace(options.RuntimeCommand) == "" {
		options.RuntimeCommand = "docker"
	}
	if strings.TrimSpace(options.Image) == "" {
		return nil, fmt.Errorf("agent image is required")
	}
	if !strings.HasPrefix(options.AgentCommand, "/") || len(options.AgentCommand) > 4096 {
		return nil, fmt.Errorf("agent command must be an absolute in-container path")
	}
	if len(options.AgentArgs) > 64 {
		return nil, fmt.Errorf("agent argument count exceeds 64")
	}
	for index, argument := range options.AgentArgs {
		if len(argument) > 4096 || strings.ContainsRune(argument, '\x00') {
			return nil, fmt.Errorf("agent argument %d is invalid", index)
		}
	}
	if len(options.Environment) > 32 {
		return nil, fmt.Errorf("agent environment allowlist exceeds 32 entries")
	}
	seenEnvironment := make(map[string]struct{}, len(options.Environment))
	for _, name := range options.Environment {
		if err := validateEnvironmentName(name); err != nil {
			return nil, err
		}
		if _, exists := seenEnvironment[name]; exists {
			return nil, fmt.Errorf("agent environment variable %q is duplicated", name)
		}
		seenEnvironment[name] = struct{}{}
	}
	sort.Strings(options.Environment)
	if options.Network == "" {
		options.Network = "none"
	}
	if options.Network != "none" && options.Network != "bridge" {
		return nil, fmt.Errorf("agent network must be none or bridge")
	}
	if options.Memory == "" {
		options.Memory = "1g"
	}
	if options.CPUs == "" {
		options.CPUs = "1"
	}
	if options.PIDs == 0 {
		options.PIDs = 128
	}
	if options.PIDs < 16 || options.PIDs > 1024 {
		return nil, fmt.Errorf("agent PID limit must be between 16 and 1024")
	}
	if options.Process == nil {
		options.Process = execProcessRunner{}
	}
	return &ContainerSandbox{options: options}, nil
}

func (sandbox *ContainerSandbox) Identity(ctx context.Context) (SandboxIdentity, error) {
	if sandbox.identity != nil {
		return cloneSandboxIdentity(*sandbox.identity), nil
	}
	runtimePath, err := filepath.Abs(sandbox.options.RuntimeCommand)
	if err != nil {
		return SandboxIdentity{}, fmt.Errorf("resolve container runtime: %w", err)
	}
	if !filepath.IsAbs(sandbox.options.RuntimeCommand) {
		if found, lookupErr := executablePath(sandbox.options.RuntimeCommand); lookupErr == nil {
			runtimePath = found
		} else {
			return SandboxIdentity{}, lookupErr
		}
	}
	environment, err := filteredEnvironment(sandbox.options.Environment)
	if err != nil {
		return SandboxIdentity{}, err
	}
	inspectContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, err := sandbox.options.Process.Run(inspectContext, processSpec{
		Command:     runtimePath,
		Args:        []string{"image", "inspect", "--format", "{{.Id}}", sandbox.options.Image},
		Env:         environment,
		StdoutLimit: 4096,
		StderrLimit: 64 << 10,
	})
	if err != nil {
		return SandboxIdentity{}, fmt.Errorf("inspect agent image: %w", err)
	}
	if result.ExitCode != 0 {
		return SandboxIdentity{}, fmt.Errorf("inspect agent image exited %d: %s", result.ExitCode, safeMessage(result.Stderr, 2048))
	}
	imageID := strings.TrimSpace(string(result.Stdout))
	if !imageIDPattern.MatchString(imageID) {
		return SandboxIdentity{}, fmt.Errorf("container runtime returned invalid immutable image ID %q", safeMessage([]byte(imageID), 128))
	}
	identity := SandboxIdentity{
		RuntimeCommand: runtimePath,
		ImageReference: sandbox.options.Image,
		ImageID:        imageID,
		AgentCommand:   sandbox.options.AgentCommand,
		AgentArgs:      append([]string(nil), sandbox.options.AgentArgs...),
		Network:        sandbox.options.Network,
	}
	sandbox.identity = &identity
	return cloneSandboxIdentity(identity), nil
}

func (sandbox *ContainerSandbox) Run(ctx context.Context, workspace string, request AgentRequest) (AgentExecution, error) {
	identity, err := sandbox.Identity(ctx)
	if err != nil {
		return AgentExecution{}, err
	}
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		return AgentExecution{}, fmt.Errorf("resolve workspace: %w", err)
	}
	if strings.Contains(workspace, ",") {
		return AgentExecution{}, fmt.Errorf("workspace path cannot contain a comma because the container mount syntax is unambiguous by design")
	}
	requestContents, err := EncodeAgentRequest(request)
	if err != nil {
		return AgentExecution{}, err
	}
	environment, err := filteredEnvironment(sandbox.options.Environment)
	if err != nil {
		return AgentExecution{}, err
	}
	currentUser, err := user.Current()
	if err != nil {
		return AgentExecution{}, fmt.Errorf("resolve unprivileged container user: %w", err)
	}
	if currentUser.Uid == "" || currentUser.Gid == "" {
		return AgentExecution{}, fmt.Errorf("resolve unprivileged container user: UID and GID are required")
	}

	arguments := []string{
		"run",
		"--rm",
		"--interactive",
		"--pull", "never",
		"--read-only",
		"--network", sandbox.options.Network,
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--pids-limit", fmt.Sprintf("%d", sandbox.options.PIDs),
		"--memory", sandbox.options.Memory,
		"--cpus", sandbox.options.CPUs,
		"--user", currentUser.Uid + ":" + currentUser.Gid,
		"--hostname", "tcg-agent",
		"--workdir", "/workspace",
		// Docker bind mounts are writable by default. Keeping the long syntax to
		// key=value fields preserves compatibility with runtimes that reject the
		// legacy bare `rw` token.
		"--mount", "type=bind,src=" + workspace + ",dst=/workspace",
		"--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=67108864",
	}
	for _, name := range sandbox.options.Environment {
		arguments = append(arguments, "--env", name)
	}
	arguments = append(arguments, identity.ImageID, sandbox.options.AgentCommand)
	arguments = append(arguments, sandbox.options.AgentArgs...)

	result, runErr := sandbox.options.Process.Run(ctx, processSpec{
		Command:     identity.RuntimeCommand,
		Args:        arguments,
		Env:         environment,
		Stdin:       requestContents,
		StdoutLimit: maxAgentStdout,
		StderrLimit: maxAgentStderr,
	})
	execution := AgentExecution{
		RawResponse: append([]byte(nil), result.Stdout...),
		Stderr:      append([]byte(nil), result.Stderr...),
		ExitCode:    result.ExitCode,
		TimedOut:    result.TimedOut,
		Duration:    result.Duration,
		Started:     true,
	}
	if runErr != nil {
		return execution, fmt.Errorf("run sandboxed agent: %w", runErr)
	}
	if result.ExitCode != 0 {
		return execution, fmt.Errorf("sandboxed agent exited %d", result.ExitCode)
	}
	response, err := DecodeAgentResponse(result.Stdout)
	if err != nil {
		return execution, fmt.Errorf("decode sandboxed agent response: %w", err)
	}
	execution.Response = response
	return execution, nil
}

func cloneSandboxIdentity(identity SandboxIdentity) SandboxIdentity {
	identity.AgentArgs = append([]string(nil), identity.AgentArgs...)
	return identity
}
