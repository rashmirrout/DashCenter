# dashw Web Console — Scalability & Production Design Analysis

> **Purpose.** Living document capturing deep analysis, design
> discussions, and decisions on dashw's production readiness:
> scalability, caching, fault tolerance, multi-user sessions, and
> operational resilience.
>
> **Format.** Each analysis session is captured as a dated section with
> requirements, analysis, options, verdicts, and action items. Verdicts
> accumulate — later sessions build on earlier ones.
>
> **Companion documents:**
> - [`specs/HLD/dashw-web-hld.md`](dashw-web-hld.md) — architecture
> - [`specs/LLD/dashw-web-lld.md`](../LLD/dashw-web-lld.md) — implementation
> - [`specs/Impl-Plan/dashw-web-impl-plan.md`](../Impl-Plan/dashw-web-impl-plan.md) — phases
> - [`specs/HLD/dashw-web-vision.md`](dashw-web-vision.md) — next-gen features

---

## Table of Contents

1. [Analysis Day 1 — Load, Caching, Sessions, Fault Tolerance](#analysis-day-1)
2. *(future sessions appended below)*

---

# Analysis Day 1 — Load, Caching, Sessions, Fault Tolerance

**Date:** 2026-06-11
**Participants:** Architecture review
**Trigger:** Production readiness review of dashw BFF design

---

## 1. Problem Statement

The current dashw BFF design (HLD §5, LLD §3-§8) is:
- **Stateless** Go binary — no database, no Redis, no session store
- Uses `singleflight.Group` for concurrent request coalescing
- No explicit caching layer
- No multi-user session management
- No rate limiting
- Each WebSocket connection opens a dedicated gRPC stream to dashd

**Questions to answer:**
1. How much load does dashw put on dashd for serving web pages?
2. It is read-heavy and multiple users can access — how does it scale?
3. Does it use cache? If so, what mechanism and strategy? How useful?
4. Does it use external cache like Redis?
5. Though stateless, how does it handle multi-user web sessions?
6. What production-ready patterns are missing?

---

## 2. Load Analysis — The Amplification Problem

### 2.1 Request amplification math

Each browser tab generates N polling requests per view:

| View | REST polls per refresh cycle | dashd calls per poll (fan-out) | Effective dashd load |
|---|---|---|---|
| Dashboard | 4 panels × 10s interval | fleet/summary → 4 fan-out | 16 dashd calls / 10s |
| Fleet | 2 panels × 15s | topology → 4 fan-out | 8 / 15s |
| DPU | 3 panels × 15s | dpu/detail → 6 fan-out | 18 / 15s |
| Vnet | 2 panels × 30s | vnet/canvas → 4 fan-out | 8 / 30s |
| Other views | ~2 each × 15-30s | ~1-2 fan-out | ~4 / 15s |

**Single operator, single active tab:** ~20-30 dashd calls per 10s cycle.

### 2.2 Multi-user amplification

| Concurrent operators | Active tabs (avg 2) | dashd calls/10s (no cache) | dashd calls/10s (with cache) |
|---|---|---|---|
| 1 | 2 | ~50 | ~50 (first user, cache cold) |
| 5 | 10 | ~250 | ~50 (cache warm, 5× reduction) |
| 10 | 20 | ~500 | ~50 (cache warm, 10× reduction) |
| 50 | 100 | ~2,500 | ~50 (cache warm, 50× reduction) |
| 100 | 200 | ~5,000 | ~50 (cache warm, 100× reduction) |

**Key insight:** Without caching, dashd load scales linearly with user
count. With a 5-second TTL in-process cache, dashd load is **constant
regardless of user count** (determined solely by cache TTL, not user count).

### 2.3 WebSocket stream amplification

Current design: each WS connection = one dedicated gRPC stream.

| Concurrent operators | WS connections per tab | gRPC streams (no hub) | gRPC streams (with hub) |
|---|---|---|---|
| 1 | 1-3 | 1-3 | 1-3 |
| 10 | 10-30 | 10-30 | 9 (one per stream type) |
| 50 | 50-150 | 50-150 | 9 |
| 100 | 100-300 | 100-300 | 9 |

**Without fan-out hub:** 100 operators = 300 gRPC streams to dashd. dashd
must maintain 300 concurrent server-streams — potentially overwhelming.

**With fan-out hub:** 100 operators = 9 gRPC streams to dashd. Each
stream type has one shared subscription, fanned out in-process.

### 2.4 Verdict: Load

| Concern | Severity | Fix |
|---|---|---|
| REST polling amplification | **Critical** at >10 users | In-process TTL cache |
| Aggregation fan-out amplification | **Critical** at >10 users | In-process TTL cache |
| WebSocket stream duplication | **High** at >20 users | Server-side fan-out hub |
| Write (mutation) amplification | **Low** (mutations are rare) | No fix needed; pass-through |

---

## 3. Caching Strategy — Deep Analysis

### 3.1 Requirements

| Requirement | Priority |
|---|---|
| Reduce dashd load to constant regardless of user count | **P0** |
| Zero external dependencies (no Redis, no Memcached) | **P0** for v1 |
| Cache invalidation on mutations (PUT/DELETE must see fresh data) | **P0** |
| Stale-while-revalidate (serve stale on dashd unavailability) | **P1** |
| Cross-instance cache coherence (multi-replica BFF) | **P2** (post-v1) |
| Per-user cache isolation | **Not needed** (all users see the same fleet data) |

### 3.2 Option analysis

#### Option A: In-Process TTL Cache ✅ RECOMMENDED (v1)

```go
type CacheEntry struct {
    Data      []byte
    ExpiresAt time.Time
    StaleAt   time.Time  // stale-while-revalidate window
}

type Cache struct {
    mu    sync.RWMutex
    items map[string]*CacheEntry
}
```

| Dimension | Assessment |
|---|---|
| **Complexity** | Low — ~100 lines of Go code |
| **Dependencies** | Zero (stdlib only) or lightweight: `patrickmn/go-cache` (~200KB) |
| **Memory** | ~50 cache entries × 10KB avg = **500KB**. Trivial. |
| **Latency** | Sub-microsecond (in-process map lookup) |
| **Staleness** | Configurable per endpoint (5s for fast-poll, 30s for slow-poll) |
| **Invalidation** | On mutation: delete affected cache keys. Simple key-pattern matching. |
| **Multi-replica** | Each replica has independent cache. Redundant dashd calls across replicas. Acceptable at ≤3 replicas. |
| **dashd load** | **Constant O(1)** regardless of user count. One dashd call per TTL expiry per endpoint. |
| **Failure mode** | If cache fills memory (impossible at 500KB), LRU eviction. If cache corrupts, restart clears it. |

**Why this is sufficient for v1:**
- All cache content is **derived** from dashd — it's not user-specific
- Cache is **small** (50 entries, 500KB) — no memory pressure
- Cache is **ephemeral** — restart clears it, no consistency issues
- TTL matches frontend expectations (TanStack Query already expects 5-10s staleness)

#### Option B: External Cache (Redis) ❌ NOT recommended for v1

| Dimension | Assessment |
|---|---|
| **Complexity** | Medium — Redis client, serialization, connection management |
| **Dependencies** | Redis server + Go client library (`go-redis/redis`) |
| **Memory** | External (Redis default 64MB) — massively over-provisioned for our use case |
| **Latency** | 0.1-0.5ms (network round-trip to Redis) vs sub-μs for in-process |
| **Staleness** | Same configurable TTL |
| **Invalidation** | Same pattern, but via Redis `DEL` |
| **Multi-replica** | **This is Redis's strength** — shared cache across BFF replicas |
| **dashd load** | Same constant O(1) |
| **Failure mode** | Redis down → cache miss → fallback to dashd (graceful). But adds operational complexity. |

**When Redis becomes necessary:**
- **>3 BFF replicas** serving the same fleet: redundant dashd calls across replicas become wasteful
- **>1000 concurrent users**: in-process cache + fan-out hub still works at 1000 users per instance; Redis only helps if you need many instances
- **Cross-region deployment**: BFF replicas in different regions need shared cache

**For v1 (1-3 replicas, <100 users per instance):** Redis is pure overhead.

#### Option C: Reverse Proxy Cache (Nginx/Caddy) ⬜ DEFERRED

| Dimension | Assessment |
|---|---|
| **Complexity** | Low (config only, no code) |
| **Benefit** | Caches HTTP responses transparently |
| **Limitation** | Doesn't help with aggregation fan-out (BFF still computes on miss) |
| **Limitation** | Doesn't cache WebSocket streams |
| **Limitation** | Adds another component to deploy |

**Verdict:** Useful as an additional layer in front of a load balancer,
but doesn't replace in-process caching. Deferred to post-v1.

### 3.3 Cache key design

```
cache key format: "{endpoint}:{params_hash}"

Examples:
  "fleet/summary"                          → FleetSummary JSON
  "dpu/detail:dpu-1"                       → DpuDetail JSON for dpu-1
  "topology"                               → TopologyGraph JSON
  "vnet/detail:vnet-prod"                  → VnetDetail JSON for vnet-prod
  "vnet/canvas:vnet-prod"                  → VnetCanvasData JSON
  "capacity"                               → CapacityStats JSON
  "ha/sets"                                → HaSetOverview[] JSON
  "ha/detail:default:ha-set-prod"          → HaSetDetail JSON
  "capacity/fleet"                         → FleetCapacity JSON
  "drift/explain:dpu-3"                    → DriftExplanationList JSON
  "dependencies"                           → DependencyGraph JSON
```

### 3.4 Cache invalidation strategy

| Trigger | What to invalidate |
|---|---|
| `PUT /api/v1/{ns}/vnets/{name}` | `fleet/summary`, `topology`, `vnet/detail:{name}`, `vnet/canvas:{name}`, `dependencies` |
| `PUT /api/v1/{ns}/enis/{name}` | `fleet/summary`, `topology`, `dpu/detail:*`, `vnet/detail:*`, `vnet/canvas:*`, `capacity`, `dependencies` |
| `PUT /api/v1/{ns}/acl-policies/{name}` | `dependencies` |
| `PUT /api/v1/{ns}/route-policies/{name}` | `dependencies` |
| `PUT /api/v1/{ns}/service-tunnels/{name}` | `vnet/detail:*`, `vnet/canvas:*`, `dependencies` |
| `POST /api/v1/reconcile` | All keys (full flush) |
| `POST /api/v1/inventory/{id}/cordon` | `fleet/summary`, `topology`, `dpu/detail:{id}` |
| `POST /api/v1/inventory/{id}/drain` | `fleet/summary`, `topology`, `dpu/detail:{id}`, `capacity` |

**Implementation:** The BFF REST proxy intercepts mutation responses.
On successful `2xx` for a write method (PUT/POST/DELETE), it calls
`cache.InvalidatePattern(patterns)` before returning the response.

### 3.5 Stale-while-revalidate

```
Timeline:
  t=0s    Cache populated (fresh)
  t=5s    TTL expires → entry becomes "stale"
  t=5s-35s  Stale window (30s)
  t=35s   Entry evicted

Request at t=7s (stale):
  1. Return cached data immediately (stale but fast)
  2. Set response header: X-Cache: stale
  3. Trigger background goroutine to refresh from dashd
  4. When background refresh completes, update cache entry
  5. Next request at t=8s gets the refreshed data (fresh)

Request at t=40s (evicted):
  1. Cache miss
  2. Synchronous dashd call
  3. Populate cache
  4. Return fresh data
```

**Why this matters:** When dashd restarts or is briefly unreachable,
operators still see data (stale but valid). The staleness indicator in
the frontend shows "Last updated Xs ago" but the page doesn't break.

### 3.6 Verdict: Caching

| Decision | Rationale |
|---|---|
| **In-process TTL cache with stale-while-revalidate** | Zero dependency, 500KB memory, 50× dashd load reduction, constant O(1) backend load |
| **No Redis for v1** | Over-engineered for <100 users. Add when >3 replicas needed. |
| **Mutation-aware invalidation** | BFF proxy intercepts write responses and flushes affected cache keys |
| **Per-endpoint TTL** | Fast-poll: 5s, slow-poll: 30s, stale window: 30s beyond TTL |

---

## 4. WebSocket Fan-Out Hub — Deep Analysis

### 4.1 Problem

Current design: 1 WS connection = 1 gRPC stream to dashd.

```
Browser A ── WS /ws/dpu-status ── BFF ── gRPC GetDpuStatus ── dashd
Browser B ── WS /ws/dpu-status ── BFF ── gRPC GetDpuStatus ── dashd
Browser C ── WS /ws/dpu-status ── BFF ── gRPC GetDpuStatus ── dashd
```

3 browsers = 3 gRPC streams carrying identical data.

### 4.2 Solution: Fan-Out Hub

```go
// StreamHub manages one shared gRPC stream per stream type,
// fanning received messages out to all subscribed WebSocket clients.
type StreamHub struct {
    mu          sync.RWMutex
    streams     map[string]*SharedStream  // key = stream type (e.g., "dpu-status")
}

type SharedStream struct {
    cancel      context.CancelFunc
    subscribers map[string]chan<- []byte   // key = subscriber ID (e.g., WS connection ID)
    mu          sync.RWMutex
    refCount    int32
}
```

**Lifecycle:**
1. First WS client connects to `/ws/dpu-status`
2. Hub checks: is there an active `SharedStream` for "dpu-status"?
   - No → open gRPC stream to dashd, create `SharedStream`, start pump goroutine
   - Yes → subscribe to existing `SharedStream`
3. Pump goroutine: reads from gRPC stream, fans out to all subscriber channels
4. WS client disconnects → unsubscribe from `SharedStream`, decrement refCount
5. refCount reaches 0 → cancel gRPC stream context, remove `SharedStream`

**Failure handling:**
- gRPC stream disconnects → reconnect with backoff, notify all subscribers via error frame
- During reconnect gap, subscribers see `{"type":"error","code":"STREAM_INTERRUPTED"}`
- On reconnect, gRPC stream sends initial snapshot (protocol behavior) — subscribers get full state refresh

### 4.3 Per-stream-type vs per-parameter streams

Some streams are parameterized:
- `/ws/flows/{dpuId}` — per-DPU flow stream
- `/ws/counters/{dpuId}` — per-DPU counter stream
- `/ws/acl-hits/{eniName}` — per-ENI ACL hit stream

Hub key must include the parameter:

```
Hub key: "flows:dpu-1"    → shared stream for flows on DPU-1
Hub key: "flows:dpu-2"    → separate shared stream for DPU-2
Hub key: "dpu-status"     → shared stream for all DPU status (global)
Hub key: "events"         → shared stream for all events (global)
```

If 5 operators are all viewing DPU-1 flows, they share one gRPC stream.
If they're viewing different DPUs, each DPU gets its own gRPC stream.

### 4.4 Memory and goroutine budget

| Component | Per connection | 50 users | 100 users |
|---|---|---|---|
| WS read goroutine | 1 | 50 | 100 |
| WS write goroutine | 1 | 50 | 100 |
| WS read buffer | 4KB | 200KB | 400KB |
| WS write buffer | 4KB | 200KB | 400KB |
| Subscriber channel | 1 (buffered 16) | 50 | 100 |
| gRPC streams (with hub) | Shared | ~9-15 | ~9-15 |
| Hub pump goroutines | Per shared stream | ~9-15 | ~9-15 |
| **Total goroutines** | | ~115 | ~215 |
| **Total memory** | | ~12MB | ~24MB |

Go handles 10,000+ goroutines routinely. 215 goroutines is trivial.

### 4.5 Verdict: WebSocket Fan-Out

| Decision | Rationale |
|---|---|
| **Implement StreamHub with shared gRPC streams** | Reduces gRPC stream count from O(users) to O(stream-types). Critical for >10 users. |
| **Hub key includes parameters** | `/ws/flows/dpu-1` and `/ws/flows/dpu-2` are separate shared streams |
| **Graceful degradation on gRPC disconnect** | Error frame to all subscribers, reconnect with backoff, full snapshot on reconnect |
| **Reference counting for cleanup** | Last subscriber disconnects → close gRPC stream |

---

## 5. Multi-User Session Handling

### 5.1 What "stateless" means in practice

The BFF stores **zero per-user state**. All user-specific state lives
in the browser:

| State | Location | Persistence |
|---|---|---|
| UI preferences | Browser `localStorage` (Zustand persist) | Across sessions |
| Trace history | Browser session memory (Zustand) | Current tab only |
| Command history | Browser session memory | Current tab only |
| Current view/route | Browser URL | Shareable/bookmarkable |
| Auth token (Phase B+) | Browser `httpOnly` cookie | Expires with session |
| WebSocket connections | Browser tab | Current tab only |

### 5.2 What the BFF does per user

| Concern | BFF handling |
|---|---|
| **Connection tracking** | WS connections tracked by connection ID (for fan-out hub). No per-user grouping. |
| **Request isolation** | Go HTTP server handles each request independently. No shared state between requests from the same user. |
| **Auth propagation** | Token/cert from browser forwarded to dashd. BFF never stores credentials. |
| **Resource cleanup** | On WS close: unsubscribe from hub, decrement refCount. On HTTP idle: standard Go HTTP timeout. |

### 5.3 Multi-tab handling

One user may have 5 browser tabs open. Each tab:
- Has its own Zustand stores (independent SPA instances)
- Opens its own WS connections (independent WebSocket objects)
- Makes its own REST polls (independent TanStack Query instances)

The BFF doesn't know (or care) that 5 connections come from the same user.
The fan-out hub treats all subscribers equally — this is correct behavior.

### 5.4 Verdict: Sessions

| Decision | Rationale |
|---|---|
| **No server-side sessions** | All user state is client-side. BFF is truly stateless. |
| **No Redis for sessions** | Nothing to store. Auth token is in cookie, forwarded to dashd. |
| **Multi-tab is fine** | Each tab is an independent client. Fan-out hub handles dedup. |

---

## 6. Fault Tolerance & Resilience

### 6.1 Failure mode analysis

| Failure | Impact | Current handling | Improved handling |
|---|---|---|---|
| **dashd REST down** | All REST polls fail | BFF returns 502 | Return stale cache + `X-Cache: stale` header. SPA shows staleness warning. |
| **dashd gRPC down** | All WS streams break | WS receives close frame | Hub reconnects with backoff. WS clients get error frame. SPA shows "Reconnecting..." |
| **dashd slow (>5s)** | Requests timeout | BFF returns 504 | Return stale cache immediately. Background refresh. |
| **BFF crash** | All connections lost | Process restarts (container orchestrator) | SPA auto-reconnects WS. REST polls resume. No data loss (stateless). |
| **BFF memory pressure** | Slow/OOM | No handling | Cache LRU eviction. Goroutine budget monitoring. `pprof` endpoint. |
| **Network partition** | Intermittent failures | Retry at transport level | Circuit breaker: after 5 failures in 30s, stop calling dashd for 30s. Serve stale. |
| **dashd returns errors** | 4xx/5xx from dashd | Pass through | Cache last-good response. Return error + stale data in parallel. |
| **Browser goes to sleep** | WS times out | WS closes | `useWebSocket` auto-reconnects on focus. WS pong timeout detects dead connection. |

### 6.2 Circuit breaker for dashd calls

```go
type CircuitBreaker struct {
    state       State  // CLOSED, OPEN, HALF_OPEN
    failures    int32
    lastFailure time.Time
    threshold   int32         // 5 failures
    timeout     time.Duration // 30s
}

// Call wraps a dashd request with circuit breaker logic
func (cb *CircuitBreaker) Call(fn func() error) error {
    if cb.state == OPEN {
        if time.Since(cb.lastFailure) > cb.timeout {
            cb.state = HALF_OPEN  // try one request
        } else {
            return ErrCircuitOpen  // fail fast
        }
    }
    err := fn()
    if err != nil {
        cb.failures++
        cb.lastFailure = time.Now()
        if cb.failures >= cb.threshold {
            cb.state = OPEN
        }
        return err
    }
    cb.state = CLOSED
    cb.failures = 0
    return nil
}
```

When circuit is OPEN: return stale cached data immediately.

### 6.3 Health probes (Kubernetes-ready)

| Endpoint | Type | Checks | Failure response |
|---|---|---|---|
| `GET /healthz` | Liveness | Process is alive, not deadlocked | 503 → K8s restarts pod |
| `GET /readyz` | Readiness | dashd REST reachable + at least 1 cache entry populated | 503 → K8s removes from LB |

`/readyz` ensures new BFF instances don't receive traffic until they've
populated at least one cache entry (warm-up).

### 6.4 Graceful shutdown sequence

```
SIGTERM received
  │
  ├── 1. Stop accepting new HTTP connections
  ├── 2. Send WS close frame to all connected clients
  ├── 3. Cancel all gRPC stream contexts (via hub)
  ├── 4. Wait for in-flight HTTP requests to complete (15s timeout)
  ├── 5. Flush metrics (if enabled)
  └── 6. Exit 0
```

### 6.5 Verdict: Fault Tolerance

| Decision | Rationale |
|---|---|
| **Stale-while-revalidate on dashd failures** | Operators see data (possibly stale) vs blank page |
| **Circuit breaker for dashd calls** | Prevent thundering herd on dashd recovery |
| **Kubernetes readiness probe** | New instances don't receive traffic until cache warm |
| **Graceful shutdown** | Clean WS close + gRPC cancel + request drain |

---

## 7. Rate Limiting

### 7.1 Read rate limiting

**Not needed.** The in-process cache makes reads effectively free.
100 operators polling every 10s = 100 cache hits/10s = negligible.

### 7.2 Write rate limiting

Mutations (PUT/DELETE) pass through to dashd and affect fleet state.
Accidental bulk mutations (script gone wrong, double-click) should be limited.

| Limit | Value | Scope |
|---|---|---|
| Mutations per second | 10 | Per source IP |
| Mutations per minute | 100 | Per source IP |
| Batch size | 50 objects | Per ApplyBatch request |

**Implementation:** Go `golang.org/x/time/rate` token bucket limiter
keyed by `X-Real-IP` or `X-Forwarded-For`.

### 7.3 Verdict: Rate Limiting

| Decision | Rationale |
|---|---|
| **No read rate limiting** | Cache handles it |
| **Write rate limiting: 10/s, 100/min per IP** | Prevents accidental bulk mutations |
| **Batch size limit: 50** | Prevents single request from overwhelming dashd dispatch |

---

## 8. Horizontal Scaling Architecture

### 8.1 Single instance capacity

| Metric | Capacity |
|---|---|
| Concurrent users | **50-100** per instance |
| REST requests handled | ~1000/s (cache hits, Go HTTP is fast) |
| WS connections | ~300 per instance |
| gRPC streams to dashd | ~9-15 per instance (shared via hub) |
| Memory | ~50-100MB |
| CPU | <1 core (cache hits are sub-μs) |

### 8.2 Multi-instance scaling

```
                        ┌─── BFF-1 (cache + hub) ───┐
LB (round-robin) ──────┤─── BFF-2 (cache + hub) ───┼── dashd
                        └─── BFF-3 (cache + hub) ───┘
```

| Instances | Users | dashd gRPC streams | dashd REST (cache miss) |
|---|---|---|---|
| 1 | 50-100 | ~15 | ~50/10s |
| 2 | 100-200 | ~30 | ~100/10s |
| 3 | 150-300 | ~45 | ~150/10s |

**Note:** Each instance has its own cache, so dashd calls scale
linearly with instances (not users). Redis becomes beneficial at
5+ instances to share cache.

### 8.3 WS sticky sessions

WebSocket connections must be sticky to one BFF instance (WS is
stateful). Load balancer config:

```nginx
upstream dashw {
    # IP-hash for WS stickiness
    ip_hash;
    server bff-1:8080;
    server bff-2:8080;
    server bff-3:8080;
}
```

Or use connection-level stickiness (HTTP upgrade is already per-connection).

### 8.4 When to add Redis

| Signal | Threshold | Action |
|---|---|---|
| >3 BFF replicas | Redundant dashd calls | Add Redis as shared cache |
| >500 concurrent users | Memory per instance | Add Redis + increase replicas |
| Cross-region deployment | Latency to dashd | Add Redis in each region |
| Cache hit ratio drops | Monitoring shows <80% | Increase TTL or add Redis |

---

## 9. Summary — All Decisions

| # | Decision | Status | Phase |
|---|---|---|---|
| 1 | In-process TTL cache with stale-while-revalidate | **APPROVED** | Phase A |
| 2 | No Redis for v1 | **APPROVED** | — |
| 3 | WebSocket fan-out hub (shared gRPC streams) | **APPROVED** | Phase B |
| 4 | No server-side sessions | **APPROVED** | — |
| 5 | Mutation-aware cache invalidation | **APPROVED** | Phase A |
| 6 | Circuit breaker for dashd calls | **APPROVED** | Phase A |
| 7 | Kubernetes readiness probe (`/readyz`) | **APPROVED** | Phase A |
| 8 | Graceful shutdown (WS close + gRPC cancel + drain) | **APPROVED** | Phase A |
| 9 | Write rate limiting (10/s, 100/min per IP) | **APPROVED** | Phase A |
| 10 | Stale-while-revalidate on dashd failures | **APPROVED** | Phase A |
| 11 | WS sticky sessions for LB | **APPROVED** | Deployment |
| 12 | Redis consideration at >3 replicas / >500 users | **DEFERRED** | Post-v1 |

---

## 10. Action Items from Day 1

| # | Action | Target document | Status |
|---|---|---|---|
| AI-1 | Add "Scalability & Resilience" section to HLD | `dashw-web-hld.md` | ✅ Added §14 with cache, hub, circuit breaker, rate limiter, health probes, scaling |
| AI-2 | Add cache implementation to LLD (TTL cache, invalidation, stale-while-revalidate) | `dashw-web-lld.md` | ⬜ Deferred to Day 2 (LLD update is large; impl-plan gates have AI Agent Instructions) |
| AI-3 | Add fan-out hub implementation to LLD | `dashw-web-lld.md` | ⬜ Deferred to Day 2 |
| AI-4 | Add circuit breaker implementation to LLD | `dashw-web-lld.md` | ⬜ Deferred to Day 2 |
| AI-5 | Add rate limiter implementation to LLD | `dashw-web-lld.md` | ⬜ Deferred to Day 2 |
| AI-6 | Add readiness probe to LLD | `dashw-web-lld.md` | ⬜ Deferred to Day 2 |
| AI-7 | Add cache + hub + resilience gates to impl-plan Phase A | `dashw-web-impl-plan.md` | ✅ Added A1b sub-phase with 6 gates (A1b-G1 through A1b-G6) |
| AI-8 | Move fan-out hub from Phase B to Phase A (needed for scale) | `dashw-web-impl-plan.md` | ✅ Fan-out hub infra in A1b; WS bridge stays in Phase B (hub is used by bridge) |

---

*(End of Day 1 analysis. Future sessions will be appended below.)*