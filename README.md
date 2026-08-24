# Telemetry Change Guard

> Know what will break before telemetry changes reach production.

Telemetry changes are API changes. Renaming or removing a metric or label can
silently empty dashboards, disable alerts, and invalidate SLOs. Telemetry
Change Guard is an open-source, local-first tool for analyzing those changes
before merge or deployment. The canonical executable is
`telemetry-change-guard`; the existing `tmr` executable remains a supported
migration compatibility entry point during the transition.

## Current status

The deterministic Prometheus v0.1 engine is implemented through the ecosystem
integration milestones. Its local adapters do not require AI, a database, a
network connection, or a hosted service; remote evidence sources are explicit
and optional.

Implemented:

- A strict `tcg/v1alpha1` `ChangeSet` root model with deterministic, deep-copy
  normalization from every supported legacy migration change kind.
- A policy-independent impact layer with immutable, provenance-bearing
  findings for visibility, alerting, SLO, scaling, deployment-gate,
  automation, and semantic risk.
- A versioned `tcg-result/v1alpha1` safety result with deterministic
  `PASS`, `WARN`, `BLOCK`, `INCOMPLETE`, and `ERROR` status precedence.
- Explicit `audit`, `warn`, and `enforce` policy rollout modes that change
  enforcement without suppressing underlying findings.
- A canonical `telemetry-change-guard` CLI for generic checks, validation,
  impact exploration, graph export, and nested migration workflows, backed by
  the same command implementation as the temporary `tmr` compatibility binary.
- Deterministic change sources for explicit ChangeSets, mapped Weaver diffs,
  and bounded Prometheus baseline/candidate telemetry snapshots, with a
  versioned full-diff report and fail-closed semantic uncertainty.
- Strict canonical `tcg/v1alpha1` configuration with transparent
  `tmr/v1alpha1` document normalization and conflict-safe `TCG_*`/`TMR_*`
  environment fallback for configured secret references.
- Prometheus-domain metric renames and removals.
- Prometheus-domain label renames and removals.
- Strict YAML decoding with unknown-field rejection.
- Official Prometheus PromQL AST analysis, including selectors, matchers,
  aggregations, vector matching, and label functions.
- Prometheus rule, PrometheusRule CRD, Grafana, Sloth, and Pyrra adapters.
- Cycle-safe transitive dependency graphs through recording rules.
- Fail-closed `READY`, `BLOCKED`, and `INCOMPLETE` decisions.
- Console, versioned JSON, Markdown, and graph JSON output.
- `analyze`, `advise`, `remediate`, `validate`, `explain`, and `graph` CLI
  commands.
- Optional OpenTelemetry Weaver V1/V2 registry-diff import with mandatory,
  explicit Prometheus backend mappings.
- Optional Perses metrics-usage HTTP evidence for dashboards, recording rules,
  alert rules, partial metrics, and pending usage.
- Optional read-only AI explanations through a provider-neutral local process;
  deterministic readiness remains authoritative.
- Candidate-only AI remediation for local Prometheus YAML and Grafana JSON,
  validated by parsing, adapter reload, graph rebuild, and readiness reanalysis.
- Optional advisory ownership discovery from explicit repository metadata,
  GitHub CODEOWNERS, and conventional Grafana tags.
- Optional runtime PromQL evidence from bounded Prometheus query logs and a
  provider-neutral, versioned JSONL history format.
- Optional Tempo-validated TraceQL evidence for explicitly mapped
  OpenTelemetry span and resource attribute migrations.
- A pinned live Prometheus/Grafana/Sloth migration lifecycle that verifies
  predictions against runtime behavior.

## Requirements

- Go 1.27 or newer.

## Build and run

```bash
go build -o ./bin/telemetry-change-guard ./cmd/telemetry-change-guard
go build -o ./bin/tmr ./cmd/tmr

./bin/telemetry-change-guard validate \
  --changes ./examples/checkout-migration/changes.yaml
./bin/telemetry-change-guard check \
  --config ./examples/checkout-migration/tcg.yaml \
  --changes ./examples/checkout-migration/changes.yaml
```

Successful ChangeSet validation prints:

```text
ChangeSet manifest is valid.
Changes: 2
```

Invalid input is written to standard error and returns a nonzero exit code.

The generic check exit-code contract is `0` for `PASS` or `WARN`, `1` for a
tool/configuration/runtime `ERROR`, `2` for `BLOCK`, and `3` for
`INCOMPLETE`. The migration compatibility commands retain the existing
`READY`/`BLOCKED` contract with the same numeric meanings.

Automatic change detection does not require a handwritten ChangeSet:

```bash
./bin/telemetry-change-guard snapshot \
  --prometheus http://localhost:9090 \
  --output ./candidate-contract.json

./bin/telemetry-change-guard check \
  --config ./tcg.yaml \
  --baseline ./main-contract.json \
  --candidate ./candidate-contract.json
```

See [change sources and telemetry snapshots](docs/CHANGE_SOURCES.md) for the
strict artifact schema, diff command, limits, and evidence boundaries.

## GitHub Action

```yaml
permissions:
  contents: read
  pull-requests: write

steps:
  - uses: actions/checkout@v7
  - uses: tadurisaikiran/telemetry-change-guard@v1
    with:
      config: tcg.yaml
      changes: changes.yaml
```

Use a `baseline`/`candidate` pair or a `weaver-diff`/`weaver-mapping` pair as
alternative generic sources. Use `migration: migration.yaml` instead for the
compatibility workflow. Supplying partial pairs, multiple sources, or no source
fails as a configuration error. The Action
runs one authoritative evaluation, writes the Markdown job summary, uploads a
versioned JSON artifact, and preserves the CLI status and exit code.

Existing `tadurisaikiran/telemetry-migration-readiness@v1` workflows remain
supported by the frozen legacy repository and do not need to migrate
immediately. The canonical Action creates or updates one pull-request comment
by default. See
[the Action documentation](docs/GITHUB_ACTION.md) for inputs, outputs, and
permission details.

## Example manifest

```yaml
apiVersion: telemetry-migration/v1alpha1
kind: Migration
metadata:
  name: checkout-http-migration
spec:
  description: Migrate checkout HTTP duration telemetry.
  changes:
    - id: checkout-duration
      kind: metric_rename
      domain: prometheus
      from:
        metric: checkout_request_duration_seconds
      to:
        metric: checkout_server_request_duration_seconds

    - id: checkout-method
      kind: label_rename
      domain: prometheus
      metric: checkout_server_request_duration_seconds
      from:
        label: http_method
      to:
        label: http_request_method
```

See [the migration model](docs/MIGRATION_MODEL.md) for the complete implemented
schema and validation rules.

The generic `ChangeSet` contract is documented in
[the ChangeSet model guide](docs/CHANGESET.md). The current `tmr` workflow
continues to accept legacy migration manifests unchanged while normalizing them
into that model internally.

Canonical and legacy analysis configuration, defaults, and environment
fallback rules are documented in the
[configuration guide](docs/CONFIGURATION.md).

The generic impact taxonomy, default policy, status precedence, machine schema,
and exit-code contract are documented in
[the safety engine guide](docs/SAFETY_ENGINE.md). Command usage, rollout modes,
and migration compatibility are documented in [the CLI guide](docs/CLI.md).

## Weaver registry diffs

Weaver can be used as a generic change source when an explicit backend mapping
is available:

```bash
telemetry-change-guard check \
  --config ./tcg.yaml \
  --weaver-diff ./weaver-diff.json \
  --weaver-mapping ./weaver-mapping.yaml
```

Telemetry Change Guard never assumes that an OpenTelemetry identifier maps
directly to a Prometheus name. See
[the Weaver integration guide](docs/WEAVER.md).

## Perses metrics-usage evidence

Telemetry Change Guard can augment local discovery from a separately deployed Perses
metrics-usage service:

```yaml
sources:
  persesUsage:
    - url: https://metrics-usage.example.com
      required: true
      timeout: 10s
      bearerTokenEnv: TCG_PERSES_TOKEN
```

The adapter consumes the documented API only; Perses is not a Telemetry Change
Guard dependency. See
[the Perses metrics-usage integration guide](docs/PERSES.md).

## Consumer ownership

An opt-in `ownership` section can enrich blockers and uncertainties from
explicit Telemetry Change Guard metadata, GitHub CODEOWNERS, and Grafana
`team:`/`owner:` tags.
Ownership provenance and ambiguity remain visible in JSON and optional AI
explanations, but ownership never changes readiness. See
[the ownership discovery guide](docs/OWNERSHIP.md).

## Runtime query evidence

Telemetry Change Guard can add observed PromQL executions to configured
dashboards, rules, and SLOs without treating an empty history as proof of
non-use:

```yaml
sources:
  runtimeQueries:
    - path: ./evidence/prometheus-query.log
      format: prometheus_query_log
      window: 168h
      criticality: high
      required: true
```

Each expression is parsed with the official PromQL parser and reported with a
deterministic execution count, first/last observation, origin, and evidence
window. The window is anchored to the newest valid record in the file rather
than the machine clock, so the same input produces the same result later. See
[the runtime query evidence guide](docs/RUNTIME_EVIDENCE.md).

## Tempo and TraceQL evidence

Telemetry Change Guard can analyze strict local TraceQL consumer manifests
while asking a configured Tempo deployment to validate each expression with
its official parser:

```yaml
sources:
  tempoQueries:
    - url: https://tempo.example.com
      path: ./trace-queries/*.yaml
      required: true
      timeout: 60s
      bearerTokenEnv: TCG_TEMPO_TOKEN
mappings:
  traceAttributes:
    - scope: span
      opentelemetry: http.request.method
      tempo: http.method
```

OpenTelemetry and Tempo remain separate domains; similar attribute names do
not create a dependency. Required validation, mapping, or source failures stop
`READY`. See [the Tempo and TraceQL guide](docs/TEMPO.md).

## Optional AI explanations

AI is disabled unless `tmr advise` is given an explicit local provider
executable:

```bash
tmr advise \
  --config ./tmr.yaml \
  --migration ./migration.yaml \
  --question "Why is this blocked, and what should migrate first?" \
  --ai-command ./my-tmr-ai-provider
```

Telemetry Change Guard sends a bounded, redacted JSON evidence packet over
standard input and accepts one strict JSON explanation on standard output. The provider cannot
return a readiness status or a patch. `advise` preserves the deterministic
exit code, so a useful explanation of a blocked migration still exits `2`.
See [the AI explanation protocol](docs/AI_AGENT.md) and
[threat model](docs/THREAT_MODEL.md).

## Validated candidate remediation

An explicit provider can propose a replacement for a confirmed local legacy
expression:

```bash
tmr remediate \
  --config ./tmr.yaml \
  --migration ./migration.yaml \
  --ai-command ./my-tmr-ai-provider
```

Telemetry Change Guard labels output as a validated candidate only after the
official PromQL parser proves the legacy reference is gone and the destination is present, the
in-memory YAML or Grafana JSON artifact reparses through its adapter, and the
dependency graph/readiness engine succeeds on the simulated artifact. It never
writes the source file. See [the remediation protocol](docs/REMEDIATION.md).

## Design principles

- Deterministic analysis owns facts and safety decisions.
- Parsing or adapter failures must never be interpreted as absence of risk.
- Telemetry Change Guard remains useful without an LLM, network connection,
  database, or hosted service.
- AI output is explanatory and can neither weaken evidence nor change status.
- Telemetry domains remain separate unless an explicit mapping connects them.
- Every dependency finding retains evidence and provenance.

The architecture and milestone boundaries are documented in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). The mandatory verification plan is
documented in [docs/TESTING.md](docs/TESTING.md).

For a problem-first explanation of the migration lifecycle, read
[When Telemetry Migrations Fail Silently](docs/articles/when-telemetry-migrations-fail-silently.md).

See [the roadmap](docs/ROADMAP.md), [contribution guide](CONTRIBUTING.md), and
[security policy](SECURITY.md) before proposing or reporting work.

Engineers evaluating a real migration can use the
[design-user program guide](docs/DESIGN_USER_PROGRAM.md) and submit only
sanitized findings through the design-user feedback issue form.

## Development

```bash
gofmt -w .
go vet ./...
go test -race ./...
```

## License

Apache License 2.0. See [LICENSE](LICENSE).
