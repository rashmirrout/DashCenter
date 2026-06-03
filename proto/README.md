# `proto/` — wire contracts (single source of truth)

Every implementation under `src/impl-XX/` consumes the proto files in this
directory. Do **not** duplicate or fork these files per-language; instead, each
implementation generates its own language bindings from this single source.

## Layout

```
proto/
├── vendor/sonic-dash-api/   Upstream DASH object schemas (Apache 2.0).
│                            Vendored byte-for-byte from
│                            https://github.com/sonic-net/sonic-dash-api.
│                            Refresh with scripts/vendor-protos.{ps1,sh}.
├── dashsim/v1/              dash-sim's gRPC wrapper service (we own this).
├── dashcenter/v1/           dashd northbound API (REST + gRPC + WebSocket).
└── cluster/v1/              dashd internal Raft/gossip RPCs (Model 2).
```

## Versioning rule

* Once an RPC ships in `v1/`, it is **append-only**. Breaking changes go to `v2/`.
* `buf breaking` (in `src/impl-go/codegen/buf/`) enforces this in CI.

## Re-syncing upstream protos

```powershell
./scripts/vendor-protos.ps1   # Windows
./scripts/vendor-protos.sh    # Linux/macOS
```

The script pins a specific commit of `sonic-net/sonic-dash-api` and copies the
`.proto` files into `proto/vendor/sonic-dash-api/`. The pinned commit is
recorded in [vendor/sonic-dash-api/VERSION](vendor/sonic-dash-api/VERSION).
