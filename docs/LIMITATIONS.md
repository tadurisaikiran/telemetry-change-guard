# Public alpha limitations

These boundaries are part of TCG's safety contract. The project reports what
the configured evidence establishes; it does not market missing evidence as
coverage.

## Maturity and distribution

- `v0.1.0-alpha.1` is a candidate, not a published or stable release.
- No release archive, registry image, Homebrew tap, stable Action tag, or
  compatibility guarantee beyond the documented alpha contracts is published.
- macOS candidate archives are not notarized; Windows candidates are not
  Authenticode-signed.
- Real external design-user evidence remains limited. The built-in benchmark
  is synthetic regression evidence, not independent field accuracy proof.

## Evidence scope

- TCG does not crawl an organization or automatically discover every repository,
  dashboard service, runtime query, or generated configuration.
- Local source globs and optional remote adapters must be explicitly configured.
- A `PASS` is scoped to those configured sources and the selected policy.
- Snapshot evidence represents what the queried Prometheus deployment exposed
  at capture time; absence from a snapshot does not prove a signal cannot be
  emitted.
- Runtime query history is additive. An empty history never proves non-use.
- Dynamic or malformed required evidence produces `INCOMPLETE` or `ERROR`.

## Semantic scope

- Supported changes are the documented metric, label, span-attribute, and
  resource-attribute changes. See [ChangeSet](CHANGESET.md).
- Prometheus, OpenTelemetry, Tempo, Kubernetes, and infrastructure identities
  remain separate unless explicit mapping evidence connects them.
- Similar names do not establish identity or dependency.
- Transitive analysis follows productions and references discovered in the
  configured evidence; it cannot traverse an unavailable graph.
- PromQL is parsed statically. TraceQL dependencies require configured Tempo
  validation. LogQL and arbitrary query languages are not supported.

## Adapter scope

The alpha supports the adapters listed in [Adapters](ADAPTERS.md), including
Prometheus rules, Grafana exports, Sloth/Pyrra, runtime query records, KEDA,
Argo Rollouts, explicitly mapped HPA metrics, Weaver evidence, Perses usage,
and Tempo/TraceQL paths subject to each guide's limits.

CloudFormation loading is bounded and non-executing, but CloudWatch consumer
safety decisions are not implemented. Collector configuration discovery,
arbitrary source-code discovery, server/UI mode, MCP, and organization-wide
repository orchestration remain roadmap work.

## Operational boundaries

- The core is local-first and needs no database. Optional remote adapters need
  network access and explicit trusted-origin controls.
- TCG does not mutate consumer files during a normal check.
- Optional explanation and remediation providers are external processes with
  strict protocols. Their output cannot approve a change or alter the result.
- The experimental agentic controller produces an uncommitted review diff in
  an isolated workspace; it does not commit, push, open, approve, or merge a
  change request.
- Repository branch protection, required checks, secret scanning, release
  environments, tag policy, and package visibility remain owner-administered.

## Not established by current evidence

The project does not currently claim organization-wide coverage, independent
precision or recall, a fixed maximum analysis latency for arbitrary inputs,
production adoption, or that a clean result eliminates the need for canaries,
monitoring, rollback, and human review.

For an evaluation record, capture the exact version, source inventory, required
flags, expected ground truth, actual status, findings, diagnostics, time, and
environment. See [`evaluation/`](../evaluation/README.md).
