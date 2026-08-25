#!/bin/sh
set -eu

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository=$(CDPATH= cd -- "$script_directory/../../../.." && pwd)
image=${1:-tcg-agent-fixture:local}
build_context=$(mktemp -d "${TMPDIR:-/tmp}/tcg-agent-fixture.XXXXXX")
cleanup() {
  rm -rf -- "$build_context"
}
trap cleanup EXIT HUP INT TERM

runtime_architecture=$(docker version --format '{{.Server.Arch}}')
case "$runtime_architecture" in
  amd64|x86_64)
    target_architecture=amd64
    ;;
  arm64|aarch64)
    target_architecture=arm64
    ;;
  *)
    echo "unsupported Docker server architecture: $runtime_architecture" >&2
    exit 1
    ;;
esac

cd "$repository"
CGO_ENABLED=0 GOOS=linux GOARCH="$target_architecture" go build -trimpath -ldflags=-buildid= -o "$build_context/tcg-agent-fixture" ./experiments/agentic/testdata/adapter
docker build --platform "linux/$target_architecture" --network=none --pull=false --file "$script_directory/Dockerfile" --tag "$image" "$build_context"
