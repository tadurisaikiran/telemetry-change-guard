#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
release_dir="${1:-${root}/dist/release}"
go_command="${GO:-go}"

cd -- "${root}"
"${go_command}" run ./internal/releasetool verify --dir "${release_dir}"
