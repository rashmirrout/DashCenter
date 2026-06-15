#!/usr/bin/env pwsh
<#
.SYNOPSIS
  Render deploy/test-setup/03-multi-docker-fleet/docker-compose.fleet.yaml
  + .env from the active fleet.{yaml,json}.

.DESCRIPTION
  Invoked automatically by 03-multi-docker-fleet's wrapper, or
  on demand: `pwsh -File .\lib\render-compose.ps1`.

  Re-run whenever fleet.{yaml,json} changes. The output files are
  marked AUTO-GENERATED and listed in 03-multi-docker-fleet/.gitignore.

.PARAMETER Config
  Path to a fleet config file. Default: resolved via Resolve-FleetConfigPath.

.PARAMETER Out
  Output compose file. Default:
  <test-setup>/03-multi-docker-fleet/docker-compose.fleet.yaml
#>
[CmdletBinding()]
param(
  [string] $Config,
  [string] $Out
)

$ErrorActionPreference = 'Stop'
Import-Module (Join-Path $PSScriptRoot 'Fleet.psm1') -Force

$cfgPath = Resolve-FleetConfigPath -Config $Config
$cfg     = Import-FleetConfig -Path $cfgPath
Test-FleetConfig -Config $cfg

$setupRoot = Get-TestSetupRoot
$repoRoot  = Get-RepoRoot
if (-not $Out) {
    $Out = Join-Path $setupRoot '03-multi-docker-fleet/docker-compose.fleet.yaml'
}
$envFile   = Join-Path (Split-Path -Parent $Out) '.env'
$composeDir= Split-Path -Parent $Out

# Compose path expressions must be relative to the compose file so the
# docker build context resolves cleanly on every host.
function _PosixRel($from, $to) {
    $rel = [IO.Path]::GetRelativePath($from, $to)
    return $rel -replace '\\','/'
}

$buildContext   = _PosixRel $composeDir $repoRoot
$scenariosMount = _PosixRel $composeDir (Join-Path $setupRoot 'scenarios')

$bindHost = $cfg.Defaults['bindHost']
$network  = $cfg.Defaults['network']
$imageTag = $cfg.Defaults['imageTag']

$sb = New-Object System.Text.StringBuilder
function _W([string]$line = '') { [void]$sb.AppendLine($line) }

_W "# AUTO-GENERATED from $($cfg.SourcePath) — DO NOT EDIT."
_W "# Re-run: pwsh -File ../lib/render-compose.ps1   (or ../lib/render-compose.sh)"
_W "# Source of truth: deploy/test-setup/fleet.{yaml,json}"
_W ""
_W "services:"

# Optional redis container.
if ($cfg.Adapter -and $cfg.Adapter.enabled -and $cfg.Adapter.redis.mode -eq 'container') {
    $redisPort = $cfg.Adapter.redis.hostPort
    _W "  redis:"
    _W "    image: redis:7-alpine"
    _W "    container_name: dc-redis-fleet"
    _W "    ports: [`"${bindHost}:${redisPort}:6379`"]"
    _W "    restart: unless-stopped"
    _W "    networks: [`"$network`"]"
    _W ""
}

# One service per DPU.
foreach ($d in $cfg.Dpus) {
    $svc       = "dash-sim-$($d.deviceId)"
    $container = "dc-$svc"
    $tag       = $d.imageTag
    $scenarioRel = $d.scenario
    if (-not $scenarioRel) { $scenarioRel = $cfg.Defaults['scenario'] }
    $scenarioBase = if ($scenarioRel) { Split-Path -Leaf $scenarioRel } else { $null }

    _W "  ${svc}:"
    _W "    build:"
    _W "      context: $buildContext"
    _W "      dockerfile: src/impl-go/dash-sim/Dockerfile"
    _W "    image: dashcenter/dash-sim:$tag"
    _W "    container_name: $container"
    _W "    restart: unless-stopped"
    _W "    networks: [`"$network`"]"
    _W "    volumes:"
    _W "      - ${scenariosMount}:/scenarios:ro"
    _W "    command:"
    _W "      - --grpc-listen=:50051"
    _W "      - --admin-listen=:8080"
    _W "      - --device-id=$($d.deviceId)"
    if ($scenarioBase) {
        _W "      - --scenario=/scenarios/$scenarioBase"
    }
    foreach ($arg in $d.extraArgs) { _W "      - $arg" }
    _W "    ports:"
    _W "      - `"${bindHost}:$($d.grpcPort):50051`""
    _W "      - `"${bindHost}:$($d.adminPort):8080`""
    _W ""
}

# Adapter.
if ($cfg.Adapter -and $cfg.Adapter.enabled) {
    _W "  dash-redis-adapter:"
    _W "    build:"
    _W "      context: $buildContext"
    _W "      dockerfile: src/impl-go/dash-redis-adapter/Dockerfile"
    _W "    image: dashcenter/dash-redis-adapter:$imageTag"
    _W "    container_name: dc-dash-redis-adapter"
    _W "    restart: unless-stopped"
    _W "    networks: [`"$network`"]"
    _W "    command:"
    _W "      - --grpc-listen=:52051"
    switch ($cfg.Adapter.redis.mode) {
        'embedded' {
            _W "      - --embedded-redis"
        }
        'external' {
            _W "      - --redis=$($cfg.Adapter.redis.address)"
        }
        'container' {
            _W "      - --redis=redis:6379"
            _W "    depends_on: [`"redis`"]"
        }
    }
    _W "    ports:"
    _W "      - `"${bindHost}:$($cfg.Adapter.grpcPort):52051`""
    _W ""
}

# CLI helper (profile=cli, one-shot).
_W "  cli:"
_W "    build:"
_W "      context: $buildContext"
_W "      dockerfile: src/impl-go/dash-sim-client/Dockerfile"
_W "    image: dashcenter/dash-sim-client:$imageTag"
_W "    profiles: [`"cli`"]"
_W "    networks: [`"$network`"]"
_W "    entrypoint: [`"/usr/local/bin/dash-sim-client`"]"
_W ""

_W "networks:"
_W "  ${network}:"
_W "    name: $network"

# Ensure output dir exists.
New-Item -ItemType Directory -Path $composeDir -Force | Out-Null
Set-Content -Path $Out -Value $sb.ToString() -Encoding UTF8

# .env (informational only; consumed by `docker compose --env-file`).
@(
    "# AUTO-GENERATED from $($cfg.SourcePath) — DO NOT EDIT.",
    "DC_FLEET_CONFIG=$($cfg.SourcePath)",
    "DC_IMAGE_TAG=$imageTag",
    "DC_BIND_HOST=$bindHost",
    "DC_NETWORK=$network"
) | Set-Content -Path $envFile -Encoding UTF8

Write-Host "wrote $Out"
Write-Host "wrote $envFile"
Write-Host ""
Write-Host "Next: docker compose -f `"$Out`" up -d --build"
