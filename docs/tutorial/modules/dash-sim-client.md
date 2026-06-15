# dash-sim-client — the operator CLI

| Field | Value |
|---|---|
| Source | [`src/impl-go/dash-sim-client/`](../../../src/impl-go/dash-sim-client/) |
| Binary | `dash-sim-client` (or `.exe`) |
| Status | stable |
| External deps | `google.golang.org/grpc`, `github.com/spf13/cobra`, `gopkg.in/yaml.v3`, `google.golang.org/protobuf` |

> This page documents the **module internals** (SDK layout, command tree,
> render package). For the **end-user CLI reference** (every flag, every
> command, sample input/output) see
> [docs/CLI_GUIDE.md](../../CLI_GUIDE.md) and
> [07 — dash-sim-client](../07-dash-sim-client.md).

---

## 1. Role

A **transport-only** Cobra-based CLI that speaks `dashapi.v1.DashApi`. It
talks to:
- `dash-sim` (in-memory simulator)
- `dash-redis-adapter` (Redis APP_DB backend)
- any future DashApi server (real DPU agent)

It is **completely unaware** of how the server stores state. The only
imports it has are:
- the generated proto stubs (`gen/go/dashapi/v1` + `gen/go/dash/...`)
- the shared kinds registry (`dashapi-runtime/kinds`)
- standard library + `grpc` + `cobra` + `yaml`

Critically, it does **not** import any `dash-sim/internal/*` package.

---

## 2. Layout

```
dash-sim-client/
├── go.mod
├── README.md
├── Dockerfile
├── cmd/dash-sim-client/main.go     -- 8-line entry: calls cmd.Execute()
├── internal/
│   ├── cmd/                        -- Cobra subcommand tree
│   │   ├── root.go                 -- root, persistent flags (--target, -o, ...)
│   │   ├── apply.go                -- apply (inline OR -f file)
│   │   ├── crud.go                 -- get, delete, list, counters, ping
│   │   ├── subscribe.go            -- subscribe + kinds discovery
│   │   ├── simulate.go             -- simulate (SimulatePacket RPC)
│   │   ├── codec.go                -- json/yaml dispatch helper
│   │   └── helpers.go              -- key parsing, ack printing, value coercion
│   └── render/                     -- output formatting
│       └── render.go               -- Object / Objects / CountersMap in json|yaml|table
└── pkg/
    └── client/                     -- thin SDK
        └── client.go               -- Dial(...), Apply, Get, Delete, List, Subscribe, GetCounters
```

`pkg/client` is **exported** (other Go projects can `import` and embed it).
`internal/cmd` and `internal/render` are private to the CLI.

---

## 3. The SDK (`pkg/client`)

A single `Client` value wrapping `*grpc.ClientConn` and
`dashapi.DashApiClient`. Methods are 1:1 with the gRPC service.

```go
type Client struct { /* unexported */ }

func Dial(addr string, opts ...Option) (*Client, error)
func (c *Client) Close() error
func (c *Client) Raw() dashapi.DashApiClient   // escape hatch for unwrapped use

func (c *Client) Apply(ctx, *dashapi.Object) (*dashapi.Ack, error)
func (c *Client) Delete(ctx, dashapi.ObjectKind, key []string) (*dashapi.Ack, error)
func (c *Client) Get(ctx, dashapi.ObjectKind, key []string) (*dashapi.Object, error)
func (c *Client) List(ctx, dashapi.ObjectKind, keyPrefix string) ([]*dashapi.Object, error)
func (c *Client) Subscribe(ctx, kinds []dashapi.ObjectKind, snapshotFirst bool)
    (<-chan *dashapi.Event, <-chan error, error)
func (c *Client) GetCounters(ctx, dashapi.ObjectKind, key []string) (map[string]int64, error)
```

Insecure-by-default credentials (TLS not wired yet). To add interceptors,
custom credentials, or compression, use:

```go
client.Dial(addr, client.WithDialOptions(
    grpc.WithDefaultCallOptions(grpc.UseCompressor("gzip")),
))
```

---

## 4. Cobra command tree

Built by `cmd.NewRootCmd()`:

```
dash-sim-client
├── kinds          -- list every ObjectKind + key_parts
├── ping           -- dial + list vnets to confirm connectivity
├── apply          -- create/replace (inline or from -f scenario file)
├── get            -- read one Object
├── delete         -- remove one Object
├── list           -- stream every Object of a kind (with --prefix, --limit)
├── subscribe      -- stream Events (snapshot + live)
├── counters       -- read synthetic counter snapshot
└── simulate       -- run a packet through the dash-sim pipeline
```

Persistent flags (every subcommand):

| Flag | Default | Purpose |
|---|---|---|
| `--target` | `localhost:50051` | gRPC endpoint |
| `-o, --output` | `json` | json, yaml, or table |
| `--timeout` | `10s` | per-RPC timeout |
| `--insecure` | `true` | use plaintext gRPC |

`subscribe` and `simulate` have their own per-command flags
(`--snapshot`, `--kinds`, `--direction`, `--src-ip`, `--dst-ip`, `--trace`,
etc.). See [07](../07-dash-sim-client.md) for the full table.

---

## 5. `render` package

Single responsibility: turn an `*dashapi.Object` (or slice, or counters map)
into json, yaml, or table output.

- **json** uses `protojson` to honor upstream `snake_case` field names.
- **yaml** round-trips the json through `gopkg.in/yaml.v3`.
- **table** emits `KIND  KEY  VALUE` with a one-line tight-JSON in the
  `VALUE` column.

The envelope (`kind`, `key`, `value`) is constructed here — it does NOT
ship over the wire; it's a human-readable shell.

---

## 6. Apply input modes

The `apply` subcommand accepts:

### Inline
```bash
dash-sim-client apply --kind vnet --key vnet-prod --value '{"vni":1001}'
```

### From a YAML or JSON file (`-f`)
```yaml
# scenario.yaml
- kind: vnet
  key: [vnet-prod]
  value: { vni: 1001 }
- kind: eni
  key: [eni-001]
  value:
    eni_id: "11111111-1111-1111-1111-111111111111"
    mac_address: "ABEiM0RV"
    vnet: "vnet-prod"
    admin_state: STATE_ENABLED
```
```bash
dash-sim-client apply -f scenario.yaml
```

Either form converts `value` → JSON → upstream proto via `protojson`.

---

## 7. Embedding the SDK in another Go program

```go
import (
    "context"
    "time"

    dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
    dash_vnet "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/vnet"
    "github.com/rashmirrout/DashCenter/src/impl-go/dash-sim-client/pkg/client"
    "github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime/kinds"
)

func main() {
    cl, _ := client.Dial("localhost:50051")
    defer cl.Close()

    obj, _ := kinds.WrapObject(
        dashapi.ObjectKind_OBJECT_KIND_VNET,
        []string{"vnet-prod"},
        &dash_vnet.Vnet{Vni: 1001},
    )
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    cl.Apply(ctx, obj)
}
```

---

## 7a. Referential integrity errors

When `dash-sim` runs with `--strict-refs` (the default), `apply`
commands that reference missing objects are **rejected**. The error
message names the missing ref, the field, and suggests the fix.

### Why Apply can fail even with valid syntax

The JSON/YAML you pass is syntactically correct — the kind exists, the
key has the right number of parts, the value fields match the proto
schema. But the sim also checks **semantic correctness**: does the
object you're referencing actually exist in the store?

This is called **referential integrity** — the same concept as foreign
keys in a relational database. An ENI's `vnet` field is a foreign key
to a `vnet` object. If that vnet doesn't exist, the ENI is orphaned
and the pipeline will drop packets.

### Experiment: wrong config → error → understand → fix → retry

**Step 1 — Apply an ENI with a typo'd vnet (FAIL):**

```bash
dash-sim-client apply --kind eni --key eni-001 --value '{"vnet":"vnet-bllue"}'
```

**Output:**

```
referential integrity: eni references vnet "vnet-bllue" (field vnet)
which does not exist; create it first
```

**What the error tells you:**
- `eni` — the kind you tried to create
- `vnet "vnet-bllue"` — the missing dependency (the typo)
- `(field vnet)` — which field carries the bad reference
- `create it first` — the fix: create the vnet before the ENI

**Step 2 — Create the dependency first (PASS):**

```bash
dash-sim-client apply --kind vnet --key vnet-bllue --value '{"vni":100}'
# → accepted (vnet is Tier 0 — no FK checks)

dash-sim-client apply --kind eni --key eni-001 --value '{"vnet":"vnet-bllue"}'
# → accepted (vnet-bllue now exists — FK check passes)
```

### The dependency tiers

The 29 DASH object kinds follow a strict creation order:

```
Tier 0 (roots — create first, no dependencies):
  vnet, qos, acl_group, route_group, tunnel, meter_policy, ha_set, ...

Tier 1 (references Tier 0 only):
  eni → vnet         acl_rule → acl_group
  route → route_group + vnet   vnet_mapping → vnet

Tier 2 (references Tier 0 + Tier 1):
  eni_route → eni + route_group
  acl_in/out → eni + acl_group
  route_rule → eni + vnet
```

**Rule**: create bottom-up (Tier 0 → 1 → 2). Delete top-down (2 → 1 → 0).

### Using `validate -f` for pre-flight checks

For scenario files with many objects, use `validate` to check them all
at once instead of applying one at a time:

```bash
dash-sim-client validate -f scenario.yaml
# INDEX  STATUS  KIND       KEY         ERROR
# 0      ✅ OK   vnet       vnet-prod
# 1      ✅ OK   eni        eni-001
# 2      ❌ FAIL eni_route  eni-ghost   referential integrity: ...
#
# Total: 3  Accepted: 2  Rejected: 1
```

See [referential-integrity-validation.md](../../dashd-features/referential-integrity-validation.md)
for the complete FK map.

---

## 8. Adding a new subcommand

1. Create `internal/cmd/<name>.go` with a `newXxxCmd() *cobra.Command` that
   calls SDK methods.
2. Register it in `root.go` inside `NewRootCmd()`:
   ```go
   root.AddCommand(newXxxCmd())
   ```
3. Rebuild and re-test:
   ```bash
   go build ./...
   dash-sim-client xxx --help
   ```

---

## 9. Where the user-facing CLI reference lives

→ [docs/CLI_GUIDE.md](../../CLI_GUIDE.md) — every subcommand + every output.

→ [07 — dash-sim-client](../07-dash-sim-client.md) — tutorial-style summary
+ tips not in the reference.
