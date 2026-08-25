#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
failures=0
version_comment_pattern='#[[:space:]]*v[0-9]+([.][0-9]+)*([-.][0-9A-Za-z.-]+)?[[:space:]]*$'

check_uses() {
  local file="$1"
  local line
  local line_number=0
  local reference

  while IFS= read -r line || [[ -n "${line}" ]]; do
    line_number=$((line_number + 1))
    if [[ "${line}" =~ ^[[:space:]]*uses:[[:space:]]*([^[:space:]#]+) ]]; then
      reference="${BASH_REMATCH[1]}"
      if [[ "${reference}" == ./* ]]; then
        continue
      fi
      if [[ ! "${reference}" =~ ^[^@]+@[0-9a-f]{40}$ ]]; then
        echo "${file#"${root}/"}:${line_number}: external Action is not pinned to a full commit SHA: ${reference}" >&2
        failures=1
      fi
      if [[ ! "${line}" =~ ${version_comment_pattern} ]]; then
        echo "${file#"${root}/"}:${line_number}: pinned Action is missing an exact version comment" >&2
        failures=1
      fi
    fi
  done <"${file}"
}

check_default_permissions() {
  local file="$1"
  local permissions

  permissions="$({
    awk '
      /^permissions:[[:space:]]*$/ { in_permissions = 1; next }
      in_permissions && /^[^[:space:]]/ { exit }
      in_permissions && /^  [A-Za-z0-9_-]+:[[:space:]]*/ { print }
    ' "${file}"
  } || true)"
  if [[ "${permissions}" != "  contents: read" ]]; then
    echo "${file#"${root}/"}: top-level permissions must be exactly 'contents: read'" >&2
    failures=1
  fi
  if grep -Eq '^permissions:[[:space:]]+(read-all|write-all)$' "${file}"; then
    echo "${file#"${root}/"}: broad default permissions are forbidden" >&2
    failures=1
  fi
}

check_uses "${root}/action.yml"
for workflow in "${root}"/.github/workflows/*.yml "${root}"/.github/workflows/*.yaml; do
  [[ -e "${workflow}" ]] || continue
  check_uses "${workflow}"
  check_default_permissions "${workflow}"
done

while IFS= read -r -d '' workflow; do
  check_uses "${workflow}"
  check_default_permissions "${workflow}"
done < <(
  find "${root}/release-fixtures" -type f \
    \( -path '*/.github/workflows/*.yml' -o -path '*/.github/workflows/*.yaml' \) \
    -print0 2>/dev/null
)

exit "${failures}"
