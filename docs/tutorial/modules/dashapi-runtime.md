# dashapi-runtime — the kinds registry

| Field | Value |
|---|---|
| Source | [`src/impl-go/dashapi-runtime/`](../../../src/impl-go/dashapi-runtime/) |
| Module path | `github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime` |
| Status | stable |

---

## Purpose

`dashapi-runtime/kinds` is the **single source of truth** for the 29 DASH
object kinds. Every per-kind switch in the codebase — packing a payload,
listing key parts, building the SONiC table name — lives here. Adding a
new kind is a **one-place change**.

Without this registry, the model store, the gRPC server, the scenario
loader, the CLI's render package, and the Redis adapter would each contain
their own 29-arm switch. With it, they all iterate `kinds.All`.

---

## Public API

```go
type Info struct {
    Kind     dashapi.ObjectKind          // the enum value
    Name     string                       // short, lower_snake_case: "vnet_mapping"
    KeyParts []string                     // names from upstream <Kind>Key, in order
    NewZero  func() proto.Message         // zero-value of the typed payload
    Pack     func(*dashapi.Object, proto.Message)        // set the oneof payload
    Unpack   func(*dashapi.Object) (proto.Message, bool) // extract the oneof payload
}

func (i Info) TableName() string          // "DASH_<NAME-UPPERCASE>_TABLE"

var All []Info                            // every kind, ordered by enum value
func Lookup(kind dashapi.ObjectKind) (Info, error)
func LookupByName(name string) (Info, error)
func Names() []string

func PayloadOf(o *dashapi.Object) (proto.Message, error)
func WrapObject(kind dashapi.ObjectKind, key []string, m proto.Message) (*dashapi.Object, error)
```

---

## Example uses

### Pack a payload into an Object

```go
obj, err := kinds.WrapObject(
    dashapi.ObjectKind_OBJECT_KIND_VNET,
    []string{"vnet-prod"},
    &dash_vnet.Vnet{Vni: 1001},
)
```

### Unpack a payload out of an Object

```go
payload, err := kinds.PayloadOf(obj)
vnet := payload.(*dash_vnet.Vnet)
```

### Iterate every kind

The Redis adapter uses this for snapshot streaming:

```go
for _, info := range kinds.All {
    pattern := info.TableName() + ":*"
    ... // SCAN, decode, send
}
```

### Compute the SONiC APP_DB table name

```go
info, _ := kinds.Lookup(dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING)
fmt.Println(info.TableName())          // DASH_VNET_MAPPING_TABLE
```

---

## Adding a new ObjectKind

Six-step checklist:

1. **Vendor the upstream proto** (if it isn't already):
   ```bash
   pwsh -NoProfile -File scripts/vendor-protos.ps1   # bump to latest or pin to commit
   ```
2. **Register an ObjectKind value** in `proto/dashapi/v1/dashapi.proto`:
   ```protobuf
   enum ObjectKind {
     ...
     OBJECT_KIND_NEW_THING = 30;   // append; never reuse numbers
   }
   ```
3. **Import the upstream package** in `dashapi.proto` and add a field to
   the `Object.payload` oneof:
   ```protobuf
   import "new_thing.proto";
   message Object {
     ...
     oneof payload {
       ...
       dash.new_thing.NewThing new_thing = 129;   // append, never reuse
     }
   }
   ```
4. **Regenerate Go stubs**:
   ```bash
   pwsh -NoProfile -File scripts/codegen-go.ps1
   ```
5. **Register the kind** in
   [`dashapi-runtime/kinds/kinds.go`](../../../src/impl-go/dashapi-runtime/kinds/kinds.go):
   ```go
   {
     Kind: dashapi.ObjectKind_OBJECT_KIND_NEW_THING, Name: "new_thing",
     KeyParts: []string{"thing_id"},
     NewZero:  func() proto.Message { return &dash_new_thing.NewThing{} },
     Pack: func(o *dashapi.Object, m proto.Message) {
       o.Payload = &dashapi.Object_NewThing{NewThing: m.(*dash_new_thing.NewThing)}
     },
     Unpack: func(o *dashapi.Object) (proto.Message, bool) {
       x, ok := o.Payload.(*dashapi.Object_NewThing)
       if !ok || x == nil { return nil, false }
       return x.NewThing, true
     },
   },
   ```
6. **Build + test**:
   ```bash
   cd src/impl-go
   go build ./...
   go test ./dash-sim/... ./dash-redis-adapter/...
   ```

Nothing else needs to change. The CLI will discover the new kind via
`Names()`; the model store will accept Apply/Get/Delete for it; the Redis
adapter will write to `DASH_NEW_THING_TABLE:<key>`.

---

## How `TableName()` works

```go
func (i Info) TableName() string {
    return "DASH_" + strings.ToUpper(i.Name) + "_TABLE"
}
```

So `vnet_mapping` → `DASH_VNET_MAPPING_TABLE`, matching exactly what SONiC's
DASH orchagent expects in APP_DB.
