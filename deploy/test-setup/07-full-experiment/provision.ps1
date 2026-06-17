#!/usr/bin/env pwsh
# 07-full-experiment/provision.ps1 — apply the 449-object manifest set.
#
# Applies manifests in dependency order:
#   00-vnets (25) → 01-service-tunnels (5) → 02-enis (120) →
#   03-vnet-mappings (240) → 04-route-policies (17) → 05-acl-policies (32) →
#   06-ha-sets (10)
#
# Total: ~449 objects across 50 DPUs.

[CmdletBinding()]
param(
    [string]$Endpoint = "http://localhost:28443",
    [string]$AdminEndpoint,
    [int]$MaxRetries = 6,
    [int]$MaxWaitSeconds = 90,
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

if (-not $AdminEndpoint) {
    $AdminEndpoint = if ($env:DASHCTL_ADMIN_ENDPOINT) { $env:DASHCTL_ADMIN_ENDPOINT } else { $Endpoint -replace ':28443', ':27443' }
}
if ($MaxRetries -lt 1) { throw "MaxRetries must be >= 1" }
if ($MaxWaitSeconds -lt 1) { throw "MaxWaitSeconds must be >= 1" }

# Try host dashctl first, fall back to container
$dashctl = $null
$hostBin = Join-Path $PSScriptRoot "../../.." "src/impl-go/dashctl/bin/dashctl.exe"
if (Test-Path $hostBin) {
    $dashctl = { param($args_) & $hostBin --endpoint $Endpoint --insecure @args_ }
    Write-Host "Using host dashctl: $hostBin" -ForegroundColor Cyan
} else {
    $dashctl = { param($args_) docker compose run --rm dashctl @args_ }
    Write-Host "Using container dashctl" -ForegroundColor Cyan
}

$forceFlag = @()
if ($Force) { $forceFlag = @('--force') }

function Wait-ControlPlaneReady {
    param(
        [string]$Url,
        [int]$MaxSeconds
    )
    $deadline = (Get-Date).AddSeconds($MaxSeconds)
    while ((Get-Date) -lt $deadline) {
        try {
            $null = Invoke-WebRequest -Uri "$Url/admin/health" -Method Get -TimeoutSec 2 -UseBasicParsing
            return
        } catch {
            Start-Sleep -Seconds 2
        }
    }
    throw "Control plane not healthy at $Url after ${MaxSeconds}s"
}

function Is-TransientApplyError {
    param([string]$Text)
    return $Text -match '(?i)network error|connection reset by peer|\bEOF\b|context deadline exceeded|i/o timeout|connection refused|server closed idle connection'
}

function Apply-ManifestWithRetry {
    param(
        [string]$Manifest,
        [int]$Retries
    )

    for ($attempt = 1; $attempt -le $Retries; $attempt++) {
        Wait-ControlPlaneReady -Url $AdminEndpoint -MaxSeconds $MaxWaitSeconds
        Write-Host "==> Applying $Manifest (attempt $attempt/$Retries)" -ForegroundColor Cyan

        $allArgs = @('apply', '-f', $Manifest) + $forceFlag
        $output = (& $dashctl $allArgs 2>&1 | Out-String)
        $exitCode = $LASTEXITCODE

        if ($exitCode -eq 0) {
            Write-Host $output.TrimEnd()
            return
        }

        Write-Warning ($output.TrimEnd())
        if (Is-TransientApplyError -Text $output) {
            if ($attempt -lt $Retries) {
                $backoff = [Math]::Min(20, $attempt * 2)
                Write-Warning "Transient apply failure. Retrying in ${backoff}s..."
                Start-Sleep -Seconds $backoff
                continue
            }
            throw "Exhausted retries for $Manifest"
        }

        throw "Non-transient apply failure for $Manifest"
    }
}

$manifests = @(
    "manifest/00-vnets.yaml",
    "manifest/01-service-tunnels.yaml",
    "manifest/02-enis.yaml",
    "manifest/03-vnet-mappings.yaml",
    "manifest/04-route-policies.yaml",
    "manifest/05-acl-policies.yaml",
    "manifest/06-ha-sets.yaml"
)

$total = 0
foreach ($m in $manifests) {
    Apply-ManifestWithRetry -Manifest $m -Retries $MaxRetries
    $total++
}

Write-Host ""
Write-Host "Provisioned $total manifest files (~449 objects)" -ForegroundColor Green
Write-Host ""
Write-Host "Verify:" -ForegroundColor Yellow
Write-Host "  dashctl get vnet -o table           # 25 VNets"
Write-Host "  dashctl get eni -o wide             # 120 ENIs (2-3 per DPU)"
Write-Host "  dashctl get vnet-mapping -o table    # 240 mappings"
Write-Host "  dashctl get route-policy -o table    # 17 route policies"
Write-Host "  dashctl get acl-policy -o table      # 32 ACL policies"
Write-Host "  dashctl get ha-set -o table          # 10 HA sets"
Write-Host "  dashctl dpu list -o table            # 50 DPUs"
