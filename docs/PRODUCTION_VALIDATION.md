# Production-style validation

Telemetry Change Guard uses production-style canaries without claiming access
to a customer's production systems. The gates exercise real binaries, strict
machine contracts, pinned upstream services, failure paths, and distributable
artifacts against controlled evidence whose correct result is known in
advance.

No single fixture proves universal telemetry coverage. A result is only as
complete as the evidence configured for that run. Required missing, malformed,
or unresolved evidence must fail closed.

## Primary product canaries

Run the two user journeys that must remain shareable:

```bash
make canary
```

The command builds the public CLI unless `TCG_BINARY` names an executable, then
checks all four transitions:

| Journey | Canary state | Required result |
| --- | --- | --- |
| Protect a proposed change | A critical alert still reads the removed metric | `BLOCK`, exit `2`, exact `ALERTING_RISK` finding for `CheckoutTrafficMissing` |
| Protect a proposed change | The declared change has no affected configured consumer | `PASS`, exit `0`, no findings |
| Plan a migration | Five consumers still use the legacy metric | `BLOCKED`, exit `2`, `0%` progress |
| Plan a migration | All five consumers use the replacement metric | `READY`, exit `0`, `100%` progress |

Each case also verifies the authoritative status artifact and the exact
versioned JSON schema. This catches incorrect results, not merely crashes.
The same canary target also runs deterministic local-provider checks proving
that `migration advise` cannot alter `BLOCKED`/exit `2` and that `migration
remediate` validates a candidate only in memory without changing its source
file. These canaries run on every pull request and push to `main`.

## Validation map

| Implemented surface | Production-style evidence |
| --- | --- |
| ChangeSet validation and generic `check` | Strict parser/golden/fuzz tests, CLI integration, benchmark corpus, external-consumer fixtures, and the proposed-change canary |
| Prometheus snapshot and `diff` change source | Parser and semantic-diff tests, hosted Action snapshot mode, external-consumer snapshot contract, and live Prometheus lifecycle |
| Mapped OpenTelemetry Weaver change source | V1/V2 parser and mapping tests, fuzzing, and the live lifecycle rerun with a mapped V2 diff |
| `impact` and `graph` | Direct, transitive, cyclic, unresolved, ordering, limit, and CLI contract tests |
| Migration readiness | Truth-table and golden tests, the `BLOCKED -> READY` primary canary, and a seven-stage live lifecycle including premature cutover |
| Prometheus rules, Grafana, and Sloth | Component fixtures plus pinned live Prometheus/Grafana/Sloth validation and runtime query assertions |
| Pyrra | Deterministic component fixtures and parser/integration tests; not a live Pyrra service canary |
| KEDA, Argo Rollouts, and mapped HPA | Exact combined control-plane `BLOCK -> PASS -> INCOMPLETE` lifecycle |
| Runtime query history | Bounded decoder/fuzz tests and integration cases proving observed legacy use blocks while empty history cannot erase static findings |
| Tempo and TraceQL | Parser/fuzz/component tests plus a digest-pinned live `BLOCKED -> READY -> INCOMPLETE` Tempo lifecycle |
| Perses usage | Bounded remote-response component tests and strict pending/partial evidence behavior; not a live Perses deployment canary |
| Ownership | CODEOWNERS and metadata parser, precedence, ambiguity, determinism, integration, and fuzz tests; ownership cannot change readiness |
| CloudFormation and Cloud Assembly loading | Bounded parser, manifest, containment, and fuzz tests only; CloudWatch safety decisions are explicitly not implemented |
| GitHub Action | Hosted explicit, snapshot, and migration matrix; external immutable-coordinate consumer matrix verifies artifacts, status, exit, and schema |
| Optional AI explanation and remediation | Adversarial provider-process and CLI tests prove bounded protocols, unchanged status/exit authority, validated in-memory candidates, and no source edit |
| Experimental agentic loop | Unit, fuzz, sandbox, integrity, state-machine, and local Docker fixture proving `BLOCK -> repair -> PASS` with an uncommitted review diff |
| Archives and source distribution | Two clean multi-platform builds with byte-identical public checksums, strict payload verification, native host smoke, SBOMs, and source archive checks |
| OCI container | Multi-architecture OCI verification, non-root distroless runtime, shell absence, read-only/no-network execution, labels, SPDX SBOM, and SLSA provenance |

The complete test inventory and threat-focused cases are in
[Testing and Verification](TESTING.md). Adapter-specific boundaries are in the
[adapter guide](ADAPTERS.md) and [limitations](LIMITATIONS.md).

## Release-candidate sequence

Run these gates from a clean checkout with the repository's required Go and
Docker versions:

```bash
make verify
make canary
make e2e
make release-reproducible
make container-snapshot
```

- `make verify` covers deterministic, race, fuzz-smoke, vulnerability,
  workflow, documentation, distribution-contract, and benchmark gates.
- `make canary` isolates the two primary user journeys for a fast signal.
- `make e2e` starts pinned Prometheus, Grafana, Sloth, Tempo, and controlled
  exporter services while also exercising control-plane manifests.
- `make release-reproducible` performs two non-publishing release builds and
  requires byte-identical public payloads.
- `make container-snapshot` builds but does not publish the OCI image.

On GitHub, an absent or skipped check is not evidence of readiness. Pull
requests must report the protected `test`, `live-lifecycle`, and
`trace-lifecycle` contexts before merge. The manually dispatchable external
consumer, compatibility, release-snapshot, and container-snapshot workflows
provide additional hosted evidence for an exact candidate commit; they do not
replace the protected pull-request contexts. If GitHub fails to deliver a pull
request event, a manual Compatibility run reports those same three protected
contexts only after rerunning their complete test and lifecycle commands.

The optional agentic fixture has additional prerequisites and an explicit
experimental acknowledgement; follow its
[five-minute guide](../experiments/agentic/README.md#five-minute-local-fixture).

## What these gates do not prove

- They do not prove that an organization's configuration includes every
  repository, dashboard, runtime query, or remote system.
- They do not execute a customer's production queries or mutate production.
- They do not prove semantic equivalence of a human- or AI-authored query.
- They do not make experimental agentic behavior a supported release feature.
- They do not turn roadmap adapters or unsupported domains into implemented
  safety decisions.

Before enforcing TCG in a real repository, start in `audit` or `warn`, require
the evidence sources your team considers authoritative, review `INCOMPLETE`
diagnostics, and compare findings with owner knowledge during a controlled
evaluation.
