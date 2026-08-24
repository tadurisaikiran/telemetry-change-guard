# Architecture decision records

Architecture decision records (ADRs) capture decisions that materially affect
Telemetry Change Guard's safety model, public contracts, or compatibility.

Statuses used here are:

- **Proposed**: ready for review but not authorized for implementation.
- **Accepted**: approved and authoritative for implementation.
- **Superseded**: replaced by a later ADR that links back to the original.
- **Rejected**: considered but intentionally not adopted.

An ADR is immutable after acceptance except for spelling, links, or an explicit
status change. A material change requires a new ADR.

| ADR | Decision | Status |
| --- | --- | --- |
| [0001](0001-product-scope-and-repository-transition.md) | Product scope and repository transition | Proposed |
| [0002](0002-changeset-root-domain-primitive.md) | ChangeSet as the root domain primitive | Proposed |
| [0003](0003-cross-domain-symbol-and-change-semantics.md) | Cross-domain symbol and change semantics | Proposed |
| [0004](0004-generic-safety-policy-and-migration-readiness.md) | Generic safety policy and migration readiness | Proposed |
