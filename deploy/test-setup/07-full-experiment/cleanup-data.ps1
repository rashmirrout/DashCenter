#!/usr/bin/env pwsh
# 07-full-experiment/cleanup-data.ps1 — delete all provisioned objects.

[CmdletBinding()]
param(
    [string]$Endpoint = "http://localhost:28443"
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$hostBin = Join-Path $PSScriptRoot "../../.." "src/impl-go/dashctl/bin/dashctl.exe"
$dc = if (Test-Path $hostBin) { $hostBin } else { "dashctl" }
$base = @('--endpoint', $Endpoint, '--insecure')

Write-Host "==> Deleting ACL policies" -ForegroundColor Cyan
$tenants = @("bank","retail","media","iot","analytics","telecom","health","fintech","gaming","logistics")
foreach ($t in $tenants) {
    & $dc @base delete acl-policy "acl-$t-inbound" --ignore-not-found 2>$null
    & $dc @base delete acl-policy "acl-$t-outbound" --ignore-not-found 2>$null
}

Write-Host "==> Deleting route policies" -ForegroundColor Cyan
foreach ($t in $tenants) {
    & $dc @base delete route-policy "rp-$t-prod" --ignore-not-found 2>$null
}

Write-Host "==> Deleting HA sets" -ForegroundColor Cyan
1..5 | ForEach-Object { & $dc @base delete ha-set "ha-appliance-$_" --ignore-not-found 2>$null }

Write-Host "==> Deleting VNet mappings" -ForegroundColor Cyan
$tiers = @("web","api","db","cache","worker")
$idx = 1
foreach ($t in $tenants) {
    foreach ($tier in $tiers) {
        $subnet = 100 + $idx
        & $dc @base delete vnet-mapping "$t-prod-192.168.$subnet.1" --ignore-not-found 2>$null
        $idx++
    }
}

Write-Host "==> Deleting ENIs" -ForegroundColor Cyan
foreach ($t in $tenants) {
    foreach ($tier in $tiers) {
        & $dc @base delete eni "eni-$t-$tier-01" --ignore-not-found 2>$null
    }
}

Write-Host "==> Deleting service tunnels" -ForegroundColor Cyan
@("st-internet-egress","st-dc-west-east","st-backup-link","st-monitoring","st-mgmt-vpn") | ForEach-Object {
    & $dc @base delete service-tunnel $_ --ignore-not-found 2>$null
}

Write-Host "==> Deleting VNets" -ForegroundColor Cyan
foreach ($t in $tenants) {
    & $dc @base delete vnet "$t-prod" --ignore-not-found 2>$null
    & $dc @base delete vnet "$t-staging" --ignore-not-found 2>$null
}

Write-Host "All objects deleted" -ForegroundColor Green
