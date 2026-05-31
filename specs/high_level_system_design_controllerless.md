# High-Level Design (HLD) Specification: DASHCenter Controllerless Architecture

This document defines an alternate, **controllerless** deployment model for **DASHCenter**. Unlike the [centralized controller HLD](high_level_system_design.md) — where a dedicated `clidemon` appliance owns aggregation, caching, and API serving — this model embeds the full DASHCenter management stack directly on every DPU in the fleet. The fleet self-organizes through a **gossip-based membership protocol** and **leader election (Raft-style consensus)** to elect a `Master`, one or more `Secondary` replicas, and `Backup` followers, eliminating any external single point of failure.

The operator may log in to **any node** (CLI, Web Console, or REST/gRPC client). The local node transparently **redirects writes and aggregated reads to the current Master**, while local cache hits can be served from the node's own warm replica when consistency requirements allow.

---

## 1. Design Goals

| Goal | Description |
|---|---|
| **No external controller** | The management plane lives on the DPUs themselves. Removing the dedicated x86 controller eliminates a deployment dependency and a hard failure domain. |
| **Symmetric nodes** | Every DPU runs the identical `dashd` binary. Role (Master / Secondary / Backup / Voter) is assigned dynamically at runtime, not by static configuration. |
| **Self-healing membership** | Node failures, network partitions, and rolling upgrades are absorbed by gossip + consensus without operator intervention. |
| **Location-independent UX** | Operators may target any reachable node; the system internally routes the request to the authoritative replica. |
| **Hot replicas** | Secondary and Backup nodes maintain a fully warmed cache (Redis + RedisTimeSeries + RediSearch indexes) so failover is sub-second and reads remain local where safe. |
| **Same API surface** | The CLI (`dashctl`), Web Console, and 3rd-party SDKs use the **identical** REST / gRPC contract as the centralized model — only the transport routing layer changes. |

---

## 2. Cluster Roles

A DASHCenter cluster of `N` DPUs (recommended `N >= 3`, ideally `2f+1` for `f` tolerated failures) self-organizes into the following roles:

| Role | Count | Responsibility |
|---|---|---|
| **Master (Leader)** | 1 | Owns all *writes* to the replicated state machine, serves *strongly-consistent reads*, runs *cross-fleet aggregation*, and is the single source of truth for membership commits. |
| **Secondary (Hot Standby)** | 1–2 | Synchronously replicated. Eligible to be elected Master on failure. Serves *bounded-staleness reads* locally. |
| **Backup (Warm Follower)** | 0..N-3 | Asynchronously replicated. Eligible voters in elections. Provides extra read fan-out and disaster recovery. |
| **Voter-Only / Witness** *(optional)* | 0–1 | Participates in quorum but does not hold a data replica. Used to break ties in even-numbered clusters. |

> **Note:** Every node — regardless of role — continues to run its **local probe agent** that ingests state from its *own* DASH-SAI pipeline. The role only governs *aggregation, persistence, and API serving* — not local hardware data extraction.

---

## 3. System Component Block Diagram

```
==================================================================================================
                                      USER INTERACTION LAYER
==================================================================================================
  +---------------------------+   +---------------------------+   +-----------------------------+
  |    dashctl CLI Client     |   |   DashCenter Web Console  |   |  3rd-Party / Automation     |
  | (Targets ANY fleet node)  |   |   (Browser SPA)           |   |  (SDKs, IaC, CI scripts)    |
  +-------------+-------------+   +-------------+-------------+   +--------------+--------------+
                |                               |                                |
                |   "dashctl --node dpu-07 get enis --all-devices"               |
                |   (May land on any node; node will internally redirect)        |
                +---------------+---------------+----------------+---------------+
                                                |
                                                v  (REST / gRPC — identical to centralized HLD)
==================================================================================================
                       SYMMETRIC FLEET  (every DPU runs the identical dashd binary)
==================================================================================================

   +-------------------------------+    +-------------------------------+    +-------------------------------+
   |        DPU-01  [MASTER]       |    |     DPU-02  [SECONDARY]       |    |      DPU-03  [BACKUP]         |
   |                               |    |                               |    |                               |
   |  +-------------------------+  |    |  +-------------------------+  |    |  +-------------------------+  |
   |  |   API Front Door        |  |    |  |   API Front Door        |  |    |  |   API Front Door        |  |
   |  |  (REST + gRPC listener) |  |    |  |  (REST + gRPC listener) |  |    |  |  (REST + gRPC listener) |  |
   |  |   - Resolves leader     |  |    |  |   - Resolves leader     |  |    |  |   - Resolves leader     |  |
   |  |   - Local read or       |  |    |  |   - Local read or       |  |    |  |   - Always redirect     |  |
   |  |     redirect to Master  |  |    |  |     redirect to Master  |  |    |  |     writes to Master    |  |
   |  +-----------+-------------+  |    |  +-----------+-------------+  |    |  +-----------+-------------+  |
   |              |                |    |              |                |    |              |                |
   |              v                |    |              v                |    |              v                |
   |  +-------------------------+  |    |  +-------------------------+  |    |  +-------------------------+  |
   |  |  State Aggregator Core  |  |    |  |  Read-Only Aggregator   |  |    |  |  Read-Only Aggregator   |  |
   |  |  (Active: owns writes)  |  |    |  |  (Hot standby)          |  |    |  |  (Warm follower)        |  |
   |  +-----------+-------------+  |    |  +-------------------------+  |    |  +-------------------------+  |
   |              |                |    |                               |    |                               |
   |              v                |    |  +-------------------------+  |    |  +-------------------------+  |
   |  +-------------------------+  |    |  | Replicated Redis Cache  |  |    |  | Replicated Redis Cache  |  |
   |  | Replicated Redis Cache  |<====SYNC===>| (Synchronous follower)  |<==ASYNC==>| (Lagging follower)      |  |
   |  | (Primary; authoritative)|  |    |  +-------------------------+  |    |  +-------------------------+  |
   |  +-----------+-------------+  |    |                               |    |                               |
   |              ^                |    |  +-------------------------+  |    |  +-------------------------+  |
   |              | local writes   |    |  |   Local Probe Agent     |  |    |  |   Local Probe Agent     |  |
   |  +-----------+-------------+  |    |  | (Ingests THIS DPU only) |  |    |  | (Ingests THIS DPU only) |  |
   |  |   Local Probe Agent     |  |    |  +-----------+-------------+  |    |  +-----------+-------------+  |
   |  | (Ingests THIS DPU only) |  |    |              |                |    |              |                |
   |  +-----------+-------------+  |    |              v                |    |              v                |
   |              |                |    |  +-------------------------+  |    |  +-------------------------+  |
   |              v                |    |  |  DASH-SAI / P4 Pipeline |  |    |  |  DASH-SAI / P4 Pipeline |  |
   |  +-------------------------+  |    |  +-------------------------+  |    |  +-------------------------+  |
   |  |  DASH-SAI / P4 Pipeline |  |    |                               |    |                               |
   |  +-------------------------+  |    |                               |    |                               |
   +---------------+---------------+    +---------------+---------------+    +---------------+---------------+
                   |                                    |                                    |
                   |                                    |                                    |
                   +=========================== CLUSTER FABRIC ============================+
                                                  (Two overlay protocols)
                                                          |
                          +-------------------------------+-------------------------------+
                          |                                                               |
                          v                                                               v
        +---------------------------------------+                       +---------------------------------------+
        |  GOSSIP MEMBERSHIP (SWIM-style UDP)   |                       |   RAFT CONSENSUS (gRPC, TCP)          |
        |  - Liveness / failure detection       |                       |   - Leader election                   |
        |  - Lightweight rumor propagation      |                       |   - Replicated log (config & ENIs)    |
        |  - Anti-entropy reconciliation        |                       |   - Strong consistency for writes     |
        +---------------------------------------+                       +---------------------------------------+
```

---

## 4. Cluster Fabric — Two Overlay Protocols

The fleet relies on two complementary protocols. Splitting concerns this way keeps gossip cheap and consensus correct.

### 4.1 Gossip Membership Layer (SWIM-style)

* **Transport:** UDP, lightweight, fan-out logarithmic in `N`.
* **Responsibilities:**
  * Liveness / suspicion / failure detection of peer DPUs.
  * Propagation of *soft* state: node role hints, current leader epoch, build version, load metrics.
  * Anti-entropy passes that reconcile metadata drift between peers without needing the leader.
* **Why gossip:** It scales horizontally, tolerates partitions gracefully, and provides fast failure detection (sub-second) without burdening the consensus log.

### 4.2 Raft Consensus Layer

* **Transport:** gRPC over TCP, mTLS authenticated.
* **Responsibilities:**
  * **Leader election** — Master is the Raft leader.
  * **Replicated log** for the authoritative configuration state machine (ENI registrations, VNET bindings, fleet inventory, ACL policies).
  * **Quorum commit** for any operator-initiated write before it is acknowledged.
* **Why Raft (not Paxos / gossip-only):** Operator writes require *linearizable* semantics. Raft gives a well-understood leader model that maps cleanly onto our Master / Secondary / Backup roles.

> **Separation of concerns:** Gossip handles *who is alive*; Raft handles *what is true*. A Master is the node that is **both** the current Raft leader **and** marked alive by the gossip layer.

---

## 5. Leader Election & Failover Flow

```
   Time ──►   t0: DPU-01 is MASTER, DPU-02 SECONDARY, DPU-03 BACKUP, DPU-04 BACKUP

   t1: DPU-01 hardware fault. Local probe agent and dashd both stop responding.

   t2: Gossip suspicion timer on peers fires.
       DPU-02, DPU-03, DPU-04 each mark DPU-01 as SUSPECT, then FAULTY.

   t3: Raft heartbeat from DPU-01 stops arriving at followers.
       DPU-02 (Secondary) was already most-up-to-date; its election timer fires first.
       DPU-02 transitions: FOLLOWER ──► CANDIDATE, increments term, requests votes.

   t4: DPU-03 and DPU-04 vote YES (they confirmed DPU-01 dead via gossip).
       DPU-02 wins quorum (3 of 4 votes including self).

   t5: DPU-02 transitions: CANDIDATE ──► LEADER, broadcasts AppendEntries.
       Gossip role hint updates: DPU-02 = MASTER.
       DPU-03 promoted: BACKUP ──► SECONDARY (sync replica).
       DPU-04 stays BACKUP.

   t6: Any in-flight client request that landed on DPU-03 or DPU-04 is now
       transparently redirected to DPU-02 (new Master) via the API Front Door.

   t7: When DPU-01 recovers, it rejoins gossip, catches up the Raft log
       from the new Master, and becomes a BACKUP (will not preempt the Master).
```

**Typical failover budget:** gossip detection (~1s) + Raft election (~150–300ms) + cache promotion (~negligible since Secondary is already hot) ≈ **under 2 seconds end-to-end**.

---

## 6. Request Routing — "Log in Anywhere"

Every node exposes the same REST + gRPC API. The **API Front Door** module on each node implements the routing rules:

```
 [User runs: dashctl --endpoint dpu-07 get enis --all-devices]
        │
        ▼
 [DPU-07 API Front Door]
        │
        ├─► Is this a READ that tolerates bounded staleness?
        │       └─► YES → serve from LOCAL Redis replica  ──► return to user
        │
        ├─► Is this a STRONGLY-CONSISTENT READ or a WRITE?
        │       └─► YES → look up current Master from gossip leader-hint table
        │                 │
        │                 ├─► If THIS node is Master → execute locally
        │                 │
        │                 └─► Else → forward gRPC call to Master
        │                            (transparent proxy; user URL unchanged)
        │
        └─► Is this a CROSS-FLEET AGGREGATION (e.g. `get enis --all-devices`)?
                └─► Always route to Master, which holds the unified replicated index.
```

* **Redirect semantics:** Implemented as a **server-side proxy hop** (not an HTTP 307). The user's connection terminates on DPU-07; DPU-07 dials the Master, streams the response back. From the operator's perspective the call is indistinguishable from a local call.
* **Leader hint cache:** Every node keeps the current `leader_id` and `raft_term` in memory (refreshed by gossip). A stale hint causes one extra hop, never a wrong answer — the contacted node will itself forward to the real leader.
* **Web Console:** Connects via the same API. The browser may keep a WebSocket to its login node for streaming; the login node forwards subscriptions to the Master and pipes the stream back.

---

## 7. Data Replication Model

| Layer | Mechanism | Consistency | Notes |
|---|---|---|---|
| **Configuration state machine** (ENIs, VNETs, ACLs, inventory) | **Raft replicated log** committed by quorum | Strong / linearizable | Authoritative source. Every write goes through the Master. |
| **Redis configuration cache** (hashes, RediSearch indexes) | Rebuilt on each node from the committed Raft log | Strong (after apply) | Identical schema as centralized HLD; key prefix `appliance:<id>:config:*`. |
| **Flow tables** (`appliance:<id>:flow:<5tuple>`) | Each node owns the cache of its **own** flows; cross-fleet aggregation pulls on demand from Master's index | Eventual | Volumes too high to fit in Raft log; flows have short TTL anyway. |
| **TimeSeries telemetry** (counters, drops) | Local on each node; rolled up to Master via gNMI subscription between peers | Eventual | The Master keeps the multi-DPU aggregate; per-DPU history stays local. |
| **Membership / role assignments** | Gossip soft state + Raft hard state | Gossip = eventual, Raft = strong | Two-tier so that benign metadata gossiping doesn't bloat the Raft log. |

> **Backup nodes** receive Raft log entries asynchronously (a few entries behind Master). They are *eligible voters* in elections but *not* counted in the synchronous write quorum, keeping write latency bounded even with many backups.

---

## 8. Failure Scenarios

| Failure | Detection | Recovery |
|---|---|---|
| **Master DPU crashes** | Gossip declares FAULTY (~1s) + Raft heartbeat timeout | Secondary wins election, promotes itself; backups rebalance roles. |
| **Network partition (minority side)** | Minority loses quorum | Minority side rejects writes, serves stale reads only (flagged in response header `X-Replica-Stale: true`). |
| **Network partition (majority side)** | Majority still has quorum | Continues normal operation; absent nodes marked SUSPECT. |
| **Secondary lags** | Raft `matchIndex` falls behind threshold | Demoted to Backup; next-best Backup promoted to Secondary. |
| **Split-brain attempt** | Two Masters would require two quorums on same term | Impossible by Raft invariant; only one leader per term can be elected. |
| **Rolling upgrade** | Operator drains one node at a time | Drain triggers voluntary step-down if leader; cluster auto-elects new leader within seconds. |
| **Total cluster restart** | All nodes start as Followers | First node to time out becomes Candidate; election proceeds normally once `quorum` nodes are up. |

---

## 9. Comparison: Centralized vs. Controllerless

| Dimension | Centralized HLD | Controllerless HLD |
|---|---|---|
| **Management binary** | `clidemon` on dedicated controller host | `dashd` on every DPU (symmetric) |
| **Single point of failure** | Controller host (mitigated by HA pair) | None — any `f` failures tolerated in `2f+1` cluster |
| **Footprint** | Extra x86 server(s) | Zero additional hardware |
| **CLI / Web entry point** | Fixed controller VIP | Any DPU in the fleet |
| **Write path** | Operator → controller → DPUs | Operator → any node → Master DPU → quorum |
| **Read path (default)** | Controller's Redis cache | Local DPU's Redis replica (bounded staleness) |
| **Consistency model for config** | Strong (single writer) | Strong via Raft quorum |
| **Failover time** | Manual or VRRP/keepalived (~seconds) | Automatic via Raft (~under 2s) |
| **Best fit** | Brownfield deployments with existing controller hardware; very large fleets (>50 DPUs) | Greenfield edge/branch deployments; small-to-mid fleets where additional infra is undesirable |
| **API surface seen by clients** | **Identical** — REST + gRPC contract is preserved across both models |

---

## 10. Deployment Footprint

```
Centralized:                            Controllerless:

   +-----------------+                     (no external box)
   |  Controller VM  |
   |   (clidemon)    |                     +-----+   +-----+   +-----+
   +--------+--------+                     | DPU |   | DPU |   | DPU |   ...
            | gRPC                         |dashd|<->|dashd|<->|dashd|
   +--------+--------+--------+            +-----+   +-----+   +-----+
   |        |        |        |               \________|________/
 [DPU]    [DPU]    [DPU]    [DPU]                    gossip
                                                     + Raft
```

This model is ideal for **edge sites, branch racks, and self-contained DPU pods** where shipping an additional controller appliance is operationally undesirable, while still preserving the full DASHCenter CLI, Web Console, and API experience defined in the [original HLD](high_level_system_design.md).

---

## 11. Open Items / Future Work

* **Cluster bootstrap** — first-boot ceremony to form the initial quorum (seed list vs. mDNS vs. operator-driven `dashctl cluster init`).
* **Cross-cluster federation** — multi-rack / multi-site DASHCenter clusters peering via read-only replication.
* **Resource budgeting** — formal limits on CPU / memory consumed by `dashd` on a DPU's management cores so it cannot compete with the dataplane.
* **Encrypted gossip** — mTLS or symmetric AEAD for the SWIM channel (Raft is mTLS-protected by default).
* **Operator UX for role visibility** — `dashctl cluster status` and a Web Console panel showing per-node Role / Term / Lag.
