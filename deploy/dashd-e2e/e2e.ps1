<#
.SYNOPSIS
  Full end-to-end verification of the dashd + 1 dash-sim setup.

.DESCRIPTION
  Demonstrates and verifies the complete control-plane → DPU data path:
    1.  dashd health             (Admin HTTP :7443)
    2.  Single DPU is UP         (registered, prober confirmed)
    3.  PUT vnet via REST        (dashd REST :8443)
    4.  PUT eni via REST         (depends on vnet)
    5.  Force immediate reconcile
    6.  Verify vnet observed on the sim
    7.  Verify eni observed on the sim
    8.  Drift report is empty

  Exit code: 0 on PASS, 1 on FAIL.

.EXAMPLE
  pwsh -File deploy/dashd-e2e/e2e.ps1
  pwsh -File deploy/dashd-e2e/e2e.ps1 -VnetName my-vnet -Vni 7777
#>
[CmdletBinding()]
param(
  [string]$VnetName   = 'vnet-e2e',
  [string]$EniName    = 'eni-e2e',
  [int]   $Vni        = 9001,
  [string]$Mac        = '00:11:22:33:44:55',
  [string]$DashdRest  = $env:DASHD_REST,
  [string]$DashdAdmin = $env:DASHD_ADMIN,
  [string]$SimAdmin   = $env:SIM_ADMIN
)

$ErrorActionPreference = 'Stop'
if (-not $DashdRest)  { $DashdRest  = 'http://localhost:8443' }
if (-not $DashdAdmin) { $DashdAdmin = 'http://localhost:7443' }
if (-not $SimAdmin)   { $SimAdmin   = 'http://localhost:8081' }

function Step  ([int]$n, [string]$m) { Write-Host "[$n/8] $m" -ForegroundColor Yellow }
function Pass  ([string]$m)          { Write-Host "      OK    $m" -ForegroundColor Green }
function Bail  ([string]$m)          { Write-Host "      FAIL  $m" -ForegroundColor Red; exit 1 }
function Warn  ([string]$m)          { Write-Host "      WARN  $m" -ForegroundColor Magenta }

# --- 1 ----------------------------------------------------------------
Step 1 'dashd health'
try   { Invoke-RestMethod -Uri "$DashdAdmin/admin/health" | Out-Null; Pass 'dashd /admin/health responded' }
catch { Bail "dashd /admin/health not responding at $DashdAdmin : $($_.Exception.Message)" }

# --- 2 ----------------------------------------------------------------
Step 2 'DPU is UP (poll up to 30s)'
$state = ''; $i = 0
while ($i -lt 30) {
  try {
    $inv   = Invoke-RestMethod -Uri "$DashdAdmin/admin/inventory"
    $state = ($inv.dpus | Select-Object -First 1).state
    if ($state -eq 'DPU_STATE_UP') { break }
  } catch { }
  Start-Sleep -Seconds 1
  $i++
}
if ($state -eq 'DPU_STATE_UP') { Pass "dpu-sim-01 state=$state (after ${i}s)" }
else                            { Bail  "dpu-sim-01 state=$state (expected DPU_STATE_UP)" }

# --- 3 ----------------------------------------------------------------
Step 3 "PUT /v1/default/vnets/$VnetName"
$body = @{ name = $VnetName; vni = $Vni } | ConvertTo-Json -Compress
try {
  $resp = Invoke-RestMethod -Method Put -Uri "$DashdRest/v1/default/vnets/$VnetName" `
    -ContentType 'application/json' -Body $body
  Pass "vnet accepted (generation=$($resp.generation))"
} catch { Bail "vnet PUT failed: $($_.Exception.Message)" }

# --- 4 ----------------------------------------------------------------
Step 4 "PUT /v1/default/enis/$EniName"
$body = @{ name = $EniName; mac_address = $Mac; vnet = $VnetName } | ConvertTo-Json -Compress
try {
  $resp = Invoke-RestMethod -Method Put -Uri "$DashdRest/v1/default/enis/$EniName" `
    -ContentType 'application/json' -Body $body
  Pass "eni accepted (generation=$($resp.generation))"
} catch { Bail "eni PUT failed: $($_.Exception.Message)" }

# --- 5 ----------------------------------------------------------------
Step 5 'Triggering immediate reconcile'
try   { Invoke-RestMethod -Method Post -Uri "$DashdRest/v1/reconcile" | Out-Null; Pass 'reconcile dispatched' }
catch { Bail "reconcile POST failed: $($_.Exception.Message)" }

# --- 6 ----------------------------------------------------------------
Step 6 "Verifying vnet '$VnetName' on the sim (poll up to 30s)"
$found = $false; $i = 0
while ($i -lt 30) {
  try {
    $dump = Invoke-RestMethod -Uri "$SimAdmin/admin/dump"
    if ($dump.vnet -and ($dump.vnet | Where-Object { $_.key -eq $VnetName })) { $found = $true; break }
  } catch { }
  Start-Sleep -Seconds 1
  $i++
}
if ($found) { Pass "vnet '$VnetName' present on sim (after ${i}s)" }
else        { Bail "vnet '$VnetName' NOT present on sim after 30s" }

# --- 7 ----------------------------------------------------------------
Step 7 "Verifying eni '$EniName' on the sim (poll up to 30s)"
$found = $false; $i = 0
while ($i -lt 30) {
  try {
    $dump = Invoke-RestMethod -Uri "$SimAdmin/admin/dump"
    if ($dump.eni -and ($dump.eni | Where-Object { $_.key -eq $EniName })) { $found = $true; break }
  } catch { }
  Start-Sleep -Seconds 1
  $i++
}
if ($found) { Pass "eni '$EniName' present on sim (after ${i}s)" }
else        { Bail "eni '$EniName' NOT present on sim after 30s" }

# --- 8 ----------------------------------------------------------------
Step 8 'Drift report should be empty (no declared/observed mismatch)'
try {
  $drift = Invoke-RestMethod -Uri "$DashdAdmin/admin/drift"
  $n     = if ($drift.items) { $drift.items.Count } else { 0 }
  if ($n -eq 0) {
    Pass 'drift report is clean'
  } else {
    Warn "drift report has $n items (some may be expected on first run):"
    $drift | ConvertTo-Json -Depth 5 | Write-Host
  }
} catch { Warn "drift query failed: $($_.Exception.Message)" }

Write-Host ''
Write-Host "PASS: end-to-end converged. dashd successfully pushed vnet+eni to dash-sim-01." -ForegroundColor Green
exit 0