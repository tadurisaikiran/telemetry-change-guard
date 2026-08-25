# Anonymization procedure

Sanitization must protect the participant without destroying the semantic
relationship needed to evaluate correctness.

## Remove or replace

- organization, product, customer, repository, service, team, and owner names;
- internal domains, IP addresses, cluster names, namespaces, account IDs, and
  regions when not required for semantics;
- tokens, cookies, headers, credentials, private keys, signed URLs, and secret
  references;
- proprietary queries, thresholds, labels, annotations, runbooks, ticket IDs,
  and incident details; and
- personal data and any regulated or contract-restricted content.

Do not redact secrets into recognizable prefixes. Remove them and rotate any
credential that may have been exposed during preparation.

## Preserve

- the change kind and telemetry domain;
- exact old/new identity relationships using invented names;
- consumer kind, required/optional evidence state, and criticality class;
- the direct or transitive graph shape;
- the parseable query structure necessary to reproduce the behavior; and
- the expected status, impact, diagnostic category, and exit-code relation.

## Revalidate

1. Construct new synthetic files rather than mechanically masking a production
   dump when possible.
2. Search the pack for original names, domains, paths, emails, tokens, and
   high-entropy values.
3. Run TCG again in an isolated environment with network access disabled unless
   the reproduction specifically requires a reviewed local endpoint.
4. Confirm the defect and the pre-recorded expectation still hold.
5. Review the complete diff and generated JSON; reports can repeat sensitive
   input values.
6. Obtain the required public-sharing approval.

Anonymized does not mean authorized. A sanitized pack stays private until the
participant or authorized organization representative permits publication.
