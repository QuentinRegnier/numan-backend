#!/usr/bin/env bash
# Creates/updates every Kubernetes Secret from a local, git-ignored env file. No secret ever appears on a command line
# or in a manifest in git. Requires kubectl access to the target cluster.
#
#   cp 01-config/secrets.env.example 01-config/secrets.env && chmod 600 01-config/secrets.env   # fill it
#   scripts/create-secrets.sh [--dry-run] [path/to/secrets.env]
#
# Secrets are only base64 in etcd. Enable K3s encryption at rest (README "Secrets") and keep an encrypted
# copy of secrets.env OFF the cluster (SOPS+age or a password manager).
set -euo pipefail
cd "$(dirname "$0")/.."
DRY=0; ENVFILE="01-config/secrets.env"
for a in "$@"; do case "$a" in --dry-run) DRY=1;; *) ENVFILE="$a";; esac; done
[ -f "$ENVFILE" ] || { echo "missing $ENVFILE"; exit 1; }
perm=$(stat -c '%a' "$ENVFILE" 2>/dev/null || stat -f '%Lp' "$ENVFILE")
[ "$perm" = "600" ] || [ "$perm" = "400" ] || { echo "refusing: $ENVFILE must be chmod 600 (is $perm)"; exit 1; }

declare -A V
while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in ''|'#'*) continue;; esac
  V["${line%%=*}"]="${line#*=}"
done < "$ENVFILE"

WORK="$(mktemp -d)"; chmod 700 "$WORK"; trap 'rm -rf "$WORK"' EXIT
apply_stdin() { if [ "$DRY" = 1 ]; then cat >/dev/null; else kubectl apply -f -; fi; }

# make_secret <secret-name> <DEST_KEY=SRC_VAR>...   (fails if any SRC_VAR is empty)
make_secret() {
  local name="$1"; shift; local f="$WORK/$name.env"; : > "$f"; chmod 600 "$f"
  for pair in "$@"; do
    local dest="${pair%%=*}" src="${pair#*=}"
    [ -n "${V[$src]:-}" ] || { echo "ERROR: $src is empty in $ENVFILE (needed by Secret $name)"; return 1; }
    printf '%s=%s\n' "$dest" "${V[$src]}" >> "$f"
  done
  kubectl create secret generic "$name" -n numan --from-env-file="$f" --dry-run=client -o yaml \
    | kubectl label --local -f - app.kubernetes.io/part-of=numan -o yaml | apply_stdin
  echo "  secret/$name"
}

if [ "$DRY" = 0 ]; then kubectl get namespace numan >/dev/null 2>&1 || kubectl create namespace numan >/dev/null; fi   # labels (Pod Security) are applied by the core deployment
echo "Creating Secrets in namespace numan (dry-run=$DRY)"
make_secret postgres-credentials POSTGRES_USER=POSTGRES_USER POSTGRES_PASSWORD=POSTGRES_PASSWORD
make_secret mongo-credentials MONGO_ROOT_USER=MONGO_ROOT_USER MONGO_ROOT_PASSWORD=MONGO_ROOT_PASSWORD
make_secret redis-credentials REDIS_PASSWORD=REDIS_PASSWORD
make_secret numan-app-secrets JWT_SECRET=JWT_SECRET HMAC_SECRET=HMAC_SECRET WATERMARK_SECRET_KEY=WATERMARK_SECRET_KEY
make_secret numan-watermark-secrets WATERMARK_SECRET_KEY=WATERMARK_SECRET_KEY
for t in HOT COLD; do
  l=$(echo "$t" | tr 'A-Z' 'a-z')
  make_secret "minio-$l-root" MINIO_ROOT_USER=MINIO_${t}_ROOT_USER MINIO_ROOT_PASSWORD=MINIO_${t}_ROOT_PASSWORD
  make_secret "minio-$l-users" S3_ACCESS_KEY=MINIO_${t}_APP_ACCESS_KEY S3_SECRET_KEY=MINIO_${t}_APP_SECRET_KEY \
    BACKUP_ACCESS_KEY=MINIO_${t}_BACKUP_ACCESS_KEY BACKUP_SECRET_KEY=MINIO_${t}_BACKUP_SECRET_KEY
done
make_secret backup-credentials RESTIC_REPOSITORY=RESTIC_REPOSITORY RESTIC_PASSWORD=RESTIC_PASSWORD \
  AWS_ACCESS_KEY_ID=BACKUP_AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY=BACKUP_AWS_SECRET_ACCESS_KEY \
  BACKUP_S3_ENDPOINT=BACKUP_S3_ENDPOINT BACKUP_MIRROR_BUCKET=BACKUP_MIRROR_BUCKET

# Optional admin-tool Secrets: created only when all of their variables are filled in.
if [ -n "${V[PGADMIN_EMAIL]:-}" ] && [ -n "${V[PGADMIN_PASSWORD]:-}" ]; then
  make_secret pgadmin-credentials PGADMIN_DEFAULT_EMAIL=PGADMIN_EMAIL PGADMIN_DEFAULT_PASSWORD=PGADMIN_PASSWORD; fi
if [ -n "${V[MONGO_UI_USER]:-}" ] && [ -n "${V[MONGO_UI_PASSWORD]:-}" ]; then
  make_secret mongo-express-credentials ME_CONFIG_BASICAUTH_USERNAME=MONGO_UI_USER ME_CONFIG_BASICAUTH_PASSWORD=MONGO_UI_PASSWORD; fi
if [ -n "${V[REDIS_UI_USER]:-}" ] && [ -n "${V[REDIS_UI_PASSWORD]:-}" ]; then
  make_secret redis-commander-credentials HTTP_USER=REDIS_UI_USER HTTP_PASSWORD=REDIS_UI_PASSWORD; fi
echo "Done. Changing a Secret does NOT restart pods: run 'kubectl -n numan rollout restart statefulset/api deployment/watermark'."
