# Social copy drafts

> **Do not post yet.** Owner approval, published artifact verification, final
> links, and a clean public review are required. Remove placeholders and choose
> only copy that matches the actual publication state.

## Short announcement — candidate evaluation

Telemetry is an API contract, but normal CI often checks only the producer.

Telemetry Change Guard checks a proposed metric, label, or attribute change
against configured alerts, dashboards, SLOs, autoscalers, rollout gates, and
other supported evidence—then returns deterministic paths, impact, policy
status, and a versioned machine result.

The alpha candidate is open for evidence-driven evaluation at an immutable
commit. Start with one synthetic or sanitized change: [QUICKSTART LINK]

What we want to learn: would you make this a required PR check, and what would
stop you?

## Short announcement — after verified prerelease publication

We published Telemetry Change Guard `v0.1.0-alpha.1`.

TCG treats telemetry like an API contract and checks which configured consumers
will break before the contract changes. It covers supported alert, dashboard,
SLO, KEDA, Argo Rollouts, mapped HPA, migration, and runtime-evidence paths with
deterministic `PASS`, `WARN`, `BLOCK`, `INCOMPLETE`, or `ERROR` results.

Try the five-minute blocked-alert example: [RELEASED QUICKSTART LINK]

This is an alpha. Read the evidence scope and limitations before enforcement:
[LIMITATIONS LINK]

## LinkedIn-style draft

A one-line metric rename can pass application CI and still blind an alert,
invalidate an SLO, or change a production control loop.

The problem is structural: the producer and its operational consumers often
live in different artifacts, repositories, or systems.

Telemetry Change Guard treats telemetry as an API contract. Give it an explicit
proposed change and configured consumer evidence; it returns affected consumers,
dependency paths, operational impact, policy reason, a versioned JSON report,
and one deterministic status.

The candidate includes direct and transitive Prometheus analysis plus supported
Grafana, SLO, KEDA, Argo Rollouts, mapped HPA, runtime, Weaver, Perses, and
Tempo paths. Missing required evidence fails incomplete instead of appearing
safe.

AI can help draft an input, explain evidence, or propose a repair—but cannot
override the engine. AI proposes; humans approve; TCG verifies and decides.

We are looking for SRE, platform, observability, and application engineers to
try one sanitized representative change and answer a hard question: would you
make this a required pull-request check? Why or why not?

[PROJECT LINK] · [QUICKSTART LINK] · [LIMITATIONS LINK]

## Hacker News / technical forum draft

Title: Telemetry Change Guard – deterministic CI checks for telemetry contract changes

We built TCG to catch a gap we kept seeing in telemetry migrations: producer
code can compile and deploy while checked-in alerts, dashboards, SLOs, scaling
rules, or rollout gates still consume the old contract.

TCG is a local-first Go CLI and composite GitHub Action. It accepts an explicit
ChangeSet, snapshot diff, mapped Weaver diff, or migration plan; extracts typed
references from configured evidence; follows direct/transitive paths; and emits
a versioned result plus exact status/exit contract.

The safety boundary is intentional: configured evidence only, strict parsing,
explicit mappings across domains, and `INCOMPLETE` when required evidence cannot
be established. Optional AI can explain or draft a candidate repair but has no
status or repository authority.

The repository has an 11-case synthetic regression corpus and hosted lifecycle,
Action-consumer, security, race/fuzz, release, and container gates. We are not
presenting that as independent field accuracy; we want design-user cases with
pre-recorded expected results.

Code: [PROJECT LINK]
Quickstart: [QUICKSTART LINK]
Limitations: [LIMITATIONS LINK]

Feedback on false-safe cases, over-reporting, uncertainty, and configuration
friction is especially useful.

## Single-post microcopy

One-line metric rename. Green application CI. Blind production alert.

TCG checks proposed telemetry contract changes against configured operational
consumers and returns deterministic dependency paths + policy status before
merge.

Alpha evaluation: [LINK]

## Thread outline

1. A metric/label/attribute is an operational API.
2. Producer tests rarely load every alert, dashboard, SLO, scaler, and rollout
   gate.
3. TCG accepts an explicit change and configured evidence.
4. It extracts typed references, follows transitive paths, classifies impact,
   and applies policy.
5. `BLOCK` is known enforced risk; `INCOMPLETE` is missing/unresolved required
   evidence. They are distinct exits.
6. The core is local-first and deterministic. Optional AI cannot decide status.
7. The alpha evidence is strong regression engineering, not a claim about
   unconfigured systems.
8. Try one sanitized case and tell us whether it belongs as a required PR check.

## Publication checklist

- [ ] Exact release/tag/commit and every install coordinate are live and tested.
- [ ] Links resolve from a logged-out session.
- [ ] Claims match [Limitations](../LIMITATIONS.md) and current test evidence.
- [ ] No synthetic result is described as independent validation.
- [ ] No organization, person, quote, logo, or artifact lacks authorization.
- [ ] No credential, private endpoint, internal issue, or embargoed detail is
      present.
- [ ] Owner approved the exact copy and channel.
- [ ] A maintainer is available to triage correctness and security reports.
