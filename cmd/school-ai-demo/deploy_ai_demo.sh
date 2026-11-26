#!/usr/bin/env bash
# Narrow, committed-source deployment. No Go service restart or database writes.
set -euo pipefail
cd "$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
commit=$(git rev-parse HEAD)
base=${1:-HEAD^} # commit whose Caddyfile/data.js must match production
host=${AI_DEMO_SSH_HOST:-root@49.232.129.114}
key=${AI_DEMO_SSH_KEY:-$HOME/.ssh/keychain_deploy_ed25519}
ssh_opts=(-i "$key" -o BatchMode=yes -o ConnectTimeout=15)
git diff --quiet HEAD -- Caddyfile data.js cmd/school-ai-demo docs/ai-demo-deployment.md
id="$(date +%Y%m%d-%H%M%S)-${commit:0:10}"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
git archive "$commit:cmd/school-ai-demo/web" | gzip >"$tmp/web.tar.gz"
git show "$commit:Caddyfile" >"$tmp/site.caddy"
git show "$commit:data.js" >"$tmp/data.js"
git show "$base:Caddyfile" >"$tmp/expected-site.caddy"
git show "$base:data.js" >"$tmp/expected-data.js"
remote="/tmp/ai-demo-$id"
ssh "${ssh_opts[@]}" "$host" "mkdir -m 700 '$remote'"
scp -q "${ssh_opts[@]}" "$tmp/"* "$host:$remote/"
ssh "${ssh_opts[@]}" "$host" bash -s -- "$id" "$commit" "$remote" <<'REMOTE'
set -euo pipefail
id=$1; commit=$2; stage=$3
site=/etc/caddy/sites-enabled/liangz77.cn.caddy
nav=$(readlink -f /srv/sites/liangz77.cn/current)/data.js
app=/srv/apps/ai-demo
cmp "$stage/expected-site.caddy" "$site"
cmp "$stage/expected-data.js" "$nav"
if [ -e "$app/current" ] && [ ! -L "$app/current" ]; then echo 'Refusing to replace non-symlink current'; exit 1; fi
old=$(readlink "$app/current" || true)
backup="$app/backups/$id"
release="$app/releases/$id"
mkdir -p "$backup" "$release"
cp -a "$site" "$backup/site.caddy"
cp -a "$nav" "$backup/data.js"
printf '%s\n' "$old" >"$backup/previous-target"
printf '%s\n' "$commit" >"$backup/commit"
tar -xzf "$stage/web.tar.gz" --no-same-owner -C "$release"
printf '%s\n' "$commit" >"$release/REVISION"
chmod -R a+rX "$release"
caddy validate --config "$stage/site.caddy" --adapter caddyfile
rollback() {
  echo 'Deployment failed; restoring only this release changes' >&2
  cp -a "$backup/site.caddy" "$site"
  cp -a "$backup/data.js" "$nav"
  if [ -n "$old" ]; then ln -s "$old" "$app/current.rollback"; mv -Tf "$app/current.rollback" "$app/current"; else rm -f "$app/current"; fi
  caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile && systemctl reload caddy
  curl -fsS -o /dev/null https://liangz77.cn/school-ai-demo/
}
trap rollback ERR
ln -s "$release" "$app/current.next"
mv -Tf "$app/current.next" "$app/current"
install -m 644 "$stage/site.caddy" "$site"
caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
systemctl reload caddy
for path in '' customer-email/ quotation/ order-validation/ lesson-plan.html essay-review.html math-review.html app.css app.js cases.css cases.js favicon.svg api/state api/templates/enrollment api/essay-review/status api/math-review/status; do
  echo "Checking /ai-demo/$path"
  curl --retry 2 -fsS -o /dev/null "https://liangz77.cn/ai-demo/$path"
done
[ "$(curl -fsS https://liangz77.cn/ai-demo/REVISION)" = "$commit" ]
[ "$(curl -s -o /dev/null -w '%{http_code}' https://liangz77.cn/ai-demo/missing-case/)" = 404 ]
[ "$(curl -s -o /dev/null -w '%{http_code}' https://liangz77.cn/school-ai-demo/)" = 308 ]
# Publish navigation only after the app and APIs work.
install -m 644 "$stage/data.js" "$nav"
systemctl is-active school-ai-demo school-essay-demo caddy
trap - ERR
printf 'DEPLOYED %s\nCOMMIT %s\nBACKUP %s\n' "$release" "$commit" "$backup"
REMOTE
