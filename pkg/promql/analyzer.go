// Package promql extracts telemetry references from PromQL using the official
// Prometheus parser and AST.
package promql

import (
	"fmt"
	"sort"
	"strings"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
)

const metricNameLabel = "__name__"

// Unresolved describes an expression fragment that could not be converted to
// a confirmed static reference.
type Unresolved struct {
	Expression string `json:"expression"`
	Reason     string `json:"reason"`
}

// Analysis is the deterministic output of PromQL reference extraction.
type Analysis struct {
	References []domain.Reference `json:"references"`
	Unresolved []Unresolved       `json:"unresolved,omitempty"`
}

// Analyze parses expr and extracts metric, label, grouping, vector-matching,
// and label-function references. Grafana-style templates are returned as
// unresolved evidence rather than being misclassified as absent dependencies.
func Analyze(expr string) (Analysis, error) {
	if strings.TrimSpace(expr) == "" {
		return Analysis{}, fmt.Errorf("PromQL expression is empty")
	}
	if containsTemplate(expr) {
		return Analysis{
			Unresolved: []Unresolved{{
				Expression: expr,
				Reason:     "expression contains an unresolved template variable",
			}},
		}, nil
	}

	root, err := parser.NewParser(parser.Options{}).ParseExpr(expr)
	if err != nil {
		return Analysis{}, fmt.Errorf("parse PromQL: %w", err)
	}

	collector := referenceCollector{
		expression: expr,
		seen:       make(map[string]struct{}),
	}
	parser.Inspect(root, func(node parser.Node, _ []parser.Node) error {
		switch typed := node.(type) {
		case *parser.VectorSelector:
			collector.collectVectorSelector(typed)
		case *parser.AggregateExpr:
			collector.collectAggregation(typed)
		case *parser.BinaryExpr:
			collector.collectVectorMatching(typed)
		case *parser.Call:
			collector.collectLabelFunction(typed)
		}
		return nil
	})

	sort.Slice(collector.references, func(i, j int) bool {
		left := collector.references[i]
		right := collector.references[j]
		return referenceSortKey(left) < referenceSortKey(right)
	})

	return Analysis{References: collector.references}, nil
}

// Validate returns a parse error for invalid or unresolved templated PromQL.
func Validate(expr string) error {
	analysis, err := Analyze(expr)
	if err != nil {
		return err
	}
	if len(analysis.Unresolved) != 0 {
		return fmt.Errorf("PromQL expression is unresolved: %s", analysis.Unresolved[0].Reason)
	}
	return nil
}

type referenceCollector struct {
	expression string
	references []domain.Reference
	seen       map[string]struct{}
}

func (collector *referenceCollector) collectVectorSelector(selector *parser.VectorSelector) {
	metricName, pattern := selectorMetric(selector)
	if metricName != "" {
		collector.addMetric(metricName, domain.UsageSelector, "metric selector", "", false)
	} else if pattern != "" {
		collector.addMetric(pattern, domain.UsagePattern, "metric-name pattern selector", pattern, true)
	}

	for _, matcher := range selector.LabelMatchers {
		if matcher.Name == metricNameLabel {
			continue
		}
		collector.addLabel(
			matcher.Name,
			metricName,
			domain.UsageFilter,
			"label matcher",
			pattern,
			metricName == "",
		)
	}
}

func (collector *referenceCollector) collectAggregation(expression *parser.AggregateExpr) {
	metrics := metricNames(expression.Expr)
	usage := "aggregation grouping"
	if expression.Without {
		usage = "aggregation without grouping"
	}
	for _, label := range expression.Grouping {
		if label == metricNameLabel {
			continue
		}
		collector.addLabelForMetrics(label, metrics, domain.UsageGrouping, usage)
	}
}

func (collector *referenceCollector) collectVectorMatching(expression *parser.BinaryExpr) {
	if expression.VectorMatching == nil {
		return
	}
	metrics := append(metricNames(expression.LHS), metricNames(expression.RHS)...)
	metrics = uniqueStrings(metrics)
	for _, label := range expression.VectorMatching.MatchingLabels {
		if label == metricNameLabel {
			continue
		}
		explanation := "ignoring vector-match label"
		if expression.VectorMatching.On {
			explanation = "on vector-match label"
		}
		collector.addLabelForMetrics(label, metrics, domain.UsageVectorMatching, explanation)
	}
	for _, label := range expression.VectorMatching.Include {
		if label == metricNameLabel {
			continue
		}
		collector.addLabelForMetrics(label, metrics, domain.UsageVectorMatching, "group vector-match include label")
	}
}

func (collector *referenceCollector) collectLabelFunction(call *parser.Call) {
	if call.Func == nil || len(call.Args) == 0 {
		return
	}
	metrics := metricNames(call.Args[0])

	switch call.Func.Name {
	case "label_replace":
		if destination, ok := stringArgument(call, 1); ok {
			collector.addLabelForMetrics(destination, metrics, domain.UsageGeneratedName, "label_replace destination")
		}
		if source, ok := stringArgument(call, 3); ok {
			collector.addLabelForMetrics(source, metrics, domain.UsageFilter, "label_replace source")
		}
	case "label_join":
		if destination, ok := stringArgument(call, 1); ok {
			collector.addLabelForMetrics(destination, metrics, domain.UsageGeneratedName, "label_join destination")
		}
		for index := 3; index < len(call.Args); index++ {
			if source, ok := stringArgument(call, index); ok {
				collector.addLabelForMetrics(source, metrics, domain.UsageFilter, "label_join source")
			}
		}
	}
}

func (collector *referenceCollector) addLabelForMetrics(
	label string,
	metrics []string,
	usage domain.UsageType,
	explanation string,
) {
	if len(metrics) == 0 {
		collector.addLabel(label, "", usage, explanation, "", true)
		return
	}
	for _, metric := range metrics {
		collector.addLabel(label, metric, usage, explanation, "", false)
	}
}

func (collector *referenceCollector) addMetric(
	name string,
	usage domain.UsageType,
	explanation string,
	pattern string,
	requiresResolution bool,
) {
	confidence := domain.ConfidenceConfirmed
	if requiresResolution {
		confidence = domain.ConfidenceMedium
	}
	collector.add(domain.Reference{
		Symbol: domain.Symbol{
			Domain: domain.DomainPrometheus,
			Kind:   domain.SymbolKindMetric,
			Name:   name,
		},
		Usage: usage,
		Evidence: domain.Evidence{
			Method:      domain.EvidenceMethodPromQLAST,
			Confidence:  confidence,
			Expression:  collector.expression,
			Explanation: explanation,
		},
		Pattern:            pattern,
		RequiresResolution: requiresResolution,
	})
}

func (collector *referenceCollector) addLabel(
	name string,
	parent string,
	usage domain.UsageType,
	explanation string,
	pattern string,
	requiresResolution bool,
) {
	if name == "" {
		return
	}
	confidence := domain.ConfidenceConfirmed
	if requiresResolution {
		confidence = domain.ConfidenceMedium
	}
	collector.add(domain.Reference{
		Symbol: domain.Symbol{
			Domain: domain.DomainPrometheus,
			Kind:   domain.SymbolKindLabel,
			Name:   name,
			Parent: parent,
		},
		Usage: usage,
		Evidence: domain.Evidence{
			Method:      domain.EvidenceMethodPromQLAST,
			Confidence:  confidence,
			Expression:  collector.expression,
			Explanation: explanation,
		},
		Pattern:            pattern,
		RequiresResolution: requiresResolution,
	})
}

func (collector *referenceCollector) add(reference domain.Reference) {
	key := referenceSortKey(reference)
	if _, exists := collector.seen[key]; exists {
		return
	}
	collector.seen[key] = struct{}{}
	collector.references = append(collector.references, reference)
}

func selectorMetric(selector *parser.VectorSelector) (name, pattern string) {
	if selector.Name != "" {
		return selector.Name, ""
	}
	for _, matcher := range selector.LabelMatchers {
		if matcher.Name != metricNameLabel {
			continue
		}
		if matcher.Type == labels.MatchEqual {
			return matcher.Value, ""
		}
		return "", matcher.String()
	}
	return "", ""
}

func metricNames(node parser.Node) []string {
	var metrics []string
	parser.Inspect(node, func(current parser.Node, _ []parser.Node) error {
		selector, ok := current.(*parser.VectorSelector)
		if !ok {
			return nil
		}
		name, _ := selectorMetric(selector)
		if name != "" {
			metrics = append(metrics, name)
		}
		return nil
	})
	return uniqueStrings(metrics)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return unique
}

func stringArgument(call *parser.Call, index int) (string, bool) {
	if index >= len(call.Args) {
		return "", false
	}
	literal, ok := call.Args[index].(*parser.StringLiteral)
	if !ok || literal.Val == "" {
		return "", false
	}
	return literal.Val, true
}

func containsTemplate(expression string) bool {
	return strings.Contains(expression, "${") ||
		strings.Contains(expression, "[[") ||
		strings.Contains(expression, "$__")
}

func referenceSortKey(reference domain.Reference) string {
	return strings.Join([]string{
		string(reference.Symbol.Kind),
		reference.Symbol.Parent,
		reference.Symbol.Name,
		string(reference.Usage),
		reference.Pattern,
	}, "\x00")
}
