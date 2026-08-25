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
- Transparent single-maintainer governance, opt-in adopter criteria, citation
  metadata, current related-work positioning, and a multi-workflow design-user
  evaluation guide.

## Current adoption and release path

1. Publish an honest pre-release with binaries, checksums, SBOM/provenance, and
   an immutable Action coordinate
   ([issue #29](https://github.com/tadurisaikiran/telemetry-change-guard/issues/29)).
2. Run the redesigned planned-migration, proposed-change, and control-plane
   design-user workflows against real or faithful sanitized repositories
   ([issue #30](https://github.com/tadurisaikiran/telemetry-change-guard/issues/30)).
3. Earn the first independent required-CI use and accept adopter entries only
   through the opt-in process in [`ADOPTERS.md`](../ADOPTERS.md).
4. Build a licensed, reproducible historical-change corpus and report
   precision, recall, critical false negatives, false positives, runtime, and
   memory ([issue #31](https://github.com/tadurisaikiran/telemetry-change-guard/issues/31)).
5. Use evaluation evidence to prioritize correctness, onboarding, and
   ecosystem work; publish limitations alongside results.

## Optional agentic workstream

The existing deterministic product remains the safety authority and primary
external adoption path. An optional agentic layer may proceed in parallel as
an experiment that calls the public CLI; it does not replace or delay the core
release, Action, or design-user program.

1. Build an isolated, provider-neutral and bounded feedback-loop MVP with no
   branch, change-request, merge, or production mutation
   ([issue #39](https://github.com/tadurisaikiran/telemetry-change-guard/issues/39)).
2. Evaluate no-guard, LLM self-review, conventional-check, and TCG-feedback
   conditions against independent ground truth
   ([issue #40](https://github.com/tadurisaikiran/telemetry-change-guard/issues/40)).
3. Package an opt-in design-user review workflow only after the isolation and
   evaluation gates pass
   ([issue #41](https://github.com/tadurisaikiran/telemetry-change-guard/issues/41)).
4. Evaluate AI-assisted source/diff extraction separately; never make an
   unsupported completeness claim or require it for the MVP
   ([issue #42](https://github.com/tadurisaikiran/telemetry-change-guard/issues/42)).

See the [agentic roadmap](AGENTIC_ROADMAP.md) for shipped/planned boundaries,
architecture, compatibility, promotion gates, and the external sharing plan.

## AWS workstream

AWS continues in parallel without delaying evaluation of the implemented
product:

1. Complete fail-closed `EXACT`/`PARTIAL`/`UNKNOWN` resolution for the
   supported CloudFormation intrinsic subset
   ([issue #18](https://github.com/tadurisaikiran/telemetry-change-guard/issues/18)).
2. Discover standard CloudWatch alarms and their exact Classic metric
   dependencies ([issue #19](https://github.com/tadurisaikiran/telemetry-change-guard/issues/19)).
3. Add metric-math and composite-alarm dependency traversal
   ([issue #20](https://github.com/tadurisaikiran/telemetry-change-guard/issues/20),
   [issue #21](https://github.com/tadurisaikiran/telemetry-change-guard/issues/21)).
4. Discover dashboards, CloudWatch OTel PromQL alarms, Application Auto
   Scaling policies, and alarm actions
   ([issue #22](https://github.com/tadurisaikiran/telemetry-change-guard/issues/22),
   [#23](https://github.com/tadurisaikiran/telemetry-change-guard/issues/23),
   [#24](https://github.com/tadurisaikiran/telemetry-change-guard/issues/24),
   [#25](https://github.com/tadurisaikiran/telemetry-change-guard/issues/25)).
5. Add read-only live discovery and reconcile deployed dependencies with
   candidate synthesized state
   ([issue #26](https://github.com/tadurisaikiran/telemetry-change-guard/issues/26),
   [issue #27](https://github.com/tadurisaikiran/telemetry-change-guard/issues/27)).
6. Validate the complete lifecycle in a short-lived-role AWS sandbox
   ([issue #28](https://github.com/tadurisaikiran/telemetry-change-guard/issues/28)).

## Optional integrations after the core release gate

- LogQL, Collector configuration, MCP, and server/UI modes.

These additions cannot weaken the local deterministic readiness result. The
ordering may change in response to design-user evidence; changes should be
recorded in issues and pull requests.
