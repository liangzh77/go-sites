# Paseo Relay

The official [Paseo Relay](https://github.com/getpaseo/paseo-relay) is separate from `cmd/relay-gateway`. It stores no agent data. Macs and phones connect outbound; each Mac runs its own daemon and is paired separately.

- Paseo relay endpoint: `liangz77.cn:443` with TLS. Paseo 0.17.4 fixes WebSocket traffic at `wss://liangz77.cn/ws`; it cannot accept a URL path in the relay setting.
- Public readiness: `https://liangz77.cn/paseo-relay/health`. No public metrics or plaintext listener.
- Caddy routes only the HTTPS main host and WebSocket upgrades. Relay binds `127.0.0.1:9040` under the `paseo-relay` system account and `paseo-relay.service`.
- Runtime uses the packaged `/app` Erlang release from the pinned linux/amd64 OCI artifact (see `stage-rootfs.sh`), unpacked under `/srv/apps/paseo-relay/releases/`, with `current` pointing to the selected release. No Docker daemon or database is required on this host.

## Deployment

Stage the pinned release with `bash cmd/paseo-relay/stage-rootfs.sh root@49.232.129.114` (requires `crane`). The script stages a first installation and refuses to overwrite `current`; to upgrade, stage the new digest separately and switch the symlink after verification. Install the included unit in `/etc/systemd/system/`, create its non-login system user, then start and enable it. Check `curl -fsS http://127.0.0.1:9040/ready` before exposing it. Merge only the `https://liangz77.cn` Paseo handlers from `Caddyfile` into the live site config, keeping a backup. Validate and reload Caddy, then check public readiness and the main site.

Rollback: remove only the Paseo Caddy handlers (or restore the pre-change backup if there have been no intervening edits), validate/reload, and `systemctl disable --now paseo-relay`. For a release upgrade, switch `current` back to the previous digest and restart the service.

## Adding Macs

Merge `{"enabled":true,"endpoint":"liangz77.cn:443","publicEndpoint":"liangz77.cn:443","useTls":true,"publicUseTls":true}` into `daemon.relay` in `~/.paseo/config.json`; do not replace the rest of the file. Keep a daemon password, restart the worker when no agent is running, and run `paseo daemon pair`. The pairing offer is sensitive: scan locally, don't post or commit it. Existing private-network connections can remain as fallback.
