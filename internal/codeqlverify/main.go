// Command codeqlverify rejects incomplete CodeQL analysis results.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const maxSARIFBytes = 128 << 20

type sarifLog struct {
	Runs []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool struct {
		Driver struct {
			Name string `json:"name"`
		} `json:"driver"`
	} `json:"tool"`
	Results     []sarifResult     `json:"results"`
	Invocations []sarifInvocation `json:"invocations"`
}

type sarifResult struct {
	RuleID  string       `json:"ruleId"`
	Message sarifMessage `json:"message"`
}

type sarifInvocation struct {
	ExecutionSuccessful        *bool               `json:"executionSuccessful"`
	ConfigurationNotifications []sarifNotification `json:"configurationNotifications"`
	ToolExecutionNotifications []sarifNotification `json:"toolExecutionNotifications"`
}

type sarifNotification struct {
	Level   string       `json:"level"`
	Message sarifMessage `json:"message"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

func main() {
	path := flag.String("path", "", "CodeQL SARIF file or directory")
	flag.Parse()
	if flag.NArg() != 0 || strings.TrimSpace(*path) == "" {
		fmt.Fprintln(os.Stderr, "usage: codeqlverify --path <CodeQL SARIF file or directory>")
		os.Exit(2)
	}
	if err := verify(*path); err != nil {
		fmt.Fprintf(os.Stderr, "CodeQL verification failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Verified CodeQL SARIF: analysis completed without extraction errors")
}

func verify(root string) error {
	files, err := sarifFiles(root)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return errors.New("no SARIF files found")
	}

	codeQLRuns := 0
	for _, file := range files {
		runs, err := readSARIF(file)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		for _, run := range runs {
			if !strings.Contains(strings.ToLower(run.Tool.Driver.Name), "codeql") {
				continue
			}
			codeQLRuns++
			if err := verifyRun(run); err != nil {
				return fmt.Errorf("%s: %w", file, err)
			}
		}
	}
	if codeQLRuns == 0 {
		return errors.New("SARIF did not contain a CodeQL run")
	}
	return nil
}

func sarifFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat SARIF path: %w", err)
	}
	if !info.IsDir() {
		if !isSARIF(root) {
			return nil, fmt.Errorf("file %q does not have a .sarif or .sarif.json extension", root)
		}
		return []string{root}, nil
	}

	var files []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() && entry.Type().IsRegular() && isSARIF(path) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk SARIF path: %w", err)
	}
	return files, nil
}

func isSARIF(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".sarif") || strings.HasSuffix(lower, ".sarif.json")
}

func readSARIF(path string) ([]sarifRun, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	if info.Size() > maxSARIFBytes {
		return nil, fmt.Errorf("SARIF exceeds %d-byte limit", maxSARIFBytes)
	}

	decoder := json.NewDecoder(io.LimitReader(file, maxSARIFBytes+1))
	var log sarifLog
	if err := decoder.Decode(&log); err != nil {
		return nil, fmt.Errorf("decode SARIF: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("decode SARIF: trailing JSON value")
		}
		return nil, fmt.Errorf("decode SARIF trailer: %w", err)
	}
	return log.Runs, nil
}

func verifyRun(run sarifRun) error {
	for _, result := range run.Results {
		if strings.Contains(strings.ToLower(result.RuleID), "diagnostics/extraction-errors") {
			return fmt.Errorf("extraction diagnostic %q: %s", result.RuleID, result.Message.Text)
		}
	}
	for _, invocation := range run.Invocations {
		if invocation.ExecutionSuccessful != nil && !*invocation.ExecutionSuccessful {
			return errors.New("CodeQL tool invocation reported executionSuccessful=false")
		}
		notifications := append(
			append([]sarifNotification(nil), invocation.ConfigurationNotifications...),
			invocation.ToolExecutionNotifications...,
		)
		for _, notification := range notifications {
			if strings.EqualFold(notification.Level, "error") {
				return fmt.Errorf("CodeQL invocation error: %s", notification.Message.Text)
			}
		}
	}
	return nil
}
