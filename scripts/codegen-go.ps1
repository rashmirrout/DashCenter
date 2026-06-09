#!/usr/bin/env pwsh
<#
.SYNOPSIS
  Generates Go code for:
    - vendored upstream sonic-dash-api protos (proto/vendor/sonic-dash-api/*.proto)
    - our DashApi service envelope (proto/dashapi/v1/dashapi.proto)
  into src/impl-go/gen/go/...

  Because upstream protos do NOT carry `go_package` options, we inject one via
  `--go_opt=M<file>=<import-path>` for every upstream file. The mapping is
  generated from the file name (which matches the proto package name modulo
  prefix_tag.proto -> dash.tag).
#>

[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"

$repoRoot   = (Resolve-Path "$PSScriptRoot/..").Path
$vendorDir  = Join-Path $repoRoot "proto/vendor/sonic-dash-api"
$apiDir     = Join-Path $repoRoot "proto/dashapi/v1"
$outDir     = Join-Path $repoRoot "src/impl-go/gen/go"
$modPrefix  = "github.com/rashmirrout/DashCenter/src/impl-go/gen/go"

if (-not (Get-Command protoc -ErrorAction SilentlyContinue)) {
  throw "protoc not found on PATH. Add `$env:USERPROFILE\protoc\bin to PATH."
}
foreach ($p in @("protoc-gen-go", "protoc-gen-go-grpc")) {
  if (-not (Get-Command "$p.exe" -ErrorAction SilentlyContinue)) {
    throw "$p.exe not found. Add `$env:USERPROFILE\go\bin to PATH."
  }
}

# Wipe & recreate output.
if (Test-Path $outDir) {
  Remove-Item -Recurse -Force $outDir
}
New-Item -ItemType Directory -Force -Path $outDir | Out-Null

# Stage directory under a temp prefix that mirrors the Go import path. protoc
# with `paths=import` writes <stage>/<import-path>/<file>.pb.go which we then
# flatten back to $outDir.
$stage = Join-Path ([System.IO.Path]::GetTempPath()) "dashcenter-pb-stage"
if (Test-Path $stage) { Remove-Item -Recurse -Force $stage }
New-Item -ItemType Directory -Force -Path $stage | Out-Null

# Map each upstream proto file -> its Go subdirectory.
# Special case: prefix_tag.proto lives in package dash.tag (not dash.prefix_tag).
#               meter.proto lives in package dash.meter_rule (alongside meter_rule.proto).
$pkgMap = @{
  "acl_group.proto"               = "dash/acl_group"
  "acl_in.proto"                  = "dash/acl_in"
  "acl_out.proto"                 = "dash/acl_out"
  "acl_rule.proto"                = "dash/acl_rule"
  "appliance.proto"               = "dash/appliance"
  "eni.proto"                     = "dash/eni"
  "eni_route.proto"               = "dash/eni_route"
  "ha_scope.proto"                = "dash/ha_scope"
  "ha_scope_config.proto"         = "dash/ha_scope_config"
  "ha_scope_state.proto"          = "dash/ha_scope_state"
  "ha_set.proto"                  = "dash/ha_set"
  "ha_set_config.proto"           = "dash/ha_set_config"
  "ha_set_state.proto"            = "dash/ha_set_state"
  "meter.proto"                   = "dash/meter_rule"   # same Go package as meter_rule.proto
  "meter_policy.proto"            = "dash/meter_policy"
  "meter_rule.proto"              = "dash/meter_rule"
  "outbound_port_map.proto"       = "dash/outbound_port_map"
  "outbound_port_map_range.proto" = "dash/outbound_port_map_range"
  "pa_validation.proto"           = "dash/pa_validation"
  "prefix_tag.proto"              = "dash/tag"          # upstream package is dash.tag
  "qos.proto"                     = "dash/qos"
  "route.proto"                   = "dash/route"
  "route_group.proto"             = "dash/route_group"
  "route_rule.proto"              = "dash/route_rule"
  "route_type.proto"              = "dash/route_type"
  "routing_appliance.proto"       = "dash/routing_appliance"
  "tunnel.proto"                  = "dash/tunnel"
  "types.proto"                   = "dash/types"
  "vnet.proto"                    = "dash/vnet"
  "vnet_mapping.proto"            = "dash/vnet_mapping"
}

$goOpts = @("paths=import")
$grpcOpts = @("paths=import", "require_unimplemented_servers=false")
foreach ($file in $pkgMap.Keys) {
  $pkg  = $pkgMap[$file]
  $goOpts += "M${file}=${modPrefix}/${pkg}"
  $grpcOpts += "M${file}=${modPrefix}/${pkg}"
}

$protoFiles = @()
foreach ($file in $pkgMap.Keys) {
  $protoFiles += (Join-Path $vendorDir $file)
}
$protoFiles += (Join-Path $apiDir "dashapi.proto")

$goOptArgs   = $goOpts   | ForEach-Object { "--go_opt=$_" }
$grpcOptArgs = $grpcOpts | ForEach-Object { "--go-grpc_opt=$_" }

$argList = @(
  "--proto_path=$vendorDir",
  "--proto_path=$apiDir",
  "--go_out=$stage",
  "--go-grpc_out=$stage"
) + $goOptArgs + $grpcOptArgs + $protoFiles

Write-Host "protoc $($argList.Count) args, $($protoFiles.Count) files" -ForegroundColor Cyan
& protoc @argList
if ($LASTEXITCODE -ne 0) { throw "protoc failed with exit $LASTEXITCODE" }

# -----------------------------------------------------------------------------
# dashcenter/v1 protos (northbound DashCenter API)
# -----------------------------------------------------------------------------
# These protos carry their own go_package option so we don't need an M-map.
# proto_path is the parent (proto/) so that `import "dashcenter/v1/types.proto"`
# in control_plane.proto resolves correctly.
$dcProtoRoot = Join-Path $repoRoot "proto"
$dcV1Dir     = Join-Path $repoRoot "proto/dashcenter/v1"
$dcFiles     = @()
Get-ChildItem -Path $dcV1Dir -Filter "*.proto" | ForEach-Object {
  $dcFiles += $_.FullName
}
if ($dcFiles.Count -eq 0) {
  throw "No dashcenter/v1 proto files found under $dcV1Dir"
}

$dcArgList = @(
  "--proto_path=$dcProtoRoot",
  "--go_out=$stage",
  "--go-grpc_out=$stage",
  "--go_opt=paths=import",
  "--go-grpc_opt=paths=import",
  "--go-grpc_opt=require_unimplemented_servers=false"
) + $dcFiles

Write-Host "protoc dashcenter/v1: $($dcFiles.Count) files" -ForegroundColor Cyan
& protoc @dcArgList
if ($LASTEXITCODE -ne 0) { throw "protoc dashcenter/v1 failed with exit $LASTEXITCODE" }

# Flatten: stage\<modPrefix>\* -> outDir\*
$flatten = Join-Path $stage ($modPrefix -replace "/", "\")
if (-not (Test-Path $flatten)) {
  throw "Expected staged tree not found at $flatten"
}
Copy-Item -Recurse -Force "$flatten\*" $outDir
Remove-Item -Recurse -Force $stage

# Preserve go.mod if it existed, otherwise create one.
$goMod = Join-Path $outDir "go.mod"
if (-not (Test-Path $goMod)) {
  @"
module $modPrefix

go 1.22

require (
  google.golang.org/grpc v1.65.0
  google.golang.org/protobuf v1.34.2
)
"@ | Set-Content -Path $goMod
}

Write-Host "Generated Go stubs into $outDir" -ForegroundColor Green
Get-ChildItem $outDir -Recurse -Filter *.pb.go | ForEach-Object {
  $rel = $_.FullName.Substring($outDir.Length + 1)
  Write-Host "  $rel"
}
