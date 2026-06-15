Start-Sleep -Seconds 3

# 1. Get entry chunk hash from index.html
$html = (Invoke-WebRequest -Uri 'http://localhost:3000/' -UseBasicParsing).Content
$null = $html -match 'index-([A-Za-z0-9_-]+)\.js'
$entryHash = $matches[1]
Write-Host "  entry chunk: index-$entryHash.js"

# 2. Get lazy-chunk hashes from the entry chunk's __vite__mapDeps
$entryJs = (Invoke-WebRequest -Uri "http://localhost:3000/assets/index-$entryHash.js" -UseBasicParsing).Content

$resourcesHash = $null
$dashboardHash = $null

foreach ($view in 'ResourcesView', 'DashboardView') {
    $regex = $view + '-([A-Za-z0-9_-]+)\.js'
    if ($entryJs -match $regex) {
        $hash = $matches[1]
        if ($view -eq 'ResourcesView') { $resourcesHash = $hash }
        else { $dashboardHash = $hash }
        $url = "http://localhost:3000/assets/$view-$hash.js"
        $r = Invoke-WebRequest -Uri $url -UseBasicParsing -Method Head
        $len = $r.Headers['Content-Length']
        Write-Host ("  {0,-15}  {1}  ({2} bytes)" -f $view, "$view-$hash.js", $len)
    }
}

# 3. Probe widening fingerprints in the deployed ResourcesView chunk
Write-Host ''
Write-Host '  Mini-map widening fingerprints (deployed ResourcesView chunk):'
$resourcesJs = (Invoke-WebRequest -Uri "http://localhost:3000/assets/ResourcesView-$resourcesHash.js" -UseBasicParsing).Content
$probes = @(
    @{ name = 'COL_WIDTH=250 (new)';       needle = 'COL_WIDTH=250'; alt = '=250' },
    @{ name = 'NODE_W=200 (new)';          needle = 'NODE_W=200';    alt = '=200' },
    @{ name = 'Old COL_WIDTH=130 absent';  needle = '=130';          invert = $true },
    @{ name = 'Old NODE_W=110 absent';     needle = '=110';          invert = $true }
)
foreach ($p in $probes) {
    $present = $resourcesJs.Contains($p.needle) -or ($p.alt -and $resourcesJs.Contains($p.alt))
    $ok = if ($p.invert) { -not $present } else { $present }
    $tag = if ($ok) { 'PASS' } else { 'FAIL' }
    Write-Host ("    {0,-32}  {1}" -f $p.name, $tag)
}

# 4. Probe Fleet Connectivity layout fingerprints
# Latest layout (3rd iteration): h-[520px] FIXED card height so the box stays
# compact; viz fills the available square area inside via width+height measurement.
Write-Host ''
Write-Host '  Fleet Connectivity layout fingerprints (deployed DashboardView chunk):'
$dashboardJs = (Invoke-WebRequest -Uri "http://localhost:3000/assets/DashboardView-$dashboardHash.js" -UseBasicParsing).Content
$dprobes = @(
    @{ name = 'lg:grid-cols-[1fr_280px] (hero grid)'; needle = 'lg:grid-cols-[1fr_280px]' },
    @{ name = 'h-[520px] (fixed compact card)';       needle = 'h-[520px]' },
    @{ name = 'flex-1 min-h-0 (viz fills card)';      needle = 'flex-1 min-h-0' },
    @{ name = 'maxFillSize:720 prop';                 needle = 'maxFillSize:720'; alt = 'fill:!0' },
    @{ name = 'Old size=540 absent';                  needle = 'size:540';       invert = $true },
    @{ name = 'Old size=480 absent';                  needle = 'size:480';       invert = $true },
    @{ name = 'Old min-h-[620px] absent';             needle = 'min-h-[620px]';  invert = $true },
    @{ name = 'Old min-h-[560px] absent';             needle = 'min-h-[560px]';  invert = $true },
    @{ name = 'Old min-h-[540px] absent';             needle = 'min-h-[540px]';  invert = $true },
    @{ name = 'Old minmax(520px absent';              needle = 'minmax(520px';   invert = $true },
    @{ name = 'Old sm:grid-cols-2 KPI col absent';    needle = 'sm:grid-cols-2 gap-4 content-start'; invert = $true }
)
foreach ($p in $dprobes) {
    $present = $dashboardJs.Contains($p.needle) -or ($p.alt -and $dashboardJs.Contains($p.alt))
    $ok = if ($p.invert) { -not $present } else { $present }
    $tag = if ($ok) { 'PASS' } else { 'FAIL' }
    Write-Host ("    {0,-46}  {1}" -f $p.name, $tag)
}

# 5. Probe FleetConnectivityViz auto-fill plumbing
# The viz code is bundled into the main `index-*.js` entry chunk (not lazy-loaded)
# because it's eagerly imported by DashboardView via a static import.
# Critical new behavior: measures BOTH clientWidth AND clientHeight so the viz
# is sized as the largest square that fits inside its container.
Write-Host ''
Write-Host '  FleetConnectivityViz auto-fill plumbing (deployed entry chunk):'
$vprobes = @(
    @{ name = 'ResizeObserver call';                 needle = 'ResizeObserver' },
    @{ name = 'clientWidth read';                    needle = 'clientWidth' },
    @{ name = 'clientHeight read (NEW)';             needle = 'clientHeight' },
    @{ name = 'maxFillSize default 720';             needle = '720' },  # weak but present
    @{ name = 'autoSize state hook';                 needle = 'autoSize'; alt = 'fill:' }
)
foreach ($p in $vprobes) {
    $present = $entryJs.Contains($p.needle) -or $dashboardJs.Contains($p.needle) -or ($p.alt -and ($entryJs.Contains($p.alt) -or $dashboardJs.Contains($p.alt)))
    $ok = if ($p.invert) { -not $present } else { $present }
    $tag = if ($ok) { 'PASS' } else { 'FAIL' }
    Write-Host ("    {0,-46}  {1}" -f $p.name, $tag)
}
