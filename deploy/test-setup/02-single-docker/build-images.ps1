#!/usr/bin/env pwsh
<#
.SYNOPSIS
  Build the three DashCenter container images from the repo root.

  Tags produced (override with -Tag):
    dashcenter/dash-sim:dev
    dashcenter/dash-redis-adapter:dev
    dashcenter/dash-sim-client:dev
#>
[CmdletBinding()]
param(
  [string] $Tag = "dev"
)

$ErrorActionPreference = "Stop"
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Resolve-Path (Join-Path $here "..\..\..")

function Build-Image {
  param([string]$Image, [string]$Dockerfile)
  Write-Host "==> docker build -t $Image -f $Dockerfile $repoRoot" -ForegroundColor Cyan
  docker build -t $Image -f $Dockerfile $repoRoot
  if ($LASTEXITCODE -ne 0) { throw "docker build failed for $Image" }
}

Build-Image -Image "dashcenter/dash-sim:$Tag"           -Dockerfile "$repoRoot\src\impl-go\dash-sim\Dockerfile"
Build-Image -Image "dashcenter/dash-redis-adapter:$Tag" -Dockerfile "$repoRoot\src\impl-go\dash-redis-adapter\Dockerfile"
Build-Image -Image "dashcenter/dash-sim-client:$Tag"    -Dockerfile "$repoRoot\src\impl-go\dash-sim-client\Dockerfile"

Write-Host ""
Write-Host "==> Done. Images:" -ForegroundColor Green
docker images --filter "reference=dashcenter/*:$Tag"
