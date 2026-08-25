#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
mode="${1:-snapshot}"
[[ "${mode}" == "snapshot" || "${mode}" == "tag" ]] || {
  echo "release reproducibility: mode must be snapshot or tag" >&2
  exit 1
}

temporary="$(mktemp -d "${TMPDIR:-/tmp}/tcg-release-repeat.XXXXXX")"
trap 'rm -rf -- "${temporary}"' EXIT

"${root}/scripts/build-release.sh" "${mode}"
cp "${root}/dist/release/SHA256SUMS" "${temporary}/first-SHA256SUMS"
"${root}/scripts/build-release.sh" "${mode}"

if ! cmp -s "${temporary}/first-SHA256SUMS" "${root}/dist/release/SHA256SUMS"; then
  echo "release reproducibility: public payload changed across clean rebuilds" >&2
  diff -u "${temporary}/first-SHA256SUMS" "${root}/dist/release/SHA256SUMS" >&2 || true
  exit 1
fi

echo "Verified byte-reproducible public payload across two clean ${mode} builds"
