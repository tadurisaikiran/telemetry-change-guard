#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
release_directory="${1:-${root}/dist/release}"
output="${2:-${root}/dist/homebrew/Formula/telemetry-change-guard.rb}"
metadata="${root}/release/metadata.env"

metadata_value() {
  local key="$1"
  awk -F= -v key="${key}" '
    $1 == key { count++; value = substr($0, index($0, "=") + 1) }
    END { if (count != 1 || value == "") exit 1; print value }
  ' "${metadata}"
}

version="$(metadata_value TCG_CANDIDATE_VERSION)"
checksums="${release_directory}/SHA256SUMS"
[[ -f "${checksums}" ]] || {
  echo "missing verified release checksum manifest: ${checksums}" >&2
  exit 1
}

checksum() {
  local file="$1"
  local value
  value="$(awk -v file="${file}" '$2 == file { count++; digest=$1 } END { if (count != 1) exit 1; print digest }' "${checksums}")"
  [[ "${value}" =~ ^[0-9a-f]{64}$ ]] || {
    echo "missing or invalid checksum for ${file}" >&2
    exit 1
  }
  printf '%s' "${value}"
}

darwin_amd64="telemetry-change-guard_${version}_darwin_amd64.tar.gz"
darwin_arm64="telemetry-change-guard_${version}_darwin_arm64.tar.gz"
linux_amd64="telemetry-change-guard_${version}_linux_amd64.tar.gz"
linux_arm64="telemetry-change-guard_${version}_linux_arm64.tar.gz"

mkdir -p -- "$(dirname -- "${output}")"
temporary="$(mktemp "${TMPDIR:-/tmp}/tcg-formula.XXXXXX")"
trap 'rm -f -- "${temporary}"' EXIT

cat >"${temporary}" <<FORMULA
class TelemetryChangeGuard < Formula
  desc "Deterministic telemetry contract change-impact analysis"
  homepage "https://github.com/tadurisaikiran/telemetry-change-guard"
  version "${version}"
  license "Apache-2.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/tadurisaikiran/telemetry-change-guard/releases/download/v#{version}/${darwin_arm64}"
      sha256 "$(checksum "${darwin_arm64}")"
    else
      url "https://github.com/tadurisaikiran/telemetry-change-guard/releases/download/v#{version}/${darwin_amd64}"
      sha256 "$(checksum "${darwin_amd64}")"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/tadurisaikiran/telemetry-change-guard/releases/download/v#{version}/${linux_arm64}"
      sha256 "$(checksum "${linux_arm64}")"
    else
      url "https://github.com/tadurisaikiran/telemetry-change-guard/releases/download/v#{version}/${linux_amd64}"
      sha256 "$(checksum "${linux_amd64}")"
    end
  end

  def install
    bin.install "telemetry-change-guard"
    bin.install "tmr"
  end

  test do
    identity = shell_output("#{bin}/telemetry-change-guard version --format json")
    assert_match %Q("version": "#{version}"), identity
    assert_match %q("dirty": false), identity
  end
end
FORMULA

if command -v ruby >/dev/null 2>&1; then
  ruby -c "${temporary}" >/dev/null
fi
install -m 0644 "${temporary}" "${output}"
echo "Generated unpublished Homebrew formula: ${output}"
