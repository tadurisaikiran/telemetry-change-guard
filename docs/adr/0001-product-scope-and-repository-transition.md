# ADR-0001: Telemetry Change Guard product scope and repository transition

- Status: Proposed
- Date: 2026-08-24
- Owners: Project maintainers
- Decision scope: Product identity, repository ownership, and compatibility

## Context

Telemetry Migration Readiness (TMR) already provides a deterministic migration
engine, dependency discovery, fail-closed readiness decisions, a CLI, and a
published GitHub Action. The broader product must analyze any supported
telemetry contract change and its operational impact, while preserving the
existing migration workflow.

Renaming the existing repository is unsafe because GitHub Action references do
not follow repository rename redirects. Existing users must not lose a working
Action or a stable historical release location.

The source transition is anchored at immutable commit
`bbaff105332f927f6caa65bc7721aa50cb5e4734`. The new repository was created by
copying that commit and its reachable history without rewriting commits.

The proposed short executable name `tcg` has an existing global executable in a
common package registry and is a heavily used software/security acronym. That
creates avoidable installation, search, and support ambiguity.

## Decision

1. The product is **Telemetry Change Guard**.
2. The canonical repository is
   `github.com/tadurisaikiran/telemetry-change-guard`.
3. `github.com/tadurisaikiran/telemetry-migration-readiness` remains available
   as the legacy compatibility repository. It is not renamed or deleted.
4. The canonical executable will be `telemetry-change-guard`. **TCG** may be
   used as human shorthand, but `tcg` will not initially be installed as a
   binary or package command.
5. The legacy repository retains its `tmr` executable, Action coordinates,
   machine schemas, releases, and historical tags. The new source tree also
   retains a temporary `tmr` compatibility entrypoint backed by shared CLI
   implementation; it must not fork into a second behavior path.
6. Development moves in narrow, reviewable stages: repository identity,
   product branding, generic domain model, generic policy, and CLI/Action
   compatibility. Repository identity changes must not alter runtime behavior.
7. The product remains a local-first, read-only, deterministic safety layer.
   It is not an observability backend and does not introduce a database, server,
   UI, or multi-agent AI system without a separately justified requirement.

## Compatibility contract

- Existing `uses: tadurisaikiran/telemetry-migration-readiness@...` references
  continue to resolve.
- Existing `tmr` commands and legacy configuration/result schemas retain their
  behavior during the transition.
- Historical TMR tags and releases remain canonical only in the legacy
  repository; they are not copied indiscriminately to the new product.
- New product releases and Action coordinates originate from the new
  repository only after compatibility and external Action validation.

## Consequences

- The project carries two repository identities during a deliberate
  compatibility window.
- Documentation must clearly distinguish the legacy compatibility surface from
  active product development.
- The longer canonical executable avoids a known collision at the cost of more
  typing. A short alias may be reconsidered only after separate ecosystem due
  diligence and an explicit compatibility design.
- Transition pull requests remain small enough to distinguish relocation bugs
  from behavior changes.

## Rejected alternatives

- **Rename the legacy repository:** rejected because it can break existing
  GitHub Action consumers.
- **Rewrite history into a clean new project:** rejected because it destroys
  provenance and complicates verification.
- **Keep Migration as the root product abstraction:** rejected because it does
  not describe removals, semantic changes, control-plane dependencies, or
  generic pull-request safety decisions.
- **Install `tcg` immediately:** rejected because the known command and acronym
  collisions create needless production support risk.

## Validation

- Legacy and new repository `main` must initially resolve to the source freeze
  SHA.
- No product implementation begins until this ADR and ADR-0002 through
  ADR-0004 are accepted.
- Every transition change runs the complete compatibility, race, fuzz,
  vulnerability, workflow-lint, and live E2E gates.
