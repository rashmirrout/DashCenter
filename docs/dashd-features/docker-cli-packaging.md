# Docker CLI Packaging — bundled operator CLIs in every container

> **Audience**: operators running DashCenter fleets in Docker / Docker
> Compose / Kubernetes; CI pipeline authors; SREs debugging live
> containers.
> **Scope**: the Dockerfile changes that ship `dash-sim-client` inside
> `dash-sim` containers and `dashctl` inside `dashd` containers, plus
> the switch from `distroless` to Alpine for shell access.
> **Companion docs**:
> [CLI_GUIDE.md §15](../CLI_GUIDE.md) (quick reference + in-container
> examples),
> [features.md §11](features.md) (Docker packaging table),
> [counter-streaming.md §7](counter-streaming.md) (operator UX),
> [05-full-console README](../../deploy/test-setup/05-full-console/README.md)
> + [manual-handson Lab 13](../../deploy/test-setup/05-full-console/manual-handson.md),
> [06-fleet-ui-diagnostics README](../../deploy/test-setup/06-fleet-ui-diagnostics/README.md)
> + [manual-handson Lab 14](../../deploy/test-setup/06-fleet-ui-diagnostics/manual-handson.md).
> **Status**: ✅ Complete — 2026-06-14.

---

## Table of contents

1. [Problem statement](#1-problem-statement)
2. [Goals & non-goals](#2-goals--non-goals)
3. [Architecture](#3-architecture)
4. [Implementation](#4-implementation)
5. [Operator UX](#5-operator-ux)
6. [Test verification](#6-test-verification)
7. [Future Scopes](#7-future-scopes)

---

## 1. Problem statement

Before this change, every Docker image used `gcr.io/distroless/static-
debian12:nonroot` as the runtime base. This is excellent for production
security posture (no shell, no package manager, minimal attack surface)
but hostile for operator debugging:

1. **No shell access.** `docker exec -it <container> sh` fails with
   `exec: "sh": executable file not found`. Operators can't inspect
   the container's state, check network connectivity, or run ad-hoc
   commands.

2. **No operator CLI.** `dash-sim-client` is not shipped inside the
   `dash-sim` image; `dashctl` is not shipped inside the `dashd` image.
   Operators who want to run diagnostics directly against a container's
   localhost endpoint (bypassing the network for faster debugging) must
   build and copy the binary themselves.

3. **CI friction.** Integration tests that need to run CLI commands
   inside a container (e.g., `docker exec dashd-1 dashctl counters`)
   require a multi-stage copy dance or a sidecar container.

Real-world scenario that triggered this work:

> "I want to `docker exec` into `dc-console-sim-01` and run
> `dash-sim-client reset-counters --target localhost:50051` to zero
> the accumulators directly, without going through dashd. I can't —
> the image has no shell and no CLI binary."

---

## 2. Goals & non-goals

### 2.1 Goals

- **G1**: Ship `dash-sim-client` alongside `dash-sim` in every
  `dashcenter/dash-sim:*` image.
- **G2**: Ship `dashctl` alongside `dashd` in every
  `dashcenter/dashd:*` image.
- **G3**: Provide a shell (`sh`) so operators can `docker exec -it
  <container> sh` for interactive debugging.
- **G4**: Minimal image size impact (< 10 MB delta).
- **G5**: Document the capability in every applicable guide, lab,
  and manual.

### 2.2 Non-goals

- **NG1**: We do NOT add debugging tools beyond what Alpine provides
  by default (`sh`, `ls`, `cat`, `wget`, `env`). Operators who need
  `tcpdump`, `strace`, etc. use ephemeral debug containers.
- **NG2**: We do NOT change the dashw (console) Dockerfile — dashw
  already runs Node and has a full shell.
- **NG3**: We do NOT change the entrypoint or default command of
  either image. The server binary is still the entrypoint; the CLI
  is invoked only via `docker exec`.

---

## 3. Architecture

### 3.1 Before

```
┌─────────────────────────────────────┐
│ dashcenter/dash-sim:dev             │
│ Base: distroless (no shell)         │
│ /usr/local/bin/dash-sim             │
│ (no CLI)                            │
└─────────────────────────────────────┘

┌─────────────────────────────────────┐
│ dashcenter/dashd:dev                │
│ Base: distroless (no shell)         │
│ /usr/local/bin/dashd                │
│ (no CLI)                            │
└─────────────────────────────────────┘
```

### 3.2 After

```
┌─────────────────────────────────────┐
│ dashcenter/dash-sim:dev             │
│ Base: alpine:3.20 (sh + coreutils) │
│ /usr/local/bin/dash-sim             │
│ /usr/local/bin/dash-sim-client  ←── │
│ User: nobody:nobody                 │
└─────────────────────────────────────┘

┌─────────────────────────────────────┐
│ dashcenter/dashd:dev                │
│ Base: alpine:3.20 (sh + coreutils) │
│ /usr/local/bin/dashd                │
│ /usr/local/bin/dashctl          ←── │
│ User: nobody:nobody                 │
└─────────────────────────────────────┘
```

### 3.3 Image size impact

| Image | Before (distroless) | After (Alpine + 2 binaries) | Delta |
|---|---|---|---|
| `dash-sim` | ~12 MB | ~20 MB | +8 MB |
| `dashd` | ~16 MB | ~24 MB | +8 MB |

The delta comes from Alpine base (~5 MB) + the second Go binary
(~3-5 MB each, statically linked with `-trimpath -ldflags="-s -w"`).

---

## 4. Implementation

### 4.1 `src/impl-go/dash-sim/Dockerfile`

```dockerfile
FROM golang:1.22-alpine AS build
WORKDIR /workspace
COPY . .
WORKDIR /workspace/src/impl-go
RUN go build -trimpath -ldflags="-s -w" -o /out/dash-sim        ./dash-sim/cmd/dash-sim
RUN go build -trimpath -ldflags="-s -w" -o /out/dash-sim-client ./dash-sim-client/cmd/dash-sim-client

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/dash-sim        /usr/local/bin/dash-sim
COPY --from=build /out/dash-sim-client /usr/local/bin/dash-sim-client
EXPOSE 50051 8080
USER nobody:nobody
ENTRYPOINT ["/usr/local/bin/dash-sim"]
```

Changes from the previous Dockerfile:
- **Added**: second `go build` line for `dash-sim-client`.
- **Changed**: runtime base from `gcr.io/distroless/static-debian12:nonroot` to `alpine:3.20`.
- **Changed**: user from `nonroot:nonroot` to `nobody:nobody` (Alpine's equivalent).
- **Added**: `ca-certificates` package (distroless includes certs by default; Alpine doesn't).

### 4.2 `src/impl-go/dashd/Dockerfile`

```dockerfile
FROM golang:1.22-alpine AS build
WORKDIR /workspace
COPY . .
WORKDIR /workspace/src/impl-go
RUN go build -trimpath -ldflags="-s -w" -o /out/dashd   ./dashd/cmd/dashd
RUN go build -trimpath -ldflags="-s -w" -o /out/dashctl ./dashctl/cmd/dashctl

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/dashd   /usr/local/bin/dashd
COPY --from=build /out/dashctl /usr/local/bin/dashctl
EXPOSE 8443 9443 7443
USER nobody:nobody
ENTRYPOINT ["/usr/local/bin/dashd"]
CMD ["--config", "/etc/dashd/dashd.yaml"]
```

Same pattern as dash-sim: second binary + Alpine base.

---

## 5. Operator UX

### 5.1 dash-sim-client inside sim containers

```bash
# Enter a sim container (interactive shell):
docker exec -it dc-console-sim-01 sh

# All commands work against localhost:50051:
dash-sim-client ping --target localhost:50051
dash-sim-client dpu-counters --target localhost:50051 -o table
dash-sim-client dpu-counters --include-enis --target localhost:50051
dash-sim-client reset-counters --target localhost:50051
dash-sim-client kinds --target localhost:50051 -o table

# One-liner (no shell entry needed):
docker exec dc-console-sim-01 dash-sim-client reset-counters --target localhost:50051
```

### 5.2 dashctl inside dashd containers

```bash
# Enter a dashd container:
docker exec -it dc-console-dashd-1 sh

# All commands work against localhost:8443:
dashctl version --endpoint http://localhost:8443 --insecure
dashctl counters --endpoint http://localhost:8443 --insecure
dashctl counters details --dpu=dpu-sim-01 --endpoint http://localhost:8443 --insecure
dashctl counters clear --reset-sim --endpoint http://localhost:8443 --insecure
dashctl topology --endpoint http://localhost:8443 --insecure

# One-liner:
docker exec dc-console-dashd-1 dashctl counters --endpoint http://localhost:8443 --insecure
```

### 5.3 When to use which path

| Scenario | Use |
|---|---|
| Host machine can reach dashd REST port (28443) | `dashctl` from the host |
| Host network can't reach dashd (firewalled / Docker-only network) | `docker exec <dashd> dashctl --endpoint http://localhost:8443 --insecure` |
| Direct sim debugging (bypass dashd) | `docker exec <sim> dash-sim-client --target localhost:50051` |
| Browser-based clear | SPA Clear button (calls `DELETE ?reset_sim=true` through dashw proxy) |

---

## 6. Test verification

Verified live on the 05-full-console fleet after rebuild + deploy:

```
$ docker exec dc-console-sim-01 sh -c "ls /usr/local/bin/"
dash-sim
dash-sim-client

$ docker exec dc-console-sim-01 dash-sim-client ping --target localhost:50051
ok: target=localhost:50051 vnets=3

$ docker exec dc-console-sim-01 dash-sim-client reset-counters --target localhost:50051
{ "keys_reset": 69 }

$ docker exec dc-console-dashd-1 sh -c "ls /usr/local/bin/"
dashctl
dashd

$ docker exec dc-console-dashd-1 dashctl version --endpoint http://localhost:8443 --insecure
Client: dashctl 0.1.0-dev (commit none, built unknown)
Server: dashd  dashd (transport=rest endpoint=http://localhost:8443) leader=false

$ docker exec dc-console-dashd-1 dashctl counters --endpoint http://localhost:8443 --insecure
DPU         SAMPLED                          DECAP   ENCAP   DROP_IN ...
dpu-sim-01  2026-06-14T17:11:42.282Z         3714    3732    30      ...
dpu-sim-02  2026-06-14T17:11:42.290Z         4023528 ...
```

---

## 7. Future Scopes

### 7.1 Debug sidecar with extra tools

- **Trigger**: operator needs `tcpdump`, `strace`, `dig` inside a container.
- **Proposal**: publish a `dashcenter/debug:dev` image with networking + tracing tools. Use `kubectl debug` (or `docker run --pid=container:...`) to attach it as a sidecar.
- **Why not now**: Alpine's built-in `wget`, `cat`, `ls`, `env` cover 90% of debugging; adding tcpdump etc. to every container bloats the attack surface.

### 7.2 dashctl auto-detect localhost endpoint

- **Trigger**: typing `--endpoint http://localhost:8443 --insecure` inside the container is verbose.
- **Proposal**: dashctl detects it's running inside the dashd container (via `$DASHD_INSIDE_CONTAINER=1` env var set in the Dockerfile) and defaults to `http://localhost:8443 --insecure` when no `--endpoint` is given and no saved context exists.
- **Backward-compat**: only applies when the env var is set; external dashctl behavior unchanged.

### 7.3 Ship dashctl inside dashw too

- **Trigger**: debugging dashw → dashd connectivity from inside the BFF container.
- **Proposal**: add `dashctl` to the dashw Dockerfile. Lower priority since dashw already has Node + npm and operators typically debug at the dashd or sim level.

---

> **Change Log** — appended as follow-ups land.
>
> | Date | Change |
> |---|---|
> | 2026-06-14 | Initial: Alpine base + bundled CLIs for dash-sim + dashd |
