# Contributing to Telemetry Change Guard

Thank you for helping make telemetry migrations safer. Telemetry Change Guard
is deterministic and fail-closed: a missing dependency is more dangerous than
an extra finding.
Changes that affect discovery, graph traversal, or readiness therefore need
evidence-focused tests.

Project authority and decision-making are documented in
[`GOVERNANCE.md`](GOVERNANCE.md); current merge authorities are listed in
[`MAINTAINERS.md`](MAINTAINERS.md). Review [`RELATED_WORK.md`](RELATED_WORK.md)
before proposing a capability on the basis that no existing tool addresses it.

## Development workflow

1. Search existing issues and discussions before proposing work.
2. Open or claim an issue so the scope and acceptance criteria are visible.
3. Create a focused branch from `main`; do not develop directly on `main`.
4. Add tests at the appropriate unit, component, golden, integration, or E2E
   layer.
5. Open a pull request that links the issue and explains safety implications.
6. Address review feedback with additional commits so engineering history
   remains understandable.

Maintainers use the same issue → branch → pull-request workflow. Small typo or
documentation fixes may skip a prior issue when their scope is self-evident.

## Local setup

Telemetry Change Guard requires the security-patched Go version declared in
`go.mod` (currently Go 1.26.7). The core test suite uses only local fixtures.

```bash
go mod download
go test ./...
go vet ./...
go test -race ./...
```

Before opening a pull request, also verify formatting:

```bash
test -z "$(gofmt -l .)"
```

Docker is required only for the live E2E suite.

## Engineering rules

- The deterministic generic safety and migration-readiness evaluators own every
  authoritative status in their respective versioned contracts.
- Never use an LLM where a formal parser or deterministic algorithm exists.
- Never interpret parse, source, or adapter failure as evidence of absence.
- Preserve source, expression, extraction method, and confidence for findings.
- Keep Prometheus, OpenTelemetry, Tempo, and Loki domains explicit.
- Keep external products behind small adapters.
- Avoid new persistence or network dependencies in the local core.
- Add a regression test for every bug fix.

PromQL work must use Prometheus's official AST. New consumer adapters should
return canonical `domain.Discovery` data and must not decide readiness.

## Test expectations

- Parser and algorithm changes: unit tests and relevant fuzz seeds.
- Adapter changes: component fixtures for real supported document shapes.
- Graph/readiness changes: transitive, cycle, and fail-closed tests.
- Machine schema changes: update the version deliberately and update goldens.
- E2E changes: prove both predicted failure and successful migration.

Do not refresh a golden file until you have reviewed and can explain every
changed field.

## Pull requests

Keep pull requests narrow enough to review. Describe the problem, the chosen
contract, test evidence, compatibility impact, and any known limitations. A
maintainer may ask to split unrelated changes.

By participating, you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md).
Security vulnerabilities must follow [SECURITY.md](SECURITY.md), not a public
issue.

Organizations that want to identify themselves as users should follow the
authorized opt-in process in [`ADOPTERS.md`](ADOPTERS.md). Maintainers do not
turn private evaluations into public adopter claims.
