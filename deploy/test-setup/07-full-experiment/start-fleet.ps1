#!/usr/bin/env pwsh
# 07-full-experiment/start-fleet.ps1 — bring up the 50-DPU experiment stack.
#
# Brings up: 2 etcd + 50 dash-sim + 3 dashd (+ optionally dashw web console).
#
# Usage:
#   pwsh ./start-fleet.ps1                  # build + start (no dashw) + wait for leader
#   pwsh ./start-fleet.ps1 -WithConsole     # also start dashw at :3000
#   pwsh ./start-fleet.ps1 -SkipBuild       # reuse cached images
#   pwsh ./start-fleet.ps1 -ReadyTimeoutSec 180

[CmdletBinding()]
param(
    [switch]$WithConsole,
    [switch]$SkipBuild,
    [int]$ReadyTimeoutSec = 180
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

Write-Host "==> Verifying docker is available" -ForegroundColor Cyan
docker version --format '{{.Server.Version}}' | Out-Null

# Core fleet: 2 etcd + 50 sims + 3 dashd
$simNames = 1..50 | ForEach-Object { "dash-sim-{0:D2}" -f $_ }
$coreServices = @('etcd-1', 'etcd-2') + $simNames + @('dashd-1', 'dashd-2', 'dashd-3')
$allServices = $coreServices
if ($WithConsole) { $allServices = $coreServices + 'dashw' }

if (-not $SkipBuild) {
    Write-Host "==> Building images (dash-sim, dashd$(if ($WithConsole) {', dashw'}))" -ForegroundColor Cyan
    $buildTargets = @('dash-sim-01', 'dashd-1')
    if ($WithConsole) { $buildTargets += 'dashw' }
    docker compose build @buildTargets 2>&1 | Out-Null
    Write-Host "    Build done" -ForegroundColor Green
}

Write-Host "==> Starting $($allServices.Count) services" -ForegroundColor Cyan
docker compose up -d @allServices

Write-Host "==> Waiting for etcd cluster health" -ForegroundColor Cyan
$deadline = (Get-Date).AddSeconds(60)
while ((Get-Date) -lt $deadline) {
    try {
        $h = docker exec dc-exp-etcd-1 etcdctl endpoint health --endpoints=http://127.0.0.1:2379 2>&1
        if ($h -match 'true') { Write-Host "    etcd-1 healthy" -ForegroundColor Green; break }
    } catch {}
    Start-Sleep -Seconds 2
}

Write-Host "==> Waiting for dashd leader election (timeout ${ReadyTimeoutSec}s)" -ForegroundColor Cyan
$deadline = (Get-Date).AddSeconds($ReadyTimeoutSec)
while ((Get-Date) -lt $deadline) {
    try {
        $r = Invoke-RestMethod -Uri "http://127.0.0.1:27443/admin/health" -TimeoutSec 3 -ErrorAction Stop
        if ($r.leader -eq $true) {
            Write-Host "    Leader: $($r.node_id)" -ForegroundColor Green
            break
        }
    } catch {}
    Start-Sleep -Seconds 3
}
if ((Get-Date) -ge $deadline) {
    Write-Host "    WARNING: No leader within ${ReadyTimeoutSec}s — fleet may still be converging" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "Fleet is up: 50 DPU sims + 3 dashd + 2 etcd" -ForegroundColor Green
Write-Host "  REST:  http://localhost:28443" -ForegroundColor Cyan
Write-Host "  Admin: http://localhost:27443" -ForegroundColor Cyan
if ($WithConsole) {
    Write-Host "  Console: http://localhost:3000" -ForegroundColor Cyan
}
Write-Host ""
Write-Host "Next: pwsh ./provision.ps1" -ForegroundColor Yellow
