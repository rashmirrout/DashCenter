# `codegen/buf/` — DEFAULT proto pipeline

```powershell
# From src/impl-go/
make protos           # PROTOGEN=buf is the default
```

`buf.gen.yaml` writes generated Go into `../../gen/go/`. Both
`protocolbuffers/go` and `grpc/go` plugins are pulled from buf's remote
registry, so contributors don't need `protoc-gen-go` installed locally.
