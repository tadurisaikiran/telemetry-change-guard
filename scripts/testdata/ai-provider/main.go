// Command ai-provider is a deterministic protocol fixture for executable-level
// AI explanation and remediation canaries. It does not call a model.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	explanationRequest  = "tmr-ai-explanation-request/v1alpha1"
	explanationResponse = "tmr-ai-explanation-response/v1alpha1"
	remediationRequest  = "tmr-ai-remediation-request/v1alpha1"
	remediationResponse = "tmr-ai-remediation-response/v1alpha1"
)

type request struct {
	SchemaVersion string `json:"schemaVersion"`
	Findings      []struct {
		Consumer struct {
			ID string `json:"id"`
		} `json:"consumer"`
	} `json:"findings"`
	Targets []struct {
		ID               string `json:"id"`
		BeforeExpression string `json:"beforeExpression"`
		From             struct {
			Name string `json:"name"`
		} `json:"from"`
		To struct {
			Name string `json:"name"`
		} `json:"to"`
	} `json:"targets"`
}

func main() {
	var input request
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fail("decode request: %v", err)
	}
	encoder := json.NewEncoder(os.Stdout)
	switch input.SchemaVersion {
	case explanationRequest:
		if len(input.Findings) == 0 || input.Findings[0].Consumer.ID == "" {
			fail("explanation request has no finding")
		}
		response := map[string]any{
			"schemaVersion": explanationResponse,
			"answer":        "Start with the highest-ranked confirmed dependency and keep unresolved evidence open.",
			"priorities": []any{map[string]any{
				"order":      1,
				"consumerId": input.Findings[0].Consumer.ID,
				"action":     "Migrate the confirmed upstream dependency first.",
				"rationale":  "TCG ranked this configured consumer first by deterministic risk.",
			}},
			"limitations": []string{"This fixture explains evidence but cannot change readiness."},
		}
		if err := encoder.Encode(response); err != nil {
			fail("encode explanation: %v", err)
		}
	case remediationRequest:
		if len(input.Targets) == 0 {
			fail("remediation request has no eligible target")
		}
		target := input.Targets[0]
		after := strings.ReplaceAll(target.BeforeExpression, target.From.Name, target.To.Name)
		if target.ID == "" || target.From.Name == "" || target.To.Name == "" || after == target.BeforeExpression {
			fail("remediation target cannot be transformed")
		}
		response := map[string]any{
			"schemaVersion": remediationResponse,
			"candidates": []any{map[string]any{
				"id":               "deterministic-canary-candidate",
				"targetId":         target.ID,
				"beforeExpression": target.BeforeExpression,
				"afterExpression":  after,
				"rationale":        "Use the explicit rename destination supplied by deterministic evidence.",
			}},
		}
		if err := encoder.Encode(response); err != nil {
			fail("encode remediation: %v", err)
		}
	default:
		fail("unsupported request schema %q", input.SchemaVersion)
	}
}

func fail(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", arguments...)
	os.Exit(1)
}
