$kinds = @('vnets', 'enis', 'service-tunnels', 'vnet-mappings', 'acl-policies', 'route-policies', 'ha-sets')
foreach ($k in $kinds) {
    try {
        $r = Invoke-RestMethod -Uri "http://localhost:3000/api/v1/default/$k" -TimeoutSec 5 -ErrorAction Stop
        $c = if ($r.items) { $r.items.Count } else { 0 }
        Write-Host ("  {0,-18}  {1,3}  items" -f $k, $c)
    } catch {
        Write-Host ("  {0,-18}  ERR  {1}" -f $k, $_.Exception.Message)
    }
}