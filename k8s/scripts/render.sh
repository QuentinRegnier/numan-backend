#!/usr/bin/env bash
# Compile the templates into plain Kubernetes manifests. Never touches the sources, never applies anything.
#
#   scripts/render.sh [--env-file FILE] [--strict] [--out DIR]
#     --env-file  environment file (default: $ENV_FILE or ./infrastructure.env)
#     --strict    also reject reserved placeholder domains/hosts (required by scripts/deploy.sh)
#     --out       build directory (default: ./build)
#
# Pipeline: lint templates -> validate env file -> resolve variables (template defaults for omitted ones) ->
#           substitute with an explicit allowlist in a CLEAN environment (env -i) -> verify nothing unresolved remains ->
#           parse every rendered file as YAML -> kustomize build the rendered tree -> write resolved.env + hash stamp.
# Outputs (all under DIR, git-ignored):  rendered/  manifests/{core,tls,admin,traefik}.yaml  resolved.env  .render.meta
set -euo pipefail
cd "$(dirname "$0")/.."
# shellcheck source=scripts/lib.sh
. scripts/lib.sh

ENV_FILE="${ENV_FILE:-infrastructure.env}"; STRICT=0; OUT=build
while [ $# -gt 0 ]; do
  case "$1" in
    --env-file) ENV_FILE="${2:?}"; shift 2;;
    --strict)   STRICT=1; shift;;
    --out)      OUT="${2:?}"; shift 2;;
    *) die "unknown argument $1";;
  esac
done
need python3 "template engine"; need envsubst "package gettext-base / gettext"
command -v kustomize >/dev/null 2>&1 || command -v kubectl >/dev/null 2>&1 || die "kustomize (or kubectl) is required"
[ -f "$ENV_FILE" ] || die "environment file not found: $ENV_FILE (copy infrastructure.env.template and fill it in)"
umask 077

echo "==> 1/6 lint templates (T == E, syntax, collisions)"
python3 scripts/envtool.py lint
echo "==> 2/6 validate $ENV_FILE"
if [ "$STRICT" = 1 ]; then python3 scripts/envtool.py check-env "$ENV_FILE" --strict-deploy; else python3 scripts/envtool.py check-env "$ENV_FILE"; fi

# Resolved values travel as NAME=VALUE arguments to `env -i`: only allowlisted variables exist for envsubst,
# nothing from the operator's shell can leak into the output. Values are non-secret by construction.
ENVARGS=()
while IFS= read -r -d '' kv; do ENVARGS+=("$kv"); done < <(python3 scripts/envtool.py emit "$ENV_FILE")
[ "${#ENVARGS[@]}" -gt 0 ] || die "no variables resolved"
ALLOW="$(python3 scripts/envtool.py allowlist)"

echo "==> 3/6 substitute (${#ENVARGS[@]} allowlisted variables)"
rm -rf "$OUT/rendered" "$OUT/manifests" "$OUT/.render.meta" "$OUT/.validated"
mkdir -p "$OUT/rendered" "$OUT/manifests"
count=0
while IFS= read -r rel; do
  mkdir -p "$OUT/rendered/$(dirname "$rel")"
  case "$rel" in
    *kustomization.yaml) # the guard line makes the unrendered tree unbuildable; drop it only in the rendered copy
      grep -v '__TEMPLATE_NOT_RENDERED__' "$rel" | env -i PATH="$PATH" LC_ALL=C "${ENVARGS[@]}" envsubst "$ALLOW" > "$OUT/rendered/$rel";;
    *) env -i PATH="$PATH" LC_ALL=C "${ENVARGS[@]}" envsubst "$ALLOW" < "$rel" > "$OUT/rendered/$rel";;
  esac
  count=$((count+1))
done < <(python3 scripts/envtool.py sources)
echo "    $count files rendered"

echo "==> 4/6 verify nothing unresolved remains"
if grep -rnE '\$\{[A-Za-z_][A-Za-z0-9_]*\}|__TEMPLATE_NOT_RENDERED__|__REQUIRED__' "$OUT/rendered"; then
  die "unresolved deployment placeholder(s) in rendered output (listed above)"
fi
python3 - "$OUT/rendered" <<'PY'
import os, sys
try:
    import yaml
except ImportError:
    sys.exit("PyYAML is required (pip install pyyaml)")
bad = 0
for d, _, fs in os.walk(sys.argv[1]):
    for f in sorted(fs):
        p = os.path.join(d, f)
        try:
            list(yaml.safe_load_all(open(p, encoding="utf-8")))
        except yaml.YAMLError as e:
            print(f"INVALID YAML in {p}: {e}"); bad += 1
sys.exit(1 if bad else 0)
PY

echo "==> 5/6 kustomize build of the RENDERED tree"
kbuild "$OUT/rendered"                          > "$OUT/manifests/core.yaml"
kbuild "$OUT/rendered/06-ingress/tls"           > "$OUT/manifests/tls.yaml"
kbuild "$OUT/rendered/08-operations/admin-tools" > "$OUT/manifests/admin.yaml"
cp "$OUT/rendered/06-ingress/optional/traefik-helmchartconfig.yaml" "$OUT/manifests/traefik.yaml"
python3 scripts/envtool.py resolved "$ENV_FILE" > "$OUT/resolved.env"; chmod 600 "$OUT/resolved.env"

echo "==> 6/6 stamp"
H="$(render_hash "$OUT")"
{ echo "hash=$H"; echo "strict=$STRICT"; echo "env_file=$ENV_FILE"; } > "$OUT/.render.meta"
echo "rendered OK  hash=${H:0:16}  strict=$STRICT  ->  $OUT/manifests/{core,tls,admin,traefik}.yaml"
echo "Rendered files hold no secrets by construction, but are build artifacts: never commit them."
