# ADR-0002: ChangeSet as the root domain primitive

- Status: Proposed
- Date: 2026-08-24
- Owners: Project maintainers
- Decision scope: Public change input and internal normalization

## Context

The existing `Migration` model expresses a coordinated transition from legacy
telemetry to replacement telemetry. The broader product must also express
removal, addition, metadata mutation, and semantic change without inventing a
fake migration for each case.

Discovery evidence, dependency graphs, policy, and results have different
lifecycles from the proposed change. Combining them into one mutable object
would make provenance and deterministic re-evaluation harder to reason about.

## Decision

1. `ChangeSet` is the root representation of a proposed telemetry contract
   change.
2. The initial public manifest identity is:

   ```yaml
   apiVersion: tcg/v1alpha1
   kind: ChangeSet
   ```

3. A ChangeSet contains metadata, a human description, and an ordered list of
   uniquely identified changes. It does not contain discovered consumers,
   policy outcomes, AI explanations, or mutable runtime state.
4. Every change identifies its telemetry domain, change kind, source symbol,
   optional destination symbol, metadata, and source provenance. A removal has
   no destination; kinds that require a destination fail validation when it is
   absent.
5. Legacy manifests remain accepted unchanged:

   ```yaml
   apiVersion: telemetry-migration/v1alpha1
   kind: Migration
   ```

6. A legacy `Migration` is validated under its existing rules and normalized
   into a ChangeSet before generic discovery. Normalization is deterministic,
   lossless for migration semantics, and does not reinterpret unknown fields as
   safe defaults.
7. The generic pipeline consumes normalized ChangeSets. The legacy readiness
   wrapper may retain migration-specific context required for its unchanged
   result schema and classifications.
8. Input ordering is preserved for diagnostics, while result aggregation and
   graph traversal use explicit stable ordering so repeated runs are bytewise
   reproducible where formats permit it.

## Invariants

- Change IDs are non-empty and unique within a ChangeSet.
- A symbol's identity is domain-qualified; plain-name equality is insufficient.
- Unsupported kinds and unsupported schema versions fail explicitly.
- Missing required input produces a validation error or incomplete analysis,
  never an empty dependency set.
- Provenance survives normalization and is attached to downstream findings.
- Adapters cannot mutate the caller's ChangeSet.

## Consequences

- Generic and migration workflows share discovery and graph construction
  without forcing all generic changes into legacy readiness classifications.
- A new public alpha schema is introduced while the legacy schema remains
  stable.
- Normalization becomes a high-value compatibility boundary and requires golden
  tests for all legacy change types.
- Future change sources—Weaver diffs, telemetry snapshots, and supported
  infrastructure diffs—can target one normalized contract.

## Rejected alternatives

- **Expand Migration until it represents every change:** rejected because it
  preserves the wrong root abstraction and produces confusing semantics.
- **Delete or rewrite the legacy model:** rejected because existing users and
  machine contracts must remain valid.
- **Put policy decisions in ChangeSet:** rejected because inputs and
  authoritative analysis results must remain separate and reproducible.
- **Infer a missing destination:** rejected because safety-critical input must
  not be guessed.

## Validation

- Golden tests prove each valid legacy manifest normalizes deterministically.
- Invalid and partially supported inputs prove fail-closed behavior.
- Round-trip tests cover the new alpha manifest without promising compatibility
  beyond the versioning policy.
- No generic policy implementation begins until the normalization boundary and
  its compatibility tests are accepted.
