# LLD — dash-redis-adapter (SONiC-compatible Redis backend)

> Older notes refer to this as "dash-shim-redis". The current name is
> **dash-redis-adapter**.

This document is the **definitive low-level design** of
`dash-redis-adapter`. It exposes the exact same `dashapi.v1.DashApi` gRPC
service as `dash-sim` but stores state in Redis in the precise APP_DB
layout SONiC's DASH orchagent consumes. This is the bridge from the
DashCenter CLI/SDK to real DASH-compliant hardware.

---

## Table of contents

1. [Scope and non-goals](#1-scope-and-non-goals)
2. [Architecture](#2-architecture)
3. [SONiC APP_DB wire format](#3-sonic-app_db-wire-format)
4. [Module layout](#4-module-layout)
5. [Per-RPC implementation](#5-per-rpc-implementation)
6. [Subscribe via Redis Pub/Sub](#6-subscribe-via-redis-pubsub)
7. [Connection lifecycle](#7-connection-lifecycle)
8. [Concurrency model](#8-concurrency-model)
9. [Per upstream proto — table mapping](#9-per-upstream-proto-table-mapping)
10. [Rust pseudocode parity](#10-rust-pseudocode-parity)
11. [Failure modes](#11-failure-modes)
12. [Operational notes (real Redis, miniredis, persistence, cluster)](#12-operational-notes)
13. [Compatibility checklist against SONiC orchagent](#13-compatibility-checklist-against-sonic-orchagent)
14. [Test surface](#14-test-surface)

---

## 1. Scope and non-goals

### In scope

- Implement every RPC of `dashapi.v1.DashApi` against a Redis backend.
- Use the **canonical SONiC table naming** `DASH_<KIND>_TABLE:<joined-key>`.
- Serialize payloads as **binary protobuf** in a Redis hash field, with a
  sidecar JSON `meta` field for timestamps.
- Stream live changes via **Redis Pub/Sub** on channel `dashapi.events`.
- Provide an `--embedded-redis` mode (in-process `miniredis`) so the
  binary is self-contained for demos and CI.

### Explicitly out of scope

- The behavioural packet pipeline (`SimulatePacket` returns
  `codes.Unimplemented`). Real DPUs run their own data plane; for
  hardware-free behavioural testing use `dash-sim`.
- Counter synthesis. `GetCounters` reads from
  `DASH_COUNTERS:<joined-key>` — written by an external collector
  (e.g. a SONiC counter-collector daemon).
- Atomic multi-object transactions. Each Apply is one HSET + one PUBLISH.
  Multi-key transactions and consistency models are on the roadmap.
- Authentication beyond Redis password. TLS termination is on the roadmap.

---

## 2. Architecture

```
                      ┌────────────────────────────────────────┐
                      │       dash-redis-adapter process       │
gRPC ─────────────────┤                                        │
:52051                │   ┌──────────────┐                     │
                      │   │   server     │                     │
                      │   │ (DashApi)    │                     │
                      │   │              │                     │
                      │   │  Apply ──────┼─► HSET DASH_X_TABLE:K
                      │   │  Delete ─────┼─► DEL                │
                      │   │  Get ────────┼─► HGET               │
                      │   │  List ───────┼─► SCAN + HGET        │
                      │   │  Subscribe ──┼─► PSUBSCRIBE         │
                      │   │              │                     │
                      │   │  GetCounters─┼─► HGETALL DASH_CTRS:K│
                      │   │  SimulatePkt ┼─► Unimplemented      │
                      │   └─────┬────────┘                     │
                      │         │                              │
                      │         │ go-redis client              │
                      │         ▼                              │
                      │   ┌────────────────────────┐           │
                      │   │   *redis.Client        │           │
                      │   └─────────┬──────────────┘           │
                      └─────────────│──────────────────────────┘
                                    │ TCP
                                    ▼
                ┌──────────────────────────────────────┐
                │  Redis 6+  (real)   OR  miniredis    │
                │      (--embedded-redis)              │
                └──────────────────────────────────────┘
                                    ▲
                                    │ same APP_DB layout
                ┌───────────────────┴───────────────────┐
                │  SONiC DASH orchagent (on real DPU)   │
                │  reads keys, applies via SAI          │
                └───────────────────────────────────────┘
```

---

## 3. SONiC APP_DB wire format

The adapter's storage layout is **byte-compatible** with what SONiC DASH
orchagent expects in APP_DB.

### 3.1 Key format

```
DASH_<KIND>_TABLE:<joined-key>
```

- `<KIND>` is `kinds.Info.TableName()` =
  `"DASH_" + strings.ToUpper(info.Name) + "_TABLE"`.
- `<joined-key>` is the key parts joined with `:` — same order and meaning
  as the upstream `<Kind>Key` proto.

| Example | Resolved key |
|---|---|
| `vnet` with key `[vnet-prod]` | `DASH_VNET_TABLE:vnet-prod` |
| `vnet_mapping` with key `[vnet-prod, 10.0.0.10]` | `DASH_VNET_MAPPING_TABLE:vnet-prod:10.0.0.10` |
| `acl_rule` with key `[acl-prod-in, 100]` | `DASH_ACL_RULE_TABLE:acl-prod-in:100` |
| `route_rule` with key `[eni-001, 1001, 0.0.0.0/0, 100]` | `DASH_ROUTE_RULE_TABLE:eni-001:1001:0.0.0.0/0:100` |
| `ha_scope_config` with key `[vdpu-1, eni-001]` | `DASH_HA_SCOPE_CONFIG_TABLE:vdpu-1:eni-001` |

### 3.2 Value format (Redis HASH)

| Field | Type | Content |
|---|---|---|
| `pb` | binary string | `proto.Marshal(payload)` — the upstream proto message serialized to wire format. |
| `meta` | JSON string | `{"created_ts_ns": <int>, "updated_ts_ns": <int>}` for adapter bookkeeping. |

`pb` is what SONiC orchagent reads — the field name and the binary payload
match exactly.

`meta` is **adapter-private** (orchagent ignores unknown fields). Keeping
it as a separate HASH field rather than mixed into `pb` means adapter
bookkeeping never alters the bytes orchagent sees.

### 3.3 Counter format

| Key | Field | Content |
|---|---|---|
| `DASH_COUNTERS:<joined-key>` | `packets_in`, `packets_out`, `bytes_in`, `bytes_out`, `drops` | int (string-encoded) |

Adapter only READS this key (via `HGETALL`). A separate collector daemon
writes the actual counter values from the DPU.

### 3.4 Pub/Sub channel

| Channel | Payload (JSON) |
|---|---|
| `dashapi.events` | `{"type": "CREATED|UPDATED|DELETED", "kind": "<name>", "key": [...], "pb": "<base64>", "tx_id": "...", "ts_ns": <int>}` |

The Pub/Sub is **adapter-internal** — it is not part of the canonical
SONiC APP_DB layout. Existing SONiC components do not subscribe to this
channel; only `dash-redis-adapter` does (to drive `DashApi.Subscribe`).

---

## 4. Module layout

```
dash-redis-adapter/
├── go.mod                   -- requires go-redis/v9, miniredis/v2, grpc, kinds, gen/go
├── Dockerfile               -- distroless multi-stage build
├── cmd/dash-redis-adapter/main.go    -- binary entrypoint (flags, dial, serve)
└── internal/adapter/
    ├── server.go            -- the DashApi service over Redis
    └── server_test.go       -- bufconn + miniredis integration tests
```

There is no `internal/...` package other than `adapter`. All
table-name / key-encoding / payload-pack/unpack logic comes from the
shared `dashapi-runtime/kinds`.

---

## 5. Per-RPC implementation

Every handler delegates to a small, predictable Redis command sequence.
Below: pseudocode, errors, and side effects per RPC.

### 5.1 `Apply(ApplyRequest) → Ack`

```go
func (s *Server) Apply(ctx, req) (*Ack, error) {
    info  := kinds.Lookup(req.Object.Kind)
    valid := len(req.Object.Key) == len(info.KeyParts) && all parts non-empty
    if !valid {
        return ack("", err), nil
    }
    payload := kinds.PayloadOf(req.Object)
    raw     := proto.Marshal(payload)              // binary wire bytes

    rkey    := info.TableName() + ":" + strings.Join(req.Object.Key, ":")

    existed := rdb.Exists(ctx, rkey) == 1
    meta    := loadMetaOrCreate(rkey, existed)     // preserve created_ts_ns
    metaJSON, _ := json.Marshal(meta)

    rdb.HSet(ctx, rkey, "pb", raw, "meta", metaJSON)

    evType := if existed { "UPDATED" } else { "CREATED" }
    rdb.Publish(ctx, "dashapi.events", json{
        type: evType, kind: info.Name, key: req.Object.Key,
        pb:   raw,  tx_id: s.txID(), ts_ns: now(),
    })

    return Ack{accepted: true, ...}, nil
}
```

| Aspect | Spec |
|---|---|
| Validation | Same as `dash-sim`: kind known, key parts count match, no empty parts. |
| CREATED vs UPDATED | Decided by `EXISTS` before the HSET. |
| Atomicity | HSET + PUBLISH are **separate commands**. Today not transactional; in practice fine because PUBLISH is non-blocking and HSET is the durable write. |
| Errors | All validation/Redis errors come back in `Ack.accepted=false` with `error` set. RPC error itself is `nil` so the client always receives a structured Ack. |
| Fault op name | None today (no fault injector in adapter; on the roadmap to mirror `dash-sim`). |

### 5.2 `Delete(DeleteRequest) → Ack`

```go
func (s *Server) Delete(ctx, req) (*Ack, error) {
    info := kinds.Lookup(req.Kind); ensure key parts
    rkey := info.TableName() + ":" + strings.Join(req.Key, ":")
    raw  := rdb.HGet(ctx, rkey, "pb").Bytes()   // for the wire event
    n    := rdb.Del(ctx, rkey)
    if n == 0 { return Ack{error: "not found"}, nil }
    rdb.Publish("dashapi.events", json{type: "DELETED", kind, key, pb: raw, ...})
    return Ack{accepted: true, ...}, nil
}
```

### 5.3 `Get(GetRequest) → GetResponse`

```go
func (s *Server) Get(ctx, req) (*GetResponse, error) {
    info := kinds.Lookup(req.Kind)
    rkey := info.TableName() + ":" + strings.Join(req.Key, ":")
    raw  := rdb.HGet(ctx, rkey, "pb").Bytes()
    if redis.Nil { return nil, status.Error(NotFound, "not found") }
    msg  := info.NewZero(); proto.Unmarshal(raw, msg)
    return &GetResponse{ Object: kinds.WrapObject(info.Kind, req.Key, msg) }, nil
}
```

### 5.4 `List(ListRequest) → stream ListItem`

```go
func (s *Server) List(req, stream) error {
    info    := kinds.Lookup(req.Kind)
    pattern := info.TableName() + ":*"
    if req.KeyPrefix != "" { pattern = info.TableName() + ":" + req.KeyPrefix + "*" }

    var cursor uint64 = 0
    sent := 0
    for {
        keys, cursor = rdb.Scan(ctx, cursor, pattern, 100)
        sortStrings(keys)
        for _, rkey := range keys {
            raw := rdb.HGet(ctx, rkey, "pb").Bytes()
            msg := info.NewZero(); proto.Unmarshal(raw, msg)
            keyParts := strings.SplitN(rkey[len(info.TableName())+1:], ":", len(info.KeyParts))
            obj := kinds.WrapObject(info.Kind, keyParts, msg)
            stream.Send(&ListItem{Object: obj})
            sent++
            if req.Limit > 0 && sent >= req.Limit { return nil }
        }
        if cursor == 0 { return nil }
    }
}
```

| Aspect | Spec |
|---|---|
| Filter | `key_prefix` becomes a `MATCH` glob suffix; cheaper than client-side filtering. |
| Order | Sorted within each SCAN batch for stable output. Cross-batch ordering is not strict (SCAN may return overlaps), but in practice no duplicates because SCAN cursors are stable for steady-state datasets. |
| Limit | Hard cap when `req.Limit > 0`. |

### 5.5 `Subscribe(SubscribeRequest) → stream Event`

See [§6](#6-subscribe-via-redis-pubsub).

### 5.6 `GetCounters(CountersRequest) → CountersResponse`

```go
func (s *Server) GetCounters(ctx, req) (*CountersResponse, error) {
    rkey := "DASH_COUNTERS:" + strings.Join(req.Key, ":")
    raw  := rdb.HGetAll(ctx, rkey)        // may be empty
    out  := defaultZeroCounters()
    for k, v := range raw {
        out[k] = parseInt64(v)
    }
    return &CountersResponse{Counters: out, ServerTsNs: now()}, nil
}
```

### 5.7 `SimulatePacket(SimulatePacketRequest)`

```go
return nil, status.Error(codes.Unimplemented,
    "SimulatePacket is not supported by dash-redis-adapter (Redis APP_DB has no behavioural pipeline); use dash-sim instead")
```

---

## 6. Subscribe via Redis Pub/Sub

Two-stage flow per subscriber connection:

```
┌──────────────────────────────────────────────────────────────────────┐
│ if req.snapshot_first:                                               │
│     for each ObjectKind k (filtered):                                │
│         SCAN DASH_<K>_TABLE:* in batches of 100                       │
│         for each key:                                                 │
│             HGET pb                                                   │
│             proto.Unmarshal -> typed payload                          │
│             stream.Send(Event{type:SNAPSHOT, object:Wrap(...)})       │
│                                                                       │
│ pubsub = rdb.Subscribe(ctx, "dashapi.events")                        │
│ for msg in pubsub.Channel():                                          │
│     wireEvent = json.Unmarshal(msg.Payload)                           │
│     if wireEvent.kind not in req.kinds: skip                          │
│     payload  = info.NewZero(); proto.Unmarshal(wireEvent.pb, payload) │
│     obj      = kinds.WrapObject(info.Kind, wireEvent.key, payload)    │
│     evType   = map(wireEvent.type) -> CREATED/UPDATED/DELETED         │
│     stream.Send(Event{type: evType, object: obj, ...})                │
└──────────────────────────────────────────────────────────────────────┘
```

| Property | Behaviour |
|---|---|
| Backpressure | go-redis pub/sub channel buffer is 128 (library default); slow consumers cause `pubsub.Channel()` to drop. Adapter does not currently surface a `dropped_events` counter; see roadmap. |
| Ordering | Per-channel ordering is preserved by Redis pub/sub. |
| Live-only mode | Skip snapshot loop. |
| Filtering | Applied at the receive side (matches the client's `kinds` field). |

> Why not Redis keyspace events (`notify-keyspace-events Khg`)? Those need
> server-side configuration the operator may not control. Using an
> internal channel keeps the adapter self-contained.

---

## 7. Connection lifecycle

```
main()
  flag.Parse()
  if --embedded-redis:
      mr = miniredis.Run()
      --redis = mr.Addr()
  rdb = redis.NewClient({Addr, DB, Password})
  rdb.Ping(ctx, 5s)             // fail fast on bad config
  svc = adapter.New(rdb)
  grpcSrv = grpc.NewServer()
  dashapi.RegisterDashApiServer(grpcSrv, svc)
  reflection.Register(grpcSrv)
  listen and Serve
  <-signal
  grpcSrv.GracefulStop()
  rdb.Close()
```

---

## 8. Concurrency model

| Component | Lock | Notes |
|---|---|---|
| `*Server` | `atomic.Uint64` for `nextTx` | No mutex; all state is in Redis. |
| Redis client | thread-safe by design (go-redis) | One client serves all RPCs. |
| Pub/Sub channel | one per subscriber call | Each `Subscribe` RPC allocates its own pubsub. |
| gRPC server | tonic / grpc-go scheduler | Standard. |

Net effect: **the adapter has essentially no in-process state** — it is a
stateless translation layer between gRPC and Redis. That makes it
trivially replicable and easy to run multiple instances against the same
Redis.

---

## 9. Per upstream proto — table mapping

| ObjectKind | Table key | Sample full key |
|---|---|---|
| `appliance` | `DASH_APPLIANCE_TABLE:<appliance_id>` | `DASH_APPLIANCE_TABLE:appliance-01` |
| `vnet` | `DASH_VNET_TABLE:<vnet_name>` | `DASH_VNET_TABLE:vnet-prod` |
| `eni` | `DASH_ENI_TABLE:<eni>` | `DASH_ENI_TABLE:eni-001` |
| `eni_route` | `DASH_ENI_ROUTE_TABLE:<eni>` | `DASH_ENI_ROUTE_TABLE:eni-001` |
| `acl_group` | `DASH_ACL_GROUP_TABLE:<group_id>` | `DASH_ACL_GROUP_TABLE:acl-prod-in` |
| `acl_rule` | `DASH_ACL_RULE_TABLE:<group_id>:<rule_num>` | `DASH_ACL_RULE_TABLE:acl-prod-in:100` |
| `acl_in` | `DASH_ACL_IN_TABLE:<eni>:<stage>` | `DASH_ACL_IN_TABLE:eni-001:1` |
| `acl_out` | `DASH_ACL_OUT_TABLE:<eni>:<stage>` | `DASH_ACL_OUT_TABLE:eni-001:1` |
| `route` | `DASH_ROUTE_TABLE:<group_id>:<prefix>` | `DASH_ROUTE_TABLE:rg-prod:10.1.0.0/16` |
| `route_group` | `DASH_ROUTE_GROUP_TABLE:<group_id>` | `DASH_ROUTE_GROUP_TABLE:rg-prod` |
| `route_rule` | `DASH_ROUTE_RULE_TABLE:<eni>:<vni>:<prefix_or_tag>:<priority>` | `DASH_ROUTE_RULE_TABLE:eni-001:1001:0.0.0.0/0:100` |
| `route_type` | `DASH_ROUTE_TYPE_TABLE:<routing_type>` | `DASH_ROUTE_TYPE_TABLE:vnet` |
| `routing_appliance` | `DASH_ROUTING_APPLIANCE_TABLE:<appliance_id>` | `DASH_ROUTING_APPLIANCE_TABLE:appl-1` |
| `prefix_tag` | `DASH_PREFIX_TAG_TABLE:<tag_name>` | `DASH_PREFIX_TAG_TABLE:trusted-net` |
| `vnet_mapping` | `DASH_VNET_MAPPING_TABLE:<vnet>:<ip_address>` | `DASH_VNET_MAPPING_TABLE:vnet-prod:10.0.0.10` |
| `tunnel` | `DASH_TUNNEL_TABLE:<tunnel_name>` | `DASH_TUNNEL_TABLE:t-1` |
| `pa_validation` | `DASH_PA_VALIDATION_TABLE:<vni>` | `DASH_PA_VALIDATION_TABLE:1001` |
| `qos` | `DASH_QOS_TABLE:<qos_name>` | `DASH_QOS_TABLE:q-1` |
| `meter` | `DASH_METER_TABLE:<eni>:<metering_class_id>` | `DASH_METER_TABLE:eni-001:42` |
| `meter_policy` | `DASH_METER_POLICY_TABLE:<meter_policy_id>` | `DASH_METER_POLICY_TABLE:mp-1` |
| `meter_rule` | `DASH_METER_RULE_TABLE:<meter_policy_id>:<rule_num>` | `DASH_METER_RULE_TABLE:mp-1:10` |
| `outbound_port_map` | `DASH_OUTBOUND_PORT_MAP_TABLE:<map_id>` | `DASH_OUTBOUND_PORT_MAP_TABLE:pm-1` |
| `outbound_port_map_range` | `DASH_OUTBOUND_PORT_MAP_RANGE_TABLE:<map_id>:<start>:<end>` | `DASH_OUTBOUND_PORT_MAP_RANGE_TABLE:pm-1:1000:2000` |
| `ha_scope` | `DASH_HA_SCOPE_TABLE:<ha_scope_id>` | `DASH_HA_SCOPE_TABLE:s-1` |
| `ha_scope_config` | `DASH_HA_SCOPE_CONFIG_TABLE:<vdpu_id>:<ha_scope_id>` | `DASH_HA_SCOPE_CONFIG_TABLE:vdpu-1:eni-001` |
| `ha_scope_state` | `DASH_HA_SCOPE_STATE_TABLE:<ha_scope_id>` | `DASH_HA_SCOPE_STATE_TABLE:eni-001` |
| `ha_set` | `DASH_HA_SET_TABLE:<ha_set_id>` | `DASH_HA_SET_TABLE:hs-1` |
| `ha_set_config` | `DASH_HA_SET_CONFIG_TABLE:<ha_set_id>` | `DASH_HA_SET_CONFIG_TABLE:hs-1` |
| `ha_set_state` | `DASH_HA_SET_STATE_TABLE:<ha_set_id>` | `DASH_HA_SET_STATE_TABLE:hs-1` |

> **Note**: SONiC orchagent may also use slightly different prefixes in
> some HA tables (`DASH_HA_SCOPE_CONFIG_TABLE` vs. `DASH_HA_SCOPE_TABLE`
> with separate fields). The adapter's mapping reflects what the
> registry produces from `info.Name`; aligning per-table edge cases with
> the live orchagent is tracked in
> [docs/roadmap.md](../../docs/roadmap.md).

---

## 10. Rust pseudocode parity

For a future `impl-rust/crates/dash-redis-adapter`:

```rust
use redis::{AsyncCommands, aio::MultiplexedConnection};

async fn apply(&self, req: ApplyRequest) -> Result<Ack, Status> {
    let obj  = req.object.ok_or_else(|| Status::invalid_argument("nil object"))?;
    let info = kinds::lookup(obj.kind)?;
    if obj.key.len() != info.key_parts.len() {
        return Ok(ack(format!("kind {} expects {} key parts", info.name, info.key_parts.len()), false));
    }
    let payload = kinds::payload_of(&obj)?;
    let raw     = payload.encode_to_vec();
    let rkey    = format!("{}:{}", info.table_name(), obj.key.join(":"));
    let existed: bool = self.rdb.clone().exists(&rkey).await?;

    let meta = if existed {
        let cur: Option<String> = self.rdb.clone().hget(&rkey, "meta").await?;
        let mut m: Meta = cur.as_deref().and_then(|s| serde_json::from_str(s).ok()).unwrap_or_default();
        m.updated_ts_ns = now_ns(); m
    } else {
        Meta { created_ts_ns: now_ns(), updated_ts_ns: now_ns() }
    };

    let _: () = self.rdb.clone()
        .hset_multiple(&rkey, &[("pb", &raw[..]), ("meta", serde_json::to_string(&meta)?.as_bytes())])
        .await?;

    let ev = WireEvent {
        r#type: if existed { "UPDATED" } else { "CREATED" }.to_string(),
        kind: info.name.clone(), key: obj.key.clone(),
        pb: raw, tx_id: self.next_tx(), ts_ns: now_ns(),
    };
    let _: () = self.rdb.clone().publish("dashapi.events", serde_json::to_string(&ev)?).await?;

    Ok(ack_ok(ev.tx_id))
}
```

```rust
async fn subscribe(&self, req: Request<SubscribeRequest>) -> Result<Response<Self::SubscribeStream>, Status> {
    let req = req.into_inner();
    let (tx, rx) = mpsc::channel(128);
    let rdb = self.rdb.clone();

    tokio::spawn(async move {
        if req.snapshot_first {
            emit_snapshot(&rdb, &req.kinds, &tx).await.ok();
        }
        let mut pubsub = rdb.into_pubsub();
        if pubsub.subscribe("dashapi.events").await.is_ok() {
            let mut stream = pubsub.on_message();
            while let Some(msg) = stream.next().await {
                if let Ok(payload) = msg.get_payload::<String>() {
                    if let Some(ev) = decode_wire_event(&payload, &req.kinds) {
                        let _ = tx.send(Ok(ev)).await;
                    }
                }
            }
        }
    });

    Ok(Response::new(Box::pin(tokio_stream::wrappers::ReceiverStream::new(rx))))
}
```

---

## 11. Failure modes

| Failure | Where surfaced | Recovery |
|---|---|---|
| Redis down on dial | `main.go` Ping → `Fatalf` | Operator restarts Redis or fixes `--redis`. |
| Redis dies after start | next command returns error | go-redis transparently reconnects; the failing RPC returns `Internal`. |
| Wrong kind | `Ack.accepted=false, error=...` | Caller fixes input. |
| Object not found on Get | gRPC `NotFound` | Caller fixes input. |
| Object not found on Delete | `Ack.error="not found"` | Caller fixes input. |
| HSET partial failure | Redis returns error → `Ack.error=...` | go-redis retries the next command. |
| Pub/Sub consumer slow | go-redis drops messages internally | Re-subscribe with `snapshot_first=true`. |
| SimulatePacket called | gRPC `Unimplemented` | Use `dash-sim` instead. |

---

## 12. Operational notes

| Topic | Notes |
|---|---|
| Persistence | Set `appendonly yes` and tune `save` in your Redis config if you want durability. Embedded `miniredis` is **in-memory only**. |
| Multi-instance | Multiple adapters against the same Redis are safe — every adapter is stateless. The last write wins. |
| Memory | Each object is one HASH with two fields. Average size is hundreds of bytes. 100k objects ≈ tens of MB in Redis. |
| Cluster mode | Today the adapter uses a single-node `redis.Client`. Migrating to `redis.ClusterClient` is a roadmap item; key-space sharding by table prefix is straightforward because every operation hits a single key. |
| TLS | Use `redis.NewClient` with TLS options; planned to be exposed as `--redis-tls`. |
| Auth | `--redis-password` today. Future: ACL user / AAD. |
| Observability | Roadmap: Prometheus metrics, structured logging. |

---

## 13. Compatibility checklist against SONiC orchagent

These items must all hold for a SONiC DASH orchagent pointed at the same
Redis to consume `dash-redis-adapter`'s writes correctly:

- [x] Key prefix matches `DASH_<KIND>_TABLE`.
- [x] Key suffix uses `:` separators in upstream `<Kind>Key` field order.
- [x] HASH field `pb` contains `proto.Marshal(<upstream message>)`.
- [x] Adding our own `meta` field does not break orchagent (unknown HASH
      fields are ignored).
- [x] DELETE corresponds to `redis.DEL` on the same key.
- [ ] **HA tables** — verify exact table name match for ha_scope_config /
      ha_set_config (depends on orchagent build; see roadmap).
- [ ] **Channels** — orchagent listens to keyspace events or a different
      channel; today we use our private `dashapi.events`. For real
      hardware integration, **enable keyspace notifications** in Redis
      and consume them in the adapter, OR have orchagent dual-subscribe.
- [ ] **Field ordering inside proto** — already guaranteed by protobuf
      determinism; nothing to do.

---

## 14. Test surface

[`server_test.go`](../../src/impl-go/dash-redis-adapter/internal/adapter/server_test.go)
runs against:
- in-process `miniredis` instance (no real Redis required)
- in-process gRPC server (`google.golang.org/grpc/test/bufconn`)
- the **standard** `dashapi.DashApiClient` (same client used in
  `dash-sim-client`)

Five tests cover Apply/Get/Delete round-trip, ordered List, snapshot +
live Subscribe, bytes round-trip, and `SimulatePacket → Unimplemented`.

Run:
```bash
go test ./dash-redis-adapter/...
```
