# Public launch kit

> **Draft only — owner approval required before publication.** These files do
> not authorize a release, tag, package, image, tap, external repository,
> adopter statement, social post, article, or announcement.

This kit provides one evidence-constrained narrative in formats suited to
different audiences:

| Artifact | Primary use |
| --- | --- |
| [Product brief](PRODUCT_BRIEF.md) | One-page internal/external overview |
| [Launch article](LAUNCH_BLOG.md) | Long-form announcement after publication |
| [Technical deep dive](TECHNICAL_DEEP_DIVE.md) | Architecture and safety review |
| [Tutorial](TUTORIAL.md) | Guided hands-on evaluation |
| [Demo script](DEMO_SCRIPT.md) | Repeatable live or recorded demonstration |
| [Launch FAQ](FAQ.md) | Reviewer and early-user objections |
| [Social copy](SOCIAL_COPY.md) | Owner-reviewed post drafts |
| [Case-study template](CASE_STUDY_TEMPLATE.md) | Authorized design-user story |

## Claim source of truth

Before publishing, reconcile every version, commit, install command, status,
test count, and availability statement with:

- [Releases and distribution status](../RELEASES.md);
- [Public alpha limitations](../LIMITATIONS.md);
- [Installation](../INSTALLATION.md);
- [Release-gate benchmark](../../benchmarks/README.md);
- [Adoption evidence rules](../../evaluation/ADOPTION_EVIDENCE.md); and
- the successful checks on the exact release commit.

The stable product sentence is:

> TCG treats telemetry like an API contract and checks which configured
> consumers will break before the contract changes.

Use “configured evidence” or “configured consumers.” Never imply that TCG
crawls every repository or proves the state of inaccessible systems. Do not
convert the synthetic regression corpus into independent accuracy statistics.

## Publication gate

Replace every bracketed placeholder, verify all commands from a clean external
environment, obtain owner approval for the exact release commit and channels,
and confirm that any named organization, logo, quote, repository, or result has
separate written authorization. If the alpha is not published, keep these as
repository drafts and direct evaluators to the immutable commit path.
