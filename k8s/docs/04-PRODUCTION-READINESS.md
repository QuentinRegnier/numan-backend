# Part H — Production readiness assessment

**Verdict: not production-ready yet. It is a reviewed, statically validated baseline.** Nothing has been applied to a cluster, the application images and Dockerfile
were not available, and several decisions below belong to you.

## 1. Completed (evidence in brackets)

* Templates render deterministically from `infrastructure.env.template` (84 variables, T == E enforced), the rendered output builds with Kustomize and passes strict schema validation against Kubernetes 1.35 + Traefik/cert-manager CRD schemas, and 18 invalid inputs are rejected with the right error [`make test`: 38 checks].
* Cross-reference consistency (Secret/ConfigMap keys, Services↔pods↔ports, Ingress backends, ServiceAccounts, HPA targets, PDBs, NetworkPolicy labels, requests+limits on every container) [`scripts/check-refs.py`; negative-tested].
* Shell scripts pass ShellCheck (warning level).
* Legacy behaviours mapped with evidence: routing, HSTS, redirect, 20 MiB limit, WebSocket upgrade, NODE_ID, env-var names preserved ([00-AUDIT](00-AUDIT.md), [02-MIGRATION-MAP](02-MIGRATION-MAP.md)).
* Secrets separated per consumer; no secret value in any manifest; guard against hard-coded secrets and `CLEAN_DB_ON_STARTUP=true`.
* Default-deny NetworkPolicies, non-root/drop-all/seccomp/read-only-FS settings, PSA, quotas, PDBs, HPAs.
* Idempotent MinIO bootstrap (hot/cold), backup CronJobs to off-site, disabled-by-default admin tools, deploy/verify scripts.

## 2. Not yet proven (needs a real cluster — run `scripts/verify.sh`)

* Pods start under the configured UIDs/read-only filesystems; probes pass.
* NetworkPolicies are enforced by your CNI and do not block anything the app needs (outbound dependencies are unknown; only TCP/443 egress is allowed for api/worker).
* Traefik routes as designed on your K3s/Traefik chart version (the bundled chart changes between K3s releases; `HelmChartConfig` keys must be checked).
* Media streaming through Traefik → API → `minio-hot` (first-byte latency, large files, slow clients; Traefik `readTimeout` defaults to 60 s) and idle WebSocket behaviour.
* Backups succeed against your off-site target and **restore** (no restore has been run).
* All third-party image tags exist and are the newest patches (`validate.sh --check-images`).

## 3. Remaining manual tasks

1. Build and push application images; create the watermark image; replace image placeholders.
2. Replace domain/e-mail placeholders; create DNS; open 80/443.
3. Choose and apply the **MinIO image strategy** (README).
4. Create `secrets.env` and the Secrets; enable K3s secrets encryption; rotate the weak watermark key.
5. Decide the storage class and PVC sizes from real data volumes.
6. Verify the Go behaviours listed in §6; adjust probes (`/health`), UIDs, worker enablement, replica caps.
7. Provision the off-site backup bucket (versioned/locked) and run a restore drill.
8. Migrate data and plan the cut-over/rollback window (README §7).
9. Choose observability tooling and alert routing (docs/03).

## 4. Security limitations

* Secrets are base64 in etcd; protection depends on K3s encryption-at-rest and RBAC (operator-enabled).
* Database/Redis traffic is unencrypted inside the cluster (legacy `sslmode=disable`); protected by NetworkPolicy only.
* The app uses MongoDB **root** credentials (no app-user variable exists in the legacy configuration).
* Hot bucket anonymous read is now **off** by default (`MINIO_HOT_ANONYMOUS_READ=false`); if you turn it on, any pod allowed by NetworkPolicy can read objects unauthenticated.
* `/process` is publicly routed (as in legacy); its protection is the application's check of `WATERMARK_SECRET_KEY` (unverified).
* The pinned MinIO images pre-date the fix for CVE-2025-62506; the project is archived (README).
* `includeSubDomains` in HSTS affects every subdomain for a year.
* `/` and `/ws` have no 20 MiB limit at the edge (only `/process` does, to avoid buffering media responses); the application's own limit applies.
* No Kubernetes API audit logging, image signing, admission policy, or runtime detection.
* Admin UIs hold datastore credentials while enabled; access = whoever has `kubectl port-forward` rights.

## 5. Availability limitations

* PostgreSQL, MongoDB, Redis, MinIO hot/cold are **single instances** on node-local storage: a node or disk failure means downtime until the node returns or a restore completes; data loss up to the last backup (provisional RPO 24 h).
* API/watermark survive pod loss; survive node loss only with ≥ 2 schedulable nodes (spread constraints are best-effort).
* Ingress has one DNS target per node unless you provide a floating IP/VIP/LB; round-robin DNS is not failover.
* Control plane HA requires 3 server nodes and an API endpoint that survives a node loss; with 1–2 servers it is not highly available.
* API replicas are capped (HPA max 6) by the unverified NODE_ID range; scaling pods does not scale the databases.
* Rolling updates disconnect WebSocket clients.
* Redis cannot be made durable beyond AOF-everysec by this design.

## 6. Decisions requiring operator confirmation

| # | Decision / fact to confirm | Default shipped | Consequence if wrong |
|---|---|---|---|
| 1 | ~~Does the Go code emit `X-Accel-Redirect`?~~ **Resolved:** no (confirmed by the project owner); the nginx hop was removed | direct routing | – |
| 2 | NODE_ID semantics/maximum (ID generator?) | unique per pod, 1–6 API, 11+ workers | duplicate IDs / startup failure |
| 3 | Is `./numan-worker` in the image; meaning of `SERVER_ROLE`; job idempotency | worker disabled | duplicate processing if enabled prematurely |
| 4 | Does the client send `REDIS_PASSWORD`? | requirepass ON | Redis auth errors at start |
| 5 | Is there a `/health` endpoint? | TCP probes | weaker readiness |
| 6 | Does the app need Mongo transactions/change streams? | standalone | requires replica set |
| 7 | Anonymous read on the hot bucket needed? (`mc anonymous get` on legacy) | `false` (`MINIO_HOT_ANONYMOUS_READ`) | an unsigned reader would get 403; the bootstrap Job also REMOVES an existing anonymous policy |
| 8 | Cold storage: variable names, who moves objects, retention, Object Lock, legal basis | cold MinIO deployed, unwired, no retention | compliance gap; **Object Lock can't be added later** |
| 9 | Does the app read `MINIO_ROOT_*` or need broader S3 rights (e.g. bucket creation, ILM)? | least-privilege bucket user | AccessDenied at runtime |
| 10 | Outbound dependencies (push, mail, SMTP ports, CDN) | TCP/443 only | blocked features |
| 11 | Does the watermark service need DB/S3/Redis? | only its secret + PORT | CrashLoop; add single variables + policy |
| 12 | Watermark `/process` public or internal-only? | public (legacy) | exposure |
| 13 | MinIO image strategy (A/B/C/D) | unpatched Quay image | known CVE class exposure |
| 14 | Storage class, PVC sizes, node count, hardware | `local-path`, placeholders | capacity/availability |
| 15 | RPO/RTO, backup target, retention periods | 24 h / 4 h provisional | unmet business needs |
| 16 | HSTS `includeSubDomains` | on (legacy) | subdomain lock-in |
| 17 | WebSocket idle behaviour (legacy 60 s nginx cap) | no cap | zombie connections if app has no ping/deadline |
| 18 | DB TLS, Mongo app user, Postgres HA (CloudNativePG), Redis HA | not implemented | see limitations |
