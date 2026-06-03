#!/usr/bin/env pwsh
<#
.SYNOPSIS
  Vendors the sonic-net/sonic-dash-api .proto files into
  proto/vendor/sonic-dash-api/ at the commit pinned in
  proto/vendor/sonic-dash-api/VERSION.

.NOTES
  Requires: git, PowerShell 7+
#>

[CmdletBinding()]
param(
  [string]$Commit = "main"
)

$ErrorActionPreference = "Stop"

$repoRoot   = (Resolve-Path "$PSScriptRoot/..").Path
$vendorDir  = Join-Path $repoRoot "proto/vendor/sonic-dash-api"
$thirdParty = Join-Path $repoRoot "third_party/sonic-dash-api"
$tmp        = Join-Path ([System.IO.Path]::GetTempPath()) "sonic-dash-api-vendor"

if (Test-Path $tmp) { Remove-Item -Recurse -Force $tmp }
git clone --depth 1 https://github.com/sonic-net/sonic-dash-api $tmp
if ($Commit -ne "main") {
  Push-Location $tmp
  git fetch origin $Commit
  git checkout $Commit
  Pop-Location
}

New-Item -ItemType Directory -Force -Path $vendorDir   | Out-Null
New-Item -ItemType Directory -Force -Path $thirdParty  | Out-Null

# Copy .proto files (upstream layout uses proto/ at repo root)
Copy-Item -Recurse -Force "$tmp/proto/*.proto" $vendorDir
Copy-Item -Force "$tmp/LICENSE" "$thirdParty/LICENSE"

$resolvedCommit = (& git -C $tmp rev-parse HEAD).Trim()
@"
repo=https://github.com/sonic-net/sonic-dash-api
commit=$resolvedCommit
date=$([DateTime]::UtcNow.ToString("o"))
"@ | Set-Content -Path (Join-Path $vendorDir "VERSION")

Write-Host "Vendored sonic-dash-api @ $resolvedCommit into $vendorDir"
