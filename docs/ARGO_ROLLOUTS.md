# Argo Rollouts deployment-gate analysis

Telemetry Change Guard discovers Prometheus query dependencies in local Argo
Rollouts `AnalysisTemplate` and `ClusterAnalysisTemplate` resources. A changed
metric or label used by an analysis measurement produces a `deployment_gate`
consumer and the deterministic `DEPLOYMENT_GATE_RISK` impact type.

```yaml
sources:
  argoRollouts:
    - path: ./deploy/analysis-templates/*.yaml
      required: true
```

Scalar paths are also accepted and default to `required: true`. A required
missing path, unreadable file, malformed resource, or unresolved Prometheus
measurement makes the result `INCOMPLETE`.

## Supported contract

The adapter supports `apiVersion: argoproj.io/v1alpha1` resources with either
of these kinds:

- `AnalysisTemplate`; or
- `ClusterAnalysisTemplate`.

Every `spec.metrics` entry must have a unique name and exactly one provider.
Prometheus providers must contain a non-empty `query`. Instant and range
queries are both supported because Argo uses the same PromQL field for each;
`rangeQuery` changes execution timing, not telemetry identity. Template
composition through `spec.templates` and non-Prometheus metric providers is
accepted but does not create Prometheus evidence.

Multiple YAML documents and multiple Prometheus measurements are supported.
Each measurement becomes a separate deployment-gate consumer, preserving the
template kind/name, metric name, scope, query type, and exact source location.

## Argo arguments

Argo commonly substitutes `{{args.name}}` values at AnalysisRun creation time.
Telemetry Change Guard masks an argument only when the official Prometheus AST
proves that it occurs inside a label matcher value. The metric and label names
remain exact in that case, so dependencies can be established safely and the
original expression is retained as evidence.

Arguments that could alter telemetry identity remain unresolved, including:

- dynamic metric names or `__name__` matcher values;
- dynamic label names in functions such as `label_replace`;
- dynamic range-selector durations; and
- unsupported template syntax.

The adapter always delegates dependency extraction to the shared PromQL AST
analyzer; it does not implement a second query parser.

## Criticality and data handling

Deployment gates default to `high`. They default to `critical` when any of the
following exact template labels has the value `prod` or `production`
(case-insensitive):

- `environment`;
- `env`; or
- `app.kubernetes.io/environment`.

No namespace or template-name inference is performed. The default enforce
policy blocks both high and critical `DEPLOYMENT_GATE_RISK` findings.

The loader has an 8 MiB per-file limit, accepts at most 4,096 argument
occurrences per query, and observes context cancellation.
Prometheus addresses, headers, OAuth/basic-auth configuration, secret
references, and other provider settings are deliberately not copied into
normalized evidence or reports.

## Example

```bash
telemetry-change-guard check \
  --config ./examples/argo-rollouts/tcg.yaml \
  --changes ./examples/argo-rollouts/changes.yaml
```

The example removes `checkout_requests_total`, which is used by a
production-tagged rollout analysis. The result is `BLOCK` with a critical
`DEPLOYMENT_GATE_RISK` finding.

The supported fields follow Argo Rollouts'
[analysis template documentation](https://argo-rollouts.readthedocs.io/en/stable/features/analysis/)
and [Prometheus provider documentation](https://argo-rollouts.readthedocs.io/en/stable/analysis/prometheus/).
