# Part C — Migration mapping

> Deployment-time variables (domain, images, storage, sizes, schedules, …) are a separate layer, documented in [06-TEMPLATING.md](06-TEMPLATING.md) and [05-VARIABLES.md](05-VARIABLES.md). This document covers the legacy → Kubernetes mapping and the **application runtime** variables.

## 1. Legacy component → Kubernetes resource

| Legacy | Kubernetes resource(s) | File | Key changes |
|---|---|---|---|
| `nginx` (TLS, redirect, HSTS, routing, 20M) | Traefik (bundled) + Ingresses + Middlewares + cert-manager `Certificate` | `06-ingress/*` | TLS moves to Traefik; 80→443 and HSTS via middleware; body limit via `buffering` |
| nginx `/internal_minio/` | *(none)* | – | not migrated: the application streams media itself (confirmed); no proxy hop remains |
| `api1`, `api2` | StatefulSet `api` (+ Services `api`, `api-headless`, PDB, HPA) | `05-apps/api.yaml`, `07-autoscaling/hpa.yaml` | `NODE_ID` from ordinal (1, 2, …); TCP probes; graceful termination; spread constraint |
| infra `worker` (`./numan-worker`) | StatefulSet `worker` (replicas 0) | `05-apps/worker.yaml` | disabled until verified; ordinals 11+ |
| `watermark` | Deployment + Service `watermark` + PDB + HPA | `05-apps/watermark.yaml` | needs its own prod image; only its own secret |
| `postgres` | StatefulSet + Services `postgres`, `postgres-headless` + PVC template | `03-databases/postgresql.yaml` | `PGDATA` subdir; non-root; probes; retained PVCs |
| `mongo` | StatefulSet + Services `mongo`, `mongo-headless` | `03-databases/mongodb.yaml` | auth always on (infra file had none) |
| `redis` | StatefulSet + Services `redis`, `redis-headless` | `03-databases/redis.yaml` | `--requirepass` added; `maxmemory` from ConfigMap |
| `minio` | StatefulSets `minio-hot` and `minio-cold` + Services | `04-storage/*` | split in two; S3 port only; console by port-forward |
| `createbuckets` | Jobs `minio-hot-init`, `minio-cold-init` | `04-storage/bucket-init-jobs.yaml` | retries, real failures, least-privilege users |
| `pgadmin`, `mongo-express`, `redis-commander` | Deployments at replicas 0 + ClusterIP Services | `08-operations/admin-tools/` | off by default, port-forward only |
| `.env` (non-secret) | ConfigMaps `numan-config`, `minio-init-config` | `01-config/configmaps.yaml` | runtime values stay in the ConfigMap; deployment-specific ones are deployment variables (`infrastructure.env.template`) |
| `.env` (secret) | 13 Secrets via `create-secrets.sh` | `01-config/secret-templates.yaml` | split by consumer |
| `depends_on` | probes + init containers + Job retries | – | Compose ordering never guaranteed readiness |
| `restart: always` | Pod restart semantics + probes | – | |
| Docker networks | Namespace + NetworkPolicies | `02-security/networkpolicies.yaml` | default deny |
| Named volumes (`*_data`) | `volumeClaimTemplates` (storage class via kustomize) | – | data migrated by dump/restore, not by volume copy |
| Published DB/UI ports (dev) | nothing published | – | port-forward when needed |
| Makefile `dev/prod/run-*` | `scripts/deploy.sh`, `validate.sh`, `verify.sh`, `admin.sh` | `scripts/` | profile-based role split replaced by separate workloads |
| (none) | backups | `08-operations/backup-jobs.yaml` | new |
| (none) | quotas, PDBs, HPAs, PSA | `02-security`, `05-apps`, `07-autoscaling` | new |
| Consul / custom DNS | CoreDNS + Services | – | not present in legacy files |

## 2. Environment variables

Legend — **Class**: S = sensitive (Secret), N = non-sensitive (ConfigMap/literal). **Status**: ✔ confirmed from the files; ⚠ requires verification against the Go code.
Sources: `.env` unless stated. "api" = StatefulSet `api` (and `worker`, which receives the same set).

| Variable | Source | Destination | Class | Consumed by | Status |
|---|---|---|---|---|---|
| `NODE_ID` | `.env`, compose `environment` | pod ordinal via downward API (label `apps.kubernetes.io/pod-index`) | N | api, worker | ✔ set per instance in legacy; ⚠ semantics and maximum |
| `SERVER_ROLE` | `.env` | ConfigMap `numan-config` (`monolith`); worker overrides to `worker_node` | N | api, worker | ✔ value; ⚠ meaning of roles |
| `PORT` | `.env` | ConfigMap `numan-config` (`8080`) | N | api, watermark | ✔ (nginx upstream :8080) |
| `CLEAN_DB_ON_STARTUP` | `.env` | ConfigMap, pinned `false`, guarded by `validate.sh` | N | api | ✔ |
| `JWT_SECRET` | `.env` | Secret `numan-app-secrets` | S | api, worker | ✔ name; value = operator (reuse existing) |
| `HMAC_SECRET` | `.env` | Secret `numan-app-secrets` | S | api, worker | ✔ name; value = operator |
| `WATERMARK_SECRET_KEY` | `.env` | Secrets `numan-app-secrets` and `numan-watermark-secrets` | S | api, worker, watermark | ✔ name; rotate (weak legacy value) |
| `WORKER_MAX_BATCH_SIZE` / `WORKER_MIN_BACKOFF_MS` / `WORKER_MAX_BACKOFF_MS` | `.env` | ConfigMap `numan-config` | N | api, worker | ✔ values |
| `POSTGRES_HOST`, `POSTGRES_PORT` | `.env` | ConfigMap (`postgres`, `5432`) | N | api, worker | ✔ (Service named `postgres`) |
| `POSTGRES_DB` | `.env` | ConfigMap (`numan`) | N | api, worker, postgres, backup | ✔ |
| `POSTGRES_USER`, `POSTGRES_PASSWORD` | `.env` | Secret `postgres-credentials` | S | api, worker, postgres, backup-postgres | ✔ names; values = operator |
| `DB_DSN` | `.env` (Compose-interpolated) | built in pod spec with `$(POSTGRES_*)` | S (contains password; never stored as a literal) | api, worker | ✔ format; ⚠ `sslmode=disable` kept |
| `MONGO_HOST`, `MONGO_PORT`, `MONGO_DB` | `.env` | ConfigMap (`mongo`, `27017`, `numan`) | N | api, worker | ✔ |
| `MONGO_ROOT_USER`, `MONGO_ROOT_PASSWORD` | `.env` | Secret `mongo-credentials` | S | api, worker, mongo (as `MONGO_INITDB_ROOT_*`), backup-mongo, mongo-express | ✔ names; ⚠ app really uses root |
| `REDIS_ADDR` | `.env` | ConfigMap (`redis:6379`) | N | api, worker | ✔ |
| `REDIS_PASSWORD` | `.env` | Secret `redis-credentials`; Redis `--requirepass` | S | api, worker, redis, backup-redis, redis-commander | ✔ name; ⚠ app sends AUTH (legacy server had none) |
| `REDIS_MAXMEMORY` | `.env` | ConfigMap; also used by the redis container (`--maxmemory`) | N | api(if read), redis | ✔ |
| `REDIS_CONTAINER_LIMIT` | `.env` | ConfigMap (informational); real limit `4608Mi` in `redis.yaml` | N | api (if read) | ⚠ unused in Compose |
| `MINIO_ENDPOINT` | `.env` (`minio:9000`) | ConfigMap → **`minio-hot:9000`** | N | api, worker | ✔ name; value changed on purpose |
| `MINIO_BUCKET_NAME` | `.env` | ConfigMap `numan-config` (value from deployment variable `MINIO_HOT_BUCKET`, shared with the bootstrap Job) | N | api, worker, init Job, backup | ✔ |
| `MINIO_ROOT_USER`, `MINIO_ROOT_PASSWORD` | `.env` | Secrets `minio-hot-root` / `minio-cold-root` (different values) | S | minio-*, init Jobs only | ✔ names; ⚠ app might read them |
| `S3_ACCESS_KEY`, `S3_SECRET_KEY` | `.env` (`=${MINIO_ROOT_*}`) | Secret `minio-hot-users` (**least-privilege user**, created by init Job) | S | api, worker | ✔ names; value deliberately not root |
| `WATERMARK_API_URL` | `.env` | ConfigMap (`http://watermark:8080`) | N | api, worker | ✔ |
| `PGADMIN_EMAIL` / `PGADMIN_PASSWORD` | `.env` | Secret `pgadmin-credentials` as `PGADMIN_DEFAULT_EMAIL/PASSWORD` | S | pgadmin | ✔ mapping from compose |
| `MONGO_UI_USER` / `MONGO_UI_PASSWORD` | `.env` | Secret `mongo-express-credentials` as `ME_CONFIG_BASICAUTH_*` | S | mongo-express | ✔ |
| `REDIS_UI_USER` / `REDIS_UI_PASSWORD` | `.env` | Secret `redis-commander-credentials` as `HTTP_USER/HTTP_PASSWORD` | S | redis-commander | ✔ |
| `ME_CONFIG_MONGODB_SERVER` | compose | literal `mongo` | N | mongo-express | ✔ |
| `REDIS_HOSTS` (compose `local:redis:6379`) | compose | replaced by `REDIS_HOST/PORT/PASSWORD` | S (password) | redis-commander | ⚠ tool variables, not app variables |
| `MINIO_UPDATE`, `HOME`, `PGDATA`, `REDISCLI_AUTH`, `PGADMIN_LISTEN_PORT`, `MC_CONFIG_DIR` | new, infra-only | pod specs | N/S | respective tool containers | n/a (not read by the Go app) |
| *(proposed, absent in legacy)* cold storage endpoint/keys/bucket | – | commented placeholder in `api.yaml`; ConfigMap `minio-init-config.COLD_BUCKET` | – | api, worker | ⚠ variable names unknown |

Removed: none. Every legacy variable keeps its name. The legacy values of `MINIO_ENDPOINT`, `S3_*` and the Redis server auth behaviour are the only intentional value changes.
`SERVER_ROLE` and `NODE_ID` are **not** treated as obsolete.
