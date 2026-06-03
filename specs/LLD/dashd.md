# LLD — dashd (DashCenter fleet controller daemon) — INITIAL DRAFT

> **Status: DRAFT.** This document captures the planned design of `dashd`,
> the Phase 4 fleet controller. The binary exists today as a scaffold
> ([`src/impl-go/dashd/`](../../src/impl-go/dashd/)) that prints a version
> banner. This LLD is intended to drive its implementation. It will be
> revised as code lands and ratified in PRs.

`dashd` is the **central controller** that sits **above** individual DPU
agents. Where `dash-sim-client` talks to a single DPU's `dashapi.v1.DashApi`,
`dashd` orchestrates **many** DPUs by reconciling a declared **desired
state** against each DPU's observed state.

This document is in DRAFT form: section structure is complete, key
decisions are recorded, but algorithms and proto definitions are
intentionally specified at a level a contributor can pick up and turn into
code.

---

## Table of contents

1. [Purpose and positioning](#1-purpose-and-positioning)
2. [Non-goals](#2-non-goals)
3. [Architecture overview](#3-architecture-overview)
4. [Layered model: desired state → observed state](#4-layered-model)
5. [`dashcenter.v1` — proposed proto surface](#5-dashcenterv1-proposed-proto-surface)
6. [DPU inventory and connectivity](#6-dpu-inventory-and-connectivity)
7. [State store (controller-internal)](#7-state-store)
8. [Reconciliation loop](#8-reconciliation-loop)
9. [Per-DPU worker model](#9-per-dpu-worker-model)
10. [Subscribe pump (Observed state ingestion)](#10-subscribe-pump)
11. [HA and leader election](#11-ha-and-leader-election)
12. [Cross-DPU object types (VPC, VPC peering, HA set rollout)](#12-cross-dpu-object-types)
13. [REST front-end](#13-rest-front-end)
14. [Admin HTTP](#14-admin-http)
15. [Configuration model](#15-configuration-model)
16. [Concurrency and back-pressure](#16-concurrency-and-back-pressure)
17. [Failure semantics](#17-failure-semantics)
18. [Observability](#18-observability)
19. [Security (authn/authz, TLS, audit)](#19-security)
20. [Persistence](#20-persistence)
21. [Bootstrap and shutdown](#21-bootstrap-and-shutdown)
22. [Rust pseudocode parity (sketch)](#22-rust-pseudocode-parity-sketch)
23. [Open questions](#23-open-questions)
24. [Phased implementation milestones](#24-phased-implementation-milestones)

---

## 1. Purpose and positioning

```
                        ┌──────────────────────────┐
                        │   operator / pipeline    │
                        │  (Terraform, GitOps,     │
                        │   UI, dashctl, scripts)  │
                        └──────────────┬───────────┘
                                       │ dashcenter.v1
                                       │ (REST + gRPC)
                                       ▼
                        ┌──────────────────────────┐
                        │           dashd          │  ← Phase 4, this LLD
                        │  - desired-state store   │
                        │  - reconciliation loop   │
                        │  - per-DPU workers       │
                        │  - HA / leader election  │
                        └──────────────┬───────────┘
                                       │ dashapi.v1 (per DPU)
            ┌──────────────────────────┼──────────────────────────┐
            │                          │                          │
       ┌────▼────┐                ┌────▼────┐                ┌────▼────┐
       │ dash-sim│                │ dash-   │                │ real    │
       │  (dev / │                │ redis-  │                │ DPU agent│
       │   CI)   │                │ adapter │                │ (hw)    │
       └─────────┘                └─────────┘                └─────────┘
```

`dashd`'s job is **reconciliation**: take what the operator declared
(desired state in `dashcenter.v1` terms) and make each DPU's observed
state match (by issuing `dashapi.v1.Apply` / `Delete` per DPU).

It is **not** itself a DPU agent — it has no `dashapi.v1.DashApi` surface.
Operators use [`dashctl`](dashctl.md) (the Phase 4 CLI) or any HTTP client
to drive `dashd`.

---

## 2. Non-goals

- **No data plane.** `dashd` never sees or processes a packet.
- **No SAI calls.** It only speaks `dashapi.v1` to DPU agents.
- **No general-purpose Kubernetes-style scheduler.** Object placement
  rules are explicit and DASH-specific (an ENI lives on a specific DPU;
  a `vnet_mapping` is replicated to all DPUs that host ENIs of that VNET).
- **No HLD authoring tool.** Higher-level abstractions (e.g. "VPC" with
  CIDR plan) are exposed as proto types in `dashcenter.v1` but their
  lifecycle is owned by the upstream operator (Terraform, Pulumi, hand
  YAML, UI). `dashd` only consumes them.

---

## 3. Architecture overview

```
┌────────────────────────────────────────────────────────────────────┐
│                          dashd process                             │
│                                                                    │
│ HTTP :8443 ─► ┌─────────────────────┐                              │
│               │   rest/             │                              │
│               │   (operator API)    │                              │
│               └─────────┬───────────┘                              │
│                         │                                          │
│ gRPC :9443 ─► ┌─────────▼───────────┐    ┌─────────────────────┐   │
│               │   server/           │    │   reconciler/       │   │
│               │ (dashcenter.v1.     │    │ (worker pool)       │   │
│               │  ControlPlane impl) │    │                     │   │
│               └─────────┬───────────┘    └─────┬───────────────┘   │
│                         │ write desired state  │ tick (per DPU)    │
│                         ▼                      │                   │
│               ┌─────────────────────┐          │                   │
│               │   model/            │◄─────────┘                   │
│               │  (desired + observed│                              │
│               │   keyed by DPU)     │                              │
│               └─────────┬───────────┘                              │
│                         │ resolve placement                        │
│                         ▼                                          │
│               ┌─────────────────────┐                              │
│               │   dispatch/         │ ── 1 worker / DPU            │
│               │   per-DPU clients   │                              │
│               └─────────┬───────────┘                              │
│                         │ dashapi.v1.Apply / Delete                │
│ ─────────────────────────│──────────────────────────────────────►  │
│                         │ dashapi.v1.Subscribe                     │
│ ◄────────────────────────│──────────────────────────────────────── │
│                         │                                          │
│               ┌─────────▼───────────┐                              │
│               │   ha/               │ leader election (etcd/Raft)  │
│               └─────────────────────┘                              │
│                                                                    │
│               ┌─────────────────────┐                              │
│               │  store/             │ desired state persistence    │
│               │ (etcd | postgres |  │                              │
│               │  redis | file)      │                              │
│               └─────────────────────┘                              │
└────────────────────────────────────────────────────────────────────┘
```

---

## 4. Layered model — desired state → observed state

Three coordinate spaces, related by the reconciler:

| Space | Owner | Representation | Storage |
|---|---|---|---|
| **Declared / desired** | operator | `dashcenter.v1` "policy" types: `Appliance`, `ENI`, `Vnet`, `VnetPeering`, `AclPolicy`, `RoutePolicy`, `HaSet`, ... | persistent store in `dashd` (etcd / postgres / file) |
| **Resolved / placed** | `dashd` | `dashapi.v1.Object`s grouped by DPU id; produced by the **placement function** | derived; cached in memory |
| **Observed** | DPU agents | `dashapi.v1.Object`s as actually present in each DPU's store | per-DPU; subscribed via `dashapi.v1.Subscribe` |

The reconciler's invariant:

> `Placed(desired, inventory)` for every DPU equals the union of that DPU's
> `Observed` state (modulo monitoring of drift).

When operator changes desired state OR observed state diverges, the
reconciler emits per-DPU `Apply` / `Delete` calls to converge them.

---

## 5. `dashcenter.v1` — proposed proto surface

This proto **does not exist today**. The shape below is the recommended
v1 starting point.

```protobuf
syntax = "proto3";
package dashcenter.v1;

import "dashapi/v1/dashapi.proto";   // re-uses the same upstream payload types

// Identity of a DPU as known to the controller.
message DpuIdentity {
  string id        = 1;     // operator-assigned, e.g. "dpu-rack1-r5"
  string endpoint  = 2;     // dashapi.v1 gRPC target, e.g. "10.0.5.7:50051"
  string region    = 3;
  string site      = 4;
  map<string,string> labels = 5;
}

// Inventory: declared DPU fleet.
message Inventory { repeated DpuIdentity dpus = 1; }

// Logical policy types. These are higher-level than dashapi.v1; the
// placement function turns each into one or more dashapi.v1.Objects on the
// appropriate DPU(s).

message VnetSpec {
  string name = 1;
  uint32 vni  = 2;
  repeated string peers = 3;              // peer VNET names
  map<string,string> labels = 4;
}

message EniSpec {
  string id            = 1;
  string mac           = 2;               // "aa:bb:..." human form
  string vnet          = 3;
  string dpu_id        = 4;               // placement hint
  repeated string addresses = 5;
  string admin_state   = 6;               // ENABLED|DISABLED
  string acl_in_group_v4   = 7;
  string acl_out_group_v4  = 8;
  // (future) optional per-stage bindings, qos_id, meter policy ids, ...
}

message VnetMappingSpec {
  string vnet         = 1;
  string overlay_ip   = 2;
  string underlay_ip  = 3;
  string mac          = 4;
  uint32 vni          = 5;
  string routing_type = 6;                // VNET | PRIVATELINK | ...
}

message AclPolicySpec {
  string group_id  = 1;
  string ip_version = 2;                  // IPV4 | IPV6
  repeated AclRuleSpec rules = 3;
}
message AclRuleSpec {
  uint32 num      = 1;
  uint32 priority = 2;
  string action   = 3;                    // PERMIT | DENY
  bool terminating = 4;
  repeated uint32 protocol = 5;
  repeated string src_prefixes = 6;
  repeated string dst_prefixes = 7;
  repeated string src_ports = 8;          // "80" or "1000-2000"
  repeated string dst_ports = 9;
  repeated string src_tags = 10;
  repeated string dst_tags = 11;
}

message RoutePolicySpec {
  string group_id = 1;
  repeated RouteSpec routes = 2;
}
message RouteSpec {
  string prefix = 1;
  string routing_type = 2;
  string vnet = 3;
  string next_hop_ip = 4;
}

// Service shape — control plane RPCs.
service ControlPlane {
  // Fleet inventory
  rpc PutInventory (Inventory)         returns (Ack);
  rpc GetInventory (google.protobuf.Empty) returns (Inventory);

  // Policy CRUD — keyed by spec.name / id
  rpc PutVnet         (VnetSpec)         returns (Ack);
  rpc PutEni          (EniSpec)          returns (Ack);
  rpc PutVnetMapping  (VnetMappingSpec)  returns (Ack);
  rpc PutAclPolicy    (AclPolicySpec)    returns (Ack);
  rpc PutRoutePolicy  (RoutePolicySpec)  returns (Ack);

  rpc DeleteByName    (NameRef) returns (Ack);
  rpc Get             (NameRef) returns (PolicyObject);
  rpc List            (KindFilter) returns (stream PolicyObject);

  // Observability
  rpc DpuStatus       (DpuRef) returns (stream DpuStatusReport);
  rpc Reconcile       (ReconcileRequest) returns (Ack);     // trigger manual sync
}

message Ack { string txn_id = 1; bool accepted = 2; string error = 3; }
message NameRef { string kind = 1; string name = 2; }
message KindFilter { string kind = 1; }
message DpuRef { string id = 1; }
message ReconcileRequest { repeated string dpu_ids = 1; }   // empty = all

message PolicyObject {
  string kind = 1;
  string name = 2;
  oneof spec {
    VnetSpec         vnet          = 100;
    EniSpec          eni           = 101;
    VnetMappingSpec  vnet_mapping  = 102;
    AclPolicySpec    acl_policy    = 103;
    RoutePolicySpec  route_policy  = 104;
    // ... add as we lift more dashapi types into policy form
  }
}

message DpuStatusReport {
  string  dpu_id    = 1;
  enum Health { HEALTH_UNSPECIFIED=0; HEALTHY=1; DEGRADED=2; OFFLINE=3; }
  Health  health    = 2;
  int64   last_seen_ts_ns = 3;
  int32   desired_objects = 4;
  int32   observed_objects= 5;
  int32   drift_objects   = 6;
  repeated string recent_errors = 7;
}
```

> Note: `dashcenter.v1` is **distinct from** `dashapi.v1`. The latter is
> the per-DPU object surface (vendored sonic-dash-api types). The former
> is the operator-facing fleet surface. Many `dashcenter.v1` specs trans-
> compile into multiple `dashapi.v1.Object`s on multiple DPUs.

---

## 6. DPU inventory and connectivity

- Inventory is supplied via `ControlPlane.PutInventory` or via a config
  file `inventory.yaml`.
- For each `DpuIdentity`, `dashd` lazily creates a `dashapi.DashApiClient`
  using `dash-sim-client/pkg/client` semantics (dial, hold a single conn,
  reuse). The dispatch package owns these clients.
- A periodic **liveness probe** (`Get(vnet, "")` or a no-op `List(vnet,
  limit=0)`) maintains health state for each DPU.

---

## 7. State store

Pluggable through an interface; v1 ships with two implementations:

| Backend | Use case | Persistence | HA |
|---|---|---|---|
| `file` | dev / single-node | JSONL files under `--state-dir` | none |
| `etcd` | production | etcd cluster | yes (also used for leader election) |

Future: `postgres`, `redis-cluster`.

```go
type DesiredStore interface {
    Put(ctx, kind string, name string, spec proto.Message) (txn string, err error)
    Delete(ctx, kind string, name string) (txn string, err error)
    Get(ctx, kind string, name string) (proto.Message, error)
    List(ctx, kind string) ([]NameAndSpec, error)
    Watch(ctx) (<-chan DesiredEvent, error)
}
```

The reconciler is woken up by either:
- a write to `DesiredStore` (Watch channel),
- an observed-state event from a DPU's `Subscribe` stream, or
- a periodic full-sweep tick (default 30s) as a safety net.

---

## 8. Reconciliation loop

Pseudocode:

```
loop:
    select:
        case desEv := <-desiredWatch:
            mark dirty: every DPU whose placement includes desEv.name
        case obsEv := <-observedWatch:
            mark dirty: obsEv.dpu_id
        case <-tick(30s):
            mark dirty: all DPUs

    for dpu in drainDirty():
        worker[dpu].schedule()
```

Per-DPU worker:

```
desired := placement.Resolve(dpu, allDesiredSpecs)     // map[(kind,key)] -> Object
observed := obsCache[dpu]                              // map[(kind,key)] -> Object

add    := desired \ observed       // want, don't have
remove := observed \ desired       // have, don't want
update := { x : desired[x] != observed[x] }

batch := orderForDependencies(add, update, remove)
for op in batch:
    ack := dpuClient.Apply | Delete(op)
    record(op, ack)
    if ack.accepted is false:
        backoff, retry, surface to /status
```

### Dependency ordering rules

Apply order (creates / updates):

```
1. appliance, vnet, route_type, prefix_tag, tunnel, qos, meter_policy,
   route_group, acl_group
2. meter_rule, route, acl_rule, routing_appliance, pa_validation
3. eni, eni_route
4. vnet_mapping, acl_in, acl_out, route_rule
5. ha_set, ha_set_config, ha_scope, ha_scope_config, outbound_port_map,
   outbound_port_map_range
```

Delete order: reverse.

These mirror the foreign-key shape of upstream DASH protos.

---

## 9. Per-DPU worker model

- One goroutine per DPU (Rust: one tokio task per DPU).
- Each worker owns a `dashapi.DashApiClient` for its DPU.
- A bounded **inbox channel** receives wake-up signals; collisions
  coalesce (worker just runs another reconcile pass).
- Workers are restarted on permanent failure (DPU dropped from inventory,
  endpoint changed, connection torpedoed).

```go
type DpuWorker struct {
    id     string
    client dashapi.DashApiClient
    inbox  chan struct{}     // capacity 1 — coalesces
    halt   context.CancelFunc
}
```

---

## 10. Subscribe pump (Observed state ingestion)

Each DPU worker maintains a long-lived `dashapi.Subscribe` stream with
`snapshot_first=true`:

```
go func dpuSubscribePump(dpu):
    for {
        ev, err := stream.Recv()
        if err != nil:
            backoff, reconnect with snapshot_first=true
            continue
        obsCache[dpu][key(ev)] = ev.Object
        notify reconciler "dpu dirty"
    }
```

On reconnect the snapshot rebuilds `obsCache[dpu]` from scratch — so
transient disconnects can never leave stale observed state.

---

## 11. HA and leader election

- In single-node deployments (`--ha=none`), the process is the leader.
- In multi-node deployments (`--ha=etcd`), an etcd lease decides
  leadership. Followers serve **read-only** `Get/List/DpuStatus` traffic
  from the same etcd store; only the leader runs reconciliation and
  Subscribe pumps.
- Failover is intentionally **gradual**: a follower wins the lease,
  spins up DPU workers (re-running snapshot resync), then takes over.
  Operator clients see brief 503s on writes during the transition.

---

## 12. Cross-DPU object types

Some `dashcenter.v1` specs **fan out** across DPUs. Placement rules:

| Spec | Placement |
|---|---|
| `VnetSpec` | every DPU that hosts an ENI in that VNET |
| `EniSpec` | the single DPU named by `dpu_id` |
| `VnetMappingSpec` | every DPU that hosts an ENI in the same VNET |
| `AclPolicySpec` | every DPU that hosts an ENI referencing that ACL group |
| `RoutePolicySpec` | every DPU that hosts an ENI referencing that route group |
| `HaSetConfigSpec` | every DPU that participates in the HA set |

The placement function is pure: `(allSpecs, inventory) → map[dpu] →
[]Object`. Side-effect-free and unit-testable.

---

## 13. REST front-end

The operator REST API maps 1:1 to `ControlPlane` RPCs:

| HTTP | Path | RPC |
|---|---|---|
| PUT | `/v1/inventory` | `PutInventory` |
| GET | `/v1/inventory` | `GetInventory` |
| PUT | `/v1/vnets/{name}` | `PutVnet` |
| PUT | `/v1/enis/{id}` | `PutEni` |
| PUT | `/v1/vnet-mappings/{vnet}/{ip}` | `PutVnetMapping` |
| PUT | `/v1/acl-policies/{name}` | `PutAclPolicy` |
| PUT | `/v1/route-policies/{name}` | `PutRoutePolicy` |
| DELETE | `/v1/{kind}/{name}` | `DeleteByName` |
| GET | `/v1/{kind}/{name}` | `Get` |
| GET | `/v1/{kind}` | `List` |
| GET | `/v1/dpus/{id}/status` | `DpuStatus` (SSE stream) |
| POST | `/v1/reconcile` | `Reconcile` |

JSON body shapes follow protojson conventions (same as
[`dash-sim-client`](dash-sim-client.md)). gRPC clients hit `:9443`; REST
clients hit `:8443`.

---

## 14. Admin HTTP

Like `dash-sim`, `dashd` exposes a small admin surface:

| Method | Path | Purpose |
|---|---|---|
| GET | `/admin/health` | overall + per-DPU health |
| GET | `/admin/leader` | whether this node is the leader |
| GET | `/admin/inventory` | resolved inventory |
| GET | `/admin/desired?kind=` | dump desired state |
| GET | `/admin/observed?dpu=` | dump observed state for a DPU |
| GET | `/admin/drift?dpu=` | computed add / update / remove queue |
| POST | `/admin/reconcile` | force a sweep |

---

## 15. Configuration model

A single YAML file `dashd.yaml`:

```yaml
listen:
  rest_addr: ":8443"
  grpc_addr: ":9443"
  admin_addr: ":7443"
storage:
  backend: file       # file | etcd
  file:
    state_dir: /var/lib/dashd
  etcd:
    endpoints: ["etcd-0:2379", "etcd-1:2379"]
ha:
  mode: none          # none | etcd
  lease_ttl: 15s
inventory:
  source: file        # file | api
  file: /etc/dashd/inventory.yaml
reconcile:
  tick: 30s
  per_dpu_inbox: 1
log:
  level: info
  format: json
```

Flags override any field.

---

## 16. Concurrency and back-pressure

- **One reconciler goroutine** picks dirty DPUs and dispatches to workers.
- **One worker goroutine per DPU.** Inbox is capacity-1; coalesces wakes.
- **One Subscribe pump goroutine per DPU.** Independent lifecycle from
  worker — even when reconciliation is paused (e.g. during failover),
  observed state keeps tracking.
- **Apply rate limit per DPU.** Default 100 ops/s with token bucket; tunable.
- **Global error budget.** If a DPU returns >N errors/min, place it in
  quarantine and stop applying writes until manually cleared.

---

## 17. Failure semantics

| Failure | Behaviour |
|---|---|
| DPU unreachable | mark `OFFLINE`; Subscribe pump backs off (1, 2, 5, 10, 30s); reconciler skips |
| Apply rejected (`ack.accepted=false`) | retry with exponential backoff; surface in `DpuStatusReport.recent_errors`; never silently drop |
| Leader lease lost | stop workers cleanly; close clients; return non-error from `/admin/leader` |
| `dashd` crash | systemd / orchestrator restarts; state survives via persistent store; new leader resumes |
| Storage backend down | reads from in-memory cache; writes are rejected with `Unavailable` |

---

## 18. Observability

- Structured logs via `log/slog` (JSON to stdout).
- Prometheus metrics endpoint at `/metrics`:
  - `dashd_reconcile_runs_total{dpu, result}`
  - `dashd_apply_total{dpu, kind, accepted}`
  - `dashd_subscribe_disconnects_total{dpu}`
  - `dashd_desired_objects{kind}`
  - `dashd_observed_objects{dpu, kind}`
  - `dashd_drift_objects{dpu, kind, op}`
  - `dashd_dpu_health{dpu, state}`
- OpenTelemetry tracing for each `Apply` / `Delete` (parent → child span
  per DPU op).
- Audit log: append-only file (one JSON line per write).

---

## 19. Security

- TLS for REST and gRPC (`--tls-cert`, `--tls-key`, `--tls-client-ca`).
- mTLS for inbound gRPC.
- For outbound to DPUs: per-DPU TLS config (CA pool from inventory).
- RBAC: `viewer` (read-only RPCs), `operator` (Put/Delete), `admin`
  (all). Initial implementation: token-in-header, mapped to role via
  config; production: OIDC/AAD.

---

## 20. Persistence

| Data | Where |
|---|---|
| Desired specs | `DesiredStore` backend (file/etcd) |
| Inventory | same |
| Reconciler queue / dirty bits | in-memory; reconstructed on restart by full sweep |
| Observed cache | in-memory; reconstructed via Subscribe snapshot |
| Counters / metrics | Prometheus (external) |

---

## 21. Bootstrap and shutdown

```
main():
  cfg := loadConfig()
  store := newDesiredStore(cfg.storage)
  inv   := loadInventory(cfg.inventory)
  ha    := newLeaderElection(cfg.ha)
  rest  := newRestServer(cfg.listen.rest_addr, store)
  grpc  := newGrpcServer(cfg.listen.grpc_addr, store)
  admin := newAdminServer(cfg.listen.admin_addr, store, dispatch)

  go rest.Serve()
  go grpc.Serve()
  go admin.Serve()

  for {
    ha.AwaitLeadership()                          // blocks until leader
    dispatch := newDispatcher(inv, store)
    reconciler := newReconciler(store, dispatch)
    go reconciler.Run()
    <-ha.LostLeadership() OR <-ctx.Done()
    reconciler.Stop()
    dispatch.Stop()
  }
```

`SIGTERM`: gracefully stop reconciler, drain in-flight Applies, close
DPU clients, then close REST/gRPC/admin.

---

## 22. Rust pseudocode parity (sketch)

```rust
struct Dashd { store: Arc<dyn DesiredStore>, dispatch: Dispatcher, ha: Leader }

impl Dashd {
    async fn run(&self) -> Result<()> {
        let _lease = self.ha.acquire().await?;
        let mut reconciler = Reconciler::new(self.store.clone(), self.dispatch.clone());
        reconciler.run().await
    }
}

struct DpuWorker { id: String, client: DashApiClient<Channel>, inbox: Receiver<()> }
impl DpuWorker {
    async fn run(mut self, store: Arc<dyn DesiredStore>, obs: Arc<ObsCache>) {
        while self.inbox.recv().await.is_some() {
            let desired = placement::resolve(&self.id, &store).await;
            let observed = obs.for_dpu(&self.id);
            let (add, upd, rem) = diff(&desired, &observed);
            for op in order(add, upd, rem) {
                let _ = match op {
                    Op::Apply(o)  => self.client.apply(ApplyRequest{ object: Some(o) }).await,
                    Op::Delete(d) => self.client.delete(d).await,
                };
            }
        }
    }
}
```

---

## 23. Open questions

1. **`dashcenter.v1` shape**: should we use **YAML CRDs** as the
   primary API (Kubernetes-style) and treat REST/gRPC as a thin
   transport, or keep gRPC as the primary contract? Recommendation:
   gRPC primary, YAML loaders as sugar.
2. **Multi-tenant?** v1: single tenant per `dashd`. Multi-tenant later.
3. **Cross-DPU transaction guarantees**: do we need atomic "rollout
   completed on N/M DPUs"? Probably yes for VPC peering; design TBD.
4. **HA backend default**: etcd vs Raft-embedded. Etcd is simpler ops-wise.
5. **Schema evolution**: how do we handle a DPU running an older
   `dashapi.v1` minor than `dashd`? Recommendation: dashd negotiates via
   reflection; rejects writes that use kinds the DPU does not list.
6. **Drift policy**: if a human manually `Apply`s on a DPU, do we
   re-converge automatically? v1: yes (single source of truth is dashd).
   Future: pinning / drift-allowed labels.

---

## 24. Phased implementation milestones

The end-state is large; ship it as four landings.

### M1 — single-node skeleton (1 week)

- Flags + config loader
- `file` DesiredStore
- `inventory.yaml` loader
- `ControlPlane.PutInventory` / `Get` / `List`
- `ControlPlane.PutVnet` (one kind)
- No reconciliation: stores writes to disk

**Exit criterion**: `dashd` accepts `PutInventory` + `PutVnet` and survives restart.

### M2 — reconciliation (2 weeks)

- Dispatcher + per-DPU worker
- Subscribe pump
- Placement function for `Vnet`, `Eni`, `VnetMapping`
- File-based desired store with watch
- `/admin/reconcile`, `/admin/drift`, `/admin/observed`

**Exit criterion**: declared inventory + VNETs + ENIs end up in a live
`dash-sim` (or `dash-redis-adapter`) without manual help, and edits
re-converge.

### M3 — HA (1 week)

- etcd-backed `DesiredStore`
- Leader election
- Read-only REST on followers
- Graceful leader handover smoke test

**Exit criterion**: three-node `dashd` cluster keeps reconciling through a
leader kill.

### M4 — TLS + auth + observability + dashctl (parallelisable)

- TLS / mTLS
- Token auth + roles
- Prometheus metrics
- `dashctl` (see [LLD/dashctl.md](dashctl.md)) catching up to the new RPCs
- Production-ready Dockerfile + Helm chart

**Exit criterion**: a real operator can run `dashd` in production behind
a load balancer with three replicas, audit log, metrics, and a usable
`dashctl`.

---

> When implementation begins, please update §§ 5, 8, 13, and 22 with the
> exact decisions made, then promote this LLD from DRAFT to STABLE.
