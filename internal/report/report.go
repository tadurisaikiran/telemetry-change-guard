// Package report renders deterministic generic safety and migration readiness
// results for humans and machines.
package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/domain"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/graph"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/ownership"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/readiness"
)

const productName = "Telemetry Change Guard"

// GraphSchemaVersion identifies the stable dependency-graph machine contract.
const GraphSchemaVersion = "tcg-graph/v1alpha1"

// JSON renders the versioned machine result.
func JSON(result readiness.Result) ([]byte, error) {
	contents, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode JSON report: %w", err)
	}
	return append(contents, '\n'), nil
}

// Console renders a compact terminal report.
func Console(writer io.Writer, result readiness.Result) error {
	var output bytes.Buffer
	fmt.Fprintln(&output, productName)
	fmt.Fprintln(&output, strings.Repeat("=", len(productName)))
	fmt.Fprintf(&output, "Migration: %s\n", terminalValue(result.Migration.Metadata.Name))
	for _, change := range result.Migration.Changes {
		if change.To == nil {
			fmt.Fprintf(&output, "  %s: remove %s\n", terminalValue(change.ID), terminalValue(change.From.Name))
		} else {
			fmt.Fprintf(&output, "  %s: %s -> %s\n", terminalValue(change.ID), terminalValue(change.From.Name), terminalValue(change.To.Name))
		}
	}
	fmt.Fprintln(&output)
	fmt.Fprintf(&output, "Consumers scanned: %d\n", result.Summary.TotalConsumers)
	if runtimeCount := runtimeConsumerCount(result); runtimeCount != 0 {
		fmt.Fprintf(&output, "Runtime-observed:  %d (additive evidence)\n", runtimeCount)
	}
	fmt.Fprintf(&output, "Migrated:          %d\n", result.Summary.Migrated)
	fmt.Fprintf(&output, "Legacy only:       %d\n", result.Summary.LegacyOnly)
	fmt.Fprintf(&output, "Dual-compatible:   %d\n", result.Summary.Dual)
	fmt.Fprintf(&output, "Uncertain:         %d\n", result.Summary.Uncertain)
	fmt.Fprintf(&output, "Unaffected:        %d\n", result.Summary.Unaffected)
	fmt.Fprintf(&output, "Progress:          %d%% (informational only)\n", result.Summary.Progress)

	blockers := findings(result, readiness.ClassificationLegacyOnly)
	if len(blockers) != 0 {
		fmt.Fprintln(&output, "\nBLOCKERS")
		for _, finding := range blockers {
			fmt.Fprintf(&output, "  - %s [%s] (%s)\n", terminalValue(finding.name), terminalValue(finding.changeID), terminalValue(formatLocation(finding.file, finding.line)))
			if finding.owner != "" {
				fmt.Fprintf(&output, "    Owner: %s\n", terminalValue(finding.owner))
			}
			if finding.runtime != "" {
				fmt.Fprintf(&output, "    Runtime: %s\n", terminalValue(finding.runtime))
			}
			if finding.path != "" {
				fmt.Fprintf(&output, "    Path: %s\n", terminalValue(finding.path))
			}
		}
	}

	uncertain := findings(result, readiness.ClassificationUncertain)
	if len(uncertain) != 0 {
		fmt.Fprintln(&output, "\nUNCERTAIN")
		for _, finding := range uncertain {
			fmt.Fprintf(&output, "  - %s [%s] (%s)\n", terminalValue(finding.name), terminalValue(finding.changeID), terminalValue(formatLocation(finding.file, finding.line)))
			if finding.owner != "" {
				fmt.Fprintf(&output, "    Owner: %s\n", terminalValue(finding.owner))
			}
			if finding.runtime != "" {
				fmt.Fprintf(&output, "    Runtime: %s\n", terminalValue(finding.runtime))
			}
		}
	}

	if len(result.Diagnostics) != 0 {
		fmt.Fprintln(&output, "\nDIAGNOSTICS")
		for _, diagnostic := range result.Diagnostics {
			required := "optional"
			if diagnostic.Required {
				required = "required"
			}
			fmt.Fprintf(&output, "  - [%s/%s] %s: %s\n", terminalValue(diagnostic.Adapter), required, terminalValue(formatLocation(diagnostic.Source.File, diagnostic.Source.Line)), terminalValue(diagnostic.Message))
		}
	}

	fmt.Fprintf(&output, "\nSTATUS: %s\n", result.Summary.Status)
	_, err := writer.Write(output.Bytes())
	return err
}

// Markdown renders a report suitable for pull-request comments and artifacts.
func Markdown(writer io.Writer, result readiness.Result) error {
	var output bytes.Buffer
	fmt.Fprintf(&output, "# %s\n", productName)
	fmt.Fprintf(&output, "\n**Migration:** %s  \n", markdownCode(result.Migration.Metadata.Name))
	fmt.Fprintf(&output, "**Status:** **%s**  \n", result.Summary.Status)
	fmt.Fprintf(&output, "**Progress:** %d%% _(informational only)_\n", result.Summary.Progress)
	if runtimeCount := runtimeConsumerCount(result); runtimeCount != 0 {
		fmt.Fprintf(&output, "**Runtime-observed consumers:** %d _(additive evidence)_\n", runtimeCount)
	}
	fmt.Fprintln(&output, "\n| Classification | Consumers |")
	fmt.Fprintln(&output, "| --- | ---: |")
	fmt.Fprintf(&output, "| Migrated | %d |\n", result.Summary.Migrated)
	fmt.Fprintf(&output, "| Legacy only | %d |\n", result.Summary.LegacyOnly)
	fmt.Fprintf(&output, "| Dual-compatible | %d |\n", result.Summary.Dual)
	fmt.Fprintf(&output, "| Uncertain | %d |\n", result.Summary.Uncertain)
	fmt.Fprintf(&output, "| Unaffected | %d |\n", result.Summary.Unaffected)

	for _, section := range []struct {
		title          string
		classification readiness.Classification
	}{
		{title: "Blockers", classification: readiness.ClassificationLegacyOnly},
		{title: "Uncertainties", classification: readiness.ClassificationUncertain},
	} {
		items := findings(result, section.classification)
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(&output, "\n## %s\n", section.title)
		for _, item := range items {
			fmt.Fprintf(&output, "\n- %s (%s) — %s\n", markdownCode(item.name), markdownCode(item.changeID), markdownCode(formatLocation(item.file, item.line)))
			if item.owner != "" {
				fmt.Fprintf(&output, "  - Owner: %s\n", markdownCode(item.owner))
			}
			if item.runtime != "" {
				fmt.Fprintf(&output, "  - Runtime: %s\n", markdownCode(item.runtime))
			}
			if item.path != "" {
				fmt.Fprintf(&output, "  - Dependency path: %s\n", markdownCode(item.path))
			}
		}
	}

	if len(result.Diagnostics) != 0 {
		fmt.Fprintln(&output, "\n## Diagnostics")
		for _, diagnostic := range result.Diagnostics {
			fmt.Fprintf(&output, "\n- %s: %s\n", markdownCode(diagnostic.Adapter), markdownCode(diagnostic.Message))
		}
	}

	_, err := writer.Write(output.Bytes())
	return err
}

// GraphJSON renders the dependency graph as a stable JSON document.
func GraphJSON(target *graph.Graph) ([]byte, error) {
	if target == nil {
		return nil, fmt.Errorf("encode graph JSON: graph is required")
	}
	type graphSymbol struct {
		Domain string `json:"domain"`
		Kind   string `json:"kind"`
		Name   string `json:"name"`
		Parent string `json:"parent,omitempty"`
	}
	type graphSource struct {
		File   string `json:"file,omitempty"`
		Line   int    `json:"line,omitempty"`
		Column int    `json:"column,omitempty"`
		URL    string `json:"url,omitempty"`
		Repo   string `json:"repo,omitempty"`
	}
	type graphOwner struct {
		Name  string `json:"name"`
		Email string `json:"email,omitempty"`
	}
	type graphRuntime struct {
		Format           string   `json:"format"`
		ExecutionCount   int      `json:"executionCount"`
		FirstSeen        string   `json:"firstSeen"`
		LastSeen         string   `json:"lastSeen"`
		Window           string   `json:"window"`
		WindowStart      string   `json:"windowStart,omitempty"`
		WindowAnchor     string   `json:"windowAnchor"`
		ExecutionsPerDay string   `json:"executionsPerDay,omitempty"`
		Origins          []string `json:"origins"`
		OriginDetails    []string `json:"originDetails,omitempty"`
	}
	type graphConsumer struct {
		ID          string            `json:"id"`
		Kind        string            `json:"kind"`
		Name        string            `json:"name"`
		Source      graphSource       `json:"source"`
		Criticality string            `json:"criticality"`
		Owner       *graphOwner       `json:"owner,omitempty"`
		Runtime     *graphRuntime     `json:"runtime,omitempty"`
		Expression  string            `json:"expression,omitempty"`
		Metadata    map[string]string `json:"metadata,omitempty"`
		Unresolved  bool              `json:"unresolved,omitempty"`
	}
	type graphNode struct {
		ID       string         `json:"id"`
		Kind     string         `json:"kind"`
		Name     string         `json:"name"`
		Symbol   *graphSymbol   `json:"symbol,omitempty"`
		Consumer *graphConsumer `json:"consumer,omitempty"`
	}
	type graphEdge struct {
		From string `json:"from"`
		To   string `json:"to"`
		Kind string `json:"kind"`
	}
	document := struct {
		SchemaVersion string      `json:"schemaVersion"`
		Nodes         []graphNode `json:"nodes"`
		Edges         []graphEdge `json:"edges"`
	}{
		SchemaVersion: GraphSchemaVersion,
		Nodes:         make([]graphNode, 0, len(target.Nodes())),
		Edges:         make([]graphEdge, 0, len(target.Edges())),
	}
	for _, node := range target.Nodes() {
		encoded := graphNode{ID: node.ID, Kind: string(node.Kind), Name: node.Name}
		if node.Symbol != nil {
			encoded.Symbol = &graphSymbol{
				Domain: string(node.Symbol.Domain), Kind: string(node.Symbol.Kind),
				Name: node.Symbol.Name, Parent: node.Symbol.Parent,
			}
		}
		if node.Consumer != nil {
			consumer := node.Consumer
			encoded.Consumer = &graphConsumer{
				ID: consumer.ID, Kind: string(consumer.Kind), Name: consumer.Name,
				Source: graphSource{
					File: consumer.Source.File, Line: consumer.Source.Line, Column: consumer.Source.Column,
					URL: consumer.Source.URL, Repo: consumer.Source.Repo,
				},
				Criticality: string(consumer.Criticality), Expression: consumer.Expression,
				Metadata: consumer.Metadata, Unresolved: consumer.Unresolved,
			}
			if consumer.Owner != nil {
				encoded.Consumer.Owner = &graphOwner{Name: consumer.Owner.Name, Email: consumer.Owner.Email}
			}
			if consumer.Runtime != nil {
				runtime := consumer.Runtime
				encoded.Consumer.Runtime = &graphRuntime{
					Format: runtime.Format, ExecutionCount: runtime.ExecutionCount,
					FirstSeen: runtime.FirstSeen, LastSeen: runtime.LastSeen, Window: runtime.Window,
					WindowStart: runtime.WindowStart, WindowAnchor: runtime.WindowAnchor,
					ExecutionsPerDay: runtime.ExecutionsPerDay, Origins: runtime.Origins,
					OriginDetails: runtime.OriginDetails,
				}
			}
		}
		document.Nodes = append(document.Nodes, encoded)
	}
	for _, edge := range target.Edges() {
		document.Edges = append(document.Edges, graphEdge{From: edge.From, To: edge.To, Kind: string(edge.Kind)})
	}
	contents, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode graph JSON: %w", err)
	}
	return append(contents, '\n'), nil
}

type finding struct {
	changeID string
	name     string
	file     string
	line     int
	path     string
	owner    string
	runtime  string
}

func findings(result readiness.Result, classification readiness.Classification) []finding {
	var resultFindings []finding
	for _, change := range result.Changes {
		for _, consumer := range change.Consumers {
			if consumer.Classification != classification {
				continue
			}
			path := ""
			if len(consumer.Paths) != 0 {
				path = strings.Join(consumer.Paths[0].Nodes, " -> ")
			}
			resultFindings = append(resultFindings, finding{
				changeID: change.Change.ID,
				name:     consumer.Consumer.Name,
				file:     consumer.Consumer.Source.File,
				line:     consumer.Consumer.Source.Line,
				path:     path,
				owner:    ownerLabel(consumer.Consumer),
				runtime:  runtimeLabel(consumer.Consumer),
			})
		}
	}
	sort.Slice(resultFindings, func(i, j int) bool {
		if resultFindings[i].changeID != resultFindings[j].changeID {
			return resultFindings[i].changeID < resultFindings[j].changeID
		}
		return resultFindings[i].name < resultFindings[j].name
	})
	return resultFindings
}

func runtimeConsumerCount(result readiness.Result) int {
	observed := make(map[string]struct{})
	for _, change := range result.Changes {
		for _, consumer := range change.Consumers {
			if consumer.Consumer.Runtime != nil {
				observed[consumer.Consumer.ID] = struct{}{}
			}
		}
	}
	return len(observed)
}

func runtimeLabel(consumer domain.Consumer) string {
	if consumer.Runtime == nil {
		return ""
	}
	return fmt.Sprintf(
		"%d execution(s), last %s, window %s",
		consumer.Runtime.ExecutionCount,
		consumer.Runtime.LastSeen,
		consumer.Runtime.Window,
	)
}

func ownerLabel(consumer domain.Consumer) string {
	if consumer.Owner != nil {
		return consumer.Owner.Name
	}
	candidates := ownership.Candidates(consumer)
	if len(candidates) != 0 {
		return "ambiguous: " + strings.Join(candidates, ", ")
	}
	if ownership.Unassigned(consumer) {
		return "unassigned by CODEOWNERS"
	}
	return ""
}

func formatLocation(file string, line int) string {
	if file == "" {
		return "unknown source"
	}
	if line == 0 {
		return file
	}
	return fmt.Sprintf("%s:%d", file, line)
}
