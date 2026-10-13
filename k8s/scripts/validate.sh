#!/usr/bin/env bash
# Validate the RENDERED manifests (run `make render` / scripts/render.sh first). Offline unless noted.
#
#   scripts/validate.sh [--out DIR] [--check-images]
#
# Checks: render stamp matches the files on disk (nothing edited since render) · kubeconform --strict against the
# Kubernetes version in KUBERNETES_VERSION (Traefik/cert-manager CRD schemas from the datree catalog) · cross-references,
# quota fit and Redis memory consistency (scripts/check-refs.py) · hard-coded-secret scan · CLEAN_DB_ON_STARTUP guard ·
# optional image existence (skopeo) · server-side dry-run when the cluster is reachable AND is the expected context.
# On success it writes DIR/.validated (= render hash), which scripts/deploy.sh requires.
#
# LIMITS: proves schemas are valid and objects consistent. It does NOT prove images start under the configured UIDs, probes
# pass, the CNI enforces policies, Traefik routes as intended or backups restore (scripts/verify.sh + restore drills do).
set -euo pipefail
cd "$(dirname "$0")/.."
# shellcheck source=scripts/lib.sh
. scripts/lib.sh

OUT=build; CHECK_IMAGES=0
while [ $# -gt 0 ]; do
  case "$1" in --out) OUT="${2:?}"; shift 2;; --check-images) CHECK_IMAGES=1; shift;; *) die "unknown argument $1";; esac
done
need kubeconform "https://github.com/yannh/kubeconform"; need python3 "checks"
[ -f "$OUT/.render.meta" ] || die "nothing rendered in $OUT (run: make render)"
rm -f "$OUT/.validated"
want="$(grep '^hash=' "$OUT/.render.meta" | cut -d= -f2)"
have="$(render_hash "$OUT")"
[ "$want" = "$have" ] || die "rendered files changed after rendering (stamp mismatch): re-run make render"

K8S_VERSION="$(read_var KUBERNETES_VERSION "$OUT")"
SCHEMAS=(-schema-location default -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json')
fail=0

echo "==> kubeconform (Kubernetes ${K8S_VERSION}, strict)"
for m in core tls admin; do
  kubeconform -strict -summary -kubernetes-version "$K8S_VERSION" "${SCHEMAS[@]}" "$OUT/manifests/$m.yaml" || fail=1
done
# the optional HelmChartConfig is a K3s CRD without a published schema: structural YAML check only
python3 -c "import sys,yaml; list(yaml.safe_load_all(open(sys.argv[1])))" "$OUT/manifests/traefik.yaml" || fail=1
cp 01-config/secret-templates.yaml "$OUT/.secret-templates.check.yaml"
kubeconform -strict -summary -kubernetes-version "$K8S_VERSION" "${SCHEMAS[@]}" "$OUT/.secret-templates.check.yaml" || fail=1
rm -f "$OUT/.secret-templates.check.yaml"

echo "==> cross-reference, quota and memory checks"
python3 scripts/check-refs.py 01-config/secret-templates.yaml < "$OUT/manifests/core.yaml" || fail=1

echo "==> guards"
grep -q 'CLEAN_DB_ON_STARTUP: "false"' "$OUT/manifests/core.yaml" || { echo "ERROR: CLEAN_DB_ON_STARTUP is not \"false\" in the rendered ConfigMap"; fail=1; }
if grep -nE '(password|secret|token)[A-Za-z_]*: *"?[A-Za-z0-9+/=_-]{24,}' "$OUT"/manifests/*.yaml | grep -v 'secretName\|secretKeyRef'; then
  echo "ERROR: possible hard-coded secret in rendered output"; fail=1
fi
if grep -n 'edge-gateway' "$OUT"/manifests/*.yaml; then echo "ERROR: obsolete gateway reference in rendered output"; fail=1; fi
if grep -nE ':latest(\s|$)' "$OUT"/manifests/*.yaml; then echo "ERROR: mutable :latest image tag in rendered output"; fail=1; fi

if [ "$CHECK_IMAGES" = 1 ]; then
  need skopeo "image existence check"
  echo "==> verifying every image reference exists in its registry"
  grep -hE '^\s+(-\s+)?image:' "$OUT"/manifests/core.yaml "$OUT"/manifests/admin.yaml | awk '{print $NF}' | sort -u | while read -r img; do
    if skopeo inspect --no-tags "docker://$img" >/dev/null 2>&1; then echo "  ok      $img"; else echo "  MISSING $img"; echo 1 > "$OUT/.imgfail"; fi
  done
  if [ -f "$OUT/.imgfail" ]; then rm -f "$OUT/.imgfail"; fail=1; fi
fi

CTX="$(read_var KUBE_CONTEXT "$OUT")"
if command -v kubectl >/dev/null 2>&1 && [ "$(kubectl config current-context 2>/dev/null || true)" = "$CTX" ] && kubectl version --request-timeout=3s >/dev/null 2>&1; then
  echo "==> server-side dry-run on context $CTX (errors about not-yet-existing namespace/secrets/CRDs are expected before the first deploy)"
  kubectl apply --dry-run=server -f "$OUT/manifests/core.yaml" >/dev/null 2>&1 && echo "  server dry-run OK" || echo "  server dry-run reported errors (run it manually to read them)"
else
  echo "==> server-side dry-run skipped (current kube context is not the configured KUBE_CONTEXT or the cluster is unreachable)"
fi

if [ "$fail" = 0 ]; then
  echo "$have" > "$OUT/.validated"
  echo "VALIDATION PASSED (static checks only) - stamp written"
else
  echo "VALIDATION FAILED"; exit 1
fi
