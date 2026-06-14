# dashw — DashCenter Web Console (BFF)

The **Backend-for-Frontend** for the DashCenter Web Console. A single
Go binary that:

- Serves the React SPA via `go:embed`
- Reverse-proxies dashd REST (`:8443`) and Admin (`:7443`) APIs
- Aggregates multi-endpoint data into view models
- Bridges gRPC streams → WebSocket for real-time data
- Provides in-process TTL cache, circuit breaker, rate limiter

## Quick Start

```bash
# Build the binary (SPA must be built first — see below)
make build

# Run locally (assumes dashd at localhost:8443/7443/9443)
./../../bin/dashw

# Or with custom targets:
./../../bin/dashw --listen :3000 \
--dashd-rest http://dashd-1:8443 \
--dashd-admin http://dashd-1:7443 \
--dashd-grpc dashd-1:9443 \
--cors
```

Open `http://localhost:8080` (or `:3000` if overridden).

## Build

```bash
# Go BFF only (uses placeholder SPA if web/dist/ is empty)
make bff-build

# Full build: SPA + Go BFF
make full-build

# Docker image (multi-stage: node → go → distroless)
make docker

# Run tests with coverage
make test-cover
```

## Project Structure

```
src/impl-go/console/
├── cmd/dashw/main.go              # Entry point
├── internal/
│   ├── config/                    # CLI flags + env config
│   │   ├── config.go
│   │   └── config_test.go
│   ├── server/                    # HTTP server, router, middleware
│   │   ├── server.go             # Server lifecycle (New, Run, shutdown)
│   │   ├── router.go             # Chi router with all routes
│   │   ├── middleware.go          # RequestID, RealIP, logging, recovery
│   │   ├── spa.go                # SPA fallback handler
│   │   └── server_test.go        # Integration tests
│   ├── health/                    # /healthz + /readyz handlers
│   │   ├── health.go
│   │   └── health_test.go
│   ├── proxy/                     # Reverse proxies (Phase A2)
│   ├── aggregation/               # Aggregation endpoints (Phase A3)
│   ├── cache/                     # In-process TTL cache (Phase A1b)
│   └── resilience/                # Circuit breaker + rate limiter (Phase A1b)
├── web/
│   ├── embed.go                   # go:embed dist/* declaration
│   └── dist/                      # SPA build output (gitignored except .gitkeep)
├── Dockerfile                     # Multi-stage: node → go → distroless
├── Makefile                       # Build, test, lint, Docker targets
└── go.mod
```

## Configuration

| Flag | Env Var | Default | Description |
|---|---|---|---|
| `--listen` | `DASHW_LISTEN` | `:8080` | HTTP listen address |
| `--dashd-rest` | `DASHD_REST_ADDR` | `http://localhost:8443` | dashd REST |
| `--dashd-grpc` | `DASHD_GRPC_ADDR` | `localhost:9443` | dashd gRPC |
| `--dashd-admin` | `DASHD_ADMIN_ADDR` | `http://localhost:7443` | dashd Admin |
| `--cors` | `DASHW_CORS` | `false` | Enable CORS |
| `--metrics` | `DASHW_METRICS` | `false` | Enable /metrics |
| `--log-level` | `DASHW_LOG_LEVEL` | `info` | Log level |

See `internal/config/config.go` for all options.

## Endpoints

| Method | Path | Handler | Phase |
|---|---|---|---|
| GET | `/healthz` | Liveness probe | A |
| GET | `/readyz` | Readiness probe (dashd check) | A |
| ALL | `/api/v1/*` | REST proxy → dashd :8443 | A2 |
| ALL | `/api/admin/*` | Admin proxy → dashd :7443 | A2 |
| GET | `/api/console/fleet/summary` | Aggregation | A3 |
| GET | `/api/console/dpu/{id}/detail` | Aggregation | A3 |
| GET | `/api/console/topology` | Aggregation | A3 |
| GET | `/api/console/vnet/{name}/detail` | Aggregation | A3 |
| GET | `/api/console/vnet/{name}/canvas` | Aggregation | A3 |
| GET | `/api/console/stats/capacity` | Aggregation | A3 |
| WS | `/ws/dpu-status` | gRPC bridge | B |
| WS | `/ws/events` | gRPC bridge | B |
| GET | `/*` | SPA fallback | A |

## Specs

- [HLD](../../../specs/HLD/dashw-web-hld.md)
- [LLD](../../../specs/LLD/dashw-web-lld.md)
- [Implementation Plan](../../../specs/Impl-Plan/dashw-web-impl-plan.md)
- [Vision](../../../specs/HLD/dashw-web-vision.md)
- [Scale Analysis](../../../specs/HLD/dashw-web-scale-design-req-analysis.md)