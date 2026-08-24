// Package cloudformation loads synthesized AWS CloudFormation JSON without
// executing templates, transforms, CDK applications, or asset commands.
package cloudformation

import "encoding/json"

const (
	// MaxSupportedAssemblyMajor is the newest Cloud Assembly schema revision
	// this loader has been tested against.
	MaxSupportedAssemblyMajor = 54

	defaultMaxTemplateBytes      = int64(1 << 20)
	defaultMaxManifestBytes      = int64(8 << 20)
	defaultMaxTotalTemplateBytes = int64(64 << 20)
	defaultMaxTotalManifestBytes = int64(64 << 20)
	defaultMaxStacks             = 512
	defaultMaxArtifacts          = 4096
	defaultMaxResources          = 500
	defaultMaxAssemblyDepth      = 16
	defaultMaxJSONDepth          = 128
	defaultMaxJSONValues         = 1_000_000
)

// Limits bounds all data controlled by a template or Cloud Assembly. Zero
// fields use the production defaults. Negative fields are invalid.
type Limits struct {
	MaxTemplateBytes      int64
	MaxManifestBytes      int64
	MaxTotalTemplateBytes int64
	MaxTotalManifestBytes int64
	MaxStacks             int
	MaxArtifacts          int
	MaxResources          int
	MaxAssemblyDepth      int
	MaxJSONDepth          int
	MaxJSONValues         int
}

// DefaultLimits returns the production loader limits. The per-template byte
// and resource limits match CloudFormation quotas; aggregate limits protect a
// workstation or CI runner while loading a multi-stack assembly.
func DefaultLimits() Limits {
	return Limits{
		MaxTemplateBytes:      defaultMaxTemplateBytes,
		MaxManifestBytes:      defaultMaxManifestBytes,
		MaxTotalTemplateBytes: defaultMaxTotalTemplateBytes,
		MaxTotalManifestBytes: defaultMaxTotalManifestBytes,
		MaxStacks:             defaultMaxStacks,
		MaxArtifacts:          defaultMaxArtifacts,
		MaxResources:          defaultMaxResources,
		MaxAssemblyDepth:      defaultMaxAssemblyDepth,
		MaxJSONDepth:          defaultMaxJSONDepth,
		MaxJSONValues:         defaultMaxJSONValues,
	}
}

// InputKind identifies the trusted input boundary selected by LoadPath.
type InputKind string

const (
	InputKindTemplate      InputKind = "standalone_template"
	InputKindCloudAssembly InputKind = "cloud_assembly"
)

// Bundle is a deterministic set of synthesized stacks.
type Bundle struct {
	Kind            InputKind `json:"kind"`
	Source          string    `json:"source"`
	ManifestVersion string    `json:"manifestVersion,omitempty"`
	Stacks          []Stack   `json:"stacks"`
}

// Stack represents one synthesized CloudFormation stack artifact.
type Stack struct {
	ID           string      `json:"id"`
	ArtifactID   string      `json:"artifactId"`
	AssemblyPath []string    `json:"assemblyPath,omitempty"`
	DisplayName  string      `json:"displayName,omitempty"`
	StackName    string      `json:"stackName,omitempty"`
	Environment  Environment `json:"environment"`
	Dependencies []string    `json:"dependencies,omitempty"`
	ManifestFile string      `json:"manifestFile,omitempty"`
	Template     Template    `json:"template"`
}

// Environment is the stack's synthesis target. Empty fields are not inferred.
// Environment-agnostic CDK stacks use the explicit unknown-account and
// unknown-region values emitted in their assembly manifest.
type Environment struct {
	Raw     string `json:"raw,omitempty"`
	Account string `json:"account,omitempty"`
	Region  string `json:"region,omitempty"`
}

// Template retains all sections needed by later semantic phases. Named maps
// and resources are converted to sorted slices so callers cannot accidentally
// depend on Go map iteration order.
type Template struct {
	Source        string          `json:"source"`
	FormatVersion string          `json:"formatVersion,omitempty"`
	Description   string          `json:"description,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
	Parameters    []NamedValue    `json:"parameters,omitempty"`
	Rules         []NamedValue    `json:"rules,omitempty"`
	Mappings      []NamedValue    `json:"mappings,omitempty"`
	Conditions    []NamedValue    `json:"conditions,omitempty"`
	Transform     json.RawMessage `json:"transform,omitempty"`
	Resources     []Resource      `json:"resources"`
	Outputs       []NamedValue    `json:"outputs,omitempty"`
}

// NamedValue preserves an unresolved CloudFormation value under a stable key.
type NamedValue struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

// Resource preserves the complete unresolved resource definition while also
// exposing its validated type and properties.
type Resource struct {
	LogicalID  string          `json:"logicalId"`
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties,omitempty"`
	Definition json.RawMessage `json:"definition"`
	Provenance Provenance      `json:"provenance"`
}

// Provenance identifies the synthesized input location of a resource.
type Provenance struct {
	ManifestFile string   `json:"manifestFile,omitempty"`
	AssemblyPath []string `json:"assemblyPath,omitempty"`
	ArtifactID   string   `json:"artifactId"`
	StackName    string   `json:"stackName,omitempty"`
	TemplateFile string   `json:"templateFile"`
	LogicalID    string   `json:"logicalId"`
}
