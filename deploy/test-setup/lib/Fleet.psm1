# Fleet.psm1 — shared loader + validator for DashCenter test-setup configs.
#
# Public functions:
#   Resolve-FleetConfigPath [-Config <path>]
#   Import-FleetConfig      -Path <path>
#   Test-FleetConfig        -Config <object> [-AllowPrivilegedPorts]
#   Resolve-FleetPath       -Base <dir>  -Path <path>
#   Get-RepoRoot
#
# All scripts in deploy/test-setup/ import this module. It is the SINGLE
# source of truth for config interpretation, so YAML and JSON consumers
# end up with identical resolved objects.

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# -----------------------------------------------------------------------------
# Paths
# -----------------------------------------------------------------------------

function Get-TestSetupRoot {
    [CmdletBinding()] param()
    # This module lives at <repo>/deploy/test-setup/lib/Fleet.psm1
    return (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
}

function Get-RepoRoot {
    [CmdletBinding()] param()
    # Walk up from this module to find go.work (marker for src/impl-go).
    $dir = $PSScriptRoot
    for ($i = 0; $i -lt 8; $i++) {
        $candidate = Join-Path $dir 'src/impl-go/go.work'
        if (Test-Path $candidate) { return (Resolve-Path $dir).Path }
        $parent = Split-Path -Parent $dir
        if (-not $parent -or $parent -eq $dir) { break }
        $dir = $parent
    }
    throw "Get-RepoRoot: could not find src/impl-go/go.work walking up from $PSScriptRoot"
}

function Resolve-FleetPath {
    <#
    .SYNOPSIS
      Resolve a config-relative path (POSIX-style) against a base directory.
    #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)] [string] $Base,
        [Parameter(Mandatory)] [string] $Path
    )
    if ([System.IO.Path]::IsPathRooted($Path)) { return $Path }
    # Normalise forward slashes — config files MUST use POSIX paths.
    $native = $Path -replace '/', [IO.Path]::DirectorySeparatorChar
    return [IO.Path]::GetFullPath((Join-Path $Base $native))
}

# -----------------------------------------------------------------------------
# Config resolution + loading
# -----------------------------------------------------------------------------

function Resolve-FleetConfigPath {
    <#
    .SYNOPSIS
      Locate the fleet config file using the documented precedence:
        1. -Config <path>
        2. $env:DASHCENTER_FLEET_CONFIG
        3. <test-setup>/fleet.yaml
        4. <test-setup>/fleet.json
        5. <test-setup>/fleet.example.yaml  (warned)
    #>
    [CmdletBinding()]
    param([string] $Config)

    if ($Config) {
        if (-not (Test-Path $Config)) { throw "fleet config not found: $Config" }
        return (Resolve-Path $Config).Path
    }
    if ($env:DASHCENTER_FLEET_CONFIG) {
        if (-not (Test-Path $env:DASHCENTER_FLEET_CONFIG)) {
            throw "DASHCENTER_FLEET_CONFIG points to a missing file: $($env:DASHCENTER_FLEET_CONFIG)"
        }
        return (Resolve-Path $env:DASHCENTER_FLEET_CONFIG).Path
    }
    $root = Get-TestSetupRoot
    foreach ($name in 'fleet.yaml', 'fleet.yml', 'fleet.json') {
        $p = Join-Path $root $name
        if (Test-Path $p) { return (Resolve-Path $p).Path }
    }
    $fallback = Join-Path $root 'fleet.example.yaml'
    if (Test-Path $fallback) {
        Write-Warning "fleet config: no fleet.{yaml,json} found; falling back to fleet.example.yaml"
        return (Resolve-Path $fallback).Path
    }
    throw "fleet config: nothing to load (no fleet.yaml, fleet.json, or fleet.example.yaml in $root)"
}

function _ConvertTo-Hashtable {
    # Recursively turn PSCustomObject (from ConvertFrom-Json) into hashtables /
    # arrays so YAML and JSON loaders return the same shape.
    param($obj)
    if ($null -eq $obj) { return $null }
    if ($obj -is [System.Collections.IDictionary]) {
        $h = @{}
        foreach ($k in $obj.Keys) { $h[$k] = _ConvertTo-Hashtable $obj[$k] }
        return $h
    }
    if ($obj -is [System.Management.Automation.PSCustomObject]) {
        $h = @{}
        foreach ($p in $obj.PSObject.Properties) { $h[$p.Name] = _ConvertTo-Hashtable $p.Value }
        return $h
    }
    if ($obj -is [System.Collections.IEnumerable] -and -not ($obj -is [string])) {
        return @($obj | ForEach-Object { _ConvertTo-Hashtable $_ })
    }
    return $obj
}

function Import-FleetConfig {
    <#
    .SYNOPSIS
      Load a fleet config (YAML or JSON, auto-detected by extension).

    .OUTPUTS
      Hashtable with fields:
        SourcePath        : absolute path to the file
        BaseDir           : directory containing the file (resolves relative paths)
        Raw               : the parsed object (hashtable form)
        Defaults          : hashtable of resolved defaults
        Dpus              : array of per-DPU hashtables (resolved scenario, etc.)
        Adapter           : adapter hashtable or $null
    #>
    [CmdletBinding()]
    param([Parameter(Mandatory)] [string] $Path)

    $abs    = (Resolve-Path $Path).Path
    $baseDir= Split-Path -Parent $abs
    $ext    = [IO.Path]::GetExtension($abs).ToLowerInvariant()

    $raw = $null
    switch ($ext) {
        '.json' {
            $raw = Get-Content -Raw $abs | ConvertFrom-Json
        }
        { $_ -in '.yaml', '.yml' } {
            if (-not (Get-Command ConvertFrom-Yaml -ErrorAction SilentlyContinue)) {
                throw @"
YAML fleet config requires the PowerShell-Yaml module:
  Install-Module PowerShell-Yaml -Scope CurrentUser -Force
Alternatively, use a JSON fleet config (no extra dependencies).
"@
            }
            $raw = Get-Content -Raw $abs | ConvertFrom-Yaml
        }
        default {
            throw "unsupported fleet config extension '$ext' (use .yaml, .yml, or .json)"
        }
    }

    $h = _ConvertTo-Hashtable $raw
    if ($null -eq $h) { throw "fleet config $abs parsed as null" }

    # Defaults with sane fallbacks.
    $defaults = @{
        scenario     = $null
        imageTag     = 'dev'
        bindHost     = '127.0.0.1'
        network      = 'dashcenter-fleet'
        tickInterval = '1s'
    }
    if ($h.ContainsKey('defaults') -and $h['defaults']) {
        foreach ($k in $h['defaults'].Keys) { $defaults[$k] = $h['defaults'][$k] }
    }

    # DPUs — apply defaults.
    $dpus = @()
    foreach ($d in @($h['dpus'])) {
        $entry = @{
            deviceId  = $d['deviceId']
            grpcPort  = [int]$d['grpcPort']
            adminPort = [int]$d['adminPort']
            scenario  = $(if ($d.ContainsKey('scenario') -and $d['scenario']) { $d['scenario'] } else { $defaults['scenario'] })
            imageTag  = $(if ($d.ContainsKey('imageTag') -and $d['imageTag']) { $d['imageTag'] } else { $defaults['imageTag'] })
            extraArgs = $(if ($d.ContainsKey('extraArgs') -and $d['extraArgs']) { @($d['extraArgs']) } else { @() })
        }
        $dpus += ,$entry
    }

    # Adapter (optional).
    $adapter = $null
    if ($h.ContainsKey('adapter') -and $h['adapter']) {
        $a = $h['adapter']
        $redis = @{ mode = 'embedded'; hostPort = 6379; address = 'localhost:6379' }
        if ($a.ContainsKey('redis') -and $a['redis']) {
            foreach ($k in $a['redis'].Keys) { $redis[$k] = $a['redis'][$k] }
        }
        $adapter = @{
            enabled  = [bool]$a['enabled']
            grpcPort = $(if ($a.ContainsKey('grpcPort')) { [int]$a['grpcPort'] } else { 52051 })
            redis    = $redis
        }
    }

    return @{
        SourcePath = $abs
        BaseDir    = $baseDir
        Raw        = $h
        Defaults   = $defaults
        Dpus       = $dpus
        Adapter    = $adapter
    }
}

# -----------------------------------------------------------------------------
# Validation
# -----------------------------------------------------------------------------

function Test-FleetConfig {
    <#
    .SYNOPSIS
      Validate a config returned by Import-FleetConfig. Throws on any failure.
    #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)] $Config,
        [switch] $AllowPrivilegedPorts
    )

    $errors = New-Object 'System.Collections.Generic.List[string]'

    if ($Config.Raw['apiVersion'] -ne 'dashcenter.io/test-setup/v1') {
        $errors.Add("apiVersion: expected 'dashcenter.io/test-setup/v1', got '$($Config.Raw['apiVersion'])'")
    }
    if ($Config.Raw['kind'] -ne 'FleetConfig') {
        $errors.Add("kind: expected 'FleetConfig', got '$($Config.Raw['kind'])'")
    }
    if (-not $Config.Dpus -or $Config.Dpus.Count -eq 0) {
        $errors.Add('dpus: must contain at least one entry')
    }

    # Unique device IDs.
    $seenIds = @{}
    for ($i = 0; $i -lt $Config.Dpus.Count; $i++) {
        $id = $Config.Dpus[$i].deviceId
        if (-not $id) { $errors.Add("dpus[$i].deviceId: missing"); continue }
        if ($seenIds.ContainsKey($id)) {
            $errors.Add("dpus[$i].deviceId '$id' duplicates dpus[$($seenIds[$id])].deviceId")
        }
        $seenIds[$id] = $i
    }

    # Unique host-side ports across DPUs + adapter + redis container.
    $portOwners = @{}
    function _claimPort($port, $owner, [ref]$errs) {
        if ($null -eq $port) { return }
        if (-not ($port -is [int])) { $port = [int]$port }
        if ($portOwners.ContainsKey($port)) {
            $errs.Value.Add("port ${port}: $owner conflicts with $($portOwners[$port])")
        } else {
            $portOwners[$port] = $owner
        }
    }

    for ($i = 0; $i -lt $Config.Dpus.Count; $i++) {
        $d = $Config.Dpus[$i]
        _claimPort $d.grpcPort  "dpus[$i].grpcPort"  ([ref]$errors)
        _claimPort $d.adminPort "dpus[$i].adminPort" ([ref]$errors)
    }
    if ($Config.Adapter -and $Config.Adapter.enabled) {
        _claimPort $Config.Adapter.grpcPort 'adapter.grpcPort' ([ref]$errors)
        if ($Config.Adapter.redis.mode -eq 'container') {
            _claimPort $Config.Adapter.redis.hostPort 'adapter.redis.hostPort' ([ref]$errors)
        }
    }

    # Port range sanity.
    if (-not $AllowPrivilegedPorts) {
        foreach ($kv in $portOwners.GetEnumerator()) {
            if ($kv.Key -lt 1024) {
                $errors.Add("port $($kv.Key) ($($kv.Value)): privileged (<1024); pass -AllowPrivilegedPorts to permit")
            }
            if ($kv.Key -gt 65535) {
                $errors.Add("port $($kv.Key) ($($kv.Value)): out of range (>65535)")
            }
        }
    }

    # Adapter / redis mode consistency.
    if ($Config.Adapter -and $Config.Adapter.enabled) {
        $mode = $Config.Adapter.redis.mode
        switch ($mode) {
            'embedded'  { }
            'external'  {
                if (-not $Config.Adapter.redis.address) {
                    $errors.Add("adapter.redis.address: required when adapter.redis.mode='external'")
                }
            }
            'container' {
                if (-not $Config.Adapter.redis.hostPort) {
                    $errors.Add("adapter.redis.hostPort: required when adapter.redis.mode='container'")
                }
            }
            default {
                $errors.Add("adapter.redis.mode: expected one of embedded|external|container, got '$mode'")
            }
        }
    }

    # Scenario files exist.
    for ($i = 0; $i -lt $Config.Dpus.Count; $i++) {
        $s = $Config.Dpus[$i].scenario
        if (-not $s) { continue }
        $full = Resolve-FleetPath -Base $Config.BaseDir -Path $s
        if (-not (Test-Path $full)) {
            $errors.Add("dpus[$i].scenario: file not found (resolved to '$full')")
        }
    }

    if ($errors.Count -gt 0) {
        throw "fleet config invalid:`n  - " + ($errors -join "`n  - ")
    }
}

Export-ModuleMember -Function `
    Get-TestSetupRoot, Get-RepoRoot, Resolve-FleetPath, `
    Resolve-FleetConfigPath, Import-FleetConfig, Test-FleetConfig
