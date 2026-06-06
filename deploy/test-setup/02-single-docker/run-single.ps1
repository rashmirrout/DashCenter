#!/usr/bin/env pwsh
<#
.SYNOPSIS
  Run a single dash-sim container plus (optionally) a dash-redis-adapter
  container, picking the port plan from the active fleet config.

.PARAMETER Stop
  Tear down containers + network instead of starting.

.PARAMETER DeviceId
  Which DPU entry in the fleet config to launch. Default: the first.

.PARAMETER Config
  Override the fleet config path (see ../fleet.example.yaml for resolution order).

.PARAMETER NoAdapter
  Skip starting the dash-redis-adapter container even if enabled in the config.

.EXAMPLE
  pwsh -File .\run-single.ps1
  pwsh -File .\run-single.ps1 -DeviceId dpu-sim-02
  pwsh -File .\run-single.ps1 -Stop
#>
[CmdletBinding()]
param(
  [switch] $Stop,
  [string] $DeviceId,
  [string] $Config,
  [switch] $NoAdapter,
  [switch] $AllowPrivilegedPorts
)

$ErrorActionPreference = 'Stop'
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
Import-Module (Join-Path $here '..\lib\Fleet.psm1') -Force

$simName     = 'dc-single-dash-sim'
$adapterName = 'dc-single-dash-redis-adapter'

# Resolve config first so we know the network name + image tag for both
# stop and start paths.
$cfgPath = Resolve-FleetConfigPath -Config $Config
$cfg     = Import-FleetConfig -Path $cfgPath
if ($AllowPrivilegedPorts) {
    Test-FleetConfig -Config $cfg -AllowPrivilegedPorts
} else {
    Test-FleetConfig -Config $cfg
}
$network = $cfg.Defaults['network'] + '-single'    # isolate from topology 03
$tag     = $cfg.Defaults['imageTag']
$bindHost= $cfg.Defaults['bindHost']

if ($Stop) {
    Write-Host '==> Stopping single-DPU topology' -ForegroundColor Cyan
    docker rm -f $simName     2>$null | Out-Null
    docker rm -f $adapterName 2>$null | Out-Null
    docker network rm $network 2>$null | Out-Null
    Write-Host 'Done.' -ForegroundColor Green
    return
}

# Pick DPU.
$dpu = $null
if ($DeviceId) {
    $dpu = $cfg.Dpus | Where-Object { $_.deviceId -eq $DeviceId } | Select-Object -First 1
    if (-not $dpu) { throw "DeviceId '$DeviceId' not found in $cfgPath" }
} else {
    $dpu = $cfg.Dpus[0]
    Write-Host "==> No -DeviceId specified; using $($dpu.deviceId)" -ForegroundColor Cyan
}
Write-Host "==> Fleet config: $cfgPath" -ForegroundColor Cyan

$imgSim     = "dashcenter/dash-sim:$($dpu.imageTag)"
$imgAdapter = "dashcenter/dash-redis-adapter:$tag"
$imgCli     = "dashcenter/dash-sim-client:$tag"

# Check images exist.
$missing = @()
$required = @($imgSim, $imgCli)
if ($cfg.Adapter -and $cfg.Adapter.enabled -and -not $NoAdapter) { $required += $imgAdapter }
foreach ($img in $required) {
    docker image inspect $img *>$null
    if ($LASTEXITCODE -ne 0) { $missing += $img }
}
if ($missing.Count -gt 0) {
    throw "Missing image(s): $($missing -join ', ')`nRun: pwsh -File .\build-images.ps1"
}

# Network (idempotent).
docker network inspect $network *>$null
if ($LASTEXITCODE -ne 0) {
    Write-Host "==> Creating network $network" -ForegroundColor Cyan
    docker network create $network | Out-Null
}

# Idempotent removal.
docker rm -f $simName     2>$null | Out-Null
docker rm -f $adapterName 2>$null | Out-Null

# ---------- dash-sim ----------
$scenarioMountSrc = (Join-Path (Get-TestSetupRoot) 'scenarios').Replace('\','/')
$scenarioRel = $dpu.scenario
$scenarioBase = if ($scenarioRel) { Split-Path -Leaf $scenarioRel } else { $null }

$simArgs = @(
    'run','-d','--name', $simName, '--network', $network,
    '-p', "${bindHost}:$($dpu.grpcPort):50051",
    '-p', "${bindHost}:$($dpu.adminPort):8080",
    '-v', "${scenarioMountSrc}:/scenarios:ro",
    $imgSim,
    '--grpc-listen=:50051',
    '--admin-listen=:8080',
    "--device-id=$($dpu.deviceId)"
)
if ($scenarioBase) { $simArgs += "--scenario=/scenarios/$scenarioBase" }
foreach ($a in $dpu.extraArgs) { $simArgs += $a }

Write-Host "==> Starting $simName" -ForegroundColor Cyan
& docker @simArgs | Out-Null

# ---------- dash-redis-adapter (optional) ----------
if ($cfg.Adapter -and $cfg.Adapter.enabled -and -not $NoAdapter) {
    $adArgs = @(
        'run','-d','--name', $adapterName, '--network', $network,
        '-p', "${bindHost}:$($cfg.Adapter.grpcPort):52051",
        $imgAdapter,
        '--grpc-listen=:52051'
    )
    switch ($cfg.Adapter.redis.mode) {
        'embedded' { $adArgs += '--embedded-redis' }
        'external' { $adArgs += "--redis=$($cfg.Adapter.redis.address)" }
        'container'{ throw "adapter.redis.mode='container' is not supported in topology 02 (use topology 03)." }
    }
    Write-Host "==> Starting $adapterName" -ForegroundColor Cyan
    & docker @adArgs | Out-Null
}

Start-Sleep -Seconds 1

Write-Host ''
Write-Host '==> Containers:' -ForegroundColor Green
docker ps --filter "name=dc-single-" --format "table {{.Names}}\t{{.Image}}\t{{.Ports}}\t{{.Status}}"

Write-Host ''
Write-Host '==> Smoke test (via CLI container, on the docker network):' -ForegroundColor Cyan
docker run --rm --network $network $imgCli --target "${simName}:50051" ping
if ($cfg.Adapter -and $cfg.Adapter.enabled -and -not $NoAdapter) {
    docker run --rm --network $network $imgCli --target "${adapterName}:52051" ping
}

Write-Host ''
Write-Host "Drive from host: dash-sim-client --target ${bindHost}:$($dpu.grpcPort) ping"
Write-Host 'Tear down:       pwsh -File .\run-single.ps1 -Stop'
