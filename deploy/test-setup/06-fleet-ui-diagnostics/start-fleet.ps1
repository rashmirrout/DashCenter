#!/usr/bin/env pwsh
# 06-fleet-ui-diagnostics/start-fleet.ps1 — bring up the full DashCenter stack.
#
# Brings up: etcd + 10 dash-sim + 3 dashd (+ optionally dashw web console).
#
# Usage:
#   pwsh ./start-fleet.ps1                  # build + start (no dashw) + wait for leader + setup dashctl context
#   pwsh ./start-fleet.ps1 -WithConsole     # also build & start the dashw web console at :3000
#   pwsh ./start-fleet.ps1 -SkipBuild       # reuse cached images
#   pwsh ./start-fleet.ps1 -SkipContext     # skip the `dashctl config set-context` step
#   pwsh ./start-fleet.ps1 -ReadyTimeoutSec 120

[CmdletBinding()]
param(
    [switch]$WithConsole,
    [switch]$SkipBuild,
    [switch]$SkipContext,
    [int]$ReadyTimeoutSec = 90
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

Write-Host "==> Verifying docker is available" -ForegroundColor Cyan
docker version --format '{{.Server.Version}}' | Out-Null

# Core fleet (no dashw): etcd + 10 sims + 3 dashd
$coreServices = @(
    'etcd',
    'dash-sim-01','dash-sim-02','dash-sim-03','dash-sim-04','dash-sim-05',
    'dash-sim-06','dash-sim-07','dash-sim-08','dash-sim-09','dash-sim-10',
    'dashd-1','dashd-2','dashd-3'
)
$allServices = $coreServices
if ($WithConsole) { $allServices = $coreServices + 'dashw' }

if (-not $SkipBuild) {
    # Build only one of each image kind; compose reuses the cached image
    # for replicas (e.g. dash-sim-02..10 reuse the dash-sim-01 build).
    $buildTargets = @('dashd-1','dash-sim-01')
    if ($WithConsole) { $buildTargets += 'dashw' }
    Write-Host ("==> Building images: " + ($buildTargets -join ', ')) -ForegroundColor Cyan
    docker compose build @buildTargets
}

$svcLabel = if ($WithConsole) { '14 (incl. dashw)' } else { '13 (core)' }
Write-Host ("==> Starting fleet ({0} services)" -f $svcLabel) -ForegroundColor Cyan
docker compose up -d @allServices

Write-Host ("==> Waiting for a dashd leader to be elected (max {0}s)" -f $ReadyTimeoutSec) -ForegroundColor Cyan
$deadline = (Get-Date).AddSeconds($ReadyTimeoutSec)
$leader = $null
# NOTE: use 127.0.0.1 (not 'localhost') — on Windows 'localhost' may resolve
# to ::1 first and the IPv6 connect blocks ~5s before falling back to IPv4,
# which exceeds the per-request timeout and the loop falsely concludes
# "no leader".
while ((Get-Date) -lt $deadline) {
    foreach ($port in 37443, 37453, 37463) {
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

# Auto-configure a dashctl context. Look in this dir first, then 04-ha-fleet
# (a colocated build), then PATH. This points at dashd-1 by default; reads
# work from any node.
if (-not $SkipContext) {
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

    if ($dashctl) {
        & $dashctl config set-context fleet-ui-diagnostics `
            --endpoint http://127.0.0.1:38443 `
            --admin-endpoint http://127.0.0.1:37443 | Out-Null
        & $dashctl config use-context fleet-ui-diagnostics | Out-Null
        Write-Host "==> dashctl context 'fleet-ui-diagnostics' active (using $dashctl)" -ForegroundColor Green
    } else {
        Write-Host "   (dashctl not found in fleet dir, ../04-ha-fleet, or PATH — skipped context setup)" -ForegroundColor DarkGray
        Write-Host "   Build it once: pushd ../../../src/impl-go/dashctl; go build -o ../../../deploy/test-setup/06-fleet-ui-diagnostics/dashctl.exe ./cmd/dashctl; popd" -ForegroundColor DarkGray
    }
}

Write-Host ""
Write-Host "Per-node REST/admin endpoints (host):"
Write-Host "  dashd-1: http://127.0.0.1:38443  (admin :37443)"
Write-Host "  dashd-2: http://127.0.0.1:38453  (admin :37453)"
Write-Host "  dashd-3: http://127.0.0.1:38463  (admin :37463)"
if ($WithConsole) {
    Write-Host "  dashw  : http://localhost:3001   (web console)"
}
Write-Host ""
Write-Host "Confirm leader on every node:"
Write-Host "  pwsh ./show-leader.ps1"
Write-Host ""
Write-Host "Load the 157-object pre-built scenario:"
Write-Host "  pwsh ./provision.ps1                      # uses dashctl if found, else bootstrap.py"
Write-Host "  python manifest/bootstrap.py              # direct REST PUTs (no dashctl required)"
Write-Host ""
Write-Host "Clean fleet teardown:"
Write-Host "  pwsh ./stop-fleet.ps1                     # down + remove volumes (clean slate)"
Write-Host "  pwsh ./stop-fleet.ps1 -KeepVolumes        # down + keep etcd state"