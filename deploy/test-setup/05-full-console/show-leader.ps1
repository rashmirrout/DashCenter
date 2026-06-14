#!/usr/bin/env pwsh
# 05-full-console/show-leader.ps1 — print leader status for all 3 dashd.
#
# Probes the admin endpoint on each of the 3 dashd controllers and prints
# the leader / lease info as a tidy table. Uses dashctl `version` if a
# dashctl binary is reachable (matches 04-ha-fleet pattern); otherwise
# falls back to the raw /admin/leader REST endpoint.
#
# Usage:
#   pwsh ./show-leader.ps1

[CmdletBinding()] param()

$ErrorActionPreference = 'Continue'
Set-Location $PSScriptRoot

# Node label -> (rest port, admin port). Iteration order is deterministic.
$nodes = [ordered]@{
    'dashd-1' = @(28443, 27443)
    'dashd-2' = @(28453, 27453)
    'dashd-3' = @(28463, 27463)
}

# Resolve a dashctl binary: this dir, sibling 04-ha-fleet, then PATH.
$dashctl = $null
foreach ($candidate in @(
    (Join-Path $PSScriptRoot 'dashctl.exe'),
    (Join-Path $PSScriptRoot '..' '04-ha-fleet' 'dashctl.exe')
)) {
    if (Test-Path $candidate) { $dashctl = (Resolve-Path $candidate).Path; break }
}
if (-not $dashctl) {
    $cmd = Get-Command 'dashctl' -ErrorAction SilentlyContinue
    if ($cmd) { $dashctl = $cmd.Source }
}

"{0,-9} {1,-7} {2}" -f 'NODE','LEADER','DETAIL' | Write-Host
foreach ($name in $nodes.Keys) {
    $rest, $admin = $nodes[$name]
    $leader = '?'
    $detail = ''

    if ($dashctl) {
        $out = & $dashctl `
            --endpoint "http://127.0.0.1:$rest" `
            --admin-endpoint "http://127.0.0.1:$admin" `
            version 2>&1
        $serverLine = $out | Where-Object { $_ -match '^Server:' }
        if ($serverLine) {
            if ($serverLine -match 'leader=(\S+)') { $leader = $matches[1] }
            $detail = "$serverLine"
        } else {
            $detail = '(no response — container down?)'
        }
    } else {
        # Fallback: raw REST
        try {
            $r = Invoke-RestMethod -Uri "http://127.0.0.1:$admin/admin/leader" -TimeoutSec 3
            $leader = if ($r.leader) { 'true' } else { 'false' }
            $detail = "leader_id=$($r.leader_id) lease_ttl=$($r.lease_ttl_sec)s"
        } catch {
            $detail = '(no response — container down?)'
        }
    }

    $color = switch ($leader) {
        'true'  { 'Green' }
        'false' { 'Gray' }
        default { 'Red' }
    }
    "{0,-9} {1,-7} {2}" -f $name,$leader,$detail | Write-Host -ForegroundColor $color
}