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

cd "$repository"
CGO_ENABLED=0 go build -trimpath -ldflags=-buildid= -o "$build_context/tcg-agent-fixture" ./experiments/agentic/testdata/adapter
docker build --network=none --pull=false --file "$script_directory/Dockerfile" --tag "$image" "$build_context"
