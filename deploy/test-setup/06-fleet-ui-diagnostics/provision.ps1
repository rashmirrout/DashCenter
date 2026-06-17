#!/usr/bin/env pwsh
# 06-fleet-ui-diagnostics/provision.ps1 — load the rich superset (157 objects, 3 ns).
#
# Loads the YAML manifests under ./manifest/ into dashd. Prefers `dashctl
# apply -R -f` if a dashctl binary is reachable (1 RPC per file, generation
# tracking, dry-run support). Falls back to `python manifest/bootstrap.py`
# (pure-stdlib REST PUTs) if dashctl is not available.
#
# Usage:
#   pwsh ./provision.ps1                 # dashctl preferred, bootstrap.py fallback
#   pwsh ./provision.ps1 -UseBootstrap   # force bootstrap.py (skip dashctl)
#   pwsh ./provision.ps1 -DryRun         # dashctl --dry-run (no fallback)
#   pwsh ./provision.ps1 -Endpoint http://10.0.0.5:38443
#   pwsh ./provision.ps1 -MaxRetries 10 -MaxWaitSeconds 180   # retry transient errors

[CmdletBinding()]
param(
    [string]$Endpoint = 'http://127.0.0.1:38443',
    [int]$MaxRetries = 6,
    [int]$MaxWaitSeconds = 90,
    [switch]$UseBootstrap,
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

if ($MaxRetries -lt 1) { throw 'MaxRetries must be >= 1' }
if ($MaxWaitSeconds -lt 1) { throw 'MaxWaitSeconds must be >= 1' }

function Wait-ControlPlaneReady {
    param([string]$Url, [int]$MaxSeconds)
    $deadline = (Get-Date).AddSeconds($MaxSeconds)
    while ((Get-Date) -lt $deadline) {
        try {
            $null = Invoke-WebRequest -Uri "$Url/admin/health" -Method Get -TimeoutSec 2 -UseBasicParsing
            return
        } catch { Start-Sleep -Seconds 2 }
    }
    throw "Control plane not healthy at $Url after ${MaxSeconds}s"
}

function Test-TransientApplyError {
    param([string]$Text)
    return $Text -match '(?i)network error|connection reset by peer|\bEOF\b|context deadline exceeded|i/o timeout|connection refused|server closed idle connection'
}

$manifestDir = Join-Path $PSScriptRoot 'manifest'
if (-not (Test-Path $manifestDir)) {
    Write-Host "!! manifest/ not found at $manifestDir" -ForegroundColor Red
    exit 1
}

function Resolve-Dashctl {
    foreach ($candidate in @(
        (Join-Path $PSScriptRoot 'dashctl.exe'),
        (Join-Path $PSScriptRoot '..' '04-ha-fleet' 'dashctl.exe')
    )) {
        if (Test-Path $candidate) { return (Resolve-Path $candidate).Path }
    }
    $cmd = Get-Command 'dashctl' -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    return $null
}

$dashctl = if ($UseBootstrap) { $null } else { Resolve-Dashctl }

if ($dashctl) {
    Write-Host "==> Provisioning via dashctl ($dashctl)" -ForegroundColor Cyan
    # NOTE: dashctl `-R -f <dir>` would walk every file in manifest/ which
    # also contains bootstrap.py / bootstrap.sh / bootstrap.json. dashctl
    # tries to YAML-parse them and fails. So we enumerate *.yaml explicitly
    # and pass one -f per file. Order matters for FK validation: numeric
    # prefix (00–10) is preserved by Sort-Object.
    $yamls = Get-ChildItem -Path $manifestDir -Filter '*.yaml' | Sort-Object Name
    if (-not $yamls) {
        Write-Host "!! No *.yaml files found under $manifestDir" -ForegroundColor Red
        exit 1
    }
    $args = @(
        '--endpoint', $Endpoint,
        '--admin-endpoint', ($Endpoint -replace ':38443', ':37443'),
        'apply'
    )
    foreach ($y in $yamls) { $args += '-f'; $args += $y.FullName }
    if ($DryRun) { $args += '--dry-run'; $args += 'server' }
    $adminEp = $Endpoint -replace ':38443', ':37443'
    for ($attempt = 1; $attempt -le $MaxRetries; $attempt++) {
        Wait-ControlPlaneReady -Url $adminEp -MaxSeconds $MaxWaitSeconds
        Write-Host "==> dashctl apply (attempt $attempt/$MaxRetries)" -ForegroundColor Cyan
        $out = (& $dashctl @args 2>&1 | Out-String)
        $exit = $LASTEXITCODE
        if ($exit -eq 0) { Write-Host $out.TrimEnd(); break }
        Write-Warning $out.TrimEnd()
        if ((Test-TransientApplyError -Text $out) -and ($attempt -lt $MaxRetries)) {
            $backoff = [Math]::Min(20, $attempt * 2)
            Write-Warning "transient failure; retrying in ${backoff}s..."
            Start-Sleep -Seconds $backoff
            continue
        }
        Write-Host "!! dashctl apply failed (exit $exit). Try: pwsh ./provision.ps1 -UseBootstrap" -ForegroundColor Red
        exit $exit
    }
    Write-Host "==> dashctl apply complete" -ForegroundColor Green
} else {
    if ($DryRun) {
        Write-Host "!! -DryRun is only supported with dashctl. dashctl not found." -ForegroundColor Red
        exit 1
    }
    if (-not $UseBootstrap) {
        Write-Host "   (dashctl not found in fleet dir, ../04-ha-fleet, or PATH — using bootstrap.py)" -ForegroundColor DarkGray
    }
    Write-Host "==> Provisioning via bootstrap.py against $Endpoint" -ForegroundColor Cyan
    $python = (Get-Command 'python' -ErrorAction SilentlyContinue) `
        ?? (Get-Command 'python3' -ErrorAction SilentlyContinue)
    if (-not $python) {
        Write-Host "!! python not found on PATH." -ForegroundColor Red
        exit 1
    }
    & $python.Source (Join-Path $manifestDir 'bootstrap.py') $Endpoint
    if ($LASTEXITCODE -ne 0) {
        Write-Host "!! bootstrap.py exited $LASTEXITCODE" -ForegroundColor Red
        exit $LASTEXITCODE
    }
}

Write-Host ""
Write-Host "Verify loaded resources:"
Write-Host "  curl -s http://127.0.0.1:38443/v1/default/vnets | python -m json.tool"
Write-Host "  pwsh ./show-leader.ps1"