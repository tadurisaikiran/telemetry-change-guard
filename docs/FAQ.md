# Frequently asked questions

## What problem does TCG solve?

TCG treats telemetry as an API contract. Given a proposed contract change and
configured consumer evidence, it reports affected dependency paths and applies
deterministic policy before merge or deployment.

## Why do normal tests and CI miss this?

Producer tests usually prove that application code compiles and emits expected
signals. Alerts, dashboards, SLOs, autoscalers, rollout gates, and runtime
queries may live in different files, repositories, or systems. TCG checks the
consumer evidence you explicitly make available.

## Does `PASS` mean the organization has no other consumers?

No. `PASS` means no blocking impact was found under the configured evidence and
policy. It is not proof about unconfigured repositories, inaccessible systems,
unknown query generators, or signals absent from the chosen snapshot.

## What is KEDA?

KEDA is Kubernetes Event-driven Autoscaling. A KEDA `ScaledObject` can query a
Prometheus metric to decide how many replicas a workload needs. If that metric
is renamed or removed, the control loop can scale incorrectly. TCG's KEDA
adapter extracts supported Prometheus triggers and classifies a confirmed
dependency as `SCALING_RISK`; see [KEDA](KEDA.md).

## What input do I provide?

Provide one proposed change source—an explicit ChangeSet, a baseline/candidate
snapshot pair, or a mapped OpenTelemetry Weaver diff—plus a `tcg.yaml` that
selects evidence sources and policy. Migration compatibility mode accepts a
migration plan. See [Quickstart](QUICKSTART.md) and
[Change sources](CHANGE_SOURCES.md).

## What output should I expect?

Generic checks return `PASS`, `WARN`, `BLOCK`, `INCOMPLETE`, or `ERROR` plus
findings, diagnostics, dependency paths, and a versioned JSON result. Exit
codes are `0`, `0`, `2`, `3`, and `1` respectively. Migration compatibility
uses `READY`, `BLOCKED`, `INCOMPLETE`, and `ERROR`.

## Does TCG execute PromQL or application code?

Local discovery parses supported artifacts and does not execute the queries it
finds. PromQL uses a typed parser. Optional remote adapters make bounded reads
only when configured. The CloudFormation loader does not execute templates.

## Is AI required?

No. Discovery, graph analysis, policy, reports, and exit codes are deterministic
and work without AI. Optional AI can draft inputs, explain evidence, or propose
a bounded repair, but its output is untrusted and cannot change TCG's status.
The agentic repair loop remains an isolated experiment.

## Can TCG automatically read my source-code diff?

Not in the public CLI today. A human or external coding assistant may draft a
ChangeSet from a diff, but TCG validates the accepted structure rather than
claiming the draft found every telemetry change. Native source/diff extraction
is tracked separately.

## Is the project ready for production?

It is a public-alpha candidate, not a stable or production-proven release. The
candidate has broad automated and hosted verification, but independent design-
user evidence and actual artifact publication remain incomplete. Evaluate it
as an additional pull-request control, define required evidence deliberately,
and keep rollback available. Read [Limitations](LIMITATIONS.md).

## How can an external SRE evaluate it safely?

Start with the five-minute fixture, then use a sanitized representative change
inside the evaluator's environment. Pin the exact commit, avoid production
credentials, record expected versus actual behavior before the run, and share
only authorized anonymized evidence. The [Design User Program](DESIGN_USER_PROGRAM.md)
and [`evaluation/`](../evaluation/README.md) provide the protocol.

## How do I report a missed dependency?

Treat a possible false-safe result as high priority. Preserve the exact version,
expected ground truth, sanitized minimal inputs, actual JSON result, and
environment. Use the bug template for public-safe cases or private vulnerability
reporting when security-sensitive. Do not upload proprietary telemetry.
