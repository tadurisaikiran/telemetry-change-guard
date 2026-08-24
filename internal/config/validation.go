package config

import (
	"fmt"
	"strings"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
)

// FieldError describes one invalid manifest field.
type FieldError struct {
	Path    string
	Message string
}

// ValidationError contains every deterministic validation issue found in a
// migration manifest. Issues retain discovery order so CLI output and tests
// remain stable.
type ValidationError struct {
	Issues []FieldError
}

// Error returns a human-readable, multi-line validation report.
func (e *ValidationError) Error() string {
	if e == nil || len(e.Issues) == 0 {
		return "migration manifest is invalid"
	}

	var builder strings.Builder
	builder.WriteString("migration manifest is invalid:")
	for _, issue := range e.Issues {
		fmt.Fprintf(&builder, "\n  - %s: %s", issue.Path, issue.Message)
	}

	return builder.String()
}

func (e *ValidationError) add(path, message string) {
	e.Issues = append(e.Issues, FieldError{Path: path, Message: message})
}

func (e *ValidationError) append(other *ValidationError) {
	if other != nil {
		e.Issues = append(e.Issues, other.Issues...)
	}
}

func (e *ValidationError) errOrNil() error {
	if len(e.Issues) == 0 {
		return nil
	}
	return e
}

// ValidateMigration validates a canonical migration. It is kept separate from
// YAML decoding so later change sources can reuse the same invariants.
func ValidateMigration(migration domain.Migration) error {
	issues := &ValidationError{}

	if migration.APIVersion != domain.MigrationAPIVersion {
		issues.add("apiVersion", fmt.Sprintf("must be %q", domain.MigrationAPIVersion))
	}
	if migration.Kind != domain.MigrationKind {
		issues.add("kind", fmt.Sprintf("must be %q", domain.MigrationKind))
	}
	if isBlank(migration.Metadata.Name) {
		issues.add("metadata.name", "is required")
	}
	if len(migration.Changes) == 0 {
		issues.add("spec.changes", "must contain at least one change")
	}

	seenIDs := make(map[string]int, len(migration.Changes))
	for index, change := range migration.Changes {
		path := fmt.Sprintf("spec.changes[%d]", index)

		if isBlank(change.ID) {
			issues.add(path+".id", "is required")
		} else if previous, exists := seenIDs[change.ID]; exists {
			issues.add(path+".id", fmt.Sprintf("duplicates spec.changes[%d].id", previous))
		} else {
			seenIDs[change.ID] = index
		}

		if !isSupportedDomain(change.Domain) {
			issues.add(path+".domain", fmt.Sprintf("unsupported domain %q; supported domains: prometheus", change.Domain))
		}
		if !isSupportedChangeKind(change.Kind) {
			issues.add(path+".kind", fmt.Sprintf("unsupported change kind %q", change.Kind))
			continue
		}

		expectedKind := expectedSymbolKind(change.Kind)
		validateSymbol(issues, path+".from", change.From, change.Domain, expectedKind)
		if expectedKind == domain.SymbolKindLabel && isBlank(change.From.Parent) {
			issues.add(path+".metric", "parent metric is required for a label change")
		}

		if isRename(change.Kind) {
			if change.To == nil {
				issues.add(path+".to", "is required for a rename")
				continue
			}

			validateSymbol(issues, path+".to", *change.To, change.Domain, expectedKind)
			if change.From.Name == change.To.Name && !isBlank(change.From.Name) {
				issues.add(path+".to", "must differ from the source of a rename")
			}
			if expectedKind == domain.SymbolKindLabel &&
				change.From.Parent != change.To.Parent {
				issues.add(path+".to.parent", "must match the source parent metric")
			}
		} else if change.To != nil {
			issues.add(path+".to", "must be omitted for a removal")
		}
	}

	return issues.errOrNil()
}

func validateSymbol(
	issues *ValidationError,
	path string,
	symbol domain.Symbol,
	expectedDomain domain.Domain,
	expectedKind domain.SymbolKind,
) {
	if symbol.Domain != expectedDomain {
		issues.add(path+".domain", "must match the change domain")
	}
	if symbol.Kind != expectedKind {
		issues.add(path+".kind", fmt.Sprintf("must be %q", expectedKind))
	}
	if isBlank(symbol.Name) {
		issues.add(path+"."+string(expectedKind), "is required")
	}

	if expectedKind == domain.SymbolKindMetric && symbol.Parent != "" {
		issues.add(path+".parent", "must be empty for a metric change")
	}
}

func isBlank(value string) bool {
	return strings.TrimSpace(value) == ""
}

func isSupportedDomain(value domain.Domain) bool {
	return value == domain.DomainPrometheus
}

func isSupportedChangeKind(value domain.ChangeKind) bool {
	switch value {
	case domain.ChangeKindMetricRename,
		domain.ChangeKindMetricRemove,
		domain.ChangeKindLabelRename,
		domain.ChangeKindLabelRemove:
		return true
	default:
		return false
	}
}

func isRename(value domain.ChangeKind) bool {
	return value == domain.ChangeKindMetricRename || value == domain.ChangeKindLabelRename
}

func expectedSymbolKind(value domain.ChangeKind) domain.SymbolKind {
	if value == domain.ChangeKindLabelRename || value == domain.ChangeKindLabelRemove {
		return domain.SymbolKindLabel
	}
	return domain.SymbolKindMetric
}
