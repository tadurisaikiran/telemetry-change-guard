# Quickstart

This guide takes a new user from installation to a useful, reproducible policy
result. It uses a deliberately unsafe metric removal so the first successful
run demonstrates a `BLOCK` instead of an empty pass.

## Prerequisites

- Git;
- Go `1.26.7` or newer; and
- macOS, Linux, or Windows for the CLI. The prepared container is Linux-only.

No database, hosted service, AI provider, or production credential is needed.

## 1. Install the reviewed alpha candidate

Until an owner publishes `v0.1.0-alpha.1`, install the exact verified commit:

```bash
go install github.com/tadurisaikiran/telemetry-change-guard/cmd/telemetry-change-guard@626f5443021f14a8a4a3ddb67e8b7af6e92afed8
telemetry-change-guard version --format json
```

If the command is not found, add `$(go env GOPATH)/bin` to `PATH`, or set
`GOBIN` explicitly before installation. Do not substitute `@latest`, `@main`,
`@v1`, or an unofficial binary.

## 2. Get the matching example

```bash
git clone https://github.com/tadurisaikiran/telemetry-change-guard.git
cd telemetry-change-guard
git checkout 626f5443021f14a8a4a3ddb67e8b7af6e92afed8
```

Run all following commands from this repository root. Paths in `tcg.yaml` are
resolved from the current working directory, not from the config file's
directory.

## 3. Understand the three inputs

| File | Meaning |
| --- | --- |
| [`examples/getting-started/changes.yaml`](../examples/getting-started/changes.yaml) | The proposed removal of `checkout_requests_total` |
| [`examples/getting-started/tcg.yaml`](../examples/getting-started/tcg.yaml) | Required evidence sources, analysis behavior, and policy |
| [`examples/getting-started/prometheus/rules.yaml`](../examples/getting-started/prometheus/rules.yaml) | A critical alert that still consumes the metric |

The ChangeSet answers “what will change?” The configuration answers “where
should TCG look and how should it decide?” The consumer artifacts answer “what
currently uses that contract?”

## 4. Validate, then check

```bash
telemetry-change-guard validate \
  --changes ./examples/getting-started/changes.yaml

telemetry-change-guard check \
  --config ./examples/getting-started/tcg.yaml \
  --changes ./examples/getting-started/changes.yaml \
  --mode enforce \
  --format console \
  --json-output ./tcg-result.json
```

Validation exits `0`. The check exits `2` and reports:

```text
Status:    BLOCK
Findings:  1

[BLOCK] ALERTING_RISK — CheckoutTrafficMissing
  Source:      examples/getting-started/prometheus/rules.yaml:4

STATUS: BLOCK
```

Exit `2` is a successful safety decision: the configured policy rejects the
proposed change. `tcg-result.json` contains the versioned
`tcg-result/v1alpha1` machine contract for CI and review tooling.

## 5. Apply the pattern to your repository

Start with one explicit, reviewable ChangeSet and one or two local consumer
types. A minimal configuration for Prometheus rules is:

```yaml
apiVersion: tcg/v1alpha1
kind: Config
sources:
  prometheusRules:
    - path: ./observability/rules/*.yaml
      required: true
analysis:
  includeTransitiveDependencies: true
  unresolvedReferencePolicy: error
policy:
  failOnCriticalLegacyConsumer: true
  failOnCriticalUnknown: true
  minimumBlockingCriticality: high
output:
  formats: [console, json]
```

Mark evidence `required: true` when its absence must prevent a safe result.
Run locally in `audit` or `warn` mode while tuning sources and ownership, then
move to `enforce` only after reviewing representative safe, blocking, and
incomplete changes.

## 6. Add the pull-request check

Pin the immutable Action commit:

```yaml
permissions:
  contents: read

steps:
  - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
  - uses: tadurisaikiran/telemetry-change-guard@626f5443021f14a8a4a3ddb67e8b7af6e92afed8 # v0.1.0-alpha.1 candidate
    with:
      config: tcg.yaml
      changes: changes.yaml
      mode: enforce
      comment: "false"
```

Read [Secure CI usage](SECURE_CI_USAGE.md) before enabling remote evidence or
credentials. The Action uploads a bounded report artifact and preserves the
CLI's authoritative status/exit behavior.

## Next steps

- [Configuration](CONFIGURATION.md) defines every source and policy field.
- [Change sources](CHANGE_SOURCES.md) covers snapshots and Weaver diffs.
- [Adapters](ADAPTERS.md) lists supported consumer evidence.
- [Troubleshooting](TROUBLESHOOTING.md) diagnoses common setup failures.
- [Limitations](LIMITATIONS.md) states what the alpha does not establish.
