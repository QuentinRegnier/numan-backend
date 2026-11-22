#!/usr/bin/env bash
# Generates (and optionally applies) a WaitForFirstConsumer StorageClass + local PersistentVolume
# pinning a dedicated physical NVMe SSD mountpoint on a K3s node to PostgreSQL, MongoDB, or Redis.
#
# Never formats or mounts block devices automatically (zero data-loss risk).
#
# Usage:
#   scripts/bind-nvme-disk.sh \
#     --node <k8s-node-hostname> \
#     --path </mnt/nvme0/postgres> \
#     --class <nvme-postgres|nvme-mongo|nvme-redis> \
#     --size <200Gi> \
#     [--name <pv-name>] \
#     [--context <kube-context>] \
#     [--apply]
set -euo pipefail

NODE=""
DISK_PATH=""
STORAGE_CLASS=""
SIZE=""
PV_NAME=""
KUBE_CTX=""
APPLY=0

while [ $# -gt 0 ]; do
  case "$1" in
    --node)    NODE="${2:?}"; shift 2;;
    --path)    DISK_PATH="${2:?}"; shift 2;;
    --class)   STORAGE_CLASS="${2:?}"; shift 2;;
    --size)    SIZE="${2:?}"; shift 2;;
    --name)    PV_NAME="${2:?}"; shift 2;;
    --context) KUBE_CTX="${2:?}"; shift 2;;
    --apply)   APPLY=1; shift;;
    --dry-run) APPLY=0; shift;;
    -h|--help)
      sed -n '2,15p' "$0"
      exit 0
      ;;
    *) echo "ERROR: unknown argument $1" >&2; exit 2;;
  esac
done

[ -n "$NODE" ] || { echo "ERROR: --node is required" >&2; exit 2; }
[ -n "$DISK_PATH" ] || { echo "ERROR: --path is required (e.g. /mnt/nvme0/postgres)" >&2; exit 2; }
[ -n "$STORAGE_CLASS" ] || { echo "ERROR: --class is required (e.g. nvme-postgres)" >&2; exit 2; }
[ -n "$SIZE" ] || { echo "ERROR: --size is required (e.g. 200Gi)" >&2; exit 2; }

case "$DISK_PATH" in
  /*) ;;
  *) echo "ERROR: --path must be an absolute path (got '$DISK_PATH')" >&2; exit 2;;
esac

if [ -z "$PV_NAME" ]; then
  SLUG="$(printf '%s' "$DISK_PATH" | tr '/_' '--' | sed 's/^-*//;s/-*$//' | tr 'A-Z' 'a-z')"
  NODE_SLUG="$(printf '%s' "$NODE" | tr '._' '--' | tr 'A-Z' 'a-z')"
  PV_NAME="pv-${STORAGE_CLASS}-${NODE_SLUG}-${SLUG}"
fi

MANIFEST="$(cat <<YAML
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: ${STORAGE_CLASS}
provisioner: kubernetes.io/no-provisioner
volumeBindingMode: WaitForFirstConsumer
reclaimPolicy: Retain
---
apiVersion: v1
kind: PersistentVolume
metadata:
  name: ${PV_NAME}
  labels:
    app.kubernetes.io/part-of: numan
    numan.io/storage-tier: ${STORAGE_CLASS}
spec:
  capacity:
    storage: ${SIZE}
  volumeMode: Filesystem
  accessModes:
    - ReadWriteOnce
  persistentVolumeReclaimPolicy: Retain
  storageClassName: ${STORAGE_CLASS}
  local:
    path: ${DISK_PATH}
  nodeAffinity:
    required:
      nodeSelectorTerms:
        - matchExpressions:
            - key: kubernetes.io/hostname
              operator: In
              values:
                - ${NODE}
YAML
)"

if [ "$APPLY" = 0 ]; then
  printf '%s\n' "$MANIFEST"
  echo "" >&2
  echo "# Host prerequisite on ${NODE} before pods mount this PV:" >&2
  echo "#   sudo mkdir -p ${DISK_PATH} && sudo chown 999:999 ${DISK_PATH} && sudo chmod 700 ${DISK_PATH}" >&2
  echo "# Re-run with --apply to apply directly to Kubernetes." >&2
  exit 0
fi

command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl is required for --apply" >&2; exit 1; }
KCTL=(kubectl)
if [ -n "$KUBE_CTX" ]; then KCTL+=(--context "$KUBE_CTX"); fi
printf '%s\n' "$MANIFEST" | "${KCTL[@]}" apply -f -
echo "Applied StorageClass/${STORAGE_CLASS} and PersistentVolume/${PV_NAME} on node ${NODE} (${DISK_PATH}, ${SIZE})."
