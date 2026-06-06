#!/usr/bin/env pwsh
<#
.SYNOPSIS
  Launch a multi-DPU DashCenter fleet on the local host (native processes),
  driven by a fleet.{yaml,json} config file.

.DESCRIPTION
  Reads the fleet configuration (see ../fleet.example.yaml), validates
  it, then spawns one dash-sim.exe per DPU entry and (optionally) one
  dash-redis-adapter.exe. PIDs and log paths are recorded in
  .fleet-state.json so stop-fleet.ps1 can tear the fleet down cleanly.

.PARAMETER Config
  Path to a fleet config file. Resolution order:
    1. -Config <path>
    2. $env:DASHCENTER_FLEET_CONFIG
    3. <test-setup>/fleet.yaml
    4. <test-setup>/fleet.json
    5. <test-setup>/fleet.example.yaml (fallback, with warning)

.PARAMETER BinDir
  Override the binary directory. Default: <repo>/src/impl-go/bin

.PARAMETER AllowPrivilegedPorts
  Permit any port < 1024 in the config.

.PARAMETER NoSmokeTest
  Skip the post-start dash-sim-client ping smoke test.

.EXAMPLE
  pwsh -File .\start-fleet.ps1
  pwsh -File .\start-fleet.ps1 -Config ..\my-fleet.json
#>
[CmdletBinding()]
param(
  [string] $Config,
  [string] $BinDir,
  [switch] $AllowPrivilegedPorts,
  [switch] $NoSmokeTest
)

$ErrorActionPreference = 'Stop'
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
Import-Module (Join-Path $here '..\lib\Fleet.psm1') -Force

# ---------- Resolve + validate config ----------
$cfgPath = Resolve-FleetConfigPath -Config $Config
$cfg     = Import-FleetConfig -Path $cfgPath
if ($AllowPrivilegedPorts) {
    Test-FleetConfig -Config $cfg -AllowPrivilegedPorts
} else {
    Test-FleetConfig -Config $cfg
}
Write-Host "==> Fleet config: $cfgPath" -ForegroundColor Cyan

# ---------- Locate binaries ----------
if (-not $BinDir) {
    $BinDir = Join-Path (Get-RepoRoot) 'src/impl-go/bin'
}
if (-not (Test-Path $BinDir)) {
    throw "Binary directory not found: $BinDir`nBuild first: see src/impl-go/dash-sim/RUN_AND_TEST.md"
}
$dashSim     = Join-Path $BinDir 'dash-sim.exe'
$dashAdapter = Join-Path $BinDir 'dash-redis-adapter.exe'
$dashCli     = Join-Path $BinDir 'dash-sim-client.exe'
foreach ($f in @($dashSim, $dashAdapter)) {
    if (-not (Test-Path $f)) { throw "Missing binary: $f" }
}

# ---------- Pre-flight: state file + log dir ----------
$logsDir = Join-Path $here 'logs'
New-Item -ItemType Directory -Path $logsDir -Force | Out-Null
$statePath = Join-Path $here '.fleet-state.json'
if (Test-Path $statePath) {
    Write-Warning "Existing $statePath found. Run stop-fleet.ps1 first if a previous fleet is still up."
}

function Start-Component {
    param(
        [string]   $Name,
        [string]   $Exe,
        [string[]] $ArgList,
        [string]   $LogFile
    )
    Write-Host "==> Starting $Name" -ForegroundColor Cyan
    Write-Host "    $Exe $($ArgList -join ' ')"
    Write-Host "    log: $LogFile"
    $p = Start-Process -FilePath $Exe `
                       -ArgumentList $ArgList `
                       -RedirectStandardOutput $LogFile `
                       -RedirectStandardError  "$LogFile.err" `
                       -PassThru `
                       -WindowStyle Hidden
    Start-Sleep -Milliseconds 300
    if ($p.HasExited) {
        Get-Content $LogFile        -ErrorAction SilentlyContinue | Write-Host
        Get-Content "$LogFile.err"  -ErrorAction SilentlyContinue | Write-Host
        throw "$Name exited immediately (exit code $($p.ExitCode))"
    }
    return $p
}

$fleet = @()
try {
    # ---------- dash-sim per DPU ----------
    foreach ($d in $cfg.Dpus) {
        $name = "dash-sim/$($d.deviceId)"
        $log  = Join-Path $logsDir "$($d.deviceId).log"
        $args = @(
            '--grpc-listen',  ":$($d.grpcPort)",
            '--admin-listen', ":$($d.adminPort)",
            '--device-id',    $d.deviceId,
            '--tick-interval',$cfg.Defaults['tickInterval']
        )
        if ($d.scenario) {
            $scenarioPath = Resolve-FleetPath -Base $cfg.BaseDir -Path $d.scenario
            $args += @('--scenario', $scenarioPath)
        }
        foreach ($arg in $d.extraArgs) { $args += $arg }

        $p = Start-Component -Name $name -Exe $dashSim -ArgList $args -LogFile $log
        $fleet += [pscustomobject]@{
            role      = 'dash-sim'
            device_id = $d.deviceId
            pid       = $p.Id
            grpc      = "$($cfg.Defaults['bindHost']):$($d.grpcPort)"
            admin     = "http://$($cfg.Defaults['bindHost']):$($d.adminPort)"
            log       = $log
        }
    }

    # ---------- dash-redis-adapter ----------
    if ($cfg.Adapter -and $cfg.Adapter.enabled) {
        $name = 'dash-redis-adapter'
        $log  = Join-Path $logsDir "$name.log"
        $args = @('--grpc-listen', ":$($cfg.Adapter.grpcPort)")
        switch ($cfg.Adapter.redis.mode) {
            'embedded'  { $args += '--embedded-redis' }
            'external'  { $args += @('--redis', $cfg.Adapter.redis.address) }
            'container' {
                throw "adapter.redis.mode='container' is only supported in topology 03 (docker compose). Use 'embedded' or 'external' for native processes."
            }
        }
        $p = Start-Component -Name $name -Exe $dashAdapter -ArgList $args -LogFile $log
        $fleet += [pscustomobject]@{
            role      = 'dash-redis-adapter'
            device_id = 'redis-adapter'
            pid       = $p.Id
            grpc      = "$($cfg.Defaults['bindHost']):$($cfg.Adapter.grpcPort)"
            admin     = $null
            log       = $log
        }
    }
}
catch {
    Write-Error $_
    foreach ($entry in $fleet) {
        try { Stop-Process -Id $entry.pid -Force -ErrorAction SilentlyContinue } catch {}
    }
    throw
}

$state = [pscustomobject]@{
    started_at  = (Get-Date).ToString('o')
    config_path = $cfg.SourcePath
    bin_dir     = $BinDir
    components  = $fleet
}
$state | ConvertTo-Json -Depth 5 | Set-Content -Path $statePath -Encoding UTF8

Write-Host ''
Write-Host "==> Fleet up. State: $statePath" -ForegroundColor Green
$fleet | Format-Table role, device_id, pid, grpc, admin -AutoSize

if (-not $NoSmokeTest -and (Test-Path $dashCli)) {
    Write-Host 'Smoke test:' -ForegroundColor Cyan
    foreach ($e in $fleet) {
        Write-Host "  $($e.grpc) ($($e.role)) ..." -NoNewline
        try {
            & $dashCli --target $e.grpc ping | Out-Null
            Write-Host ' OK' -ForegroundColor Green
        } catch {
            Write-Host " FAIL ($($_.Exception.Message))" -ForegroundColor Red
        }
    }
} elseif (-not (Test-Path $dashCli)) {
    Write-Warning "dash-sim-client.exe not found at $dashCli — skipping smoke test."
}

Write-Host ''
Write-Host 'Stop with: pwsh -File .\stop-fleet.ps1'
