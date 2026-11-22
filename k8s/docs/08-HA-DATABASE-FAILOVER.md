# High Availability, Read Replicas, Automatic Election & Hardware Plasticity

This runbook documents the Kubernetes HA architecture for **PostgreSQL (CloudNativePG)**, **MongoDB (Native Replica Set `rs0`)**, **Redis (Primary/Replicas + Redis Sentinel)**, **Hardware-Aware NVMe Local PVs**, and **WireGuard-secured Admin UIs**.

---

## 1. Topology Plasticity (1-Node Mac Dev → 1-Node Multi-NVMe → Multi-Server HA)

Every datastore in `k8s/` scales smoothly across four hardware stages without rewriting manifests:

| Stage | Profile Preset | PostgreSQL | MongoDB (`rs0`) | Redis + Sentinel | Storage |
|---|---|---|---|---|---|
| **1. Mac / Single VPS** | `profiles/all-in-one.env.example` | Standalone (`1` pod) | Single-node `rs0` (`1` pod) | Standalone (`1` Redis, `0` Sentinel) | `local-path` |
| **2. Local Replica & Failover Lab** | `profiles/dev-replica-test.env.example` | CNPG (`2` pods: Primary + 1 RO) | `2` pods (Primary + 1 Secondary) | `2` Redis + `3` Sentinels | `local-path` |
| **3. Single Bare-Metal + 3 NVMe SSDs** | `profiles/ha-cluster.env.example` | CNPG (`3` pods, `nvme-postgres`) | `3` pods (`nvme-mongo`) | `3` Redis (`nvme-redis`) + `3` Sentinels | `WaitForFirstConsumer` Local PVs |
| **4. Multi-Server Datacenter HA** | `profiles/ha-cluster.env.example` | CNPG (`3` pods across 3 servers) | `3` pods across 3 servers | `3` Redis + `3` Sentinels across 3 servers | Local NVMe per server |

### Why 1-Node MongoDB `rs0` + Soft Anti-Affinity Enables Hot-Swapping
- **MongoDB starts with `--replSet rs0 --keyFile` even at `DB_MONGO_REPLICAS=1`:** scaling from 1 to 3 replicas hot-adds `mongo-1` and `mongo-2` online and runs `job/mongo-init` (`rs.reconfig()`) with **zero downtime and zero restart of `mongo-0`**.
- **Soft `podAntiAffinity` (`preferredDuringSchedulingIgnoredDuringExecution`):**
  - On **1 physical server** with 3 NVMe SSDs, Kubernetes schedules all 3 replicas on the same host and binds each pod to a **distinct physical NVMe drive** (`nvme-postgres`, `nvme-mongo`, `nvme-redis`).
  - As soon as **2nd and 3rd physical servers** join the K3s cluster, Kubernetes automatically prefers placing replicas on separate physical nodes.

### Quorum Guardrails (`scripts/envtool.py`)
- **`1` instance:** Valid everywhere (standalone).
- **`2` instances (Primary + 1 Replica):** Accepted by `make render` with a non-blocking dev warning (ideal for testing read routing & manual failover on Mac), **rejected by `--strict` / `make deploy`** so a quorum-less 2-node cluster cannot accidentally ship to production.
- **Odd `>= 3` (`3`, `5`, `7`, `9`):** Full production HA quorum. Even counts `>= 4` are rejected.
- **Redis Sentinel:** Must be `0` (standalone) or odd `>= 3` (`3`, `5`, `7`), with `quorum > replicas / 2`.

---

## 2. Dedicated Physical NVMe Binding (Single-Server Multi-SSD or Multi-Server)

Use `scripts/bind-nvme-disk.sh` (and `04-storage/nvme-local-pv.example.yaml`) to bind each replica to its own physical NVMe drive:

```bash
# 1. On the physical Linux server, mount each NVMe SSD and prepare UID 999 directory:
sudo mkdir -p /mnt/nvme0/postgres /mnt/nvme1/postgres /mnt/nvme2/postgres
sudo chown 999:999 /mnt/nvme0/postgres /mnt/nvme1/postgres /mnt/nvme2/postgres
sudo chmod 700 /mnt/nvme0/postgres /mnt/nvme1/postgres /mnt/nvme2/postgres

# 2. Generate & inspect (dry-run default) then apply PVs:
k8s/scripts/bind-nvme-disk.sh --node server-fr-01 --path /mnt/nvme0/postgres --class nvme-postgres --size 200Gi --apply
k8s/scripts/bind-nvme-disk.sh --node server-fr-01 --path /mnt/nvme1/postgres --class nvme-postgres --size 200Gi --apply
k8s/scripts/bind-nvme-disk.sh --node server-fr-01 --path /mnt/nvme2/postgres --class nvme-postgres --size 200Gi --apply

# 3. Point infrastructure.env at your NVMe StorageClass:
#    STORAGE_CLASS_POSTGRES=nvme-postgres
#    STORAGE_CLASS_MONGODB=nvme-mongo
#    STORAGE_CLASS_REDIS=nvme-redis
```

---

## 3. PostgreSQL HA (CloudNativePG) Deployment & Failover Drill

### Deployment
```bash
# One-time operator installation (pinned v1.25.1)
make -C k8s install-cnpg

# Ensure postgres-cnpg-credentials exists
k8s/scripts/create-secrets.sh k8s/01-config/secrets.env

# Deploy CNPG HA Cluster (ensure DB_POSTGRES_STANDALONE_REPLICAS=0, DB_POSTGRES_INSTANCES=3,
# DB_POSTGRES_HOST=postgres-rw, DB_POSTGRES_READ_HOST=postgres-ro in infrastructure.env)
make -C k8s deploy-postgres-ha ENV_FILE=infrastructure.env
make -C k8s deploy ENV_FILE=infrastructure.env
```

### Services Exposed by CNPG
- **`postgres-rw:5432`** → Live Primary (all writes + read-your-writes).
- **`postgres-ro:5432`** → Healthy Read Replicas only.
- **`postgres-ro-local:5432`** → Topology-aware (`trafficDistribution: PreferClose`) Read Replicas (lowest latency).
- **`postgres-r:5432`** → All instances (automatic fallback if replicas are temporarily offline).

### Failover Drill (PostgreSQL)
```bash
# Check current Primary & replication lag
kubectl -n numan get cluster postgres
kubectl -n numan get pods -l cnpg.io/cluster=postgres -L cnpg.io/instanceRole

# Controlled switchover (zero data loss)
kubectl cnpg promote postgres postgres-2 -n numan

# Crash failover drill (simulate sudden Primary death)
PRIMARY=$(kubectl -n numan get pod -l cnpg.io/cluster=postgres,cnpg.io/instanceRole=primary -o jsonpath='{.items[0].metadata.name}')
kubectl -n numan delete pod "$PRIMARY" --force --grace-period=0
# Observe CNPG promoting a replica to Primary within ~10-15s and updating postgres-rw Endpoints:
kubectl -n numan get endpoints postgres-rw -w
```

---

## 4. MongoDB HA (Native Replica Set `rs0`) Failover Drill

### Inspecting Membership & Priorities
```bash
kubectl -n numan exec -it mongo-0 -- mongosh --quiet \
  -u "$MONGO_ROOT_USER" -p "$MONGO_ROOT_PASSWORD" --authenticationDatabase admin \
  --eval 'rs.status().members.map(m => ({name: m.name, state: m.stateStr, health: m.health}))'
```

### Scaling Online (1 → 3 Members with Zero Downtime)
1. Update `infrastructure.env`:
   - `DB_MONGO_REPLICAS=3`
   - `DB_MONGO_HOSTS=mongo-0.mongo-headless:27017,mongo-1.mongo-headless:27017,mongo-2.mongo-headless:27017`
   - `DB_MONGO_READ_PREFERENCE=nearest`
2. Run `make -C k8s deploy ENV_FILE=infrastructure.env`:
   - Kubernetes adds `mongo-1` and `mongo-2` while `mongo-0` stays online.
   - `job/mongo-init` executes `rs.reconfig()` against the live Primary to register the new members with `mongo-0` priority `${DB_MONGO_PRIMARY_PRIORITY}` (`2`) and secondaries priority `1`.

### Failover Drill (MongoDB)
```bash
# Graceful primary step-down (elects mongo-1 or mongo-2 in <5s)
kubectl -n numan exec -it mongo-0 -- mongosh --quiet \
  -u "$MONGO_ROOT_USER" -p "$MONGO_ROOT_PASSWORD" --authenticationDatabase admin \
  --eval 'rs.stepDown(60)'

# Crash failover drill
kubectl -n numan delete pod mongo-0 --force --grace-period=0
# Verify new Primary elected among mongo-1 / mongo-2:
kubectl -n numan exec -it mongo-1 -- mongosh --quiet \
  -u "$MONGO_ROOT_USER" -p "$MONGO_ROOT_PASSWORD" --authenticationDatabase admin \
  --eval 'db.hello().primary'
```

---

## 5. Redis HA (Primary/Replicas + Sentinel) Failover Drill

### Inspecting Live Master & Replicas via Sentinel
```bash
kubectl -n numan exec -it redis-sentinel-0 -- redis-cli -p 26379 SENTINEL get-master-addr-by-name numan-master
kubectl -n numan exec -it redis-sentinel-0 -- redis-cli -p 26379 SENTINEL replicas numan-master
```

### Failover Drill (Redis Sentinel)
```bash
# Manual controlled Sentinel failover
kubectl -n numan exec -it redis-sentinel-0 -- redis-cli -p 26379 SENTINEL failover numan-master

# Crash failover drill (kill current master pod)
kubectl -n numan delete pod redis-0 --force --grace-period=0
# Within ~5-15s (down-after-milliseconds=5000), 2/3 Sentinels elect redis-1 or redis-2.
# When redis-0 boots back up, its startup entrypoint queries redis-sentinel:26379 and automatically
# attaches as a Replica of the newly elected Primary (zero split-brain!).
kubectl -n numan exec -it redis-sentinel-0 -- redis-cli -p 26379 SENTINEL get-master-addr-by-name numan-master
```

---

## 6. Admin UIs (`pgAdmin`, `mongo-express`, `redis-commander`) — Dual Access Modes

All three UIs deploy at **`replicas: 0`** by default and are HA-aware (pgAdmin pre-loads both Primary & `postgres-ro`; `mongo-express` connects to `rs0` with `secondaryPreferred`; `redis-commander` connects via Sentinel).

### Option A — Zero-Ingress `kubectl port-forward` (Default, Highest Isolation)
```bash
make -C k8s deploy-admin ENV_FILE=infrastructure.env
k8s/scripts/admin.sh on pgadmin
k8s/scripts/admin.sh forward pgadmin        # http://localhost:8080 (127.0.0.1 only)
# Always turn off when finished:
k8s/scripts/admin.sh off all
```

### Option B — WireGuard-Only Browser Ingress (Traefik `ipAllowList`)
> **CRITICAL SECURITY PREREQUISITE:** Traefik **MUST** run with `externalTrafficPolicy: Local` so Kubernetes does NOT SNAT client IPs to a node IP. `make deploy-admin-ingress` automatically checks `kube-system/svc/traefik` and refuses to deploy if `externalTrafficPolicy != Local`.

```bash
# 1. Enable Traefik externalTrafficPolicy: Local
make -C k8s deploy-traefik ENV_FILE=infrastructure.env

# 2. Deploy WireGuard-filtered Ingresses (Middleware/wireguard-only = ADMIN_WIREGUARD_SUBNET)
make -C k8s deploy-admin-ingress ENV_FILE=infrastructure.env

# 3. Turn on the UI you need, browse over WireGuard, then turn off:
k8s/scripts/admin.sh on pgadmin
# Browse https://pgadmin.<NUMAN_DOMAIN> (private WireGuard DNS / /etc/hosts only)
k8s/scripts/admin.sh off all
```
