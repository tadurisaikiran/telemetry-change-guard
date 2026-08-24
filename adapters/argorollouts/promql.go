package argorollouts

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/prometheus/prometheus/promql/parser"

	tcgpromql "github.com/tadurisaikiran/telemetry-change-guard/pkg/promql"
)

const (
	argumentMarkerPrefix = "tcg_argo_argument_value_98f1a6c4_"
	argumentMarkerSuffix = "_end"
	maxArgumentTemplates = 4096
)

var argumentPattern = regexp.MustCompile(`\{\{\s*args\.[A-Za-z_][A-Za-z0-9_.-]*\s*\}\}`)

// analyzePromQL preserves exact dependencies when Argo arguments occur only
// in label matcher values. Other dynamic syntax remains unresolved because it
// could alter metric or label identity.
func analyzePromQL(query string) (tcgpromql.Analysis, error) {
	if !strings.Contains(query, "{{") && !strings.Contains(query, "}}") {
		return tcgpromql.Analyze(query)
	}
	if strings.Contains(query, argumentMarkerPrefix) {
		return unresolvedAnalysis(query, "expression collides with the internal Argo argument marker"), nil
	}

	matches := argumentPattern.FindAllStringIndex(query, -1)
	if len(matches) == 0 {
		return unresolvedAnalysis(query, "expression contains an unsupported Argo template"), nil
	}
	if len(matches) > maxArgumentTemplates {
		return unresolvedAnalysis(query, "expression exceeds the Argo argument template limit"), nil
	}
	var masked strings.Builder
	masked.Grow(len(query) + len(matches)*len(argumentMarkerPrefix))
	previous := 0
	for index, match := range matches {
		marker := argumentMarkerPrefix + strconv.Itoa(index) + argumentMarkerSuffix
		masked.WriteString(query[previous:match[0]])
		masked.WriteString(marker)
		previous = match[1]
	}
	masked.WriteString(query[previous:])
	maskedQuery := masked.String()
	if strings.Contains(maskedQuery, "{{") || strings.Contains(maskedQuery, "}}") {
		return unresolvedAnalysis(query, "expression contains an unsupported Argo template"), nil
	}

	root, err := parser.NewParser(parser.Options{}).ParseExpr(maskedQuery)
	if err != nil {
		return tcgpromql.Analysis{}, fmt.Errorf("parse PromQL: %w", err)
	}
	matchedMarkers := make([]bool, len(matches))
	parser.Inspect(root, func(node parser.Node, _ []parser.Node) error {
		selector, ok := node.(*parser.VectorSelector)
		if !ok {
			return nil
		}
		for _, matcher := range selector.LabelMatchers {
			if matcher.Name == "__name__" {
				continue
			}
			markMatcherArguments(matcher.Value, matchedMarkers)
		}
		return nil
	})
	for _, matched := range matchedMarkers {
		if !matched {
			return unresolvedAnalysis(
				query,
				"Argo arguments are supported only inside PromQL label matcher values",
			), nil
		}
	}

	analysis, err := tcgpromql.Analyze(maskedQuery)
	if err != nil {
		return tcgpromql.Analysis{}, err
	}
	for index := range analysis.References {
		analysis.References[index].Evidence.Expression = query
	}
	return analysis, nil
}

func markMatcherArguments(value string, matched []bool) {
	for {
		start := strings.Index(value, argumentMarkerPrefix)
		if start < 0 {
			return
		}
		value = value[start+len(argumentMarkerPrefix):]
		end := strings.Index(value, argumentMarkerSuffix)
		if end < 0 {
			return
		}
		index, err := strconv.Atoi(value[:end])
		if err == nil && index >= 0 && index < len(matched) {
			matched[index] = true
		}
		value = value[end+len(argumentMarkerSuffix):]
	}
}

func unresolvedAnalysis(query, reason string) tcgpromql.Analysis {
	return tcgpromql.Analysis{Unresolved: []tcgpromql.Unresolved{{Expression: query, Reason: reason}}}
}
