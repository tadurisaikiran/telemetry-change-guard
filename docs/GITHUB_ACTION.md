# GitHub Action

The canonical composite Action runs the deterministic
`telemetry-change-guard` CLI in either generic change-source mode or migration
compatibility mode. Every invocation performs one authoritative evaluation and
derives its Markdown report, versioned JSON evidence, outputs, job summary, and
optional pull-request comment from that result.

## Pre-release coordinate

The canonical repository does not currently publish a stable release or a
`v1` tag. Until the first pre-release is published, pin the last fully verified
implementation commit. The authoritative candidate coordinate is maintained in
[`release/metadata.env`](../release/metadata.env) and checked against every
README and documentation reference in CI:

```text
tadurisaikiran/telemetry-change-guard@4e211b7571d9a84fde7c6bfe3d92ac43d9ecde3b
```

Do not use `tadurisaikiran/telemetry-change-guard@v1`: that tag does not exist.
The first signed pre-release, checksums, SBOM/provenance, and immutable Action
coordinate are tracked in
[issue #29](https://github.com/tadurisaikiran/telemetry-change-guard/issues/29).
This guide will move to the release tag only after that release is published
and verified.

## Generic ChangeSet mode

```yaml
permissions:
  contents: read
  pull-requests: write

steps:
  - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
  - id: telemetry
    uses: tadurisaikiran/telemetry-change-guard@4e211b7571d9a84fde7c6bfe3d92ac43d9ecde3b
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
    uses: tadurisaikiran/telemetry-change-guard@4e211b7571d9a84fde7c6bfe3d92ac43d9ecde3b
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
  - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
  - id: telemetry
    uses: tadurisaikiran/telemetry-change-guard@4e211b7571d9a84fde7c6bfe3d92ac43d9ecde3b
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
| `remote-evidence` | `disabled` | Explicit remote evidence policy. Enable only in a trusted workflow. |
| `allowed-remote-origins` | empty | Newline-separated exact approved origins. Required when remote evidence is enabled. |
| `allow-insecure-loopback` | `"false"` | Permit credentialed HTTP only for an exact allowlisted loopback development endpoint. |
| `remote-bearer-token` | empty | Dedicated read-only token exposed to analysis only as `TCG_REMOTE_BEARER_TOKEN`. |

If the Action is invoked more than once in one job, give each invocation a
different `artifact-name`.

Remote evidence is deliberately disabled by default because `tcg.yaml` comes
from the analyzed checkout. A trusted workflow must authorize exact origins
independently of that file. The Action clears the analysis environment and
exposes only the fixed remote token when one is supplied, so repository
configuration cannot select another job secret. See [Secure CI usage](SECURE_CI_USAGE.md)
for fork-safe, protected internal, local, comment-permission, and secret
handling examples. Never combine `pull_request_target`, a privileged token,
and execution of an untrusted pull-request checkout.

## Outputs and artifacts

| Output | Description |
| --- | --- |
| `status` | Authoritative generic or migration status. |
| `exit-code` | Exact deterministic CLI process code. |
| `report` | Absolute path to the generated Markdown report. |
| `json-report` | Absolute path to the versioned JSON result. |
| `mode` | `generic`, `migration`, or `invalid` for input errors. |

The JSON report is uploaded with `actions/upload-artifact`. The job summary
records the CLI build identity before appending the Markdown analysis report.
An externally consumed Action records its commit only when `github.action_ref`
is an immutable full SHA; a local `uses: ./` invocation records the workflow
commit. A movable external tag remains `unknown` rather than being mislabeled
with the consumer repository's commit. A missing artifact, an unrecognized
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
legacy repository and do not need an immediate edit. The pre-release canonical
coordinate is the exact commit documented above; its migration mode provides
the transition path when a team is ready to upgrade.
