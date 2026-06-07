# dashd — Phase 1 (Basic) Implementation Plan

> **Status**: Authoritative implementation guide for Phase 1.
> **Audience**: Any engineer or AI agent implementing dashd from scratch.
> **Ground truth**: derived from [`specs/LLD/dashd.md`](../LLD/dashd.md)
> and [`proto/dashcenter/v1/`](../../proto/dashcenter/v1/). If this plan
> conflicts with either, those documents win — file a PR to fix this plan.

## Table of contents

1. [Overview & exit criteria](#1-overview--exit-criteria)
2. [Repository context](#2-repository-context-what-exists-what-to-reuse-what-to-retire)
3. [Phase 1 architecture & data flow](#3-phase-1-architecture--data-flow)
4. [Package dependency graph](#4-package-dependency-graph)
5. [Step 0 — Scaffold cleanup](#5-step-0--scaffold-cleanup)
6. [Step 1 — `internal/config/`](#6-step-1--internalconfig)
7. [Step 2 — `internal/store/`](#7-step-2--internalstore)
8. [Step 3 — `internal/inventory/`](#8-step-3--internalinventory)
9. [Step 4 — `internal/model/`](#9-step-4--internalmodel)
10. [Step 5 — `internal/placement/`](#10-step-5--internalplacement)
11. [Step 6 — `internal/subscribe/`](#11-step-6--internalsubscribe)
12. [Step 7 — `internal/dispatch/`](#12-step-7--internaldispatch)
13. [Step 8 — `internal/reconciler/`](#13-step-8--internalreconciler)
14. [Step 9 — `internal/server/grpc/`](#14-step-9--internalservergrpc)
15. [Step 10 — `internal/server/rest/`](#15-step-10--internalserverrest)
16. [Step 11 — `internal/server/admin/`](#16-step-11--internalserveradmin)
17. [Step 12 — `cmd/dashd/main.go`](#17-step-12--cmddashdmaingo)
18. [Complete file tree](#18-complete-file-tree-phase-1)
19. [Go module dependencies](#19-go-module-dependencies)
20. [Quality gates](#20-quality-gates)
21. [Implementation order & checkpoints](#21-implementation-order--checkpoints)

---

## 1. Overview & exit criteria

### What Phase 1 delivers

A single-node `dashd` binary that:
* Accepts the **northbound `dashcenter.v1` API** over gRPC (`:9443`) and REST (`:8443`)
* **Persists desired state to disk** (JSON-on-disk store; survives restarts)
* **Discovers DPUs** from inventory YAML, dials each via `dashapi.v1`
* **Probes liveness** of each DPU on a periodic interval
* **Normalizes** `dashcenter.v1` specs → per-DPU `dashapi.v1.Object`s via a pure placement function
* **Reconciles** declared state → observed state with one goroutine per DPU
* Exposes an **admin HTTP surface** (`:7443`) for health, drift, force-reconcile
* Emits structured JSON logs via `log/slog`

### What Phase 1 explicitly does NOT deliver

* No etcd backend (file only) · no multi-node HA / leader election (single process is always leader)
* No multi-tenancy *enforcement* (namespace field exists on every key from day one, defaulting to `"default"`, but cross-namespace validation and RBAC binding are Phase 2)
* No capacity admission control · no schema/capability gating
* No HA orchestration · no ENI live migration · no cordon/drain (services stubbed → `Unimplemented`)
* No TLS / mTLS / RBAC / audit log
* No diagnostic RPCs (TraceFlow, ExplainMatch, ExplainDrift, TriggerResimulation — stubbed) · no saga across DPUs (`ApplyBatch` stubbed → `Unimplemented`)

All of the above are covered by Phase 2 ([`impl-plan-advanced.md`](impl-plan-advanced.md)).

### Exit criteria (must all pass)

```bash
# 1. Build and unit tests
cd src/impl-go/dashd
go build ./...                 # zero errors
go vet ./...                   # zero warnings
go test -race ./...            # all tests pass with race detector

# 2-3. Start dash-sim + dashd
cd ../dash-sim && go run ./cmd/dash-sim &
cd ../dashd && go run ./cmd/dashd --config configs/dashd.example.yaml &

# 4. Register a DPU
curl -X PUT http://localhost:8443/v1/inventory \
  -d '{"dpus":[{"id":"dpu-0","endpoint":"localhost:50051"}]}'

# 5-6. Create a VNet and an ENI
curl -X PUT http://localhost:8443/v1/vnets/vnet-1 -d '{"vni":100}'
curl -X PUT http://localhost:8443/v1/enis/eni-1 \
  -d '{"dpu_id":"dpu-0","vnet":"vnet-1","mac_address":"00:11:22:33:44:55"}'

# 7. Within 2s, dashd log shows: Apply(vnet → dpu-0) and Apply(eni → dpu-0)

# 8. Drift returns empty
curl http://localhost:7443/admin/drift?dpu=dpu-0    # {"items":[]}

# 9. Update — re-Apply within 5s
curl -X PUT http://localhost:8443/v1/enis/eni-1 \
  -d '{"dpu_id":"dpu-0","vnet":"vnet-1","mac_address":"00:AA:BB:CC:DD:EE"}'

# 10. Restart dashd — state persists
kill %2 && go run ./cmd/dashd --config configs/dashd.example.yaml &
curl http://localhost:8443/v1/vnets/vnet-1          # 200 with persisted data

# 11. Health
curl http://localhost:7443/admin/health             # status:ok, dpu-0:ONLINE
```

If all 11 steps work, Phase 1 is done.

---

## 2. Repository context: what exists, what to reuse, what to retire

### Existing components Phase 1 builds on

| Path | Purpose | How Phase 1 uses it |
|---|---|---|
| `proto/dashcenter/v1/` | 7-file northbound proto surface, 6 services | gRPC server registers `ControlPlane` + `ObservabilityService`; other 4 services stubbed `Unimplemented` |
| `proto/dashapi/v1/` | Per-DPU southbound proto | `dispatch/` and `subscribe/` use it to talk to each DPU |
| `src/impl-go/gen/go/dashcenter/v1/` | Generated northbound stubs | imported as `dashcenterv1` |
| `src/impl-go/gen/go/dashapi/v1/` | Generated southbound stubs | imported as `dashapiv1` |
| `src/impl-go/dashapi-runtime/kinds/` | 29-kind registry (Lookup, Pack, Unpack, TableName) | used by `placement/translate.go` and `model/obs_cache.go` |
| `src/impl-go/dash-sim-client/pkg/client/` | Thin SDK wrapping `dashapiv1.DashApiClient` | reused verbatim by `dispatch/worker.go` and `subscribe/pump.go` |
| `src/impl-go/dash-sim/` | Reference DASH simulator | runs as a DPU under test during integration |

### Existing scaffold to retire

The current `internal/README.md` lists 15 packages from an earlier Redis-centric design (`store/` was Redis, `ingest/` polled, `normalize/` translated to Redis hashes, `api/ws/` was WebSocket, `cluster/` was embedded Raft). **Phase 1 abandons that design entirely.** The new design uses file/etcd (no Redis), `dashapi.Subscribe` push (no polling), gRPC streaming (no WebSocket), etcd-lease leader election in Phase 2 (no embedded Raft).

Step 0 retires the old scaffold cleanly **and rewrites `src/impl-go/dashd/README.md`** (the top-level README currently describes the old Redis-centric design — it must point at the new impl plans as the source of truth).

---

## 3. Phase 1 architecture & data flow

```
                          operator
                             │
                             │ northbound (REST :8443 / gRPC :9443)
                             │ dashcenter.v1
                             ▼
┌──────────────────────────────────────────────────────────────────────┐
│                          dashd process                               │
│                                                                      │
│  ┌──────────────────┐    ┌──────────────────┐                        │
│  │ server/rest      │    │ server/grpc      │                        │
│  │ (HTTP gateway)   │    │ (ControlPlane,   │                        │
│  │                  │    │  Observability)  │                        │
│  └────────┬─────────┘    └────────┬─────────┘                        │
│           │ Put/Get/List          │                                  │
│           ▼                       │                                  │
│  ┌──────────────────────────────────────┐                            │
│  │ store/file                           │ persistent JSON on disk    │
│  │ (DesiredStore interface)             │ Watch() → DesiredEvent ch  │
│  └──────────────┬───────────────────────┘                            │
│                 │                                                    │
│                 ▼                                                    │
│  ┌──────────────────────────────────────┐                            │
│  │ reconciler                           │ select: desired Watch,     │
│  │   - tick every 30s                   │   DirtyC, 30s tick         │
│  │   - watch DirtyC from dispatch       │                            │
│  │   - call mgr.Sync(dpu)               │                            │
│  └──────────────┬───────────────────────┘                            │
│                 │ Sync(dpu)                                          │
│                 ▼                                                    │
│  ┌──────────────────────────────────────┐                            │
│  │ dispatch/Manager                     │ map[dpu] → *DpuWorker      │
│  │   per-DPU goroutine + capacity-1     │ inbox (coalescing)         │
│  └──────┬───────────────────────┬───────┘                            │
│         │ for each dpu:         │ exposes DirtyC                     │
│         │   1. load specs       │                                    │
│         │   2. placement.Resolve│                                    │
│         │   3. obsCache.Diff    │                                    │
│         │   4. order by tier    │                                    │
│         │   5. Apply/Delete     │                                    │
│         ▼                       │                                    │
│  ┌──────────────────────────────────────┐  ┌──────────────────────┐  │
│  │ dispatch/DpuWorker                   │  │ subscribe/Pump       │  │
│  │  - dash-sim-client.Client            │  │  - Subscribe stream  │  │
│  │  - rate.Limiter (100/s)              │  │  - snapshot-first    │  │
│  │  - error budget                      │  │  - backoff reconnect │  │
│  └──────┬───────────────────────────────┘  └────────┬─────────────┘  │
│         │ Apply/Delete                              │ events         │
│         ▼                                           ▼                │
│  ┌──────────────────────────┐         ┌──────────────────────────┐   │
│  │ obsCache (model)         │◄────────│ populates per-DPU cache; │   │
│  │ per-DPU observed state   │         │ signals DirtyC           │   │
│  └──────────────────────────┘         └──────────────────────────┘   │
│                                                                      │
│  ┌──────────────────────────────────────┐                            │
│  │ inventory + Prober                   │ DpuEntry registry,         │
│  │  (periodic dashapi.List())           │ ONLINE/OFFLINE state       │
│  └──────────────────────────────────────┘                            │
│                                                                      │
│  ┌──────────────────────────────────────┐                            │
│  │ server/admin (HTTP :7443)            │ /admin/health, /drift,     │
│  │                                      │ /reconcile, etc.           │
│  └──────────────────────────────────────┘                            │
└──────────────────────────────────────────────────────────────────────┘
                             │
                             │ southbound: dashapi.v1
                             ▼
            ┌─────────────────────────────────────────┐
            │  dash-sim (dev) / real DPU agent (prod) │
            └─────────────────────────────────────────┘
```

### Data flow: a single `PutEni` RPC

1. Operator calls `gRPC ControlPlane.PutEni(spec)` on `:9443` (or `PUT /v1/enis/{name}` on `:8443`)
2. gRPC handler validates spec (non-nil, non-empty name) and calls `store.Put("eni", spec.Name, spec, spec.ExpectedGeneration)` → returns `Ack{generation: N}`
3. `store.file` writes `<state_dir>/eni/<name>.json` and emits a `DesiredEvent{Type: PUT}` on its internal Watch channel
4. `reconciler` receives the event, calls `placement.AffectedDpus(key)` (for `EniSpec`: the single DPU in `spec.DpuId`), and calls `mgr.Sync("dpu-X")`
5. `dispatch.Manager.Sync()` sends an empty struct to that DPU's `inbox` channel (cap 1, non-blocking — multiple Syncs coalesce into one wake)
6. The DPU's `DpuWorker` goroutine wakes:
   * Loads all desired specs from `store.List()`
   * Calls `placement.Resolve(dpuID, allSpecs, inv)` → `[]*dashapi.Object`
   * Calls `obsCache.Diff(dpuID, resolved)` → `{Add: [Eni], Update: [], Remove: []}`
   * Orders objects via `placement.OrderForApply` (tier 1 first)
   * For each object: `client.Apply(obj)`, rate-limited at 100 ops/s
   * On success: optimistically `obsCache.Set(dpuID, obj)`
7. Within seconds, `dashapi.Subscribe` pushes an `Event{Type: PUT}` confirming. `subscribe.Pump` calls `obsCache.Set` (no-op) and signals `DirtyC` (no-op since diff is now empty)

Steady state: zero traffic. Drift caught on next 30s tick. Subscribe pump keeps cache fresh between ticks.

---

## 4. Package dependency graph

```
                       ┌────────┐
                       │ config │  (no deps; pure data)
                       └────┬───┘
                            │
            ┌───────────────┼───────────────┐
            │               │               │
        ┌───▼───┐    ┌──────▼──────┐   ┌────▼─────┐
        │ store │    │  inventory  │   │  model   │
        │ (intf)│    │ (registry + │   │ (ObsCache│
        │       │    │   Prober)   │   │   + Diff)│
        └───┬───┘    └──────┬──────┘   └────┬─────┘
            │               │               │
            └───────┬───────┴───────┬───────┘
                    │               │
                ┌───▼───────────────▼───┐
                │     placement         │  pure function
                │  (Resolve + Translate │  no I/O, no goroutines
                │   + DependencyOrder)  │
                └───────────┬───────────┘
                            │
            ┌───────────────┼───────────────┐
            │               │               │
        ┌───▼─────┐  ┌──────▼──────┐  ┌─────▼─────┐
        │subscribe│  │  dispatch   │  │reconciler │
        │ (Pump)  │  │ (Manager +  │  │ (select   │
        │         │  │  DpuWorker) │  │   loop)   │
        └─────────┘  └──────┬──────┘  └─────┬─────┘
                            │               │
                            └───────┬───────┘
                                    │
            ┌───────────────────────┼────────────────────────┐
            │                       │                        │
       ┌────▼─────┐         ┌───────▼──────┐         ┌───────▼──────┐
       │server/   │         │ server/rest  │         │ server/admin │
       │grpc      │         │              │         │              │
       └──────────┘         └──────────────┘         └──────────────┘
                                    │
                                    ▼
                          ┌──────────────────┐
                          │ cmd/dashd/main.go│  bootstrap
                          └──────────────────┘
```

**Hard rules**:
* `placement` MUST remain pure: no goroutines, no I/O, no global state, no time-dependent behavior
* `dispatch` is the ONLY package that holds `*dashapi.DashApiClient` (one per DPU)
* `subscribe` reads from `dashapi.Subscribe`, writes to `model.ObsCache` + a `DirtyC` channel; never calls `dispatch` directly
* `reconciler` never calls `dashapi` directly — it only calls `dispatch.Manager.Sync(dpuID)`
* `server/*` depend on every business package but never the reverse

---

## 5. Step 0 — Scaffold cleanup

Goal: make the workspace match the new Phase 1 design before any new code lands.

### Files to retire

For each path below, **replace** with a single `doc.go`:
```go
// Package <name> is reserved. The Phase 1 design uses different
// internal packages — see internal/README.md.
//
// Deprecated: superseded by Phase 1 design.
package <name>
```

Paths (skip any that don't exist):
* `internal/ingest/doc.go`
* `internal/normalize/doc.go`
* `internal/api/grpc/doc.go`
* `internal/api/rest/doc.go`
* `internal/api/ws/doc.go`
* `internal/read/doc.go`
* `internal/write/doc.go`
* `internal/events/doc.go`
* `internal/invalidate/doc.go`
* `internal/compute/doc.go`
* `internal/reconcile/doc.go`
* `internal/cluster/doc.go`
* `internal/telemetry/doc.go`

For `internal/store/`, `internal/inventory/`, `internal/config/` — **delete any old `doc.go`** and start fresh in Steps 1–3.

### Files to replace

#### `internal/README.md`

```markdown
# `internal/` — dashd subsystems (Phase 1)

dashd is implemented as 11 internal packages plus the bootstrap in
`cmd/dashd/main.go`. Full plan: [`../impl-plan-basic.md`](../impl-plan-basic.md).

## Phase 1 packages

| Package | Purpose |
|---|---|
| `config/`     | YAML config loader + defaults + flag overrides |
| `store/`      | `DesiredStore` interface |
| `store/file/` | On-disk JSON backend |
| `inventory/`  | DPU registry + liveness prober |
| `model/`      | Domain types: `ObsCache`, `Diff`, `ObjectKey` |
| `placement/`  | Pure function: fleet specs → per-DPU dashapi objects |
| `subscribe/`  | Per-DPU Subscribe pump (observed state ingestion) |
| `dispatch/`   | Per-DPU worker pool (Apply/Delete dispatch) |
| `reconciler/` | Dirty-set manager + tick loop |
| `server/grpc/` | gRPC server (ControlPlane + Observability) |
| `server/rest/` | HTTP REST gateway |
| `server/admin/` | Admin HTTP (health, drift, reconcile) |

## Dependency rules
- `placement/` must remain pure (no I/O, no goroutines, no global state)
- `dispatch/` is the only package that owns `*dashapi.DashApiClient`
- `server/*` packages depend on business packages but never the reverse

## Phase 2 (not in Phase 1)
`store/etcd/`, `ha/leader/`, `ha/orchestrator/`, `namespace/`, `capacity/`,
`schema/`, `migration/`, `operations/`, `auth/`, `audit/`, `flow/`,
`saga/`, `api/gnmi/`. See [`../impl-plan-advanced.md`](../impl-plan-advanced.md).
```

#### `configs/dashd.example.yaml`

```yaml
# dashd Phase 1 example configuration.
listen:
  grpc_addr:  ":9443"
  rest_addr:  ":8443"
  admin_addr: ":7443"

storage:
  backend: file         # file | etcd (etcd: Phase 2)
  file:
    state_dir: /var/lib/dashd

inventory:
  source: file          # file | api
  file: /etc/dashd/inventory.example.yaml

reconcile:
  tick_interval: 30s
  per_dpu_inbox_size: 1
  apply_rate_limit: 100         # ops/s per DPU
  error_budget_per_min: 10

log:
  level:  info          # debug | info | warn | error
  format: json          # json | text
```

#### `configs/inventory.example.yaml` (new)

```yaml
dpus:
  - id: dpu-0
    endpoint: localhost:50051
    labels:
      rack: A1
  - id: dpu-1
    endpoint: localhost:50052
    labels:
      rack: A1
```

### Verification

```bash
cd src/impl-go/dashd && go build ./...   # must succeed
```

---

## 6. Step 1 — `internal/config/`

**Package path**: `github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/config`

### Files: `config.go`, `config_test.go`

### Types

```go
type Config struct {
    Listen    ListenConfig    `yaml:"listen"`
    Storage   StorageConfig   `yaml:"storage"`
    Inventory InventoryConfig `yaml:"inventory"`
    Reconcile ReconcileConfig `yaml:"reconcile"`
    Log       LogConfig       `yaml:"log"`
}

type ListenConfig struct {
    GRPCAddr  string `yaml:"grpc_addr"`   // default ":9443"
    RESTAddr  string `yaml:"rest_addr"`   // default ":8443"
    AdminAddr string `yaml:"admin_addr"`  // default ":7443"
}

type StorageConfig struct {
    Backend string          `yaml:"backend"`     // "file" only in Phase 1
    File    FileStoreConfig `yaml:"file"`
}
type FileStoreConfig struct { StateDir string `yaml:"state_dir"` }

type InventoryConfig struct {
    Source string `yaml:"source"`   // "file" | "api"
    File   string `yaml:"file"`     // required when Source=="file"
}

type ReconcileConfig struct {
    TickInterval      time.Duration `yaml:"tick_interval"`        // default 30s
    PerDPUInboxSize   int           `yaml:"per_dpu_inbox_size"`   // default 1
    ApplyRateLimit    float64       `yaml:"apply_rate_limit"`     // default 100
    ErrorBudgetPerMin int           `yaml:"error_budget_per_min"` // default 10
}

type LogConfig struct {
    Level  string `yaml:"level"`   // debug|info|warn|error, default info
    Format string `yaml:"format"`  // json|text, default json
}
```

### Functions

```go
func Default() *Config                          // returns Config with all production defaults
func Load(path string) (*Config, error)         // reads YAML, applies defaults, validates
func (c *Config) Validate() error               // returns error if invalid
// internal helper: applyDefaults(c *Config)    // fills zero-value fields from Default()
```

### Validation rules

* `Storage.Backend == "etcd"` → error: `"storage.backend=etcd is not supported in Phase 1"`
* `Storage.Backend` not in `{file}` → error
* `Storage.Backend == "file"` AND `Storage.File.StateDir == ""` → error
* `Inventory.Source` not in `{file, api}` → error
* `Inventory.Source == "file"` AND `Inventory.File == ""` → error
* `Reconcile.TickInterval <= 0` → error
* `Reconcile.PerDPUInboxSize < 1` → error
* `Reconcile.ApplyRateLimit <= 0` → error
* `Reconcile.ErrorBudgetPerMin < 1` → error
* `Log.Level` not in `{debug,info,warn,error}` → error
* `Log.Format` not in `{json,text}` → error

### Test cases (table-driven)

1. `Default()` passes `Validate()`
2. `Load(valid.yaml)` returns matching Config
3. `Load("/nonexistent")` returns wrapped `os.ErrNotExist`
4. `Load(malformed.yaml)` returns parse error
5. Partial YAML (only `listen.grpc_addr`) → other fields use defaults
6. `Backend: "etcd"` → error mentions Phase 1
7. `Backend: "redis"` → error
8. `Log.Level: "trace"` → error
9. Construct Config manually with `TickInterval: 0` → error

---

## 7. Step 2 — `internal/store/`

**Package paths**:
* `.../dashd/internal/store`
* `.../dashd/internal/store/file`

### Files: `store/store.go`, `store/file/file.go`, `store/file/file_test.go`

### `store/store.go` — interface

> **Review fix [A1]:** `ObjectKey` includes `Namespace` from Phase 1 (default `"default"`).
> This eliminates the forklift breaking change that was previously deferred to Phase 2-M3.
> Phase 2 adds *enforcement* (cross-namespace validation, RBAC binding) — not structure.
>
> **Review fix [B1]:** `StoredSpec.EtcdRevision` is present from Phase 1 (zero for file backend).
> `Generation` is documented as a per-key opaque monotonic token.
>
> **Review fix [B2]:** `EventResync` sentinel signals subscribers that events were dropped
> and they must re-list to restore consistency.
>
> **Review fix [A2]:** `Watch()` contract documents the strictest semantics both backends
> must provide (channel may close, caller must re-subscribe).

```go
type ObjectKey struct {
    Namespace string  // required; defaults to "default"; Validate() rejects ""
    Kind      string  // lowercase snake_case ("vnet", "eni", "vnet_mapping")
    Name      string  // operator-supplied resource name
}
func (k ObjectKey) String() string { return k.Namespace + "/" + k.Kind + "/" + k.Name }

// DefaultNamespace is used when the caller omits the namespace.
const DefaultNamespace = "default"

type StoredSpec struct {
    Key          ObjectKey
    Generation   int64      // per-key opaque monotonic token; starts at 1 for file backend.
                            // Do NOT compare generations across keys.
                            // For the etcd backend (Phase 2) this is etcd ModRevision.
    EtcdRevision int64      // populated only by the etcd backend; 0 for file backend.
    Data         []byte     // protojson-encoded spec
    UpdatedAt    time.Time
}

type EventType int
const (
    EventPut    EventType = 1
    EventDelete EventType = 2
    EventResync EventType = 3  // sentinel: subscriber missed events; must re-list
)

type DesiredEvent struct {
    Type EventType
    Key  ObjectKey      // zero-value for EventResync
    Spec *StoredSpec    // nil for EventDelete and EventResync
}

type DesiredStore interface {
    // Put creates or replaces. Returns the new generation.
    // key.Namespace must be non-empty (caller defaults to DefaultNamespace).
    // expectedGeneration > 0 and != current → ErrGenerationMismatch.
    // expectedGeneration == 0 disables the check (last-write-wins).
    Put(ctx context.Context, key ObjectKey, spec proto.Message, expectedGeneration int64) (int64, error)

    // Delete removes the spec. Returns ErrNotFound if absent.
    Delete(ctx context.Context, key ObjectKey) error

    // Get returns the stored spec. Returns ErrNotFound if absent.
    Get(ctx context.Context, key ObjectKey) (*StoredSpec, error)

    // List returns all specs for (namespace, kind), sorted by Name.
    // Empty slice if none.
    List(ctx context.Context, namespace, kind string) ([]*StoredSpec, error)

    // Watch returns a channel receiving a snapshot of current state
    // followed by live mutations. The channel MAY be closed without warning
    // (compaction, store restart, slow-subscriber drop, etc.); the caller
    // MUST be prepared to re-Subscribe.
    //
    // Buffered (64). When a subscriber would be dropped due to back-pressure,
    // the store sends EventResync before dropping, so the consumer knows it
    // must re-list to restore consistency.
    //
    // Generation values are strictly monotonic *per key* and globally
    // unique only for the etcd backend; callers SHOULD NOT compare
    // generations across keys.
    Watch(ctx context.Context) (<-chan DesiredEvent, error)

    // Close releases resources. Idempotent.
    Close() error
}

var (
    ErrNotFound           = errors.New("store: not found")
    ErrGenerationMismatch = errors.New("store: generation mismatch")
    ErrClosed             = errors.New("store: closed")
)
```

### `store/file/` — disk layout

> **Review fix [A1]:** namespace is part of the on-disk path from day one.

```
<state_dir>/<namespace>/<kind>/<name>.json
```

Phase 1 always uses `"default"`, so the path is `<state_dir>/default/<kind>/<name>.json`.
Phase 2 multi-tenancy simply creates additional namespace directories — no migration tool needed.

Each `.json` file:
```json
{
  "namespace": "default",
  "kind": "vnet",
  "name": "vnet-1",
  "generation": 3,
  "updated_at": "2026-06-07T10:00:00.123Z",
  "spec": { /* protojson-encoded spec */ }
}
```

### `store/file/` — implementation rules

**Type**:
```go
type FileStore struct {
    dir    string
    mu     sync.RWMutex
    index  map[store.ObjectKey]*store.StoredSpec
    subs   map[chan store.DesiredEvent]struct{}
    closed bool
}
func Open(dir string) (*FileStore, error)
```

**`Open()`**: `os.MkdirAll(dir, 0o750)`, walk all `.json` files (using `filepath.WalkDir`), parse envelopes, populate `index`. Malformed file → return wrapped error.

**`Put()`**:
1. Take write lock
2. If key exists and `expectedGeneration > 0` and `expectedGeneration != current` → `ErrGenerationMismatch`
3. Marshal spec via `protojson.Marshal(spec)`
4. Build envelope `{kind, name, generation: current+1 (or 1), updated_at: now, spec: rawJSON}`
5. `os.WriteFile(tmpPath, encoded, 0o640)` then `os.Rename(tmpPath, finalPath)` (atomic)
6. Update `index`
7. Broadcast `EventPut` to all subs (non-blocking sends)
8. Return new generation

**`Delete()`**:
1. Write lock
2. If key absent → `ErrNotFound`
3. `os.Remove(path)` — if file missing, log warning but proceed (index is source of truth)
4. Remove from `index`
5. Broadcast `EventDelete`

**`Watch()`**:
1. Allocate `ch := make(chan DesiredEvent, 64)`
2. Write lock; add `ch` to `s.subs`
3. **Snapshot**: under lock, iterate `s.index` and send `EventPut` for each entry on `ch`. Use non-blocking send (`select { case ch <- ev: default: return nil, fmt.Errorf("subscriber too slow") }`)
4. Release lock; spawn goroutine: `<-ctx.Done()`, take write lock, remove ch from subs, close ch
5. Return ch

**Broadcast (in Put/Delete)** — done under write lock so subscribers see events in commit order:

> **Review fix [B2]:** on back-pressure, send `EventResync` sentinel before dropping,
> so the consumer knows it must re-list. Also increment
> `dashd_subscribe_events_dropped_total` counter (see §metrics).

```go
ev := store.DesiredEvent{...}
for ch := range s.subs {
    select {
    case ch <- ev:
    default:
        // back-pressure: send resync sentinel so consumer knows to re-list
        select {
        case ch <- store.DesiredEvent{Type: store.EventResync}:
        default:
            // channel completely stuck — consumer will catch up via 30s tick
        }
        slog.Warn("store: subscriber too slow, sent EventResync")
    }
}
```

**`Close()`**: write lock, set `closed=true`, close all sub channels, clear maps. Idempotent (check `closed` flag).

### Test cases (`store/file/file_test.go`)

Use `dashcenterv1.VnetSpec` (or any other proto) as test payload.

1. `Open` empty dir → `List(anyKind)` returns empty slice
2. First `Put` returns generation 1
3. Two `Put`s of same key (expected=0) → generations 1, 2
4. `Put` with `expectedGeneration=99` against gen=1 → `errors.Is(err, ErrGenerationMismatch)`
5. `Put` with matching `expectedGeneration=1` → new gen=2
6. `Get` non-existent → `errors.Is(err, ErrNotFound)`
7. `Get` after Put → returns same spec (protojson-decode to verify)
8. `Delete` non-existent → `ErrNotFound`
9. `Delete` after `Put` → `Get` returns `ErrNotFound`
10. `List` returns specs sorted by Name
11. `List` unknown kind → empty slice (not nil)
12. **Restart**: Open, Put 3 specs, Close. Re-Open same dir → `List` returns same 3 with original generations
13. `Watch`: pre-populate 2 specs; subscribe; first 2 events are EventPut snapshots; subsequent Put delivers EventPut; Delete delivers EventDelete
14. `Watch` ctx cancel closes channel within 1s
15. Multiple `Watch` subscribers each receive each event
16. After successful `Put`, no `.tmp` file remains on disk
17. 100 concurrent goroutines doing `Put` with different names → all succeed; `List` returns 100 (run with `-race`)
18. `Open` with malformed `.json` file in dir → error mentioning path

---

## 8. Step 3 — `internal/inventory/`

**Package path**: `.../dashd/internal/inventory`

### Files: `inventory.go`, `inventory_test.go`, `probe.go`, `probe_test.go`

### State machine

`DpuState` enum from `proto/dashcenter/v1/types.proto`:
* `DPU_STATE_REGISTERING = 1` — newly registered, no probe success yet
* `DPU_STATE_ONLINE = 2` — last probe succeeded
* `DPU_STATE_OFFLINE = 3` — 3 consecutive probe failures
* `DPU_STATE_RECONNECTING = 4` — was OFFLINE, latest probe succeeded once
* `DPU_STATE_CORDONED = 5` — Phase 2
* `DPU_STATE_DEGRADED = 6` — set by dispatch when error budget exceeded
* `DPU_STATE_REMOVING/REMOVED = 7/8` — Phase 2

Transitions:
```
NONE ──Register()──► REGISTERING ──probe ok──► ONLINE
                          │
                          │  probe fail × 3
                          ▼
                       OFFLINE ──probe ok──► RECONNECTING ──probe ok──► ONLINE
```

### Types

```go
type DpuEntry struct {
    ID           string
    Endpoint     string
    Labels       map[string]string
    State        dashcenterv1.DpuState
    Capabilities *dashcenterv1.DpuCapabilities  // nil until first successful probe
    LastSeen     time.Time
    ConsecErrors int                            // reset on success
}
func (e DpuEntry) Clone() DpuEntry              // deep-copy Labels

type Inventory struct { /* mu sync.RWMutex; byID map[string]*DpuEntry */ }
func New() *Inventory
```

### Methods

```go
// Register adds new or updates Endpoint/Labels (preserves State+Capabilities).
// New entries start in DPU_STATE_REGISTERING. Empty ID or Endpoint → error.
func (inv *Inventory) Register(e DpuEntry) error

// Deregister removes. Returns ErrNotFound if absent.
func (inv *Inventory) Deregister(id string) error

// Get returns a clone. Returns ErrNotFound if absent.
func (inv *Inventory) Get(id string) (DpuEntry, error)

// List returns clones, sorted by ID.
func (inv *Inventory) List() []DpuEntry

// SetState updates State + LastSeen=now. Returns ErrNotFound.
func (inv *Inventory) SetState(id string, state dashcenterv1.DpuState) error

// SetCapabilities stores capabilities advertised by the DPU.
func (inv *Inventory) SetCapabilities(id string, caps *dashcenterv1.DpuCapabilities) error

// IncrementErrors atomically bumps ConsecErrors, returns new value.
func (inv *Inventory) IncrementErrors(id string) (int, error)

// ResetErrors zeros ConsecErrors.
func (inv *Inventory) ResetErrors(id string) error

// LoadFromFile reads YAML and Register()s each DPU.
// YAML: dpus: [{id, endpoint, labels: {k: v}}]
func LoadFromFile(path string, inv *Inventory) error

// Subscribe fires fn after every Register/Deregister. Used by main.go
// to start/stop dispatch workers and subscribe pumps for late DPUs.
func (inv *Inventory) Subscribe(fn func(dpuID, endpoint string, removed bool))

var ErrNotFound = errors.New("inventory: dpu not found")
```

### `probe.go`

```go
type Prober struct {
    inv      *Inventory
    interval time.Duration
    clients  sync.Map  // dpuID → *client.Client
}
func NewProber(inv *Inventory, interval time.Duration) *Prober

// Run launches one probe goroutine per DPU. Reconciles goroutine set
// against inventory on every tick (new DPUs picked up, removed DPUs stopped).
// Blocks until ctx cancelled; on cancel, closes all client connections.
func (p *Prober) Run(ctx context.Context)
```

**Probe payload**: `dashapi.List(kind=OBJECT_KIND_VNET, key_prefix="")` with 5s context timeout. This is the cheapest call that exercises gRPC without side effects.

**On success** (`recordSuccess`):
* If state is REGISTERING / OFFLINE / RECONNECTING → `SetState(ONLINE)` + log info
* `ResetErrors`
* (Capability negotiation: Phase 1 does not implement; Phase 2 calls a CapabilitiesNegotiation RPC and stores via `SetCapabilities`)

**On failure** (`recordFailure`):
* `IncrementErrors` → if < 3: log debug, return
* If ≥ 3 and current state != OFFLINE: `SetState(OFFLINE)`, log warn, close + delete stale client from `p.clients`

### Test cases — `inventory_test.go`

1. `Register` adds entry, state = REGISTERING
2. `Register` empty ID → error
3. `Register` empty Endpoint → error
4. `Register` existing ID updates Endpoint, preserves State (set to ONLINE first, re-Register, state still ONLINE)
5. `Deregister` removes
6. `Deregister` non-existent → ErrNotFound
7. `Get` non-existent → ErrNotFound
8. `List` sorted by ID
9. `SetState` updates LastSeen
10. `SetCapabilities`
11. 100 concurrent `IncrementErrors` → final count == 100 (`-race`)
12. `ResetErrors`
13. `LoadFromFile` valid YAML
14. `LoadFromFile` missing file → error
15. `LoadFromFile` malformed YAML → error
16. `LoadFromFile` duplicate ID → last wins (no error)
17. `Clone` deep-copies Labels (mutate clone → original unchanged)
18. `Subscribe` callback fires on Register/Deregister

### Test cases — `probe_test.go`

Use a fake `dashapiv1.DashApiServer` on a real TCP listener (`net.Listen("tcp", "127.0.0.1:0")` + `grpc.NewServer()`). Use `go.uber.org/goleak` for leak checks.

1. First success → REGISTERING → ONLINE
2. 3 consecutive failures (pointing at unreachable port) → OFFLINE
3. Recovery: ONLINE → (stop server) → OFFLINE → (restart server) → ONLINE
4. New DPU added mid-flight picked up on next tick
5. Deregistered DPU stops probing within 2 ticks
6. ctx cancel → no goroutine leaks (goleak)

---

## 9. Step 4 — `internal/model/`

**Package path**: `.../dashd/internal/model`

### Files: `types.go`, `obs_cache.go`, `obs_cache_test.go`

### `types.go`

```go
type ObjectKey struct {
    DpuID string
    Kind  dashapiv1.ObjectKind
    Key   []string  // joined key components
}
func (k ObjectKey) JoinedKey() string  // strings.Join(k.Key, "/")
func KeyOf(dpuID string, obj *dashapiv1.Object) ObjectKey  // deep-copy Key

type DiffResult struct {
    Add    []*dashapiv1.Object  // in desired, not in observed
    Update []*dashapiv1.Object  // in both, payload differs
    Remove []*dashapiv1.Object  // in observed, not in desired
}
func (d DiffResult) IsEmpty() bool
func (d DiffResult) Total() int
```

### `obs_cache.go`

```go
type ObsCache struct {
    mu   sync.RWMutex
    data map[string]map[string]*dashapiv1.Object  // dpuID → innerKey → obj
}
func NewObsCache() *ObsCache

// Set inserts/replaces obj in dpuID's cache.
func (c *ObsCache) Set(dpuID string, obj *dashapiv1.Object)

// Delete removes (kind, key) from dpuID's cache.
func (c *ObsCache) Delete(dpuID string, kind dashapiv1.ObjectKind, key []string)

// ClearDpu atomically replaces all entries for dpuID with empty set.
// Called by subscribe/Pump on every reconnect (snapshot-first re-sync).
func (c *ObsCache) ClearDpu(dpuID string)

// GetDpu returns a defensive copy of dpuID's cache (callers may mutate).
func (c *ObsCache) GetDpu(dpuID string) map[string]*dashapiv1.Object

// Diff computes Add/Update/Remove for dpuID vs the given desired set.
// Equality: same (kind, key) AND payloads compare equal under proto.Equal.
// Generation is NOT compared.
// Output is stable-sorted by (kind, joined_key) for reproducible logs.
func (c *ObsCache) Diff(dpuID string, desired []*dashapiv1.Object) DiffResult
```

**`innerKey` helper** (private): `fmt.Sprintf("%d:%s", int(kind), strings.Join(key, "/"))`

**`payloadsEqual` helper** (private):
```go
ma, err1 := kinds.PayloadOf(a)
mb, err2 := kinds.PayloadOf(b)
if err1 != nil || err2 != nil { return false }
return proto.Equal(ma, mb)
```

### Test cases (`obs_cache_test.go`)

1. `Set` then `GetDpu` returns object
2. `Set` overwrites — second `Set` for same key wins
3. `Delete` removes
4. `Delete` unknown key — no panic
5. `ClearDpu` empties cache for that DPU
6. `GetDpu` returns defensive copy — mutating returned map doesn't affect cache
7. `Diff(empty desired, empty observed)` → empty
8. `Diff(desired=3, observed=0)` → Add has 3
9. `Diff(desired=0, observed=3)` → Remove has 3
10. `Diff` same payload → IsEmpty
11. `Diff` different payload → Update
12. `Diff` stable order across runs with shuffled inputs
13. 20 concurrent goroutines × 100 ops Set/Get/Delete (`-race`)
14. `KeyOf` deep-copies key (mutate returned ObjectKey.Key → source unchanged)

---

## 10. Step 5 — `internal/placement/`

**Package path**: `.../dashd/internal/placement`

### Files: `placement.go`, `translate.go`, `order.go`, `placement_test.go`, `translate_test.go`, `order_test.go`

### `placement.go` — public API

```go
type DesiredSpecs struct {
    Vnets         map[string]*dashcenterv1.VnetSpec
    Enis          map[string]*dashcenterv1.EniSpec
    VnetMappings  map[string]*dashcenterv1.VnetMappingSpec
    AclPolicies   map[string]*dashcenterv1.AclPolicySpec
    RoutePolicies map[string]*dashcenterv1.RoutePolicySpec
    HaSets        map[string]*dashcenterv1.HaSetSpec
}

// Resolve returns the complete set of dashapi.v1 Objects that should
// exist on dpuID given the desired specs and inventory.
// PURE: no I/O, no goroutines, no global state. Freshly allocated output.
func Resolve(dpuID string, specs *DesiredSpecs, inv *inventory.Inventory) []*dashapiv1.Object

// ResolveAll = Resolve for every DPU in inv. Used for /admin/drift.
func ResolveAll(specs *DesiredSpecs, inv *inventory.Inventory) map[string][]*dashapiv1.Object

// AffectedDpus returns DPU IDs whose Resolve output may have changed
// when the spec at (kind, name) changes. Used by reconciler to minimize Sync.
// Unknown kind → conservative fallback: all DPUs.
func AffectedDpus(kind, name string, specs *DesiredSpecs, inv *inventory.Inventory) []string
```

### Placement rules (LLD §12)

| Spec kind | Goes to which DPUs |
|---|---|
| `VnetSpec` | every DPU that hosts ≥1 ENI of that VNET |
| `EniSpec` | the single DPU named by `EniSpec.DpuId` |
| `VnetMappingSpec` | every DPU that hosts ≥1 ENI in the same VNET |
| `AclPolicySpec` | every DPU that hosts ≥1 ENI referencing that ACL group |
| `RoutePolicySpec` | every DPU that hosts ≥1 ENI referencing that route group |
| `HaSetSpec` | every DPU listed in `HaSetSpec.MemberDpuIds` |

### `Resolve` algorithm (pseudo-code)

```
1. out := []
2. vnetsOnDpu := set{}
   for each EniSpec e in specs.Enis:
     if e.DpuId == dpuID:
       vnetsOnDpu.add(e.Vnet)
       out += TranslateEni(name, e)              // 1 or 2 objects
3. for each VnetSpec v in specs.Vnets:
     if v.Name in vnetsOnDpu:
       out += TranslateVnet(name, v)
4. for each VnetMappingSpec vm in specs.VnetMappings:
     if vm.Vnet in vnetsOnDpu:
       out += TranslateVnetMapping(name, vm)
5. aclGroups, routeGroups := referenced by ENIs on dpuID
6. for each AclPolicySpec a:
     if a.Name in aclGroups:
       out += TranslateAclPolicy(name, a)        // AclGroup + N×AclRule
7. for each RoutePolicySpec r:
     if r.Name in routeGroups:
       out += TranslateRoutePolicy(name, r)      // RouteGroup + N×Route
8. for each HaSetSpec h:
     if dpuID in h.MemberDpuIds:
       out += TranslateHaSet(name, h)            // HaSet + HaSetConfig
9. return out
```

### `translate.go` — signatures

All translators use `kinds.WrapObject(ObjectKind, key, &payload)` from `dashapi-runtime/kinds`.

```go
func TranslateVnet(name string, s *dashcenterv1.VnetSpec) (*dashapiv1.Object, error)
func TranslateEni(name string, s *dashcenterv1.EniSpec) ([]*dashapiv1.Object, error)        // 1 or 2
func TranslateVnetMapping(name string, s *dashcenterv1.VnetMappingSpec) (*dashapiv1.Object, error)
func TranslateAclPolicy(name string, s *dashcenterv1.AclPolicySpec) ([]*dashapiv1.Object, error)   // 1+N
func TranslateRoutePolicy(name string, s *dashcenterv1.RoutePolicySpec) ([]*dashapiv1.Object, error) // 1+N
func TranslateHaSet(name string, s *dashcenterv1.HaSetSpec) ([]*dashapiv1.Object, error)    // 2
```

### Field-level translation tables

**`VnetSpec` → `dash.vnet.Vnet`** (key = `[name]`):
| Source | Target | Notes |
|---|---|---|
| `Vni` | `Vni` | uint32 |
| `GuidAddress` | (Phase 1: optional, may drop) | |
| `Tags` | (custom mapping) | optional |

**`EniSpec` → `dash.eni.Eni` (+ optional `dash.eni_route.EniRoute`)** (keys: `[name]`, `[name]`):
| Source | Target | Notes |
|---|---|---|
| `MacAddress` | `Eni.MacAddress` | required; missing → error |
| `Vnet` | `Eni.VnetName` | |
| `UnderlayIp` | `Eni.UnderlayIp` | string → IPAddress proto |
| `AdminState` | `Eni.AdminState` | enum map |
| `RouteGroupRefs[0]` | `EniRoute.GroupId` | if non-empty: emit 2nd object |
| `Qos` | `Eni.QosName` | optional |

**`VnetMappingSpec` → `dash.vnet_mapping.VnetMapping`** (key = `[Vnet, IpAddress]`):
| Source | Target |
|---|---|
| `MacAddress` | `MacAddress` |
| `UnderlayIp` | `UnderlayIp` |
| `RoutingType` | `RoutingType` (enum) |

**`AclPolicySpec` → `dash.acl_group.AclGroup` + N×`dash.acl_rule.AclRule`**:
* `AclGroup` key = `[name]`
* `AclRule` key = `[name, strconv.Itoa(int(rule.Priority))]`

| Source | Target |
|---|---|
| `Stage` | `AclGroup.AclStage` |
| `IpVersion` | `AclGroup.IpVersion` |
| `Rules[i].Priority` | `AclRule.Priority` |
| `Rules[i].Action` | `AclRule.Action` |
| `Rules[i].SrcPrefixes` | `AclRule.SrcPrefix` |
| `Rules[i].DstPrefixes` | `AclRule.DstPrefix` |
| `Rules[i].Protocol` | `AclRule.Protocol` |
| `Rules[i].SrcPortRange` | `AclRule.SrcPortRange` |
| `Rules[i].DstPortRange` | `AclRule.DstPortRange` |

**`RoutePolicySpec` → `dash.route_group.RouteGroup` + N×`dash.route.Route`**:
* `RouteGroup` key = `[name]`
* `Route` key = `[name, route.Prefix]`

| Source | Target | Notes |
|---|---|---|
| `Routes[i].Prefix` | `Route.Prefix` | |
| `Routes[i].NextHop` | `Route.NextHop` | union: vnet \| underlay \| tunnel |
| `Routes[i].RouteType` | `Route.RoutingType` | enum |
| `Routes[i].EcmpMembers` | `Route.NextHopGroup` | Phase 1: pick first member; full ECMP in Phase 2 |

**`HaSetSpec` → `dash.ha_set.HaSet` + `dash.ha_set_config.HaSetConfig`** (both keys = `[name]`):
| Source | Target |
|---|---|
| `LocalIp` | `HaSet.LocalIp` |
| `PeerIp` | `HaSet.PeerIp` |
| `Vip` | `HaSet.Vip` |
| `OwnerMode` | `HaSetConfig.OwnerMode` |

**Translation errors**: missing required field → `fmt.Errorf("translate %s: missing %s", kind, field)`. Errors abort `Resolve` and surface to the operator.

### `order.go` — dependency tiers (LLD §8)

```go
// Tier returns 1-5; lower applied first. Unknown kind → 99.
func Tier(kind dashapiv1.ObjectKind) int

func OrderForApply(objects []*dashapiv1.Object) []*dashapiv1.Object   // ascending tier
func OrderForDelete(objects []*dashapiv1.Object) []*dashapiv1.Object  // descending tier
```

Tier table:
| Tier | Kinds |
|---|---|
| 1 | `appliance`, `vnet`, `route_type`, `prefix_tag`, `tunnel`, `qos`, `meter_policy`, `route_group`, `acl_group` |
| 2 | `meter_rule`, `route`, `acl_rule`, `routing_appliance`, `pa_validation` |
| 3 | `eni`, `eni_route` |
| 4 | `vnet_mapping`, `acl_in`, `acl_out`, `route_rule`, `meter` |
| 5 | `ha_set`, `ha_set_config`, `ha_scope`, `ha_scope_config`, `outbound_port_map`, `outbound_port_map_range` |

`sort.SliceStable` keeps within-tier order stable for reproducible tests.

### Test scenarios

**`placement_test.go`**:
1. Empty specs → empty result
2. Single ENI on dpu-0: Resolve("dpu-0") = [Vnet, Eni]; Resolve("dpu-1") = []
3. VnetMapping follows ENI placement (only DPUs with that VNET's ENIs)
4. AclPolicy follows ENI's AclGroupRefs
5. Two ENIs in same VNET on different DPUs: both receive the VnetMapping
6. HaSet spans MemberDpuIds; non-member DPU gets nothing
7. ENI with unknown DpuId excluded from ResolveAll
8. Purity: two Resolve calls with identical inputs return proto.Equal contents
9. `AffectedDpus("vnet", "v1")` → every DPU hosting an ENI in v1
10. `AffectedDpus("eni", "e1")` → the single new DpuId
11. `AffectedDpus("unknown", ...)` → all DPUs (conservative)

**`translate_test.go`**:
1. `TranslateVnet` all fields appear in payload
2. `TranslateVnet` missing required → error
3. `TranslateEni` no RouteGroupRefs → 1 object
4. `TranslateEni` with RouteGroupRefs → 2 objects; 2nd is EniRoute with group_id
5. `TranslateVnetMapping` key = [Vnet, IpAddress]
6. `TranslateAclPolicy` N rules → 1+N objects
7. `TranslateRoutePolicy` all routes emitted
8. `TranslateHaSet` emits HaSet + HaSetConfig
9. `TranslateEni` missing MAC → error
10. Round-trip: Translate → kinds.PayloadOf → re-marshal → proto.Equal

**`order_test.go`**:
1. Every dashapiv1.ObjectKind value has Tier in 1..5 (none == 99)
2. `OrderForApply([HaSet, Vnet, Eni])` = `[Vnet, Eni, HaSet]`
3. `OrderForDelete` is reverse
4. Input slice unmutated after call
5. Stable within tier

---

## 11. Step 6 — `internal/subscribe/`

**Package path**: `.../dashd/internal/subscribe`

### Files: `pump.go`, `pump_test.go`

### Types

```go
type Pump struct {
    dpuID    string
    endpoint string
    obs      *model.ObsCache
    dirty    chan<- string  // non-blocking sends; dropped if full
}
func New(dpuID, endpoint string, obs *model.ObsCache, dirty chan<- string) *Pump

// Run blocks until ctx cancelled. Reconnect loop with backoff.
func (p *Pump) Run(ctx context.Context)
```

### Run loop algorithm

```
backoffs := [1s, 2s, 5s, 10s, 30s]
bi := 0
for {
    if ctx.Err() != nil: return
    stayedUp := runOnce(ctx)
    if stayedUp > 30s: bi = 0   // reset after long-running stream
    select { case <-ctx.Done(): return; case <-time.After(backoffs[bi]): }
    if bi < len(backoffs)-1: bi++
}
```

### `runOnce` algorithm

1. `client.Dial(endpoint)` — on failure: log warn, return
2. `client.Subscribe(ctx, []ObjectKind{}, snapshotFirst=true)` — empty kinds = "all"
3. **Snapshot-first contract**: immediately `obs.ClearDpu(p.dpuID)` AND `signalDirty()` — stale entries from before reconnect are wiped
4. Loop:
   * `<-ctx.Done()` → return
   * `<-errCh` → log warn (skip io.EOF), return
   * `<-evCh` → `applyEvent(ev)` + `signalDirty()`

### `applyEvent`

* `EVENT_TYPE_PUT` or `EVENT_TYPE_SNAPSHOT`: `obs.Set(dpuID, ev.Object)`
* `EVENT_TYPE_DELETE`: `obs.Delete(dpuID, ev.Kind, ev.Key)`

### `signalDirty`

```go
select {
case p.dirty <- p.dpuID:
default:  // channel full → drop; reconciler 30s tick catches it
}
```

### `PumpSet` — manages multiple Pumps

```go
type PumpSet struct {
    obs   *model.ObsCache
    dirty chan<- string
    mu    sync.Mutex
    pumps map[string]context.CancelFunc
    wg    sync.WaitGroup
}
func NewSet(obs *model.ObsCache, dirty chan<- string) *PumpSet

// Start launches a Pump if not already running. Idempotent.
func (s *PumpSet) Start(ctx context.Context, dpuID, endpoint string)

// Stop terminates pump for dpuID. Idempotent.
func (s *PumpSet) Stop(dpuID string)

// StopAll stops every pump and waits for goroutines to exit.
func (s *PumpSet) StopAll()
```

### Test cases (`pump_test.go`)

Use fake `dashapiv1.DashApiServer` on real TCP listener. `go.uber.org/goleak`.

1. PUT event populates cache + dirty channel receives dpuID
2. DELETE event removes from cache
3. Snapshot clears previously-stale entries (pre-populate cache, then connect → only snapshot objects remain)
4. Stream error → reconnect after ~1s (first backoff); second reconnect at ~2s
5. Dirty channel cap=1: fire 100 events → no blocking; reconciler sees some signals; cache still gets all 100 objects
6. ctx cancel → no goroutine leaks
7. `PumpSet.Start` then `Stop` works
8. `PumpSet.Start` same dpuID twice → only one pump
9. `PumpSet.StopAll` waits — no leaks after return

---

## 12. Step 7 — `internal/dispatch/`

**Package path**: `.../dashd/internal/dispatch`

### Files: `manager.go`, `worker.go`, `ratelimit.go`, `worker_test.go`

### `manager.go`

```go
type Manager struct {
    cfg    *config.ReconcileConfig
    obs    *model.ObsCache
    store  store.DesiredStore
    inv    *inventory.Inventory
    dirty  chan string  // buffered; shared with subscribe.Pump

    mu      sync.Mutex
    workers map[string]*worker  // dpuID → worker
    wg      sync.WaitGroup
}

func New(obs *model.ObsCache, cfg *config.ReconcileConfig) *Manager

// Wiring (called once at startup).
func (m *Manager) SetStore(s store.DesiredStore)
func (m *Manager) SetInventory(inv *inventory.Inventory)

// DirtyC returns the writable side for subscribe.Pump.
func (m *Manager) DirtyC() chan<- string

// DirtyReadC returns the readable side for reconciler.
func (m *Manager) DirtyReadC() <-chan string

// Start launches a worker for every DPU in inventory. Idempotent.
func (m *Manager) Start(ctx context.Context)

// EnsureWorker creates worker for a single new DPU if not running.
func (m *Manager) EnsureWorker(ctx context.Context, dpuID, endpoint string)

// RemoveWorker stops worker for dpuID and closes its client.
func (m *Manager) RemoveWorker(dpuID string)

// Sync requests a reconcile pass; non-blocking (coalescing).
func (m *Manager) Sync(dpuID string)

// SyncAll calls Sync for every managed DPU.
func (m *Manager) SyncAll()

// Stop gracefully stops all workers; waits for goroutines.
func (m *Manager) Stop()
```

**Buffer sizing**: `dirty` channel capacity = max(len(inventory)*2, 16) to absorb bursts.

### `worker.go`

```go
type worker struct {
    id       string
    endpoint string
    client   *client.Client    // lazy-dialed in reconcilePass
    inbox    chan struct{}     // cap=1; coalescing
    limiter  *rate.Limiter
    obs      *model.ObsCache
    store    store.DesiredStore
    inv      *inventory.Inventory
    budget   int
    errCount int32             // atomic; sliding 1-min window
    cancel   context.CancelFunc
}

func (w *worker) run(ctx context.Context) {
    defer func() { if w.client != nil { _ = w.client.Close() } }()
    go w.errorBudgetTicker(ctx)  // every 60s: atomic.StoreInt32(&errCount, 0)
    for {
        select {
        case <-ctx.Done(): return
        case <-w.inbox: w.reconcilePass(ctx)
        }
    }
}
```

### `reconcilePass` algorithm

1. If `w.client == nil`: `client.Dial(w.endpoint)` — on failure log + return
2. `specs := w.loadSpecs(ctx)` — calls `store.List(store.DefaultNamespace, kind)` for each of vnet/eni/vnet_mapping/acl_policy/route_policy/ha_set; `protojson.Unmarshal` each into typed proto; populate `placement.DesiredSpecs`
3. `resolved := placement.Resolve(w.id, specs, w.inv)`
4. `diff := w.obs.Diff(w.id, resolved)` — if empty: return
5. `applies := placement.OrderForApply(append(diff.Add, diff.Update...))`
6. `deletes := placement.OrderForDelete(diff.Remove)`
7. For each obj in `applies`: `w.applyOne(ctx, obj)` — on error: `recordError` + return (will retry on next Sync)
8. For each obj in `deletes`: `w.deleteOne(ctx, obj)` — on error: `recordError` + return

### `applyOne` / `deleteOne`

```go
func (w *worker) applyOne(ctx context.Context, obj *dashapiv1.Object) error {
    if err := w.limiter.Wait(ctx); err != nil { return err }
    rpcCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
    defer cancel()
    ack, err := w.client.Apply(rpcCtx, obj)
    if err != nil { return err }
    if !ack.GetAccepted() { return errors.New(ack.GetMessage()) }
    w.obs.Set(w.id, obj)   // optimistic cache update
    return nil
}
```

`deleteOne` is symmetric.

### `recordError`

```go
n := atomic.AddInt32(&w.errCount, 1)
if int(n) > w.budget {
    _ = w.inv.SetState(w.id, dashcenterv1.DpuState_DPU_STATE_DEGRADED)
    slog.Error("dispatch: error budget exceeded — DPU quarantined", ...)
}
```

DPU is automatically un-quarantined when prober succeeds (prober transitions to ONLINE which overrides DEGRADED).

### `ratelimit.go`

```go
func newLimiter(opsPerSec float64) *rate.Limiter {
    return rate.NewLimiter(rate.Limit(opsPerSec), int(opsPerSec))
}
```

### Test cases (`worker_test.go`)

Fake `dashapiv1.DashApiServer` on bufconn:

1. Empty diff → no Apply/Delete calls
2. Add: Apply called per object in tier order (Vnet before Eni)
3. Remove: Delete called in reverse order
4. Update: Apply with new content
5. Rate limit: 50 Adds with 100/s limit → ≥ ~400ms elapsed (with burst=100)
6. Ack.accepted=false → errCount becomes 1
7. Budget=3, 4 rejections → DPU state == DEGRADED
8. Successful Apply → obs cache contains new object before any Subscribe push
9. Manager.Start/Stop — no goroutine leaks
10. 100 Sync calls before worker wakes → only 1 reconcilePass executes
11. EnsureWorker starts new DPU mid-flight

---

## 13. Step 8 — `internal/reconciler/`

**Package path**: `.../dashd/internal/reconciler`

### Files: `reconciler.go`, `reconciler_test.go`

### Type

```go
type Reconciler struct {
    store   store.DesiredStore
    inv     *inventory.Inventory
    mgr     *dispatch.Manager
    tick    time.Duration
    forceCh chan struct{}  // cap=1; coalescing
}
func New(s store.DesiredStore, inv *inventory.Inventory, mgr *dispatch.Manager, tick time.Duration) *Reconciler
```

### Run loop

```go
func (r *Reconciler) Run(ctx context.Context) error {
    desCh, err := r.store.Watch(ctx)
    if err != nil { return err }
    dirtyCh := r.mgr.DirtyReadC()
    ticker := time.NewTicker(r.tick)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done(): return nil
        case ev, ok := <-desCh:
            if !ok { return nil }
            r.onDesiredChange(ctx, ev)
        case dpuID := <-dirtyCh:
            r.mgr.Sync(dpuID)
        case <-r.forceCh:
            r.mgr.SyncAll()
        case <-ticker.C:
            r.mgr.SyncAll()
        }
    }
}

// ForceReconcile triggers SyncAll. Non-blocking (coalescing).
func (r *Reconciler) ForceReconcile() {
    select { case r.forceCh <- struct{}{}: default: }
}
```

### `onDesiredChange`

```go
specs := loadSpecsAll(ctx, r.store)  // helper shared with worker.loadSpecs (or duplicated for Phase 1)
affected := placement.AffectedDpus(ev.Key.Kind, ev.Key.Name, specs, r.inv)
if len(affected) == 0 { r.mgr.SyncAll(); return }
for _, id := range affected { r.mgr.Sync(id) }
```

### Test cases (`reconciler_test.go`)

Mock `store.DesiredStore` and `dispatch.Manager` (interface-based or struct with recording methods):

1. Desired event → Sync called for each affected DPU
2. Dirty signal → Sync(that dpuID)
3. 10ms tick interval, wait 30ms → SyncAll called ~3 times
4. ForceReconcile → SyncAll called once
5. ForceReconcile 100 times in tight loop → no panic, reconciler eventually consumes
6. ctx cancel → Run returns nil; goleak verifies clean shutdown

---

## 14. Step 9 — `internal/server/grpc/`

**Package path**: `.../dashd/internal/server/grpc`

### Files: `server.go`, `interceptors.go`, `control_plane.go`, `observability.go`, `stubs.go`, `server_test.go`

### `server.go`

```go
type Server struct{ srv *grpc.Server }

func New(st store.DesiredStore, inv *inventory.Inventory, rec *reconciler.Reconciler, obs *model.ObsCache) *Server {
    g := grpc.NewServer(
        grpc.UnaryInterceptor(unaryChain(loggingInterceptor, recoveryInterceptor)),
        grpc.StreamInterceptor(streamChain(streamLoggingInterceptor, streamRecoveryInterceptor)),
    )
    dashcenterv1.RegisterControlPlaneServer(g, newControlPlaneServer(st, inv, rec))
    dashcenterv1.RegisterObservabilityServiceServer(g, newObservabilityServer(inv, obs, st))
    // Phase 2 services stubbed:
    dashcenterv1.RegisterDiagnosticsServiceServer(g, &diagnosticsStub{})
    dashcenterv1.RegisterOperationsServiceServer(g, &operationsStub{})
    dashcenterv1.RegisterMigrationServiceServer(g, &migrationStub{})
    dashcenterv1.RegisterHaServiceServer(g, &haStub{})
    return &Server{srv: g}
}

func (s *Server) Serve(addr string) error {
    lis, err := net.Listen("tcp", addr)
    if err != nil { return err }
    return s.srv.Serve(lis)
}
func (s *Server) Stop() { s.srv.GracefulStop() }
```

### `interceptors.go`

Three middlewares, applied in order (outermost first):

**Recovery**: defer/recover panics, log full stack via `debug.Stack()`, return `status.Errorf(codes.Internal, ...)`

**Logging**: record `method, code, duration_ms, peer_addr` via `slog.Info("grpc", ...)` on completion. Use `peer.FromContext(ctx)` for peer address. Use `status.Code(err)` for code.

**Stream variants**: same logic for stream interceptors (separate functions; gRPC has distinct interceptor types).

**Chain helpers**: standard pattern that composes N interceptors into one.

### `control_plane.go` — handler patterns

Generic put helper:
```go
func (s *controlPlaneServer) put(ctx context.Context, kind, name string, expected int64, spec proto.Message) (*dashcenterv1.Ack, error) {
    if name == "" { return nil, status.Error(codes.InvalidArgument, "name required") }
    if spec == nil { return nil, status.Error(codes.InvalidArgument, "spec required") }
    gen, err := s.store.Put(ctx, store.ObjectKey{Namespace: store.DefaultNamespace, Kind: kind, Name: name}, spec, expected)
    if err != nil {
        if errors.Is(err, store.ErrGenerationMismatch) {
            return nil, status.Errorf(codes.FailedPrecondition, "%v", err)
        }
        return nil, status.Errorf(codes.Internal, "%v", err)
    }
    return &dashcenterv1.Ack{Accepted: true, Generation: gen}, nil
}
```

| RPC | Implementation |
|---|---|
| `PutInventory` | loop over `req.Dpus`, validate id+endpoint, `inv.Register(...)` |
| `RegisterDpu` | validate, `inv.Register(...)` |
| `DeregisterDpu` | `inv.Deregister(...)`; ErrNotFound → `codes.NotFound` |
| `PutVnet` | `s.put(ctx, "vnet", ...)` |
| `PutEni` | `s.put(ctx, "eni", ...)` |
| `PutVnetMapping` | `s.put(ctx, "vnet_mapping", ...)` |
| `PutAclPolicy` | `s.put(ctx, "acl_policy", ...)` |
| `PutRoutePolicy` | `s.put(ctx, "route_policy", ...)` |
| `PutHaSet` | `s.put(ctx, "ha_set", ...)` |
| `PutServiceTunnel` | Phase 1: return `codes.Unimplemented` |
| `Delete` | `store.Delete(...)`; NotFound → `codes.NotFound` |
| `Get` | `store.Get(...)`; build GetResponse with `kind, name, generation, spec_json` |
| `List` (streaming) | `store.List(...)`, stream `ListItem{kind, name, generation, spec_json}` for each |
| `Reconcile` | `rec.ForceReconcile()`; return `Ack{accepted:true}` |
| `SimulateApply` | Phase 1: return `codes.Unimplemented` |
| `ApplyBatch` (client streaming) | **Phase 1: return `codes.Unimplemented`** — saga-backed implementation in Phase 2-M12. Without transactional guarantees, partial failures leave the fleet inconsistent and clients cannot detect the state. (Review fix [A5]) |

### `observability.go`

| RPC | Implementation |
|---|---|
| `GetDpuStatus` (server streaming) | Phase 1 one-shot: loop `inv.List()`, send DpuStatusReport per DPU, close stream. (Phase 2: live watch.) |
| `GetDrift` (unary) | For each DPU in `inv.List()` (filtered by req.DpuId): `placement.Resolve` + `obs.Diff`, append DriftItems for Add/Update/Remove with appropriate `DriftOp` |
| `GetFlowList`, `GetCounters`, `WatchEvents`, `GetAuditLog`, `GetFlowStats` | Phase 1: return `codes.Unimplemented` |

### `stubs.go`

```go
type diagnosticsStub struct{ dashcenterv1.UnimplementedDiagnosticsServiceServer }
type operationsStub struct{ dashcenterv1.UnimplementedOperationsServiceServer }
type migrationStub struct{ dashcenterv1.UnimplementedMigrationServiceServer }
type haStub struct{ dashcenterv1.UnimplementedHaServiceServer }
```

### Canonical gRPC status code mapping

| Condition | Code |
|---|---|
| Required field empty | `InvalidArgument` |
| Spec body missing | `InvalidArgument` |
| `store.ErrNotFound` | `NotFound` |
| `inventory.ErrNotFound` | `NotFound` |
| `store.ErrGenerationMismatch` | `FailedPrecondition` |
| DPU not in inventory (ENI refs unknown DPU) | `FailedPrecondition` |
| Phase 2 RPC called in Phase 1 | `Unimplemented` |
| Anything unexpected | `Internal` (also logged at Error level) |

### Test cases (`server_test.go`)

Use `bufconn` for in-process gRPC:

1. `PutVnet` stores in backend
2. `PutVnet` empty name → `InvalidArgument`
3. `PutVnet` gen mismatch → `FailedPrecondition`
4. `PutEni` + `Get` round-trip
5. `List` streams all specs
6. `Delete` non-existent → `NotFound`
7. `GetDpuStatus` streams one report per DPU
8. `GetDrift` after convergence → empty items
9. `Reconcile` triggers ForceReconcile (mock recorder)
10. `SimulateApply`, `TriggerSwitchover`, etc. → `codes.Unimplemented`

---

## 15. Step 10 — `internal/server/rest/`

**Package path**: `.../dashd/internal/server/rest`

### Files: `server.go`, `handler.go`, `handler_test.go`

### Type

```go
type Server struct{ srv *http.Server }

func New(st store.DesiredStore, inv *inventory.Inventory, rec *reconciler.Reconciler) *Server {
    h := newHandler(st, inv, rec)
    return &Server{srv: &http.Server{
        Handler:           h.router(),
        ReadHeaderTimeout: 5 * time.Second,
    }}
}
func (s *Server) Serve(addr string) error {
    s.srv.Addr = addr
    return s.srv.ListenAndServe()
}
func (s *Server) Stop() { ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second); defer cancel(); _ = s.srv.Shutdown(ctx) }
```

### Router

Use Go 1.22 pattern-based `http.ServeMux`:
```go
mux := http.NewServeMux()
mux.HandleFunc("PUT /v1/inventory",                  h.putInventory)
mux.HandleFunc("GET /v1/inventory",                  h.getInventory)
mux.HandleFunc("PUT /v1/vnets/{name}",               h.putVnet)
mux.HandleFunc("GET /v1/vnets/{name}",               h.getVnet)
mux.HandleFunc("GET /v1/vnets",                      h.listVnets)
mux.HandleFunc("PUT /v1/enis/{name}",                h.putEni)
mux.HandleFunc("GET /v1/enis/{name}",                h.getEni)
mux.HandleFunc("GET /v1/enis",                       h.listEnis)
mux.HandleFunc("PUT /v1/vnet-mappings/{name}",       h.putVnetMapping)
mux.HandleFunc("PUT /v1/acl-policies/{name}",        h.putAclPolicy)
mux.HandleFunc("PUT /v1/route-policies/{name}",      h.putRoutePolicy)
mux.HandleFunc("PUT /v1/ha-sets/{name}",             h.putHaSet)
mux.HandleFunc("DELETE /v1/{kind}/{name}",           h.delete)
mux.HandleFunc("POST /v1/reconcile",                 h.reconcile)
```

### JSON marshaling

```go
var (
    marshaler   = protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: false}
    unmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}
)
```

### HTTP status code mapping

| Result | Status | Body |
|---|---|---|
| Success PUT | 200 | `{"accepted":true,"generation":N}` |
| Success GET | 200 | protojson(spec) |
| Success DELETE | 204 | — |
| Success POST /reconcile | 200 | `{"ok":true}` |
| store.ErrNotFound | 404 | `{"error":"not found"}` |
| store.ErrGenerationMismatch | 409 | `{"error":"generation mismatch"}` |
| Validation error | 400 | `{"error":"<msg>"}` |
| Unexpected | 500 | `{"error":"internal"}` |

### Handler pattern

```go
func (h *handler) putVnet(w http.ResponseWriter, r *http.Request) {
    name := r.PathValue("name")
    body, err := io.ReadAll(r.Body)
    if err != nil { writeErr(w, 400, err); return }
    spec := &dashcenterv1.VnetSpec{}
    if err := unmarshaler.Unmarshal(body, spec); err != nil {
        writeErr(w, 400, fmt.Errorf("invalid spec: %w", err)); return
    }
    gen, err := h.store.Put(r.Context(), store.ObjectKey{Namespace: store.DefaultNamespace, Kind: "vnet", Name: name}, spec, 0)
    if err != nil {
        if errors.Is(err, store.ErrGenerationMismatch) { writeErr(w, 409, err); return }
        writeErr(w, 500, err); return
    }
    writeJSON(w, 200, map[string]any{"accepted": true, "generation": gen})
}
```

### Test cases (`handler_test.go`)

`httptest.NewServer`:

1. PUT /v1/vnets/v1 with `{"vni":100}` → 200; GET returns same
2. PUT with malformed JSON → 400
3. GET non-existent → 404
4. DELETE → 204
5. POST /reconcile → 200 + mock ForceReconcile called
6. GET /v1/inventory → lists registered DPUs

---

## 16. Step 11 — `internal/server/admin/`

**Package path**: `.../dashd/internal/server/admin`

### Files: `server.go`, `server_test.go`

### Endpoints

| Method | Path | Response |
|---|---|---|
| GET | `/admin/health` | `{"status":"ok\|degraded","leader":true,"dpus":[{id,state,last_seen}]}` |
| GET | `/admin/leader` | `{"leader":true}` (Phase 1: always true) |
| GET | `/admin/inventory` | `inv.List()` as JSON |
| GET | `/admin/desired?kind=<kind>` | `store.List(kind)` as JSON |
| GET | `/admin/observed?dpu=<id>` | `obs.GetDpu(id)` as list of protojson |
| GET | `/admin/drift?dpu=<id>` | computed drift (omit `dpu=` for all DPUs) |
| POST | `/admin/reconcile` | `{"ok":true}` |

### `health` implementation

```go
type dpu struct {
    ID, State, LastSeen string
}
out := struct {
    Status string `json:"status"`
    Leader bool   `json:"leader"`
    Dpus   []dpu  `json:"dpus"`
}{Status: "ok", Leader: true}

allOk := true
for _, e := range h.inv.List() {
    out.Dpus = append(out.Dpus, dpu{
        ID: e.ID,
        State: e.State.String(),
        LastSeen: formatTime(e.LastSeen),
    })
    if e.State != dashcenterv1.DpuState_DPU_STATE_ONLINE { allOk = false }
}
if !allOk { out.Status = "degraded" }
writeJSON(w, 200, out)
```

Health always returns 200 (degraded is a body field, not an HTTP status) so kubelet-style probes succeed unless dashd itself is down.

### Test cases

1. All ONLINE → status: ok
2. One OFFLINE → status: degraded
3. inventory lists all DPUs
4. desired without `kind=` → 400
5. observed without `dpu=` → 400
6. drift after convergence → empty items
7. reconcile triggers ForceReconcile (mock recorder)

---

## 17. Step 12 — `cmd/dashd/main.go`

Replace the 22-line scaffold with the full bootstrap from LLD §21.

### Flags

```go
configPath := flag.String("config", "/etc/dashd/dashd.yaml", "path to config YAML")
showVer    := flag.Bool("version", false, "print version and exit")
flag.Parse()
```

### Bootstrap sequence

1. Print version and exit if `--version`
2. Load config (use `Default()` if file missing — log warning)
3. Initialize `slog` with JSON or text handler, set as default
4. Open store: `filstore.Open(cfg.Storage.File.StateDir)`
5. Build inventory: `inventory.New()`; if `cfg.Inventory.Source == "file"`: `LoadFromFile`
6. Create `obs := model.NewObsCache()`
7. Create `mgr := dispatch.New(obs, &cfg.Reconcile)`; `mgr.SetStore(st)`; `mgr.SetInventory(inv)`
8. Create `rec := reconciler.New(st, inv, mgr, cfg.Reconcile.TickInterval)`
9. Create servers:
   * `grpcSrv := grpcserver.New(st, inv, rec, obs)`
   * `restSrv := restserver.New(st, inv, rec)`
   * `adminSrv := adminserver.New(inv, st, obs, rec)`
10. Create `prober := inventory.NewProber(inv, 10*time.Second)` and `pumpSet := subscribe.NewSet(obs, mgr.DirtyC())`
11. Hook inventory subscription:
    ```go
    inv.Subscribe(func(dpuID, endpoint string, removed bool) {
        if removed {
            pumpSet.Stop(dpuID); mgr.RemoveWorker(dpuID)
        } else {
            pumpSet.Start(rootCtx, dpuID, endpoint); mgr.EnsureWorker(rootCtx, dpuID, endpoint)
        }
    })
    ```
12. Create root context with signal: `signal.NotifyContext(context.Background(), SIGINT, SIGTERM)`
13. Launch all goroutines via `sync.WaitGroup`:
    * `grpcSrv.Serve(...)` — on error: `cancel()`
    * `restSrv.Serve(...)` — same (ignore `http.ErrServerClosed`)
    * `adminSrv.Serve(...)` — same
    * `prober.Run(ctx)`
    * `mgr.Start(ctx)` (synchronous wiring; spawns one goroutine per DPU internally)
    * For each currently-registered DPU: `pumpSet.Start(ctx, ...)` and `mgr.EnsureWorker(...)` (Subscribe hook handles later additions)
    * `rec.Run(ctx)`
14. Log "dashd ready" with addresses
15. `<-ctx.Done()` — wait for signal
16. Graceful shutdown in order: `grpcSrv.Stop()`, `restSrv.Stop()`, `adminSrv.Stop()`, `pumpSet.StopAll()`, `mgr.Stop()`, `st.Close()`
17. `wg.Wait()`; log "dashd stopped"

### Log level helper

```go
func parseLogLevel(s string) slog.Level {
    switch s {
    case "debug": return slog.LevelDebug
    case "warn":  return slog.LevelWarn
    case "error": return slog.LevelError
    default:      return slog.LevelInfo
    }
}
```

---

## 18. Complete file tree (Phase 1)

```
src/impl-go/dashd/
├── cmd/dashd/
│   └── main.go                          [Step 12]
├── configs/
│   ├── dashd.example.yaml               [Step 0 — rewritten]
│   └── inventory.example.yaml           [Step 0 — new]
├── impl-plan-basic.md                   [this file]
├── impl-plan-advanced.md                [Phase 2 plan]
├── internal/
│   ├── README.md                        [Step 0 — rewritten]
│   ├── config/
│   │   ├── config.go                    [Step 1]
│   │   └── config_test.go               [Step 1]
│   ├── store/
│   │   ├── store.go                     [Step 2]
│   │   └── file/
│   │       ├── file.go                  [Step 2]
│   │       └── file_test.go             [Step 2]
│   ├── inventory/
│   │   ├── inventory.go                 [Step 3]
│   │   ├── inventory_test.go            [Step 3]
│   │   ├── probe.go                     [Step 3]
│   │   └── probe_test.go                [Step 3]
│   ├── model/
│   │   ├── types.go                     [Step 4]
│   │   ├── obs_cache.go                 [Step 4]
│   │   └── obs_cache_test.go            [Step 4]
│   ├── placement/
│   │   ├── placement.go                 [Step 5]
│   │   ├── translate.go                 [Step 5]
│   │   ├── order.go                     [Step 5]
│   │   ├── placement_test.go            [Step 5]
│   │   ├── translate_test.go            [Step 5]
│   │   └── order_test.go                [Step 5]
│   ├── subscribe/
│   │   ├── pump.go                      [Step 6]
│   │   └── pump_test.go                 [Step 6]
│   ├── dispatch/
│   │   ├── manager.go                   [Step 7]
│   │   ├── worker.go                    [Step 7]
│   │   ├── ratelimit.go                 [Step 7]
│   │   └── worker_test.go               [Step 7]
│   ├── reconciler/
│   │   ├── reconciler.go                [Step 8]
│   │   └── reconciler_test.go           [Step 8]
│   └── server/
│       ├── grpc/
│       │   ├── server.go                [Step 9]
│       │   ├── interceptors.go          [Step 9]
│       │   ├── control_plane.go         [Step 9]
│       │   ├── observability.go         [Step 9]
│       │   ├── stubs.go                 [Step 9]
│       │   └── server_test.go           [Step 9]
│       ├── rest/
│       │   ├── server.go                [Step 10]
│       │   ├── handler.go               [Step 10]
│       │   └── handler_test.go          [Step 10]
│       └── admin/
│           ├── server.go                [Step 11]
│           └── server_test.go           [Step 11]
```

~32 source files, ~4,500–5,500 LoC including tests.

---

## 19. Go module dependencies

Add to `src/impl-go/dashd/go.mod`:

```go
require (
    google.golang.org/grpc v1.62.0
    google.golang.org/protobuf v1.33.0
    gopkg.in/yaml.v3 v3.0.1
    golang.org/x/time v0.5.0   // rate limiter

    // Reused workspace modules (already in go.work):
    github.com/rashmirrout/DashCenter/src/impl-go/gen/go v0.0.0
    github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime v0.0.0
    github.com/rashmirrout/DashCenter/src/impl-go/dash-sim-client v0.0.0
)

// Testing only:
require (
    go.uber.org/goleak v1.3.0
)
```

Run `go mod tidy` after each step that introduces a new import.

---

## 20. Quality gates

All must pass before Phase 1 is declared done:

| # | Gate | Verification |
|---|---|---|
| 1 | Builds | `cd src/impl-go/dashd && go build ./...` zero errors |
| 2 | No vet warnings | `go vet ./...` zero warnings |
| 3 | Tests pass with race | `go test -race ./...` all pass |
| 4 | No goroutine leaks | tests using `goleak.VerifyNone(t)` pass |
| 5 | Placement scenarios | all 11 `placement_test.go` scenarios pass |
| 6 | Translation completeness | round-trip protoequal tests pass for all 6 spec kinds |
| 7 | Store restart-survival | put 3 specs, close, reopen → all recovered |
| 8 | Store concurrent safety | 100-goroutine concurrent Put test passes |
| 9 | gRPC end-to-end | PutVnet via gRPC → store has spec |
| 10 | REST end-to-end | PUT /v1/vnets/v1 → GET returns same |
| 11 | Integration with dash-sim | declared ENI converges within 5s |
| 12 | Edit re-converges | spec change re-applied within 5s of next event/tick |
| 13 | Drift returns empty | post-convergence /admin/drift?dpu=... has `items:[]` |
| 14 | Health endpoint | /admin/health returns 200 with all DPU states |
| 15 | Graceful shutdown | SIGTERM completes in < 10s; no leaked goroutines |

---

## 21. Implementation order & checkpoints

Implement strictly in this order. After each checkpoint, run the listed verification before moving on.

| Step | After this, verify |
|---|---|
| 0 (cleanup) | `go build ./...` succeeds with retired stubs |
| 1 (config) | `go test ./internal/config/...` passes |
| 2 (store) | `go test -race ./internal/store/...` passes |
| 3 (inventory) | `go test -race ./internal/inventory/...` passes (probe tests need fake server) |
| 4 (model) | `go test -race ./internal/model/...` passes |
| 5 (placement) | `go test ./internal/placement/...` — all scenarios |
| 6 (subscribe) | `go test -race ./internal/subscribe/...` |
| 7 (dispatch) | `go test -race ./internal/dispatch/...` |
| 8 (reconciler) | `go test -race ./internal/reconciler/...` |
| 9 (grpc server) | `go test -race ./internal/server/grpc/...` (uses bufconn) |
| 10 (rest server) | `go test -race ./internal/server/rest/...` (uses httptest) |
| 11 (admin server) | `go test -race ./internal/server/admin/...` |
| 12 (main.go) | `go build ./cmd/dashd` succeeds; run integration scenario (§ 1) |

**At any failure**: do NOT proceed to the next step. Re-read the relevant section of this plan, check the ground-truth proto definitions in `proto/dashcenter/v1/`, and fix.

**Final**: run all 15 quality gates. Tag the commit `dashd-phase1-complete`. Phase 2 ([`impl-plan-advanced.md`](impl-plan-advanced.md)) starts only after this tag.

---

## Annex A — Controller Invariants (Lessons from Kubernetes)

> **Review fix [D]:** These four invariants are stated explicitly so that every
> Phase 1 and Phase 2 package is reviewed against them. They are the
> difference between "a controller that works" and "a controller that
> survives 18 months of production."

The dashd reconcile loop deliberately mirrors `kube-controller-manager`.
Every package MUST respect these four invariants:

### Invariant 1 — Reconcile is level-driven, not edge-driven

The dirty channel may drop events; the 30s tick re-reconciles regardless.
The reconcile function must compute the **full diff** from current state,
not depend on seeing every individual change event. `EventResync` is
the explicit signal that events were lost — but even without it, the
30s tick ensures convergence.

### Invariant 2 — Reconcile is idempotent

Every `reconcilePass` produces the same result for the same
(desired-spec, observed-state) pair. Optimistic cache update +
subscribe push means a repeat call is a no-op when state has converged.
No reconciler should have side effects beyond Apply/Delete on the DPU.

### Invariant 3 — Reconcile is per-DPU isolated

One worker goroutine per DPU, rate-limited, with its own error budget.
No per-DPU failure affects another DPU. The quarantine (DEGRADED state)
stops a bad DPU from exhausting the cluster's control-plane capacity.

### Invariant 4 — Reconcile is observable

> ⚠ **This was the gap identified in the review (§A6).**

Every reconcile pass, every Apply/Delete, every DPU state change, and
every subscribe event drop MUST be metered. Phase 1 does not ship a
full `internal/metrics/` package (that is a Phase 2 hardening item),
but the following counters/gauges SHOULD be tracked from day one via
`slog` structured fields at minimum, and promoted to Prometheus metrics
in Phase 2:

| Metric | Type | Labels |
|---|---|---|
| `dashd_reconcile_total` | counter | `dpu`, `outcome` (ok/error) |
| `dashd_apply_total` | counter | `dpu`, `kind`, `outcome` |
| `dashd_apply_duration_seconds` | histogram | `dpu`, `kind` |
| `dashd_dpu_state` | gauge | `dpu`, `state` |
| `dashd_store_objects` | gauge | `kind` |
| `dashd_reconcile_lag_seconds` | gauge | `dpu` |
| `dashd_leader_role` | gauge | — (1=leader, 0=follower) |
| `dashd_subscribe_events_dropped_total` | counter | `dpu`, `reason` |

Phase 2 adds a formal `internal/metrics/` package with a Prometheus
registry, gRPC server interceptors (latency / total / in-flight), and
exposes all metrics on `:7443/metrics`.

---

## Annex B — Review Response Tracker

This plan has been updated in response to the architectural review at
[`docs/dashd-impl-plan-review.md`](../../docs/dashd-impl-plan-review.md).
The following table maps each review item to the fix applied:

| Review item | Status | Where fixed |
|---|---|---|
| **A1** — Namespace forklift | ✅ Fixed | §7 `ObjectKey` includes `Namespace` from Phase 1 |
| **A2** — Store contract godoc | ✅ Fixed | §7 `Watch()` contract documents strictest semantics |
| **A4** — Controller-runtime | 📋 Deferred to Phase 2 | Annex A documents invariants; formal `internal/controller/` is Phase 2 scope |
| **A5** — ApplyBatch gap | ✅ Fixed | §14 `ApplyBatch` → `codes.Unimplemented` in Phase 1 |
| **A6** — Observability gap | ✅ Documented | Annex A §Invariant 4 + metric table; formal `internal/metrics/` is Phase 2 |
| **B1** — EtcdRevision field | ✅ Fixed | §7 `StoredSpec.EtcdRevision` added |
| **B2** — EventResync sentinel | ✅ Fixed | §7 `EventResync` type + broadcast logic |
| **C1** — Rewrite README.md | ✅ Fixed | §2 + §5 Step 0 includes top-level README rewrite |
| **C2** — Deferred list alignment | ✅ Fixed | §1 "does NOT deliver" aligned with Phase 2 |
| **D** — Controller invariants annex | ✅ Fixed | Annex A |
| **A3, A7, B3–B9, C3** | 📋 Applied to Phase 2 plan | See `impl-plan-advanced.md` |
