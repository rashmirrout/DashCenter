#!/usr/bin/env pwsh
# 06-fleet-ui-diagnostics/cleanup-data.ps1 — remove the loaded dataset (keeps fleet running).
#
# Reverse of provision.ps1: deletes every loaded object across the 3
# namespaces (default + edge + staging) via the dashd REST API. The dashd
# controllers, sims, and etcd state remain up — only the application
# objects are removed.
#
# Use this when you want to reset state for the next provision iteration
# without paying the docker compose restart cost.
#
# For a full fleet shutdown (containers + volumes), use stop-fleet.ps1.
#
# NOTE: This script does not use `dashctl delete` because the current
# dashctl version takes only `delete <kind> <name>` (no -f / batch mode);
# direct REST DELETEs are faster and don't require dashctl at all.
#
# Usage:
#   pwsh ./cleanup-data.ps1
#   pwsh ./cleanup-data.ps1 -Endpoint http://10.0.0.5:38443

[CmdletBinding()]
param(
    [string]$Endpoint = 'http://127.0.0.1:38443'
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

Write-Host "==> Cleaning data via REST against $Endpoint" -ForegroundColor Cyan

# Reverse-dependency order: ACLs → routes → mappings → ENIs → tunnels → vnets → HA sets.
$kinds = @(
    'acl-policies',
    'route-policies',
    'vnet-mappings',
    'enis',
    'service-tunnels',
    'vnets',
    'ha'
)
$namespaces = @('default', 'edge', 'staging')
$total = 0; $ok = 0; $err = 0

foreach ($ns in $namespaces) {
    foreach ($k in $kinds) {
        $url = "$Endpoint/v1/$ns/$k"
        try {
            $list = Invoke-RestMethod -Uri $url -Method GET -TimeoutSec 5
            # dashd returns { items: [{ kind, name, namespace, generation, spec }] }
            $items = if ($list.items) { $list.items } else { $list }
            foreach ($item in $items) {
                # name is at the top level on dashd; fall back to metadata.name
                # if a future server version moves it.
                $name = if ($item.name) { $item.name } else { $item.metadata.name }
                if (-not $name) { continue }
                # Only delete objects that actually live in this namespace.
                # GET /v1/default/<kind> may include cross-namespace entries
                # on some dashd builds; filter to avoid double-deletes.
                if ($item.namespace -and $item.namespace -ne $ns) { continue }
                $total++
                try {
                    Invoke-RestMethod -Uri "$url/$name" -Method DELETE -TimeoutSec 5 | Out-Null
                    Write-Host "  ✓ DELETE /v1/$ns/$k/$name" -ForegroundColor DarkGreen
                    $ok++
                } catch {
                    Write-Host "  ✗ DELETE /v1/$ns/$k/$name → $($_.Exception.Message)" -ForegroundColor Red
                    $err++
                }
            }
        } catch {
            # 404 (namespace empty / kind not registered) is fine; skip.
        }
    }
}

Write-Host ""
Write-Host ("==> Cleanup complete: {0}/{1} ok, {2} errors" -f $ok, $total, $err) `
    -ForegroundColor $(if ($err -eq 0) { 'Green' } else { 'Yellow' })
if ($err -gt 0) { exit 1 }