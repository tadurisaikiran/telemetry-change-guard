# The one-line telemetry change your tests cannot see

> **Draft launch article — publish only after owner-approved alpha artifacts
> exist and every command below succeeds from a clean external environment.**

Renaming a metric often looks harmless in code review:

```diff
- checkout_requests_total
+ checkout_server_requests_total
```

The producer compiles. Its tests pass. The deployment is healthy.

But the metric is not just an implementation detail. A paging alert may still
query the old name. An SLO may calculate against it. A dashboard may lose its
most important panel. KEDA or an HPA mapping may use it to scale production. An
Argo Rollouts analysis can use it to decide whether a release continues.

The code change is local; the operational contract is distributed.

## Telemetry is an API, even when it has no compiler

We have mature checks for source APIs, schemas, and dependency manifests. A
telemetry contract crosses a different boundary. Producers emit metrics,
labels, span attributes, and resource attributes. Consumers live in query and
control-plane artifacts that can be owned by other teams or stored in other
systems.

Normal CI sees the producer. It often cannot answer: “Which operational
consumers in the evidence we require still depend on this signal?”

That is the question Telemetry Change Guard is built to answer.

**TCG treats telemetry like an API contract and checks which configured
consumers will break before the contract changes.**

## A review-time safety contract

TCG starts with an explicit proposed change: a ChangeSet, a baseline/candidate
Prometheus snapshot pair, a mapped OpenTelemetry Weaver diff, or a migration
plan. A configuration tells it which consumer evidence to inspect and which
policy to apply.

From there, the engine is deterministic:

```text
proposed change
    + configured consumer evidence
    -> typed references and productions
    -> direct and transitive dependency paths
    -> operational impact and criticality
    -> explicit policy
    -> status + findings + exit code + versioned JSON
```

The result is not a paragraph saying “this looks risky.” It identifies the
affected consumer, its source, the dependency path, the impact category, and
the policy reason.

## The first five-minute check

The repository includes a small example. It proposes removing
`checkout_requests_total`; a critical alert named `CheckoutTrafficMissing`
still queries the metric.

Install the exact reviewed candidate commit with Go 1.26.7 or newer:

```bash
go install github.com/tadurisaikiran/telemetry-change-guard/cmd/telemetry-change-guard@90c0b41629b76f67f6bfda42ad88eb93bd275328
```

Then run the matching example from the repository root:

```bash
telemetry-change-guard check \
  --config ./examples/getting-started/tcg.yaml \
  --changes ./examples/getting-started/changes.yaml \
  --mode enforce
```

The check returns:

```text
Status:    BLOCK
Findings:  1

[BLOCK] ALERTING_RISK — CheckoutTrafficMissing
  Source:      examples/getting-started/prometheus/rules.yaml:4

STATUS: BLOCK
```

The process exits `2`: analysis succeeded and policy rejected the proposed
change. A required source that cannot be loaded or resolved returns
`INCOMPLETE` with exit `3`, so missing evidence is not silently interpreted as
safety.

## More than dashboards and alerts

TCG classifies the operational role of a dependency. A dashboard can be
`VISIBILITY_LOSS`; an alert can be `ALERTING_RISK`; an SLO can be `SLO_RISK`;
a KEDA or mapped HPA dependency can be `SCALING_RISK`; and an Argo Rollouts
analysis can be `DEPLOYMENT_GATE_RISK`.

Prometheus recording rules also make the graph transitive. A base metric can
feed a recording rule that feeds a paging alert. TCG follows the configured
path rather than stopping at the first reference.

## Useful without a platform migration

The core is a local CLI. It needs no database and has no default network
dependency. Teams can begin with checked-in files and an explicit ChangeSet,
use `audit` or `warn` during evaluation, and promote the same result contract
to `enforce` when evidence and policy are ready.

The GitHub Action runs one analysis, preserves versioned JSON and a Markdown
summary, and enforces the CLI's exact status/exit relation. Remote adapters are
off by default and require explicit trusted-origin configuration.

## AI can accelerate the work without deciding safety

An AI assistant can read a source diff and draft a candidate ChangeSet. It can
summarize deterministic findings, propose a query migration, or draft a repair
for review. The repository also contains an isolated experimental repair loop
that rechecks an edited workspace and produces an uncommitted diff.

The authority boundary stays simple:

> AI proposes. Humans approve. TCG verifies and decides.

AI output never turns `BLOCK` into `PASS`, never treats absence as proof, and
never commits, pushes, approves, or merges through the experimental controller.

## What this alpha candidate establishes—and what it does not

The candidate is exercised by race tests, fuzz smoke tests, vulnerability and
workflow security checks, external Action-consumer fixtures, reproducible
release builds, multi-platform container verification, and pinned live
lifecycles. An 11-case synthetic corpus protects exact result contracts for
direct, transitive, control-plane, incomplete, migration, and negative cases.

Those are engineering regression controls. They are not independent adoption
evidence or a field-wide accuracy study. TCG checks configured evidence; it
does not crawl inaccessible repositories or systems. A pass must always be
read with that scope.

At the time of this draft, release archives, a registry image, Homebrew tap,
and stable Action tag are not published. The immutable commit path is the
supported evaluation route until the owner approves the release.

## Help test the question that matters

The design-user question is not “Do you like the idea?” It is:

> Would you make this a required pull-request check? Why or why not?

Try one sanitized representative change. Record the consumers and status you
expect before running TCG. Then tell us what it missed, over-reported, made
unclear, or made too difficult to configure.

Start with the [quickstart](../QUICKSTART.md), review the
[limitations](../LIMITATIONS.md), and use the
[evaluation kit](../../evaluation/README.md) to share evidence safely.
