# Governance

Telemetry Change Guard is an open-source project in a single-maintainer phase.
This document makes that current reality explicit and defines how the project
can grow without manufacturing consensus or community participation.

## Principles

- Safety decisions remain deterministic, reproducible, and fail closed.
- Technical decisions, limitations, and disagreements are recorded publicly.
- Authority follows sustained responsibility, not affiliation or title.
- Adoption, testimonials, and contributor identities are never fabricated.
- Security and private conduct reports remain confidential.

## Roles

**Contributors** open issues, propose changes, review pull requests, improve
documentation, or help other users. Anyone participating under the Code of
Conduct can be a contributor.

**Maintainers** have review and merge authority for a documented scope and are
accountable for project health. The current maintainers are listed in
[`MAINTAINERS.md`](MAINTAINERS.md).

The **lead maintainer** coordinates releases, resolves decisions that cannot
reach consensus, and ensures the rationale is recorded. The lead role does not
permit bypassing required checks or the project's safety contracts.

## Change process

Normal work follows:

```text
issue or documented problem → focused branch → pull request → checks → review → merge
```

Small, self-evident documentation corrections may begin directly with a pull
request. Contributors should use issues or discussions before investing in a
new public contract, adapter, dependency, or architectural direction.

A safety-affecting change must include tests and evidence. This includes
changes to status or exit-code contracts, parsing and discovery, dependency
graphs, policy, incomplete-evidence behavior, machine schemas, and release
enforcement.

## Decisions

Maintainers seek consensus using the following evidence, in order:

1. documented safety and compatibility contracts;
2. reproducible tests and real user evidence;
3. accepted architecture decision records;
4. operational simplicity and long-term maintenance cost; and
5. maintainer judgment.

Material architecture decisions use an ADR or a design issue. If consensus is
not possible, the lead maintainer makes the narrowest reversible decision and
records the alternatives, objections, and rationale. During the current
single-maintainer phase, the lead maintainer has final merge authority.

Once additional maintainers are active, they decide by consensus. If they
remain split, the lead maintainer resolves the immediate decision in writing;
a maintainer with a direct conflict of interest does not cast the deciding
vote.

## Releases and security

Releases require the documented automated checks and immutable release
artifacts. Stable compatibility is not claimed before a stable release exists.
The lead maintainer coordinates release publication; another maintainer may do
so when explicitly delegated.

Security reports follow [`SECURITY.md`](SECURITY.md). A maintainer may apply a
private, time-sensitive fix before public discussion, then publish an advisory
and technical record when disclosure is safe.

## Governance changes

Changes to this document use a pull request and explain why the current model
no longer fits. Governance should evolve in response to actual contributors,
maintainers, users, and project risk—not anticipated appearances.
