package domain

const (
	// MigrationAPIVersion is the only manifest version supported by v0.1.
	MigrationAPIVersion = "telemetry-migration/v1alpha1"
	// MigrationKind is the required YAML document kind.
	MigrationKind = "Migration"
)

// MigrationMetadata identifies a migration.
type MigrationMetadata struct {
	Name string
}

// Migration is the canonical, validated representation of a migration
// manifest.
type Migration struct {
	APIVersion  string
	Kind        string
	Metadata    MigrationMetadata
	Description string
	Changes     []Change
}
