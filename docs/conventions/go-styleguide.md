# Go Style Guide — `numan-backend`

## Package Organization
- `internal/api/handlers/<domain>_handlers/`: one file per endpoint (`<action>_<entity>_handler.go`).
- `internal/domain/models/<domain>_models/`: strict separation across `*_i_models.go`, `*_o_models.go`, `*_io_models.go`, `*Payload.go`, and `*_view.go`.
- `internal/service/<domain>_service/`: one file per business use case.
- `internal/repository/postgres/`: one `func_<name>.go` file per SQL function/query wrapper.

## Project Idioms
1. **Systematic Validation:** Every I/O struct implements `Validate() error`.
2. **Unified Time Helpers:** Always use `internal/domain/helpers.go` (`NowMillis`, `NowTime`, `TimeToMillis`, `MillisToTime`).
3. **Contextual Logging:** Always use `numan_log.Info(ctx)`, `Warn(ctx)`, `Error(ctx)`, `Debug(ctx)` to preserve `TraceID` and `UserID`.
4. **SQL Cursor Cleanup:** Always check errors on `rows.Close()` and `rows.Err()`.
5. **Formatting & Linting:** `goimports` + `go vet ./...` + `staticcheck ./...` (`make lint`).
