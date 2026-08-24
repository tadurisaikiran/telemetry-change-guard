// Package prometheusrules discovers consumers and references in Prometheus
// rule files and Prometheus Operator PrometheusRule resources.
package prometheusrules

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
	tmrpromql "github.com/tadurisaikiran/telemetry-change-guard/pkg/promql"
)

const maxRuleFileBytes = 8 << 20

// Loader controls how rule-source failures affect readiness.
type Loader struct {
	Required bool
}

// LoadFile loads one local rule file.
func (loader Loader) LoadFile(ctx context.Context, path string) (domain.Discovery, error) {
	if err := ctx.Err(); err != nil {
		return domain.Discovery{}, fmt.Errorf("load Prometheus rules %q: %w", path, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("open Prometheus rules %q: %w", path, err)
	}
	defer file.Close()

	discovery, err := loader.Parse(ctx, filepath.Clean(path), file)
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("load Prometheus rules %q: %w", path, err)
	}
	return discovery, nil
}

// Parse loads standard Prometheus rules or PrometheusRule CRDs from reader.
// Multiple YAML documents are supported because Kubernetes manifests commonly
// use that representation.
func (loader Loader) Parse(ctx context.Context, source string, reader io.Reader) (domain.Discovery, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maxRuleFileBytes+1))
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("read rules: %w", err)
	}
	if len(contents) > maxRuleFileBytes {
		return domain.Discovery{}, fmt.Errorf("rule file exceeds the %d-byte size limit", maxRuleFileBytes)
	}

	decoder := yaml.NewDecoder(strings.NewReader(string(contents)))
	var combined domain.Discovery
	documentIndex := 0
	for {
		if err := ctx.Err(); err != nil {
			return domain.Discovery{}, err
		}

		var document ruleDocument
		if err := decoder.Decode(&document); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return domain.Discovery{}, fmt.Errorf("decode YAML document %d: %w", documentIndex+1, err)
		}
		documentIndex++
		if document.empty() {
			continue
		}

		groups, err := document.ruleGroups()
		if err != nil {
			return domain.Discovery{}, fmt.Errorf("document %d: %w", documentIndex, err)
		}
		discovery, err := loader.discoverGroups(source, documentIndex, groups)
		if err != nil {
			return domain.Discovery{}, err
		}
		appendDiscovery(&combined, discovery)
	}

	if documentIndex == 0 {
		return domain.Discovery{}, fmt.Errorf("rule file is empty")
	}
	return combined, nil
}

type ruleDocument struct {
	APIVersion string      `yaml:"apiVersion"`
	Kind       string      `yaml:"kind"`
	Groups     []ruleGroup `yaml:"groups"`
	Spec       ruleSpec    `yaml:"spec"`
}

type ruleSpec struct {
	Groups []ruleGroup `yaml:"groups"`
}

type ruleGroup struct {
	Name  string     `yaml:"name"`
	Rules []ruleYAML `yaml:"rules"`
}

type ruleYAML struct {
	Record        string            `yaml:"record"`
	Alert         string            `yaml:"alert"`
	Expr          string            `yaml:"expr"`
	For           string            `yaml:"for"`
	KeepFiringFor string            `yaml:"keep_firing_for"`
	Labels        map[string]string `yaml:"labels"`
	Annotations   map[string]string `yaml:"annotations"`
	line          int
	column        int
}

func (rule *ruleYAML) UnmarshalYAML(node *yaml.Node) error {
	type plainRule ruleYAML
	var decoded plainRule
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*rule = ruleYAML(decoded)
	rule.line = node.Line
	rule.column = node.Column
	return nil
}

func (document ruleDocument) empty() bool {
	return document.APIVersion == "" && document.Kind == "" && len(document.Groups) == 0 && len(document.Spec.Groups) == 0
}

func (document ruleDocument) ruleGroups() ([]ruleGroup, error) {
	if document.Kind == "PrometheusRule" {
		if len(document.Spec.Groups) == 0 {
			return nil, fmt.Errorf("PrometheusRule spec.groups must contain at least one group")
		}
		return document.Spec.Groups, nil
	}
	if len(document.Groups) == 0 {
		return nil, fmt.Errorf("expected standard groups or kind PrometheusRule")
	}
	return document.Groups, nil
}

func (loader Loader) discoverGroups(source string, documentIndex int, groups []ruleGroup) (domain.Discovery, error) {
	var discovery domain.Discovery
	for groupIndex, group := range groups {
		if strings.TrimSpace(group.Name) == "" {
			return domain.Discovery{}, fmt.Errorf("document %d group %d: name is required", documentIndex, groupIndex)
		}
		for ruleIndex, rule := range group.Rules {
			consumer, production, err := normalizeRule(source, documentIndex, group.Name, ruleIndex, rule)
			if err != nil {
				return domain.Discovery{}, err
			}

			analysis, analysisErr := tmrpromql.Analyze(rule.Expr)
			if analysisErr != nil || len(analysis.Unresolved) != 0 {
				consumer.Unresolved = true
				diagnosticMessage := "PromQL expression is unresolved"
				if analysisErr != nil {
					diagnosticMessage = analysisErr.Error()
				} else if len(analysis.Unresolved) != 0 {
					diagnosticMessage = analysis.Unresolved[0].Reason
				}
				discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
					Adapter:  "prometheus_rules",
					Source:   consumer.Source,
					Message:  diagnosticMessage,
					Required: loader.Required,
				})
			} else {
				for _, reference := range analysis.References {
					reference.ConsumerID = consumer.ID
					reference.Evidence.Source = consumer.Source
					discovery.References = append(discovery.References, reference)
				}
			}

			discovery.Consumers = append(discovery.Consumers, consumer)
			if production != nil {
				discovery.Productions = append(discovery.Productions, *production)
			}
		}
	}
	return discovery, nil
}

func normalizeRule(
	source string,
	documentIndex int,
	groupName string,
	ruleIndex int,
	rule ruleYAML,
) (domain.Consumer, *domain.Production, error) {
	if (rule.Record == "") == (rule.Alert == "") {
		return domain.Consumer{}, nil, fmt.Errorf(
			"document %d group %q rule %d: exactly one of record or alert is required",
			documentIndex,
			groupName,
			ruleIndex,
		)
	}
	if strings.TrimSpace(rule.Expr) == "" {
		return domain.Consumer{}, nil, fmt.Errorf(
			"document %d group %q rule %d: expr is required",
			documentIndex,
			groupName,
			ruleIndex,
		)
	}

	location := domain.SourceLocation{File: source, Line: rule.line, Column: rule.column}
	if rule.Record != "" {
		consumer := domain.Consumer{
			ID:          ruleID(source, documentIndex, groupName, "record", rule.Record, ruleIndex),
			Kind:        domain.ConsumerKindRecordingRule,
			Name:        rule.Record,
			Source:      location,
			Criticality: domain.CriticalityHigh,
			Expression:  rule.Expr,
			Metadata: map[string]string{
				"group": groupName,
			},
		}
		production := domain.Production{
			ConsumerID: consumer.ID,
			Symbol: domain.Symbol{
				Domain: domain.DomainPrometheus,
				Kind:   domain.SymbolKindMetric,
				Name:   rule.Record,
			},
		}
		return consumer, &production, nil
	}

	return domain.Consumer{
		ID:          ruleID(source, documentIndex, groupName, "alert", rule.Alert, ruleIndex),
		Kind:        domain.ConsumerKindAlertRule,
		Name:        rule.Alert,
		Source:      location,
		Criticality: alertCriticality(rule.Labels),
		Expression:  rule.Expr,
		Metadata: map[string]string{
			"group": groupName,
			"for":   rule.For,
		},
	}, nil, nil
}

func alertCriticality(labels map[string]string) domain.Criticality {
	severity := strings.ToLower(labels["severity"])
	if severity == "page" || severity == "paging" || severity == "critical" {
		return domain.CriticalityCritical
	}
	return domain.CriticalityHigh
}

func ruleID(source string, documentIndex int, group, kind, name string, ruleIndex int) string {
	return fmt.Sprintf("prometheus:%s:%d:%s:%s:%s:%d", source, documentIndex, group, kind, name, ruleIndex)
}

func appendDiscovery(target *domain.Discovery, additional domain.Discovery) {
	target.Consumers = append(target.Consumers, additional.Consumers...)
	target.References = append(target.References, additional.References...)
	target.Productions = append(target.Productions, additional.Productions...)
	target.Diagnostics = append(target.Diagnostics, additional.Diagnostics...)
}
