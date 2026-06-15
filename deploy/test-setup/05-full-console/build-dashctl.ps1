#!/usr/bin/env pwsh
# build-dashctl.ps1 — compile dashctl from source and place it in this directory.
#
# Requires Go 1.22+ on PATH. Automatically called by start-fleet.ps1
# unless -SkipDashctlBuild is passed.
#
# Usage:
#   pwsh ./build-dashctl.ps1              # build and place ./dashctl.exe
#   pwsh ./build-dashctl.ps1 -Check       # exit 0 if already built, 1 if missing

[CmdletBinding()]
param(
    [switch]$Check
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..\..'))
$srcDir   = Join-Path $repoRoot 'src\impl-go'
$output   = Join-Path $PSScriptRoot 'dashctl.exe'

if ($Check) {
    if (Test-Path $output) { exit 0 } else { exit 1 }
}

# Check if already built and up-to-date.
$goMod = Join-Path $srcDir 'dashctl\go.mod'
if ((Test-Path $output) -and (Test-Path $goMod)) {
    if ((Get-Item $output).LastWriteTime -gt (Get-Item $goMod).LastWriteTime) {
        Write-Host "==> dashctl already built at $output (up-to-date)" -ForegroundColor Green
        exit 0
    }
}

$goCmd = Get-Command 'go' -ErrorAction SilentlyContinue
if (-not $goCmd) {
    Write-Host "   (Go not found on PATH — skipping dashctl build)" -ForegroundColor DarkGray
    Write-Host "   Install Go 1.22+: https://go.dev/dl/" -ForegroundColor DarkGray
    Write-Host "   Or use the bootstrap.py fallback: python manifest/bootstrap.py" -ForegroundColor DarkGray
    exit 0
}

Write-Host "==> Building dashctl (Go: $(go version))" -ForegroundColor Cyan

Push-Location $srcDir
try {
    $env:CGO_ENABLED = '0'
    go build -trimpath -ldflags="-s -w" -o $output ./dashctl/cmd/dashctl
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
} finally {
    Pop-Location
}

Write-Host "==> dashctl built at $output" -ForegroundColor Green
& $output version 2>$null
