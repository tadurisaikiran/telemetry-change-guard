# AI-assisted workflows

Telemetry Change Guard is designed for teams that want the speed of an AI
assistant without delegating a production-safety decision to a language model.
The division of responsibility is deliberate:

> AI can be the reader, explainer, fixer, and migration assistant—never the
> judge. Humans approve. TCG verifies and decides.

TCG's parsers, dependency graph, impact classification, policy, and process
exit code remain deterministic. AI is optional, disabled unless explicitly
invoked, and cannot override the authoritative result.

## Capability map

The distinction between built-in and externally orchestrated behavior matters
when evaluating the product:

| Capability | Status | What TCG guarantees |
| --- | --- | --- |
| Explain and prioritize migration findings | Built in through `migration advise` | A bounded, redacted request, strict response schema, non-authoritative rendering, and unchanged readiness exit code |
| Draft a replacement expression | Built in through `migration remediate` for eligible local Prometheus and Grafana rename targets | Strict candidate schema plus PromQL, artifact, graph, and policy validation against an in-memory copy |
| Read application code or a source diff and draft TCG inputs | Compatible external AI workflow | Strict input decoding once the draft reaches `validate`; TCG does not guarantee the AI found every change |
| Draft bounded edits in a disposable workspace and recheck them | Experimental `experiments/agentic` MVP | A container-isolated adapter can propose changes; the public TCG CLI independently rechecks the actual tree and produces an uncommitted review diff |
| Edit a branch or open a change request | Compatible external coding-agent workflow | Nothing until the real edited tree is checked again; TCG itself never writes the branch edit or opens the request |
| Draft tests, runbooks, tickets, or review summaries | Compatible external AI workflow | Versioned deterministic findings can be used as source material; generated prose has no authority |

The current release does not bundle a model, model SDK, source-code scanner,
agent runtime, or hosted AI service. A provider is a user-selected executable
that may connect to a local model or an approved remote service.

## 1. Use AI as a change reader

An external coding assistant can inspect an application repository,
instrumentation changes, or a proposed diff and draft:

- a `tcg/v1alpha1` ChangeSet or bounded `TelemetrySnapshot`;
- a `tcg.yaml` product configuration;
- explicit OpenTelemetry, Weaver, or HPA mappings; and
- ownership metadata.

Treat every generated file as a candidate. Language models can miss dynamic
instrumentation, generated code, conditional paths, other repositories, or
runtime-only telemetry. They can also invent a relationship based on similar
names. In particular:

- never claim that an AI found *all* telemetry changes;
- require a human to confirm repository and change scope;
- require domain owners to review semantic mappings; and
- prefer snapshot diffs or explicitly mapped Weaver diffs when those
  deterministic sources are available.

Pass the candidate through the public validators before analysis:

```bash
telemetry-change-guard validate --changes ./candidate-changes.yaml
telemetry-change-guard validate --config ./candidate-tcg.yaml

telemetry-change-guard check \
  --config ./candidate-tcg.yaml \
  --changes ./candidate-changes.yaml
```

Validation proves that the document satisfies TCG's accepted structure and
constraints. It does not prove that the inventory is complete or that a
human-authored or AI-authored mapping is semantically correct.

## 2. Use AI as an evidence explainer

`migration advise` is the built-in read-only AI workflow:

```bash
telemetry-change-guard migration advise \
  --config ./tcg.yaml \
  --plan ./migration.yaml \
  --question "Why is this blocked, what evidence is missing, and what should we migrate first?" \
  --ai-command ./my-tcg-ai-provider \
  --ai-timeout 30s
```

TCG first performs the deterministic migration analysis. It then sends only a
bounded evidence packet containing the authoritative status, canonical
changes, aggregate counts, blocking or uncertain findings, relevant paths,
available ownership/runtime context, and diagnostics. Unaffected and
already-migrated repository content is not included.

Useful questions include:

- Why is this migration blocked?
- Which confirmed or uncertain consumers carry the highest risk?
- What is the dependency path to this transitive alert?
- Which team should coordinate each migration, based on deterministic
  ownership evidence?
- What mapping, runtime evidence, or source coverage is missing?

The response may contain an explanation, priorities, and limitations. It has
no status, patch, command, or mutation field. TCG rejects unknown fields and
unknown consumer IDs, labels the answer non-authoritative, and preserves the
deterministic readiness exit code.

## 3. Use AI as a constrained fixer

`migration remediate` implements generative remediation for a narrow set of
targets:

```bash
telemetry-change-guard migration remediate \
  --config ./tcg.yaml \
  --plan ./migration.yaml \
  --ai-command ./my-tcg-ai-provider \
  --ai-timeout 30s
```

The complete built-in loop is:

1. TCG finds an eligible `LEGACY_ONLY` consumer with a confirmed direct
   reference and an explicit rename destination.
2. The provider returns an expression replacement for the opaque target TCG
   selected.
3. TCG parses the new PromQL, proves that the old symbol is absent and the
   explicit destination is present, and locates exactly one source scalar.
4. TCG edits only an in-memory copy, reparses the full artifact, rebuilds the
   graph, and reruns the configured readiness policy.
5. TCG prints `VALIDATED CANDIDATE` only if those checks pass. The current
   source and authoritative status remain unchanged.

Eligibility is intentionally limited to confirmed direct rename references in
local Prometheus rule YAML and exported Grafana dashboard JSON. Removals,
unresolved findings, transitive-only consumers, remote resources, SLO files,
arbitrary source files, and secret-redacted targets are not sent for
remediation.

A validated candidate proves syntax and dependency movement under TCG's
model. It does not prove equivalent query semantics. Window lengths,
aggregations, label logic, thresholds, and operational intent still require
human review and independent tests.

## 4. Add a human-governed change-request loop

Teams can combine the built-in candidate validation with an external coding
agent or automation:

```text
TCG finds the migration target
  -> AI drafts an expression replacement
  -> TCG validates it in memory
  -> external agent applies the selected edit on a branch
  -> tests and human review approve the actual diff
  -> TCG re-verifies the checked-out branch in CI
```

This realizes the “engine finds -> AI drafts -> human approves -> engine
re-verifies” workflow today, with one important qualification: branch and
change-request creation are orchestration outside TCG. Do not describe TCG as
opening, approving, or merging a pull request or change request.

TCG now includes an explicitly experimental isolated runner and bounded repair
controller under [`experiments/agentic`](../experiments/agentic/README.md). It
produces an uncommitted review bundle and never runs unless its separate binary
and acknowledgement flag are selected. Comparative evaluation and any
supported design-user workflow remain future gates in the
[optional agentic roadmap](AGENTIC_ROADMAP.md).

The final check must run against the actual edited files. A previously
simulated result is not evidence that the committed change is safe.

## Additional high-value AI uses

These workflows require no AI authority and can be added around TCG's
deterministic outputs:

| Use | Safe source of truth | Required review |
| --- | --- | --- |
| Onboarding copilot | Repository layout plus TCG configuration schemas | Engineers confirm required sources, repository boundaries, mappings, and credentials |
| Evidence-gap assistant | `INCOMPLETE` findings and diagnostics | Owners decide how to collect or require missing evidence; AI cannot turn absence into safety |
| Migration planner | Finding criticality, paths, owners, and runtime counts | Teams confirm sequencing, maintenance windows, and operational dependencies |
| Test author | Validated candidate plus existing rule and dashboard tests | Engineers verify assertions and run product-specific semantic tests |
| Runbook or ticket writer | Versioned JSON result and source locators | Generated text must retain exact findings and authoritative status |
| Review summarizer | Actual branch diff plus the post-edit TCG result | Reviewer validates the real diff; an earlier candidate result is insufficient |
| Iterative migration agent | One approved edit followed by a fresh TCG run | Stop on `BLOCKED`, `INCOMPLETE`, `ERROR`, or unexpected findings rather than reasoning around them |

Future product integrations can make these loops more convenient, but should
preserve the same separation: models propose; deterministic evidence and
policy decide.

## Security and provider responsibility

The built-in `migration advise` and `migration remediate` provider protocols
minimize data but are not sandboxes. Their selected executable runs with the
user's operating-system permissions and may access files, environment
variables, or the network independently. Use only a reviewed provider, and
evaluate the model service's access, retention, and training policies before
sending sensitive operational data.

The separate experimental agentic harness adds container isolation, resource
limits, an immutable image ID, no network by default, and one writable
workspace mount. Its [security and operational limits](../experiments/agentic/README.md#security-and-operational-limits)
still require a trusted container runtime, trusted local repository, reviewed
adapter image, and human review.

Repository text is untrusted and may contain prompt injection. TCG marks it as
data, bounds requests and responses, redacts common secret patterns, rejects
unknown protocol fields, and removes terminal control characters. Pattern
redaction cannot prove that every possible secret format is absent.

Read the exact [explanation protocol](AI_AGENT.md),
[candidate-remediation contract](REMEDIATION.md), and
[threat model](THREAT_MODEL.md) before enabling a provider in a sensitive
repository.

## Non-negotiable claims

- No built-in AI provider response can create, remove, suppress, weaken, or
  override a TCG finding.
- AI never decides `PASS`, `WARN`, `BLOCK`, `READY`, `BLOCKED`, `INCOMPLETE`,
  or `ERROR`.
- AI-inferred absence never proves that a telemetry dependency is unused.
- AI-generated mappings, inputs, code, and prose remain candidates until
  validated and reviewed.
- Human approval does not replace the final TCG run against the actual change.
