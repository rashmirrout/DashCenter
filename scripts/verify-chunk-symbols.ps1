$url = 'http://localhost:3000/assets/ResourcesView-BX7vn6eo.js'
$js = (Invoke-WebRequest -Uri $url -UseBasicParsing).Content
# Property names and string literals survive minification; bare identifiers don't.
# Each probe is a unique fingerprint of code introduced in this session.
$needles = @(
    'hasUpstreamRefs',         # ResourceDef object key (new field for refs column)
    'Dependents',              # column header text
    'References',              # column header text
    'creationOrder',           # ResourceDef object key
    'service-tunnels',         # API kind string
    'vnet-mappings',           # API kind string
    'acl-policies',
    'route-policies',
    'ha-sets',
    'next_hop_target',         # forward-dep field probed in resource-deps.ts
    'ecmp_members',            # forward-dep field probed in resource-deps.ts
    'vnet_name',               # forward-dep field probed in resource-deps.ts
    'underlay_vnet',           # forward-dep field probed in resource-deps.ts
    'will be affected',        # delete-warning UI copy
    'depends on'               # references-badge UI copy
)
foreach ($needle in $needles) {
    $found = $js.Contains($needle)
    $tag = if ($found) { 'PRESENT' } else { 'MISSING' }
    Write-Host ('  {0,-22}  {1}' -f $needle, $tag)
}
Write-Host ''
Write-Host ('  chunk char count: {0:N0}' -f $js.Length)