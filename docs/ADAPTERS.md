# Adapters

Adapters translate external artifact shapes into the canonical consumer,
reference, production, and diagnostic model. They never decide readiness.

## Local adapters

- `prometheusrules` reads standard Prometheus rule groups and Prometheus
  Operator `PrometheusRule` resources. Recording-rule outputs become produced
  symbols so impact can propagate transitively.
- `grafana` reads file-exported or API-envelope dashboard JSON, walks nested
  panels, and analyzes Prometheus targets. Templated or malformed expressions
  remain unresolved.
- `sloth` reads service-level objectives and analyzes raw error/total event
  queries. SLO consumers default to critical.
- `pyrra` reads Pyrra SLO resources and analyzes indicator metric expressions.
  SLO consumers default to critical.

Every configured source can be marked `required`. A load, parse, or expansion
failure on a required source prevents `READY`. All adapters preserve file,
line, expression, extraction method, and confidence where available.

Remote and ecosystem integrations are optional additions. They must add
evidence without becoming prerequisites for the local deterministic core.
