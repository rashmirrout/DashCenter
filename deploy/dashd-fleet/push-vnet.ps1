<#
.SYNOPSIS
  Push a vnet across the 5-DPU dashd-fleet and verify convergence.

.DESCRIPTION
  1. PUT /v1/default/vnets/<Name> on dashd REST (port 8443)
  2. POST /v1/reconcile — force the reconciler to run now
  3. GET /admin/inventory — sanity-check all 5 DPUs are UP
  4. Poll each dash-sim admin :8081..8085 for the vnet by name (up to 30s)

.PARAMETER Name
  Vnet name (default: vnet-fleet-001).

.PARAMETER Vni
  VNI value (default: 1001).

.EXAMPLE
  pwsh -File deploy/dashd-fleet/push-vnet.ps1
  pwsh -File deploy/dashd-fleet/push-vnet.ps1 -Name vnet-foo -Vni 4242
#>
[CmdletBinding()]
param(
  [string]$Name = 'vnet-fleet-001',
  [int]   $Vni  = 1001,
  [string]$DashdRest  = $env:DASHD_REST,
  [string]$DashdAdmin = $env:DASHD_ADMIN
)

$ErrorActionPreference = 'Stop'
if (-not $DashdRest)  { $DashdRest  = 'http://localhost:8443' }
if (-not $DashdAdmin) { $DashdAdmin = 'http://localhost:7443' }

function Write-Step  ([string]$m) { Write-Host $m -ForegroundColor Yellow }
function Write-Ok    ([string]$m) { Write-Host "      $m" -ForegroundColor Green }
function Write-Fail  ([string]$m) { Write-Host "      $m" -ForegroundColor Red }

Write-Step "[1/4] Pushing vnet name=$Name vni=$Vni to dashd at $DashdRest"
$body = @{ name = $Name; vni = $Vni } | ConvertTo-Json -Compress
try {
  $resp = Invoke-RestMethod -Method Put `
    -Uri "$DashdRest/v1/default/vnets/$Name" `
    -ContentType 'application/json' `
    -Body $body
  Write-Ok "OK (generation=$($resp.generation))"
}
catch {
  Write-Fail "FAIL: PUT failed: $($_.Exception.Message)"
  exit 1
}

Write-Step "[2/4] Triggering immediate reconcile"
try {
  Invoke-RestMethod -Method Post -Uri "$DashdRest/v1/reconcile" | Out-Null
  Write-Ok 'OK'
}
catch {
  Write-Fail "FAIL: reconcile POST failed: $($_.Exception.Message)"
  exit 1
}

Write-Step "[3/4] Checking inventory — expect 5 × DPU_STATE_UP"
try {
  $inv = Invoke-RestMethod -Method Get -Uri "$DashdAdmin/admin/inventory"
  $up    = ($inv.dpus | Where-Object { $_.state -eq 'DPU_STATE_UP' }).Count
  $total = $inv.dpus.Count
  if ($up -lt 5) {
    Write-Fail "FAIL: only $up / $total DPUs are UP"
    $inv | ConvertTo-Json -Depth 5 | Write-Host
    exit 1
  }
  Write-Ok "OK ($up / $total DPUs UP)"
}
catch {
  Write-Fail "FAIL: inventory query failed: $($_.Exception.Message)"
  exit 1
}

Write-Step "[4/4] Verifying vnet '$Name' landed on each dash-sim (poll up to 30s)"
$ok   = 0
$fail = 0
foreach ($port in 8081, 8082, 8083, 8084, 8085) {
  $found = $false
  for ($i = 0; $i -lt 30; $i++) {
    try {
      $dump = Invoke-RestMethod -Method Get -Uri "http://localhost:$port/admin/dump"
      if ($dump.vnet -and ($dump.vnet | Where-Object { $_.key -eq $Name })) {
        $found = $true
        break
      }
    }
    catch { }
    Start-Sleep -Seconds 1
  }
  if ($found) {
    Write-Ok "sim:$port  vnet '$Name' present (after ${i}s)"
    $ok++
  }
  else {
    Write-Fail "sim:$port  vnet '$Name' NOT present after 30s"
    $fail++
  }
}

Write-Host ''
if ($fail -eq 0) {
  Write-Host "PASS: vnet $Name (vni=$Vni) converged to all 5 DPUs" -ForegroundColor Green
  exit 0
}
Write-Host "FAIL: vnet $Name converged to $ok / 5 DPUs" -ForegroundColor Red
exit 1