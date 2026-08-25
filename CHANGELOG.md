# Changelog

All notable changes to Telemetry Change Guard (TCG) are recorded here. The
project follows the prerelease policy in [docs/VERSIONING.md](docs/VERSIONING.md).

## [0.1.0-alpha.1] - Unreleased

This is a release candidate, not a published release. Its contents and
platform matrix remain subject to the required release gates and explicit
owner approval.

### Added

- deterministic telemetry ChangeSet, snapshot-diff, mapped Weaver, and legacy
  migration analysis;
- direct and transitive dependency discovery across configured observability
  and control-plane consumers;
- `PASS`, `WARN`, `BLOCK`, `INCOMPLETE`, and `ERROR` safety results with the
  permanent `0/1/2/3` process contract;
- fail-closed local and allowlisted remote evidence loading;
- composite GitHub Action with Markdown summary, versioned JSON evidence,
  optional bounded pull-request comments, and exact exit enforcement;
- `telemetry-change-guard version`, `version --format json`, and `--version`
  build-identity commands shared with the compatibility executable;
- byte-reproducible Linux, macOS, and Windows archives for amd64 and arm64,
  each containing the canonical and compatibility executables;
- exact-commit source archive, sorted SHA-256 set, strict release manifest,
  and per-archive SPDX and CycloneDX SBOMs;
- read-only pull-request snapshot builds plus an inert, environment-gated tag
  workflow prepared for GitHub provenance and prerelease publication;
- a build-only, digest-pinned distroless OCI image for Linux amd64/arm64 with
  non-root/read-only smoke tests, SPDX SBOM attestations, and SLSA provenance;
- a checksum-bound generated Homebrew formula and clean-environment
  `go install` verification, both still blocked on publication of their
  external coordinates;
- a complete external-consumer repository fixture plus hosted remote-coordinate
  checks for `PASS`, `BLOCK`, `INCOMPLETE`, snapshot, and migration contracts;
- CodeQL, dependency review, workflow policy, actionlint, zizmor, race,
  fuzz-smoke, vulnerability, and pinned lifecycle validation.

### Security

- remote evidence is disabled by default in the Action;
- authenticated requests require HTTPS except explicitly enabled loopback
  development, and credentials can reach only trusted exact origins;
- overlapping evidence patterns merge required semantics with logical OR;
- every third-party Action dependency is pinned to an immutable commit.

### Compatibility

- the temporary `tmr` executable and `tmr-result/v1alpha1` contract remain
  available for existing migration automation;
- `tcg-result/v1alpha1` analysis documents do not gain dynamic build fields;
  build identity is exposed through the adjacent version command and release
  provenance instead.

### Known limitations

- no release artifacts, container image, Homebrew tap, or stable tag have been
  published; generated candidate outputs do not imply availability;
- supported coverage is limited to explicitly configured evidence and
  documented adapters; TCG does not claim organization-wide completeness;
- current fixtures are regression evidence, not independent production
  adoption or a measured zero-false-negative claim;
- AI explanations and remediation are optional and never authoritative;
  agentic repair remains experimental.
