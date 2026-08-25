// Command docverify checks local Markdown links and rejects unsupported public
// claims from the project's primary product and launch documents.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var (
	markdownLink = regexp.MustCompile(`!?\[[^]]*\]\(([^)]+)\)`)
	heading      = regexp.MustCompile(`^(#{1,6})[ \t]+(.+?)[ \t]*#*[ \t]*$`)
	htmlTag      = regexp.MustCompile(`<[^>]+>`)
	claimRules   = []struct {
		pattern *regexp.Regexp
		message string
	}{
		{regexp.MustCompile(`(?i)\b(?:finds?|maps?|shows?) (?:the|your|an?) (?:entire|complete|full) blast radius\b`), "scope the claim to configured evidence"},
		{regexp.MustCompile(`(?i)\b(?:the|your) (?:entire|complete|full) blast radius\b`), "scope the claim to configured evidence"},
		{regexp.MustCompile(`(?i)\ball downstream (?:consumers|dependencies)\b`), "do not claim unconfigured consumer coverage"},
		{regexp.MustCompile(`(?i)\b(?:production[- ]proven|battle[- ]tested)\b`), "requires published production evidence"},
		{regexp.MustCompile(`(?i)\b(?:zero|no) false (?:negative|positive)s?\b`), "requires independent accuracy evidence"},
		{regexp.MustCompile(`(?i)\b100% (?:accurate|precision|recall|coverage)\b`), "requires independent accuracy evidence"},
		{regexp.MustCompile(`(?i)\bindustry[- ]first\b`), "requires a reviewed competitive study"},
	}
)

func main() {
	rootFlag := flag.String("root", ".", "repository root")
	flag.Parse()
	if flag.NArg() != 0 {
		fatal(errors.New("usage: docverify [--root <path>]"))
	}
	root, err := filepath.Abs(*rootFlag)
	if err != nil {
		fatal(err)
	}
	markdown, err := markdownFiles(root)
	if err != nil {
		fatal(err)
	}
	var failures []string
	for _, file := range markdown {
		fileFailures, err := checkLinks(root, file)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", relative(root, file), err))
			continue
		}
		failures = append(failures, fileFailures...)
	}
	for _, name := range claimFiles(root) {
		fileFailures, err := checkClaims(root, name)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", relative(root, name), err))
			continue
		}
		failures = append(failures, fileFailures...)
	}
	if len(failures) != 0 {
		sort.Strings(failures)
		for _, failure := range failures {
			fmt.Fprintln(os.Stderr, failure)
		}
		os.Exit(1)
	}
	fmt.Printf("Verified %d Markdown files and public claim boundaries.\n", len(markdown))
}

func markdownFiles(root string) ([]string, error) {
	var files []string
	for _, entry := range []string{"README.md", "CONTRIBUTING.md", "SUPPORT.md", "SECURITY.md", "docs", "benchmarks", "evaluation", "release-fixtures"} {
		path := filepath.Join(root, entry)
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			files = append(files, path)
			continue
		}
		err = filepath.WalkDir(path, func(name string, item os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !item.IsDir() && strings.EqualFold(filepath.Ext(name), ".md") {
				files = append(files, name)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

func checkLinks(root, file string) ([]string, error) {
	contents, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	anchors := headingAnchors(string(contents))
	var failures []string
	lines := strings.Split(string(contents), "\n")
	inFence := false
	for lineIndex, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, match := range markdownLink.FindAllStringSubmatch(line, -1) {
			destination := cleanDestination(match[1])
			if destination == "" || externalDestination(destination) {
				continue
			}
			parsed, parseErr := url.Parse(destination)
			if parseErr != nil {
				failures = append(failures, fmt.Sprintf("%s:%d: invalid link %q", relative(root, file), lineIndex+1, destination))
				continue
			}
			target := file
			if parsed.Path != "" {
				unescaped, unescapeErr := url.PathUnescape(parsed.Path)
				if unescapeErr != nil {
					failures = append(failures, fmt.Sprintf("%s:%d: invalid escaped path %q", relative(root, file), lineIndex+1, destination))
					continue
				}
				if strings.HasPrefix(unescaped, "/") {
					target = filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(unescaped, "/")))
				} else {
					target = filepath.Join(filepath.Dir(file), filepath.FromSlash(unescaped))
				}
			}
			info, statErr := os.Stat(target)
			if statErr != nil {
				failures = append(failures, fmt.Sprintf("%s:%d: local link target does not exist: %s", relative(root, file), lineIndex+1, destination))
				continue
			}
			if parsed.Fragment == "" || info.IsDir() || !strings.EqualFold(filepath.Ext(target), ".md") {
				continue
			}
			targetAnchors := anchors
			if filepath.Clean(target) != filepath.Clean(file) {
				targetContents, readErr := os.ReadFile(target)
				if readErr != nil {
					failures = append(failures, fmt.Sprintf("%s:%d: cannot read link target: %s", relative(root, file), lineIndex+1, destination))
					continue
				}
				targetAnchors = headingAnchors(string(targetContents))
			}
			fragment, _ := url.PathUnescape(parsed.Fragment)
			if _, found := targetAnchors[fragment]; !found {
				failures = append(failures, fmt.Sprintf("%s:%d: Markdown anchor does not exist: %s", relative(root, file), lineIndex+1, destination))
			}
		}
	}
	return failures, nil
}

func headingAnchors(contents string) map[string]struct{} {
	anchors := map[string]struct{}{}
	counts := map[string]int{}
	inFence := false
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		match := heading.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		base := githubSlug(match[2])
		slug := base
		if count := counts[base]; count != 0 {
			slug = fmt.Sprintf("%s-%d", base, count)
		}
		counts[base]++
		anchors[slug] = struct{}{}
	}
	return anchors
}

func githubSlug(value string) string {
	value = htmlTag.ReplaceAllString(value, "")
	value = strings.ToLower(strings.TrimSpace(value))
	var result strings.Builder
	for _, character := range value {
		switch {
		case unicode.IsLetter(character), unicode.IsDigit(character), character == '-', character == '_':
			result.WriteRune(character)
		case unicode.IsSpace(character):
			result.WriteByte('-')
		}
	}
	return result.String()
}

func checkClaims(root, file string) ([]string, error) {
	contents, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var failures []string
	for index, line := range strings.Split(string(contents), "\n") {
		for _, rule := range claimRules {
			if rule.pattern.MatchString(line) {
				failures = append(failures, fmt.Sprintf("%s:%d: unsupported public claim (%s)", relative(root, file), index+1, rule.message))
				break
			}
		}
	}
	return failures, nil
}

func claimFiles(root string) []string {
	files := []string{filepath.Join(root, "README.md")}
	launch := filepath.Join(root, "docs", "launch")
	_ = filepath.WalkDir(launch, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry != nil && !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".md") {
			files = append(files, path)
		}
		return nil
	})
	return files
}

func cleanDestination(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "<") {
		if end := strings.Index(value, ">"); end >= 0 {
			return value[1:end]
		}
	}
	if index := strings.IndexAny(value, " \t"); index >= 0 {
		value = value[:index]
	}
	return strings.TrimSpace(value)
}

func externalDestination(value string) bool {
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "data:")
}

func relative(root, file string) string {
	value, err := filepath.Rel(root, file)
	if err != nil {
		return file
	}
	return filepath.ToSlash(value)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "documentation verification failed:", err)
	os.Exit(1)
}
