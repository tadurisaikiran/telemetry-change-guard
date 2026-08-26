# Public-alpha release procedure

This runbook prepares `v0.1.0-alpha.1`. It does not authorize publication.
Only a repository owner may approve the exact candidate and create the tag.

## Locked release inputs

`release/metadata.env` is the single machine-readable source for the candidate
version, verified Action coordinate, GoReleaser version, Syft version, and the
SHA-256 digests of both tools' upstream checksum manifests. It also locks the
container name and immutable Go-builder and distroless runtime manifests. Workflows install
the same versions through immutable Action commits. Local builds download the
official archives over HTTPS and verify the pinned manifest before extraction.

The release system produces:

- Linux, macOS, and Windows archives for amd64 and arm64;
- both `telemetry-change-guard` and `tmr` in every platform archive;
- an exact-commit source archive;
- SPDX and CycloneDX SBOMs for every platform and source archive;
- a deterministic release manifest and sorted `SHA256SUMS`;
- GitHub-hosted build provenance for every public payload file.

Separate build-only gates also produce a multi-architecture OCI layout with
per-platform SBOM/provenance attestations and a checksum-bound Homebrew formula.
Neither output is published by an active workflow.

## Candidate rehearsal

From a clean commit with the pinned release toolchain, Go `1.26.7`:

```sh
make release-snapshot
make homebrew-formula
make container-snapshot
```

The target installs locked tools into `.cache/release-tools` when necessary,
builds without publishing, stages only the public payload under
`dist/release`, and runs the deep verifier. `Release Snapshot` repeats this on
relevant pull requests with read-only repository permissions. The workflow
uses `make release-reproducible` to build twice and require an identical
checksum set before retaining the verified payload for seven days.

The container target requires a local Docker daemon and Buildx. Do not upload a
rehearsal as a release or call it published.

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
4. Approve the `public-alpha` environment only after validation, reproducible
   candidate construction, and the Linux, macOS, and Windows native archive
   smoke jobs pass.
5. Verify the created prerelease and several downloaded assets using
   `docs/VERIFY_RELEASE.md` before any announcement.
6. If separately authorized, publish the already verified OCI image by exact
   tag and record its immutable digest. Registry authentication and package
   write permission are deliberately absent until that authorization exists.
7. If separately authorized, copy the generated formula to the owner-created
   `homebrew-tap` repository and test it against the published archives.
8. If separately authorized, create the external Action fixture repository
   from `release-fixtures/external-consumer-repository` and require its complete
   workflow to pass before claiming external verification.

The dormant `Public Alpha Release` workflow revalidates the annotated tag and
protected-main ancestry, reruns the full verifier and pinned E2E lifecycles,
and builds a reproducible candidate in a read-only job. Hosted Linux, macOS,
and Windows jobs download that exact artifact and execute its native archive.
Only after all three pass can the protected `public-alpha` job download and
reverify the same bytes, record provenance, and create a GitHub prerelease.
The privileged job never rebuilds the payload. The workflow has no schedule,
manual-dispatch entry, branch trigger, package push, container push, or
announcement step.

## Recovery

If validation fails, fix the source through a new pull request and choose a new
prerelease coordinate. Do not move a published release tag. If publication
partially succeeds, stop announcements, preserve workflow evidence, document
which assets were exposed, and follow GitHub's release-removal procedure only
with explicit owner approval.
