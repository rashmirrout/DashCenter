# proto/dashapi/v1 — the gRPC service contract

| Field | Value |
|---|---|
| Source | [`proto/dashapi/v1/dashapi.proto`](../../../proto/dashapi/v1/dashapi.proto) |
| Package | `dashapi.v1` |
| Go import | `github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1` |
| Status | stable |

---

## What it is

`dashapi.v1.DashApi` is the **only RPC surface** any DashCenter binary
exposes or consumes. It is a thin envelope over the upstream
[sonic-net/sonic-dash-api](https://github.com/sonic-net/sonic-dash-api)
proto types — every payload message is taken **verbatim** from the upstream
proto files vendored under `proto/vendor/sonic-dash-api/`.

```
                               dashapi.v1
                                    │
              ┌─────────────────────┼─────────────────────┐
              │                     │                     │
            Object              SubscribeRequest          Packet
        ┌────┴────┐             SimulatePacketRequest
        │ kind    │
        │ key[]   │  ─── 29 ObjectKinds, each pointing into one
        │ oneof:  │       upstream dash.<area> message:
        │   Vnet  │            dash.vnet.Vnet
        │   Eni   │            dash.eni.Eni
        │   ...   │            dash.acl_rule.AclRule
        └─────────┘            ... (29 in total)
```

---

## The service

```protobuf
service DashApi {
  rpc Apply         (ApplyRequest)        returns (Ack);
  rpc Delete        (DeleteRequest)       returns (Ack);
  rpc Get           (GetRequest)          returns (GetResponse);
  rpc List          (ListRequest)         returns (stream ListItem);
  rpc Subscribe     (SubscribeRequest)    returns (stream Event);
  rpc GetCounters   (CountersRequest)     returns (CountersResponse);
  rpc SimulatePacket(SimulatePacketRequest) returns (SimulatePacketResponse);
}
```

| RPC | Implemented in dash-sim | Implemented in dash-redis-adapter |
|---|---|---|
| `Apply` | ✓ in-memory store + event bus | ✓ Redis HSET + pub/sub |
| `Delete` | ✓ | ✓ |
| `Get` | ✓ | ✓ |
| `List` | ✓ | ✓ (SCAN over `DASH_<KIND>_TABLE:*`) |
| `Subscribe` | ✓ (in-process bus) | ✓ (Redis Pub/Sub on channel `dashapi.events`) |
| `GetCounters` | ✓ (synthetic) | ✓ (reads `DASH_COUNTERS:<key>`) |
| `SimulatePacket` | ✓ (full pipeline) | ✗ returns `Unimplemented` (Redis APP_DB has no pipeline state) |

---

## The 29 object kinds

```protobuf
enum ObjectKind {
  OBJECT_KIND_APPLIANCE               = 1;
  OBJECT_KIND_VNET                    = 2;
  OBJECT_KIND_ENI                     = 3;
  OBJECT_KIND_ENI_ROUTE               = 4;
  OBJECT_KIND_ACL_GROUP               = 5;
  OBJECT_KIND_ACL_RULE                = 6;
  OBJECT_KIND_ACL_IN                  = 7;
  OBJECT_KIND_ACL_OUT                 = 8;
  OBJECT_KIND_ROUTE                   = 9;
  OBJECT_KIND_ROUTE_GROUP             = 10;
  OBJECT_KIND_ROUTE_RULE              = 11;
  OBJECT_KIND_ROUTE_TYPE              = 12;
  OBJECT_KIND_ROUTING_APPLIANCE       = 13;
  OBJECT_KIND_PREFIX_TAG              = 14;
  OBJECT_KIND_VNET_MAPPING            = 15;
  OBJECT_KIND_TUNNEL                  = 16;
  OBJECT_KIND_PA_VALIDATION           = 17;
  OBJECT_KIND_QOS                     = 18;
  OBJECT_KIND_METER                   = 19;
  OBJECT_KIND_METER_POLICY            = 20;
  OBJECT_KIND_METER_RULE              = 21;
  OBJECT_KIND_OUTBOUND_PORT_MAP       = 22;
  OBJECT_KIND_OUTBOUND_PORT_MAP_RANGE = 23;
  OBJECT_KIND_HA_SCOPE                = 24;
  OBJECT_KIND_HA_SCOPE_CONFIG         = 25;
  OBJECT_KIND_HA_SCOPE_STATE          = 26;
  OBJECT_KIND_HA_SET                  = 27;
  OBJECT_KIND_HA_SET_CONFIG           = 28;
  OBJECT_KIND_HA_SET_STATE            = 29;
}
```

Each enum value corresponds 1:1 with an upstream
`.proto` file under `proto/vendor/sonic-dash-api/`. To add a new kind, see
[dashapi-runtime.md](dashapi-runtime.md).

---

## Key encoding (the bridge to SONiC APP_DB)

Upstream defines a typed `<Kind>Key` message for every kind. To keep wire
format compact and to map 1:1 with **SONiC APP_DB key suffixes**, we encode
keys as an ordered `repeated string`:

| Kind | KeyParts | Example joined key |
|---|---|---|
| `vnet` | `[vnet_name]` | `vnet-prod` |
| `eni` | `[eni]` | `eni-001` |
| `vnet_mapping` | `[vnet, ip_address]` | `vnet-prod:10.0.0.10` |
| `route` | `[group_id, prefix]` | `rg-prod:10.1.0.0/16` |
| `route_rule` | `[eni, vni, prefix_or_tag, priority]` | `eni-001:1001:0.0.0.0/0:100` |
| `acl_rule` | `[group_id, rule_num]` | `acl-prod-in:100` |
| `acl_in` / `acl_out` | `[eni, stage]` | `eni-001:1` |
| `meter` | `[eni, metering_class_id]` | `eni-001:42` |
| `meter_rule` | `[meter_policy_id, rule_num]` | `mp-1:10` |
| `outbound_port_map_range` | `[map_id, start_port, end_port]` | `pm-1:1000:2000` |
| `ha_scope_config` | `[vdpu_id, ha_scope_id]` | `vdpu-1:eni-001` |

When the adapter writes to Redis, the final key becomes:
```
DASH_<KIND>_TABLE:<joined-key>
e.g. DASH_VNET_MAPPING_TABLE:vnet-prod:10.0.0.10
```
This matches what SONiC's DASH orchagent reads.

---

## The `Object` envelope

```protobuf
message Object {
  ObjectKind kind = 1;
  repeated string key = 2;
  oneof payload {
    dash.vnet.Vnet         vnet         = 101;
    dash.eni.Eni           eni          = 102;
    dash.vnet_mapping.VnetMapping vnet_mapping = 114;
    ... (29 oneof fields)
  }
}
```

Field numbers (100+) are reserved for payloads so adding new ObjectKinds
never collides with envelope fields.

---

## `Event` and `Subscribe`

```protobuf
message Event {
  string    txn_id       = 1;
  EventType type         = 2;      // CREATED|UPDATED|DELETED|SNAPSHOT
  Object    object       = 3;
  int64     server_ts_ns = 4;
}
```

`Subscribe(kinds, snapshot_first)`:

- If `kinds` is empty, every kind is sent.
- If `snapshot_first=true`, the server sends one `EVENT_TYPE_SNAPSHOT`
  per existing object (filtered by `kinds`) before live updates.
- Live updates arrive as `EVENT_TYPE_CREATED|UPDATED|DELETED` in the order
  produced by the server.

---

## `Packet` and `SimulatePacket`

```protobuf
message Packet {
  Direction direction = 1;  // OUTBOUND|INBOUND
  string eni = 2;
  uint32 vni = 3;
  string src_mac, dst_mac, src_ip, dst_ip;
  uint32 protocol, src_port, dst_port, length_bytes;
}
message Decision {
  Action action = 1;        // FORWARD|DROP|ENCAP
  string reason, out_eni, out_underlay_ip, out_routing_type;
  uint32 out_vni;
  uint32 matched_acl_stage, matched_acl_priority;
  string matched_route_prefix;
  repeated string trace;    // populated only when SimulatePacketRequest.trace=true
}
```

See [dash-sim.md § pipeline](dash-sim.md#5-pipeline) for the full evaluation
algorithm.

---

## Versioning policy

- The package is `dashapi.v1`. Backwards-incompatible changes go to
  `dashapi.v2/dashapi.proto` (new directory).
- Field numbers are never reused or repurposed.
- New ObjectKind values are appended (next free number).
- New RPCs are added at the end of the service block.

---

## Editing the contract — workflow

1. Edit `proto/dashapi/v1/dashapi.proto` (or vendor a new upstream
   `sonic-dash-api` snapshot via
   [`scripts/vendor-protos.ps1`](../../../scripts/vendor-protos.ps1)).
2. Regenerate stubs:
   ```bash
   pwsh -NoProfile -File scripts/codegen-go.ps1
   ```
3. Build affected modules:
   ```bash
   cd src/impl-go
   go build ./...
   ```
4. If you added an `ObjectKind`, register it in
   [`dashapi-runtime/kinds/kinds.go`](../../../src/impl-go/dashapi-runtime/kinds/kinds.go).
   See [dashapi-runtime.md](dashapi-runtime.md).
5. Run tests:
   ```bash
   go test ./dash-sim/... ./dash-redis-adapter/...
   ```
