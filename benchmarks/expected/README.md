# Ground-truth method

Ground truth is established before TCG runs by manually reading the proposed
contract change and the complete synthetic consumer fixture. The manifest
states the expected status and exact observable contract for each case.

Reviewers must confirm:

1. the source is complete for the isolated case;
2. the query or explicit mapping proves or disproves the dependency;
3. the operational impact category matches the consumer type;
4. required malformed or unresolved evidence fails closed; and
5. safe lookalike identifiers do not become dependencies.

Changing an expectation merely to match current program output is forbidden.
A product bug must be fixed or the case must document why its reviewed ground
truth changed.
