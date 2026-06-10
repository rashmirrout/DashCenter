# DashCenter — Explore With Docker (Self-Contained Hands-On Manual)

> A fully self-contained, beginner-friendly lab guide.
>
> This document is **standalone**. It does not link to any other file in
> the repository. Every config, manifest, command, and expected output
> you need is inlined below. Follow the steps top-to-bottom and you will
> have a 6-container DashCenter fleet running locally, driven by the
> `dashctl` CLI end-to-end, and you will see — verbatim — the same
> output the maintainer saw on the verification run.
>
> **Audience**: a first-time contributor who has never seen this
> codebase, sitting in front of a Windows laptop with Docker Desktop and
> Go installed.
>
> **Time to first green**: ~10 minutes (5 of which is the initial
> `docker build`).
>
> **What you will achieve**:
> 1. Build `dashctl.exe` from source.
> 2. Bring up `dashd` + 5 simulated DPUs in Docker.
> 3. Apply 2 VNets + 5 ENIs through `dashctl apply -f`.
> 4. Watch all 5 DPUs converge with `0 drift items`.
> 5. Exercise 36 distinct `dashctl` commands and observe their outputs.
> 6. Drive the same fleet from inside a container.
> 7. Tear everything down cleanly.

---

## Table of contents

1. [What you will build](#1-what-you-will-build)
2. [Prerequisites & verification](#2-prerequisites--verification)
3. [Get the code](#3-get-the-code)
4. [Repository layout (only what we touch)](#4-repository-layout-only-what-we-touch)
5. [Build the `dashctl` host binary](#5-build-the-dashctl-host-binary)
6. [Bring the fleet up](#6-bring-the-fleet-up)
7. [Smoke checks — fleet is healthy](#7-smoke-checks--fleet-is-healthy)
8. [The 36-step `dashctl` walkthrough (host binary)](#8-the-36-step-dashctl-walkthrough-host-binary)
9. [Drive the fleet from inside a container](#9-drive-the-fleet-from-inside-a-container)
10. [Tear down](#10-tear-down)
11. [Troubleshooting (8 self-contained recipes)](#11-troubleshooting-8-self-contained-recipes)
12. [Appendix A — Full host environment-variable reference](#appendix-a--full-host-environment-variable-reference)
13. [Appendix B — Exit codes (stable contract)](#appendix-b--exit-codes-stable-contract)
14. [Appendix C — Inlined manifest, compose, and config files](#appendix-c--inlined-manifest-compose-and-config-files)
15. [Appendix D — Quick reference card](#appendix-d--quick-reference-card)

---

## 1. What you will build

Six Docker containers on a private bridge network, plus a host-side
`dashctl.exe` binary you drive interactively:

```
┌────────────────────────────── host laptop ──────────────────────────────┐
│                                                                          │
│   bin\dashctl.exe  ──HTTP──►  localhost:8443  (REST)                     │
│                    ──HTTP──►  localhost:7443  (admin)                    │
│                                                                          │
│   curl, jq, browser, anything you like also goes to those ports.         │
│                                                                          │
│                     ┌─────────────── docker network: dc-ctl-fleet ───┐   │
│                     │                                                 │   │
│                     │   dc-ctl-dashd        ◄── port-fwd 8443/7443   │   │
│                     │     │                                           │   │
│                     │     │  southbound gRPC :50051 (in-net only)    │   │
│                     │     ├──►  dc-ctl-sim-1   (admin: host:8181)    │   │
│                     │     ├──►  dc-ctl-sim-2   (admin: host:8182)    │   │
│                     │     ├──►  dc-ctl-sim-3   (admin: host:8183)    │   │
│                     │     ├──►  dc-ctl-sim-4   (admin: host:8184)    │   │
│                     │     └──►  dc-ctl-sim-5   (admin: host:8185)    │   │
│                     │                                                 │   │
│                     │   dc-ctl-dashd-init  (one-shot; chown volume)  │   │
│                     │   dc-ctl-dashctl     (profile-gated; on-demand)│   │
│                     │                                                 │   │
│                     │   named volume: dashd-state-ctl-fleet           │   │
│                     └─────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────────────┘
```

**Container roles**

| Container | Role | Lifecycle |
|---|---|---|
| `dc-ctl-sim-1` … `dc-ctl-sim-5` | DPU simulators (gRPC `:50051`, admin `:8080`) | long-running |
| `dc-ctl-dashd-init` | Pre-chowns the named volume to UID 65532 | one-shot, exits 0 |
| `dc-ctl-dashd` | Control plane: REST `:8443`, gRPC `:9443`, admin `:7443` | long-running |
| `dc-ctl-dashctl` | Operator CLI image | only created on `compose run` |

**Expected steady state after `up`**

- All 5 DPUs transition `REGISTERING` → `DPU_STATE_UP` within ~5 s.
- `dashctl get vnet` returns an empty list (no specs applied yet).
- After `dashctl apply -f` the manifests, you have 2 vnets + 5 ENIs.
- After `dashctl reconcile`, each DPU reports `0 drift items.`.

---

## 2. Prerequisites & verification

| Tool | Min. version | Why |
|---|---|---|
| Windows 10/11 with PowerShell 5+ or 7+ | — | Examples use `pwsh` |
| Git | any recent | Clone the repo |
| Go | 1.22.x | Build `dashctl.exe` from source |
| GNU make | 4.x | Optional but recommended (`choco install make`) |
| Docker Desktop | 24.0+ with `docker compose v2` plugin | Bring the fleet up |
| `curl.exe` | bundled with Windows 10+ | Manual REST verification |
| `jq` (optional) | any | Pretty-print JSON in examples |

**Verify each tool** — copy/paste exactly. If a command fails, install
that tool before proceeding.

```powershell
PS> git --version
git version 2.45.x.windows.1

PS> docker version --format '{{.Server.Version}}'
28.5.1

PS> docker compose version
Docker Compose version v2.40.0-desktop.1

PS> $env:Path = "C:\Users\rashmirout\go-sdk\go\bin;C:\Users\rashmirout\go\bin;$env:Path"
PS> go version
go version go1.22.10 windows/amd64

PS> make --version | Select-Object -First 1
GNU Make 4.4.1

PS> curl.exe --version | Select-Object -First 1
curl 8.x.x (Windows) libcurl/...
```

> **Adjust `$env:Path`** to where your own Go SDK lives. The examples
> assume `C:\Users\rashmirout\go-sdk\go\bin` and
> `C:\Users\rashmirout\go\bin`. Update both occurrences if yours differ.

**Required free ports on the host**: `8443`, `7443`, `9443`, `8181`,
`8182`, `8183`, `8184`, `8185`. Check with:

```powershell
PS> @(8443,7443,9443,8181,8182,8183,8184,8185) | ForEach-Object {
>>   $p = Get-NetTCPConnection -LocalPort $_ -State Listen -ErrorAction SilentlyContinue
>>   "{0}: {1}" -f $_, ($(if ($p) { 'IN USE - free it first' } else { 'free' }))
>> }
8443: free
7443: free
9443: free
8181: free
8182: free
8183: free
8184: free
8185: free
```

If any port shows `IN USE`, see §11.7.

---

## 3. Get the code

```powershell
PS> cd C:\WorkSpace\PS\PublicRepo
PS> git clone https://github.com/<your-fork>/DashCenter.git
PS> cd DashCenter
PS> git rev-parse --short HEAD
1f296ac
```

(Substitute your own clone path. The remainder of this manual uses
`C:\WorkSpace\PS\PublicRepo\DashCenter` as the working directory; adjust
to your own.)

---

## 4. Repository layout (only what we touch)

```
DashCenter\
├── deploy\
│   └── dashctl-fleet\           ← the fleet topology used in this lab
│       ├── docker-compose.yml   ← 6-container compose (verbatim copy in Appx C)
│       ├── configs\
│       │   ├── dashd.yaml       ← dashd runtime config (Appx C)
│       │   └── inventory.yaml   ← 5-DPU inventory (Appx C)
│       └── manifests\
│           ├── 00-vnets.yaml    ← 2 vnets (Appx C)
│           └── 10-enis.yaml     ← 5 ENIs, one per simulator (Appx C)
├── src\impl-go\
│   ├── dashd\                   ← control plane (built into dashd image)
│   ├── dash-sim\                ← DPU simulator  (built into dash-sim image)
│   └── dashctl\                 ← operator CLI
│       ├── cmd\dashctl\main.go  ← entry point
│       ├── Makefile             ← build / test / image targets
│       └── bin\                 ← where make build writes dashctl.exe
└── explore-with-docker\
    └── manual-handson.md        ← THIS FILE
```

Everything you need to run the lab is under `deploy\dashctl-fleet\` and
`src\impl-go\dashctl\`. The compose file builds the `dashd` and
`dash-sim` images from source automatically — no separate steps needed.

---

## 5. Build the `dashctl` host binary

You will use a host-side binary in steps §8 because it is faster and has
better shell ergonomics than `docker compose run` for interactive work.
§9 shows the in-container alternative.

### 5.1 Build with `make` (recommended)

```powershell
PS> $env:Path = "C:\Users\rashmirout\go-sdk\go\bin;C:\Users\rashmirout\go\bin;$env:Path"
PS> cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashctl
PS> make build
go build -trimpath -ldflags "-s -w -X main.version=0.1.0-dev -X main.commit=1f296ac287a9 -X main.buildDate=2026-06-09T12:38:10Z" -o bin/dashctl.exe ./cmd/dashctl
built bin/dashctl.exe (0.1.0-dev 1f296ac287a9)
```

Sanity-check:

```powershell
PS> .\bin\dashctl.exe version --client
Client: dashctl 0.1.0-dev (commit 1f296ac287a9, built 2026-06-09T12:38:10Z)
```

### 5.2 Build without `make`

```powershell
PS> cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashctl
PS> $env:CGO_ENABLED = "0"
PS> go build -trimpath -ldflags "-s -w" -o bin\dashctl.exe .\cmd\dashctl
PS> .\bin\dashctl.exe version --client
Client: dashctl 0.1.0-dev (commit none, built unknown)
```

(With raw `go build`, the version/commit/date ldflags are blank — that's
expected and harmless.)

### 5.3 Cross-compile every release target (optional)

```powershell
PS> make build-all
"-> dist/dashctl-0.1.0-dev-linux-amd64"
go env -w CGO_ENABLED=0 && go build -trimpath ... -o dist/dashctl-0.1.0-dev-linux-amd64 ./cmd/dashctl
"-> dist/dashctl-0.1.0-dev-linux-arm64"
...

PS> Get-ChildItem dist\ | Select-Object Name, Length
Name                                 Length
----                                 ------
dashctl-0.1.0-dev-darwin-amd64      8199936
dashctl-0.1.0-dev-darwin-arm64      7875922
dashctl-0.1.0-dev-linux-amd64       7983256
dashctl-0.1.0-dev-linux-arm64       7733400
dashctl-0.1.0-dev-windows-amd64.exe 8329728
```

### 5.4 Run the unit suite (optional, recommended)

```powershell
PS> make test
go test -count=1 -timeout 120s ./...
?       .../dashctl/cmd/dashctl                                          [no test files]
ok      .../dashctl/internal/cli                                         0.318s
ok      .../dashctl/internal/cmd                                         8.378s
ok      .../dashctl/internal/config                                      3.374s
ok      .../dashctl/internal/errors                                      0.288s
ok      .../dashctl/internal/render                                      3.174s
ok      .../dashctl/pkg/client                                           2.062s
ok      .../dashctl/pkg/client/rest                                      4.141s
ok      .../dashctl/pkg/manifest                                         0.304s

PS> make test-cover | Select-Object -Last 9
ok      .../dashctl/internal/cli      ...   coverage: 87.7% of statements
ok      .../dashctl/internal/cmd      ...   coverage: 80.7% of statements
ok      .../dashctl/internal/config   ...   coverage: 91.9% of statements
ok      .../dashctl/internal/errors   ...   coverage: 98.9% of statements
ok      .../dashctl/internal/render   ...   coverage: 92.6% of statements
ok      .../dashctl/pkg/client        ...   coverage: 100.0% of statements
ok      .../dashctl/pkg/client/rest   ...   coverage: 94.8% of statements
ok      .../dashctl/pkg/manifest      ...   coverage: 98.0% of statements
```

Aggregate coverage: **87.9 %** across the module.

---

## 6. Bring the fleet up

### 6.1 One-command bring-up

```powershell
PS> cd C:\WorkSpace\PS\PublicRepo\DashCenter
PS> docker compose -f deploy/dashctl-fleet/docker-compose.yml up -d --build
[+] Building 5/5
 => [dash-sim internal] load build definition from Dockerfile           0.0s
 => => transferring dockerfile: 1.45kB                                  0.0s
 => [dashd internal] load build definition from Dockerfile              0.0s
 => => transferring dockerfile: 1.62kB                                  0.0s
 => [dash-sim builder 1/5] FROM golang:1.22-bookworm                    0.4s
 => [dash-sim builder 2/5] WORKDIR /src                                 0.0s
 => [dash-sim builder 3/5] COPY src/impl-go/go.work ./                  0.0s
 => [dash-sim builder 4/5] COPY src/impl-go ./src/impl-go               0.1s
 => [dash-sim builder 5/5] RUN cd src/impl-go/dash-sim && go build...  92.3s
 => [dashd builder 5/5] RUN cd src/impl-go/dashd && go build...        87.1s
 => exporting to image                                                  0.4s
[+] Running 8/8
 ✔ Network dashctl-fleet_dc-ctl-fleet  Created                          0.1s
 ✔ Volume "dashd-state-ctl-fleet"      Created                          0.0s
 ✔ Container dc-ctl-sim-2              Started                          0.6s
 ✔ Container dc-ctl-sim-4              Started                          0.5s
 ✔ Container dc-ctl-sim-5              Started                          0.6s
 ✔ Container dc-ctl-sim-1              Started                          0.6s
 ✔ Container dc-ctl-sim-3              Started                          0.6s
 ✔ Container dc-ctl-dashd-init         Started                          0.7s
 ✔ Container dc-ctl-dashd              Started                          1.1s
```

> First run takes the longest because Docker has to download the Go
> base image and compile both binaries. Subsequent `up -d` calls reuse
> the cached layers and finish in seconds.

**Why the init container?** The distroless `dashd` image runs as
nonroot UID 65532, but Docker creates named volumes owned by root. Without
the init step that runs `chown -R 65532:65532 /var/lib/dashd` first, the
first call to `dashctl apply` fails with a silent EACCES inside dashd
and the client sees a bare `500 internal`. See §11.1.

### 6.2 Watch the logs (optional)

In a second terminal:

```powershell
PS> docker compose -f deploy/dashctl-fleet/docker-compose.yml logs -f dashd
dashd  | {"time":"...","level":"INFO","msg":"REST listening","addr":":8443"}
dashd  | {"time":"...","level":"INFO","msg":"admin listening","addr":":7443"}
dashd  | {"time":"...","level":"INFO","msg":"prober: DPU is UP","dpu":"dpu-sim-01"}
dashd  | {"time":"...","level":"INFO","msg":"prober: DPU is UP","dpu":"dpu-sim-02"}
dashd  | {"time":"...","level":"INFO","msg":"prober: DPU is UP","dpu":"dpu-sim-03"}
dashd  | {"time":"...","level":"INFO","msg":"prober: DPU is UP","dpu":"dpu-sim-04"}
dashd  | {"time":"...","level":"INFO","msg":"prober: DPU is UP","dpu":"dpu-sim-05"}
```

Press `Ctrl-C` to stop following (containers keep running).

---

## 7. Smoke checks — fleet is healthy

Three independent verifications before driving `dashctl`.

### 7.1 Container status

```powershell
PS> docker ps --filter "name=dc-ctl-" --format "table {{.Names}}\t{{.Status}}"
NAMES               STATUS
dc-ctl-dashd        Up About a minute
dc-ctl-sim-2        Up About a minute
dc-ctl-sim-4        Up About a minute
dc-ctl-sim-5        Up About a minute
dc-ctl-sim-3        Up About a minute
dc-ctl-sim-1        Up About a minute
```

The init container exited 0 immediately, so it does not show in
`docker ps` by default. Confirm it ran successfully:

```powershell
PS> docker ps -a --filter "name=dc-ctl-dashd-init" --format "table {{.Names}}\t{{.Status}}"
NAMES                STATUS
dc-ctl-dashd-init    Exited (0) 2 minutes ago
```

### 7.2 Dashd admin health (high-level)

```powershell
PS> curl.exe -s http://localhost:7443/admin/health | ConvertFrom-Json |
>>   Select-Object status, leader, @{Name="UP";Expression={
>>     ($_.dpus | Where-Object state -eq "DPU_STATE_UP").Count
>>   }}

status leader UP
------ ------ --
ok       True  5
```

### 7.3 Raw inventory (REST)

```powershell
PS> curl.exe -s http://localhost:8443/v1/inventory | jq
{
  "dpus": [
    { "id": "dpu-sim-01", "endpoint": "dash-sim-1:50051", "state": "DPU_STATE_UP" },
    { "id": "dpu-sim-02", "endpoint": "dash-sim-2:50051", "state": "DPU_STATE_UP" },
    { "id": "dpu-sim-03", "endpoint": "dash-sim-3:50051", "state": "DPU_STATE_UP" },
    { "id": "dpu-sim-04", "endpoint": "dash-sim-4:50051", "state": "DPU_STATE_UP" },
    { "id": "dpu-sim-05", "endpoint": "dash-sim-5:50051", "state": "DPU_STATE_UP" }
  ]
}
```

Notice the `"state"` field is the **string** `"DPU_STATE_UP"`, not a
number. If yours shows a number (e.g. `"state": 2`), you are on a build
that pre-dates the fix in §11.2 — pull latest.

If you don't have `jq` installed, this works too:

```powershell
PS> curl.exe -s http://localhost:8443/v1/inventory
{"dpus":[{"id":"dpu-sim-01","endpoint":"dash-sim-1:50051","state":"DPU_STATE_UP"},...]}
```

---

## 8. The 36-step `dashctl` walkthrough (host binary)

Run these in a fresh PowerShell window. Set up once:

```powershell
PS> $env:Path = "C:\Users\rashmirout\go-sdk\go\bin;C:\Users\rashmirout\go\bin;$env:Path"
PS> $env:DASHCTL_ENDPOINT       = "http://localhost:8443"
PS> $env:DASHCTL_ADMIN_ENDPOINT = "http://localhost:7443"
PS> $bin = "C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashctl\bin\dashctl.exe"
```

Each step shows the **command** and the **verbatim output** from the
maintainer's verification run.

---

### Step 1 — `version` (client + server)

```powershell
PS> & $bin version
Client: dashctl 0.1.0-dev (commit 1f296ac287a9, built 2026-06-09T12:38:10Z)
Server: dashd  dashd (transport=rest endpoint=http://localhost:8443) leader=true
```

### Step 2 — `version --client` (no server dial)

```powershell
PS> & $bin version --client
Client: dashctl 0.1.0-dev (commit 1f296ac287a9, built 2026-06-09T12:38:10Z)
```

### Step 3 — `dpu list -o table`

```powershell
PS> & $bin dpu list -o table
ID           ENDPOINT   STATE          LAST_SEEN
dpu-sim-01              DPU_STATE_UP   2026-06-09T12:39:51Z
dpu-sim-02              DPU_STATE_UP   2026-06-09T12:39:51Z
dpu-sim-03              DPU_STATE_UP   2026-06-09T12:39:51Z
dpu-sim-04              DPU_STATE_UP   2026-06-09T12:39:51Z
dpu-sim-05              DPU_STATE_UP   2026-06-09T12:39:51Z
```

### Step 4 — `dpu list -o json`

```powershell
PS> & $bin dpu list -o json
{
  "apiVersion": "dashcenter.v1",
  "dpus": [
    { "id": "dpu-sim-01", "state": "DPU_STATE_UP", "last_seen": "2026-06-09T12:39:51Z" },
    { "id": "dpu-sim-02", "state": "DPU_STATE_UP", "last_seen": "2026-06-09T12:39:51Z" },
    { "id": "dpu-sim-03", "state": "DPU_STATE_UP", "last_seen": "2026-06-09T12:39:51Z" },
    { "id": "dpu-sim-04", "state": "DPU_STATE_UP", "last_seen": "2026-06-09T12:39:51Z" },
    { "id": "dpu-sim-05", "state": "DPU_STATE_UP", "last_seen": "2026-06-09T12:39:51Z" }
  ],
  "kind": "InventoryList"
}
```

### Step 5 — `dpu list -o name` (one ref per line, scriptable)

```powershell
PS> & $bin dpu list -o name
dpu/dpu-sim-01
dpu/dpu-sim-02
dpu/dpu-sim-03
dpu/dpu-sim-04
dpu/dpu-sim-05
```

### Step 6 — `inventory get -o table`

```powershell
PS> & $bin inventory get -o table
ID           ENDPOINT           STATE          LAST_SEEN
dpu-sim-01   dash-sim-1:50051   DPU_STATE_UP
dpu-sim-02   dash-sim-2:50051   DPU_STATE_UP
dpu-sim-03   dash-sim-3:50051   DPU_STATE_UP
dpu-sim-04   dash-sim-4:50051   DPU_STATE_UP
dpu-sim-05   dash-sim-5:50051   DPU_STATE_UP
```

### Step 7 — `apply -f <manifest-dir>`

```powershell
PS> & $bin apply -f C:\WorkSpace\PS\PublicRepo\DashCenter\deploy\dashctl-fleet\manifests
vnet/vnet-app apply in namespace default (generation 1)
vnet/vnet-db apply in namespace default (generation 1)
eni/eni-app-01 apply in namespace default (generation 1)
eni/eni-app-02 apply in namespace default (generation 1)
eni/eni-db-01 apply in namespace default (generation 1)
eni/eni-db-02 apply in namespace default (generation 1)
eni/eni-db-03 apply in namespace default (generation 1)
```

`dashctl` walks the directory in lexicographic order (`00-vnets.yaml`,
`10-enis.yaml`), parses each as a multi-document YAML stream, and PUTs
each envelope to dashd's REST endpoint as
`/v1/default/{plural}/{name}`.

### Step 8 — `get vnet -o table`

```powershell
PS> & $bin get vnet -o table
NAMESPACE   NAME       VNI    GENERATION   LABELS
default     vnet-app   1001   1            tier=app
default     vnet-db    1002   1            tier=db
```

### Step 9 — `get vnet vnet-app -o yaml`

```powershell
PS> & $bin get vnet vnet-app -o yaml
apiVersion: dashcenter.v1
kind: Vnet
metadata:
  namespace: default
  name: vnet-app
  generation: 1
  labels:
    tier: app
spec:
  vni: 1001
```

### Step 10 — `get eni -o wide`

```powershell
PS> & $bin get eni -o wide
NAMESPACE   NAME         VNET       MAC                 UNDERLAY    ADMIN   PLACED-ON    GEN
default     eni-app-01   vnet-app   00:11:22:00:00:01   10.0.5.11   up      dpu-sim-01   1
default     eni-app-02   vnet-app   00:11:22:00:00:02   10.0.5.12   up      dpu-sim-02   1
default     eni-db-01    vnet-db    00:11:22:00:00:03   10.0.6.11   up      dpu-sim-03   1
default     eni-db-02    vnet-db    00:11:22:00:00:04   10.0.6.12   up      dpu-sim-04   1
default     eni-db-03    vnet-db    00:11:22:00:00:05   10.0.6.13   up      dpu-sim-05   1
```

`-o wide` reveals the `PLACED-ON` column.

### Step 11 — Label selector

```powershell
PS> & $bin get eni -l tier=db -o name
eni/eni-db-01
eni/eni-db-02
eni/eni-db-03
```

### Step 12 — `describe eni eni-app-01`

```powershell
PS> & $bin describe eni eni-app-01
Name:        eni-app-01
Namespace:   default
Kind:        Eni
Generation:  1
Labels:      tier=app
Spec:
  admin_state: up
  mac_address: 00:11:22:00:00:01
  placement_hint_dpu_ids: [dpu-sim-01]
  underlay_ip: 10.0.5.11
  vnet_name: vnet-app
```

### Step 13 — `reconcile` (fan-out to every DPU)

```powershell
PS> & $bin reconcile
Triggered reconcile on all DPUs.
```

### Step 14 — `dpu drift --dpu dpu-sim-01`

```powershell
PS> Start-Sleep 6   # let the reconciler tick
PS> & $bin dpu drift --dpu dpu-sim-01
0 drift items.
```

### Step 15 — `dpu describe dpu-sim-03`

```powershell
PS> & $bin dpu describe dpu-sim-03
Name:        dpu-sim-03
Endpoint:
State:       DPU_STATE_UP
Last seen:   2026-06-09T12:39:51Z
Labels:      <none>
Drift:       0 item(s)
```

### Step 16 — `get` with `-o jsonpath`

```powershell
PS> & $bin get vnet vnet-app -o "jsonpath={.spec.vni}"
1001
```

### Step 17 — `get` with `-o template`

```powershell
PS> & $bin get vnet vnet-app -o "template={{ .spec.vni }}`n"
1001
```

### Step 18 — `delete eni eni-db-03`

```powershell
PS> & $bin delete eni eni-db-03
eni/eni-db-03 deleted
```

### Step 19 — Verify deletion

```powershell
PS> & $bin get eni -o table
NAMESPACE   NAME         VNET       MAC                 UNDERLAY    ADMIN   GEN
default     eni-app-01   vnet-app   00:11:22:00:00:01   10.0.5.11   up      1
default     eni-app-02   vnet-app   00:11:22:00:00:02   10.0.5.12   up      1
default     eni-db-01    vnet-db    00:11:22:00:00:03   10.0.6.11   up      1
default     eni-db-02    vnet-db    00:11:22:00:00:04   10.0.6.12   up      1
```

### Step 20 — Idempotent re-delete (stable exit code 3 = NOT_FOUND)

```powershell
PS> & $bin delete eni eni-db-03
Error: not found
Code: NOT_FOUND
PS> $LASTEXITCODE
3

PS> & $bin delete eni eni-db-03 --ignore-not-found
eni/eni-db-03 deleted
PS> $LASTEXITCODE
0
```

### Step 21 — `replace -f` with `metadata.generation` (CAS happy path)

```powershell
PS> @"
apiVersion: dashcenter.v1
kind: Vnet
metadata:
  name: vnet-app
  generation: 1
spec:
  vni: 1099
"@ | Set-Content -Path C:\Temp\v.yaml -Encoding ascii

PS> & $bin replace -f C:\Temp\v.yaml
vnet/vnet-app apply in namespace default (generation 2)
```

### Step 22 — Stale-generation `replace` (CAS rejects the write)

```powershell
PS> & $bin replace -f C:\Temp\v.yaml          # file still says generation: 1, server is now 2
vnet/vnet-app FAILED apply in namespace default: generation mismatch
Error: generation mismatch
Code: FAILED_PRECONDITION
Hint: re-fetch and retry with the latest generation
PS> $LASTEXITCODE
4
```

### Step 23 — Typed-kind subgroup (`vnet put`, `vnet list`, `vnet delete`)

```powershell
PS> @"
apiVersion: dashcenter.v1
kind: Vnet
metadata: { name: vnet-staging }
spec: { vni: 1003 }
"@ | Set-Content -Path C:\Temp\vstg.yaml -Encoding ascii

PS> & $bin vnet put -f C:\Temp\vstg.yaml
vnet/vnet-staging apply in namespace default (generation 1)

PS> & $bin vnet list -o name
vnet/vnet-app
vnet/vnet-db
vnet/vnet-staging

PS> & $bin vnet describe vnet-staging
Name:        vnet-staging
Namespace:   default
Kind:        Vnet
Generation:  1
Spec:
  vni: 1003

PS> & $bin vnet delete vnet-staging
PS> $LASTEXITCODE
0
```

### Step 24 — `config set-context` + `config view`

```powershell
PS> & $bin config set-context fleet --endpoint http://localhost:8443 --namespace default
Context "fleet" saved.

PS> & $bin config view
apiVersion: dashctl/v1
contexts:
  fleet:
    auth: {}
    endpoint: http://localhost:8443
    namespace: default
    tls: {}
    transport: ""
current-context: fleet
kind: Config
preferences: {}
```

Note the lowercase YAML keys (`apiVersion`, `contexts`, `current-context`,
`kind`, `preferences`) — this is the post-fix output. If yours shows
PascalCase Go-style names (`APIVersion`, `Contexts`, …), see §11.4.

### Step 25 — Other `config` subcommands

```powershell
PS> & $bin config get-contexts
* fleet

PS> & $bin config current-context
fleet

PS> & $bin config rename-context fleet local-fleet
Context "fleet" → "local-fleet".

PS> & $bin config delete-context local-fleet
Context "local-fleet" deleted.
```

### Step 26 — `diff -f` no-op (no spurious deltas)

```powershell
PS> @"
apiVersion: dashcenter.v1
kind: Vnet
metadata: { name: vnet-app, labels: { tier: app } }
spec: { vni: 1099 }
"@ | Set-Content -Path C:\Temp\d.yaml -Encoding ascii

PS> & $bin diff -f C:\Temp\d.yaml
no changes
```

If you see spurious `name → <nil>` or `expected_generation → <nil>`
rows, see §11.5.

### Step 27 — `diff -f` with a real change

```powershell
PS> @"
apiVersion: dashcenter.v1
kind: Vnet
metadata: { name: vnet-app }
spec: { vni: 9999 }
"@ | Set-Content -Path C:\Temp\d.yaml -Encoding ascii

PS> & $bin diff -f C:\Temp\d.yaml
vnet/vnet-app  vni: 1099 → 9999

1 spec(s) would change.
```

### Step 28 — `apply --dry-run client`

```powershell
PS> & $bin apply -f C:\Temp\d.yaml --dry-run client
vnet/vnet-app would dry-run in namespace default
```

### Step 29 — `explain vnet` (offline; no server dial)

```powershell
PS> & $bin explain vnet
KIND:     Vnet
VERSION:  dashcenter.v1

FIELDS:
  namespace     <string>
    Tenant namespace (defaults to 'default').

  name  <string>
    VNet name. Required and unique within a namespace.

  vni   <uint32>
    L2/L3 VNet identifier.

  guid  <string>
    Optional opaque global identifier.

  labels        <map<string,string>>
    Free-form labels for selectors.

  expected_generation   <uint64>
    If non-zero, optimistic concurrency.
```

### Step 30 — Shell completion (Cobra-generated)

```powershell
PS> & $bin completion bash | Select-Object -First 5
# bash completion V2 for dashctl                              -*- shell-script -*-

__dashctl_debug()
{
    if [[ -n ${BASH_COMP_DEBUG_FILE-} ]]; then

# Source the completion in your current PowerShell session:
PS> & $bin completion powershell | Out-String | Invoke-Expression

# Other shells available:
PS> & $bin completion zsh
PS> & $bin completion fish
```

### Step 31 — Phase-2 stubs return typed `UNIMPLEMENTED`

```powershell
PS> & $bin ha switchover
Error: switchover: requires dashd Phase 2
Code: UNIMPLEMENTED
Hint: Track progress in specs/Impl-Plan/impl-phases.md
PS> $LASTEXITCODE
9

PS> & $bin migration plan
Error: plan: requires dashd Phase 2
Code: UNIMPLEMENTED

PS> & $bin trace flow
Error: flow: requires dashd Phase 2
Code: UNIMPLEMENTED
```

### Step 32 — `events --watch` (placeholder for Phase-2 streaming)

```powershell
PS> & $bin events --watch
dashctl events: streaming is provided by dashd Phase 2 (WatchEvents).
Error: events: not yet supported by dashd Phase 1B
Code: UNIMPLEMENTED
```

### Step 33 — Fleet-wide drift sweep (the end-to-end victory)

```powershell
PS> & $bin reconcile
Triggered reconcile on all DPUs.

PS> Start-Sleep 6
PS> foreach ($d in @("dpu-sim-01","dpu-sim-02","dpu-sim-03","dpu-sim-04","dpu-sim-05")) {
>>     Write-Host "  $d :" -NoNewline; & $bin dpu drift --dpu $d
>> }
  dpu-sim-01 :0 drift items.
  dpu-sim-02 :0 drift items.
  dpu-sim-03 :0 drift items.
  dpu-sim-04 :0 drift items.
  dpu-sim-05 :0 drift items.
```

**All 5 DPUs converged. End-to-end working.**

### Step 34 — `--help` for any verb

```powershell
PS> & $bin apply --help
Apply reads manifests from -f and PUTs each spec to dashd.

Manifests may be a single YAML/JSON file, a directory of YAML/JSON files,
or "-" for stdin. Multi-document YAML (separated by '---') is supported.

Usage:
  dashctl apply [flags]

Flags:
      --dry-run string         none|client|server (default "none")
  -f, --filename stringArray   manifest file, directory, or '-' for stdin (repeatable)
  -h, --help                   help for apply
  -R, --recursive              recursively process the given directory
```

### Step 35 — Unreachable-server graceful exit

```powershell
PS> $env:DASHCTL_ENDPOINT       = "http://127.0.0.1:1"
PS> $env:DASHCTL_ADMIN_ENDPOINT = "http://127.0.0.1:1"

PS> & $bin version
Client: dashctl 0.1.0-dev (commit 1f296ac287a9, built 2026-06-09T12:38:10Z)
Server: unavailable (network error: Get "http://127.0.0.1:1/admin/health": dial tcp 127.0.0.1:1: connectex: No connection could be made because the target machine actively refused it.)
PS> $LASTEXITCODE
0
```

(`version` is documented to always return exit 0, even with an
unreachable server. Other verbs return exit code 7 = UNAVAILABLE.)

Reset for subsequent steps:

```powershell
PS> $env:DASHCTL_ENDPOINT       = "http://localhost:8443"
PS> $env:DASHCTL_ADMIN_ENDPOINT = "http://localhost:7443"
```

### Step 36 — `-n <ns>` namespace override

```powershell
PS> & $bin -n default get vnet -o table
NAMESPACE   NAME       VNI    GENERATION   LABELS
default     vnet-app   1099   2            tier=app
default     vnet-db    1002   1            tier=db
```

`-n default` overrides the default namespace coming from env or config.
This is the safest invocation when you are not sure what context is
currently active.

---

## 9. Drive the fleet from inside a container

The `dashctl` compose service is profile-gated, so `up` does NOT start
it. Invoke it on demand.

### Step C1 — Container `version`

```powershell
PS> docker compose -f deploy/dashctl-fleet/docker-compose.yml run --rm dashctl version
Client: dashctl 0.1.0-dev (commit none, built unknown)
Server: dashd  dashd (transport=rest endpoint=http://dashd:8443) leader=true
```

The container reaches dashd via the in-network hostname `dashd:8443`.
No `--insecure` flag is required because the compose file sets
`DASHCTL_INSECURE: "true"` (see Appendix C, line `DASHCTL_INSECURE`).
If yours errors with `plaintext HTTP to "http://dashd:8443" refused`,
see §11.6.

### Step C2 — Container `dpu list`

```powershell
PS> docker compose -f deploy/dashctl-fleet/docker-compose.yml run --rm dashctl dpu list -o table
ID           ENDPOINT   STATE          LAST_SEEN
dpu-sim-01              DPU_STATE_UP   2026-06-09T12:39:51Z
dpu-sim-02              DPU_STATE_UP   2026-06-09T12:39:51Z
dpu-sim-03              DPU_STATE_UP   2026-06-09T12:39:51Z
dpu-sim-04              DPU_STATE_UP   2026-06-09T12:39:51Z
dpu-sim-05              DPU_STATE_UP   2026-06-09T12:39:51Z
```

### Step C3 — Container `apply -f` with a bind mount

```powershell
PS> docker compose -f deploy/dashctl-fleet/docker-compose.yml run --rm `
>>     -v "$(pwd)/deploy/dashctl-fleet/manifests:/work:ro" `
>>     --entrypoint /usr/local/bin/dashctl `
>>     dashctl -n default apply -f /work
vnet/vnet-app apply in namespace default (generation 2)
vnet/vnet-db apply in namespace default (generation 2)
eni/eni-app-01 apply in namespace default (generation 2)
eni/eni-app-02 apply in namespace default (generation 2)
eni/eni-db-01 apply in namespace default (generation 2)
eni/eni-db-02 apply in namespace default (generation 2)
eni/eni-db-03 apply in namespace default (generation 2)
```

Each spec advanced its generation by 1 because the body changed (these
manifests don't carry `metadata.generation`, so dashd treats them as
unconditional upserts and bumps gen).

### Step C4 — `docker exec` into a sidecar shell (debug)

The distroless `dashctl` image has no shell. To poke around interactively,
use an Alpine container on the same network and run the binary by hand:

```powershell
PS> docker run --rm -it --network dashctl-fleet_dc-ctl-fleet `
>>     -e DASHCTL_ENDPOINT=http://dashd:8443 `
>>     -e DASHCTL_ADMIN_ENDPOINT=http://dashd:7443 `
>>     -e DASHCTL_INSECURE=true `
>>     -v "${PWD}\src\impl-go\dashctl\bin\dashctl.exe:/usr/local/bin/dashctl:ro" `
>>     alpine:3.20 sh

/ # /usr/local/bin/dashctl get vnet
NAMESPACE   NAME       VNI    GENERATION   LABELS
default     vnet-app   1099   2            tier=app
default     vnet-db    1002   2            tier=db
/ # exit
```

(Linux binary required for that mount. On Windows, alternatively build a
Linux binary with `make build-all` first — see §5.3 — and mount
`dist/dashctl-0.1.0-dev-linux-amd64`.)

---

## 10. Tear down

### 10.1 Stop, keep state on the volume

```powershell
PS> docker compose -f deploy/dashctl-fleet/docker-compose.yml down
[+] Running 7/7
 ✔ Container dc-ctl-dashd          Stopped     1.2s
 ✔ Container dc-ctl-dashd-init     Removed     0.0s
 ✔ Container dc-ctl-sim-1          Stopped     0.7s
 ✔ Container dc-ctl-sim-2          Stopped     0.7s
 ✔ Container dc-ctl-sim-3          Stopped     0.7s
 ✔ Container dc-ctl-sim-4          Stopped     0.7s
 ✔ Container dc-ctl-sim-5          Stopped     0.7s
 ✔ Network dashctl-fleet_dc-ctl-fleet  Removed   0.4s
```

(Next `up -d` re-uses the named volume, so previously applied specs
survive a restart.)

### 10.2 Stop AND wipe state

```powershell
PS> docker compose -f deploy/dashctl-fleet/docker-compose.yml down -v
 Network dashctl-fleet_default  Removing
 Volume dashd-state-ctl-fleet   Removing
 Volume dashd-state-ctl-fleet   Removed
 Network dashctl-fleet_dc-ctl-fleet   Removed
 Network dashctl-fleet_default  Removed
```

Use `-v` when you want a true clean slate for the next run.

---

## 11. Troubleshooting (8 self-contained recipes)

Each subsection is **independent**: symptom you can recognise, root
cause in plain English, the change the maintainers made, and a one-liner
you can run today to confirm you're not affected.

### 11.1 First `dashctl apply` fails with `internal`

| | |
|---|---|
| **Symptom** | `vnet/vnet-app FAILED apply in namespace default: internal` (no detail). |
| **Root cause** | The distroless `dashd` image runs as UID 65532, but Docker created the named volume `dashd-state-ctl-fleet` owned by root. Dashd's first `store.Put` hits `EACCES`. |
| **Fix in repo** | An Alpine init container (`dc-ctl-dashd-init`) runs `chown -R 65532:65532 /var/lib/dashd` before dashd starts. Dashd `depends_on: dashd-init: { condition: service_completed_successfully }`. Both fleets carry this init container. |
| **Verify on your box** | `docker ps -a --filter "name=dc-ctl-dashd-init" --format "{{.Status}}"` shows `Exited (0)`. Step 7 (`dashctl apply`) succeeds with `generation 1`. |
| **Manual recovery** if you ever lose the init container: `docker run --rm -v dashd-state-ctl-fleet:/data alpine chown -R 65532:65532 /data` then `docker restart dc-ctl-dashd`. |

### 11.2 `dashctl inventory get` fails to decode

| | |
|---|---|
| **Symptom** | `Error: rest: decode response`. But `dashctl dpu list` works fine. |
| **Root cause** | Old dashd REST `/v1/inventory` serialized `DpuState` as a number (proto enum), but dashctl's `DpuStatus.State` is a string. `json.Unmarshal` of a number into a string field errors out with `cannot unmarshal number into Go struct field`. |
| **Fix in repo** | `getInventory` now projects `service.DpuStatus` to an anonymous struct that calls `s.State.String()` before JSON marshalling. |
| **Verify** | `curl http://localhost:8443/v1/inventory \| jq '.dpus[].state'` returns `"DPU_STATE_UP"` (string, with quotes). Step 6 succeeds. |

### 11.3 Dashd returns bare `500 internal` body

| | |
|---|---|
| **Symptom** | A failing request returns `{"error":"internal"}` with nothing useful, and dashd's log has no matching error line. |
| **Root cause** | The REST error handler used to call `writeErr(w, 500, errors.New("internal"))` for any unclassified failure, swallowing the real reason. |
| **Fix in repo** | Same handler now `slog.Error`-logs the underlying error AND returns `{"error":"internal: <truncated reason>"}`. Reason is truncated to 240 chars to avoid log-injection-style abuse. |
| **Verify** | Trigger any 500 (e.g. by replicating §11.1) and the response body now contains `internal: <reason>` plus the dashd container log shows `level=ERROR msg="rest: internal error returned to client"`. |

### 11.4 `dashctl config view` prints Go field names

| | |
|---|---|
| **Symptom** | `dashctl config view` shows PascalCase Go-style keys (`APIVersion`, `Contexts`, `CurrentContext`, …) even though the on-disk file uses lowercase. |
| **Root cause** | The render layer pretty-prints YAML by round-tripping through JSON. The `Config` struct had `yaml:` tags but no matching `json:` tags, so `json.Marshal` emitted Go field names and `yaml.Marshal` faithfully re-emitted them. |
| **Fix in repo** | Every `Config` / `ContextEntry` / `AuthConfig` / `TLSConfig` / `Preferences` field now carries matching `yaml:` AND `json:` tags. |
| **Verify** | Step 24's output shows `apiVersion`, `contexts`, `current-context`, `kind`, `preferences` — all lowercase. |

### 11.5 `dashctl diff -f` shows spurious `<nil>` rows

| | |
|---|---|
| **Symptom** | `dashctl diff -f stable.yaml` of an unchanged manifest reports `name → <nil>` or `expected_generation → <nil>`. |
| **Root cause** | `compareSpecs` stripped projected fields (`name`, `namespace`, `expected_generation`, `labels`) from the proposed-side map but not from the server-side map. Dashd embeds those fields inside the stored spec body. |
| **Fix in repo** | A new `pruneProjected(m map[string]any) map[string]any` helper is now applied to **both** sides before the diff. |
| **Verify** | Step 26 (no-op diff) prints exactly `no changes`. Step 27 (real change) prints exactly one line: `vnet/vnet-app  vni: 1099 → 9999`. |

### 11.6 Container `dashctl` errors `plaintext HTTP refused`

| | |
|---|---|
| **Symptom** | `docker compose run --rm dashctl version` → `Server: unavailable (config: plaintext HTTP to "http://dashd:8443" refused; pass --insecure or use https://)`. |
| **Root cause** | dashctl refuses plaintext HTTP to non-localhost endpoints unless `--insecure` is set. From inside the container, `dashd:8443` is not localhost. |
| **Fix in repo** | dashctl now reads `DASHCTL_INSECURE` (truthy: `1/true/yes/y/on`) from the environment, and the compose `dashctl` service sets `DASHCTL_INSECURE: "true"`. |
| **Verify** | Step C1 succeeds without `--insecure` and prints `Server: dashd dashd (transport=rest endpoint=http://dashd:8443) leader=true`. |

### 11.7 Port already in use

| | |
|---|---|
| **Symptom** | `docker compose ... up` fails with `bind: Only one usage of each socket address (protocol/network address/port) is normally permitted`. |
| **Root cause** | Another stack (often a leftover `go run dashd` from an integration test, or a sibling compose) is holding `:8443 / :9443 / :7443 / :8181-8185`. |
| **Diagnose** | `Get-NetTCPConnection -LocalPort 8443 -State Listen \| ForEach-Object { Get-Process -Id $_.OwningProcess }` |
| **Fix** | (a) `docker compose -f <other-stack-compose>.yml down -v` for sibling compose stacks, or (b) `taskkill /F /PID <pid>` for stray Go processes. PowerShell's `Stop-Process` sometimes fails silently on Go binaries — use `taskkill /F` for reliability. |

### 11.8 Stale dashctl config sets the wrong namespace

| | |
|---|---|
| **Symptom** | `dashctl get vnet` returns nothing even though apply succeeded. `dashctl -n default get vnet` works (Step 36). |
| **Root cause** | A previous `dashctl config set-context … --namespace ns-a` left a config file at `%APPDATA%\dashctl\config` (Windows) or `~/.config/dashctl/config` (POSIX). The current-context's `namespace: ns-a` overrides the default. |
| **Fix** | `Remove-Item $env:APPDATA\dashctl\config` then re-run, OR `dashctl config delete-context <name>` then `dashctl config set-context fleet --namespace default`. |
| **Prevent** | Always pass `-n default` (or whichever namespace you mean) when in doubt. `dashctl config view` shows the active context — see Step 24. |

---

## Appendix A — Full host environment-variable reference

| Variable | Default | Purpose |
|---|---|---|
| `DASHCTL_CONFIG` | platform default (`%APPDATA%\dashctl\config` on Windows) | path to dashctl config file |
| `DASHCTL_CONTEXT` | `current-context` from config | named context to use |
| `DASHCTL_ENDPOINT` | `http://localhost:8443` | dashd REST endpoint |
| `DASHCTL_ADMIN_ENDPOINT` | `http://localhost:7443` | dashd admin endpoint |
| `DASHCTL_TRANSPORT` | `rest` (Phase 1) | `rest` / `grpc` |
| `DASHCTL_NAMESPACE` | `default` | spec namespace |
| `DASHCTL_OUTPUT` | auto (table on TTY, json on pipe) | default `-o` |
| `DASHCTL_TIMEOUT` | `30s` | per-RPC timeout |
| `DASHCTL_TOKEN` | — | bearer token |
| `DASHCTL_INSECURE` | `false` | allow plaintext HTTP to non-localhost (`1/true/yes/y/on`) |
| `NO_COLOR` | unset | disable colour output (industry standard) |

---

## Appendix B — Exit codes (stable contract)

| Code | Meaning | Example |
|---|---|---|
| 0 | success | most happy paths |
| 1 | generic CLI error | bad flag, parse failure |
| 2 | usage error | Cobra surfaced this |
| 3 | not-found | `dashctl get vnet missing` (Step 20) |
| 4 | conflict / generation mismatch | `dashctl replace -f stale.yaml` (Step 22) |
| 5 | validation error | bad spec field, server returned `INVALID_ARGUMENT` |
| 6 | permission denied | bad token, role boundary (Phase 2) |
| 7 | unavailable | `UNAVAILABLE` / 503 (every verb except `version` if server is down) |
| 8 | timeout | `DEADLINE_EXCEEDED` |
| 9 | unimplemented | `dashctl ha switchover` (Step 31) |
| 10 | internal | unclassified 5xx |
| 130 | cancelled by signal | Ctrl-C |

---

## Appendix C — Inlined manifest, compose, and config files

Reproduced verbatim so you can run this lab without browsing the repo
tree. If you want to edit them, the on-disk paths are shown in the
heading.

### C.1 `deploy\dashctl-fleet\manifests\00-vnets.yaml`

```yaml
# dashctl manifest set for the dashctl-fleet end-to-end walkthrough.
#
# Applied in lexicographic order by `dashctl apply -f <dir>`:
#   00-vnets.yaml
#   10-enis.yaml

apiVersion: dashcenter.v1
kind: Vnet
metadata:
  name: vnet-app
  namespace: default
  labels: { tier: app }
spec:
  vni: 1001
---
apiVersion: dashcenter.v1
kind: Vnet
metadata:
  name: vnet-db
  namespace: default
  labels: { tier: db }
spec:
  vni: 1002
```

### C.2 `deploy\dashctl-fleet\manifests\10-enis.yaml`

```yaml
# ENI manifests spread across the 5-DPU fleet.
# Names line up with sim device IDs (dpu-sim-01..05) advertised by
# the inventory in configs/inventory.yaml.

apiVersion: dashcenter.v1
kind: Eni
metadata:
  name: eni-app-01
  namespace: default
  labels: { tier: app }
spec:
  vnet_name: vnet-app
  mac_address: "00:11:22:00:00:01"
  underlay_ip: "10.0.5.11"
  admin_state: "up"
  placement_hint_dpu_ids: ["dpu-sim-01"]
---
apiVersion: dashcenter.v1
kind: Eni
metadata:
  name: eni-app-02
  namespace: default
  labels: { tier: app }
spec:
  vnet_name: vnet-app
  mac_address: "00:11:22:00:00:02"
  underlay_ip: "10.0.5.12"
  admin_state: "up"
  placement_hint_dpu_ids: ["dpu-sim-02"]
---
apiVersion: dashcenter.v1
kind: Eni
metadata:
  name: eni-db-01
  namespace: default
  labels: { tier: db }
spec:
  vnet_name: vnet-db
  mac_address: "00:11:22:00:00:03"
  underlay_ip: "10.0.6.11"
  admin_state: "up"
  placement_hint_dpu_ids: ["dpu-sim-03"]
---
apiVersion: dashcenter.v1
kind: Eni
metadata:
  name: eni-db-02
  namespace: default
  labels: { tier: db }
spec:
  vnet_name: vnet-db
  mac_address: "00:11:22:00:00:04"
  underlay_ip: "10.0.6.12"
  admin_state: "up"
  placement_hint_dpu_ids: ["dpu-sim-04"]
---
apiVersion: dashcenter.v1
kind: Eni
metadata:
  name: eni-db-03
  namespace: default
  labels: { tier: db }
spec:
  vnet_name: vnet-db
  mac_address: "00:11:22:00:00:05"
  underlay_ip: "10.0.6.13"
  admin_state: "up"
  placement_hint_dpu_ids: ["dpu-sim-05"]
```

### C.3 `deploy\dashctl-fleet\configs\dashd.yaml`

```yaml
# dashd configuration for the dashctl-fleet (5-DPU + CLI demo).

listen:
  rest_addr:  ":8443"
  grpc_addr:  ":9443"
  admin_addr: ":7443"

storage:
  backend: file
  file:
    state_dir: /var/lib/dashd

inventory:
  source: file
  file:   /etc/dashd/inventory.yaml

reconcile:
  tick_interval:        15s
  per_dpu_inbox_size:   1
  apply_rate_limit:     100
  error_budget_per_min: 10

log:
  level:  info
  format: json
```

### C.4 `deploy\dashctl-fleet\configs\inventory.yaml`

```yaml
# Static DPU inventory for the dashctl-fleet (5 simulators).

dpus:
  - id: dpu-sim-01
    endpoint: dash-sim-1:50051
    labels: { rack: sim, slot: "1" }
  - id: dpu-sim-02
    endpoint: dash-sim-2:50051
    labels: { rack: sim, slot: "2" }
  - id: dpu-sim-03
    endpoint: dash-sim-3:50051
    labels: { rack: sim, slot: "3" }
  - id: dpu-sim-04
    endpoint: dash-sim-4:50051
    labels: { rack: sim, slot: "4" }
  - id: dpu-sim-05
    endpoint: dash-sim-5:50051
    labels: { rack: sim, slot: "5" }
```

### C.5 `deploy\dashctl-fleet\docker-compose.yml`

```yaml
name: dashctl-fleet

networks:
  dc-ctl-fleet:
    driver: bridge

volumes:
  dashd-state-ctl-fleet:
    name: dashd-state-ctl-fleet

services:
  # ── DPU simulators (five) ─────────────────────────────────────────
  dash-sim-1:
    build:
      context: ../..
      dockerfile: src/impl-go/dash-sim/Dockerfile
    image: dashcenter/dash-sim:dev
    container_name: dc-ctl-sim-1
    command: ["--grpc-listen", ":50051", "--admin-listen", ":8080", "--device-id", "dpu-sim-01"]
    networks: [dc-ctl-fleet]
    ports: ["8181:8080"]
    restart: unless-stopped

  dash-sim-2:
    build: { context: ../.., dockerfile: src/impl-go/dash-sim/Dockerfile }
    image: dashcenter/dash-sim:dev
    container_name: dc-ctl-sim-2
    command: ["--grpc-listen", ":50051", "--admin-listen", ":8080", "--device-id", "dpu-sim-02"]
    networks: [dc-ctl-fleet]
    ports: ["8182:8080"]
    restart: unless-stopped

  dash-sim-3:
    build: { context: ../.., dockerfile: src/impl-go/dash-sim/Dockerfile }
    image: dashcenter/dash-sim:dev
    container_name: dc-ctl-sim-3
    command: ["--grpc-listen", ":50051", "--admin-listen", ":8080", "--device-id", "dpu-sim-03"]
    networks: [dc-ctl-fleet]
    ports: ["8183:8080"]
    restart: unless-stopped

  dash-sim-4:
    build: { context: ../.., dockerfile: src/impl-go/dash-sim/Dockerfile }
    image: dashcenter/dash-sim:dev
    container_name: dc-ctl-sim-4
    command: ["--grpc-listen", ":50051", "--admin-listen", ":8080", "--device-id", "dpu-sim-04"]
    networks: [dc-ctl-fleet]
    ports: ["8184:8080"]
    restart: unless-stopped

  dash-sim-5:
    build: { context: ../.., dockerfile: src/impl-go/dash-sim/Dockerfile }
    image: dashcenter/dash-sim:dev
    container_name: dc-ctl-sim-5
    command: ["--grpc-listen", ":50051", "--admin-listen", ":8080", "--device-id", "dpu-sim-05"]
    networks: [dc-ctl-fleet]
    ports: ["8185:8080"]
    restart: unless-stopped

  # ── Volume-permissions init ───────────────────────────────────────
  # Pre-chowns /var/lib/dashd to nonroot UID 65532 so the distroless
  # dashd image can write to the named volume. Without this, the first
  # store.Put fails with EACCES — see §11.1.
  dashd-init:
    image: alpine:3.20
    container_name: dc-ctl-dashd-init
    command: ["sh", "-c", "chown -R 65532:65532 /var/lib/dashd"]
    volumes:
      - dashd-state-ctl-fleet:/var/lib/dashd
    restart: "no"

  # ── Control plane ─────────────────────────────────────────────────
  dashd:
    build:
      context: ../..
      dockerfile: src/impl-go/dashd/Dockerfile
    image: dashcenter/dashd:dev
    container_name: dc-ctl-dashd
    depends_on:
      dashd-init: { condition: service_completed_successfully }
      dash-sim-1: { condition: service_started }
      dash-sim-2: { condition: service_started }
      dash-sim-3: { condition: service_started }
      dash-sim-4: { condition: service_started }
      dash-sim-5: { condition: service_started }
    volumes:
      - ./configs/dashd.yaml:/etc/dashd/dashd.yaml:ro
      - ./configs/inventory.yaml:/etc/dashd/inventory.yaml:ro
      - dashd-state-ctl-fleet:/var/lib/dashd
    networks: [dc-ctl-fleet]
    ports:
      - "8443:8443"   # REST
      - "9443:9443"   # gRPC (Phase 2)
      - "7443:7443"   # Admin
    restart: unless-stopped

  # ── Operator CLI (one-shot, profile-gated) ────────────────────────
  dashctl:
    build:
      context: ../..
      dockerfile: src/impl-go/dashctl/Dockerfile
    image: dashcenter/dashctl:dev
    container_name: dc-ctl-dashctl
    profiles: [cli]
    networks: [dc-ctl-fleet]
    environment:
      DASHCTL_ENDPOINT:        http://dashd:8443
      DASHCTL_ADMIN_ENDPOINT:  http://dashd:7443
      DASHCTL_OUTPUT:          table
      DASHCTL_INSECURE:        "true"
    entrypoint: ["/usr/local/bin/dashctl"]
```

---

## Appendix D — Quick reference card

```
# ── fleet lifecycle ─────────────────────────────────────────────
docker compose -f deploy/dashctl-fleet/docker-compose.yml up -d --build
docker compose -f deploy/dashctl-fleet/docker-compose.yml down       # keep state
docker compose -f deploy/dashctl-fleet/docker-compose.yml down -v    # wipe state

# ── build host binary ───────────────────────────────────────────
cd src/impl-go/dashctl
make build              # -> bin/dashctl.exe
make test               # unit tests
make test-cover         # with per-package coverage
make build-all          # 5-platform release matrix

# ── env you typically want ──────────────────────────────────────
$env:DASHCTL_ENDPOINT       = "http://localhost:8443"
$env:DASHCTL_ADMIN_ENDPOINT = "http://localhost:7443"
$bin = "C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashctl\bin\dashctl.exe"

# ── daily ops ───────────────────────────────────────────────────
& $bin version
& $bin apply -f deploy/dashctl-fleet/manifests
& $bin get vnet  -o table
& $bin get eni   -o wide
& $bin describe eni eni-app-01
& $bin reconcile
& $bin dpu list
& $bin dpu drift --dpu dpu-sim-01
& $bin delete eni eni-db-03 --ignore-not-found
& $bin explain vnet
& $bin diff -f some.yaml
& $bin replace -f some.yaml
& $bin config set-context fleet --endpoint http://localhost:8443
& $bin config use-context  fleet
& $bin config view

# ── in-container one-shots ──────────────────────────────────────
docker compose -f deploy/dashctl-fleet/docker-compose.yml run --rm dashctl version
docker compose -f deploy/dashctl-fleet/docker-compose.yml run --rm dashctl dpu list -o table
```

---

# Experiment 2 — Provision VNets / ENIs / VnetMappings / ACLs / Routes, then play with a new ENI on a DPU

> **Goal of this experiment.** Take a brand-new DashCenter laptop, deploy
> the full fleet, push a realistic policy set across **every spec kind
> dashd ships in Phase 1** (VNets, ENIs, VnetMappings, AclPolicies,
> RoutePolicies), inspect each kind from multiple angles, then go
> off-script and **create a brand-new ENI on a specific DPU, attach an
> ACL policy to just that ENI, and verify dashd places it correctly**.
> Finish by repeating the same flow from inside a container, and tear
> the lab down cleanly.
>
> **Who this is for.** Anyone who has never touched the codebase before
> and wants a 30-minute, copy-paste-ready exploration. Every command
> below was executed live on **2026-06-10** against a fresh fleet on a
> Windows laptop and the output is **verbatim**.
>
> **What's new vs. Experiment 1 (§1-§11 above).** Experiment 1 covers
> the canonical 36-step `dashctl` walkthrough with VNets + ENIs only.
> Experiment 2 expands the scope to **all 5 Phase-1 spec kinds**, adds a
> custom **policy-attach playground** (Step E2.7), and explains the
> **end-to-end flow from `dashctl` → REST → dashd's store → dispatcher →
> dash-sim** so you understand what each command actually causes the
> control plane to do.

## E2.0 — Mental model: how a `dashctl apply` becomes DPU state

Before any commands, this is the data path you are about to drive
end-to-end:

```
   you, at the keyboard
        │
        │  PowerShell process
        ▼
   bin/dashctl.exe ────► HTTP PUT  /v1/<ns>/<plural>/<name>
                                                ▲
                                                │ TCP :8443 (REST)
                                                ▼
                                  ┌─────────────────────────────────┐
                                  │  dc-ctl-dashd  (control plane)  │
                                  │                                 │
                                  │  1. REST handler validates JSON │
                                  │  2. Service layer applies CAS   │
                                  │  3. File store writes           │
                                  │       /var/lib/dashd/<ns>/<kind>/<name>.json
                                  │  4. Reconciler enqueues diff    │
                                  │  5. Dispatcher fans out per-DPU │
                                  └────────────┬────────────────────┘
                                               │  gRPC ApplyBatch :50051
                                               │  on the dc-ctl-fleet bridge
                                               ▼
                              ┌──────────────────────────────────────┐
                              │ dc-ctl-sim-1 … dc-ctl-sim-5 (DPUs)  │
                              │                                      │
                              │  In-memory "observed state" updated  │
                              │  Drift = (desired − observed) = ∅    │
                              └──────────────────────────────────────┘
```

Key insight: **every spec you `apply` is durable in dashd's named volume
within milliseconds, but it does not affect a DPU until the reconciler
sees a non-empty diff and the dispatcher delivers it.** The
`dashctl reconcile` verb is what guarantees the next inspect command
sees a converged state.

## E2.1 — Deploy the fleet (one command, one minute)

```powershell
PS> cd C:\WorkSpace\PS\PublicRepo\DashCenter
PS> docker compose -f deploy/dashctl-fleet/docker-compose.yml up -d --build
```

What this does:

| Container | Role |
|---|---|
| `dc-ctl-sim-1` … `dc-ctl-sim-5` | Five **simulated DPUs**. Each speaks `dashapi.v1` on `:50051` (in-network) and an admin HTTP on `:8080` (host: `8181..8185`). |
| `dc-ctl-dashd-init` | One-shot `chown -R 65532:65532 /var/lib/dashd`. Required because dashd runs as nonroot but Docker creates volumes as root. Exits 0 then disappears from `docker ps`. |
| `dc-ctl-dashd` | The **control plane**. REST on `:8443`, gRPC on `:9443`, admin on `:7443`. |

Smoke check:

```powershell
PS> Start-Sleep 8
PS> docker ps --filter "name=dc-ctl-" --format "table {{.Names}}\t{{.Status}}"
NAMES          STATUS
dc-ctl-dashd   Up 20 seconds
dc-ctl-sim-2   Up 21 seconds
dc-ctl-sim-3   Up 21 seconds
dc-ctl-sim-1   Up 21 seconds
dc-ctl-sim-5   Up 21 seconds
dc-ctl-sim-4   Up 21 seconds

PS> curl.exe -s http://localhost:7443/admin/health
{"status":"ok","leader":true,"dpus":[{"id":"dpu-sim-01","state":"DPU_STATE_UP",...},...]}
```

`status: ok` + 5 DPUs in `DPU_STATE_UP` means the prober has tcp-dialed
every sim and they answered. You're ready for the next step.

## E2.2 — Set the operator shell

Run this **once** in your PowerShell session. Every command in this
experiment uses `$bin` and the two `DASHCTL_*` env vars.

```powershell
PS> $env:Path = "C:\Users\rashmirout\go-sdk\go\bin;C:\Users\rashmirout\go\bin;$env:Path"
PS> $env:DASHCTL_ENDPOINT       = "http://localhost:8443"
PS> $env:DASHCTL_ADMIN_ENDPOINT = "http://localhost:7443"
PS> $bin = "c:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashctl\bin\dashctl.exe"
PS> & $bin version
Client: dashctl 0.1.0-dev (commit 3c3d8277877c, built 2026-06-10T09:42:07Z)
Server: dashd  dashd (transport=rest endpoint=http://localhost:8443) leader=true
```

> **What `dashctl version` actually does.** Two calls in parallel:
> (1) returns the binary's stamped build info (no network), and
> (2) `GET http://localhost:7443/admin/health` to read dashd's reported
> name and leader status. If the second call fails, the client section
> still prints and `version` exits 0 (every other verb returns exit 7 =
> UNAVAILABLE on a dead server).

## E2.3 — Inspect the inventory (DPUs only)

```powershell
PS> & $bin dpu list -o table
ID           ENDPOINT   STATE          LAST_SEEN
dpu-sim-01              DPU_STATE_UP   2026-06-10T09:41:28Z
dpu-sim-02              DPU_STATE_UP   2026-06-10T09:41:28Z
dpu-sim-03              DPU_STATE_UP   2026-06-10T09:41:28Z
dpu-sim-04              DPU_STATE_UP   2026-06-10T09:41:28Z
dpu-sim-05              DPU_STATE_UP   2026-06-10T09:41:28Z
```

**What this command does.** `dpu list` calls `GET :7443/admin/inventory`
(not the REST API). The admin surface always returns
`DPU_STATE_*` as a string (B1-fix on the REST surface made the two
align). The `ENDPOINT` column is empty here only because the table
view uses the **admin** projection; the REST projection
(`dashctl inventory get -o table`) shows it.

## E2.4 — Apply the full Phase-1 policy set in one shot

The repo ships a complete manifest set at
`explore-with-docker/manifests/`. Five files, applied in lexicographic
order:

| File | Specs | Lines |
|---|---|---|
| `00-vnets.yaml` | 2 VNets (`vnet-app`, `vnet-db`) | 16 |
| `10-enis.yaml` | 5 ENIs, one per DPU | 60 |
| `20-vnet-mappings.yaml` | 4 overlay→underlay rewrites | 48 |
| `30-acl-policies.yaml` | 3 ACL policies (app-in, app-out, db-in) | 65 |
| `40-route-policies.yaml` | 2 route policies (one per tier) | 45 |

Apply them all at once:

```powershell
PS> & $bin -n default apply -f c:\WorkSpace\PS\PublicRepo\DashCenter\explore-with-docker\manifests
vnet/vnet-app apply in namespace default (generation 1)
vnet/vnet-db apply in namespace default (generation 1)
eni/eni-app-01 apply in namespace default (generation 1)
eni/eni-app-02 apply in namespace default (generation 1)
eni/eni-db-01 apply in namespace default (generation 1)
eni/eni-db-02 apply in namespace default (generation 1)
eni/eni-db-03 apply in namespace default (generation 1)
vnetmapping/map-app-10 apply in namespace default (generation 1)
vnetmapping/map-app-11 apply in namespace default (generation 1)
vnetmapping/map-db-20 apply in namespace default (generation 1)
vnetmapping/map-db-21 apply in namespace default (generation 1)
aclpolicy/acl-app-in apply in namespace default (generation 1)
aclpolicy/acl-app-out apply in namespace default (generation 1)
aclpolicy/acl-db-in apply in namespace default (generation 1)
routepolicy/routes-app apply in namespace default (generation 1)
routepolicy/routes-db apply in namespace default (generation 1)
```

**What just happened, step by step:**

1. `dashctl` walked the directory in lexicographic order
   (`00-…` → `40-…`), parsed each YAML as a multi-document stream, and
   wrapped each document in a kind-aware envelope.
2. For every envelope, dashctl PUT to
   `http://localhost:8443/v1/default/<plural>/<name>` —
   plural is derived from the kind registry
   (`Vnet` → `vnets`, `Eni` → `enis`, `VnetMapping` → `vnet-mappings`,
   `AclPolicy` → `acl-policies`, `RoutePolicy` → `route-policies`).
3. Dashd's REST handler validated each JSON body, called the in-process
   `service.ControlPlane.Put*` for that kind, which wrote the JSON to
   `/var/lib/dashd/default/<store_kind>/<name>.json` and bumped the
   generation.
4. The reconciler picked up the diff and the dispatcher fanned out
   gRPC `ApplyBatch` calls to every sim that should host any of those
   specs (placement-hint driven: every ENI has a
   `placement_hint_dpu_ids` list, and VnetMappings/ACLs/Routes follow
   their parent ENI's placement).

**Sanity check the counts:**

```powershell
PS> foreach ($k in @("vnet","eni","vnetmapping","aclpolicy","routepolicy")) {
>>   $n = (& $bin -n default get $k -o name | Measure-Object).Count
>>   Write-Host ("  {0,-12} = {1}" -f $k, $n)
>> }
  vnet         = 2
  eni          = 5
  vnetmapping  = 4
  aclpolicy    = 3
  routepolicy  = 2
```

**Trigger one reconcile so the inspect commands below see a converged
state:**

```powershell
PS> & $bin reconcile
Triggered reconcile on all DPUs.

PS> Start-Sleep 6
PS> foreach ($d in @("dpu-sim-01","dpu-sim-02","dpu-sim-03","dpu-sim-04","dpu-sim-05")) {
>>     Write-Host "  $d :" -NoNewline; & $bin dpu drift --dpu $d
>> }
  dpu-sim-01 :0 drift items.
  dpu-sim-02 :0 drift items.
  dpu-sim-03 :0 drift items.
  dpu-sim-04 :0 drift items.
  dpu-sim-05 :0 drift items.
```

**0 drift on every DPU** means every spec dashd thinks should be on a
DPU is in fact present in that DPU's observed state. From here on, you
can read the state from either side and the answers will match.

## E2.5 — Inspect the provisioned policy set (with explanations)

Each subsection shows a verb, what dashd actually does, and the live
output.

### E2.5.1 — VNets

```powershell
PS> & $bin -n default get vnet -o table
NAMESPACE   NAME       VNI    GENERATION   LABELS
default     vnet-app   1001   1            tier=app
default     vnet-db    1002   1            tier=db
```

`get vnet` → `GET /v1/default/vnets` → dashd reads
`/var/lib/dashd/default/vnet/*.json` and returns an `items` array.
The render layer projects to the `VnetColumns` table.

YAML view of one:

```powershell
PS> & $bin -n default get vnet vnet-app -o yaml
```

`get vnet vnet-app` → `GET /v1/default/vnets/vnet-app` (singular, no
list path) → same store file, single object.

Selector:

```powershell
PS> & $bin -n default get vnet -l tier=db -o name
vnet/vnet-db
```

Selectors today are **client-side**: dashctl pulls the full list, then
filters in-process. Server-side push-down is a Phase-2 enhancement
(see [specs/Impl-Plan/dashctl-impl-phases.md](../specs/Impl-Plan/dashctl-impl-phases.md) §"Honest open items").

### E2.5.2 — ENIs (the most interesting kind)

```powershell
PS> & $bin -n default get eni -o wide
NAMESPACE   NAME         VNET       MAC                 UNDERLAY    ADMIN   PLACED-ON    GEN
default     eni-app-01   vnet-app   00:11:22:00:00:01   10.0.5.11   up      dpu-sim-01   2
default     eni-app-02   vnet-app   00:11:22:00:00:02   10.0.5.12   up      dpu-sim-02   1
default     eni-db-01    vnet-db    00:11:22:00:00:03   10.0.6.11   up      dpu-sim-03   1
default     eni-db-02    vnet-db    00:11:22:00:00:04   10.0.6.12   up      dpu-sim-04   1
default     eni-db-03    vnet-db    00:11:22:00:00:05   10.0.6.13   up      dpu-sim-05   1
```

`-o wide` is the same `GET …/enis` but the table includes the wide-only
`PLACED-ON` column (read from the spec's `placement_hint_dpu_ids[0]`).
`eni-app-01` shows generation **2** because Experiment-1 mutated it
once; the rest are still at gen 1.

```powershell
PS> & $bin -n default describe eni eni-app-01
Name:        eni-app-01
Namespace:   default
Kind:        Eni
Generation:  2
Labels:      app=web,tier=app
Spec:
  admin_state: up
  mac_address: 00:11:22:00:00:01
  placement_hint_dpu_ids: [dpu-sim-01]
  underlay_ip: 10.0.5.11
  vnet_name: vnet-app
```

`describe` is just a human renderer over the same `GET` — it does not
issue extra calls and does not (yet) cross-reference attached ACLs or
routes (that's a Phase-2 enhancement; see Phase 3.A.7 in the tracker).

Label match across two dimensions:

```powershell
PS> & $bin -n default get eni -l tier=db -o name
eni/eni-db-01
eni/eni-db-02
eni/eni-db-03
```

### E2.5.3 — VnetMappings

```powershell
PS> & $bin -n default get vnetmapping -o table
NAMESPACE   NAME                  VNET       OVERLAY      UNDERLAY    ACTION       GEN
default     vnet-app-10.10.0.10   vnet-app   10.10.0.10   10.0.5.11   vnet_encap   2
default     vnet-app-10.10.0.11   vnet-app   10.10.0.11   10.0.5.12   vnet_encap   2
default     vnet-db-10.20.0.10    vnet-db    10.20.0.10   10.0.6.11   vnet_encap   2
default     vnet-db-10.20.0.11    vnet-db    10.20.0.11   10.0.6.12   vnet_encap   2
```

**Interesting detail.** The manifest authored the mappings with logical
names like `map-app-10`, but dashd's `VnetMappingService` keys them by
`<vnet_name>-<ip_address>` because the (vnet, overlay-IP) pair is the
true unique identity of a mapping. The `metadata.name` you wrote is
honoured for the apply request, but the **canonical store key** is the
synthesised one — that's what `get` returns. If you re-apply the same
overlay-IP under a different `metadata.name`, you'll bump the gen of
the existing row instead of creating a duplicate.

Aliases the kind registry accepts: `vnetmapping`, `vnet-mapping`,
`mapping`, `mappings`.

```powershell
PS> & $bin -n default get mapping -o name
vnet-mapping/vnet-app-10.10.0.10
vnet-mapping/vnet-app-10.10.0.11
vnet-mapping/vnet-db-10.20.0.10
vnet-mapping/vnet-db-10.20.0.11
```

### E2.5.4 — ACL policies

```powershell
PS> & $bin -n default get aclpolicy -o table
NAMESPACE   NAME          STAGE      ENIs                            RULES   GEN
default     acl-app-in    inbound    eni-app-01,eni-app-02           2       1
default     acl-app-out   outbound   eni-app-01,eni-app-02           2       1
default     acl-db-in     inbound    eni-db-01,eni-db-02,eni-db-03   2       1
```

The `ENIs` column is the `eni_names` list from the spec — i.e. **the set
of ENIs this policy is bound to**. dashd uses this list to compute, for
each DPU, the union of ACL policies that apply to any ENI hosted on
that DPU, and ships that union in the gRPC ApplyBatch.

Drill into one policy to see the rules:

```powershell
PS> & $bin -n default describe aclpolicy acl-app-in
Name:        acl-app-in
Namespace:   default
Kind:        AclPolicy
Generation:  1
Labels:      dir=in,tier=app
Spec:
  eni_names: [eni-app-01 eni-app-02]
  rules: [map[action:allow description:permit web from db tier dst_ports:[80 443] priority:100 protocols:[tcp] src_prefixes:[10.20.0.0/16]] map[action:deny description:default deny priority:200 src_prefixes:[0.0.0.0/0]]]
  stage: inbound
```

The render layer collapses the rule list to a single line (Phase-1
limitation, kubectl-describe-style multiline render is a Phase-3 item).
For a cleaner read, use `-o yaml`:

```powershell
PS> & $bin -n default get aclpolicy acl-app-in -o yaml
```

### E2.5.5 — Route policies

```powershell
PS> & $bin -n default get routepolicy -o table
NAMESPACE   NAME         ENIs                            ROUTES   GEN
default     routes-app   eni-app-01,eni-app-02           3        1
default     routes-db    eni-db-01,eni-db-02,eni-db-03   3        1

PS> & $bin -n default describe routepolicy routes-app
Name:        routes-app
Namespace:   default
Kind:        RoutePolicy
Generation:  1
Labels:      tier=app
Spec:
  eni_names: [eni-app-01 eni-app-02]
  routes: [map[metric:10 next_hop_target:vnet-app next_hop_type:vnet prefix:10.10.0.0/16] map[metric:20 next_hop_target:vnet-db next_hop_type:vnet prefix:10.20.0.0/16] map[metric:9999 next_hop_type:drop prefix:0.0.0.0/0]]
```

Routes follow the same binding model as ACLs: a `RoutePolicy.eni_names`
list pins the policy to a set of ENIs, and dashd ships the union to the
hosting DPU.

### E2.5.6 — The persistent store on disk (proof of durability)

```powershell
PS> docker run --rm -v dashd-state-ctl-fleet:/data alpine find /data -type f
/data/default/acl_policy/acl-app-out.json
/data/default/acl_policy/acl-db-in.json
/data/default/acl_policy/acl-app-in.json
/data/default/route_policy/routes-db.json
/data/default/route_policy/routes-app.json
/data/default/vnet/vnet-db.json
/data/default/vnet/vnet-app.json
/data/default/eni/eni-db-03.json
/data/default/eni/eni-app-02.json
/data/default/eni/eni-db-02.json
/data/default/eni/eni-app-01.json
/data/default/eni/eni-db-01.json
/data/default/vnet_mapping/vnet-app-10.10.0.11.json
/data/default/vnet_mapping/vnet-db-10.20.0.11.json
/data/default/vnet_mapping/vnet-db-10.20.0.10.json
/data/default/vnet_mapping/vnet-app-10.10.0.10.json
```

Every spec lives as one JSON file under
`/var/lib/dashd/<namespace>/<store_kind>/<name>.json`. The named volume
`dashd-state-ctl-fleet` survives `docker compose down` (without `-v`),
so the next `up` resumes from the same state.

### E2.5.7 — What the dispatcher actually sent

```powershell
PS> docker logs --tail 12 dc-ctl-dashd 2>&1
{"time":"...","level":"INFO","msg":"dispatch: reconcile complete","dpu":"dpu-sim-03"}
{"time":"...","level":"INFO","msg":"dispatch: reconcile complete","dpu":"dpu-sim-01"}
{"time":"...","level":"INFO","msg":"dispatch: reconcile complete","dpu":"dpu-sim-04"}
{"time":"...","level":"INFO","msg":"dispatch: reconcile","dpu":"dpu-sim-04","add":9,"update":0,"remove":0}
{"time":"...","level":"INFO","msg":"dispatch: reconcile complete","dpu":"dpu-sim-04"}
{"time":"...","level":"INFO","msg":"dispatch: reconcile","dpu":"dpu-sim-03","add":11,"update":0,"remove":0}
{"time":"...","level":"INFO","msg":"dispatch: reconcile complete","dpu":"dpu-sim-03"}
```

The `add: N` field tells you **how many specs that DPU received in the
last batch** (ENI + its mappings + its ACL + its routes). `update: 0,
remove: 0` means the batch was a pure add — first-time placement.

## E2.6 — Playground: create a brand-new ENI on a chosen DPU, attach an ACL, verify end-to-end

This is the "I want to actually feel the system respond" step. We will:

1. Create a **new ENI** called `eni-app-99`, **pinned to `dpu-sim-04`**,
   in `vnet-app`.
2. Create a **new ACL policy** called `acl-eni99-in` bound **only** to
   `eni-app-99`.
3. Apply both.
4. Verify dashd placed them on `dpu-sim-04` (and **only** `dpu-sim-04`)
   by reading the dispatcher counts and the per-DPU drift.
5. Then delete both so the fleet returns to the canonical state.

### E2.6.1 — Author the two manifests in-place

```powershell
PS> @"
apiVersion: dashcenter.v1
kind: Eni
metadata:
  name: eni-app-99
  namespace: default
  labels: { tier: app, app: web, owner: explore }
spec:
  vnet_name: vnet-app
  mac_address: "00:11:22:00:00:99"
  underlay_ip: "10.0.5.99"
  admin_state: "up"
  placement_hint_dpu_ids: ["dpu-sim-04"]
"@ | Set-Content -Path C:\Temp\new-eni.yaml -Encoding ascii

PS> @"
apiVersion: dashcenter.v1
kind: AclPolicy
metadata:
  name: acl-eni99-in
  namespace: default
  labels: { tier: app, owner: explore }
spec:
  stage: "inbound"
  eni_names: ["eni-app-99"]
  rules:
    - priority: 100
      action: "allow"
      src_prefixes: ["10.10.0.0/16"]
      dst_ports:    ["8080"]
      protocols:    ["tcp"]
      description:  "permit 8080 from app overlays"
    - priority: 200
      action: "deny"
      src_prefixes: ["0.0.0.0/0"]
      description:  "default deny"
"@ | Set-Content -Path C:\Temp\new-acl.yaml -Encoding ascii
```

**Why label everything `owner=explore`?** So you can later select and
delete just the experimental objects with `-l owner=explore` without
touching the canonical fleet.

### E2.6.2 — Apply

```powershell
PS> & $bin -n default apply -f C:\Temp\new-eni.yaml
eni/eni-app-99 apply in namespace default (generation 1)

PS> & $bin -n default apply -f C:\Temp\new-acl.yaml
aclpolicy/acl-eni99-in apply in namespace default (generation 1)
```

**Flow:**

1. dashctl PUT `/v1/default/enis/eni-app-99` with the YAML→JSON envelope.
2. dashd's `ControlPlane.PutEni` validated, wrote
   `/var/lib/dashd/default/eni/eni-app-99.json`, generation 1.
3. Reconciler saw a new ENI bound to `dpu-sim-04` → dispatcher queued an
   ApplyBatch for sim-4 only.
4. Same flow for the ACL: `/v1/default/acl-policies/acl-eni99-in` →
   `/var/lib/dashd/default/acl_policy/acl-eni99-in.json` → because its
   `eni_names = [eni-app-99]` and that ENI is placed on `dpu-sim-04`,
   the ACL goes to **sim-4 only**.

### E2.6.3 — Verify placement and attachment

```powershell
PS> & $bin -n default get eni -o wide
NAMESPACE   NAME         VNET       MAC                 UNDERLAY    ADMIN   PLACED-ON    GEN
default     eni-app-01   vnet-app   00:11:22:00:00:01   10.0.5.11   up      dpu-sim-01   2
default     eni-app-02   vnet-app   00:11:22:00:00:02   10.0.5.12   up      dpu-sim-02   1
default     eni-app-99   vnet-app   00:11:22:00:00:99   10.0.5.99   up      dpu-sim-04   1
default     eni-db-01    vnet-db    00:11:22:00:00:03   10.0.6.11   up      dpu-sim-03   1
default     eni-db-02    vnet-db    00:11:22:00:00:04   10.0.6.12   up      dpu-sim-04   1
default     eni-db-03    vnet-db    00:11:22:00:00:05   10.0.6.13   up      dpu-sim-05   1
```

`eni-app-99` is in the table, pinned to `dpu-sim-04` (which already
hosts `eni-db-02`, so sim-4 now hosts 2 ENIs).

```powershell
PS> & $bin -n default get acl -l owner=explore -o table
NAMESPACE   NAME           STAGE     ENIs         RULES   GEN
default     acl-eni99-in   inbound   eni-app-99   2       1
```

Label-selector confirms only **one** policy carries the `owner=explore`
label — the one we just created. Its `ENIs` column proves it is bound
solely to `eni-app-99`.

```powershell
PS> & $bin -n default describe eni eni-app-99
Name:        eni-app-99
Namespace:   default
Kind:        Eni
Generation:  1
Labels:      app=web,owner=explore,tier=app
Spec:
  admin_state: up
  mac_address: 00:11:22:00:00:99
  placement_hint_dpu_ids: [dpu-sim-04]
  underlay_ip: 10.0.5.99
  vnet_name: vnet-app
```

### E2.6.4 — Reconcile and verify dashd actually shipped them

```powershell
PS> & $bin reconcile
Triggered reconcile on all DPUs.

PS> Start-Sleep 6
PS> foreach ($d in @("dpu-sim-01","dpu-sim-02","dpu-sim-03","dpu-sim-04","dpu-sim-05")) {
>>     Write-Host "  $d :" -NoNewline; & $bin dpu drift --dpu $d
>> }
  dpu-sim-01 :0 drift items.
  dpu-sim-02 :0 drift items.
  dpu-sim-03 :0 drift items.
  dpu-sim-04 :0 drift items.
  dpu-sim-05 :0 drift items.
```

The dispatcher log proves where the new specs went:

```powershell
PS> docker logs --tail 4 dc-ctl-dashd 2>&1
{"time":"...","level":"INFO","msg":"dispatch: reconcile","dpu":"dpu-sim-04","add":4,"update":0,"remove":0}
{"time":"...","level":"INFO","msg":"dispatch: reconcile complete","dpu":"dpu-sim-04"}
{"time":"...","level":"INFO","msg":"dispatch: reconcile","dpu":"dpu-sim-04","add":3,"update":0,"remove":0}
{"time":"...","level":"INFO","msg":"dispatch: reconcile complete","dpu":"dpu-sim-04"}
```

**Only `dpu-sim-04` got a non-zero `add` count.** The other four sims
saw a no-op reconcile (no new specs apply to them). This is the
placement-hint working: dashd is fan-out-aware, not broadcast.

### E2.6.5 — Clean up just the experimental objects

```powershell
PS> & $bin -n default delete aclpolicy acl-eni99-in
acl_policy/acl-eni99-in deleted

PS> & $bin -n default delete eni eni-app-99
eni/eni-app-99 deleted
```

Verify:

```powershell
PS> & $bin -n default get eni -o name
eni/eni-app-01
eni/eni-app-02
eni/eni-db-01
eni/eni-db-02
eni/eni-db-03

PS> & $bin -n default get aclpolicy -o name
acl_policy/acl-app-in
acl_policy/acl-app-out
acl_policy/acl-db-in
```

Both gone. The fleet is back to the canonical 16-object state. A
follow-up `reconcile` would log a `remove: N` line on `dpu-sim-04` —
that's the dispatcher pushing the deletion down so the DPU's observed
state catches up.

> **Pro tip.** Because both experimental objects share `owner=explore`,
> you can wipe them in one shot once the **Phase 2 `--selector` flag**
> on `delete` lands. For now, delete by name as above.

## E2.7 — Do the whole thing from inside a container

The host-side binary is the easiest UX, but for CI pipelines, ops
toolchains, or "I don't want to install Go on this laptop" scenarios,
the containerised `dashctl` is the right answer.

The `dashctl` service in the compose file is **profile-gated**
(`profiles: [cli]`), so `compose up` does NOT start it. You invoke it
on demand:

### E2.7.1 — Container `version`

```powershell
PS> docker compose -f deploy/dashctl-fleet/docker-compose.yml run --rm dashctl version
Client: dashctl 0.1.0-dev (commit none, built unknown)
Server: dashd  dashd (transport=rest endpoint=http://dashd:8443) leader=true
```

Three things to notice:

1. **Server endpoint is `http://dashd:8443`** (not `localhost`) — the
   container reaches dashd over the Docker bridge by its compose name.
2. **No `--insecure` flag was passed.** dashctl normally refuses
   plaintext HTTP to non-localhost targets, but the compose file sets
   `DASHCTL_INSECURE: "true"` so the in-net call goes through. (This is
   the B5 fix.)
3. **`commit none, built unknown`** because the image was built without
   the `make build` ldflags. The host binary stamps them; the
   distroless image does not.

### E2.7.2 — Container `get` for each kind

```powershell
PS> docker compose -f deploy/dashctl-fleet/docker-compose.yml run --rm dashctl get eni -o table
NAMESPACE   NAME         VNET       MAC                 UNDERLAY    ADMIN   GEN
default     eni-app-01   vnet-app   00:11:22:00:00:01   10.0.5.11   up      2
default     eni-app-02   vnet-app   00:11:22:00:00:02   10.0.5.12   up      1
default     eni-db-01    vnet-db    00:11:22:00:00:03   10.0.6.11   up      1
default     eni-db-02    vnet-db    00:11:22:00:00:04   10.0.6.12   up      1
default     eni-db-03    vnet-db    00:11:22:00:00:05   10.0.6.13   up      1

PS> docker compose -f deploy/dashctl-fleet/docker-compose.yml run --rm dashctl get aclpolicy -o table
NAMESPACE   NAME           STAGE      ENIs                            RULES   GEN
default     acl-app-in     inbound    eni-app-01,eni-app-02           2       1
default     acl-app-out    outbound   eni-app-01,eni-app-02           2       1
default     acl-db-in      inbound    eni-db-01,eni-db-02,eni-db-03   2       1
```

These are full round trips through the Docker bridge:
`dc-ctl-dashctl` → `dc-ctl-dashd:8443` → store → response.

### E2.7.3 — Container apply with a bind-mount

Mount the manifest directory into the container and `apply` it just
like you would on the host:

```powershell
PS> docker compose -f deploy/dashctl-fleet/docker-compose.yml run --rm `
>>   -v "${PWD}/explore-with-docker/manifests:/work:ro" `
>>   --entrypoint /usr/local/bin/dashctl `
>>   dashctl -n default apply -f /work
```

(Bumps the generation by 1 on every spec — dashd has no
`equals`-guard, so a no-op-content apply still increments gen.)

> **Why `--entrypoint`?** The compose `dashctl` service already sets
> `entrypoint: ["/usr/local/bin/dashctl"]` and the default `command:`
> from the image. When you pass `-v` and `apply -f /work`, you need to
> re-state the entrypoint so the args land in the right slot of the
> docker CLI.

## E2.8 — Tear down cleanly

When you're done exploring:

```powershell
# Stop the containers but keep the store (next 'up' resumes from current state)
PS> docker compose -f deploy/dashctl-fleet/docker-compose.yml down

# Stop AND wipe the named volume for a true clean slate next time
PS> docker compose -f deploy/dashctl-fleet/docker-compose.yml down -v
```

What happens behind the scenes:

| Action | Effect on dashd | Effect on store |
|---|---|---|
| `docker compose down`  | All 6 containers stopped + removed; bridge network removed | Named volume `dashd-state-ctl-fleet` survives |
| `docker compose down -v` | Same as above | Named volume **deleted**; next `up -d --build` starts from empty state and re-runs `dc-ctl-dashd-init` to pre-chown the new volume |

A useful intermediate cleanup if you only want to drop the experimental
ENI/ACL but keep the canonical 16 specs is to delete them by name
(§ E2.6.5) and skip the `compose down -v`.

## E2.9 — Quick troubleshooting (Experiment-2 specific)

| Symptom | Likely cause | Fix |
|---|---|---|
| `apply` returns `internal` 500 on a fresh volume | Volume created as root, dashd runs as UID 65532 | Should never happen — `dc-ctl-dashd-init` runs `chown` first. If it didn't, see § 11.1 above. |
| `get vnetmapping` shows synthesized names (`vnet-app-10.10.0.10`) instead of `map-app-10` | **By design** — VnetMapping store key is `<vnet>-<overlayip>`, not `metadata.name` | None — read § E2.5.3 |
| `dpu drift --dpu …` returns a non-zero count after `apply` | Reconcile didn't run yet | `& $bin reconcile; Start-Sleep 6` |
| `docker compose run --rm dashctl …` errors `plaintext HTTP refused` | `DASHCTL_INSECURE` env not picked up | Make sure you're using the dashctl-fleet compose — it sets the env var. Or pass `-e DASHCTL_INSECURE=true` to the run command. |
| `delete eni X` returns `not found` even though the table showed it | You're in a different namespace context | Always pass `-n default` if your `dashctl config view` shows a different `namespace:` field |
| Volume contents listing is empty after `up -d --build` | Init container ran *after* dashd or didn't run | `docker ps -a --filter "name=dc-ctl-dashd-init"` should show `Exited (0)`. If it shows `Created` only, your compose file is missing the `depends_on: dashd-init: { condition: service_completed_successfully }` clause. |

## E2.10 — What you learned

By the end of Experiment 2 you have:

- Brought up a 6-container DashCenter fleet from scratch.
- Pushed **every Phase-1 spec kind** (Vnet, Eni, VnetMapping,
  AclPolicy, RoutePolicy) through dashctl in one `apply` call.
- Confirmed every spec landed durably in dashd's named-volume store.
- Reconciled the cluster and confirmed all 5 DPUs report **0 drift**.
- Inspected each kind with `get`/`describe`/`-o yaml`/`-o name`/`-l`
  selectors, and seen the kind-aware column projections.
- **Created a brand-new ENI** on a specific DPU, **attached a
  brand-new ACL policy** to just that ENI, watched the dispatcher
  ship **only** to the targeted DPU, then surgically cleaned it back
  out.
- Repeated the inspect step from inside a container, proving the same
  binary works in CI pipelines and air-gapped operator workstations.
- Torn the lab down cleanly with both "keep state" and "wipe state"
  options.

You can now point to any line of [src/impl-go/dashd/](../src/impl-go/dashd/) or
[src/impl-go/dashctl/](../src/impl-go/dashctl/) and have a concrete mental model
of what request, what file, what response, what dispatch the code is
producing.

---

> **Hit a step that doesn't work for you?**
> Capture the exact command, the verbatim output, your OS version, your
> Go version, and your Docker Desktop version, then open an issue
> referencing this manual + the step number. The maintainers use this
> document as the authoritative reproduction recipe.
