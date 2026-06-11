#!/usr/bin/env pwsh
# 04-ha-fleet/show-leader.ps1 — print leader status for all 3 dashd.
#
# Uses `dashctl version` against each node's REST + admin endpoint pair.
# `version` already reports `leader=true|false` from the contacted node.
# This wrapper just iterates so you see all 3 in one shot.
#
# Usage:
#   pwsh ./show-leader.ps1

[CmdletBinding()] param()

$ErrorActionPreference = 'Continue'
Set-Location $PSScriptRoot

$dashctl = Join-Path $PSScriptRoot 'dashctl.exe'
if (-not (Test-Path $dashctl)) {
    Write-Host "dashctl.exe not found at $dashctl — build it first:" -ForegroundColor Yellow
    Write-Host '  go build -o ./dashctl.exe ../../../src/impl-go/dashctl/cmd/dashctl'
    exit 1
}

# Node label -> (rest port, admin port). Iteration order is deterministic.
$nodes = [ordered]@{
    'dashd-1' = @(28443, 27443)
    'dashd-2' = @(28453, 27453)
    'dashd-3' = @(28463, 27463)
}

"{0,-9} {1,-10} {2}" -f 'NODE','LEADER','RESPONSE' | Write-Host
foreach ($name in $nodes.Keys) {
    $rest, $admin = $nodes[$name]
    $out = & $dashctl `
        --endpoint "http://127.0.0.1:$rest" `
        --admin-endpoint "http://127.0.0.1:$admin" `
        version 2>&1
    $serverLine = $out | Where-Object { $_ -match '^Server:' }
    if (-not $serverLine) {
        "{0,-9} {1,-10} {2}" -f $name,'?','(no response — container down?)' | Write-Host -ForegroundColor Red
        continue
    }
    $leader = if ($serverLine -match 'leader=(\S+)') { $matches[1] } else { '?' }
    $color = if ($leader -eq 'true') { 'Green' } else { 'Gray' }
    "{0,-9} {1,-10} {2}" -f $name,$leader,$serverLine | Write-Host -ForegroundColor $color
}
