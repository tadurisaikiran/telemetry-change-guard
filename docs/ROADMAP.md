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
- Canonical dual-mode GitHub Action with one-evaluation Markdown/JSON output,
  bounded comment updates, JSON artifact upload, and fail-closed enforcement.
- Deterministic change-source normalization for explicit ChangeSets, mapped
  Weaver diffs, and bounded Prometheus baseline/candidate snapshots, including
  full delta output and canonical Action support.
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
- KEDA ScaledObject Prometheus dependencies with production-aware autoscaler
  criticality and deterministic `SCALING_RISK` enforcement.
- Argo Rollouts AnalysisTemplate and ClusterAnalysisTemplate Prometheus
  deployment gates with fail-closed argument handling and deterministic
  `DEPLOYMENT_GATE_RISK` enforcement.
- Kubernetes autoscaling/v2 HPA External/Object/Pods dependencies backed only
  by explicit Kubernetes-to-Prometheus metric and selector-label mappings.
- Combined KEDA, Argo Rollouts, and HPA control-plane lifecycle covering exact
  blocking findings, completed migration, and required uncertainty.
- Accepted AWS CloudWatch identity and migration semantics for Classic and OTel
  metrics, including origin account/Region, units, and explicit mappings.
- Bounded, deterministic synthesized CloudFormation JSON and Cloud Assembly
  ingestion with strict JSON validation, safe rooted file access, complete
  resource provenance, and no template or application execution.
- Pinned live Prometheus/Grafana/Sloth migration lifecycle.

## Current release path

1. Add fail-closed `EXACT`/`PARTIAL`/`UNKNOWN` resolution for the supported
   CloudFormation intrinsic subset.
2. Discover standard CloudWatch alarms and their exact Classic metric
   dependencies.
3. Add metric-math dependency extraction and transitive analysis.
4. Add composite-alarm dependency extraction and cycle-safe traversal.
5. Discover CloudWatch dashboard metric consumers.
6. Analyze CloudWatch OTel PromQL alarms without collapsing their domain into
   Prometheus.
7. Discover Application Auto Scaling metric and metric-math dependencies.
8. Discover CloudWatch alarm actions and their automation impact.
9. Add the read-only, paginated, throttling-aware live CloudWatch adapter.
10. Reconcile actual deployed AWS dependencies with candidate synthesized
    CloudFormation state.
11. Validate the complete AWS lifecycle in a short-lived-role sandbox E2E.
12. Complete compatibility, failure-injection, fuzz, vulnerability, benchmark,
   release-provenance, and reproducible-demo gates for the first TCG release.

## Optional integrations after the core release gate

- LogQL, Collector configuration, MCP, and server/UI modes.

These additions cannot weaken the local deterministic readiness result. The
ordering may change in response to design-user evidence; changes should be
recorded in issues and pull requests.
