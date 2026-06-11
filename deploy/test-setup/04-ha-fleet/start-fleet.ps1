#!/usr/bin/env pwsh
# 04-ha-fleet/start-fleet.ps1 — bring up the full HA fleet.
#
# Usage:
#   pwsh ./start-fleet.ps1              # build + start + wait for leader + setup dashctl context
#   pwsh ./start-fleet.ps1 -SkipBuild   # reuse cached images
#   pwsh ./start-fleet.ps1 -SkipContext # skip the `dashctl config set-context ha-fleet` step

[CmdletBinding()]
param(
    [switch]$SkipBuild,
    [switch]$SkipContext,
    [int]$ReadyTimeoutSec = 60
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

Write-Host "==> Verifying docker is available" -ForegroundColor Cyan
docker version --format '{{.Server.Version}}' | Out-Null

if (-not $SkipBuild) {
    Write-Host "==> Building dashd, dashctl, dash-sim images" -ForegroundColor Cyan
    docker compose build dashd-1 dashctl dash-sim-1
}

Write-Host "==> Starting fleet (etcd + 5 dash-sim + 3 dashd)" -ForegroundColor Cyan
docker compose up -d etcd dash-sim-1 dash-sim-2 dash-sim-3 dash-sim-4 dash-sim-5 dashd-1 dashd-2 dashd-3

Write-Host "==> Waiting for a dashd leader to be elected (max $ReadyTimeoutSec s)" -ForegroundColor Cyan
$deadline = (Get-Date).AddSeconds($ReadyTimeoutSec)
$leader = $null
# NOTE: use 127.0.0.1 (not 'localhost'). On Windows 'localhost' resolves to
# ::1 first; dashd listens IPv4-only on the mapped port, so the IPv6 connect
# blocks ~5s before the v4 fallback even tries — which exceeds our per-request
# timeout and the loop falsely concludes "no leader".
while ((Get-Date) -lt $deadline) {
    foreach ($port in 27443, 27453, 27463) {
        try {
            $r = Invoke-RestMethod -Uri "http://127.0.0.1:$port/admin/leader" -TimeoutSec 3 -ErrorAction Stop
            if ($r.leader) { $leader = $r.leader_id; break }
        } catch { }
    }
    if ($leader) { break }
    Start-Sleep -Milliseconds 500
}

if (-not $leader) {
    Write-Host "!! No leader within ${ReadyTimeoutSec}s. Check 'docker compose logs dashd-1 dashd-2 dashd-3'." -ForegroundColor Red
    exit 1
}

Write-Host "==> Leader: $leader" -ForegroundColor Green
Write-Host ""

# Auto-configure a dashctl context so subsequent commands don't need
# --endpoint / --admin-endpoint. This points at dashd-1 by default; reads
# work from any node because dashd serves linearizable reads from etcd
# (followers do not forward). For writes against a known leader, override
# with --endpoint http://127.0.0.1:284{43|53|63}.
$dashctl = Join-Path $PSScriptRoot 'dashctl.exe'
if (-not $SkipContext -and (Test-Path $dashctl)) {
    & $dashctl config set-context ha-fleet `
        --endpoint http://127.0.0.1:28443 `
        --admin-endpoint http://127.0.0.1:27443 | Out-Null
    & $dashctl config use-context ha-fleet | Out-Null
    Write-Host "==> dashctl context 'ha-fleet' active (no --endpoint needed)" -ForegroundColor Green
} elseif (-not $SkipContext) {
    Write-Host "   (dashctl.exe not in fleet dir — skipped context setup; build it or pass -SkipContext to silence)" -ForegroundColor DarkGray
}
Write-Host ""
Write-Host "Per-node REST/admin endpoints (host):"
Write-Host "  dashd-1: http://127.0.0.1:28443  (admin :27443)"
Write-Host "  dashd-2: http://127.0.0.1:28453  (admin :27453)"
Write-Host "  dashd-3: http://127.0.0.1:28463  (admin :27463)"
Write-Host ""
Write-Host "Confirm leader on every node:"
Write-Host "  pwsh ./show-leader.ps1"
Write-Host ""
Write-Host "Apply the pre-built manifest set (~130 objects across 2 namespaces):"
Write-Host "  ./dashctl.exe apply -R -f ./manifest"
