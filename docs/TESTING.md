# Testing and Verification

Testing is part of the safety contract. A parser or adapter error must never be
mistaken for evidence that a dependency does not exist.

## Implemented test layers

The current foundation has:

- unit tests for every required migration validation rule;
- valid YAML fixtures covering all four implemented change kinds;
- invalid YAML fixtures with exact golden diagnostics;
- CLI tests for output and exit behavior;
- CI checks for formatting, vetting, and race-enabled tests.

Run the local checks with:

```bash
gofmt -w .
go vet ./...
go test -race ./...
```

## Mandatory future live E2E release gate

The live harness is not implemented in the current milestone. It is mandatory
before TMR is described as stable.

The harness will run a pinned Docker Compose stack containing a controlled
exporter, Prometheus, Grafana, and Sloth, with Pyrra added as a second tier. It
will exercise this telemetry lifecycle:

```text
old only -> dual write -> partial consumer migration
         -> complete consumer migration -> old telemetry removed
```

It must prove both prediction directions:

1. TMR reports `BLOCKED` before an intentionally premature cutover, and the
   isolated stack exhibits the predicted missing critical data.
2. TMR reports `READY` only after every required consumer is migrated, and the
   same critical queries, rules, dashboards, and SLOs continue working after
   legacy telemetry is removed.

The stable release gate will also require:

- direct metric and label dependency detection;
- Grafana, alert, recording-rule, and SLO consumer discovery;
- complete transitive propagation through recording rules;
- fail-closed behavior for unresolved critical queries and required adapter
  failures;
- no panic or false safety result for malformed PromQL or graph cycles;
- independent `promtool` checks and rule tests;
- Sloth validation and generated-rule cross-checks;
- proof that optional AI output cannot override deterministic readiness.

Pinned E2E tests will run on pull requests. Compatibility matrices and latest
upstream versions will run on scheduled workflows after the harness exists.
