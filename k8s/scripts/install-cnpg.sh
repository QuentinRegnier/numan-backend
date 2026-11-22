#!/usr/bin/env bash
# Installs the CloudNativePG (CNPG) operator into namespace `cnpg-system` at a pinned version.
# Required once per Kubernetes cluster before deploying the optional `postgres-ha` stack
# (`make deploy-postgres-ha`).
#
# Usage:
#   scripts/install-cnpg.sh [--version 1.25.1] [--context KUBE_CONTEXT] [--dry-run]
set -euo pipefail

CNPG_VERSION="1.25.1"
KUBE_CTX=""
DRY_RUN=0

while [ $# -gt 0 ]; do
  case "$1" in
    --version) CNPG_VERSION="${2:?}"; shift 2;;
    --context) KUBE_CTX="${2:?}"; shift 2;;
    --dry-run) DRY_RUN=1; shift;;
    -h|--help)
      sed -n '2,8p' "$0"
      exit 0
      ;;
    *) echo "ERROR: unknown argument $1" >&2; exit 2;;
  esac
done

case "$CNPG_VERSION" in
  [0-9]*.[0-9]*.[0-9]*) ;;
  *) echo "ERROR: --version must be MAJOR.MINOR.PATCH (got '$CNPG_VERSION')" >&2; exit 2;;
esac

MAJOR_MINOR="${CNPG_VERSION%.*}"
MANIFEST_URL="https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-${MAJOR_MINOR}/releases/cnpg-${CNPG_VERSION}.yaml"

KCTL=(kubectl)
if [ -n "$KUBE_CTX" ]; then
  KCTL+=(--context "$KUBE_CTX")
fi

echo "==> CloudNativePG operator install"
echo "    Version  : v${CNPG_VERSION}"
echo "    Manifest : ${MANIFEST_URL}"
echo "    Context  : ${KUBE_CTX:-<current>}"
echo "    Dry-run  : ${DRY_RUN}"

if [ "$DRY_RUN" = 1 ]; then
  echo "[dry-run] Would run:"
  echo "  ${KCTL[*]} apply --server-side -f ${MANIFEST_URL}"
  echo "  ${KCTL[*]} -n cnpg-system rollout status deployment/cnpg-controller-manager --timeout=300s"
  exit 0
fi

command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl is required" >&2; exit 1; }

"${KCTL[@]}" apply --server-side -f "$MANIFEST_URL"
echo "==> Waiting for CloudNativePG controller-manager..."
"${KCTL[@]}" -n cnpg-system rollout status deployment/cnpg-controller-manager --timeout=300s
echo "==> Verifying Cluster CRD..."
"${KCTL[@]}" get crd clusters.postgresql.cnpg.io >/dev/null
echo "CloudNativePG v${CNPG_VERSION} is ready. Next: make deploy-postgres-ha ENV_FILE=infrastructure.env"
