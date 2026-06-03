# LLD — dash-sim (behavioural DASH-DPU simulator)

> Older notes refer to this binary as "dash-shim". The current name is
> **dash-sim** — they are the same component.

This document is the **definitive low-level design** of `dash-sim`. It
covers data structures, every RPC, the behavioural packet pipeline, the
admin HTTP control plane, fault injection, scenario loading, and concurrency
guarantees. Every upstream sonic-net/sonic-dash-api object kind is documented
along with how `dash-sim` uses it.

---

## Table of contents

1. [Scope and non-goals](#1-scope-and-non-goals)
2. [Architecture](#2-architecture)
3. [Internal package layout](#3-internal-package-layout)
4. [Kinds registry](#4-kinds-registry)
5. [Model store](#5-model-store)
6. [Event bus](#6-event-bus)
7. [Fault injector](#7-fault-injector)
8. [Counters registry](#8-counters-registry)
9. [Pipeline — behavioural model](#9-pipeline-behavioural-model)
10. [Scenario loader](#10-scenario-loader)
11. [Admin HTTP control plane](#11-admin-http-control-plane)
12. [gRPC service — every RPC](#12-grpc-service-every-rpc)
13. [Per upstream proto — usage & stitching](#13-per-upstream-proto-usage-and-stitching)
14. [Concurrency model](#14-concurrency-model)
15. [Rust pseudocode parity](#15-rust-pseudocode-parity)
16. [Failure modes and error taxonomy](#16-failure-modes-and-error-taxonomy)
17. [Extension recipes](#17-extension-recipes)
18. [Test surface](#18-test-surface)

---

## 1. Scope and non-goals

### In scope

- Implement every RPC of `dashapi.v1.DashApi` with **in-memory** storage.
- Implement the canonical DASH packet pipeline (direction lookup → ACL
  stages 1..5 → route LPM → vnet_mapping / service-tunnel / appliance encap)
  exposed via `SimulatePacket`.
- Provide a YAML scenario loader so deterministic test fixtures can be
  re-applied at process start or via the admin API.
- Provide a fault injection mechanism keyed by RPC name.
- Provide synthetic per-key counters that look like real DPU counters.
- Provide an admin HTTP control plane for human/operator inspection.

### Explicitly out of scope (delegated)

- Real packet forwarding (no data plane).
- SONiC APP_DB persistence — that belongs to
  [dash-redis-adapter.md](dash-redis-adapter.md).
- HA leader election (HA objects are stored faithfully but the simulator
  does not run any HA state machine; see roadmap).
- Authentication and TLS termination (planned; see roadmap).
- Cross-DPU orchestration — that belongs to a future `dashd` controller.

---

## 2. Architecture

```
            ┌────────────────────────────────────────────────────┐
            │                  dash-sim process                  │
            │                                                    │
            │  ┌──────────────┐         ┌─────────────────────┐  │
gRPC ───────┼─►│   server     │────────►│       model        │  │
50051       │  │ (DashApi    )│         │ (kind+key store)   │  │
            │  │              │         └────┬────────┬───────┘  │
            │  │              │              │        │          │
            │  │              │     write    │        │ read     │
            │  │              │              ▼        │          │
            │  │              │         ┌─────────────┴──────┐   │
            │  │              │         │       events       │   │
            │  │              │         │ (non-blocking bus) │   │
            │  │              │         └─────────────┬──────┘   │
            │  │              │                       │ subscribe│
            │  │              │                       ▼          │
            │  │              │◄─────────── stream events ───────┤
            │  │  Apply       │                                  │
            │  │  Delete      │   ┌────────────────────────┐     │
            │  │  Get/List    │──►│        faults          │     │
            │  │  Subscribe   │   └────────────────────────┘     │
            │  │  Counters    │                                  │
            │  │  SimulatePkt │──►┌────────────────────────┐     │
            │  └──────────────┘   │       pipeline         │     │
            │                     └──────────┬─────────────┘     │
            │                                │ tick               │
            │                                ▼                    │
            │                       ┌────────────────────┐        │
            │                       │     counters       │        │
            │                       └─────────┬──────────┘        │
            │                                 │                    │
HTTP  ──────┼───────────►┌────────────────┐   │                    │
8080        │            │     admin      │◄──┘                    │
            │            └──────┬─────────┘                        │
            │                   │ load                              │
            │                   ▼                                   │
            │            ┌────────────────┐                         │
            │            │   scenarios    │                         │
            │            └────────────────┘                         │
            └────────────────────────────────────────────────────────┘
```

All packages are `internal/sim/*`; nothing outside `dash-sim` may import
them. The **only stable API** is the gRPC service contract.

---

## 3. Internal package layout

| Package | Responsibility | Key types |
|---|---|---|
| `internal/sim/events` | non-blocking fan-out bus for `Subscribe` | `Bus`, `Subscription` |
| `internal/sim/faults` | RPC-keyed fault injection | `Injector`, `Spec`, `Mode` |
| `internal/sim/counters` | per-key synthetic counters | `Registry` |
| `internal/sim/model` | generic kind+key proto store | `Store` |
| `internal/sim/pipeline` | behavioural DASH packet evaluation | `Engine`, internal helpers |
| `internal/sim/scenarios` | YAML scenario loader | `Document`, `LoadFile` |
| `internal/sim/admin` | HTTP control plane | `Handler` |
| `internal/sim/server` | gRPC service implementation | `Server` (implements `dashapi.DashApiServer`) |

Public-but-shared (lives outside `dash-sim` so the adapter can reuse it):

| Package | Responsibility |
|---|---|
| `dashapi-runtime/kinds` | single source of truth for all 29 DASH object kinds — packs/unpacks oneof payloads, knows key parts, knows table names |

---

## 4. Kinds registry

Lives in [`dashapi-runtime/kinds`](../../src/impl-go/dashapi-runtime/kinds/kinds.go).
Every per-kind switch in the codebase reads from this registry, so adding
a new kind is a one-place change.

```go
type Info struct {
    Kind     dashapi.ObjectKind
    Name     string   // lower_snake_case, matches CLI: "vnet_mapping"
    KeyParts []string // upstream <Kind>Key fields, in declaration order
    NewZero  func() proto.Message
    Pack     func(*dashapi.Object, proto.Message)
    Unpack   func(*dashapi.Object) (proto.Message, bool)
}

func (i Info) TableName() string { return "DASH_" + strings.ToUpper(i.Name) + "_TABLE" }

var All []Info                                  // ordered by enum value
func Lookup(dashapi.ObjectKind) (Info, error)
func LookupByName(string) (Info, error)
func PayloadOf(*dashapi.Object) (proto.Message, error)
func WrapObject(dashapi.ObjectKind, []string, proto.Message) (*dashapi.Object, error)
```

The full 29-kind table is in [proto-dashapi.md § Key encoding](../../docs/tutorial/modules/proto-dashapi.md#key-encoding-the-bridge-to-sonic-app_db).

---

## 5. Model store

### 5.1 Data structure

```
Store
├── mu      sync.RWMutex
├── tables  map[ObjectKind] -> map[joinedKey] -> *row
│              (joinedKey is strings.Join(keyParts, ":"))
├── bus     *events.Bus
└── nextTx  atomic.Uint64

row
├── val          proto.Message      (deep-copied via proto.Clone on Apply)
├── createdTsNs  int64
└── updatedTsNs  int64
```

### 5.2 Invariants

- Every payload stored in `row.val` is a **defensive clone** of the input
  proto — callers cannot mutate stored state by retaining the input
  pointer.
- Every read (`Get`, `List`, `SnapshotEvents`) returns another clone — so
  the gRPC layer can mutate the response payload safely.
- `createdTsNs` is preserved across updates; `updatedTsNs` is refreshed
  on every Apply.
- Reads use `RLock`; mutations use `Lock`.

### 5.3 Operation: Apply

```go
func (s *Store) Apply(obj *dashapi.Object) (txnID string, eventType dashapi.EventType, err error) {
    info := kinds.Lookup(obj.Kind)
    require: len(obj.Key) == len(info.KeyParts) && all parts non-empty
    payload := kinds.PayloadOf(obj)             // typed
    clone   := proto.Clone(payload)
    key     := strings.Join(obj.Key, ":")
    txn     := fmt.Sprintf("tx-%d-%d", now, atomic++)

    s.mu.Lock()
    tbl := getOrCreate(s.tables, obj.Kind)
    cur, exists := tbl[key]
    now := time.Now().UnixNano()
    var ev dashapi.EventType
    if exists {
        cur.val = clone
        cur.updatedTsNs = now
        ev = EVENT_TYPE_UPDATED
    } else {
        tbl[key] = &row{val: clone, createdTsNs: now, updatedTsNs: now}
        ev = EVENT_TYPE_CREATED
    }
    s.mu.Unlock()

    out := kinds.WrapObject(obj.Kind, obj.Key, proto.Clone(clone))
    s.bus.Publish(&dashapi.Event{TxnId: txn, Type: ev, Object: out, ServerTsNs: now})
    return txn, ev, nil
}
```

### 5.4 Operation: Delete

Reads `cur` for the wire event, then `delete(tbl, key)`, then publishes
`EVENT_TYPE_DELETED` carrying the deleted payload (so subscribers can
re-derive prior state).

### 5.5 Operation: List

Prefix-filters joined keys, sorts ascending, deep-clones every match.

### 5.6 Operation: SnapshotEvents

Iterates `kinds.All` in enum order (so subscriber stream order is stable)
and emits one `EVENT_TYPE_SNAPSHOT` per stored object.

---

## 6. Event bus

### 6.1 Design goals

| Goal | How achieved |
|---|---|
| Multiple concurrent subscribers | Per-sub channel; publisher snapshots the subscriber map under RLock then sends outside the lock |
| One slow consumer must not stall others | Non-blocking `select { case ch <- ev: default: drop++ }` |
| Filter by `ObjectKind` | Per-sub `kinds` set; publisher checks `matches()` before sending |
| Visibility into drops | `Dropped() uint64` + exposed via `/admin/health` |

### 6.2 Bus API

```go
const DefaultBuffer = 256

func New() *Bus
func (b *Bus) Subscribe(kinds []dashapi.ObjectKind) *Subscription
func (b *Bus) Publish(ev *dashapi.Event)
func (b *Bus) SubscriberCount() int
func (b *Bus) Dropped() uint64

type Subscription struct {
    C      <-chan *dashapi.Event     // read-only
    // closed exactly once via Close()
}
func (s *Subscription) Close()
```

### 6.3 Publish algorithm

```
mu.RLock()
matched := [s in subs if s.matches(ev.kind)]
mu.RUnlock()
for s in matched:
    select {
        case s.ch <- ev:                  // happy path
        default:  atomic.Inc(&dropped)    // slow consumer; drop
    }
```

Drops are intentional. Subscribers that fall behind reconnect with
`snapshot_first=true` to resync.

---

## 7. Fault injector

### 7.1 Spec semantics

```go
type Spec struct {
    Op      string   // RPC name ("Apply", "Get", ...) or "*"
    Mode    Mode     // "error" | "delay" | "drop"
    Count   int      // remaining triggers; <=0 == infinite; 0 defaults to 1
    DelayMs int      // for mode=delay
    Message string   // for mode=error|drop
}
```

### 7.2 Apply contract

Called at the top of every RPC handler:

```go
if err := faults.Apply("Apply"); err != nil {
    return ack("", err), nil   // returns Ack{accepted:false, error:msg}
}
```

For `mode=delay` the function sleeps `DelayMs` then returns nil so the
handler continues normally. A matched spec is decremented; when `count`
hits 0 it is removed from the active list.

---

## 8. Counters registry

### 8.1 Storage

`map[joinedKey] -> objectCounters` where `objectCounters` holds five
`atomic.Int64` values: `packets_in`, `packets_out`, `bytes_in`,
`bytes_out`, `drops`.

### 8.2 Tick formula (deterministic, hash-derived)

```
h = FNV-32a(key)
packetsIn  += 1 + (h        & 0x0f)
packetsOut += 1 + ((h >> 4) & 0x0f)
bytesIn    += 64 * (1 + (h        & 0xff))
bytesOut   += 64 * (1 + ((h >> 8) & 0xff))
if h % 23 == 0: drops += 1
```

Deterministic so unit tests can assert exact counter values per tick.

### 8.3 Tick driver

A goroutine in `main.go` runs every `--tick-interval` and calls
`registry.Tick(key)` for every `(kind, key)` returned by `Store.AllKeys()`.
The pipeline additionally ticks the matched ENI (and the `vnet:dst_ip`
mapping key on ENCAP).

---

## 9. Pipeline — behavioural model

This is the heart of `dash-sim`. It implements the canonical DASH packet
flow as defined in
[sonic-net/DASH/documentation](https://github.com/sonic-net/DASH/tree/main/documentation/general).

### 9.1 Entry point

```go
type Engine struct {
    Store    *model.Store
    Counters *counters.Registry
}
func (e *Engine) Evaluate(pkt *dashapi.Packet, trace bool) *dashapi.Decision
```

### 9.2 Outbound pipeline (VM → network)

```
Packet{direction=OUTBOUND, eni, src_ip, dst_ip, protocol, sport, dport}
        │
        ▼
(1) Lookup ENI by eni key
        require: admin_state == STATE_ENABLED                   else → DROP
        │
        ▼
(2) ACL_OUT stages 1..5  (acl_out[(eni, stage)] → group_id)
        For each stage 1..5:
            bind := Store.Get(acl_out, [eni, stage])           skip if missing
            group_id := isIPv4 ? bind.v4_acl_group_id : bind.v6_acl_group_id
            require: acl_group[group_id] exists                 skip if missing
            rules := List(acl_rule, group_id+":") sorted by priority ASC
            For each rule:
                if !aclRuleMatches(rule, pkt): continue
                if rule.action == DENY  → DROP (record stage, priority)
                if rule.terminating     → break stage
        │
        ▼
(3) Resolve route group:
        eni_route[eni] → group_id                               else → DROP
        │
        ▼
(4) Route LPM over (group_id, dst_ip):
        Items := List(route, group_id+":")
        Filter to keys with prefix containing dst_ip
        Pick longest-prefix-match                                else → DROP
        record matched_route_prefix
        │
        ▼
(5) Dispatch by route.routing_type:
        DROP                       → DROP
        DIRECT                     → FORWARD (no encap)
        VNET / VNET_DIRECT / VNET_ENCAP:
              vnet_mapping[(route.vnet, dst_ip)] → underlay_ip, vni
              tick counters on eni AND "vnet:dst_ip"
              → ENCAP (out_underlay_ip, out_vni)
        SERVICETUNNEL              → ENCAP using route.service_tunnel.underlay_dip
        APPLIANCE                  → routing_appliance[appliance_id] → ENCAP
        (others)                   → DROP (unsupported)
```

### 9.3 Inbound pipeline (network → VM)

```
Packet{direction=INBOUND, dst_mac OR eni, vni, src_ip, dst_ip, ...}
        │
        ▼
(1) Resolve ENI:
        if eni != ""           : use it
        else scan ENIs by mac_address == dst_mac                else → DROP
        │
        ▼
(2) admin_state check                                            else → DROP
        │
        ▼
(3) route_rule lookup:
        candidates := List(route_rule, eni+":"+vni+":")
        filter: key[2] is CIDR string and contains src_ip
        pick lowest priority (key[3])                            else → DROP
        │
        ▼
(4) rule.action_type:
        DECAP / MAPDECAP → continue
        DROP             → DROP
        │
        ▼
(5) ACL_IN stages 1..5  (acl_in[(eni, stage)] → group_id)
        Same algorithm as ACL_OUT.
        │
        ▼
(6) FORWARD to ENI (deliver to VM)
```

### 9.4 ACL rule matching algorithm

```
aclRuleMatches(rule, pkt):
    if len(rule.protocol) > 0   and pkt.protocol not in rule.protocol  : return false
    if len(rule.src_addr) > 0   and !anyPrefixMatches(rule.src_addr, pkt.src_ip) : return false
    if len(rule.dst_addr) > 0   and !anyPrefixMatches(rule.dst_addr, pkt.dst_ip) : return false
    if len(rule.src_port) > 0   and !anyPortMatches(rule.src_port, pkt.src_port) : return false
    if len(rule.dst_port) > 0   and !anyPortMatches(rule.dst_port, pkt.dst_port) : return false
    return true
```

Empty repeated fields are **match-all**, per upstream proto comments.

### 9.5 Prefix and port matching

Upstream `IpPrefix` is `{ip, mask}` — we convert mask bytes to a netmask
bit-count and use Go's `netip.Prefix.Contains(addr)`. For IPv6 we use
the 16-byte `bytes` field; for IPv4 the network-byte-order `fixed32`.

`ValueOrRange` is a oneof of `value uint32` and `Range{min, max}` — both
shapes are supported in `anyPortMatches`.

### 9.6 Counter ticks on outcomes

| Outcome | Tick |
|---|---|
| DROP | none (intentional — DPU drop counter is incremented elsewhere by hash-driven background tick) |
| FORWARD | none beyond background tick |
| ENCAP | `counters.Tick(eni)` AND `counters.Tick(vnet + ":" + dst_ip)` |

### 9.7 Trace

When `SimulatePacketRequest.trace = true`, each step appends a one-line
record to `Decision.trace[]`. Reduces noise in production calls (default
off) while keeping deep debuggability one flag away.

---

## 10. Scenario loader

A scenario YAML is a list of `{kind, key, value}` entries that get
`Apply`-ed in declaration order. Each `value` is converted as:

```
YAML node  →  encoding/json bytes  →  protojson.Unmarshal into kinds.NewZero()
```

This means **upstream proto field names** (snake_case, enum strings,
base64 for bytes, network-byte-order int for `fixed32` IpAddress) are
exactly what the scenario author writes. There is no project-specific
DSL; the YAML is a transparent view of the upstream proto.

Worked example:
[`testdata/scenarios/small.yaml`](../../src/impl-go/dash-sim/testdata/scenarios/small.yaml).

---

## 11. Admin HTTP control plane

| Method | Path | Body | Purpose |
|---|---|---|---|
| GET | `/admin/health` | — | `{status, device_id, subscribers, dropped_events, sizes{<kind>:N}}` |
| GET | `/admin/dump` | — | `{<kind>: [{key, value}, ...]}` for every kind |
| POST | `/admin/reset` | — | wipe store |
| GET | `/admin/faults` | — | list active `Spec`s |
| POST | `/admin/faults` | `Spec` | add a Spec |
| DELETE | `/admin/faults` | — | clear all |
| POST | `/admin/scenario` | `{path, reset?}` | server-side YAML load |
| GET | `/admin/counters?k=joined` | — | counter snapshot |
| GET | `/admin/kinds` | — | enumerate kinds + `key_parts` |

All responses are JSON. `/admin/dump` renders proto payloads via `protojson`
with `UseProtoNames=true` so the field names match the wire format exactly.

---

## 12. gRPC service — every RPC

The `Server` type embeds `dashapi.UnimplementedDashApiServer` and overrides
each handler. Every handler starts with `faults.Apply("<RpcName>")` and
delegates to model/pipeline/counters. This section documents the contract
of each.

### 12.1 `Apply(ApplyRequest) → Ack`

| Aspect | Spec |
|---|---|
| Purpose | Create or replace one Object. |
| Validation | kind must be known; key parts length must match `kinds.Info.KeyParts`; no empty parts. |
| Behaviour | First call CREATES, subsequent calls UPDATE; both emit an Event. |
| Side effects | `model.Store.Apply` → `events.Bus.Publish` (CREATED|UPDATED). |
| Errors | All validation errors and store errors are returned in `Ack{accepted:false, error:...}` — the RPC itself returns `nil` error so the client always gets a structured Ack. |
| Idempotency | Idempotent on payload identity (re-applying the same payload bumps `updated_ts_ns` and emits an UPDATED event, but yields the same observable state). |
| Fault op name | `"Apply"` |

### 12.2 `Delete(DeleteRequest) → Ack`

| Aspect | Spec |
|---|---|
| Purpose | Remove one Object. |
| Validation | kind known; key parts count correct. |
| Side effects | `model.Store.Delete` → `events.Bus.Publish(DELETED)`; `counters.Forget(joined-key)`. |
| Errors | `Ack.error = "not found"` if absent; otherwise `accepted=true`. |
| Cascading | None at this layer — deleting a VNET does NOT automatically delete its ENIs. Higher-level reconciliation (planned `dashd`) handles cascades. |
| Fault op name | `"Delete"` |

### 12.3 `Get(GetRequest) → GetResponse`

| Aspect | Spec |
|---|---|
| Purpose | Read one Object. |
| Returns | `GetResponse{object: <wrapped, deep-cloned>}`. |
| Errors | gRPC `NotFound` if absent; gRPC `InvalidArgument` for unknown kind. |
| Fault op name | `"Get"` |

### 12.4 `List(ListRequest) → stream ListItem`

| Aspect | Spec |
|---|---|
| Purpose | Stream every Object of a kind. |
| Filter | Optional `key_prefix` filters joined keys with `strings.HasPrefix`. |
| Limit | If `req.Limit > 0`, server stops after sending that many items. |
| Order | Ascending by joined key (stable across calls). |
| Errors | gRPC `InvalidArgument` for unknown kind; `Unavailable` if fault injected. |
| Fault op name | `"List"` |

### 12.5 `Subscribe(SubscribeRequest) → stream Event`

| Aspect | Spec |
|---|---|
| Purpose | Live event stream + optional snapshot. |
| Snapshot | If `snapshot_first=true`, server first sends one `EVENT_TYPE_SNAPSHOT` per existing object (filtered by `kinds`). |
| Filter | Empty `kinds` = all kinds. Filter is applied at publish-time on the bus, not in the handler. |
| Lifetime | Stream lives until ctx is cancelled or peer closes. |
| Backpressure | Per-subscriber channel buffer is 256. If full, events drop and `/admin/health.dropped_events` counter increments. Client should re-subscribe with snapshot on detection. |
| Fault op name | `"Subscribe"` |

### 12.6 `GetCounters(CountersRequest) → CountersResponse`

| Aspect | Spec |
|---|---|
| Purpose | Read counter snapshot for one object key. |
| Storage | In-process `counters.Registry`. |
| Auto-ticks | Driven by a goroutine in `main.go` at `--tick-interval` (default 1s). |
| Returns | Always returns all five counter names (zero values if key unknown). |
| Fault op name | `"GetCounters"` |

### 12.7 `SimulatePacket(SimulatePacketRequest) → SimulatePacketResponse`

| Aspect | Spec |
|---|---|
| Purpose | Run a synthetic packet through the behavioural pipeline. |
| Side effects | Updates counters on FORWARD/ENCAP outcomes. Does NOT mutate the model store. |
| Returns | `Decision{action, reason, out_eni, out_underlay_ip, out_vni, out_routing_type, matched_acl_stage, matched_acl_priority, matched_route_prefix, trace?}`. |
| Trace | Empty unless `request.trace = true`. |
| Errors | Pipeline errors are reported in `Decision.action = DROP` with a human reason — the RPC always succeeds at the transport layer. |
| Fault op name | `"SimulatePacket"` |

---

## 13. Per upstream proto — usage and stitching

Each row below maps an upstream `.proto` file to its role in `dash-sim`.
Files marked **stored** are kept in `model.Store` and observable via
`Get/List/Subscribe`. Files marked **active** participate in `SimulatePacket`.

| Upstream file | Stored? | Active in pipeline? | How used |
|---|---|---|---|
| `vnet.proto` | yes | indirectly | Referenced from `eni.vnet`, `route.vnet`, `vnet_mapping.vnet`; identifies the VNI tenant for encap. |
| `eni.proto` | yes | yes | First lookup on every packet. `admin_state` gates the pipeline. `mac_address` resolves inbound packets by `dst_mac`. |
| `eni_route.proto` | yes | yes | `eni → route_group_id` binding. Required for outbound route LPM. |
| `acl_group.proto` | yes | yes | Defines `ip_version` for stage match; rules under the same `group_id` are evaluated together. |
| `acl_rule.proto` | yes | yes | 5-tuple match. `priority` sets evaluation order. `terminating` ends the stage. `DENY` drops. |
| `acl_in.proto` | yes | yes | `eni × stage(1..5) → v4/v6_acl_group_id` for inbound stages. |
| `acl_out.proto` | yes | yes | Same, for outbound stages. |
| `route.proto` | yes | yes | Outbound LPM result. `routing_type` dispatches: DROP / DIRECT / VNET* / SERVICETUNNEL / APPLIANCE. |
| `route_group.proto` | yes | indirectly | Acts as a parent / version anchor for routes; the LPM does not need its payload but it must exist for clean orchestration. |
| `route_rule.proto` | yes | yes | Inbound `(eni, vni, src-prefix, priority)` table. Selects `action_type` and `vnet`. |
| `route_type.proto` | yes | indirectly | Defines `ActionType` / `RoutingType` enums and a configurable action set (`RouteType.items`); enum values are used directly by pipeline dispatch. |
| `routing_appliance.proto` | yes | yes | Resolves `APPLIANCE` route action — picks the first `addresses[0]` as the underlay next-hop and uses `vni`. |
| `prefix_tag.proto` (`dash.tag`) | yes | indirectly | Pipeline does NOT yet resolve `src_tag`/`dst_tag` rule lists into prefix sets (TODO; see roadmap). Stored so it can be Apply/Get/Subscribed. |
| `vnet_mapping.proto` | yes | yes | Resolves `(vnet, overlay_dst_ip) → underlay_ip + vni` for outbound encap. |
| `tunnel.proto` | yes | indirectly | Stored. Referenced via `route.tunnel` and `vnet_mapping.tunnel` (TODO: pipeline does not yet honour explicit tunnel refs; see roadmap). |
| `pa_validation.proto` | yes | indirectly | Stored. Not yet consulted by inbound — `route_rule.pa_validation` honors a boolean flag in the spec, but PA list validation is on the roadmap. |
| `qos.proto` | yes | no | Stored only. No rate-limiting in the simulator yet (planned). |
| `meter.proto` | yes | no | Stored only. Counters are synthetic; metering integration on the roadmap. |
| `meter_policy.proto` | yes | no | Stored only. |
| `meter_rule.proto` | yes | no | Stored only. |
| `outbound_port_map.proto` | yes | no | Stored only. Port-NAT not yet implemented in the pipeline. |
| `outbound_port_map_range.proto` | yes | no | Stored only. |
| `appliance.proto` | yes | no | Stored only. Represents the DPU's own identity / VM VNI. The pipeline doesn't read it today — `--device-id` flag substitutes for runtime identity. |
| `ha_scope.proto` | yes | no | Stored only. HA state machine is not implemented; LLD parity is faithful round-trip + Subscribe. |
| `ha_scope_config.proto` | yes | no | Stored only. |
| `ha_scope_state.proto` | yes | no | Stored only. |
| `ha_set.proto` | yes | no | Stored only. |
| `ha_set_config.proto` | yes | no | Stored only. |
| `ha_set_state.proto` | yes | no | Stored only. |
| `types.proto` | n/a | n/a | Provides `IpAddress`, `IpPrefix`, `ValueOrRange`, `Guid`, `IpVersion`, `HaState`, `HaRole` etc. used by every other message. |

> "Stored only" kinds are full-fidelity at the data layer: you can apply,
> list, subscribe, and snapshot them, and they survive byte-for-byte. They
> are simply not yet consulted by the **packet pipeline**. The roadmap
> tracks each gap.

---

## 14. Concurrency model

| Component | Lock | Notes |
|---|---|---|
| `model.Store.mu` | `sync.RWMutex` | Writers (Apply/Delete) take Lock; readers (Get/List/Snapshot) take RLock; clones are taken outside the lock for the wire layer. |
| `events.Bus.mu` | `sync.RWMutex` | RLock for snapshotting subscriber set; Lock only for (un)subscribe. Publish-to-channel happens outside the lock. |
| `faults.Injector.mu` | `sync.Mutex` | Trivial — fault list is short. |
| `counters.Registry.mu` | `sync.RWMutex` | RLock for read; double-checked lock pattern in `get()` to create-on-miss. |
| Per-object counter | `atomic.Int64` × 5 | Lock-free hot path. |
| Goroutines | tick driver, gRPC server, admin HTTP server, per-Subscribe sender (the gRPC stream goroutine). | Each shuts down on the root context. |

No mutex is held across a channel send or a network call. The
`events.Bus` is intentionally lossy under load — see § 6.

---

## 15. Rust pseudocode parity

The same algorithms in idiomatic Rust + tonic. This is reference design
for the future `impl-rust/crates/dash-sim`.

### 15.1 Store and Apply

```rust
struct Store {
    inner: RwLock<HashMap<ObjectKind, HashMap<String, Row>>>,
    bus:   Arc<Bus>,
    next_tx: AtomicU64,
}

struct Row {
    val: Box<dyn Message + Send + Sync>,
    created_ts_ns: i64,
    updated_ts_ns: i64,
}

impl Store {
    fn apply(&self, obj: Object) -> Result<(String, EventType), ApplyError> {
        let info = kinds::lookup(obj.kind)?;
        ensure_eq!(obj.key.len(), info.key_parts.len());

        let payload = kinds::payload_of(&obj)?;
        let clone   = payload.clone_box();
        let key     = obj.key.join(":");
        let txn     = format!("tx-{}-{}", now_ns(), self.next_tx.fetch_add(1, SeqCst));

        let mut t = self.inner.write();
        let tbl = t.entry(obj.kind).or_insert_with(HashMap::new);
        let now = now_ns();
        let ev = if let Some(row) = tbl.get_mut(&key) {
            row.val = clone.clone_box();
            row.updated_ts_ns = now;
            EventType::Updated
        } else {
            tbl.insert(key.clone(), Row { val: clone.clone_box(), created_ts_ns: now, updated_ts_ns: now });
            EventType::Created
        };
        drop(t);

        let out = kinds::wrap_object(obj.kind, &obj.key, clone)?;
        self.bus.publish(Event { txn_id: txn.clone(), r#type: ev as i32, object: Some(out), server_ts_ns: now });
        Ok((txn, ev))
    }
}
```

### 15.2 Event bus

```rust
struct Bus {
    inner: RwLock<HashMap<u64, Subscription>>,
    next:  AtomicU64,
    dropped: AtomicU64,
}

struct Subscription {
    tx:    mpsc::Sender<Event>,   // bounded; capacity 256
    kinds: HashSet<ObjectKind>,
}

impl Bus {
    fn publish(&self, ev: Event) {
        let kind = ev.object.as_ref().map(|o| o.kind()).unwrap_or_default();
        let subs: Vec<_> = {
            let g = self.inner.read();
            g.values().filter(|s| s.kinds.is_empty() || s.kinds.contains(&kind)).cloned().collect()
        };
        for s in subs {
            if s.tx.try_send(ev.clone()).is_err() {
                self.dropped.fetch_add(1, Relaxed);
            }
        }
    }
}
```

### 15.3 Pipeline dispatch (outbound, abbreviated)

```rust
fn evaluate_outbound(eng: &Engine, pkt: &Packet, trace: &mut Trace) -> Decision {
    let eni = match load_eni(&eng.store, &pkt.eni) {
        Some(e) if e.admin_state == State::Enabled as i32 => e,
        Some(_) => return drop(format!("eni {} admin_state=DISABLED", pkt.eni), trace),
        None    => return drop(format!("eni {} not found", pkt.eni), trace),
    };

    if let Some((stage, prio)) = eval_acl_out(eng, &pkt, &pkt.eni, trace) {
        return drop_acl(stage, prio, trace);
    }

    let group_id = match load_eni_route_group(&eng.store, &pkt.eni) {
        Ok(g) => g,
        Err(e) => return drop(format!("eni_route: {e}"), trace),
    };
    let (route, prefix) = match lookup_route(&eng.store, &group_id, &pkt.dst_ip, trace) {
        Ok(x)  => x,
        Err(e) => return drop(format!("route lookup: {e}"), trace),
    };

    match RoutingType::try_from(route.routing_type).unwrap_or_default() {
        RoutingType::Drop      => drop("route action=DROP".into(), trace),
        RoutingType::Direct    => forward(&pkt.eni, "direct", trace),
        RoutingType::Vnet | RoutingType::VnetDirect | RoutingType::VnetEncap => {
            let vnet = route_vnet(&route);
            let mapping = lookup_vnet_mapping(&eng.store, &vnet, &pkt.dst_ip, trace)?;
            let underlay = ip_string(mapping.underlay_ip.as_ref());
            eng.counters.tick(&pkt.eni); eng.counters.tick(&format!("{vnet}:{}", pkt.dst_ip));
            encap(&pkt.eni, &underlay, 0, trace)
        }
        RoutingType::ServiceTunnel => { /* ... */ }
        RoutingType::Appliance     => { /* ... */ }
        other => drop(format!("routing_type {other:?} unsupported"), trace),
    }
}
```

### 15.4 ACL evaluation

```rust
fn acl_rule_matches(r: &AclRule, pkt: &Packet) -> bool {
    if !r.protocol.is_empty() && !r.protocol.contains(&pkt.protocol) { return false; }
    if !r.src_addr.is_empty() && !any_prefix_matches(&r.src_addr, &pkt.src_ip) { return false; }
    if !r.dst_addr.is_empty() && !any_prefix_matches(&r.dst_addr, &pkt.dst_ip) { return false; }
    if !r.src_port.is_empty() && !any_port_matches(&r.src_port, pkt.src_port) { return false; }
    if !r.dst_port.is_empty() && !any_port_matches(&r.dst_port, pkt.dst_port) { return false; }
    true
}
```

---

## 16. Failure modes and error taxonomy

| Failure | Where surfaced | Recovery |
|---|---|---|
| Unknown ObjectKind | `kinds.Lookup` → handler returns `InvalidArgument` (or `Ack.error`) | Caller fixes input. |
| Wrong key parts count | `model.Store.Apply/Delete` → `Ack.error = "kind X expects N key parts ..."` | Caller fixes input. |
| Empty key part | `Ack.error = "kind X key part \"...\" is empty"` | Caller fixes input. |
| Object not found | `Get` → gRPC `NotFound`; `Delete` → `Ack.error = "not found"` | Caller fixes input. |
| Subscribe buffer full | event dropped silently; `/admin/health.dropped_events` increments | Resubscribe with `snapshot_first=true`. |
| Pipeline DROP | `Decision{action=DROP, reason=...}` | Inspect reason / trace; reconfigure store. |
| Fault injected | `Ack.error = <message>` OR gRPC `Unavailable` | Clear faults via `DELETE /admin/faults`. |
| Server crash | OS kill | systemd / orchestrator restarts the process; state is lost (in-memory by design). |

---

## 17. Extension recipes

### 17.1 Add a new ObjectKind

Six-step recipe in
[modules/dashapi-runtime.md § Adding a new ObjectKind](../../docs/tutorial/modules/dashapi-runtime.md#adding-a-new-objectkind).
After registration the kind is automatically:
- accepted by `model.Store`
- listed by `/admin/kinds` and `dash-sim-client kinds`
- subscribable
- usable in scenario YAMLs

### 17.2 Add a new pipeline branch

Steps:
1. Add the new `RoutingType` enum value (already provided by upstream — no
   proto change needed).
2. Add a `case` in the outbound dispatch in
   [`pipeline.go § outbound`](../../src/impl-go/dash-sim/internal/sim/pipeline/pipeline.go).
3. Add a unit test in
   [`pipeline_test.go`](../../src/impl-go/dash-sim/internal/sim/pipeline/pipeline_test.go).

### 17.3 Add a new admin endpoint

Add a `mux.HandleFunc("/admin/<path>", h.<method>)` line in
[`admin/http.go`](../../src/impl-go/dash-sim/internal/sim/admin/http.go).
Keep handlers JSON in / JSON out for simplicity.

---

## 18. Test surface

| Layer | Tests | Where |
|---|---|---|
| Pipeline | 8 conformance cases | [`internal/sim/pipeline/pipeline_test.go`](../../src/impl-go/dash-sim/internal/sim/pipeline/pipeline_test.go) |
| Store / events / faults / counters | exercised through pipeline tests | (same) |
| Cross-binary | manual smoke (page [06](../../docs/tutorial/06-test.md)) | tutorial |
| API contract | implicit via `dash-sim-client` against live binary | tutorial |

Run with:
```bash
go test ./dash-sim/...
```
