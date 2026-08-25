# Secure CI usage

Telemetry Change Guard (TCG) reads repository-controlled configuration and can
optionally contact remote evidence services. Treat those as separate trust
domains whenever a workflow also has credentials.

The safe default is simple:

> Pull-request analysis uses local repository evidence with remote evidence
> disabled. A credentialed remote source is enabled only by a trusted workflow,
> constrained to exact approved origins, and given only a dedicated read-only
> token.

This document describes the enforced controls and the workflow responsibilities
that code cannot infer on its own.

## Security model

The composite Action defaults to:

```yaml
remote-evidence: disabled
allow-insecure-loopback: "false"
```

In this mode, Perses and Tempo requests are not attempted. A configured required
remote source produces an authoritative incomplete diagnostic; it cannot become
`PASS` or legacy `READY`. An optional denied source remains visible as an
advisory diagnostic and does not erase findings from local evidence.

Enabling remote evidence in the Action requires all of the following:

- `remote-evidence: enabled`;
- at least one exact origin in `allowed-remote-origins`;
- HTTPS for every credentialed origin;
- a dedicated token passed through `remote-bearer-token`, when authentication
  is required; and
- `bearerTokenEnv: TCG_REMOTE_BEARER_TOKEN` in the matching source definition.

The Action launches analysis with a cleared environment and exposes only the
fixed `TCG_REMOTE_BEARER_TOKEN` value when supplied. A pull-request-controlled
`bearerTokenEnv` therefore cannot select another job environment variable.

TCG also enforces these adapter boundaries:

- URL user information, query strings, and fragments are rejected;
- allowed destinations use canonical exact origins, including explicit ports;
- default ports and hostname case are normalized before comparison;
- wildcard and suffix origin matching are not supported;
- redirects are bounded and cannot leave the configured origin;
- bearer values are not included in reports or adapter error messages; and
- plaintext bearer authentication is rejected, except for an explicitly
  enabled and allowlisted loopback development endpoint.

An origin is the scheme, hostname, and effective port—for example,
`https://tempo.example.com` or `https://perses.example.com:8443`. A path such
as `/api` is not part of an allowed-origin value.

## Public repositories and fork pull requests

Fork pull requests are untrusted. Do not expose repository, organization, or
environment secrets to their checkout. Keep remote evidence disabled and use
read-only permissions:

```yaml
name: Telemetry safety

on:
  pull_request:

permissions:
  contents: read

jobs:
  telemetry-change-guard:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@<FULL_COMMIT_SHA> # pin the reviewed release
      - uses: tadurisaikiran/telemetry-change-guard@<FULL_COMMIT_SHA>
        with:
          config: tcg.yaml
          changes: changes.yaml
          remote-evidence: disabled
          comment: "false"
```

Replace each placeholder with the full reviewed commit SHA. The release guide
will provide immutable coordinates after an owner-approved alpha exists.

With `pull-requests` permission omitted, the Action cannot create a PR comment.
The Markdown job summary, JSON artifact, output status, and authoritative job
result remain available.

Do not assume a secret is safe merely because GitHub normally withholds it from
forks. Repository configuration should still be evaluated as attacker
controlled, and the workflow should remain safe if permissions or event
behavior change later.

## Private repositories with trusted contributors

For a trusted internal workflow, prefer local evidence first. Enable remote
evidence only when it materially improves the decision and the service has a
dedicated read-only credential.

Store the approved origin in a repository or environment variable managed
through GitHub settings, not in `tcg.yaml`:

```text
TCG_ALLOWED_REMOTE_ORIGINS=https://tempo.example.com
```

Store the token as an environment or repository secret:

```text
TCG_REMOTE_BEARER_TOKEN=<read-only service token>
```

The source configuration identifies the fixed Action token name but cannot
authorize its destination:

```yaml
apiVersion: tcg/v1alpha1
kind: Config
sources:
  tempoQueries:
    - url: https://tempo.example.com
      path: ./observability/trace-queries.yaml
      bearerTokenEnv: TCG_REMOTE_BEARER_TOKEN
      required: true
```

A protected workflow can then supply the independent policy:

```yaml
permissions:
  contents: read

jobs:
  telemetry-change-guard:
    environment: telemetry-evidence
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@<FULL_COMMIT_SHA> # pin the reviewed release
      - uses: tadurisaikiran/telemetry-change-guard@<FULL_COMMIT_SHA>
        with:
          config: tcg.yaml
          changes: changes.yaml
          remote-evidence: enabled
          allowed-remote-origins: ${{ vars.TCG_ALLOWED_REMOTE_ORIGINS }}
          remote-bearer-token: ${{ secrets.TCG_REMOTE_BEARER_TOKEN }}
          comment: "false"
```

Use a protected GitHub environment when the repository's contributor model
requires approval before a credentialed job runs. A repository variable is
independent of `tcg.yaml`, but a workflow file on a pull-request branch can also
be changed. Branch protection, environment approval, CODEOWNERS review, or a
trusted reusable workflow must protect the workflow-to-secret boundary.

If contributors are not trusted to modify workflow files, do not make a secret
available to a workflow definition taken from their branch. Run credentialed
remote discovery only from protected workflow code and a reviewed commit.

## Local execution with no remote evidence

Remote evidence is disabled by default. Keep the intent explicit in scripts:

```bash
telemetry-change-guard check \
  --config ./tcg.yaml \
  --changes ./changes.yaml \
  --remote-evidence disabled
```

Required remote sources make this result incomplete. Optional sources produce
visible advisory diagnostics. Local alerts, dashboards, SLOs, autoscalers, and
deployment-gate findings are still analyzed.

## Local execution with remote evidence

Use an exact HTTPS origin and a dedicated token:

```bash
export TCG_REMOTE_BEARER_TOKEN='<read-only-token>'

telemetry-change-guard check \
  --config ./tcg.yaml \
  --changes ./changes.yaml \
  --remote-evidence enabled \
  --allowed-remote-origin https://tempo.example.com
```

Repeat `--allowed-remote-origin` for each explicitly trusted Perses or Tempo
origin. If a configuration references a bearer-token environment variable and
the origin is not allowlisted, TCG fails closed before reading or sending the
token.

Every enabled remote source, including an unauthenticated local development
service, requires an exact trusted-origin allowlist supplied on the command
line. Repository configuration can never authorize its own destination.

## Plaintext loopback development exception

Bearer authentication over HTTP is never appropriate for a remote host. For a
local test server only, all three conditions are required:

1. the host is exactly `localhost` or a loopback IP literal;
2. the origin is explicitly allowlisted; and
3. `--allow-insecure-loopback` is supplied.

```bash
telemetry-change-guard check \
  --config ./tcg.local.yaml \
  --changes ./changes.yaml \
  --remote-evidence enabled \
  --allowed-remote-origin http://127.0.0.1:3200 \
  --allow-insecure-loopback
```

Names such as `localhost.example.com` are not loopback exceptions. Do not use
this option on a shared runner.

The snapshot command applies the same transport rule. Its Prometheus URL is a
direct CLI input rather than repository configuration:

```bash
telemetry-change-guard snapshot \
  --prometheus https://prometheus.example.com \
  --bearer-token-env TCG_PROMETHEUS_TOKEN \
  --output ./baseline.json
```

## PR comments and permissions

The Action's authoritative behavior does not depend on comment creation.
Comment failures remain advisory; the deterministic status and exit code decide
the job.

For no-comment mode, grant only:

```yaml
permissions:
  contents: read
```

To enable comments on trusted same-repository pull requests, grant the job:

```yaml
permissions:
  contents: read
  pull-requests: write
```

and set:

```yaml
with:
  comment: "true"
```

Do not grant write permissions at workflow scope when only one job needs them.
Fork pull requests commonly receive a read-only token, so use no-comment mode
for predictable behavior.

## Secrets and repository variables

- Use one read-only credential per evidence service and repository or
  environment boundary.
- Do not reuse deployment, cloud-administration, or write-capable credentials.
- Pass the Action token only through `remote-bearer-token`.
- In Action-managed configs, use exactly
  `bearerTokenEnv: TCG_REMOTE_BEARER_TOKEN`.
- Keep approved origins in protected repository/environment variables or a
  trusted reusable workflow.
- Never put a token in a URL, query parameter, config file, report path, command
  argument, fixture, or artifact.
- Rotate the credential if a workflow, runner, or repository boundary is
  suspected to be compromised.
- Review workflow logs and artifacts before sharing them externally even though
  TCG does not intentionally render token values.

Repository variables are not secrets. They are suitable for approved origins,
not tokens.

## Never combine `pull_request_target` with an untrusted checkout

Do not use `pull_request_target` to gain secrets or write permissions and then
check out or execute the contributor's head commit. That pattern runs untrusted
content in a privileged context.

In particular, do not:

```yaml
on: pull_request_target

steps:
  - uses: actions/checkout@<FULL_COMMIT_SHA>
    with:
      ref: ${{ github.event.pull_request.head.sha }}
  - run: ./script-from-the-pull-request.sh
    env:
      TOKEN: ${{ secrets.SENSITIVE_TOKEN }}
```

Use an ordinary `pull_request` workflow with remote evidence disabled, or use a
separate protected process that never executes the untrusted checkout.

## Failure interpretation

| Condition | Required source | Optional source |
| --- | --- | --- |
| Remote evidence disabled | Incomplete unless a confirmed block already determines the legacy migration result | Advisory diagnostic |
| Origin not allowlisted | Incomplete | Advisory diagnostic |
| Credentialed HTTP URL | Incomplete | Advisory diagnostic |
| Missing token | Incomplete | Advisory diagnostic |
| Cross-origin redirect | Incomplete | Advisory diagnostic |
| Invalid execution policy | `ERROR` | `ERROR` |

Confirmed findings remain in the result in every case. A denied or incomplete
remote source does not erase a known alert, SLO, scaling, or rollout-gate risk.
