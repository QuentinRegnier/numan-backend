# ADR 0003 — Write-Behind Persistence via Topological Sort and `pq.CopyIn`

- **Status:** Accepted (implemented in `internal/worker/postgres_batch.go`)

## Context
Synchronous row-by-row PostgreSQL writes degrade HTTP latency under high social interaction volume (likes, messages, views, posts).

## Decision
- Absorb writes in Redis (`REQUEST Cache` / `WorkerQueue`) and return immediately to the HTTP client.
- Drain queued events in background workers inside `internal/worker/postgres_batch.go`, sort entity batches according to a strict topological `executionOrder`, and execute bulk inserts via `pq.CopyIn` with dichotomous fallback to the `postgres_errors` DLQ on failure.

## Consequences
- Sub-5ms HTTP write latency and dramatically lower PostgreSQL disk IOPS.
- Any new relational entity must be added to `executionOrder` at the proper foreign-key dependency rank.
