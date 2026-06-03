# LLD — dash-sim-client (operator CLI + Go SDK)

> Older notes refer to this as "dash-shim-client". The current name is
> **dash-sim-client**.

This document is the **definitive low-level design** of `dash-sim-client`.
It is the transport-only operator interface to any `dashapi.v1.DashApi`
server: today's `dash-sim` and `dash-redis-adapter`, tomorrow's `dashd`
controller, and future real-DPU agents.

---

## Table of contents

1. [Scope](#1-scope)
2. [Architectural invariant](#2-architectural-invariant)
3. [Module layout](#3-module-layout)
4. [Public SDK (pkg/client)](#4-public-sdk-pkgclient)
5. [Cobra command tree](#5-cobra-command-tree)
6. [Per-command low-level design](#6-per-command-low-level-design)
7. [Key parsing](#7-key-parsing)
8. [Value parsing](#8-value-parsing)
9. [Output rendering (json / yaml / table)](#9-output-rendering)
10. [Subscribe loop](#10-subscribe-loop)
11. [Simulate flow](#11-simulate-flow)
12. [Rust pseudocode parity (clap + tonic)](#12-rust-pseudocode-parity)
13. [Embedding the SDK in other programs](#13-embedding-the-sdk)
14. [Extension recipes](#14-extension-recipes)
15. [Test surface](#15-test-surface)

---

## 1. Scope

A single binary that:
- dials a `dashapi.v1.DashApi` gRPC service at `--target`
- exposes every RPC (`Apply`, `Get`, `Delete`, `List`, `Subscribe`,
  `GetCounters`, `SimulatePacket`) plus two discovery helpers (`kinds`,
  `ping`)
- accepts JSON or YAML values, supports composite keys joined with `:`,
  emits json / yaml / table output
- ships a small embeddable Go SDK under `pkg/client` for use in tests or
  third-party tools

---

## 2. Architectural invariant

`dash-sim-client` **does not import** any package under
`dash-sim/internal/*` or `dash-redis-adapter/internal/*`. Its only imports
are:

| Import | Purpose |
|---|---|
| `gen/go/dashapi/v1` | gRPC service stubs |
| `gen/go/dash/...` | upstream proto types (only via `kinds.Info.NewZero()` results) |
| `dashapi-runtime/kinds` | central registry — name ↔ ObjectKind ↔ key parts ↔ payload pack/unpack |
| `google.golang.org/grpc`, `google.golang.org/protobuf` | gRPC transport, protojson |
| `github.com/spf13/cobra`, `github.com/spf13/pflag` | command-line parsing |
| `gopkg.in/yaml.v3` | YAML decoding |

This guarantees the binary works against **any** DashApi server. If you
swap `dash-sim` for `dash-redis-adapter`, no recompile is needed — just
change `--target`.

---

## 3. Module layout

```
dash-sim-client/
├── go.mod
├── Dockerfile
├── README.md
├── cmd/dash-sim-client/main.go     -- 1-line: calls cmd.Execute()
├── internal/
│   ├── cmd/                        -- Cobra subcommand tree
│   │   ├── root.go                 -- root cmd + persistent flags
│   │   ├── apply.go                -- apply (inline OR -f)
│   │   ├── crud.go                 -- get, delete, list, counters, ping
│   │   ├── subscribe.go            -- subscribe + kinds
│   │   ├── simulate.go             -- simulate (calls SimulatePacket)
│   │   ├── codec.go                -- json/yaml dispatch helpers
│   │   └── helpers.go              -- parseKindArg, parseKeyArg, printAck, ...
│   └── render/
│       └── render.go               -- Object / Objects / CountersMap in 3 formats
└── pkg/
    └── client/
        └── client.go               -- Dial(...), Apply, Get, Delete, List, Subscribe, GetCounters
```

`pkg/client` is **exported**. `internal/cmd` and `internal/render` are
private to the CLI.

---

## 4. Public SDK (pkg/client)

### 4.1 Surface

```go
type Client struct { /* unexported */ }

type Option func(*options)
func WithDialOptions(opts ...grpc.DialOption) Option

func Dial(addr string, opts ...Option) (*Client, error)
func (c *Client) Close() error
func (c *Client) Raw() dashapi.DashApiClient   // escape hatch

func (c *Client) Apply(ctx, *dashapi.Object) (*dashapi.Ack, error)
func (c *Client) Delete(ctx, ObjectKind, key []string) (*dashapi.Ack, error)
func (c *Client) Get(ctx, ObjectKind, key []string) (*dashapi.Object, error)
func (c *Client) List(ctx, ObjectKind, keyPrefix string) ([]*dashapi.Object, error)
func (c *Client) Subscribe(ctx, kinds []ObjectKind, snapshotFirst bool)
    (<-chan *dashapi.Event, <-chan error, error)
func (c *Client) GetCounters(ctx, ObjectKind, key []string) (map[string]int64, error)
```

### 4.2 Defaults

- Insecure transport credentials (no TLS).
- 10s per-RPC timeout (settable via `context.WithTimeout` from the caller).
- One `*grpc.ClientConn` per `Client`; safe to share across goroutines.

### 4.3 Dial pseudocode

```go
func Dial(addr string, opts ...Option) (*Client, error) {
    o := &options{ dialOpts: []grpc.DialOption{
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    }}
    for _, fn := range opts { fn(o) }

    conn, err := grpc.NewClient(addr, o.dialOpts...)
    if err != nil { return nil, fmt.Errorf("dial %s: %w", addr, err) }

    return &Client{conn: conn, api: dashapi.NewDashApiClient(conn)}, nil
}
```

### 4.4 List pseudocode

```go
func (c *Client) List(ctx, kind, prefix) ([]*dashapi.Object, error) {
    stream := c.api.List(ctx, &ListRequest{Kind: kind, KeyPrefix: prefix})
    var out []*dashapi.Object
    for {
        item, err := stream.Recv()
        if errors.Is(err, io.EOF) { break }
        if err != nil { return nil, err }
        out = append(out, item.GetObject())
    }
    return out, nil
}
```

### 4.5 Subscribe pseudocode

```go
func (c *Client) Subscribe(ctx, kinds, snapshotFirst) (<-chan *Event, <-chan error, error) {
    stream := c.api.Subscribe(ctx, &SubscribeRequest{Kinds, SnapshotFirst})
    evCh   := make(chan *Event, 64)
    errCh  := make(chan error, 1)
    go func() {
        defer close(evCh); defer close(errCh)
        for {
            ev, err := stream.Recv()
            if err != nil {
                if !errors.Is(err, io.EOF) { errCh <- err }
                return
            }
            select {
                case evCh <- ev:
                case <-ctx.Done(): errCh <- ctx.Err(); return
            }
        }
    }()
    return evCh, errCh, nil
}
```

---

## 5. Cobra command tree

```
dash-sim-client
├── kinds          -- list every ObjectKind + key_parts
├── ping           -- dial + list vnets
├── apply          -- create/replace one or many objects
├── get            -- read one
├── delete         -- remove one
├── list           -- stream every object of a kind
├── subscribe      -- stream events (snapshot + live)
├── counters       -- read counter snapshot
└── simulate       -- run a packet through the pipeline
```

### 5.1 Persistent flags (every subcommand)

| Flag | Default | Purpose |
|---|---|---|
| `--target` | `localhost:50051` | gRPC endpoint host:port |
| `-o, --output` | `json` | `json`, `yaml`, or `table` |
| `--timeout` | `10s` | per-RPC timeout |
| `--insecure` | `true` | use plaintext gRPC (TLS not wired yet) |

---

## 6. Per-command low-level design

### 6.1 `apply`

Two input modes, mutually exclusive:

| Mode | Flag | Source |
|---|---|---|
| inline | `--kind --key --value` | one object on the command line |
| file | `-f, --file` | one or many `{kind, key, value}` entries from JSON / YAML / YAML stream |

Algorithm:

```
docs = if file != "" { readDocuments(file) }
       else          { [{kind, parseKeyArg(key), parseValueLoose(value)}] }
for each doc:
    info := kinds.LookupByName(doc.kind)
    msg  := info.NewZero()
    valJSON := json.Marshal(coerceToJSONCompat(doc.value))
    protojson.Unmarshal(valJSON, msg)            // upstream field names
    obj := kinds.WrapObject(info.Kind, doc.key, msg)
    ack := cl.Apply(ctx, obj)
    printAck(ack)
```

`readDocuments(file)` accepts:
- A JSON array of `{kind, key, value}`.
- A single JSON `{kind, key, value}` object.
- A YAML stream (one doc per `---` separator).

### 6.2 `get`

```
k    := parseKindArg(--kind)
obj  := cl.Get(ctx, k, parseKeyArg(--key))     -> may return NotFound
render.Object(stdout, format, obj)
```

### 6.3 `delete`

```
k   := parseKindArg(--kind)
ack := cl.Delete(ctx, k, parseKeyArg(--key))
printAck(ack)                                    -- Ack.error="not found" if absent
```

### 6.4 `list`

```
k     := parseKindArg(--kind)
items := cl.List(ctx, k, --prefix)
if --limit > 0 { items = items[:--limit] }
render.Objects(stdout, format, items)
```

### 6.5 `subscribe`

See [§10](#10-subscribe-loop).

### 6.6 `counters`

```
k := parseKindArg(--kind)
m := cl.GetCounters(ctx, k, parseKeyArg(--key))
render.CountersMap(stdout, format, m)
```

### 6.7 `simulate`

See [§11](#11-simulate-flow).

### 6.8 `kinds`

Pure local function — does not call the server. Iterates `kinds.All` and
prints `{name, enum, key_parts}` in the chosen format.

### 6.9 `ping`

```
cl := dial()
vs := cl.List(ctx, ObjectKind_VNET, "")
print "ok: target=<addr> vnets=<len(vs)>"
```

Doubles as a health probe and a basic connectivity check.

---

## 7. Key parsing

```go
func parseKeyArg(s string) []string {
    if s == "" { return nil }
    return strings.Split(s, ":")
}
```

The user provides keys as `:`-joined strings (`acl-prod-in:100`,
`eni-001:1`, `vnet-prod:10.0.0.10`). The server validates the count
against `kinds.Info.KeyParts`.

For prefixes that contain colons (rare for DASH but theoretically
possible) the user must escape them out-of-band; today the CLI does not
provide an escape mechanism (tracked in roadmap).

---

## 8. Value parsing

Inputs accepted by `apply --value`:
- compact JSON: `'{"vni":1001}'`
- multi-line YAML inside quoted shell strings

Algorithm:

```
trimmed := strings.TrimSpace(value)
if trimmed[0] in {'{', '['}: json.Unmarshal(value, &interface{})
else:                        yaml.Unmarshal(value, &interface{})
```

Both produce `map[string]interface{}` / `[]interface{}` trees. They are
then **coerced to JSON-compatible form** (replacing
`map[interface{}]interface{}` from yaml.v3 with `map[string]interface{}`)
and round-tripped via `json.Marshal` →
`protojson.Unmarshal(<info.NewZero()>)`. This is what lets the user write
upstream proto field names directly.

---

## 9. Output rendering

Implemented in [`internal/render/render.go`](../../src/impl-go/dash-sim-client/internal/render/render.go).

```go
type Format string
const (
    FormatJSON  Format = "json"
    FormatYAML  Format = "yaml"
    FormatTable Format = "table"
)

func Object(w io.Writer, format Format, obj *dashapi.Object) error
func Objects(w io.Writer, format Format, objs []*dashapi.Object) error
func CountersMap(w io.Writer, format Format, m map[string]int64) error
```

### 9.1 Envelope

For every Object the renderer produces a `{kind, key, value}` envelope:

```json
{
  "kind": "vnet",
  "key":  ["vnet-prod"],
  "value": { "vni": 1001 }
}
```

The envelope is a renderer concern — it does NOT travel over the wire.

### 9.2 Value rendering

```go
prettyJSON = protojson.MarshalOptions{
    Multiline: true, Indent: "  ", UseProtoNames: true, EmitUnpopulated: false,
}
tightJSON = protojson.MarshalOptions{
    UseProtoNames: true, EmitUnpopulated: false,
}
```

| Format | Algorithm |
|---|---|
| json | `prettyJSON.Marshal(payload)` → put under `value` → `json.MarshalIndent(envelope)` |
| yaml | same json → `yaml.Marshal` for human-readable indented output |
| table | header `KIND KEY VALUE`; `VALUE` is `tightJSON.Marshal(payload)` (one line per row) |

### 9.3 CountersMap

Outputs as a flat object/dict (json/yaml) or as a two-column table
(`COUNTER`, `VALUE`).

---

## 10. Subscribe loop

```
parsed := []ObjectKind{}
for _, s := range --kinds: parsed = append(parsed, parseKindArg(s))

ctx, cancel := streamContext()              // also wires SIGINT to cancel
defer cancel()

evCh, errCh := cl.Subscribe(ctx, parsed, --snapshot)
enc := json.NewEncoder(stdout); enc.SetIndent("", "")
for {
    select {
        case ev, ok := <-evCh:
            if !ok {
                if err := <-errCh; err != nil { return err }
                return nil
            }
            out := envelope(ev)
            out["type"] = strings.TrimPrefix(ev.Type.String(), "EVENT_TYPE_")
            enc.Encode(out)                      // one JSON line per event
        case err := <-errCh:
            return err
    }
}
```

Each event renders as **a single JSON line** suitable for piping into
`jq`, `grep`, or other line-oriented tools.

---

## 11. Simulate flow

```
pkt := if --file != "" {
    raw := os.ReadFile(file)
    p   := &dashapi.Packet{}
    protojson.Unmarshal(raw, p)
    p
} else {
    &dashapi.Packet{
        Direction: parseDirection(--direction),
        Eni: --eni, Vni: --vni,
        SrcMac, DstMac, SrcIp, DstIp,
        Protocol, SrcPort, DstPort,
        LengthBytes: --length,
    }
}
resp := cl.Raw().SimulatePacket(ctx, &SimulatePacketRequest{Packet: pkt, Trace: --trace})
printDecision(resp.Decision, withTrace=--trace)
```

The Decision is rendered as a JSON dict with `action`, `reason`,
`out_eni`, `out_underlay_ip`, `out_vni`, `out_routing_type`,
`matched_acl_stage`, `matched_acl_priority`, `matched_route_prefix`, and
(optionally) `trace`.

---

## 12. Rust pseudocode parity

For a future `impl-rust/crates/dash-sim-client`:

### 12.1 Cargo deps (sketch)

```toml
[dependencies]
tonic        = "0.11"
prost        = "0.12"
tokio        = { version = "1", features = ["full"] }
clap         = { version = "4", features = ["derive"] }
serde_json   = "1"
serde_yaml   = "0.9"
dashapi      = { path = "../../gen-rust/dashapi" }   # tonic-build output
kinds        = { path = "../dashapi-runtime/kinds" }
```

### 12.2 Root CLI

```rust
#[derive(Parser, Debug)]
#[command(name = "dash-sim-client", about = "Operator CLI for dashapi.v1.DashApi")]
struct Cli {
    #[arg(long, default_value = "localhost:50051")] target:  String,
    #[arg(long, short = 'o', default_value = "json")] output: String,
    #[arg(long, default_value = "10s")]              timeout: humantime::Duration,
    #[arg(long, default_value_t = true)]             insecure: bool,
    #[command(subcommand)] cmd: Cmd,
}

#[derive(Subcommand, Debug)]
enum Cmd {
    Kinds,
    Ping,
    Apply  (ApplyArgs),
    Get    (GetArgs),
    Delete (GetArgs),
    List   (ListArgs),
    Subscribe(SubscribeArgs),
    Counters(GetArgs),
    Simulate(SimulateArgs),
}
```

### 12.3 Apply

```rust
async fn apply(args: ApplyArgs, cl: &mut DashApiClient<Channel>) -> anyhow::Result<()> {
    let docs = if let Some(path) = &args.file { read_documents(path)? }
               else { vec![Doc { kind: args.kind.clone().unwrap(), key: split_key(&args.key), value: serde_yaml::from_str(&args.value)? }] };
    for d in docs {
        let info = kinds::lookup_by_name(&d.kind)?;
        let mut msg = (info.new_zero)();
        let val_json = serde_json::to_string(&d.value)?;
        protobuf_json_mapping::merge_from_str(&mut *msg, &val_json)?;
        let obj = kinds::wrap_object(info.kind, &d.key, &*msg)?;
        let ack = cl.apply(ApplyRequest { object: Some(obj) }).await?.into_inner();
        print_ack(&ack)?;
    }
    Ok(())
}
```

### 12.4 Subscribe

```rust
async fn subscribe(args: SubscribeArgs, cl: &mut DashApiClient<Channel>) -> anyhow::Result<()> {
    let kinds: Vec<i32> = args.kinds.iter().map(|s| parse_kind(s)).collect::<Result<_,_>>()?;
    let req  = SubscribeRequest { kinds, snapshot_first: args.snapshot };
    let mut stream = cl.subscribe(req).await?.into_inner();
    while let Some(ev) = stream.message().await? {
        let env = envelope(&ev);
        println!("{}", serde_json::to_string(&env)?);
    }
    Ok(())
}
```

### 12.5 Simulate

```rust
async fn simulate(args: SimulateArgs, cl: &mut DashApiClient<Channel>) -> anyhow::Result<()> {
    let pkt = build_packet(&args)?;
    let resp = cl.simulate_packet(SimulatePacketRequest { packet: Some(pkt), trace: args.trace })
                 .await?.into_inner();
    print_decision(&resp.decision.unwrap_or_default(), args.trace);
    Ok(())
}
```

---

## 13. Embedding the SDK

The `pkg/client` package is a stable Go API. Example consumer:

```go
import (
    "context"
    "time"

    dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
    dash_vnet "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/vnet"
    "github.com/rashmirrout/DashCenter/src/impl-go/dash-sim-client/pkg/client"
    "github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime/kinds"
)

func bootstrapVnet(addr string) error {
    cl, err := client.Dial(addr)
    if err != nil { return err }
    defer cl.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    obj, err := kinds.WrapObject(
        dashapi.ObjectKind_OBJECT_KIND_VNET,
        []string{"vnet-prod"},
        &dash_vnet.Vnet{Vni: 1001},
    )
    if err != nil { return err }

    _, err = cl.Apply(ctx, obj)
    return err
}
```

---

## 14. Extension recipes

### 14.1 Add a new subcommand

1. Create `internal/cmd/<name>.go` with `func new<Name>Cmd() *cobra.Command`.
2. Register it in `root.go`:
   ```go
   root.AddCommand(new<Name>Cmd())
   ```
3. Implement the command, calling SDK methods.
4. Rebuild:
   ```bash
   go build ./...
   ./bin/dash-sim-client <name> --help
   ```

### 14.2 Add a new output format

1. Extend the `Format` enum in `internal/render/render.go`.
2. Add a `case` for the new format in `Object`, `Objects`, `CountersMap`.
3. Update `ParseFormat` to accept the new name.

### 14.3 Support a new auth scheme

1. Extend `client.Option` with `WithTLS(certPool, serverName)` and/or
   `WithToken(string)`.
2. Add corresponding root persistent flags in `internal/cmd/root.go`.
3. Wire them into `dial()` in `root.go`.

---

## 15. Test surface

Today: covered indirectly by smoke tests in
[`docs/CLI_GUIDE.md`](../../docs/CLI_GUIDE.md) and the run-and-test page
([06](../../docs/tutorial/06-test.md)) which exercise the binary against
both backends.

Planned:
- Unit tests for `parseKindArg`, `parseKeyArg`, `coerceToJSONCompat`.
- Golden-file tests for `render.Object` / `render.Objects` output.
- An in-process bufconn fixture mirroring the adapter's test setup.

See [`docs/roadmap.md`](../../docs/roadmap.md).
