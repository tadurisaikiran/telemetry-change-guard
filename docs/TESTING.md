# Testing and Verification

Testing is part of the safety contract. A parser or adapter error must never be
mistaken for evidence that a dependency does not exist.

The optional AI explanation boundary is tested as an adversarial protocol:
status fields and unknown consumer IDs are rejected, provider timeouts and
oversized output are bounded, secrets are redacted, terminal controls are
removed, stable risk ordering is asserted, and CLI tests prove that model prose
cannot change readiness exit codes.

Candidate remediation adds independent query, YAML, dashboard, and full graph
tests. Adversarial cases cover invalid PromQL, retained legacy references,
missing destinations, secret-like provider text, duplicate or unknown targets,
ambiguous artifact scalars, response status/patch claims, timeouts, oversized
process output, and proof that source files remain byte-for-byte unchanged.

The experimental agentic boundary has strict-protocol, fuzz, state-machine,
process, sandbox-invocation, integrity, Git-containment, and public-CLI contract
tests. They cover `BLOCK -> repair -> PASS`, retry exhaustion, `INCOMPLETE`,
`ERROR`, adapter timeout, bounded process output, malformed and authority-
claiming responses, TCG JSON/status/exit disagreement, control-file tampering,
workspace escape, source-checkout immutability, exactly one writable bind mount,
an immutable image ID, and hardened container flags. The fully local fixture
under `experiments/agentic/testdata` exercises the real public TCG binary and
Docker adapter. `make agentic-canary` asserts the exact `BLOCK -> repair ->
PASS` lifecycle and source-checkout immutability; it is included in `make e2e`.
See its [quickstart](../experiments/agentic/README.md#five-minute-local-fixture).

Ownership discovery has parser, wildcard, source-order, last-match, precedence,
joint-owner, ambiguity, determinism, and fuzz coverage. An integration invariant
compares ownership-disabled, valid, and malformed runs and requires identical
readiness summaries; malformed ownership diagnostics must remain advisory.

Runtime query evidence has decoder, aggregation, deterministic-window,
resource-bound, unresolved-query, required/optional failure, and fuzz coverage.
Integration tests prove that observed legacy queries block removal, observed
destination-only queries do not, and an empty history cannot erase a blocker
discovered from configured artifacts.

Trace evidence has scoped/quoted attribute extraction, deduplication, mapping,
strict-manifest, Tempo authentication, parser rejection, timeout, response
bound, redirect, integration, exact-domain matching, and fuzz coverage. Tests
prove that legacy OTel attributes block only through an explicit mapping and
that a missing required mapping is `INCOMPLETE`.

Change-source coverage includes strict snapshot parsing and fuzzing,
byte-stable normalization, full baseline/candidate delta ordering, generated
ChangeSet round trips, empty safe diffs, semantic uncertainty, file
provenance, Prometheus authentication, response and count bounds, API
warnings, metadata conflicts, timeout, redirect rejection, CLI source
exclusivity, and hosted snapshot-mode Action execution.

Control-plane coverage includes KEDA Prometheus triggers, Argo Rollouts
Prometheus measurements, and Kubernetes HPA external/custom metrics. HPA tests
prove that same-name metrics remain unresolved without explicit backend
evidence, every selector label is mapped or documented as adapter-only,
non-Prometheus ignore decisions suppress false dependencies, conflicting
mappings fail closed, sensitive selector values are discarded, and mapping and
manifest decoders remain bounded and panic-free under fuzzing.
The combined lifecycle then proves the same legacy metric and label produce
six exact control-plane findings, that fully migrated artifacts pass with no
residual findings, and that dynamic KEDA/Argo identity plus a missing same-name
HPA mapping yields three required diagnostics and `INCOMPLETE`.

## Implemented test layers

The current deterministic engine has:

- strict ChangeSet parsing, validation, deep-copy normalization, all-kind
  compatibility golden, round-trip, mutation-safety, and fuzz tests;
- generic impact tests for direct and transitive paths, operational taxonomy,
  unresolved evidence, Prometheus metric families, cross-domain isolation,
  deterministic ordering, and input immutability;
- generic policy truth tables for every status and rollout mode, criticality
  thresholds, malformed policy and findings, finding preservation, stable exit
  codes, and an exact `tcg-result/v1alpha1` JSON golden;
- compatibility integration tests proving one evidence set can produce the
  intentionally different generic and migration statuses without changing the
  legacy schema or golden result;
- canonical CLI tests for generic machine results, rollout and exit contracts,
  strict ChangeSet validation, collision-free executable naming, and bytewise
  equality between nested migration checks and the `tmr` compatibility path;
- configuration tests proving canonical and legacy documents normalize
  identically, plus environment tests for canonical, fallback, matching,
  conflicting, empty, and unrelated names without secret disclosure, with
  configuration parsing included in CI fuzz smoke coverage;
- Action contract tests cover both modes, mutually exclusive inputs, required
  inputs and source pairs, Markdown/JSON artifact creation, and status/exit
  disagreement; a hosted matrix invokes explicit, snapshot, and migration
  modes on every PR;
- unit tests for every required migration validation rule;
- valid YAML fixtures and validation tests covering all implemented metric,
  label, span-attribute, and resource-attribute change kinds;
- invalid YAML fixtures with exact golden diagnostics;
- PromQL AST unit and fuzz tests;
- telemetry snapshot parser fuzz tests;
- CODEOWNERS and strict ownership-metadata unit and fuzz tests;
- Prometheus query-log and Telemetry Change Guard query-history unit and fuzz tests;
- TraceQL attribute-scanner unit and fuzz tests plus Tempo API component tests;
- component fixtures for Prometheus rules, PrometheusRule CRDs, Grafana, Sloth,
  Pyrra, KEDA, Argo Rollouts, and explicitly mapped Kubernetes HPA resources;
- cycle and transitive-chain graph tests;
- fail-closed readiness and required-source failure tests;
- an exact JSON golden report for the checkout migration;
- CLI integration tests for output and the permanent `0/1/2/3` exit contract;
- adversarial CLI/source tests for parent traversal, out-of-root absolute paths,
  symlinks, aggregate budgets, atomic private outputs, and Markdown/terminal
  containment;
- CI checks for formatting, vetting, and race-enabled tests.
- pinned live Docker lifecycles against Prometheus, Grafana, and Sloth, plus a
  digest-pinned Tempo TraceQL validation tier;
- a combined KEDA, Argo Rollouts, and explicitly mapped HPA lifecycle with
  exact machine-result verification.

Run the local checks with:

```bash
make verify
```

`make verify` checks module consistency, formatting, vet, race-enabled tests,
the complete parser fuzz-smoke matrix, reachable Go vulnerabilities with the
pinned scanner version, GitHub workflow syntax and supply-chain policy, and
Action shell syntax. It does not modify source formatting.

Workflow validation uses three deliberately separate controls:

- `actionlint` `v1.7.12` validates workflow syntax, expressions, and runner
  commands locally and in the `Workflow Security` job;
- `scripts/check-workflow-policy.sh` rejects movable third-party Action
  references, missing version comments, and default permissions broader than
  `contents: read`. It also locks the public-release trust boundary: an
  unprivileged candidate, exact-artifact native smoke tests on Linux, macOS,
  and Windows, and a non-building protected publish job;
- `zizmor` `v1.29.0` performs a high-confidence, medium-or-higher local
  security audit without online collection. The wrapper Action and its own
  binary version are both pinned.

CodeQL runs the `security-extended` Go queries on pushes, pull requests, and a
weekly schedule. Dependency Review rejects newly introduced high-severity
dependencies on pull requests without posting a token-powered comment. The
existing pinned `govulncheck` remains the reachable Go vulnerability gate.
Dependabot separately proposes reviewed updates for both Go modules and
GitHub Actions; an update must preserve full-SHA pins and pass the same policy
checks.

Build-identity tests cover development defaults, malformed linker values,
tri-state dirty metadata, exact human and `tcg-version/v1alpha1` JSON goldens,
shared canonical/compatibility output, Action commit selection, and a real
release-shaped `-trimpath` binary. The integration test injects candidate
version, commit, date, and clean state, executes both version forms, and scans
the binary for the local build path.

Release verification builds both executables for Linux, macOS, and Windows on
amd64 and arm64 with locked GoReleaser, Syft, and Go versions. The maintainer
tool requires the exact 23-file public payload, strict manifest schema, sorted
SHA-256 set, safe archive paths, stable modes and timestamps, `CGO_ENABLED=0`,
`-trimpath`, embedded version/commit/date, per-artifact SPDX and CycloneDX
documents, and a clean source archive. It executes both host binaries' version
commands and the real getting-started `BLOCK`/exit `2` fixture. The release
snapshot workflow then downloads that exact payload on Linux, macOS, and
Windows and executes the native host archive. Reproducible builds require
byte-identical checksum sets:

```bash
make release-snapshot       # one non-publishing build plus deep verification
make release-reproducible   # two builds; fail on any public-payload drift
```

Neither target publishes a tag, release, package, container, or announcement.

## Primary product canaries

The fast, no-Docker primary product canaries are separate from the broad unit
and live suites:

```bash
make canary
```

They assert exact proposed-change `BLOCK`/`PASS` and migration
`BLOCKED`/`READY` results, status artifacts, JSON schemas, exit codes, finding
identity, and migration counts. See the
[production-style validation map](PRODUCTION_VALIDATION.md) for the expected
states and the complete release-candidate sequence.

## Mandatory live E2E release gate

The live harness is implemented under `e2e/` and is a pull-request release
gate. It runs pinned versions for reproducibility; a weekly workflow exercises
previous-supported and upstream-latest combinations.

The harness runs a pinned Docker Compose stack containing a controlled
exporter, Prometheus, Grafana, and Sloth. Pyrra is covered by deterministic
component fixtures. The live stack covers this telemetry lifecycle:

```text
old only -> dual write -> partial consumer migration
         -> complete consumer migration -> old telemetry removed
```

It must prove both prediction directions:

1. Telemetry Change Guard reports `BLOCKED` before an intentionally premature
   cutover, and the isolated stack exhibits the predicted missing critical data.
2. Telemetry Change Guard reports `READY` only after every required consumer is
   migrated, and the same critical queries, rules, dashboards, and SLOs continue working after
   legacy telemetry is removed.

The release gate proves:

- direct metric and label dependency detection;
- Grafana, alert, recording-rule, and SLO consumer discovery;
- complete transitive propagation through recording rules;
- fail-closed behavior for unresolved critical queries and required adapter
  failures;
- no panic or false safety result for malformed PromQL or graph cycles;
- independent `promtool` checks and rule tests;
- Sloth validation and generated-rule cross-checks;
- proof that the deterministic result retains authority (the adversarial AI
  test is added with the optional AI milestone).

Run the core and live layers with:

```bash
make verify
make e2e
```

See [the E2E harness guide](../e2e/README.md) for pinned versions, scenario
expectations, and runtime assertions.

Pinned E2E tests run on pull requests. Compatibility matrices and latest
upstream versions run in the scheduled compatibility workflow.
