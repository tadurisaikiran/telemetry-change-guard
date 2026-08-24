package cloudformation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	assemblyVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	accountPattern         = regexp.MustCompile(`^[0-9]{12}$`)
	regionPattern          = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)
)

var supportedArtifactTypes = map[string]struct{}{
	"none":                     {},
	"aws:cloudformation:stack": {},
	"cdk:asset-manifest":       {},
	"cdk:cloud-assembly":       {},
	"cdk:feature-flag-report":  {},
	"cdk:tree":                 {},
}

// Loader ingests a standalone synthesized JSON template or a Cloud Assembly.
// It performs no external I/O beyond reading the explicitly selected local
// path and files referenced by a manifest within that assembly root.
type Loader struct {
	Limits Limits
}

// LoadPath loads a standalone JSON file, a manifest.json file, or a directory
// containing a Cloud Assembly manifest.json.
func (loader Loader) LoadPath(ctx context.Context, inputPath string) (Bundle, error) {
	limits, err := loader.normalizedLimits()
	if err != nil {
		return Bundle{}, err
	}
	if err := ctx.Err(); err != nil {
		return Bundle{}, fmt.Errorf("load CloudFormation input %q: %w", inputPath, err)
	}
	cleaned := filepath.Clean(inputPath)
	info, err := os.Stat(cleaned)
	if err != nil {
		return Bundle{}, fmt.Errorf("stat CloudFormation input %q: %w", cleaned, err)
	}
	if info.IsDir() {
		return loader.loadAssembly(ctx, cleaned, limits)
	}
	if !info.Mode().IsRegular() {
		return Bundle{}, fmt.Errorf("CloudFormation input %q is not a regular file or directory", cleaned)
	}
	if filepath.Base(cleaned) == "manifest.json" {
		return loader.loadAssembly(ctx, filepath.Dir(cleaned), limits)
	}

	file, err := os.Open(cleaned)
	if err != nil {
		return Bundle{}, fmt.Errorf("open CloudFormation template %q: %w", cleaned, err)
	}
	template, parseErr := loader.ParseTemplate(ctx, cleaned, file)
	closeErr := file.Close()
	if parseErr != nil {
		return Bundle{}, parseErr
	}
	if closeErr != nil {
		return Bundle{}, fmt.Errorf("close CloudFormation template %q: %w", cleaned, closeErr)
	}

	artifactID := strings.TrimSuffix(filepath.Base(cleaned), filepath.Ext(cleaned))
	if artifactID == "" || artifactID == "." {
		artifactID = "stack"
	}
	for index := range template.Resources {
		template.Resources[index].Provenance.ArtifactID = artifactID
	}
	return Bundle{
		Kind:   InputKindTemplate,
		Source: cleaned,
		Stacks: []Stack{{
			ID:         artifactID,
			ArtifactID: artifactID,
			Template:   template,
		}},
	}, nil
}

func (loader Loader) loadAssembly(ctx context.Context, directory string, limits Limits) (Bundle, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return Bundle{}, fmt.Errorf("open Cloud Assembly root %q: %w", directory, err)
	}
	state := assemblyState{
		ctx:      ctx,
		root:     root,
		rootPath: directory,
		limits:   limits,
		visited:  make(map[string]struct{}),
	}
	version, loadErr := state.loadManifest("manifest.json", nil, 1)
	closeErr := root.Close()
	if loadErr != nil {
		return Bundle{}, fmt.Errorf("load Cloud Assembly %q: %w", directory, loadErr)
	}
	if closeErr != nil {
		return Bundle{}, fmt.Errorf("close Cloud Assembly root %q: %w", directory, closeErr)
	}
	if len(state.stacks) == 0 {
		return Bundle{}, fmt.Errorf("load Cloud Assembly %q: assembly contains no CloudFormation stack artifacts", directory)
	}
	sort.Slice(state.stacks, func(i, j int) bool { return state.stacks[i].ID < state.stacks[j].ID })
	return Bundle{
		Kind:            InputKindCloudAssembly,
		Source:          directory,
		ManifestVersion: version,
		Stacks:          state.stacks,
	}, nil
}

type assemblyState struct {
	ctx                context.Context
	root               *os.Root
	rootPath           string
	limits             Limits
	visited            map[string]struct{}
	artifactCount      int
	stackCount         int
	totalTemplateBytes int64
	totalManifestBytes int64
	stacks             []Stack
}

type assemblyManifest struct {
	Version   string
	Artifacts map[string]json.RawMessage
	Missing   []json.RawMessage
}

type artifactDescriptor struct {
	Type         string          `json:"type"`
	Environment  string          `json:"environment"`
	Dependencies []string        `json:"dependencies"`
	DisplayName  string          `json:"displayName"`
	Properties   json.RawMessage `json:"properties"`
}

type stackProperties struct {
	TemplateFile string `json:"templateFile"`
	StackName    string `json:"stackName"`
}

type nestedAssemblyProperties struct {
	DirectoryName string `json:"directoryName"`
}

func (state *assemblyState) loadManifest(manifestFile string, assemblyPath []string, depth int) (string, error) {
	if err := state.ctx.Err(); err != nil {
		return "", err
	}
	if depth > state.limits.MaxAssemblyDepth {
		return "", fmt.Errorf("nested assembly depth exceeds the limit of %d", state.limits.MaxAssemblyDepth)
	}
	if _, seen := state.visited[manifestFile]; seen {
		return "", fmt.Errorf("assembly manifest %q is referenced more than once or recursively", manifestFile)
	}
	state.visited[manifestFile] = struct{}{}

	contents, err := state.readFile(manifestFile, state.limits.MaxManifestBytes)
	if err != nil {
		return "", fmt.Errorf("read manifest %q: %w", manifestFile, err)
	}
	state.totalManifestBytes += int64(len(contents))
	if state.totalManifestBytes > state.limits.MaxTotalManifestBytes {
		return "", fmt.Errorf("total manifest bytes exceed the limit of %d", state.limits.MaxTotalManifestBytes)
	}
	manifest, err := parseAssemblyManifest(contents, state.limits)
	if err != nil {
		return "", fmt.Errorf("parse manifest %q: %w", manifestFile, err)
	}
	if len(manifest.Missing) != 0 {
		return "", fmt.Errorf("manifest %q contains %d missing context entries; synthesize it with complete context", manifestFile, len(manifest.Missing))
	}
	state.artifactCount += len(manifest.Artifacts)
	if state.artifactCount > state.limits.MaxArtifacts {
		return "", fmt.Errorf("artifact count exceeds the limit of %d", state.limits.MaxArtifacts)
	}

	artifactIDs := make([]string, 0, len(manifest.Artifacts))
	descriptors := make(map[string]artifactDescriptor, len(manifest.Artifacts))
	for artifactID, raw := range manifest.Artifacts {
		if strings.TrimSpace(artifactID) == "" || artifactID != strings.TrimSpace(artifactID) {
			return "", fmt.Errorf("manifest %q contains an empty or whitespace-padded artifact ID", manifestFile)
		}
		descriptor, err := decodeArtifactDescriptor(raw)
		if err != nil {
			return "", fmt.Errorf("artifact %q must be an object with valid field types: %w", artifactID, err)
		}
		if _, supported := supportedArtifactTypes[descriptor.Type]; !supported {
			return "", fmt.Errorf("artifact %q has unsupported type %q", artifactID, descriptor.Type)
		}
		dependencies := make(map[string]struct{}, len(descriptor.Dependencies))
		for _, dependency := range descriptor.Dependencies {
			if dependency == "" || dependency != strings.TrimSpace(dependency) {
				return "", fmt.Errorf("artifact %q contains an empty or whitespace-padded dependency", artifactID)
			}
			if _, duplicate := dependencies[dependency]; duplicate {
				return "", fmt.Errorf("artifact %q contains duplicate dependency %q", artifactID, dependency)
			}
			dependencies[dependency] = struct{}{}
		}
		descriptors[artifactID] = descriptor
		artifactIDs = append(artifactIDs, artifactID)
	}
	sort.Strings(artifactIDs)
	if err := validateArtifactGraph(state.ctx, artifactIDs, descriptors); err != nil {
		return "", fmt.Errorf("manifest %q: %w", manifestFile, err)
	}

	manifestDirectory := path.Dir(manifestFile)
	for _, artifactID := range artifactIDs {
		if err := state.ctx.Err(); err != nil {
			return "", err
		}
		descriptor := descriptors[artifactID]
		switch descriptor.Type {
		case "aws:cloudformation:stack":
			if err := state.loadStack(manifestFile, manifestDirectory, assemblyPath, artifactID, descriptor); err != nil {
				return "", err
			}
		case "cdk:cloud-assembly":
			var properties nestedAssemblyProperties
			if err := unmarshalProperties(artifactID, descriptor.Properties, &properties); err != nil {
				return "", err
			}
			directoryName, err := safeManifestPath(properties.DirectoryName, "directoryName")
			if err != nil || directoryName == "." {
				if err == nil {
					err = errors.New("directoryName must identify a child directory")
				}
				return "", fmt.Errorf("artifact %q: %w", artifactID, err)
			}
			nestedManifest := joinAssemblyPath(manifestDirectory, directoryName, "manifest.json")
			if _, err := state.loadManifest(nestedManifest, appendCopy(assemblyPath, artifactID), depth+1); err != nil {
				return "", fmt.Errorf("nested assembly artifact %q: %w", artifactID, err)
			}
		}
	}
	return manifest.Version, nil
}

func (state *assemblyState) loadStack(
	manifestFile string,
	manifestDirectory string,
	assemblyPath []string,
	artifactID string,
	descriptor artifactDescriptor,
) error {
	var properties stackProperties
	if err := unmarshalProperties(artifactID, descriptor.Properties, &properties); err != nil {
		return err
	}
	templateName, err := safeManifestPath(properties.TemplateFile, "templateFile")
	if err != nil || templateName == "." {
		if err == nil {
			err = errors.New("templateFile must identify a file")
		}
		return fmt.Errorf("artifact %q: %w", artifactID, err)
	}
	templateFile := joinAssemblyPath(manifestDirectory, templateName)
	contents, err := state.readFile(templateFile, state.limits.MaxTemplateBytes)
	if err != nil {
		return fmt.Errorf("artifact %q read template %q: %w", artifactID, templateFile, err)
	}
	state.totalTemplateBytes += int64(len(contents))
	if state.totalTemplateBytes > state.limits.MaxTotalTemplateBytes {
		return fmt.Errorf("total template bytes exceed the limit of %d", state.limits.MaxTotalTemplateBytes)
	}
	state.stackCount++
	if state.stackCount > state.limits.MaxStacks {
		return fmt.Errorf("stack count exceeds the limit of %d", state.limits.MaxStacks)
	}

	displayTemplate := state.displayPath(templateFile)
	template, err := parseTemplateBytes(displayTemplate, contents, state.limits)
	if err != nil {
		return fmt.Errorf("artifact %q parse template %q: %w", artifactID, templateFile, err)
	}
	environment, err := parseEnvironment(descriptor.Environment)
	if err != nil {
		return fmt.Errorf("artifact %q: %w", artifactID, err)
	}
	if properties.StackName != strings.TrimSpace(properties.StackName) {
		return fmt.Errorf("artifact %q stackName must not have surrounding whitespace", artifactID)
	}
	if descriptor.DisplayName != strings.TrimSpace(descriptor.DisplayName) {
		return fmt.Errorf("artifact %q displayName must not have surrounding whitespace", artifactID)
	}

	qualifiedDependencies := make([]string, len(descriptor.Dependencies))
	for index, dependency := range descriptor.Dependencies {
		qualifiedDependencies[index] = qualifyArtifact(assemblyPath, dependency)
	}
	sort.Strings(qualifiedDependencies)
	manifestDisplay := state.displayPath(manifestFile)
	for index := range template.Resources {
		template.Resources[index].Provenance = Provenance{
			ManifestFile: manifestDisplay,
			AssemblyPath: appendCopy(nil, assemblyPath...),
			ArtifactID:   artifactID,
			StackName:    properties.StackName,
			TemplateFile: displayTemplate,
			LogicalID:    template.Resources[index].LogicalID,
		}
	}
	state.stacks = append(state.stacks, Stack{
		ID:           qualifyArtifact(assemblyPath, artifactID),
		ArtifactID:   artifactID,
		AssemblyPath: appendCopy(nil, assemblyPath...),
		DisplayName:  descriptor.DisplayName,
		StackName:    properties.StackName,
		Environment:  environment,
		Dependencies: qualifiedDependencies,
		ManifestFile: manifestDisplay,
		Template:     template,
	})
	return nil
}

func parseAssemblyManifest(contents []byte, limits Limits) (assemblyManifest, error) {
	if len(contents) == 0 {
		return assemblyManifest{}, errors.New("manifest is empty")
	}
	if err := validateStrictJSON(contents, limits); err != nil {
		return assemblyManifest{}, err
	}
	if trimmed := bytes.TrimSpace(contents); len(trimmed) == 0 || trimmed[0] != '{' {
		return assemblyManifest{}, errors.New("manifest root must be a JSON object")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(contents, &raw); err != nil || raw == nil {
		return assemblyManifest{}, errors.New("manifest root must be a JSON object")
	}
	var manifest assemblyManifest
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return assemblyManifest{}, fmt.Errorf("decode manifest fields: %w", err)
	}
	if err := validateAssemblyVersion(manifest.Version); err != nil {
		return assemblyManifest{}, err
	}
	if missing, exists := raw["missing"]; exists && isJSONNull(missing) {
		return assemblyManifest{}, errors.New("missing must be an array when present")
	}
	if artifacts, exists := raw["artifacts"]; exists && isJSONNull(artifacts) {
		return assemblyManifest{}, errors.New("artifacts must be an object when present")
	}
	return manifest, nil
}

func validateAssemblyVersion(version string) error {
	matches := assemblyVersionPattern.FindStringSubmatch(version)
	if matches == nil {
		return fmt.Errorf("Cloud Assembly version %q is not valid semantic versioning", version)
	}
	major, err := strconv.Atoi(matches[1])
	if err != nil || major < 1 {
		return fmt.Errorf("Cloud Assembly version %q has an invalid major version", version)
	}
	if major > MaxSupportedAssemblyMajor {
		return fmt.Errorf("Cloud Assembly schema major %d is newer than supported major %d", major, MaxSupportedAssemblyMajor)
	}
	return nil
}

func validateArtifactGraph(ctx context.Context, ids []string, descriptors map[string]artifactDescriptor) error {
	for _, artifactID := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, dependency := range descriptors[artifactID].Dependencies {
			if _, exists := descriptors[dependency]; !exists {
				return fmt.Errorf("artifact %q depends on unknown artifact %q", artifactID, dependency)
			}
		}
	}
	state := make(map[string]uint8, len(ids))
	var visit func(string) error
	visit = func(artifactID string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch state[artifactID] {
		case 1:
			return fmt.Errorf("artifact dependency graph contains a cycle at %q", artifactID)
		case 2:
			return nil
		}
		state[artifactID] = 1
		dependencies := append([]string(nil), descriptors[artifactID].Dependencies...)
		sort.Strings(dependencies)
		for _, dependency := range dependencies {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[artifactID] = 2
		return nil
	}
	for _, artifactID := range ids {
		if err := visit(artifactID); err != nil {
			return err
		}
	}
	return nil
}

func (state *assemblyState) readFile(name string, maximum int64) ([]byte, error) {
	if err := state.ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := safeManifestPath(name, "assembly path"); err != nil {
		return nil, err
	}
	file, err := state.root.Open(name)
	if err != nil {
		return nil, err
	}
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return nil, statErr
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, errors.New("referenced path is not a regular file")
	}
	contents, readErr := readBounded(file, maximum)
	closeErr := file.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err := state.ctx.Err(); err != nil {
		return nil, err
	}
	return contents, nil
}

func safeManifestPath(value string, field string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if strings.Contains(value, `\`) || !fs.ValidPath(value) {
		return "", fmt.Errorf("%s %q is not a safe relative assembly path", field, value)
	}
	return value, nil
}

func joinAssemblyPath(elements ...string) string {
	joined := path.Join(elements...)
	if joined == "." {
		return joined
	}
	return strings.TrimPrefix(joined, "./")
}

func (state *assemblyState) displayPath(relative string) string {
	return filepath.Clean(filepath.Join(state.rootPath, filepath.FromSlash(relative)))
}

func unmarshalProperties(artifactID string, raw json.RawMessage, destination any) error {
	if len(raw) == 0 || isJSONNull(raw) {
		return fmt.Errorf("artifact %q properties are required", artifactID)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return fmt.Errorf("artifact %q properties must be a JSON object", artifactID)
	}
	if err := json.Unmarshal(raw, destination); err != nil {
		return fmt.Errorf("artifact %q properties must be an object with valid field types: %w", artifactID, err)
	}
	return nil
}

func decodeArtifactDescriptor(raw json.RawMessage) (artifactDescriptor, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return artifactDescriptor{}, errors.New("descriptor must be a JSON object")
	}
	for _, field := range []string{"type", "environment", "dependencies", "displayName", "properties"} {
		if value, exists := fields[field]; exists && isJSONNull(value) {
			return artifactDescriptor{}, fmt.Errorf("%s must not be null", field)
		}
	}
	var descriptor artifactDescriptor
	if err := json.Unmarshal(raw, &descriptor); err != nil {
		return artifactDescriptor{}, err
	}
	if descriptor.Type == "" {
		return artifactDescriptor{}, errors.New("type is required")
	}
	return descriptor, nil
}

func parseEnvironment(raw string) (Environment, error) {
	if raw == "" {
		return Environment{}, nil
	}
	if raw != strings.TrimSpace(raw) {
		return Environment{}, errors.New("environment must not have surrounding whitespace")
	}
	address, ok := strings.CutPrefix(raw, "aws://")
	if !ok {
		return Environment{}, fmt.Errorf("environment %q must use aws://account/region", raw)
	}
	account, region, ok := strings.Cut(address, "/")
	if !ok || strings.Contains(region, "/") {
		return Environment{}, fmt.Errorf("environment %q must use aws://account/region", raw)
	}
	if account != "unknown-account" && !accountPattern.MatchString(account) {
		return Environment{}, fmt.Errorf("environment account %q must be a 12-digit AWS account ID or unknown-account", account)
	}
	if region != "unknown-region" && !regionPattern.MatchString(region) {
		return Environment{}, fmt.Errorf("environment Region %q is invalid", region)
	}
	return Environment{Raw: raw, Account: account, Region: region}, nil
}

func qualifyArtifact(assemblyPath []string, artifactID string) string {
	segments := appendCopy(assemblyPath, artifactID)
	for index, segment := range segments {
		segment = strings.ReplaceAll(segment, "~", "~0")
		segments[index] = strings.ReplaceAll(segment, "/", "~1")
	}
	return strings.Join(segments, "/")
}

func appendCopy(values []string, additions ...string) []string {
	result := make([]string, 0, len(values)+len(additions))
	result = append(result, values...)
	return append(result, additions...)
}

func (loader Loader) normalizedLimits() (Limits, error) {
	limits := loader.Limits
	defaults := DefaultLimits()
	if limits.MaxTemplateBytes == 0 {
		limits.MaxTemplateBytes = defaults.MaxTemplateBytes
	}
	if limits.MaxManifestBytes == 0 {
		limits.MaxManifestBytes = defaults.MaxManifestBytes
	}
	if limits.MaxTotalTemplateBytes == 0 {
		limits.MaxTotalTemplateBytes = defaults.MaxTotalTemplateBytes
	}
	if limits.MaxTotalManifestBytes == 0 {
		limits.MaxTotalManifestBytes = defaults.MaxTotalManifestBytes
	}
	if limits.MaxStacks == 0 {
		limits.MaxStacks = defaults.MaxStacks
	}
	if limits.MaxArtifacts == 0 {
		limits.MaxArtifacts = defaults.MaxArtifacts
	}
	if limits.MaxResources == 0 {
		limits.MaxResources = defaults.MaxResources
	}
	if limits.MaxAssemblyDepth == 0 {
		limits.MaxAssemblyDepth = defaults.MaxAssemblyDepth
	}
	if limits.MaxJSONDepth == 0 {
		limits.MaxJSONDepth = defaults.MaxJSONDepth
	}
	if limits.MaxJSONValues == 0 {
		limits.MaxJSONValues = defaults.MaxJSONValues
	}
	if limits.MaxTemplateBytes < 0 || limits.MaxManifestBytes < 0 || limits.MaxTotalTemplateBytes < 0 || limits.MaxTotalManifestBytes < 0 ||
		limits.MaxStacks < 0 || limits.MaxArtifacts < 0 || limits.MaxResources < 0 ||
		limits.MaxAssemblyDepth < 0 || limits.MaxJSONDepth < 0 || limits.MaxJSONValues < 0 {
		return Limits{}, errors.New("CloudFormation loader limits must be positive")
	}
	return limits, nil
}
