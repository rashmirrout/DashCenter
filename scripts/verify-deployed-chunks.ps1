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

# 4. Probe Fleet Connectivity widening fingerprints
Write-Host ''
Write-Host '  Fleet Connectivity widening fingerprints (deployed DashboardView chunk):'
$dashboardJs = (Invoke-WebRequest -Uri "http://localhost:3000/assets/DashboardView-$dashboardHash.js" -UseBasicParsing).Content
$dprobes = @(
    @{ name = 'size=480 (new viz size)';   needle = 'size:480' },
    @{ name = 'min-h-[540px] (new card height)'; needle = 'min-h-[540px]' },
    @{ name = 'minmax(520px (new grid)';   needle = 'minmax(520px' },
    @{ name = 'Old size=380 absent';       needle = 'size:380'; invert = $true },
    @{ name = 'Old [420px_1fr] absent';    needle = '[420px_1fr]'; invert = $true }
)
foreach ($p in $dprobes) {
    $present = $dashboardJs.Contains($p.needle)
    $ok = if ($p.invert) { -not $present } else { $present }
    $tag = if ($ok) { 'PASS' } else { 'FAIL' }
    Write-Host ("    {0,-38}  {1}" -f $p.name, $tag)
}