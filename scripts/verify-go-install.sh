#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
module="github.com/tadurisaikiran/telemetry-change-guard/cmd/telemetry-change-guard"
reference="${1:?usage: verify-go-install.sh <git-ref> [expected-version]}"
expected_version="${2:-}"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/tcg-go-install.XXXXXX")"
trap 'rm -rf -- "${temporary}"' EXIT

GOBIN="${temporary}/bin" "${GO:-go}" install "${module}@${reference}"
binary="${temporary}/bin/telemetry-change-guard"
[[ -x "${binary}" ]] || {
  echo "go install did not create telemetry-change-guard" >&2
  exit 1
}

identity="$(${binary} version --format json)"
grep -Fq '"schemaVersion": "tcg-version/v1alpha1"' <<<"${identity}"
if [[ -n "${expected_version}" ]]; then
  grep -Fq "\"version\": \"${expected_version}\"" <<<"${identity}" || {
    echo "go install identity does not report ${expected_version}:" >&2
    echo "${identity}" >&2
    exit 1
  }
elif grep -Fq '"version": "dev"' <<<"${identity}"; then
  echo "go install from immutable ref reported an unidentifiable dev version" >&2
  exit 1
fi

set +e
(
  cd -- "${root}"
  "${binary}" check \
    --config examples/getting-started/tcg.yaml \
    --changes examples/getting-started/changes.yaml \
    --format json >"${temporary}/result.json"
)
actual_exit=$?
set -e
[[ "${actual_exit}" -eq 2 ]]
grep -Fq '"status": "BLOCK"' "${temporary}/result.json"

echo "Verified go install ${module}@${reference} and the getting-started BLOCK contract"
