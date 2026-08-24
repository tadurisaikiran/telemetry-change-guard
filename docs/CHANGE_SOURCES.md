# Change sources and telemetry snapshots

Telemetry Change Guard normalizes every supported change source into the same
`domain.ChangeSet` before consumer discovery and policy evaluation. Source
selection does not change safety semantics.

Supported generic sources are:

1. an explicit `tcg/v1alpha1` `ChangeSet`;
2. a structured Weaver registry diff with an exact backend mapping;
3. a baseline/candidate pair of deterministic telemetry snapshots.

Exactly one source is required by `check` and by the canonical GitHub Action.
Malformed source input is an `ERROR`. A well-formed source that exposes
required semantic uncertainty is `INCOMPLETE`; it is never converted to a
clean pass.

## Capture a Prometheus contract

```bash
telemetry-change-guard snapshot \
  --prometheus http://localhost:9090 \
  --name checkout-main \
  --output ./telemetry-contract.json
```

For authenticated Prometheus deployments, pass an environment variable name,
not a token value:

```bash
telemetry-change-guard snapshot \
  --prometheus https://prometheus.example.com \
  --bearer-token-env TCG_PROMETHEUS_TOKEN \
  --output ./telemetry-contract.json
```

The collector reads `/api/v1/metadata` and `/api/v1/series`. It retains metric
names, metric type, unit when known, and the union of label names. It never
stores samples, label values, credentials, or a generated timestamp. Metrics
and labels are sorted, so the same returned contract produces identical JSON
bytes.

Collection is bounded and fail closed:

- the default limits are 50,000 metric families and 100,000 series;
- hard limits are 100,000 metric families and 1,000,000 series;
- each API response is limited to 64 MiB;
- the default total timeout is 60 seconds and cannot exceed 10 minutes;
- cross-origin redirects are rejected;
- API warnings, truncation, conflicting type/unit metadata, missing metric
  names, invalid response envelopes, and limit violations fail collection.

`--max-metrics`, `--max-series`, and `--timeout` may lower or tune the defaults
without disabling hard bounds.

The snapshot artifact is strict JSON:

```json
{
  "apiVersion": "tcg/v1alpha1",
  "kind": "TelemetrySnapshot",
  "metadata": { "name": "checkout-main" },
  "spec": {
    "domain": "prometheus",
    "metrics": [
      {
        "name": "checkout_requests_total",
        "type": "counter",
        "unit": "requests",
        "labels": ["job", "method"]
      }
    ]
  }
}
```

Unknown fields, duplicate metrics or labels, unsupported metric types,
oversized input, trailing JSON values, and non-Prometheus domain identity are
rejected. `telemetry-change-guard validate --snapshot <path>` validates and
normalizes the artifact without contacting Prometheus.

## Compare baseline and candidate

```bash
telemetry-change-guard diff \
  --baseline ./main-contract.json \
  --candidate ./candidate-contract.json \
  --output ./telemetry-diff.json \
  --changes-output ./detected-changes.yaml
```

The versioned `tcg-snapshot-diff/v1alpha1` JSON report records:

- metrics added or removed;
- labels added or removed on retained metrics;
- known metric type changes;
- known unit changes;
- type or unit metadata that is available on only one side of the comparison.

Metric and label removals become actionable `metric_remove` and `label_remove`
entries in the generated ChangeSet. A removed metric does not also generate
redundant removals for all of its labels. Additions remain visible in the full
diff but do not create a breaking-change finding.

The initial ChangeSet model intentionally does not guess semantics for metric
type or unit changes. Those differences, and asymmetric semantic metadata
availability, become required diagnostics. `diff` still writes the full report and
actionable ChangeSet, then exits `3`; a direct `check` returns `INCOMPLETE`.
This preserves known removals while preventing incomplete semantic analysis
from passing.

Evaluate a snapshot pair directly:

```bash
telemetry-change-guard check \
  --config ./tcg.yaml \
  --baseline ./main-contract.json \
  --candidate ./candidate-contract.json
```

A pair with no actionable removal produces a valid empty detected ChangeSet.
That permits an authoritative `PASS` when discovery is complete and there are
no diagnostics. It does not make absence in a partial snapshot proof of source
code behavior.

## Weaver as a generic source

```bash
telemetry-change-guard check \
  --config ./tcg.yaml \
  --weaver-diff ./weaver-diff.json \
  --weaver-mapping ./weaver-mapping.yaml
```

Weaver input uses the existing strict V1/V2 parser and explicit
OpenTelemetry-to-Prometheus mapping. Known mapped changes are retained when
another source change lacks a mapping; the missing decision becomes a required
diagnostic and the generic result is `INCOMPLETE`. Telemetry Change Guard does
the same when Weaver reports a valid change without field-level mapping
information. It does not invoke Weaver and never infers backend names.

## Evidence limits

A Prometheus snapshot describes the bounded metadata and series evidence
returned by the queried server. Retention, scrape health, relabeling, tenancy,
federation, and the selected Prometheus deployment determine what is visible.
Teams should capture baseline and candidate from comparable environments and
retain the artifacts in CI.

Telemetry Change Guard does not claim that a snapshot proves every telemetry
signal an application can emit. It does not scan arbitrary application source
code, and it does not infer instrumentation behavior that the telemetry API did
not expose. A checked-in walkthrough is available under
[`examples/snapshot-diff`](../examples/snapshot-diff/README.md).
