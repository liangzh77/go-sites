[CmdletBinding()]
param(
    [string]$Domain = "liangz77.cn",
    [string]$RemoteHost = "43.163.98.43",
    [string]$User = "root",
    [int]$Port = 22,
    [string]$IdentityFile = "C:\Users\liang\.ssh\keychain_deploy_ed25519",
    [string]$LocalBuildDir = "",
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
        "site-config.js"
    )

    Get-ChildItem -LiteralPath $SourceDir -Force | ForEach-Object {
        if ($excludeNames -contains $_.Name) {
            return
        }

        if ($_.Name -like ".local-demo-server.exe*") {
            return
        }

        if ($_.Extension -in @(".md", ".txt")) {
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

echo "Rollback completed: $previous_target"
'@
    $rollbackScript = $rollbackScript.Replace("__REMOTE_SITE_ROOT__", $remoteSiteRoot).Replace("__DOMAIN__", $Domain)

    Invoke-RemoteScript -Target $target -RemotePort $Port -KeyFile $resolvedIdentityFile -Script $rollbackScript
    exit 0
}

$sourceDir = Get-SourceDir -RequestedDir $LocalBuildDir
$stagingDir = New-StagingDir -SourceDir $sourceDir
$releaseId = Get-Date -Format "yyyyMMdd-HHmmss"
$archivePath = Join-Path ([System.IO.Path]::GetTempPath()) "$Domain-$releaseId.tgz"
$remoteArchive = "/tmp/$Domain-$releaseId.tgz"
$demoServerDir = Join-Path $PSScriptRoot "cmd\demo-server"
$demoBinaryPath = Join-Path ([System.IO.Path]::GetTempPath()) "go-sites-demo-$releaseId"
$remoteDemoBinary = "/tmp/go-sites-demo-$releaseId"
$remoteSiteCaddyfile = "/tmp/$Domain-$releaseId.caddy"
$shouldDeployDemoServer = Test-Path $demoServerDir

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

    & scp @scpArgs (Join-Path $PSScriptRoot "Caddyfile") "${target}:$remoteSiteCaddyfile"
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to upload site Caddyfile."
    }

    $deployScript = @'
set -euo pipefail
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

mkdir -p "$RELEASES_DIR" "$NEW_RELEASE" "$SHARED_DIR/demos"
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

site_password=''
if [[ -f "$SITE_ROOT/shared/site-config.js" ]]; then
  site_password=$(sed -n 's/.*password: *"\([^"]*\)".*/\1/p' "$SITE_ROOT/shared/site-config.js" | head -n 1)
fi

if [[ ! -f "$CONFIG" ]]; then
  session_secret=$(openssl rand -hex 32 2>/dev/null || date +%s%N | sha256sum | awk '{print $1}')
  cat >"$CONFIG" <<EOF
DEMO_SERVER_ADDR=127.0.0.1:9005
DEMO_DATA_DIR=$SHARED_DIR
SITE_ROOT=$SITE_ROOT/current
PUBLIC_ORIGIN=https://$DOMAIN
DEMO_ADMIN_PASSWORD=$site_password
DEMO_SESSION_SECRET=$session_secret
EOF
else
  if grep -q '^DEMO_ADMIN_PASSWORD=' "$CONFIG"; then
    sed -i "s|^DEMO_ADMIN_PASSWORD=.*|DEMO_ADMIN_PASSWORD=$site_password|" "$CONFIG"
  else
    printf '\nDEMO_ADMIN_PASSWORD=%s\n' "$site_password" >>"$CONFIG"
  fi
fi

chown -R "$service_user:$service_group" "$SHARED_DIR"
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
}
