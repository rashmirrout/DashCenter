# 06 — Test

How to run every test in the project and what each one proves. There are
three layers:

1. **Per-module Go unit tests** (run via `go test`).
2. **Pipeline conformance tests** (`dash-sim/internal/sim/pipeline`).
3. **Adapter integration tests** (`dash-redis-adapter` with an in-process
   miniredis).

There is no separate "test runner" — Go's built-in `go test` is the entire
toolchain.

> **Prerequisite**: [04 — Build](04-build.md) succeeded.

---

## 1. Run everything

```powershell
# Windows
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go
go test .\dash-sim\... .\dash-redis-adapter\...
```
```bash
# Linux
cd ~/work/DashCenter/src/impl-go
go test ./dash-sim/... ./dash-redis-adapter/...
```

Expected output:
```
ok      github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/pipeline    1.923s
ok      github.com/rashmirrout/DashCenter/src/impl-go/dash-redis-adapter/internal/adapter        2.382s
```

The `[no test files]` notices for packages without tests are normal.

---

## 2. Test each module individually

### 2.1 `dash-sim` pipeline

The behavioural DASH packet pipeline. **8 test cases** in
[`pipeline_test.go`](../../src/impl-go/dash-sim/internal/sim/pipeline/pipeline_test.go):

| Test | What it proves |
|---|---|
| `TestOutbound_EncapViaVnetMapping` | Full outbound flow → ACL_OUT permit → route LPM match `10.1.0.0/16` → `vnet_mapping` lookup → ENCAP with correct underlay IP. |
| `TestOutbound_RouteDrop` | A `ROUTING_TYPE_DROP` route → DROP decision. |
| `TestOutbound_RouteDirect` | A `ROUTING_TYPE_DIRECT` route → FORWARD, no encap. |
| `TestOutbound_AclDeny` | A higher-priority DENY rule short-circuits the pipeline. Reports stage + priority. |
| `TestOutbound_DisabledEni` | `admin_state=STATE_DISABLED` → DROP with a clear reason. |
| `TestOutbound_NoRouteMatch` | Destination outside every route prefix → DROP. |
| `TestInbound_DeliverViaRouteRuleAndAclPermit` | `route_rule` priority match → DECAP → ACL_IN permit → FORWARD. |
| `TestInbound_NoRouteRule_Drops` | No matching `route_rule` → DROP. |
| `TestInbound_ResolveByMac` | ENI resolved by destination MAC when `eni` not supplied. |

Run:
```bash
go test ./dash-sim/internal/sim/pipeline/...
```

With verbose output:
```bash
go test -v ./dash-sim/internal/sim/pipeline/...
```

### 2.2 `dash-redis-adapter`

End-to-end gRPC tests through an in-process **miniredis** instance.
**5 test cases** in
[`server_test.go`](../../src/impl-go/dash-redis-adapter/internal/adapter/server_test.go):

| Test | What it proves |
|---|---|
| `TestApply_Get_Delete` | Round-trip an object; second `Apply` is an UPDATE; `Delete` + `Get` → `NotFound`. |
| `TestList_OrderedByKey` | `List` returns results in sorted order regardless of insertion sequence. |
| `TestSubscribe_SnapshotAndLive` | Subscribe sends a SNAPSHOT per existing object, then live CREATED via Redis Pub/Sub. |
| `TestEni_RoundTrip` | `bytes` (mac_address) survives proto.Marshal → Redis HSET → proto.Unmarshal. |
| `TestSimulatePacket_Unimplemented` | Adapter explicitly returns `codes.Unimplemented` with a hint to use `dash-sim`. |

Run:
```bash
go test ./dash-redis-adapter/internal/adapter/...
```

### 2.3 With race detector

```bash
go test -race ./dash-sim/... ./dash-redis-adapter/...
```

### 2.4 With coverage

```bash
go test -cover ./dash-sim/... ./dash-redis-adapter/...
# ok ... 1.923s  coverage: 71.4% of statements
```

Generate an HTML report:
```bash
go test -coverprofile=cover.out ./dash-sim/internal/sim/pipeline/...
go tool cover -html=cover.out -o cover.html
```

### 2.5 Just one test

```bash
go test -run TestOutbound_EncapViaVnetMapping -v ./dash-sim/internal/sim/pipeline/...
```

---

## 3. End-to-end manual smoke tests

Beyond the automated suites, here's the **happy path** to manually validate
a fresh build:

### 3.1 dash-sim happy path

Terminal A:
```bash
./bin/dash-sim --grpc-listen :50051 --admin-listen :8080
```

Terminal B:
```bash
c=./bin/dash-sim-client
$c --target localhost:50051 ping
$c --target localhost:50051 kinds -o table | head
$c --target localhost:50051 apply --kind vnet --key vnet-prod --value '{"vni":1001}'
$c --target localhost:50051 list  --kind vnet -o table
$c --target localhost:50051 delete --kind vnet --key vnet-prod
```

Expected: `ping` succeeds, `kinds` shows 29 rows, `apply` returns `OK`,
`list` shows the new row, `delete` returns `OK`.

### 3.2 dash-redis-adapter happy path

Terminal A:
```bash
./bin/dash-redis-adapter --grpc-listen :52051 --embedded-redis
```

Terminal B (same commands, different port):
```bash
c=./bin/dash-sim-client
$c --target localhost:52051 ping
$c --target localhost:52051 apply --kind vnet --key vnet-prod --value '{"vni":1001}'
$c --target localhost:52051 list  --kind vnet -o table
```

### 3.3 Cross-backend parity

Apply the same object to both backends — outputs should be byte-identical:

```bash
$c --target localhost:50051 apply --kind vnet --key vnet-x --value '{"vni":42}'
$c --target localhost:52051 apply --kind vnet --key vnet-x --value '{"vni":42}'

$c --target localhost:50051 get --kind vnet --key vnet-x > sim.json
$c --target localhost:52051 get --kind vnet --key vnet-x > adapter.json
diff sim.json adapter.json     # should be empty
```

---

## 4. Lint

```bash
cd src/impl-go
# requires github.com/golangci/golangci-lint installed separately
golangci-lint run --config .golangci.yml ./...
```

---

## 5. Continuous Integration target

For CI, the canonical "everything green" command is:

```bash
cd src/impl-go
go build .\dash-sim\... .\dash-sim-client\... .\dash-redis-adapter\... \
         .\dashapi-runtime\... .\gen\go\... .\dashd\... .\dashctl\...
go test  .\dash-sim\... .\dash-redis-adapter\...
```

Both should exit `0`.

---

## Where to go next

- → [07 — dash-sim-client](07-dash-sim-client.md) — the full CLI guide.
