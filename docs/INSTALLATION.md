# Install Telemetry Change Guard

Telemetry Change Guard (TCG) is preparing `v0.1.0-alpha.1`. No GitHub release,
container image, or Homebrew tap is published yet. The commands labeled
**after owner publication** are exact candidate instructions, but they will
return `404` until the owner authorizes the release.

For evaluation today, use the exact GitHub Action commit or build from source.
Do not download binaries or images from an unofficial location.

## Choose an installation path

| Need | Recommended path | Current state |
| --- | --- | --- |
| Pull-request check | [Immutable GitHub Action](#github-action) | Available at an exact verified commit |
| Local CLI without Go | [Verified release archive](#verified-release-archive) | Prepared; pending owner publication |
| Local CLI with Go 1.27 | [`go install`](#go-install) | Candidate tag pending; commit installs are CI-tested |
| Hermetic Linux execution | [OCI image](#oci-container) | Multi-arch build verified; registry publication pending |
| Homebrew | [Generated formula](#homebrew) | Formula generated and syntax-checked; tap pending |

Release archives are the preferred CLI installation because they include full
version, commit, build-date, and clean-tree identity and are covered by release
checksums, two SBOM formats, and GitHub provenance.

## Verified release archive

### Linux — after owner publication

Set `arch` to `amd64` or `arm64`:

```sh
version=0.1.0-alpha.1
arch=amd64
archive="telemetry-change-guard_${version}_linux_${arch}.tar.gz"
base="https://github.com/tadurisaikiran/telemetry-change-guard/releases/download/v${version}"

curl -fLO "${base}/${archive}"
curl -fLO "${base}/SHA256SUMS"
grep "  ${archive}$" SHA256SUMS | sha256sum --check -

tar -xzf "${archive}"
mkdir -p "${HOME}/.local/bin"
install -m 0755 "${archive%.tar.gz}/telemetry-change-guard" "${HOME}/.local/bin/telemetry-change-guard"
"${HOME}/.local/bin/telemetry-change-guard" version --format json
```

### macOS — after owner publication

Set `arch=arm64` for Apple silicon or `arch=amd64` for Intel:

```sh
version=0.1.0-alpha.1
arch=arm64
archive="telemetry-change-guard_${version}_darwin_${arch}.tar.gz"
base="https://github.com/tadurisaikiran/telemetry-change-guard/releases/download/v${version}"

curl -fLO "${base}/${archive}"
curl -fLO "${base}/SHA256SUMS"
grep "  ${archive}$" SHA256SUMS | shasum -a 256 --check -

tar -xzf "${archive}"
mkdir -p "${HOME}/.local/bin"
install -m 0755 "${archive%.tar.gz}/telemetry-change-guard" "${HOME}/.local/bin/telemetry-change-guard"
"${HOME}/.local/bin/telemetry-change-guard" version --format json
```

The alpha macOS binary is not notarized. Checksum and provenance verification
are mandatory; macOS may still require an explicit local approval to execute
the downloaded binary.

### Windows PowerShell — after owner publication

Set `$Arch` to `amd64` or `arm64`:

```powershell
$Version = '0.1.0-alpha.1'
$Arch = 'amd64'
$Archive = "telemetry-change-guard_${Version}_windows_${Arch}.zip"
$Base = "https://github.com/tadurisaikiran/telemetry-change-guard/releases/download/v${Version}"

Invoke-WebRequest "${Base}/${Archive}" -OutFile $Archive
Invoke-WebRequest "${Base}/SHA256SUMS" -OutFile SHA256SUMS
$Expected = ((Get-Content SHA256SUMS | Where-Object { $_ -match "  $([regex]::Escape($Archive))$" }) -split '\s+')[0]
$Actual = (Get-FileHash $Archive -Algorithm SHA256).Hash.ToLowerInvariant()
if ($Actual -ne $Expected) { throw "SHA-256 mismatch for $Archive" }

Expand-Archive $Archive -DestinationPath . -Force
$Directory = $Archive -replace '\.zip$', ''
& ".\${Directory}\telemetry-change-guard.exe" version --format json
```

Windows binaries are not Authenticode-signed in this alpha.

For every platform, read [Verify a release](VERIFY_RELEASE.md) before trusting
the executable. The compatibility `tmr` binary is in the same archive for
existing migration automation.

## `go install`

This path requires Go `1.27.0` or newer. After the exact tag is published:

```sh
GOBIN="${HOME}/.local/bin" \
  go install github.com/tadurisaikiran/telemetry-change-guard/cmd/telemetry-change-guard@v0.1.0-alpha.1
"${HOME}/.local/bin/telemetry-change-guard" version --format json
```

To retain the compatibility executable:

```sh
GOBIN="${HOME}/.local/bin" \
  go install github.com/tadurisaikiran/telemetry-change-guard/cmd/tmr@v0.1.0-alpha.1
```

The tag workflow installs the candidate from a clean environment and runs the
getting-started policy block before publishing. `go install` reports the
immutable module version, but Go does not inject the release pipeline's commit
or build date; those fields may be `unknown`. Use release archives when full
artifact provenance is required.

Never use `@latest`, `@main`, or a nonexistent `@v1` in a required check.

## GitHub Action

The Action builds the CLI inside the job, so no separate installation is
needed. This exact commit is the current fully verified coordinate:

```yaml
permissions:
  contents: read

steps:
  - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
  - uses: tadurisaikiran/telemetry-change-guard@4bb5ea7345f56291bebc65c63e8375e46d002f12 # v0.1.0-alpha.1 candidate
    with:
      config: tcg.yaml
      changes: changes.yaml
      comment: "false"
```

Remote evidence is disabled by default. Read [Secure CI usage](SECURE_CI_USAGE.md)
before adding credentials or network access. The generated external-consumer
fixture tests remote immutable consumption, but publication of a truly separate
fixture repository remains an owner-controlled release blocker.

## OCI container

The image is Linux-only and targets `linux/amd64` and `linux/arm64`. It runs as
`nonroot:nonroot` on a distroless filesystem with no shell or package manager.
It supports read-only root filesystems and has no default network dependency.

No registry image is published yet. Maintainers can reproduce the build-only
candidate from a clean checkout with Docker Buildx:

```sh
make container-snapshot
```

That command does not push. It smoke-tests the host image with networking
disabled and a read-only root, then writes `dist/container/telemetry-change-guard.oci.tar`
for `linux/amd64` and `linux/arm64`. The verifier requires exact OCI labels,
non-root execution, per-platform SPDX SBOM attestations, and SLSA provenance.

After separately authorized publication, use the immutable digest recorded in
the release evidence—not a movable tag:

```sh
image='ghcr.io/tadurisaikiran/telemetry-change-guard@sha256:<published-digest>'
docker run --rm --read-only --network none --cap-drop ALL \
  --security-opt no-new-privileges \
  --volume "${PWD}:/workspace:ro" \
  "${image}" check --config tcg.yaml --changes changes.yaml
```

If TCG must write `--output`, `--json-output`, or `--status-output`, mount only
the chosen output directory read-write; keep input mounts read-only.

Registry authentication and package publication are intentionally absent from
active workflows until the owner explicitly authorizes GHCR publication.

## Homebrew

The intended future command is:

```sh
brew install tadurisaikiran/tap/telemetry-change-guard
```

It does not work yet because the owner has not authorized creation of the
`homebrew-tap` repository or formula publication. The release snapshot creates
a checksum-bound formula without publishing it:

```sh
make release-snapshot
make homebrew-formula
ruby -c dist/homebrew/Formula/telemetry-change-guard.rb
```

The generated formula selects macOS/Linux and amd64/arm64 archives, installs
both CLI names, and verifies candidate version output. Actual `brew install`
testing remains blocked until the GitHub release assets and tap exist.

## Build from a checkout

For contributor development—not release installation:

```sh
git clone https://github.com/tadurisaikiran/telemetry-change-guard.git
cd telemetry-change-guard
git checkout 4bb5ea7345f56291bebc65c63e8375e46d002f12
go build -buildvcs=false -trimpath -o ./bin/telemetry-change-guard ./cmd/telemetry-change-guard
./bin/telemetry-change-guard version
```

Checkout builds honestly report development identity. Do not redistribute them
as release binaries.
