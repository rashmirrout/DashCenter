# dash-redis-adapter — SONiC-compatible Redis backend

| Field | Value |
|---|---|
| Source | [`src/impl-go/dash-redis-adapter/`](../../../src/impl-go/dash-redis-adapter/) |
| Binary | `dash-redis-adapter` (or `.exe`) |
| Default port | `:52051` (gRPC) |
| Status | stable |

---

## 1. Role

Implements **the same `dashapi.v1.DashApi` gRPC service as `dash-sim`**, but
backed by **Redis** in the exact APP_DB layout SONiC's DASH orchagent reads.
This means:

- The same `dash-sim-client` binary works against this server unchanged.
- The Redis state produced by `apply` calls is **byte-identical** to what
  the upstream `swssconfig`-style DASH config tooling produces.
- A real SONiC DPU pointed at the same Redis would see and act on the
  written objects.

Use it for:
- validating that your config produces the correct SONiC APP_DB layout
- integration testing against real Redis or the embedded `miniredis`
- the **path to real-hardware control** — drop in a SONiC DPU pointed at
  the same Redis and your CLI workflow works end-to-end

Does **not** implement `SimulatePacket` — Redis APP_DB has no behavioural
pipeline state. Returns `codes.Unimplemented` with a hint to use `dash-sim`.

---

## 2. Layout

```
dash-redis-adapter/
├── go.mod
├── Dockerfile
├── cmd/dash-redis-adapter/main.go     -- binary entrypoint
└── internal/adapter/
    ├── server.go                      -- DashApi service over Redis
    └── server_test.go                 -- 5 integration tests via miniredis
```

---

## 3. Wire format — the SONiC APP_DB layout

For every object:

```
Redis key:    DASH_<KIND>_TABLE:<joined-key>
              e.g. DASH_VNET_TABLE:vnet-prod
                   DASH_VNET_MAPPING_TABLE:vnet-prod:10.0.0.10
                   DASH_ACL_RULE_TABLE:acl-prod-in:100

Redis value:  HASH with two fields
                pb    -> binary proto.Marshal(payload)
                meta  -> JSON {"created_ts_ns": ..., "updated_ts_ns": ...}
```

`<KIND>` comes from `kinds.Info.TableName()` —
`strings.ToUpper(info.Name)`.

You can verify a write directly with `redis-cli`:

```bash
$ docker exec -it dc-redis redis-cli
127.0.0.1:6379> HGETALL DASH_VNET_TABLE:vnet-prod
1) "pb"
2) "\x08\xe9\x07"          # binary-encoded Vnet{vni:1001}
3) "meta"
4) "{\"created_ts_ns\":1780520339,\"updated_ts_ns\":1780520339}"
```

A SONiC DASH orchagent reading this key sees the same `Vnet` message.

---

## 4. Subscribe — Redis Pub/Sub

`DashApi.Subscribe` is delivered via Redis **Pub/Sub** on channel
`dashapi.events`. Every `Apply`/`Delete` publishes a JSON envelope:

```json
{
  "type": "CREATED|UPDATED|DELETED",
  "kind": "vnet",
  "key":  ["vnet-prod"],
  "pb":   "<base64 of proto.Marshal(payload)>",
  "tx_id": "tx-...",
  "ts_ns": 1780520339000000000
}
```

The adapter's `Subscribe` handler:
1. Optionally streams `EVENT_TYPE_SNAPSHOT` for every existing key (SCAN).
2. Subscribes to `dashapi.events`.
3. Decodes each message, re-wraps it as an `*dashapi.Event`, and forwards
   to the gRPC stream.

This avoids requiring `notify-keyspace-events` to be enabled on the Redis
server (which would mean operator config).

---

## 5. Implementation walk-through

### Apply
```
1. validate kind exists, key parts count, no empty key parts
2. proto.Marshal(payload) -> binary bytes
3. HEXISTS DASH_<KIND>_TABLE:<key> to detect CREATED vs UPDATED
4. HSET DASH_<KIND>_TABLE:<key> pb <bytes> meta <json>
5. PUBLISH dashapi.events <wireEvent json>
```

### Get
```
1. HGET DASH_<KIND>_TABLE:<key> pb -> bytes
2. proto.Unmarshal into the typed payload (via kinds.Info.NewZero())
3. WrapObject and return
```

### List
```
1. SCAN cursor MATCH "DASH_<KIND>_TABLE:*"
2. For each key: HGET pb, proto.Unmarshal, WrapObject, send on stream
3. Sort within each batch for stable output
```

### Delete
```
1. HGET DASH_<KIND>_TABLE:<key> pb  (for the wire event payload)
2. DEL DASH_<KIND>_TABLE:<key>
3. PUBLISH dashapi.events {type:DELETED, ...}
```

### GetCounters
Reads `HGETALL DASH_COUNTERS:<joined-key>`. Adapter does not synthesize
counters — they're written by an external collector (e.g. a SONiC counter
collector daemon).

### SimulatePacket
Returns `status.Error(codes.Unimplemented, ...)`.

---

## 6. Embedded mode (`--embedded-redis`)

Start an in-process `github.com/alicebob/miniredis/v2` instance and dial
that. Same wire format, zero external dependencies — ideal for demos and
quick tests:

```bash
./bin/dash-redis-adapter --grpc-listen :52051 --embedded-redis
```

The embedded miniredis dies with the adapter process — state is not
persistent.

---

## 7. Tests

[`server_test.go`](../../../src/impl-go/dash-redis-adapter/internal/adapter/server_test.go)
spins up the adapter against miniredis + an in-process **bufconn** gRPC
server, then drives it through a normal `dashapi.DashApiClient`:

1. `TestApply_Get_Delete` — full lifecycle (CREATED → UPDATED → DELETED)
2. `TestList_OrderedByKey` — stable ordering
3. `TestSubscribe_SnapshotAndLive` — snapshot first, then live CREATED
4. `TestEni_RoundTrip` — bytes (mac_address) survive proto.Marshal
5. `TestSimulatePacket_Unimplemented` — explicit Unimplemented return

Run:
```bash
go test ./dash-redis-adapter/internal/adapter/...
```

---

## 8. Flags

| Flag | Default | Purpose |
|---|---|---|
| `--grpc-listen` | `:52051` | DashApi gRPC bind address |
| `--redis` | `localhost:6379` | Redis address (host:port) |
| `--redis-db` | `0` | Redis logical DB |
| `--redis-password` | (empty) | Optional password |
| `--embedded-redis` | `false` | Start an in-process miniredis; overrides `--redis` |

---

## 9. Operational notes

- **Persistence**: enabled only if your real Redis has persistence enabled
  (AOF/RDB). The embedded miniredis is in-memory only.
- **Pub/Sub fan-out**: every subscribing gRPC stream creates its own
  `pubsub.Subscribe`; the Redis server handles the fan-out.
- **No transactions**: each `Apply` is a single HSET + PUBLISH. To get
  multi-object atomicity in future, wrap into a Redis transaction (MULTI/EXEC).
- **Counters**: writing to `DASH_COUNTERS:<key>` is out of scope for this
  adapter; it just reads.
