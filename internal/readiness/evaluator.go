// Package readiness owns deterministic migration classification and safety
// decisions. Optional AI components may consume these results but cannot alter
// them.
package readiness

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/graph"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/impact"
)

const ResultSchemaVersion = "tmr-result/v1alpha1"

// Status is the authoritative migration readiness state.
type Status string

const (
	StatusReady      Status = "READY"
	StatusBlocked    Status = "BLOCKED"
	StatusIncomplete Status = "INCOMPLETE"
	StatusError      Status = "ERROR"
)

// Classification describes one consumer's state for a change.
type Classification string

const (
	ClassificationLegacyOnly Classification = "LEGACY_ONLY"
	ClassificationMigrated   Classification = "MIGRATED"
	ClassificationDual       Classification = "DUAL"
	ClassificationUnaffected Classification = "UNAFFECTED"
	ClassificationUncertain  Classification = "UNCERTAIN"
)

// Policy controls fail-closed readiness gates.
type Policy struct {
	FailOnCriticalLegacyConsumer bool
	FailOnCriticalUnknown        bool
	MinimumBlockingCriticality   domain.Criticality
	IncludeTransitive            bool
}

// Summary contains global consumer counts. Progress is informational and never
// determines Status.
type Summary struct {
	Status         Status `json:"status"`
	TotalConsumers int    `json:"totalConsumers"`
	LegacyOnly     int    `json:"legacyOnly"`
	Migrated       int    `json:"migrated"`
	Dual           int    `json:"dual"`
	Unaffected     int    `json:"unaffected"`
	Uncertain      int    `json:"uncertain"`
	Progress       int    `json:"progressPercent"`
}

// ConsumerResult records classification evidence and transitive paths.
type ConsumerResult struct {
	Consumer       domain.Consumer    `json:"consumer"`
	Classification Classification     `json:"classification"`
	References     []domain.Reference `json:"references,omitempty"`
	Paths          []graph.Path       `json:"paths,omitempty"`
}

// ChangeResult contains every classified consumer for one migration change.
type ChangeResult struct {
	Change    domain.Change    `json:"change"`
	Status    Status           `json:"status"`
	Consumers []ConsumerResult `json:"consumers"`
}

// Result is the versioned deterministic machine result.
type Result struct {
	SchemaVersion string              `json:"schemaVersion"`
	Migration     domain.Migration    `json:"migration"`
	Summary       Summary             `json:"summary"`
	Changes       []ChangeResult      `json:"changes"`
	Diagnostics   []domain.Diagnostic `json:"diagnostics,omitempty"`
}

// Evaluate classifies every discovered consumer and produces the authoritative
// readiness result.
func Evaluate(
	migration domain.Migration,
	discovery domain.Discovery,
	dependencyGraph *graph.Graph,
	policy Policy,
) (Result, error) {
	if dependencyGraph == nil {
		return Result{}, fmt.Errorf("dependency graph is required")
	}
	if policy.MinimumBlockingCriticality == "" {
		policy.MinimumBlockingCriticality = domain.CriticalityHigh
	}

	consumers := make([]domain.Consumer, len(discovery.Consumers))
	copy(consumers, discovery.Consumers)
	sort.Slice(consumers, func(i, j int) bool { return consumers[i].ID < consumers[j].ID })
	consumerByID := make(map[string]domain.Consumer, len(consumers))
	for _, consumer := range consumers {
		if consumer.ID == "" {
			return Result{}, fmt.Errorf("consumer ID is required")
		}
		if _, exists := consumerByID[consumer.ID]; exists {
			return Result{}, fmt.Errorf("duplicate consumer ID %q", consumer.ID)
		}
		consumerByID[consumer.ID] = consumer
	}

	global := make(map[string]Classification, len(consumers))
	changeResults := make([]ChangeResult, 0, len(migration.Changes))
	for _, change := range migration.Changes {
		oldImpact := impact.ImpactedConsumers(dependencyGraph, change.From, policy.IncludeTransitive)
		newImpact := map[string][]graph.Path{}
		if change.To != nil {
			newImpact = impact.ImpactedConsumers(dependencyGraph, *change.To, policy.IncludeTransitive)
		}

		consumerResults := make([]ConsumerResult, 0, len(consumers))
		for _, consumer := range consumers {
			oldPaths, hasOld := oldImpact[consumer.ID]
			newPaths, hasNew := newImpact[consumer.ID]
			uncertain := consumer.Unresolved || consumerHasUnresolvedReference(discovery.References, consumer.ID, change)
			classification := classify(change, hasOld, hasNew, uncertain)
			global[consumer.ID] = worseClassification(global[consumer.ID], classification)
			consumerResults = append(consumerResults, ConsumerResult{
				Consumer:       consumer,
				Classification: classification,
				References:     referencesForConsumerAndChange(discovery.References, consumer.ID, change),
				Paths:          append(oldPaths, newPaths...),
			})
		}

		changeResults = append(changeResults, ChangeResult{
			Change:    change,
			Status:    statusForConsumers(consumerResults, discovery.Diagnostics, policy),
			Consumers: consumerResults,
		})
	}

	summary := summarize(consumers, global)
	summary.Status = statusForChanges(changeResults, discovery.Diagnostics)
	return Result{
		SchemaVersion: ResultSchemaVersion,
		Migration:     migration,
		Summary:       summary,
		Changes:       changeResults,
		Diagnostics:   discovery.Diagnostics,
	}, nil
}

func classify(change domain.Change, hasOld, hasNew, uncertain bool) Classification {
	if change.To == nil {
		if hasOld {
			return ClassificationLegacyOnly
		}
		if uncertain {
			return ClassificationUncertain
		}
		return ClassificationUnaffected
	}
	if hasOld && hasNew {
		return ClassificationDual
	}
	if hasOld {
		return ClassificationLegacyOnly
	}
	if uncertain {
		return ClassificationUncertain
	}
	if hasNew {
		return ClassificationMigrated
	}
	return ClassificationUnaffected
}

func consumerHasUnresolvedReference(references []domain.Reference, consumerID string, change domain.Change) bool {
	for _, reference := range references {
		if reference.ConsumerID == consumerID && impact.UnresolvedReferenceApplies(reference, change) {
			return true
		}
	}
	return false
}

func referencesForConsumerAndChange(
	references []domain.Reference,
	consumerID string,
	change domain.Change,
) []domain.Reference {
	var result []domain.Reference
	for _, reference := range references {
		if reference.ConsumerID != consumerID {
			continue
		}
		if impact.SymbolMatches(reference.Symbol, change.From) ||
			(change.To != nil && impact.SymbolMatches(reference.Symbol, *change.To)) ||
			impact.UnresolvedReferenceApplies(reference, change) {
			result = append(result, reference)
		}
	}
	return result
}

func statusForConsumers(results []ConsumerResult, diagnostics []domain.Diagnostic, policy Policy) Status {
	for _, result := range results {
		if result.Classification == ClassificationLegacyOnly &&
			policy.FailOnCriticalLegacyConsumer &&
			criticalityRank(result.Consumer.Criticality) >= criticalityRank(policy.MinimumBlockingCriticality) {
			return StatusBlocked
		}
	}
	if hasRequiredDiagnostic(diagnostics) {
		return StatusIncomplete
	}
	for _, result := range results {
		if result.Classification == ClassificationUncertain &&
			policy.FailOnCriticalUnknown &&
			criticalityRank(result.Consumer.Criticality) >= criticalityRank(domain.CriticalityCritical) {
			return StatusIncomplete
		}
	}
	return StatusReady
}

func statusForChanges(changes []ChangeResult, diagnostics []domain.Diagnostic) Status {
	for _, change := range changes {
		if change.Status == StatusBlocked {
			return StatusBlocked
		}
	}
	if hasRequiredDiagnostic(diagnostics) {
		return StatusIncomplete
	}
	for _, change := range changes {
		if change.Status == StatusIncomplete {
			return StatusIncomplete
		}
	}
	return StatusReady
}

func hasRequiredDiagnostic(diagnostics []domain.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Required {
			return true
		}
	}
	return false
}

func summarize(consumers []domain.Consumer, classifications map[string]Classification) Summary {
	summary := Summary{TotalConsumers: len(consumers)}
	for _, consumer := range consumers {
		switch classifications[consumer.ID] {
		case ClassificationLegacyOnly:
			summary.LegacyOnly++
		case ClassificationMigrated:
			summary.Migrated++
		case ClassificationDual:
			summary.Dual++
		case ClassificationUncertain:
			summary.Uncertain++
		default:
			summary.Unaffected++
		}
	}
	denominator := summary.LegacyOnly + summary.Migrated + summary.Dual + summary.Uncertain
	if denominator == 0 {
		summary.Progress = 100
	} else {
		summary.Progress = ((summary.Migrated + summary.Dual) * 100) / denominator
	}
	return summary
}

func worseClassification(current, candidate Classification) Classification {
	rank := map[Classification]int{
		ClassificationUnaffected: 1,
		ClassificationMigrated:   2,
		ClassificationDual:       3,
		ClassificationUncertain:  4,
		ClassificationLegacyOnly: 5,
	}
	if rank[candidate] > rank[current] {
		return candidate
	}
	return current
}

func criticalityRank(value domain.Criticality) int {
	switch strings.ToLower(string(value)) {
	case string(domain.CriticalityCritical):
		return 4
	case string(domain.CriticalityHigh):
		return 3
	case string(domain.CriticalityMedium):
		return 2
	case string(domain.CriticalityLow):
		return 1
	default:
		return 2
	}
}
