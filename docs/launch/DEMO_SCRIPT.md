# Demo script

> **Draft — for an owner-approved live or recorded demonstration.** Use the
> exact reviewed commit, a clean terminal, and synthetic fixtures. Do not show
> credentials, private repositories, production endpoints, or participant data.

## Demo promise

In five minutes, show a one-line metric removal, the critical alert normal CI
does not connect to it, and TCG's deterministic `BLOCK` with source evidence.

## Preflight

1. Install the exact candidate commit from [Quickstart](../QUICKSTART.md).
2. Check out the matching repository commit.
3. Run the demo once and confirm expected exit `2`.
4. Reset the terminal to the repository root.
5. Increase terminal font size and hide notifications.
6. Keep `git status --short` clean and network access unnecessary during demo.

## Five-minute version

### 0:00–0:40 — Frame the failure

Say:

> A metric is an operational API. Alerts, SLOs, dashboards, autoscalers, and
> rollout gates consume it. Renaming the producer can pass normal CI while
> breaking those consumers elsewhere.

Show:

```bash
sed -n '1,80p' examples/getting-started/changes.yaml
```

Point to the removal of `checkout_requests_total`.

### 0:40–1:30 — Show the consumer

```bash
sed -n '1,80p' examples/getting-started/prometheus/rules.yaml
```

Say:

> This critical alert still queries the metric. The producer change and the
> operational consumer are valid files independently; the missing check is
> their contract relationship.

### 1:30–2:10 — Show configuration and scope

```bash
sed -n '1,120p' examples/getting-started/tcg.yaml
```

Say:

> We make scope explicit. This rules source is required, transitive analysis is
> enabled, unresolved evidence is an error, and high criticality can block.
> The result covers configured evidence, not inaccessible systems.

### 2:10–3:20 — Run TCG

```bash
telemetry-change-guard validate \
  --changes ./examples/getting-started/changes.yaml

telemetry-change-guard check \
  --config ./examples/getting-started/tcg.yaml \
  --changes ./examples/getting-started/changes.yaml \
  --mode enforce
```

The check intentionally exits `2`. Point to:

- `Status: BLOCK`;
- `ALERTING_RISK`;
- `CheckoutTrafficMissing`;
- criticality;
- source line;
- dependency path; and
- policy reason.

Say:

> Exit two means analysis succeeded and policy rejected the change. Missing
> required evidence has a separate incomplete status and exit three.

### 3:20–4:10 — Show machine evidence

Run again with one companion JSON result:

```bash
set +e
telemetry-change-guard check \
  --config ./examples/getting-started/tcg.yaml \
  --changes ./examples/getting-started/changes.yaml \
  --format console \
  --json-output /tmp/tcg-demo-result.json
set -e
sed -n '1,100p' /tmp/tcg-demo-result.json
```

Say:

> Human review and CI consume the same evaluation. The JSON schema and top-level
> status are versioned; integrations do not need to scrape prose.

### 4:10–5:00 — Close with boundaries and action

Say:

> TCG is local-first and deterministic. AI can draft inputs or fixes, but it
> cannot change this result. This alpha candidate has strong regression and
> hosted verification, while external design-user evidence is still being
> collected. Try one sanitized representative change and tell us whether you
> would make this a required pull-request check.

Show the [Quickstart](../QUICKSTART.md) and
[Limitations](../LIMITATIONS.md).

## Ten-minute extensions

Choose at most two based on the audience:

- `make benchmark` to show exact expected-versus-actual regression contracts;
- `examples/keda` for scaling risk;
- `examples/argo-rollouts` for deployment-gate risk;
- `examples/hpa` for an explicit cross-system mapping;
- `examples/checkout-migration` for transitive paths and unresolved evidence;
- `telemetry-change-guard graph` for machine graph export; or
- the GitHub Action summary/artifact from a synthetic pull request.

When showing the benchmark, say “11 synthetic release-gate cases passed.” Do
not translate that result into a field accuracy percentage.

## Common questions

**Does it find consumers in every repository?**

No. It evaluates configured local and optional remote evidence. Teams decide
which sources are required, and unresolved required evidence fails incomplete.

**Why not let an LLM decide?**

Models are useful readers and fixers, but safety status needs a reproducible
contract. Optional AI stays outside the decision boundary.

**Can it run without Go?**

Prepared release archives, container, and Homebrew paths are pending owner
publication. The current immutable evaluation path uses Go 1.27+ or the pinned
GitHub Action.

**Is it ready to become a required check?**

That is an environment-specific design-user decision. Start in audit/warn,
verify representative safe/block/incomplete cases, review the alpha limits,
then choose enforcement and rollback deliberately.

## Abort conditions

Do not continue a public demo if the exact version is wrong, the working tree
contains private files, expected output differs, a release command returns
`404`, hosted checks are failing, or the speaker cannot accurately explain an
`INCOMPLETE` result. Fix the environment and rehearse again.
