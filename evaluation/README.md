# Evaluation kit

This directory supports repeatable, privacy-conscious design-user evaluations.
It separates three things that should never be conflated:

1. internal synthetic regression evidence;
2. independently recorded expected-versus-actual behavior; and
3. authorized public adoption or case-study evidence.

The current repository establishes the first. The templates make it possible
to collect the second and third without claiming they already exist.

## Contents

- [`schemas/evaluation-record.schema.json`](schemas/evaluation-record.schema.json)
  defines a machine-checkable record.
- [`templates/expected-vs-actual.yaml`](templates/expected-vs-actual.yaml)
  records ground truth before the tool is run.
- [`templates/case-study-consent.md`](templates/case-study-consent.md) separates
  evaluation consent from permission to publish names, quotes, or artifacts.
- [`templates/public-defect-report.md`](templates/public-defect-report.md)
  captures a sanitized correctness defect.
- [`ANONYMIZATION.md`](ANONYMIZATION.md) defines a redaction and revalidation
  procedure.
- [`ADOPTION_EVIDENCE.md`](ADOPTION_EVIDENCE.md) defines what may be counted or
  stated publicly.
- [`fixtures/README.md`](fixtures/README.md) identifies the sanitized fixture
  packs and their evidence class.

## Evaluation workflow

1. Pin the exact TCG version or commit.
2. Inventory the evidence sources that are and are not in scope.
3. Record expected status, consumers, dependency paths, and uncertainty before
   running TCG.
4. Run one authoritative check and preserve the versioned JSON output.
5. Compare expected versus actual without changing ground truth to match the
   tool.
6. Classify mismatches, setup friction, and time to first useful result.
7. Sanitize a new synthetic reproduction when the original cannot be shared.
8. Obtain separate authorization before publishing an organization name,
   quotation, logo, repository, artifact, or case study.

Do not submit credentials, production endpoints, customer data, proprietary
queries, internal repository names, or security-sensitive findings publicly.
Use [private vulnerability reporting](../SECURITY.md) where appropriate.

## Evidence language

Use “TCG matched the pre-recorded expectation for this evaluation” for a
matching case. Do not turn a small, selected evaluation set into a percentage,
precision/recall claim, organization-wide coverage claim, or production-adopter
claim. The [benchmark](../benchmarks/README.md) has the same explicit boundary.
