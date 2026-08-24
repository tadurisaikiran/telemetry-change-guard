package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
	"gopkg.in/yaml.v3"
)

const (
	maxChangeSetManifestBytes = 1 << 20
	maxChangeMetadataEntries  = 64
	maxChangeMetadataKeyBytes = 128
	maxChangeMetadataValBytes = 4096
)

type changeSetDocument struct {
	APIVersion string                    `yaml:"apiVersion"`
	Kind       string                    `yaml:"kind"`
	Metadata   changeSetMetadataDocument `yaml:"metadata"`
	Spec       changeSetSpecDocument     `yaml:"spec"`
}

type changeSetMetadataDocument struct {
	Name string `yaml:"name"`
}

type changeSetSpecDocument struct {
	Description string                    `yaml:"description,omitempty"`
	Changes     []canonicalChangeDocument `yaml:"changes"`
}

type canonicalChangeDocument struct {
	ID       string                   `yaml:"id"`
	Kind     domain.ChangeKind        `yaml:"kind"`
	Domain   domain.Domain            `yaml:"domain"`
	From     canonicalSymbolDocument  `yaml:"from"`
	To       *canonicalSymbolDocument `yaml:"to,omitempty"`
	Metadata map[string]string        `yaml:"metadata,omitempty"`
}

type canonicalSymbolDocument struct {
	Domain domain.Domain     `yaml:"domain"`
	Kind   domain.SymbolKind `yaml:"kind"`
	Name   string            `yaml:"name"`
	Parent string            `yaml:"parent,omitempty"`
}

// MarshalChangeSet serializes a validated ChangeSet as its canonical YAML
// manifest envelope. Generated change sources use this instead of exposing
// internal document structs or relying on the machine-result JSON shape.
func MarshalChangeSet(changeSet domain.ChangeSet) ([]byte, error) {
	if err := ValidateChangeSet(changeSet); err != nil {
		return nil, err
	}
	document := changeSetDocument{
		APIVersion: changeSet.APIVersion,
		Kind:       changeSet.Kind,
		Metadata:   changeSetMetadataDocument{Name: changeSet.Metadata.Name},
		Spec: changeSetSpecDocument{
			Description: changeSet.Description,
			Changes:     make([]canonicalChangeDocument, 0, len(changeSet.Changes)),
		},
	}
	for _, change := range changeSet.Changes {
		var destination *canonicalSymbolDocument
		if change.To != nil {
			value := canonicalSymbolFromDomain(*change.To)
			destination = &value
		}
		document.Spec.Changes = append(document.Spec.Changes, canonicalChangeDocument{
			ID:       change.ID,
			Kind:     change.Kind,
			Domain:   change.Domain,
			From:     canonicalSymbolFromDomain(change.From),
			To:       destination,
			Metadata: cloneMetadata(change.Metadata),
		})
	}
	contents, err := yaml.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode change set manifest: %w", err)
	}
	return contents, nil
}

func canonicalSymbolFromDomain(symbol domain.Symbol) canonicalSymbolDocument {
	return canonicalSymbolDocument{
		Domain: symbol.Domain,
		Kind:   symbol.Kind,
		Name:   symbol.Name,
		Parent: symbol.Parent,
	}
}

// LoadChangeSet reads and validates one native ChangeSet manifest from path.
func LoadChangeSet(ctx context.Context, path string) (domain.ChangeSet, error) {
	if err := ctx.Err(); err != nil {
		return domain.ChangeSet{}, fmt.Errorf("load change set %q: %w", path, err)
	}

	file, err := os.Open(path)
	if err != nil {
		return domain.ChangeSet{}, fmt.Errorf("open change set %q: %w", path, err)
	}
	defer file.Close()

	changeSet, err := ParseChangeSet(file)
	if err != nil {
		return domain.ChangeSet{}, fmt.Errorf("load change set %q: %w", path, err)
	}
	if err := ctx.Err(); err != nil {
		return domain.ChangeSet{}, fmt.Errorf("load change set %q: %w", path, err)
	}
	return changeSet, nil
}

// ParseChangeSet strictly decodes and validates one native ChangeSet manifest.
// Legacy Migration manifests use ParseMigration and NormalizeMigration.
func ParseChangeSet(reader io.Reader) (domain.ChangeSet, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maxChangeSetManifestBytes+1))
	if err != nil {
		return domain.ChangeSet{}, fmt.Errorf("read change set manifest: %w", err)
	}
	if len(contents) > maxChangeSetManifestBytes {
		return domain.ChangeSet{}, fmt.Errorf(
			"change set manifest exceeds the %d-byte size limit",
			maxChangeSetManifestBytes,
		)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	var document changeSetDocument
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return domain.ChangeSet{}, errors.New("change set manifest is empty")
		}
		return domain.ChangeSet{}, fmt.Errorf("decode change set manifest: %w", err)
	}

	var additionalDocument any
	if err := decoder.Decode(&additionalDocument); err == nil {
		return domain.ChangeSet{}, errors.New("change set manifest must contain exactly one YAML document")
	} else if !errors.Is(err, io.EOF) {
		return domain.ChangeSet{}, fmt.Errorf("decode trailing change set document: %w", err)
	}

	changeSet := document.toDomain()
	if err := ValidateChangeSet(changeSet); err != nil {
		return domain.ChangeSet{}, err
	}
	return changeSet, nil
}

// NormalizeMigration validates and deep-copies a legacy Migration into the
// generic ChangeSet model. The caller and returned value share no mutable maps
// or slices.
func NormalizeMigration(migration domain.Migration) (domain.ChangeSet, error) {
	if err := ValidateMigration(migration); err != nil {
		return domain.ChangeSet{}, err
	}
	changeSet := domain.ChangeSet{
		APIVersion:  domain.ChangeSetAPIVersion,
		Kind:        domain.ChangeSetKind,
		Metadata:    domain.ChangeSetMetadata{Name: migration.Metadata.Name},
		Description: migration.Description,
		Changes:     cloneChanges(migration.Changes),
	}
	if err := ValidateChangeSet(changeSet); err != nil {
		return domain.ChangeSet{}, fmt.Errorf("validate normalized change set: %w", err)
	}
	return changeSet, nil
}

func (document changeSetDocument) toDomain() domain.ChangeSet {
	changes := make([]domain.Change, 0, len(document.Spec.Changes))
	for _, change := range document.Spec.Changes {
		var destination *domain.Symbol
		if change.To != nil {
			value := change.To.toDomain()
			destination = &value
		}
		changes = append(changes, domain.Change{
			ID:       change.ID,
			Kind:     change.Kind,
			Domain:   change.Domain,
			From:     change.From.toDomain(),
			To:       destination,
			Metadata: cloneMetadata(change.Metadata),
		})
	}
	return domain.ChangeSet{
		APIVersion:  document.APIVersion,
		Kind:        document.Kind,
		Metadata:    domain.ChangeSetMetadata{Name: document.Metadata.Name},
		Description: document.Spec.Description,
		Changes:     changes,
	}
}

func (document canonicalSymbolDocument) toDomain() domain.Symbol {
	return domain.Symbol{
		Domain: document.Domain,
		Kind:   document.Kind,
		Name:   document.Name,
		Parent: document.Parent,
	}
}

func cloneChanges(source []domain.Change) []domain.Change {
	result := make([]domain.Change, len(source))
	for index, change := range source {
		result[index] = change
		result[index].Metadata = cloneMetadata(change.Metadata)
		if change.To != nil {
			destination := *change.To
			result[index].To = &destination
		}
	}
	return result
}

func cloneMetadata(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
