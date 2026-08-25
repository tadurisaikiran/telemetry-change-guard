#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/tcg-docs.XXXXXX")"
trap 'rm -rf -- "${temporary}"' EXIT

cd -- "${root}"
"${GO:-go}" run ./internal/docverify --root "${root}"
./scripts/check-release-coordinates.sh

"${GO:-go}" build -buildvcs=false -trimpath \
  -o "${temporary}/telemetry-change-guard" \
  ./cmd/telemetry-change-guard
"${temporary}/telemetry-change-guard" version --format json >"${temporary}/version.json"
grep -Fq '"schemaVersion": "tcg-version/v1alpha1"' "${temporary}/version.json"
"${temporary}/telemetry-change-guard" validate \
  --changes ./examples/getting-started/changes.yaml

set +e
"${temporary}/telemetry-change-guard" check \
  --config ./examples/getting-started/tcg.yaml \
  --changes ./examples/getting-started/changes.yaml \
  --mode enforce \
  --format json \
  --output "${temporary}/result.json"
status=$?
set -e
[[ "${status}" -eq 2 ]] || {
  echo "getting-started check returned ${status}; want 2" >&2
  exit 1
}
grep -Fq '"schemaVersion": "tcg-result/v1alpha1"' "${temporary}/result.json"
grep -Fq '"status": "BLOCK"' "${temporary}/result.json"

echo "Documentation commands and machine-output contracts verified."
