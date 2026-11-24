#!/usr/bin/env bash
# =============================================================================
# cluster-init-dc.sh — Initialise le premier nœud K3s (control-plane) d'un
# datacenter Numan avec les labels normalisés (datacenter, région, profil).
#
# Usage :
#   ./cluster-init-dc.sh --datacenter fr-par --region eu-west-fr --profile all-in-one
#   ./cluster-init-dc.sh --datacenter de-fra --profile db --dedicated --tls-san 203.0.113.10
#   ./cluster-init-dc.sh --datacenter de-fra --profile api --dry-run --json
# =============================================================================
set -euo pipefail

ALLOWED_PROFILES="all-in-one ingress api websocket worker scheduler watermark mail db db-sql db-mongo db-redis storage"

DATACENTER=""
REGION=""
PROFILE="all-in-one"
DEDICATED="false"
DRY_RUN="false"
JSON_OUT="false"
SHOW_TOKEN="false"
TLS_SANS=()

usage() {
  cat <<EOF
Usage: $(basename "$0") --datacenter <id> [options]

Options:
  --datacenter <id>     Identifiant logique du datacenter (ex: fr-par, de-fra) [REQUIS]
  --region <region>     Label topology.kubernetes.io/region (défaut: valeur de --datacenter)
  --profile <profile>   Profil du nœud : ${ALLOWED_PROFILES} (défaut: all-in-one)
  --dedicated           Ajoute le taint numan.io/dedicated=<profile>:NoSchedule
  --tls-san <host|ip>   Ajoute un SAN TLS au certificat API K3s (répétable)
  --show-token          Affiche le node-token en clair dans la sortie (sinon affiche le chemin fichier)
  --json                Sortie JSON structurée pour intégration CLI
  --dry-run             Affiche la commande sans modifier le système
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
    --datacenter) DATACENTER="${2:-}"; shift 2 ;;
    --region) REGION="${2:-}"; shift 2 ;;
    --profile) PROFILE="${2:-}"; shift 2 ;;
    --dedicated) DEDICATED="true"; shift ;;
    --tls-san) TLS_SANS+=("${2:-}"); shift 2 ;;
    --show-token) SHOW_TOKEN="true"; shift ;;
    --json) JSON_OUT="true"; shift ;;
    --dry-run) DRY_RUN="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Erreur : option inconnue '$1'" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ -z "$DATACENTER" ]]; then
  echo "Erreur : --datacenter est requis." >&2
  usage >&2
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

K3S_ARGS=(
  "server"
  "--write-kubeconfig-mode=640"
  "--node-label=numan.io/datacenter=${DATACENTER}"
  "--node-label=numan.io/node-profile=${PROFILE}"
  "--node-label=topology.kubernetes.io/region=${REGION}"
)

if [[ "$DEDICATED" == "true" ]]; then
  K3S_ARGS+=("--node-taint=numan.io/dedicated=${PROFILE}:NoSchedule")
fi

for san in "${TLS_SANS[@]+"${TLS_SANS[@]}"}"; do
  if [[ -n "$san" ]]; then
    K3S_ARGS+=("--tls-san=${san}")
  fi
done

EXEC_STR="${K3S_ARGS[*]}"

if [[ "$DRY_RUN" == "true" ]]; then
  if [[ "$JSON_OUT" == "true" ]]; then
    cat <<EOF
{"status":"dry_run","datacenter":"${DATACENTER}","region":"${REGION}","profile":"${PROFILE}","dedicated":${DEDICATED},"install_k3s_exec":"${EXEC_STR}"}
EOF
  else
    echo "[dry-run] curl -sfL https://get.k3s.io | INSTALL_K3S_EXEC=\"${EXEC_STR}\" sh -"
  fi
  exit 0
fi

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "Erreur : ce script doit être exécuté en root (ou via sudo) pour installer K3s." >&2
  exit 1
fi

echo "Initialisation du cluster K3s pour le datacenter '${DATACENTER}' (profil=${PROFILE}, région=${REGION})..."
curl -sfL https://get.k3s.io | INSTALL_K3S_EXEC="${EXEC_STR}" sh -

TOKEN_FILE="/var/lib/rancher/k3s/server/node-token"
KUBECONFIG_FILE="/etc/rancher/k3s/k3s.yaml"
SERVER_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
API_ENDPOINT="https://${SERVER_IP:-127.0.0.1}:6443"

TOKEN_VALUE="<read from ${TOKEN_FILE}>"
if [[ "$SHOW_TOKEN" == "true" && -r "$TOKEN_FILE" ]]; then
  TOKEN_VALUE="$(tr -d '\r\n' < "$TOKEN_FILE")"
fi

if [[ "$JSON_OUT" == "true" ]]; then
  cat <<EOF
{"status":"initialized","datacenter":"${DATACENTER}","region":"${REGION}","profile":"${PROFILE}","dedicated":${DEDICATED},"api_endpoint":"${API_ENDPOINT}","kubeconfig":"${KUBECONFIG_FILE}","token_file":"${TOKEN_FILE}","token":"${TOKEN_VALUE}"}
EOF
else
  echo "Cluster K3s initialisé avec succès."
  echo "  Datacenter   : ${DATACENTER}"
  echo "  Profil nœud  : ${PROFILE} (dedicated=${DEDICATED})"
  echo "  API Endpoint : ${API_ENDPOINT}"
  echo "  Kubeconfig   : ${KUBECONFIG_FILE}"
  echo "  Node Token   : ${TOKEN_VALUE}"
  echo ""
  echo "Pour ajouter un 2e serveur dans ce datacenter :"
  echo "  ./node-join-dc.sh --server ${API_ENDPOINT} --token-file ${TOKEN_FILE} --datacenter ${DATACENTER} --profile api"
fi
