// Package agentic implements the experimental, bounded coding-agent feedback
// loop. It deliberately consumes Telemetry Change Guard through its public
// executable and machine contracts instead of importing internal packages.
package agentic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	TaskSchemaVersion          = "tcg-agent-task/v1alpha1"
	AgentRequestSchemaVersion  = "tcg-agent-request/v1alpha1"
	AgentResponseSchemaVersion = "tcg-agent-response/v1alpha1"
	AttemptResultSchemaVersion = "tcg-agent-attempt/v1alpha1"
	RunResultSchemaVersion     = "tcg-agent-run-result/v1alpha1"
	TCGResultSchemaVersion     = "tcg-result/v1alpha1"

	maxTaskBytes        = 1 << 20
	maxAgentRequest     = 1 << 20
	maxAgentResponse    = 1 << 20
	maxTextBytes        = 4096
	maxFeedbackFindings = 256
	maxDiagnostics      = 128
	maxPathsPerFinding  = 16
	maxPathNodes        = 64
	maxLimitations      = 32
)

var (
	idPattern          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	environmentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Task is a strict, versioned description of one experimental run. Paths in
// Repository and TCG are resolved relative to the task document.
type Task struct {
	SchemaVersion  string         `json:"schemaVersion"`
	ID             string         `json:"id"`
	Description    string         `json:"description"`
	Repository     RepositorySpec `json:"repository"`
	AgentWorkspace string         `json:"agentWorkspace"`
	TCG            TCGSpec        `json:"tcg"`
	IntegrityPaths []string       `json:"integrityPaths,omitempty"`
	Limits         LimitsSpec     `json:"limits,omitempty"`
}

type RepositorySpec struct {
	Path     string `json:"path"`
	Revision string `json:"revision"`
}

// TCGSpec intentionally starts with the explicit ChangeSet workflow. Snapshot
// and mapped-Weaver sources can be added only with equally strict schemas.
type TCGSpec struct {
	Config  string `json:"config"`
	Changes string `json:"changes"`
	Timeout string `json:"timeout,omitempty"`
}

type LimitsSpec struct {
	MaxAttempts     int    `json:"maxAttempts,omitempty"`
	AgentTimeout    string `json:"agentTimeout,omitempty"`
	TotalTimeout    string `json:"totalTimeout,omitempty"`
	MaxChangedFiles int    `json:"maxChangedFiles,omitempty"`
	MaxDiffBytes    int64  `json:"maxDiffBytes,omitempty"`
}

// ResolvedTask contains validated absolute control paths and parsed limits.
// The repository worktree itself is created later at an immutable commit.
type ResolvedTask struct {
	Task
	TaskPath          string
	RepositoryPath    string
	ConfigPath        string
	ChangesPath       string
	IntegrityAbsPaths []string
	AgentTimeout      time.Duration
	TCGTimeout        time.Duration
	TotalTimeout      time.Duration
	MaxAttempts       int
	MaxChangedFiles   int
	MaxDiffBytes      int64
}

// AgentRequest is the only input sent to the untrusted adapter.
type AgentRequest struct {
	SchemaVersion string            `json:"schemaVersion"`
	Task          AgentTask         `json:"task"`
	Attempt       int               `json:"attempt"`
	Workspace     string            `json:"workspace"`
	Guardrails    []string          `json:"guardrails"`
	Feedback      *Feedback         `json:"feedback,omitempty"`
	Context       map[string]string `json:"context,omitempty"`
}

type AgentTask struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// AgentResponse contains narrative claims only. It cannot carry a status,
// command, patch, approval, or mutation instruction.
type AgentResponse struct {
	SchemaVersion string   `json:"schemaVersion"`
	Summary       string   `json:"summary"`
	ChangedFiles  []string `json:"changedFiles,omitempty"`
	Limitations   []string `json:"limitations,omitempty"`
}

type Feedback struct {
	AuthoritativeStatus string               `json:"authoritativeStatus"`
	Findings            []FeedbackFinding    `json:"findings,omitempty"`
	Diagnostics         []FeedbackDiagnostic `json:"diagnostics,omitempty"`
	Truncated           bool                 `json:"truncated,omitempty"`
}

type FeedbackFinding struct {
	ChangeID     string         `json:"changeId"`
	ConsumerID   string         `json:"consumerId"`
	ConsumerKind string         `json:"consumerKind"`
	ConsumerName string         `json:"consumerName"`
	Impact       string         `json:"impact"`
	Criticality  string         `json:"criticality"`
	Uncertain    bool           `json:"uncertain,omitempty"`
	Source       SourceLocation `json:"source"`
	Paths        [][]string     `json:"dependencyPaths,omitempty"`
}

type FeedbackDiagnostic struct {
	Adapter  string         `json:"adapter"`
	Source   SourceLocation `json:"source"`
	Message  string         `json:"message"`
	Required bool           `json:"required"`
}

type SourceLocation struct {
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
	URL    string `json:"url,omitempty"`
	Repo   string `json:"repo,omitempty"`
}

type Outcome string

const (
	OutcomeReviewReady     Outcome = "REVIEW_READY"
	OutcomeBlocked         Outcome = "BLOCKED"
	OutcomeIncomplete      Outcome = "INCOMPLETE"
	OutcomeError           Outcome = "ERROR"
	OutcomeAgentFailure    Outcome = "AGENT_FAILURE"
	OutcomeIntegrityFailed Outcome = "INTEGRITY_FAILURE"
)

type RunResult struct {
	SchemaVersion       string          `json:"schemaVersion"`
	TaskID              string          `json:"taskId"`
	RepositoryCommit    string          `json:"repositoryCommit"`
	Outcome             Outcome         `json:"outcome"`
	AuthoritativeStatus string          `json:"authoritativeStatus,omitempty"`
	StartedAt           string          `json:"startedAt"`
	FinishedAt          string          `json:"finishedAt"`
	TCG                 ToolIdentity    `json:"tcg"`
	Sandbox             SandboxIdentity `json:"sandbox"`
	ControlDigests      []FileDigest    `json:"controlDigests"`
	Attempts            []AttemptResult `json:"attempts"`
	FinalChangedFiles   []string        `json:"finalChangedFiles,omitempty"`
	FinalDiffArtifact   string          `json:"finalDiffArtifact,omitempty"`
	Error               string          `json:"error,omitempty"`
}

type ToolIdentity struct {
	Command     string `json:"command"`
	SHA256      string `json:"sha256"`
	ModulePath  string `json:"modulePath,omitempty"`
	Module      string `json:"moduleVersion,omitempty"`
	VCSRevision string `json:"vcsRevision,omitempty"`
	VCSModified bool   `json:"vcsModified,omitempty"`
}

type SandboxIdentity struct {
	RuntimeCommand string   `json:"runtimeCommand"`
	ImageReference string   `json:"imageReference"`
	ImageID        string   `json:"imageId"`
	AgentCommand   string   `json:"agentCommand"`
	AgentArgs      []string `json:"agentArgs,omitempty"`
	Network        string   `json:"network"`
}

type FileDigest struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type AttemptResult struct {
	SchemaVersion        string   `json:"schemaVersion"`
	Number               int      `json:"number"`
	StartedAt            string   `json:"startedAt"`
	FinishedAt           string   `json:"finishedAt"`
	AgentResponse        string   `json:"agentResponse,omitempty"`
	AgentStderr          string   `json:"agentStderr,omitempty"`
	AgentExitCode        *int     `json:"agentExitCode,omitempty"`
	AgentTimedOut        bool     `json:"agentTimedOut,omitempty"`
	AgentDuration        string   `json:"agentDuration"`
	ReportedChangedFiles []string `json:"reportedChangedFiles,omitempty"`
	ActualChangedFiles   []string `json:"actualChangedFiles,omitempty"`
	TCGStatus            string   `json:"tcgStatus,omitempty"`
	TCGExitCode          *int     `json:"tcgExitCode,omitempty"`
	TCGDuration          string   `json:"tcgDuration,omitempty"`
	TCGCommand           []string `json:"tcgCommand,omitempty"`
	TCGResultArtifact    string   `json:"tcgResultArtifact,omitempty"`
	TCGStatusArtifact    string   `json:"tcgStatusArtifact,omitempty"`
	TCGStderrArtifact    string   `json:"tcgStderrArtifact,omitempty"`
	DiffArtifact         string   `json:"diffArtifact,omitempty"`
	Error                string   `json:"error,omitempty"`
}

func LoadTask(path string) (ResolvedTask, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return ResolvedTask{}, fmt.Errorf("resolve task path: %w", err)
	}
	contents, err := readRegularFile(absPath, maxTaskBytes)
	if err != nil {
		return ResolvedTask{}, fmt.Errorf("read task: %w", err)
	}
	var task Task
	if err := decodeStrict(contents, maxTaskBytes, &task); err != nil {
		return ResolvedTask{}, fmt.Errorf("decode task: %w", err)
	}
	if err := validateTask(task); err != nil {
		return ResolvedTask{}, err
	}

	base := filepath.Dir(absPath)
	resolved := ResolvedTask{Task: task, TaskPath: absPath}
	resolved.RepositoryPath, err = resolveExistingDirectory(base, task.Repository.Path)
	if err != nil {
		return ResolvedTask{}, fmt.Errorf("repository.path: %w", err)
	}
	resolved.ConfigPath, err = resolveRegularPath(base, task.TCG.Config, maxTaskBytes)
	if err != nil {
		return ResolvedTask{}, fmt.Errorf("tcg.config: %w", err)
	}
	resolved.ChangesPath, err = resolveRegularPath(base, task.TCG.Changes, maxTaskBytes)
	if err != nil {
		return ResolvedTask{}, fmt.Errorf("tcg.changes: %w", err)
	}

	paths := append([]string(nil), task.IntegrityPaths...)
	paths = append(paths, task.TCG.Config, task.TCG.Changes, filepath.Base(absPath))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		candidate, resolveErr := resolveIntegrityPath(base, path)
		if resolveErr != nil {
			return ResolvedTask{}, fmt.Errorf("integrity path %q: %w", path, resolveErr)
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		resolved.IntegrityAbsPaths = append(resolved.IntegrityAbsPaths, candidate)
	}
	sort.Strings(resolved.IntegrityAbsPaths)

	resolved.MaxAttempts = defaultInt(task.Limits.MaxAttempts, 3)
	resolved.MaxChangedFiles = defaultInt(task.Limits.MaxChangedFiles, 256)
	resolved.MaxDiffBytes = task.Limits.MaxDiffBytes
	if resolved.MaxDiffBytes == 0 {
		resolved.MaxDiffBytes = 4 << 20
	}
	resolved.AgentTimeout, err = parseBoundedDuration(task.Limits.AgentTimeout, 2*time.Minute, 10*time.Minute, "limits.agentTimeout")
	if err != nil {
		return ResolvedTask{}, err
	}
	resolved.TCGTimeout, err = parseBoundedDuration(task.TCG.Timeout, 2*time.Minute, 10*time.Minute, "tcg.timeout")
	if err != nil {
		return ResolvedTask{}, err
	}
	resolved.TotalTimeout, err = parseBoundedDuration(task.Limits.TotalTimeout, 15*time.Minute, 30*time.Minute, "limits.totalTimeout")
	if err != nil {
		return ResolvedTask{}, err
	}
	return resolved, nil
}

func validateTask(task Task) error {
	if task.SchemaVersion != TaskSchemaVersion {
		return fmt.Errorf("task schemaVersion must be %q", TaskSchemaVersion)
	}
	if !idPattern.MatchString(task.ID) {
		return fmt.Errorf("task id is invalid")
	}
	if strings.TrimSpace(task.Description) == "" || len(task.Description) > maxTextBytes || !utf8.ValidString(task.Description) {
		return fmt.Errorf("task description must contain 1 to %d valid UTF-8 bytes", maxTextBytes)
	}
	if strings.TrimSpace(task.Repository.Path) == "" || strings.TrimSpace(task.Repository.Revision) == "" || len(task.Repository.Revision) > 256 {
		return fmt.Errorf("repository path and revision are required")
	}
	if err := validateRelativePath(task.AgentWorkspace, "agentWorkspace"); err != nil {
		return err
	}
	if filepath.Clean(task.AgentWorkspace) == "." {
		return fmt.Errorf("agentWorkspace must be a repository subtree, not the repository root")
	}
	if strings.TrimSpace(task.TCG.Config) == "" || strings.TrimSpace(task.TCG.Changes) == "" {
		return fmt.Errorf("tcg config and changes paths are required")
	}
	if task.Limits.MaxAttempts < 0 || task.Limits.MaxAttempts > 3 {
		return fmt.Errorf("limits.maxAttempts must be between 1 and 3 when set")
	}
	if task.Limits.MaxChangedFiles < 0 || task.Limits.MaxChangedFiles > 2048 {
		return fmt.Errorf("limits.maxChangedFiles must be between 1 and 2048 when set")
	}
	if task.Limits.MaxDiffBytes < 0 || task.Limits.MaxDiffBytes > 16<<20 {
		return fmt.Errorf("limits.maxDiffBytes must be between 1 and %d when set", 16<<20)
	}
	if len(task.IntegrityPaths) > 256 {
		return fmt.Errorf("integrityPaths exceeds the 256-entry limit")
	}
	for index, path := range task.IntegrityPaths {
		if strings.TrimSpace(path) == "" || len(path) > 4096 {
			return fmt.Errorf("integrityPaths[%d] is invalid", index)
		}
	}
	return nil
}

func EncodeAgentRequest(request AgentRequest) ([]byte, error) {
	if request.SchemaVersion != AgentRequestSchemaVersion || request.Attempt < 1 || request.Workspace != "/workspace" {
		return nil, fmt.Errorf("invalid agent request envelope")
	}
	contents, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode agent request: %w", err)
	}
	if len(contents) > maxAgentRequest {
		return nil, fmt.Errorf("agent request exceeds the %d-byte limit", maxAgentRequest)
	}
	return append(contents, '\n'), nil
}

func DecodeAgentResponse(contents []byte) (AgentResponse, error) {
	var response AgentResponse
	if err := decodeStrict(contents, maxAgentResponse, &response); err != nil {
		return AgentResponse{}, err
	}
	if response.SchemaVersion != AgentResponseSchemaVersion {
		return AgentResponse{}, fmt.Errorf("agent response schemaVersion must be %q", AgentResponseSchemaVersion)
	}
	if len(response.Summary) > maxTextBytes || !utf8.ValidString(response.Summary) {
		return AgentResponse{}, fmt.Errorf("agent response summary exceeds its limit or is invalid UTF-8")
	}
	if len(response.ChangedFiles) > 512 {
		return AgentResponse{}, fmt.Errorf("agent response contains too many changedFiles")
	}
	seen := make(map[string]struct{}, len(response.ChangedFiles))
	for index, path := range response.ChangedFiles {
		if err := validateRelativePath(path, fmt.Sprintf("changedFiles[%d]", index)); err != nil {
			return AgentResponse{}, err
		}
		if _, exists := seen[path]; exists {
			return AgentResponse{}, fmt.Errorf("agent response changedFiles contains duplicate %q", path)
		}
		seen[path] = struct{}{}
	}
	if len(response.Limitations) > maxLimitations {
		return AgentResponse{}, fmt.Errorf("agent response contains too many limitations")
	}
	for index, limitation := range response.Limitations {
		if len(limitation) > 1024 || !utf8.ValidString(limitation) {
			return AgentResponse{}, fmt.Errorf("agent response limitation %d is invalid", index)
		}
	}
	return response, nil
}

func decodeStrict(contents []byte, limit int64, target any) error {
	if int64(len(contents)) > limit {
		return fmt.Errorf("document exceeds the %d-byte limit", limit)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return fmt.Errorf("document must contain exactly one JSON value")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return nil
}

func validateRelativePath(path, field string) error {
	if path == "" || len(path) > 4096 || !utf8.ValidString(path) || filepath.IsAbs(path) || strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("%s must be a nonempty relative path", field)
	}
	clean := filepath.Clean(path)
	if clean != path {
		return fmt.Errorf("%s must use a canonical relative path", field)
	}
	for _, character := range path {
		if unicode.IsControl(character) {
			return fmt.Errorf("%s must not contain control characters", field)
		}
	}
	if clean == "." {
		return nil
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s must not escape its root", field)
	}
	return nil
}

func validateEnvironmentName(name string) error {
	if !environmentPattern.MatchString(name) {
		return fmt.Errorf("environment variable name %q is invalid", name)
	}
	return nil
}

func parseBoundedDuration(value string, fallback, maximum time.Duration, field string) (time.Duration, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", field, err)
	}
	if parsed <= 0 || parsed > maximum {
		return 0, fmt.Errorf("%s must be positive and no greater than %s", field, maximum)
	}
	return parsed, nil
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func resolveExistingDirectory(base, path string) (string, error) {
	resolved := resolvePath(base, path)
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", resolved)
	}
	real, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", err
	}
	return filepath.Abs(real)
}

func resolveRegularPath(base, path string, maxBytes int64) (string, error) {
	resolved := resolvePath(base, path)
	if _, err := readRegularFile(resolved, maxBytes); err != nil {
		return "", err
	}
	return filepath.Abs(resolved)
}

func resolveIntegrityPath(base, path string) (string, error) {
	resolved := resolvePath(base, path)
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("symlinks are not permitted")
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return "", fmt.Errorf("path is neither a regular file nor a directory")
	}
	return filepath.Abs(resolved)
}

func resolvePath(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(base, path))
}

func readRegularFile(path string, maxBytes int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%q must be a regular non-symlink file", path)
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("%q exceeds the %d-byte limit", path, maxBytes)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return contents, nil
}
