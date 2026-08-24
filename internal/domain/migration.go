package domain

const (
	// MigrationAPIVersion is the only manifest version supported by v0.1.
	MigrationAPIVersion = "telemetry-migration/v1alpha1"
	// MigrationKind is the required YAML document kind.
	MigrationKind = "Migration"
)

// MigrationMetadata identifies a migration.
type MigrationMetadata struct {
	Name string `json:"name"`
}

// Migration is the canonical, validated representation of a migration
// manifest.
type Migration struct {
	APIVersion  string            `json:"apiVersion"`
	Kind        string            `json:"kind"`
	Metadata    MigrationMetadata `json:"metadata"`
	Description string            `json:"description,omitempty"`
	Changes     []Change          `json:"changes"`
}
