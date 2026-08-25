# Design User Program

The design-user program tests whether Telemetry Change Guard (TCG) produces
useful, trustworthy evidence for real telemetry changes. It is not a launch
list, sales funnel, or request for endorsements.

The primary product question is:

> **Would you make this a required pull-request check? Why or why not?**

Participants should try to break the tool. A missing dependency, false safety
decision, confusing uncertainty, or difficult setup is more useful than
general praise.

## Initial targets

The first program milestone is:

- 10–20 discovery conversations;
- 5–10 hands-on evaluations;
- evaluations inside at least 3 real repositories, even if only faithful
  sanitized reproductions can be retained publicly; and
- at least 1 repository or organization that chooses to keep TCG as a CI
  check.

These are learning targets, not adoption claims. An organization is not an
adopter until an authorized representative opts in through
[`ADOPTERS.md`](../ADOPTERS.md).

## Participant profile

A useful design user owns or regularly changes two or more of these:

- application metrics or OpenTelemetry instrumentation;
- Prometheus or PrometheusRule recording and alerting rules;
- Grafana dashboards;
- Sloth or Pyrra SLO definitions;
- KEDA ScaledObjects;
- Argo Rollouts analysis templates; or
- Kubernetes HPA external, object, or pods metrics.

A large installation is not required. Prefer a case with multiple consumer
types, a transitive recording-rule dependency, or a production control-loop
consumer.

## Before the session

The canonical repository is pre-release. Until
[issue #29](https://github.com/tadurisaikiran/telemetry-change-guard/issues/29)
publishes verified artifacts, build the fully tested commit used by the Action
examples:

```bash
git checkout 626f5443021f14a8a4a3ddb67e8b7af6e92afed8
mkdir -p ./bin
go build -trimpath -o ./bin/telemetry-change-guard ./cmd/telemetry-change-guard
```

Record the exact commit in the evaluation. Do not ask a participant to use an
unpublished `v1` tag.

Use the versioned [evaluation record and privacy templates](../evaluation/README.md)
to pre-register expected behavior, compare the machine result, anonymize a
reproduction, and separate private participation from public-reference consent.

## Evaluation workflows

Choose one primary workflow per session. More than one may be evaluated if
time permits.

### A. Planned migration

Use a migration plan when the old and destination contracts are both known and
the goal is to prove cutover readiness:

```bash
telemetry-change-guard migration validate --plan ./migration.yaml
telemetry-change-guard migration check \
  --config ./tcg.yaml \
  --plan ./migration.yaml
```

This compatibility workflow reports `READY`, `BLOCKED`, `INCOMPLETE`, or
`ERROR` and is useful for phased metric or label migrations.

### B. Proposed breaking change

Use a native ChangeSet for an explicit rename, removal, or other supported
telemetry contract change:

```bash
telemetry-change-guard validate --changes ./changes.yaml
telemetry-change-guard check \
  --config ./tcg.yaml \
  --changes ./changes.yaml \
  --mode enforce
```

If Prometheus endpoints are available, a baseline/candidate snapshot pair can
replace a handwritten ChangeSet. Review the complete diff before treating it
as the proposed change:

```bash
telemetry-change-guard diff \
  --baseline ./main-contract.json \
  --candidate ./candidate-contract.json \
  --changes-output ./changes.yaml
```

### C. Control-plane impact

Use the generic `check` workflow with at least one KEDA, Argo Rollouts, or
explicitly mapped HPA source. Confirm that the report distinguishes scaling
risk from deployment-gate risk and retains the exact consumer and source
location. The repository includes runnable
[`examples/keda`](../examples/keda),
[`examples/argo-rollouts`](../examples/argo-rollouts), and
[`examples/hpa`](../examples/hpa) starting points.

AWS/CloudWatch evaluations will become a fourth workflow only after consumer
discovery is implemented and tested. The current AWS adapter loads synthesized
CloudFormation artifacts but does not yet make CloudWatch safety decisions.

## Safety and privacy

TCG is local-first, but configuration and consumer files can contain sensitive
names, queries, repository paths, URLs, namespaces, or operational thresholds.

- Never request credentials, tokens, customer data, production endpoints, or
  unredacted telemetry.
- Never ask a participant to upload proprietary artifacts to a public issue.
- Run TCG inside the participant's environment when artifacts cannot leave it.
- Share aggregate counts, permanent diagnostic codes, and newly constructed
  synthetic reproductions instead of sensitive input.
- Remove organization, service, owner, path, namespace, and dashboard
  identifiers unless they are required to reproduce a defect.
- Report suspected vulnerabilities through GitHub private vulnerability
  reporting, as described in [`SECURITY.md`](../SECURITY.md).
- Do not publish a participant's name, organization, quotation, logo, or case
  study without separate approval from an authorized representative.

## The 45-minute evaluation

### 1. Establish the expected result — 5 minutes

Before running TCG, record:

- the selected workflow;
- the telemetry change and expected affected consumers;
- which evidence sources are required;
- the expected status; and
- the most serious outcome if a dependency is missed.

Recording the expectation first prevents the tool output from changing the
participant's recollection afterward.

### 2. Prepare the smallest faithful input — 10 minutes

Use local, sanitized artifacts and include only the sources required for the
case. Validate the ChangeSet or migration plan before checking it. Capture
validation errors and permanent diagnostic codes instead of silently working
around them.

### 3. Run one authoritative check — 10 minutes

Capture human-readable and versioned JSON results from the same evaluation:

```bash
telemetry-change-guard check \
  --config ./tcg.yaml \
  --changes ./changes.yaml \
  --format console \
  --json-output ./tcg-result.json
```

The generic exit contract is:

| Exit | Status | Meaning |
| ---: | --- | --- |
| `0` | `PASS` or `WARN` | Policy permits the change. Findings may remain. |
| `1` | `ERROR` | The tool, configuration, adapter, or runtime failed. |
| `2` | `BLOCK` | Policy rejects at least one known impact. |
| `3` | `INCOMPLETE` | Required evidence is missing or unresolved. |

The migration workflow uses the same numeric meanings with `READY` and
`BLOCKED` in place of `PASS` and `BLOCK`.

### 4. Inspect one dependency path — 10 minutes

Select the most critical or surprising result, then check its consumer,
criticality, source location, expression, provenance, and transitive path:

```bash
telemetry-change-guard impact \
  --config ./tcg.yaml \
  --symbol checkout_requests_total
telemetry-change-guard graph \
  --config ./tcg.yaml \
  --output ./dependency-graph.json
```

### 5. Debrief — 10 minutes

Ask these questions in order:

1. Would you make this a required pull-request check? Why or why not?
2. Did TCG match the expected status?
3. Did it miss any critical consumer or report a false dependency?
4. Was every `INCOMPLETE` result specific and actionable?
5. Could you identify the next file or owner needed to make the change safe?
6. Which setup step or output required explanation?
7. What is the smallest change that would make you run TCG again?

## Evaluation record

Create one record per hands-on evaluation, including sessions that find no
bug. Keep identities and contact details outside the technical record.

```yaml
session_id: anonymous-01
tcg_version: commit-or-release
workflow: proposed_breaking_change
change_kind: metric_remove
consumer_types:
  - keda
expected_status: BLOCK
actual_status: BLOCK
exit_code: 2
critical_consumers_expected: 1
critical_consumers_found: 1
false_positives: 0
critical_false_negatives: 0
unresolved_critical: 0
time_to_first_result_minutes: 8
would_require_in_pr: yes
reproducible_fixture: true
follow_up_issue: issue-number-or-none
```

Do not convert a private response into a public adoption claim. Public adopter
entries and case studies require explicit, separate consent.

## Triage order

Handle findings in this order:

1. Possible false `PASS`/`READY` or a missed critical dependency.
2. Parser, adapter, or evidence failure interpreted as absence.
3. Incorrect `BLOCK`, `BLOCKED`, or `INCOMPLETE` result.
4. Incorrect source location, provenance, or transitive path.
5. Installation, documentation, and workflow friction.
6. Feature requests outside the supported contract.

Every correctness fix needs a sanitized regression fixture and a test at the
lowest useful layer. Safety-decision changes also require appropriate
integration or E2E coverage.

## Outreach draft

The maintainer should edit and send this personally. Do not automate outreach
or add recipients to a mailing list.

> I am testing an open-source, local CLI that checks whether a telemetry change
> can break alerts, SLOs, dashboards, recording rules, autoscalers, or
> deployment gates. I am looking for engineers willing to spend 45 minutes
> trying it against a real or faithful sanitized change and showing me where
> the result is wrong, incomplete, or hard to use. You would not need to share
> proprietary files. Would this be relevant to a change your team is handling?

Follow up once after a reasonable interval and make it easy to decline.

## Program completion criteria

The initial milestone is complete when:

- the conversation and evaluation targets above are met;
- every session has an expected-versus-actual record;
- every possible safety false negative has a sanitized regression or a private
  security report;
- recurring onboarding friction is fixed or documented;
- at least one evaluation exercises a transitive dependency;
- at least one evaluation exercises a control-plane consumer;
- at least one required unknown returns `INCOMPLETE` rather than false safety;
- at least one independent repository chooses to keep TCG in CI; and
- no public result identifies a participant without explicit consent.

Use the design-user feedback issue form only for sanitized public findings.
Public fixtures and further evaluation tooling are tracked in
[issue #32](https://github.com/tadurisaikiran/telemetry-change-guard/issues/32).
