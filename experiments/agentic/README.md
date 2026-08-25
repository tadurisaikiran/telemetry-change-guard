# Experimental agentic feedback loop

This directory contains an opt-in, provider-neutral MVP for a bounded
`agent attempt -> TCG decision -> repair -> recheck -> human review` loop. The
coding agent proposes edits. The existing public `telemetry-change-guard`
executable remains the only safety authority.

> **Experimental—not a supported production feature.** The harness runs only
> when a separate binary is built and `--acknowledge-experimental` is supplied.
> It never commits, pushes, opens or merges a change request, approves a diff,
> or calls production APIs. A `REVIEW_READY` result still requires independent
> tests, security review, and human approval.

## What the MVP proves

- A user-selected agent adapter can edit one declared workspace in a fresh,
  detached Git worktree.
- The adapter runs in a container with a read-only root filesystem, no Linux
  capabilities, `no-new-privileges`, an unprivileged host UID/GID, bounded
  memory/CPU/PIDs/output/time, and no network by default.
- The only writable bind mount is the declared agent workspace. TCG policy,
  ChangeSet/evidence, the TCG executable, previous artifacts, and controller
  stay on the host and are not mounted into the agent container.
- Task, adapter request/response, attempt, TCG result, and run-result contracts
  are strict, versioned, bounded JSON. Agent prose cannot carry a status,
  command, patch, approval, or policy mutation.
- Control-file hashes are verified before and after each agent attempt and TCG
  evaluation. Changes outside the declared workspace, symlinks, special files,
  oversized workspaces/diffs, and changed control files fail closed.
- `PASS` and visible `WARN` produce an uncommitted review diff; `BLOCK` returns
  bounded deterministic feedback for at most three attempts; `INCOMPLETE`,
  `ERROR`, timeout, malformed output, integrity failure, and retry exhaustion
  stop safely.

This MVP accepts an explicit ChangeSet. It does not scan source code, prove
that every telemetry change was discovered, execute repository-specific tests,
or prove semantic equivalence of an AI-authored query. Those remain separate
human, CI, and evaluation responsibilities.

## Five-minute local fixture

Prerequisites are Go 1.27+, Git, Docker, and a running local Docker daemon.
Run these commands from the repository root:

```bash
mkdir -p ./bin
go build -o ./bin/telemetry-change-guard ./cmd/telemetry-change-guard
go build -o ./bin/tcg-agent-eval ./experiments/agentic/cmd/tcg-agent-eval

./experiments/agentic/testdata/adapter/build-image.sh tcg-agent-fixture:local

./bin/tcg-agent-eval \
  --acknowledge-experimental \
  --task ./experiments/agentic/testdata/repair/task.json \
  --output ./agentic-run \
  --tcg-command ./bin/telemetry-change-guard \
  --agent-image tcg-agent-fixture:local \
  --agent-command /tcg-agent-fixture
```

`--output` must name a path that does not already exist. The fixture deliberately
leaves attempt 1 unchanged, receives TCG's authoritative `BLOCK` findings, edits
the Prometheus rule on attempt 2, and is re-evaluated as `PASS`. The command
prints a small JSON summary and exits with:

| Exit | Outcome | Meaning |
| ---: | --- | --- |
| `0` | `REVIEW_READY` | TCG returned `PASS` or visible `WARN`; review the diff |
| `2` | `BLOCKED` | The bounded repair attempts were exhausted |
| `3` | `INCOMPLETE` | Required evidence was incomplete; escalate rather than guess |
| `1` | failure | TCG `ERROR`, timeout, malformed output, sandbox, tool, or integrity failure |
| `64` | usage error | Required experimental acknowledgement or command arguments are invalid |

Inspect `agentic-run/run.json` first. Each `attempt-NNN/` directory contains the
exact adapter request/response, bounded stderr, workspace diff, authoritative
TCG JSON, companion status, and TCG stderr. `final.diff` is the review artifact.
It is never applied to the source checkout automatically.

## Task contract

Task paths are resolved relative to the task document. `repository.revision`
must resolve to a commit. `agentWorkspace` is a repository-relative directory;
the adapter sees only that directory at `/workspace`.

```json
{
  "schemaVersion": "tcg-agent-task/v1alpha1",
  "id": "metric-rename-review",
  "description": "Update declared consumers for the reviewed metric rename.",
  "repository": {"path": "../../repo", "revision": "HEAD"},
  "agentWorkspace": "observability",
  "tcg": {
    "config": "control/tcg.yaml",
    "changes": "control/changes.yaml",
    "timeout": "30s"
  },
  "integrityPaths": ["control", "oracle-tests"],
  "limits": {
    "maxAttempts": 3,
    "agentTimeout": "2m",
    "totalTimeout": "15m",
    "maxChangedFiles": 256,
    "maxDiffBytes": 4194304
  }
}
```

Unknown fields, symlinked control paths, invalid paths, unsupported file types,
oversized documents, and limits above the hard ceilings are rejected. Put every
read-only evidence or oracle-test path in `integrityPaths`; do not put secrets in
the task description.

## Adapter protocol

The image is resolved to its immutable `sha256:` image ID before the first
attempt. Its absolute `--agent-command` is invoked directly—never through a
shell. The adapter reads one `tcg-agent-request/v1alpha1` JSON object from stdin,
edits files only beneath `/workspace`, and writes exactly one response to
stdout:

```json
{
  "schemaVersion": "tcg-agent-response/v1alpha1",
  "summary": "Updated two recording rules; TCG must re-verify them.",
  "changedFiles": ["prometheus/rules.yaml"],
  "limitations": ["Query semantic equivalence still needs owner review."]
}
```

`changedFiles` and `summary` are untrusted claims. The controller derives the
actual changed paths and binary review diff from Git. On a retry, `feedback`
contains only bounded deterministic status, finding identity/risk/criticality,
source locators, dependency paths, and required diagnostics. Repository text is
explicitly labeled untrusted data in the guardrails.

Pass adapter arguments with repeated `--agent-arg`. Environment variables are
not inherited except a small runtime allowlist; each model credential must be
explicitly named with repeated `--agent-env`. Network is `none` by default.
`--agent-network bridge` is an explicit confidentiality and supply-chain opt-in
for adapters that call an approved remote provider.

## Security and operational limits

The container runtime and local repository are trusted inputs. The adapter
image and all repository/model text are untrusted. A compromised container
runtime or daemon is outside this isolation boundary. Repository checkout can
also invoke locally configured Git content filters, so do not point the harness
at an untrusted repository or Git configuration.

The container controls are defense in depth, not proof that every host kernel
or runtime vulnerability is impossible. Use a dedicated runner for sensitive
evaluation, pin and scan the adapter image, avoid mounting a container socket,
keep network disabled where possible, review provider retention/training terms,
and never supply production credentials. Common secret patterns are redacted
from bounded feedback, but pattern matching cannot prove arbitrary secrets are
absent.

The MVP intentionally does not execute repository tests because running
untrusted repository code on the host would violate the isolation boundary.
Run approved product-specific tests in a separate, equally isolated CI job
against the review diff. A TCG `PASS` proves only the modeled telemetry safety
contract for the provided evidence.

See the [agentic roadmap](../../docs/AGENTIC_ROADMAP.md),
[AI workflows](../../docs/AI_WORKFLOWS.md), and
[threat model](../../docs/THREAT_MODEL.md) before evaluating a real adapter.
