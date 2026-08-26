#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/tcg-ai-canaries.XXXXXX")"
trap 'rm -rf -- "${temporary}"' EXIT

go_command="${GO:-go}"
binary="${temporary}/telemetry-change-guard"
provider="${temporary}/ai-provider"
"${go_command}" build -buildvcs=false -trimpath -o "${binary}" "${root}/cmd/telemetry-change-guard"
"${go_command}" build -buildvcs=false -trimpath -o "${provider}" "${root}/scripts/testdata/ai-provider"

config="${root}/examples/checkout-migration/tcg.yaml"
plan="${root}/examples/checkout-migration/migration.yaml"

set +e
"${binary}" migration advise \
  --repository-root "${root}" \
  --config "${config}" \
  --plan "${plan}" \
  --question "Which deterministic blocker should be migrated first?" \
  --ai-command "${provider}" \
  > "${temporary}/advise.txt" 2> "${temporary}/advise.stderr"
advise_exit=$?
set -e
[[ "${advise_exit}" -eq 2 ]] || {
  echo "AI explanation canary exited ${advise_exit}; expected 2" >&2
  sed -n '1,120p' "${temporary}/advise.stderr" >&2
  exit 1
}
grep -Fq 'AI Explanation (non-authoritative)' "${temporary}/advise.txt"
grep -Fq 'Authoritative status: BLOCKED' "${temporary}/advise.txt"
grep -Fq 'Authoritative status remains: BLOCKED' "${temporary}/advise.txt"
grep -Fq 'This fixture explains evidence but cannot change readiness.' "${temporary}/advise.txt"

dashboard="${root}/examples/checkout-migration/grafana/checkout-legacy.json"
dashboard_hash_before="$(git -C "${root}" hash-object -- "${dashboard}")"
set +e
"${binary}" migration remediate \
  --repository-root "${root}" \
  --config "${config}" \
  --plan "${plan}" \
  --ai-command "${provider}" \
  > "${temporary}/remediate.txt" 2> "${temporary}/remediate.stderr"
remediate_exit=$?
set -e
[[ "${remediate_exit}" -eq 2 ]] || {
  echo "AI remediation canary exited ${remediate_exit}; expected 2" >&2
  sed -n '1,120p' "${temporary}/remediate.stderr" >&2
  exit 1
}
grep -Fq 'VALIDATED CANDIDATE 1' "${temporary}/remediate.txt"
grep -Fq 'CANDIDATES ONLY — NO FILES WERE MODIFIED' "${temporary}/remediate.txt"
grep -Fq 'consumer => MIGRATED' "${temporary}/remediate.txt"
grep -Fq 'Current authoritative status remains: BLOCKED' "${temporary}/remediate.txt"
dashboard_hash_after="$(git -C "${root}" hash-object -- "${dashboard}")"
[[ "${dashboard_hash_after}" == "${dashboard_hash_before}" ]] || {
  echo "AI remediation canary modified its source dashboard" >&2
  exit 1
}

echo "Optional AI canaries passed: non-authoritative explanation and validated in-memory remediation"
