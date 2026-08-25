#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
metadata="${root}/release/metadata.env"

metadata_value() {
  local key="$1"
  awk -F= -v key="${key}" '
    $1 == key { count++; value = substr($0, index($0, "=") + 1) }
    END { if (count != 1 || value == "") exit 1; print value }
  ' "${metadata}"
}

cd -- "${root}"
[[ -z "$(git status --porcelain --untracked-files=all)" ]] || {
  echo "container snapshot requires a clean worktree" >&2
  exit 1
}

version="$(metadata_value TCG_CANDIDATE_VERSION)"
commit="$(git rev-parse HEAD)"
build_date="$(git show -s --format=%cI HEAD)"
image="telemetry-change-guard:smoke"
output="${root}/dist/container/telemetry-change-guard.oci.tar"

case "$(docker info --format '{{.Architecture}}')" in
  amd64|x86_64) host_platform=linux/amd64 ;;
  arm64|aarch64) host_platform=linux/arm64 ;;
  *)
    echo "unsupported Docker host architecture" >&2
    exit 1
    ;;
esac

build_args=(
  --build-arg "TCG_VERSION=${version}"
  --build-arg "TCG_COMMIT=${commit}"
  --build-arg "TCG_BUILD_DATE=${build_date}"
  --build-arg TCG_DIRTY=false
)

docker buildx build \
  --load \
  --platform "${host_platform}" \
  --provenance=false \
  --sbom=false \
  --tag "${image}" \
  "${build_args[@]}" \
  .

TCG_VERSION="${version}" TCG_COMMIT="${commit}" TCG_BUILD_DATE="${build_date}" \
  "${root}/scripts/verify-container.sh" "${image}"

mkdir -p -- "$(dirname -- "${output}")"
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --output "type=oci,dest=${output}" \
  --provenance=mode=max \
  --sbom=true \
  "${build_args[@]}" \
  .

"${GO:-go}" run ./internal/containerverify \
  --layout "${output}" \
  --version "${version}" \
  --revision "${commit}" \
  --created "${build_date}"

echo "Verified unpublished container snapshot: ${output}"
