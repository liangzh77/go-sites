#!/usr/bin/env bash

set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "Please run as root."
  exit 1
fi

DEPLOY_USER="${DEPLOY_USER:-deploy}"
APP_GROUP="${APP_GROUP:-www-data}"
SITE_DOMAINS=(
  "liangz77.cn"
  "tools.liangz77.cn"
  "demo.liangz77.cn"
)

apt-get update
apt-get install -y debian-keyring debian-archive-keyring apt-transport-https curl gnupg ufw sudo

if [[ ! -f /usr/share/keyrings/caddy-stable-archive-keyring.gpg ]]; then
  curl -fsSL https://dl.cloudsmith.io/public/caddy/stable/gpg.key \
    | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
fi

if [[ ! -f /etc/apt/sources.list.d/caddy-stable.list ]]; then
  curl -fsSL https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt \
    | tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null
fi

apt-get update
apt-get install -y caddy

if ! id -u "${DEPLOY_USER}" >/dev/null 2>&1; then
  adduser --disabled-password --gecos "" "${DEPLOY_USER}"
fi

usermod -aG "${APP_GROUP}" "${DEPLOY_USER}"
install -d -m 755 /srv/git /srv/build /srv/sites /srv/apps /srv/backups

for domain in "${SITE_DOMAINS[@]}"; do
  install -d -m 775 "/srv/sites/${domain}"
  install -d -m 775 "/srv/sites/${domain}/releases"
  install -d -m 775 "/srv/sites/${domain}/shared"

  if [[ ! -L "/srv/sites/${domain}/current" && ! -e "/srv/sites/${domain}/current" ]]; then
    mkdir -p "/srv/sites/${domain}/releases/bootstrap"
    ln -sfn "/srv/sites/${domain}/releases/bootstrap" "/srv/sites/${domain}/current"
  fi
done

chown -R root:"${APP_GROUP}" /srv/apps /srv/backups
chown -R "${DEPLOY_USER}":"${APP_GROUP}" /srv/git /srv/build /srv/sites
find /srv/sites -type d -exec chmod 755 {} \;
find /srv/apps -type d -exec chmod 755 {} \;
find /srv/backups -type d -exec chmod 755 {} \;
find /srv/sites -maxdepth 2 -type d \( -path "/srv/sites" -o -path "/srv/sites/*" -o -path "/srv/sites/*/releases" -o -path "/srv/sites/*/shared" \) -exec chmod 775 {} \;

cat >/etc/sudoers.d/deploy-systemctl <<'EOF'
deploy ALL=NOPASSWD: /usr/bin/systemctl reload caddy, /usr/bin/systemctl status caddy
EOF
chmod 440 /etc/sudoers.d/deploy-systemctl

ufw allow OpenSSH
ufw allow 80/tcp
ufw allow 443/tcp
ufw --force enable

systemctl enable caddy
systemctl restart caddy
systemctl status caddy --no-pager

echo "Server initialization completed."
echo "Next steps:"
echo "1. Upload your SSH public key to /home/${DEPLOY_USER}/.ssh/authorized_keys"
echo "2. Copy the repo Caddyfile to /etc/caddy/Caddyfile"
echo "3. Run caddy validate --config /etc/caddy/Caddyfile"
echo "4. Run deploy.ps1 from your local machine"
