#!/usr/bin/env bash
# =============================================================================
# node-join-dc.sh — Ajoute un nouveau serveur Linux comme nœud agent K3s dans
# un datacenter existant avec le profil choisi (api, websocket, worker, db, ...).
#
# Usage :
#   ./node-join-dc.sh --server https://10.0.0.10:6443 --token-file /root/dc.token \
#     --datacenter de-fra --profile websocket --dedicated
# =============================================================================
set -euo pipefail

ALLOWED_PROFILES="all-in-one ingress api websocket worker scheduler watermark mail db db-sql db-mongo db-redis storage"

SERVER_URL=""
TOKEN=""
TOKEN_FILE=""
DATACENTER=""
REGION=""
PROFILE=""
DEDICATED="false"
DRY_RUN="false"

usage() {
  cat <<EOF
Usage: $(basename "$0") --server <url> (--token <tok> | --token-file <path>) --datacenter <id> --profile <profile> [options]

Options:
  --server <url>        URL de l'API K3s du control-plane (ex: https://10.0.0.10:6443) [REQUIS]
  --token <token>       Jeton K3s de jonction (ou utiliser --token-file / env K3S_TOKEN)
  --token-file <path>   Fichier contenant le jeton K3s (recommandé pour éviter ps/history)
  --datacenter <id>     Identifiant logique du datacenter cible (ex: fr-par, de-fra) [REQUIS]
  --region <region>     Label topology.kubernetes.io/region (défaut: valeur de --datacenter)
  --profile <profile>   Profil du nœud : ${ALLOWED_PROFILES} [REQUIS]
  --dedicated           Ajoute le taint numan.io/dedicated=<profile>:NoSchedule
  --dry-run             Affiche la commande sans modifier le système (jeton masqué)
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
    --server) SERVER_URL="${2:-}"; shift 2 ;;
    --token) TOKEN="${2:-}"; shift 2 ;;
    --token-file) TOKEN_FILE="${2:-}"; shift 2 ;;
    --datacenter) DATACENTER="${2:-}"; shift 2 ;;
    --region) REGION="${2:-}"; shift 2 ;;
    --profile) PROFILE="${2:-}"; shift 2 ;;
    --dedicated) DEDICATED="true"; shift ;;
    --dry-run) DRY_RUN="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Erreur : option inconnue '$1'" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ -z "$TOKEN" && -n "$TOKEN_FILE" ]]; then
  if [[ "$DRY_RUN" == "true" ]]; then
    TOKEN="dry-run-placeholder"
  elif [[ -r "$TOKEN_FILE" ]]; then
    TOKEN="$(tr -d '\r\n' < "$TOKEN_FILE")"
  else
    echo "Erreur : impossible de lire --token-file '$TOKEN_FILE'." >&2
    exit 2
  fi
fi

if [[ -z "$TOKEN" && -n "${K3S_TOKEN:-}" ]]; then
  TOKEN="$K3S_TOKEN"
fi

if [[ -z "$SERVER_URL" || -z "$TOKEN" || -z "$DATACENTER" || -z "$PROFILE" ]]; then
  echo "Erreur : --server, --token (ou --token-file), --datacenter et --profile sont requis." >&2
  usage >&2
  exit 2
fi

if [[ ! "$SERVER_URL" =~ ^https:// ]]; then
  echo "Erreur : --server doit commencer par https:// (reçu: '$SERVER_URL')." >&2
  exit 2
fi

if [[ ! "$DATACENTER" =~ ^[a-z0-9][a-z0-9-]{1,31}$ ]]; then
  echo "Erreur : --datacenter doit respecter ^[a-z0-9][a-z0-9-]{1,31}$ (reçu: '$DATACENTER')." >&2
  exit 2
fi

if ! is_valid_profile "$PROFILE"; then
  echo "Erreur : profil invalide '$PROFILE'. Valeurs autorisées : $ALLOWED_PROFILES" >&2
  exit 2
fi

if [[ -z "$REGION" ]]; then
  REGION="$DATACENTER"
fi

AGENT_ARGS=(
  "agent"
  "--node-label=numan.io/datacenter=${DATACENTER}"
  "--node-label=numan.io/node-profile=${PROFILE}"
  "--node-label=topology.kubernetes.io/region=${REGION}"
)

if [[ "$DEDICATED" == "true" ]]; then
  AGENT_ARGS+=("--node-taint=numan.io/dedicated=${PROFILE}:NoSchedule")
fi

EXEC_STR="${AGENT_ARGS[*]}"

if [[ "$DRY_RUN" == "true" ]]; then
  echo "[dry-run] curl -sfL https://get.k3s.io | K3S_URL=\"${SERVER_URL}\" K3S_TOKEN=\"***REDACTED***\" INSTALL_K3S_EXEC=\"${EXEC_STR}\" sh -"
  exit 0
fi

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "Erreur : ce script doit être exécuté en root (ou via sudo) pour joindre le cluster K3s." >&2
  exit 1
fi

echo "Jonction du serveur au datacenter '${DATACENTER}' (${SERVER_URL}) avec le profil '${PROFILE}' (dedicated=${DEDICATED})..."
curl -sfL https://get.k3s.io | K3S_URL="${SERVER_URL}" K3S_TOKEN="${TOKEN}" INSTALL_K3S_EXEC="${EXEC_STR}" sh -
echo "Nœud agent K3s rattaché avec succès au datacenter '${DATACENTER}'."
