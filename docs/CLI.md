# Command-line interface

The canonical executable is `telemetry-change-guard`:

```bash
mkdir -p ./bin
go build -o ./bin/telemetry-change-guard ./cmd/telemetry-change-guard
```

The longer name is intentional. The proposed `tcg` command collides with an
existing global executable and a widely used software/security acronym, so it
is not installed or advertised. `TCG` remains acceptable human shorthand for
the product.

## Generic change safety

Validate a native ChangeSet:

```bash
telemetry-change-guard validate --changes ./changes.yaml
```

Evaluate downstream operational impact:

```bash
telemetry-change-guard check \
  --config ./tcg.yaml \
  --changes ./changes.yaml \
  --mode enforce \
  --format console
```

`check` accepts exactly one deterministic change source. In addition to
`--changes`, use either a mapped Weaver diff:

```bash
telemetry-change-guard check \
  --config ./tcg.yaml \
  --weaver-diff ./weaver-diff.json \
  --weaver-mapping ./weaver-mapping.yaml
```

or a baseline/candidate telemetry snapshot pair:

```bash
telemetry-change-guard snapshot \
  --prometheus http://localhost:9090 \
  --output ./candidate-contract.json

telemetry-change-guard check \
  --config ./tcg.yaml \
  --baseline ./main-contract.json \
  --candidate ./candidate-contract.json
```

Use `telemetry-change-guard diff --baseline <path> --candidate <path>` for the
full versioned delta. `--changes-output <path>` additionally writes the
actionable removal ChangeSet. See [change sources](CHANGE_SOURCES.md) for the
snapshot schema, resource bounds, semantic uncertainty, and evidence limits.

`--config` is required. A missing configuration is never interpreted as a
valid empty evidence set. Canonical `tcg/v1alpha1` configuration and existing
`tmr/v1alpha1` configuration normalize to the same internal model. See the
[configuration guide](CONFIGURATION.md) for envelope and environment fallback
rules.

Supported report formats are `console`, `json`, and `markdown`. If `--format`
is omitted, the first configured output format is used. `--output <path>` writes
the report to an explicit file instead of standard output.
`--json-output <path>` writes a companion versioned JSON result from the same
evaluation; it must identify a different file than `--output`. The canonical
Action uses this option to avoid running remote discovery twice.
`--status-output <path>` writes the authoritative status from that evaluation
for integrations that must not infer a top-level decision from nested JSON
fields. All requested output paths must be distinct.

Policy rollout can be selected explicitly:

- `audit` retains every finding and does not enforce configured blocks;
- `warn` retains every finding and downgrades configured blocks to warnings;
- `enforce` applies the default deterministic blocking thresholds.

The default is `enforce`. Invalid modes fail before configuration or source
loading. Rollout changes decisions only; it never removes a finding.

Generic process exit codes are:

| Result | Exit code |
| --- | ---: |
| `PASS`, `WARN` | 0 |
| `ERROR` | 1 |
| `BLOCK` | 2 |
| `INCOMPLETE` | 3 |

## Impact and graph exploration

Explain confirmed dependency paths from one Prometheus metric:

```bash
telemetry-change-guard impact \
  --config ./tcg.yaml \
  --symbol checkout_requests_total
```

Export the complete deterministic graph:

```bash
telemetry-change-guard graph \
  --config ./tcg.yaml \
  --output ./dependency-graph.json
```

Impact exploration is read-only and does not produce a safety status for a
proposed change. Use `check` for an authoritative generic decision. Impact and
canonical graph exploration still return exit `3` when required discovery
evidence is incomplete; they retain any known paths while refusing to present
an empty partial graph as complete.

## Migration compatibility

Migration readiness is available as a first-class nested workflow:

```bash
telemetry-change-guard migration check \
  --config ./tcg.yaml \
  --plan ./migration.yaml
```

Validation, optional explanation, and validated candidate remediation remain
available under `telemetry-change-guard migration validate`, `migration
advise`, and `migration remediate`.

The temporary `tmr` executable remains buildable:

```bash
mkdir -p ./bin
go build -o ./bin/tmr ./cmd/tmr
./bin/tmr analyze --config ./tmr.yaml --migration ./migration.yaml
```

Both entry points call the same `internal/cli` implementation. Given equivalent
arguments, canonical `migration check` and `tmr analyze` produce byte-for-byte
identical reports and the same legacy exit code. The existing
`tmr-result/v1alpha1`, `READY`, `BLOCKED`, `INCOMPLETE`, `ERROR`, and readiness
classification contracts are unchanged.

The canonical GitHub Action supports explicit, snapshot, and mapped-Weaver
generic sources plus migration compatibility mode. Existing
workflows may continue using the frozen legacy repository coordinate without
changes; see the [Action guide](GITHUB_ACTION.md).
