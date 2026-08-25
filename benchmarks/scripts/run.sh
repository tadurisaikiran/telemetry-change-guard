#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)"
output="${1:-${root}/dist/benchmark/results.json}"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/tcg-benchmark.XXXXXX")"
trap 'rm -rf -- "${temporary}"' EXIT

"${GO:-go}" build -buildvcs=false -trimpath \
  -o "${temporary}/telemetry-change-guard" \
  "${root}/cmd/telemetry-change-guard"

cd -- "${root}"
"${GO:-go}" run ./benchmarks/cmd/tcg-benchmark \
  --binary "${temporary}/telemetry-change-guard" \
  --manifest benchmarks/manifest/corpus.json \
  --output "${output}" \
  --root "${root}"

echo "Machine-readable benchmark result: ${output}"
