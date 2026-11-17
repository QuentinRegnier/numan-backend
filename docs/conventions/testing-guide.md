# Unit Testing Guide

## Infrastructure Fakes (`internal/infrastructure/test/`)
- **L1 Redis:** `test.SetupTestRedis(t)` spins up an isolated in-memory `miniredis/v2` instance.
- **L3 PostgreSQL:** `test.SetupMockDB(t, queryFn, execFn)` + `test.NewMockRows(columns, rows)` intercepts `database/sql` without a live PostgreSQL instance.
- **MongoDB / MinIO:** `mongo_fake.go` and `minio_fake.go`.

## Common Commands
```bash
# Single package
go test -v ./internal/domain/models/auth_models/...

# Caches and workers
go test -v ./internal/service/cache_service/... ./internal/worker/...

# Full lint + SQL contract + test suite
make check
```

## Best Practices
- Call `_ = pkg.InitSnowflake(1)` whenever tested code generates Snowflake IDs.
- Always cover `Validate()` in `*_validation_test.go` for any new field or model.
- Keep `test.NewMockRows(...)` columns strictly synchronized with the `rows.Scan(...)` call in the tested repository.
