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

workflow_job() {
  local file="$1"
  local job="$2"

  awk -v job="${job}" '
    $0 == "jobs:" { in_jobs = 1; next }
    in_jobs && $0 == "  " job ":" { in_job = 1 }
    in_job && $0 ~ /^  [A-Za-z0-9_-]+:[[:space:]]*$/ && $0 != "  " job ":" { exit }
    in_job { print }
  ' "${file}"
}

require_job_pattern() {
  local file="$1"
  local job="$2"
  local description="$3"
  local pattern="$4"
  local block

  block="$(workflow_job "${file}" "${job}")"
  if [[ -z "${block}" ]]; then
    echo "${file#"${root}/"}: required job is missing: ${job}" >&2
    failures=1
    return
  fi
  if ! grep -Eq -- "${pattern}" <<<"${block}"; then
    echo "${file#"${root}/"}: ${job} job must ${description}" >&2
    failures=1
  fi
}

check_release_boundary() {
  local file="${root}/.github/workflows/release.yml"
  local candidate
  local platform_smoke
  local publish
  local environment_count
  local public_environment_count
  local write_permission_count

  candidate="$(workflow_job "${file}" candidate)"
  platform_smoke="$(workflow_job "${file}" platform-smoke)"
  publish="$(workflow_job "${file}" publish)"
  environment_count="$(grep -Ec '^[[:space:]]+environment:' "${file}" || true)"
  public_environment_count="$(grep -Ec '^[[:space:]]+environment:[[:space:]]+public-alpha[[:space:]]*$' "${file}" || true)"
  write_permission_count="$(grep -Ec '^[[:space:]]+[A-Za-z0-9_-]+:[[:space:]]+write[[:space:]]*$' "${file}" || true)"

  require_job_pattern "${file}" candidate "build the reproducible tagged payload" 'make release-tag-reproducible'
  require_job_pattern "${file}" candidate "upload the immutable candidate artifact" 'name:[[:space:]]+tcg-release-candidate-\$\{\{ github[.]sha \}\}'
  require_job_pattern "${file}" platform-smoke "depend on the candidate job" 'needs:[[:space:]]+candidate'
  require_job_pattern "${file}" platform-smoke "exercise Linux" 'ubuntu-latest'
  require_job_pattern "${file}" platform-smoke "exercise macOS" 'macos-latest'
  require_job_pattern "${file}" platform-smoke "exercise Windows" 'windows-latest'
  require_job_pattern "${file}" platform-smoke "download the exact candidate artifact" 'name:[[:space:]]+tcg-release-candidate-\$\{\{ github[.]sha \}\}'
  require_job_pattern "${file}" platform-smoke "run the deep release verifier" 'releasetool verify --dir dist/release'
  require_job_pattern "${file}" publish "depend on candidate creation" '^      - candidate[[:space:]]*$'
  require_job_pattern "${file}" publish "depend on native platform smoke tests" '^      - platform-smoke[[:space:]]*$'
  require_job_pattern "${file}" publish "use the protected public-alpha environment" 'environment:[[:space:]]+public-alpha'
  require_job_pattern "${file}" publish "hold release write permission" 'contents:[[:space:]]+write'
  require_job_pattern "${file}" publish "hold provenance identity permission" 'id-token:[[:space:]]+write'
  require_job_pattern "${file}" publish "hold attestation permission" 'attestations:[[:space:]]+write'
  require_job_pattern "${file}" publish "download the exact tested candidate artifact" 'name:[[:space:]]+tcg-release-candidate-\$\{\{ github[.]sha \}\}'
  require_job_pattern "${file}" publish "reverify the downloaded payload" 'releasetool verify --dir dist/release'
  require_job_pattern "${file}" publish "attest the release payload" 'actions/attest-build-provenance@'
  require_job_pattern "${file}" publish "create the GitHub prerelease" 'gh release create'

  if [[ "${environment_count}" != "1" || "${public_environment_count}" != "1" ]]; then
    echo "${file#"${root}/"}: public-alpha environment must appear exactly once" >&2
    failures=1
  fi
  if [[ "${write_permission_count}" != "3" ]]; then
    echo "${file#"${root}/"}: only publish may hold the three required write permissions" >&2
    failures=1
  fi
  if grep -Eq 'release-tag-reproducible|goreleaser-action|download-syft|homebrew-formula' <<<"${publish}"; then
    echo "${file#"${root}/"}: publish job must consume the tested candidate without rebuilding it" >&2
    failures=1
  fi
  if grep -Eq 'environment:|[[:space:]](contents|id-token|attestations):[[:space:]]+write' <<<"${candidate}${platform_smoke}"; then
    echo "${file#"${root}/"}: candidate and platform-smoke jobs must remain unprivileged" >&2
    failures=1
  fi
  if ! grep -Eq 'release-tag-reproducible' <<<"${candidate}"; then
    echo "${file#"${root}/"}: candidate job must own the release build" >&2
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

check_release_boundary

exit "${failures}"
