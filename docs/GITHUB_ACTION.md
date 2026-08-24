# GitHub Action

Telemetry Change Guard ships as a composite action that builds the pinned Go
module, analyzes the repository's local artifacts, writes a Markdown job summary, and optionally
creates or updates one pull-request comment.

The Action recognizes both the legacy `telemetry-migration-readiness` comment
marker and the current `telemetry-change-guard` marker. It updates either
existing bot comment in place and writes the current marker, so an upgrade does
not create a duplicate comment.

```yaml
permissions:
  contents: read
  pull-requests: write

steps:
  - uses: actions/checkout@v5
  - uses: tadurisaikiran/telemetry-migration-readiness@v1
    with:
      config: tmr.yaml
      migration: migration.yaml
```

The legacy repository coordinate remains the supported release target until a
Telemetry Change Guard Action release completes external validation.

The action exposes `status`, `exit-code`, and `report` outputs, then preserves
the CLI contract: blocked migrations fail with code 2 and incomplete required
evidence fails with code 3. Set `comment: "false"` when pull-request write
permission is unavailable; the job summary is always produced.
