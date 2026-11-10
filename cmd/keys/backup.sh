#!/usr/bin/env bash
set -euo pipefail

APP_ROOT='/srv/apps/go-sites-keys'
SHARED_DIR="$APP_ROOT/shared"
SECRETS_DIR='/srv/secrets/liangz77-keys'
BACKUP_PATH="${1:-}"

if [[ -z "$BACKUP_PATH" ]]; then
  echo "usage: $0 /srv/backups/sqlite/go-sites-keys-YYYY-MM-DD.db" >&2
  exit 2
fi
if [[ -e "$BACKUP_PATH" ]]; then
  echo "backup target already exists: $BACKUP_PATH" >&2
  exit 1
fi

run_user='www-data'
if ! id "$run_user" >/dev/null 2>&1; then
  run_user='caddy'
fi
if ! id "$run_user" >/dev/null 2>&1; then
  run_user='root'
fi

install -d -m 750 -o "$run_user" -g "$run_user" "$(dirname "$BACKUP_PATH")"
runuser -u "$run_user" -- env \
  KEYS_DB_PATH="$SHARED_DIR/data/keys.db" \
  KEYS_DATA_DIR="$SHARED_DIR/data" \
  KEYS_PUBLIC_ORIGIN='https://liangz77.cn' \
  KEYS_COOKIE_SECURE='false' \
  KEYS_MASTER_KEY_FILE="$SECRETS_DIR/master.key" \
  "$APP_ROOT/current/go-sites-keys" -backup "$BACKUP_PATH"
chmod 600 "$BACKUP_PATH"

echo "database backup written: $BACKUP_PATH"
echo "master key is intentionally NOT included; back up $SECRETS_DIR/master.key separately."
