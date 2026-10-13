# Operations guide

## 1. Observability (practical baseline, no heavy stack imposed)

Infrastructure metrics and application metrics are separate. **No application metrics endpoint is evidenced** in the legacy files, so none is assumed or scraped.

| Signal | Baseline that works with this repo today | Optional enhancement (not installed) |
|---|---|---|
| Container logs | `kubectl -n numan logs <pod> -c <container>`; containerd rotates node-local logs | Loki + Promtail/Alloy, or an external log service. **Retention of access logs containing client IPs is a legal decision** (docs/01 §7) |
| Kubernetes events | `kubectl -n numan get events --sort-by=.lastTimestamp` (events expire after ~1 h) | kubernetes-event-exporter |
| CPU / memory | metrics-server (bundled in K3s): `kubectl top pods -n numan`; feeds the HPAs | Prometheus + node-exporter |
| Pod restarts / crash loops | `kubectl -n numan get pods` (RESTARTS), `verify.sh` | kube-state-metrics alert `increase(kube_pod_container_status_restarts_total[15m]) > 3` |
| Volume capacity | `kubectl -n numan exec <pod> -- df -h <mount>` | kubelet metric `kubelet_volume_stats_available_bytes / kubelet_volume_stats_capacity_bytes < 0.15` (verify it is reported for `local-path`) |
| Database health | probes + `verify.sh` | postgres_exporter, mongodb_exporter, redis_exporter; MinIO Prometheus endpoints (auth config required) |
| Ingress error rate / latency | Traefik access logs (enable with the optional HelmChartConfig) | Traefik Prometheus metrics (confirm they are enabled in the bundled chart): `sum(rate(traefik_service_requests_total{code=~"5.."}[5m]))`, request-duration histograms |
| Backup status | `kubectl -n numan get cronjob,job`; `restic snapshots` | `time() - kube_cronjob_status_last_successful_time > 129600` (36 h) |
| Certificate expiry | `kubectl -n numan get certificate`; `verify.sh` prints the expiry date | `certmanager_certificate_expiration_timestamp_seconds - time() < 21*86400` |
| Alerting | none by default | Alertmanager, **or** an external HTTPS uptime check on `https://HOST/` plus a scheduled `verify.sh`. Alerts that live inside the cluster do not fire when the cluster is down |

Mandatory for this repo to function: metrics-server (HPA) and Traefik. Everything else above is optional. If you install kube-prometheus-stack, add a NetworkPolicy allowing the monitoring namespace to scrape the pods you choose (the default-deny policy blocks it) and budget its CPU/RAM/disk.

## 2. Backup and disaster recovery

A PVC is not a backup; a backup on the same server does not survive that server. All backups go **off the cluster** (Secret `backup-credentials`). **[Operator input]** the target bucket (in another failure domain/site), its versioning/Object Lock, and who can delete from it.

| Data | Method | Frequency (Europe/Paris) | Retention | Encryption | Provisional RPO / RTO |
|---|---|---|---|---|---|
| PostgreSQL | `pg_dump -Fc` + `pg_dumpall --globals-only` → restic | 02:15 daily | restic: 14 daily, 8 weekly, 12 monthly | restic client-side (AES-256) | RPO 24 h / RTO 4 h |
| MongoDB | `mongodump --archive --gzip` → restic | 02:45 daily | same | same | 24 h / 4 h |
| Redis | `redis-cli --rdb` snapshot → restic | 03:15 daily | same | same | 24 h (queue contents between snapshots are lost) / 2 h |
| MinIO hot | `mc mirror` (read-only user, no `--remove`) → off-site bucket | 04:30 daily | governed by the off-site bucket (use versioning/lifecycle there) | server-side/at-rest at the target **[Operator input]** | 24 h / depends on volume (hours per TB) |
| MinIO cold | same | 05:30 daily | same | same | 24 h / same |
| Kubernetes objects | this git repository (declarative) | on change | git history | – | RTO = redeploy time |
| Secrets | `secrets.env` encrypted copy (password manager / SOPS+age) **off-cluster** | on change | – | yes | – |
| K3s state | etcd snapshots (`k3s etcd-snapshot save`; schedule with `etcd-snapshot-schedule-cron`, ship with `etcd-s3` options) + the server `token` and `cred/` (holds the secrets-encryption config) | per your K3s config | – | – | **[Operator input]** |

All RPO/RTO values are **provisional**; confirm with the business. Lower the Postgres RPO with WAL archiving (CloudNativePG/barman). Redis RPO cannot be lowered by snapshots; if queues matter, make them durable in a way the application supports.
Backups are only trustworthy after a successful **restore drill**.

### 2.1 Run a backup now
```bash
kubectl -n numan create job --from=cronjob/backup-postgres bk-pg-1 && kubectl -n numan logs -f job/bk-pg-1 -c backup
kubectl -n numan logs job/bk-pg-1 -c dump      # dump step
```

### 2.2 Restore procedures (from an operator workstation with restic, psql/pg_restore ≥16, mongorestore, mc, kubectl)
```bash
export RESTIC_REPOSITORY=… RESTIC_PASSWORD=… AWS_ACCESS_KEY_ID=… AWS_SECRET_ACCESS_KEY=…   # from your secrets store
restic snapshots                                    # list; tags: postgres | mongo | redis
```
**PostgreSQL** (stop API writers first: `kubectl -n numan scale statefulset/api --replicas=0`)
```bash
restic restore latest --tag postgres --target ./restore-pg      # files land in ./restore-pg/backup/
kubectl -n numan port-forward svc/postgres 15432:5432 &
export PGPASSWORD=…
psql -h 127.0.0.1 -p 15432 -U "$PGUSER" -d postgres -f restore-pg/backup/globals.sql      # roles; "already exists" errors are expected
pg_restore -h 127.0.0.1 -p 15432 -U "$PGUSER" -d "$PGDB" --clean --if-exists --no-owner restore-pg/backup/numan.dump
```
**MongoDB**
```bash
restic restore latest --tag mongo --target ./restore-mongo
kubectl -n numan port-forward svc/mongo 27018:27017 &
mongorestore --host 127.0.0.1 --port 27018 -u "$MONGO_ROOT_USER" -p "$MONGO_ROOT_PASSWORD" --authenticationDatabase admin \
  --archive=restore-mongo/backup/mongo.archive.gz --gzip --drop
```
**Redis** (Redis 7 with `appendonly yes` ignores `dump.rdb` when no AOF exists, so load the RDB with AOF off, then re-enable AOF)
```bash
restic restore latest --tag redis --target ./restore-redis
kubectl -n numan scale statefulset/api --replicas=0 ; kubectl -n numan scale statefulset/redis --replicas=0
# loader pod mounting the existing PVC (data-redis-0), AOF disabled:
kubectl -n numan apply -f - <<'EOF'
apiVersion: v1
kind: Pod
metadata:
  name: redis-restore
  namespace: numan
  labels: {app.kubernetes.io/name: redis-restore}
spec:
  securityContext: {runAsNonRoot: true, runAsUser: 999, runAsGroup: 999, fsGroup: 999, seccompProfile: {type: RuntimeDefault}}
  containers:
    - name: redis
      image: redis:7.4.2
      command: ["sleep", "3600"]
      resources: {requests: {cpu: 100m, memory: 256Mi}, limits: {cpu: "1", memory: 5Gi}}
      securityContext: {allowPrivilegeEscalation: false, capabilities: {drop: ["ALL"]}}
      volumeMounts: [{name: data, mountPath: /data}]
  volumes: [{name: data, persistentVolumeClaim: {claimName: data-redis-0}}]
EOF
kubectl -n numan wait --for=condition=Ready pod/redis-restore
kubectl -n numan exec redis-restore -- sh -c 'rm -rf /data/appendonlydir /data/dump.rdb'
kubectl -n numan cp restore-redis/backup/redis.rdb redis-restore:/data/dump.rdb
kubectl -n numan exec redis-restore -- sh -c 'redis-server --dir /data --appendonly no --daemonize yes && sleep 5 && redis-cli DBSIZE'
kubectl -n numan exec redis-restore -- sh -c 'redis-cli CONFIG SET appendonly yes && sleep 10 && redis-cli INFO persistence | grep -E "aof_enabled|aof_rewrite_in_progress"'
kubectl -n numan exec redis-restore -- sh -c 'redis-cli SHUTDOWN SAVE'   # (the pod has no auth because it was started without requirepass)
kubectl -n numan delete pod redis-restore
kubectl -n numan scale statefulset/redis --replicas=1 ; kubectl -n numan scale statefulset/api --replicas=2
```
**MinIO** (hot shown; cold identical with `minio-cold` and its root credentials)
```bash
kubectl -n numan port-forward svc/minio-hot 9000:9000 &
mc alias set offsite "$BACKUP_S3_ENDPOINT" "$AWS_ACCESS_KEY_ID" "$AWS_SECRET_ACCESS_KEY"
mc alias set hot http://127.0.0.1:9000 "$MINIO_HOT_ROOT_USER" "$MINIO_HOT_ROOT_PASSWORD"
mc mirror --preserve "offsite/$BACKUP_MIRROR_BUCKET/minio-hot/numan-bucket" hot/numan-bucket
```
**Whole cluster lost:** rebuild K3s (restore etcd snapshot or reinstall), recreate Secrets from the off-cluster `secrets.env`, `deploy.sh`, then restore each datastore as above, re-apply TLS, run `verify.sh`.

### 2.3 Restore drills (do them, record the numbers)
1. Monthly: restore the latest snapshots into a **scratch** cluster/namespace (never over production). Record wall-clock time → this is your measured RTO.
2. Compare row/document/object counts and run `verify.sh` against the scratch deployment.
3. Quarterly: simulate loss of a node holding `local-path` volumes (`kubectl drain`/power off) and recover one datastore from backup.
4. Test that the off-site credentials **cannot delete** backups (or that the bucket is versioned/locked).

## 3. Node failure and maintenance runbook

| Event | Behaviour | Action |
|---|---|---|
| Node with api/watermark pods fails (≥2 nodes) | pods rescheduled after the node is marked NotReady (~5 min default tolerations) | none; confirm with `kubectl get pods -o wide` |
| Node with a datastore PVC (`local-path`) fails | pod `Pending`; **no data movement** | bring the node back; otherwise delete the PVC and restore on another node |
| Planned maintenance | `kubectl drain <node> --ignore-daemonsets --delete-emptydir-data` | datastores stay down for the duration (single instance); schedule a window; PDBs keep ≥1 API/watermark pod |
| Disk full on a datastore volume | database stops / MinIO read-only | expand the PVC if the class supports expansion (`local-path` does **not**) or restore into a larger volume |
| Upgrading an application image | change `API_IMAGE_TAG`/`WATERMARK_IMAGE_TAG` in the env file, `make diff`, `make deploy`; StatefulSets roll one pod at a time (third-party images: edit the pinned tag in the manifest, same commands) | back up first; major DB versions need dump/restore or an operator |
| Rotating a secret | update `secrets.env`, `create-secrets.sh`, rolling restart | JWT/HMAC rotation invalidates sessions/signatures |
| Scaling | API: HPA or `kubectl -n numan scale statefulset/api --replicas=N` (N ≤ 6 unless NODE_ID range verified) | check DB connection limits first |
| Enabling workers | follow the 7 points in `05-apps/worker.yaml` | one replica first |

## 4. Admin UIs
`bash scripts/admin.sh on pgadmin` → `forward pgadmin` → browse `http://localhost:8080` → `off all` when finished. They have no Ingress and no inbound NetworkPolicy by design. Do not add a public route without an authentication layer and an explicit decision; mongo-express and Redis Commander hold datastore credentials in their environment.
