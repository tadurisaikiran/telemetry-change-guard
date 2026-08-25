# Release-gate benchmark

This benchmark is an original, synthetic regression corpus for the behavior
TCG currently claims to support. It answers one question: does this exact
build still produce the reviewed status, exit code, schema, findings, and
diagnostics for these fixtures?

It is **not** independent adoption evidence, a representative sample of real
repositories, or proof of field-wide precision, recall, performance, or zero
false negatives. Those claims require a separately versioned public corpus,
independent ground truth, and published methodology.

Run the corpus from the repository root:

```bash
make benchmark
```

The command writes `dist/benchmark/results.json` with schema
`tcg-benchmark-result/v1alpha1`. It records the repository revision, dirty
state, environment, expected and actual contracts, elapsed time, and child
process peak resident memory where the operating system exposes it.

## Layout

- [`manifest/corpus.json`](manifest/corpus.json) is the versioned case index.
- [`corpus/`](corpus/) contains the self-contained inputs.
- [`expected/README.md`](expected/README.md) explains ground-truth review.
- [`results/README.md`](results/README.md) defines result handling and claim
  boundaries.
- [`scripts/run.sh`](scripts/run.sh) builds an isolated CLI and runs the
  machine verifier.

Every corpus artifact is synthetic and licensed under the repository's
Apache-2.0 license. A case is changed only through review of both its
operational ground truth and the TCG expectation.

## Cases

| Case | Contract under test |
| --- | --- |
| `direct-alert` | Direct Prometheus alert dependency blocks removal |
| `transitive-recording-rule` | Metric → recording rule → alert traversal |
| `grafana-dashboard` | Medium-criticality visibility loss remains a warning under fixture policy |
| `sloth-slo` | SLI dependency is classified as SLO risk |
| `keda-scaling` | Prometheus-backed scaling dependency blocks removal |
| `argo-rollout-gate` | Rollout analysis dependency is deployment-gate risk |
| `mapped-hpa` | Explicit Kubernetes-to-Prometheus metric mapping establishes scaling risk |
| `migrated-consumer` | Replacement-only consumer is migration-ready |
| `malformed-required` | Required malformed evidence fails incomplete |
| `unresolved-dynamic-query` | Known findings are retained while a required dynamic query fails incomplete |
| `safe-lookalike` | A similar identifier is not treated as the removed symbol |

The runner compares status, exit code, result schema, finding and diagnostic
counts, impact classes, and migration classifications. It also records elapsed
time and child-process peak resident memory when supported, but it applies no
public performance threshold and generated host-specific measurements are not
committed.
