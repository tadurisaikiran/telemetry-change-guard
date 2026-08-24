package impact

import (
	"fmt"
	"sort"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/graph"
)

// Type describes the deterministic operational consequence of a dependency.
type Type string

const (
	TypeVisibilityLoss     Type = "VISIBILITY_LOSS"
	TypeAlertingRisk       Type = "ALERTING_RISK"
	TypeSLORisk            Type = "SLO_RISK"
	TypeScalingRisk        Type = "SCALING_RISK"
	TypeDeploymentGateRisk Type = "DEPLOYMENT_GATE_RISK"
	TypeAutomationRisk     Type = "AUTOMATION_RISK"
	TypeSemanticRisk       Type = "SEMANTIC_RISK"
)

// Consumer identifies the affected downstream system without retaining
// mutable adapter metadata in an authoritative finding.
type Consumer struct {
	ID          string                `json:"id"`
	Kind        domain.ConsumerKind   `json:"kind"`
	Name        string                `json:"name"`
	Criticality domain.Criticality    `json:"criticality"`
	Source      domain.SourceLocation `json:"source"`
}

// Path is a stable, machine-facing dependency path.
type Path struct {
	Nodes []string         `json:"nodes"`
	Edges []graph.EdgeKind `json:"edges"`
}

// Finding is an immutable fact produced before policy evaluation.
type Finding struct {
	Change      domain.Change      `json:"change"`
	Consumer    Consumer           `json:"consumer"`
	Impact      Type               `json:"impact"`
	Criticality domain.Criticality `json:"criticality"`
	Uncertain   bool               `json:"uncertain,omitempty"`
	References  []domain.Reference `json:"references,omitempty"`
	Paths       []Path             `json:"paths,omitempty"`
}

// Analyze derives deterministic findings from a validated ChangeSet and
// dependency graph. It never applies policy or chooses an aggregate status.
func Analyze(
	changeSet domain.ChangeSet,
	discovery domain.Discovery,
	dependencyGraph *graph.Graph,
	includeTransitive bool,
) ([]Finding, error) {
	if dependencyGraph == nil {
		return nil, fmt.Errorf("dependency graph is required")
	}

	consumers := make([]domain.Consumer, len(discovery.Consumers))
	copy(consumers, discovery.Consumers)
	sort.Slice(consumers, func(i, j int) bool { return consumers[i].ID < consumers[j].ID })
	seenConsumers := make(map[string]struct{}, len(consumers))
	for _, consumer := range consumers {
		if consumer.ID == "" {
			return nil, fmt.Errorf("consumer ID is required")
		}
		if _, exists := seenConsumers[consumer.ID]; exists {
			return nil, fmt.Errorf("duplicate consumer ID %q", consumer.ID)
		}
		seenConsumers[consumer.ID] = struct{}{}
	}

	changes := make([]domain.Change, len(changeSet.Changes))
	for index, change := range changeSet.Changes {
		changes[index] = cloneChange(change)
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].ID < changes[j].ID })

	var findings []Finding
	for _, change := range changes {
		impacted := ImpactedConsumers(dependencyGraph, change.From, includeTransitive)
		for _, consumer := range consumers {
			paths := impacted[consumer.ID]
			references := sourceReferences(discovery.References, consumer.ID, change)
			uncertain := consumer.Unresolved || consumer.Criticality == "" || hasUnresolved(references)
			if len(paths) == 0 && !uncertain {
				continue
			}
			impactType, err := TypeForConsumer(consumer.Kind)
			if err != nil {
				return findings, err
			}
			findings = append(findings, Finding{
				Change: cloneChange(change),
				Consumer: Consumer{
					ID:          consumer.ID,
					Kind:        consumer.Kind,
					Name:        consumer.Name,
					Criticality: consumer.Criticality,
					Source:      consumer.Source,
				},
				Impact:      impactType,
				Criticality: consumer.Criticality,
				Uncertain:   uncertain,
				References:  append([]domain.Reference(nil), references...),
				Paths:       publicPaths(paths),
			})
		}
	}
	return findings, nil
}

// TypeForConsumer maps consumer identity to operational consequence without
// consulting policy or AI.
func TypeForConsumer(kind domain.ConsumerKind) (Type, error) {
	switch kind {
	case domain.ConsumerKindDashboard,
		domain.ConsumerKindDashboardPanel,
		domain.ConsumerKindQuery,
		domain.ConsumerKindRunbook:
		return TypeVisibilityLoss, nil
	case domain.ConsumerKindAlertRule:
		return TypeAlertingRisk, nil
	case domain.ConsumerKindSLO:
		return TypeSLORisk, nil
	case domain.ConsumerKindAutoscaler:
		return TypeScalingRisk, nil
	case domain.ConsumerKindDeploymentGate:
		return TypeDeploymentGateRisk, nil
	case domain.ConsumerKindAutomation:
		return TypeAutomationRisk, nil
	case domain.ConsumerKindRecordingRule,
		domain.ConsumerKindCollector,
		domain.ConsumerKindSourceCode:
		return TypeSemanticRisk, nil
	default:
		return "", fmt.Errorf("consumer kind %q has no deterministic impact mapping", kind)
	}
}

// ImpactedConsumers returns deterministic dependency paths keyed by consumer.
func ImpactedConsumers(
	target *graph.Graph,
	symbol domain.Symbol,
	transitive bool,
) map[string][]graph.Path {
	result := make(map[string][]graph.Path)
	if target == nil {
		return result
	}
	for _, node := range target.Nodes() {
		if node.Kind != graph.NodeKindSymbol || node.Symbol == nil || !SymbolMatches(*node.Symbol, symbol) {
			continue
		}
		for _, path := range target.ImpactPaths(node.ID) {
			if !transitive && len(path.Edges) > 1 {
				continue
			}
			end, exists := target.Node(path.Nodes[len(path.Nodes)-1])
			if !exists || end.Consumer == nil {
				continue
			}
			result[end.Consumer.ID] = append(result[end.Consumer.ID], path)
		}
	}
	return result
}

// SymbolMatches applies domain-specific identity rules. It never matches
// across domains.
func SymbolMatches(reference, changed domain.Symbol) bool {
	if reference.Domain != changed.Domain || reference.Kind != changed.Kind {
		return false
	}
	if reference.Domain == domain.DomainPrometheus && reference.Kind == domain.SymbolKindMetric {
		return MetricFamilyMatches(reference.Name, changed.Name)
	}
	if reference.Domain == domain.DomainPrometheus && reference.Kind == domain.SymbolKindLabel {
		return reference.Name == changed.Name && MetricFamilyMatches(reference.Parent, changed.Parent)
	}
	return reference.Name == changed.Name && reference.Parent == changed.Parent
}

// MetricFamilyMatches applies Prometheus's generated family suffixes only.
func MetricFamilyMatches(reference, base string) bool {
	if reference == base {
		return true
	}
	for _, suffix := range []string{"_bucket", "_sum", "_count", "_created"} {
		if reference == base+suffix {
			return true
		}
	}
	return false
}

// UnresolvedReferenceApplies determines whether unresolved evidence is
// relevant to a specific change.
func UnresolvedReferenceApplies(reference domain.Reference, change domain.Change) bool {
	if !reference.RequiresResolution {
		return false
	}
	if reference.ResolutionScope != domain.ResolutionScopeLabels {
		return true
	}
	if change.From.Kind != domain.SymbolKindLabel || reference.Symbol.Kind != domain.SymbolKindMetric {
		return false
	}
	if MetricFamilyMatches(reference.Symbol.Name, change.From.Parent) {
		return true
	}
	return change.To != nil && MetricFamilyMatches(reference.Symbol.Name, change.To.Parent)
}

func sourceReferences(references []domain.Reference, consumerID string, change domain.Change) []domain.Reference {
	var result []domain.Reference
	for _, reference := range references {
		if reference.ConsumerID == consumerID &&
			(SymbolMatches(reference.Symbol, change.From) || UnresolvedReferenceApplies(reference, change)) {
			result = append(result, reference)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		if left.Symbol.Domain != right.Symbol.Domain {
			return left.Symbol.Domain < right.Symbol.Domain
		}
		if left.Symbol.Kind != right.Symbol.Kind {
			return left.Symbol.Kind < right.Symbol.Kind
		}
		if left.Symbol.Parent != right.Symbol.Parent {
			return left.Symbol.Parent < right.Symbol.Parent
		}
		if left.Symbol.Name != right.Symbol.Name {
			return left.Symbol.Name < right.Symbol.Name
		}
		if left.Evidence.Source.File != right.Evidence.Source.File {
			return left.Evidence.Source.File < right.Evidence.Source.File
		}
		if left.Evidence.Source.Line != right.Evidence.Source.Line {
			return left.Evidence.Source.Line < right.Evidence.Source.Line
		}
		if left.Evidence.Source.Column != right.Evidence.Source.Column {
			return left.Evidence.Source.Column < right.Evidence.Source.Column
		}
		if left.Evidence.Source.URL != right.Evidence.Source.URL {
			return left.Evidence.Source.URL < right.Evidence.Source.URL
		}
		if left.Evidence.Source.Repo != right.Evidence.Source.Repo {
			return left.Evidence.Source.Repo < right.Evidence.Source.Repo
		}
		if left.Evidence.Method != right.Evidence.Method {
			return left.Evidence.Method < right.Evidence.Method
		}
		if left.Evidence.Confidence != right.Evidence.Confidence {
			return left.Evidence.Confidence < right.Evidence.Confidence
		}
		if left.Evidence.Expression != right.Evidence.Expression {
			return left.Evidence.Expression < right.Evidence.Expression
		}
		if left.Evidence.Explanation != right.Evidence.Explanation {
			return left.Evidence.Explanation < right.Evidence.Explanation
		}
		if left.Usage != right.Usage {
			return left.Usage < right.Usage
		}
		if left.Pattern != right.Pattern {
			return left.Pattern < right.Pattern
		}
		if left.RequiresResolution != right.RequiresResolution {
			return !left.RequiresResolution
		}
		return left.ResolutionScope < right.ResolutionScope
	})
	return result
}

func hasUnresolved(references []domain.Reference) bool {
	for _, reference := range references {
		if reference.RequiresResolution {
			return true
		}
	}
	return false
}

func publicPaths(paths []graph.Path) []Path {
	result := make([]Path, 0, len(paths))
	for _, path := range paths {
		result = append(result, Path{
			Nodes: append([]string(nil), path.Nodes...),
			Edges: append([]graph.EdgeKind(nil), path.Edges...),
		})
	}
	return result
}

func cloneChange(change domain.Change) domain.Change {
	result := change
	if change.To != nil {
		destination := *change.To
		result.To = &destination
	}
	if change.Metadata != nil {
		result.Metadata = make(map[string]string, len(change.Metadata))
		for key, value := range change.Metadata {
			result.Metadata[key] = value
		}
	}
	return result
}
