# dashd — Phase 2 (Advanced) Implementation Plan

> **Status**: Authoritative implementation guide for Phase 2.
> **Audience**: Any engineer or AI agent extending dashd with HA, ENI live migration, operations, security, and diagnostics.
> **Prerequisite**: [`impl-plan-basic.md`](impl-plan-basic.md) — every Phase 1 quality gate must pass before any Phase 2 module begins.
> **Ground truth**: derived from [`specs/LLD/dashd.md`](../LLD/dashd.md), [`proto/dashcenter/v1/`](../../proto/dashcenter/v1/), and the production-gap audit in [`docs/dash-sim-on-par-with-sonic-audit.md`](../../docs/dash-sim-on-par-with-sonic-audit.md). If this plan conflicts with any of those, those documents win.
> **Review response**: This plan incorporates feedback from [`docs/dashd-impl-plan-review.md`](../../docs/dashd-impl-plan-review.md). Changes are tagged `[Review fix ...]` inline.

## Table of contents

1. [Overview & exit criteria](#1-overview--exit-criteria)
2. [Module inventory](#2-module-inventory)
3. [Phase 1 → Phase 2 architecture deltas](#3-phase-1--phase-2-architecture-deltas)
4. [Updated package dependency graph](#4-updated-package-dependency-graph)
5. [P2-M1 — `internal/store/etcd/`](#5-p2-m1--internalstoreetcd)
6. [P2-M2 — `internal/ha/leader/`](#6-p2-m2--internalhaleader)
7. [P2-M3 — `internal/namespace/`](#7-p2-m3--internalnamespace)
8. [P2-M4 — `internal/capacity/`](#8-p2-m4--internalcapacity)
9. [P2-M5 — `internal/schema/`](#9-p2-m5--internalschema)
10. [P2-M6 — `internal/ha/orchestrator/`](#10-p2-m6--internalhaorchestrator)
11. [P2-M7 — `internal/migration/`](#11-p2-m7--internalmigration)
12. [P2-M8 — `internal/operations/`](#12-p2-m8--internaloperations)
13. [P2-M9 — `internal/auth/`](#13-p2-m9--internalauth)
14. [P2-M10 — `internal/audit/`](#14-p2-m10--internalaudit)
15. [P2-M11 — `internal/flow/`](#15-p2-m11--internalflow)
16. [P2-M12 — `internal/saga/` + `internal/api/gnmi/`](#16-p2-m12--internalsaga--internalapignmi)
17. [Phase 2 file tree](#17-phase-2-file-tree)
18. [Module dependency additions](#18-module-dependency-additions)
19. [Quality gates](#19-quality-gates)
20. [Implementation order & milestones](#20-implementation-order--milestones)

---

## 1. Overview & exit criteria

### What Phase 2 delivers

A production-ready dashd that:
* **Multi-node HA** with etcd-backed leader election (followers serve read-only)
* **Multi-tenancy** — every spec scoped to a namespace; cross-namespace refs rejected
* **Capacity admission** — `RESOURCE_EXHAUSTED` if exceeding DPU `DpuCapacityLimits`
* **Schema/capability gating** — `PutServiceTunnel` only on DPUs advertising the capability
* **HA orchestration** — full `HaService` with planned switchover and unplanned failover
* **ENI live migration** — full 10-phase state machine with rollback
* **Operations** — cordon, drain (parallel migration of all ENIs), full transactional `ApplyBatch`
* **Security** — TLS / mTLS, token-based RBAC (viewer/operator/admin), append-only audit log
* **Counter & event streaming** — `GetCounters`, `GetAuditLog`, `WatchEvents` all server-streaming
* **Diagnostics** — `TraceFlow`, `ExplainMatch`, `GetAclHitStats`, `TriggerResimulation`
* **Saga coordinator** — atomic cross-DPU rollback on partial batch failure
* **gNMI shim** — minimal gNMI Subscribe bridge to `WatchEvents`

### Out of scope (deferred)

* OIDC/AAD integration (stub interface only — production replaces via implementation swap)
* PostgreSQL or other non-etcd backends
* WebUI (the proto surface is consumed externally)
* gNMI Get/Set (only Subscribe is bridged)

### Exit criteria (must all pass)

```bash
# 1. All Phase 1 gates still pass (no regression)
cd src/impl-go/dashd
go test -race ./...

# 2. 3-node dashd cluster with etcd
# leader-kill test: kill leader → new leader resumes reconciliation in <15s
docker-compose -f deploy/compose/dashd-3node.yaml up
# (separate terminal)
dashctl leader-kill && dashctl drift   # converges within 15s

# 3. Namespace isolation
dashctl eni put eni-1 --namespace tenant-A --dpu dpu-0 ...   # ok
dashctl eni get eni-1 --namespace tenant-B                   # NOT_FOUND

# 4. Capacity admission
# DPU advertises max_enis=10; fill to 10 + try one more
for i in $(seq 1 11); do dashctl eni put eni-$i --dpu dpu-0 ...; done
# 11th returns RESOURCE_EXHAUSTED

# 5. ENI live migration — full 10 phases
dashctl migration create --eni eni-1 --from dpu-0 --to dpu-1 --strategy NEW_FLOWS_FIRST_DRAIN
SID=$(dashctl migration list --eni eni-1 -o id)
for phase in VALIDATED INITIALIZED DUAL_WRITE FLOW_DRAIN CUTOVER VERIFICATION CLEANUP COMMITTED; do
  dashctl migration advance --session $SID --to $phase
done
# Final: eni-1 lives only on dpu-1

# 6. Rollback test
dashctl migration create --eni eni-2 --from dpu-0 --to dpu-1
SID=$(dashctl migration list --eni eni-2 -o id)
dashctl migration advance --session $SID --to FLOW_DRAIN
dashctl migration rollback --session $SID
# eni-2 still on dpu-0; nothing on dpu-1

# 7. Drain DPU
# 5 ENIs on dpu-0
dashctl dpu drain dpu-0 --watch
# Streams progress: PLANNING → MIGRATING → DRAINING → COMPLETE
# All 5 ENIs now on other DPUs; dpu-0 state == CORDONED

# 8. TLS + RBAC
# Bad token → UNAUTHENTICATED
grpcurl -H "Authorization: Bearer badtoken" ... PutVnet
# Viewer token can't write → PERMISSION_DENIED
grpcurl -H "Authorization: Bearer viewer-token" ... PutVnet

# 9. Audit log
dashctl audit tail            # streams entries in real time as RPCs happen

# 10. TraceFlow
# Configure ACL rule denying 10.0.0.1:80 on eni-1
dashctl trace-flow --src 10.0.0.1 --dst 10.0.0.2 --dport 80 --eni eni-1
# Returns verdict: DENIED, stage: ACL_INBOUND, matched_rule: <id>

# 11. Saga rollback
# ApplyBatch of 10 objects; force #5 to fail (intentional bad spec)
dashctl apply-batch --file fail-test.yaml
# All 10 rolled back; observable state shows none of them applied

# 12. gNMI subscribe
gnmic --target localhost:9443 subscribe --path /dashcenter/v1/events
# (separate terminal) dashctl vnet put vnet-99 → gnmic receives Notification
```

If all 12 scenarios pass, Phase 2 is complete.

---

## 2. Module inventory

| # | Module | Package | Key new RPCs | Estimated time |
|---|---|---|---|---|
| P2-M1 | etcd backend | `store/etcd/` | (infrastructure) | 1 week |
| P2-M2 | Leader election | `ha/leader/` | (infrastructure) | 1 week |
| P2-M3 | Multi-tenancy | `namespace/` | namespace field enforcement on all RPCs | 1.5 weeks |
| P2-M4 | Capacity admission | `capacity/` | `SimulateApply` (full), capacity errors | 1 week |
| P2-M5 | Schema negotiation | `schema/` | `PutServiceTunnel` (gated) | 0.5 week |
| P2-M6 | HA orchestration | `ha/orchestrator/` | full `HaService` (6 RPCs) | 1.5 weeks |
| P2-M7 | ENI live migration | `migration/` | full `MigrationService` (12 RPCs) | 2 weeks |
| P2-M8 | Operations | `operations/` | `CordonDpu`, `UncordonDpu`, `DrainDpu` | 1 week |
| P2-M9 | Security | `auth/` | TLS/mTLS, token→role | 1 week |
| P2-M10 | Audit + counters | `audit/` | `GetAuditLog`, `GetCounters` | 1 week |
| P2-M11 | Diagnostics | `flow/` | `TraceFlow`, `ExplainMatch`, `TriggerResimulation` | 1.5 weeks |
| P2-M12 | Saga + gNMI | `saga/`, `api/gnmi/` | `WatchEvents` (full), gNMI Subscribe bridge | 1 week |

**Total**: 14 weeks of solo engineering time. Modules within the same milestone group (e.g., M3–M5) can be parallelized.

---

## 3. Phase 1 → Phase 2 architecture deltas

### Where the new modules plug in

```
┌──────────────────────────────────────────────────────────────────────┐
│                        dashd process (Phase 2)                       │
│                                                                      │
│  TLS+mTLS + auth interceptor  ◄──── NEW (P2-M9)                      │
│  on gRPC :9443 / REST :8443                                          │
│                                                                      │
│   ┌──────────────────┐         ┌──────────────────────┐              │
│   │ server/rest      │◄────────│ namespace gate       │  NEW (P2-M3) │
│   │ server/grpc      │         │ capacity gate        │  NEW (P2-M4) │
│   └────────┬─────────┘         │ schema gate          │  NEW (P2-M5) │
│            │ writes             └──────────┬───────────┘              │
│            ▼                               │ accepted writes          │
│   ┌──────────────────────────────────────┐ │                          │
│   │ store/etcd  (replaces store/file in  │◄┘   NEW (P2-M1)            │
│   │  prod; file backend stays for dev)   │                            │
│   └──────────────────────────────────────┘                            │
│            │                                                          │
│            │ Watch                                                    │
│            ▼                                                          │
│   ┌──────────────────┐    ┌─────────────────────┐                     │
│   │ reconciler       │    │ ha/leader           │  NEW (P2-M2)        │
│   │ (only runs on    │◄───│ (etcd lease;        │  bootstrap/teardown │
│   │  leader)         │    │  followers RO)      │  reconciler on      │
│   └──────────────────┘    └─────────────────────┘  leader change      │
│            │                                                          │
│            ▼                                                          │
│   ┌──────────────────────────────────────┐                            │
│   │ dispatch                             │                            │
│   │  + saga coordinator (for ApplyBatch) │◄──── NEW (P2-M12)          │
│   │  + counter polling per DPU            │◄──── NEW (P2-M10)          │
│   └──────────────────────────────────────┘                            │
│                                                                       │
│   New gRPC service implementations:                                   │
│   ┌──────────────────────────────────────┐                            │
│   │ ha/orchestrator (HaService)          │  NEW (P2-M6)               │
│   │ migration (MigrationService)         │  NEW (P2-M7)               │
│   │ operations (OperationsService)       │  NEW (P2-M8)               │
│   │ flow (DiagnosticsService)            │  NEW (P2-M11)              │
│   │ api/gnmi (gNMI bridge)               │  NEW (P2-M12)              │
│   └──────────────────────────────────────┘                            │
│                                                                       │
│   New observability/audit:                                            │
│   ┌──────────────────────────────────────┐                            │
│   │ audit (append-only log + tail-follow)│  NEW (P2-M10)              │
│   │ event broadcaster (WatchEvents fan)  │  NEW (P2-M12)              │
│   └──────────────────────────────────────┘                            │
└──────────────────────────────────────────────────────────────────────┘
```

### Files modified in Phase 2 (not just additions)

| Phase 1 file | Modification |
|---|---|
| `internal/config/config.go` | Add `Storage.Etcd`, `HA`, `Auth`, `Audit` config sections |
| `internal/store/store.go` | No interface change — **Phase 1 already includes `Namespace` on `ObjectKey` and `List(ctx, namespace, kind)` signature** (Review fix [A1]). Phase 2 adds *behavioral* enforcement only. |
| `internal/server/grpc/server.go` | Replace `Unimplemented` stubs with real implementations from new packages |
| `internal/server/grpc/control_plane.go` | Add namespace/capacity/schema gate wiring; `SimulateApply` impl; `PutServiceTunnel` impl; saga-backed `ApplyBatch` |
| `internal/server/grpc/observability.go` | `GetDpuStatus` becomes long-lived stream; `GetCounters` / `GetAuditLog` / `WatchEvents` implemented |
| `internal/server/grpc/interceptors.go` | Add auth interceptor (TLS extracted in `server.go` setup) |
| `internal/dispatch/manager.go` | Add saga path; counter polling per worker |
| `internal/inventory/probe.go` | Add capability negotiation on first successful probe |
| `cmd/dashd/main.go` | Add HA loop, TLS loading, auth wiring |

---

## 4. Updated package dependency graph

```
                    config (+ Etcd/HA/Auth/Audit sections)
                          │
                          │
  ┌───────────────────────┼──────────────────┬──────────────────┐
  │                       │                  │                  │
store/file              store/etcd          inventory         model
  └──────────┬────────────┘  NEW
             │                                 │                │
             └────────────┬────────────────────┘                │
                          │                                     │
                          │   ┌─────────────────────────────────┘
                          │   │
                  ┌───────▼───▼─┐
                  │  placement  │ (Phase 2: + service_tunnel translator)
                  └──────┬──────┘
                         │
   ┌─────────────────────┼─────────────────────────────────────┐
   │                     │                                     │
subscribe              dispatch                            reconciler
                          │
                          │
                  ┌───────┴────────┐
                  │ saga (P2-M12)  │   for ApplyBatch
                  └────────────────┘
                          │
   ┌──────────────────────┼──────────────────────┐
   │                      │                      │
  ha/leader            namespace          ha/orchestrator     migration
  (P2-M2)              capacity           (P2-M6)              (P2-M7)
                       schema
                       (P2-M3-5)
                                          operations              flow
                                          (P2-M8)              (P2-M11)
                                                                   │
   ┌──────────────────────┬──────────────────────────────┬─────────┘
   │                      │                              │
  auth                  audit                       server/grpc / rest / admin
  (P2-M9)               (P2-M10)                    api/gnmi (P2-M12)
                                                    (real impls replace stubs)
                                                          │
                                                          ▼
                                                cmd/dashd/main.go
                                              (+ HA loop, TLS, auth)
```

---

## 5. P2-M1 — `internal/store/etcd/`

**Purpose**: Production-grade desired-state backend. Implements the same `store.DesiredStore` interface introduced in Phase 1.

### Files

```
internal/store/etcd/
├── etcd.go
├── etcd_test.go
└── compaction.go        # background compaction policy
```

### Config additions

In `internal/config/config.go`, extend `StorageConfig`:
```go
type StorageConfig struct {
    Backend string          `yaml:"backend"` // "file" | "etcd"
    File    FileStoreConfig `yaml:"file"`
    Etcd    EtcdStoreConfig `yaml:"etcd"`   // NEW
}

type EtcdStoreConfig struct {
    Endpoints       []string      `yaml:"endpoints"`        // e.g. ["etcd-0:2379", "etcd-1:2379"]
    DialTimeout     time.Duration `yaml:"dial_timeout"`     // default 5s
    RequestTimeout  time.Duration `yaml:"request_timeout"`  // default 10s
    Username        string        `yaml:"username"`         // optional
    Password        string        `yaml:"password"`         // optional
    TLSCertFile     string        `yaml:"tls_cert_file"`    // optional
    TLSKeyFile      string        `yaml:"tls_key_file"`     // optional
    TLSCAFile       string        `yaml:"tls_ca_file"`      // optional
    KeyPrefix       string        `yaml:"key_prefix"`       // default "/dashd/"
}
```

Validation: when `Backend == "etcd"`, `Endpoints` must be non-empty.

### Key layout in etcd

```
<prefix>desired/<kind>/<name>      → StoredSpec JSON
<prefix>migrations/<session_id>    → MigrationSession JSON (P2-M7)
<prefix>sagas/<saga_id>            → SagaState JSON (P2-M12)
<prefix>leader                     → leader lease (P2-M2)
```

Namespace (P2-M3) inserts a level: `<prefix>desired/<namespace>/<kind>/<name>`.

### Type

```go
package etcd

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "path"
    "sync"
    "time"

    clientv3 "go.etcd.io/etcd/client/v3"
    "google.golang.org/protobuf/encoding/protojson"
    "google.golang.org/protobuf/proto"

    "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

type EtcdStore struct {
    cli       *clientv3.Client
    prefix    string         // e.g. "/dashd/desired/"
    reqTO     time.Duration
}

// Open dials the cluster and returns an EtcdStore.
func Open(cfg EtcdConfig) (*EtcdStore, error)

// Implements store.DesiredStore — same interface as store/file.
func (s *EtcdStore) Put(ctx context.Context, key store.ObjectKey, spec proto.Message, expectedGeneration int64) (int64, error)
func (s *EtcdStore) Delete(ctx context.Context, key store.ObjectKey) error
func (s *EtcdStore) Get(ctx context.Context, key store.ObjectKey) (*store.StoredSpec, error)
func (s *EtcdStore) List(ctx context.Context, kind string) ([]*store.StoredSpec, error)
func (s *EtcdStore) Watch(ctx context.Context) (<-chan store.DesiredEvent, error)
func (s *EtcdStore) Close() error
```

### Implementation rules

**Key encoding**:
```go
func (s *EtcdStore) keyOf(key store.ObjectKey) string {
    return path.Join(s.prefix, key.Kind, key.Name)
}
func (s *EtcdStore) kindPrefix(kind string) string {
    return path.Join(s.prefix, kind) + "/"
}
```

**Generation**: use etcd's `ModRevision` as the generation counter. This is strongly consistent and globally monotonic — no need to maintain a separate counter.

**`Put` with optimistic concurrency**:
```go
encoded, err := protojson.Marshal(spec)
if err != nil { return 0, err }
envelope := envelope{Kind: key.Kind, Name: key.Name, Spec: encoded, UpdatedAt: time.Now()}
body, _ := json.Marshal(envelope)
fullKey := s.keyOf(key)

ctx, cancel := context.WithTimeout(ctx, s.reqTO)
defer cancel()

if expectedGeneration > 0 {
    txn := s.cli.Txn(ctx).
        If(clientv3.Compare(clientv3.ModRevision(fullKey), "=", expectedGeneration)).
        Then(clientv3.OpPut(fullKey, string(body))).
        Else(clientv3.OpGet(fullKey))
    resp, err := txn.Commit()
    if err != nil { return 0, err }
    if !resp.Succeeded {
        return 0, store.ErrGenerationMismatch
    }
    return resp.Responses[0].GetResponsePut().Header.Revision, nil
}

// expected == 0: last-write-wins
resp, err := s.cli.Put(ctx, fullKey, string(body))
if err != nil { return 0, err }
return resp.Header.Revision, nil
```

The `Generation` field returned from etcd is the header's `Revision` — globally unique across the entire etcd keyspace.

**`Delete`**:
```go
ctx, cancel := context.WithTimeout(ctx, s.reqTO)
defer cancel()
resp, err := s.cli.Delete(ctx, s.keyOf(key))
if err != nil { return err }
if resp.Deleted == 0 { return store.ErrNotFound }
return nil
```

**`Get`**:
```go
resp, err := s.cli.Get(ctx, s.keyOf(key))
if err != nil { return nil, err }
if len(resp.Kvs) == 0 { return nil, store.ErrNotFound }
kv := resp.Kvs[0]
var env envelope
if err := json.Unmarshal(kv.Value, &env); err != nil { return nil, err }
return &store.StoredSpec{
    Key:        key,
    Generation: kv.ModRevision,
    Data:       env.Spec,
    UpdatedAt:  env.UpdatedAt,
}, nil
```

**`List`**:
```go
resp, err := s.cli.Get(ctx, s.kindPrefix(kind), clientv3.WithPrefix(), clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend))
// decode each kv into StoredSpec; sorted by key (which includes name)
```

**`Watch`**: use etcd's prefix watch. The snapshot-first contract from Phase 1 is preserved:
```go
func (s *EtcdStore) Watch(ctx context.Context) (<-chan store.DesiredEvent, error) {
    ch := make(chan store.DesiredEvent, 256)

    // 1. Snapshot: List the full prefix at a known revision; emit EventPut for each.
    snap, err := s.cli.Get(ctx, s.prefix, clientv3.WithPrefix())
    if err != nil { return nil, err }
    go func() {
        defer close(ch)
        for _, kv := range snap.Kvs {
            if ev := s.kvToEvent(store.EventPut, kv); ev != nil {
                select { case ch <- *ev: case <-ctx.Done(): return }
            }
        }
        // 2. Watch from the revision after the snapshot.
        wch := s.cli.Watch(ctx, s.prefix, clientv3.WithPrefix(), clientv3.WithRev(snap.Header.Revision+1))
        for wr := range wch {
            for _, ev := range wr.Events {
                var typ store.EventType
                switch ev.Type {
                case clientv3.EventTypePut:    typ = store.EventPut
                case clientv3.EventTypeDelete: typ = store.EventDelete
                }
                if dev := s.kvToEvent(typ, ev.Kv); dev != nil {
                    select { case ch <- *dev: case <-ctx.Done(): return }
                }
            }
        }
    }()
    return ch, nil
}
```

**Resumability**: if Watch returns a `Compacted` error (revision too old), it must auto-resync by calling Watch again with a fresh snapshot. Implement an outer retry loop with exponential backoff.

### `compaction.go`

Background goroutine that periodically calls `cli.Compact()` with revision = current - retention. Default retention: 24h worth of revisions (rough). Configurable via `EtcdStoreConfig.Retention`.

### Tests

Use `go.etcd.io/etcd/tests/v3/integration` to run an in-process etcd cluster in tests.

1. `Put`/`Get` round-trip
2. `Put` returns monotonically increasing generations
3. `Put` with stale `expectedGeneration` → `ErrGenerationMismatch`
4. `Delete` removes; second Delete → `ErrNotFound`
5. `List` returns sorted by key
6. `Watch` delivers snapshot, then live PUT, then DELETE
7. `Watch` survives compaction (re-syncs)
8. Concurrent `Put`s with same expected generation: only one succeeds
9. `Close` then Put → error (client closed)
10. 100 concurrent Puts with different keys → all succeed; List returns 100

### Wiring in `cmd/dashd/main.go`

```go
var st store.DesiredStore
switch cfg.Storage.Backend {
case "file":
    st, err = filstore.Open(cfg.Storage.File.StateDir)
case "etcd":
    st, err = etcdstore.Open(cfg.Storage.Etcd)
default:
    return fmt.Errorf("unsupported storage backend %q", cfg.Storage.Backend)
}
```

---

## 6. P2-M2 — `internal/ha/leader/`

**Purpose**: etcd-lease leader election. Exactly one dashd instance per cluster runs the reconciler at any moment.

### Files

```
internal/ha/leader/
├── leader.go
├── none.go        # NoneElector for single-node dev
├── etcd.go        # EtcdElector
└── etcd_test.go
```

### Config additions

```go
type Config struct {
    // ... existing fields
    HA HAConfig `yaml:"ha"`   // NEW
}

type HAConfig struct {
    Mode      string        `yaml:"mode"`        // "none" | "etcd"
    LeaseTTL  time.Duration `yaml:"lease_ttl"`   // default 15s
    LeaderKey string        `yaml:"leader_key"`  // default "/dashd/leader"
    NodeID    string        `yaml:"node_id"`     // default: hostname; uniqueness required
}
```

### Interface

```go
package leader

// Elector is the leader-election contract.
type Elector interface {
    // AwaitLeadership blocks until this node wins leadership OR ctx is cancelled.
    // Returns nil on success, ctx.Err() on cancellation, or another error on failure.
    AwaitLeadership(ctx context.Context) error

    // LostLeadership returns a channel that is closed the moment this node
    // loses leadership (lease expired, ctx cancelled, or step-down).
    LostLeadership() <-chan struct{}

    // IsLeader reports current leadership (non-blocking).
    IsLeader() bool

    // LeaderID returns the node ID of the current leader (best-effort).
    // Returns "" if unknown or no leader.
    LeaderID(ctx context.Context) (string, error)

    // Close releases the lease (if held) and stops the election loop.
    // Idempotent.
    Close() error
}
```

### `NoneElector` (single-node)

```go
type NoneElector struct {
    nodeID string
    lost   chan struct{}   // never closed
    closed bool
    mu     sync.Mutex
}

func NewNone(nodeID string) *NoneElector
// AwaitLeadership: returns nil immediately (always leader)
// LostLeadership:  returns a never-closing channel (until Close)
// IsLeader:        true
// LeaderID:        self
// Close:           closes the channel (so AwaitLeadership callers waiting on LostLeadership unblock)
```

### `EtcdElector`

Uses `clientv3/concurrency.Session` + `clientv3/concurrency.Election`:

```go
type EtcdElector struct {
    cli      *clientv3.Client
    sess     *concurrency.Session
    election *concurrency.Election
    nodeID   string
    leaderKey string
    leaseTTL time.Duration
    lost     chan struct{}
    mu       sync.Mutex
    elected  bool
}

func NewEtcd(cli *clientv3.Client, cfg HAConfig) (*EtcdElector, error)
```

**`AwaitLeadership` loop**:
```go
sess, err := concurrency.NewSession(e.cli, concurrency.WithTTL(int(e.leaseTTL.Seconds())))
if err != nil { return err }
e.sess = sess
e.election = concurrency.NewElection(sess, e.leaderKey)

// Block until campaign succeeds (or ctx cancelled)
if err := e.election.Campaign(ctx, e.nodeID); err != nil { return err }

e.elected = true
e.lost = make(chan struct{})

// Goroutine watching session expiry
go func() {
    <-sess.Done()  // closes when lease expires or session closes
    e.mu.Lock()
    e.elected = false
    close(e.lost)
    e.mu.Unlock()
}()

return nil
```

**`LeaderID`**: `e.election.Leader(ctx)` returns the value associated with the leader key, which is `e.nodeID`.

**`Close`**: `e.election.Resign(context.Background())` + `e.sess.Close()` + close `lost` channel if not already closed.

### `main.go` HA loop (replaces the simple single-goroutine reconciler launch)

```go
elector := ha.NewElector(cfg.HA, etcdCli)   // or NoneElector if mode=="none"
defer elector.Close()

for {
    if err := ctx.Err(); err != nil { break }

    slog.Info("ha: awaiting leadership", "node_id", cfg.HA.NodeID)
    if err := elector.AwaitLeadership(ctx); err != nil {
        slog.Warn("ha: await leadership", "err", err)
        time.Sleep(2 * time.Second)
        continue
    }
    slog.Info("ha: became leader")

    // Start leader-only goroutines.
    leaderCtx, leaderCancel := context.WithCancel(ctx)
    mgr.Start(leaderCtx)
    for _, e := range inv.List() {
        pumpSet.Start(leaderCtx, e.ID, e.Endpoint)
        mgr.EnsureWorker(leaderCtx, e.ID, e.Endpoint)
    }
    recDone := make(chan struct{})
    go func() {
        defer close(recDone)
        _ = rec.Run(leaderCtx)
    }()

    // Wait for leadership loss or shutdown.
    select {
    case <-elector.LostLeadership():
        slog.Warn("ha: lost leadership")
    case <-ctx.Done():
        slog.Info("ha: shutdown")
    }

    // Tear down leader-only goroutines.
    leaderCancel()
    <-recDone
    mgr.Stop()
    pumpSet.StopAll()
}
```

### Followers serve read-only

`server/grpc/control_plane.go` gets a leadership check at the top of every mutating handler:
```go
if !s.elector.IsLeader() {
    leader, _ := s.elector.LeaderID(ctx)
    return nil, status.Errorf(codes.Unavailable, "not the leader; current leader=%s", leader)
}
```

`Get`, `List`, `GetDpuStatus`, `GetDrift` are served from the local store on followers — the etcd store is strongly consistent so this is safe.

### Tests

In-process etcd cluster:

1. `NewEtcd` + `AwaitLeadership` succeeds when no other contender
2. Two contenders: only one becomes leader; the other blocks on AwaitLeadership
3. Leader calls `Close()` → contender becomes leader within `2*LeaseTTL`
4. Leader process killed (simulated via session.Close): `LostLeadership` channel fires
5. `IsLeader` reports correctly throughout
6. `LeaderID` returns the elected nodeID
7. `NoneElector`: AwaitLeadership returns nil immediately; IsLeader always true

---

## 7. P2-M3 — `internal/namespace/`

**Purpose**: Multi-tenancy *enforcement*. Every spec is scoped to a namespace; cross-namespace references are rejected.

> **Review fix [A1]:** Phase 1 already includes `Namespace` on `ObjectKey` (defaulting to
> `"default"`) and the on-disk layout is `<state_dir>/<namespace>/<kind>/<name>.json`.
> P2-M3 therefore does **NOT** mutate `ObjectKey` or change the storage layout. It only adds
> *behavioral* enforcement: cross-namespace validation, namespace-scoped RBAC, and
> namespace listing. No migration tool is needed.

### Files

```
internal/namespace/
├── namespace.go
├── validator.go
└── validator_test.go
```

### Concepts

* A **namespace** is an opaque string (e.g. `"tenant-acme"`, `"prod"`, `"default"`). Created implicitly on first write.
* Default namespace: `"default"`.
* Cross-namespace refs are rejected: an ENI in namespace `"A"` cannot reference an AclPolicy in namespace `"B"`.
* RBAC: a principal has a set of allowed namespaces; reading/writing outside is `PermissionDenied`.

### Store layout (no change from Phase 1)

Phase 1 already uses:
```
<state_dir>/<namespace>/<kind>/<name>.json           (file store)
<prefix>desired/<namespace>/<kind>/<name>             (etcd store)
```

`store.ObjectKey` already contains `Namespace`. `store.List` already takes `(ctx, namespace, kind)`.
No breaking changes are introduced in this module — only behavioral enforcement is added.

### Validator API

```go
// Validator enforces namespace rules.
type Validator struct { /* unexported */ }

func New() *Validator

// CheckSpec rejects specs whose embedded references cross namespaces.
// Specifically:
//   - EniSpec.Vnet must exist in the same namespace
//   - EniSpec.AclGroupRefs[i] must exist in the same namespace
//   - EniSpec.RouteGroupRefs[i] must exist in the same namespace
//   - VnetMappingSpec.Vnet must exist in the same namespace
//   - HaSetSpec.MemberDpuIds — DPUs are namespace-agnostic (operator-owned), so no check
//
// CheckSpec calls into store.Get for each reference; ErrNotFound is treated as
// "cross-namespace ref" and returns codes.InvalidArgument.
func (v *Validator) CheckSpec(ctx context.Context, st store.DesiredStore, namespace string, spec proto.Message) error
```

### Plugged into `control_plane.go`

```go
func (s *controlPlaneServer) PutEni(ctx context.Context, req *dashcenterv1.PutEniRequest) (*dashcenterv1.Ack, error) {
    ns := req.GetSpec().GetNamespace()
    if ns == "" { ns = "default" }

    if err := s.nsValidator.CheckSpec(ctx, s.store, ns, req.GetSpec()); err != nil {
        return nil, status.Errorf(codes.InvalidArgument, "namespace: %v", err)
    }

    return s.put(ctx, ns, "eni", req.GetName(), req.GetExpectedGeneration(), req.GetSpec())
}
```

The Phase 1 `put` helper gains a `namespace` argument:
```go
func (s *controlPlaneServer) put(ctx context.Context, ns, kind, name string, expected int64, spec proto.Message) (*dashcenterv1.Ack, error)
```

### Placement and reconciler updates

`placement.DesiredSpecs` becomes namespace-scoped. The reconciler loads specs per namespace and computes placement per namespace. Cross-namespace specs are never merged.

In practice:
* Each placement run is per `(namespace, dpuID)` pair
* The `obs.Diff` is per namespace — a Phase 2 `obs.DiffNamespace(dpuID, namespace, desired)` helper is added
* `inventory` remains global (DPUs are operator-owned, not tenant-owned)

### Tests

1. `CheckSpec` valid (all refs in same namespace) → nil
2. `CheckSpec` ENI referencing AclPolicy in different namespace → error
3. `CheckSpec` ENI referencing non-existent AclPolicy → error
4. Store layout: Put in namespace A; Get in namespace B → NOT_FOUND
5. List filtered by namespace
6. Migration tool: Phase 1 dir → Phase 2 dir with "default" namespace

---

## 8. P2-M4 — `internal/capacity/`

**Purpose**: Reject `Put*` operations that would push a DPU over its `DpuCapacityLimits`.

### Files

```
internal/capacity/
├── capacity.go
├── tracker.go
└── tracker_test.go
```

### Concepts

* `DpuCapacityLimits` is part of `DpuCapabilities` (proto field on `DpuRecord`)
* Each DPU advertises max counts on first probe: `max_enis`, `max_acl_rules`, `max_routes_per_eni`, `max_vnet_mappings`, `max_meter_policies`, etc. (18 fields total — see `types.proto`)
* The capacity tracker maintains in-memory per-DPU counts derived from the desired-state store
* Every `Put*` triggers a capacity recompute for the affected DPU(s) BEFORE the write goes through

### API

```go
type Tracker struct { /* unexported */ }

func New(inv *inventory.Inventory) *Tracker

// Recount rebuilds counts from the current store contents.
// Called once at startup, and after every successful Put/Delete.
func (t *Tracker) Recount(ctx context.Context, st store.DesiredStore, namespace string) error

// Usage returns the current usage for dpuID (across all namespaces — capacity is per-physical-DPU).
func (t *Tracker) Usage(dpuID string) DpuUsage

// CheckPut returns an error if applying spec to namespace would exceed limits.
// Used by control_plane.go before calling store.Put.
func (t *Tracker) CheckPut(ctx context.Context, namespace, kind, name string, spec proto.Message) error

// CheckBatch returns an aggregate ok/over-limit report for a batch of Puts
// (used by SimulateApply for capacity preview).
func (t *Tracker) CheckBatch(ctx context.Context, items []BatchItem) []CapacityReport
```

```go
type DpuUsage struct {
    EnisInUse           int32
    AclRulesInUse       int32
    RoutesInUse         int32
    VnetMappingsInUse   int32
    // ... 14 more fields matching DpuCapacityLimits
}

type CapacityReport struct {
    DpuID     string
    Field     string      // e.g. "max_enis"
    Limit     int32
    Current   int32
    Requested int32
    OK        bool
}
```

### Field-by-kind table

When admission-checking a Put, the tracker increments specific fields based on the spec kind:

| Spec kind | Field(s) consumed | Notes |
|---|---|---|
| `EniSpec` | `max_enis` (+1 on affected DPU) | |
| `VnetMappingSpec` | `max_vnet_mappings` (+1 on each DPU hosting an ENI in that VNET) | |
| `AclPolicySpec` | `max_acl_rules` (+ len(spec.Rules) on each DPU with ENIs referencing this group) | |
| `RoutePolicySpec` | `max_routes_per_eni` × max(ENIs referencing) | |
| `VnetSpec` | `max_vnets` (+1 if vnet is new to that DPU) | |
| `HaSetSpec` | `max_ha_sets` (+1 on each member DPU) | |
| `ServiceTunnelSpec` | `max_service_tunnels` (+1 on each affected DPU) | |

### `CheckPut` algorithm

```
1. Diff the proposed spec against the existing spec (if any) to compute net delta per field.
2. For each (dpuID, field, delta) in the placement-projected delta:
   a. cur := t.usage[dpuID][field]
   b. lim := t.dpuLimits[dpuID][field]
   c. if cur + delta > lim: return RESOURCE_EXHAUSTED with detailed message
3. (Do NOT commit the delta — that happens on Recount after successful Put.)
```

### Plugged into `control_plane.go`

```go
func (s *controlPlaneServer) PutEni(ctx context.Context, req *dashcenterv1.PutEniRequest) (*dashcenterv1.Ack, error) {
    ns := /* ... */
    if err := s.nsValidator.CheckSpec(...); err != nil { return ... }
    if err := s.capacity.CheckPut(ctx, ns, "eni", req.GetName(), req.GetSpec()); err != nil {
        return nil, status.Errorf(codes.ResourceExhausted, "capacity: %v", err)
    }
    ack, err := s.put(ctx, ns, "eni", ...)
    if err == nil { _ = s.capacity.Recount(ctx, s.store, ns) }
    return ack, err
}
```

### `SimulateApply` (full implementation in P2-M4)

`SimulateApplyRequest` contains a batch of proposed Puts/Deletes. The handler:
1. Validates namespace + cross-namespace refs
2. Builds a virtual `DesiredSpecs` snapshot with the proposed changes applied
3. Calls `placement.ResolveAll` against the virtual snapshot
4. Computes capacity diff per DPU
5. Returns `SimulateApplyResponse` with: list of accepted items, list of rejected items (with reason), per-DPU capacity preview

No writes to the store.

### Tests

1. `CheckPut` ENI with capacity remaining → ok
2. `CheckPut` ENI at max → RESOURCE_EXHAUSTED with limit-detail message
3. `CheckPut` ACL policy with 100 rules but only 50 slots free → RESOURCE_EXHAUSTED
4. `Recount` after Put updates Usage correctly
5. `Recount` after Delete decrements
6. Concurrent `CheckPut` calls — atomic snapshot semantics
7. SimulateApply with mixed accept/reject items returns full report

---

## 9. P2-M5 — `internal/schema/`

**Purpose**: Reject Puts for kinds the target DPU does not support.

### Files

```
internal/schema/
├── gate.go
└── gate_test.go
```

### Capability flags (from `DpuCapabilities`)

13 bools and a version field:
* `service_tunnel`, `ipv6`, `eni_live_migration`, `dual_active_ha`, `fast_path_icmp`, `trusted_vni`, `ecmp_route`, `meter_policy`, `gnmi`, `audit_v2`, `flow_export`, `inline_acl_in_pa`, `acl_per_direction`
* `dash_api_schema_version` (uint32)

### Gate API

```go
type Gate struct { /* unexported */ }

func New(inv *inventory.Inventory) *Gate

// CheckKind returns an error if dpuID does not support kind.
// Some kinds require specific capability flags:
//   service_tunnel    → caps.service_tunnel
//   ha_set            → caps.dual_active_ha
//   outbound_port_map → caps.ecmp_route  (Phase 2 ECMP)
//
// CheckKind also rejects if dash_api_schema_version is below the
// minimum required for this kind.
func (g *Gate) CheckKind(dpuID, kind string) error

// CheckSpec validates capability requirements derived from spec content.
// For example: EniSpec with ipv6 underlay requires caps.ipv6.
func (g *Gate) CheckSpec(dpuID, kind string, spec proto.Message) error
```

### Required-capability matrix

| Kind / spec feature | Capability flag |
|---|---|
| `service_tunnel` | `service_tunnel` |
| `ha_set` (active-active mode) | `dual_active_ha` |
| `route` with multiple ecmp_members | `ecmp_route` |
| `meter_policy` | `meter_policy` |
| ENI with IPv6 underlay | `ipv6` |
| ENI with `trusted_vni: true` | `trusted_vni` |
| ENI with `fast_path_icmp: true` | `fast_path_icmp` |
| Audit log kind | `audit_v2` |

### Minimum schema version (illustrative)

| Kind | min `dash_api_schema_version` |
|---|---|
| `service_tunnel` | 2 |
| `ha_scope_state` | 3 |
| `outbound_port_map_range` | 4 |

(The actual values come from the dash-api proto release matrix.)

### Plugged into `control_plane.go`

```go
// In every Put* handler, after namespace + capacity checks:
for _, dpuID := range affectedDpus {
    if err := s.schema.CheckKind(dpuID, "service_tunnel"); err != nil {
        return nil, status.Errorf(codes.FailedPrecondition, "schema: %v", err)
    }
    if err := s.schema.CheckSpec(dpuID, "service_tunnel", req.GetSpec()); err != nil {
        return nil, status.Errorf(codes.FailedPrecondition, "schema: %v", err)
    }
}
```

### `PutServiceTunnel` (now implemented, replaces Phase 1 stub)

Same pattern as other Put handlers, with schema gate enforced before store.Put.

### Capability negotiation in `inventory/probe.go`

Add to `recordSuccess`: after a first successful probe, call a `dashapi.GetCapabilities` RPC (introduce this in dash-sim if not present, or infer capabilities from a feature-discovery dashapi.List call) and store via `inv.SetCapabilities`.

### Tests

1. `CheckKind(dpu-0, "service_tunnel")` with caps.service_tunnel=false → error
2. Same with true → nil
3. `CheckSpec` for ENI with IPv6 underlay on DPU without ipv6 cap → error
4. Schema version below minimum → error
5. `PutServiceTunnel` on capable DPU → ok; on incapable → FAILED_PRECONDITION

---

## 10. P2-M6 — `internal/ha/orchestrator/`

**Purpose**: Implement the full `HaService` (6 RPCs from `proto/dashcenter/v1/ha.proto`).

### Files

```
internal/ha/orchestrator/
├── orchestrator.go        # Server impl
├── state.go               # HaScope / HaSet state mirroring
├── switchover.go          # planned switchover flow
├── failover.go            # unplanned failover flow
├── broadcaster.go         # WatchHaEvents fan-out
└── orchestrator_test.go
```

### Service interface

| RPC | Type | Behavior |
|---|---|---|
| `GetHaSetState` | unary | Read from obs cache, return all HaSet states |
| `GetHaScopeState` | unary | Read scope states |
| `TriggerSwitchover` | server-streaming | Drain-first, planned role flip; stream progress |
| `TriggerFailover` | server-streaming | Immediate role flip; stream progress |
| `WatchHaEvents` | server-streaming | Live HaEvent fan-out |
| `GetFlowSyncStats` | unary | Read flow-sync state from obs cache |

### State mirroring

`obs.ObsCache` already holds `ha_scope_state` and `ha_set_state` objects (populated by subscribe pump). The orchestrator wraps these in a higher-level state machine:

```go
type haStateView struct {
    obs *model.ObsCache
}

// HaSetView returns the full state of an HA set across all member DPUs.
func (v *haStateView) HaSetView(haSetID string) HaSetView

// HaScopeView returns the full state of an HA scope across all member DPUs.
func (v *haStateView) HaScopeView(haScopeID string) HaScopeView
```

### `TriggerSwitchover` flow (planned, drain-first)

```
Input: req.HaSetId, req.NewActiveDpuId, req.TimeoutSec (default 60)

1. Validate: HA set exists; req.NewActiveDpuId is a member; not currently active
2. Stream HaEvent{type:SWITCHOVER_INITIATED, ha_set_id:...}
3. On current-active DPU: dispatch Apply(ha_scope_config{role:STANDBY, switchover:true})
   This tells the DPU to drain existing flows before stepping down.
4. Subscribe to obs cache changes for ha_scope_state of current-active.
   Wait until state.local_role == STANDBY (within timeout).
   Stream HaEvent{type:DRAIN_PROGRESS, drained_flows:N} per state update.
5. On new-active DPU: dispatch Apply(ha_scope_config{role:ACTIVE})
   Wait until obs state.local_role == ACTIVE.
6. Stream HaEvent{type:SWITCHOVER_COMPLETE, new_active:...}
7. Close stream.

On any timeout: stream HaEvent{type:SWITCHOVER_FAILED, reason:...}; do NOT roll back
(operator must explicitly recover — the state is "split" but functional).
```

### `TriggerFailover` flow (unplanned, immediate)

```
Input: req.HaSetId, req.NewActiveDpuId (the standby to promote)

1. Validate as above
2. Stream HaEvent{type:FAILOVER_INITIATED}
3. On new-active DPU only: dispatch Apply(ha_scope_config{role:ACTIVE, force:true})
   Do NOT contact old-active (it's presumed dead/unreachable)
4. Wait for obs state.local_role == ACTIVE (with shorter 10s timeout)
5. Stream HaEvent{type:FAILOVER_COMPLETE}
```

### `WatchHaEvents` fan-out

```go
type broadcaster struct {
    mu   sync.Mutex
    subs map[chan *dashcenterv1.HaEvent]struct{}
}

func (b *broadcaster) Subscribe() (<-chan *dashcenterv1.HaEvent, func())
func (b *broadcaster) Publish(ev *dashcenterv1.HaEvent)
```

The `subscribe.Pump` (from Phase 1) is extended to call `b.Publish(...)` on every `ha_scope_state` or `ha_set_state` event observed. Add a hook in `subscribe/pump.go`:
```go
// optional callback for HA event tap; nil disables
type Pump struct {
    // ... existing
    haTap func(dpuID string, obj *dashapiv1.Object)
}
```

`main.go` wires `pump.haTap = haOrch.OnObservedHaObject`.

### Tests

Use fake dash-sim with controllable ha_scope_state responses:

1. `GetHaSetState` returns mirrored states
2. `TriggerSwitchover` happy path: stream completes within timeout
3. `TriggerSwitchover` timeout on drain: stream emits FAILED event
4. `TriggerFailover` does not touch old-active
5. `WatchHaEvents`: subscribe, then trigger switchover, observer receives events in order
6. `GetFlowSyncStats` reads from obs cache

---

## 11. P2-M7 — `internal/migration/`

**Purpose**: Implement the full `MigrationService` (12 RPCs) for ENI live migration.

### Files

```
internal/migration/
├── migration.go          # Service impl
├── session.go            # MigrationSession persistence + load
├── statemachine.go       # 10-phase state machine
├── strategies.go         # 4 strategies (NEW_FLOWS_FIRST_DRAIN, FULL_REHOME, MAINTENANCE_FAST, CANARY_SPLIT)
├── bundle.go             # Export/Import bundle chunking
├── coordinator.go        # Orchestrates phase advancement + DPU calls
└── migration_test.go
```

### Persistence

Migration sessions stored in `DesiredStore` under kind `"migration_session"`. The session itself is persisted using the same store backend as everything else — survives restart.

### State machine (10 phases + ROLLBACK + ABORTED)

```
                          ┌── ABORT() ───────────┐
                          │                      │
PLANNING ──validate──► VALIDATED ──start──► INITIALIZED
                                                  │
                                                  ▼
                                            DUAL_WRITE
                                            (write both DPUs)
                                                  │
                                                  ▼
                                            FLOW_DRAIN
                                            (wait existing flows out)
                                                  │
                                                  ▼
                                            CUTOVER
                                            (flip HA active role)
                                                  │
                                                  ▼
                                            VERIFICATION
                                            (smoke test new placement)
                                                  │
                                                  ▼
                                            CLEANUP
                                            (remove from old DPU)
                                                  │
                                                  ▼
                                            COMMITTED   ← terminal
                                                  │
                                                  └─── rollback only allowed before this point


At any phase before COMMITTED, RollbackMigration():
  1. Set session.Phase = ROLLBACK
  2. Reverse-apply: remove from new DPU; ensure old DPU has full state
  3. Set session.Phase = ABORTED on success
```

### RPC implementations

| RPC | Behavior |
|---|---|
| `CreateMigrationPlan` | Compute plan; do not persist session; return plan for review |
| `ValidateMigrationPlan` | Run capacity check, capability check on destination DPU; return validation result |
| `StartMigrationSession` | Persist `MigrationSession{phase:PLANNING}` to store; return session_id |
| `AdvanceMigrationPhase` | **Generation-gated**: require `expected_generation`; phase must advance by exactly one step; persists new state |
| `StreamMigrationSession` | Server-stream: emit a `MigrationProgress` event for every phase change of session_id |
| `RollbackMigration` | Begin reverse flow; stream progress |
| `AbortMigration` | Mark session ABORTED; if already past CUTOVER, requires RollbackMigration first |
| `CommitMigration` | Finalize CLEANUP → COMMITTED; only valid from VERIFICATION |
| `GetMigrationSession` | Read session by id |
| `ListMigrationSessions` | List all sessions (optionally filtered by eni_id or status) |
| `ExportMigrationBundle` | Server-stream chunks: header, payload (objects), trailer |
| `ImportMigrationBundle` | Client-stream chunks: assemble bundle, validate, persist |

### `AdvanceMigrationPhase` generation gate

```go
func (s *migrationServer) AdvanceMigrationPhase(ctx context.Context, req *dashcenterv1.AdvanceMigrationPhaseRequest) (*dashcenterv1.AdvanceMigrationPhaseResponse, error) {
    sessKey := store.ObjectKey{Namespace: req.Namespace, Kind: "migration_session", Name: req.SessionId}
    cur, err := s.store.Get(ctx, sessKey)
    if err != nil { return nil, codeFromErr(err) }

    var sess dashcenterv1.MigrationSession
    if err := protojson.Unmarshal(cur.Data, &sess); err != nil { return nil, ... }

    // Generation check
    if req.ExpectedGeneration > 0 && cur.Generation != req.ExpectedGeneration {
        return nil, status.Error(codes.FailedPrecondition, "generation mismatch")
    }

    // Phase transition check
    if !validTransition(sess.Phase, req.TargetPhase) {
        return nil, status.Errorf(codes.FailedPrecondition,
            "cannot advance %s → %s", sess.Phase, req.TargetPhase)
    }

    // Run the phase action
    if err := s.coordinator.Execute(ctx, &sess, req.TargetPhase); err != nil {
        // Don't persist on error — caller can retry
        return nil, status.Errorf(codes.Internal, "phase action: %v", err)
    }

    sess.Phase = req.TargetPhase
    sess.PhaseStartedAt[req.TargetPhase.String()] = timestamppb.Now()
    newData, _ := protojson.Marshal(&sess)
    newGen, err := s.store.Put(ctx, sessKey, &sess, cur.Generation)
    if err != nil { return nil, codeFromErr(err) }

    return &dashcenterv1.AdvanceMigrationPhaseResponse{
        SessionId: sess.SessionId,
        Phase:     sess.Phase,
        Generation: newGen,
    }, nil
}
```

### Per-phase actions (`coordinator.Execute`)

| Target phase | Action |
|---|---|
| `VALIDATED` | Re-run validation (idempotent) |
| `INITIALIZED` | Pre-stage on destination: create empty placeholder ENI on dest DPU |
| `DUAL_WRITE` | Apply the ENI + dependencies to destination; both DPUs now have it; HA scope on source remains ACTIVE |
| `FLOW_DRAIN` | Use HA controls: set source to PREPARE_STANDBY mode; wait for in-flight flows to drop to 0 (poll obs cache for FlowSyncStats); timeout = `req.DrainTimeoutSec` |
| `CUTOVER` | Call `ha/orchestrator.TriggerSwitchover` to flip ACTIVE role to dest DPU; wait for confirmation |
| `VERIFICATION` | Run smoke checks: ENI present on dest, ha_scope_state.local_role==ACTIVE, FlowSyncStats.synced_flows > 0 |
| `CLEANUP` | Delete ENI + dependencies from source DPU; dispatch Delete in correct dependency order |
| `COMMITTED` | Mark session COMMITTED; emit final event |

### Strategies (`strategies.go`)

| Strategy | Difference |
|---|---|
| `NEW_FLOWS_FIRST_DRAIN` (default) | DUAL_WRITE allows new flows on dest; FLOW_DRAIN waits for source flows to age out (long timeout) |
| `FULL_REHOME` | DUAL_WRITE forces all new + existing flows to dest; FLOW_DRAIN is short (just confirm) |
| `MAINTENANCE_FAST` | Skip FLOW_DRAIN entirely; CUTOVER is immediate; some flows will be reset |
| `CANARY_SPLIT` | Stay in DUAL_WRITE indefinitely with operator-defined flow split (Phase 2.5 — possibly deferred) |

### Bundle export/import

```
ExportMigrationBundle (server-streaming):
   Chunk 1: BundleHeader{eni_id, source_dpu, dest_dpu, generated_at}
   Chunk 2..N: BundlePayload{kind, key, protojson_data}  for each object in the ENI's dep closure
   Chunk N+1: BundleTrailer{checksum:sha256(all payloads), object_count:N}

Chunk size limit: 64KB. Large object payloads are split across multiple BundlePayload chunks
with continuation flag.

ImportMigrationBundle (client-streaming):
   Server reads header → allocates session
   Server reads each payload → validates → stages on dest
   Server reads trailer → verifies checksum
   Server returns ImportResult with session_id
```

### Tests

Use fake dash-sim instances for source and dest:

1. Happy path: 10 phase advances complete; ENI lives on dest after COMMITTED
2. Generation mismatch on advance → FAILED_PRECONDITION
3. Invalid transition (PLANNING → CUTOVER) → FAILED_PRECONDITION
4. Rollback from FLOW_DRAIN: dest cleaned up; source unaffected
5. Rollback from CUTOVER: HA flipped back to source
6. Abort from COMMITTED: rejected (already terminal)
7. ExportMigrationBundle then ImportMigrationBundle round-trip
8. Checksum mismatch on import → INVALID_ARGUMENT
9. Restart dashd mid-migration: session recovered from store; can resume

---

## 12. P2-M8 — `internal/operations/`

**Purpose**: Implement `OperationsService` (cordon, uncordon, drain) and the saga-backed `ApplyBatch`.

### Files

```
internal/operations/
├── operations.go         # Service impl
├── drain.go              # DrainDpu orchestration
└── operations_test.go
```

### RPC implementations

| RPC | Behavior |
|---|---|
| `CordonDpu` | `inv.SetState(dpuID, DPU_STATE_CORDONED)`; placement function will exclude this DPU from new ENI assignments |
| `UncordonDpu` | `inv.SetState(dpuID, DPU_STATE_ONLINE)` (if prober has it as online) |
| `DrainDpu` | Server-stream `DrainProgress`; multi-phase orchestration (see below) |
| `EniMigrationLink` | Read-only: list all in-flight migrations involving dpuID |

### `CordonDpu` implementation

```go
func (s *operationsServer) CordonDpu(ctx context.Context, req *dashcenterv1.CordonDpuRequest) (*dashcenterv1.Ack, error) {
    if !s.elector.IsLeader() { return nil, status.Error(codes.Unavailable, "not the leader") }
    if err := s.inv.SetState(req.DpuId, dashcenterv1.DpuState_DPU_STATE_CORDONED); err != nil {
        return nil, codeFromErr(err)
    }
    s.events.Publish(&dashcenterv1.PolicyEvent{
        EventType: dashcenterv1.PolicyEventType_POLICY_EVENT_DPU_CORDONED,
        DpuId:     req.DpuId,
        Timestamp: timestamppb.Now(),
    })
    return &dashcenterv1.Ack{Accepted: true}, nil
}
```

### Placement update for cordoned DPUs

In `placement.AffectedDpus` and the ENI placement rule, exclude DPUs whose state is `CORDONED`:
```go
// Inside Resolve, when computing where an ENI goes:
//   if EniSpec.DpuId is CORDONED → skip (no Eni object emitted for this DPU)
//   (the operator should have already migrated; CORDONED prevents accidental new ENIs)
```

### `DrainDpu` flow

```
Input: req.DpuId, req.MaxParallelMigrations (default 3), req.Timeout

PLANNING (stream event)
  - Enumerate all ENIs on dpuID: store.List("eni") filtered by spec.DpuId == dpuID
  - For each ENI, pick a destination DPU:
      candidates := online + non-cordoned + has spare capacity
      pick least-loaded (lowest EnisInUse/MaxEnis ratio)
  - If any ENI has no suitable destination: stream FAILED event; return error.
  - Build list of MigrationPlan{eni, source: dpuID, dest: chosen}

MIGRATING (stream events)
  - sem := semaphore.New(req.MaxParallelMigrations)
  - For each MigrationPlan in parallel:
      sem.Acquire()
      go {
        sessID := startMigrationSession(plan)
        for phase := VALIDATED..COMMITTED:
          advancePhase(sessID, phase)
          stream DrainProgress{eni: plan.eni, phase: phase}
        sem.Release()
      }
  - Wait for all migrations to complete or fail.
  - If any fail: stream FAILED with details; abort remaining sessions.

DRAINING (stream events)
  - After all ENIs migrated: wait for flow count on dpuID to drop to 0.
  - Poll FlowSyncStats from obs cache every 5s.
  - Stream DrainProgress with current flow_count until 0 (or timeout).

COMPLETE (stream final event)
  - Set inv state to CORDONED (already was, but reaffirm)
  - Stream DrainProgress{phase: COMPLETE}
  - Close stream
```

### Saga-backed `ApplyBatch`

The Phase 1 `ApplyBatch` was best-effort (no rollback). Phase 2 replaces it with:

```go
func (s *controlPlaneServer) ApplyBatch(stream dashcenterv1.ControlPlane_ApplyBatchServer) error {
    var items []*dashcenterv1.BatchItem
    for {
        item, err := stream.Recv()
        if err == io.EOF { break }
        if err != nil { return err }
        items = append(items, item)
    }
    if len(items) == 0 {
        return stream.SendAndClose(&dashcenterv1.BatchResult{Accepted: true})
    }

    sagaID, err := s.saga.Begin(stream.Context(), items)
    if err != nil { return err }

    if err := s.saga.Commit(stream.Context(), sagaID); err != nil {
        // Rollback automatically triggered inside Commit on failure
        return stream.SendAndClose(&dashcenterv1.BatchResult{
            Accepted: false,
            Error:    err.Error(),
            SagaId:   sagaID,
        })
    }

    return stream.SendAndClose(&dashcenterv1.BatchResult{Accepted: true, SagaId: sagaID})
}
```

The actual saga logic lives in `internal/saga/` (P2-M12).

### Tests

1. `CordonDpu` → placement excludes from new ENIs
2. `DrainDpu` with 5 ENIs, max_parallel=2: streams 5 migrations; all complete; final state COMPLETE
3. `DrainDpu` with no suitable destination → FAILED in PLANNING phase
4. `DrainDpu` cancellation: ctx cancel cleanly aborts in-flight migrations
5. `UncordonDpu` → placement re-includes the DPU

---

## 13. P2-M9 — `internal/auth/`

**Purpose**: TLS / mTLS, Bearer token → role mapping, OIDC hook.

### Files

```
internal/auth/
├── tls.go
├── roles.go
├── interceptor.go
└── auth_test.go
```

### Config additions

```go
type Config struct {
    // ... existing
    Auth AuthConfig `yaml:"auth"`
}

type AuthConfig struct {
    Mode         string                  `yaml:"mode"`            // "none" | "token" | "oidc" (Phase 2: token only; oidc stub)
    TLS          TLSConfig               `yaml:"tls"`
    Tokens       map[string]string       `yaml:"tokens"`          // token → role
    RoleRPCs     map[string][]string     `yaml:"role_rpcs"`       // role → allowed RPC method names ("*" = all)
    OIDC         OIDCConfig              `yaml:"oidc"`            // stub for future
}

type TLSConfig struct {
    Enabled        bool   `yaml:"enabled"`
    CertFile       string `yaml:"cert_file"`
    KeyFile        string `yaml:"key_file"`
    ClientCAFile   string `yaml:"client_ca_file"`   // for mTLS; optional
    RequireClient  bool   `yaml:"require_client"`   // when true, mTLS required
}

type OIDCConfig struct {
    IssuerURL string   `yaml:"issuer_url"`
    Audience  string   `yaml:"audience"`
}
```

Example YAML:
```yaml
auth:
  mode: token
  tls:
    enabled: true
    cert_file: /etc/dashd/tls/cert.pem
    key_file:  /etc/dashd/tls/key.pem
    client_ca_file: /etc/dashd/tls/client_ca.pem
    require_client: true
  tokens:
    "secret-op-token":    "operator"
    "secret-admin-token": "admin"
    "secret-view-token":  "viewer"
  role_rpcs:
    viewer:   ["Get", "List", "GetDpuStatus", "GetDrift", "GetAuditLog", "GetFlowList", "GetCounters", "WatchEvents", "TraceFlow", "ExplainMatch", "GetAclHitStats"]
    operator: ["*"]      # all except admin RPCs (cordon/drain require operator+)
    admin:    ["*"]      # all
```

### `tls.go`

```go
// LoadServerTLS builds a grpc.ServerOption for TLS (and optional mTLS).
func LoadServerTLS(cfg TLSConfig) (grpc.ServerOption, error)

// LoadHTTPSTLS builds a tls.Config for the REST gateway.
func LoadHTTPSTLS(cfg TLSConfig) (*tls.Config, error)

// LoadClientTLS builds transport credentials for outbound dashapi.v1 calls.
// Used by dispatch.worker.client.Dial.
func LoadClientTLS(caFile string) (credentials.TransportCredentials, error)
```

### `roles.go`

```go
type Role string

type Resolver struct {
    tokens   map[string]Role
    roleRPCs map[Role]map[string]bool   // role → set of allowed methods; "*" means all
}

func NewResolver(cfg AuthConfig) *Resolver

// Authenticate extracts the Bearer token from metadata, returns the role.
// Returns "", false if no token or invalid.
func (r *Resolver) Authenticate(ctx context.Context) (Role, bool)

// Authorize returns true if role is allowed to call fullMethod (e.g. "/dashcenter.v1.ControlPlane/PutVnet").
// Extracts the method name (last path segment) and checks roleRPCs.
func (r *Resolver) Authorize(role Role, fullMethod string) bool
```

### `interceptor.go`

```go
// UnaryAuth returns a grpc.UnaryServerInterceptor that:
//  1. Extracts Bearer token from metadata
//  2. Resolves role via Resolver.Authenticate
//  3. If no role → UNAUTHENTICATED
//  4. If role not allowed for method → PERMISSION_DENIED
//  5. Otherwise attaches role to ctx and calls handler
func UnaryAuth(r *Resolver) grpc.UnaryServerInterceptor

// StreamAuth is the streaming counterpart.
func StreamAuth(r *Resolver) grpc.StreamServerInterceptor

// FromContext extracts the role attached by the interceptor.
func FromContext(ctx context.Context) (Role, bool)
```

### Wiring in `server/grpc/server.go`

```go
opts := []grpc.ServerOption{
    grpc.UnaryInterceptor(unaryChain(
        recoveryInterceptor,
        auth.UnaryAuth(authResolver),
        audit.UnaryAudit(auditWriter),     // P2-M10
        loggingInterceptor,
    )),
    grpc.StreamInterceptor(streamChain(
        streamRecoveryInterceptor,
        auth.StreamAuth(authResolver),
        audit.StreamAudit(auditWriter),
        streamLoggingInterceptor,
    )),
}
if tlsOpt, err := auth.LoadServerTLS(cfg.Auth.TLS); err == nil && cfg.Auth.TLS.Enabled {
    opts = append(opts, tlsOpt)
}
g := grpc.NewServer(opts...)
```

### Tests

1. No token → UNAUTHENTICATED
2. Bad token → UNAUTHENTICATED
3. Viewer token + PutVnet → PERMISSION_DENIED
4. Viewer token + GetVnet → ok
5. Operator token + PutVnet → ok
6. Admin token + anything → ok
7. TLS handshake: client without cert + RequireClient=true → connection refused
8. mTLS valid client cert → connection ok

---

## 14. P2-M10 — `internal/audit/`

**Purpose**: Append-only audit log for every mutating RPC; tail-follow streaming reader.

### Files

```
internal/audit/
├── writer.go
├── reader.go
├── interceptor.go
└── audit_test.go
```

### Disk format

```
<state_dir>/audit.jsonl       # newline-delimited JSON
```

Each line is a single `AuditEntry` (proto from `dashcenter/v1/types.proto`) protojson-encoded:
```json
{"timestamp":"2026-06-07T10:00:00Z","actor":"alice","role":"operator","rpc":"PutVnet","namespace":"prod","object_kind":"vnet","object_name":"v1","result":"OK"}
```

Rotation: rotate by size (default 100MB) or daily, keep last 7 days. Use a simple naming scheme: `audit.jsonl`, `audit.jsonl.1`, `audit.jsonl.2`, ...

### Writer

```go
type Writer struct {
    dir      string
    maxSize  int64
    mu       sync.Mutex
    f        *os.File
    notify   chan struct{}  // signals tailers (broadcast pattern)
    notifyMu sync.Mutex
    subs     map[chan struct{}]struct{}
}

func NewWriter(dir string, maxSize int64) (*Writer, error)

// Write appends an AuditEntry. fsync on every call (durability over latency).
// Rotates file if size exceeds maxSize.
func (w *Writer) Write(entry *dashcenterv1.AuditEntry) error

// Subscribe returns a channel that receives an empty struct after every Write.
// Used by tail readers.
func (w *Writer) Subscribe() (<-chan struct{}, func())

func (w *Writer) Close() error
```

### Audit interceptor

```go
func UnaryAudit(w *Writer) grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
        // Only audit mutating RPCs (whitelist: Put*, Delete*, Trigger*, Drain*, Cordon*, Reconcile, ApplyBatch, etc.)
        if !isMutating(info.FullMethod) {
            return handler(ctx, req)
        }
        role, _ := auth.FromContext(ctx)
        actor := actorFromCtx(ctx)        // pull from peer cert CN or token metadata
        ns, kind, name := extractObject(req)  // best-effort extraction via reflection

        resp, err := handler(ctx, req)

        entry := &dashcenterv1.AuditEntry{
            Timestamp:  timestamppb.Now(),
            Actor:      actor,
            Role:       string(role),
            Rpc:        info.FullMethod,
            Namespace:  ns,
            ObjectKind: kind,
            ObjectName: name,
            Result:     codeFromErr(err).String(),
            ErrorMsg:   errMsgOrEmpty(err),
        }
        if writeErr := w.Write(entry); writeErr != nil {
            slog.Warn("audit: write failed", "err", writeErr)
        }
        return resp, err
    }
}
```

### Reader (tail-follow)

```go
type Reader struct {
    w *Writer
}

func NewReader(w *Writer) *Reader

// Tail reads existing entries (applying filter) then follows live appends
// until ctx is cancelled. Emits each entry on the returned channel.
func (r *Reader) Tail(ctx context.Context, filter AuditFilter) (<-chan *dashcenterv1.AuditEntry, error)
```

### `GetAuditLog` RPC (server streaming)

```go
func (s *observabilityServer) GetAuditLog(req *dashcenterv1.GetAuditLogRequest, stream dashcenterv1.ObservabilityService_GetAuditLogServer) error {
    ch, err := s.auditReader.Tail(stream.Context(), req.GetFilter())
    if err != nil { return err }
    for entry := range ch {
        if err := stream.Send(entry); err != nil { return err }
    }
    return nil
}
```

### Counter polling (also P2-M10)

The Phase 1 `dispatch.worker` is extended with a counter-polling goroutine:

```go
// Inside worker.run, add:
go w.counterPoller(ctx)

func (w *worker) counterPoller(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C:
            for _, kind := range counterKinds {  // eni, acl_rule, route, etc.
                objs := w.obs.GetDpu(w.id)
                for _, obj := range objs {
                    if obj.GetKind() != kind { continue }
                    counters, err := w.client.GetCounters(ctx, kind, obj.GetKey())
                    if err == nil {
                        w.counterStore.Set(w.id, obj.GetKind(), obj.GetKey(), counters)
                    }
                }
            }
        }
    }
}
```

Where `counterStore` is a new `sync.Map` shared with the gRPC server:
```go
type CounterStore struct {
    mu sync.RWMutex
    data map[string]*dashcenterv1.CounterReport  // key = dpuID:kind:joinedKey
}
```

### `GetCounters` RPC (server streaming with follow)

```go
func (s *observabilityServer) GetCounters(req *dashcenterv1.GetCountersRequest, stream dashcenterv1.ObservabilityService_GetCountersServer) error {
    // Initial send: all current counters matching req filter
    for _, rep := range s.counters.Filtered(req.GetFilter()) {
        if err := stream.Send(rep); err != nil { return err }
    }
    if !req.GetFollow() {
        return nil  // one-shot
    }
    // Follow mode: subscribe to counter store updates
    updates := s.counters.Subscribe()
    defer updates.Close()
    for {
        select {
        case <-stream.Context().Done(): return nil
        case rep := <-updates:
            if matchesFilter(rep, req.GetFilter()) {
                if err := stream.Send(rep); err != nil { return err }
            }
        }
    }
}
```

### Tests

1. Audit writer appends entries; lines parse as AuditEntry
2. Audit writer rotates at maxSize
3. Audit writer fsyncs each entry (crash-test: kill process mid-write; entries before crash present)
4. Audit interceptor populates Actor, Role, RPC fields
5. Audit interceptor only fires on mutating RPCs (Get not audited)
6. Tail reader emits existing entries then follows live
7. Counter poller fetches every 30s
8. GetCounters one-shot returns current snapshot
9. GetCounters follow mode delivers updates

---

## 15. P2-M11 — `internal/flow/`

**Purpose**: Implement `DiagnosticsService` (TraceFlow, ExplainMatch, ExplainDrift, GetAclHitStats, TriggerResimulation).

### Files

```
internal/flow/
├── trace.go              # TraceFlow synthetic-packet simulation
├── explain.go            # ExplainMatch + ExplainDrift
├── stats.go              # GetAclHitStats
├── resim.go              # TriggerResimulation
└── flow_test.go
```

### `TraceFlow` algorithm

```go
func (f *Service) TraceFlow(ctx context.Context, req *dashcenterv1.TraceFlowRequest) (*dashcenterv1.FlowTraceResult, error) {
    // 1. Find the ENI in obs cache
    eniObj, dpuID, err := f.findEni(req.GetEniId())
    if err != nil { return nil, codes.NotFound }
    eni, _ := kinds.PayloadOf(eniObj)

    result := &dashcenterv1.FlowTraceResult{}

    // 2. Determine direction: inbound (dst is ENI) or outbound (src is ENI)
    direction := classifyDirection(req.GetFlow(), eni)
    result.Direction = direction

    // 3. Walk inbound ACL chain (acl_in for this ENI)
    if direction == INBOUND {
        for _, stage := range []AclStage{INGRESS_1, INGRESS_2, INGRESS_3} {
            aclIn := f.lookupAclIn(dpuID, req.GetEniId(), stage)
            if aclIn == nil { continue }
            groupID := aclIn.GetGroupId()
            verdict, ruleID := f.evaluateAclGroup(dpuID, groupID, req.GetFlow())
            result.StageTrace = append(result.StageTrace, &dashcenterv1.StageTrace{
                Stage:        fmt.Sprintf("ACL_INBOUND_%d", stage),
                Verdict:      verdict,
                MatchedRule:  ruleID,
            })
            if verdict == DENIED {
                result.FinalVerdict = DENIED
                result.MatchedRuleId = ruleID
                return result, nil
            }
        }
    }

    // 4. Walk route table (RoutePolicy / Route objects)
    route := f.longestPrefixMatch(dpuID, req.GetFlow().GetDstIp())
    result.StageTrace = append(result.StageTrace, &dashcenterv1.StageTrace{
        Stage: "ROUTE_LOOKUP",
        MatchedRouteId: route.GetGroupId(),
    })

    // 5. Walk outbound ACL chain
    // ...

    // 6. Final verdict
    result.FinalVerdict = PERMITTED
    return result, nil
}
```

The trace runs entirely in dashd memory against the cached observed objects. No network call to the DPU. This makes it deterministic and fast.

**ACL evaluation** (`evaluateAclGroup`):
```go
func (f *Service) evaluateAclGroup(dpuID, groupID string, flow *FlowDescriptor) (Verdict, string) {
    rules := f.listAclRules(dpuID, groupID)  // sorted by priority asc
    for _, r := range rules {
        if matches(r, flow) {
            return aclActionToVerdict(r.GetAction()), r.GetRuleId()
        }
    }
    return DEFAULT_PERMIT, ""   // no match → default policy
}

func matches(rule *dash.acl_rule.AclRule, flow *FlowDescriptor) bool {
    if !cidrMatches(rule.SrcPrefix, flow.SrcIp) { return false }
    if !cidrMatches(rule.DstPrefix, flow.DstIp) { return false }
    if rule.Protocol != 0 && rule.Protocol != flow.Protocol { return false }
    if !portInRange(rule.SrcPortRange, flow.SrcPort) { return false }
    if !portInRange(rule.DstPortRange, flow.DstPort) { return false }
    return true
}
```

### `ExplainMatch`

For each candidate rule (in priority order), explain why it did or did not match:
```go
type MatchExplanation struct {
    RuleId    string
    Matched   bool
    Reasons   []string   // e.g. ["src 10.0.0.1 in 10.0.0.0/24", "dst 10.0.0.2 in 0.0.0.0/0", "protocol mismatch: rule=TCP flow=UDP"]
}
```

### `GetAclHitStats`

Streams `AclStatsPerDpu` for each DPU. Reads from the counter store (P2-M10). If `req.ZeroHitsOnly` is true, filter rules with `hit_count == 0` — surfaces dead rules.

### `ExplainDrift`

Given a drift item from `GetDrift`, return a narrative:
```
"Object eni-1 (kind: ENI) on dpu-0 is in observed state but not desired.
Likely root cause: an operator directly applied this on the DPU outside dashd.
Remediation: REMOVE_FROM_DPU (dashd will re-apply on next reconcile), or
ACCEPT_AS_DESIRED (PUT a matching spec via dashctl)."
```

### `TriggerResimulation`

For an ENI, tell the DPU to re-evaluate all active flows through the current policy:
```go
func (f *Service) TriggerResimulation(ctx context.Context, req *dashcenterv1.TriggerResimulationRequest) (*dashcenterv1.Ack, error) {
    // Find the ENI
    eni, dpuID, _ := f.findEni(req.GetEniId())
    // Send Apply with the resimulate_flows flag set true
    eniObj := proto.Clone(eni).(*dashapiv1.Object)
    // ... set resimulate_flows=true in the typed payload
    return f.dispatch.ApplyOne(dpuID, eniObj)  // direct dispatch path
}
```

### Tests

1. TraceFlow with permit ACL → PERMITTED + matched_rule
2. TraceFlow with deny ACL → DENIED + matched_rule + stage
3. TraceFlow with no matching ACL → DEFAULT_PERMIT
4. ExplainMatch returns reasons for each candidate
5. GetAclHitStats reads from counters; ZeroHitsOnly filter works
6. ExplainDrift returns narrative + remediation
7. TriggerResimulation issues Apply with flag set

---

## 16. P2-M12 — `internal/saga/` + `internal/api/gnmi/`

### `internal/saga/`

**Purpose**: Atomic cross-DPU rollback for `ApplyBatch`.

#### Files

```
internal/saga/
├── coordinator.go
├── state.go
├── recovery.go
└── coordinator_test.go
```

#### Types

```go
type SagaState int
const (
    SagaPending     SagaState = 1
    SagaCommitted   SagaState = 2
    SagaRollingBack SagaState = 3
    SagaRolledBack  SagaState = 4
    SagaFailed      SagaState = 5
)

type SagaEntry struct {
    SagaId    string
    State     SagaState
    Items     []SagaItem  // (kind, name, namespace, spec_json)
    Applied   []int       // indices of items successfully Put
    StartedAt time.Time
}

type Coordinator struct {
    store    store.DesiredStore   // sagas persisted here
    dispatch *dispatch.Manager
}

func New(st store.DesiredStore, mgr *dispatch.Manager) *Coordinator

// Begin persists a saga and returns its ID.
func (c *Coordinator) Begin(ctx context.Context, items []*dashcenterv1.BatchItem) (string, error)

// Commit applies each item in order. On the first failure, automatically
// rolls back all successfully applied items. Returns nil on full success.
func (c *Coordinator) Commit(ctx context.Context, sagaID string) error

// Rollback explicitly rolls back a saga that was previously Begin'd.
func (c *Coordinator) Rollback(ctx context.Context, sagaID string) error

// Resume is called at startup. Scans all sagas; for any in SagaRollingBack
// state, completes the rollback. For any in SagaPending older than 1h,
// marks as SagaFailed.
func (c *Coordinator) Resume(ctx context.Context) error
```

#### Persistence

Stored under kind `"saga"` in the same `DesiredStore` used for desired specs. Recovery on dashd restart calls `Coordinator.Resume(ctx)`.

#### Commit/rollback algorithm

```
Commit(sagaID):
  load saga
  for i, item in saga.Items:
    err := store.Put(item)
    if err != nil:
      saga.State = SagaRollingBack
      persist saga
      rollback(saga.Applied)   // reverse-order delete
      saga.State = SagaRolledBack
      persist saga
      return err
    saga.Applied += i
  saga.State = SagaCommitted
  persist saga
  return nil

rollback(applied []int):
  for i := len(applied)-1; i >= 0; i--:
    item := saga.Items[applied[i]]
    _ = store.Delete(item.Key)   // best-effort; log warnings
```

#### Tests

1. Commit all 5 items succeed → SagaCommitted
2. Commit fails at item #3 → items #1, #2 deleted; saga state SagaRolledBack
3. Restart mid-rollback: Resume completes the rollback
4. Concurrent sagas don't interfere

### `internal/api/gnmi/`

**Purpose**: Minimal gNMI Subscribe bridge to `WatchEvents`.

#### Files

```
internal/api/gnmi/
├── server.go
└── server_test.go
```

#### Implementation

Implement only the gNMI `Subscribe` RPC. Other gNMI RPCs (`Get`, `Set`, `Capabilities`) return `Unimplemented`.

```go
// gNMI service definition is in gnmi.proto from openconfig/gnmi.
// Add the module to go.mod: github.com/openconfig/gnmi v0.10.0

type Server struct {
    gnmi.UnimplementedGNMIServer
    events *EventBroadcaster
}

func New(events *EventBroadcaster) *Server

// Subscribe is the only implemented gNMI RPC. It accepts a
// SubscribeRequest with paths under /dashcenter/v1/events and bridges
// to the internal WatchEvents fan-out.
//
// Supported subscription modes: STREAM (ON_CHANGE only). POLL and ONCE
// return Unimplemented.
func (s *Server) Subscribe(stream gnmi.GNMI_SubscribeServer) error
```

**Path mapping**:
* `/dashcenter/v1/events` → all PolicyEvent fan-out
* `/dashcenter/v1/events/<event_type>` → filter by type (e.g. `vnet_changed`)
* `/dashcenter/v1/events/<event_type>/<namespace>` → further filter

For each PolicyEvent received from the broadcaster, build a `gnmi.SubscribeResponse{update: Notification{...}}` with `update.val` = protojson-encoded PolicyEvent.

### `EventBroadcaster` — added to `internal/observability/` (small new package)

```go
package observability

type EventBroadcaster struct { /* sync.Map of subscriber channels */ }

func New() *EventBroadcaster
func (b *EventBroadcaster) Publish(ev *dashcenterv1.PolicyEvent)
func (b *EventBroadcaster) Subscribe() (<-chan *dashcenterv1.PolicyEvent, func())
```

Every Put/Delete/Migration phase change/HA event in dashd publishes a `PolicyEvent`. The gRPC `WatchEvents` and the gNMI `Subscribe` both consume from this broadcaster.

### `WatchEvents` full implementation

```go
func (s *observabilityServer) WatchEvents(req *dashcenterv1.WatchEventsRequest, stream dashcenterv1.ObservabilityService_WatchEventsServer) error {
    ch, unsub := s.events.Subscribe()
    defer unsub()
    for {
        select {
        case <-stream.Context().Done(): return nil
        case ev := <-ch:
            if matchesFilter(ev, req.GetFilter()) {
                if err := stream.Send(ev); err != nil { return err }
            }
        }
    }
}
```

### Tests

1. WatchEvents receives events fired during the subscription
2. WatchEvents filter (by event_type) works
3. WatchEvents disconnect cleanly removes subscriber
4. gNMI Subscribe receives bridged events as Notifications
5. gNMI Subscribe with unsupported mode (POLL) → Unimplemented

---

## 17. Phase 2 file tree

Additions and modifications on top of Phase 1:

```
src/impl-go/dashd/
├── configs/
│   └── dashd.example.yaml             [MOD: add etcd, ha, auth, audit sections]
├── impl-plan-advanced.md              [this file]
├── internal/
│   ├── store/
│   │   └── etcd/                      [NEW: P2-M1]
│   │       ├── etcd.go
│   │       ├── etcd_test.go
│   │       └── compaction.go
│   ├── ha/
│   │   ├── leader/                    [NEW: P2-M2]
│   │   │   ├── leader.go
│   │   │   ├── none.go
│   │   │   ├── etcd.go
│   │   │   └── etcd_test.go
│   │   └── orchestrator/              [NEW: P2-M6]
│   │       ├── orchestrator.go
│   │       ├── state.go
│   │       ├── switchover.go
│   │       ├── failover.go
│   │       ├── broadcaster.go
│   │       └── orchestrator_test.go
│   ├── namespace/                     [NEW: P2-M3]
│   │   ├── namespace.go
│   │   ├── validator.go
│   │   └── validator_test.go
│   ├── capacity/                      [NEW: P2-M4]
│   │   ├── capacity.go
│   │   ├── tracker.go
│   │   └── tracker_test.go
│   ├── schema/                        [NEW: P2-M5]
│   │   ├── gate.go
│   │   └── gate_test.go
│   ├── migration/                     [NEW: P2-M7]
│   │   ├── migration.go
│   │   ├── session.go
│   │   ├── statemachine.go
│   │   ├── strategies.go
│   │   ├── bundle.go
│   │   ├── coordinator.go
│   │   └── migration_test.go
│   ├── operations/                    [NEW: P2-M8]
│   │   ├── operations.go
│   │   ├── drain.go
│   │   └── operations_test.go
│   ├── auth/                          [NEW: P2-M9]
│   │   ├── tls.go
│   │   ├── roles.go
│   │   ├── interceptor.go
│   │   └── auth_test.go
│   ├── audit/                         [NEW: P2-M10]
│   │   ├── writer.go
│   │   ├── reader.go
│   │   ├── interceptor.go
│   │   └── audit_test.go
│   ├── observability/                 [NEW: P2-M12 small helper]
│   │   ├── broadcaster.go
│   │   └── counter_store.go
│   ├── flow/                          [NEW: P2-M11]
│   │   ├── trace.go
│   │   ├── explain.go
│   │   ├── stats.go
│   │   ├── resim.go
│   │   └── flow_test.go
│   ├── saga/                          [NEW: P2-M12]
│   │   ├── coordinator.go
│   │   ├── state.go
│   │   ├── recovery.go
│   │   └── coordinator_test.go
│   ├── api/
│   │   └── gnmi/                      [NEW: P2-M12]
│   │       ├── server.go
│   │       └── server_test.go
│   ├── store/store.go                 [MOD: ObjectKey adds Namespace field]
│   ├── store/file/file.go             [MOD: directory layout includes namespace]
│   ├── inventory/probe.go             [MOD: capability negotiation on first probe]
│   ├── dispatch/manager.go            [MOD: add saga path; expose ApplyOne]
│   ├── dispatch/worker.go             [MOD: counter polling goroutine]
│   ├── server/grpc/server.go          [MOD: wire in real impls, TLS, auth, audit interceptors]
│   ├── server/grpc/control_plane.go   [MOD: namespace/capacity/schema gates; real ApplyBatch (saga)]
│   ├── server/grpc/observability.go   [MOD: real GetDpuStatus/GetCounters/GetAuditLog/WatchEvents]
│   ├── server/grpc/interceptors.go    [MOD: chain extended]
│   └── config/config.go               [MOD: Etcd, HA, Auth, Audit sections]
└── cmd/dashd/
    └── main.go                        [MOD: etcd store wiring, HA loop, TLS, auth, audit, all new services]
```

~25 additional source files (~6,000–8,000 additional LoC including tests).

---

## 18. Module dependency additions

Add to `src/impl-go/dashd/go.mod`:

```go
require (
    // Phase 1 deps continue
    google.golang.org/grpc v1.62.0
    google.golang.org/protobuf v1.33.0
    gopkg.in/yaml.v3 v3.0.1
    golang.org/x/time v0.5.0

    // Phase 2 additions
    go.etcd.io/etcd/client/v3 v3.5.12
    go.etcd.io/etcd/client/pkg/v3 v3.5.12
    github.com/openconfig/gnmi v0.10.0       // gNMI proto + service

    // Testing
    go.uber.org/goleak v1.3.0
    go.etcd.io/etcd/tests/v3 v3.5.12         // in-process etcd for tests
)
```

For OIDC (stub in Phase 2; full impl deferred):
```go
require github.com/coreos/go-oidc/v3 v3.9.0  // optional, only when mode=oidc
```

---

## 19. Quality gates

All must pass before Phase 2 is declared done:

| # | Gate | Verification |
|---|---|---|
| 1 | All Phase 1 gates still pass | `go test -race ./...` (no regressions) |
| 2 | etcd store passes interface tests | `go test ./internal/store/etcd/...` (uses in-process etcd) |
| 3 | Leader election: 3-node cluster | leader-kill test → new leader within 15s |
| 4 | Follower read-only | mutating RPC on follower → UNAVAILABLE with leader hint |
| 5 | Namespace isolation | cross-namespace Get returns NOT_FOUND |
| 6 | Cross-namespace ref rejected | ENI in ns-A referencing AclPolicy in ns-B → INVALID_ARGUMENT |
| 7 | Capacity admission | DPU-fill test returns RESOURCE_EXHAUSTED at limit+1 |
| 8 | SimulateApply | dry-run returns capacity preview without writing |
| 9 | Schema gate | PutServiceTunnel on incapable DPU → FAILED_PRECONDITION |
| 10 | HA switchover | end-to-end switchover between two dash-sims |
| 11 | HA failover | failover does not contact old active |
| 12 | WatchHaEvents | event delivery during switchover |
| 13 | ENI migration 10 phases | full migration completes; ENI on dest only |
| 14 | Migration rollback | rollback from FLOW_DRAIN restores original |
| 15 | Migration restart-recovery | dashd restart mid-migration → resume from store |
| 16 | Drain DPU with 5 ENIs | all 5 migrate in parallel up to limit; final state CORDONED |
| 17 | TLS handshake | client without cert + RequireClient → connection refused |
| 18 | mTLS valid client | accepted |
| 19 | RBAC | viewer/operator/admin role boundaries enforced |
| 20 | Audit log | every mutating RPC produces an entry; tail-follow works |
| 21 | Counter polling | GetCounters follow mode delivers updates |
| 22 | TraceFlow | deny verdict + matched rule for known-deny ACL |
| 23 | GetAclHitStats zero-hits-only | surfaces unused rules |
| 24 | Saga atomic rollback | 10-item batch with #5 failing → all 10 absent from store |
| 25 | Saga recovery | restart mid-rollback completes rollback |
| 26 | WatchEvents fan-out | multiple subscribers each receive events |
| 27 | gNMI Subscribe | gnmic receives Notification on PutVnet |
| 28 | Graceful shutdown | SIGTERM completes in < 30s under load |

---

## 20. Implementation order & milestones

Phase 2 modules are mostly independent — they can be parallelized within a milestone. The recommended order:

### Milestone PA — Infrastructure (3 weeks, blocks everything else)

| Order | Module | Why first |
|---|---|---|
| 1 | P2-M1 (etcd store) | All Phase 2 features assume production storage |
| 2 | P2-M2 (leader election) | Required to deploy multi-node; tests for everything else need it |
| 3 | P2-M3 (namespace) | Touches store interface; must land before capacity/schema/migration |

### Milestone PB — Admission (1.5 weeks, parallelizable)

Can be developed simultaneously since they all plug into `control_plane.go` independently:
* P2-M4 (capacity)
* P2-M5 (schema)

### Milestone PC — Operations (4 weeks, mostly sequential)

| Order | Module | Why this order |
|---|---|---|
| 1 | P2-M6 (HA orchestrator) | Migration CUTOVER depends on it |
| 2 | P2-M7 (migration) | Drain depends on it |
| 3 | P2-M8 (operations: cordon/drain/saga ApplyBatch) | Most user-facing; depends on M6+M7 |

### Milestone PD — Security & observability (2 weeks, parallelizable)

* P2-M9 (auth/TLS)
* P2-M10 (audit + counters)

### Milestone PE — Diagnostics & gNMI (2 weeks, parallelizable)

* P2-M11 (flow/diagnostics)
* P2-M12 (saga + gNMI)

**Total**: ~14 weeks for one engineer, ~8 weeks with 2–3 engineers running parallel tracks.

### After each milestone

Run the relevant subset of Phase 2 quality gates. **Do not advance** to the next milestone if any gate fails in the current milestone — fix it first.

### After Phase 2 completes

Run all 28 quality gates. Tag the commit `dashd-phase2-complete`. dashd is now feature-complete for the planned scope.

---

## Annex A — Review Response Tracker

This plan has been updated in response to the architectural review at
[`docs/dashd-impl-plan-review.md`](../../docs/dashd-impl-plan-review.md).

| Review item | Status | Where fixed |
|---|---|---|
| **A1** — Namespace forklift | ✅ Fixed | §3 store table + §7 M3 rewritten — Phase 1 owns structure, Phase 2 owns enforcement |
| **A3** — Leader-only components implicit | 📋 Specified below | Annex A §Leader-Only Components Table |
| **A7** — Auth interface needs sharpening | 📋 Specified below | Annex A §Authorizer Interface |
| **B5** — HA tier ordering | 📋 Flagged | Annex A §Tier Ordering Note |
| **B6** — Migration phase idempotency | 📋 Specified below | Annex A §Migration Phase Idempotency |
| **B7** — Saga compensation order | 📋 Specified below | Annex A §Saga Compensation |
| **B8** — Drain timeout interaction | 📋 Specified below | Annex A §Drain Timeout Interaction |
| **C3** — Broken audit doc reference | ✅ Fixed | §header + §Cross-references |

### Leader-Only Components Table (Review fix [A3])

Every Phase 2 package/goroutine MUST be classified. New packages are not
mergeable until they appear in this table. Instead of 30+ scattered
`if !elector.IsLeader()` checks, a single `leaderRole` table + interceptor
centralizes this decision in `server/grpc/interceptors.go`.

| Package / goroutine | Classification | Notes |
|---|---|---|
| `reconciler.Run` | **leaderOnly** | only leader runs reconcile loop |
| `dispatch.Manager.Start` | **leaderOnly** | workers only run on leader |
| `subscribe.PumpSet` | **leaderOnly** | subscribe pumps only on leader |
| `ha/orchestrator` (TriggerSwitchover, TriggerFailover) | **leaderOnly** | orchestration |
| `migration.Coordinator` | **leaderOnly** | migration state machine |
| `operations` (CordonDpu, DrainDpu) | **leaderOnly** | state mutation |
| `audit.Writer` | **leaderOnly** | single-writer invariant |
| `capacity.Tracker.Recount` | **leaderOnly** | only leader writes |
| `dispatch.worker.counterPoller` | **leaderOnly** | per-DPU polling |
| `ControlPlane.Put*`, `Delete`, `ApplyBatch`, `Reconcile` | **leaderOnly** | mutating RPCs |
| `ControlPlane.Get`, `List`, `SimulateApply` | **leaderOrFollower** | pure read from etcd |
| `ObservabilityService.GetDpuStatus`, `GetDrift` | **leaderOrFollower** | reads inventory/store |
| `ObservabilityService.WatchEvents`, `GetAuditLog`, `GetCounters` | **followerStream** | requires local broadcaster; reject if not running |
| `HaService.GetHaSetState`, `GetHaScopeState`, `GetFlowSyncStats` | **leaderOrFollower** | reads obs cache |
| `HaService.WatchHaEvents` | **followerStream** | requires local broadcaster |

The interceptor uses a `rpcRoles map[string]leaderRole` lookup; unknown methods default to `leaderOnly` (safe default).

### Authorizer Interface (Review fix [A7])

The auth interceptor's interface is designed for future extension from day one:

```go
type Subject struct {
    Principal  string              // user or service account
    Roles      []string            // e.g. ["operator", "admin"]
    Namespaces []string            // subject scope (empty = all namespaces)
    Attributes map[string]string   // for future ABAC extensions
}

type Authorizer interface {
    Authorize(ctx context.Context, sub Subject, verb, namespace, kind string) error
}
```

Phase 2-M9 implements `StaticTokenAuthorizer` (viewer/operator/admin).
OIDC/AAD is a config-swap against the same `Authorizer` interface — no code changes needed.
The `Subject.Namespaces` field enables per-namespace RBAC without interface changes.

### Tier Ordering Note (Review fix [B5])

The tier table in `placement/order.go` should place HA kinds (`ha_set`, `ha_set_config`,
`ha_scope`, `ha_scope_config`) at **tier 2** (after `vnet`, before `eni`), not tier 5.
HA configuration must be set up on a DPU **before** ENIs that participate, so the DPU
already knows its HA peer on first ENI creation. Phase 1 should update its tier table.

### Migration Phase Idempotency (Review fix [B6])

Each migration phase action must be either idempotent on retry OR use
`phase=X_IN_PROGRESS` pre-persist. Missing idempotency is the #1 source of migration bugs.

| Phase | Idempotent? | Crash recovery |
|---|---|---|
| VALIDATED | ✅ Yes | Safe to re-run |
| INITIALIZED | ✅ Yes — Apply is idempotent | Re-Apply is no-op |
| DUAL_WRITE | ✅ Yes — Apply is idempotent | Re-Apply is no-op |
| FLOW_DRAIN | ⚠ Partially — timer restarts | Pre-persist `FLOW_DRAIN_IN_PROGRESS`; resume polls |
| CUTOVER | ⚠ Check obs cache first — if dest already ACTIVE, skip | Pre-persist `CUTOVER_IN_PROGRESS` |
| VERIFICATION | ✅ Yes — read-only | Safe to re-run |
| CLEANUP | ✅ Yes — Delete is idempotent | Re-Delete is no-op |
| COMMITTED | ✅ Yes — store.Put is idempotent | Re-persist is no-op |

Test case added: "Restart dashd between Execute and Persist → session resumes correctly."

### Saga Compensation (Review fix [B7])

Compensation details for `ApplyBatch` saga rollback:

1. **Compensation order**: delete in reverse dependency-tier order (tier 5 → tier 1)
2. **Best-effort policy**: retry up to 3 times with 1s backoff per item; if still failing, mark saga `SagaFailed` ("stuck for operator")
3. **Audit log distinction**: compensation entries tagged `audit_source: "saga_compensation"` to distinguish from user-initiated deletes

### Drain Timeout Interaction (Review fix [B8])

When `DrainDpu.Timeout` is reached but ENIs are still mid-migration:

1. **Stop accepting new migrations** — no more `startMigrationSession` calls
2. **In-flight migrations get remaining time** — each active migration's per-phase timeout = `min(original_timeout, drain_remaining_time)`
3. **Return detailed progress** — `DrainProgress{state: TIMEOUT, succeeded: [...], inflight: [...], queued: [...]}`
4. The operator decides: rollback in-flight, wait longer, or force-cordon

---

## Cross-references

* Phase 1 plan: [`impl-plan-basic.md`](impl-plan-basic.md)
* LLD: [`specs/LLD/dashd.md`](../LLD/dashd.md)
* Proto surface: [`proto/dashcenter/v1/`](../../proto/dashcenter/v1/)
* Per-DPU proto: [`proto/dashapi/v1/`](../../proto/dashapi/v1/)
* Kinds registry: [`src/impl-go/dashapi-runtime/kinds/kinds.go`](../../src/impl-go/dashapi-runtime/kinds/kinds.go)
* Production-gap audit (21 gaps): [`docs/dash-sim-on-par-with-sonic-audit.md`](../../docs/dash-sim-on-par-with-sonic-audit.md)
* ENI migration spec: [`specs/ENI-MIGRATION/`](../ENI-MIGRATION/)
