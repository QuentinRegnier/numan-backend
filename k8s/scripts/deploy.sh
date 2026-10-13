#!/usr/bin/env bash
# Guarded deployment of RENDERED manifests. Always re-renders and re-validates in the same run, then applies
# build/manifests/<stack>.yaml. Source templates are never applied.
#
#   scripts/deploy.sh [--env-file FILE] [--stack core|tls|admin|traefik] [--dry-run] [--yes]
#     core     (default) the whole application stack
#     tls      ClusterIssuers + Certificate           (needs cert-manager installed)
#     admin    optional admin UIs, still replicas 0   (needs their *-credentials Secrets)
#     traefik  optional cluster-wide Traefik tuning   (kube-system)
#     --dry-run  server-side dry run only, nothing changes
#     --out DIR  build directory (default: ./build)
#     --yes      skip the interactive confirmation (CI/CLI use; the context guard still applies)
set -euo pipefail
cd "$(dirname "$0")/.."
# shellcheck source=scripts/lib.sh
. scripts/lib.sh

ENV_FILE="${ENV_FILE:-infrastructure.env}"; STACK=core; DRY=0; YES=0; OUT=build; NS=numan
while [ $# -gt 0 ]; do
  case "$1" in
    --env-file) ENV_FILE="${2:?}"; shift 2;;
    --stack)    STACK="${2:?}"; shift 2;;
    --out)      OUT="${2:?}"; shift 2;;
    --dry-run)  DRY=1; shift;;
    --yes)      YES=1; shift;;
    *) die "unknown argument $1";;
  esac
done
case "$STACK" in core|tls|admin|traefik) ;; *) die "unknown stack $STACK";; esac
need kubectl "cluster access"

# 1. compile + validate (fails closed); strict mode rejects placeholder domains
bash scripts/render.sh --env-file "$ENV_FILE" --strict --out "$OUT"
bash scripts/validate.sh --out "$OUT"

# 2. the stamps must describe exactly what is on disk
meta_hash="$(grep '^hash=' "$OUT/.render.meta" | cut -d= -f2)"
[ "$(grep '^strict=' "$OUT/.render.meta" | cut -d= -f2)" = 1 ] || die "output was not rendered with --strict"
[ -f "$OUT/.validated" ] && [ "$(cat "$OUT/.validated")" = "$meta_hash" ] && [ "$(render_hash "$OUT")" = "$meta_hash" ] \
  || die "rendered output is not validated (stamp mismatch)"

# 3. cluster guard: never deploy to a context other than the configured one
EXPECTED_CONTEXT="$(read_var KUBE_CONTEXT "$OUT")"
CURRENT_CONTEXT="$(kubectl config current-context)"
[ "$CURRENT_CONTEXT" = "$EXPECTED_CONTEXT" ] || die "current kubectl context is '$CURRENT_CONTEXT' but KUBE_CONTEXT is '$EXPECTED_CONTEXT'"
echo "Cluster context : $CURRENT_CONTEXT"
echo "Server version  : $(kubectl version -o json 2>/dev/null | python3 -c 'import sys,json; print(json.load(sys.stdin)["serverVersion"]["gitVersion"])' 2>/dev/null || echo unknown)"
echo "Stack           : $STACK   (manifest hash ${meta_hash:0:16})"
MANIFEST="$OUT/manifests/$STACK.yaml"

# 4. prerequisites per stack
case "$STACK" in
  core)
    kubectl get crd middlewares.traefik.io >/dev/null || die "Traefik Middleware CRD missing (is K3s' bundled Traefik disabled?)"
    SC="$(read_var STORAGE_CLASS "$OUT")"
    kubectl get storageclass "$SC" >/dev/null || die "StorageClass '$SC' (STORAGE_CLASS) does not exist in this cluster"
    kubectl get apiservice v1beta1.metrics.k8s.io >/dev/null 2>&1 || echo "WARNING: metrics-server not available: HPAs will not work"
    if [ "$DRY" = 0 ]; then kubectl get namespace "$NS" >/dev/null 2>&1 || kubectl create namespace "$NS" >/dev/null; fi
    for s in postgres-credentials mongo-credentials redis-credentials numan-app-secrets numan-watermark-secrets \
             minio-hot-root minio-hot-users minio-cold-root minio-cold-users backup-credentials; do
      kubectl -n "$NS" get secret "$s" >/dev/null 2>&1 || { [ "$DRY" = 1 ] && echo "NOTE: Secret $s missing (dry-run continues)" || die "missing Secret $s -> scripts/create-secrets.sh"; }
    done;;
  tls)     kubectl get crd clusterissuers.cert-manager.io certificates.cert-manager.io >/dev/null || die "cert-manager CRDs missing: install cert-manager first (README)";;
  admin)   for s in pgadmin-credentials mongo-express-credentials redis-commander-credentials; do kubectl -n "$NS" get secret "$s" >/dev/null 2>&1 || die "missing Secret $s (fill the admin variables in secrets.env and run scripts/create-secrets.sh)"; done;;
  traefik) echo "NOTE: this changes the cluster-wide Traefik in kube-system.";;
esac

if [ "$DRY" = 1 ]; then kubectl apply --dry-run=server -f "$MANIFEST"; echo "dry-run complete, nothing applied"; exit 0; fi

# 5. review + confirm
echo "==> kubectl diff (review)"
set +e; kubectl diff -f "$MANIFEST"; rc=$?; set -e
[ "$rc" -le 1 ] || die "kubectl diff failed (exit $rc)"
if [ "$YES" = 0 ]; then read -r -p "Apply to '$CURRENT_CONTEXT'? [type 'yes'] " ans; [ "$ans" = yes ] || die "aborted"; fi

# 6. apply
if [ "$STACK" = core ]; then
  echo "==> removing finished init Jobs (Jobs are immutable; bootstrap is idempotent)"
  kubectl -n "$NS" delete job -l numan.io/job-type=init --ignore-not-found --wait=true
fi
kubectl apply -f "$MANIFEST"

if [ "$STACK" = core ]; then
  echo "==> waiting for datastores"
  for r in postgres mongo redis minio-hot minio-cold; do kubectl -n "$NS" rollout status statefulset/$r --timeout=600s; done
  echo "==> waiting for bucket bootstrap"
  kubectl -n "$NS" wait --for=condition=complete job/minio-hot-init job/minio-cold-init --timeout=900s
  echo "==> waiting for applications"
  kubectl -n "$NS" rollout status statefulset/api --timeout=600s
  kubectl -n "$NS" rollout status deployment/watermark --timeout=600s
  echo "Deployed. Next: make verify   (HOST=<your domain> for the external checks)"
fi
