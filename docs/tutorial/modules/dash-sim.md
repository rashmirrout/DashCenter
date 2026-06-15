# dash-sim — the behavioural DASH-DPU simulator

| Field | Value |
|---|---|
| Source | [`src/impl-go/dash-sim/`](../../../src/impl-go/dash-sim/) |
| Binary | `dash-sim` (or `dash-sim.exe`) |
| Default ports | `:50051` (gRPC), `:8080` (admin HTTP) |
| Status | stable |

> `dash-sim` is sometimes referred to as "dash-shim" in older notes. They
> are the same binary.

---

## 1. Role

A **single-process behavioural simulator** for a DASH-compliant DPU agent.
It implements every RPC in `dashapi.v1.DashApi` (including `SimulatePacket`)
backed by an **in-memory store** and a **pub/sub event bus**.

Use it for:
- local development and iteration (no hardware required)
- pipeline experimentation (`SimulatePacket` walks the canonical DASH flow)
- CI fixtures (deterministic, scriptable, scenario-driven)
- demos and tutorials

Do **not** use it for:
- real packet forwarding (it has no data plane)
- SONiC interop (use `dash-redis-adapter` for the SONiC-compatible APP_DB layout)
- HA leader election (HA objects are stored faithfully but no state-machine is run)

---

## 2. Internal layout

```
dash-sim/
├── go.mod
├── Dockerfile
├── cmd/dash-sim/main.go        -- wires everything together
└── internal/sim/
    ├── admin/                  -- HTTP /admin/* endpoints
    │   └── http.go
    ├── counters/               -- per-key synthetic packet/byte counters
    │   └── counters.go
    ├── events/                 -- in-process fan-out pub/sub bus
    │   └── bus.go
    ├── faults/                 -- fault injection (matches RPC names)
    │   └── faults.go
    ├── model/                  -- generic kind+key in-memory store
    │   └── store.go
    ├── pipeline/               -- BEHAVIOURAL DASH packet pipeline
    │   ├── pipeline.go
    │   └── pipeline_test.go    -- 8 conformance tests
    ├── scenarios/              -- YAML scenario loader (uses protojson)
    │   └── loader.go
    └── server/                 -- DashApi gRPC service implementation
        └── service.go
```

Every package is `internal/` because nothing outside `dash-sim` is meant to
import simulator internals (only the on-the-wire DashApi contract).

---

## 3. `model.Store` — the generic state container

A single `Store` value holds every object kind. Keyed by
`(dashapi.ObjectKind, joined-key-string)`. Mutations:

1. Validate kind + key parts (number of parts must match `kinds.Lookup(k).KeyParts`)
2. **FK validation** — when `strictRefs` is `true` (default), check that
   every foreign-key reference in the object already exists in the store.
   25 southbound FK relationships are validated via a declarative
   `fkRules` table in `model/refs.go`. If a reference is missing,
   `Apply()` returns an error naming the missing object and the field:
   ```
   referential integrity: eni references vnet "vnet-bllue" (field vnet)
   which does not exist; create it first
   ```
   See [referential-integrity-validation.md](../../dashd-features/referential-integrity-validation.md)
   for the full FK map and tier ordering.
3. `proto.Clone` the payload (defensive copy)
4. Stamp `created_ts_ns` / `updated_ts_ns` in a sidecar map
5. Publish an `*dashapi.Event` onto the `events.Bus`

`SetStrictRefs(false)` disables FK checks (for tests or legacy pipelines).

API:

```go
func New(bus *events.Bus) *Store
func (s *Store) Apply(obj *dashapi.Object) (txnID string, evType dashapi.EventType, err error)
func (s *Store) Delete(kind dashapi.ObjectKind, key []string) (txnID string, err error)
func (s *Store) Get(kind dashapi.ObjectKind, key []string) (*dashapi.Object, error)
func (s *Store) List(kind dashapi.ObjectKind, keyPrefix string) ([]*dashapi.Object, error)
func (s *Store) SnapshotEvents(filter []dashapi.ObjectKind) []*dashapi.Event
func (s *Store) AllKeys() map[dashapi.ObjectKind][]string
func (s *Store) Reset()
func (s *Store) Len() map[string]int
```

---

## 4. `events.Bus` — non-blocking fan-out

In-process pub/sub powering `Subscribe`. Each subscriber gets a
**bounded channel** (256 by default); slow consumers lose events and the
global `Dropped()` counter increments.

```go
func New() *Bus
func (b *Bus) Subscribe(kinds []dashapi.ObjectKind) *Subscription
func (b *Bus) Publish(ev *dashapi.Event)
func (b *Bus) SubscriberCount() int
func (b *Bus) Dropped() uint64

type Subscription struct{ C <-chan *dashapi.Event }
func (s *Subscription) Close()
```

If a subscriber filters by `kinds`, only matching events go in their
channel (filter happens at publish time).

---

## 5. Pipeline — the canonical DASH packet flow

The crown jewel. Implements the documented DASH pipeline against the model
store. Sources:
[`pipeline.go`](../../../src/impl-go/dash-sim/internal/sim/pipeline/pipeline.go)
+
[`pipeline_test.go`](../../../src/impl-go/dash-sim/internal/sim/pipeline/pipeline_test.go).

### 5.1 Outbound (VM → network)

```
Packet{direction=OUTBOUND, eni, src_ip, dst_ip, protocol, src_port, dst_port}
    │
    ▼
ENI lookup (eni)
    │  - admin_state must be STATE_ENABLED  -> else DROP
    ▼
ACL_OUT stages 1..5
    │  for stage in 1..5:
    │     bind = Store.Get(acl_out, [eni, stage])     -> if missing, skip stage
    │     group_id = (ipv4 ? v4_acl_group_id : v6_acl_group_id)
    │     rules = List(acl_rule, group_id+":") sorted by priority ASC
    │     for rule in rules:
    │        if matches(rule, packet):
    │           if rule.action == DENY  -> DROP (record stage + priority)
    │           if rule.terminating     -> break stage
    ▼
eni_route -> route_group_id
    │
    ▼
Route LPM (group_id, dst_ip)
    │  Picks the route with the longest prefix containing dst_ip.
    │  If none -> DROP.
    ▼
Route action:
    DROP             -> DROP
    DIRECT           -> FORWARD (no encap)
    VNET / VNET_DIRECT / VNET_ENCAP:
        vnet_mapping[(route.vnet, dst_ip)] -> underlay_ip, vni
        -> ENCAP (out_underlay_ip, out_vni)
        + tick counter on eni AND on "vnet:dst_ip"
    SERVICETUNNEL    -> ENCAP using route.service_tunnel.underlay_dip
    APPLIANCE        -> routing_appliance[appliance_id] -> ENCAP
    other            -> DROP
```

### 5.2 Inbound (network → VM)

```
Packet{direction=INBOUND, dst_mac OR eni, vni, src_ip, dst_ip, ...}
    │
    ▼
ENI lookup
    - if eni supplied -> use it
    - else scan ENIs by mac_address == dst_mac
    -> if not found  -> DROP
    │
    ▼
admin_state check (STATE_ENABLED)
    │
    ▼
route_rule lookup
    Prefix-filter Store.List(route_rule, eni+":"+vni+":")
    Of those, prefix-match src_ip against key[2] (CIDR string).
    Pick the lowest priority value. If none -> DROP.
    │
    ▼
rule.action_type:
    DECAP / MAPDECAP -> continue
    DROP             -> DROP
    │
    ▼
ACL_IN stages 1..5 (same shape as ACL_OUT)
    │
    ▼
FORWARD to ENI (deliver to VM)
```

### 5.3 5-tuple ACL matching

```go
func aclRuleMatches(r *AclRule, pkt *Packet) bool {
    // Each repeated field, when empty, matches everything.
    return (len(r.protocol) == 0 || contains(r.protocol, pkt.protocol)) &&
           (len(r.src_addr) == 0 || anyPrefixMatches(r.src_addr, pkt.src_ip)) &&
           (len(r.dst_addr) == 0 || anyPrefixMatches(r.dst_addr, pkt.dst_ip)) &&
           (len(r.src_port) == 0 || anyPortMatches(r.src_port, pkt.src_port)) &&
           (len(r.dst_port) == 0 || anyPortMatches(r.dst_port, pkt.dst_port))
}
```

`anyPrefixMatches` uses the upstream `IpPrefix{ip, mask}` shape and
converts the mask to a `netip.Prefix` bit count before testing
`prefix.Contains(addr)`.

### 5.4 Counters

On every non-DROP outcome the engine ticks the matched ENI's counter via
`counters.Registry.Tick(eniKey)`. On `ENCAP` it also ticks the matching
`vnet:dst_ip` mapping key.

---

## 6. `server.Service` — gRPC implementation

Implements `dashapi.DashApiServer`. Each handler:

1. Calls `faults.Apply("RpcName")` — short-circuits with an injected error if a
   matching fault is active.
2. Delegates to `model.Store` or `pipeline.Engine`.
3. Returns the appropriate response.

`Subscribe` consumes from a `events.Bus` subscription, optionally
prepending `SnapshotEvents` from the store.

`SimulatePacket` calls `pipeline.Engine.Evaluate(pkt, trace)`.

---

## 7. Admin HTTP

Endpoints (sources in [`admin/http.go`](../../../src/impl-go/dash-sim/internal/sim/admin/http.go)):

| Method | Path | Purpose |
|---|---|---|
| GET | `/admin/health` | `{status, device_id, subscribers, dropped_events, sizes{<kind>: N}}` |
| GET | `/admin/dump` | `{ <kind>: [{key, value}, ...] }` for the whole store |
| POST | `/admin/reset` | Wipe store |
| GET | `/admin/faults` | List active fault specs |
| POST | `/admin/faults` | Add a fault `{op, mode, count?, delay_ms?, message?}` |
| DELETE | `/admin/faults` | Clear all faults |
| POST | `/admin/scenario` | Body `{path, reset?}` — load YAML from a server-side path |
| GET | `/admin/counters?k=<joined>` | Counter snapshot for `k` |
| GET | `/admin/kinds` | List supported kinds + `key_parts` |

Examples are in [docs/CLI_GUIDE.md § 14](../../CLI_GUIDE.md#14-admin-http-dash-sim-only-bonus).

---

## 8. Scenarios

`scenarios.LoadFile(path, store)` parses a YAML doc whose `spec` is a list of
`{kind, key, value}` entries. Each `value` is converted (yaml→json→
protojson) into the typed payload, then `Apply`-ed.

Reference scenario:
[`testdata/scenarios/small.yaml`](../../../src/impl-go/dash-sim/testdata/scenarios/small.yaml)
— covers 9 different upstream kinds.

---

## 9. Faults

```
POST /admin/faults  {"op":"Apply",         "mode":"error",  "count":1, "message":"injected"}
POST /admin/faults  {"op":"GetCounters",   "mode":"delay",  "delay_ms":500, "count":3}
POST /admin/faults  {"op":"*",             "mode":"drop",   "count":1}     # any RPC
DELETE /admin/faults                                                       # clear
```

`op` values match `DashApi` RPC names. `*` matches any. `count<=0` = infinite;
`count=0` defaults to 1.

---

## 10. Tests

Run with:
```bash
go test ./dash-sim/internal/sim/pipeline/...
```

The 8 conformance cases pin the exact pipeline semantics. Read them like
contract tests — any behavioural change in the pipeline must update these
tests.

### 10a. Referential integrity — how objects depend on each other

The DASH object graph is deeply interconnected. Before `Apply()` writes
an object to the store, it validates that every FK reference points to
an existing object. This is implemented in `model/refs.go` via a
declarative `fkRules` table with 25 rules covering all object kinds.

**Why this matters**: without FK validation, a typo in `vnet_name` goes
undetected. The ENI sits in the store with a dangling reference. Packets
arrive, the pipeline looks up the vnet mapping, finds nothing, and
drops silently. The operator discovers it 20 minutes later via counter
spikes — instead of at the PUT that caused it.

**How it works internally**:

1. `Store.Apply()` receives an object (kind + key + payload).
2. It iterates `fkRules` to find rules matching the object's kind.
3. Each rule extracts a ref value (e.g., `Eni.vnet`) and checks if
   the referenced object exists in the store.
4. If any ref is missing → error with kind, field, ref value, and fix.
5. If all refs resolve → object is written.

**The dependency tiers**:

```
Tier 0: vnet, qos, acl_group, route_group, tunnel, ...   (no FKs)
Tier 1: eni→vnet, acl_rule→acl_group, route→route_group  (refs T0)
Tier 2: eni_route→eni+route_group, acl_in→eni+acl_group  (refs T0+T1)
```

**Example error** (Apply an ENI when the vnet doesn't exist):

```
referential integrity: eni references vnet "vnet-bllue" (field vnet)
which does not exist; create it first
```

**Key design decisions**:
- `--strict-refs=true` is the default. Use `false` only for tests that
  don't care about FK order.
- Optional refs (empty string) are skipped — an ENI with `qos: ""`
  passes without looking up a qos object.
- The check runs under the store's write lock, so the ref can't be
  deleted between check and write.

**Tests**:

```bash
# 51 unit tests — all 25 FK families, StrictRefs toggle, error quality
go test -v ./dash-sim/internal/sim/model/ -run TestRefs

# 9 integration tests — FK validation over the full gRPC stack
go test -v ./dash-sim/test/integration/ -run TestIntegration_Refs
```

For the hands-on experiments (wrong config → error → right config → success),
see [RUN_AND_TEST.md §9c](../../../src/impl-go/dash-sim/RUN_AND_TEST.md).
