#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
fixture="${root}/release-fixtures/external-consumer-repository"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/tcg-consumer-fixtures.XXXXXX")"
trap 'rm -rf -- "${temporary}"' EXIT

binary="${temporary}/telemetry-change-guard"
"${GO:-go}" build -buildvcs=false -trimpath -o "${binary}" "${root}/cmd/telemetry-change-guard"

run_case() {
  local name="$1"
  local expected_status="$2"
  local expected_exit="$3"
  local expected_schema="$4"
  shift 4

  local report="${temporary}/${name}.json"
  local actual_exit
  set +e
  (
    cd -- "${fixture}"
    "${binary}" "$@" --format json --output "${report}"
  )
  actual_exit=$?
  set -e

  [[ "${actual_exit}" -eq "${expected_exit}" ]] || {
    echo "${name}: exit ${actual_exit}; expected ${expected_exit}" >&2
    return 1
  }
  grep -Fq "\"schemaVersion\": \"${expected_schema}\"" "${report}" || {
    echo "${name}: missing schema ${expected_schema}" >&2
    return 1
  }
  grep -Fq "\"status\": \"${expected_status}\"" "${report}" || {
    echo "${name}: missing status ${expected_status}" >&2
    return 1
  }
}

run_case pass PASS 0 tcg-result/v1alpha1 \
  check --config fixtures/pass/tcg.yaml --changes fixtures/pass/changes.yaml
run_case block BLOCK 2 tcg-result/v1alpha1 \
  check --config fixtures/block/tcg.yaml --changes fixtures/block/changes.yaml
run_case incomplete INCOMPLETE 3 tcg-result/v1alpha1 \
  check --config fixtures/incomplete/tcg.yaml --changes fixtures/incomplete/changes.yaml
run_case snapshot BLOCK 2 tcg-result/v1alpha1 \
  check --config fixtures/snapshot/tcg.yaml \
  --baseline fixtures/snapshot/baseline.json --candidate fixtures/snapshot/candidate.json
run_case migration READY 0 tmr-result/v1alpha1 \
  migration check --config fixtures/migration/tcg.yaml --plan fixtures/migration/migration.yaml

echo "Verified external-consumer PASS, BLOCK, INCOMPLETE, snapshot, and migration contracts"
