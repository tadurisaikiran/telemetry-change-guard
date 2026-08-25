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
normalizes only invocation-dependent timestamps, SPDX namespace, and the
optional CycloneDX random serial number before checksumming them; identified
components are not rewritten. SBOMs describe what Syft found and are not a
claim that every vulnerability or license concern has been resolved.

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
