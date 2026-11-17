---
description: Mandatory unit testing standards, centralized fakes reuse, and deterministic coverage
globs: ["**/*_test.go", "internal/infrastructure/test/**/*.go"]
---

# Rule 06 — Testing Standards

## 1. Reuse Centralized Infrastructure Fakes
- Always reuse `internal/infrastructure/test/` rather than creating ad-hoc mocks:
  - `test.SetupTestRedis(t)` (`miniredis/v2` isolated L1 & `WorkerQueue`).
  - `test.SetupMockDB(t, queryFn, execFn)` and `test.NewMockRows(cols, rows)` (`database/sql` driver mock).
  - `mongo_fake.go` and `minio_fake.go` for L2 and S3.
- Call `_ = pkg.InitSnowflake(1)` when testing code that generates Snowflake IDs.

## 2. Mandatory Coverage per Change
- **No untested production code changes:** Every modified production file requires updating/extending its tests in the same cycle; every new production `.go` file requires a corresponding `*_test.go` file.
- **I/O Models (`internal/domain/models/`):** Add/update `*_validation_test.go` covering valid, invalid, and boundary inputs for `Validate()`.
- **Services (`internal/service/`):** Test nominal paths, validation failures, typed `numan_error.AppError` responses, and Redis L1 / `WorkerQueue` side effects.
- **PostgreSQL Repositories (`internal/repository/postgres/`):** Keep `test.NewMockRows` columns strictly aligned with `rows.Scan(...)`.

## 3. Determinism & Concurrency Safety
- All unit tests must run offline without Docker (`go test ./...`).
- Prefer channels, wait groups, or context cancellation over arbitrary `time.Sleep` calls in async tests.
- Clean up modified globals with `t.Cleanup`.
