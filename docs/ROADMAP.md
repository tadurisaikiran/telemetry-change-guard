# Roadmap

The roadmap is ordered by evidence and user value. Milestone completion means
the documented acceptance tests pass; it does not imply every ecosystem or
deployment is supported.

## Implemented

- Telemetry Change Guard repository transition, product identity, accepted
  architectural decisions, and strict generic `ChangeSet` root model.
- Policy-independent generic impact findings and a deterministic, versioned
  `PASS`/`WARN`/`BLOCK`/`INCOMPLETE`/`ERROR` safety engine while preserving the
  legacy migration readiness contract.
- Canonical `telemetry-change-guard` CLI over shared command code, with generic
  check/impact workflows, nested migration commands, and the temporary `tmr`
  compatibility binary.
- Canonical `tcg/v1alpha1` configuration with normalized `tmr/v1alpha1`
  compatibility and explicit `TCG_*`/`TMR_*` environment conflict handling.
- Repository and canonical migration model (Milestones 0–1).
- PromQL AST extraction and Prometheus rule discovery (Milestones 2–3).
- In-memory transitive graph (Milestone 4).
- Grafana, Sloth, and Pyrra local adapters (Milestones 5–6).
- Fail-closed readiness and versioned reports (Milestones 7–8).
- GitHub Action packaging (Milestone 9).
- Weaver registry-diff import with explicit backend mappings (Milestone 10).
- Perses metrics-usage evidence through a bounded, fail-closed HTTP adapter
  (Milestone 11).
- Provider-neutral, read-only AI explanation over redacted deterministic
  evidence (Milestone 12).
- Candidate PromQL remediation for Prometheus rule YAML and Grafana dashboard
  JSON with adapter reparse and full graph/readiness reanalysis (Milestone 13).
- Advisory consumer ownership discovery from strict Telemetry Change Guard metadata, GitHub
  CODEOWNERS, and conventional Grafana tags (Milestone 14).
- Deterministic runtime query evidence from Prometheus query logs and a
  provider-neutral query-history format (Milestone 15).
- Tempo-validated TraceQL consumers with explicit OpenTelemetry span/resource
  attribute mappings (Milestone 16).
- Pinned live Prometheus/Grafana/Sloth migration lifecycle.

## Current release path

1. Publish the new generic/migration dual-mode GitHub Action without breaking
   the legacy repository Action.
2. Add explicit and snapshot/diff change-source adapters, then prioritized
   control-plane adapters such as KEDA and Argo Rollouts.
3. Complete compatibility, failure-injection, fuzz, vulnerability, benchmark,
   release-provenance, and reproducible-demo gates for the first TCG release.

## Optional integrations after the core release gate

- LogQL, Collector configuration, MCP, and server/UI modes.

These additions cannot weaken the local deterministic readiness result. The
ordering may change in response to design-user evidence; changes should be
recorded in issues and pull requests.
