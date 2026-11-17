---
name: cache-pipeline-verifier
description: Audit and verify L1 (Redis MsgPack) -> L2 (MongoDB) -> L3 (PostgreSQL) hydration and asynchronous Write-Behind integrity when modifying entities, caches, or workers.
---

# Skill: `cache-pipeline-verifier`

## When to Trigger
- Modifying services in `internal/service/cache_service/` or `internal/service/cache_service/object_cache_service/`.
- Adding or modifying Redis keys in `internal/variables/`.
- Modifying MsgPack-serialized payloads (`internal/domain/models/*Payload.go`).
- Modifying asynchronous write flows (`redis.EnqueueDB` / `internal/worker/`).

## Mandatory Verification Checklist

### 1. MsgPack Compatibility (L1 Object Cache)
- Check struct tags (`msgpack:"..."` / `json:"..."`) on payloads stored via `redis.<Entity>.SetObject(...)`.
- Ensure field edits on `*Payload` structs do not break decoding of existing L1 Redis entries or tests in `internal/service/cache_service/object_cache_service/*_test.go`.

### 2. L1 -> L2 -> L3 Hydration & Self-Healing Promotion
- For batch reads, verify the exact cascade:
  1. L1 Redis (`MGET` / `GetObject`).
  2. L2 MongoDB (`$in`) only for missed IDs.
  3. L3 PostgreSQL only for remaining missed IDs.
  4. Immediate promotion (re-insertion) into L1 for entities recovered from L2/L3.

### 3. Write-Behind Integrity
- Verify emitted `AsyncEvent` payloads (`Type`, `Action`) are handled in `internal/worker/postgres_batch.go` and `internal/worker/mongo_batch.go`.
- If introducing a new relational entity, place it at the correct dependency rank in `executionOrder` inside `flushPostgres` to prevent foreign-key violations during `pq.CopyIn`.

### 4. Test Verification
```bash
go test -v ./internal/service/cache_service/... ./internal/worker/...
```

## References
- `.agents/rules/01-architecture-l123.md`
- `docs/architecture/02-tiers-l1-l2-l3.md`
- `docs/architecture/03-write-behind-workers.md`
