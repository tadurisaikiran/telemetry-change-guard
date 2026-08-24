package domain

const (
	// ChangeSetAPIVersion is the initial generic telemetry change schema.
	ChangeSetAPIVersion = "tcg/v1alpha1"
	// ChangeSetKind is the required generic change document kind.
	ChangeSetKind = "ChangeSet"
)

// ChangeSetMetadata identifies a proposed set of telemetry changes.
type ChangeSetMetadata struct {
	Name string `json:"name"`
}

// ChangeSet is the canonical input to generic discovery and impact analysis.
// It contains proposed changes only; evidence and policy results live in
// separate models.
type ChangeSet struct {
	APIVersion  string            `json:"apiVersion"`
	Kind        string            `json:"kind"`
	Metadata    ChangeSetMetadata `json:"metadata"`
	Description string            `json:"description,omitempty"`
	Changes     []Change          `json:"changes"`
}
