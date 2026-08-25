#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
metadata="${root}/release/metadata.env"
failures=0

metadata_value() {
  local key="$1"
  awk -F= -v key="${key}" '
    $1 == key { count++; value = substr($0, index($0, "=") + 1) }
    END { if (count != 1 || value == "") exit 1; print value }
  ' "${metadata}"
}

action_repository="$(metadata_value TCG_ACTION_REPOSITORY)"
action_ref="$(metadata_value TCG_ACTION_REF)"
candidate_version="$(metadata_value TCG_CANDIDATE_VERSION)"
dockerfile_frontend="$(metadata_value TCG_DOCKERFILE_FRONTEND)"
builder_image="$(metadata_value TCG_GO_BUILDER_IMAGE)"
runtime_image="$(metadata_value TCG_RUNTIME_IMAGE)"

[[ "${action_ref}" =~ ^[0-9a-f]{40}$ ]] || {
  echo "release/metadata.env: TCG_ACTION_REF must be a full commit SHA" >&2
  failures=1
}
[[ "${dockerfile_frontend}" =~ @sha256:[0-9a-f]{64}$ ]] || {
  echo "release/metadata.env: TCG_DOCKERFILE_FRONTEND must use a SHA-256 digest" >&2
  failures=1
}
[[ "${builder_image}" =~ @sha256:[0-9a-f]{64}$ ]] || {
  echo "release/metadata.env: TCG_GO_BUILDER_IMAGE must use a SHA-256 digest" >&2
  failures=1
}
[[ "${runtime_image}" =~ @sha256:[0-9a-f]{64}$ ]] || {
  echo "release/metadata.env: TCG_RUNTIME_IMAGE must use a SHA-256 digest" >&2
  failures=1
}

if ! grep -Fq "# syntax=${dockerfile_frontend}" "${root}/Dockerfile"; then
  echo "Dockerfile: frontend differs from release metadata" >&2
  failures=1
fi
if ! grep -Fq "FROM --platform=\$BUILDPLATFORM ${builder_image} AS build" "${root}/Dockerfile"; then
  echo "Dockerfile: builder image differs from release metadata" >&2
  failures=1
fi
if ! grep -Fq "FROM ${runtime_image}" "${root}/Dockerfile"; then
  echo "Dockerfile: runtime image differs from release metadata" >&2
  failures=1
fi

files=("${root}/README.md")
while IFS= read -r -d '' file; do
  files+=("${file}")
done < <(
  find "${root}/docs" "${root}/.github/workflows" "${root}/release-fixtures" \
    -type f \( -name '*.md' -o -name '*.yml' -o -name '*.yaml' \) -print0
)
for file in "${files[@]}"; do
  while IFS= read -r coordinate; do
    [[ -z "${coordinate}" ]] && continue
    if [[ "${coordinate}" != "${action_repository}@${action_ref}" ]]; then
      echo "${file#"${root}/"}: stale or inconsistent Action coordinate: ${coordinate}" >&2
      failures=1
    fi
  done < <(grep -Eo "${action_repository}@[0-9a-f]{40}" "${file}" || true)
  if grep -Eq "uses:[[:space:]]+${action_repository}@v[0-9]" "${file}"; then
    echo "${file#"${root}/"}: unpublished movable TCG Action tag is forbidden" >&2
    failures=1
  fi
  while IFS= read -r coordinate; do
    [[ -z "${coordinate}" ]] && continue
    if [[ "${coordinate}" != "github.com/${action_repository}/cmd/telemetry-change-guard@${action_ref}" && \
          "${coordinate}" != "github.com/${action_repository}/cmd/tmr@${action_ref}" ]]; then
      echo "${file#"${root}/"}: stale or inconsistent commit-based go install coordinate: ${coordinate}" >&2
      failures=1
    fi
  done < <(grep -Eo "github.com/${action_repository}/cmd/(telemetry-change-guard|tmr)@[0-9a-f]{40}" "${file}" || true)
  while IFS= read -r coordinate; do
    [[ -z "${coordinate}" ]] && continue
    if [[ "${coordinate}" != "github.com/${action_repository}/cmd/telemetry-change-guard@v${candidate_version}" && \
          "${coordinate}" != "github.com/${action_repository}/cmd/tmr@v${candidate_version}" ]]; then
      echo "${file#"${root}/"}: stale or inconsistent candidate go install coordinate: ${coordinate}" >&2
      failures=1
    fi
  done < <(grep -Eo "github.com/${action_repository}/cmd/(telemetry-change-guard|tmr)@v[0-9][0-9A-Za-z.+-]*" "${file}" || true)
  while IFS= read -r checkout_ref; do
    [[ -z "${checkout_ref}" ]] && continue
    if [[ "${checkout_ref}" != "git checkout ${action_ref}" ]]; then
      echo "${file#"${root}/"}: stale or inconsistent checkout coordinate: ${checkout_ref}" >&2
      failures=1
    fi
  done < <(grep -Eo 'git checkout [0-9a-f]{40}' "${file}" || true)
done

if ! grep -Fq "git checkout ${action_ref}" "${root}/docs/DESIGN_USER_PROGRAM.md"; then
  echo "docs/DESIGN_USER_PROGRAM.md: checkout coordinate differs from release metadata" >&2
  failures=1
fi

exit "${failures}"
