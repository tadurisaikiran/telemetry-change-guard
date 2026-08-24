# Kubernetes HPA analysis

Telemetry Change Guard discovers Prometheus dependencies in Kubernetes
`autoscaling/v2` `HorizontalPodAutoscaler` manifests only when a separate,
explicit mapping proves the backend identity. A changed mapped metric or label
produces an `autoscaler` consumer and the deterministic `SCALING_RISK` impact
type.

```yaml
sources:
  horizontalPodAutoscalers:
    - path: ./deploy/hpa/*.yaml
      mapping: ./config/hpa-mapping.yaml
      required: true
```

Unlike other local source patterns, an HPA source is always a mapping because
`mapping` is mandatory. This prevents a Kubernetes API metric name from being
silently treated as a Prometheus metric name.

## Explicit backend mapping

The mapping is a strict, versioned, single-document YAML file:

```yaml
apiVersion: tcg.hpa/v1alpha1
kind: HPAMetricMappings
spec:
  mappings:
    - kubernetes:
        type: External
        metric: checkout_queue_depth
      prometheus:
        metric: rabbitmq_queue_messages_ready
        labels:
          queue: rabbitmq_queue
        ignoredLabels:
          tenant: consumed only by the deployed adapter's routing rule

    - kubernetes:
        type: External
        metric: cloud_queue_depth
      ignore: supplied by the deployed CloudWatch metrics adapter
```

Each `(type, metric)` pair is unique and must set exactly one of:

- `prometheus`, which establishes the exact Prometheus metric identity; or
- `ignore`, which documents why that Kubernetes metric is not backed by
  Prometheus.

Only `External`, `Object`, and `Pods` mappings are accepted. These correspond
to Kubernetes external and custom metrics APIs. A mapping should be derived
from the deployed metrics-adapter configuration and reviewed with that
configuration; similar spelling is not evidence. Even when both metric names
are identical, an absent mapping remains unresolved and a required source
makes the result `INCOMPLETE`.

HPA selectors need the same proof. Every selector label used by `matchLabels`
or `matchExpressions` must appear under either:

- `prometheus.labels`, as `Kubernetes label: Prometheus label`; or
- `prometheus.ignoredLabels`, with a non-empty reason proving it affects only
  adapter routing and not the Prometheus selector.

Mapping a selector label to `__name__`, mapping two Kubernetes labels to one
Prometheus label, ambiguous duplicate entries, and undocumented selector
labels are rejected or left unresolved. Selector values are never retained in
evidence or reports.

## Supported HPA contract

The adapter accepts exact `apiVersion: autoscaling/v2` resources with
`kind: HorizontalPodAutoscaler`. It requires `metadata.name` and
`spec.scaleTargetRef.name`. Every configured metric must contain exactly one
metric source matching its `type`.

- `External`, `Object`, and `Pods` metrics require an explicit mapping or
  ignore decision.
- `Resource` and `ContainerResource` metrics are Kubernetes resource metrics
  and do not create Prometheus dependencies.
- An omitted `spec.metrics` is accepted because Kubernetes can apply its
  default resource-metric behavior.
- Unrelated resources in the same multi-document manifest are ignored.

For label selector requirements, the adapter accepts the Kubernetes
`In`, `NotIn`, `Exists`, and `DoesNotExist` operators and validates their value
cardinality. It retains label names only.

The HPA manifest limit is 8 MiB per file. The mapping limit is 1 MiB and
unknown mapping fields or additional documents are rejected. Both loaders
observe context cancellation. Configuring one manifest with different mapping
documents is ambiguous and fails closed.

## Criticality

HPA autoscalers default to `high`. They default to `critical` when any of the
following exact HPA labels has the value `prod` or `production`
(case-insensitive):

- `environment`;
- `env`; or
- `app.kubernetes.io/environment`.

No namespace, workload-name, or fuzzy environment inference is performed.
The default enforce policy blocks both high and critical `SCALING_RISK`
findings.

## Example

```bash
telemetry-change-guard check \
  --config ./examples/hpa/tcg.yaml \
  --changes ./examples/hpa/changes.yaml
```

The example removes `rabbitmq_queue_messages_ready`, which is explicitly
mapped to the production `checkout-worker` HPA. The result is `BLOCK` with a
critical `SCALING_RISK` finding.

The supported fields follow Kubernetes' [HorizontalPodAutoscaler v2 API
reference](https://kubernetes.io/docs/reference/kubernetes-api/autoscaling/horizontal-pod-autoscaler-v2/)
and [autoscaling concepts](https://kubernetes.io/docs/concepts/workloads/autoscaling/).
