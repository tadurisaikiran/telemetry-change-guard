# Launch FAQ

> **Draft — reconcile availability answers with the release state immediately
> before publication.**

## What is Telemetry Change Guard?

TCG is a local-first CLI and GitHub Action that treats telemetry as an API
contract. It checks proposed telemetry changes against configured consumer
evidence, follows dependency paths, classifies operational impact, and returns
deterministic policy evidence before merge or deployment.

## What makes this different from linting instrumentation code?

Instrumentation linting checks the producer. TCG relates the proposed contract
change to operational consumers such as alerts, dashboards, SLOs, scaling
rules, rollout gates, and supported runtime evidence. The relation can be
transitive through recording rules.

## What does a user provide?

One proposed change source plus `tcg.yaml`. The change source can be a
ChangeSet, baseline/candidate snapshots, a mapped Weaver diff, or a migration
plan in compatibility mode. The configuration selects evidence sources,
required/optional behavior, analysis settings, and policy.

## What does a user get?

A console or Markdown report for humans, a versioned JSON result for automation,
dependency paths and diagnostics, and an authoritative status/exit code.

## Does it cover metrics only?

No. The model includes documented metric, label, span-attribute, and resource-
attribute changes, subject to available mappings and consumer adapters. The
largest current consumer surface is Prometheus-centered; consult the adapter
and limitation guides for exact coverage.

## Does it work with KEDA and Kubernetes HPA?

Yes, within explicit alpha contracts. TCG parses supported Prometheus KEDA
triggers. HPA dependencies require checked-in mappings from Kubernetes metric
identity to the Prometheus source identity. Confirmed dependencies are scaling
risk.

## Does it query production?

Not by default. Local file evidence needs no network. Snapshot capture and
optional remote evidence use explicitly configured endpoints with bounded
clients and trusted-origin controls. Teams should evaluate with read-only,
least-privilege credentials and sanitized data.

## Can a missing source produce `PASS`?

A source marked required cannot silently disappear into a clean result.
Missing, malformed, denied, dynamic, or unresolved required evidence produces
`INCOMPLETE` or `ERROR` according to the contract and retains confirmed
findings.

## Is AI built in?

The deterministic product works without AI. Optional protocols support
read-only explanation and candidate remediation through a user-selected
provider process. An experimental isolated repair loop can draft an uncommitted
diff and rerun TCG. AI cannot create, suppress, weaken, or override status.

## Does AI automatically find every telemetry change in code?

No. An external coding assistant can draft a candidate ChangeSet or config,
and TCG strictly validates accepted structure. Native source/diff extraction is
not a shipped public capability, and no assistant can prove it observed every
change without separately defined evidence.

## Is the agentic experiment a replacement for TCG?

No. It calls the public deterministic CLI as its verifier. The existing CLI,
Action, schemas, and evidence model remain the product; the optional layer is
additive and experimental.

## What is published today?

At the time of this draft, no release tag, archive, container image, Homebrew
tap, or stable Action tag is published. The fully verified commit can be
installed with Go or pinned as an Action. Update this answer only after every
published coordinate is independently tested.

## What evidence supports the alpha candidate?

Hosted and local gates cover race tests, fuzz smoke tests, vulnerability and
workflow analysis, CodeQL, dependency review, external Action consumption,
reproducible release artifacts, container properties, and pinned live
lifecycles. The 11-case synthetic corpus protects exact regression contracts.

That corpus is not independent usage or field accuracy evidence. Design-user
evaluation is a separate workstream.

## Does `PASS` guarantee a safe deployment?

No. It means no blocking impact was found within configured evidence and
policy. Teams still need review, canaries, monitoring, rollback, and accurate
source inventory.

## Who should evaluate it first?

SRE, platform, observability, and application teams that own both telemetry
producers and at least one operational consumer type. A useful trial has a
known expected result, a sanitized representative change, and a clear answer
to which sources are required.

## How should a company name be referenced?

Only with authorization from an appropriate representative and only for the
exact approved claim. Employee interest, a private evaluation, a star, a fork,
or an issue does not establish organization adoption.

## Where should feedback go?

Use the design-user issue template for public-safe feedback, the bug template
for a sanitized correctness reproduction, and private vulnerability reporting
for security-sensitive issues. Never submit credentials or proprietary
telemetry to a public issue.
