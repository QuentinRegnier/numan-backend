# Three-Tier Data Ecosystem (L1 / L2 / L3)

## Tier 1 (L1) — Redis 7 (RAM, `volatile-lfu`)
- **Object Cache (`internal/service/cache_service/object_cache_service/`):** Full entities serialized in **MsgPack** (`vmihailenco/msgpack/v5`) under `volatile-lfu` eviction and elastic TTLs.
- **MOST Cache:** `ZSET` structures for trending content, hashtag search, and ranked feeds.
- **SPEED Cache:** Lightweight projections (users, followers, communities, conversations, messages, watermarks).
- **Session & Presence Cache:** JWT/Ratchet session secrets, 2FA state, and real-time presence.
- **REQUEST Cache / WorkerQueue:** FIFO queues absorbing writes before background persistence.

## Tier 2 (L2) — MongoDB 7 (Warm Storage)
- Stores denormalized documents (with a 30-day sliding TTL based on `last_use`) to absorb Redis L1 *Cache Misses* via batched `$in` queries without hitting PostgreSQL.
- Indexes are ensured on startup via `mongo.EnsureIndexes` in `cmd/main.go`.

## Tier 3 (L3) — PostgreSQL 16 (ACID Source of Truth)
- Schema defined in `schema.sql` and partitioned into logical schemas:
  - `auth`: users, sessions, passkeys, social graph relations.
  - `content`: posts, comments, media, likes, saved items, tags.
  - `messaging`: conversations, members, messages, reactions.
  - `moderation`: reports, quarantine, blame records.
  - `views` & `util`: read projections and time helpers (`util.now_ms()`, `util.ms_to_ts()`).

## Hydration & Self-Healing Pipeline
1. Batch L1 Redis lookup (`MGET` / `GetObject`).
2. L2 MongoDB fallback for missed IDs only.
3. L3 PostgreSQL fallback as a last resort.
4. Immediate promotion (re-insertion) into L1 to warm the cache.
5. On startup, if `RedisKeyStrictRecent` is empty (`ZCard == 0`), `cmd/main.go` triggers conditional cache seeding (`SeedMostCache`, `SeedGraphCache`, `SeedSpeedCache`, `SeedUserCache`).
