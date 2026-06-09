#!/usr/bin/env pwsh
# Wrapper to invoke `go` (build/test/vet/etc.) with the correct toolchain on PATH.
# Mirrors run-codegen.ps1. Accepts any args, forwards them to go.
#
# Usage:
#   .\scripts\run-go.ps1 build ./...
#   .\scripts\run-go.ps1 vet ./...
#   .\scripts\run-go.ps1 test -count=1 ./...
#   .\scripts\run-go.ps1 -Cwd src/impl-go/dashd test -tags=integration ./...
[CmdletBinding()]
param(
  [string]$Cwd = "",
  [Parameter(ValueFromRemainingArguments = $true)]
  [string[]]$GoArgs
)
$ErrorActionPreference = "Stop"
$env:PATH   = "$env:USERPROFILE\go-sdk\go\bin;$env:USERPROFILE\go\bin;$env:USERPROFILE\protoc\bin;$env:PATH"
$env:GOPATH = "$env:USERPROFILE\go"
$env:GOBIN  = "$env:USERPROFILE\go\bin"

$logFile = Join-Path (Get-Location).Path "run-go.log"
if (Test-Path $logFile) { Remove-Item -Force $logFile }

if ($Cwd) {
  Push-Location $Cwd
  try {
    & go @GoArgs 2>&1 | Tee-Object -FilePath $logFile
    $exit = $LASTEXITCODE
  } finally {
    Pop-Location
  }
} else {
  & go @GoArgs 2>&1 | Tee-Object -FilePath $logFile
  $exit = $LASTEXITCODE
}
Write-Host "----"
Write-Host "go exited with code $exit; log: $logFile"
exit $exit
