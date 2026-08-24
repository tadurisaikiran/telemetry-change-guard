# Architecture

Telemetry Migration Readiness analyzes telemetry contract changes and their
downstream consumers. The long-term product answers three questions:

1. What changed?
2. Which consumers still depend on the legacy contract?
3. Has deterministic policy established that backward compatibility can be
   removed?

Only the migration-model foundation described below is implemented today.

## Non-negotiable rules

1. The deterministic core is authoritative.
2. An LLM must never make or override a migration safety decision.
3. Formal parsers take precedence over regular expressions and model inference.
4. Missing, malformed, or unresolved required evidence fails closed.
5. Every dependency finding retains its evidence and source location.
6. Prometheus and OpenTelemetry are separate domains. Similar names do not
   establish a mapping.
7. External systems enter through adapters and do not shape the core model.
8. The core remains local-first and does not phone home.

## Implemented foundation

```text
YAML manifest
     |
     v
strict config decoder
     |
     v
shape + semantic validation
     |
     v
canonical domain.Migration
     |
     v
tmr validate
```

`internal/config` owns YAML-specific document structs. It rejects unknown
fields, multiple YAML documents, oversized input, and invalid change shapes.
It then normalizes the document into `internal/domain` and applies reusable
domain validation.

`internal/domain` contains only the implemented canonical concepts: migration,
change, domain, and symbol. It intentionally contains no Grafana, Prometheus
rule, graph, evidence, AI, or persistence abstractions yet.

`cmd/tmr` is a thin CLI boundary. It converts validation outcomes to human
output and process exit behavior without embedding migration rules.

## Planned deterministic pipeline

Later milestones will add components in this order:

```text
change source -> consumer adapters -> reference analysis
     -> dependency graph -> impact classification
     -> readiness policy -> versioned reports
```

The first consumer ecosystem will be Prometheus rules, PrometheusRule CRDs,
Grafana PromQL dashboards, Sloth SLOs, and Pyrra SLOs loaded from local files.
The PromQL analyzer will use Prometheus's parser and AST.

The readiness states will eventually be `READY`, `BLOCKED`, `INCOMPLETE`, and
`ERROR`. Progress percentages will be informational and will not establish
safety.

## Explicitly outside the current milestone

- PromQL parsing and dependency extraction.
- Grafana, Prometheus rule, Sloth, or Pyrra adapters.
- Dependency graphs and readiness evaluation.
- OpenTelemetry, trace, and log analysis.
- AI explanation or remediation.
- Runtime backends, databases, APIs, web UI, or hosted services.
- The live end-to-end stack described in `TESTING.md`.
