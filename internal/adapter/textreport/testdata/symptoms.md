# Symptoms of api: 222222222222 → 111111111111

Baseline 2222222222222222222222222222222222222222, bad version 1111111111111111111111111111111111111111 (ordered by time).

> The baseline is 222222222222 because it appeared first in the traces.

**Generalized regression:** at least 75% of the operations with the minimum sample regressed together. This usually points to a saturated shared resource (pool, lock, CPU), not to each operation.

| Operation | Regression | n (base → bad) | n ratio | p50 ms (base → bad) | p50 ratio | p95 ms (base → bad) | errors (base → bad) | onset | ceiling |
|---|---|---|---|---|---|---|---|---|---|
| GET /orders | latency | 40 → 38 | 0.95 | 10.0 → 100.0 | 10.00x | 12.0 → 130.0 | 0.0% → 5.0% | +12.5s | 10000 ms |
| GET /health | sample < 30 | 40 → 1 | **0.03 drop** | 1.0 → 2.0 | 2.00x | 1.0 → 2.0 | 0.0% → 0.0% | - | - |

onset: the 3rd trace above the baseline p95, counted from the first trace of the bad version. drop: the bad version has under half the traces (stuck requests that never became spans, or a different load). ceiling: at least 20% of the traces stop together, usually cut by a timeout.

Suggested operation (a suggestion, not the origin): GET /orders.

    rootline analyze --operation 'GET /orders'
