#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
metadata="${root}/release/metadata.env"
mode="${1:-snapshot}"

die() {
  echo "release build: $*" >&2
  exit 1
}

metadata_value() {
  local key="$1"
  awk -F= -v key="${key}" '
    $1 == key {
      count++
      value = substr($0, index($0, "=") + 1)
    }
    END {
      if (count != 1 || value == "") {
        exit 1
      }
      print value
    }
  ' "${metadata}" || die "missing or duplicate ${key} in release/metadata.env"
}

tool_path() {
  local requested="$1"
  command -v "${requested}" 2>/dev/null || die "required tool not found: ${requested}"
}

tool_has_version() {
  local command_path="$1"
  local expected="$2"
  shift 2
  local output
  output="$("${command_path}" "$@" 2>&1)" || die "could not read version from ${command_path}"
  grep -Eq "(^|[^0-9])v?${expected//./\\.}([^0-9]|$)" <<<"${output}" ||
    die "${command_path} does not report locked version ${expected}"
}

[[ "${mode}" == "snapshot" || "${mode}" == "tag" ]] || die "mode must be snapshot or tag"
cd -- "${root}"

version="$(metadata_value TCG_CANDIDATE_VERSION)"
goreleaser_version="$(metadata_value TCG_GORELEASER_VERSION)"
syft_version="$(metadata_value TCG_SYFT_VERSION)"
[[ "${version}" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-[0-9A-Za-z][0-9A-Za-z.-]*$ ]] ||
  die "candidate version is not a semantic prerelease"

go_command="$(tool_path "${GO:-go}")"
goreleaser_command="$(tool_path "${GORELEASER:-goreleaser}")"
syft_command="$(tool_path "${SYFT:-syft}")"
# GoReleaser invokes the canonical `go` and `syft` names while loading module
# metadata, building, and cataloging. Put the already validated exact tools
# first without replacing the caller's remaining PATH.
export PATH="$(dirname -- "${go_command}"):$(dirname -- "${syft_command}"):${PATH}"
expected_go="$(awk '$1 == "go" { print "go" $2 }' go.mod)"
actual_go="$("${go_command}" env GOVERSION)"
[[ -n "${expected_go}" && "${actual_go}" == "${expected_go}" ]] ||
  die "release requires ${expected_go}; found ${actual_go}"
tool_has_version "${goreleaser_command}" "${goreleaser_version#v}" --version
tool_has_version "${syft_command}" "${syft_version#v}" version

status="$(git status --porcelain --untracked-files=all)"
[[ -z "${status}" ]] || die "the release worktree must be clean"

commit="$(git rev-parse HEAD)"
build_date="$(git show -s --format=%cI HEAD)"
commit_epoch="$(git show -s --format=%ct HEAD)"
[[ "${commit}" =~ ^[0-9a-f]{40}$ ]] || die "could not resolve a full commit"
[[ "${build_date}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T ]] || die "could not resolve the commit date"
[[ "${commit_epoch}" =~ ^[0-9]+$ ]] || die "could not resolve the commit timestamp"

# A single worker makes multi-binary tar entry ordering independent of build
# completion timing. The binaries themselves are already deterministic.
release_args=(release --clean --config .goreleaser.yaml --parallelism=1)
if [[ "${mode}" == "snapshot" ]]; then
  release_args+=(--snapshot)
else
  expected_tag="v${version}"
  actual_tag="$(git describe --tags --exact-match HEAD 2>/dev/null || true)"
  [[ "${actual_tag}" == "${expected_tag}" ]] || die "tag build requires ${expected_tag} at HEAD"
  release_args+=(--skip=publish)
fi

echo "Building ${version} from ${commit} (${mode}; no publication)"
TCG_BUILD_DATE="${build_date}" \
TCG_SYFT="${syft_command}" \
SOURCE_DATE_EPOCH="${commit_epoch}" \
GORELEASER_CURRENT_TAG="v${version}" \
  "${goreleaser_command}" "${release_args[@]}"

"${go_command}" run ./internal/releasetool stage \
  --raw dist/raw \
  --out dist/release \
  --version "${version}" \
  --commit "${commit}" \
  --build-date "${build_date}" \
  --go-version "${actual_go}" \
  --goreleaser-version "${goreleaser_version}" \
  --syft-version "${syft_version}"

GO="${go_command}" "${root}/scripts/verify-release.sh" "${root}/dist/release"
echo "Verified release candidate assets: ${root}/dist/release"
