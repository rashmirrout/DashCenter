#!/usr/bin/env pwsh
# 05-full-console/stop-fleet.ps1 — tear down the fleet and remove volumes.
#
# Usage:
#   pwsh ./stop-fleet.ps1               # graceful shutdown + volume removal (clean slate)
#   pwsh ./stop-fleet.ps1 -KeepVolumes  # graceful shutdown only (preserves etcd state)
#   pwsh ./stop-fleet.ps1 -RemoveImages # also remove the built images (deep clean)

[CmdletBinding()]
param(
    [switch]$KeepVolumes,
    [switch]$RemoveImages
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

Write-Host "==> Stopping 05-full-console" -ForegroundColor Cyan
$args = @()
if (-not $KeepVolumes) { $args += '-v' }
if ($RemoveImages)     { $args += '--rmi'; $args += 'local' }

if ($args.Count -gt 0) {
    docker compose down @args
} else {
    docker compose down
}
Write-Host "==> Done" -ForegroundColor Green