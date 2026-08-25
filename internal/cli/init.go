package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const starterConfig = `apiVersion: tcg/v1alpha1
kind: Config
sources:
  prometheusRules:
    - path: .tcg/getting-started/prometheus/*.yaml
      required: true
analysis:
  includeTransitiveDependencies: true
  unresolvedReferencePolicy: error
policy:
  failOnCriticalLegacyConsumer: true
  failOnCriticalUnknown: true
  minimumBlockingCriticality: high
output:
  formats: [console]
`

const starterChanges = `apiVersion: tcg/v1alpha1
kind: ChangeSet
metadata:
  name: getting-started-removal
spec:
  changes:
    - id: remove-example-metric
      kind: metric_remove
      domain: prometheus
      from:
        domain: prometheus
        kind: metric
        name: replace_me_metric
`

const starterRules = `groups:
  - name: getting-started
    rules:
      - alert: ReplaceMeMetricMissing
        expr: replace_me_metric == 0
        labels:
          severity: critical
`

func runInit(_ context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: telemetry-change-guard init [--directory <path>]")
		flags.PrintDefaults()
	}
	directory := flags.String("directory", ".", "directory in which to create the runnable starter files")
	if err := flags.Parse(args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "init accepts no positional arguments")
		return 1
	}
	root, err := filepath.Abs(filepath.Clean(*directory))
	if err != nil {
		fmt.Fprintf(stderr, "Error: resolve starter directory: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		fmt.Fprintf(stderr, "Error: create starter directory: %v\n", err)
		return 1
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		fmt.Fprintf(stderr, "Error: inspect starter directory: %v\n", err)
		return 1
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		fmt.Fprintln(stderr, "Error: starter directory must be a real directory, not a symbolic link")
		return 1
	}
	targets := []struct {
		path     string
		contents string
	}{
		{path: filepath.Join(root, "tcg.yaml"), contents: starterConfig},
		{path: filepath.Join(root, "tcg-changes.example.yaml"), contents: starterChanges},
		{path: filepath.Join(root, ".tcg", "getting-started", "prometheus", "rules.yaml"), contents: starterRules},
	}
	for _, target := range targets {
		if _, err := os.Lstat(target.path); err == nil {
			fmt.Fprintf(stderr, "Error: refusing to overwrite existing starter path %q\n", target.path)
			return 1
		} else if !os.IsNotExist(err) {
			fmt.Fprintf(stderr, "Error: inspect starter path %q: %v\n", target.path, err)
			return 1
		}
	}
	created := make([]starterFile, 0, len(targets))
	for _, target := range targets {
		if err := ensureStarterParent(root, target.path); err != nil {
			removeStarterFiles(created)
			fmt.Fprintf(stderr, "Error: create starter directories: %v\n", err)
			return 1
		}
		info, err := writeStarterFile(target.path, []byte(target.contents))
		if err != nil {
			removeStarterFiles(created)
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		created = append(created, starterFile{path: target.path, info: info})
	}
	fmt.Fprintf(stdout, "Created a runnable Telemetry Change Guard starter in %s\n", root)
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "Next:")
	fmt.Fprintln(stdout, "  telemetry-change-guard validate --config ./tcg.yaml --changes ./tcg-changes.example.yaml")
	fmt.Fprintln(stdout, "  telemetry-change-guard check --config ./tcg.yaml --changes ./tcg-changes.example.yaml")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "The example check intentionally exits 2 with BLOCK. Replace the sample rule, source paths, and ChangeSet with your repository's telemetry contract.")
	return 0
}

type starterFile struct {
	path string
	info fs.FileInfo
}

func writeStarterFile(path string, contents []byte) (info fs.FileInfo, err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create starter file %q without overwriting: %w", path, err)
	}
	info, err = file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("stat starter file %q: %w", path, err)
	}
	complete := false
	defer func() {
		_ = file.Close()
		if !complete {
			removeStarterFile(starterFile{path: path, info: info})
		}
	}()
	if _, err := file.Write(contents); err != nil {
		return nil, fmt.Errorf("write starter file %q: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		return nil, fmt.Errorf("sync starter file %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close starter file %q: %w", path, err)
	}
	complete = true
	return info, nil
}

func removeStarterFiles(files []starterFile) {
	for index := len(files) - 1; index >= 0; index-- {
		removeStarterFile(files[index])
	}
}

func removeStarterFile(file starterFile) {
	current, err := os.Lstat(file.path)
	if err == nil && file.info != nil && os.SameFile(file.info, current) {
		_ = os.Remove(file.path)
	}
}

func ensureStarterParent(root, target string) error {
	relative, err := filepath.Rel(root, filepath.Dir(target))
	if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("starter path resolves outside target directory")
	}
	current := root
	for _, component := range strings.Split(filepath.Clean(relative), string(filepath.Separator)) {
		if component == "." || component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("starter parent %q must be a real directory", current)
			}
			continue
		}
		if !os.IsNotExist(err) {
			return err
		}
		if err := os.Mkdir(current, 0o755); err != nil {
			return err
		}
	}
	return nil
}
