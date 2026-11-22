# 07 — Profils de Serveurs, Auto-Discovery & Multi-Datacenter

Ce document décrit le contrat d'architecture et d'intégration CLI permettant de provisionner des serveurs spécialisés, de regrouper plusieurs machines au sein d'un même datacenter virtuel (ex. **France `fr-par`**, **Allemagne `de-fra`**), et d'inspecter en temps réel la topologie de la flotte.

---

## 1. Architecture « Monolithe Modulaire » (`./numan` multi-rôle)

Un seul binaire Go (`./numan`, image `numan-api`) pilote quatre rôles opérationnels via la variable `SERVER_ROLE` (`cmd/role.go`) :

| `SERVER_ROLE` | Routes HTTP REST | WebSocket `/ws` & Hub | Workers Write-Behind & Crons | Cuckoo Filter & Smart Seeding | Pool Postgres par pod |
| :--- | :---: | :---: | :---: | :---: | :---: |
| **`monolith`** (défaut) | ✅ | ✅ | ✅ | ✅ (verrou Redis `startup_seeding`) | 15 conns |
| **`api_node`** | ✅ | ❌ (404) | ❌ | ✅ (verrou Redis `startup_seeding`) | 15 conns |
| **`ws_node`** | ❌ (404) | ✅ | ❌ | ❌ (boot éclair) | 5 conns |
| **`worker_node`** | ❌ (404) | ❌ (404) | ✅ | ✅ (verrou Redis `startup_seeding`) | 15 conns |

- **Microservice indépendant :** `cmd/watermark` (`numan-watermark`) reste un binaire et un `Deployment` séparés (stéganographie AVIF DCT sans accès BDD).
- **Dépendances réelles respectées :** tous les rôles `./numan` initialisent Redis L1, MongoDB L2, PostgreSQL L3, MinIO (`ws_node` en a besoin pour activer les médias dans les messages, `worker_node` pour purger les orphelins) et les variables dynamiques.
- **Sondes unifiées :** tous les rôles exposent `/healthz`, `/readyz` (incluant `datacenter`, `role`, `node_id`) et `/metrics` interne sur `:9090`.
- **Anti-collision au démarrage :** le Smart Seeding et `mongo.EnsureIndexes` sont protégés par le verrou distribué Redis `startup_seeding` (TTL 2 min, libéré dès la fin du seeding).

---

## 2. Catalogue des Profils de Nœuds Kubernetes

Chaque serveur Linux intégré dans un cluster K3s reçoit des labels normalisés :

- `numan.io/datacenter=<dc-id>` (ex. `fr-par`, `de-fra`)
- `numan.io/node-profile=<profile>`
- `topology.kubernetes.io/region=<region>`

| Profil (`numan.io/node-profile`) | Workloads attirés (poids 100) | Repli sur `all-in-one` (poids 50) | Taint dédié optionnel (`--dedicated`) |
| :--- | :--- | :---: | :--- |
| `all-in-one` | Tous les workloads (cluster mono-serveur ou compact) | — | — |
| `ingress` | `Deployment/traefik` (`kube-system`, Load Balancer L7 + TLS, `externalTrafficPolicy: Local`) | ✅ | `numan.io/dedicated=ingress:NoSchedule` |
| `api` | `StatefulSet/api` (`monolith` ou `api_node`) | ✅ | `numan.io/dedicated=api:NoSchedule` |
| `websocket` | `StatefulSet/websocket` (`ws_node`) | ✅ | `numan.io/dedicated=websocket:NoSchedule` |
| `worker` | `StatefulSet/worker` (`worker_node`) | ✅ | `numan.io/dedicated=worker:NoSchedule` |
| `watermark` | `Deployment/watermark` | ✅ | `numan.io/dedicated=watermark:NoSchedule` |
| `mail` | `StatefulSet/maddy` | ✅ | `numan.io/dedicated=mail:NoSchedule` |
| `db` | `postgres` + `mongo` + `redis` (poids 80) | ✅ | `numan.io/dedicated=db:NoSchedule` |
| `db-sql` | `StatefulSet/postgres` | ✅ | `numan.io/dedicated=db-sql:NoSchedule` |
| `db-mongo` | `StatefulSet/mongo` | ✅ | `numan.io/dedicated=db-mongo:NoSchedule` |
| `db-redis` | `StatefulSet/redis` | ✅ | `numan.io/dedicated=db-redis:NoSchedule` |
| `storage` | `StatefulSet/minio-hot` + `StatefulSet/minio-cold` | ✅ | `numan.io/dedicated=storage:NoSchedule` |

> **Affinité souple (`preferredDuringSchedulingIgnoredDuringExecution`) :**
> - Sur **1 seul serveur** (`all-in-one`), toute la stack démarre sans jamais rester `Pending`.
> - Dès que tu achètes un **2ᵉ serveur** (ex. `--profile websocket` ou `--profile db`), Kubernetes y place automatiquement les pods correspondants.
> - Si tu ajoutes `--dedicated`, seul le workload ciblé (qui porte la `toleration` dédiée) peut s'y exécuter.

---

## 3. Partitionnement Snowflake 10-bit (8 Datacenters × 128 Nœuds)

Les IDs Snowflake Go utilisent 10 bits de `NODE_ID` (`0..1023`). L'espace est découpé en **8 blocs disjoints** pilotés par `DC_INDEX` (`0..7`) :

| `DC_INDEX` | Exemple `DC_ID` | Plage Snowflake totale | API (`API_NODE_ID_START`) | WebSocket (`WS_NODE_ID_START`) | Worker (`WORKER_NODE_ID_START`) |
| :---: | :--- | :---: | :---: | :---: | :---: |
| `0` | `fr-par` | `0..127` | `1..63` | `64..95` | `96..127` |
| `1` | `de-fra` | `128..255` | `129..191` | `192..223` | `224..255` |
| `2` | `nl-ams` | `256..383` | `257..319` | `320..351` | `352..383` |
| `3` | `uk-lon` | `384..511` | `385..447` | `448..479` | `480..511` |
| `4..7` | `...` | `[idx*128 .. idx*128+127]` | `idx*128 + 1` | `idx*128 + 64` | `idx*128 + 96` |

- `scripts/envtool.py` refuse tout rendu si une plage `API`, `WebSocket` ou `Worker` sort du bloc `DC_INDEX`, dépasse `1023`, ou se chevauche.
- `scripts/check-fleet.py` valide plusieurs fichiers `.env` de datacenters ensemble :
  ```bash
  make -C k8s check-fleet ENV_FILES="dc-fr-par.env dc-de-fra.env"
  ```

---

## 4. Presets de Profils (`k8s/profiles/`)

| Fichier | Usage | `API_SERVER_ROLE` | `WS_BACKEND_SERVICE` | `WS_REPLICAS` | `WORKER_REPLICAS` |
| :--- | :--- | :---: | :---: | :---: | :---: |
| `profiles/all-in-one.env.example` | 1 serveur ou petit cluster compact (`fr-par`, DC 0) | `monolith` | `api` | `0` | `0` |
| `profiles/distributed.env.example` | Production multi-nœuds découplée (`fr-par`, DC 0) | `api_node` | `websocket` | `2` | `2` |
| `profiles/edge-stateless.env.example` | 2ᵉ datacenter régional (`de-fra`, DC 1) | `api_node` | `websocket` | `2` | `2` |

---

## 5. Contrat d'Intégration pour la CLI d'Orchestration

### A. Initialiser un nouveau datacenter (1er serveur)

```bash
./k8s/scripts/cluster-init-dc.sh \
  --datacenter de-fra \
  --region eu-central-de \
  --profile all-in-one \
  --tls-san 203.0.113.20 \
  --json
```

Sortie JSON (mode `--json`) :
```json
{
  "status": "initialized",
  "datacenter": "de-fra",
  "region": "eu-central-de",
  "profile": "all-in-one",
  "dedicated": false,
  "api_endpoint": "https://203.0.113.20:6443",
  "kubeconfig": "/etc/rancher/k3s/k3s.yaml",
  "token_file": "/var/lib/rancher/k3s/server/node-token",
  "token": "<read from /var/lib/rancher/k3s/server/node-token>"
}
```

### B. Rattacher un nouveau serveur à un datacenter existant

```bash
./k8s/scripts/node-join-dc.sh \
  --server https://203.0.113.20:6443 \
  --token-file /root/de-fra.token \
  --datacenter de-fra \
  --profile websocket \
  --dedicated
```

### C. Re-assigner le profil d'un nœud à chaud

```bash
./k8s/scripts/node-relabel.sh \
  --node srv-de-02 \
  --profile worker \
  --dedicated
```

### D. Auto-Discovery temps réel (`GET /admin/cluster/topology`)

Chaque instance `./numan` publie un heartbeat toutes les 10s (TTL 30s) dans Redis L1 (`cluster:dc:<dc>:nodes` + `cluster:node:<dc>:<role>:<node_id>`) et se désenregistre immédiatement sur `SIGTERM`/`SIGINT`.

Endpoint sécurisé (`JWT` + `HMAC` + `AdminGuard` WireGuard) :
- `GET /admin/cluster/topology` (datacenter local par défaut)
- `GET /admin/cluster/topology?datacenter=de-fra`

Exemple de réponse JSON :
```json
{
  "datacenter": "de-fra",
  "known_datacenters": ["de-fra", "fr-par"],
  "total_nodes": 3,
  "role_counts": {
    "api_node": 1,
    "worker_node": 1,
    "ws_node": 1
  },
  "total_ws_users": 420,
  "total_ws_connections": 510,
  "redis_queue_depth": 14,
  "collected_at_ms": 1795348800000,
  "nodes": [
    {
      "node_id": 129,
      "role": "api_node",
      "datacenter": "de-fra",
      "hostname": "api-129",
      "pod_name": "api-129",
      "status": "ready",
      "started_at_ms": 1795340000000,
      "last_seen_ms": 1795348795000,
      "ws_users": 0,
      "ws_connections": 0
    }
  ]
}
```
