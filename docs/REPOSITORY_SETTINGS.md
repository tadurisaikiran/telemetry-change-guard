# Repository settings checklist

Code cannot prove GitHub administrative settings. An owner must verify these
controls in the repository UI or organization policy before public-alpha
publication and retain dated evidence of the review.

## Main branch ruleset

Recommended rules for `main`:

- require a pull request before merging;
- require at least one approving review from a qualified maintainer;
- dismiss stale approvals when protected files change;
- require conversation resolution;
- require branches to be current before merge;
- require the current CI, CodeQL, dependency review, workflow-security,
  distribution, and pinned E2E checks selected by maintainers;
- block force pushes and branch deletion;
- restrict bypass to named emergency maintainers; and
- document the emergency bypass and require a follow-up review.

Check names can change as workflows evolve. Select the checks from a successful
current main run rather than copying stale names from prose.

## Security features

Enable where the GitHub plan and repository visibility support them:

- dependency graph and Dependabot alerts;
- Dependabot security updates;
- secret scanning and push protection;
- CodeQL default or advanced setup without duplicate/conflicting analysis;
- private vulnerability reporting; and
- security policy discovery through [`SECURITY.md`](../SECURITY.md).

Confirm that Actions from forks cannot access repository secrets and that
workflow permissions default to read-only.

## Actions and environments

- Restrict allowed Actions to the reviewed policy while preserving every
  immutable dependency used by TCG.
- Keep default `GITHUB_TOKEN` permissions read-only; grant job-level writes
  only where documented.
- Create a protected release environment with limited approvers for any
  publication job.
- Store publication credentials only in that environment, never in repository
  files or ordinary pull-request jobs.
- Require manual approval for package/image/tap publication if those channels
  are enabled later.

## Release and tag policy

- Decide how maintainers create and verify annotated release tags.
- Prevent unreviewed workflows or broad tokens from creating release tags.
- Enable immutable releases when available and compatible with the release
  process.
- Keep GitHub Release publication separate from optional GHCR and Homebrew
  publication so each channel can be independently stopped.
- Never create or move a stable `v1` tag during the alpha.

## External distribution dependencies

Before advertising those paths, an owner must separately approve and verify:

- a truly external consumer repository created from
  [`release-fixtures/external-consumer-repository`](../release-fixtures/external-consumer-repository);
- GHCR package visibility, retention, immutable digest instructions, and
  attestations;
- a `homebrew-tap` repository, ownership, branch protection, and formula test;
  and
- public documentation/announcement links from a clean logged-out session.

## Review record

Record the review date, reviewer, ruleset URL or exported configuration,
required check names, enabled security features, release-environment approvers,
tag policy, approved distribution channels, and any accepted exception. Do not
put secrets, recovery codes, private security findings, or internal-only URLs
in a public issue.

Repository settings are a release blocker when their state is unknown. The
implementation may be otherwise ready while publication remains unauthorized.
