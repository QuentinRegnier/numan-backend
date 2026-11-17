# ☁️ AGENT DIRECTIVES — NUMAN BACKEND

Welcome to the **numan** backend, a high-performance social network and real-time messaging backend written in **Go 1.25**.
This document is the single entry point and authoritative reference for every AI agent operating in this repository.

---

## 1. SYSTEM ARCHITECTURE (L1 / L2 / L3 + WRITE-BEHIND)

`numan-backend` is not a synchronous CRUD monolith; it is an asynchronous three-tier data router built for sub-5ms API latency:

1. **L1 — Redis 7 (`volatile-lfu`, MsgPack, ZSET, FIFO):** Hot in-memory storage (Object Cache, MOST Cache, SPEED Cache, Session/Presence Cache) and asynchronous write queues (`REQUEST Cache` / `WorkerQueue` via `redis.EnqueueDB`).
2. **L2 — MongoDB 7:** Warm denormalized document storage absorbing L1 *Cache Misses* without hitting PostgreSQL.
3. **L3 — PostgreSQL 16 (`schema.sql`):** ACID relational source of truth, updated asynchronously in batches (*Write-Behind* + topological ordering + `pq.CopyIn`) inside `internal/worker/`.

---

## 2. NON-NEGOTIABLE GOLDEN RULES

1. **Zero SQL / `Scan()` Regressions:**
   Any change to `schema.sql` (table, view, or `RETURNS TABLE` / `RETURNS SETOF` function) or to a repository in `internal/repository/postgres/` MUST preserve strict alignment in count, order, and Go destination types for `rows.Scan(...)` and `pq.CopyIn` mappers. Unix millisecond timestamps use `BIGINT` with `domain.TimeToMillis` / `domain.MillisToTime`. No raw business SQL queries (`db.Query("SELECT ...")`) may be hardcoded in Go handlers or services.
2. **Strict L1 / L2 / L3 Pipeline & DDD Boundaries:**
   HTTP/WebSocket handlers MUST **never** call Redis, MongoDB, or PostgreSQL directly. Services MUST **never** bypass `cache_service` or `internal/repository/` abstractions to touch DB clients directly. Reads follow `L1 Redis -> fallback L2 Mongo -> fallback L3 Postgres -> L1 rehydration/promotion`.
3. **Mandatory Write-Behind for Mutations:**
   High-frequency mutations update L1 Redis synchronously and enqueue an `AsyncEvent` via `redis.EnqueueDB` for batch persistence in `internal/worker/postgres_batch.go` and `mongo_batch.go`. Never use `EnqueueDB` for read operations.
4. **Systematic Payload Validation:**
   Every input/output model in `internal/domain/models/` implements `Validate() error` and is validated before executing service business logic.
5. **Typed Errors & Contextual Structured Logging:**
   - Always return `*numan_error.AppError` using constants from `internal/domain/numan_error/app_error.go`. Never hardcode error codes or expose internal error details to clients, and never call `panic()` in handlers, services, or workers.
   - Always use `numan_log.Info(ctx)` / `Warn(ctx)` / `Error(ctx)` / `Debug(ctx)` to propagate `TraceID` and `UserID`. Never log secrets (HMAC secrets, TOTP secrets, JWTs, passwords, device leaks).
6. **Mandatory Testing & Verification:**
   Every production code addition or modification MUST be accompanied by updated/new automated tests reusing `internal/infrastructure/test/` (`miniredis`, `postgres_fake.go`, `mongo_fake.go`, `minio_fake.go`).
7. **Protect Uncommitted User Work:**
   Never modify, reset, stage, or delete in-progress files outside the explicit scope of the task (such as ongoing edits in `internal/templates/emails/`).

---

## 3. REPOSITORY MAP

- `cmd/main.go`: Main HTTP/WebSocket entrypoint, infrastructure bootstrap, conditional Redis seeding, and background workers (`SERVER_ROLE`).
- `cmd/watermark/main.go`: Zero-disk serverless AVIF DCT steganography watermarking microservice.
- `cmd/admin/main.go` & `admin/service/`: Forensic extraction CLI for watermarked media.
- `internal/api/`: Gin router (`routes.go`), middlewares (`JWTMiddleware`, `HMACMiddleware`, `RateLimiter`, `MaxBodySize`), domain handlers, and WebSocket hub (`internal/api/websocket/`).
- `internal/domain/`: I/O models (`models/`), time helpers (`helpers.go`), typed errors (`numan_error/`), structured logging (`numan_log/`), and Mongo schemas (`schemas/`).
- `internal/service/`: Business services, recommendation engine (`algorithm_service/`), caches (`cache_service/`), moderation, discovery map/POI, messaging, auth/2FA, and real-time broadcast (`realtime_service/`).
- `internal/repository/`: `redis/` (`manager.go`, `async_queue.go`), `mongo/`, and `postgres/` abstractions.
- `internal/worker/`: Write-Behind batch workers, mappers, time-decay, and orphan cleanup workers.
- `internal/infrastructure/test/`: Centralized test fakes (`postgres_fake.go`, `redis_fake.go`, `mongo_fake.go`, `minio_fake.go`).
- `internal/variables/`: Global constants, Redis keys, and dynamic runtime variables.
- `schema.sql` & `asyncapi.yaml`: PostgreSQL L3 schema and WebSocket AsyncAPI contract.

---

## 4. PRE-DELIVERY VALIDATION PROTOCOL

Before marking any code task complete:
1. Run targeted unit tests for affected packages and callers: `go test -v ./internal/<package>/...`
2. Run static analysis: `make lint` (`go vet ./...` and `staticcheck` when built for Go 1.25+).
3. Validate SQL/Go scan contracts if `schema.sql` or `internal/repository/postgres/` changed: `make check-sql`.
4. Regenerate Swagger docs if routes, handlers, or I/O contracts changed: `$(go env GOPATH)/bin/swag init -g cmd/main.go -d . --parseDependency --parseInternal` (or `make docs`).

---

## 5. RULES, SKILLS & DOCUMENTATION GOVERNANCE

- **Modular Rules:** `.agents/rules/` (architecture, Go conventions, SQL persistence, error/logging, security, testing, Git workflow).
- **Agent Skills:** `skills/` (including `api-expert-go`, `clean-coding-architecture`, `make-perfect-route`, `api-error-standards`, `api-logging-standards`, `mandatory-testing-standards`, `go-test-suite`, `schema-contract-validator`, `api-docs-sync`, `cache-pipeline-verifier`, `continuous-learning`).
- **Knowledge & ADRs:** `docs/architecture/`, `docs/adr/`, `docs/conventions/`, `docs/knowledge/`, `docs/runbooks/`.
- **Continuous Improvement:** Record recurring pitfalls in `docs/knowledge/common-pitfalls.md`. Any change to `AGENTS.md`, `.agents/rules/`, or security policies requires explicit human approval.
