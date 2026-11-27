#!/usr/bin/env bash
# Run locally from go-sites: bash cmd/paseo-relay/stage-rootfs.sh root@49.232.129.114
# Requires crane, a verified SSH host key and an authorized deploy key.
set -euo pipefail
host=${1:?specify deployment SSH target}
image=ghcr.io/getpaseo/paseo-relay@sha256:36820d87bb62c27b1ea64094c40731ca1d6edbf67be602ca7e0de14dca0abb0a
release_id=${image##*:}
config=$(crane config --platform linux/amd64 "$image")
python3 -c 'import json,sys; c=json.load(sys.stdin); sys.exit((c["os"], c["architecture"]) != ("linux", "amd64"))' <<< "$config"
bundle=$(mktemp)
trap 'rm -f -- "$bundle"' EXIT
crane export --platform linux/amd64 "$image" "$bundle"
ssh -o BatchMode=yes -o ConnectTimeout=7 -i "$HOME/.ssh/keychain_deploy_ed25519" "$host" "bash -c '$(cat <<'REMOTE'
set -euo pipefail
umask 022
base=/srv/apps/paseo-relay
release="$base/releases/$1"
[[ $(uname -m) == x86_64 && ! -e "$release" && ! -e "$base/current" ]]
install -d "$base/releases"
stage=$(mktemp -d "$base/releases/.stage.XXXXXXXX")
trap "rm -rf -- \"\$stage\"" EXIT
install -d -m 0755 "$stage/rootfs"
tar --numeric-owner -xf - -C "$stage/rootfs"
test -x "$stage/rootfs/app/bin/paseo_relay"
chmod 0755 "$stage/rootfs"
mv -T "$stage/rootfs" "$release"
ln -s "$release" "$base/current"
printf "%s\n" "$release"
REMOTE
)' -- '$release_id'" < "$bundle"
