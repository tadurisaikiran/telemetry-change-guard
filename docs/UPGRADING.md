# Upgrading Telemetry Change Guard

There is no published TCG release yet. These procedures define how to move
between future prereleases without silently changing a required safety check.

## Before upgrading

1. Read [`CHANGELOG.md`](../CHANGELOG.md) and the target release's limitations.
2. Verify the target checksum and provenance using the release verification
   procedure when it becomes available.
3. Run `telemetry-change-guard version --format json` and confirm the expected
   version, full commit, build date, clean state, Go version, and platform.
4. Run the target against a sanitized fixture and a representative safe and
   unsafe change before changing a required repository check.
5. Compare versioned JSON by schema and semantics; do not parse console prose.

## CLI and GitHub Action upgrades

Use an immutable commit for the GitHub Action. Update the coordinate in one
reviewed change, retain the prior SHA for rollback, and verify `PASS`, `WARN`,
`BLOCK`, `INCOMPLETE`, and `ERROR` behavior relevant to the repository. Do not
replace a required check merely because a newer tag exists.

For binaries, install the new version beside the old executable, verify it,
then atomically switch the caller or PATH entry. Do not overwrite the only
known-good copy before verification.

## Migration compatibility

Existing `tmr` automation may continue to use the compatibility executable.
New integrations should prefer:

```bash
telemetry-change-guard migration check --config ./tcg.yaml --plan ./migration.yaml
```

The compatibility and canonical paths share one implementation and retain the
legacy result schema and exit behavior. Any future removal of `tmr` requires a
separately announced and tested deprecation window.

## Rollback

Restore the previous exact binary, container digest, or Action commit; rerun
the representative fixture; and document why the upgrade was reverted. Never
move an existing release tag. If a published prerelease is defective, preserve
its evidence, mark it withdrawn, fix through normal review, and publish a new
prerelease such as `v0.1.0-alpha.2`.
