# Leader Election Reliability Hardening

> **Audience**: dashd maintainers, operators deploying multi-node
> controller fleets, anyone debugging leader loss events.
> **Scope**: etcd-backed leader election (`EtcdElector`), REST write
> throttling, elector lifecycle in `leaderLoop`.
> **Status**: ✅ Shipped 2026-06-15.

---

## Table of contents

1. [Problem statement](#1-problem-statement)
2. [Root cause analysis](#2-root-cause-analysis)
3. [Architecture of the fix](#3-architecture-of-the-fix)
4. [Implementation details](#4-implementation-details)
5. [Configuration](#5-configuration)
6. [Operator UX](#6-operator-ux)
7. [Test strategy](#7-test-strategy)
8. [Files changed](#8-files-changed)

---

## 1. Problem statement

In a 3-node dashd cluster (etcd-backed HA), a burst of 150+ REST PUTs
(e.g. `bootstrap.py` provisioning) could cause the leader to lose its
etcd lease. Once lost, the node became a **zombie** — still serving
REST/gRPC but never re-acquiring leadership or reconciling to DPUs.

**Impact**: silent data plane staleness. Objects accepted via REST
(200 OK) were persisted to etcd but never pushed to DPUs. The operator
sees `leader: false` on all nodes in `/admin/leader`.

**Trigger conditions**:
- Single-node etcd (demo/staging — the common development setup)
- Rapid-fire sequential PUTs without pacing
- `lease_ttl` set below 10s in config
- Resource-constrained VM (small CPU/disk IOPS)

---

## 2. Root cause analysis

### 2.1 Why leadership is lost

The etcd Go client's `concurrency.Session` maintains a keepalive
goroutine that sends periodic lease renewal RPCs. When the etcd server
is overwhelmed with store writes (all hitting the same Raft leader),
keepalive RPCs queue behind write commits. If the keepalive response
arrives after the lease TTL expires, the session dies.

```
Store client ─── 150 PUTs ──→ etcd server ← ─ keepalive RPC (queued)
                               ▲ Raft commit
                               │ latency spike
                               │
                Elector client ─── keepalive ──→ (delayed past TTL)
                                                  → lease expired
                                                  → session.Done() fires
                                                  → LostLeadership()
```

The store and elector use **separate etcd clients** — but they hit
the **same etcd server**. Under burst load, the server is the
bottleneck, not the client.

### 2.2 Why recovery failed (the critical bug)

After `LostLeadership()` fired, `leaderLoop` looped back and called
`AwaitLeadership()` on the **same elector**. But the elector's etcd
session was dead — `Campaign()` on a dead session always fails
because the underlying lease no longer exists.

```go
// BEFORE (broken):
func leaderLoop(elector Elector, ...) {
    for {
        elector.AwaitLeadership(ctx)  // ← fails on dead session
        // ...
        <-elector.LostLeadership()
        // loops back — same dead elector, Campaign always fails
    }
}
```

The error was non-nil, and `leaderLoop` returned permanently. The
node became a zombie — serving reads but never becoming leader again
until a full process restart.

---

## 3. Architecture of the fix

Four-layer defense:

### Layer 1: LeaderProxy — stable reference with hot-swap

A thread-safe wrapper (`leader.LeaderProxy`) delegates `IsLeader()`
and `LeaderID()` to the current inner elector. Admin server, cluster
aggregator, and all other consumers hold the proxy (stable reference).
`leaderLoop` swaps the inner elector when it creates a fresh one.

```
Admin Server ──→ LeaderProxy ──→ EtcdElector (current)
Cluster Agg ──→ LeaderProxy ──→     ↑
                                    │ Swap() on re-campaign
leaderLoop  ────────────────────────┘
```

### Layer 2: Elector factory in leaderLoop

`leaderLoop` accepts an `electorFactory` function. On every leadership
loss, it:
1. Closes the dead elector (releases etcd session)
2. Creates a fresh elector via the factory (new session + lease)
3. Swaps it into the proxy
4. Re-campaigns on the fresh elector

On campaign failure (e.g. etcd unreachable), exponential backoff
(2s → 30s) retries indefinitely. The node can **always recover
without a process restart**.

### Layer 3: Minimum lease TTL floor

`NewEtcdElector` enforces a 10s minimum TTL, regardless of user
config. Below 10s, the elector logs a warning and clamps:

```
WARN leader.etcd: lease_ttl below minimum, clamping configured=5s minimum=10s
```

### Layer 4: REST write rate limiter

A `writeThrottleMiddleware` in the REST server rate-limits mutating
requests (PUT/POST/DELETE/PATCH) to 200 RPS by default. Read requests
(GET) pass through unthrottled. When exceeded, returns:

```
HTTP 429 Too Many Requests
Retry-After: 1
{"error":"write rate limit exceeded, retry after 1s"}
```

---

## 4. Implementation details

### LeaderProxy (`leader/proxy.go`)

```go
type LeaderProxy struct {
    mu    sync.RWMutex
    inner Elector
}

func NewProxy(initial Elector) *LeaderProxy
func (p *LeaderProxy) Swap(next Elector)
func (p *LeaderProxy) IsLeader() bool     // delegates to inner
func (p *LeaderProxy) LeaderID() string   // delegates to inner
func (p *LeaderProxy) Inner() Elector     // for leaderLoop access
```

### leaderLoop (`cmd/dashd/main.go`)

```go
func leaderLoop(
    rootCtx     context.Context,
    proxy       *leader.LeaderProxy,
    newElectorFn electorFactory,    // ← fresh elector on each retry
    inv, pumpSet, mgr, rec ...
)
```

Key behaviors:
- On `LostLeadership()`: close dead elector → create fresh → swap → loop
- On campaign failure: exponential backoff 2s→30s, retry forever
- On shutdown (`rootCtx.Done()`): clean exit

### TTL floor (`leader/etcd.go`)

```go
const minLeaseTTL = 10 * time.Second
if leaseTTL < minLeaseTTL {
    slog.Warn("leader.etcd: lease_ttl below minimum, clamping", ...)
    leaseTTL = minLeaseTTL
}
```

### Write throttle (`rest/server.go`)

```go
func writeThrottleMiddleware(rps float64) func(http.Handler) http.Handler
```

Token-bucket limiter via `golang.org/x/time/rate`. Burst = RPS.

---

## 5. Configuration

| Setting | Where | Default | Notes |
|---------|-------|---------|-------|
| `ha.controller.elector.lease_ttl` | dashd.yaml | 15s | Floor: 10s (clamped with warning) |
| `WriteRateLimit` | `rest.Options` | 200 RPS | -1 to disable; 0 = use default |

No new config file fields are required. The TTL floor and write
throttle are automatic.

---

## 6. Operator UX

### Observing leadership recovery in logs

```json
{"msg":"leaderLoop: lost leadership — tearing down and re-creating elector"}
{"msg":"leader.etcd: assumed leadership","node_id":"dashd-1"}
{"msg":"leaderLoop: assumed leadership, starting leader-only subsystems"}
```

### Observing TTL clamping

```json
{"msg":"leader.etcd: lease_ttl below minimum, clamping","configured":"5s","minimum":"10s"}
```

### Observing write throttling

```
HTTP/1.1 429 Too Many Requests
Retry-After: 1
{"error":"write rate limit exceeded, retry after 1s"}
```

### Confirming leader health

```bash
./show-leader.sh
# NODE      LEADER  DETAIL
# dashd-1   true    leader_id=dashd-1 lease_ttl=s
# dashd-2   false   leader_id=dashd-1 lease_ttl=s
# dashd-3   false   leader_id=dashd-1 lease_ttl=s
```

---

## 7. Test strategy

### Unit tests added

| File | Tests | Coverage |
|------|-------|----------|
| `leader/proxy_test.go` | 7 tests (init, nil, swap, delegate, concurrent, inner access) | 100% on proxy.go |
| `rest/write_throttle_test.go` | 6 tests (GET pass, PUT allow/deny, all methods, independence, default) | 100% on writeThrottleMiddleware |

### TTL floor coverage

The existing etcd test harness uses `LeaseTTL: 2s` and `5s` — both
trigger the clamping path. All 27 existing leader tests pass with the
floor active (visible in test output as `WARN` logs).

### Integration verification

```bash
# On Linux VM:
./start-fleet.sh --with-console
./provision.sh                    # 157 objects, no leader loss
./show-leader.sh                  # one node shows leader=true
```

---

## 8. Files changed

| File | Change |
|------|--------|
| **NEW** `dashd/internal/ha/leader/proxy.go` | LeaderProxy with Swap/IsLeader/LeaderID/Inner |
| **NEW** `dashd/internal/ha/leader/proxy_test.go` | 7 unit tests, 100% coverage |
| **NEW** `dashd/internal/server/rest/write_throttle_test.go` | 6 unit tests, 100% coverage |
| `dashd/cmd/dashd/main.go` | leaderLoop with electorFactory + proxy wiring |
| `dashd/internal/ha/leader/etcd.go` | Minimum 10s TTL floor with warning log |
| `dashd/internal/server/rest/server.go` | writeThrottleMiddleware + Options.WriteRateLimit |
| `deploy/test-setup/05-full-console/manifest/bootstrap.py` | Retry logic + section pacing |
| `deploy/test-setup/06-fleet-ui-diagnostics/manifest/bootstrap.py` | Same |
| `deploy/test-setup/05-full-console/configs/dashd-{1,2,3}.yaml` | lease_ttl 8s → 15s |
