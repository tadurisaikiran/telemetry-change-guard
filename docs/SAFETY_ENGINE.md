# Generic safety engine

Telemetry Change Guard separates discovered facts from policy. A native
`tcg/v1alpha1` `ChangeSet` enters the existing adapter and dependency-graph
pipeline. The impact layer then emits findings before any policy rule is
consulted:

```text
ChangeSet
    -> discovery and diagnostics
    -> dependency graph
    -> immutable impact findings
    -> generic safety policy
    -> tcg-result/v1alpha1
```

The canonical `telemetry-change-guard check` command and composite GitHub
Action expose the engine for local and CI use. Explicit ChangeSets, mapped
Weaver diffs, and snapshot pairs normalize before this pipeline; source choice
cannot change policy semantics.

## Findings

Each finding identifies one change and affected consumer, and retains the
consumer source, criticality, matching references, provenance, and direct or
transitive dependency paths. Findings are copied and deterministically sorted
before policy runs. Policy may change handling but never delete, edit, or hide
a finding.

The initial consumer-to-impact mapping is:

| Consumer kinds | Impact |
| --- | --- |
| dashboard, dashboard panel, query, runbook | `VISIBILITY_LOSS` |
| alert rule | `ALERTING_RISK` |
| SLO | `SLO_RISK` |
| autoscaler | `SCALING_RISK` |
| deployment gate | `DEPLOYMENT_GATE_RISK` |
| automation | `AUTOMATION_RISK` |
| recording rule, collector configuration, source code | `SEMANTIC_RISK` |

Consumer kind and operational impact are separate dimensions. Unsupported
relevant consumer kinds fail instead of being assigned a guessed impact.
Symbols match only inside the same telemetry domain and symbol kind.
Prometheus metric-family suffix handling is explicit and does not create
cross-domain aliases.

## Policy

The default enforcement policy warns on visibility and semantic findings. It
blocks alerting, SLO, scaling, deployment-gate, and automation findings at
`high` or `critical` consumer criticality. Findings below a configured blocking
threshold remain warnings; they never become a clean pass.

Rollout modes are:

- `audit`: retain findings and report attention without enforcing block rules;
- `warn`: retain findings and downgrade configured blocks to warnings;
- `enforce`: apply configured blocking actions at their criticality threshold.

An omitted impact rule defaults to a warning. Unknown impact types, actions,
criticalities, rollout modes, contradictory finding data, and duplicate
findings produce `ERROR` and preserve the supplied findings for inspection.

Controlled, expiring policy exceptions are part of the accepted architecture
but are not loaded by this initial engine milestone. They will be introduced
with strict configuration validation and auditable result fields; no exception
will remove its underlying finding.

## Status precedence

The result has one authoritative status:

1. `ERROR` — the requested analysis or policy evaluation could not execute
   correctly.
2. `INCOMPLETE` — required relevant evidence is unavailable or unresolved.
3. `BLOCK` — complete deterministic evidence violates enforced policy.
4. `WARN` — findings or advisory diagnostics require attention without an
   enforced violation.
5. `PASS` — required analysis completed and no finding or diagnostic remains.

Known findings remain present under `ERROR` and `INCOMPLETE`. Required adapter
failure is distinct from a valid empty result, and a consumer with unresolved
relevant evidence can never produce `PASS`.

## Machine and process contract

The generic JSON schema version is `tcg-result/v1alpha1`. Fields use
lower-camel JSON names and include the normalized `changeSet`, `status`,
`findings`, policy `decisions`, discovery `diagnostics`, and evaluation
`errors`. The exact shape is protected by a checked-in golden test.

Generic exit codes are:

| Status | Exit code |
| --- | ---: |
| `PASS`, `WARN` | 0 |
| `ERROR` | 1 |
| `BLOCK` | 2 |
| `INCOMPLETE` | 3 |

The legacy `tmr-result/v1alpha1` model, readiness classifications, status
precedence, and exit codes remain unchanged. Generic safety and migration
readiness share deterministic graph and symbol-matching facts, but each owns
its separate machine contract.

AI is outside this decision boundary. It may explain a result or propose a
candidate remediation, but it cannot create, suppress, or modify a finding,
status, decision, or exit code.
