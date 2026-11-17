# ADR 0001 — Store Timestamps as `BIGINT` (Unix Epoch Milliseconds)

- **Status:** Accepted (implemented in `schema.sql` Decision D1 and `internal/domain/helpers.go`)

## Context
Write-Behind workers bulk-load PostgreSQL via `pq.CopyIn` from Go structs whose timestamps are `int64` Unix milliseconds. During `COPY FROM STDIN`, `lib/pq` encodes `int64` values as decimal text (e.g., `'1717600000000'`), which PostgreSQL rejects for `timestamptz` columns.

## Decision
- Store worker-written timestamp columns as `BIGINT` (UTC Unix milliseconds), manipulated in Go via `domain.TimeToMillis`, `domain.MillisToTime`, and `domain.NowMillis`.
- Provide SQL helpers `util.now_ms()`, `util.ms_to_ts(bigint)`, `util.ts_to_ms(timestamptz)`, and `util.ms_to_rfc3339(bigint)` for views scanned into `time.Time` or `sql.NullTime`.
- Retain `timestamptz` only on legacy tables whose payloads carry RFC3339 strings (`comments`, `likes`).

## Consequences
- Eliminates bulk `pq.CopyIn` type conversion failures.
- All new tables and SQL views must follow this convention and never mix seconds with milliseconds.
