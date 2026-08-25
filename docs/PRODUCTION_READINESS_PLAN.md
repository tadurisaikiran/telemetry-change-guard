# Telemetry Change Guard public-alpha production-readiness plan

Status: in progress  
Target candidate: `v0.1.0-alpha.1`  
Publication status: blocked pending explicit owner authorization

## Purpose

This plan takes Telemetry Change Guard (TCG) from a source-only prerelease to
an honest public-alpha release candidate. The work prioritizes security,
fail-closed correctness, reproducible distribution, first-user clarity, and
independent verification. It does not expand the product into new telemetry
domains, hosted services, UI work, or additional AI capabilities.

TCG's deterministic engine remains authoritative. AI may explain or draft, but
it does not decide or override `PASS`, `WARN`, `BLOCK`, `INCOMPLETE`, or
`ERROR`.

## Repository baseline

Baseline captured on 2026-08-25 (America/Chicago).

| Item | Baseline |
| --- | --- |
| Repository | `tadurisaikiran/telemetry-change-guard` |
| Branch | `main` |
| Commit | `b82abfd60d09e1df2c4d81307a2c9af4a3cc9613` |
| Worktree | Clean; synchronized with `origin/main` |
| Go module version | `go 1.27.0` |
| Local toolchain | `go1.27.0 darwin/arm64` |
| Docker client | `29.1.3` |
| Docker daemon | Unavailable in the local execution environment |
| Tags | None |
| GitHub releases | None |
| Latest main CI | Successful, run `32799162090` |
| Latest pinned E2E | Successful, run `32799162091` |
| Published binary/container/Homebrew package | None |

### Baseline verification

The following commands ran against the baseline commit:

| Command | Outcome |
| --- | --- |
| `go mod tidy` | Passed; no `go.mod` or `go.sum` diff |
| `gofmt -l .` | Passed; no files reported |
| `go vet ./...` | Passed |
| `go test -race ./...` | Passed with loopback access required by `httptest` |
| `go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...` | Passed; no vulnerabilities found |
| `./e2e/scripts/run-control-plane-e2e.sh` | Local run blocked: Docker daemon unavailable; exact-commit GitHub job passed |
| `./e2e/scripts/run-e2e.sh` | Local run blocked: Docker daemon unavailable; exact-commit GitHub job passed |
| `./e2e/scripts/run-tempo-e2e.sh` | Local run blocked: Docker daemon unavailable; exact-commit GitHub job passed |

The first sandboxed race run could not bind IPv6 loopback ports used by
`httptest`. The identical command passed after loopback access was granted, so
this was an execution-environment restriction rather than a product failure.

### Existing warnings and annotations

Each local composite-Action smoke job (`action-generic`, `action-snapshot`, and
`action-migration`) succeeds with this warning:

```text
Restore cache failed: Invalid pattern
'/home/runner/work/telemetry-change-guard/telemetry-change-guard/.//go.sum'.
Relative pathing '.' and '..' is not allowed.
```

The warning originates from using `${{ github.action_path }}/go.sum` while the
Action itself is consumed through `uses: ./`. It is tracked by issue #35 and is
a release blocker because it creates avoidable doubt for a first evaluator.

## Product truth and immutable contracts

TCG treats telemetry like an API contract and checks which configured
consumers will break before the contract changes. It analyzes only configured,
supported evidence; it does not claim to find an entire organization-wide
blast radius.

The following contracts must not regress:

| Generic status | Exit |
| --- | ---: |
| `PASS`, `WARN` | `0` |
| `ERROR` | `1` |
| `BLOCK` | `2` |
| `INCOMPLETE` | `3` |

Legacy migration compatibility remains supported, including its existing
machine schemas and `READY`/`BLOCKED` vocabulary. Required evidence that is
missing, malformed, denied, unresolved, unavailable, or incomplete must not
become safe. Confirmed findings must remain visible even when a higher
precedence incomplete or error condition exists.

## Discovered risks

### P0: PR-controlled remote credential destination

Repository-controlled Perses or Tempo URLs can currently receive a bearer
token selected through repository configuration. The Action has no independent
network policy or trusted-origin allowlist. Authenticated HTTP is allowed.
Although both adapters already reject cross-origin redirects, the initial
secret destination is not protected from an untrusted configuration change.

### P0: overlapping required-source downgrade

Generic local sources and runtime-query sources load each file on its first
match and skip later matches. An optional broad glob encountered before a
required narrow glob can therefore downgrade malformed required evidence.
Tempo de-duplicates by URL and file with the same first-match behavior. HPA
already merges `required` with logical OR, but its canonical identity and
ordering still require review.

### P0: mutable workflow dependencies and coordinate drift

All third-party Actions currently use movable major tags. README, Action guide,
and design-user instructions reference different TCG commits. This conflicts
with TCG's immutable supply-chain guidance.

### P1: source-only distribution and missing version identity

There is no release, version command, binary archive, checksum manifest, SBOM,
provenance, external-consumer fixture, container, or tested package-manager
path. Source builds require the newly released Go 1.27 toolchain.

### P1: first-use breadth

The README is accurate but introduces advanced AI and agentic surfaces before
a public evaluator has installed and run the deterministic safety check. The
front door needs a release-backed install, five-minute check, boundaries, and
alpha maturity statement before advanced material.

### P1: independent validation is absent

Current fixtures and E2E suites are strong regression evidence but are not
independent adoption or field validation. The project must not claim
production proof, broad adoption, or measured false-negative performance until
external evaluations and a documented benchmark exist.

## Controlled pull-request sequence

Branches and PRs are focused, human-named, independently testable, and merged
without force-pushing shared history.

1. `security/p0-fail-closed-hardening`
   - add this plan and baseline;
   - merge overlapping-source requirements deterministically;
   - add explicit remote-evidence policy and trusted-origin enforcement;
   - add adversarial regression tests and secure CI guidance.
2. `action/production-hardening`
   - pin every Action dependency;
   - minimize permissions;
   - fix local cache warnings;
   - add CodeQL, dependency review, workflow lint/security checks, and release
     coordinate validation;
   - preserve authoritative Action outputs and exit enforcement.
3. `release/version-and-build-metadata`
   - add version commands, linker-injected metadata, and compatibility policy;
   - add `CHANGELOG.md`, versioning, compatibility, and upgrade guidance.
4. `release/artifacts-and-provenance`
   - add reproducible multi-platform archives, checksums, SBOMs, manifests,
     provenance-ready workflows, snapshot builds, and verification scripts;
   - add `make verify`, `make e2e`, and `make release-snapshot`.
5. `docs/install-quickstart-and-security`
   - rebuild the README front door and installation/quickstart hierarchy;
   - document security, configuration, limitations, troubleshooting, releases,
     verification, repository settings, and all supported modes.
6. `validation/release-consumer-fixtures`
   - add generated external-consumer repository contents;
   - add original/licensed benchmark cases, machine results, and evaluation
     templates;
   - add container and package-install verification.
7. `docs/public-launch-kit`
   - add an evidence-constrained product brief, launch article, deep dive,
     tutorial, demo, FAQ, social drafts, and case-study template.

Action hardening moved ahead of version metadata after the P0 merge because
immutable dependencies and warning-free hosted Action checks are prerequisites
for trusting the later release workflows. No publication behavior is included
in that security-focused reordering.

### Execution record

| Phase | Pull request | Result |
| --- | --- | --- |
| P0 correctness and secret boundary | `#46` | Merged; fail-closed overlap and remote-origin controls verified |
| Action and workflow hardening | `#47` | Merged; immutable dependencies, permissions, and warning-free smoke tests |
| Version and build identity | `#49` | Merged; canonical and compatibility identity contracts |
| Reproducible artifacts and provenance | `#50` | Merged at `4bb5ea7345f56291bebc65c63e8375e46d002f12`; CodeQL-clean, byte-reproducible candidate payload |
| Install and consumer validation | `#51` | Merged at `8528ab9d7017eda3190377d2e726ec3ac750ce91`; external Action, exact-ref `go install`, multi-arch container, formula, SBOM, and provenance paths verified |
| Public-alpha documentation and regression corpus | current branch | In progress on `docs/public-alpha-readiness`; front door, doc checks, 11-case synthetic benchmark, and evaluation kit |

The current documentation phase uses `docs/public-alpha-readiness`, a
human-named branch from the fully verified `#51` merge. The preceding phases
prepare but do not publish a GitHub release, tag, package, image, tap, external
repository, or announcement.

## Acceptance criteria by phase

### Correctness and security

- All source loaders merge overlapping `required` values with logical OR.
- File identity, load order, and diagnostics are deterministic across
  configuration order.
- Remote evidence is disabled by default in the GitHub Action.
- Authenticated requests require HTTPS, except explicitly enabled loopback
  development endpoints.
- Credentialed origins must be supplied by trusted workflow configuration and
  matched exactly after canonical normalization.
- Cross-origin redirects, URL user information, query/fragment injection, and
  untrusted destinations are rejected.
- Denied required evidence fails closed without removing local findings.
- Tokens never appear in logs, reports, errors, summaries, or artifacts.
- Every correctness/security change has positive, negative, and adversarial
  regression coverage.

### Versioning and release engineering

- `telemetry-change-guard version`, `version --format json`, and `--version`
  report stable, tested build metadata.
- Release snapshot builds canonical and compatibility executables for every
  documented platform without publishing.
- Archives, checksums, SBOMs, release manifest, notes, and verification scripts
  agree on version and commit.
- Tag-triggered publication remains inert until an owner creates/approves the
  exact prerelease tag.
- No documentation or workflow invents a `v1` coordinate.

### Action and installation

- Every third-party Action is pinned to a full commit SHA with a version
  comment and Dependabot coverage.
- Workflow/job permissions are least privilege.
- Local and external Action cache paths work without annotations.
- The Action preserves one analysis, JSON evidence, Markdown summary, bounded
  optional comment, and exact status/exit enforcement.
- GitHub Release, `go install`, Action, container, and prepared Homebrew paths
  are testable from generated candidate artifacts.
- All user-facing installation coordinates come from one source of truth.

### Documentation and validation

- A new user can understand the failure, install the alpha, run the fixture,
  interpret exit `2`, and add a minimal repository check in under five minutes.
- Advanced adapters, migration, AI, and agentic experiments use progressive
  disclosure rather than dominating the front door.
- Limitations and alpha maturity are prominent.
- Documentation commands, links, status tables, coordinates, and CLI output
  are checked in CI.
- `make benchmark` emits machine-readable results for a clearly scoped corpus.
- Internal fixtures are described as regression evidence, not independent
  proof.

## Release blockers

The release candidate is not ready while any of these are true:

- a P0 security or fail-closed correctness item is open;
- required CI, race, fuzz smoke, vulnerability, E2E, release-snapshot,
  artifact, installation, Action, documentation, or benchmark gates fail;
- a required platform is untested and not explicitly scoped out;
- release coordinates differ across user-facing documentation;
- release binaries lack checksums, SBOMs, version identity, or verification;
- remote evidence can send a credential to a repository-controlled origin;
- release notes omit material limitations;
- the candidate is described as stable or production-proven;
- owner approval for the exact tag and publication is absent.

## Manual repository and owner actions

These operations are intentionally not performed by implementation PRs:

- require pull requests, current checks, and conversation resolution on `main`;
- block force pushes and branch deletion;
- enable private vulnerability reporting, secret scanning, dependency graph,
  and Dependabot alerts where the GitHub plan supports them;
- enable immutable releases if available;
- create and protect a release environment with limited approvers;
- choose the signed-tag or equivalent tag-verification policy;
- create the optional `homebrew-tap` repository;
- create the external Action fixture repository from the prepared contents;
- approve and create `v0.1.0-alpha.1`;
- publish the GitHub prerelease, OCI image, or Homebrew formula;
- enable Marketplace publication after the alpha has real immutable assets.

Exact setting names, required check names, and emergency procedures will be
recorded in `docs/REPOSITORY_SETTINGS.md` after workflows stabilize.

## Unresolved decisions

- Whether the Homebrew tap will exist for the first alpha or remain a prepared,
  blocked integration.
- Whether the first public alpha requires a published external fixture
  repository or owner acceptance of a fully generated but unpublished fixture.
- Whether Go 1.26 can be supported without changing dependencies; verified
  binary artifacts remain the primary low-friction path regardless.

## Rollback strategy

Implementation PRs are independently revertible. If a merged hardening change
causes a regression, revert the specific merge commit through a reviewed PR;
do not rewrite history or weaken fail-closed behavior to restore compatibility.

After a prerelease is published:

1. Do not move or silently replace the tag or its artifacts.
2. Mark the defective prerelease as withdrawn/deprecated in GitHub and the
   changelog while preserving immutable evidence.
3. Document the defect, affected platforms/contracts, and safe workaround.
4. Fix through normal reviewed commits and rerun every release gate.
5. Publish a new prerelease such as `v0.1.0-alpha.2` with new checksums,
   SBOMs, provenance, notes, and coordinates.
6. Update install guidance to the corrected immutable release without deleting
   the historical record.

## Execution record

This section will be updated by each focused PR with its commit, checks,
artifacts, remaining blockers, and any justified deviation from this plan.

### P0 fail-closed hardening candidate

Branch: `security/p0-fail-closed-hardening`

Implemented:

- deterministic two-stage local source expansion with canonical file identity,
  logical-OR requirement merging, stable ordering, and pattern provenance;
- matching fail-closed merging for runtime-query, Tempo, Perses, and HPA source
  aliases, including explicit diagnostics for conflicting settings;
- exact canonical remote-origin policy supplied outside repository YAML;
- HTTPS enforcement for bearer-authenticated Perses, Tempo, and Prometheus
  snapshot requests, with an explicit loopback-only development exception;
- Action remote evidence disabled by default, exact allowlists, a fixed token
  input, and a cleared analysis environment;
- bounded same-origin redirects and token-redaction regression coverage;
- `docs/SECURE_CI_USAGE.md`, plus linked configuration, threat-model, Action,
  testing, and README guidance;
- `make verify` and `make e2e` developer entry points.

Verified locally on Go 1.27.0:

- `make verify GO=/private/tmp/tcg-go1.27/go/bin/go` passed, including module
  consistency, formatting, vet, all race-enabled tests, every configured fuzz
  smoke target, `govulncheck@v1.7.0`, and Action shell syntax;
- the getting-started scenario still returned the documented `BLOCK` and exit
  `2` with its alert dependency intact;
- all local links across 48 Markdown files resolved;
- `git diff --check` passed.

Docker-dependent local E2E remained environment-blocked because Docker Desktop
was not running. PR #46 passed CI plus all three hosted pinned lifecycle jobs
and was squash-merged as `9692424086189256a94ef3c2d89902dddc04d78c`.

### Action production hardening

- Branch: `action/production-hardening`
- Pull request: #47
- Merged main commit: `4e211b7571d9a84fde7c6bfe3d92ac43d9ecde3b`

Implemented:

- every existing and newly introduced third-party Action is pinned to a full
  commit SHA with an exact version comment;
- Action-owned Go dependency paths are canonicalized before `setup-go`,
  preserving cache use for local and external consumption without `/.//`;
- workflow defaults are read-only, while CodeQL's write permission is isolated
  to its analysis job;
- CodeQL, dependency review, pinned actionlint, offline zizmor auditing, and a
  repository policy check provide complementary, maintainable security gates;
- `release/metadata.env` is the single candidate Action coordinate source,
  with a test rejecting stale SHA references in README or documentation;
- Dependabot continues to cover both Go modules and GitHub Actions.

Local candidate verification so far:

- `make verify GO=/private/tmp/tcg-go1.27/go/bin/go` passes with the same
  loopback access required by the baseline, including module consistency,
  formatting, vet, race tests, all fuzz smoke targets, `govulncheck`,
  actionlint, workflow policy, and shell syntax;
- Action contract and path-resolution tests pass for local dot-segment and
  external symlinked consumption;
- `actionlint@v1.7.12` reports no workflow errors;
- `zizmor@v1.29.0` reports no medium-or-higher, high-confidence offline
  findings after adding a seven-day Dependabot update cooldown;
- the workflow policy script, shell syntax checks, and `git diff --check` pass.

Hosted validation passed eleven checks: full CI, all three Action modes,
CodeQL analysis and its code-scanning result, dependency review, workflow
security, and all three pinned lifecycle jobs. The generic, snapshot, and
migration Action check runs each returned an empty annotation list, proving the
invalid cache-path warning was removed rather than hidden.

### Version and build-metadata candidate

Branch: `release/version-and-build-metadata`

Implemented so far:

- release-injectable product version, full commit, build date, and tri-state
  dirty metadata plus runtime Go/platform identity;
- shared `version`, `version --format json`, and `--version` commands for the
  canonical and compatibility executables;
- stable `tcg-version/v1alpha1` JSON and release-shaped linker/trimpath tests;
- Action job-summary identity that records only an authoritative Action or
  local-workflow commit and leaves a movable external ref unknown;
- changelog, versioning, compatibility, and upgrade policies that preserve
  both existing analysis schemas without inserting dynamic build fields.

Local candidate verification:

- `make verify GO=/private/tmp/tcg-go1.27/go/bin/go` passes module consistency,
  formatting, vet, all race-enabled tests, all fuzz-smoke targets,
  `govulncheck`, actionlint, workflow policy, and shell syntax;
- a real release-shaped binary reports injected candidate version, exact full
  commit, RFC 3339 build date, clean state, runtime, and platform, and does not
  contain the local checkout path;
- development canonical and `tmr` binaries report matching explicit `dev`
  identity; malformed release linker values normalize to non-release values;
- offline `zizmor@v1.29.0` reports no scoped findings and all local links
  across 53 Markdown files resolve;
- `git diff --check` passes.

### Reproducible artifacts and provenance candidate

Branch: `release/artifacts-and-provenance`

Implemented so far:

- GoReleaser `v2.18.0` builds `telemetry-change-guard` and `tmr` with Go
  `1.27.0`, `CGO_ENABLED=0`, `-trimpath`, exact commit/date identity, and fixed
  modification times for Linux, macOS, and Windows on amd64 and arm64;
- each platform archive contains both binaries, `LICENSE`, a concise release
  README, and alpha notices; an exact-commit source archive is separate;
- Syft `v1.51.0` produces per-archive SPDX and CycloneDX SBOMs whose
  invocation-dependent timestamps, namespaces, extraction prefixes, random
  serial, and ordering are normalized before checksumming;
- a strict `tcg-release-manifest/v1alpha1` document records version, commit,
  build date, clean state, tools, platforms, files, sizes, and SHA-256 digests;
- the deep verifier rejects extra/missing files, traversal, symlinks, bad
  modes/times, build-path leakage, wrong Go settings, identity disagreement,
  malformed SBOMs, and checksum/manifest drift, then runs the host binaries and
  getting-started `BLOCK`/exit `2` fixture;
- pull-request snapshots have read-only permissions, no dependency cache, no
  persisted checkout credential, immutable Action pins, and no publisher;
- the dormant tag workflow requires the exact annotated prerelease tag on
  protected-main history, reruns `make verify` and pinned E2E, rebuilds twice,
  verifies, attests with GitHub OIDC, and only then prepares a GitHub
  prerelease. It cannot run until an owner creates the tag and approves the
  `public-alpha` environment.

Local candidate verification:

- the official GoReleaser and Syft archives were installed only after their
  upstream checksum manifests matched repository-pinned SHA-256 values;
- `make release-reproducible` built 12 binaries, six platform archives, one
  source archive, and 14 SBOMs twice from commit `941ad333c6be51654f80274b211d11620b45744b`;
- both complete public payloads had an identical checksum set, while each run
  independently passed the 21-artifact deep verifier;
- actionlint and workflow policy validation passed; offline
  `zizmor@v1.29.0` reported no findings for either release workflow;
- no tag, GitHub Release, package, image, external repository, or announcement
  was created.
