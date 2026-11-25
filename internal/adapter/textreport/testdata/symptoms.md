# Symptoms of api: 222222222222 → 111111111111

Baseline 2222222222222222222222222222222222222222, bad version 1111111111111111111111111111111111111111 (ordered by time).

> The baseline is 222222222222 because it appeared first in the traces.

| Operation | Regression | n (base → bad) | n ratio | p50 ms (base → bad) | p50 ratio | p95 ms (base → bad) | errors (base → bad) |
|---|---|---|---|---|---|---|---|
| GET /orders | latency | 40 → 38 | 0.95 | 10.0 → 100.0 | 10.00x | 12.0 → 130.0 | 0.0% → 5.0% |
| GET /health | sample < 30 | 3 → 0 | - | 1.0 → 0.0 | - | 1.0 → 0.0 | 0.0% → 0.0% |

Suggested operation (a suggestion, not the origin): GET /orders.

    rootline analyze --operation 'GET /orders'
