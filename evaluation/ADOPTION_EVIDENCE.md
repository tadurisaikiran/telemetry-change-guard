# Adoption and evidence rules

Public project claims must be traceable to evidence and scoped to what that
evidence actually establishes.

## Evidence classes

| Class | What it can support | What it cannot support |
| --- | --- | --- |
| Unit, integration, E2E, and synthetic benchmark | Implemented behavior and regression protection for reviewed fixtures | Independent field accuracy or adoption |
| Maintainer-run sanitized evaluation | Reproduction of one expected behavior | Independent endorsement or production use |
| Independent design-user evaluation | Expected-versus-actual outcome for the recorded scope | Organization-wide coverage or use beyond the session |
| Authorized adopter statement | The exact approved use by the named organization | Broader deployment, duration, or success claims not approved |
| Authorized case study | The reviewed facts, quote, and outcomes in that study | General accuracy or outcome guarantees |

## Counting rules

- Count a repository once per materially distinct evaluation record, not once
  per command or fixture.
- Identify synthetic, sanitized representative, and real in-environment cases
  separately.
- Record non-matches, setup failures, incomplete results, and withdrawals; do
  not count only successful cases.
- Never infer a public adopter from a star, fork, download, issue, conversation,
  private evaluation, or employee interest.
- Never identify a company based only on an employee's participation. Public
  organization claims require authorized representative approval.
- Do not calculate public precision/recall from selected, evolving, or
  maintainer-labeled fixtures.

## Acceptable language

- “The synthetic release-gate corpus passed 11 reviewed cases.”
- “One independent evaluation matched its pre-recorded expectation for the
  configured sources.”
- “Organization X uses TCG as described in its approved adopter statement.”

Include the corpus or evaluation version, date, scope, and link to authorized
evidence whenever practical.

## Prohibited shortcuts

Do not describe the project as production-proven, organization-wide, or
independently validated without the corresponding evidence. Do not promise
that a pass eliminates incidents. Do not publish a participant's name, logo,
quote, repository, artifacts, or results because they appeared in a private
session.

When evidence changes, update or remove the claim. If authorization is
withdrawn, remove future references promptly and preserve only the minimum
private audit record required by project governance.
