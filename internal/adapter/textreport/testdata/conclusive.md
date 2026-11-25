# demo-app GET /customers/{id}/orders at 111111111111 (baseline 222222222222)

## Cause: n_plus_one (C1, confidence high)

- Location: example.com/demo-app/internal/orders.(*Service).List, demo-app/internal/orders/service.go:42 (level 1, in a loop)
- Commit: 333333333333 (touches the location)

## Evidence

- E1 repeated query `SELECT * FROM items WHERE order_id = $1`: 10 times per parent (median), 3 rows each, +1 matched in 100%, in 40 of 40 traces
- E2 commit 333333333333 changed example.com/demo-app/internal/orders.(*Service).List (location)
- E3 version 111111111111: 40 traces, median 12.5 ms, median 11 query spans

## Checks

- **missing_index** (skipped): no query plans
  - Hint: Plans come from the evaluation today.
- **n_plus_one** (ran): 1 candidate(s) in 40 traces
- **commit_correlation** (ran): touched: C1 by commit 3333333 (location)
