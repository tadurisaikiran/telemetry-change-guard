# Related Work

Telemetry Change Guard (TCG) overlaps with telemetry schema tools, usage
inventory systems, vendor asset indexes, and configuration linters. Those tools
solve valuable adjacent problems. TCG's current differentiation is a proposed
telemetry change evaluated against a cross-consumer dependency graph, including
transitive recording rules and supported control-plane consumers, to produce a
deterministic, fail-closed pre-merge decision.

This comparison describes documented primary capabilities as of 2026-08-24.
“Not documented” means the cited project does not present the capability as a
primary feature; it does not claim that the capability is technically
impossible or absent from every edition.

## Capability comparison

| Project | Documented primary purpose | Change or schema input | Consumer or usage evidence | Transitive/control-plane analysis | Pre-merge role | Deployment model |
| --- | --- | --- | --- | --- | --- | --- |
| **Telemetry Change Guard** | Change-impact safety for telemetry contracts | Explicit ChangeSet, bounded snapshots, or explicitly mapped Weaver diff | Local definitions plus optional bounded runtime and remote evidence | Recording-rule paths; KEDA, Argo Rollouts, and explicitly mapped HPA | Authoritative `PASS`/`WARN`/`BLOCK`/`INCOMPLETE`/`ERROR` gate | Open source, local-first, vendor-neutral core |
| [OpenTelemetry Weaver](https://github.com/open-telemetry/weaver) | Develop, validate, resolve, generate, and live-check semantic-convention registries | Telemetry schema and registry versions | Instrumentation compliance through `live-check` | Operational-consumer graphs are not a documented primary purpose | Registry/schema validation can run in automation | Open source OpenTelemetry tooling |
| [Perses metrics-usage](https://github.com/perses/metrics-usage) | Track Prometheus metric usage across dashboards and alerting/recording rules | Static definitions and Prometheus inventory, not a proposed telemetry contract change | Perses/Grafana dashboards and Prometheus rules | Does not document TCG-style change-policy or control-plane traversal | Evidence service rather than an authoritative change gate | Open source, Prometheus-focused service |
| [Grafana Adaptive Metrics](https://grafana.com/docs/grafana-cloud/cost-management-and-billing/reduce-costs/metrics-costs/control-metrics-usage-via-adaptive-metrics/) | Reduce metric volume and cost using observed usage | Optimization recommendations, not a source contract diff | Grafana Cloud dashboards, alerts, and queries | Not documented as a cross-repository change graph or control-plane safety engine | Optimization workflow rather than a proposed-change gate | Managed Grafana Cloud capability |
| [Chronosphere Telemetry Usage Analyzer](https://docs.chronosphere.io/investigate/analyze/usage) | Rank telemetry by usage, utility, volume, and cardinality | Usage and shaping analysis, not a source contract diff | Dashboards, monitors, shaping rules, and Metrics Explorer queries | Not documented as transitive migration or control-plane analysis | Usage/governance workflow rather than a deterministic PR gate | Managed Chronosphere capability |
| [Datadog Related Assets](https://docs.datadoghq.com/api/latest/metrics/related-assets-to-a-metric/) | Return Datadog assets that reference a metric | Metric-name lookup, not a proposed contract change | Dashboards, monitors, notebooks, and SLOs; documented as updated every 24 hours | Not documented as transitive or control-plane analysis | Asset lookup rather than a fail-closed change gate | Managed Datadog API |
| [pint](https://github.com/cloudflare/pint) | Lint and validate Prometheus rules, including checks against Prometheus | Proposed Prometheus rule files | Backend-aware rule and series checks | Validates rules; does not document reverse cross-consumer migration graphs | Strong complementary CI check for rule quality | Open source, Prometheus-focused CLI |
| [Grafana Dashboard Linter](https://github.com/grafana/dashboard-linter) | Find common mistakes and recommend dashboard best practices | Grafana dashboard definitions | The dashboard being linted | Does not document dependency impact from an emitted-telemetry change | Complementary CI check for dashboard quality | Open source, currently Prometheus-dashboard focused |

## How the tools fit together

- Weaver can define and validate the telemetry schema; TCG can consume an
  explicitly mapped Weaver diff as one change source.
- Perses metrics-usage can provide remote consumer evidence; TCG's adapter
  treats it as bounded evidence and keeps policy local.
- Vendor usage and related-asset systems help decide which telemetry is useful
  inside their platforms. TCG focuses on the safety of a proposed contract
  change across configured sources.
- pint and dashboard linters validate the quality of consumer definitions. TCG
  asks a different question: which existing consumers are affected by this
  telemetry change, directly or transitively?

TCG does not currently discover every telemetry domain or vendor asset. In
particular, the implemented CloudFormation loader is only an ingestion
foundation; CloudWatch consumer analysis remains roadmap work. Required
unsupported or unresolved evidence must be represented as incomplete rather
than safe.

Corrections and additions are welcome through a pull request that cites a
first-party source and states the exact capability being compared.
