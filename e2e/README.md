# End-to-End Harness

The deterministic control-plane tier runs a shared Kubernetes bundle through
the canonical CLI and verifies the complete machine result:

```bash
./e2e/scripts/run-control-plane-e2e.sh
```

| Scenario | KEDA | Argo Rollouts | HPA mapping | Expected result |
| --- | --- | --- | --- | --- |
| `legacy` | legacy metric/label | legacy metric/label | legacy metric/label | `BLOCK`, six exact findings |
| `migrated` | replacement metric/label | replacement metric/label | replacement metric/label | `PASS`, no findings |
| `incomplete` | dynamic metric identity | dynamic metric identity | missing same-name mapping | `INCOMPLETE`, three required diagnostics |

The verifier asserts every change-to-consumer pair, impact type, criticality,
evidence method, policy decision, diagnostic source, schema version, and exit
code. It also proves that connection addresses do not enter the report. The
same multi-document manifest is presented to all three adapters, exercising
their resource filtering and shared graph/policy integration without requiring
cluster-side controllers.

## Live telemetry lifecycle

This harness experimentally compares Telemetry Change Guard predictions with a
running telemetry stack. It uses a controlled Go exporter, Prometheus, provisioned Grafana, and
Sloth. The normal suite pins:

| Component | Version |
| --- | --- |
| Go builder | 1.26.7 (digest-pinned) |
| Prometheus / promtool | 3.13.2 LTS |
| Grafana | 13.1.3 |
| Sloth | 0.16.0 |
| Tempo | 2.10.5 (digest-pinned) |

Run it from the repository root with Docker Compose v2:

```bash
./e2e/scripts/run-e2e.sh
```

The script builds both canonical and compatibility executables, independently
runs `promtool check rules` for every scenario, runs `promtool test rules`,
validates and generates the Sloth spec, then starts and queries the stack for
each lifecycle stage.
For every migration stage it also requires the explicit migration manifest and
the mapped Weaver V2 diff to produce the same status and exit code.
The first and final live Prometheus stages are captured as telemetry snapshots;
their full diff must detect the old metric removal and new metric addition, the
generated ChangeSet must validate, and generic analysis against migrated
consumers must return `PASS`.

| Scenario | Exporter | Consumers | Expected Telemetry Change Guard | Runtime |
| --- | --- | --- | --- | --- |
| `01-before` | old | old | baseline | healthy |
| `02-dual-write` | old + new | old | `BLOCKED` | healthy |
| `03-partial` | old + new | mixed | `BLOCKED` | healthy |
| `04-uncertain` | old + new | migrated + dynamic critical query | `INCOMPLETE` | healthy |
| `05-migrated` | old + new | new | `READY` | healthy |
| `06-premature-cutover` | new | old | `BLOCKED` | predicted recordings absent |
| `07-legacy-removed` | new | new | `READY` | critical recordings present |

The last two stages prove both directions: Telemetry Change Guard predicts an
observable failure before an unsafe cutover and predicts readiness before a successful cutover.
Each stack uses a fresh Prometheus data volume, so an old time series cannot
hide a broken scenario.

The independent trace tier runs a digest-pinned Tempo and validates the full
TraceQL lifecycle through Tempo's official Search API:

```bash
./e2e/scripts/run-tempo-e2e.sh
```

It requires a critical legacy span attribute query to report `BLOCKED`, the
migrated query to report `READY`, and an expression rejected by Tempo's parser
to fail closed as `INCOMPLETE`. OpenTelemetry-to-Tempo attribute mappings are
explicit in every scenario; no name-based cross-domain match is allowed.

All three E2E tiers run on every pull request. The scheduled compatibility
workflow tests previous supported versions and floating upstream latest tags
without making normal CI non-reproducible.
