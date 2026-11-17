---
description: Standardized error handling with numan_error and structured contextual logging with numan_log
globs: ["internal/**/*.go", "cmd/**/*.go"]
---

# Rule 04 — Error Handling & Structured Logging

## 1. Application Errors (`numan_error`)
- Use `*numan_error.AppError` and semantic constructors from `internal/domain/numan_error/app_error.go`:
  - `NewBadRequest`, `NewUnauthorized`, `NewForbidden`, `NewNotFound`, `NewConflict`, `NewTooManyRequests`, `NewInternal`, `NewAppError`.
- Use only constants `Code*` declared in `app_error.go` (`CodeInvalidPayload`, `CodeDatabaseError`, `CodeCacheError`, `CodeNotFound`, etc.). Register any new business error code there first.
- Never leak raw technical errors (`Err`), SQL statements, connection strings, or stack traces in client-facing `Message` responses.
- Always render HTTP errors in Gin handlers using `numan_error.RespondWithError(c, err)`.
- Do not wrap an existing `*numan_error.AppError` unnecessarily with `numan_error.NewInternal()`.
- Never use `panic()` for operational request failures (reserved for fatal bootstrap in `cmd/main.go`).

## 2. Structured Logging (`numan_log`)
- Always use `internal/domain/numan_log` (`Info(ctx)`, `Warn(ctx)`, `Error(ctx)`, `Debug(ctx)`) rather than `fmt.Println`, `log.Println`, or introducing a parallel logger.
- Pass request `ctx` so `enrichFromContext` automatically attaches `TraceID` and `UserID`.
- Avoid duplicate error logging across every layer; log at the appropriate boundary with structured fields.
- **Zero Secrets in Logs:** Never log passwords, JWTs, HMAC secrets, TOTP secrets, session secrets, private keys, or sensitive payloads.
