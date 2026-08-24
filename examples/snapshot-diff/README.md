# Snapshot change detection example

The baseline contains a request metric with `method` and `status` labels. The
candidate removes the metric and drops the `zone` label from a retained gauge.
It also adds a new metric, which remains visible in the full diff without being
classified as a breaking removal.

```bash
telemetry-change-guard diff \
  --baseline ./examples/snapshot-diff/baseline.json \
  --candidate ./examples/snapshot-diff/candidate.json \
  --output /tmp/telemetry-diff.json \
  --changes-output /tmp/detected-changes.yaml

telemetry-change-guard check \
  --config ./examples/checkout-migration/tcg.yaml \
  --baseline ./examples/snapshot-diff/baseline.json \
  --candidate ./examples/snapshot-diff/candidate.json
```

The paths under `/tmp` are examples only; CI should retain both the full diff
and versioned safety result as build artifacts.
