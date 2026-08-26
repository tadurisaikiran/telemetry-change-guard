#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/tcg-product-canaries.XXXXXX")"
trap 'rm -rf -- "${temporary}"' EXIT

binary="${TCG_BINARY:-}"
if [[ -z "${binary}" ]]; then
  binary="${temporary}/telemetry-change-guard"
  "${GO:-go}" build -buildvcs=false -trimpath -o "${binary}" "${root}/cmd/telemetry-change-guard"
fi
binary="$(cd -- "$(dirname -- "${binary}")" && pwd -P)/$(basename -- "${binary}")"
[[ -x "${binary}" ]] || {
  echo "product canary binary is not executable: ${binary}" >&2
  exit 1
}

last_report=""
run_case() {
  local name="$1"
  local workdir="$2"
  local expected_status="$3"
  local expected_exit="$4"
  local expected_schema="$5"
  shift 5

  local report="${temporary}/${name}.json"
  local status_file="${temporary}/${name}.status"
  local actual_exit
  set +e
  (
    cd -- "${workdir}"
    "${binary}" "$@" --format json --output "${report}" --status-output "${status_file}"
  )
  actual_exit=$?
  set -e

  [[ "${actual_exit}" -eq "${expected_exit}" ]] || {
    echo "${name}: exit ${actual_exit}; expected ${expected_exit}" >&2
    exit 1
  }
  [[ "$(tr -d '\r\n' < "${status_file}")" == "${expected_status}" ]] || {
    echo "${name}: authoritative status artifact does not equal ${expected_status}" >&2
    exit 1
  }
  grep -Fq "\"schemaVersion\": \"${expected_schema}\"" "${report}" || {
    echo "${name}: missing schema ${expected_schema}" >&2
    exit 1
  }
  grep -Fq "\"status\": \"${expected_status}\"" "${report}" || {
    echo "${name}: JSON result does not contain expected status ${expected_status}" >&2
    exit 1
  }
  last_report="${report}"
}

# Use case 1: protect a proposed telemetry change before merge.
run_case proposed-change-block "${root}" BLOCK 2 tcg-result/v1alpha1 \
  check \
  --config ./examples/getting-started/tcg.yaml \
  --changes ./examples/getting-started/changes.yaml \
  --mode enforce
grep -Fq '"name": "CheckoutTrafficMissing"' "${last_report}"
grep -Fq '"impact": "ALERTING_RISK"' "${last_report}"

consumer_fixture="${root}/release-fixtures/external-consumer-repository"
run_case proposed-change-pass "${consumer_fixture}" PASS 0 tcg-result/v1alpha1 \
  check \
  --config fixtures/pass/tcg.yaml \
  --changes fixtures/pass/changes.yaml \
  --mode enforce
grep -Fq '"findings": []' "${last_report}"

# Use case 2: gate a planned migration before and after consumer cutover.
run_case migration-blocked "${root}" BLOCKED 2 tmr-result/v1alpha1 \
  migration check \
  --config ./e2e/scenarios/01-before/tmr.yaml \
  --plan ./e2e/migrations/metric-rename.yml
grep -Fq '"legacyOnly": 5' "${last_report}"
grep -Fq '"migrated": 0' "${last_report}"
grep -Fq '"progressPercent": 0' "${last_report}"
grep -Fq '"name": "CheckoutLatencyHigh"' "${last_report}"

run_case migration-ready "${root}" READY 0 tmr-result/v1alpha1 \
  migration check \
  --config ./e2e/scenarios/05-migrated/tmr.yaml \
  --plan ./e2e/migrations/metric-rename.yml
grep -Fq '"legacyOnly": 0' "${last_report}"
grep -Fq '"migrated": 5' "${last_report}"
grep -Fq '"progressPercent": 100' "${last_report}"
grep -Fq '"name": "CheckoutLatencyHigh"' "${last_report}"

echo "Primary product canaries passed: proposed-change BLOCK/PASS and migration BLOCKED/READY"
