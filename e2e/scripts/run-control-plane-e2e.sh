#!/usr/bin/env bash
set -euo pipefail

e2e_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
repo_dir=$(cd "${e2e_dir}/.." && pwd)
temporary_dir=$(mktemp -d "${TMPDIR:-/tmp}/tcg-control-plane-e2e.XXXXXX")
tcg_bin=${TCG_BIN:-"${temporary_dir}/telemetry-change-guard"}
verifier_bin="${temporary_dir}/control-plane-verifier"

cleanup() {
  rm -rf -- "${temporary_dir}"
}
trap cleanup EXIT

if [[ -z "${TCG_BIN:-}" ]]; then
  (cd "${repo_dir}" && go build -trimpath -o "${tcg_bin}" ./cmd/telemetry-change-guard)
fi
(cd "${repo_dir}" && go build -trimpath -o "${verifier_bin}" ./e2e/controlplane/verifier)

"${verifier_bin}" --binary "${tcg_bin}" --repository "${repo_dir}"
