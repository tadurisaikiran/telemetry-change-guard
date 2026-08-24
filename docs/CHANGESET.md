# ChangeSet model

`ChangeSet` is the root representation of a proposed telemetry contract
change. It contains input facts only: identity, description, ordered changes,
and source metadata. Discovered consumers, dependency paths, policy decisions,
and AI output are deliberately separate.

The initial strict manifest identity is:

```yaml
apiVersion: tcg/v1alpha1
kind: ChangeSet
metadata:
  name: checkout-contract
spec:
  description: Change the checkout request metric contract.
  changes:
    - id: request-duration
      kind: metric_rename
      domain: prometheus
      from:
        domain: prometheus
        kind: metric
        name: checkout_request_duration_seconds
      to:
        domain: prometheus
        kind: metric
        name: checkout_server_request_duration_seconds
      metadata:
        source.adapter: explicit
        ticket: OBS-142
```

Every change ID must be non-empty and unique. Source and destination symbols
are domain-qualified and include their symbol kind; a same-looking name in a
different domain is not the same symbol. Rename kinds require `to`, while
removal kinds reject it. Unknown fields, unsupported versions or kinds,
multiple YAML documents, and invalid domain/symbol combinations fail
explicitly.

Change metadata is bounded to 64 entries, 128 bytes per key, and 4 KiB per
value. Metadata is evidence and provenance only; it cannot select a safety
status.

## Legacy normalization

Existing manifests remain valid without modification:

```yaml
apiVersion: telemetry-migration/v1alpha1
kind: Migration
```

Telemetry Change Guard validates each legacy manifest under its existing rules
and deep-copies it into a canonical `ChangeSet` before discovery. Name,
description, ordered changes, domain-qualified symbols, destinations, and
adapter provenance are preserved. The normalized value shares no mutable maps,
slices, or destination pointers with the legacy input.

The current `tmr` CLI and `tmr-result/v1alpha1` readiness result remain the
compatibility interface. Native `ChangeSet` command and Action workflows will
be enabled with the generic policy layer; this model does not silently assign
generic safety semantics to the legacy readiness statuses.
