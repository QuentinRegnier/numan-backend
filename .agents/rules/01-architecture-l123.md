---
description: L1/L2/L3 cache hierarchy, hydration pipeline, Write-Behind, and strict DDD layer boundaries
globs: ["internal/**/*.go", "cmd/**/*.go"]
---

# Rule 01 — Architecture L1 / L2 / L3, DDD & Write-Behind

## 1. Strict Layer Separation
- **`internal/api/` (Routes, Handlers & WebSocket):**
  - `routes.go` owns route wiring; handlers live in domain subdirectories (`<domain>_handlers/`).
  - Handlers remain thin: extract auth identity (`pkg.GetUserIDFromContext`), bind/parse HTTP payload, call business service, and render response (`numan_error.RespondWithError` or `c.JSON`).
  - **Forbidden:** Directly calling Redis, MongoDB, or PostgreSQL clients, writing SQL, or embedding business logic in handlers.
- **`internal/service/`:**
  - Owns business rules, calls `input.Validate()`, orchestrates L1 (`cache_service`) and L2/L3 fallbacks, and enqueues asynchronous writes.
  - Service A must never call Service B's repository directly; it calls Service B.
  - Services must never depend on HTTP framework details (`*gin.Context`).
- **`internal/repository/`:**
  - `redis/` (`manager.go`, `async_queue.go`): L1 structures (`MsgPack` Object Cache, `ZSET` MOST/SPEED Cache, `WorkerQueue` / `EnqueueDB`).
  - `mongo/`: L2 warm document storage (`$in` queries, `last_use` sliding TTL).
  - `postgres/`: L3 relational source of truth via SQL functions defined in `schema.sql`.
- **`internal/worker/`:**
  - Consumes `WorkerQueue` events, enforces topological ordering (`flushPostgres`), executes bulk `pq.CopyIn` / Mongo `BulkWrite`, and runs periodic decay and orphan GC jobs.

## 2. Read Hydration Pipeline (L1 -> L2 -> L3)
1. Query **L1 (Redis)** in batch (`MGET` / `object_cache_service`).
2. On *Cache Miss*, fetch missing IDs from **L2 (MongoDB)** (`$in`).
3. As a last resort, query **L3 (PostgreSQL)**.
4. **Self-Healing Promotion:** Immediately re-insert (promote) entities fetched from L2 or L3 back into L1.
5. Never use `EnqueueDB` for read operations.

## 3. Write-Behind & Real-Time CQRS
- High-frequency mutations update L1 Redis synchronously and push an `AsyncEvent` via `redis.EnqueueDB` for deferred L3/L2 persistence.
- REST endpoints handle commands with minimal payloads; real-time state updates broadcast over WebSocket via `realtime_service` (Redis Pub/Sub), excluding the originating emitter to prevent UI duplicates.
- When a required repository or cache abstraction is missing, add it to the proper layer rather than bypassing the architecture.
