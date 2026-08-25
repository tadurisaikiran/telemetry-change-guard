# Telemetry Change Guard alpha binaries

This archive contains two statically linked command-line programs:

- `telemetry-change-guard` is the canonical command.
- `tmr` is the compatibility command and reports the same build identity.

Confirm the binary before use:

```sh
./telemetry-change-guard version --format json
```

Run the deterministic guard from a repository containing a TCG configuration
and a change set:

```sh
./telemetry-change-guard analyze \
  --config path/to/tcg.yaml \
  --changes path/to/changes.yaml \
  --format json
```

Generic analysis exits `0` for `PASS` or `WARN`, `1` for `ERROR`, `2` for
`BLOCK`, and `3` for `INCOMPLETE`. Treat every non-zero exit as an enforced
CI result; do not discard it with `|| true`.

This is an alpha candidate, not a claim of organization-wide blast-radius
discovery or production proof. TCG evaluates only the supported evidence you
configure. Verify the archive against `SHA256SUMS`, its two SBOMs, the release
manifest, and (when published) GitHub build provenance before installing it.

Project documentation: https://github.com/tadurisaikiran/telemetry-change-guard
