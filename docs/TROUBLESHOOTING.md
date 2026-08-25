# Troubleshooting

Start with the exact command, exit code, status, permanent diagnostic code,
and `telemetry-change-guard version --format json`. Remove credentials and
sensitive telemetry before sharing any output.

## Fast diagnosis

| Symptom | Likely cause | What to do |
| --- | --- | --- |
| `telemetry-change-guard: command not found` | Go's binary directory is not on `PATH` | Run `go env GOBIN GOPATH`; use `$(go env GOPATH)/bin/telemetry-change-guard` or set `GOBIN` and reinstall |
| Configuration file not found | Command ran from a different directory | Run from the repository root or make every CLI/config path correct relative to the working directory |
| Exit `2` / `BLOCK` | A known finding violates enforce policy | Read the finding's change, consumer, source, path, criticality, and policy; migrate the consumer or intentionally revise reviewed policy |
| Exit `3` / `INCOMPLETE` | Required evidence is absent, malformed, dynamic, denied, or unresolved | Fix the required source or mapping; do not downgrade it merely to obtain a pass |
| Exit `1` / `ERROR` | Invalid input/configuration or an evaluation failure | Read stderr and diagnostics, validate the ChangeSet/plan separately, then retry |
| `WARN` but exit `0` | Risk is visible but current policy permits it | Review findings; exit `0` does not mean there are no consumers |
| No findings when one was expected | Wrong symbol domain/name, source glob, working directory, hidden target, unsupported consumer, or missing mapping | Run `graph`/`impact`, review loaded sources, and reduce to a sanitized fixture |
| Dynamic Grafana query causes incomplete evidence | Template expansion prevents a static metric identity | Replace it with an exported concrete query, runtime evidence, or another explicit mapping; do not infer by similar names |
| Action cannot comment | Pull-request token permissions or fork policy disallow writes | Keep `comment: "false"`, use the artifact/summary, or grant only the documented permission after security review |
| Remote adapter is denied | Remote evidence is disabled or the trusted origin does not match | Read [Secure CI usage](SECURE_CI_USAGE.md); configure an exact trusted HTTPS origin outside untrusted repository input |

## Confirm inputs before policy

Validate the proposed change first:

```bash
telemetry-change-guard validate --changes ./changes.yaml
```

Then inspect the configured graph without changing policy:

```bash
telemetry-change-guard graph \
  --config ./tcg.yaml \
  --output ./dependency-graph.json

telemetry-change-guard impact \
  --config ./tcg.yaml \
  --symbol checkout_requests_total
```

`graph` and `impact` also exit `3` when required discovery is incomplete. That
is deliberate: retained paths are useful, but a partial graph is not complete
evidence.

## Reduce a reproducible case

Copy only the failing ChangeSet, configuration, and smallest consumer artifact
into a private scratch directory. Replace organization, service, owner,
namespace, endpoint, and query values while preserving the semantic relation.
Record the expected result before rerunning TCG.

For a public bug, use the repository's bug template. Suspected credential
exposure, origin-bypass, arbitrary execution, or other vulnerabilities belong
in [private vulnerability reporting](../SECURITY.md), never a public issue.

## Installation and release failures

No packaged alpha is published yet. A `404` from a documented release archive,
container tag, Homebrew tap, or `v0.1.0-alpha.1` module tag is expected until
the owner authorizes publication. Use the exact commit from
[Installation](INSTALLATION.md). Never work around a missing release with an
unofficial binary.

For a published artifact, stop if its checksum, manifest, SBOM, provenance,
version, or commit does not match. Follow [Verify a release](VERIFY_RELEASE.md)
instead of bypassing verification.
