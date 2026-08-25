# Benchmark results

`make benchmark` writes the current machine result to
`dist/benchmark/results.json`; generated timing and memory values are not
committed because they depend on the host and workload.

When publishing a future benchmark result, retain the exact corpus version,
TCG release or commit, clean-tree state, environment, raw JSON, and review
method. Do not compare timings across unlike hosts. Do not describe this
internal corpus as independent validation.
