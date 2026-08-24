# ADR-0004: Generic safety policy versus migration readiness

- Status: Proposed
- Date: 2026-08-24
- Owners: Project maintainers
- Decision scope: Findings, impact taxonomy, policy, and compatibility results

## Context

Migration readiness answers whether legacy consumers have moved to replacement
telemetry. Generic change safety answers whether any supported telemetry change
creates unacceptable operational impact. They use the same evidence and graph,
but their statuses and user contracts are not interchangeable.

Policy must never erase deterministic evidence. AI explanations and
remediation proposals must remain downstream of the authoritative decision.

## Decision

1. Discovery and graph analysis produce immutable findings before policy runs.
   A finding includes the change, consumer, dependency paths, provenance,
   consumer criticality, operational impact type, and uncertainty.
2. `ConsumerKind` describes what depends on telemetry. `ImpactType` describes
   the operational consequence. They remain separate dimensions.
3. Initial generic impact types are:
   `VISIBILITY_LOSS`, `ALERTING_RISK`, `SLO_RISK`, `SCALING_RISK`,
   `DEPLOYMENT_GATE_RISK`, `AUTOMATION_RISK`, and `SEMANTIC_RISK`.
   An adapter emits only impacts it can support with deterministic evidence.
4. The generic machine result uses `tcg-result/v1alpha1` and one authoritative
   status: `PASS`, `WARN`, `BLOCK`, `INCOMPLETE`, or `ERROR`.
5. Aggregate evaluation precedence is:
   - `ERROR` when the requested analysis cannot execute correctly;
   - `INCOMPLETE` when required relevant evidence cannot be resolved;
   - `BLOCK` when complete deterministic evidence violates enforced policy;
   - `WARN` when findings require attention but do not violate enforced policy;
   - `PASS` only when required analysis completed and no warning or blocking
     finding remains.
6. Known findings remain present when an aggregate status is `ERROR` or
   `INCOMPLETE`; status precedence must not hide already proven impact.
7. Generic exit codes are `0` for `PASS` and `WARN`, `1` for `ERROR`, `2` for
   `BLOCK`, and `3` for `INCOMPLETE`.
8. Policy supports `audit`, `warn`, and `enforce` rollout modes. Rollout mode can
   change enforcement behavior but cannot change or remove findings.
9. Exceptions are explicit, scoped to a change and consumer, owned, justified,
   expiring, and auditable. Expired or malformed exceptions fail. An exception
   changes policy handling only; the underlying finding remains.
10. Legacy migration results retain `tmr-result/v1alpha1`, the existing
    `READY`, `BLOCKED`, `INCOMPLETE`, and `ERROR` statuses, existing
    classifications, and existing exit behavior. They are derived through a
    compatibility layer over shared facts, not silently redefined.
11. AI may explain, prioritize, or propose remediation. It cannot alter a
    finding, status, exception, or exit code. Candidate remediation must be
    parsed and re-evaluated deterministically.

## Fail-closed rules

- Required unresolved evidence produces `INCOMPLETE`, not `PASS` or `WARN`.
- Adapter failure is distinguished from an empty valid result.
- Unsupported relevant syntax is reported, not skipped.
- Absence of runtime observations is never proof that a dependency is absent.
- A policy default cannot lower a known consumer's explicitly configured
  criticality.

## Consequences

- Generic safety and migration readiness can evolve without breaking each
  other's machine contracts.
- Reports may show both an aggregate status and detailed proven findings, which
  is essential when uncertainty coexists with known impact.
- Rollout modes enable adoption without teaching the engine to call incomplete
  analysis safe.
- New control-plane adapters map to explicit impact types instead of overloading
  consumer categories.

## Rejected alternatives

- **Reuse READY/BLOCKED for all changes:** rejected because readiness and
  generic safety answer different questions.
- **Let policy suppress findings:** rejected because it destroys auditability.
- **Treat unknown evidence as a warning:** rejected because required missing
  evidence must fail closed.
- **Let AI decide safety:** rejected because safety decisions must be
  reproducible and testable.
- **Mutate the legacy result schema:** rejected because it would break existing
  users and Action consumers.

## Validation

- Truth-table tests cover every status precedence and rollout mode.
- Compatibility golden tests prove legacy results and exit codes do not change.
- Failure-injection tests distinguish adapter error, unsupported input, empty
  valid discovery, and unresolved relevant evidence.
- Exception tests cover scope, ownership, expiry, conflicts, and preservation
  of underlying findings.
- Red-team tests attempt false `PASS` and false `READY` outcomes before the
  implementation is eligible for review.
