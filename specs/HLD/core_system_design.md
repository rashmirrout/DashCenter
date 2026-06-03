# Core System Design: Reads, Writes & Compute Pipeline

This document is the authoritative specification for **how DASHCenter actually moves data** between operators, its internal state stores, and the live DASH appliances. Where the [Centralized HLD](high_level_system_design.md) and the [Controllerless HLD](high_level_system_design_controllerless.md) define the *topology*, this document defines the **operational pipelines** that run on top of that topology:

1. The **Read Pipeline** — how cached state is built and served.
2. The **Compute Pipeline** — how derived answers (ACL match, packet transposition, trace) are produced.
3. The **Write Pipeline** — how operator-initiated mutations (ENI MAC change, new ACL, route add) are validated, staged, dispatched to the DPU, and committed.

The same pipelines apply to **both deployment models** — the only difference is *where the daemon runs* (dedicated controller vs. embedded on every DPU).

---

## 1. Design Principles

| Principle | Implication |
|---|---|
| **Cache is a projection, never the source of truth** | The DPU's DASH-SAI tables are always authoritative. Redis is a fast, queryable mirror that can be rebuilt at any time. |
| **Reads should never block on the DPU** (steady state) | Operators get sub-100 ms answers from cache. Live calls are opt-in (`--live`) or triggered when the cache is known stale. |
| **Writes are never silently optimistic** | Every mutation is **validated, staged in a Pending Copy, dispatched, ACKed, and only then committed** to the canonical store. A failed dispatch never leaves the cache lying. |
| **Compute is memoized, not free** | The first ACL/route trace is computed against the DPU (or the cache); the result is keyed and stored so identical follow-up queries are served instantly until the underlying state mutates. |
| **Every mutation invalidates a precise scope** | Writes carry a *touched-keys* manifest so the compute cache and any dependent indexes can invalidate the minimum required surface — no global flushes. |
| **Phased rollout** | Phase 1 computes live against the DPU on every request (correctness first). Phase 2 introduces the validated-cache fast path (latency second). |

---

## 2. State Stores

DASHCenter maintains four logically separate stores. All four live inside the same Redis Stack instance (or per-node Redis in the controllerless model) but are addressed by disjoint key prefixes.

| Store | Key prefix | Contents | Lifetime |
|---|---|---|---|
| **Canonical State DB** | `state:<appliance>:*` | Mirror of DPU configuration (ENIs, VNETs, ACLs, routes). Authoritative *within DASHCenter*; reconciled to the DPU. | Long-lived; rebuildable from DPU on cold start. |
| **Compute Cache** | `compute:<hash>` | Memoized results of derived queries (ACL match outcome, packet trace, route resolution). | TTL'd; invalidated on related state mutation. |
| **Pending Writes Copy** | `pending:<txn_id>` | In-flight mutations awaiting DPU ACK. | Cleared on commit or abort. |
| **TimeSeries Telemetry** | `metric:<appliance>:*` | RedisTimeSeries counters and drop vectors. | Retention window (e.g. 7 days). |

---

## 3. Read Pipeline

### 3.1 Asynchronous Cache Population (background daemon)

A pool of ingestion workers (one logical channel per appliance) continuously keeps `state:<appliance>:*` synchronized with the DPU. Two complementary mechanisms run side by side.

```
                  +-----------------------------------------------+
                  |     STATE INGESTION POOL (one per appliance)  |
                  +-----------------------+-----------------------+
                                          |
            +-----------------------------+-----------------------------+
            |                                                           |
            v                                                           v
  +----------------------+                                    +----------------------+
  | Event-Driven Stream  |   <-- preferred, low latency       | Periodic Reconciler  |
  | (gNMI Subscribe      |                                    | (gNMI / gRPC poll    |
  |  ON_CHANGE / DPU     |                                    |  every N seconds)    |
  |  push notifications) |                                    |                      |
  +----------+-----------+                                    +----------+-----------+
             |                                                           |
             |  delta events                                  full-table snapshots
             v                                                           v
       +-------------------------------------------------------------+
       |               State Normalizer (Protobuf -> Redis schema)   |
       +------------------------------+------------------------------+
                                      |
                                      v
                +------------------------------------------+
                |   Canonical State DB  (state:<appl>:*)   |
                |   - HSET on configuration objects        |
                |   - RediSearch indexes auto-update       |
                |   - Publishes Redis Stream `state:events`|
                +------------------------------------------+
                                      |
                                      v
                +------------------------------------------+
                |  Cache Invalidator (consumes events)     |
                |  - Drops affected compute:<hash> entries |
                +------------------------------------------+
```

* **Event-driven channel** is the steady-state mechanism. The DPU pushes deltas the moment a table row changes; the normalizer transforms the Protobuf message into the Redis hash schema and writes it under the appliance's key prefix.
* **Periodic reconciler** is a safety net. On a configurable interval it pulls full snapshots of *configuration* tables and diffs them against the cache. Any drift (caused by missed events, agent restart, or out-of-band changes) is corrected and emitted as a synthetic delta. This also covers cold start — if Redis is empty, the reconciler primes it before serving traffic.
* **State events stream** (`XADD state:events`) is the single fan-out point that downstream consumers (Cache Invalidator, audit log, web-socket watchers) subscribe to. This decouples ingestion from invalidation logic.

### 3.2 Read Request Flow (operator-initiated)

```
 [User]  dashctl get enis --namespace=prod
   │
   ▼
 [API Front Door]
   │
   ├─► Parse intent: is this a raw state read or a derived/computed read?
   │
   │   ┌──────────────────────────────────────────────┐
   │   │  RAW STATE READ (e.g. `get enis`)            │
   │   │  - RediSearch on state:*                     │
   │   │  - Return formatted JSON/table               │
   │   │  - Typical latency: 5-30 ms                  │
   │   └──────────────────────────────────────────────┘
   │
   └─► If `--live` or `--no-cache` flag: bypass cache, force live gRPC to DPU,
       write-through update the cache, then respond.  See §3.3.
```

### 3.3 Forced Live Read (cache bypass)

Used when an operator suspects cache drift or is investigating a transient.

1. Client adds header `X-Bypass-Cache: true` (or `--live` flag).
2. API Front Door routes to the appliance's gRPC channel pool, issues a synchronous `Get()` against the DPU agent.
3. Returned Protobuf is normalized and **write-through** updates the Canonical State DB (so the next non-live caller benefits).
4. Any `compute:<hash>` entries that depended on the touched keys are invalidated.

---

## 4. Compute Pipeline (derived answers)

Examples of compute requests: `dashctl explain match`, `dashctl trace flow`, `dashctl get acl-hit --src 10.0.0.4 --dst 20.0.0.8`, ENI bundle assembly, packet-walk simulations. These cannot be served by a raw `HGET` — they require running the request through the DPU's matching logic.

A dedicated **Compute Worker Pool** (separate thread/coroutine pool from ingestion) handles these.

```
                                 +-----------------------------+
   dashctl trace flow            |       API Front Door        |
   ─────────────────────────►    |  - Classify: compute request|
                                 +--------------+--------------+
                                                │
                                                ▼
                                 +-----------------------------+
                                 |     Request Hasher          |
                                 |  hash = H(request_kind,     |
                                 |         appliance, params,  |
                                 |         state_epoch)        |
                                 +--------------+--------------+
                                                │
                                                ▼
                                  ┌──── compute:<hash> exists? ────┐
                                  │                                │
                                  │ YES (cache hit)                │ NO (cache miss)
                                  ▼                                ▼
                       +-------------------+        +-----------------------------+
                       | Return cached     |        |   Dispatch to Compute       |
                       | result            |        |   Worker (thread/coroutine) |
                       +-------------------+        +--------------+--------------+
                                                                   │
                                            ┌──────── Phase 1 ─────┴──────── Phase 2 ────────┐
                                            │                                                │
                                            ▼                                                ▼
                          +---------------------------------+      +--------------------------------------+
                          | DIRECT LIVE COMPUTE             |      | CACHE COMPUTE + DPU VALIDATION       |
                          | - Send compute request to DPU   |      | 1. Compute against Canonical State DB|
                          |   agent (e.g. ACL_Match RPC)    |      | 2. Issue confirmation RPC to DPU     |
                          | - DPU returns authoritative     |      | 3. If they agree: store & return     |
                          |   answer                        |      | 4. If they disagree: log drift,      |
                          | - Format & return               |      |    trust DPU, force reconciler pass  |
                          +----------------+----------------+      +-----------------+--------------------+
                                           │                                         │
                                           └────────────────┬────────────────────────┘
                                                            ▼
                                          +-----------------------------------+
                                          |   Write to Compute Cache          |
                                          |   SET compute:<hash> <result>     |
                                          |   EX <ttl>                        |
                                          |   Tag with dependency keys for    |
                                          |   precise invalidation            |
                                          +-----------------------------------+
                                                            │
                                                            ▼
                                                       [ Reply ]
```

### 4.1 Why two phases?

| | **Phase 1 (correctness-first)** | **Phase 2 (latency-optimized)** |
|---|---|---|
| **First request** | DPU computes; ~50–200 ms | Cache computes + DPU validates; ~20–60 ms |
| **Repeat request** | DPU computes again; ~50–200 ms | Served from `compute:<hash>`; <5 ms |
| **Risk** | DPU load grows linearly with operator queries | Drift between cache compute and DPU |
| **Mitigation** | None needed | Validation RPC on first miss; drift triggers reconciler |

Phase 1 ships first to establish the API contract and absolute correctness. Phase 2 turns on the cache fast-path once we have telemetry confirming the cache truly mirrors the DPU.

### 4.2 Compute cache invalidation

Each `compute:<hash>` entry stores not just the result but the list of `state:<appliance>:*` keys it depended on (`dep:compute:<hash>` set). When the State Events stream fires for any of those keys, the Cache Invalidator does:

```
SUNION dep:compute:* matching touched key
DEL    affected compute:<hash> entries
```

This means a single ACL rule change invalidates only the traces that actually consulted that rule — not the global compute cache.

---

## 5. Write Pipeline

Writes are the most consequential operations DASHCenter performs. They must be **atomic from the operator's perspective**, **never half-applied**, and **always consistent with the DPU**.

### 5.1 The Five Stages

```
   ┌────────────────────────────────────────────────────────────────────────────────┐
   │                                  WRITE LIFECYCLE                               │
   └────────────────────────────────────────────────────────────────────────────────┘

   ┌──────────┐    ┌───────────┐    ┌──────────┐    ┌──────────┐    ┌──────────────┐
   │ 1.       │    │ 2.        │    │ 3.       │    │ 4.       │    │ 5.           │
   │ Receive  │ -> │ Validate  │ -> │ Stage in │ -> │ Dispatch │ -> │ Commit OR    │
   │ & Parse  │    │ vs Cache  │    │ Pending  │    │ to DPU   │    │ Rollback     │
   └──────────┘    └───────────┘    └──────────┘    └──────────┘    └──────────────┘
                                                                            │
                                                  ┌─────── success ─────────┘
                                                  │
                                                  ▼
                                       canonical DB updated,
                                       pending cleared,
                                       compute cache invalidated,
                                       200 OK to user
```

### 5.2 Stage-by-stage

#### Stage 1 — Receive & Parse

* Operator submits e.g. `dashctl set eni vnic-101 --mac 02:00:00:aa:bb:cc` or `POST /v1/appliances/dpu-01/enis/vnic-101`.
* Front Door issues a `txn_id` (UUID v7 — time-ordered) and binds it to the request.
* Request is classified by **scope**: per-appliance (most writes) vs. fleet-wide (rare, e.g. mass ACL push). Fleet writes are decomposed into N per-appliance transactions, each independently tracked under a parent `batch_id`.

#### Stage 2 — Validate against Canonical DB / Cache

A dedicated **Validator** runs the request against the current cached state. This catches obvious errors *before* touching the DPU.

Checks include:

* **Existence:** does `state:dpu-01:eni:vnic-101` exist? (404 if not)
* **Schema:** mandatory fields, value ranges, MAC format, CIDR validity.
* **Referential integrity:** does the VNET referenced by an ENI exist? Does the ACL referenced by a rule exist?
* **Idempotency:** is the requested value already the current value? (Returns 200 OK with `unchanged: true`, no DPU call.)
* **Conflict / CAS:** if the client provided `If-Match: <etag>`, compare to the current state hash; reject with 412 on mismatch.
* **Authorization:** RBAC scope check against the appliance and object kind.

If validation fails: respond immediately, **never proceed to staging**. No DPU traffic; cache untouched.

#### Stage 3 — Stage in Pending Copy

The mutation is materialized into `pending:<txn_id>` with the full post-image and metadata:

```
HSET pending:<txn_id>
     txn_id        <uuid>
     state         "DISPATCHING"        # one of: DISPATCHING|ACKED|COMMITTED|FAILED|ROLLED_BACK
     appliance     dpu-01
     object_key    state:dpu-01:eni:vnic-101
     pre_image     <json snapshot before change>
     post_image    <json snapshot after change>
     created_at    <unix-ms>
     deadline_at   <unix-ms + dispatch_timeout>
     submitted_by  <user>
```

A Redis Stream entry `XADD writes:pending` notifies the dispatch workers.

**Why stage?**

* **Crash safety:** if the daemon dies mid-dispatch, the Recovery Worker on restart finds all `state=DISPATCHING` entries and decides — based on a fresh DPU read — whether to commit, rollback, or retry.
* **Audit trail:** every mutation passes through a single, inspectable queue.
* **Concurrency control:** a per-object lock on `object_key` serializes conflicting writes (last-wins surprises avoided).

#### Stage 4 — Dispatch to DPU

A pool of **Dispatch Workers** pulls from `writes:pending` and issues the actual gRPC/gNMI Set against the target DPU's local agent.

* Synchronous call, bounded by `deadline_at` (default ~2 s, configurable per object kind).
* On success → DPU acknowledges with the committed object; worker advances `state=ACKED`.
* On error → worker classifies:
  * **Validation error from DPU** (hardware rejected): `state=FAILED`; pending row preserved for diagnostics; respond 4xx to operator with DPU's error code.
  * **Timeout / channel error**: `state=FAILED` after retry budget exhausted; respond 5xx; flag the appliance as degraded in gossip/health.

#### Stage 5 — Commit (or Rollback)

On `ACKED`:

```
MULTI
  HSET state:dpu-01:eni:vnic-101 ... (post_image)
  XADD state:events ... (notify compute-cache invalidator)
  DEL  pending:<txn_id>
  XADD writes:committed ... (audit)
EXEC
```

The atomic Redis transaction guarantees that either the canonical DB reflects the change *and* the pending entry is cleared *and* the event is emitted, or none of those happen (and the Recovery Worker will retry).

On `FAILED` after dispatch:

* If the DPU rejected before applying → no rollback needed; pending row archived (`writes:failed` stream).
* If the DPU partially applied or state is unknown → trigger an **out-of-band reconciliation read** of the affected object from the DPU, then overwrite the canonical entry from reality. Operator gets a 5xx with `reconciliation_triggered: true`.

### 5.3 Concurrency Model

```
                +-----------------------+
                |  Per-object lockset   |
                |  (Redis SET NX EX)    |
                +-----------+-----------+
                            |
       ┌────────────────────┼────────────────────┐
       │                                         │
  txn_A wants state:dpu-01:eni:vnic-101    txn_B wants state:dpu-01:eni:vnic-101
       │                                         │
       v                                         v
  Acquire lock → proceed                  Block / queue (with timeout)
       │
       v
  Stage 3 → Stage 4 → Stage 5
       │
       v
  Release lock → txn_B proceeds
```

* Locks are held *only* across Stages 3-5 of a single transaction, not across user think time.
* Lock keys are scoped to `object_key`; writes to disjoint objects on the same DPU run in parallel.
* Lock TTL = `deadline_at + grace`; a crashed worker auto-releases.

### 5.4 Recovery Worker

Runs at daemon start and periodically (e.g. every 30 s) thereafter.

* Scans `pending:*` for entries where `state in {DISPATCHING, ACKED}` and `now > deadline_at`.
* For each, issues a **live read** of the affected object from the DPU.
* Reconciles:
  * If DPU shows the post-image → finish the commit (Stage 5).
  * If DPU shows the pre-image → mark `state=FAILED`, archive, leave canonical DB untouched.
  * If DPU shows neither → log as `state=UNKNOWN_RECONCILED`, overwrite canonical with whatever the DPU reports, alert the operator.

---

## 6. End-to-End Example: `dashctl set eni vnic-101 --mac 02:00:00:aa:bb:cc`

```
 [User]                                                                            t=0    ms
   │  dashctl set eni vnic-101 --mac 02:00:00:aa:bb:cc
   ▼
 [API Front Door]                                                                  t=5    ms
   │  txn_id=018f...   appliance=dpu-01   object=eni:vnic-101
   ▼
 [Validator]                                                                       t=12   ms
   │  - exists? yes
   │  - mac valid? yes
   │  - same as current? no  → proceed
   │  - lock acquired on state:dpu-01:eni:vnic-101
   ▼
 [Pending Copy Writer]                                                             t=15   ms
   │  HSET pending:018f... state=DISPATCHING pre=... post=...
   │  XADD writes:pending
   ▼
 [Dispatch Worker]                                                                 t=18   ms
   │  gRPC Set(eni:vnic-101 mac=02:00:00:aa:bb:cc) → dpu-01
   ▼
 [DPU-01 Local Agent]                                                              t=45   ms
   │  Applies via DASH-SAI; ACK with new state hash
   ▼
 [Dispatch Worker]                                                                 t=70   ms
   │  HSET pending:018f... state=ACKED
   ▼
 [Commit (MULTI/EXEC)]                                                             t=72   ms
   │  HSET state:dpu-01:eni:vnic-101 mac=...
   │  XADD state:events key=state:dpu-01:eni:vnic-101 op=update
   │  DEL  pending:018f...
   │  XADD writes:committed
   ▼
 [Cache Invalidator]                                                               t=74   ms
   │  Find compute:* entries whose dep set contains
   │  state:dpu-01:eni:vnic-101  → DEL them
   ▼
 [Response to User]                                                                t=80   ms
   │  200 OK  { txn_id, applied_at, etag }
```

Total: ~80 ms steady-state, dominated by the single gRPC round trip to the DPU.

---

## 7. Failure Matrix

| Failure point | Symptom | Behaviour | Operator-visible result |
|---|---|---|---|
| Validation fails | Bad input | Stage 2 short-circuits; no DPU call | 4xx with reason |
| Pending write fails to stage | Redis unavailable | Front Door returns 503 | 503; operator retries |
| Dispatch times out | DPU unresponsive | Recovery Worker reconciles on next pass | 5xx with `reconciliation_pending: true` |
| DPU rejects | Hardware limit / schema error | `state=FAILED`; canonical DB untouched | 4xx with DPU error code |
| Commit fails (Redis dies between ACK and Commit) | Mutation applied on DPU but not in cache | Recovery Worker finds `state=ACKED`; finishes commit on restart | Eventually consistent; audit visible |
| Cache vs DPU drift detected during Phase-2 compute | Compute mismatch | Trust DPU, log drift, force full reconciler pass on appliance | Result still correct; metric `dashcenter_drift_total` increments |
| Daemon crash mid-write | All in-flight `pending:*` entries survive | Recovery Worker runs at startup | Eventually consistent within seconds |

---

## 8. Observability Hooks

Every stage emits structured logs and Prometheus metrics keyed by `(appliance, object_kind, stage, outcome)`:

* `dashcenter_read_latency_seconds{path="cache|live"}` histogram
* `dashcenter_compute_cache_hit_ratio` gauge
* `dashcenter_write_stage_duration_seconds{stage="validate|stage|dispatch|commit"}` histogram
* `dashcenter_pending_writes_inflight` gauge
* `dashcenter_drift_total{appliance,object_kind}` counter
* `dashcenter_recovery_actions_total{outcome}` counter

The `writes:committed` and `writes:failed` Redis streams act as a permanent **transactional audit log** consumable by `dashctl audit` and any external SIEM.

---

## 9. Phase Roadmap (recap)

| Phase | Read Pipeline | Compute Pipeline | Write Pipeline |
|---|---|---|---|
| **Phase 1** | Cache built by background daemon; raw reads served from Redis. `--live` bypass available. | **Direct live compute** against DPU for every request. No compute caching. | Full 5-stage write pipeline with pending copy. |
| **Phase 2** | Same as Phase 1 plus stream-based push invalidation everywhere. | **Cache compute + DPU validation**; memoize in `compute:<hash>` with dependency-scoped invalidation. | Same; add batched fleet-wide writes under parent `batch_id`. |
| **Phase 3** | Watch / streaming subscriptions (WebSocket / gRPC streaming) directly off `state:events`. | Speculative pre-compute for hot queries based on telemetry. | Multi-DPU coordinated transactions (saga pattern) for cross-fleet policy rollouts. |

---

## 10. Relationship to the other HLDs

* **Topology lives in:** [Centralized HLD](high_level_system_design.md) and [Controllerless HLD](high_level_system_design_controllerless.md).
* **Pipelines defined here are topology-agnostic.** In the centralized model they run inside `clidemon` on the controller host. In the controllerless model they run inside `dashd` on the elected Master, with Secondary/Backup nodes replicating the Canonical State DB and Pending Copy via the Raft log so that failover preserves in-flight transactions.
