# `codegen/protoc/` — FALLBACK proto pipeline

```powershell
# From src/impl-go/
$env:PROTOGEN="protoc"; make protos
```

Equivalent to the buf pipeline, but uses plain `protoc` + locally-installed
plugins. Useful when you need exact control of plugin versions or you can't
reach `buf.build` from your network.

## First-time setup

```powershell
make -f codegen/protoc/protoc.mk tools
```

That installs `protoc-gen-go` and `protoc-gen-go-grpc` to `$GOBIN`.
