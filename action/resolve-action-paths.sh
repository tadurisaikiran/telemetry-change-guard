#!/usr/bin/env bash
set -euo pipefail

: "${GITHUB_ACTION_PATH:?GITHUB_ACTION_PATH is required}"
: "${GITHUB_OUTPUT:?GITHUB_OUTPUT is required}"

action_path="$(cd -- "${GITHUB_ACTION_PATH}" && pwd -P)"
go_mod="${action_path}/go.mod"
go_sum="${action_path}/go.sum"

if [[ ! -f "${go_mod}" || ! -f "${go_sum}" ]]; then
  echo "Telemetry Change Guard Action dependencies are incomplete under ${action_path}" >&2
  exit 1
fi

{
  printf 'go-mod=%s\n' "${go_mod}"
} >>"${GITHUB_OUTPUT}"
