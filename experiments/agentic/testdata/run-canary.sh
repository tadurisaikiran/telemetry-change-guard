#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../../.." && pwd -P)"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/tcg-agentic-canary.XXXXXX")"
trap 'rm -rf -- "${temporary}"' EXIT

go_command="${GO_COMMAND:-go}"
docker_command="${DOCKER_COMMAND:-docker}"
image="tcg-agent-fixture:canary"
source_rule="${root}/experiments/agentic/testdata/repair/workspace/prometheus/rules.yaml"
source_hash_before="$(git -C "${root}" hash-object -- "${source_rule}")"

"${go_command}" build -buildvcs=false -trimpath \
  -o "${temporary}/telemetry-change-guard" \
  "${root}/cmd/telemetry-change-guard"
"${go_command}" build -buildvcs=false -trimpath \
  -o "${temporary}/tcg-agent-eval" \
  "${root}/experiments/agentic/cmd/tcg-agent-eval"

GO_COMMAND="${go_command}" DOCKER_COMMAND="${docker_command}" \
  "${root}/experiments/agentic/testdata/adapter/build-image.sh" "${image}"

"${temporary}/tcg-agent-eval" \
  --acknowledge-experimental \
  --task "${root}/experiments/agentic/testdata/repair/task.json" \
  --output "${temporary}/run" \
  --tcg-command "${temporary}/telemetry-change-guard" \
  --container-runtime "${docker_command}" \
  --agent-image "${image}" \
  --agent-command /tcg-agent-fixture

result="${temporary}/run/run.json"
diff="${temporary}/run/final.diff"
grep -Fq '"outcome": "REVIEW_READY"' "${result}"
grep -Fq '"authoritativeStatus": "PASS"' "${result}"
[[ "$(grep -Fc '"tcgStatus": "BLOCK"' "${result}")" -eq 1 ]]
[[ "$(grep -Fc '"tcgStatus": "PASS"' "${result}")" -eq 1 ]]
grep -Fq '"number": 2' "${result}"
grep -Fq 'checkout_requests_total' "${diff}"
grep -Fq 'checkout_server_requests_total' "${diff}"

source_hash_after="$(git -C "${root}" hash-object -- "${source_rule}")"
[[ "${source_hash_after}" == "${source_hash_before}" ]] || {
  echo "agentic canary modified the source checkout" >&2
  exit 1
}

echo "Experimental agentic canary passed: BLOCK -> repair -> PASS with review-only diff"
