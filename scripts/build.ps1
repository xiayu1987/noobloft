# Copyright (c) 2026 xiayu
# Contact: 126240622+xiayu1987@users.noreply.github.com
# SPDX-License-Identifier: MIT

param([string]$OutputDir = '')
$ErrorActionPreference = 'Stop'

$repoDir = Split-Path -Parent $PSScriptRoot
$goCommand = Get-Command go -ErrorAction SilentlyContinue
if ($goCommand) {
    $go = $goCommand.Source
} else {
    $go = Join-Path $env:ProgramFiles 'Go\bin\go.exe'
    if (-not (Test-Path $go)) {
        throw 'Go was not found in PATH or the standard Program Files location'
    }
}

Push-Location (Join-Path $repoDir 'web')
try {
    npm ci
    if ($LASTEXITCODE -ne 0) { throw "npm ci failed with exit code $LASTEXITCODE" }
    npm run build
    if ($LASTEXITCODE -ne 0) { throw "npm run build failed with exit code $LASTEXITCODE" }
} finally {
    Pop-Location
}

$binDir = if ($OutputDir) { [System.IO.Path]::GetFullPath($OutputDir) } else { Join-Path $repoDir 'bin' }
New-Item -ItemType Directory -Force -Path $binDir | Out-Null
$output = Join-Path $binDir 'noobloftd.exe'
$trayOutput = Join-Path $binDir 'noobloftd-tray.exe'

Push-Location $repoDir
try {
    & $go build -o $output ./cmd/noobloftd
    if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE" }
    & $go build -ldflags '-H=windowsgui -X main.trayBuild=1' -o $trayOutput ./cmd/noobloftd
    if ($LASTEXITCODE -ne 0) { throw "go build (tray) failed with exit code $LASTEXITCODE" }
} finally {
    Pop-Location
}
