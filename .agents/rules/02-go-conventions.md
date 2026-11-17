---
description: Go 1.25 coding conventions, context propagation, domain helpers, and resource management
globs: ["**/*.go"]
---

# Rule 02 — Go 1.25 Conventions

## 1. Signatures, Context, and Time Handling
- **Context Propagation:** Every service, cache, repository, and worker function accepts `ctx context.Context` as its first argument. Propagate `c.Request.Context()` from HTTP handlers rather than creating `context.Background()` mid-request.
- **Time & Timestamps:**
  - Always use `internal/domain/helpers.go` (`domain.NowMillis()`, `domain.NowTime()`, `domain.TimeToMillis()`, `domain.MillisToTime()`, `domain.TimeToSeconds()`).
  - Never persist local non-UTC `time.Now()` timestamps.
- **Identifiers:** Distributed entity IDs use `int64` Snowflake IDs generated via `internal/pkg/snowflake.go` (`pkg.GenerateID()`).

## 2. I/O Models and Validation
- Follow `internal/domain/models/<domain>_models/` naming conventions:
  - `*_i_models.go` (input only), `*_o_models.go` (output only), `*_io_models.go` (input & output), `*Payload.go`, `*_view.go`.
- Do not create redundant empty response structs when `200 OK` without a body suffices.
- Every input struct and payload implements `Validate() error`, enforced at service entry.
- Prefer value semantics over unnecessary pointers for small models/views.

## 3. Concurrency, Formatting, and Resource Cleanup
- Always close SQL cursors (`defer rows.Close()`) and log close errors via `numan_log.Error(ctx)`.
- Always check `rows.Err()` after `for rows.Next()`.
- Do not spawn unguarded background goroutines without cancellation via `ctx.Done()` and panic recovery.
- Format code with `gofmt` / `goimports` (matching `.idea/go.imports.xml`) and validate with `go vet ./...`.
