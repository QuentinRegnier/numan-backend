# ADR 0002 — Binary MsgPack Serialization in Redis L1 (`volatile-lfu`)

- **Status:** Accepted (implemented in `internal/repository/redis/` and `object_cache_service/`)

## Context
The L1 Object Cache holds full domain entities (Users, Posts, Comments, Messages) in Redis RAM under a `volatile-lfu` eviction policy. Raw JSON incurs high RAM overhead and CPU cost during burst `MGET` hydration.

## Decision
Use `github.com/vmihailenco/msgpack/v5` to serialize and deserialize payloads cached in Redis L1.

## Consequences
- Significant reduction in Redis memory footprint and faster server-side serialization/deserialization.
- Incompatible struct tag or field type changes on `*Payload` models must account for existing cached objects in Redis during rollouts.
