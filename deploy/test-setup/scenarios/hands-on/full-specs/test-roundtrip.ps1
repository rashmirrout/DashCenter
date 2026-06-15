#!/usr/bin/env pwsh
<#
.SYNOPSIS
    Roundtrip tester for the full-specs library.

.DESCRIPTION
    Applies every full-spec YAML against a running dashd, verifies the
    expected resources exist (by label), tears them down, and verifies
    the namespace is clean again.

    Each full-spec file labels every object with `demo=<filename>` so
    apply/delete can be verified independently.

.PARAMETER Action
    apply  - dashctl apply -f for every spec file
    verify - count resources with each demo label and report
    delete - dashctl delete -f for every spec file (--ignore-not-found)
    test   - apply, verify, delete, verify-gone (full roundtrip)

.PARAMETER Endpoint
    dashd REST endpoint. Defaults to $env:DASHCTL_ENDPOINT, then http://localhost:28443.

.PARAMETER Dashctl
    Path to dashctl binary. Auto-discovers test-setup/04-ha-fleet/dashctl.exe.

.EXAMPLE
    pwsh ./test-roundtrip.ps1 -Action test
#>
[CmdletBinding()]
param(
    [ValidateSet('apply','verify','delete','test')]
    [string]$Action = 'test',
    [string]$Endpoint = '',
    [string]$Dashctl  = ''
)

$ErrorActionPreference = 'Stop'
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

# ── Resolve endpoint ────────────────────────────────────────────────────
if (-not $Endpoint) {
    $Endpoint = if ($env:DASHCTL_ENDPOINT) { $env:DASHCTL_ENDPOINT } else { 'http://localhost:28443' }
}
$env:DASHCTL_ENDPOINT = $Endpoint

# ── Resolve dashctl binary ──────────────────────────────────────────────
if (-not $Dashctl) {
    $candidates = @(
        (Join-Path $scriptDir '..\..\..\..\..\src\impl-go\dashctl\dashctl.exe'),
        (Join-Path $scriptDir '..\..\..\04-ha-fleet\dashctl.exe'),
        (Join-Path $scriptDir '..\..\..\05-full-console\dashctl.exe'),
        (Join-Path $scriptDir '..\..\..\07-full-experiment\dashctl.exe'),
        'dashctl'
    )
    foreach ($c in $candidates) {
        if (Test-Path $c) { $Dashctl = (Resolve-Path $c).Path; break }
        $cmd = Get-Command $c -ErrorAction SilentlyContinue
        if ($cmd) { $Dashctl = $cmd.Source; break }
    }
}
if (-not $Dashctl -or -not (Test-Path $Dashctl)) {
    if (-not (Get-Command 'dashctl' -ErrorAction SilentlyContinue)) {
        throw "dashctl not found. Pass -Dashctl <path> or put it on PATH."
    }
    $Dashctl = 'dashctl'
}

# ── Spec files (filename → demo label) ──────────────────────────────────
$specs = [ordered]@{
    'eni-full.yaml'            = 'eni-full'
    'vnet-full.yaml'           = 'vnet-full'
    'route-full.yaml'          = 'route-full'
    'mapping-full.yaml'        = 'mapping-full'
    'acl-full.yaml'            = 'acl-full'
    'service-tunnel-full.yaml' = 'st-full'
    'private-link-full.yaml'   = 'pl-full'
    'ha-full.yaml'             = 'ha-full'
}

$kinds = @('vnet','service-tunnel','eni','vnet-mapping','route-policy','acl-policy','ha-set')

function Run-Dashctl {
    param([Parameter(ValueFromRemainingArguments)] [string[]]$args)
    & $Dashctl @args
    $script:LastDashctlExit = $LASTEXITCODE
}

function Apply-Specs {
    Write-Host "==> Apply (endpoint=$Endpoint)" -ForegroundColor Cyan
    foreach ($file in $specs.Keys) {
        $path = Join-Path $scriptDir $file
        Write-Host "    apply $file" -ForegroundColor Gray
        & $Dashctl 'apply' '-f' $path '--force'
        $code = $LASTEXITCODE
        if ($code -ne 0) {
            Write-Host "    ! apply $file exited $code" -ForegroundColor Yellow
        }
    }
}

function Count-Label {
    param([string]$Label)
    $total = 0
    foreach ($k in $kinds) {
        $out = & $Dashctl 'get' $k '-l' "demo=$Label" '-o' 'name' 2>$null
        if ($LASTEXITCODE -eq 0 -and $out) {
            $total += @($out | Where-Object { $_ -match '\S' }).Count
        }
    }
    return $total
}

function Verify-Specs {
    param([switch]$ExpectGone)
    $label = if ($ExpectGone) { 'gone' } else { 'present' }
    Write-Host "==> Verify ($label)" -ForegroundColor Cyan
    $ok = $true
    foreach ($file in $specs.Keys) {
        $demo = $specs[$file]
        $count = Count-Label $demo
        if ($ExpectGone) {
            if ($count -eq 0) {
                Write-Host ("    OK   {0,-28} demo={1} → {2} objects" -f $file, $demo, $count) -ForegroundColor Green
            } else {
                Write-Host ("    FAIL {0,-28} demo={1} → {2} objects (expected 0)" -f $file, $demo, $count) -ForegroundColor Red
                $ok = $false
            }
        } else {
            if ($count -gt 0) {
                Write-Host ("    OK   {0,-28} demo={1} → {2} objects" -f $file, $demo, $count) -ForegroundColor Green
            } else {
                Write-Host ("    FAIL {0,-28} demo={1} → {2} objects (expected >0)" -f $file, $demo, $count) -ForegroundColor Red
                $ok = $false
            }
        }
    }
    return $ok
}

function Delete-Specs {
    Write-Host "==> Delete (endpoint=$Endpoint)" -ForegroundColor Cyan
    # dashctl delete takes <kind> <name>, not -f. We delete by label in
    # REVERSE tier order (policies → Tier 1 → Tier 0) to honour FK protection.
    $deleteOrder = @('acl-policy','route-policy','ha-set','vnet-mapping','eni','service-tunnel','vnet')
    foreach ($file in $specs.Keys) {
        $demo = $specs[$file]
        Write-Host "    delete demo=$demo" -ForegroundColor Gray
        foreach ($k in $deleteOrder) {
            $names = & $Dashctl 'get' $k '-l' "demo=$demo" '-o' 'name' 2>$null
            if ($LASTEXITCODE -ne 0 -or -not $names) { continue }
            foreach ($n in $names) {
                if (-not ($n -match '\S')) { continue }
                # name format may be "kind/name" — keep just the name portion
                $name = ($n -split '/')[-1].Trim()
                if (-not $name) { continue }
                & $Dashctl 'delete' $k $name '--ignore-not-found' 2>$null | Out-Null
            }
        }
    }
}

switch ($Action) {
    'apply'  { Apply-Specs }
    'verify' { if (-not (Verify-Specs))  { exit 1 } }
    'delete' { Delete-Specs }
    'test'   {
        Apply-Specs
        Start-Sleep -Seconds 2
        if (-not (Verify-Specs))                 { Write-Host "ROUNDTRIP FAIL (after apply)"  -ForegroundColor Red; exit 1 }
        Delete-Specs
        Start-Sleep -Seconds 2
        if (-not (Verify-Specs -ExpectGone))     { Write-Host "ROUNDTRIP FAIL (after delete)" -ForegroundColor Red; exit 1 }
        Write-Host "==> ROUNDTRIP OK" -ForegroundColor Green
    }
}
