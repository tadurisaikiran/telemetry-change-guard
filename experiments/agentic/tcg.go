package agentic

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	maxTCGResultBytes      = 16 << 20
	evaluationControlsName = "controls"
)

type Evaluator interface {
	Identity() ToolIdentity
	Evaluate(context.Context, string, string, string, string, string, time.Duration) (Evaluation, error)
}

type TCGEvaluator struct {
	command     string
	identity    ToolIdentity
	environment []string
	process     processRunner
}

type Evaluation struct {
	Status             string
	ExitCode           int
	Duration           time.Duration
	Command            []string
	Started            bool
	Result             tcgMachineResult
	Stderr             []byte
	ResultArtifact     string
	StatusArtifact     string
	StderrArtifact     string
	AuthoritativeFiles []string
}

func NewTCGEvaluator(command string, environmentNames []string, process processRunner) (*TCGEvaluator, error) {
	if len(environmentNames) > 32 {
		return nil, fmt.Errorf("TCG environment allowlist exceeds 32 entries")
	}
	for _, name := range environmentNames {
		if err := validateEnvironmentName(name); err != nil {
			return nil, err
		}
	}
	environment, err := filteredEnvironment(environmentNames)
	if err != nil {
		return nil, err
	}
	identity, err := toolIdentity(command)
	if err != nil {
		return nil, err
	}
	if process == nil {
		process = execProcessRunner{}
	}
	return &TCGEvaluator{command: identity.Command, identity: identity, environment: environment, process: process}, nil
}

func (evaluator *TCGEvaluator) Identity() ToolIdentity {
	return evaluator.identity
}

func (evaluator *TCGEvaluator) Evaluate(
	ctx context.Context,
	workspaceRoot string,
	trustedRoot string,
	configPath string,
	changesPath string,
	attemptDirectory string,
	timeout time.Duration,
) (Evaluation, error) {
	if current, err := toolIdentity(evaluator.command); err != nil || current.SHA256 != evaluator.identity.SHA256 {
		if err != nil {
			return Evaluation{}, fmt.Errorf("verify TCG executable integrity: %w", err)
		}
		return Evaluation{}, fmt.Errorf("TCG executable integrity changed during run")
	}
	if err := os.MkdirAll(attemptDirectory, 0o700); err != nil {
		return Evaluation{}, fmt.Errorf("create attempt directory: %w", err)
	}
	resultPath := filepath.Join(attemptDirectory, "tcg-result.json")
	statusPath := filepath.Join(attemptDirectory, "tcg-status.txt")
	stderrPath := filepath.Join(attemptDirectory, "tcg-stderr.txt")
	for _, path := range []string{resultPath, statusPath, stderrPath} {
		if _, err := os.Lstat(path); err == nil || !os.IsNotExist(err) {
			return Evaluation{}, fmt.Errorf("TCG artifact path %q already exists or cannot be inspected", path)
		}
	}

	evaluationContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	arguments := []string{
		"check",
		"--config", configPath,
		"--changes", changesPath,
		"--repository-root", trustedRoot,
		"--mode", "enforce",
		"--format", "json",
		"--output", resultPath,
		"--status-output", statusPath,
	}
	processResult, processErr := evaluator.process.Run(evaluationContext, processSpec{
		Command:     evaluator.command,
		Args:        arguments,
		Dir:         workspaceRoot,
		Env:         evaluator.environment,
		StdoutLimit: 64 << 10,
		StderrLimit: 1 << 20,
	})
	evaluation := Evaluation{
		ExitCode:       processResult.ExitCode,
		Duration:       processResult.Duration,
		Command:        append([]string{evaluator.command}, arguments...),
		Started:        true,
		Stderr:         append([]byte(nil), processResult.Stderr...),
		ResultArtifact: resultPath,
		StatusArtifact: statusPath,
		StderrArtifact: stderrPath,
	}
	if err := os.WriteFile(stderrPath, processResult.Stderr, 0o600); err != nil {
		return evaluation, fmt.Errorf("write TCG stderr artifact: %w", err)
	}
	if processErr != nil {
		return evaluation, fmt.Errorf("execute TCG: %w", processErr)
	}
	if len(processResult.Stdout) != 0 {
		return evaluation, fmt.Errorf("TCG wrote unexpected stdout while --output was configured")
	}
	resultContents, resultErr := readRegularFile(resultPath, maxTCGResultBytes)
	statusContents, statusErr := readRegularFile(statusPath, 128)
	if resultErr != nil || statusErr != nil {
		return evaluation, fmt.Errorf(
			"TCG did not produce complete authoritative artifacts (result: %v; status: %v; exit: %d)",
			resultErr,
			statusErr,
			processResult.ExitCode,
		)
	}
	var machine tcgMachineResult
	if err := decodeStrict(resultContents, maxTCGResultBytes, &machine); err != nil {
		return evaluation, fmt.Errorf("decode TCG result: %w", err)
	}
	if machine.SchemaVersion != TCGResultSchemaVersion {
		return evaluation, fmt.Errorf("TCG result schemaVersion must be %q", TCGResultSchemaVersion)
	}
	status := strings.TrimSpace(string(statusContents))
	if status != machine.Status {
		return evaluation, fmt.Errorf("TCG status artifact %q disagrees with JSON status %q", status, machine.Status)
	}
	if expected := tcgExitCode(status); expected != processResult.ExitCode {
		return evaluation, fmt.Errorf("TCG status %s requires exit %d, got %d", status, expected, processResult.ExitCode)
	}
	evaluation.Status = status
	evaluation.Result = machine
	evaluation.AuthoritativeFiles = []string{resultPath, statusPath, stderrPath}
	return evaluation, nil
}

func tcgExitCode(status string) int {
	switch status {
	case "PASS", "WARN":
		return 0
	case "BLOCK":
		return 2
	case "INCOMPLETE":
		return 3
	default:
		return 1
	}
}

// evaluationControls are private siblings of the detached repository. The
// agent receives only its declared repository subtree as a container mount,
// so it cannot read or alter these copies. The common private parent exists
// only so TCG can enforce one narrow --repository-root for controls and the
// repository evidence it evaluates.
type evaluationControls struct {
	directory   string
	ConfigPath  string
	ChangesPath string
	digests     []FileDigest
}

func materializeEvaluationControls(evaluationRoot, configSource, changesSource string) (evaluationControls, error) {
	directory := filepath.Join(evaluationRoot, evaluationControlsName)
	if err := os.Mkdir(directory, 0o700); err != nil {
		return evaluationControls{}, fmt.Errorf("create private TCG control directory: %w", err)
	}
	controls := evaluationControls{
		directory:   directory,
		ConfigPath:  filepath.Join(directory, "config.yaml"),
		ChangesPath: filepath.Join(directory, "changes.yaml"),
	}
	cleanupOnError := func(cause error) (evaluationControls, error) {
		if cleanupErr := controls.Close(); cleanupErr != nil {
			return evaluationControls{}, fmt.Errorf("%v; clean private TCG controls: %w", cause, cleanupErr)
		}
		return evaluationControls{}, cause
	}
	for _, item := range []struct {
		source      string
		destination string
		name        string
	}{
		{source: configSource, destination: controls.ConfigPath, name: "config"},
		{source: changesSource, destination: controls.ChangesPath, name: "changes"},
	} {
		contents, err := readRegularFile(item.source, maxTaskBytes)
		if err != nil {
			return cleanupOnError(fmt.Errorf("read TCG %s control: %w", item.name, err))
		}
		file, err := os.OpenFile(item.destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return cleanupOnError(fmt.Errorf("create private TCG %s control: %w", item.name, err))
		}
		if _, err := file.Write(contents); err != nil {
			_ = file.Close()
			return cleanupOnError(fmt.Errorf("write private TCG %s control: %w", item.name, err))
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			return cleanupOnError(fmt.Errorf("sync private TCG %s control: %w", item.name, err))
		}
		if err := file.Close(); err != nil {
			return cleanupOnError(fmt.Errorf("close private TCG %s control: %w", item.name, err))
		}
	}
	digests, err := digestPaths([]string{directory})
	if err != nil {
		return cleanupOnError(fmt.Errorf("digest private TCG controls: %w", err))
	}
	controls.digests = digests
	return controls, nil
}

func (controls evaluationControls) Verify() error {
	return verifyDigests([]string{controls.directory}, controls.digests)
}

func (controls evaluationControls) Close() error {
	var failures []string
	for _, file := range []string{controls.ConfigPath, controls.ChangesPath} {
		if file == "" {
			continue
		}
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			failures = append(failures, err.Error())
		}
	}
	if controls.directory != "" {
		if err := os.Remove(controls.directory); err != nil && !os.IsNotExist(err) {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) != 0 {
		return fmt.Errorf("remove private TCG controls: %s", strings.Join(failures, "; "))
	}
	return nil
}

func feedbackFromEvaluation(evaluation Evaluation) Feedback {
	feedback := Feedback{AuthoritativeStatus: evaluation.Status}
	for index, finding := range evaluation.Result.Findings {
		if index >= maxFeedbackFindings {
			feedback.Truncated = true
			break
		}
		converted := FeedbackFinding{
			ChangeID:     safeMessage([]byte(finding.Change.ID), 256),
			ConsumerID:   safeMessage([]byte(finding.Consumer.ID), 512),
			ConsumerKind: safeMessage([]byte(finding.Consumer.Kind), 128),
			ConsumerName: safeMessage([]byte(finding.Consumer.Name), 512),
			Impact:       safeMessage([]byte(finding.Impact), 128),
			Criticality:  safeMessage([]byte(finding.Criticality), 64),
			Uncertain:    finding.Uncertain,
			Source:       sanitizeSource(finding.Consumer.Source),
		}
		for pathIndex, path := range finding.Paths {
			if pathIndex >= maxPathsPerFinding {
				feedback.Truncated = true
				break
			}
			var nodes []string
			for nodeIndex, node := range path.Nodes {
				if nodeIndex >= maxPathNodes {
					feedback.Truncated = true
					break
				}
				nodes = append(nodes, safeMessage([]byte(node), 512))
			}
			converted.Paths = append(converted.Paths, nodes)
		}
		feedback.Findings = append(feedback.Findings, converted)
	}
	for index, diagnostic := range evaluation.Result.Diagnostics {
		if index >= maxDiagnostics {
			feedback.Truncated = true
			break
		}
		feedback.Diagnostics = append(feedback.Diagnostics, FeedbackDiagnostic{
			Adapter:  safeMessage([]byte(diagnostic.Adapter), 128),
			Source:   sanitizeSource(diagnostic.Source),
			Message:  redactCommonSecrets(safeMessage([]byte(diagnostic.Message), 1024)),
			Required: diagnostic.Required,
		})
	}
	sort.Slice(feedback.Findings, func(i, j int) bool {
		left, right := feedback.Findings[i], feedback.Findings[j]
		if left.Criticality != right.Criticality {
			return criticalityRank(left.Criticality) > criticalityRank(right.Criticality)
		}
		if left.ChangeID != right.ChangeID {
			return left.ChangeID < right.ChangeID
		}
		return left.ConsumerID < right.ConsumerID
	})
	return feedback
}

func sanitizeSource(source SourceLocation) SourceLocation {
	return SourceLocation{
		File:   safeMessage([]byte(source.File), 1024),
		Line:   source.Line,
		Column: source.Column,
		URL:    redactCommonSecrets(safeMessage([]byte(source.URL), 1024)),
		Repo:   safeMessage([]byte(source.Repo), 1024),
	}
}

func criticalityRank(value string) int {
	switch value {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

var secretAssignmentPattern = regexp.MustCompile(`(?i)(authorization|bearer|token|password|secret|api[_-]?key)(\s*[:=]\s*)([^\s,;]+)`)

func redactCommonSecrets(value string) string {
	return secretAssignmentPattern.ReplaceAllString(value, "$1$2[REDACTED]")
}

// Minimal strict mirror of the public tcg-result/v1alpha1 contract. Fields not
// needed for feedback remain RawMessage values but their enclosing field names
// are still checked by DisallowUnknownFields.
type tcgMachineResult struct {
	SchemaVersion string          `json:"schemaVersion"`
	ChangeSet     tcgChangeSet    `json:"changeSet"`
	Status        string          `json:"status"`
	Findings      []tcgFinding    `json:"findings"`
	Decisions     []tcgDecision   `json:"decisions,omitempty"`
	Diagnostics   []tcgDiagnostic `json:"diagnostics,omitempty"`
	Errors        []string        `json:"errors,omitempty"`
}

type tcgChangeSet struct {
	APIVersion  string      `json:"apiVersion"`
	Kind        string      `json:"kind"`
	Metadata    tcgMetadata `json:"metadata"`
	Description string      `json:"description,omitempty"`
	Changes     []tcgChange `json:"changes"`
}

type tcgMetadata struct {
	Name string `json:"name"`
}

type tcgChange struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Domain   string            `json:"domain"`
	From     tcgSymbol         `json:"from"`
	To       *tcgSymbol        `json:"to,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type tcgSymbol struct {
	Domain string `json:"domain"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Parent string `json:"parent,omitempty"`
}

type tcgFinding struct {
	Change      tcgChange       `json:"change"`
	Consumer    tcgConsumer     `json:"consumer"`
	Impact      string          `json:"impact"`
	Criticality string          `json:"criticality"`
	Uncertain   bool            `json:"uncertain,omitempty"`
	References  json.RawMessage `json:"references,omitempty"`
	Paths       []tcgPath       `json:"paths,omitempty"`
}

type tcgConsumer struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	Name        string         `json:"name"`
	Criticality string         `json:"criticality"`
	Source      SourceLocation `json:"source"`
}

type tcgPath struct {
	Nodes []string `json:"nodes"`
	Edges []string `json:"edges"`
}

type tcgDecision struct {
	ChangeID         string `json:"changeId"`
	ConsumerID       string `json:"consumerId"`
	Impact           string `json:"impact"`
	ConfiguredAction string `json:"configuredAction"`
	EffectiveAction  string `json:"effectiveAction"`
	Reason           string `json:"reason"`
}

type tcgDiagnostic struct {
	Adapter  string         `json:"adapter"`
	Source   SourceLocation `json:"source"`
	Message  string         `json:"message"`
	Required bool           `json:"required"`
}
