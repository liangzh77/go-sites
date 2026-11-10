[CmdletBinding()]
param(
    [string]$Domain = "liangz77.cn",
    [string]$RemoteHost = "49.232.129.114",
    [string]$User = "root",
    [int]$Port = 22,
    [string]$IdentityFile = "$env:USERPROFILE\.ssh\keychain_deploy_ed25519",
    [string]$LocalBuildDir = "",
    [string]$Version = "",
    [switch]$Rollback
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Require-Command {
    param([string]$Name)

    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "Missing required command: $Name"
    }
}

function Get-SourceDir {
    param([string]$RequestedDir)

    if ($RequestedDir) {
        return (Resolve-Path $RequestedDir).Path
    }

    $distDir = Join-Path $PSScriptRoot "dist"
    if (Test-Path $distDir) {
        return (Resolve-Path $distDir).Path
    }

    return $PSScriptRoot
}

function New-StagingDir {
    param([string]$SourceDir)

    $stagingDir = Join-Path ([System.IO.Path]::GetTempPath()) ("go-sites-stage-" + [guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Path $stagingDir | Out-Null

    $excludeNames = @(
        ".git",
        ".github",
        ".gstack",
        ".playwright-mcp",
        ".vscode",
        "cmd",
        "demo-data",
        "deploy.ps1",
        "server-init.sh",
        "Caddyfile",
        "go.mod",
        "go.sum",
        "site-config.js",
        "wiki"
    )

    Get-ChildItem -LiteralPath $SourceDir -Force | ForEach-Object {
        if ($excludeNames -contains $_.Name) {
            return
        }

        if ($_.Name -like ".local-demo-server.exe*") {
            return
        }

        if ($_.Name -like ".wiki-original-backup-*") {
            return
        }

        if ($_.Extension -in @(".md", ".txt") -and $_.Name -notin @("5768f25008cb6868cb00692e18a72154.txt", "site-version.txt")) {
            return
        }

        $target = Join-Path $stagingDir $_.Name
        Copy-Item -LiteralPath $_.FullName -Destination $target -Recurse -Force
    }

    if (-not (Get-ChildItem -LiteralPath $stagingDir -Force | Select-Object -First 1)) {
        throw "Staging directory is empty. Check LocalBuildDir or repository contents."
    }

    return $stagingDir
}

function Invoke-RemoteScript {
    param(
        [string]$Target,
        [int]$RemotePort,
        [string]$KeyFile,
        [string]$Script
    )

    $normalizedScript = $Script -replace "`r`n", "`n"
    $sshArgs = @("-p", "$RemotePort")
    if ($KeyFile) {
        $sshArgs += @("-i", $KeyFile, "-o", "IdentitiesOnly=yes")
    }

    $normalizedScript | & ssh @sshArgs $Target "bash -s"
    if ($LASTEXITCODE -ne 0) {
        throw "Remote script execution failed."
    }
}

function Get-Next-SiteVersion {
    param([string]$CurrentVersion)

    $trimmed = $CurrentVersion.Trim()
    if ($trimmed -match '^(\d+)\.(\d+)\.(\d+)$') {
        $major = [int]$Matches[1]
        $minor = [int]$Matches[2]
        $patch = [int]$Matches[3] + 1
        return "$major.$minor.$patch"
    }
    else {
        throw "Invalid current site version: $CurrentVersion. Expected semantic version like 1.0.0."
    }
}

function Update-SiteVersion {
    param(
        [string]$SourceDir,
        [string]$RequestedVersion
    )

    $versionFile = Join-Path $SourceDir "site-version.txt"
    $nextVersion = $RequestedVersion.Trim()
    if ($nextVersion) {
        if ($nextVersion -notmatch '^\d+\.\d+\.\d+$') {
            throw "Invalid requested site version: $nextVersion. Expected semantic version like 1.0.0."
        }
    }
    else {
        $currentVersion = ""
        if (Test-Path -LiteralPath $versionFile) {
            $currentVersion = (Get-Content -LiteralPath $versionFile -Raw).Trim()
        }
        if ($currentVersion) {
            $nextVersion = Get-Next-SiteVersion -CurrentVersion $currentVersion
        }
        else {
            $nextVersion = "1.0.0"
        }
    }

    Set-Content -LiteralPath $versionFile -Value $nextVersion -NoNewline -Encoding utf8
    Write-Host "Site version: $nextVersion"
    return $nextVersion
}

Require-Command "ssh"
Require-Command "scp"
Require-Command "tar"
Require-Command "go"

$resolvedIdentityFile = ""
if ($IdentityFile) {
    $resolvedIdentityFile = (Resolve-Path -LiteralPath $IdentityFile).Path
}

$target = "$User@$RemoteHost"
$remoteSiteRoot = "/srv/sites/$Domain"

if ($Rollback) {
    $rollbackScript = @'
set -euo pipefail
SITE_ROOT='__REMOTE_SITE_ROOT__'

if [[ ! -L "$SITE_ROOT/previous" ]]; then
  echo "previous link not found"
  exit 1
fi

previous_target=$(readlink -f "$SITE_ROOT/previous")
current_target=$(readlink -f "$SITE_ROOT/current")

ln -sfn "$current_target" "$SITE_ROOT/previous"
ln -sfn "$previous_target" "$SITE_ROOT/current"

if ! curl -fsS "http://127.0.0.1" -H "Host: __DOMAIN__" >/dev/null; then
  ln -sfn "$current_target" "$SITE_ROOT/current"
  ln -sfn "$previous_target" "$SITE_ROOT/previous"
  echo "Rollback health check failed"
  exit 1
fi

KEYS_ROOT='/srv/apps/go-sites-keys'
if [[ -L "$KEYS_ROOT/previous" && -L "$KEYS_ROOT/current" ]]; then
  keys_previous_target=$(readlink -f "$KEYS_ROOT/previous")
  keys_current_target=$(readlink -f "$KEYS_ROOT/current")
  ln -sfn "$keys_current_target" "$KEYS_ROOT/previous"
  ln -sfn "$keys_previous_target" "$KEYS_ROOT/current"
  systemctl restart go-sites-keys

  keys_healthy=0
  for _ in $(seq 1 20); do
    if curl -fsS http://127.0.0.1:9020/keys/healthz >/dev/null; then
      keys_healthy=1
      break
    fi
    sleep 1
  done

  if [[ "$keys_healthy" != "1" ]]; then
    ln -sfn "$keys_current_target" "$KEYS_ROOT/current"
    ln -sfn "$keys_previous_target" "$KEYS_ROOT/previous"
    systemctl restart go-sites-keys || true
    echo "Keys rollback health check failed"
    exit 1
  fi
  echo "Keys rollback completed: $keys_previous_target"
fi

echo "Rollback completed: $previous_target"
'@
    $rollbackScript = $rollbackScript.Replace("__REMOTE_SITE_ROOT__", $remoteSiteRoot).Replace("__DOMAIN__", $Domain)

    Invoke-RemoteScript -Target $target -RemotePort $Port -KeyFile $resolvedIdentityFile -Script $rollbackScript
    exit 0
}

$sourceDir = Get-SourceDir -RequestedDir $LocalBuildDir
$siteVersion = Update-SiteVersion -SourceDir $sourceDir -RequestedVersion $Version
$stagingDir = New-StagingDir -SourceDir $sourceDir
$releaseId = Get-Date -Format "yyyyMMdd-HHmmss"
$archivePath = Join-Path ([System.IO.Path]::GetTempPath()) "$Domain-$releaseId.tgz"
$remoteArchive = "/tmp/$Domain-$releaseId.tgz"
$demoServerDir = Join-Path $PSScriptRoot "cmd\demo-server"
$demoBinaryPath = Join-Path ([System.IO.Path]::GetTempPath()) "go-sites-demo-$releaseId"
$remoteDemoBinary = "/tmp/go-sites-demo-$releaseId"
$keysServerDir = Join-Path $PSScriptRoot "cmd\keys"
$keysBinaryPath = Join-Path ([System.IO.Path]::GetTempPath()) "go-sites-keys-$releaseId"
$remoteKeysBinary = "/tmp/go-sites-keys-$releaseId"
$keysBackupScript = Join-Path $keysServerDir "backup.sh"
$remoteKeysBackupScript = "/tmp/go-sites-keys-backup-$releaseId.sh"
$remoteSiteCaddyfile = "/tmp/$Domain-$releaseId.caddy"
$shouldDeployDemoServer = Test-Path $demoServerDir
$shouldDeployKeysServer = Test-Path $keysServerDir

try {
    if ($shouldDeployDemoServer) {
        $oldGoos = $env:GOOS
        $oldGoarch = $env:GOARCH
        try {
            $env:GOOS = "linux"
            $env:GOARCH = "amd64"
            & go build -o $demoBinaryPath ./cmd/demo-server
            if ($LASTEXITCODE -ne 0) {
                throw "Failed to build demo server."
            }
        }
        finally {
            $env:GOOS = $oldGoos
            $env:GOARCH = $oldGoarch
        }
    }

    if ($shouldDeployKeysServer) {
        $oldGoos = $env:GOOS
        $oldGoarch = $env:GOARCH
        try {
            $env:GOOS = "linux"
            $env:GOARCH = "amd64"
            & go build -o $keysBinaryPath ./cmd/keys
            if ($LASTEXITCODE -ne 0) {
                throw "Failed to build keys server."
            }
        }
        finally {
            $env:GOOS = $oldGoos
            $env:GOARCH = $oldGoarch
        }
    }

    tar -czf $archivePath -C $stagingDir .
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to build release archive."
    }

    $scpArgs = @("-P", "$Port")
    if ($resolvedIdentityFile) {
        $scpArgs += @("-i", $resolvedIdentityFile, "-o", "IdentitiesOnly=yes")
    }

    & scp @scpArgs $archivePath "${target}:$remoteArchive"
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to upload release archive."
    }

    if ($shouldDeployDemoServer) {
        & scp @scpArgs $demoBinaryPath "${target}:$remoteDemoBinary"
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to upload demo server binary."
        }
    }

    if ($shouldDeployKeysServer) {
        & scp @scpArgs $keysBinaryPath "${target}:$remoteKeysBinary"
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to upload keys server binary."
        }
        & scp @scpArgs $keysBackupScript "${target}:$remoteKeysBackupScript"
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to upload keys backup script."
        }
    }

    & scp @scpArgs (Join-Path $PSScriptRoot "Caddyfile") "${target}:$remoteSiteCaddyfile"
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to upload site Caddyfile."
    }

    $deployScript = @'
set -euo pipefail
umask 077
DOMAIN='__DOMAIN__'
SITE_ROOT='__REMOTE_SITE_ROOT__'
RELEASES_DIR="$SITE_ROOT/releases"
NEW_RELEASE="$RELEASES_DIR/__RELEASE_ID__"
ARCHIVE='__REMOTE_ARCHIVE__'

mkdir -p "$RELEASES_DIR"
mkdir -p "$NEW_RELEASE"
tar -xzf "$ARCHIVE" -C "$NEW_RELEASE"
rm -f "$ARCHIVE"

if [[ ! -f "$SITE_ROOT/shared/site-config.js" ]]; then
  echo "Missing server config: $SITE_ROOT/shared/site-config.js"
  rm -rf "$NEW_RELEASE"
  exit 1
fi
if grep -Eq 'privateArea[[:space:]]*:' "$SITE_ROOT/shared/site-config.js"; then
  echo "Refusing deployment: remove privateArea.password from shared/site-config.js first."
  echo "The private-area password must live only in the server-side environment file."
  rm -rf "$NEW_RELEASE"
  exit 1
fi
ln -sfn "$SITE_ROOT/shared/site-config.js" "$NEW_RELEASE/site-config.js"

if [[ -L "$SITE_ROOT/current" ]]; then
  old_target=$(readlink -f "$SITE_ROOT/current")
  ln -sfn "$old_target" "$SITE_ROOT/previous"
fi

ln -sfn "$NEW_RELEASE" "$SITE_ROOT/current"

if ! curl -fsS "http://127.0.0.1" -H "Host: $DOMAIN" >/dev/null; then
  if [[ -L "$SITE_ROOT/previous" ]]; then
    rollback_target=$(readlink -f "$SITE_ROOT/previous")
    ln -sfn "$rollback_target" "$SITE_ROOT/current"
  fi

  rm -rf "$NEW_RELEASE"
  echo "Health check failed"
  exit 1
fi

echo "Release deployed: $NEW_RELEASE"
'@
    $deployScript = $deployScript.Replace("__DOMAIN__", $Domain).Replace("__REMOTE_SITE_ROOT__", $remoteSiteRoot).Replace("__RELEASE_ID__", $releaseId).Replace("__REMOTE_ARCHIVE__", $remoteArchive)

    Invoke-RemoteScript -Target $target -RemotePort $Port -KeyFile $resolvedIdentityFile -Script $deployScript

    if ($shouldDeployDemoServer) {
        $demoDeployScript = @'
set -euo pipefail
DOMAIN='__DOMAIN__'
SITE_ROOT='__REMOTE_SITE_ROOT__'
APP_ROOT='/srv/apps/go-sites-demo'
RELEASES_DIR="$APP_ROOT/releases"
NEW_RELEASE="$RELEASES_DIR/__RELEASE_ID__"
SHARED_DIR="$APP_ROOT/shared"
BINARY='__REMOTE_DEMO_BINARY__'
CONFIG="$SHARED_DIR/config.env"

mkdir -p "$RELEASES_DIR" "$NEW_RELEASE" "$SHARED_DIR/demos" "$SHARED_DIR/wiki"
mv "$BINARY" "$NEW_RELEASE/go-sites-demo"
chmod 755 "$NEW_RELEASE/go-sites-demo"

service_user=''
service_group=''
if id www-data >/dev/null 2>&1; then
  service_user='www-data'
  service_group='www-data'
elif id caddy >/dev/null 2>&1; then
  service_user='caddy'
  service_group='caddy'
else
  service_user='root'
  service_group='root'
fi

if [[ ! -f "$CONFIG" ]]; then
  echo "Missing $CONFIG."
  echo "Create it once with a server-only DEMO_ADMIN_PASSWORD, DEMO_API_KEY and DEMO_SESSION_SECRET."
  echo "The admin password must never be read from the public site-config.js file."
  exit 1
else
  if grep -q '^WIKI_ROOT=' "$CONFIG"; then
    sed -i "s|^WIKI_ROOT=.*|WIKI_ROOT=$SHARED_DIR/wiki|" "$CONFIG"
  else
    printf '\nWIKI_ROOT=%s/wiki\n' "$SHARED_DIR" >>"$CONFIG"
  fi

  # Preserve an existing publish key. If an older config does not have one,
  # create a new random value rather than shipping a shared repository secret.
  if ! grep -q '^DEMO_API_KEY=' "$CONFIG"; then
    printf '\nDEMO_API_KEY=%s\n' "$(openssl rand -hex 32)" >>"$CONFIG"
  fi
fi

chown -R "$service_user:$service_group" "$SHARED_DIR"
chmod 600 "$CONFIG"
ln -sfn "$NEW_RELEASE" "$APP_ROOT/current"

cat >/etc/systemd/system/go-sites-demo.service <<EOF
[Unit]
Description=Go Sites Demo Manager
After=network.target

[Service]
Type=simple
User=$service_user
Group=$service_group
WorkingDirectory=/srv/apps/go-sites-demo/shared
EnvironmentFile=/srv/apps/go-sites-demo/shared/config.env
ExecStart=/srv/apps/go-sites-demo/current/go-sites-demo
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable go-sites-demo >/dev/null
systemctl restart go-sites-demo
systemctl --no-pager --lines=20 status go-sites-demo
echo "Demo server deployed: $NEW_RELEASE"
'@
        $demoDeployScript = $demoDeployScript.Replace("__DOMAIN__", $Domain).Replace("__REMOTE_SITE_ROOT__", $remoteSiteRoot).Replace("__RELEASE_ID__", $releaseId).Replace("__REMOTE_DEMO_BINARY__", $remoteDemoBinary)

        Invoke-RemoteScript -Target $target -RemotePort $Port -KeyFile $resolvedIdentityFile -Script $demoDeployScript
    }

    if ($shouldDeployKeysServer) {
        $keysDeployScript = @'
set -euo pipefail
DOMAIN='__DOMAIN__'
APP_ROOT='/srv/apps/go-sites-keys'
RELEASES_DIR="$APP_ROOT/releases"
NEW_RELEASE="$RELEASES_DIR/__RELEASE_ID__"
SHARED_DIR="$APP_ROOT/shared"
DATA_DIR="$SHARED_DIR/data"
SECRETS_DIR='/srv/secrets/liangz77-keys'
CONFIG="$SHARED_DIR/keys.env"
BINARY='__REMOTE_KEYS_BINARY__'
BACKUP_SCRIPT='__REMOTE_KEYS_BACKUP_SCRIPT__'
DEMO_CONFIG='/srv/apps/go-sites-demo/shared/config.env'

mkdir -p "$RELEASES_DIR" "$NEW_RELEASE" "$DATA_DIR" "$SECRETS_DIR"
mv "$BINARY" "$NEW_RELEASE/go-sites-keys"
chmod 755 "$NEW_RELEASE/go-sites-keys"
mv "$BACKUP_SCRIPT" "$NEW_RELEASE/backup.sh"
sed -i 's/\r$//' "$NEW_RELEASE/backup.sh"
chmod 755 "$NEW_RELEASE/backup.sh"

service_user=''
service_group=''
if id www-data >/dev/null 2>&1; then
  service_user='www-data'
  service_group='www-data'
elif id caddy >/dev/null 2>&1; then
  service_user='caddy'
  service_group='caddy'
else
  service_user='root'
  service_group='root'
fi

if [[ ! -f "$SECRETS_DIR/master.key" ]]; then
  openssl rand -base64 32 >"$SECRETS_DIR/master.key"
fi
if [[ ! -f "$SECRETS_DIR/session.key" ]]; then
  openssl rand -base64 48 >"$SECRETS_DIR/session.key"
fi
chown "root:$service_group" "$SECRETS_DIR" "$SECRETS_DIR/master.key" "$SECRETS_DIR/session.key"
chmod 750 "$SECRETS_DIR"
chmod 640 "$SECRETS_DIR/master.key" "$SECRETS_DIR/session.key"

if [[ ! -f "$CONFIG" ]]; then
  if [[ ! -f "$DEMO_CONFIG" ]]; then
    echo "Missing $DEMO_CONFIG; cannot initialize the keys admin password."
    exit 1
  fi
  admin_password=$(sed -n 's/^DEMO_ADMIN_PASSWORD=//p' "$DEMO_CONFIG" | head -n 1)
  if [[ -z "$admin_password" ]]; then
    echo "DEMO_ADMIN_PASSWORD is empty; refusing to initialize keys service."
    exit 1
  fi
  admin_hash=$(printf '%s' "$admin_password" | "$NEW_RELEASE/go-sites-keys" -hash-password)
  cat >"$CONFIG" <<EOF
KEYS_ADDR=127.0.0.1:9020
KEYS_DATA_DIR=$DATA_DIR
KEYS_DB_PATH=$DATA_DIR/keys.db
KEYS_PUBLIC_ORIGIN=https://$DOMAIN
KEYS_COOKIE_SECURE=true
KEYS_MASTER_KEY_FILE=$SECRETS_DIR/master.key
KEYS_SESSION_SECRET_FILE=$SECRETS_DIR/session.key
KEYS_ADMIN_PASSWORD_HASH=$admin_hash
EOF
fi

chown -R "$service_user:$service_group" "$SHARED_DIR"
chmod 750 "$SHARED_DIR" "$DATA_DIR"
chmod 600 "$CONFIG"

old_target=''
if [[ -L "$APP_ROOT/current" ]]; then
  old_target=$(readlink -f "$APP_ROOT/current")
  ln -sfn "$old_target" "$APP_ROOT/previous"
fi
ln -sfn "$NEW_RELEASE" "$APP_ROOT/current"

cat >/etc/systemd/system/go-sites-keys.service <<EOF
[Unit]
Description=Go Sites API Key Cabinet
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$service_user
Group=$service_group
WorkingDirectory=$SHARED_DIR
EnvironmentFile=$CONFIG
ExecStart=$APP_ROOT/current/go-sites-keys
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$SHARED_DIR
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
MemoryMax=256M

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable go-sites-keys >/dev/null
systemctl restart go-sites-keys

healthy=0
for _ in $(seq 1 20); do
  if curl -fsS http://127.0.0.1:9020/keys/healthz >/dev/null; then
    healthy=1
    break
  fi
  sleep 1
done

if [[ "$healthy" != "1" ]]; then
  systemctl --no-pager --lines=30 status go-sites-keys || true
  if [[ -n "$old_target" ]]; then
    ln -sfn "$old_target" "$APP_ROOT/current"
    systemctl restart go-sites-keys || true
  fi
  rm -rf "$NEW_RELEASE"
  echo "Keys service health check failed"
  exit 1
fi

systemctl --no-pager --lines=20 status go-sites-keys
echo "Keys service deployed: $NEW_RELEASE"
'@
        $keysDeployScript = $keysDeployScript.Replace("__DOMAIN__", $Domain).Replace("__RELEASE_ID__", $releaseId).Replace("__REMOTE_KEYS_BINARY__", $remoteKeysBinary).Replace("__REMOTE_KEYS_BACKUP_SCRIPT__", $remoteKeysBackupScript)
        Invoke-RemoteScript -Target $target -RemotePort $Port -KeyFile $resolvedIdentityFile -Script $keysDeployScript
    }

$caddyDeployScript = @'
set -euo pipefail
DOMAIN='__DOMAIN__'
SITE_CADDYFILE='__REMOTE_SITE_CADDYFILE__'
MAIN_CADDYFILE='/etc/caddy/Caddyfile'
SITES_DIR='/etc/caddy/sites-enabled'
SITE_CONFIG="$SITES_DIR/$DOMAIN.caddy"
LEGACY_CONFIG="$SITES_DIR/legacy-existing.caddy"

mkdir -p "$SITES_DIR"

if [[ ! -f "$MAIN_CADDYFILE" ]] || ! grep -Eq '^[[:space:]]*import[[:space:]]+/etc/caddy/sites-enabled/\*\.caddy' "$MAIN_CADDYFILE"; then
  timestamp=$(date +%Y%m%d-%H%M%S)
  if [[ -f "$MAIN_CADDYFILE" ]]; then
    cp "$MAIN_CADDYFILE" "$MAIN_CADDYFILE.bak.$timestamp"
    if [[ ! -f "$LEGACY_CONFIG" ]]; then
      awk '
        function is_main_site(line) {
          return line ~ /(^|[,[:space:]])https?:\/\/(www\.)?liangz77\.cn([[:space:],{]|$)/
        }
        function flush_block() {
          if (!skip && block != "") {
            printf "%s", block
            if (substr(block, length(block), 1) != "\n") {
              printf "\n"
            }
            printf "\n"
          }
          block = ""
          skip = 0
        }
        {
          line = $0 "\n"
          if (depth == 0) {
            block = ""
            skip = 0
            if ($0 ~ /^[[:space:]]*\{/) {
              skip = 1
            } else if (is_main_site($0)) {
              skip = 1
            }
          }
          block = block line
          for (i = 1; i <= length($0); i++) {
            c = substr($0, i, 1)
            if (c == "{") depth++
            if (c == "}") depth--
          }
          if (depth == 0) {
            flush_block()
          }
        }
      ' "$MAIN_CADDYFILE" >"$LEGACY_CONFIG"
    fi
  fi

  cat >"$MAIN_CADDYFILE" <<'EOF'
{
    email admin@liangz77.cn
}

import /etc/caddy/sites-enabled/*.caddy
EOF
fi

install -m 644 "$SITE_CADDYFILE" "$SITE_CONFIG"
caddy validate --config /etc/caddy/Caddyfile
systemctl reload caddy
rm -f "$SITE_CADDYFILE"
echo "Caddy reloaded"
'@
    $caddyDeployScript = $caddyDeployScript.Replace("__DOMAIN__", $Domain).Replace("__REMOTE_SITE_CADDYFILE__", $remoteSiteCaddyfile)
    Invoke-RemoteScript -Target $target -RemotePort $Port -KeyFile $resolvedIdentityFile -Script $caddyDeployScript
}
finally {
    if (Test-Path $archivePath) {
        Remove-Item -LiteralPath $archivePath -Force
    }

    if (Test-Path $stagingDir) {
        Remove-Item -LiteralPath $stagingDir -Recurse -Force
    }

    if (Test-Path $demoBinaryPath) {
        Remove-Item -LiteralPath $demoBinaryPath -Force
    }

    if (Test-Path $keysBinaryPath) {
        Remove-Item -LiteralPath $keysBinaryPath -Force
    }
}
