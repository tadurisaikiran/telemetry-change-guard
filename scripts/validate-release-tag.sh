#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
metadata="${root}/release/metadata.env"

die() {
  echo "release tag validation: $*" >&2
  exit 1
}

version="$(awk -F= '$1 == "TCG_CANDIDATE_VERSION" { count++; value = substr($0, index($0, "=") + 1) } END { if (count != 1 || value == "") exit 1; print value }' "${metadata}")" ||
  die "candidate version metadata is missing or duplicated"
expected_tag="v${version}"

cd -- "${root}"
actual_tag="${GITHUB_REF_NAME:-$(git describe --tags --exact-match HEAD 2>/dev/null || true)}"
[[ "${actual_tag}" == "${expected_tag}" ]] || die "expected exact tag ${expected_tag}; found ${actual_tag:-none}"
[[ "$(git rev-list -n 1 "${expected_tag}")" == "$(git rev-parse HEAD)" ]] || die "tag does not resolve to HEAD"
[[ "$(git cat-file -t "${expected_tag}")" == "tag" ]] || die "release tag must be annotated"
git rev-parse --verify origin/main >/dev/null 2>&1 || die "origin/main is unavailable; fetch full history first"
git merge-base --is-ancestor HEAD origin/main || die "tagged commit is not contained in origin/main"
[[ -z "$(git status --porcelain --untracked-files=all)" ]] || die "tagged worktree is dirty"

echo "Validated annotated prerelease tag ${expected_tag} on protected main history at $(git rev-parse HEAD)"
