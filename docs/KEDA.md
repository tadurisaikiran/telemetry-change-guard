# KEDA ScaledObject analysis

Telemetry Change Guard discovers Prometheus query dependencies in local KEDA
`ScaledObject` manifests. A changed metric or label used by a scaler produces
an `autoscaler` consumer and the deterministic `SCALING_RISK` impact type.

```yaml
sources:
  keda:
    - path: ./deploy/scaledobjects/*.yaml
      required: true
```

Scalar paths are also accepted and default to `required: true`. Paths use the
same deterministic local expansion as the other file adapters. A required
missing path, unreadable file, malformed YAML document, invalid ScaledObject,
or unresolved PromQL query makes the result `INCOMPLETE`.

## Supported contract

The adapter supports `apiVersion: keda.sh/v1alpha1`, `kind: ScaledObject`, and
the documented `prometheus` trigger. It requires:

- `metadata.name`;
- `spec.scaleTargetRef.name`;
- at least one item in `spec.triggers`; and
- `metadata.query` on every Prometheus trigger.

Multiple YAML documents and multiple Prometheus triggers are supported. Each
Prometheus trigger becomes a separate consumer, so evidence and failures point
to the exact trigger. Non-Prometheus triggers and unrelated Kubernetes
resources in the same manifest are ignored. PromQL analysis is delegated to
the existing official Prometheus AST analyzer; the KEDA adapter does not
implement a second query parser.

The loader has an 8 MiB per-file limit and observes context cancellation.
Prometheus server addresses, query parameters, custom headers,
`authenticationRef` values, and other trigger metadata are deliberately not
copied into normalized evidence or reports. Only the query, ScaledObject and
target identity, namespace, trigger index/name, API version, and source
location are retained.

## Criticality

KEDA autoscalers default to `high`. They default to `critical` when any of the
following exact ScaledObject labels has the value `prod` or `production`
(case-insensitive):

- `environment`;
- `env`; or
- `app.kubernetes.io/environment`.

No namespace, workload name, or fuzzy environment inference is performed.
With the default enforce policy, both high and critical `SCALING_RISK`
findings block the change; criticality remains useful for prioritization and
custom policy thresholds.

## Example

```bash
telemetry-change-guard check \
  --config ./examples/keda/tcg.yaml \
  --changes ./examples/keda/changes.yaml
```

The example removes `checkout_requests_total`, which is used by the
production-tagged `orders-worker` ScaledObject. The result is `BLOCK` with a
critical `SCALING_RISK` finding.

The supported fields follow KEDA's
[ScaledObject specification](https://keda.sh/docs/latest/reference/scaledobject-spec/)
and [Prometheus scaler specification](https://keda.sh/docs/latest/scalers/prometheus/).
