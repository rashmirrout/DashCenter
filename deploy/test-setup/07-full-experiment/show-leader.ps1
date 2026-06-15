#!/usr/bin/env pwsh
# 07-full-experiment/show-leader.ps1 — show which dashd is the current leader.

$ErrorActionPreference = 'SilentlyContinue'
$endpoints = @(
    @{ Name = "dashd-1"; URL = "http://127.0.0.1:27443/admin/health" },
    @{ Name = "dashd-2"; URL = "http://127.0.0.1:27453/admin/health" },
    @{ Name = "dashd-3"; URL = "http://127.0.0.1:27463/admin/health" }
)

foreach ($ep in $endpoints) {
    try {
        $r = Invoke-RestMethod -Uri $ep.URL -TimeoutSec 3 -ErrorAction Stop
        $role = if ($r.leader -eq $true) { "LEADER" } else { "follower" }
        Write-Host "$($ep.Name): $role  (dpus=$($r.dpus.Count) mode=$($r.mode))"
    } catch {
        Write-Host "$($ep.Name): unreachable"
    }
}
