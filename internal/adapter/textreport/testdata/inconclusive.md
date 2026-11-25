# demo-app GET /customers/{id}/orders at 111111111111 (baseline 222222222222)

## Inconclusive

No cause is supported by the evidence. The checks below say what was verified and what was missing.

## Evidence

- E1 version 111111111111: 40 traces, median 950.0 ms, median 0 query spans

## Checks

- **n_plus_one** (skipped): no database query spans in the 40 analyzed traces
  - Hint: See the db_spans check.
- **missing_index** (skipped): no query plans
  - Hint: Plans come from the evaluation today.
- **commit_correlation** (not applicable): no candidate to correlate

## Warnings

- no traces of the operation in baseline version 2222222
