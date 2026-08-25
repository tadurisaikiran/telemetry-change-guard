#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
destination="${1:-${root}/.cache/release-tools}"
metadata="${root}/release/metadata.env"

die() {
  echo "release tool installation: $*" >&2
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

sha256_file() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    LC_ALL=C sha256sum "${file}" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    LC_ALL=C shasum -a 256 "${file}" | awk '{print $1}'
    return
  fi
  if command -v openssl >/dev/null 2>&1; then
    LC_ALL=C openssl dgst -sha256 "${file}" | awk '{print $NF}'
    return
  fi
  die "sha256sum, shasum, or openssl is required"
}

download() {
  local url="$1"
  local output="$2"
  curl -fL --proto '=https' --tlsv1.2 --retry 3 --output "${output}" "${url}"
}

verify_file() {
  local file="$1"
  local expected="$2"
  local actual
  actual="$(sha256_file "${file}")"
  [[ "${actual}" == "${expected}" ]] || die "checksum mismatch for $(basename -- "${file}")"
}

manifest_checksum() {
  local manifest_file="$1"
  local asset="$2"
  local checksum
  checksum="$(awk -v asset="${asset}" '$2 == asset { count++; checksum = $1 } END { if (count != 1) exit 1; print checksum }' "${manifest_file}")" ||
    die "${asset} is missing or duplicated in the upstream checksum manifest"
  [[ "${checksum}" =~ ^[0-9a-f]{64}$ ]] || die "invalid upstream checksum for ${asset}"
  printf '%s\n' "${checksum}"
}

goreleaser_version="$(metadata_value TCG_GORELEASER_VERSION)"
goreleaser_manifest_sha="$(metadata_value TCG_GORELEASER_CHECKSUMS_SHA256)"
syft_version="$(metadata_value TCG_SYFT_VERSION)"
syft_manifest_sha="$(metadata_value TCG_SYFT_CHECKSUMS_SHA256)"

[[ "${goreleaser_version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "invalid GoReleaser version lock"
[[ "${syft_version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "invalid Syft version lock"
[[ "${goreleaser_manifest_sha}" =~ ^[0-9a-f]{64}$ ]] || die "invalid GoReleaser manifest checksum"
[[ "${syft_manifest_sha}" =~ ^[0-9a-f]{64}$ ]] || die "invalid Syft manifest checksum"

case "$(uname -s)" in
  Darwin) release_os="Darwin"; syft_os="darwin" ;;
  Linux) release_os="Linux"; syft_os="linux" ;;
  *) die "release tools are supported on macOS and Linux" ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) goreleaser_arch="x86_64"; syft_arch="amd64" ;;
  arm64 | aarch64) goreleaser_arch="arm64"; syft_arch="arm64" ;;
  *) die "release tools are supported on amd64 and arm64" ;;
esac

goreleaser_number="${goreleaser_version#v}"
syft_number="${syft_version#v}"
goreleaser_asset="goreleaser_${release_os}_${goreleaser_arch}.tar.gz"
syft_asset="syft_${syft_number}_${syft_os}_${syft_arch}.tar.gz"

temporary="$(mktemp -d "${TMPDIR:-/tmp}/tcg-release-tools.XXXXXX")"
trap 'rm -rf -- "${temporary}"' EXIT
mkdir -p -- "${temporary}/goreleaser" "${temporary}/syft" "${destination}"

goreleaser_manifest="${temporary}/goreleaser-checksums.txt"
download "https://github.com/goreleaser/goreleaser/releases/download/${goreleaser_version}/checksums.txt" "${goreleaser_manifest}"
verify_file "${goreleaser_manifest}" "${goreleaser_manifest_sha}"
goreleaser_archive="${temporary}/${goreleaser_asset}"
download "https://github.com/goreleaser/goreleaser/releases/download/${goreleaser_version}/${goreleaser_asset}" "${goreleaser_archive}"
verify_file "${goreleaser_archive}" "$(manifest_checksum "${goreleaser_manifest}" "${goreleaser_asset}")"
tar -xzf "${goreleaser_archive}" -C "${temporary}/goreleaser"
[[ -f "${temporary}/goreleaser/goreleaser" ]] || die "GoReleaser archive did not contain goreleaser"

syft_manifest="${temporary}/syft-checksums.txt"
download "https://github.com/anchore/syft/releases/download/${syft_version}/syft_${syft_number}_checksums.txt" "${syft_manifest}"
verify_file "${syft_manifest}" "${syft_manifest_sha}"
syft_archive="${temporary}/${syft_asset}"
download "https://github.com/anchore/syft/releases/download/${syft_version}/${syft_asset}" "${syft_archive}"
verify_file "${syft_archive}" "$(manifest_checksum "${syft_manifest}" "${syft_asset}")"
tar -xzf "${syft_archive}" -C "${temporary}/syft"
[[ -f "${temporary}/syft/syft" ]] || die "Syft archive did not contain syft"

install -m 0755 "${temporary}/goreleaser/goreleaser" "${destination}/goreleaser"
install -m 0755 "${temporary}/syft/syft" "${destination}/syft"

"${destination}/goreleaser" --version
"${destination}/syft" version
echo "Installed locked release tools in ${destination}"
