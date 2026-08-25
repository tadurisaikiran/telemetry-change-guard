# Verify a Telemetry Change Guard release

Release verification answers four separate questions:

1. Did the downloaded bytes match the published checksum set?
2. Did the manifest describe those same bytes, version, commit, platforms, and
   locked toolchain?
3. What packages did the pinned SBOM generator identify in each artifact?
4. When a GitHub prerelease exists, did GitHub Actions produce the bytes from
   this repository and tag workflow?

Do not replace one check with another. A valid checksum detects transfer or
storage changes, but it does not establish who built the file.

## 1. Download one platform archive and its verification files

For a published `v0.1.0-alpha.1` prerelease, download:

- the archive matching your operating system and architecture;
- `SHA256SUMS`;
- `release-manifest.json`;
- the archive's `.spdx.json` and `.cdx.json` files.

The expected archive names are:

| Platform | Archive |
| --- | --- |
| Linux amd64 | `telemetry-change-guard_0.1.0-alpha.1_linux_amd64.tar.gz` |
| Linux arm64 | `telemetry-change-guard_0.1.0-alpha.1_linux_arm64.tar.gz` |
| macOS Intel | `telemetry-change-guard_0.1.0-alpha.1_darwin_amd64.tar.gz` |
| macOS Apple silicon | `telemetry-change-guard_0.1.0-alpha.1_darwin_arm64.tar.gz` |
| Windows amd64 | `telemetry-change-guard_0.1.0-alpha.1_windows_amd64.zip` |
| Windows arm64 | `telemetry-change-guard_0.1.0-alpha.1_windows_arm64.zip` |

The repository does not currently publish a release. These commands become
applicable only after the owner authorizes and creates the exact annotated tag.

## 2. Verify SHA-256 checksums

On Linux:

```sh
sha256sum --check --ignore-missing SHA256SUMS
```

On macOS:

```sh
shasum -a 256 --check SHA256SUMS
```

On PowerShell:

```powershell
(Get-FileHash .\telemetry-change-guard_0.1.0-alpha.1_windows_amd64.zip -Algorithm SHA256).Hash.ToLower()
```

Compare the PowerShell result with the exact archive line in `SHA256SUMS`.
The checksum file is sorted, uses base filenames only, and covers all platform
archives, the source archive, both SBOM formats for every archive, and the
release manifest. It intentionally cannot checksum itself.

## 3. Inspect identity and SBOMs

After extraction, run:

```sh
./telemetry-change-guard version --format json
./tmr version --format json
```

Both commands must report the manifest's version, 40-character commit,
RFC3339 build date, `dirty: false`, Go version, operating system, and
architecture. The archive also contains `LICENSE`, `README.md`, and
`NOTICE.md`.

The `.spdx.json` and `.cdx.json` files are per-artifact Syft results. TCG
normalizes invocation-dependent timestamps, the SPDX namespace, the optional
CycloneDX random serial number, temporary archive-extraction path prefixes,
and JSON array ordering before checksumming. Package names, versions, hashes,
licenses, and relationships are not rewritten. SBOMs describe what Syft found
and are not a claim that every vulnerability or license concern has been
resolved.

## 4. Verify GitHub build provenance

Install a current GitHub CLI, authenticate, and run this only after a release
has been published:

```sh
gh attestation verify \
  telemetry-change-guard_0.1.0-alpha.1_linux_amd64.tar.gz \
  --repo tadurisaikiran/telemetry-change-guard
```

Repeat for `SHA256SUMS`, `release-manifest.json`, or any SBOM you rely on. The
attestation must identify this repository and the tag-triggered `Public Alpha
Release` workflow. A successful verification does not make the alpha stable;
review the release notes and known limitations.

## Maintainer deep verification

From the exact source commit with Go `1.27.0`:

```sh
make verify-release
```

The maintainer verifier additionally checks the complete 23-file public
payload, manifest schema, sorted checksums, archive paths and permissions,
embedded build identity, `CGO_ENABLED=0`, `-trimpath`, source layout, both SBOM
formats, both CLI version commands, and the real getting-started `BLOCK`/exit
`2` scenario on the host platform.

Release pull requests and the gated tag workflow additionally run:

```sh
make release-reproducible
```

That target creates two clean snapshot payloads and requires byte-identical
checksum sets. Since `SHA256SUMS` covers every public asset and the manifest,
equality proves the complete payload content was reproduced.

## Verify build-only distribution candidates

These commands do not publish anything:

```sh
make container-snapshot
make homebrew-formula
```

The container verifier checks both Linux architectures, OCI labels, non-root
configuration, the canonical entrypoint, bounded archive paths and digests,
and subject-bound SPDX and SLSA attestation statements. The host smoke test
runs with a read-only root and no network, expects the getting-started
`BLOCK`/exit `2`, and confirms that `/bin/sh` is absent.

The Homebrew generator reads the already verified `SHA256SUMS` and refuses
missing or duplicate platform hashes. Syntax validation is not a substitute
for a real tap install; that test remains blocked until the release and tap are
published with owner approval.
