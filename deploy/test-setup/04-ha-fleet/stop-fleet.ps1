#!/usr/bin/env pwsh
# 04-ha-fleet/stop-fleet.ps1 — tear down the fleet and remove volumes.
#
# Usage:
#   pwsh ./stop-fleet.ps1               # graceful shutdown + volume removal
#   pwsh ./stop-fleet.ps1 -KeepVolumes  # graceful shutdown only

[CmdletBinding()]
param(
    [switch]$KeepVolumes
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

Write-Host "==> Stopping 04-ha-fleet" -ForegroundColor Cyan
if ($KeepVolumes) {
    docker compose down
} else {
    docker compose down -v
}
Write-Host "==> Done" -ForegroundColor Green
