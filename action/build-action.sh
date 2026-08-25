#!/usr/bin/env bash
set -euo pipefail

: "${RUNNER_TEMP:?RUNNER_TEMP is required}"

commit="unknown"
if [[ "${TCG_ACTION_REF:-}" =~ ^[0-9a-f]{40}$ ]]; then
  commit="${TCG_ACTION_REF}"
elif [[ -z "${TCG_ACTION_REF:-}" && "${TCG_WORKFLOW_SHA:-}" =~ ^[0-9a-f]{40}$ && ( -z "${TCG_ACTION_REPOSITORY:-}" || "${TCG_ACTION_REPOSITORY}" == "${TCG_WORKFLOW_REPOSITORY:-}" ) ]]; then
  commit="${TCG_WORKFLOW_SHA}"
fi

version_package="github.com/tadurisaikiran/telemetry-change-guard/internal/version"
ldflags="-X ${version_package}.Version=dev -X ${version_package}.Commit=${commit} -X ${version_package}.Date=unknown -X ${version_package}.Dirty=unknown"

"${GO:-go}" build \
  -buildvcs=false \
  -trimpath \
  -ldflags "${ldflags}" \
  -o "${RUNNER_TEMP}/telemetry-change-guard" \
  ./cmd/telemetry-change-guard
