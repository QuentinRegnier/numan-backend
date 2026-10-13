#!/usr/bin/env bash
# Controlled access to the optional admin UIs (08-operations/admin-tools must be applied first).
#   scripts/admin.sh on  <pgadmin|mongo-express|redis-commander>   scale to 1 and wait
#   scripts/admin.sh forward <tool> [local-port]                  open a tunnel (Ctrl-C to close); then browse http://localhost:PORT
#   scripts/admin.sh off <tool|all>                               scale to 0
set -euo pipefail
NS=numan
declare -A PORT=([pgadmin]=8080 [mongo-express]=8081 [redis-commander]=8081)
cmd="${1:-}"; tool="${2:-}"
case "$cmd" in
  on)  [ -n "${PORT[$tool]:-}" ] || { echo "unknown tool"; exit 1; }
       kubectl -n $NS scale deployment/"$tool" --replicas=1
       kubectl -n $NS rollout status deployment/"$tool" --timeout=300s
       echo "Now: scripts/admin.sh forward $tool"; echo "REMEMBER to switch it off: scripts/admin.sh off $tool";;
  forward) [ -n "${PORT[$tool]:-}" ] || { echo "unknown tool"; exit 1; }
       lp="${3:-${PORT[$tool]}}"
       echo "http://localhost:$lp  (bound to 127.0.0.1 only)"
       kubectl -n $NS port-forward --address 127.0.0.1 svc/"$tool" "$lp:${PORT[$tool]}";;
  off) if [ "$tool" = all ]; then for t in "${!PORT[@]}"; do kubectl -n $NS scale deployment/"$t" --replicas=0; done
       else kubectl -n $NS scale deployment/"$tool" --replicas=0; fi;;
  *) sed -n '2,5p' "$0"; exit 1;;
esac
