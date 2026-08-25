# Sanitized fixture packs

Two in-repository packs serve different verification purposes:

- [`release-fixtures/external-consumer-repository`](../../release-fixtures/external-consumer-repository)
  is a complete synthetic repository layout used to test consumption from
  outside the Action source tree. It includes no third-party or production
  data. A truly separate GitHub repository has not been published.
- [`benchmarks/corpus`](../../benchmarks/corpus) contains original Apache-2.0
  synthetic cases with reviewed ground truth for release regression checks.

Neither pack is an independent public corpus or adoption case study. New
design-user fixtures require provenance, license/authorization, pre-recorded
ground truth, anonymization review, and a clear evidence-class label before
merge.

Do not copy public internet dashboards or rules into this repository merely
because they are viewable. Confirm license compatibility and attribution, or
construct an original semantic reproduction.
