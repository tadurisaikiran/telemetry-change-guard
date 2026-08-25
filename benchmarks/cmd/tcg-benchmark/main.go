// Command tcg-benchmark executes the versioned release-gate corpus and emits
// a machine-readable result. The corpus is regression evidence, not an
// independent measurement of field accuracy.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	manifestSchema = "tcg-benchmark/v1alpha1"
	resultSchema   = "tcg-benchmark-result/v1alpha1"
	maxInputBytes  = 1 << 20
	maxReportBytes = 8 << 20
	maxLogBytes    = 1 << 20
)

var caseIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type manifest struct {
	SchemaVersion string      `json:"schemaVersion"`
	CorpusVersion string      `json:"corpusVersion"`
	Cases         []benchmark `json:"cases"`
}

type benchmark struct {
	ID          string      `json:"id"`
	Description string      `json:"description"`
	Feature     string      `json:"feature"`
	Source      string      `json:"source"`
	License     string      `json:"license"`
	GroundTruth string      `json:"groundTruth"`
	Critical    bool        `json:"critical"`
	Command     []string    `json:"command"`
	Expected    expectation `json:"expected"`
}

type expectation struct {
	SchemaVersion           string   `json:"schemaVersion"`
	Status                  string   `json:"status"`
	ExitCode                int      `json:"exitCode"`
	FindingCount            *int     `json:"findingCount,omitempty"`
	Impacts                 []string `json:"impacts,omitempty"`
	DiagnosticCount         *int     `json:"diagnosticCount,omitempty"`
	ConsumerClassifications []string `json:"consumerClassifications,omitempty"`
}

type observation struct {
	SchemaVersion           string   `json:"schemaVersion"`
	Status                  string   `json:"status"`
	ExitCode                int      `json:"exitCode"`
	FindingCount            int      `json:"findingCount"`
	Impacts                 []string `json:"impacts,omitempty"`
	DiagnosticCount         int      `json:"diagnosticCount"`
	ConsumerClassifications []string `json:"consumerClassifications,omitempty"`
}

type result struct {
	SchemaVersion string       `json:"schemaVersion"`
	CorpusVersion string       `json:"corpusVersion"`
	Revision      string       `json:"revision"`
	Dirty         bool         `json:"dirty"`
	GeneratedAt   string       `json:"generatedAt"`
	Environment   environment  `json:"environment"`
	Summary       summary      `json:"summary"`
	Cases         []caseResult `json:"cases"`
}

type environment struct {
	GoVersion string `json:"goVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

type summary struct {
	Total                  int `json:"total"`
	Passed                 int `json:"passed"`
	Failed                 int `json:"failed"`
	CriticalFalseNegatives int `json:"criticalFalseNegatives"`
	FalsePositives         int `json:"falsePositives"`
}

type caseResult struct {
	ID              string      `json:"id"`
	Feature         string      `json:"feature"`
	Source          string      `json:"source"`
	License         string      `json:"license"`
	GroundTruth     string      `json:"groundTruth"`
	Critical        bool        `json:"critical"`
	Expected        expectation `json:"expected"`
	Actual          observation `json:"actual"`
	Passed          bool        `json:"passed"`
	Mismatches      []string    `json:"mismatches,omitempty"`
	DurationMS      float64     `json:"durationMs"`
	PeakMemoryBytes *uint64     `json:"peakMemoryBytes"`
	StandardError   string      `json:"standardError,omitempty"`
}

func main() {
	manifestPath := flag.String("manifest", "benchmarks/manifest/corpus.json", "benchmark manifest")
	binaryPath := flag.String("binary", "", "telemetry-change-guard executable")
	outputPath := flag.String("output", "dist/benchmark/results.json", "machine-readable result path")
	rootPath := flag.String("root", ".", "repository root")
	flag.Parse()
	if flag.NArg() != 0 || *binaryPath == "" {
		fatal(errors.New("usage: tcg-benchmark --binary <path> [--manifest <path>] [--output <path>] [--root <path>]"))
	}

	root, err := filepath.Abs(*rootPath)
	if err != nil {
		fatal(err)
	}
	loaded, err := loadManifest(resolve(root, *manifestPath))
	if err != nil {
		fatal(err)
	}
	binary, err := filepath.Abs(*binaryPath)
	if err != nil {
		fatal(err)
	}
	if info, err := os.Stat(binary); err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		fatal(fmt.Errorf("benchmark binary is not an executable regular file: %s", binary))
	}

	revision, dirty := repositoryIdentity(root)
	output := result{
		SchemaVersion: resultSchema,
		CorpusVersion: loaded.CorpusVersion,
		Revision:      revision,
		Dirty:         dirty,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Environment: environment{
			GoVersion: runtime.Version(),
			OS:        runtime.GOOS,
			Arch:      runtime.GOARCH,
		},
		Summary: summary{Total: len(loaded.Cases)},
	}

	for _, item := range loaded.Cases {
		caseResult := runCase(root, binary, item)
		output.Cases = append(output.Cases, caseResult)
		if caseResult.Passed {
			output.Summary.Passed++
		} else {
			output.Summary.Failed++
		}
		if item.Critical && isSafe(caseResult.Actual.Status) && !isSafe(item.Expected.Status) {
			output.Summary.CriticalFalseNegatives++
		}
		if isSafe(item.Expected.Status) && isBlocking(caseResult.Actual.Status) {
			output.Summary.FalsePositives++
		}
	}

	if err := writeResult(resolve(root, *outputPath), output); err != nil {
		fatal(err)
	}
	fmt.Printf(
		"Benchmark corpus %s: %d/%d cases passed; %d critical false negatives; %d false positives\n",
		output.CorpusVersion,
		output.Summary.Passed,
		output.Summary.Total,
		output.Summary.CriticalFalseNegatives,
		output.Summary.FalsePositives,
	)
	if output.Summary.Failed != 0 {
		os.Exit(1)
	}
}

func loadManifest(file string) (manifest, error) {
	data, err := readLimited(file, maxInputBytes)
	if err != nil {
		return manifest{}, err
	}
	var document manifest
	if err := decodeStrict(data, &document); err != nil {
		return manifest{}, fmt.Errorf("decode benchmark manifest: %w", err)
	}
	if document.SchemaVersion != manifestSchema || document.CorpusVersion == "" || len(document.Cases) == 0 {
		return manifest{}, errors.New("benchmark manifest has an unsupported schema, empty version, or no cases")
	}
	seen := map[string]struct{}{}
	previous := ""
	for _, item := range document.Cases {
		if !caseIDPattern.MatchString(item.ID) || item.ID <= previous {
			return manifest{}, errors.New("benchmark case IDs must be unique, kebab-case, and sorted")
		}
		previous = item.ID
		if _, duplicate := seen[item.ID]; duplicate {
			return manifest{}, fmt.Errorf("duplicate benchmark case %s", item.ID)
		}
		seen[item.ID] = struct{}{}
		if item.Description == "" || item.Feature == "" || item.Source == "" || item.License == "" || item.GroundTruth == "" {
			return manifest{}, fmt.Errorf("benchmark case %s has incomplete provenance", item.ID)
		}
		if err := validateCommand(item.Command); err != nil {
			return manifest{}, fmt.Errorf("benchmark case %s: %w", item.ID, err)
		}
		if item.Expected.SchemaVersion == "" || item.Expected.Status == "" || item.Expected.ExitCode < 0 || item.Expected.ExitCode > 3 {
			return manifest{}, fmt.Errorf("benchmark case %s has an invalid expectation", item.ID)
		}
	}
	return document, nil
}

func validateCommand(arguments []string) error {
	if len(arguments) == 0 {
		return errors.New("command is empty")
	}
	if arguments[0] != "check" && !(len(arguments) >= 2 && arguments[0] == "migration" && arguments[1] == "check") {
		return errors.New("only check and migration check commands are permitted")
	}
	for _, argument := range arguments {
		if argument == "--output" || argument == "--json-output" || argument == "--status-output" || argument == "--format" {
			return fmt.Errorf("runner-owned output argument %q is forbidden", argument)
		}
	}
	return nil
}

func runCase(root, binary string, item benchmark) caseResult {
	caseOutput := caseResult{
		ID:          item.ID,
		Feature:     item.Feature,
		Source:      item.Source,
		License:     item.License,
		GroundTruth: item.GroundTruth,
		Critical:    item.Critical,
		Expected:    item.Expected,
		Actual:      observation{ExitCode: -1},
	}
	temporary, err := os.MkdirTemp("", "tcg-benchmark-"+item.ID+".")
	if err != nil {
		caseOutput.Mismatches = []string{err.Error()}
		return caseOutput
	}
	defer os.RemoveAll(temporary)
	reportPath := filepath.Join(temporary, "result.json")
	arguments := append([]string{}, item.Command...)
	arguments = append(arguments, "--format", "json", "--output", reportPath)
	command := exec.Command(binary, arguments...)
	command.Dir = root
	stdout := &cappedBuffer{limit: maxLogBytes}
	stderr := &cappedBuffer{limit: maxLogBytes}
	command.Stdout = stdout
	command.Stderr = stderr

	started := time.Now()
	err = command.Run()
	caseOutput.DurationMS = float64(time.Since(started).Microseconds()) / 1000
	caseOutput.PeakMemoryBytes = peakMemoryBytes(command.ProcessState)
	caseOutput.StandardError = strings.TrimSpace(stderr.String())
	caseOutput.Actual.ExitCode = exitCode(err)
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			caseOutput.Mismatches = []string{fmt.Sprintf("execute benchmark: %v", err)}
			return caseOutput
		}
	}

	data, readErr := readLimited(reportPath, maxReportBytes)
	if readErr != nil {
		caseOutput.Mismatches = []string{fmt.Sprintf("read machine result: %v", readErr)}
		return caseOutput
	}
	actual, inspectErr := inspectReport(data, caseOutput.Actual.ExitCode)
	if inspectErr != nil {
		caseOutput.Mismatches = []string{fmt.Sprintf("inspect machine result: %v", inspectErr)}
		return caseOutput
	}
	caseOutput.Actual = actual
	caseOutput.Mismatches = compare(item.Expected, actual)
	caseOutput.Passed = len(caseOutput.Mismatches) == 0
	return caseOutput
}

func inspectReport(data []byte, exit int) (observation, error) {
	var envelope struct {
		SchemaVersion string `json:"schemaVersion"`
	}
	if err := decodeStrict(data, &envelope); err != nil {
		// Strict decoding into the narrow envelope would reject product fields.
		var loose map[string]json.RawMessage
		if json.Unmarshal(data, &loose) != nil || json.Unmarshal(loose["schemaVersion"], &envelope.SchemaVersion) != nil {
			return observation{}, errors.New("result has no valid schemaVersion")
		}
	}
	actual := observation{SchemaVersion: envelope.SchemaVersion, ExitCode: exit}
	switch envelope.SchemaVersion {
	case "tcg-result/v1alpha1":
		var document struct {
			Status   string `json:"status"`
			Findings []struct {
				Impact string `json:"impact"`
			} `json:"findings"`
			Diagnostics []json.RawMessage `json:"diagnostics"`
		}
		if err := json.Unmarshal(data, &document); err != nil {
			return observation{}, err
		}
		actual.Status = document.Status
		actual.FindingCount = len(document.Findings)
		actual.DiagnosticCount = len(document.Diagnostics)
		for _, finding := range document.Findings {
			actual.Impacts = append(actual.Impacts, finding.Impact)
		}
	case "tmr-result/v1alpha1":
		var document struct {
			Summary struct {
				Status string `json:"status"`
			} `json:"summary"`
			Changes []struct {
				Consumers []struct {
					Classification string `json:"classification"`
				} `json:"consumers"`
			} `json:"changes"`
			Diagnostics []json.RawMessage `json:"diagnostics"`
		}
		if err := json.Unmarshal(data, &document); err != nil {
			return observation{}, err
		}
		actual.Status = document.Summary.Status
		actual.DiagnosticCount = len(document.Diagnostics)
		for _, change := range document.Changes {
			for _, consumer := range change.Consumers {
				actual.ConsumerClassifications = append(actual.ConsumerClassifications, consumer.Classification)
			}
		}
	default:
		return observation{}, fmt.Errorf("unsupported result schema %q", envelope.SchemaVersion)
	}
	sort.Strings(actual.Impacts)
	sort.Strings(actual.ConsumerClassifications)
	return actual, nil
}

func compare(expected expectation, actual observation) []string {
	var mismatches []string
	if expected.SchemaVersion != actual.SchemaVersion {
		mismatches = append(mismatches, fmt.Sprintf("schema=%s; want %s", actual.SchemaVersion, expected.SchemaVersion))
	}
	if expected.Status != actual.Status {
		mismatches = append(mismatches, fmt.Sprintf("status=%s; want %s", actual.Status, expected.Status))
	}
	if expected.ExitCode != actual.ExitCode {
		mismatches = append(mismatches, fmt.Sprintf("exit=%d; want %d", actual.ExitCode, expected.ExitCode))
	}
	if expected.FindingCount != nil && *expected.FindingCount != actual.FindingCount {
		mismatches = append(mismatches, fmt.Sprintf("findings=%d; want %d", actual.FindingCount, *expected.FindingCount))
	}
	if expected.DiagnosticCount != nil && *expected.DiagnosticCount != actual.DiagnosticCount {
		mismatches = append(mismatches, fmt.Sprintf("diagnostics=%d; want %d", actual.DiagnosticCount, *expected.DiagnosticCount))
	}
	wantImpacts := append([]string{}, expected.Impacts...)
	sort.Strings(wantImpacts)
	if !equalStrings(wantImpacts, actual.Impacts) {
		mismatches = append(mismatches, fmt.Sprintf("impacts=%v; want %v", actual.Impacts, wantImpacts))
	}
	wantClassifications := append([]string{}, expected.ConsumerClassifications...)
	sort.Strings(wantClassifications)
	if !equalStrings(wantClassifications, actual.ConsumerClassifications) {
		mismatches = append(mismatches, fmt.Sprintf("classifications=%v; want %v", actual.ConsumerClassifications, wantClassifications))
	}
	return mismatches
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func repositoryIdentity(root string) (string, bool) {
	revisionCommand := exec.Command("git", "rev-parse", "HEAD")
	revisionCommand.Dir = root
	revisionBytes, err := revisionCommand.Output()
	if err != nil {
		return "unknown", true
	}
	statusCommand := exec.Command("git", "status", "--porcelain", "--untracked-files=all")
	statusCommand.Dir = root
	statusBytes, err := statusCommand.Output()
	return strings.TrimSpace(string(revisionBytes)), err != nil || len(statusBytes) != 0
}

func writeResult(file string, document result) error {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(file), ".tcg-benchmark-*.json")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, file)
}

func readLimited(file string, limit int64) ([]byte, error) {
	input, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	data, err := io.ReadAll(io.LimitReader(input, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", file, limit)
	}
	return data, nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON contains multiple values")
		}
		return err
	}
	return nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	return -1
}

func resolve(root, value string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(root, filepath.Clean(value))
}

func isSafe(status string) bool {
	return status == "PASS" || status == "WARN" || status == "READY"
}

func isBlocking(status string) bool {
	return status == "BLOCK" || status == "BLOCKED"
}

type cappedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (target *cappedBuffer) Write(data []byte) (int, error) {
	written := len(data)
	remaining := target.limit - target.buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = target.buffer.Write(data)
	}
	return written, nil
}

func (target *cappedBuffer) String() string {
	return target.buffer.String()
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "benchmark failed: %v\n", err)
	os.Exit(1)
}
