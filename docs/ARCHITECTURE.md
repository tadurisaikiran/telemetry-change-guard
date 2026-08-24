# Architecture

Telemetry Change Guard analyzes telemetry contract changes and their
downstream consumers. The long-term product answers three questions:

1. What changed?
2. Which consumers still depend on the legacy contract?
3. What operational impact follows, and does deterministic policy permit it?

Migration readiness additionally asks when backward compatibility can be
removed. It is a first-class compatibility workflow over the same facts, not
the root abstraction.

The local deterministic pipeline described below is implemented today. Its
root change input is a `domain.ChangeSet`; the legacy `domain.Migration`
compatibility API is validated and normalized before entering that pipeline.

## Non-negotiable rules

1. The deterministic core is authoritative.
2. An LLM must never make or override a migration safety decision.
3. Formal parsers take precedence over regular expressions and model inference.
4. Missing, malformed, or unresolved required evidence fails closed.
5. Every dependency finding retains its evidence and source location.
6. Prometheus, OpenTelemetry, Tempo, and Loki are separate domains. Similar
   names do not establish a mapping.
7. External systems enter through adapters and do not shape the core model.
8. The core remains local-first and does not phone home.

## Implemented deterministic pipeline

```text
explicit migration or mapped Weaver diff + source configuration
             |
             v
strict validation and local/remote evidence adapters
             |
             v
official PromQL AST / Tempo-validated TraceQL reference extraction
             |
             v
advisory ownership enrichment
             |
             v
in-memory dependency graph
             |
             v
policy-independent impact findings
             |
             v
generic safety policy or legacy readiness compatibility
             |
             v
console / JSON / Markdown reports
```

An explicitly enabled explanation path begins only after that pipeline has
finished:

```text
authoritative readiness result
             |
             v
minimal redacted evidence packet
             |
             v
optional local AI provider process
             |
             v
non-authoritative explanation + unchanged status
```

`internal/config` owns YAML-specific migration and configuration document
structs. It rejects unknown
fields, multiple YAML documents, oversized input, and invalid change shapes.
It then normalizes the document into `internal/domain` and applies reusable
domain validation.

`internal/domain` contains vendor-neutral ChangeSets, legacy migrations,
symbols, consumers, references, evidence, productions, diagnostics, source
locations, and owners.
Adapter-specific document shapes never leak into this package.

`adapters/weaver` consumes current structured Weaver V1/V2 registry diffs and
requires explicit OpenTelemetry-to-Prometheus mappings. Missing mappings stop
the pipeline before readiness evaluation. It neither invokes Weaver nor infers
backend names.

The consumer adapters normalize Prometheus rule YAML, PrometheusRule CRDs, Grafana
dashboard JSON, Sloth SLO YAML, and Pyrra SLO YAML. Malformed or unresolved
required input becomes a diagnostic rather than evidence of absence.

`adapters/persesusage` is an optional remote evidence boundary around the
Perses metrics-usage HTTP API. It imports usage associations, then independently
parses returned rule expressions and models recording-rule productions. It
does not import Perses packages or allow remote data to decide readiness.
Missing dashboard query details and partial metric names remain scoped,
unresolved evidence.

`adapters/runtimequeries` imports bounded local JSONL evidence from the
Prometheus query log or Telemetry Change Guard's provider-neutral runtime-query schema. It
aggregates identical expressions inside a window anchored to the newest valid
record, then sends every distinct expression through the same official PromQL
AST analyzer. Runtime consumers are additive: the adapter never removes a
configured consumer or interprets an absent observation as evidence of safety.
See [the runtime query evidence guide](RUNTIME_EVIDENCE.md).

`adapters/tempo` loads a strict local query inventory and submits each TraceQL
expression to the configured Tempo Search API for official parser validation.
Only then does `pkg/traceql` conservatively extract scoped span/resource
attributes. Explicit mappings add parallel OpenTelemetry symbols; no name-based
alias is inferred. Tempo remains an optional remote adapter, and its AGPL parser
is not linked into Telemetry Change Guard's Apache-licensed binary. See [the Tempo integration
guide](TEMPO.md).

`pkg/promql` uses Prometheus's official parser and walks the typed AST. It does
not use substring matching to establish metric or label dependencies.

`internal/ownership` runs after consumer discovery and before graph
construction. It applies strict Telemetry Change Guard ownership metadata, GitHub CODEOWNERS, and
Grafana tag evidence with fixed precedence. It enriches `domain.Consumer` only;
its diagnostics are advisory and it has no dependency on `internal/readiness`.
See [the ownership discovery guide](OWNERSHIP.md).

`internal/graph`, `internal/impact`, `internal/safety`, and
`internal/readiness` form the deterministic core. The graph is rebuilt in
memory for every run and traversal is cycle-safe. `internal/impact` emits
policy-independent findings with evidence and dependency paths.
`internal/safety` applies generic policy and is the only component allowed to
produce `PASS`, `WARN`, `BLOCK`, `INCOMPLETE`, or `ERROR`. The compatibility
`internal/readiness` evaluator independently preserves the existing
`READY`/`BLOCKED` migration contract over shared matching and graph facts.

Rollout policy never edits or removes findings. Required diagnostics or
unresolved relevant evidence yield `INCOMPLETE` even when a blocking finding
is already proven; both facts remain in the machine result. See
[the safety engine guide](SAFETY_ENGINE.md).

`internal/cli` is the shared command boundary. `cmd/telemetry-change-guard` is
the canonical executable over generic safety, impact exploration, graph export,
and nested migration workflows. `cmd/tmr` is a thin compatibility entry point
over the same implementation; it does not fork behavior. Versioned JSON is the
stable machine API for Actions and future optional integrations. Exit codes are
part of the public contract. Progress percentages remain informational and
never establish safety.

`internal/config` strictly accepts the canonical `tcg/v1alpha1` `Config`
envelope and the existing `tmr/v1alpha1` envelope, then normalizes both to one
canonical value. Product-owned environment references use `TCG_*` with
conflict-safe `TMR_*` fallback. Environment values are snapshotted before
adapter execution, and conflicts fail as runtime errors without exposing
secret values.

The root composite Action selects exactly one generic or migration workflow and
invokes the canonical CLI once. Companion Markdown and JSON files are rendered
from that same result. The Action uploads JSON evidence, updates at most one
bounded pull-request comment, and enforces the exact CLI exit code after
reporting steps. Missing artifacts or status/exit disagreement fail closed.

`internal/explanation` builds a minimal packet containing only blockers,
uncertainties, diagnostics, migration changes, and aggregate counts. It invokes
a user-selected executable directly through a strict JSON protocol, with no
shell and no provider SDK. The response schema has no status or patch field,
unknown fields are rejected, and rendering repeats the deterministic status on
both sides of provider-authored prose. This package cannot call the readiness
evaluator with modified evidence.

`internal/remediation` is a separate candidate-only boundary. It exposes only
confirmed direct local Prometheus-rule YAML and Grafana JSON expressions as
targets. Provider proposals cannot select a source path or locator. Telemetry Change Guard finds
one exact scalar, changes an in-memory artifact, reparses it through the owning
adapter, replaces that source's discovery in memory, rebuilds the graph, and
reruns the same readiness policy. The source file and authoritative result are
never mutated; simulated status is validation evidence, not current state.

## Next architectural layers

- Log and Collector configuration analysis.
- APIs, MCP, and server/UI modes.
- Additional live end-to-end tiers described in `TESTING.md`.

These remain adapters or optional consumers of the deterministic engine. None
may weaken or override its readiness result.
