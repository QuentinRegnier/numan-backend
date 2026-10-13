# Numan on K3s — infrastructure repository

Migration of the legacy Docker Compose stack (nginx + 2 API containers + Postgres + Mongo + Redis + MinIO +
watermark + admin UIs) to a K3s cluster on bare metal. Scope: infrastructure only; the Go source and the
Dockerfile were **not** available, so every application behaviour that could not be proven from the supplied
files is marked **VERIFY** in the manifests and in [docs/04-PRODUCTION-READINESS.md](docs/04-PRODUCTION-READINESS.md).

| Document | Content |
|---|---|
| [docs/00-AUDIT.md](docs/00-AUDIT.md) | Legacy audit: inventory, findings with file evidence, missing information |
| [docs/01-ARCHITECTURE.md](docs/01-ARCHITECTURE.md) | Target architecture, HA/failure-domain table, storage, MinIO, retention decisions |
| [docs/02-MIGRATION-MAP.md](docs/02-MIGRATION-MAP.md) | Legacy → Kubernetes mapping, full environment-variable table |
| [docs/03-OPERATIONS.md](docs/03-OPERATIONS.md) | Observability, backup/restore, restore drills, node-failure runbook |
| [docs/04-PRODUCTION-READINESS.md](docs/04-PRODUCTION-READINESS.md) | What is done, what is not, decisions you must take |
| [docs/05-VARIABLES.md](docs/05-VARIABLES.md) | Generated inventory of every deployment variable: file → resource → field, default, validation, change impact |
| [docs/06-TEMPLATING.md](docs/06-TEMPLATING.md) | The variable convention, render/validate/deploy workflow, Kustomize integration, troubleshooting |
| [docs/CHANGELOG.md](docs/CHANGELOG.md) | Decisions and changes over time (including the removal of the nginx gateway) |

## What has and has not been validated

Reproducible with `make test` (needs no cluster): template lint (every deployment variable used by a manifest is defined and vice
versa), rendering with sample values, byte-identical output for identical inputs, sources never modified, runtime references
(`$(VAR)`, `$VAR`) preserved, `kubeconform --strict` against the Kubernetes version in `KUBERNETES_VERSION` (Traefik and cert-manager
CRD schemas included), cross-reference/quota/Redis-memory checks, 18 negative cases that must be rejected with the right error, and
the Ingress routing of the rendered output (`/` and `/ws` → `api:8080`, `/process` → `watermark:8080`, HTTP redirect intact).
`make deploy` was exercised only against a **stub** `kubectl` (it proves the guard logic, not cluster behaviour).

**Not done: nothing has been applied to a real cluster.** Schema validation and dry runs do not prove that images start under the
configured UIDs, that probes pass, that NetworkPolicies are enforced by your CNI, that Traefik routes as intended, or that backups
restore. `scripts/verify.sh` runs those checks against your cluster. The Cloud CLI itself was not available for inspection
(see docs/06-TEMPLATING.md, "Cloud CLI integration contract").

## Repository layout

```
k8s/
├── infrastructure.env.template   authoritative blueprint of every deployment variable (copy to infrastructure.env)
├── Makefile                      CLI entry points: lint · render · validate · diff · deploy* · verify · inventory · test
├── kustomization.yaml            TEMPLATE root (rendered before Kustomize builds it)
├── namespace.yaml                namespace "numan" (PSA: baseline enforced, restricted audited)
├── 01-config/                    configmaps.yaml · secret-templates.yaml (NOT applied/rendered) · secrets.env.example
├── 02-security/                  serviceaccounts · networkpolicies · resourcequotas(+LimitRange)
├── 03-databases/                 postgresql · mongodb · redis   (single instances, see HA table)
├── 04-storage/                   minio-hot · minio-cold · bucket-init-jobs
├── 05-apps/                      api · worker (disabled) · watermark
├── 06-ingress/                   ingress · middleware · optional/traefik-helmchartconfig · tls/ (cert-manager)
├── 07-autoscaling/hpa.yaml
├── 08-operations/                backup-jobs.yaml · admin-tools/ (optional, replicas 0)
├── scripts/                      render · validate · deploy · verify · create-secrets · admin · selftest (.sh) ·
│                                 envtool.py (variable engine) · check-refs.py · lib.sh
├── tests/sample.env              non-secret sample values for `make test`
├── build/                        (git-ignored) rendered/ manifests/ resolved.env stamps
└── docs/
```

Every `*.yaml` outside `docs/`, `scripts/` and `build/` (except the Secret templates) is a **template**: it contains `${NAME}` deployment
variables and must not be applied directly. Layout deviations from the original request: `tls/` and `optional/` sub-directories
(external-operator resources are isolated), `secrets.env.example`, `check-refs.py`, and the templating files above.

## 1. Prerequisites

| Item | Requirement |
|---|---|
| K3s / Kubernetes | **≥ v1.31** is required by the manifests (StatefulSet `ordinals.start`, pod-index label, native `preStop.sleep`). Use a current K3s line; at the time of writing (Oct 2026) K3s release pages list 1.35–1.37 and bundle Traefik v3.x. **Check your bundled chart:** recent K3s moved the Traefik chart to v40.x, so confirm the `HelmChartConfig` keys (`kubectl -n kube-system get helmchart traefik -o yaml`) |
| Topology | Baseline works on 1 node. Control-plane HA needs **3 server nodes** (embedded etcd quorum; 2 is *worse* than 1) and an address for the Kubernetes API that survives a node loss (kube-vip / keepalived / external LB). Application HA needs ≥ 2 schedulable nodes |
| Components that must **not** be disabled | Traefik, ServiceLB, metrics-server, local-path-provisioner, and the embedded network-policy controller (no `--disable-network-policy`). `scripts/verify.sh` proves NetworkPolicy enforcement empirically |
| Secrets at rest | Start the K3s server with `--secrets-encryption` (or `secrets-encryption: true` in `/etc/rancher/k3s/config.yaml`) **before** creating Secrets; `k3s secrets-encrypt status` to verify |
| Operator tools | `make`, `bash`, `python3` ≥ 3.9 + PyYAML, `envsubst` (GNU gettext), `kustomize` (or `kubectl kustomize`), `kubeconform`, `kubectl`, `helm` (for cert-manager), optional `skopeo`, `sops`+`age` |
| Network | Public DNS A/AAAA record for your domain → the node(s) running Traefik; TCP 80 and 443 open from the Internet; with ServiceLB, Traefik binds 80/443 on the node IPs |
| Storage | Default `local-path` (node-local, **not replicated**). Decide before first deploy: a `StorageClass` change later means recreating volumes. Replicated option: Longhorn / Rook-Ceph (not included) |
| Images | Application images built from your Dockerfile and pushed to a registry (or imported on every node with `k3s ctr images import`). The watermark service needs **its own image** (see `05-apps/watermark.yaml`) |
| Off-site backup target | An S3-compatible bucket **outside** the cluster/site, plus a second bucket (or prefix) for MinIO mirrors |

### MinIO supply chain — decision required before production

Verified Oct 2026: the MinIO community repository was archived (Apr 2026), the project is source-only, and
`minio/minio` + `minio/mc` were reported removed from Docker Hub; the last official community images remain on Quay.
The pinned images here (`quay.io/minio/minio:RELEASE.2025-09-07T16-13-09Z`, `quay.io/minio/mc:RELEASE.2025-08-13T08-35-41Z`)
**pre-date the fix for CVE-2025-62506** (restricted service-account/STS session policies could be bypassed; fixed in
source release `RELEASE.2025-10-15T17-29-55Z`). Mitigations already in this repo: no service accounts/STS are used, MinIO
is never exposed outside the cluster, and the app user cannot call admin APIs. That is mitigation, not a fix. Options:

| Option | Trade-off |
|---|---|
| **A. Build the patched tag yourself** (recommended): `git checkout RELEASE.2025-10-15T17-29-55Z && TAG=<your-registry>/minio/minio:RELEASE.2025-10-15T17-29-55Z make docker`, push, set `MINIO_IMAGE` (and `MINIO_MC_IMAGE`) in `infrastructure.env` | You own patching/rebuilds forever; needs CI + registry; the console is the reduced community one |
| B. Community fork (e.g. `pgsty/minio`) | Third-party trust decision; review provenance before use; not evaluated here |
| C. Another S3 server (Garage, SeaweedFS, Ceph RGW, …) | Needs app-compat testing (S3 API behaviour the application relies on, bucket policy) and different bootstrap Jobs |
| D. Commercial MinIO AIStor | Licensing |

## 2. Configure the environment (deployment variables)

Everything that differs between environments is a **deployment variable** declared, documented and validated in
`infrastructure.env.template` (84 variables; 6 have no safe default and are marked `__REQUIRED__`). They are substituted into the
templates by a render step *before* Kustomize and `kubectl` run; application runtime variables (`JWT_SECRET`, `DB_DSN`, …) are a
different mechanism and are untouched. Full convention, safety rules and troubleshooting: [docs/06-TEMPLATING.md](docs/06-TEMPLATING.md);
per-variable details: [docs/05-VARIABLES.md](docs/05-VARIABLES.md).

```bash
cd k8s
cp infrastructure.env.template infrastructure.env       # git-ignored; or use any file via ENV_FILE=...
$EDITOR infrastructure.env                               # set the 6 __REQUIRED__ values; adjust sizes/schedules/quotas
make lint                                                # template/variable consistency
make render                                              # -> build/manifests/{core,tls,admin,traefik}.yaml (no cluster access)
make validate                                            # kubeconform + consistency + quota/Redis checks
bash scripts/validate.sh --check-images                  # needs skopeo: confirms every image reference exists
```
Required values: `KUBE_CONTEXT` (the only cluster the CLI may deploy to), `NUMAN_DOMAIN`, `ACME_EMAIL`, `IMAGE_REGISTRY`,
`API_IMAGE_TAG`, `WATERMARK_IMAGE_TAG`. Sizes (PVCs, CPU/memory, replicas, quotas) default to **initial recommendations, not
measurements**. `STORAGE_CLASS` and `PVC_SIZE_*` are immutable once the StatefulSets exist (see docs/06-TEMPLATING.md §6).
Application images must be published as `<IMAGE_REGISTRY>/numan-api:<tag>` (also used by the worker) and
`<IMAGE_REGISTRY>/numan-watermark:<tag>`. **Never apply the source files directly**: the template `kustomization.yaml` files carry a guard
that makes `kustomize build .` fail until they are rendered.

**Image pinning.** Third-party images are pinned in their manifests to explicit versions chosen from public knowledge and are
**not verified to be the newest patch**. Run `--check-images`, bump to the latest patch of the same major line, and record digests
(`skopeo inspect docker://IMAGE | jq -r .Digest`). Only the two MinIO images are deployment variables (`MINIO_IMAGE`,
`MINIO_MC_IMAGE`) because their supply chain is an operator decision.

**Private registry.** `kubectl -n numan create secret docker-registry registry-credentials --docker-server=… --docker-username=… --docker-password=…`
then uncomment `imagePullSecrets` in `02-security/serviceaccounts.yaml` (api, worker, watermark).

## 3. Secrets

The manifests reference 13 Secret objects (names/keys in `01-config/secret-templates.yaml`). Real values never go in git.

```bash
cp 01-config/secrets.env.example 01-config/secrets.env && chmod 600 01-config/secrets.env
$EDITOR 01-config/secrets.env          # reuse the EXISTING JWT_SECRET / HMAC_SECRET; hex passwords: openssl rand -hex 32
bash scripts/create-secrets.sh --dry-run
bash scripts/create-secrets.sh
```

* Practical default for bare metal: **manual creation from a git-ignored file + K3s encryption at rest**. It needs no
  operator and no extra moving part. Base64 is *not* encryption: the protection is `--secrets-encryption`, RBAC on
  `secrets`, and not committing values.
* GitOps alternative: encrypt with SOPS+age (`kubectl create secret … --dry-run=client -o yaml | sops --encrypt --age <pubkey> /dev/stdin > x.sops.yaml`)
  or use Sealed Secrets / External Secrets Operator. Each is an operator/tool you would install separately; none is assumed here.
* Back up `secrets.env` encrypted **off-cluster** together with the restic password; losing `RESTIC_PASSWORD` makes every backup unreadable.
* Rotate `WATERMARK_SECRET_KEY` (the legacy value is human-readable) and any secret that was ever pasted in a chat or ticket.
* Changing a Secret does not restart pods: `kubectl -n numan rollout restart statefulset/api deployment/watermark`.

## 4. Optional cluster-wide Traefik tuning (kube-system)

```bash
make deploy-traefik                                          # TRAEFIK_REPLICAS Traefik pods, client-IP preservation, access logs
```
With `externalTrafficPolicy: Local`, DNS must only target nodes that run a Traefik pod.

## 5. Deploy

```bash
make diff                         # render --strict + validate + kubectl diff against KUBE_CONTEXT (read-only)
make deploy ARGS=--dry-run        # server-side dry run, nothing changes
make deploy                       # render --strict → validate → context guard → diff → confirmation → apply → waits
```
`deploy` always re-renders and re-validates in the same run, refuses to run when the current kubectl context is not `KUBE_CONTEXT`, rejects
reserved placeholder domains/registries, and applies **only** `build/manifests/core.yaml`. Order is enforced by readiness, not by Compose-style
`depends_on`: datastores have startup/readiness probes, the MinIO Jobs retry for up to 15 min, and API/worker pods have a `wait-for-datastores`
init container. Manual inspection: `kubectl -n numan get pods,pvc,jobs`, `kubectl -n numan rollout status statefulset/api`.

## 6. TLS (cert-manager) and ingress activation

```bash
helm repo add jetstack https://charts.jetstack.io && helm repo update
helm install cert-manager jetstack/cert-manager -n cert-manager --create-namespace \
  --version <PIN_A_VERSION_COMPATIBLE_WITH_YOUR_K8S> --set crds.enabled=true
kubectl -n cert-manager rollout status deploy/cert-manager-webhook
make deploy-tls                                       # ClusterIssuers + Certificate; TLS_ISSUER defaults to letsencrypt-staging
kubectl -n numan describe certificate numan-tls       # wait for Ready=True (needs DNS + port 80 reachable)
CURL_OPTS=-k HOST=YOUR.DOMAIN bash scripts/verify.sh  # staging cert is untrusted: -k is expected
# set TLS_ISSUER=letsencrypt-prod in infrastructure.env, `make deploy-tls`, then delete secret numan-tls to force re-issue
```
HTTPS is **not** working until: DNS resolves to the cluster, 80/443 are reachable, and `numan-tls` exists. Until then
Traefik serves its built-in self-signed certificate.

## 7. Data initialisation and migration from the Compose stack

Fresh volumes are initialised automatically (`POSTGRES_USER/PASSWORD/DB`, `MONGO_INITDB_ROOT_*`, MinIO buckets via the Jobs).
To migrate existing data (keep the old stack running and untouched until the new one is proven):

```bash
# 1. announce a maintenance window; stop writers on the OLD stack (api1/api2/watermark), keep databases up
# 2. Postgres
docker compose -f docker-compose.prod.yml exec -T postgres pg_dump -U "$POSTGRES_USER" -Fc "$POSTGRES_DB" > numan.dump
kubectl -n numan port-forward svc/postgres 15432:5432 &     # (the port-forward is not subject to NetworkPolicy)
PGPASSWORD=… pg_restore -h 127.0.0.1 -p 15432 -U "$POSTGRES_USER" -d "$POSTGRES_DB" --no-owner --clean --if-exists numan.dump
# 3. Mongo
docker compose -f docker-compose.prod.yml exec -T mongo mongodump -u "$MONGO_ROOT_USER" -p "$MONGO_ROOT_PASSWORD" --authenticationDatabase admin --archive --gzip > mongo.archive.gz
kubectl -n numan port-forward svc/mongo 27018:27017 &
mongorestore --host 127.0.0.1 --port 27018 -u … -p … --authenticationDatabase admin --archive=mongo.archive.gz --gzip --drop
# 4. Objects: mc mirror OLD-minio/numan-bucket → NEW minio-hot (port-forward 9000, use the root creds of minio-hot)
# 5. Redis: DECISION - it holds queues/jobs as well as cache. Either drain the queues and start cold, or copy the RDB/AOF into the
#    new PVC while Redis is stopped (see docs/03-OPERATIONS.md "Redis restore"). Do not guess.
# 6. verify row counts / object counts, start API, run scripts/verify.sh, then switch DNS
```
Rollback of the cut-over = point DNS back to the old stack (keep its data untouched; data written on the new stack during the
window is not on the old one — decide on a freeze policy in advance).

## 8. Health verification and smoke tests

```bash
bash scripts/verify.sh                         # in-cluster checks + NetworkPolicy negative tests
HOST=YOUR.DOMAIN bash scripts/verify.sh        # + routing, redirect, HSTS, 413 on /process, TLS expiry
kubectl -n numan get pods -o wide              # RESTARTS should be 0
kubectl -n numan get events --sort-by=.lastTimestamp | tail -30
kubectl -n numan logs statefulset/api --all-containers --tail=100
kubectl auth can-i --list -n numan --as=system:serviceaccount:numan:api     # expect no meaningful permissions
```
Manual tests the script cannot do for you: a **real WebSocket client** through `wss://HOST/ws` with valid credentials; an
upload through `/process`; media download through `/` (the application streams it from MinIO; check first-byte latency and that large files complete); pod rescheduling
(`kubectl drain <node> --ignore-daemonsets --delete-emptydir-data`, then watch api/watermark move; datastores on `local-path` stay `Pending` until their node is back — expected);
**backup restore** ([docs/03-OPERATIONS.md](docs/03-OPERATIONS.md)). `kubectl apply --dry-run=client` and kubeconform only
check structure — they do not talk to your cluster's admission or CNI.

## 9. Backups

```bash
kubectl -n numan create job --from=cronjob/backup-postgres backup-postgres-manual-1
kubectl -n numan logs -f job/backup-postgres-manual-1 -c backup
```
Repeat for `backup-mongo`, `backup-redis`, `backup-minio-hot`, `backup-minio-cold`. A green Job is **not** a tested restore.

## 10. Optional admin UIs (disabled by default)

```bash
make deploy-admin                                # Deployments at replicas 0, no Ingress, no inbound NetworkPolicy
bash scripts/admin.sh on pgadmin && bash scripts/admin.sh forward pgadmin   # http://localhost:8080
bash scripts/admin.sh off all
```

## 11. Rollback

| Situation | Action |
|---|---|
| Bad app rollout | `kubectl -n numan rollout undo statefulset/api` (or `deployment/watermark`); `rollout status` to confirm |
| Bad manifest or variable change | `git revert` / restore the env file, `make diff` to review, then `make deploy` |
| Init Job failed | read `kubectl -n numan logs job/minio-hot-init`; fix; `make deploy` (it deletes the finished init Jobs first) |
| StatefulSet volumeClaimTemplate/storage class must change | immutable: back up, delete the StatefulSet **with** `--cascade=orphan`, handle PVCs deliberately (PVCs are retained by `persistentVolumeClaimRetentionPolicy`) |
| Data corruption / node loss | restore from off-site backups ([docs/03-OPERATIONS.md](docs/03-OPERATIONS.md)) |
| Whole migration | switch DNS back to the Compose stack |

## 12. Troubleshooting

| Symptom | Likely cause → fix |
|---|---|
| `ImagePullBackOff` | placeholder image not replaced / private registry without pull secret / image not imported on that node |
| `CreateContainerConfigError … non-root` or `Permission denied` / `read-only file system` in logs | the unseen image does not fit `runAsUser`/`readOnlyRootFilesystem` → run once with the Dockerfile's real USER, set that UID, mount a writable `emptyDir` on the path the log names; relax only the specific setting |
| API pod stuck in `Init:0/1` | `wait-for-datastores` cannot reach a datastore: `kubectl logs <pod> -c wait-for-datastores`; check NetworkPolicy labels and that the datastore is Ready |
| API crashes with NOAUTH / auth errors on Redis | the Go code does not send `REDIS_PASSWORD`. Fix the app; do **not** silently remove `--requirepass` |
| Traefik returns `404 page not found` | host mismatch with the placeholder domain, missing `ingressClassName: traefik`, or Middleware CRD/namespace annotation mismatch (`numan-<name>@kubernetescrd`) |
| Media downloads fail or are truncated | check API logs for S3 errors (credentials of the `minio-hot-users` Secret, bucket name = `MINIO_HOT_BUCKET`); Traefik `readTimeout` (default 60 s per the Traefik v3 docs) also bounds reading a slow upload, `writeTimeout` defaults to 0 (no cap on streaming responses) |
| WebSocket drops after N seconds | idle timeouts in a client/LB in front of Traefik; legacy nginx closed idle WS after 60 s (default `proxy_read_timeout`), Traefik does not — see docs/01-ARCHITECTURE.md |
| HTTP 413 on uploads | by design above 20 MiB; the app additionally enforces its own limit (legacy comment: 15 MB) |
| `Pending` PVC / pod after a node loss | `local-path` volumes are bound to one node → bring the node back or restore elsewhere |
| HPA shows `<unknown>` | metrics-server disabled/unready, or the container has no CPU request |
| Certificate stuck `Pending` | DNS not pointing here, port 80 blocked, solver pod blocked (policy `allow-traefik-to-acme-solver`), staging/prod rate limits: `kubectl describe challenge -A` |
| Everything times out after applying policies | CNI does not implement NetworkPolicy semantics as assumed, or your workload needs an egress/ingress path not listed in `02-security/networkpolicies.yaml` |
| `make render` fails with `ENV ERROR` / `LINT ERROR` | read the message: it names the variable and the constraint (table in docs/06-TEMPLATING.md §8) |
| `kustomize build .` fails mentioning `__TEMPLATE_NOT_RENDERED__` | expected: you built the template tree; use `make render` and the `build/` output |
