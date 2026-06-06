#!/usr/bin/env pwsh
<#
.SYNOPSIS
  Stop a fleet started by start-fleet.ps1.
#>
[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$statePath = Join-Path $here ".fleet-state.json"

if (-not (Test-Path $statePath)) {
  Write-Warning "No $statePath — nothing to stop."
  exit 0
}

$state = Get-Content $statePath -Raw | ConvertFrom-Json

foreach ($c in $state.components) {
  $proc = Get-Process -Id $c.pid -ErrorAction SilentlyContinue
  if ($null -eq $proc) {
    Write-Host "  $($c.role)/$($c.device_id) (pid $($c.pid)) — already gone"
    continue
  }
  Write-Host "  Stopping $($c.role)/$($c.device_id) (pid $($c.pid)) ..." -NoNewline
  try {
    Stop-Process -Id $c.pid -Force
    Write-Host " OK" -ForegroundColor Green
  } catch {
    Write-Host " FAIL ($($_.Exception.Message))" -ForegroundColor Red
  }
}

Remove-Item $statePath -Force
Write-Host "Fleet stopped." -ForegroundColor Green
