# Telemetry Change Guard — product brief

> **Draft only — do not publish before owner-approved alpha release.**

## The one-sentence product

**TCG treats telemetry like an API contract and checks which configured
consumers will break before the contract changes.**

## The problem

A metric rename can be a one-line code change. Its consumers may live elsewhere:
a Prometheus alert, Grafana dashboard, Sloth SLO, KEDA scaler, Argo Rollouts
gate, HPA mapping, or runtime query record. Producer code can compile, unit
tests can pass, and deployment can succeed while an operational dependency is
already invalid.

CI understands source and binary APIs because tools can resolve both sides of
the contract. Telemetry usually lacks that same review-time contract check.

## What TCG does

TCG accepts one explicit proposed change source and a configuration describing
the consumer evidence to inspect. It:

1. strictly decodes the proposed telemetry contract change;
2. loads configured local and optional remote evidence;
3. extracts typed telemetry references and produced symbols;
4. follows direct and transitive dependency paths;
5. classifies operational impact and criticality;
6. applies explicit `audit`, `warn`, or `enforce` policy; and
7. returns a human report, versioned JSON evidence, and authoritative exit code.

| Status | Meaning |
| --- | --- |
| `PASS` | No blocking impact was found under the configured evidence and policy |
| `WARN` | Known risk remains visible but current policy permits it |
| `BLOCK` | Known evidence violates enforced policy |
| `INCOMPLETE` | Required evidence is missing, malformed, dynamic, denied, or unresolved |
| `ERROR` | Input, configuration, discovery, or evaluation failed safely |

## Why teams evaluate it

- SRE and platform teams can put evidence about alerts, SLOs, scaling, and
  rollout gates into the same pull-request decision.
- Application teams see the consumer, source location, impact, and dependency
  path instead of receiving a generic policy failure.
- Observability teams can stage metric and label migrations and retain a
  machine-readable cutover record.
- Security-conscious teams can run the deterministic core locally without a
  database, hosted service, or AI provider.
- AI-assisted development teams can let models draft inputs or repairs while
  keeping safety authority in deterministic evaluation and human approval.

## Concrete example

The included fixture proposes removing `checkout_requests_total`. A critical
alert still evaluates it. TCG reports the alert at its source line, classifies
`ALERTING_RISK`, returns `BLOCK`, and exits `2`.

That exit is not a tool error. It is the check doing its job before the signal
change reaches production.

## Supported alpha surfaces

The candidate includes explicit ChangeSets, baseline/candidate Prometheus
snapshots, mapped OpenTelemetry Weaver diffs, and migration plans. Consumer
evidence includes the supported local and remote paths listed in
[Adapters](../ADAPTERS.md), with dedicated coverage for Prometheus rules,
Grafana, Sloth/Pyrra, runtime queries, KEDA, Argo Rollouts, explicitly mapped
HPA metrics, Perses usage, and Tempo/TraceQL subject to their documented limits.

## Evidence behind the candidate

The repository gates changes with race tests, parser fuzz smoke tests,
vulnerability analysis, CodeQL, dependency review, workflow-policy checks,
external Action-consumer fixtures, reproducible release checks, multi-platform
container verification, and pinned live lifecycles.

The synthetic release-gate corpus asserts 11 reviewed contracts spanning direct,
transitive, control-plane, safe-negative, migration, and incomplete-evidence
cases. Its purpose is regression detection. It is not independent adoption or
field-accuracy evidence.

## Alpha boundaries

- No packaged alpha or stable Action tag is published until owner approval.
- Results are scoped to configured evidence; inaccessible consumers remain
  outside the result.
- Dynamic or missing required evidence does not become a clean pass.
- Arbitrary source-code diff extraction, Collector discovery, LogQL, server/UI,
  and organization-wide orchestration are not implemented.
- CloudFormation loading exists, but CloudWatch consumer safety decisions are
  not implemented.
- AI is optional. Model output cannot create or override a TCG decision.
- Independent design-user and public-adoption evidence is still being built.

## Evaluate the reviewed commit

Before artifact publication, evaluators with Go 1.26.7+ can install:

```bash
go install github.com/tadurisaikiran/telemetry-change-guard/cmd/telemetry-change-guard@20dbd241b3934418a4d288d05dd69eb55ad85079
```

Start with the [five-minute quickstart](../QUICKSTART.md), then bring one
sanitized representative telemetry change to the
[design-user protocol](../DESIGN_USER_PROGRAM.md). The most useful feedback is
specific: an expected dependency missed, an unexpected dependency reported,
uncertainty handled incorrectly, or setup that prevents adoption as a required
pull-request check.
