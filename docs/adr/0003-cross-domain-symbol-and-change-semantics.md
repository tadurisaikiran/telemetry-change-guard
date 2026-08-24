# ADR-0003: Cross-domain symbol and change semantics

- Status: Proposed
- Date: 2026-08-24
- Owners: Project maintainers
- Decision scope: Symbol identity, equality, and cross-domain mappings

## Context

Telemetry names can look identical across Prometheus, OpenTelemetry, Tempo,
CloudWatch Classic, CloudWatch OTel, and future systems while representing
different identities and operational semantics. A name-only match can create
both false dependencies and dangerous false negatives.

Some integrations legitimately bridge domains, but those relationships require
evidence. Neither a regular expression nor an AI model can authoritatively
establish such a mapping for a safety decision.

## Decision

1. A symbol is identified by a domain-specific canonical key. The common core
   includes domain, symbol kind, name, and optional parent; adapters may add
   validated domain qualifiers required for identity.
2. Symbol equality is defined only within a domain and delegated to a formal,
   deterministic domain canonicalizer. Common code must not lower-case, trim,
   suffix-match, or otherwise normalize arbitrary domains.
3. A dependency edge is same-domain unless it carries an explicit cross-domain
   mapping.
4. A cross-domain mapping records source and destination symbols, direction,
   provenance, owner/source authority where available, and whether resolution
   is exact or incomplete.
5. Mappings are never inferred from similar names. AI output and runtime
   co-occurrence may suggest a candidate mapping but cannot make it
   authoritative.
6. Unknown, partial, conflicting, or unsupported mapping input is preserved as
   a diagnostic and required uncertainty. It is never converted into
   “dependency absent.”
7. Domain-specific conveniences remain domain-specific. For example,
   Prometheus metric-family suffix matching must not leak into OpenTelemetry or
   CloudWatch identity.
8. Dependency traversal is cycle-safe, bounded, and provenance preserving.
   Multiple paths may be deduplicated for presentation only after their evidence
   remains available.

## Mapping trust levels

- **Exact:** both endpoints and the mapping relationship are deterministically
  established by a supported source.
- **Partial:** a supported source establishes a relationship but one or more
  identity qualifiers remain unresolved.
- **Unknown:** relevant input exists but cannot be safely resolved.

Only exact mappings create authoritative cross-domain dependency edges. Partial
and unknown mappings contribute uncertainty and may cause `INCOMPLETE` under
policy.

## Consequences

- Cross-domain integrations require more explicit configuration or stronger
  source evidence.
- Reports can explain why a dependency exists instead of presenting opaque
  name matches.
- Adapters must define canonicalization and equality tests before contributing
  safety findings.
- False convenience is rejected in favor of auditable uncertainty.

## Rejected alternatives

- **Global string identity:** rejected because domains have incompatible
  identity rules.
- **Heuristic mappings based on name similarity:** rejected because they can
  create authoritative false conclusions.
- **Treat unresolved mappings as no edge:** rejected because missing evidence
  is not evidence of safety.
- **Collapse CloudWatch and OpenTelemetry domains:** rejected even when a query
  language is shared; syntax does not prove identity.

## Validation

- Equality tests cover case, parent, suffix, namespace, dimension, and domain
  boundary behavior as applicable.
- Negative tests prove identical names in different domains do not match.
- Mapping tests cover direction, conflicts, partial values, cycles, stable
  traversal, and preserved provenance.
- Fuzz tests target domain parsers and mapping decoders.
