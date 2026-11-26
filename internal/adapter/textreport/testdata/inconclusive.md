# demo-app GET /customers/{id}/orders at 111111111111 (baseline 222222222222)

## Inconclusive

No cause is supported by the evidence. The checks below say what was verified and what was missing.

## Evidence

- E1 version 111111111111: 40 traces, median 950.0 ms, median 0 query spans
- E2 time breakdown: mean per trace 4.4 → 145.6 ms (+141.2 ms; 38 and 38 traces, without the slowest 5%)
  - http:moderation:8090: 3.5 → 143.3 ms per trace, explains 99%, presence 100% → 100%, 1.0 → 1.0 calls per trace, 3.5 → 143.3 ms per call; operations: `POST` (100% → 100%)
  - db:postgresql: 0.6 → 0.7 ms per trace, explains 1%, presence 100% → 100%, 2.0 → 2.0 calls per trace, 0.3 → 0.3 ms per call; operations: `SELECT id FROM rooms WHERE id = $1` (100% → 100%), `INSERT INTO messages (room_id, body) VALUES ($1, $2)` (100% → 100%), `SELECT 1` (50% → 50%) and 1 more
  - own_time: 0.3 → 0.3 ms per trace, explains 0%, presence 100% → 100%

## Checks

- **n_plus_one** (skipped): no database query spans in the 40 analyzed traces
  - Hint: See the db_spans check.
- **missing_index** (skipped): no query plans
  - Hint: Plans come from the evaluation today.
- **trace_shape** (ran): mean per trace 4.4 → 145.6 ms (+141.2 ms, 38 and 38 traces); dominant: http:moderation:8090, 99% of the difference; nothing promoted
- **commit_correlation** (not applicable): no candidate to correlate

## Warnings

- no traces of the operation in baseline version 2222222
