[CmdletBinding()]
param(
    [string]$Domain = "liangz77.cn",
    [string]$RemoteHost,
    [string]$User = "deploy",
    [int]$Port = 22,
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
        ".vscode",
        "deploy.ps1",
        "server-init.sh",
        "Caddyfile"
    )

    Get-ChildItem -LiteralPath $SourceDir -Force | ForEach-Object {
        if ($excludeNames -contains $_.Name) {
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
        [string]$Script
    )

    $normalizedScript = $Script -replace "`r`n", "`n"
    $normalizedScript | ssh -p $RemotePort $Target "bash -s"
    if ($LASTEXITCODE -ne 0) {
        throw "Remote script execution failed."
    }
}

Require-Command "ssh"
Require-Command "scp"
Require-Command "tar"

if (-not $RemoteHost) {
    throw "RemoteHost is required. Example: .\deploy.ps1 -RemoteHost 1.2.3.4"
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

    Invoke-RemoteScript -Target $target -RemotePort $Port -Script $rollbackScript
    exit 0
}

$sourceDir = Get-SourceDir -RequestedDir $LocalBuildDir
$stagingDir = New-StagingDir -SourceDir $sourceDir
$releaseId = Get-Date -Format "yyyyMMdd-HHmmss"
$archivePath = Join-Path ([System.IO.Path]::GetTempPath()) "$Domain-$releaseId.tgz"
$remoteArchive = "/tmp/$Domain-$releaseId.tgz"

try {
    tar -czf $archivePath -C $stagingDir .
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to build release archive."
    }

    & scp -P $Port $archivePath "${target}:$remoteArchive"
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to upload release archive."
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

    Invoke-RemoteScript -Target $target -RemotePort $Port -Script $deployScript
}
finally {
    if (Test-Path $archivePath) {
        Remove-Item -LiteralPath $archivePath -Force
    }

    if (Test-Path $stagingDir) {
        Remove-Item -LiteralPath $stagingDir -Recurse -Force
    }
}
