# Releases and distribution status

## Current state

There is no published TCG release or stable `v1` coordinate. The prepared
candidate is `v0.1.0-alpha.1`; publication requires explicit owner approval for
the exact reviewed main commit and all release gates.

| Channel | Candidate state | Available now? |
| --- | --- | --- |
| Immutable GitHub Action commit | Hosted consumer paths verified | Yes, at the documented full SHA |
| `go install` at full commit | Clean external install verified | Yes, with Go 1.26.7+ |
| GitHub release archives | Reproducible archives, checksums, manifest, notes, SBOMs, and provenance prepared | No |
| OCI image | Linux amd64/arm64 non-root build and attestations verified | No registry publication |
| Homebrew | Formula generation and Ruby syntax verified | No tap publication |
| Stable Action tag | Not created | No |

Use [Installation](INSTALLATION.md) for commands that distinguish current
evaluation paths from post-publication paths.

## Release contents

The candidate release build produces canonical and compatibility executables
for documented platforms, SHA-256 checksums, SPDX and CycloneDX SBOMs, a release
manifest, release notes, and provenance inputs. Snapshot and reproducibility
targets build without publishing.

```bash
make release-snapshot
make release-reproducible
make verify-release
```

Container and formula candidates are separate build-only targets:

```bash
make container-snapshot
make homebrew-formula
```

These commands do not authorize a tag, GitHub release, package, registry push,
tap change, or announcement.

## Owner-approved publication sequence

1. Merge the exact release-readiness changes and require all documented checks
   on the resulting main commit.
2. Complete the manual settings checklist in
   [Repository settings](REPOSITORY_SETTINGS.md).
3. Review remaining risks, release notes, supported platforms, external fixture
   state, and rollback commands.
4. Explicitly approve `v0.1.0-alpha.1` for the exact commit.
5. Create the annotated tag through the documented maintainer procedure.
6. Let the gated workflow build, verify, and create the GitHub prerelease.
7. Verify downloaded assets independently before any optional container or
   Homebrew publication.
8. Publish announcements only after install commands work from a clean external
   environment.

The exact commands and abort conditions are in [Releasing](RELEASING.md).

## Rollback

If verification or smoke tests fail, stop distribution, mark the prerelease as
affected, remove or deprecate optional package coordinates according to their
registry policy, and direct users to the last known-good immutable commit or
version. Do not move an existing immutable tag to different code. Prepare a new
patch prerelease after the defect is fixed and all gates rerun.

Security incidents follow [Security](../SECURITY.md), including private
coordination before public detail when appropriate.
