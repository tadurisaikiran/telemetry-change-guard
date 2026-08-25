# Sanitized correctness defect

Use this for incorrect dependency, classification, status, or fail-closed
behavior that is safe to discuss publicly. Report vulnerabilities privately
through [`SECURITY.md`](../../SECURITY.md).

## Build

- TCG version:
- Exact commit:
- Result schema:
- OS/architecture:
- Installation path:

## Pre-recorded expectation

- Expected status:
- Expected consumers:
- Expected dependency paths:
- Expected diagnostics or uncertainty:
- Source of ground truth and reviewer:

## Actual result

- Actual status and exit code:
- Actual consumers and paths:
- Permanent diagnostic codes:
- Sanitized JSON result attached: yes/no

## Minimal reproduction

Attach the smallest synthetic ChangeSet or plan, configuration, mappings, and
consumer files that preserve the defect. State which original evidence was
excluded. Do not attach production endpoints, tokens, customer data,
proprietary queries, internal owner names, or confidential repository paths.

## Safety impact

- [ ] Possible false-safe result or missed dependency
- [ ] Incorrect operational-impact classification
- [ ] Incorrect block/warn/pass policy result
- [ ] Missing evidence was not reported incomplete
- [ ] Over-reporting or false positive
- [ ] Determinism or machine-contract difference
- [ ] Usability only

## Anonymization validation

- [ ] The reproduction contains no credentials or sensitive telemetry.
- [ ] The semantic relationship still reproduces after renaming.
- [ ] Expected behavior was not changed to match the tool.
- [ ] A second person reviewed public-sharing safety where required.
