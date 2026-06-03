# 04 — Build

Build every binary from source. Covers Go (primary) and Rust (placeholder).

> **Prerequisite**: page [03 — Build setup](03-build-setup.md) completed
> and `install-check` passes.

---

## 1. Layout recap

| Workspace | Manifest | Modules |
|---|---|---|
| Go | [`src/impl-go/go.work`](../../src/impl-go/go.work) | 7 modules; `gen/go`, `dashapi-runtime`, `dash-sim`, `dash-sim-client`, `dash-redis-adapter`, `dashd`, `dashctl` |
| Rust | [`src/impl-rust/Cargo.toml`](../../src/impl-rust/Cargo.toml) | placeholder workspace |

---

## 2. Go — full build

### 2.1 Step-by-step

```powershell
# Windows
$env:PATH="$env:USERPROFILE\go-sdk\go\bin;$env:USERPROFILE\go\bin;$env:USERPROFILE\protoc\bin;$env:PATH"
$env:GOPATH="$env:USERPROFILE\go"; $env:GOBIN="$env:USERPROFILE\go\bin"

cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go
New-Item -ItemType Directory -Path bin -Force | Out-Null

go build -o bin\dash-sim.exe           .\dash-sim\cmd\dash-sim
go build -o bin\dash-sim-client.exe    .\dash-sim-client\cmd\dash-sim-client
go build -o bin\dash-redis-adapter.exe .\dash-redis-adapter\cmd\dash-redis-adapter
```

```bash
# Linux
export PATH=/usr/local/go/bin:$HOME/go/bin:$PATH

cd ~/work/DashCenter/src/impl-go
mkdir -p bin

go build -o bin/dash-sim           ./dash-sim/cmd/dash-sim
go build -o bin/dash-sim-client    ./dash-sim-client/cmd/dash-sim-client
go build -o bin/dash-redis-adapter ./dash-redis-adapter/cmd/dash-redis-adapter
```

### 2.2 Verify

```powershell
Get-ChildItem .\bin -Filter '*.exe' | Format-Table Name, Length
# Name                     Length
# ----                     ------
# dash-redis-adapter.exe   19372032
# dash-sim.exe             17219584
# dash-sim-client.exe      15559168
```

```bash
ls -lh bin/
# -rwxr-xr-x 1 user user  19M Jun  4 03:00 dash-redis-adapter
# -rwxr-xr-x 1 user user  17M Jun  4 03:00 dash-sim
# -rwxr-xr-x 1 user user  16M Jun  4 03:00 dash-sim-client
```

### 2.3 Build a single module

```bash
cd src/impl-go/dash-sim
go build ./...           # compile every package, no binary output
```

### 2.4 Cross-compile (Linux → Windows)

```bash
GOOS=windows GOARCH=amd64 go build -o bin/dash-sim.exe ./dash-sim/cmd/dash-sim
```

### 2.5 Static / size-optimized release builds

```bash
go build -trimpath -ldflags="-s -w" -o bin/dash-sim ./dash-sim/cmd/dash-sim
```

This is what the [`dash-sim/Dockerfile`](../../src/impl-go/dash-sim/Dockerfile) does.

---

## 3. Regenerate Go stubs (only when proto changes)

The `gen/go/` directory is **fully generated**. If you edit
[`proto/dashapi/v1/dashapi.proto`](../../proto/dashapi/v1/dashapi.proto) or
bump the vendored `sonic-dash-api` snapshot, regenerate:

```powershell
# Windows
pwsh -NoProfile -File C:\WorkSpace\PS\PublicRepo\DashCenter\scripts\codegen-go.ps1
```

```bash
# Linux — same script under PowerShell 7+
pwsh -NoProfile -File ~/work/DashCenter/scripts/codegen-go.ps1
```

Output: 32 `*.pb.go` files under `src/impl-go/gen/go/`. To bump the upstream
proto snapshot first:

```bash
pwsh -NoProfile -File scripts/vendor-protos.ps1                   # main HEAD
pwsh -NoProfile -File scripts/vendor-protos.ps1 -Commit <sha>     # pin to commit
pwsh -NoProfile -File scripts/codegen-go.ps1                      # regen Go stubs
```

---

## 4. Build outputs

After a successful build:

```
src/impl-go/bin/
├── dash-sim.exe           or dash-sim
├── dash-sim-client.exe    or dash-sim-client
└── dash-redis-adapter.exe or dash-redis-adapter
```

These are **statically linked, single-file binaries** — copy them anywhere
that has a TCP stack. No runtime dependency on the source tree.

---

## 5. Rust workspace (placeholder)

The `src/impl-rust/` workspace contains a pinned toolchain
([rust-toolchain.toml](../../src/impl-rust/rust-toolchain.toml)) and an empty
crate skeleton. There are no buildable crates yet, but the `cargo` plumbing
works:

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-rust
cargo check
```

When Phase 5 lands, crates will appear under `crates/` and this command will
actually compile something.

---

## 6. Build troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `cannot find package "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/..."` | `gen/go/` not generated yet | Run §3 codegen. |
| `cannot find module providing package ...dashapi-runtime/kinds` | New module not in `go.work` | Add `./dashapi-runtime` to [`go.work`](../../src/impl-go/go.work). |
| `unrecognized import path` after a fresh clone | Go module cache missing | Run `go mod download` in the relevant module. |
| `protoc-gen-go: program not found` while running codegen | GOBIN not on PATH | Re-export PATH as in [03 — Build setup](03-build-setup.md). |
| Build hangs on Windows downloading deps | Corporate proxy | `go env -w GOPROXY=...,direct` |

---

## Where to go next

- → [05 — Run](05-run.md) — start the binaries.
