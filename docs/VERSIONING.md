# Versioning and build identity

TCG uses Semantic Versioning for product releases and independently versioned
machine schemas. The first intended public candidate is
`v0.1.0-alpha.1`; it is not published until every release gate passes and the
owner explicitly authorizes the tag and prerelease.

## Product versions

- Git tags use `vMAJOR.MINOR.PATCH[-PRERELEASE]`.
- CLI version output omits the tag's leading `v`, for example
  `0.1.0-alpha.1`.
- `alpha` means the supported surface is usable for controlled evaluation but
  may change between prereleases. It is not a stability or production-proof
  claim.
- `beta` will require broader compatibility evidence and independent
  evaluations. Stable `v1` requires an intentionally frozen public contract;
  no `v1` tag exists today.
- The latest prerelease is the only prerelease line expected to receive fixes
  unless a security notice says otherwise. There is no prerelease support SLA.

The candidate values used by documentation and release checks live in
[`release/metadata.env`](../release/metadata.env). That file is preparation,
not proof that a tag or release exists.

## Build identity

Every executable supports:

```bash
telemetry-change-guard version
telemetry-change-guard version --format json
telemetry-change-guard --version
```

The human output includes product version, exact commit, build date,
clean/dirty state, Go version, and platform. JSON uses the stable
`tcg-version/v1alpha1` shape. Its `dirty` field is a boolean when the build
system knows the answer and `null` when it does not. Development builds report
`dev`, `unknown` commit/date/dirty values, and the actual runtime and platform;
they never impersonate a release.

A binary built by `go install module/cmd@version` is a special source build:
when no linker version was supplied, TCG reads Go's immutable main-module
version and reports it without the leading `v`. Go does not expose the release
pipeline's commit or build date for that installation path, so those fields
remain `unknown`. Official archives and images retain full injected identity.

Release tooling injects the following variables with `go build -ldflags -X`:

```text
github.com/tadurisaikiran/telemetry-change-guard/internal/version.Version
github.com/tadurisaikiran/telemetry-change-guard/internal/version.Commit
github.com/tadurisaikiran/telemetry-change-guard/internal/version.Date
github.com/tadurisaikiran/telemetry-change-guard/internal/version.Dirty
```

Release-shaped builds use `-trimpath` and disable automatic VCS stamping in
favor of these explicitly verified values. The release snapshot must prove the
embedded version and commit match its candidate metadata.

## Machine-schema versions

Product, configuration, and result versions advance independently:

| Contract | Current schema | Policy |
| --- | --- | --- |
| Generic ChangeSet/configuration | `tcg/v1alpha1` | Unknown fields fail strict parsing; breaking changes require a new schema |
| Generic analysis result | `tcg-result/v1alpha1` | Status/exit semantics remain stable; breaking structural changes require a new schema |
| Migration compatibility result | `tmr-result/v1alpha1` | Preserved while the temporary compatibility surface is supported |
| Version output | `tcg-version/v1alpha1` | Additive compatible fields may be introduced; breaking changes require a new schema |

Build identity is deliberately not inserted into existing analysis documents
for this alpha candidate. Keeping it adjacent through `version`, manifests,
checksums, and provenance avoids turning runtime build data into a silent
change to either analysis schema.

## Deprecation policy

A supported contract is not removed in a patch or prerelease rebuild. A
deprecation must be documented in the changelog and upgrade guide, retain a
tested compatibility path for the announced window, and identify the first
version allowed to remove it. Published tags and artifacts are never moved or
silently replaced.
