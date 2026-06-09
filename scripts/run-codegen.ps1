#!/usr/bin/env pwsh
# Wrapper to invoke codegen-go.ps1 with the correct PATH/GOPATH/GOBIN env vars.
# Mirrors the working invocation pattern used for vendor proto compilation.
$ErrorActionPreference = "Stop"
$env:PATH    = "$env:USERPROFILE\go-sdk\go\bin;$env:USERPROFILE\go\bin;$env:USERPROFILE\protoc\bin;$env:PATH"
$env:GOPATH  = "$env:USERPROFILE\go"
$env:GOBIN   = "$env:USERPROFILE\go\bin"
& "$PSScriptRoot\codegen-go.ps1"