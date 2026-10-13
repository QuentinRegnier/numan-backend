#!/usr/bin/env bash
# Self-test of the templating system. Needs python3+PyYAML, envsubst, kustomize/kubectl and (for the schema step)
# kubeconform with network access to the schema catalogs. Needs NO cluster and NO secrets: it uses tests/sample.env
# (reserved placeholder names) and writes only to a temporary directory.
#
#   make test        (or scripts/selftest.sh)
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1
# shellcheck source=scripts/lib.sh
. scripts/lib.sh

T="$(mktemp -d)"; trap 'rm -rf "$T"' EXIT
pass=0; failn=0
ok()  { echo "PASS  $*"; pass=$((pass+1)); }
bad() { echo "FAIL  $*"; failn=$((failn+1)); }
tree_hash() { LC_ALL=C find . -type f -not -path './build/*' -not -path './.git/*' -not -path './__pycache__/*' -not -path './scripts/__pycache__/*' | LC_ALL=C sort | xargs sha256sum | sha256sum | cut -d' ' -f1; }

SRC_BEFORE="$(tree_hash)"

echo "== 1. lint (T == E, syntax rules, collisions)"
python3 scripts/envtool.py lint && ok "lint" || bad "lint"

echo "== 2. render with sample values"
if bash scripts/render.sh --env-file tests/sample.env --out "$T/a" >"$T/a.log" 2>&1; then ok "render succeeds"; else bad "render failed"; tail -20 "$T/a.log"; fi

echo "== 3. determinism"
bash scripts/render.sh --env-file tests/sample.env --out "$T/b" >"$T/b.log" 2>&1
h1="$(grep '^hash=' "$T/a/.render.meta" | cut -d= -f2)"; h2="$(grep '^hash=' "$T/b/.render.meta" | cut -d= -f2)"
[ -n "$h1" ] && [ "$h1" = "$h2" ] && ok "identical inputs -> identical output (hash ${h1:0:12})" || bad "rendering is not deterministic ($h1 vs $h2)"
diff -r "$T/a/rendered" "$T/b/rendered" >/dev/null && ok "rendered trees identical" || bad "rendered trees differ"

echo "== 4. sources untouched"
[ "$(tree_hash)" = "$SRC_BEFORE" ] && ok "source templates unchanged by rendering" || bad "rendering modified the source tree"

echo "== 5. runtime references survive rendering"
C="$T/a/manifests/core.yaml"
grep -q 'host=$(POSTGRES_HOST) user=$(POSTGRES_USER) password=$(POSTGRES_PASSWORD)' "$C" && ok "Kubernetes \$(VAR) DSN reference intact" || bad "DSN reference altered"
grep -q 'exec redis-server' "$C" && grep -q -- '--requirepass "$REDIS_PASSWORD"' "$C" && ok "shell \$REDIS_PASSWORD in Redis args intact" || bad "redis args altered"
grep -q 'set -- $hp' "$C" && ok "shell snippets intact" || bad "shell snippet altered"
! grep -rnE '\$\{[A-Za-z_]' "$T/a/rendered" "$T/a/manifests" >/dev/null && ok "no \${...} placeholder left" || bad "unresolved placeholder"

echo "== 6. schema validation + consistency of rendered output"
if command -v kubeconform >/dev/null 2>&1; then
  if bash scripts/validate.sh --out "$T/a" >"$T/v.log" 2>&1; then ok "validate.sh passes on rendered sample"; else bad "validate.sh failed"; tail -25 "$T/v.log"; fi
else echo "SKIP  kubeconform not installed"; fi

echo "== 7. gateway removal and routing"
python3 - "$C" <<'PY' && ok "ingress routing verified in rendered output" || bad "ingress routing check failed"
import sys, yaml
docs = [d for d in yaml.safe_load_all(open(sys.argv[1])) if d]
ing = {d["metadata"]["name"]: d for d in docs if d["kind"] == "Ingress"}
def backend(name, path):
    for r in ing[name]["spec"]["rules"]:
        for p in r["http"]["paths"]:
            if p["path"] == path:
                s = p["backend"]["service"]; return s["name"], s["port"]["number"]
assert backend("numan-https-app", "/") == ("api", 8080), backend("numan-https-app", "/")
assert backend("numan-https-app", "/ws") == ("api", 8080)
assert backend("numan-https-watermark", "/process") == ("watermark", 8080)
assert backend("numan-http-redirect", "/") == ("api", 8080)
red = ing["numan-http-redirect"]["metadata"]["annotations"]
assert red["traefik.ingress.kubernetes.io/router.entrypoints"] == "web"
assert red["traefik.ingress.kubernetes.io/router.middlewares"] == "numan-redirect-https@kubernetescrd"
mw = {d["metadata"]["name"]: d for d in docs if d["kind"] == "Middleware"}
assert mw["redirect-https"]["spec"]["redirectScheme"] == {"scheme": "https", "permanent": True}
bufs = [n for n, i in ing.items() if "limit-body-20m" in i["metadata"]["annotations"].get("traefik.ingress.kubernetes.io/router.middlewares", "")]
assert bufs == ["numan-https-watermark"], bufs        # buffering never sits in front of / or /ws (media streaming)
for d in docs:
    assert "gateway" not in d["metadata"]["name"], d["metadata"]["name"]
PY
grep -rIn 'edge-gateway\|nginx-gateway' "$T/a/manifests" "$T/a/rendered" >/dev/null && bad "gateway reference in rendered output" || ok "no gateway reference in rendered output"

echo "== 8. the unrendered source tree cannot be built or applied"
if kbuild . >/dev/null 2>"$T/guard.err"; then bad "source tree built without rendering"; else grep -q '__TEMPLATE_NOT_RENDERED__' "$T/guard.err" && ok "guard blocks building the unrendered tree" || bad "guard message missing"; fi

echo "== 9. strict mode refuses reserved placeholder domains"
bash scripts/render.sh --env-file tests/sample.env --strict --out "$T/s" >"$T/s.log" 2>&1 && bad "strict render accepted placeholder domains" || ok "strict render rejected the sample (placeholder domains)"

echo "== 10. shell environment cannot alter the output"
NUMAN_DOMAIN=evil.shell.test API_MAX_REPLICAS=9 HOME_PROBE=1 bash scripts/render.sh --env-file tests/sample.env --out "$T/e" >/dev/null 2>&1
h3="$(grep '^hash=' "$T/e/.render.meta" | cut -d= -f2)"
[ "$h3" = "$h1" ] && ok "exported shell variables ignored (output identical)" || bad "shell environment changed the output"

echo "== 11. negative cases (every one must FAIL the render)"
neg() { # neg <description> <NAME=value (replaces a sample value) | +NAME=value (appends, even if duplicate) | -NAME (deletes)>
  local f="$T/neg.env" o="$2"
  case "$o" in
    -*) grep -v "^${o#-}=" tests/sample.env > "$f";;
    +*) cp tests/sample.env "$f"; printf '%s\n' "${o#+}" >> "$f";;
    *)  grep -v "^${o%%=*}=" tests/sample.env > "$f"; printf '%s\n' "$o" >> "$f";;
  esac
  if bash scripts/render.sh --env-file "$f" --out "$T/n" >"$T/n.log" 2>&1; then bad "accepted: $1"
  elif grep -qE 'ENV ERROR|LINT ERROR' "$T/n.log"; then ok "rejected: $1  [$(grep -m1 -E 'ENV ERROR|LINT ERROR' "$T/n.log" | cut -c11-120)]"
  else bad "failed for the WRONG reason: $1"; tail -3 "$T/n.log"; fi
}
neg "missing required NUMAN_DOMAIN" "-NUMAN_DOMAIN"
neg "required value still __REQUIRED__" "API_IMAGE_TAG=__REQUIRED__"
neg "image tag latest" "API_IMAGE_TAG=latest"
neg "invalid quantity (GB)" "API_MEMORY_LIMIT=1GB"
neg "request above limit" "API_CPU_REQUEST=4"
neg "min replicas above max" "API_MIN_REPLICAS=5"$'\n'"API_MAX_REPLICAS=3"
neg "non-integer replicas" "WATERMARK_MIN_REPLICAS=two"
neg "invalid cron" "BACKUP_SCHEDULE_REDIS=61 25 * * *"
neg "invalid time zone" "SYSTEM_TIMEZONE=Mars/Olympus"
neg "invalid bool" "HSTS_INCLUDE_SUBDOMAINS=yes"
neg "unknown variable (typo)" "NUMAN_DOMIAN=x.test"
neg "dollar sign in value" 'STORAGE_CLASS=local$path'
neg "command substitution in value" 'STORAGE_CLASS=$(touch /tmp/selftest-pwn)'
neg "retention days without object lock" "MINIO_COLD_RETENTION_DAYS=365"
neg "bad hostname" "NUMAN_DOMAIN=Not_A_Host"
neg "bad e-mail" "ACME_EMAIL=nobody"
neg "PVC sum above quota" "PVC_SIZE_POSTGRES=2Ti"
neg "duplicate variable" "+NUMAN_DOMAIN=another.selftest.invalid"
[ -e /tmp/selftest-pwn ] && bad "command substitution was EXECUTED" || ok "no template/value content was executed"

echo "== 12. linter catches template defects (temporary copy of the repo)"
cp -r . "$T/repo" && rm -rf "$T/repo/build"
lintfail() { if (cd "$T/repo" && python3 scripts/envtool.py lint >/dev/null 2>&1); then bad "lint accepted: $1"; else ok "lint rejected: $1"; fi; }
( cd "$T/repo" && sed -i 's/\${API_CPU_LIMIT}/${API_CPU_LIMT}/' 05-apps/api.yaml ); lintfail "undefined variable used in a manifest"
cp 05-apps/api.yaml "$T/repo/05-apps/api.yaml"
( cd "$T/repo" && echo 'UNUSED_THING=1' >> infrastructure.env.template ); lintfail "variable without annotations / never used"
cp infrastructure.env.template "$T/repo/infrastructure.env.template"
( cd "$T/repo" && echo '# ${target%%:*}' >> 03-databases/postgresql.yaml ); lintfail "brace expansion that is not a plain deployment variable name"

[ "$(tree_hash)" = "$SRC_BEFORE" ] && ok "source tree still untouched after all tests" || bad "source tree modified during tests"
echo; echo "RESULT: $pass passed, $failn failed"; [ "$failn" = 0 ]
