#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
image="${1:?usage: verify-container.sh <image>}"
: "${TCG_VERSION:?TCG_VERSION is required}"
: "${TCG_COMMIT:?TCG_COMMIT is required}"
: "${TCG_BUILD_DATE:?TCG_BUILD_DATE is required}"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/tcg-container-smoke.XXXXXX")"
trap 'rm -rf -- "${temporary}"' EXIT

[[ "$(docker image inspect --format '{{.Config.User}}' "${image}")" == "nonroot:nonroot" ]]
[[ "$(docker image inspect --format '{{json .Config.Entrypoint}}' "${image}")" == '["/telemetry-change-guard"]' ]]
[[ "$(docker image inspect --format '{{.Config.WorkingDir}}' "${image}")" == "/workspace" ]]

check_label() {
  local key="$1"
  local expected="$2"
  local actual
  actual="$(docker image inspect --format "{{index .Config.Labels \"${key}\"}}" "${image}")"
  [[ "${actual}" == "${expected}" ]] || {
    echo "container label ${key}=${actual}; expected ${expected}" >&2
    exit 1
  }
}

check_label org.opencontainers.image.version "${TCG_VERSION}"
check_label org.opencontainers.image.revision "${TCG_COMMIT}"
check_label org.opencontainers.image.created "${TCG_BUILD_DATE}"
check_label org.opencontainers.image.licenses Apache-2.0
check_label org.opencontainers.image.source https://github.com/tadurisaikiran/telemetry-change-guard

size="$(docker image inspect --format '{{.Size}}' "${image}")"
[[ "${size}" =~ ^[0-9]+$ && "${size}" -le 33554432 ]] || {
  echo "container size ${size} exceeds the 32 MiB release budget" >&2
  exit 1
}

runtime=(
  docker run --rm
  --read-only
  --network none
  --cap-drop ALL
  --security-opt no-new-privileges
)

identity="$("${runtime[@]}" "${image}" version --format json)"
grep -Fq "\"version\": \"${TCG_VERSION}\"" <<<"${identity}"
grep -Fq "\"commit\": \"${TCG_COMMIT}\"" <<<"${identity}"
grep -Fq "\"buildDate\": \"${TCG_BUILD_DATE}\"" <<<"${identity}"
grep -Fq '"dirty": false' <<<"${identity}"

set +e
"${runtime[@]}" \
  --volume "${root}/examples/getting-started:/workspace/examples/getting-started:ro" \
  "${image}" check \
    --config examples/getting-started/tcg.yaml \
    --changes examples/getting-started/changes.yaml \
    --format json >"${temporary}/result.json"
actual_exit=$?
set -e
[[ "${actual_exit}" -eq 2 ]]
grep -Fq '"status": "BLOCK"' "${temporary}/result.json"

set +e
"${runtime[@]}" --entrypoint /bin/sh "${image}" -c true >/dev/null 2>&1
shell_exit=$?
set -e
[[ "${shell_exit}" -ne 0 ]] || {
  echo "container unexpectedly includes /bin/sh" >&2
  exit 1
}

echo "Verified non-root, shell-free, read-only, network-isolated container contract"
