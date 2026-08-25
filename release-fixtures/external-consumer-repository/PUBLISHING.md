# Owner-approved fixture publication

Do not create or advertise the external fixture repository without owner
approval.

When approved:

1. Create the public repository `telemetry-change-guard-action-fixture` with no
   generated starter files.
2. Copy the contents of this directory to the repository root.
3. Confirm every `uses:` reference still equals `TCG_ACTION_REF` in
   `release/metadata.env` and points to the intended release commit.
4. Push through a pull request; do not weaken the workflow's read-only default
   permissions.
5. Require all `consumer-*` matrix cases and `verify-artifacts` to pass.
6. Record the workflow URL and exact fixture commit in the alpha release
   evidence. Only then describe the Action as externally verified.

Rollback is deletion or archival of the unpublished fixture repository. After
publication, preserve failed runs as evidence; fix forward with a new commit
instead of rewriting history.
