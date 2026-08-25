# Case study template: a telemetry contract change caught before merge

> **Template only.** Do not publish a participant, organization, logo, quote,
> repository, artifact, or result without the separate authorization described
> in [`evaluation/templates/case-study-consent.md`](../../evaluation/templates/case-study-consent.md).
> This template is not legal advice.

## Publication record

- Approved organization display name: `[authorized name]`
- Authorized representative and role: `[private record reference]`
- Exact approved claims/quotation: `[authorization reference]`
- TCG version and commit: `[version]` / `[40-character SHA]`
- Evaluation record: `[authorized public or private reference]`
- Technical reviewer: `[reviewer]`
- Organization proof approval date: `[date]`

## Title

How `[organization/team]` checked `[sanitized telemetry change]` against
`[configured consumer scope]` before merge

## Summary

In `[environment/workflow]`, `[team]` evaluated Telemetry Change Guard against
a pre-recorded expectation for `[change type]`. TCG inspected `[explicit source
inventory]` and returned `[status]`, identifying `[authorized finding summary]`.

State whether TCG became an audit check, warning check, required check, or was
not adopted. Do not imply broader use.

## Context

Describe only authorized facts:

- what the telemetry contract represented;
- why it was changing;
- which teams or systems owned the producer and consumers;
- which evidence was configured and marked required; and
- which relevant evidence was known to be excluded.

Avoid customer data, internal topology, sensitive thresholds, incident detail,
and exact proprietary queries unless specifically approved.

## Pre-recorded expectation

Before running TCG, the evaluator expected:

| Field | Expected |
| --- | --- |
| Status | `[PASS/WARN/BLOCK/READY/BLOCKED/INCOMPLETE/ERROR]` |
| Affected consumers | `[sanitized list]` |
| Dependency paths | `[sanitized paths]` |
| Diagnostics/uncertainty | `[expected gaps]` |

Explain how ground truth was established and who reviewed it independently of
TCG output.

## Setup

Record reproducible scope:

```text
TCG version/commit:
Change source:
Configured local sources:
Configured remote sources:
Required evidence:
Known excluded evidence:
Policy mode and thresholds:
OS/architecture:
Time to first useful result:
```

Link only to newly constructed synthetic or explicitly authorized sanitized
artifacts.

## Result

Report actual status, exit code, affected consumers, dependency paths, impacts,
and permanent diagnostic codes. Include a short machine-result excerpt only
after reviewing it for sensitive input values.

State expected-versus-actual comparison explicitly:

- status matched: `[yes/no]`;
- consumer set matched: `[yes/no]`;
- paths matched: `[yes/no]`; and
- mismatches: `[complete list]`.

Do not remove a mismatch because the narrative is more attractive without it.

## Decision and workflow change

Describe the actual outcome:

- Was the telemetry change revised, staged, or stopped?
- Was a consumer migrated?
- Did the team add or correct required evidence?
- Did TCG stay in audit/warn, move to enforce, or get removed?
- What rollback or monitoring remained in place?

Avoid counterfactual incident-cost claims unless the organization has an
approved method and evidence for them.

## What worked

Use evidence such as time to first result, exact dependency path usefulness,
review clarity, machine integration, or a confirmed expected finding. Separate
participant quotation from maintainer interpretation.

## What did not work

Include missed/extra consumers, unclear diagnostics, setup friction, unsupported
sources, performance observations on the recorded host, or policy-tuning costs.
Link defects to public issues only when the reproduction is safe to share.

## Authorized quotation

> `[exact approved quotation—do not paraphrase beyond authorization]`

Attribute only as approved.

## Scope and limitations

End with the sources TCG did and did not evaluate, alpha maturity, and the fact
that the result did not replace canaries, monitoring, rollback, or human review.

## Reproducibility appendix

- exact command with sensitive values removed;
- checksums or commit of authorized synthetic fixtures;
- result schema version;
- evaluation date;
- anonymization review date; and
- claim/authorization review date.
