# How Telemetry Change Guard turns operational dependencies into a CI contract

> **Draft technical deep dive — verify against the exact release commit before
> publication.**

## Design objective

Telemetry Change Guard evaluates one bounded question:

> Given this proposed telemetry contract change, these configured evidence
> sources, and this policy, which known operational consumers are affected and
> what decision follows?

It deliberately does not answer that question with model prose or name
similarity. The public result must be deterministic, explainable, and scoped to
the evidence the user selected.

## Pipeline

```text
Change source
  ChangeSet | snapshots | mapped Weaver diff | migration plan
      |
Strict normalization
      |
Evidence discovery
  local files | bounded optional remote reads
      |
Typed graph
  symbols | consumers | references | productions | mappings
      |
Impact analysis
  direct paths | transitive paths | unresolved evidence
      |
Safety policy
  audit | warn | enforce
      |
Human report + versioned JSON + authoritative status/exit
```

Each stage retains provenance needed by the next. A result can include source
file and line, expression, extraction method, criticality, owner, and the
symbol-to-consumer path where available.

## Change sources are explicit

The canonical `check` command accepts exactly one change source:

- a native `tcg/v1alpha1` ChangeSet;
- baseline and candidate telemetry snapshots; or
- an OpenTelemetry Weaver diff with explicit mapping evidence.

Migration compatibility mode accepts a `telemetry-migration/v1alpha1` plan.
Strict decoding rejects unknown fields and incompatible envelopes instead of
guessing user intent.

Snapshot comparison produces a full versioned diff and can emit an actionable
removal ChangeSet. It still reflects only the queried Prometheus environment
and capture time.

## Evidence becomes a typed graph

Adapters emit domain objects rather than free-form strings:

- symbols identify telemetry contracts within an explicit domain;
- consumers represent dashboards, alerts, SLOs, queries, scalers, and gates;
- references connect a consumer expression to a symbol;
- productions represent derived symbols such as recording-rule outputs; and
- mappings connect identities only when the user supplies evidence.

Prometheus, OpenTelemetry, Tempo, Kubernetes, and infrastructure identities do
not collapse because names look alike. This avoids a convenient but unsafe
class of inferred dependencies.

PromQL extraction uses a typed parser. Static TraceQL analysis is accepted only
within the configured Tempo validation contract. Dynamic expressions remain
unresolved evidence.

## Direct and transitive impact

For a direct alert:

```text
checkout_requests_total
  -> CheckoutTrafficMissing
  -> ALERTING_RISK
```

For a derived metric:

```text
checkout_request_duration_seconds_bucket
  -> checkout:p95_latency recording rule
  -> CheckoutLatencyHigh alert
  -> ALERTING_RISK
```

The recording rule is both a consumer of the base metric and a producer of a
derived metric. With transitive analysis enabled, graph traversal carries the
change through that production edge and retains each affected consumer.

Cycle handling and ordering are deterministic. The output does not depend on
configuration order.

## Impact is operational, not merely textual

Consumer type drives the first-order risk class:

| Consumer | Example impact |
| --- | --- |
| Grafana panel or query | `VISIBILITY_LOSS` |
| Prometheus alert | `ALERTING_RISK` |
| Sloth/Pyrra SLO | `SLO_RISK` |
| KEDA or explicitly mapped HPA metric | `SCALING_RISK` |
| Argo Rollouts analysis | `DEPLOYMENT_GATE_RISK` |
| Recording-rule semantic path | `SEMANTIC_RISK` |

Policy combines that impact with consumer criticality and configured thresholds.
`audit` and `warn` change decisions without deleting findings; `enforce` applies
the blocking contract.

## Missing evidence is a result state

The dangerous shortcut is to treat “the adapter found nothing” as “nothing
depends on this.” TCG distinguishes the two.

A required glob that does not load, a malformed rules file, an unresolved
dynamic query, a denied remote origin, or a missing mapping emits diagnostics
and produces `INCOMPLETE` or `ERROR` according to the contract. Confirmed paths
remain in the report.

Generic statuses and exits are:

| Status | Exit |
| --- | ---: |
| `PASS`, `WARN` | 0 |
| `ERROR` | 1 |
| `BLOCK` | 2 |
| `INCOMPLETE` | 3 |

Integrations should read the top-level versioned status and preserve the exit
code. They should not infer safety from an empty nested array.

## Local-first and bounded remote evidence

The deterministic core has no database and no default network dependency.
Local adapters read bounded files. Remote adapters are optional and off by
default in the Action.

When credentials are configured, their trusted origin must come from trusted
workflow configuration, use HTTPS outside explicit loopback development, and
match after canonical normalization. Cross-origin redirects, URL user
information, injected query/fragment components, and untrusted destinations
are rejected. Tokens are not included in findings or reports.

## One analysis in the GitHub Action

The composite Action validates source combinations, builds the canonical CLI,
runs one analysis, writes console/JSON/Markdown views from that result, exposes
status and exit outputs, optionally updates one bounded pull-request comment,
uploads an artifact, and finally enforces the authoritative exit.

Pinning the Action to a full reviewed SHA protects callers from movable
pre-release coordinates. The candidate does not advertise `@v1`.

## Build and supply-chain controls

The release pipeline is prepared to build reproducible multi-platform archives
for the canonical and compatibility binaries, checksums, a manifest, SPDX and
CycloneDX SBOMs, and provenance inputs. Container verification covers Linux
amd64/arm64, non-root distroless execution, immutable labels, per-platform SPDX
attestations, and subject binding.

These are build and verification capabilities; no release or package is
published by ordinary pull-request workflows.

## AI is outside the decision boundary

Optional providers can receive bounded, redacted deterministic evidence for
explanation or candidate remediation. Responses are strictly decoded, size and
time bounded, treated as untrusted, and rendered without replacing the
authoritative status.

The experimental agentic controller mounts an isolated workspace, permits a
provider-neutral adapter to draft edits, reruns the real public TCG binary, and
produces an uncommitted review bundle. It has no approval, commit, push,
pull-request, or merge authority.

This design allows an AI reader or fixer to save manual work without making a
probabilistic model the production-safety judge.

## Verification strategy

The repository combines:

- unit and golden tests for parsing, graph, impact, readiness, report, and
  policy contracts;
- race detection and parser fuzz smoke tests;
- adversarial credential/origin and AI protocol tests;
- hosted Action and external-consumer modes;
- pinned Prometheus/Grafana/Sloth, Tempo, and control-plane lifecycles;
- vulnerability, CodeQL, dependency, and workflow-policy analysis;
- reproducible artifact and container checks; and
- an 11-case synthetic benchmark that compares exact expected and actual
  machine contracts.

The synthetic benchmark detects regressions in reviewed fixtures. Independent
field evaluation is collected separately through the
[evaluation kit](../../evaluation/README.md).

## Reading a result responsibly

Treat the tuple as the contract:

```text
exact TCG version/commit
+ proposed change source
+ configured required and optional evidence
+ policy mode and thresholds
= scoped result
```

Changing any element can change the conclusion. A clean result does not replace
canaries, monitoring, rollback, consumer ownership, or human review. It adds a
specific missing control: review-time analysis of configured telemetry
consumers.
