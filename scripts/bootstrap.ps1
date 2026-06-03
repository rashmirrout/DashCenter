#!/usr/bin/env pwsh
<#
.SYNOPSIS
  One-time developer setup for Windows.
  Installs Go, buf, protoc, grpcurl via winget/scoop.
#>

[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"

function Test-Cmd($name) { $null -ne (Get-Command $name -ErrorAction SilentlyContinue) }

Write-Host "==> Checking toolchain..." -ForegroundColor Cyan

if (-not (Test-Cmd go)) {
  Write-Host "Installing Go via winget..."
  winget install --id GoLang.Go --silent --accept-package-agreements --accept-source-agreements
}

if (-not (Test-Cmd buf)) {
  Write-Host "Installing buf via winget..."
  winget install --id Bufbuild.Buf --silent --accept-package-agreements --accept-source-agreements
}

if (-not (Test-Cmd protoc)) {
  Write-Host "Installing protoc via winget..."
  winget install --id Google.Protobuf --silent --accept-package-agreements --accept-source-agreements
}

if (-not (Test-Cmd grpcurl)) {
  Write-Host "Installing grpcurl via winget..."
  winget install --id FullStoryDev.Grpcurl --silent --accept-package-agreements --accept-source-agreements
}

Write-Host ""
Write-Host "==> Toolchain versions:" -ForegroundColor Cyan
go version
buf --version
protoc --version
grpcurl -version 2>&1
