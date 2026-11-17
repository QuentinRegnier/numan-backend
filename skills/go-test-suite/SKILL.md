---
name: go-test-suite
description: Run, filter, and diagnose Go unit tests and static analysis (go vet, staticcheck) in numan-backend. Trigger after modifying any Go source file or when investigating regressions.
---

# Skill: `go-test-suite`

## When to Trigger
- After creating or modifying any `.go` file under `cmd/`, `internal/`, or `admin/`.
- Before completing a task or proposing a commit.
- When diagnosing SQL scan errors, Redis (`miniredis`) failures, or domain validation failures.

## Execution Workflow

### 1. Run Targeted Package Tests
Run tests on the modified package first for fast feedback:
```bash
go test -v ./internal/<domain>/<package>/...
```
Common targets:
- Models: `go test -v ./internal/domain/models/...`
- L1 Cache: `go test -v ./internal/service/cache_service/...`
- Business services: `go test -v ./internal/service/<service_name>/...`
- Workers: `go test -v ./internal/worker/...`

### 2. Static Analysis (`lint`)
```bash
make lint
```
(Runs `go vet ./...` and `$(go env GOPATH)/bin/staticcheck ./...` when compiled for Go 1.25+.)

### 3. Full Test Suite
```bash
go test ./...
```

## Quick Failure Diagnosis
- **`sql: expected N destination arguments in Scan, not M`**:
  Column count mismatch between a `test.NewMockRows(...)` mock (or `schema.sql` function) and `rows.Scan(...)` in `internal/repository/postgres/`. Check `docs/knowledge/common-pitfalls.md` and run the `schema-contract-validator` skill.
- **Snowflake nil-node panic**:
  Ensure `_ = pkg.InitSnowflake(1)` is called at test setup.
- **Redis failure in tests**:
  Ensure `test.SetupTestRedis(t)` from `internal/infrastructure/test/redis_fake.go` is initialized.

## References
- `.agents/rules/06-testing-standards.md`
- `internal/infrastructure/test/`
