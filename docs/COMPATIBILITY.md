# Compatibility policy

This document separates what the source tree currently verifies from what the
unpublished alpha release still needs to prove.

## Current compatibility matrix

| Surface | Current contract |
| --- | --- |
| Source build | Go `1.27.0`; the module declares `go 1.27.0`, and other toolchain lines are not release-gated yet |
| Canonical CLI | `telemetry-change-guard`; generic `PASS/WARN/ERROR/BLOCK/INCOMPLETE` exits remain `0/0/1/2/3` |
| Migration compatibility | Temporary `tmr` executable plus canonical `migration` subcommands; existing `READY/BLOCKED/INCOMPLETE/ERROR` exits remain `0/2/3/1` |
| Configuration | Strict `tcg/v1alpha1`; legacy `tmr/v1alpha1` continues to normalize through the shared model |
| Machine results | `tcg-result/v1alpha1` and `tmr-result/v1alpha1` remain separate and unchanged by build identity |
| GitHub Action | Hosted smoke tests currently run on `ubuntu-latest`; other runner operating systems are not yet claimed |
| Local development | Verified on macOS ARM64 and hosted Linux AMD64; this is development evidence, not yet a published binary support promise |

The release-snapshot phase must build and smoke-test its complete documented
platform matrix before those platforms become alpha release claims. Windows,
multi-architecture containers, and package-manager installation remain pending
until their dedicated gates pass.

## Evidence and adapter compatibility

TCG analyzes supported, configured evidence. Adding an adapter may expand
coverage but may not change policy authority, downgrade required evidence, or
reinterpret an existing schema silently. Dynamic or unsupported queries remain
visible as diagnostics; required uncertainty fails closed.

Remote HTTP APIs are compatibility surfaces only within their documented
bounded subsets. Authentication, origin allowlisting, redirect policy, and
response limits are security contracts and are not relaxed for compatibility.

## GitHub Action compatibility

Consumers should pin the Action to the exact verified commit published in the
README and [`release/metadata.env`](../release/metadata.env). Inputs and outputs
are additive during alpha where practical. Removing or changing an input,
output, status, artifact contract, or exit behavior requires changelog and
upgrade guidance plus a new compatible coordinate.

## AI and agentic boundaries

Optional AI providers may explain deterministic evidence or propose candidate
repairs. They cannot change TCG's status, exit code, policy, or evidence graph.
The isolated agentic loop is experimental and not part of the alpha's stable
compatibility promise.
