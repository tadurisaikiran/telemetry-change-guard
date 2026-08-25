# Configuration

Telemetry Change Guard uses a strict, versioned product configuration. The
canonical envelope is:

```yaml
apiVersion: tcg/v1alpha1
kind: Config
sources:
  prometheusRules:
    - path: ./monitoring/*.yaml
      required: true
  keda:
    - path: ./deploy/scaledobjects/*.yaml
      required: true
  argoRollouts:
    - path: ./deploy/analysis-templates/*.yaml
      required: true
  horizontalPodAutoscalers:
    - path: ./deploy/hpa/*.yaml
      mapping: ./config/hpa-mapping.yaml
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

Unknown fields, additional YAML documents, invalid source settings, and files
larger than the configured decoder limit are rejected. Defaults are applied
before validation, and every accepted document becomes one canonical in-memory
`Config` value.

Filesystem roots, aggregate resource limits, analysis timeouts, remote-access
authorization, and credential transport exceptions are execution policy. They
are accepted only as trusted CLI/Action inputs and are deliberately excluded
from `tcg.yaml`, so repository-controlled data cannot relax its own boundary.
The CLI defaults `--repository-root` to the current working directory, rejects
symlinked inputs and evidence, and fails if a configured local source escapes.

## Legacy document compatibility

Existing documents remain accepted without edits:

```yaml
apiVersion: tmr/v1alpha1
sources:
  prometheusRules:
    - ./monitoring/*.yaml
```

The legacy document may omit `kind`, as it did historically. Both envelopes
normalize to `tcg/v1alpha1` and `kind: Config` internally; analysis, policy,
reports, and exit codes do not branch on which envelope was loaded. Invalid or
unknown versions fail explicitly.

## Environment-variable compatibility

Configuration stores secret references, never secret values. Product-owned
references use the `TCG_*` prefix:

```yaml
sources:
  persesUsage:
    - url: https://metrics-usage.example.com
      bearerTokenEnv: TCG_PERSES_TOKEN
```

All remote evidence requires execution policy supplied outside this
configuration: remote access must be explicitly enabled and every source needs
an exact allowed origin. Credentialed access additionally requires HTTPS,
except for an explicitly enabled loopback-only development exception. The
GitHub Action accepts only the fixed `TCG_REMOTE_BEARER_TOKEN` reference and
defaults remote evidence to disabled. See [Secure CI usage](SECURE_CI_USAGE.md).

For every configured `TCG_NAME`, Telemetry Change Guard resolves environment
values as follows:

| Environment | Result |
| --- | --- |
| only `TCG_NAME` is set | use the canonical value |
| only `TMR_NAME` is set | use the legacy fallback |
| both are set to the same value | use the canonical value |
| both are set differently | fail with `ERROR` before discovery |
| neither is set, or the selected value is empty | report the existing source failure |

Legacy configuration references such as `bearerTokenEnv: TMR_PERSES_TOKEN`
are normalized to the canonical name and follow the same rules. A conflict
error includes variable names only; it never includes either value. Names
outside the product-owned `TCG_*` and `TMR_*` prefixes retain exact lookup
semantics and do not gain implicit aliases.

This compatibility currently applies to configured remote-adapter bearer-token
references. CLI paths and policy rollout remain explicit flags, which keeps a
run reproducible and prevents ambient environment state from silently changing
the requested inputs.

## Source and policy reference

Implemented sources and their failure semantics are documented in the
[adapter guide](ADAPTERS.md), [Perses guide](PERSES.md),
[runtime evidence guide](RUNTIME_EVIDENCE.md), [Tempo guide](TEMPO.md), and
[KEDA guide](KEDA.md), [Argo Rollouts guide](ARGO_ROLLOUTS.md), and
[Kubernetes HPA guide](HPA.md).
Generic safety policy and migration-readiness policy remain separate as
documented in the [safety engine guide](SAFETY_ENGINE.md).
