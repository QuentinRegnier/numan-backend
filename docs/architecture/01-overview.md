# Architecture Overview — `numan-backend`

`numan-backend` is a Go 1.25 backend designed as a **low-latency asynchronous data router** rather than a synchronous CRUD monolith backed directly by PostgreSQL.

## Core Data Flow

```
Client / Mobile
   │
   ▼  HTTP / WebSocket (JWT + HMAC + Ratchet)
Traefik Ingress ──► Gin API (cmd/main.go, port 8080)
                       │
                       ├─► Read: L1 Redis (MsgPack/ZSET)
                       │     └─ miss ─► L2 MongoDB ($in)
                       │                   └─ miss ─► L3 PostgreSQL 16 (+ L1 promotion)
                       │
                       └─► Write: L1 Redis (immediate) + EnqueueDB (FIFO)
                                                             │
                                                             ▼
                                                Workers (internal/worker/)
                                                  ├─ Batching + Topological Sort
                                                  └─ pq.CopyIn (PostgreSQL) / Bulk (Mongo)
```

## Runtime Roles (`SERVER_ROLE`)
- `monolith` or `worker_node`: serves the API and starts `worker.StartBackgroundWorkers(ctx)`.
- `API_NODE`: runs HTTP/WebSocket handlers only, with background workers disabled.

## Binary Entrypoints
- `cmd/main.go`: Main HTTP/WebSocket API + WebSocket Hub + Workers.
- `cmd/watermark/main.go`: Zero-disk serverless microservice for AVIF DCT steganography watermarking.
- `cmd/admin/main.go`: Forensic inspection CLI for watermarked media (`admin/service`).
