// Package safety applies deterministic policy to immutable impact findings.
package safety

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/graph"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/impact"
)

const ResultSchemaVersion = "tcg-result/v1alpha1"

// Status is the authoritative generic change-safety result.
type Status string

const (
	StatusPass       Status = "PASS"
	StatusWarn       Status = "WARN"
	StatusBlock      Status = "BLOCK"
	StatusIncomplete Status = "INCOMPLETE"
	StatusError      Status = "ERROR"
)

// RolloutMode controls enforcement without removing underlying findings.
type RolloutMode string

const (
	RolloutAudit   RolloutMode = "audit"
	RolloutWarn    RolloutMode = "warn"
	RolloutEnforce RolloutMode = "enforce"
)

// Action is the policy response to one complete finding.
type Action string

const (
	ActionWarn  Action = "warn"
	ActionBlock Action = "block"
)

// ImpactRule configures policy for one impact type.
type ImpactRule struct {
	MinimumCriticality domain.Criticality `json:"minimumCriticality"`
	Action             Action             `json:"action"`
}

// Policy is explicit input to deterministic safety evaluation.
type Policy struct {
	Mode    RolloutMode                `json:"mode"`
	Impacts map[impact.Type]ImpactRule `json:"impacts,omitempty"`
}

// Decision records policy handling separately from the immutable finding.
type Decision struct {
	ChangeID         string      `json:"changeId"`
	ConsumerID       string      `json:"consumerId"`
	Impact           impact.Type `json:"impact"`
	ConfiguredAction Action      `json:"configuredAction"`
	EffectiveAction  Action      `json:"effectiveAction"`
	Reason           string      `json:"reason"`
}

// Result is the versioned generic machine result.
type Result struct {
	SchemaVersion string              `json:"schemaVersion"`
	ChangeSet     domain.ChangeSet    `json:"changeSet"`
	Status        Status              `json:"status"`
	Findings      []impact.Finding    `json:"findings"`
	Decisions     []Decision          `json:"decisions,omitempty"`
	Diagnostics   []domain.Diagnostic `json:"diagnostics,omitempty"`
	Errors        []string            `json:"errors,omitempty"`
}

// DefaultPolicy returns the production-safe initial policy. Visibility and
// semantic findings warn; high-or-greater operational control findings block.
func DefaultPolicy() Policy {
	return Policy{
		Mode: RolloutEnforce,
		Impacts: map[impact.Type]ImpactRule{
			impact.TypeVisibilityLoss:     {MinimumCriticality: domain.CriticalityLow, Action: ActionWarn},
			impact.TypeAlertingRisk:       {MinimumCriticality: domain.CriticalityHigh, Action: ActionBlock},
			impact.TypeSLORisk:            {MinimumCriticality: domain.CriticalityHigh, Action: ActionBlock},
			impact.TypeScalingRisk:        {MinimumCriticality: domain.CriticalityHigh, Action: ActionBlock},
			impact.TypeDeploymentGateRisk: {MinimumCriticality: domain.CriticalityHigh, Action: ActionBlock},
			impact.TypeAutomationRisk:     {MinimumCriticality: domain.CriticalityHigh, Action: ActionBlock},
			impact.TypeSemanticRisk:       {MinimumCriticality: domain.CriticalityLow, Action: ActionWarn},
		},
	}
}

// Evaluate applies policy after findings are complete. Required uncertainty
// takes precedence over policy violations, and findings are never suppressed.
func Evaluate(
	changeSet domain.ChangeSet,
	findings []impact.Finding,
	diagnostics []domain.Diagnostic,
	policy Policy,
) (Result, error) {
	result := Result{
		SchemaVersion: ResultSchemaVersion,
		ChangeSet:     cloneChangeSet(changeSet),
		Findings:      cloneAndSortFindings(findings),
		Diagnostics:   cloneAndSortDiagnostics(diagnostics),
	}
	if err := validatePolicy(policy); err != nil {
		result.Status = StatusError
		result.Errors = []string{err.Error()}
		return result, err
	}
	if err := validateFindings(result.Findings); err != nil {
		result.Status = StatusError
		result.Errors = []string{err.Error()}
		return result, err
	}

	result.Decisions = make([]Decision, 0, len(result.Findings))
	for _, finding := range result.Findings {
		rule, exists := policy.Impacts[finding.Impact]
		if !exists {
			rule = ImpactRule{MinimumCriticality: domain.CriticalityLow, Action: ActionWarn}
		}
		effective := rule.Action
		reason := fmt.Sprintf("%s policy for %s", rule.Action, finding.Impact)
		if criticalityRank(finding.Criticality) < criticalityRank(rule.MinimumCriticality) {
			effective = ActionWarn
			reason = fmt.Sprintf(
				"criticality %s is below the %s enforcement threshold",
				finding.Criticality,
				rule.MinimumCriticality,
			)
		}
		if policy.Mode != RolloutEnforce && effective == ActionBlock {
			effective = ActionWarn
			reason = fmt.Sprintf("%s rollout mode does not enforce blocking actions", policy.Mode)
		}
		result.Decisions = append(result.Decisions, Decision{
			ChangeID:         finding.Change.ID,
			ConsumerID:       finding.Consumer.ID,
			Impact:           finding.Impact,
			ConfiguredAction: rule.Action,
			EffectiveAction:  effective,
			Reason:           reason,
		})
	}

	result.Status = aggregateStatus(result.Findings, result.Decisions, result.Diagnostics)
	return result, nil
}

// ErrorResult constructs a machine result for failures outside policy
// evaluation, such as adapter or graph-construction errors. Any facts proven
// before the failure remain present.
func ErrorResult(
	changeSet domain.ChangeSet,
	findings []impact.Finding,
	diagnostics []domain.Diagnostic,
	err error,
) Result {
	result := Result{
		SchemaVersion: ResultSchemaVersion,
		ChangeSet:     cloneChangeSet(changeSet),
		Status:        StatusError,
		Findings:      cloneAndSortFindings(findings),
		Diagnostics:   cloneAndSortDiagnostics(diagnostics),
	}
	if err != nil {
		result.Errors = []string{err.Error()}
	}
	return result
}

// ExitCode returns the stable generic CLI/process contract.
func ExitCode(status Status) int {
	switch status {
	case StatusPass, StatusWarn:
		return 0
	case StatusBlock:
		return 2
	case StatusIncomplete:
		return 3
	default:
		return 1
	}
}

func validatePolicy(policy Policy) error {
	switch policy.Mode {
	case RolloutAudit, RolloutWarn, RolloutEnforce:
	default:
		return fmt.Errorf("unsupported rollout mode %q", policy.Mode)
	}
	keys := make([]string, 0, len(policy.Impacts))
	for impactType := range policy.Impacts {
		keys = append(keys, string(impactType))
	}
	sort.Strings(keys)
	for _, key := range keys {
		impactType := impact.Type(key)
		if !validImpactType(impactType) {
			return fmt.Errorf("unsupported impact type %q", impactType)
		}
		rule := policy.Impacts[impactType]
		if rule.Action != ActionWarn && rule.Action != ActionBlock {
			return fmt.Errorf("impact %s has unsupported action %q", impactType, rule.Action)
		}
		if !validCriticality(rule.MinimumCriticality) {
			return fmt.Errorf(
				"impact %s has unsupported minimum criticality %q",
				impactType,
				rule.MinimumCriticality,
			)
		}
	}
	return nil
}

func validateFindings(findings []impact.Finding) error {
	seen := make(map[string]struct{}, len(findings))
	for _, finding := range findings {
		if finding.Change.ID == "" || finding.Consumer.ID == "" {
			return fmt.Errorf("finding change ID and consumer ID are required")
		}
		if !validImpactType(finding.Impact) {
			return fmt.Errorf("finding has unsupported impact type %q", finding.Impact)
		}
		expectedImpact, err := impact.TypeForConsumer(finding.Consumer.Kind)
		if err != nil {
			return err
		}
		if expectedImpact != finding.Impact {
			return fmt.Errorf(
				"finding consumer kind %q maps to %s, not %s",
				finding.Consumer.Kind,
				expectedImpact,
				finding.Impact,
			)
		}
		if finding.Consumer.Criticality != finding.Criticality {
			return fmt.Errorf("finding criticality does not match consumer criticality")
		}
		if !validCriticality(finding.Criticality) && !(finding.Criticality == "" && finding.Uncertain) {
			return fmt.Errorf("finding has unsupported criticality %q", finding.Criticality)
		}
		key := finding.Change.ID + "\x00" + finding.Consumer.ID + "\x00" + string(finding.Impact)
		if _, exists := seen[key]; exists {
			return fmt.Errorf(
				"duplicate finding for change %q, consumer %q, and impact %q",
				finding.Change.ID,
				finding.Consumer.ID,
				finding.Impact,
			)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func aggregateStatus(
	findings []impact.Finding,
	decisions []Decision,
	diagnostics []domain.Diagnostic,
) Status {
	for _, diagnostic := range diagnostics {
		if diagnostic.Required {
			return StatusIncomplete
		}
	}
	for _, finding := range findings {
		if finding.Uncertain {
			return StatusIncomplete
		}
	}
	for _, decision := range decisions {
		if decision.EffectiveAction == ActionBlock {
			return StatusBlock
		}
	}
	if len(findings) != 0 || len(diagnostics) != 0 {
		return StatusWarn
	}
	return StatusPass
}

func validImpactType(value impact.Type) bool {
	switch value {
	case impact.TypeVisibilityLoss,
		impact.TypeAlertingRisk,
		impact.TypeSLORisk,
		impact.TypeScalingRisk,
		impact.TypeDeploymentGateRisk,
		impact.TypeAutomationRisk,
		impact.TypeSemanticRisk:
		return true
	default:
		return false
	}
}

func validCriticality(value domain.Criticality) bool {
	switch value {
	case domain.CriticalityLow,
		domain.CriticalityMedium,
		domain.CriticalityHigh,
		domain.CriticalityCritical:
		return true
	default:
		return false
	}
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
		return 0
	}
}

func cloneAndSortFindings(source []impact.Finding) []impact.Finding {
	result := make([]impact.Finding, len(source))
	for index, finding := range source {
		result[index] = finding
		result[index].Change = cloneChange(finding.Change)
		result[index].References = append([]domain.Reference(nil), finding.References...)
		result[index].Paths = make([]impact.Path, len(finding.Paths))
		for pathIndex, path := range finding.Paths {
			result[index].Paths[pathIndex] = impact.Path{
				Nodes: append([]string(nil), path.Nodes...),
				Edges: append([]graph.EdgeKind(nil), path.Edges...),
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Change.ID != result[j].Change.ID {
			return result[i].Change.ID < result[j].Change.ID
		}
		if result[i].Consumer.ID != result[j].Consumer.ID {
			return result[i].Consumer.ID < result[j].Consumer.ID
		}
		return result[i].Impact < result[j].Impact
	})
	return result
}

func cloneAndSortDiagnostics(source []domain.Diagnostic) []domain.Diagnostic {
	result := append([]domain.Diagnostic(nil), source...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Adapter != result[j].Adapter {
			return result[i].Adapter < result[j].Adapter
		}
		if result[i].Source.File != result[j].Source.File {
			return result[i].Source.File < result[j].Source.File
		}
		if result[i].Source.Line != result[j].Source.Line {
			return result[i].Source.Line < result[j].Source.Line
		}
		if result[i].Source.Column != result[j].Source.Column {
			return result[i].Source.Column < result[j].Source.Column
		}
		if result[i].Source.URL != result[j].Source.URL {
			return result[i].Source.URL < result[j].Source.URL
		}
		if result[i].Source.Repo != result[j].Source.Repo {
			return result[i].Source.Repo < result[j].Source.Repo
		}
		if result[i].Message != result[j].Message {
			return result[i].Message < result[j].Message
		}
		return !result[i].Required && result[j].Required
	})
	return result
}

func cloneChangeSet(source domain.ChangeSet) domain.ChangeSet {
	result := source
	result.Changes = make([]domain.Change, len(source.Changes))
	for index, change := range source.Changes {
		result.Changes[index] = cloneChange(change)
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
