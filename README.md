# Telemetry Change Guard

<p align="center">
  <strong>Change telemetry without breaking production.</strong>
</p>

<p align="center">
  See which configured alerts, dashboards, SLOs, autoscalers, and rollout
  gates depend on a metric, label, or trace attribute. Understand the risk.
  Stop unsafe changes before merge.
</p>

<p align="center">
  <a href="https://github.com/tadurisaikiran/telemetry-change-guard/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/tadurisaikiran/telemetry-change-guard/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://github.com/tadurisaikiran/telemetry-change-guard/actions/workflows/e2e.yml"><img alt="Pinned E2E" src="https://github.com/tadurisaikiran/telemetry-change-guard/actions/workflows/e2e.yml/badge.svg"></a>
  <a href="LICENSE"><img alt="Apache License 2.0" src="https://img.shields.io/badge/license-Apache--2.0-blue.svg"></a>
  <img alt="Go 1.26.7 or newer" src="https://img.shields.io/badge/Go-1.26.7%2B-00ADD8?logo=go&logoColor=white">
  <img alt="Project status: pre-release" src="https://img.shields.io/badge/status-pre--release-orange.svg">
</p>

<p align="center">
  <a href="#why-tcg">Why TCG</a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="#try-it-in-five-minutes">Quickstart</a> ·
  <a href="#use-it-on-your-repository">Use it</a> ·
  <a href="#coverage">Coverage</a> ·
  <a href="#ai-assisted-workflows">AI</a> ·
  <a href="#github-action">GitHub Action</a> ·
  <a href="#documentation">Docs</a>
</p>

## Why TCG

Production does not merely observe telemetry—it acts on it.

- Alerts page engineers.
- SLOs measure reliability.
- Dashboards guide incidents.
- KEDA and Kubernetes HPA scale workloads.
- Argo Rollouts can continue or stop a deployment.
- Runtime queries support automation and investigation.

That makes a metric name, label, span attribute, or resource attribute an
**operational API**. Changing the producer may take one line of code while its
consumers are spread across files, repositories, teams, and systems. The code
can compile, tests can pass, and deployment can succeed even though an alert is
blind or an autoscaler is reading a signal that no longer exists.

**Telemetry Change Guard (TCG) is a local-first CLI and GitHub Action that
checks a proposed telemetry change against the consumer evidence you require,
follows direct and transitive dependencies, and returns a deterministic safety
decision before merge or deployment.**

It does not stop at “this name appears somewhere.” TCG reports the affected
consumer, source location, operational impact, criticality, dependency path,
policy reason, and authoritative status.

```text
CHANGE                      CONFIGURED CONSUMERS
metric · label              alerts · dashboards · SLOs
span/resource attribute  +  runtime queries · KEDA · HPA · Argo
snapshot or Weaver diff
              \                  /
               v                v
          parse -> dependency graph -> impact -> policy
                              |
                              v
             PASS · WARN · BLOCK · INCOMPLETE · ERROR
                human report + versioned JSON + exit code
```

### One product, four practical workflows

| When | What TCG does | What you get |
| --- | --- | --- |
| **Review a proposed change** | Checks an explicit ChangeSet, Prometheus snapshot diff, or mapped OpenTelemetry Weaver diff against configured consumers | A merge-ready `PASS`, visible `WARN`, safety `BLOCK`, or fail-closed uncertainty |
| **Plan a migration** | Tracks legacy and replacement signals against readiness criteria | A deterministic cutover decision and the consumers still needing migration |
| **Understand impact** | Explores direct and transitive paths from one signal through recording rules and operational consumers | Source-linked dependency paths and an exportable configured-evidence graph |
| **Repair and recheck** | Lets a human or optional AI provider draft an explanation or eligible query repair, then reparses and reanalyzes it | A review candidate—never an AI-authored safety decision |

The core needs no database, hosted control plane, or AI provider. It can run
entirely against checked-in files. Remote evidence and AI are opt-in.

> **Public alpha candidate:** the CLI, GitHub Action, release build, container
> build, and external-consumer paths are tested, but no release tag, packaged
> binary, container image, or Homebrew formula is published yet. Evaluate the
> exact reviewed commit below and read the
> [alpha limitations](docs/LIMITATIONS.md) before production evaluation.

## How it works

TCG keeps the product model deliberately small:

> **Change + evidence + policy = one reviewable decision**

1. **Describe what will change.** Provide a ChangeSet, compare two Prometheus
   snapshots, import an explicitly mapped Weaver diff, or use a migration plan.
2. **Choose the evidence that matters.** Point `tcg.yaml` at the alerts,
   dashboards, SLOs, runtime queries, autoscalers, rollout gates, and optional
   remote evidence that are authoritative for your evaluation.
3. **Build the dependency graph.** Typed parsers find references and produced
   signals. Recording rules create transitive paths; name similarity does not.
4. **Classify operational impact.** TCG distinguishes visibility, alerting,
   SLO, scaling, deployment-gate, automation, and semantic risk.
5. **Apply explicit policy.** Start in `audit` or `warn`; move to `enforce`
   when your evidence and thresholds are ready.
6. **Return one result.** Humans receive concise findings. Automation receives
   versioned JSON, stable status semantics, and an exact exit code.

Required evidence is fail-closed. If a required source is missing, malformed,
dynamic, denied, or unresolved, TCG returns `INCOMPLETE` or `ERROR`; it never
silently converts an evidence gap into `PASS`.

### Who it helps

| Team | Typical question |
| --- | --- |
| SRE / platform | “Will this change blind paging, alter scaling, or weaken a rollout gate?” |
| Observability | “Which dashboards, rules, SLOs, and runtime queries still use the old contract?” |
| Application engineering | “What do I need to migrate before renaming this metric or attribute?” |
| Release engineering | “Can this telemetry change become a deterministic required pull-request check?” |
| AI-assisted engineering | “Can AI accelerate discovery and repair while deterministic policy remains the judge?” |

## Try it in five minutes

You need Git and Go 1.26.7 or newer. Install the exact reviewed CLI build:

```bash
go install github.com/tadurisaikiran/telemetry-change-guard/cmd/telemetry-change-guard@20dbd241b3934418a4d288d05dd69eb55ad85079
telemetry-change-guard version
```

Go installs the binary into `GOBIN`, or `$(go env GOPATH)/bin` when `GOBIN` is
unset. Add that directory to `PATH` if the command is not found.

Get the matching example and run one check:

```bash
git clone https://github.com/tadurisaikiran/telemetry-change-guard.git
cd telemetry-change-guard
git checkout 20dbd241b3934418a4d288d05dd69eb55ad85079

telemetry-change-guard validate \
  --config ./examples/getting-started/tcg.yaml \
  --changes ./examples/getting-started/changes.yaml

telemetry-change-guard check \
  --config ./examples/getting-started/tcg.yaml \
  --changes ./examples/getting-started/changes.yaml \
  --mode enforce
```

The example proposes removing `checkout_requests_total`. A critical alert
still queries it, so validation exits `0` and the check intentionally exits
`2` with:

```text
Status:    BLOCK
Findings:  1

[BLOCK] ALERTING_RISK — CheckoutTrafficMissing
  Source:      examples/getting-started/prometheus/rules.yaml:4
  Path:        checkout_requests_total -> CheckoutTrafficMissing

STATUS: BLOCK
```

Exit `2` means TCG ran correctly and stopped the proposed change. It is not a
crash. The [quickstart](docs/QUICKSTART.md) explains every file and expected
line of output; [troubleshooting](docs/TROUBLESHOOTING.md) covers installation
and input errors.

### Start safely in your own repository

You do not need to hand-write a configuration before learning the workflow:

```bash
telemetry-change-guard init
telemetry-change-guard validate \
  --config ./tcg.yaml \
  --changes ./tcg-changes.example.yaml
telemetry-change-guard check \
  --config ./tcg.yaml \
  --changes ./tcg-changes.example.yaml
```

`init` never overwrites an existing file. It creates a runnable, intentionally
blocked example and identifies which source, rule, and ChangeSet to replace
with your own telemetry contract.

## Use it on your repository

A normal evaluation has three concepts:

| Input | What to provide |
| --- | --- |
| **Proposed change** | One ChangeSet, baseline/candidate snapshot pair, mapped Weaver diff, or migration plan |
| **Consumer evidence** | The local artifacts and optional remote sources TCG should inspect |
| **Policy** | Which evidence is required, how unresolved references behave, and which risks must block |

TCG reports only what it can establish from configured evidence. It does not
automatically crawl every repository or runtime system in an organization.

### 1. Describe the proposed change

This `changes.yaml` removes one Prometheus metric:

```yaml
apiVersion: tcg/v1alpha1
kind: ChangeSet
metadata:
  name: checkout-metric-removal
spec:
  changes:
    - id: remove-checkout-requests
      kind: metric_remove
      domain: prometheus
      from:
        domain: prometheus
        kind: metric
        name: checkout_requests_total
```

TCG supports documented metric, label, span-attribute, and resource-attribute
renames and removals. See the [ChangeSet schema](docs/CHANGESET.md).

### 2. Point TCG to authoritative consumers

This `tcg.yaml` checks Prometheus rules, Grafana dashboards, and Sloth SLOs:

```yaml
apiVersion: tcg/v1alpha1
kind: Config
sources:
  prometheusRules:
    - path: ./monitoring/*.yaml
      required: true
  grafana:
    - path: ./dashboards/*.json
      required: true
  sloth:
    - path: ./slos/*.yaml
      required: true
analysis:
  includeTransitiveDependencies: true
  unresolvedReferencePolicy: error
policy:
  failOnCriticalLegacyConsumer: true
  failOnCriticalUnknown: true
  minimumBlockingCriticality: high
output:
  formats: [console, json, markdown]
```

Begin with sources your repository owns. Mark a source `required` when its
absence must prevent a safety decision. See the
[configuration guide](docs/CONFIGURATION.md) and
[adapter catalog](docs/ADAPTERS.md).

### 3. Run one authoritative evaluation

```bash
telemetry-change-guard check \
  --config ./tcg.yaml \
  --changes ./changes.yaml \
  --mode enforce \
  --format console \
  --json-output ./tcg-result.json
```

The console report and versioned JSON come from the same evaluation; remote
evidence is not queried twice. The command accepts exactly one change source:

| Workflow | Arguments |
| --- | --- |
| Explicit ChangeSet | `--changes ./changes.yaml` |
| Prometheus contract comparison | `--baseline ./main.json --candidate ./candidate.json` |
| OpenTelemetry Weaver diff | `--weaver-diff ./diff.json --weaver-mapping ./mapping.yaml` |

Planned migrations use the compatibility workflow:

```bash
telemetry-change-guard migration check \
  --config ./tcg.yaml \
  --plan ./migration.yaml
```

### 4. Read the decision

| Status | Exit | Meaning | Typical response |
| --- | ---: | --- | --- |
| `PASS` | `0` | No blocking impact was found under configured evidence and policy | Continue normal review |
| `WARN` | `0` | Known risk remains visible but current policy permits it | Review and schedule migration |
| `BLOCK` | `2` | Complete evidence proves an enforced policy violation | Migrate the named consumers or make an explicit policy change |
| `INCOMPLETE` | `3` | Required evidence is missing, malformed, dynamic, or unresolved | Fix the source, mapping, or query before deciding |
| `ERROR` | `1` | Input, configuration, discovery, or evaluation failed safely | Correct the reported error and rerun |

Findings remain visible even when `INCOMPLETE` or `ERROR` takes precedence.
Review confirmed impact and diagnostics together.

### 5. Send the result where it belongs

| Need | Option or command |
| --- | --- |
| Human terminal report | `--format console` |
| Versioned automation result | `--format json --output ./tcg-result.json` |
| Reviewable Markdown | `--format markdown --output ./tcg-report.md` |
| Console or Markdown plus JSON | `--json-output ./tcg-result.json` |
| Status-only integration | `--status-output ./tcg-status.txt` |
| Complete configured-evidence graph | `telemetry-change-guard graph --config ./tcg.yaml --output ./graph.json` |

Machine integrations should consume `tcg-result/v1alpha1` JSON and the
authoritative status, not parse console prose. See the [CLI guide](docs/CLI.md).

## Coverage

TCG separates change capture, evidence discovery, graph analysis, and policy.
New adapters can improve evidence coverage without becoming decision makers.

### Change sources

| Source | Implemented behavior |
| --- | --- |
| Explicit ChangeSet | Strict metric, label, span-attribute, and resource-attribute rename/removal contracts in documented domains |
| Prometheus snapshots | Bounded metadata/series capture, byte-stable baseline/candidate diff, and ChangeSet generation for removed metrics and labels |
| OpenTelemetry Weaver | Structured V1/V2 registry-diff import with mandatory explicit Prometheus mappings or documented ignore decisions |
| Migration plans | Legacy and replacement telemetry contracts normalized into the shared model with the established readiness result |

Snapshot additions remain visible but are not breaking changes. Metric type or
unit drift remains required uncertainty until it can be represented without
guessing. See [change sources](docs/CHANGE_SOURCES.md) and the
[migration model](docs/MIGRATION_MODEL.md).

### Consumers and evidence

| Evidence | What TCG establishes |
| --- | --- |
| Prometheus rules and `PrometheusRule` CRDs | Alert and recording-rule dependencies through the official PromQL AST; recording outputs create transitive graph edges |
| Grafana | Prometheus queries in exported dashboard JSON, nested panels, and API envelopes; unresolved templates stay visible |
| Sloth and Pyrra | Critical SLO indicator dependencies |
| KEDA | Prometheus triggers in `ScaledObject` resources and production-aware `SCALING_RISK` |
| Argo Rollouts | Prometheus measurements in analysis templates and `DEPLOYMENT_GATE_RISK` |
| Kubernetes HPA | External, object, and pods metrics through explicit Kubernetes-to-Prometheus mappings; same-name inference is forbidden |
| Runtime query history | Deterministically aggregated Prometheus query logs and provider-neutral history; absence never proves non-use |
| Perses metrics usage | Optional bounded dashboard and rule usage evidence, reparsed locally |
| Tempo / TraceQL | Strict local trace-query inventories validated by Tempo, with explicit OpenTelemetry attribute mappings |
| Ownership | Advisory enrichment from repository metadata, CODEOWNERS, and Grafana tags; ownership never changes status |

Local sources require no network. Perses and Tempo are optional, read-only
remote adapters with strict origins, bounded responses, same-origin redirects,
timeouts, and environment-variable credential references. Remote evidence is
disabled by default in the GitHub Action.

### Graph, impact, and policy

| Capability | Product behavior |
| --- | --- |
| Typed query analysis | Prometheus official PromQL AST and configured Tempo validation for TraceQL |
| Dependency graph | Direct, cycle-safe, transitive paths through produced signals such as recording rules |
| Impact taxonomy | `VISIBILITY_LOSS`, `ALERTING_RISK`, `SLO_RISK`, `SCALING_RISK`, `DEPLOYMENT_GATE_RISK`, `AUTOMATION_RISK`, and `SEMANTIC_RISK` |
| Immutable findings | Change, consumer, criticality, source, provenance, references, and dependency paths are retained before policy |
| Adoption modes | `audit`, `warn`, and `enforce` change handling without hiding findings |
| Fail-closed precedence | `ERROR` → `INCOMPLETE` → `BLOCK` → `WARN` → `PASS` |
| Stable machine contract | Versioned results and graph JSON with authoritative status and exit semantics |

The default enforcement policy warns on visibility and semantic findings and
blocks high or critical alerting, SLO, scaling, deployment-gate, and automation
risk. See the [safety engine](docs/SAFETY_ENGINE.md).

### Commands

```text
telemetry-change-guard init              create a safe runnable starter
telemetry-change-guard validate          validate configuration and change inputs
telemetry-change-guard snapshot          capture a bounded Prometheus contract
telemetry-change-guard diff              compare baseline and candidate contracts
telemetry-change-guard check             make one authoritative safety decision
telemetry-change-guard impact            explain paths from one Prometheus metric
telemetry-change-guard graph             export the configured dependency graph
telemetry-change-guard migration check   evaluate cutover readiness
telemetry-change-guard migration advise  request an optional AI explanation
telemetry-change-guard migration remediate validate an in-memory repair candidate
```

The temporary `tmr` executable and `tmr/v1alpha1` configuration remain
compatible with the existing migration contract.

### Runnable product scenarios

| Scenario | Change and configured dependency | Expected result |
| --- | --- | --- |
| [Getting started](examples/getting-started) | Removed metric → critical alert | `BLOCK` with `ALERTING_RISK` |
| [Observability migration](examples/checkout-migration) | Metric and label rename → Grafana, Sloth SLO, recording rule, transitive alert, and one unresolved query | `INCOMPLETE` with all seven confirmed findings retained |
| [KEDA](examples/keda) | Removed metric → production `ScaledObject` | `BLOCK` with `SCALING_RISK` |
| [Argo Rollouts](examples/argo-rollouts) | Removed metric → rollout analysis measurement | `BLOCK` with `DEPLOYMENT_GATE_RISK` |
| [Kubernetes HPA](examples/hpa) | Removed external metric → explicitly mapped HPA | `BLOCK` with `SCALING_RISK` |
| [Snapshot diff](examples/snapshot-diff) | Baseline/candidate Prometheus contracts | Deterministic diff and safety evaluation |

## AI-assisted workflows

**AI proposes. Humans approve. TCG verifies and decides.**

AI is optional and never part of the safety authority. The deterministic
engine owns discovery from configured sources, graph traversal, impact,
policy, status, and exit codes.

| AI role | What it can do | Boundary |
| --- | --- | --- |
| Reader / migrator | Inspect code or a diff and draft a ChangeSet, snapshot, configuration, mapping, or migration plan | TCG validates structure; a human confirms scope and semantic mappings |
| Explainer | Summarize blockers, dependency paths, and missing evidence through `migration advise` | Receives a bounded, redacted evidence packet and cannot change status |
| Fixer | Draft eligible replacement PromQL through `migration remediate` | TCG reparses and reanalyzes in memory; it does not edit source or claim semantic equivalence |
| Change-request assistant | Draft tests, runbooks, tickets, or review text | Generated material remains a candidate for human review |
| Agentic repair experiment | Edit one isolated workspace, receive deterministic feedback, retry, and produce an uncommitted diff | Never approves, commits, pushes, opens, or merges a change request |

An external coding agent can help translate observable code changes into TCG
inputs, but neither that agent nor `validate` can prove it saw every producer
change or consumer. Prefer deterministic snapshots or mapped Weaver diffs when
available.

### Explain or draft a bounded repair

```bash
telemetry-change-guard migration advise \
  --config ./tcg.yaml \
  --plan ./migration.yaml \
  --question "Why is this blocked, what is missing, and what should move first?" \
  --ai-command ./my-tcg-ai-provider

telemetry-change-guard migration remediate \
  --config ./tcg.yaml \
  --plan ./migration.yaml \
  --ai-command ./my-tcg-ai-provider
```

No model, account, SDK, or hosted provider is bundled. Users choose an
executable implementing the vendor-neutral JSON process protocol and remain
responsible for its data access, retention, training, and confidentiality
terms.

The optional agentic MVP demonstrates a bounded
`attempt → TCG decision → repair → recheck → human review` loop in a hardened
container. `PASS` or visible `WARN` can produce `REVIEW_READY`; `BLOCK` can
retry at most three times. Uncertainty, error, timeout, malformed output,
tampering, workspace escape, or exhausted retries stop safely.

> **The agentic loop is experimental, not a supported production feature.**
> Start with its [five-minute fixture](experiments/agentic/README.md) and read
> the [AI workflows](docs/AI_WORKFLOWS.md),
> [remediation contract](docs/REMEDIATION.md),
> [agentic roadmap](docs/AGENTIC_ROADMAP.md), and
> [threat model](docs/THREAT_MODEL.md).

## GitHub Action

Add the same deterministic decision to a pull request without installing the
CLI locally:

```yaml
permissions:
  contents: read
  pull-requests: write

steps:
  - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
  - id: telemetry
    uses: tadurisaikiran/telemetry-change-guard@20dbd241b3934418a4d288d05dd69eb55ad85079
    with:
      config: tcg.yaml
      changes: changes.yaml
      remote-evidence: disabled
```

The Action performs one evaluation, records CLI build identity, appends a
Markdown job summary, uploads versioned JSON evidence, creates or updates one
bounded pull-request comment, and enforces the exact CLI exit code. Missing
artifacts, ambiguous change sources, and status/exit disagreement fail closed.

Use `baseline` plus `candidate` for snapshots,
`weaver-diff` plus `weaver-mapping` for Weaver, or `migration` for a migration
plan. See the [Action guide](docs/GITHUB_ACTION.md) and
[secure CI guide](docs/SECURE_CI_USAGE.md).

There is no stable `v1` tag yet. Pin the exact reviewed commit above; **do not
use `@v1`**.

## Trust and safety model

- **Deterministic authority:** no LLM controls findings, policy, status, or
  exit codes.
- **Formal parsing:** PromQL uses Prometheus's official AST. TraceQL requires
  configured Tempo validation. Regex and name similarity do not prove a
  dependency.
- **Explicit domain mappings:** Prometheus, OpenTelemetry, Tempo, Kubernetes,
  and other identities remain separate unless mapping evidence connects them.
- **Fail-closed evidence:** missing, malformed, dynamic, denied, or unresolved
  required evidence becomes `INCOMPLETE` or `ERROR`.
- **Auditable findings:** file, line, expression, extraction method,
  confidence, provenance, owner, and dependency path are retained when known.
- **Bounded untrusted input:** trusted repository roots, symlink rejection,
  aggregate limits, graph and finding limits, response bounds, redirects, and
  timeouts constrain local and remote evidence.
- **Local-first operation:** the core does not phone home, execute analyzed
  queries, or require persistent storage.

The repository gates changes with race-enabled tests, parser fuzz smoke,
vulnerability analysis, CodeQL, dependency review, workflow-policy checks,
external Action-consumer fixtures, reproducible release builds, native archive
execution on Linux/macOS/Windows, multi-platform container verification, and
pinned live lifecycles. See [testing and verification](docs/TESTING.md).

## Project status and honest boundaries

TCG is pre-release software. The implemented product is ready for controlled
evaluation against supported evidence, not a claim of universal or stable
production coverage.

- Packaged binaries, a container image, Homebrew formula, stable release, and
  `v1` Action tag are not published. Use the exact reviewed commit.
- Results are scoped to configured evidence. Inaccessible repositories or
  systems remain outside the result.
- Native arbitrary source-code diff extraction is not shipped. Use a
  ChangeSet, snapshot pair, mapped Weaver diff, or migration plan; AI may draft
  those inputs but cannot certify completeness.
- A snapshot describes what one queried Prometheus deployment exposed; empty
  runtime history never proves that a signal is unused.
- Similar names across domains never create an implicit mapping.
- Bounded CloudFormation and Cloud Assembly loading exists, but CloudWatch
  consumer safety decisions are not implemented. See the
  [AWS boundary](docs/AWS.md).
- LogQL, OpenTelemetry Collector discovery, MCP, server/UI modes, and
  organization-wide orchestration remain roadmap work.
- The agentic repair loop is experimental and additive; it does not replace
  the CLI, Action, schemas, or deterministic engine.

Unsupported and unresolved behavior is documented because honest uncertainty
is part of the safety contract. See [limitations](docs/LIMITATIONS.md),
[compatibility](docs/COMPATIBILITY.md), and the [roadmap](docs/ROADMAP.md).

## Choose your next step

- **Evaluate in five minutes:** run the [quickstart](docs/QUICKSTART.md).
- **Bring a real use case:** follow the
  [design-user program](docs/DESIGN_USER_PROGRAM.md) with sanitized evidence.
- **Add a required check:** use the [GitHub Action](docs/GITHUB_ACTION.md) after
  deciding which evidence must be required.
- **Understand the design:** read the
  [architecture](docs/ARCHITECTURE.md) and
  [safety engine](docs/SAFETY_ENGINE.md).
- **Understand the problem:** read
  [When Telemetry Migrations Fail Silently](docs/articles/when-telemetry-migrations-fail-silently.md).
- **Contribute:** review the [roadmap](docs/ROADMAP.md),
  [related work](RELATED_WORK.md), and [contribution guide](CONTRIBUTING.md).

Organizations publicly using TCG may opt in through [ADOPTERS.md](ADOPTERS.md).
Private evaluation never becomes a public adopter claim without authorization.

## Documentation

| Goal | Guide |
| --- | --- |
| Start and diagnose | [Quickstart](docs/QUICKSTART.md) · [Installation](docs/INSTALLATION.md) · [Troubleshooting](docs/TROUBLESHOOTING.md) · [FAQ](docs/FAQ.md) · [Limitations](docs/LIMITATIONS.md) |
| Understand the system | [Architecture](docs/ARCHITECTURE.md) · [Safety engine](docs/SAFETY_ENGINE.md) · [Threat model](docs/THREAT_MODEL.md) |
| Define inputs | [ChangeSet](docs/CHANGESET.md) · [Change sources](docs/CHANGE_SOURCES.md) · [Migration model](docs/MIGRATION_MODEL.md) · [Configuration](docs/CONFIGURATION.md) |
| Run locally or in CI | [CLI](docs/CLI.md) · [GitHub Action](docs/GITHUB_ACTION.md) · [Secure CI usage](docs/SECURE_CI_USAGE.md) · [Testing](docs/TESTING.md) |
| Understand versions and upgrades | [Changelog](CHANGELOG.md) · [Versioning](docs/VERSIONING.md) · [Compatibility](docs/COMPATIBILITY.md) · [Upgrading](docs/UPGRADING.md) |
| Rehearse or verify a release | [Release status](docs/RELEASES.md) · [Release procedure](docs/RELEASING.md) · [Artifact and provenance verification](docs/VERIFY_RELEASE.md) · [Repository settings](docs/REPOSITORY_SETTINGS.md) |
| Configure consumers | [Adapters](docs/ADAPTERS.md) · [KEDA](docs/KEDA.md) · [Argo Rollouts](docs/ARGO_ROLLOUTS.md) · [HPA](docs/HPA.md) |
| Add change or runtime evidence | [Weaver](docs/WEAVER.md) · [Perses](docs/PERSES.md) · [Runtime queries](docs/RUNTIME_EVIDENCE.md) · [Tempo/TraceQL](docs/TEMPO.md) |
| Add human or AI context | [Ownership](docs/OWNERSHIP.md) · [AI workflows](docs/AI_WORKFLOWS.md) · [AI explanations](docs/AI_AGENT.md) · [Remediation](docs/REMEDIATION.md) · [Agentic roadmap](docs/AGENTIC_ROADMAP.md) |
| Evaluate maturity and scope | [Evaluation kit](evaluation/README.md) · [Benchmark](benchmarks/README.md) · [Design-user program](docs/DESIGN_USER_PROGRAM.md) · [Roadmap](docs/ROADMAP.md) · [Related work](RELATED_WORK.md) · [AWS boundary](docs/AWS.md) |
| Prepare owner-approved communication | [Draft launch kit](docs/launch/README.md) · [Adoption evidence rules](evaluation/ADOPTION_EVIDENCE.md) |

## Development

```bash
make verify
```

Docker is required only for the live E2E suites. Changes to discovery, graph
traversal, or policy require tests proving both safe and unsafe directions.
Read [CONTRIBUTING.md](CONTRIBUTING.md), [GOVERNANCE.md](GOVERNANCE.md), and
the [security policy](SECURITY.md) before contributing or reporting a
vulnerability.

## License

Apache License 2.0. See [LICENSE](LICENSE).
