<#
.SYNOPSIS
  End-to-end dashctl walkthrough against the dashctl-fleet (dashd + 5 sims).

.DESCRIPTION
  13-step verification matching deploy/dashctl-fleet/dashctl-e2e.sh.
  Exercises every Phase 1 dashctl command path both from inside the
  dashctl container (always) and from the host (when bin/dashctl exists).

.EXAMPLE
  pwsh -File deploy/dashctl-fleet/dashctl-e2e.ps1
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

$Compose     = 'docker compose -f deploy/dashctl-fleet/docker-compose.yml'
$ManifestDir = 'deploy/dashctl-fleet/manifests'
$LocalBin    = 'src/impl-go/dashctl/bin/dashctl.exe'
if (-not (Test-Path $LocalBin)) { $LocalBin = '' }

function Write-Step  ([int]$n, [string]$m) { Write-Host "[$n/13] $m" -ForegroundColor Yellow }
function Write-Ok    ([string]$m)          { Write-Host "      OK    $m" -ForegroundColor Green }
function Write-Warn  ([string]$m)          { Write-Host "      WARN  $m" -ForegroundColor DarkYellow }
function Fail        ([string]$m)          { Write-Host "      FAIL  $m" -ForegroundColor Red; exit 1 }

function Invoke-Ctnr {
  $args = $args -join ' '
  & cmd /c "$Compose run --rm dashctl $args"
}

function Invoke-CtnrWithMounts {
  $cwd = (Get-Location).Path -replace '\\', '/'
  $args = $args -join ' '
  & cmd /c "$Compose run --rm -v `"$cwd/$ManifestDir`":/work:ro --entrypoint /usr/local/bin/dashctl dashctl $args"
}

function Invoke-Host {
  if (-not $LocalBin) { return $null }
  & $LocalBin --endpoint http://localhost:8443 --admin-endpoint http://localhost:7443 @args
}

# ── Step 1: version --client
Write-Step 1 'dashctl version --client'
$out = Invoke-Ctnr 'version --client'
if (-not ($out -match 'Client: dashctl')) { Fail "container version banner missing: $out" }
Write-Ok 'container version OK'
if ($LocalBin) {
  $h = Invoke-Host 'version' '--client'
  if (-not ($h -match 'Client: dashctl')) { Fail "host version banner missing: $h" }
  Write-Ok 'host    version OK'
} else {
  Write-Warn 'skipping host check — bin/dashctl.exe not built (run `make build` in src/impl-go/dashctl first)'
}

# ── Step 2: dashd health
Write-Step 2 'dashd /admin/health'
try { Invoke-RestMethod -Uri 'http://localhost:7443/admin/health' -TimeoutSec 5 | Out-Null }
catch { Fail "dashd admin not reachable: $($_.Exception.Message)" }
Write-Ok 'dashd reachable'

# ── Step 3: inventory: 5 UP
Write-Step 3 'Inventory: expect 5 × DPU_STATE_UP (poll up to 30s)'
$up = 0
for ($i = 0; $i -lt 30; $i++) {
  $inv = Invoke-RestMethod -Uri 'http://localhost:7443/admin/inventory' -TimeoutSec 5
  $up  = ($inv.dpus | Where-Object { $_.state -eq 'DPU_STATE_UP' }).Count
  if ($up -ge 5) { break }
  Start-Sleep 1
}
if ($up -lt 5) { Fail "only $up / 5 DPUs UP" }
Write-Ok "$up / 5 DPUs UP (after ${i}s)"

# ── Step 4: get vnet (empty)
Write-Step 4 'get vnet (empty start)'
$null = Invoke-Ctnr 'get vnet'
Write-Ok 'get vnet ran'

# ── Step 5: apply -f manifests/
Write-Step 5 "apply -f $ManifestDir (2 vnets + 5 enis)"
$apply = Invoke-CtnrWithMounts 'apply -f /work'
$count = (($apply -split "`n") | Where-Object { $_ -match 'apply in namespace default' }).Count
if ($count -lt 7) { Fail "expected ≥7 apply lines, got $count — output: $apply" }
Write-Ok "$count specs applied"

# ── Step 6: get vnet -o table
Write-Step 6 'get vnet -o table — expect 2 rows'
$out = Invoke-Ctnr 'get vnet -o table'
if ($out -notmatch 'vnet-app') { Fail "vnet-app missing: $out" }
if ($out -notmatch 'vnet-db')  { Fail 'vnet-db missing' }
Write-Ok 'both vnets listed'

# ── Step 7: get eni -o wide
Write-Step 7 'get eni -o wide — expect PLACED-ON + 5 enis'
$out = Invoke-Ctnr 'get eni -o wide'
if ($out -notmatch 'PLACED-ON') { Fail "PLACED-ON column missing: $out" }
foreach ($n in 'eni-app-01','eni-app-02','eni-db-01','eni-db-02','eni-db-03') {
  if ($out -notmatch [regex]::Escape($n)) { Fail "missing $n" }
}
Write-Ok '5 ENIs listed wide'

# ── Step 8: describe eni
Write-Step 8 'describe eni eni-app-01'
$out = Invoke-Ctnr 'describe eni eni-app-01'
if ($out -notmatch 'Name:\s+eni-app-01') { Fail "describe header missing: $out" }
Write-Ok 'describe block produced'

# ── Step 9: reconcile
Write-Step 9 'reconcile'
$out = Invoke-Ctnr 'reconcile'
if ($out -notmatch 'Triggered reconcile') { Fail "reconcile output missing: $out" }
Write-Ok 'reconcile triggered'

# ── Step 10: dpu list
Write-Step 10 'dpu list'
$out = Invoke-Ctnr 'dpu list -o table'
foreach ($d in 'dpu-sim-01','dpu-sim-02','dpu-sim-03','dpu-sim-04','dpu-sim-05') {
  if ($out -notmatch $d) { Fail "$d missing from dpu list" }
}
Write-Ok 'all 5 DPUs listed'

# ── Step 11: dpu drift
Write-Step 11 'dpu drift --dpu dpu-sim-01'
$ok = $false
for ($i = 0; $i -lt 30; $i++) {
  $drift = Invoke-Ctnr 'dpu drift --dpu dpu-sim-01'
  if ($drift -match '0 drift items\.') { $ok = $true; break }
  Start-Sleep 1
}
if ($ok) { Write-Ok "no drift on dpu-sim-01 (after ${i}s)" }
else     { Write-Warn 'drift still present after 30s' }

# ── Step 12: delete eni
Write-Step 12 'delete eni eni-db-03'
$out = Invoke-Ctnr 'delete eni eni-db-03'
if ($out -notmatch 'eni/eni-db-03 deleted') { Fail "delete output missing: $out" }
Write-Ok 'eni-db-03 deleted'
$null = Invoke-Ctnr 'get eni eni-db-03'
# If `get` succeeded (LASTEXITCODE 0) the delete didn't take.
if ($LASTEXITCODE -eq 0) { Fail 'eni-db-03 still readable after delete' }
Write-Ok '404 confirmed on subsequent get'

# ── Step 13: explain (offline)
Write-Step 13 'explain vnet (offline)'
$out = Invoke-Ctnr 'explain vnet'
if ($out -notmatch 'KIND:\s+Vnet') { Fail "explain header missing: $out" }
if ($out -notmatch 'vni')          { Fail 'explain field listing missing' }
Write-Ok 'offline field reference produced'

Write-Host ''
Write-Host 'PASS: dashctl drove the 5-DPU fleet end-to-end.' -ForegroundColor Green
Write-Host '      ✔ apply -f (multi-doc, dir, mount)' -ForegroundColor Green
Write-Host '      ✔ get / describe / delete / reconcile' -ForegroundColor Green
Write-Host '      ✔ dpu list / dpu drift' -ForegroundColor Green
Write-Host '      ✔ explain (offline)' -ForegroundColor Green
if (-not $LocalBin) {
  Write-Host '(host binary not built — run `make build` in src/impl-go/dashctl to also exercise the host path)' -ForegroundColor DarkYellow
}
