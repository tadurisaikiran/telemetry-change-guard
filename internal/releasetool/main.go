// Command releasetool stages and verifies the public release payload. It is a
// maintainer tool, not a shipped product binary.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	productversion "github.com/tadurisaikiran/telemetry-change-guard/internal/version"
)

const (
	manifestSchema = "tcg-release-manifest/v1alpha1"
	projectName    = "telemetry-change-guard"
	repositoryURL  = "https://github.com/tadurisaikiran/telemetry-change-guard"
	manifestName   = "release-manifest.json"
	checksumsName  = "SHA256SUMS"
)

var (
	semanticPrerelease = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-[0-9A-Za-z][0-9A-Za-z.-]*$`)
	fullCommit         = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Pattern      = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type target struct {
	OS     string
	Arch   string
	Format string
}

var releaseTargets = []target{
	{OS: "darwin", Arch: "amd64", Format: "tar.gz"},
	{OS: "darwin", Arch: "arm64", Format: "tar.gz"},
	{OS: "linux", Arch: "amd64", Format: "tar.gz"},
	{OS: "linux", Arch: "arm64", Format: "tar.gz"},
	{OS: "windows", Arch: "amd64", Format: "zip"},
	{OS: "windows", Arch: "arm64", Format: "zip"},
}

type expectedArtifact struct {
	File    string
	Kind    string
	Subject string
	OS      string
	Arch    string
}

type artifactRecord struct {
	File    string `json:"file"`
	Kind    string `json:"kind"`
	Subject string `json:"subject,omitempty"`
	OS      string `json:"os,omitempty"`
	Arch    string `json:"arch,omitempty"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}

type toolVersions struct {
	Go         string `json:"go"`
	GoReleaser string `json:"goreleaser"`
	Syft       string `json:"syft"`
}

type releaseManifest struct {
	SchemaVersion string           `json:"schemaVersion"`
	Project       string           `json:"project"`
	Version       string           `json:"version"`
	Commit        string           `json:"commit"`
	BuildDate     string           `json:"buildDate"`
	Clean         bool             `json:"clean"`
	Binaries      []string         `json:"binaries"`
	Tools         toolVersions     `json:"tools"`
	Artifacts     []artifactRecord `json:"artifacts"`
}

type stageOptions struct {
	Raw               string
	Out               string
	Version           string
	Commit            string
	BuildDate         string
	GoVersion         string
	GoReleaserVersion string
	SyftVersion       string
}

type archiveEntry struct {
	Name    string
	Mode    fs.FileMode
	ModTime time.Time
	Data    []byte
}

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: releasetool <stage|verify> [flags]")
	}

	var err error
	switch os.Args[1] {
	case "stage":
		err = runStage(os.Args[2:])
	case "verify":
		err = runVerify(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "release verification: "+format+"\n", args...)
	os.Exit(1)
}

func runStage(args []string) error {
	set := flag.NewFlagSet("stage", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	options := stageOptions{}
	set.StringVar(&options.Raw, "raw", "", "GoReleaser output directory")
	set.StringVar(&options.Out, "out", "", "public release directory")
	set.StringVar(&options.Version, "version", "", "candidate version")
	set.StringVar(&options.Commit, "commit", "", "source commit")
	set.StringVar(&options.BuildDate, "build-date", "", "RFC3339 commit time")
	set.StringVar(&options.GoVersion, "go-version", "", "Go toolchain version")
	set.StringVar(&options.GoReleaserVersion, "goreleaser-version", "", "GoReleaser version")
	set.StringVar(&options.SyftVersion, "syft-version", "", "Syft version")
	if err := set.Parse(args); err != nil {
		return err
	}
	if set.NArg() != 0 {
		return errors.New("stage does not accept positional arguments")
	}
	if err := validateStageOptions(options); err != nil {
		return err
	}

	raw, err := filepath.Abs(options.Raw)
	if err != nil {
		return fmt.Errorf("resolve raw directory: %w", err)
	}
	out, err := filepath.Abs(options.Out)
	if err != nil {
		return fmt.Errorf("resolve output directory: %w", err)
	}
	if filepath.Base(raw) != "raw" || filepath.Base(filepath.Dir(raw)) != "dist" {
		return errors.New("raw directory must end in dist/raw")
	}
	if filepath.Base(out) != "release" || filepath.Base(filepath.Dir(out)) != "dist" {
		return errors.New("output directory must end in dist/release")
	}
	if raw == out {
		return errors.New("raw and output directories must differ")
	}

	buildTime, _ := time.Parse(time.RFC3339, options.BuildDate)
	expected := expectedArtifacts(options.Version)
	rawFiles, err := locateRawArtifacts(raw, expected)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(out); err != nil {
		return fmt.Errorf("clean public release directory: %w", err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return fmt.Errorf("create public release directory: %w", err)
	}

	for _, artifact := range expected {
		destination := filepath.Join(out, artifact.File)
		if err := copyFile(rawFiles[artifact.File], destination, 0o644); err != nil {
			return err
		}
	}
	for _, artifact := range expected {
		if artifact.Subject == "" {
			continue
		}
		subjectDigest, _, err := digestFile(filepath.Join(out, artifact.Subject))
		if err != nil {
			return err
		}
		file := filepath.Join(out, artifact.File)
		switch artifact.Kind {
		case "sbom-spdx":
			err = normalizeSPDX(file, artifact.Subject, subjectDigest, options.Version, options.BuildDate)
		case "sbom-cyclonedx":
			err = normalizeCycloneDX(file, options.BuildDate)
		}
		if err != nil {
			return fmt.Errorf("normalize %s: %w", artifact.File, err)
		}
	}

	records := make([]artifactRecord, 0, len(expected))
	for _, artifact := range expected {
		digest, size, err := digestFile(filepath.Join(out, artifact.File))
		if err != nil {
			return err
		}
		records = append(records, artifactRecord{
			File:    artifact.File,
			Kind:    artifact.Kind,
			Subject: artifact.Subject,
			OS:      artifact.OS,
			Arch:    artifact.Arch,
			Size:    size,
			SHA256:  digest,
		})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].File < records[j].File })

	manifest := releaseManifest{
		SchemaVersion: manifestSchema,
		Project:       projectName,
		Version:       options.Version,
		Commit:        options.Commit,
		BuildDate:     options.BuildDate,
		Clean:         true,
		Binaries:      []string{"telemetry-change-guard", "tmr"},
		Tools: toolVersions{
			Go:         options.GoVersion,
			GoReleaser: options.GoReleaserVersion,
			Syft:       options.SyftVersion,
		},
		Artifacts: records,
	}
	manifestPath := filepath.Join(out, manifestName)
	if err := writeJSON(manifestPath, manifest); err != nil {
		return err
	}

	for _, artifact := range expected {
		if err := os.Chtimes(filepath.Join(out, artifact.File), buildTime, buildTime); err != nil {
			return fmt.Errorf("set artifact timestamp: %w", err)
		}
	}
	if err := os.Chtimes(manifestPath, buildTime, buildTime); err != nil {
		return fmt.Errorf("set manifest timestamp: %w", err)
	}
	if err := writeChecksums(out, appendArtifactName(expected, manifestName)); err != nil {
		return err
	}
	if err := os.Chtimes(filepath.Join(out, checksumsName), buildTime, buildTime); err != nil {
		return fmt.Errorf("set checksum timestamp: %w", err)
	}
	return nil
}

func validateStageOptions(options stageOptions) error {
	if options.Raw == "" || options.Out == "" {
		return errors.New("stage requires --raw and --out")
	}
	if !semanticPrerelease.MatchString(options.Version) {
		return errors.New("stage version must be a semantic prerelease")
	}
	if !fullCommit.MatchString(options.Commit) {
		return errors.New("stage commit must be a full lowercase SHA")
	}
	if _, err := time.Parse(time.RFC3339, options.BuildDate); err != nil {
		return fmt.Errorf("stage build date must be RFC3339: %w", err)
	}
	if !regexp.MustCompile(`^go[0-9]+\.[0-9]+(\.[0-9]+)?`).MatchString(options.GoVersion) {
		return errors.New("stage Go version is invalid")
	}
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(options.GoReleaserVersion) {
		return errors.New("stage GoReleaser version is invalid")
	}
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(options.SyftVersion) {
		return errors.New("stage Syft version is invalid")
	}
	return nil
}

func expectedArtifacts(version string) []expectedArtifact {
	artifacts := make([]expectedArtifact, 0, 21)
	for _, platform := range releaseTargets {
		base := fmt.Sprintf("%s_%s_%s_%s.%s", projectName, version, platform.OS, platform.Arch, platform.Format)
		artifacts = append(artifacts,
			expectedArtifact{File: base, Kind: "archive", OS: platform.OS, Arch: platform.Arch},
			expectedArtifact{File: base + ".spdx.json", Kind: "sbom-spdx", Subject: base, OS: platform.OS, Arch: platform.Arch},
			expectedArtifact{File: base + ".cdx.json", Kind: "sbom-cyclonedx", Subject: base, OS: platform.OS, Arch: platform.Arch},
		)
	}
	source := fmt.Sprintf("%s_%s_source.tar.gz", projectName, version)
	artifacts = append(artifacts,
		expectedArtifact{File: source, Kind: "source"},
		expectedArtifact{File: source + ".spdx.json", Kind: "sbom-spdx", Subject: source},
		expectedArtifact{File: source + ".cdx.json", Kind: "sbom-cyclonedx", Subject: source},
	)
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].File < artifacts[j].File })
	return artifacts
}

func appendArtifactName(artifacts []expectedArtifact, extra string) []string {
	names := make([]string, 0, len(artifacts)+1)
	for _, artifact := range artifacts {
		names = append(names, artifact.File)
	}
	names = append(names, extra)
	sort.Strings(names)
	return names
}

func locateRawArtifacts(raw string, expected []expectedArtifact) (map[string]string, error) {
	wanted := make(map[string]struct{}, len(expected))
	for _, artifact := range expected {
		wanted[artifact.File] = struct{}{}
	}
	found := make(map[string]string, len(expected))
	err := filepath.WalkDir(raw, func(file string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name := filepath.Base(file)
		if _, ok := wanted[name]; !ok {
			return nil
		}
		if previous, ok := found[name]; ok {
			return fmt.Errorf("raw artifact %s is duplicated at %s and %s", name, previous, file)
		}
		found[name] = file
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan raw artifacts: %w", err)
	}
	for name := range wanted {
		if found[name] == "" {
			return nil, fmt.Errorf("required raw artifact is missing: %s", name)
		}
	}
	return found, nil
}

func copyFile(source, destination string, mode fs.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", source, err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", destination, err)
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return fmt.Errorf("copy %s: %w", destination, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", destination, closeErr)
	}
	return nil
}

func decodeJSONObject(file string) (map[string]any, error) {
	input, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	decoder := json.NewDecoder(input)
	decoder.UseNumber()
	value := map[string]any{}
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	return value, nil
}

func normalizeSPDX(file, subject, subjectDigest, version, buildDate string) error {
	document, err := decodeJSONObject(file)
	if err != nil {
		return err
	}
	spdxVersion, _ := document["spdxVersion"].(string)
	if !strings.HasPrefix(spdxVersion, "SPDX-") {
		return errors.New("not an SPDX JSON document")
	}
	creation, ok := document["creationInfo"].(map[string]any)
	if !ok {
		return errors.New("SPDX creationInfo is missing")
	}
	creation["created"] = buildDate
	document["documentNamespace"] = fmt.Sprintf("%s/sbom/%s/%s-%s", repositoryURL, version, subject, subjectDigest)
	return writeJSON(file, document)
}

func normalizeCycloneDX(file, buildDate string) error {
	document, err := decodeJSONObject(file)
	if err != nil {
		return err
	}
	if document["bomFormat"] != "CycloneDX" {
		return errors.New("not a CycloneDX JSON document")
	}
	metadata, ok := document["metadata"].(map[string]any)
	if !ok {
		return errors.New("CycloneDX metadata is missing")
	}
	metadata["timestamp"] = buildDate
	delete(document, "serialNumber")
	return writeJSON(file, canonicalizeJSON(document))
}

func canonicalizeJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			typed[key] = canonicalizeJSON(item)
		}
		return typed
	case []any:
		for index, item := range typed {
			typed[index] = canonicalizeJSON(item)
		}
		sort.SliceStable(typed, func(i, j int) bool {
			left, _ := json.Marshal(typed[i])
			right, _ := json.Marshal(typed[j])
			return bytes.Compare(left, right) < 0
		})
		return typed
	case string:
		return normalizeSyftExtractionPath(typed)
	default:
		return value
	}
}

func normalizeSyftExtractionPath(value string) string {
	for _, separator := range []string{"/syft-archive-contents-", `\syft-archive-contents-`} {
		index := strings.Index(value, separator)
		if index < 0 {
			continue
		}
		remainder := value[index+len(separator):]
		boundary := strings.IndexAny(remainder, `/\`)
		if boundary < 1 {
			return value
		}
		return strings.ReplaceAll(remainder[boundary+1:], `\`, "/")
	}
	return value
}

func writeJSON(file string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(file, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", file, err)
	}
	return nil
}

func digestFile(file string) (string, int64, error) {
	input, err := os.Open(file)
	if err != nil {
		return "", 0, fmt.Errorf("open %s: %w", file, err)
	}
	defer input.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, input)
	if err != nil {
		return "", 0, fmt.Errorf("hash %s: %w", file, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func writeChecksums(directory string, files []string) error {
	sort.Strings(files)
	var output strings.Builder
	for _, name := range files {
		if filepath.Base(name) != name {
			return fmt.Errorf("checksum target is not a base name: %s", name)
		}
		digest, _, err := digestFile(filepath.Join(directory, name))
		if err != nil {
			return err
		}
		fmt.Fprintf(&output, "%s  %s\n", digest, name)
	}
	if err := os.WriteFile(filepath.Join(directory, checksumsName), []byte(output.String()), 0o644); err != nil {
		return fmt.Errorf("write checksums: %w", err)
	}
	return nil
}

func runVerify(args []string) error {
	set := flag.NewFlagSet("verify", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	directoryFlag := set.String("dir", "", "public release directory")
	if err := set.Parse(args); err != nil {
		return err
	}
	if set.NArg() != 0 || *directoryFlag == "" {
		return errors.New("verify requires --dir and no positional arguments")
	}
	directory, err := filepath.Abs(*directoryFlag)
	if err != nil {
		return err
	}
	manifest, err := readManifest(filepath.Join(directory, manifestName))
	if err != nil {
		return err
	}
	if err := validateManifest(manifest); err != nil {
		return err
	}
	expected := expectedArtifacts(manifest.Version)
	if err := verifyArtifactRecords(directory, manifest, expected); err != nil {
		return err
	}
	if err := verifyChecksums(directory, expected); err != nil {
		return err
	}
	if err := verifyNoUnexpectedFiles(directory, expected); err != nil {
		return err
	}
	for _, artifact := range expected {
		switch artifact.Kind {
		case "archive":
			if err := verifyBinaryArchive(directory, manifest, artifact); err != nil {
				return err
			}
		case "source":
			if err := verifySourceArchive(filepath.Join(directory, artifact.File), manifest.Version); err != nil {
				return err
			}
		case "sbom-spdx", "sbom-cyclonedx":
			if err := verifySBOM(directory, manifest, artifact); err != nil {
				return err
			}
		}
	}
	if err := runHostSmoke(directory, manifest); err != nil {
		return err
	}
	fmt.Printf("Verified %d release artifacts for %s at %s\n", len(expected), manifest.Version, manifest.Commit)
	return nil
}

func readManifest(file string) (releaseManifest, error) {
	input, err := os.Open(file)
	if err != nil {
		return releaseManifest{}, fmt.Errorf("open release manifest: %w", err)
	}
	defer input.Close()
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()
	manifest := releaseManifest{}
	if err := decoder.Decode(&manifest); err != nil {
		return releaseManifest{}, fmt.Errorf("decode release manifest: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return releaseManifest{}, fmt.Errorf("decode release manifest: %w", err)
	}
	return manifest, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("unexpected data after JSON document")
}

func validateManifest(manifest releaseManifest) error {
	if manifest.SchemaVersion != manifestSchema || manifest.Project != projectName {
		return errors.New("release manifest identity is invalid")
	}
	if !semanticPrerelease.MatchString(manifest.Version) || !fullCommit.MatchString(manifest.Commit) {
		return errors.New("release manifest version or commit is invalid")
	}
	if _, err := time.Parse(time.RFC3339, manifest.BuildDate); err != nil {
		return errors.New("release manifest build date is invalid")
	}
	if !manifest.Clean {
		return errors.New("release manifest does not assert a clean build")
	}
	if len(manifest.Binaries) != 2 || manifest.Binaries[0] != "telemetry-change-guard" || manifest.Binaries[1] != "tmr" {
		return errors.New("release manifest binary set is invalid")
	}
	if manifest.Tools.Go == "" || manifest.Tools.GoReleaser == "" || manifest.Tools.Syft == "" {
		return errors.New("release manifest tool versions are incomplete")
	}
	return verifyLocalLocks(manifest)
}

func verifyLocalLocks(manifest releaseManifest) error {
	data, err := os.ReadFile("release/metadata.env")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	locks := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" || value == "" {
			return errors.New("release metadata contains an invalid assignment")
		}
		if _, exists := locks[key]; exists {
			return fmt.Errorf("release metadata duplicates %s", key)
		}
		locks[key] = value
	}
	checks := map[string]string{
		"TCG_CANDIDATE_VERSION":  manifest.Version,
		"TCG_GORELEASER_VERSION": manifest.Tools.GoReleaser,
		"TCG_SYFT_VERSION":       manifest.Tools.Syft,
	}
	for key, actual := range checks {
		if locks[key] != actual {
			return fmt.Errorf("release manifest %s does not match release metadata", key)
		}
	}
	return nil
}

func verifyArtifactRecords(directory string, manifest releaseManifest, expected []expectedArtifact) error {
	if len(manifest.Artifacts) != len(expected) {
		return fmt.Errorf("release manifest has %d artifacts; expected %d", len(manifest.Artifacts), len(expected))
	}
	expectedByName := make(map[string]expectedArtifact, len(expected))
	for _, artifact := range expected {
		expectedByName[artifact.File] = artifact
	}
	previous := ""
	seen := map[string]struct{}{}
	for _, record := range manifest.Artifacts {
		artifact, ok := expectedByName[record.File]
		if !ok {
			return fmt.Errorf("release manifest contains unexpected artifact %s", record.File)
		}
		if record.File <= previous {
			return errors.New("release manifest artifacts are not strictly sorted")
		}
		previous = record.File
		if _, ok := seen[record.File]; ok {
			return fmt.Errorf("release manifest duplicates %s", record.File)
		}
		seen[record.File] = struct{}{}
		if record.Kind != artifact.Kind || record.Subject != artifact.Subject || record.OS != artifact.OS || record.Arch != artifact.Arch {
			return fmt.Errorf("release manifest metadata is wrong for %s", record.File)
		}
		if record.Size <= 0 || !sha256Pattern.MatchString(record.SHA256) {
			return fmt.Errorf("release manifest size or digest is invalid for %s", record.File)
		}
		digest, size, err := digestFile(filepath.Join(directory, record.File))
		if err != nil {
			return err
		}
		if digest != record.SHA256 || size != record.Size {
			return fmt.Errorf("release manifest digest or size mismatch for %s", record.File)
		}
	}
	return nil
}

func parseChecksums(data []byte) (map[string]string, []string, error) {
	checksums := map[string]string{}
	order := []string{}
	for index, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" {
			return nil, nil, fmt.Errorf("checksum line %d is empty", index+1)
		}
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 || !sha256Pattern.MatchString(parts[0]) {
			return nil, nil, fmt.Errorf("checksum line %d is invalid", index+1)
		}
		name := parts[1]
		if filepath.Base(name) != name || name == "." || strings.Contains(name, "\\") {
			return nil, nil, fmt.Errorf("checksum line %d contains an unsafe path", index+1)
		}
		if _, exists := checksums[name]; exists {
			return nil, nil, fmt.Errorf("checksum file duplicates %s", name)
		}
		checksums[name] = parts[0]
		order = append(order, name)
	}
	if !sort.StringsAreSorted(order) {
		return nil, nil, errors.New("checksum file is not sorted")
	}
	return checksums, order, nil
}

func verifyChecksums(directory string, expected []expectedArtifact) error {
	data, err := os.ReadFile(filepath.Join(directory, checksumsName))
	if err != nil {
		return fmt.Errorf("read checksums: %w", err)
	}
	checksums, _, err := parseChecksums(data)
	if err != nil {
		return err
	}
	expectedNames := appendArtifactName(expected, manifestName)
	if len(checksums) != len(expectedNames) {
		return fmt.Errorf("checksum file has %d entries; expected %d", len(checksums), len(expectedNames))
	}
	for _, name := range expectedNames {
		digest, _, err := digestFile(filepath.Join(directory, name))
		if err != nil {
			return err
		}
		if checksums[name] != digest {
			return fmt.Errorf("checksum mismatch for %s", name)
		}
	}
	return nil
}

func verifyNoUnexpectedFiles(directory string, expected []expectedArtifact) error {
	allowed := map[string]struct{}{manifestName: {}, checksumsName: {}}
	for _, artifact := range expected {
		allowed[artifact.File] = struct{}{}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	if len(entries) != len(allowed) {
		return fmt.Errorf("release directory has %d entries; expected %d", len(entries), len(allowed))
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return fmt.Errorf("release directory contains unexpected directory %s", entry.Name())
		}
		if _, ok := allowed[entry.Name()]; !ok {
			return fmt.Errorf("release directory contains unexpected file %s", entry.Name())
		}
	}
	return nil
}

func verifyBinaryArchive(directory string, manifest releaseManifest, artifact expectedArtifact) error {
	file := filepath.Join(directory, artifact.File)
	entries, err := readArchive(file)
	if err != nil {
		return fmt.Errorf("read %s: %w", artifact.File, err)
	}
	prefix := fmt.Sprintf("%s_%s_%s_%s", projectName, manifest.Version, artifact.OS, artifact.Arch)
	extension := ""
	if artifact.OS == "windows" {
		extension = ".exe"
	}
	expectedNames := []string{
		path.Join(prefix, "LICENSE"),
		path.Join(prefix, "NOTICE.md"),
		path.Join(prefix, "README.md"),
		path.Join(prefix, "telemetry-change-guard"+extension),
		path.Join(prefix, "tmr"+extension),
	}
	sort.Strings(expectedNames)
	actualNames := make([]string, 0, len(entries))
	byName := make(map[string]archiveEntry, len(entries))
	for _, entry := range entries {
		actualNames = append(actualNames, entry.Name)
		byName[entry.Name] = entry
	}
	sort.Strings(actualNames)
	if !equalStrings(actualNames, expectedNames) {
		return fmt.Errorf("%s has unexpected layout: %v", artifact.File, actualNames)
	}
	buildDate, _ := time.Parse(time.RFC3339, manifest.BuildDate)
	for _, name := range expectedNames {
		entry := byName[name]
		binary := strings.HasSuffix(name, "telemetry-change-guard"+extension) || strings.HasSuffix(name, "/tmr"+extension)
		expectedMode := fs.FileMode(0o644)
		if binary {
			expectedMode = 0o755
		}
		if entry.Mode.Perm() != expectedMode {
			return fmt.Errorf("%s has mode %04o; expected %04o", name, entry.Mode.Perm(), expectedMode)
		}
		if !sameSecond(entry.ModTime, buildDate) {
			return fmt.Errorf("%s timestamp does not match build date", name)
		}
		if binary {
			if err := verifyBinary(entry.Data, manifest, artifact, name); err != nil {
				return err
			}
		}
	}
	comparisons := map[string]string{
		path.Join(prefix, "LICENSE"):   "LICENSE",
		path.Join(prefix, "NOTICE.md"): "release/NOTICE.md",
		path.Join(prefix, "README.md"): "release/README.md",
	}
	for archiveName, sourceName := range comparisons {
		source, err := os.ReadFile(sourceName)
		if err != nil {
			return err
		}
		if !bytes.Equal(source, byName[archiveName].Data) {
			return fmt.Errorf("%s does not match %s", archiveName, sourceName)
		}
	}
	return nil
}

func readArchive(file string) ([]archiveEntry, error) {
	if strings.HasSuffix(file, ".tar.gz") {
		return readTarGzip(file)
	}
	if strings.HasSuffix(file, ".zip") {
		return readZip(file)
	}
	return nil, errors.New("unsupported archive format")
}

func readTarGzip(file string) ([]archiveEntry, error) {
	input, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	gzipReader, err := gzip.NewReader(input)
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	entries := []archiveEntry{}
	seen := map[string]struct{}{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if err := validateArchivePath(header.Name); err != nil {
			return nil, err
		}
		if header.Typeflag == tar.TypeXHeader || header.Typeflag == tar.TypeXGlobalHeader {
			// PAX headers carry archive metadata. archive/tar applies their
			// records to subsequent entries, whose resolved paths and types are
			// still validated below.
			continue
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("archive contains non-regular entry %s", header.Name)
		}
		if _, ok := seen[header.Name]; ok {
			return nil, fmt.Errorf("archive duplicates %s", header.Name)
		}
		seen[header.Name] = struct{}{}
		data, err := io.ReadAll(io.LimitReader(reader, 256<<20))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) != header.Size {
			return nil, fmt.Errorf("archive entry %s is truncated", header.Name)
		}
		entries = append(entries, archiveEntry{Name: header.Name, Mode: header.FileInfo().Mode(), ModTime: header.ModTime, Data: data})
	}
	return entries, nil
}

func readZip(file string) ([]archiveEntry, error) {
	reader, err := zip.OpenReader(file)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	entries := []archiveEntry{}
	seen := map[string]struct{}{}
	for _, file := range reader.File {
		if err := validateArchivePath(file.Name); err != nil {
			return nil, err
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if !file.Mode().IsRegular() {
			return nil, fmt.Errorf("archive contains non-regular entry %s", file.Name)
		}
		if _, ok := seen[file.Name]; ok {
			return nil, fmt.Errorf("archive duplicates %s", file.Name)
		}
		seen[file.Name] = struct{}{}
		input, err := file.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(input, 256<<20))
		closeErr := input.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if uint64(len(data)) != file.UncompressedSize64 {
			return nil, fmt.Errorf("archive entry %s is truncated", file.Name)
		}
		entries = append(entries, archiveEntry{Name: file.Name, Mode: file.Mode(), ModTime: file.Modified, Data: data})
	}
	return entries, nil
}

func validateArchivePath(name string) error {
	cleanName := strings.TrimSuffix(name, "/")
	if cleanName == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || path.Clean(cleanName) != cleanName || strings.HasPrefix(cleanName, "../") {
		return fmt.Errorf("archive contains unsafe path %q", name)
	}
	return nil
}

func verifyBinary(data []byte, manifest releaseManifest, artifact expectedArtifact, name string) error {
	info, err := buildinfo.Read(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("read Go build information from %s: %w", name, err)
	}
	if info.GoVersion != manifest.Tools.Go {
		return fmt.Errorf("%s was built with %s; expected %s", name, info.GoVersion, manifest.Tools.Go)
	}
	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	expectedSettings := map[string]string{
		"CGO_ENABLED": "0",
		"GOARCH":      artifact.Arch,
		"GOOS":        artifact.OS,
		"-trimpath":   "true",
	}
	for key, expected := range expectedSettings {
		if settings[key] != expected {
			return fmt.Errorf("%s build setting %s=%q; expected %q", name, key, settings[key], expected)
		}
	}
	for _, identity := range []string{manifest.Version, manifest.Commit, manifest.BuildDate} {
		if !bytes.Contains(data, []byte(identity)) {
			return fmt.Errorf("%s does not contain embedded identity %q", name, identity)
		}
	}
	if workingDirectory, err := os.Getwd(); err == nil && bytes.Contains(data, []byte(workingDirectory)) {
		return fmt.Errorf("%s contains the local build path", name)
	}
	return nil
}

func verifySourceArchive(file, version string) error {
	entries, err := readTarGzip(file)
	if err != nil {
		return err
	}
	prefix := fmt.Sprintf("%s_%s_source/", projectName, version)
	required := map[string]bool{
		prefix + "LICENSE":           false,
		prefix + "README.md":         false,
		prefix + "go.mod":            false,
		prefix + "release/README.md": false,
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name, prefix) {
			return fmt.Errorf("source archive entry is outside %s: %s", prefix, entry.Name)
		}
		if strings.Contains(entry.Name, "/.git/") {
			return errors.New("source archive contains Git metadata")
		}
		if _, ok := required[entry.Name]; ok {
			required[entry.Name] = true
		}
	}
	for name, found := range required {
		if !found {
			return fmt.Errorf("source archive is missing %s", name)
		}
	}
	return nil
}

func verifySBOM(directory string, manifest releaseManifest, artifact expectedArtifact) error {
	file := filepath.Join(directory, artifact.File)
	raw, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if bytes.Contains(raw, []byte("syft-archive-contents-")) {
		return fmt.Errorf("%s leaks a temporary Syft extraction path", artifact.File)
	}
	document, err := decodeJSONObject(file)
	if err != nil {
		return err
	}
	subjectDigest, _, err := digestFile(filepath.Join(directory, artifact.Subject))
	if err != nil {
		return err
	}
	switch artifact.Kind {
	case "sbom-spdx":
		if spdx, _ := document["spdxVersion"].(string); !strings.HasPrefix(spdx, "SPDX-") {
			return fmt.Errorf("%s is not SPDX JSON", artifact.File)
		}
		creation, ok := document["creationInfo"].(map[string]any)
		if !ok || creation["created"] != manifest.BuildDate {
			return fmt.Errorf("%s has a non-deterministic creation time", artifact.File)
		}
		expectedNamespace := fmt.Sprintf("%s/sbom/%s/%s-%s", repositoryURL, manifest.Version, artifact.Subject, subjectDigest)
		if document["documentNamespace"] != expectedNamespace {
			return fmt.Errorf("%s has the wrong document namespace", artifact.File)
		}
		if packages, ok := document["packages"].([]any); !ok || len(packages) == 0 {
			return fmt.Errorf("%s contains no packages", artifact.File)
		}
	case "sbom-cyclonedx":
		if document["bomFormat"] != "CycloneDX" {
			return fmt.Errorf("%s is not CycloneDX JSON", artifact.File)
		}
		if _, exists := document["serialNumber"]; exists {
			return fmt.Errorf("%s contains a random serial number", artifact.File)
		}
		metadata, ok := document["metadata"].(map[string]any)
		if !ok || metadata["timestamp"] != manifest.BuildDate {
			return fmt.Errorf("%s has a non-deterministic timestamp", artifact.File)
		}
		if components, ok := document["components"].([]any); !ok || len(components) == 0 {
			return fmt.Errorf("%s contains no components", artifact.File)
		}
	}
	return nil
}

func runHostSmoke(directory string, manifest releaseManifest) error {
	var host expectedArtifact
	for _, artifact := range expectedArtifacts(manifest.Version) {
		if artifact.Kind == "archive" && artifact.OS == runtime.GOOS && artifact.Arch == runtime.GOARCH {
			host = artifact
			break
		}
	}
	if host.File == "" {
		return fmt.Errorf("release target set does not contain host %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	entries, err := readArchive(filepath.Join(directory, host.File))
	if err != nil {
		return err
	}
	temporary, err := os.MkdirTemp("", "tcg-release-smoke-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	prefix := fmt.Sprintf("%s_%s_%s_%s/", projectName, manifest.Version, host.OS, host.Arch)
	extension := ""
	if host.OS == "windows" {
		extension = ".exe"
	}
	allowedBinaries := map[string]string{
		prefix + "telemetry-change-guard" + extension: "telemetry-change-guard" + extension,
		prefix + "tmr" + extension:                    "tmr" + extension,
	}
	binaries := map[string]string{}
	for _, entry := range entries {
		// Never derive a filesystem destination from an archive-controlled path.
		// Exact expected archive paths map to hard-coded destination basenames;
		// every other entry is ignored by the executable smoke test.
		base, allowed := allowedBinaries[entry.Name]
		if !allowed {
			continue
		}
		destination := filepath.Join(temporary, base)
		if err := os.WriteFile(destination, entry.Data, 0o755); err != nil {
			return err
		}
		binaries[strings.TrimSuffix(base, ".exe")] = destination
	}
	for _, name := range []string{"telemetry-change-guard", "tmr"} {
		if binaries[name] == "" {
			return fmt.Errorf("host archive is missing %s", name)
		}
		if err := verifyVersionCommand(binaries[name], manifest); err != nil {
			return err
		}
	}
	if _, err := os.Stat("examples/getting-started/tcg.yaml"); err == nil {
		if err := verifyGettingStarted(binaries["telemetry-change-guard"]); err != nil {
			return err
		}
	}
	return nil
}

func verifyVersionCommand(binary string, manifest releaseManifest) error {
	command := exec.Command(binary, "version", "--format", "json")
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("run %s version: %w", filepath.Base(binary), err)
	}
	info := productversion.Info{}
	if err := json.Unmarshal(output, &info); err != nil {
		return fmt.Errorf("decode %s version output: %w", filepath.Base(binary), err)
	}
	if info.SchemaVersion != productversion.SchemaVersion || info.Version != manifest.Version || info.Commit != manifest.Commit || info.BuildDate != manifest.BuildDate || info.OS != runtime.GOOS || info.Arch != runtime.GOARCH || info.Dirty == nil || *info.Dirty {
		return fmt.Errorf("%s reported inconsistent build identity", filepath.Base(binary))
	}
	human, err := exec.Command(binary, "--version").Output()
	if err != nil {
		return fmt.Errorf("run %s --version: %w", filepath.Base(binary), err)
	}
	if !bytes.Contains(human, []byte(manifest.Version)) || !bytes.Contains(human, []byte(manifest.Commit)) {
		return fmt.Errorf("%s human version output is incomplete", filepath.Base(binary))
	}
	return nil
}

func verifyGettingStarted(binary string) error {
	command := exec.Command(binary,
		"check",
		"--config", "examples/getting-started/tcg.yaml",
		"--changes", "examples/getting-started/changes.yaml",
		"--format", "json",
	)
	output, err := command.Output()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 2 {
		return fmt.Errorf("getting-started smoke exit = %v; expected 2", err)
	}
	result := struct {
		Status string `json:"status"`
	}{}
	if err := json.Unmarshal(output, &result); err != nil {
		return fmt.Errorf("decode getting-started result: %w", err)
	}
	if result.Status != "BLOCK" {
		return fmt.Errorf("getting-started status = %q; expected BLOCK", result.Status)
	}
	return nil
}

func sameSecond(left, right time.Time) bool {
	difference := left.UTC().Sub(right.UTC())
	if difference < 0 {
		difference = -difference
	}
	return difference < time.Second
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
