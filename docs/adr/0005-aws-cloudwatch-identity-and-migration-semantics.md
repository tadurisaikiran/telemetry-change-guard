# ADR-0005: AWS CloudWatch identity and migration semantics

- Status: Accepted
- Date: 2026-08-24
- Owners: Project maintainers
- Decision scope: AWS metric identity, canonicalization, and cross-domain mapping

## Context

Amazon CloudWatch exposes two metric models with different identity and query
semantics. Classic CloudWatch metrics are addressed by namespace, metric name,
and dimensions. CloudWatch OpenTelemetry metrics retain the hierarchical OTLP
resource, instrumentation-scope, metric, and datapoint model and expose it
through scoped PromQL labels.

AWS explicitly recommends an incremental Classic-to-OTel migration: dual-write,
validate, recreate consumers, and only then stop Classic publication. Suggested
metric names in that guidance are examples, not evidence that any particular
Classic and OTel streams are equivalent. A safety decision therefore requires
domain-specific identity and an explicit, auditable mapping.

CloudWatch consumers can also observe metrics across accounts and Regions.
Monitoring-account location is not necessarily origin location, and centralized
copies can carry source metadata. Omitting account or Region from identity can
join unrelated production streams.

The AWS CDK already provides the language-neutral input boundary. `cdk synth`
produces a cloud assembly containing CloudFormation templates, including one
JSON template per stack. Telemetry Change Guard does not need to execute or
parse TypeScript, Python, Java, Go, or C# application source.

This ADR specializes the cross-domain rules in ADR-0003. It defines the model
that the AWS implementation must follow; it does not add production code or a
public manifest schema.

## Decision

### Domains remain distinct

1. The canonical domain names are `aws_cloudwatch_classic` and
   `aws_cloudwatch_otel`.
2. Neither domain is equal to `prometheus` or the generic `opentelemetry`
   domain. Shared PromQL syntax and shared OTLP concepts do not establish
   backend identity.
3. Same-name symbols in the two CloudWatch domains are unrelated unless an
   exact cross-domain mapping establishes the relationship.
4. Domain-specific canonicalizers own equality. Common code must not trim,
   case-fold, suffix-match, or flatten AWS identities.

### AWS address and provenance

Every exact CloudWatch metric identity includes the metric's **origin AWS
account ID** and **origin AWS Region**. The account or Region in which a
dashboard is viewed, an analysis is run, or a centralized copy is stored is
retained as provenance but does not replace origin identity.

Address values are resolved only from supported evidence:

- an explicit analysis or stack-environment input;
- a Cloud Assembly stack environment;
- exactly resolved `AWS::AccountId` or `AWS::Region` pseudo parameters; or
- a live AWS response with recorded request and source-account provenance.

Ambient CLI profiles, environment defaults, and the analyst's current account
or Region must not silently complete an offline template identity. When
multiple authoritative sources disagree, the adapter emits a required conflict
diagnostic.

Account IDs and Regions are compared as exact strings after syntax validation.
They are not case-folded. AWS-reserved query labels are translated into address
fields only by a source-aware adapter. Similar-looking labels such as
`@aws.account`, `@aws.account_id`, or a user resource attribute are not global
aliases.

### Classic CloudWatch identity

The canonical Classic metric key is the tuple:

```text
origin account ID
origin Region
namespace
metric name
complete dimension set
```

The following rules apply:

- Namespace, metric name, dimension name, and dimension value retain their
  exact strings. Whitespace and case are significant; no normalization is
  inferred.
- Dimensions are a set of name/value pairs. Input order is irrelevant.
- Canonical encoding sorts dimensions by raw name and then raw value and uses
  an unambiguous structured encoding, not delimiter concatenation.
- Duplicate dimension names, invalid values, or conflicting duplicates are not
  canonicalized. They produce a diagnostic.
- A known empty dimension set differs from an unknown or partially resolved
  dimension set.
- A search, wildcard, or partial dimension predicate is a selector over metric
  keys, not itself an exact metric key.

For example, these identities are equal:

```text
Service=checkout,Stage=prod
Stage=prod,Service=checkout
```

These are not equal:

```text
Stage=prod
stage=prod
```

### Classic units

CloudWatch does not document unit as part of the Classic unique metric key, so
Telemetry Change Guard does not add it to that key. Units remain mandatory
semantic and data-stream qualifiers because CloudWatch aggregates otherwise
identical datapoints with different units separately.

The representation distinguishes:

- an explicitly specified supported unit;
- explicit `None`; and
- an omitted or unresolved consumer unit.

AWS defines an omitted unit on custom metric publication as `None`, so a
supported producer source may normalize that specific case. Omission is not
rewritten to `None` in a consumer definition. A known unit mismatch is semantic
risk. An unresolved relevant unit prevents a claim that a unit-sensitive
migration is safe. The adapter preserves the raw supported AWS unit value and
does not infer conversions from names.

### CloudWatch OTel identity

The CloudWatch OTel model preserves two related identity levels.

An OTLP metric-stream contract is identified by:

```text
origin account ID and Region
resource identity and resource schema URL
instrumentation scope (name, version, schema URL, attributes)
metric name
datapoint type
unit
intrinsic properties such as aggregation temporality and monotonicity
```

An individual time series additionally includes its complete datapoint
attribute set. Description, histogram bucket boundaries, numeric storage type,
and individual timestamps or values are not identity.

Resource, scope, and datapoint attribute collections are unordered typed maps.
Canonicalization sorts keys but preserves key case, value type, array order,
and exact value. Duplicate keys or values that cannot be represented without
loss are diagnostics, not last-value-wins input.

CloudWatch's PromQL projection is normalized without losing OTLP scope:

- `@resource.*` remains resource-scoped;
- `@instrumentation.*` remains instrumentation-scope data;
- `@datapoint.*` remains datapoint-scoped;
- a supported bare datapoint attribute is equivalent only to its documented
  `@datapoint.` form; and
- `@aws.*` metadata is handled separately from user OTLP attributes.

No Classic namespace is invented for an OTel metric. No OTel resource,
instrumentation, or datapoint attribute is collapsed into one unscoped label
map. A PromQL selector can intentionally describe a set of time series, but the
selector remains a predicate over canonical OTel identities rather than a
replacement for those identities.

### Cross-domain Classic-to-OTel mappings

Only an explicit mapping can connect the two domains. A mapping records:

- fully qualified source and destination selectors;
- mapping direction;
- account and Region behavior;
- Classic namespace and metric name to OTel metric-name correspondence;
- Classic dimension to explicitly scoped OTel attribute correspondence;
- literal, copied, or otherwise deterministic value rules;
- Classic and OTel units plus any deterministic numeric scale conversion;
- OTel datapoint type and intrinsic semantic constraints where relevant;
- provenance, owner/source authority, and resolution state.

Every Classic dimension used by a mapped selector and every OTel attribute
needed by the destination selector must be mapped or explicitly ignored with a
reason. An ignored field does not create an identity edge. Wildcard or
same-looking-name inference is not authoritative.

Different units require an explicit compatible conversion. The initial design
permits only deterministic linear scaling represented without binary
floating-point ambiguity. Missing conversion, incompatible dimensions, metric
type changes, temporality changes, or monotonicity changes remain semantic risk
or required uncertainty; a name mapping does not erase them.

Mappings follow ADR-0003 trust levels. Only `EXACT` mappings create
authoritative cross-domain graph edges. `PARTIAL`, `UNKNOWN`, conflicting, or
unsupported mappings produce required diagnostics. AWS documentation's
suggested migration names may inform a human-authored mapping but never create
one automatically.

### Resolution boundary

AWS identity fields use the following resolution states:

- `EXACT`: every field needed for the requested identity or selector is known
  from supported evidence;
- `PARTIAL`: supported structure is known, but one or more relevant values are
  unresolved; and
- `UNKNOWN`: the adapter cannot safely identify the value or supported
  structure.

CloudFormation intrinsics are data, not executable instructions. A later
resolver may evaluate a bounded, explicitly supported subset. It must preserve
the unresolved expression and provenance when exact evaluation is impossible.
`AWS::AccountId` and `AWS::Region` become exact only when the stack environment
is exact.

Partial or unknown critical identity produces `INCOMPLETE`, not an empty
dependency set. A known dependency remains reported when another qualifier is
unresolved.

### CDK and CloudFormation boundary

The AWS adapter consumes synthesized CloudFormation JSON and Cloud Assembly
metadata. It never executes CDK applications or parses their source languages.
Synthesis is performed by the caller's build before analysis. The loader must
remain read-only, bounded, deterministic, provenance-preserving, and aware of
multiple stacks; those mechanics begin in the next milestone.

## Invariants

- Account and Region are never silently inherited across unrelated stacks,
  widgets, queries, or live-source results.
- Monitoring and destination location never overwrite origin identity.
- Dimension and attribute ordering cannot change equality or output ordering.
- Case, scope, and typed attribute values are preserved.
- Empty, partial, and unknown qualifier sets remain distinguishable.
- Unit omission is not silently converted into an exact unit match.
- Same-name Classic, OTel, and Prometheus metrics do not match without explicit
  evidence.
- Unsupported or conflicting relevant input cannot produce `PASS`.
- Mapping and intrinsic evaluation never execute arbitrary code or perform
  network lookups.

## Consequences

- AWS symbols need domain-qualified identity data beyond the common
  `domain/kind/name/parent` fields. A later ADR or alpha schema change must add
  those qualifiers without weakening existing domains.
- Environment-agnostic CloudFormation templates can legitimately yield partial
  identity until account and Region are supplied.
- Cross-account and centralized telemetry remain attributable to their source,
  avoiding false joins in monitoring accounts.
- Migrations that rename and rescale metrics can be expressed, while semantic
  changes remain visible instead of being hidden behind a name mapping.
- OTel-aware analysis retains more structure than a plain PromQL label map,
  increasing implementation work but preserving correct equality.

## Rejected alternatives

- **Use namespace and metric name as Classic identity:** rejected because
  dimensions, account, and Region distinguish operationally separate metrics.
- **Put Classic unit in the unique metric key:** rejected because CloudWatch
  defines dimensions as identity while units split aggregation streams. Unit
  remains an explicit qualifier instead.
- **Collapse CloudWatch OTel into Prometheus:** rejected because query syntax
  does not erase AWS location or OTLP scope semantics.
- **Flatten all OTel attributes into labels:** rejected because identical keys
  in different scopes would collide and produce false dependencies.
- **Infer AWS migration mappings from recommended names:** rejected because
  examples cannot prove a workload's telemetry contract.
- **Use the analyst's active AWS profile to fill template identity:** rejected
  because results would vary by machine and could analyze the wrong account.
- **Parse or execute CDK source inside Telemetry Change Guard:** rejected
  because CloudFormation is the supported language-neutral normalization
  boundary and arbitrary execution violates the local read-only model.
- **Treat unresolved intrinsics as absent dependencies:** rejected because
  missing evidence is not evidence of safety.

## Validation required before implementation is complete

- Equality tests cover account, Region, namespace, metric name, dimension
  ordering, dimension case, empty dimensions, duplicate dimensions, and units.
- OTel tests cover resource and scope separation, schema URLs, typed attribute
  equality, datapoint attributes, metric type, unit, temporality, monotonicity,
  description non-identity, and stable canonical output.
- Negative tests prove identical names across Classic, CloudWatch OTel,
  Prometheus, accounts, and Regions do not match.
- Mapping tests cover direction, copied and literal values, explicit ignores,
  unit scaling, conflicts, partial endpoints, and preserved provenance.
- Resolution tests prove unknown account, Region, dimensions, labels, or
  intrinsics fail closed when relevant.
- Centralization fixtures prove origin identity survives monitoring-account and
  destination-Region context.
- Fuzz tests target every new decoder and canonicalizer with bounded inputs.

## Authoritative references

- [CloudWatch metrics concepts](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/cloudwatch_concepts.html)
- [CloudWatch OpenTelemetry metrics](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/metrics-otel-overview.html)
- [CloudWatch PromQL querying and OTLP label scopes](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch-PromQL-Querying.html)
- [Migrate from Classic to OTel metrics](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/metrics-otel-migrate.html)
- [CloudWatch cross-account and cross-Region metrics centralization](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatchMetrics_Centralization.html)
- [CloudFormation pseudo parameters](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/pseudo-parameter-reference.html)
- [AWS CDK synthesis](https://docs.aws.amazon.com/cdk/v2/guide/configure-synth.html)
- [OpenTelemetry metrics data model](https://opentelemetry.io/docs/specs/otel/metrics/data-model/)
- [OpenTelemetry instrumentation scope](https://opentelemetry.io/docs/specs/otel/common/instrumentation-scope/)
- [OpenTelemetry attribute equality](https://opentelemetry.io/docs/specs/otel/common/)
