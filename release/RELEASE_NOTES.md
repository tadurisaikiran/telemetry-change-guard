# Telemetry Change Guard v0.1.0-alpha.1

This candidate packages the deterministic Telemetry Change Guard engine for
public-alpha evaluation. It includes the canonical `telemetry-change-guard`
CLI, the compatible `tmr` CLI, the composite GitHub Action, supported static
and runtime evidence adapters, fail-closed safety policies, and the isolated
agentic feedback-loop experiment.

## Highlights

- Analyze configured dashboards, alerts, SLOs, autoscalers, deployment gates,
  runtime query evidence, and supported trace evidence before a telemetry API
  changes.
- Preserve deterministic `PASS`, `WARN`, `BLOCK`, `INCOMPLETE`, and `ERROR`
  authority while allowing optional AI explanation and draft remediation.
- Verify build version, commit, date, clean state, Go toolchain, OS, and
  architecture through `version --format json`.
- Download static archives for Linux, macOS, and Windows on amd64 and arm64.
- Verify SHA-256 checksums, per-archive SPDX and CycloneDX SBOMs, the release
  manifest, and GitHub-hosted build provenance.

## Known limitations

- This is an alpha for evaluation, not a stable or production-proven release.
- TCG sees only supported evidence explicitly configured for the analysis; it
  does not discover an entire organization-wide telemetry blast radius.
- Runtime evidence needs deliberate network and credential policy. The GitHub
  Action disables remote evidence unless trusted workflow configuration opts
  in and supplies exact allowed origins.
- The agentic layer is an isolated experiment. It cannot approve, merge, or
  override deterministic engine decisions.
- macOS binaries are not yet signed or notarized. Windows binaries are not yet
  Authenticode-signed. Verify checksums and provenance before evaluation.
- The multi-architecture container is build-verified with non-root/read-only
  smoke tests, SPDX SBOM attestations, and SLSA provenance, but GHCR publication
  remains blocked pending separate owner authorization.
- The generated Homebrew formula is checksum-bound and syntax-checked, but the
  tap repository and real installation test do not exist yet. Homebrew
  availability is not implied by this release.
- External design-user validation and benchmark evidence are still required
  before broader readiness claims.

## Upgrade and compatibility

Read `docs/UPGRADING.md`, `docs/COMPATIBILITY.md`, and `CHANGELOG.md` in the
source archive. Machine-readable analysis schemas remain at their documented
`v1alpha1` identifiers for this candidate.
