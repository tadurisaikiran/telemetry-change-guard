# Public-alpha release procedure

This runbook prepares `v0.1.0-alpha.1`. It does not authorize publication.
Only a repository owner may approve the exact candidate and create the tag.

## Locked release inputs

`release/metadata.env` is the single machine-readable source for the candidate
version, verified Action coordinate, GoReleaser version, Syft version, and the
SHA-256 digests of both tools' upstream checksum manifests. Workflows install
the same versions through immutable Action commits. Local builds download the
official archives over HTTPS and verify the pinned manifest before extraction.

The release system produces:

- Linux, macOS, and Windows archives for amd64 and arm64;
- both `telemetry-change-guard` and `tmr` in every platform archive;
- an exact-commit source archive;
- SPDX and CycloneDX SBOMs for every platform and source archive;
- a deterministic release manifest and sorted `SHA256SUMS`;
- GitHub-hosted build provenance for every public payload file.

## Candidate rehearsal

From a clean commit with Go `1.27.0`:

```sh
make release-snapshot
```

The target installs locked tools into `.cache/release-tools` when necessary,
builds without publishing, stages only the public payload under
`dist/release`, and runs the deep verifier. `Release Snapshot` repeats this on
relevant pull requests with read-only repository permissions. The workflow
uses `make release-reproducible` to build twice and require an identical
checksum set before retaining the verified payload for seven days.

Do not upload a rehearsal as a release or call it published.

## Repository settings required before authorization

- Protect `main` with current CI, CodeQL, dependency-review, workflow-security,
  pinned E2E, and release-snapshot checks as applicable.
- Block force pushes and require pull requests and resolved conversations.
- Configure the `public-alpha` environment with an owner as a required
  reviewer. Do not place long-lived publishing credentials in that
  environment; GitHub OIDC supplies provenance identity.
- Restrict tag creation for `v*` release tags to repository owners.

These GitHub settings cannot be made enforceable by repository files alone and
must be confirmed manually.

## Owner-authorized publication

After every candidate blocker is closed and the owner approves the exact
commit:

1. Confirm `release/metadata.env`, `CHANGELOG.md`, and
   `release/RELEASE_NOTES.md` agree on `0.1.0-alpha.1`.
2. Confirm the approved commit is contained in protected `origin/main` and all
   required checks are green.
3. Create and push an annotated tag named exactly `v0.1.0-alpha.1`. A signed
   annotated tag is preferred.
4. Approve the `public-alpha` environment only after the workflow's validation
   job passes.
5. Verify the created prerelease and several downloaded assets using
   `docs/VERIFY_RELEASE.md` before any announcement.

The dormant `Public Alpha Release` workflow revalidates the annotated tag and
protected-main ancestry, reruns the full verifier and pinned E2E lifecycles,
builds without GoReleaser publication, verifies the payload, records
provenance, and only then creates a GitHub prerelease. It has no schedule,
manual-dispatch entry, branch trigger, package push, container push, or
announcement step.

## Recovery

If validation fails, fix the source through a new pull request and choose a new
prerelease coordinate. Do not move a published release tag. If publication
partially succeeds, stop announcements, preserve workflow evidence, document
which assets were exposed, and follow GitHub's release-removal procedure only
with explicit owner approval.
