#!/usr/bin/env bash
set -euo pipefail

e2e_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
repo_dir=$(cd "${e2e_dir}/.." && pwd)
compose=(docker compose --file "${e2e_dir}/docker-compose.yaml")
tmr_bin=${TMR_BIN:-"${TMPDIR:-/tmp}/tmr-e2e"}
tcg_bin=${TCG_BIN:-"${TMPDIR:-/tmp}/telemetry-change-guard-e2e"}
snapshot_dir=$(mktemp -d)
baseline_snapshot="${snapshot_dir}/baseline.json"
candidate_snapshot="${snapshot_dir}/candidate.json"

cleanup() {
  if [[ -n "${TMR_SCENARIO_DIR:-}" && -n "${TMR_EXPORT_MODE:-}" ]]; then
    "${compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

if [[ -z "${TMR_BIN:-}" ]]; then
  (cd "${repo_dir}" && go build -trimpath -o "${tmr_bin}" ./cmd/tmr)
fi
if [[ -z "${TCG_BIN:-}" ]]; then
  (cd "${repo_dir}" && go build -trimpath -o "${tcg_bin}" ./cmd/telemetry-change-guard)
fi

run_analysis() {
  local expected=$1
  local expected_exit=$2
  shift 2
  local report_file
  report_file=$(mktemp)
  set +e
  (cd "${repo_dir}" && "${tmr_bin}" analyze \
    --config "${TMR_SCENARIO_DIR}/tmr.yaml" \
    --format json --output "${report_file}" \
    "$@")
  local actual_exit=$?
  set -e
  if [[ "${actual_exit}" -ne "${expected_exit}" ]]; then
    printf 'Telemetry Change Guard exit code %s, expected %s for %s\n' \
      "${actual_exit}" "${expected_exit}" "${TMR_SCENARIO_DIR}" >&2
    return 1
  fi
  if ! grep --quiet '"status": "'"${expected}"'"' "${report_file}"; then
    printf 'Telemetry Change Guard did not report %s for %s\n' \
      "${expected}" "${TMR_SCENARIO_DIR}" >&2
    return 1
  fi
}

run_tmr() {
  run_analysis "$1" "$2" --migration e2e/migrations/metric-rename.yml
}

run_weaver_tmr() {
  run_analysis "$1" "$2" \
    --weaver-diff e2e/weaver/diff-v2.json \
    --weaver-mapping e2e/weaver/mapping.yaml
}

run_scenario() {
  local name=$1
  local export_mode=$2
  local expected_status=$3
  local expected_exit=$4
  local present=$5
  local absent=$6
  local runtime=$7

  export TMR_SCENARIO_DIR="${e2e_dir}/scenarios/${name}"
  export TMR_EXPORT_MODE="${export_mode}"
  export TMR_EXPECT_PRESENT="${present}"
  export TMR_EXPECT_ABSENT="${absent}"
  export TMR_EXPECT_RUNTIME="${runtime}"

  printf '\n=== %s (%s) ===\n' "${name}" "${expected_status}"
  "${compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
  "${compose[@]}" config --quiet
  "${compose[@]}" run --rm promtool check rules /scenario/prometheus/rules.yml
  "${compose[@]}" run --rm sloth validate -i /slo/checkout-slo.yml
  sloth_output=$(mktemp)
  "${compose[@]}" run --rm sloth generate -i /slo/checkout-slo.yml > "${sloth_output}"
  grep --quiet 'checkout:requests:rate1m' "${sloth_output}"

  if [[ "${expected_status}" != "BASELINE" ]]; then
    run_tmr "${expected_status}" "${expected_exit}"
    run_weaver_tmr "${expected_status}" "${expected_exit}"
  fi

  "${compose[@]}" up --detach --build exporter prometheus grafana
  "${e2e_dir}/scripts/wait-for-stack.sh"
  "${e2e_dir}/scripts/assert-prometheus.sh"
  if [[ "${name}" == "01-before" ]]; then
    "${tcg_bin}" snapshot --prometheus http://127.0.0.1:19090 \
      --name lifecycle-baseline --output "${baseline_snapshot}"
  elif [[ "${name}" == "07-legacy-removed" ]]; then
    "${tcg_bin}" snapshot --prometheus http://127.0.0.1:19090 \
      --name lifecycle-candidate --output "${candidate_snapshot}"
  fi
  "${compose[@]}" down --volumes --remove-orphans
}

export TMR_SCENARIO_DIR="${e2e_dir}/scenarios/01-before"
export TMR_EXPORT_MODE=old
"${compose[@]}" run --rm promtool test rules /common/rule-tests.yml

run_scenario 01-before old BASELINE 0 checkout_request_duration_seconds_count checkout_server_request_duration_seconds_count healthy
run_scenario 02-dual-write dual BLOCKED 2 checkout_request_duration_seconds_count '' healthy
run_scenario 03-partial dual BLOCKED 2 checkout_server_request_duration_seconds_count '' healthy
run_scenario 04-uncertain dual INCOMPLETE 3 checkout_server_request_duration_seconds_count '' healthy
run_scenario 05-migrated dual READY 0 checkout_server_request_duration_seconds_count '' healthy
run_scenario 06-premature-cutover new BLOCKED 2 checkout_server_request_duration_seconds_count checkout_request_duration_seconds_count broken
run_scenario 07-legacy-removed new READY 0 checkout_server_request_duration_seconds_count checkout_request_duration_seconds_count healthy

snapshot_diff="${snapshot_dir}/diff.json"
snapshot_changes="${snapshot_dir}/changes.yaml"
snapshot_result="${snapshot_dir}/result.json"
"${tcg_bin}" diff \
  --baseline "${baseline_snapshot}" \
  --candidate "${candidate_snapshot}" \
  --output "${snapshot_diff}" \
  --changes-output "${snapshot_changes}"
grep --quiet '"kind": "metric_removed"' "${snapshot_diff}"
grep --quiet '"metric": "checkout_request_duration_seconds"' "${snapshot_diff}"
grep --quiet '"kind": "metric_added"' "${snapshot_diff}"
grep --quiet '"metric": "checkout_server_request_duration_seconds"' "${snapshot_diff}"
"${tcg_bin}" validate --changes "${snapshot_changes}"
"${tcg_bin}" check \
  --config "${e2e_dir}/scenarios/07-legacy-removed/tmr.yaml" \
  --baseline "${baseline_snapshot}" \
  --candidate "${candidate_snapshot}" \
  --format json \
  --output "${snapshot_result}"
grep --quiet '"status": "PASS"' "${snapshot_result}"

printf '\nTelemetry Change Guard live E2E lifecycle passed.\n'
