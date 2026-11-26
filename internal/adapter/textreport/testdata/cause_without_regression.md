# demo-app GET /customers/{id}/orders at 111111111111 (baseline 222222222222)

## Cause: n_plus_one (C1, confidence high)

- Location: example.com/demo-app/internal/books.attachAuthors, demo-app/internal/books/authors.go:27 (level 1, in a loop)
- Commit: none correlated (see the commit_correlation check)

## Evidence

- E1 repeated query `SELECT name FROM authors WHERE id = $1`: 5 times per parent (median), 1 rows each, +1 matched in 100%, in 40 of 40 traces

## Checks

- **symptom_regression** (ran): baseline 2222222: n=40, p50 8.0 ms, p95 140.0 ms; bad 1111111: n=40, p50 10.4 ms, p95 68.6 ms; n ratio 1.00; no latency regression: p50 1.30x (+2.40 ms): under 1.5x and under 2x; p95 0.49x (-71.40 ms): under 2x
  - Hint: The detectors ran anyway. If this is not the operation that got slower, find it with rootline symptoms.
- **n_plus_one** (ran): 1 candidate(s) in 40 traces

## Warnings

- the cause is in the code of the operation, but the operation has no latency regression (see symptom_regression): the detectors read the shape of the traces, not their latency, so the cause may not explain the slowdown you are after; rootline symptoms shows whether another operation regressed
