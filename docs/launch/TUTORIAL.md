# Tutorial: protect a metric removal before merge

> **Draft tutorial.** The immutable commit path works today; replace it with a
> published release coordinate only after the owner-approved release is
> independently verified.

This tutorial starts with the included alert fixture, explains each input, and
then shows how to adapt the same check to a repository. Allow 10–15 minutes.

## Prerequisites

- Git;
- Go 1.26.7 or newer; and
- a shell from the repository root.

No Kubernetes cluster, Prometheus server, database, AI provider, or credential
is required.

## Install and identify the exact build

```bash
go install github.com/tadurisaikiran/telemetry-change-guard/cmd/telemetry-change-guard@90c0b41629b76f67f6bfda42ad88eb93bd275328
telemetry-change-guard version --format json
```

If the executable is not on `PATH`, invoke
`$(go env GOPATH)/bin/telemetry-change-guard` or reinstall with an explicit
`GOBIN`.

## Check out matching inputs

```bash
git clone https://github.com/tadurisaikiran/telemetry-change-guard.git
cd telemetry-change-guard
git checkout 90c0b41629b76f67f6bfda42ad88eb93bd275328
```

The fixture has three inputs:

```text
examples/getting-started/
├── changes.yaml             proposed metric removal
├── tcg.yaml                 sources + analysis + policy
└── prometheus/rules.yaml    current critical alert
```

Open `changes.yaml`. It declares a `metric_remove` for the Prometheus metric
`checkout_requests_total`.

Open `tcg.yaml`. The Prometheus rules glob is `required: true`, transitive
analysis is enabled, unresolved references are errors, and high-or-greater
blocking policy is enforced.

Open `prometheus/rules.yaml`. `CheckoutTrafficMissing` still evaluates
`checkout_requests_total` and is labeled critical.

## Validate structure

```bash
telemetry-change-guard validate \
  --changes ./examples/getting-started/changes.yaml
```

Expected exit: `0`.

```text
ChangeSet manifest is valid.
Changes: 1
```

Validation establishes that the accepted ChangeSet is structurally valid. It
does not inspect consumer evidence and does not prove the author listed every
telemetry change.

## Run the safety check

```bash
set +e
telemetry-change-guard check \
  --config ./examples/getting-started/tcg.yaml \
  --changes ./examples/getting-started/changes.yaml \
  --mode enforce \
  --format console \
  --json-output ./tcg-result.json
status=$?
set -e
printf 'exit=%s\n' "${status}"
```

The important output is:

```text
Status:    BLOCK
Findings:  1

[BLOCK] ALERTING_RISK — CheckoutTrafficMissing
  Criticality: critical
  Source:      examples/getting-started/prometheus/rules.yaml:4

STATUS: BLOCK
exit=2
```

The source line and dependency path explain why policy blocked the change.
Exit `2` is a normal decision contract, not a failed invocation.

Inspect the machine report:

```bash
sed -n '1,160p' ./tcg-result.json
```

It uses `tcg-result/v1alpha1` and carries the same top-level `BLOCK` status.
Automation should consume this versioned schema and preserve exit `2`.

## Make the fixture safe

In a scratch copy, update the alert expression to a reviewed replacement metric
that also appears in a matching proposed rename/migration plan. Do not simply
delete a required consumer to make the check pass. Rerun validation and the
check, then review the entire report.

For staged old/new cutovers, use `migration validate` and `migration check`
instead of representing the workflow only as removal. The migration report
classifies consumers as legacy, dual, migrated, or uncertain according to the
documented model.

## Adapt it to your repository

Create `changes.yaml` for one planned change. Add a minimal `tcg.yaml`:

```yaml
apiVersion: tcg/v1alpha1
kind: Config
sources:
  prometheusRules:
    - path: ./observability/prometheus/*.yaml
      required: true
  grafana:
    - path: ./observability/grafana/*.json
      required: true
analysis:
  includeTransitiveDependencies: true
  unresolvedReferencePolicy: error
policy:
  failOnCriticalLegacyConsumer: true
  failOnCriticalUnknown: true
  minimumBlockingCriticality: high
output:
  formats: [console, json, markdown]
```

Run from the directory those paths are relative to. Start with local checked-in
evidence. Add optional remote sources only after reviewing
[Secure CI usage](../SECURE_CI_USAGE.md).

Use rollout stages deliberately:

1. `audit` to inventory findings without enforcement;
2. `warn` to show policy outcomes while allowing the workflow; and
3. `enforce` when source completeness and thresholds have been reviewed.

Changing mode never deletes a finding.

## Add GitHub Actions

```yaml
name: Telemetry contract

on:
  pull_request:

permissions:
  contents: read

jobs:
  guard:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: tadurisaikiran/telemetry-change-guard@90c0b41629b76f67f6bfda42ad88eb93bd275328 # v0.1.0-alpha.1 candidate
        with:
          config: tcg.yaml
          changes: changes.yaml
          mode: enforce
          comment: "false"
```

The Action creates a summary and evidence artifact without needing write
permission. Enable pull-request comments only after reviewing token permissions
and fork behavior.

## Interpret the result before enforcing it

- Confirm every intended source loaded.
- Review `INCOMPLETE` diagnostics instead of changing sources to optional for
  convenience.
- Confirm symbol domains and explicit mappings.
- Test a known safe case, a known blocking case, and a missing-evidence case.
- Record the exact commit and configuration used.
- Keep deployment monitoring and rollback; TCG is a review-time control.

Continue with [Configuration](../CONFIGURATION.md),
[Adapters](../ADAPTERS.md), [Troubleshooting](../TROUBLESHOOTING.md), and the
[design-user protocol](../DESIGN_USER_PROGRAM.md).
