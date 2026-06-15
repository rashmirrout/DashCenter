#!/usr/bin/env pwsh
# 07-full-experiment/stop-fleet.ps1 — tear down the experiment stack.
#
# Usage:
#   pwsh ./stop-fleet.ps1                # stop + remove volumes
#   pwsh ./stop-fleet.ps1 -KeepVolumes   # stop but keep etcd state
#   pwsh ./stop-fleet.ps1 -RemoveImages  # also remove built images

[CmdletBinding()]
param(
    [switch]$KeepVolumes,
    [switch]$RemoveImages
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

Write-Host "==> Stopping fleet" -ForegroundColor Cyan
$args_ = @('down')
if (-not $KeepVolumes) { $args_ += '-v' }
if ($RemoveImages) { $args_ += '--rmi'; $args_ += 'local' }
docker compose @args_

Write-Host "Fleet stopped" -ForegroundColor Green
