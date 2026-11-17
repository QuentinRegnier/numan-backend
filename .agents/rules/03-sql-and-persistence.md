---
description: PostgreSQL L3 contracts, schema.sql alignment, rows.Scan safety, pq.CopyIn, and temporal types
globs: ["schema.sql", "internal/repository/postgres/**/*.go", "internal/worker/**/*.go"]
---

# Rule 03 — SQL, L3 Persistence & `Scan()` Contracts

## 1. Strict `schema.sql` <-> `rows.Scan()` Alignment
- **`schema.sql` is the single source of truth for PostgreSQL L3.**
- **No hardcoded business SQL in Go:** Never embed raw `db.Query("SELECT ...")` or `db.Exec("INSERT ...")` business queries in handlers or services; define SQL functions in `schema.sql` and expose them via `internal/repository/postgres/`.
- Before changing any SQL function (`RETURNS TABLE` / `RETURNS SETOF`), view, or `rows.Scan(dest...)` call:
  1. Verify exact column count, order, and Go destination types (`sql.NullString`, `sql.NullInt64`, `sql.NullBool`, `sql.NullTime` for nullable columns).
  2. Run `make check-sql` (`python3 skills/schema-contract-validator/scripts/check_sql_scans.py`).
  3. Update all corresponding `test.NewMockRows(...)` mocks in `*_test.go`.

## 2. Temporal Types and Soft Deletes
- **Epoch-ms as `BIGINT` (`schema.sql` Decision D1):**
  - Columns populated via `pq.CopyIn` from Go `int64` epoch-ms fields MUST be `BIGINT` (PostgreSQL rejects decimal integer text like `'1717600000000'` for `timestamptz`).
  - Only legacy RFC3339 tables (`comments`, `likes`) retain `timestamptz`.
  - Use `util.now_ms()`, `util.ms_to_ts()`, and `util.ts_to_ms()` in SQL.
- **Post Soft-Delete Filter (`schema.sql` Decision D2):**
  - Live posts satisfy `visibility >= 0` (`-1` = soft delete, `2` = friends-only). Never filter with `visibility != 2`.

## 3. `pq.CopyIn` Workers and Topological Sort
- In `internal/worker/postgres_batch.go`, `executionOrder` preserves foreign-key integrity (`EntityUser` -> `EntityPost` -> `EntityComment` -> `EntityLike` -> ...). Any new relational entity must be inserted at the proper dependency rank with its mapper and DLQ handling (`postgres_errors`).
