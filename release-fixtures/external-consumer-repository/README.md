# Telemetry Change Guard external-consumer fixture

This directory is the complete source for the proposed independent public
fixture repository. It deliberately consumes Telemetry Change Guard through an
immutable remote Action coordinate, never `uses: ./`.

The fixture proves five consumer-visible contracts:

| Case | Change source | Expected status | Expected Action outcome |
| --- | --- | --- | --- |
| `pass` | explicit ChangeSet | `PASS` / exit `0` | success |
| `block` | explicit ChangeSet | `BLOCK` / exit `2` | failure |
| `incomplete` | explicit ChangeSet plus malformed required evidence | `INCOMPLETE` / exit `3` | failure |
| `snapshot` | baseline/candidate snapshot pair | `BLOCK` / exit `2` | failure |
| `migration` | compatibility migration plan | `READY` / exit `0` | success |

Each case also verifies the versioned JSON report and job summary. A final job
downloads all five artifacts and validates their schemas and statuses.

The source is retained here because creating another GitHub repository is an
external action that requires owner authorization. Until that repository is
created and its workflow passes, this is a release-blocker fixture—not a claim
of independent verification. See [PUBLISHING.md](PUBLISHING.md).
