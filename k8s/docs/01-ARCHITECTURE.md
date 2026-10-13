# Part B — Target architecture

Labels used below: **[Implemented]** in the manifests · **[Operator input]** needs a value/decision from you ·
**[Future]** recommended improvement, not implemented · **[Not guaranteed]** cannot be ensured by manifests alone.

## 1. Overview

```
Internet ─80/443─► Traefik (K3s, kube-system, TLS via cert-manager) ───────────────┐
                    ├─ /ws        ───────────────► Service api  (StatefulSet, NODE_ID=ordinal)
                    ├─ /process   (+413 @20MiB) ──► Service watermark (Deployment)
                    └─ /          ───────────────► Service api  (direct; the app streams media itself)
 api / worker ─► postgres · mongo · redis · minio-hot · minio-cold         (NetworkPolicy allow-list, default deny)
 backups: CronJobs ─► restic / mc mirror ─► off-site S3 (outside the cluster)
```

Namespace `numan` (Pod Security: baseline enforced, restricted audited). Discovery is plain Kubernetes DNS; service names equal the
legacy hostnames (`postgres`, `mongo`, `redis`, `watermark`) so legacy variable values keep working. FQDN form:
`<svc>.numan.svc.cluster.local`. Consul/custom registration is not present in the legacy files and is not needed.

### 1.1 Categories

| # | Category | Implemented | Needs operator input | Future |
|---|---|---|---|---|
| 1 | Configuration & secrets | ConfigMaps (non-sensitive), 13 Secret objects created by script, per-service separation | secret values, K3s `--secrets-encryption` | SOPS/Sealed Secrets/ESO for GitOps |
| 2 | Databases & caches | Postgres, Mongo (standalone), Redis (AOF, requirepass) as single-instance StatefulSets | PVC sizes, storage class | CloudNativePG; Mongo replica set; Redis Sentinel |
| 3 | Object storage | `minio-hot`, `minio-cold`, bootstrap Jobs, least-privilege users | MinIO image decision, sizes, retention policy | replicated/distributed object storage |
| 4 | Stateless apps | api (StatefulSet), watermark, worker (disabled), HPA, PDB | images, verified probes/UIDs | KEDA for workers |
| 5 | Networking & ingress | Traefik Ingress, middlewares, redirect, HSTS, body limit | domain/DNS, 80/443 exposure, cert-manager | multi-IP/VIP for ingress |
| 6 | Security | ServiceAccounts, NetworkPolicies, securityContexts, quotas, PSA | image-compat validation | admission policy (Kyverno/Gatekeeper), image signing |
| 7 | Observability | kubectl-level checks, access logs option | choose log/metrics stack | Prometheus/Loki/Alertmanager (docs/03) |
| 8 | Backup & DR | restic/mirror CronJobs → off-site | target bucket, RPO/RTO sign-off, restore drills | WAL archiving / PITR |
| 9 | Ops tooling | validate/deploy/verify/admin scripts, disabled admin UIs | – | GitOps controller |

## 2. Key design decisions (and the evidence behind them)

1. **API/worker are StatefulSets** (not Deployments) to give each pod a unique `NODE_ID` without a wrapper script (audit finding 2).
   `ordinals.start: 1` reproduces the legacy ids; workers start at 11. Cost: slower rolling updates (one pod at a time).
2. **No proxy hop between Traefik and the API.** The legacy nginx `/internal_minio/` block looked like X-Accel-Redirect, but the application streams media itself (confirmed), so Traefik terminates TLS, redirects, sets HSTS and routes `/`, `/ws` and `/process` directly (docs/CHANGELOG.md).
3. **Single-instance datastores, honestly labelled.** No fake HA (see §3).
4. **Probes are TCP** for app pods because no health endpoint is evidenced; switch readiness to `/health` once confirmed.
5. **Redis auth turned on** although the legacy command had none (`REDIS_PASSWORD` exists in `.env`). **[Operator input]** confirm the client sends it.
6. **App gets least-privilege MinIO users**, not root keys. Env var names are preserved (`S3_ACCESS_KEY`, `S3_SECRET_KEY`); only the values differ.
   If the Go code also reads `MINIO_ROOT_USER/PASSWORD`, that will surface at first start and needs a decision.
7. **Watermark gets only its own secret** (not the whole `.env`).
8. **Worker disabled** until the Go behaviour is verified (duplicate-processing risk).
9. **cert-manager and CloudNativePG are not assumed**; cert-manager resources live in `06-ingress/tls`, applied after installing the operator.

## 3. Scaling, failure domains and high availability

"Horizontal scaling", "persistent storage" and "high availability" are different things. Summary for the topology shipped here:

| Component | Scaling mechanism | Failure domain | Shared-storage dependency | Node failure | Recovery | Remaining SPOF |
|---|---|---|---|---|---|---|
| API | HPA 2–6 (CPU); NODE_ID cap | pod / node (needs ≥2 nodes; spread is best-effort) | none | surviving replicas serve; WebSocket clients reconnect | rescheduled automatically | Postgres/Mongo/Redis/MinIO behind it |
| Watermark | HPA 2–6 | pod / node | none | as above | automatic | – |
| Traefik | 2 replicas **only if** the optional HelmChartConfig is applied | pod / node | none | surviving replica; **entry IP is still one DNS target per node** | automatic | DNS/VIP: round-robin DNS is not failover. Use a floating IP / kube-vip / external LB **[Operator input]** |
| PostgreSQL | none (1 replica) | the node holding the volume (`local-path`) | node-local disk | **down** until node returns or restore | restore from restic backup (RPO ≤24h provisional) | **Yes** |
| MongoDB | none (standalone) | same | same | **down** | restore | **Yes** |
| Redis | none | same | same | **down**; AOF replay on restart; ≤~1 s writes lost | restore/replay | **Yes** |
| MinIO hot/cold | none (single drive, no erasure coding) | same | same | **objects unavailable** | off-site mirror (daily) | **Yes** |
| Control plane | – | depends on server count | etcd | 1 server: cluster management lost; **2 servers: worse than 1 (no quorum on one loss)**; 3 servers tolerate 1 | etcd snapshots | **[Operator input]** topology |
| Backups | CronJobs | off-site target | – | – | – | off-site bucket and the restic password |

**Verdict: the application tier scales horizontally and tolerates pod (and, with ≥2 nodes, node) loss. The data tier is *not* highly available.**
No claim of HA for PostgreSQL, MongoDB, Redis or MinIO is made, and none should be made until the "Future" rows are implemented.

### 3.1 Storage (K3s `local-path`)
* **Locality.** A `local-path` PV is a directory on one node; its PV carries node affinity, so the pod can only run there.
* **Rescheduling limits.** If that node is down the pod stays `Pending`; the cluster will **not** move the data. Moving a datastore = restore on another node (delete PVC → recreate → restore).
* **No replication.** Disk loss = data loss. Mitigation: off-site backups (+ RAID/ZFS underneath is a hardware decision **[Operator input]**).
* **Capacity.** Defaults (50/50/20/100/100 Gi) are placeholders; plan: current size × growth over the retention of your backups + 30–50 % headroom; Redis volume ≥ 2.5 × `maxmemory`; Postgres needs space for WAL and vacuum.
* **Configurable class.** One patch in `kustomization.yaml`. `volumeClaimTemplates` are immutable → choose before the first deploy.
* **Future:** Longhorn/Rook-Ceph for replicated block storage (still not a substitute for backups), or CloudNativePG with instance-local storage and streaming replication.

### 3.2 PostgreSQL — path to HA
CloudNativePG (operator, separate install): 3 instances, streaming replication, automated failover, backups to object storage with WAL
archiving (PITR). Failure scenarios it covers: pod/node/disk loss of one instance. It does **not** cover loss of the whole site or of the backup bucket.
The client must reconnect after failover; connect via the operator's `-rw` Service. Not generated here because its CRDs would be undefined without the operator.

### 3.3 MongoDB
Standalone, as in the legacy stack. Consequences: no failover; no transactions/change streams (they require a replica set). A replica set needs `rs.initiate()`,
a shared keyFile, stable per-member DNS (the headless Service already exists), ≥3 voting members, and a driver connection string that lists the members — an application change.
Deploy it deliberately (operator such as the MongoDB Community Operator, or a hand-written StatefulSet with an init Job) after confirming the app needs it.

### 3.3a Redis
Single instance, AOF on (`appendfsync everysec` default). Failure domain: that pod/node. Evidence suggests queues/jobs live here, so persistence stays on and Redis is in the backup plan.
Sentinel (1 primary + ≥1 replica + 3 sentinels) or Cluster requires a client that supports it (the app uses a single `REDIS_ADDR`) — verify first.

## 4. Networking and ingress

* **Ports 80/443 reach the cluster** via K3s ServiceLB: the Traefik `LoadBalancer` Service binds 80/443 on node IPs. Point DNS (or a floating IP/LB) at those nodes. Firewall must allow them. **[Operator input]**
* **Routing parity:** see audit §2. No path rewriting is used (the legacy config has none). Longest-prefix selection matches nginx behaviour.
* **WebSockets:** Traefik upgrades them natively; no annotation required. Timeouts are **not changed** from the controller defaults. Per the Traefik v3 entrypoint documentation the defaults are `readTimeout` 60 s (reading the request, which also bounds a slow upload), `writeTimeout` 0 (no cap on streaming responses) and `idleTimeout` 180 s (keep-alive). Legacy nginx closed idle WebSockets after 60 s (default `proxy_read_timeout`). **How the bundled Traefik treats an idle upgraded connection was not verified here**: test with a real idle WebSocket (`verify.sh` cannot) and keep application-level ping/pong. If uploads over slow links must exceed 60 s, raise `readTimeout` in the Traefik static configuration deliberately. Rolling updates disconnect sockets by design (preStop sleep 10 s + 60 s grace).
* **Body limit:** 20 MiB on `/process` via the `buffering` middleware (Traefik buffers request **and** response bodies on that route, acceptable for watermark payloads). `/` and `/ws` are deliberately **not** buffered, so media streaming is untouched; consequence of removing the nginx hop: they have no 20 MiB edge limit any more and depend on the application's own limit (the legacy nginx comment says 15 MB via MaxBytesReader). Nginx-specific annotations are **not** used.
* **Client IP:** needs the optional `externalTrafficPolicy: Local`; otherwise the app sees a node/pod IP in `X-Forwarded-For`. Matters for rate limiting and connection logging obligations.
* **TLS:** cert-manager + ACME HTTP-01 (staging first). Alternative: bring your own certificate as a `kubernetes.io/tls` Secret named `numan-tls`. HTTPS is not claimed to work until DNS, 80/443 and the Secret exist.
* **Never public:** databases, Redis, MinIO (API and console), admin UIs. There is no public route to MinIO: the API reaches it internally.

## 5. Security posture (implemented vs to validate)

| Control | Status |
|---|---|
| Dedicated namespace, PSA baseline enforced / restricted audited | implemented |
| One ServiceAccount per workload, token automount off, no RBAC granted | implemented |
| Default-deny NetworkPolicies with explicit allow-list; DNS allowed | implemented — **requires CNI enforcement** (K3s default does; verify) |
| `runAsNonRoot`, `allowPrivilegeEscalation:false`, `capabilities.drop ALL`, `seccomp RuntimeDefault`, `readOnlyRootFilesystem` | implemented; **VALIDATE against the real images** (UIDs 10001 for app images; 999 for Postgres/Mongo/Redis; 1000 MinIO; 101 nginx) |
| Secret separation (watermark ≠ API ≠ MinIO hot ≠ cold ≠ backup) | implemented |
| Admin UIs off by default, no Ingress, no inbound policy, port-forward only | implemented |
| Images pinned | third-party pinned (**verify tags exist/newest patch**); app images are placeholders; MinIO see README |
| DB traffic encryption | **not** implemented (`sslmode=disable` is the legacy value; protected by NetworkPolicy only) — **[Future]** enable TLS on Postgres/Redis/Mongo |
| Mongo/app use root credentials | legacy behaviour kept (no app-user variable exists) — **[Future]** create an app user once the connection settings are known |
| Anonymous read on hot bucket | **off by default** (`MINIO_HOT_ANONYMOUS_READ=false`): the application streams media with its own credentials. The legacy script enabled it; turn it on only if some consumer reads unsigned URLs |
| Audit logging of K8s API | not configured (K3s `--kube-apiserver-arg=audit-log-path…`) — **[Future]** |

## 6. MinIO hot and cold

| | `minio-hot` | `minio-cold` |
|---|---|---|
| Purpose | active media, app endpoint (`MINIO_ENDPOINT=minio-hot:9000`) | archived / soft-deleted media |
| Workload / volume / Service | own StatefulSet, PVC, Service `minio-hot:9000` | own StatefulSet, PVC, Service `minio-cold:9000` |
| Credentials | `minio-hot-root`, `minio-hot-users` | `minio-cold-root`, `minio-cold-users` (different values) |
| Bootstrap Job | `minio-hot-init` (idempotent, retries, creates bucket, policy, app + backup users) | `minio-cold-init` |
| Anonymous access | optional (`MINIO_HOT_ANONYMOUS_READ`, default off) | never |
| Versioning | off by default (`MINIO_HOT_VERSIONING`) | on by default (`MINIO_COLD_VERSIONING`) |
| Object Lock / retention | – | off by default; **decide before first run** (enable-at-creation only) |
| Public exposure | none (console only by port-forward) | none |
| Backup | daily `mc mirror` (read-only user) → off-site | daily `mc mirror` → off-site |

Protection matrix — what actually protects you:

| Threat | Protection here | Gap |
|---|---|---|
| Node failure | none for availability | pod `Pending` until node returns |
| Disk failure | off-site daily mirror (RPO ≤24h provisional) | no local redundancy |
| Accidental deletion | cold: versioning; hot: only the off-site mirror (no `--remove`) unless `HOT_VERSIONING=true` | versioning is not on hot by default; off-site bucket should have versioning/Object Lock **[Operator input]** |
| Site loss | off-site mirror | RTO depends on re-provisioning |
| Hot→cold movement | **not implemented**: how/when the app (or a job) moves objects is unknown | needs the app/Go behaviour **[Operator input]** |

Two StatefulSets do **not** replicate to each other; "cold" is a separate bucket the application (or a future ILM job) must write to.
The application wiring for cold storage is a documented placeholder in `api.yaml` (the legacy `.env` has no cold-storage variable).

## 7. Retention and legal requirements

The brief mentions a 365-day retention period and French LCEN requirements. **No supplied file contains either.** Nothing here deletes data, and no lifecycle rule was added.
A bucket or lifecycle rule alone does not make anything compliant. Distinguish (1) a **legal retention/obligation** from (2) an **application soft-delete window**; the 365 days may be only (2).
Decisions needed from legal/operations (all **[Operator input]**):

| Topic | Question | Where it lands |
|---|---|---|
| Retention duration | what is legally required per data category (content, identifiers, connection logs)? | cold bucket retention; DB/log retention |
| Deletion eligibility | who/what may delete after expiry; is deletion mandatory at expiry (GDPR minimisation) or only permitted? | lifecycle rules (not created) |
| Object lock | Governance vs Compliance mode; irreversible once enabled at creation | `COLD_OBJECT_LOCK`, `COLD_DEFAULT_RETENTION_*` |
| Administrative access | who holds MinIO root, kube-admin, backup credentials; separation of duties | RBAC/secrets process |
| Audit logging | MinIO audit webhook, K8s audit log, Traefik access logs retention (client IPs) | not configured |
| Encryption | at rest (disk/LUKS, MinIO SSE/KMS) and in transit (DB TLS) | not configured |
| Legal hold | how to freeze deletion for specific objects | Object Lock legal hold (needs lock-enabled bucket) |
| Data-subject requests | how erasure/export is executed across Postgres, Mongo, Redis, both MinIO buckets **and backups** | procedure + backup expiry |
| Backup retention | backups contain deleted data until they expire; restic keeps 14 daily/8 weekly/12 monthly (provisional) | `backup-jobs.yaml` forget policy |

If an Object-Lock/legal-hold design is chosen, set the toggles in `minio-init-config` **before** the first `minio-cold-init` run (a bucket created without lock cannot be converted).
