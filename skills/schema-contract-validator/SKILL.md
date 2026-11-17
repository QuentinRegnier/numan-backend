---
name: schema-contract-validator
description: Verify strict alignment between SQL functions/views in schema.sql, rows.Scan() destinations in internal/repository/postgres/, and test mocks. Trigger whenever SQL or Postgres repositories change.
---

# Skill: `schema-contract-validator`

## When to Trigger
- Any change to `schema.sql` (adding, removing, or reordering columns in a table, view, or `RETURNS TABLE` / `RETURNS SETOF` function).
- Any change to `internal/repository/postgres/func_*.go` or `internal/worker/postgres_batch.go`.
- Any runtime or test error matching `expected N destination arguments in Scan, not M`.

## Verification Steps

### 1. Run the Static SQL/Go Contract Checker
```bash
python3 skills/schema-contract-validator/scripts/check_sql_scans.py
```
(Or via `make check-sql`.)

### 2. Verify the 4 Critical Invariants
1. **Column Count:** Number of columns returned by `RETURNS TABLE(...)` / `RETURNS SETOF <view>` in `schema.sql` MUST equal `len(dest)` in `rows.Scan(dest...)`.
2. **Nullable Columns (`NULL`):** Every nullable SQL column must scan into `sql.NullString`, `sql.NullInt64`, `sql.NullBool`, or `sql.NullTime` rather than non-nullable primitives.
3. **`pq.CopyIn` Timestamps (Decision D1):** Go `int64` epoch-ms fields persisted by `pq.CopyIn` map to `BIGINT` columns (except legacy RFC3339 `comments`/`likes` tables).
4. **Synchronized Test Mocks:** Every `test.NewMockRows(cols, rows)` stubbing the modified repository query must declare identical column count and order.

## References
- `.agents/rules/03-sql-and-persistence.md`
- `docs/knowledge/sql-signatures.md`
- `docs/knowledge/common-pitfalls.md`
