#!/usr/bin/env bash
# Read-only smoke tests. Prints PASS / FAIL / SKIP per check and exits non-zero on any FAIL.
# Nothing here is pre-recorded: every line is the result of a command run NOW against YOUR cluster.
#   scripts/verify.sh                       in-cluster checks
#   HOST=numan.example.com scripts/verify.sh  + external checks (routing, redirect, HSTS, body limit, TLS)
# Environment: NS (default numan), CURL_OPTS (e.g. "-k" while using staging/self-signed certificates).
set -uo pipefail
NS="${NS:-numan}"; HOST="${HOST:-}"; CURL_OPTS="${CURL_OPTS:-}"
pass=0; failn=0; skip=0
ok()   { echo "PASS  $*"; pass=$((pass+1)); }
bad()  { echo "FAIL  $*"; failn=$((failn+1)); }
sk()   { echo "SKIP  $*"; skip=$((skip+1)); }
inf()  { echo "INFO  $*"; }
chk()  { local d="$1"; shift; if "$@" >/dev/null 2>&1; then ok "$d"; else bad "$d"; fi; }
k()    { kubectl -n "$NS" "$@"; }

echo "== workloads"
for r in statefulset/postgres statefulset/mongo statefulset/redis statefulset/minio-hot statefulset/minio-cold \
         statefulset/api deployment/watermark; do
  chk "rollout complete: $r" k rollout status "$r" --timeout=30s
done
chk "init Job minio-hot-init succeeded" k wait --for=condition=complete job/minio-hot-init --timeout=10s
chk "init Job minio-cold-init succeeded" k wait --for=condition=complete job/minio-cold-init --timeout=10s
echo "restarts (informational):"; k get pods -o custom-columns=POD:.metadata.name,RESTARTS:.status.containerStatuses[*].restartCount --no-headers | sed 's/^/   /'

echo "== datastores (exec inside the datastore pods)"
chk "postgres accepts connections" k exec postgres-0 -- sh -c 'pg_isready -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
chk "postgres authenticated query" k exec postgres-0 -- sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "select 1"'
chk "mongo ping" k exec mongo-0 -- mongosh --quiet --eval 'db.adminCommand("ping").ok'
chk "redis requires auth (unauthenticated PING is refused)" sh -c "k exec redis-0 -- sh -c 'REDISCLI_AUTH= redis-cli ping' 2>&1 | grep -qi NOAUTH"
chk "redis authenticated PING" k exec redis-0 -- sh -c 'redis-cli ping | grep -q PONG'
chk "minio-hot ready"  k exec minio-hot-0  -- sh -c 'wget -qO- http://127.0.0.1:9000/minio/health/ready || curl -fsS http://127.0.0.1:9000/minio/health/ready'
chk "minio-cold ready" k exec minio-cold-0 -- sh -c 'wget -qO- http://127.0.0.1:9000/minio/health/ready || curl -fsS http://127.0.0.1:9000/minio/health/ready'

echo "== NetworkPolicy enforcement (proves the CNI enforces them)"
probe() { # probe <labels> <host> <port>  -> exit 0 if TCP connect works
  kubectl -n "$NS" run "np-probe-$RANDOM" --rm -i --restart=Never --quiet --image=busybox:1.37.0 \
    ${1:+--labels="$1"} --command -- sh -c "nc -z -w 4 $2 $3" 2>/dev/null; }
chk "labelled pod CAN reach postgres:5432" probe "numan.io/uses-postgres=true" postgres 5432
if probe "" postgres 5432; then bad "UNLABELLED pod reached postgres:5432 -> NetworkPolicies are NOT enforced"; else ok "unlabelled pod is blocked from postgres:5432"; fi
if probe "" minio-hot 9000; then bad "unlabelled pod reached minio-hot:9000"; else ok "unlabelled pod is blocked from minio-hot:9000"; fi

echo "== metrics / HPA"
chk "metrics-server answers (kubectl top)" k top pods
k get hpa 2>/dev/null | sed 's/^/   /'

echo "== backups (does NOT prove restorability - run a restore drill, docs/03-OPERATIONS.md)"
k get cronjob -o custom-columns=NAME:.metadata.name,SCHEDULE:.spec.schedule,LAST:.status.lastSuccessfulTime --no-headers | sed 's/^/   /'

echo "== external (HOST=${HOST:-unset})"
if [ -z "$HOST" ]; then sk "set HOST=your.domain for routing/TLS checks"; else
  code() { curl -s -o /dev/null -w '%{http_code}' $CURL_OPTS --max-time 15 "$@"; }
  c=$(code -I "http://$HOST/"); [ "$c" = 301 ] && ok "http -> https redirect (301)" || bad "http redirect expected 301, got $c"
  chk "HSTS header present" sh -c "curl -sI $CURL_OPTS --max-time 15 https://$HOST/ | grep -qi '^strict-transport-security: max-age=31536000'"
  c=$(code "https://$HOST/"); case "$c" in 404|000|502|503|504) bad "GET / -> $c (Traefik 404/5xx = routing broken)";; *) ok "GET / reaches the app (HTTP $c)";; esac
  c=$(code -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' "https://$HOST/ws")
  case "$c" in 101) ok "WebSocket upgrade accepted (101)";; 404|000|502|503|504) bad "/ws -> $c";; *) ok "/ws reaches the API (HTTP $c; app-level rejection without credentials is normal - confirm with a real WS client)";; esac
  c=$(code -X POST "https://$HOST/process"); case "$c" in 404|000|502|503|504) bad "/process -> $c";; *) ok "/process reaches watermark (HTTP $c)";; esac
  c=$(head -c 21000000 /dev/zero | code -X POST --data-binary @- "https://$HOST/process"); [ "$c" = 413 ] && ok "21 MB body rejected (413) on /process" || bad "21 MB body on /process expected 413, got $c"
  c=$(head -c 21000000 /dev/zero | code -X POST --data-binary @- "https://$HOST/")
  if [ "$c" = 413 ]; then ok "21 MB body on / rejected (413)"; else inf "21 MB body on / -> HTTP $c: no edge limit exists for / any more (no proxy hop); confirm the application enforces its own body limit"; fi
  if command -v openssl >/dev/null; then
    end=$(echo | openssl s_client -servername "$HOST" -connect "$HOST:443" 2>/dev/null | openssl x509 -noout -enddate 2>/dev/null)
    [ -n "$end" ] && echo "INFO  certificate $end (issuer: $(echo | openssl s_client -servername "$HOST" -connect "$HOST:443" 2>/dev/null | openssl x509 -noout -issuer 2>/dev/null))" || bad "could not read TLS certificate"
    chk "certificate chain verifies (fails for staging/self-signed)" sh -c "echo | openssl s_client -servername $HOST -connect $HOST:443 -verify_return_error 2>&1 | grep -q 'Verify return code: 0'"
  fi
fi

echo; echo "Pod rescheduling and restore tests are manual by design: docs/03-OPERATIONS.md."
echo "RESULT: $pass passed, $failn failed, $skip skipped"; [ "$failn" = 0 ]
