# GitHub Action

The canonical composite Action runs the deterministic
`telemetry-change-guard` CLI in either generic change-source mode or migration
compatibility mode. Every invocation performs one authoritative evaluation and
derives its Markdown report, versioned JSON evidence, outputs, job summary, and
optional pull-request comment from that result.

## Generic ChangeSet mode

```yaml
permissions:
  contents: read
  pull-requests: write

steps:
  - uses: actions/checkout@v7
  - id: telemetry
    uses: tadurisaikiran/telemetry-change-guard@v1
    with:
      config: tcg.yaml
      changes: changes.yaml
```

Generic mode preserves the CLI status and exit contract: `PASS` and `WARN`
return `0`, `ERROR` returns `1`, `BLOCK` returns `2`, and `INCOMPLETE` returns
`3`.

Instead of `changes`, a workflow can compare checked-in or previously
generated snapshots:

```yaml
  - id: telemetry
    uses: tadurisaikiran/telemetry-change-guard@v1
    with:
      config: tcg.yaml
      baseline: telemetry/main-contract.json
      candidate: telemetry/candidate-contract.json
```

Mapped Weaver input is also a generic source:

```yaml
    with:
      config: tcg.yaml
      weaver-diff: weaver-diff.json
      weaver-mapping: weaver-mapping.yaml
```

## Migration compatibility mode

```yaml
permissions:
  contents: read
  pull-requests: write

steps:
  - uses: actions/checkout@v7
  - id: telemetry
    uses: tadurisaikiran/telemetry-change-guard@v1
    with:
      config: tcg.yaml
      migration: migration.yaml
```

Migration mode preserves `READY`, `BLOCKED`, `INCOMPLETE`, and `ERROR`, plus
the existing `0`, `2`, `3`, and `1` exit codes respectively. Legacy
`tmr/v1alpha1` configuration remains valid in this mode and generic mode.

Exactly one complete generic source (`changes`, a `baseline`/`candidate` pair,
or a `weaver-diff`/`weaver-mapping` pair) or `migration` is required. Partial
pairs, multiple generic sources, a generic source combined with migration, and
missing sources are configuration `ERROR`s. The Action creates error artifacts
and fails with exit code `1` instead of choosing a mode silently.

## Inputs

| Input | Default | Description |
| --- | --- | --- |
| `config` | `tcg.yaml` | Product configuration relative to the checked-out repository. |
| `changes` | empty | Generic `tcg/v1alpha1` ChangeSet; mutually exclusive with other change sources and `migration`. |
| `baseline` | empty | Baseline `TelemetrySnapshot`; requires `candidate`. |
| `candidate` | empty | Candidate `TelemetrySnapshot`; requires `baseline`. |
| `weaver-diff` | empty | Structured Weaver registry diff; requires `weaver-mapping`. |
| `weaver-mapping` | empty | Explicit backend mapping; requires `weaver-diff`. |
| `migration` | empty | Legacy migration manifest; mutually exclusive with every generic source. |
| `comment` | `"true"` | Create or update one pull-request comment. |
| `artifact-name` | `telemetry-change-guard-report` | Name of the uploaded JSON evidence artifact. |

If the Action is invoked more than once in one job, give each invocation a
different `artifact-name`.

## Outputs and artifacts

| Output | Description |
| --- | --- |
| `status` | Authoritative generic or migration status. |
| `exit-code` | Exact deterministic CLI process code. |
| `report` | Absolute path to the generated Markdown report. |
| `json-report` | Absolute path to the versioned JSON result. |
| `mode` | `generic`, `migration`, or `invalid` for input errors. |

The JSON report is uploaded with `actions/upload-artifact`. The Markdown report
is always appended to the job summary. A missing artifact, an unrecognized
status, or disagreement between status and exit code fails closed.

## Pull-request comments

The Action recognizes both the legacy
`<!-- telemetry-migration-readiness -->` marker and the canonical
`<!-- telemetry-change-guard -->` marker. It updates an existing bot comment
in place and writes the canonical marker, so upgrading does not create a
duplicate comment. Comments are bounded to GitHub's size limit and point to the
full JSON artifact when truncated.

Set `comment: "false"` when pull-request write permission is intentionally
unavailable. Comment delivery is advisory and cannot replace or bypass the
final deterministic enforcement step.

## Legacy repository coordinate

Existing workflows using
`tadurisaikiran/telemetry-migration-readiness@v1` remain supported by the frozen
legacy repository and do not need an immediate edit. The canonical coordinate
is `tadurisaikiran/telemetry-change-guard@v1`; its migration mode provides the
transition path when a team is ready to upgrade.
