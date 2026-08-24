# Telemetry Migration Readiness

> Know what will break before changing your telemetry, automatically migrate
> what can be migrated, and know when it is safe to remove backward
> compatibility.

Telemetry changes are API changes. Renaming or removing a metric or label can
silently empty dashboards, disable alerts, and invalidate SLOs. Telemetry
Migration Readiness (`tmr`) is an open-source, local-first tool for analyzing
those migrations before backward compatibility is removed.

## Current status

The project is in its first implementation milestone. The current CLI validates
the canonical migration manifest. Consumer discovery, dependency analysis, and
readiness decisions are roadmap capabilities and are not implemented yet.

Implemented in v0.1 foundation:

- Prometheus-domain metric renames and removals.
- Prometheus-domain label renames and removals.
- Strict YAML decoding with unknown-field rejection.
- Deterministic semantic validation with actionable field paths.
- A local `tmr validate` command.

## Requirements

- Go 1.27 or newer.

## Build and run

```bash
go build -o ./bin/tmr ./cmd/tmr
./bin/tmr validate --migration ./examples/checkout-migration/migration.yaml
```

Successful validation prints:

```text
Migration manifest is valid.
Changes: 2
```

Invalid input is written to standard error and returns a nonzero exit code.

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

## Design principles

- Deterministic analysis owns facts and future safety decisions.
- Parsing or adapter failures must never be interpreted as absence of risk.
- TMR remains useful without an LLM, network connection, database, or hosted
  service.
- Telemetry domains remain separate unless an explicit mapping connects them.
- Every future dependency finding must retain evidence and provenance.

The architecture and milestone boundaries are documented in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). The mandatory verification plan is
documented in [docs/TESTING.md](docs/TESTING.md).

## Development

```bash
gofmt -w .
go vet ./...
go test -race ./...
```

## License

Apache License 2.0. See [LICENSE](LICENSE).
