package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/impact"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/safety"
)

// SafetyJSON renders the versioned generic machine result.
func SafetyJSON(result safety.Result) ([]byte, error) {
	contents, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode generic safety JSON report: %w", err)
	}
	return append(contents, '\n'), nil
}

// SafetyConsole renders a compact generic safety report for terminals.
func SafetyConsole(writer io.Writer, result safety.Result) error {
	var output bytes.Buffer
	fmt.Fprintln(&output, productName)
	fmt.Fprintln(&output, strings.Repeat("=", len(productName)))
	fmt.Fprintf(&output, "ChangeSet: %s\n", result.ChangeSet.Metadata.Name)
	fmt.Fprintf(&output, "Status:    %s\n", result.Status)
	fmt.Fprintf(&output, "Findings:  %d\n", len(result.Findings))

	decisions := safetyDecisions(result.Decisions)
	for _, finding := range result.Findings {
		decision, decided := decisions[findingKey(finding)]
		fmt.Fprintf(
			&output,
			"\n[%s] %s — %s\n",
			safetyActionLabel(decision, decided),
			finding.Impact,
			finding.Consumer.Name,
		)
		fmt.Fprintf(&output, "  Change:      %s\n", finding.Change.ID)
		fmt.Fprintf(&output, "  Consumer:    %s (%s)\n", finding.Consumer.ID, finding.Consumer.Kind)
		fmt.Fprintf(&output, "  Criticality: %s\n", finding.Criticality)
		fmt.Fprintf(&output, "  Source:      %s\n", formatLocation(finding.Consumer.Source.File, finding.Consumer.Source.Line))
		if finding.Uncertain {
			fmt.Fprintln(&output, "  Evidence:    unresolved")
		}
		if len(finding.Paths) != 0 {
			fmt.Fprintf(&output, "  Path:        %s\n", strings.Join(finding.Paths[0].Nodes, " -> "))
		}
		if decided {
			fmt.Fprintf(&output, "  Policy:      %s\n", decision.Reason)
		}
	}

	if len(result.Diagnostics) != 0 {
		fmt.Fprintln(&output, "\nDIAGNOSTICS")
		for _, diagnostic := range result.Diagnostics {
			requirement := "optional"
			if diagnostic.Required {
				requirement = "required"
			}
			fmt.Fprintf(
				&output,
				"  - [%s/%s] %s: %s\n",
				diagnostic.Adapter,
				requirement,
				formatLocation(diagnostic.Source.File, diagnostic.Source.Line),
				diagnostic.Message,
			)
		}
	}
	if len(result.Errors) != 0 {
		fmt.Fprintln(&output, "\nERRORS")
		for _, message := range result.Errors {
			fmt.Fprintf(&output, "  - %s\n", message)
		}
	}

	fmt.Fprintf(&output, "\nSTATUS: %s\n", result.Status)
	_, err := writer.Write(output.Bytes())
	return err
}

// SafetyMarkdown renders a generic safety report for pull-request comments
// and artifacts.
func SafetyMarkdown(writer io.Writer, result safety.Result) error {
	var output bytes.Buffer
	fmt.Fprintf(&output, "# %s\n", productName)
	fmt.Fprintf(&output, "\n**ChangeSet:** `%s`  \n", result.ChangeSet.Metadata.Name)
	fmt.Fprintf(&output, "**Status:** **%s**  \n", result.Status)
	fmt.Fprintf(&output, "**Findings:** %d\n", len(result.Findings))

	decisions := safetyDecisions(result.Decisions)
	if len(result.Findings) != 0 {
		fmt.Fprintln(&output, "\n## Operational impact")
	}
	for _, finding := range result.Findings {
		decision, decided := decisions[findingKey(finding)]
		fmt.Fprintf(
			&output,
			"\n- **%s — %s** (`%s`)\n",
			finding.Impact,
			finding.Consumer.Name,
			finding.Change.ID,
		)
		fmt.Fprintf(&output, "  - Consumer: `%s` (`%s`)\n", finding.Consumer.ID, finding.Consumer.Kind)
		fmt.Fprintf(&output, "  - Criticality: `%s`\n", finding.Criticality)
		fmt.Fprintf(&output, "  - Effective action: **%s**\n", safetyActionLabel(decision, decided))
		fmt.Fprintf(&output, "  - Source: `%s`\n", formatLocation(finding.Consumer.Source.File, finding.Consumer.Source.Line))
		if finding.Uncertain {
			fmt.Fprintln(&output, "  - Evidence: **unresolved**")
		}
		if len(finding.Paths) != 0 {
			fmt.Fprintf(&output, "  - Dependency path: `%s`\n", strings.Join(finding.Paths[0].Nodes, " -> "))
		}
		if decision.Reason != "" {
			fmt.Fprintf(&output, "  - Policy: %s\n", decision.Reason)
		}
	}

	if len(result.Diagnostics) != 0 {
		fmt.Fprintln(&output, "\n## Diagnostics")
		for _, diagnostic := range result.Diagnostics {
			requirement := "optional"
			if diagnostic.Required {
				requirement = "required"
			}
			fmt.Fprintf(&output, "\n- `%s/%s`: %s\n", diagnostic.Adapter, requirement, diagnostic.Message)
		}
	}
	if len(result.Errors) != 0 {
		fmt.Fprintln(&output, "\n## Errors")
		for _, message := range result.Errors {
			fmt.Fprintf(&output, "\n- %s\n", message)
		}
	}

	_, err := writer.Write(output.Bytes())
	return err
}

func safetyDecisions(decisions []safety.Decision) map[string]safety.Decision {
	result := make(map[string]safety.Decision, len(decisions))
	for _, decision := range decisions {
		result[decision.ChangeID+"\x00"+decision.ConsumerID+"\x00"+string(decision.Impact)] = decision
	}
	return result
}

func findingKey(finding impact.Finding) string {
	return finding.Change.ID + "\x00" + finding.Consumer.ID + "\x00" + string(finding.Impact)
}

func safetyActionLabel(decision safety.Decision, decided bool) string {
	if !decided {
		return "UNDECIDED"
	}
	return strings.ToUpper(string(decision.EffectiveAction))
}
