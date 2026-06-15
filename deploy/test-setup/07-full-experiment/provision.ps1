#!/usr/bin/env pwsh
# 07-full-experiment/provision.ps1 — apply the 155-object manifest set.
#
# Applies manifests in dependency order:
#   00-vnets (20) → 01-service-tunnels (5) → 02-enis (50) →
#   03-vnet-mappings (50) → 04-route-policies (10) → 05-acl-policies (20) →
#   06-ha-sets (5)
#
# Total: ~160 objects across 50 DPUs.

[CmdletBinding()]
param(
    [string]$Endpoint = "http://localhost:28443",
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

# Try host dashctl first, fall back to container
$dashctl = $null
$hostBin = Join-Path $PSScriptRoot "../../.." "src/impl-go/dashctl/bin/dashctl.exe"
if (Test-Path $hostBin) {
    $dashctl = { param($args_) & $hostBin --endpoint $Endpoint --insecure @args_ }
    Write-Host "Using host dashctl: $hostBin" -ForegroundColor Cyan
} else {
    $dashctl = { param($args_) docker compose run --rm dashctl @args_ }
    Write-Host "Using container dashctl" -ForegroundColor Cyan
}

$forceFlag = @()
if ($Force) { $forceFlag = @('--force') }

$manifests = @(
    "manifest/00-vnets.yaml",
    "manifest/01-service-tunnels.yaml",
    "manifest/02-enis.yaml",
    "manifest/03-vnet-mappings.yaml",
    "manifest/04-route-policies.yaml",
    "manifest/05-acl-policies.yaml",
    "manifest/06-ha-sets.yaml"
)

$total = 0
foreach ($m in $manifests) {
    Write-Host "==> Applying $m" -ForegroundColor Cyan
    & $dashctl @('-f', $m, @forceFlag)
    $total++
}

Write-Host ""
Write-Host "Provisioned $total manifest files (~449 objects)" -ForegroundColor Green
Write-Host ""
Write-Host "Verify:" -ForegroundColor Yellow
Write-Host "  dashctl get vnet -o table           # 25 VNets"
Write-Host "  dashctl get eni -o wide             # 120 ENIs (2-3 per DPU)"
Write-Host "  dashctl get vnet-mapping -o table    # 240 mappings"
Write-Host "  dashctl get route-policy -o table    # 17 route policies"
Write-Host "  dashctl get acl-policy -o table      # 32 ACL policies"
Write-Host "  dashctl get ha-set -o table          # 10 HA sets"
Write-Host "  dashctl dpu list -o table            # 50 DPUs"
