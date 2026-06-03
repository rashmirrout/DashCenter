# 03 — Build Setup

End-to-end instructions to install the toolchain on **Windows 10/11** and
**Linux (Ubuntu/Debian/Fedora/Arch)**. By the end of this page you will
have:

- Go 1.22+
- protoc 25+
- `protoc-gen-go` and `protoc-gen-go-grpc` Go plugins
- (Optional) Rust 1.78+
- (Optional) Docker Engine 24+
- A verification script that confirms everything is in place

---

## 1. Required toolchain summary

| Tool | Minimum | Why |
|---|---|---|
| Go | 1.22 | Builds every binary; provides the workspace tooling. |
| protoc | 25.x | Compiles `.proto` files. |
| protoc-gen-go | v1.34.x | Emits `*.pb.go`. |
| protoc-gen-go-grpc | v1.5.x | Emits `*_grpc.pb.go`. |
| Git | any modern | Required for vendoring upstream `sonic-dash-api`. |
| PowerShell 7+ (Windows) | 7.x | Runs the `.ps1` scripts. |
| Bash (Linux/macOS) | 4+ | Runs the `.sh` scripts. |

Optional:

| Tool | Minimum | Why |
|---|---|---|
| Rust toolchain | 1.78 | Building the placeholder `impl-rust` workspace. |
| Docker | 24 | Containerized fleet via Docker Compose. |
| make | any | Convenience targets. |

---

## 2. Where things live (installation paths)

The build system expects these binaries on `PATH`. Recommended install
locations:

### Windows

| Tool | Path | Add to `PATH` |
|---|---|---|
| Go | `%USERPROFILE%\go-sdk\go\bin` | yes |
| GOPATH | `%USERPROFILE%\go` | (`GOPATH` env var) |
| GOBIN | `%USERPROFILE%\go\bin` | yes (where Go plugins land) |
| protoc | `%USERPROFILE%\protoc\bin` | yes |
| `protoc-gen-go.exe` | `%USERPROFILE%\go\bin` | yes (via GOBIN) |
| `protoc-gen-go-grpc.exe` | `%USERPROFILE%\go\bin` | yes (via GOBIN) |
| Git | (default installer location) | yes |
| PowerShell 7 | (default installer location) | yes |

> No admin rights required — every tool installs to your user profile.

### Linux

| Tool | Path | Add to `PATH` |
|---|---|---|
| Go | `/usr/local/go/bin` | yes |
| GOPATH | `$HOME/go` | (`GOPATH` env var) |
| GOBIN | `$HOME/go/bin` | yes |
| protoc | `/usr/local/bin/protoc` | usually already on PATH |
| `protoc-gen-go` | `$HOME/go/bin/protoc-gen-go` | yes |
| `protoc-gen-go-grpc` | `$HOME/go/bin/protoc-gen-go-grpc` | yes |

---

## 3. Install on Windows (PowerShell — no admin required)

### 3.1 Go 1.22+

```powershell
# Download portable Go SDK zip
$ver = "1.22.10"
$url = "https://go.dev/dl/go$ver.windows-amd64.zip"
$dst = "$env:USERPROFILE\go-sdk"
New-Item -ItemType Directory -Force -Path $dst | Out-Null
Invoke-WebRequest $url -OutFile "$env:TEMP\go.zip"
Expand-Archive "$env:TEMP\go.zip" -DestinationPath $dst -Force
# Per-session env (recommended; no admin)
$env:PATH = "$env:USERPROFILE\go-sdk\go\bin;$env:USERPROFILE\go\bin;$env:PATH"
$env:GOPATH = "$env:USERPROFILE\go"
$env:GOBIN  = "$env:USERPROFILE\go\bin"
```

To make these permanent in your user profile:

```powershell
[Environment]::SetEnvironmentVariable("PATH",
  "$env:USERPROFILE\go-sdk\go\bin;$env:USERPROFILE\go\bin;" + [Environment]::GetEnvironmentVariable("PATH","User"),
  "User")
[Environment]::SetEnvironmentVariable("GOPATH", "$env:USERPROFILE\go", "User")
[Environment]::SetEnvironmentVariable("GOBIN",  "$env:USERPROFILE\go\bin", "User")
```

Verify:
```powershell
go version
# go version go1.22.10 windows/amd64
```

### 3.2 protoc 25.x

```powershell
$ver = "25.3"
$url = "https://github.com/protocolbuffers/protobuf/releases/download/v$ver/protoc-$ver-win64.zip"
$dst = "$env:USERPROFILE\protoc"
New-Item -ItemType Directory -Force -Path $dst | Out-Null
Invoke-WebRequest $url -OutFile "$env:TEMP\protoc.zip"
Expand-Archive "$env:TEMP\protoc.zip" -DestinationPath $dst -Force
$env:PATH = "$env:USERPROFILE\protoc\bin;$env:PATH"
```

Verify:
```powershell
protoc --version
# libprotoc 25.3
```

### 3.3 Go protoc plugins

```powershell
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
```

Verify both ended up in `$env:GOBIN`:
```powershell
Get-Command protoc-gen-go.exe, protoc-gen-go-grpc.exe | Select-Object Name, Source
# Name                       Source
# protoc-gen-go.exe          C:\Users\<you>\go\bin\protoc-gen-go.exe
# protoc-gen-go-grpc.exe     C:\Users\<you>\go\bin\protoc-gen-go-grpc.exe
```

### 3.4 Git + PowerShell 7

```powershell
winget install --id Git.Git -e
winget install --id Microsoft.PowerShell -e
```

### 3.5 (Optional) Docker Desktop

[Download from docker.com](https://www.docker.com/products/docker-desktop/)
and install (admin required once).

### 3.6 Verify everything

```powershell
pwsh -File <repo>\docs\tutorial\scripts\install-check.ps1
```

Expected (truncated):
```
[OK]   go         go1.22.10 windows/amd64
[OK]   protoc     libprotoc 25.3
[OK]   protoc-gen-go         v1.34.2
[OK]   protoc-gen-go-grpc    1.5.1
[OK]   git
[OK]   PATH includes GOBIN
[INFO] docker (optional)     Docker version 24.x
=== All required checks passed ===
```

---

## 4. Install on Linux

### Ubuntu / Debian

```bash
# Go
GO_VER=1.22.10
curl -fsSL "https://go.dev/dl/go${GO_VER}.linux-amd64.tar.gz" -o /tmp/go.tgz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf /tmp/go.tgz
echo 'export PATH=/usr/local/go/bin:$HOME/go/bin:$PATH' >> ~/.bashrc
echo 'export GOPATH=$HOME/go' >> ~/.bashrc
echo 'export GOBIN=$HOME/go/bin' >> ~/.bashrc
source ~/.bashrc
go version

# protoc
PROTOC_VER=25.3
curl -fsSL "https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VER}/protoc-${PROTOC_VER}-linux-x86_64.zip" -o /tmp/protoc.zip
sudo unzip -o /tmp/protoc.zip -d /usr/local
protoc --version

# protoc plugins
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

# Git + Docker (optional)
sudo apt update && sudo apt install -y git docker.io
```

### Fedora / RHEL

```bash
sudo dnf install -y golang protobuf-compiler git
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
```

### Arch

```bash
sudo pacman -Syu go protobuf git
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
```

### Verify on Linux

```bash
bash <repo>/docs/tutorial/scripts/install-check.sh
```

Expected:
```
[OK]   go         go version go1.22.10 linux/amd64
[OK]   protoc     libprotoc 25.3
[OK]   protoc-gen-go         v1.34.2
[OK]   protoc-gen-go-grpc    1.5.1
[OK]   git
[OK]   PATH includes $HOME/go/bin
[INFO] docker (optional)     Docker version 24.x
=== All required checks passed ===
```

---

## 5. (Optional) Rust toolchain

The `src/impl-rust/` workspace is a placeholder (no buildable crates yet),
but installing the toolchain now means future Rust builds work out of the
box.

### Windows + Linux (same command via rustup)

```powershell
# Windows PowerShell or Linux bash:
Invoke-RestMethod https://sh.rustup.rs -OutFile rustup.sh ; bash rustup.sh -y     # Linux
# or
winget install --id Rustlang.Rustup -e                                            # Windows
rustup toolchain install 1.78.0
rustc --version
```

The pinned toolchain is recorded in
[`src/impl-rust/rust-toolchain.toml`](../../src/impl-rust/rust-toolchain.toml).

---

## 6. Common installation issues

| Symptom | Fix |
|---|---|
| `go: command not found` | PATH doesn't include the Go bin dir. Re-source your shell config. |
| `protoc-gen-go: program not found or is not executable` | `GOBIN`/`$HOME/go/bin` not on PATH. |
| `winget install ... no applicable installer` | Use the manual zip-download path in §3 (no admin). |
| `Setting Environment Variable hangs` | Don't use `SetEnvironmentVariable` in this environment; use per-session `$env:PATH` instead. |
| `protoc` error `program 'go' not found in...` | The Go plugins (`protoc-gen-go`, `protoc-gen-go-grpc`) aren't on PATH. |
| Linux: `tar: /usr/local/go: Cannot mkdir: Permission denied` | Use `sudo`. |

---

## Where to go next

- → [04 — Build](04-build.md) — now compile every binary.
