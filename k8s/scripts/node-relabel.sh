#!/usr/bin/env bash
# =============================================================================
# node-relabel.sh — Modifie à chaud le profil (label numan.io/node-profile)
# et/ou le taint dédié (numan.io/dedicated) d'un nœud Kubernetes existant.
#
# Usage :
#   ./node-relabel.sh --node srv-de-02 --profile websocket --dedicated
#   ./node-relabel.sh --node srv-de-02 --profile all-in-one --clear-dedicated
# =============================================================================
set -euo pipefail

ALLOWED_PROFILES="all-in-one ingress api websocket worker scheduler watermark mail db db-sql db-mongo db-redis storage"

NODE_NAME=""
PROFILE=""
DATACENTER=""
DEDICATED="false"
CLEAR_DEDICATED="false"
KUBE_CTX=""
DRY_RUN="false"

usage() {
  cat <<EOF
Usage: $(basename "$0") --node <node-name> [options]

Options:
  --node <name>         Nom du nœud Kubernetes (kubectl get nodes) [REQUIS]
  --profile <profile>   Nouveau profil : ${ALLOWED_PROFILES}
  --datacenter <id>     Met à jour le label numan.io/datacenter
  --dedicated           Applique le taint numan.io/dedicated=<profile>:NoSchedule (requiert --profile)
  --clear-dedicated     Retire le taint numan.io/dedicated:NoSchedule du nœud
  --context <ctx>       Contexte kubectl cible
  --dry-run             Affiche les commandes kubectl sans les exécuter
  -h, --help            Affiche cette aide
EOF
}

is_valid_profile() {
  local target="$1"
  for p in $ALLOWED_PROFILES; do
    if [[ "$p" == "$target" ]]; then
      return 0
    fi
  done
  return 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --node) NODE_NAME="${2:-}"; shift 2 ;;
    --profile) PROFILE="${2:-}"; shift 2 ;;
    --datacenter) DATACENTER="${2:-}"; shift 2 ;;
    --dedicated) DEDICATED="true"; shift ;;
    --clear-dedicated) CLEAR_DEDICATED="true"; shift ;;
    --context) KUBE_CTX="${2:-}"; shift 2 ;;
    --dry-run) DRY_RUN="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Erreur : option inconnue '$1'" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ -z "$NODE_NAME" ]]; then
  echo "Erreur : --node est requis." >&2
  usage >&2
  exit 2
fi

if [[ -z "$PROFILE" && -z "$DATACENTER" && "$CLEAR_DEDICATED" != "true" ]]; then
  echo "Erreur : spécifiez au moins --profile, --datacenter ou --clear-dedicated." >&2
  exit 2
fi

if [[ -n "$PROFILE" ]] && ! is_valid_profile "$PROFILE"; then
  echo "Erreur : profil invalide '$PROFILE'. Valeurs autorisées : $ALLOWED_PROFILES" >&2
  exit 2
fi

if [[ -n "$DATACENTER" && ! "$DATACENTER" =~ ^[a-z0-9][a-z0-9-]{1,31}$ ]]; then
  echo "Erreur : --datacenter invalide '$DATACENTER'." >&2
  exit 2
fi

if [[ "$DEDICATED" == "true" && -z "$PROFILE" ]]; then
  echo "Erreur : --dedicated requiert --profile." >&2
  exit 2
fi

KUBECTL=(kubectl)
if [[ -n "$KUBE_CTX" ]]; then
  KUBECTL+=(--context "$KUBE_CTX")
fi

run_cmd() {
  if [[ "$DRY_RUN" == "true" ]]; then
    echo "[dry-run] $*"
  else
    "$@"
  fi
}

LABELS=()
if [[ -n "$PROFILE" ]]; then
  LABELS+=("numan.io/node-profile=${PROFILE}")
fi
if [[ -n "$DATACENTER" ]]; then
  LABELS+=("numan.io/datacenter=${DATACENTER}")
fi

if [[ ${#LABELS[@]} -gt 0 ]]; then
  run_cmd "${KUBECTL[@]}" label node "$NODE_NAME" "${LABELS[@]}" --overwrite
fi

if [[ "$CLEAR_DEDICATED" == "true" ]]; then
  if [[ "$DRY_RUN" == "true" ]]; then
    echo "[dry-run] ${KUBECTL[*]} taint nodes ${NODE_NAME} numan.io/dedicated:NoSchedule-"
  else
    "${KUBECTL[@]}" taint nodes "$NODE_NAME" "numan.io/dedicated:NoSchedule-" 2>/dev/null || true
  fi
fi

if [[ "$DEDICATED" == "true" ]]; then
  run_cmd "${KUBECTL[@]}" taint nodes "$NODE_NAME" "numan.io/dedicated=${PROFILE}:NoSchedule" --overwrite
fi

echo "Profil du nœud '${NODE_NAME}' mis à jour."
