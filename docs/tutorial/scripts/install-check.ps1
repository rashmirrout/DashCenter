# DashCenter install-check (Windows / PowerShell)
#
# Verifies that the toolchain required to build DashCenter is installed and
# correctly on PATH. Exits 0 on success, 1 on failure.
#
# Usage:
#   pwsh -NoProfile -File docs\tutorial\scripts\install-check.ps1

[CmdletBinding()]
param()

$ErrorActionPreference = "Continue"
$global:Failed = 0

function Check([string]$Label, [scriptblock]$Test, [bool]$Required = $true) {
    try {
        $output = & $Test 2>&1
        if ($LASTEXITCODE -ne 0 -and $LASTEXITCODE -ne $null) { throw "exit $LASTEXITCODE" }
        $line = ($output | Out-String).Trim()
        Write-Host ("[OK]   {0,-22} {1}" -f $Label, $line) -ForegroundColor Green
    } catch {
        if ($Required) {
            Write-Host ("[FAIL] {0,-22} {1}" -f $Label, $_.Exception.Message) -ForegroundColor Red
            $global:Failed++
        } else {
            Write-Host ("[INFO] {0,-22} not installed (optional)" -f $Label) -ForegroundColor Yellow
        }
    }
}

Write-Host "DashCenter toolchain check (Windows)" -ForegroundColor Cyan
Write-Host "PATH = $env:PATH" -ForegroundColor DarkGray
Write-Host ""

Check "go"                  { go version }
Check "protoc"              { protoc --version }
Check "protoc-gen-go"       { protoc-gen-go --version }
Check "protoc-gen-go-grpc"  { protoc-gen-go-grpc --version }
Check "git"                 { git --version }
Check "pwsh"                { $PSVersionTable.PSVersion.ToString() }

# Path sanity
$gobin = if ($env:GOBIN) { $env:GOBIN } else { "$env:USERPROFILE\go\bin" }
if (($env:PATH -split ';') -contains $gobin) {
    Write-Host ("[OK]   {0,-22} {1}" -f "PATH includes GOBIN", $gobin) -ForegroundColor Green
} else {
    Write-Host ("[FAIL] {0,-22} {1} missing from PATH" -f "PATH includes GOBIN", $gobin) -ForegroundColor Red
    $Failed++
}

# Optional tools
Check "docker (optional)"   { docker --version } $false
Check "rustc (optional)"    { rustc --version }  $false
Check "cargo (optional)"    { cargo --version }  $false

Write-Host ""
if ($Failed -eq 0) {
    Write-Host "=== All required checks passed ===" -ForegroundColor Green
    exit 0
} else {
    Write-Host "=== $Failed required check(s) failed ===" -ForegroundColor Red
    Write-Host "See docs/tutorial/03-build-setup.md for installation steps." -ForegroundColor Yellow
    exit 1
}
