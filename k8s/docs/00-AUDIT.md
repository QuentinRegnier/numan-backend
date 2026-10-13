# Part A — Legacy infrastructure audit

Sources: `docker-compose.dev.yml`, `docker-compose.infra.yml`, `docker-compose.prod.yml`, `Makefile`, `nginx.conf`,
and the `.env` supplied inline. **Not supplied:** `Dockerfile`, the Go source, `./certs`, any backup script.
No secret value is reproduced here; variables are referred to by name.

## 1. Service inventory (per file)

| Service | dev | infra (profiles) | prod | Image (legacy) | Ports | Volumes | Notes |
|---|---|---|---|---|---|---|---|
| nginx | ✔ | – | ✔ | `nginx:latest` | 80, 443 published | `nginx.conf`, `./certs` (ro) | only in dev/prod |
| api1 / api2 | ✔ (target `dev`, Air, source bind-mount) | – | ✔ (target `prod`) | built from `Dockerfile` | 8080 (internal) | dev: `./:/app`, `/app/tmp`, `./docs` | `NODE_ID=1` / `=2`; `env_file .env` |
| api | – | ✔ `api, ws, all` | – | built, target `prod` | none | – | single instance, no NODE_ID |
| worker | – | ✔ `worker, all` | – | built, target `prod` | – | – | `command: ["./numan-worker"]` ("only StartBackgroundWorkers") |
| watermark | ✔ (`go run cmd/watermark/main.go`) | – | **absent** | built, target `dev` | `8083:8080` (dev) | `./:/app` | nginx routes `/process` to it |
| postgres | ✔ | ✔ `db-master, all` | ✔ | `postgres:16` | 5432 published in dev/infra | named volume `postgres_data` | |
| mongo | ✔ | ✔ | ✔ | `mongo:7` | 27017 published in dev/infra | dev: bind `./mongo-data`; infra/prod: `mongo_data` | |
| redis | ✔ | ✔ `redis-node, all` | ✔ | `redis:7` | 6379 published in dev/infra | dev/prod: `redis_data`; infra: none | |
| minio | ✔ | ✔ `storage, all` | ✔ | `minio/minio` (unpinned) | dev: 9001 published, 9000 expose; infra: 9000+9001 published; prod: 9000 expose | `minio_data` (infra: none) | |
| createbuckets | ✔ | – | **absent** | `minio/mc` (unpinned) | – | – | creates bucket + `mc policy set download` |
| pgadmin | ✔ (8082) | – | ✔ (expose 80) | `dpage/pgadmin4` (unpinned) | | `pgadmin_data` (dev) | prod credentials from `.env`; dev has hard-coded trivial defaults |
| mongo-express | ✔ (8083) | – | ✔ (expose 8081) | `mongo-express:latest` | | | |
| redis-commander | ✔ (8081) | – | ✔ (expose 8081) | `ghcr.io/joeferner/redis-commander:latest` | | | |

Networks: prod declares `internal` and attaches every service to it. dev declares `internal` but attaches **only** `watermark`
to it (see finding 9). infra declares it and attaches nothing.
Health checks: **none** in any file. Restart policy: `restart: always` on every prod service; in dev it is missing on mongo, the three admin UIs and `createbuckets`.
Backups/recovery: **none** in any file. Consul, manual service registration, custom DNS: **not present** in any supplied file
(Compose DNS names are the only discovery mechanism), so there is nothing to remove beyond `NODE_ID` handling.

## 2. nginx behaviour that must be preserved or consciously changed

| Legacy rule | Kubernetes equivalent / decision |
|---|---|
| `:80` → `301 https://$host$request_uri` | Ingress on entrypoint `web` + `redirectScheme` middleware |
| TLS with certs from `./certs` (`server_name localhost`) | Traefik + cert-manager (domain is a placeholder) |
| `add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always` | Traefik `headers` middleware (same values) |
| `client_max_body_size 20M` (comment: app enforces 15 MB with MaxBytesReader) | `buffering` middleware on `/process` only; `/` and `/ws` rely on the application's own limit (see docs/06-TEMPLATING.md §6 and the middleware comment) |
| `/` → upstream `api1:8080, api2:8080`, forwards Host, X-Real-IP, X-Forwarded-For, X-Forwarded-Proto | Traefik routes `/` straight to Service `api` (Traefik sets Host, X-Forwarded-*, X-Real-Ip) |
| `/ws` → upstream, HTTP/1.1, `Upgrade`/`Connection: Upgrade`, **no X-Real-IP/X-Forwarded-\*, no timeouts** | Traefik routes `/ws` straight to Service `api` (native WebSocket support) |
| `/internal_minio/` `internal;` + `rewrite` + `proxy_pass http://minio:9000` + `Host: minio:9000` + hides `x-amz-*` | **No equivalent needed**: the application streams media itself (finding 1) |
| `/process` → `watermark:8080` | Traefik routes straight to Service `watermark` |
| default nginx timeouts (`proxy_read_timeout` 60 s) | Traefik has no equivalent default for hijacked WebSocket connections (finding 11) |

## 3. Findings (ordered by impact)

**1. `/internal_minio/` looked like X-Accel-Redirect: RESOLVED, not used.** `location /internal_minio/ { internal; … }` is the textbook X-Accel-Redirect target,
and the first revision of this repository kept an nginx hop because Traefik cannot process that header. The project owner has since confirmed that the Go
application streams media binaries itself from memory, so the hop was removed and Traefik routes `/` directly to the API (docs/CHANGELOG.md). Side effect: the
reason for anonymous read on the hot bucket disappeared, so `MINIO_HOT_ANONYMOUS_READ` now defaults to `false` (finding 7).

**2. `NODE_ID` differs per API instance (critical to preserve).** `api1`→1, `api2`→2, and `.env` labels the variable "identity of the
server (for horizontal scaling)". It plausibly seeds a distributed ID generator; duplicates could corrupt data silently. Pods are therefore
a StatefulSet whose ordinal gives the NODE_ID (`ordinals.start: 1` → 1, 2, 3…). Not assumed obsolete.

**3. Worker topology is ambiguous.** The production Compose has no worker service and both API containers run with `SERVER_ROLE=monolith`
(default in `.env`), so background work already runs inside both API processes. Only `docker-compose.infra.yml` defines `./numan-worker`.
Whether that binary exists in the prod image, and what `api_node`/`worker_node` do, needs the Dockerfile/source. The worker workload ships
**disabled** (replicas 0). Enabling it next to monolith APIs risks duplicate processing.

**4. Watermark is missing from prod and infra.** nginx routes `/process` to it and `.env` has `WATERMARK_API_URL`, but only the dev Compose
runs it (via `go run`). A production image/entrypoint must exist; none is evidenced. No persistent state is evidenced (source bind-mount only).

**5. Redis authentication mismatch.** `.env` defines `REDIS_PASSWORD`, but the Compose command has no `--requirepass`, and the dev/infra files
publish 6379 on the host. The infra file starts Redis with `redis-server /usr/local/etc/redis/redis.conf` but mounts no such file (it would not start).
Redis is also described as "Hot Storage & Message Broker", has `--appendonly yes`, and uses `volatile-lfu` (only TTL keys are evicted): treat it as stateful.

**6. MongoDB.** The infra file passes the variables only via `env_file`, but the image expects `MONGO_INITDB_ROOT_*` while `.env` defines
`MONGO_ROOT_*`, so infra Mongo would run **without authentication**. dev/prod map them explicitly. The application is given the **root** credentials. Standalone, not a replica set, everywhere.

**7. MinIO and buckets.** (a) `createbuckets` (dev only) runs `mc policy set download` — anonymous read — and ends with `exit 0`, hiding failures; that
`mc policy` sub-command is deprecated, so whether the policy ever applied in production is unknowable from the files. (b) prod has no bucket creation at all.
(c) The app uses the MinIO **root** credentials as S3 keys (`S3_ACCESS_KEY=${MINIO_ROOT_USER}`). (d) There is a single endpoint/bucket variable: **no cold storage,
no retention, no 365-day or LCEN setting exists in any supplied file** — those requirements come from the migration brief, and the app wiring for a cold bucket is unknown.
(e) Upstream status: community MinIO is archived/source-only (see README).

**8. Makefile does not do what its comments say.** `COMPOSE_PROD` points to `docker-compose.prod.yml`, which defines **no profiles**; services without profiles
always start, so `make run-db`, `run-api`, `run-worker`, `run-redis` each start the **entire** stack. The profile-aware file (`infra.yml`) is not referenced by any target.
`make clean` runs `docker system prune -a --volumes` (destructive). `docs` regenerates Swagger (`swag init -g cmd/main.go`), so the image build depends on generated `docs/`.

**9. dev Compose inconsistencies.** Only `watermark` is on the `internal` network; everything else is on the default network, so by name `watermark` cannot reach
`postgres/redis/minio` and the API cannot resolve `watermark` (it may not need to — unverified). Host port **8083** is bound twice (watermark `8083:8080` and mongo-express `8083:8081`).
`mongo_data` is declared but unused in dev (bind mount `./mongo-data`). Admin UIs are published with trivial default credentials in dev.

**10. Configuration hazards in `.env`.** `CLEAN_DB_ON_STARTUP` (must stay `false`; with orchestrator restarts a `true` is destructive). `DB_DSN` uses `sslmode=disable` and is a
space-separated `key=value` string built by Compose interpolation (Kubernetes cannot interpolate across Secret keys; rebuilt with `$(VAR)` expansion instead). Several values are
placeholders; the inter-service `WATERMARK_SECRET_KEY` looks human-readable/guessable — rotate. `REDIS_CONTAINER_LIMIT` is not used by any Compose file. Anything real that was pasted into
chats/tickets must be rotated.

**11. WebSocket timing.** The nginx `/ws` block sets no timeouts, so idle sockets are closed after nginx's default 60 s unless the app pings more often. Traefik does not impose that.
If the app relied on nginx to reap idle sockets, add application-level ping/deadline handling or a Traefik/entrypoint timeout. The `/ws` block also omits `X-Real-IP`/`X-Forwarded-*`;
Traefik adds them (benign, but the app may now see a client IP it previously did not).

**12. Images.** `nginx:latest`, `mongo-express:latest`, `redis-commander:latest`, unpinned `minio/*` and `dpage/pgadmin4`; databases pinned only to a major.

**13. Dev vs prod differences.** dev: Air hot reload via bind-mounts, target `dev`, DB/admin ports published, MinIO console published, pgAdmin in desktop mode with long sessions, bucket creation present.
prod: compiled `prod` target, nothing published except nginx 80/443, admin UIs credentialed from `.env` but not routed by nginx (unreachable), no bucket creation, no watermark.

## 4. Missing information and what depends on it

| Missing | Decisions that depend on it |
|---|---|
| Dockerfile (targets, USER, WORKDIR, binaries incl. `numan-worker`, watermark build) | UIDs/read-only filesystem, worker command, watermark image |
| Go source facts: NODE_ID semantics & max; `/health`; REDIS_PASSWORD use; SERVER_ROLE semantics; WebSocket ping/deadline; outbound dependencies (push, mail…); cold-storage variables; whether Mongo transactions/change streams are used | replica caps, probes, Redis auth, worker enablement, NetworkPolicy egress, cold wiring, Mongo topology |
| Cluster facts: node count/CPU/RAM/disks (SSD/HDD), network/CNI, public IPs | all sizing, storage class, HA claims, Traefik exposure |
| Domain, DNS provider, registry, off-site backup bucket | placeholders in ingress/TLS/images/backups |
| Current data volumes and growth | PVC sizes, backup windows |
| Legal/retention requirements (LCEN etc.), business RPO/RTO | cold storage policy, backup retention, Object Lock |
| State of the legacy bucket policy (`mc anonymous get`) | `HOT_ANONYMOUS_READ` |
