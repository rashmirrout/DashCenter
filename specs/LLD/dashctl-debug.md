# Low-Level Design — `dashctl debug` Subcommand Group

> **Document scope.** Complete spec for the `dashctl debug` subcommand
> group — the raw-protocol escape hatch for operators and maintainers.
> This is a companion to the main dashctl LLD
> ([`dashctl-lld.md`](dashctl-lld.md)); it is referenced from §5.4 of
> that document.
>
> **Parent specs.**
> - HLD: [`specs/HLD/dashctl-hld.md`](../HLD/dashctl-hld.md)
> - LLD: [`specs/LLD/dashctl-lld.md`](dashctl-lld.md)
> - Phase tracker: [`specs/Impl-Plan/dashctl-impl-phases.md`](../Impl-Plan/dashctl-impl-phases.md)
>
> **Last updated:** 2026-06-10

---

## Table of contents

1. [Positioning](#1-positioning)
2. [Command tree](#2-command-tree)
3. [Sub-phase placement](#3-sub-phase-placement)
4. [Per-command specs](#4-per-command-specs)
   - 4.1 [`debug put-raw`](#41-debug-put-raw)
   - 4.2 [`debug get-raw`](#42-debug-get-raw)
   - 4.3 [`debug curl`](#43-debug-curl)
   - 4.4 [`debug admin`](#44-debug-admin)
   - 4.5 [`debug grpc-stream`](#45-debug-grpc-stream)
   - 4.6 [`debug parity`](#46-debug-parity)
5. [Architecture impact](#5-architecture-impact)
6. [Quality gates](#6-quality-gates)
7. [Integration test scenarios](#7-integration-test-scenarios)
8. [Open design questions](#8-open-design-questions)

---

## 1. Positioning

### What `debug` is

`debug` is a top-level Cobra subcommand group, sibling to `get`, `apply`,
`dpu`, etc. It provides **raw-protocol escape hatches** for:

- Bypassing dashctl's typed envelope codec to test wire-level edge cases.
- Exercising gRPC server-streaming RPCs in isolation (Phase 2).
- Verifying REST ↔ gRPC parity after transport upgrades.
- Generating reproducible `curl` / `grpcurl` commands for bug reports.
- Querying dashd admin endpoints that dashctl doesn't surface as typed
  commands yet.

### What `debug` is NOT

Three invariants the `debug` group must never violate:

1. **No business logic.** It calls dashd's northbound APIs (REST `:8443`,
   gRPC `:9443`, Admin `:7443`). It **never** talks `dashapi.v1`
   southbound to DPU agents — that is `dash-sim-client`'s job.
2. **No hidden side-effects.** The only write subcommand is `put-raw`,
   which is explicitly labeled as a write operation. Everything else is
   read-only or offline.
3. **Stable exit codes.** The same taxonomy as all other dashctl commands
   ([LLD § 10.3](dashctl-lld.md#103-stable-mapping)).

### Visibility

The `debug` group is **hidden from top-level `dashctl --help`** (via
`cobra.Command{Hidden: true}`) but fully accessible by name:

```
dashctl debug --help      # works — prints all debug subcommands
dashctl debug put-raw ... # works
dashctl --help            # does NOT show "debug" in the verb list
```

This matches how `kubectl alpha` handles escape-hatch commands.

---

## 2. Command tree

```
dashctl debug
├── put-raw       bypass envelope codec; send raw JSON to dashd ControlPlane
├── get-raw       bypass codec; dump raw protojson stored in dashd
├── curl          offline: print equivalent curl / grpcurl command
├── admin         raw GET to any dashd admin endpoint (:7443)
├── grpc-stream   open a named gRPC server-stream RPC; dump events as NDJSON
└── parity        compare REST vs gRPC Get; diff and exit non-zero on mismatch
```

---

## 3. Sub-phase placement

| Command | Ships when | Transport required | Prerequisite |
|---|---|---|---|
| `put-raw` | Phase 1 extension | REST (Phase 1) or gRPC (Phase 2) | dashctl Phase 1 ✅ |
| `get-raw` | Phase 1 extension | REST (Phase 1) or gRPC (Phase 2) | dashctl Phase 1 ✅ |
| `curl` | Phase 1 extension | none (offline) | dashctl Phase 1 ✅ |
| `admin` | Phase 1 extension | REST admin | dashctl Phase 1 ✅ |
| `grpc-stream` | Sub-phase 2A | gRPC only | dashd PA + dashctl 2A |
| `parity` | Sub-phase 2A | REST + gRPC | dashd PA + dashctl 2A |

The four Phase-1-extension commands can land in a single PR today. The
two Phase-2/2A commands slot into sub-phase 2A alongside the gRPC
backend.

---

## 4. Per-command specs

### 4.1 `debug put-raw`

**Purpose.** Send a raw JSON body to dashd's `Put<Kind>` endpoint,
completely bypassing dashctl's envelope codec, spec validation, and
`metadata.generation` injection. Use this to test schema edge cases,
submit intentionally malformed specs to probe dashd's validation, or
verify what the wire actually looks like.

#### Flags

| Flag | Type | Required | Default | Notes |
|---|---|---|---|---|
| `--kind` | string | ✅ | — | e.g. `vnet`, `eni`, `acl-policy` |
| `--name` | string | ✅ | — | spec name; used in REST URL path or gRPC request |
| `--ns` | string | | context namespace | |
| `--json` | string | one of | — | raw JSON body (inline) |
| `--json-file` | string | one of | — | file path; use `-` for stdin |
| `--expected-generation` | uint64 | | — | inject `expectedGeneration` field into request |
| `--dry-run` | bool | | false | print request details, do not send |

#### Behavior

- **REST.** `PUT /v1/{ns}/{kind-plural}/{name}` with the user's JSON as
  the body verbatim. No marshaling, no codec, no envelope unwrapping.
- **gRPC.** Looks up `PutFn` in the kind registry and passes a
  `protojson.Unmarshal` of the user's JSON directly into the generated
  message. The bypass is dashctl's *client-side* codec; dashd's
  server-side validation still runs.
- **`--dry-run`.** Prints the URL / service+method + headers + body,
  exits 0 without sending.

#### Output (default `-o raw`)

```
→ PUT /v1/default/vnets/vnet-prod   [REST, 14ms]
← 200 OK
{"generation":5,"txnId":"abc-123"}
```

#### Exit codes

Same as `apply`: 0 (success), 4 (CAS mismatch), 5 (validation error),
7 (unavailable), 10 (internal).

#### How it differs from `apply -f`

`apply` requires a valid `dashcenter.v1` envelope, enforces
`apiVersion: dashcenter.v1`, runs protojson strict validation
client-side, and injects `expected_generation` automatically. `put-raw`
skips all of that — the operator supplies the exact bytes that hit the
wire.

---

### 4.2 `debug get-raw`

**Purpose.** Fetch a spec from dashd and print the exact protojson wire
shape without dashctl's envelope wrapping, column filtering, or schema
enforcement. The canonical answer to "what is dashd actually storing for
this object?"

#### Flags

| Flag | Type | Required | Default | Notes |
|---|---|---|---|---|
| `--kind` | string | ✅ | — | |
| `--name` | string | ✅ | — | |
| `--ns` | string | | context namespace | |
| `-o` | string | | `json` | `json`, `yaml`, `raw` (no indent) |

#### Behavior

Calls `client.Get(ns, kind, name)` → receives
`*dashcenterv1.PolicyObject` → serializes `PolicyObject.spec` via
`protojson.Marshal(UseProtoNames: true)` without any `Envelope`
wrapping.

#### How it differs from `get -o json`

- `get -o json` emits `{apiVersion, kind, metadata, spec}` — the
  dashctl envelope.
- `get-raw` emits the **spec bytes only**, exactly as stored in dashd.
  No wrapping, no metadata injection.

---

### 4.3 `debug curl`

**Purpose.** Print the `curl` (or `grpcurl`) command equivalent to a
given dashctl operation, with all resolved auth headers, TLS flags, and
endpoint substituted in. Does NOT execute anything. The canonical tool
for sharing reproducible bug reports and teaching new operators.

#### Flags

| Flag | Type | Required | Default | Notes |
|---|---|---|---|---|
| `--kind` | string | ✅ | — | |
| `--name` | string | | — | if absent: prints list URL |
| `--ns` | string | | context namespace | |
| `--method` | string | | `GET` | `GET`, `PUT`, `DELETE` |
| `--body` | string | | — | JSON body (for PUT); use `-` for stdin |
| `--body-file` | string | | — | file containing the JSON body |
| `--include-auth` | bool | | true | `--no-include-auth` to redact token |
| `--format` | string | | auto | `bash` (Linux/macOS), `ps1` (Windows) |

#### Output — REST context

```bash
# dashctl debug curl --kind vnet --name vnet-prod --ns default

curl -s -X GET \
  'https://dashd-prod.example.com:8443/v1/default/vnets/vnet-prod' \
  -H 'Authorization: Bearer eyJhb...[redacted 80%]' \
  -H 'Content-Type: application/json' \
  -H 'X-Dashctl-Client: dashctl/v1.2.0 (abc1234)' \
  --cacert /etc/dashctl/prod-ca.pem
```

#### Output — gRPC context (emits `grpcurl`)

```bash
# dashctl debug curl --kind vnet --name vnet-prod  [transport: grpc]

grpcurl \
  -H 'authorization: Bearer eyJhb...[redacted 80%]' \
  -d '{"namespace":"default","name":"vnet-prod"}' \
  -proto proto/dashcenter/v1/control_plane.proto \
  dashd-prod.example.com:9443 \
  dashcenter.v1.ControlPlane/Get
```

#### Phase

REST form works in Phase 1. gRPC form works in Phase 2 / 2A (needs
the correct service + method names, supplied from the kind registry's
gRPC dispatch table).

---

### 4.4 `debug admin`

**Purpose.** Issue a raw HTTP GET to any dashd admin endpoint (`:7443`),
bypassing dashctl's typed admin client wrapper (`AdminHealth`,
`AdminDrift`, etc.). Use this for admin endpoints that dashctl doesn't
surface as typed commands yet.

#### Flags

| Flag | Type | Required | Default | Notes |
|---|---|---|---|---|
| `--path` | string | ✅ | — | e.g. `/admin/health`, `/admin/drift` |
| `--admin-endpoint` | string | | derived `:7443` | override the admin address |
| `--query` | `k=v` | repeatable | — | query params, e.g. `--query dpu=dpu-sim-01` |

#### Output

```
→ GET :7443/admin/drift?dpu=dpu-sim-01   [3ms]
← 200 OK
{"drifts":[...]}
```

#### Implementation note

Uses the same `net/http` client already in `pkg/client/rest`. Requires
one new `Client` interface method (see [§ 5 Architecture
impact](#5-architecture-impact)):

```go
AdminRaw(ctx context.Context, path string, params map[string]string) (json.RawMessage, error)
```

The gRPC backend returns `ErrUnimplemented` for `AdminRaw` (admin is
REST-only on dashd by design).

---

### 4.5 `debug grpc-stream`

> **Phase.** Sub-phase 2A (requires gRPC backend).

**Purpose.** Open a named gRPC server-streaming RPC and dump each
received message as NDJSON, one line per message. This is the
lowest-level smoke-test for gRPC streaming: "does the stream open, does
it send messages, does Ctrl-C cancel within 250 ms?"

#### Flags

| Flag | Type | Required | Default | Notes |
|---|---|---|---|---|
| `--service` | string | ✅ | — | e.g. `ObservabilityService` |
| `--rpc` | string | ✅ | — | e.g. `GetDpuStatus`, `WatchEvents` |
| `--request` | string | | `{}` | protojson request body |
| `--count` | int | | 0 | exit after N messages (0 = unlimited) |
| `--timeout` | duration | | 0 | stream timeout (0 = until Ctrl-C) |

#### Supported RPC dispatch table

| Service | RPC | Example request |
|---|---|---|
| `ObservabilityService` | `GetDpuStatus` | `{"dpuIds":["dpu-sim-01"],"deltasOnly":false}` |
| `ObservabilityService` | `WatchEvents` | `{"kinds":["Vnet","Eni"]}` |
| `ObservabilityService` | `GetCounters` | `{"dpuId":"dpu-sim-01"}` |
| `ObservabilityService` | `GetDrift` | `{"dpuId":"dpu-sim-01"}` |
| `ObservabilityService` | `GetAuditLog` | `{}` |
| `ObservabilityService` | `GetFlowList` | `{"dpuId":"dpu-sim-01"}` |

This table grows as dashd Phase 2 milestones (PC, PD, PE) ship
additional streaming RPCs (e.g., `DrainDpu`, `WatchHaEvents`,
`StreamMigrationSession`, `GetAclHitStats`).

#### Output (NDJSON)

```
{"n":1,"ts":"2026-06-10T09:00:00.123Z","msg":{"dpuId":"dpu-sim-01","state":"UP","vnetCount":3}}
{"n":2,"ts":"2026-06-10T09:00:05.456Z","msg":{"dpuId":"dpu-sim-02","state":"UP","vnetCount":1}}
^C
stream closed: 2 messages in 5.3s (SIGINT)
```

#### Transport gate

Returns exit 9 (`Unimplemented`) on REST contexts with a descriptive
hint:

```
Error: debug grpc-stream requires transport: grpc; current context uses REST.
Hint: Switch with: dashctl config set-context <name> --transport grpc
```

#### Implementation

Uses a **compile-time dispatch table** in `internal/cmd/debug.go` — NOT
gRPC server reflection (keeps the binary CGO-free and avoids the
reflection import). Each supported RPC is a closure:

```go
type streamEntry struct {
    service string
    rpc     string
    open    func(ctx context.Context, client client.Client, req json.RawMessage) (stream.Stream[json.RawMessage], error)
}

var streamRegistry = map[string]streamEntry{
    "ObservabilityService.GetDpuStatus": {
        service: "ObservabilityService",
        rpc:     "GetDpuStatus",
        open:    func(ctx context.Context, c client.Client, req json.RawMessage) (stream.Stream[json.RawMessage], error) {
            // unmarshal req → DpuStatusRequest, call c.GetDpuStatusStream(), wrap as json stream
        },
    },
    // ...
}
```

Requires one new `Client` interface method (Phase 2 addition):

```go
DebugStream(ctx context.Context, key string, requestJSON json.RawMessage) (stream.Stream[json.RawMessage], error)
```

The REST backend returns `ErrUnimplemented` for `DebugStream`.

---

### 4.6 `debug parity`

> **Phase.** Sub-phase 2A (requires both REST and gRPC backends).

**Purpose.** Issue the same `Get` call over BOTH the REST backend and
the gRPC backend simultaneously, then compare the resulting protojson
field-for-field. Exit 0 if identical, exit 1 with a diff if they
differ. This is the automated regression test for sub-phase 2A's core
claim: "every Phase-1 verb produces identical results on REST and gRPC."

#### Flags

| Flag | Type | Required | Default | Notes |
|---|---|---|---|---|
| `--kind` | string | ✅ | — | |
| `--name` | string | ✅ | — | |
| `--ns` | string | | context namespace | |
| `--rest-endpoint` | string | | derive `:8443` from gRPC context | |
| `--grpc-endpoint` | string | | from context | |
| `--ignore-fields` | string | | — | comma-list of fields to exclude (e.g. `txnId,serverTimestamp`) |
| `--all` | bool | | false | run parity check on every object for every kind |

#### Output (match)

```
vnet/vnet-prod  REST ↔ gRPC ✓  (REST: 11ms, gRPC: 7ms)
```

#### Output (mismatch)

```
vnet/vnet-prod  REST ↔ gRPC ✗  DIFF

--- REST  (:8443)
+++ gRPC  (:9443)
@@ @@
-  "adminState": "up"
+  "adminState": "UP"
```

#### Exit codes

- `0` — all objects match.
- `1` — at least one mismatch (diff printed to stdout).
- `7` — one endpoint unreachable.
- `9` — gRPC not configured in context (`Unimplemented`).

#### Implementation

Dials two clients internally — does not go through the single-backend
`client.Dial`:

```go
restCfg := resolved.WithTransport(client.TransportREST, restEndpoint)
grpcCfg := resolved.WithTransport(client.TransportGRPC, grpcEndpoint)

restClient, _ := client.Dial(ctx, restCfg)
grpcClient, _ := client.Dial(ctx, grpcCfg)

// fire both Get() concurrently
// normalize protojson (sorted keys, UseProtoNames)
// compare byte-equal; if not, produce unified diff
```

No new `Client` interface methods needed. Uses the existing `Get` method
on both backends.

---

## 5. Architecture impact

### New file

```
src/impl-go/dashctl/
└── internal/cmd/
    └── debug.go    ← NEW: dashctl debug + 6 subcommands
```

No other source files are created. `debug.go` registers the `debug`
cobra group and its 6 children.

### New `Client` interface methods

Two methods are added to `pkg/client.Client`. Both follow the existing
append-only stability promise ([LLD § 18.4](dashctl-lld.md#184-public-api-stability-promise)):
new methods may be added between minor versions; signatures of existing
methods are fixed within a major version.

```go
// Phase 1 extension — supports debug admin subcommand.
// Issues a raw GET to the dashd admin surface (:7443).
// The gRPC backend returns ErrUnimplemented (admin is REST-only on dashd).
AdminRaw(ctx context.Context, path string, params map[string]string) (json.RawMessage, error)

// Phase 2 / 2A — supports debug grpc-stream subcommand.
// Opens a named server-stream by "Service.RPC" key and returns raw
// protojson messages. The REST backend returns ErrUnimplemented.
DebugStream(ctx context.Context, key string, requestJSON json.RawMessage) (stream.Stream[json.RawMessage], error)
```

### Existing code changes (minimal)

| File | Change | Phase |
|---|---|---|
| `pkg/client/client.go` | Add `AdminRaw` and `DebugStream` to the `Client` interface | Phase 1 ext + 2A |
| `pkg/client/rest/rest.go` | Implement `AdminRaw` (HTTP GET to admin endpoint); return `ErrUnimplemented` for `DebugStream` | Phase 1 ext |
| `pkg/client/grpc/grpc.go` | Return `ErrUnimplemented` for `AdminRaw`; implement `DebugStream` using dispatch table | 2A |
| `internal/cmd/root.go` | Register `debug` subcommand group | Phase 1 ext |

### No changes to

- `pkg/manifest/` — debug bypasses the codec entirely.
- `internal/render/` — debug uses raw JSON output, not the render engine.
- `internal/config/` — debug uses the existing context resolution.
- Any existing command file (`apply.go`, `get.go`, `dpu.go`, etc.).

---

## 6. Quality gates

| # | Gate | Criterion | Phase |
|---|---|---|---|
| CD-G1 | `put-raw` round-trip | `put-raw` + `get-raw` produces byte-equal spec to `apply` + `get -o json` (same spec body) | Phase 1 ext |
| CD-G2 | `put-raw` schema rejection | invalid field → `INVALID_ARGUMENT` exit 5 (dashd validates, not dashctl) | Phase 1 ext |
| CD-G3 | `put-raw --dry-run` | prints URL + body, makes zero HTTP calls | Phase 1 ext |
| CD-G4 | `curl` REST form | emitted `curl` command, when executed, gives identical JSON to `get -o json` | Phase 1 ext |
| CD-G5 | `curl` gRPC form | emitted `grpcurl` command contains correct `dashcenter.v1.ControlPlane/Get` service path | 2A |
| CD-G6 | `admin` health | `debug admin --path /admin/health` exits 0 with valid JSON | Phase 1 ext |
| CD-G7 | `admin` unknown path | non-existent path → `404` surfaced with exit 3 | Phase 1 ext |
| CD-G8 | `grpc-stream` cancellation | `SIGINT` during stream → exit 0 within 250 ms, prints event count | 2A |
| CD-G9 | `grpc-stream` on REST context | returns exit 9 with descriptive transport hint | 2A |
| CD-G10 | `parity` match | after `apply`, exits 0 (REST and gRPC return same spec) | 2A |
| CD-G11 | `parity` mismatch | injected codec drift → exits 1 with readable unified diff | 2A |
| CD-G12 | Coverage | `internal/cmd/debug.go` ≥ 80 % statements | All |

---

## 7. Integration test scenarios

Located alongside the existing Phase 1 integration suite at
`src/impl-go/dashctl/test/integration/debug_test.go` under the same
`//go:build integration` tag.

| # | Test | Gate | Phase |
|---|---|---|---|
| D1 | `TestIntegration_Debug_PutRaw_RoundTrip` | CD-G1 | Phase 1 ext |
| D2 | `TestIntegration_Debug_PutRaw_InvalidField` | CD-G2 | Phase 1 ext |
| D3 | `TestIntegration_Debug_PutRaw_DryRun` | CD-G3 | Phase 1 ext |
| D4 | `TestIntegration_Debug_Curl_REST` | CD-G4 | Phase 1 ext |
| D5 | `TestIntegration_Debug_Admin_Health` | CD-G6 | Phase 1 ext |
| D6 | `TestIntegration_Debug_Admin_UnknownPath` | CD-G7 | Phase 1 ext |
| D7 | `TestIntegration_Debug_GrpcStream_Cancel` | CD-G8 | 2A |
| D8 | `TestIntegration_Debug_GrpcStream_REST_Reject` | CD-G9 | 2A |
| D9 | `TestIntegration_Debug_Parity_Match` | CD-G10 | 2A |
| D10 | `TestIntegration_Debug_Parity_Mismatch` | CD-G11 | 2A |

---

## 8. Open design questions

| # | Question | Recommendation | Decision |
|---|---|---|---|
| DQ1 | Should `put-raw` accept multi-doc YAML (like `apply -f`)? | No — single spec only. Multi-doc is the codec's job; `put-raw` is an escape hatch from the codec. | Proposed |
| DQ2 | Should `curl` emit a ready-to-paste one-liner or multi-line with `\`? | Multi-line by default; `--oneline` flag to collapse. | Proposed |
| DQ3 | Should `parity --all` run concurrently across kinds? | Yes — bounded `GOMAXPROCS` workers. Failure in one kind does not cancel others. | Proposed |
| DQ4 | Should `grpc-stream` support client-streaming RPCs (e.g. `ApplyBatch`)? | Not initially. Add `debug grpc-client-stream` as a 2B follow-up if needed. | Proposed |
| DQ5 | Should `debug` be user-extensible (e.g., register custom paths via config)? | No — keep it a hardcoded dispatch table. Post-Phase 2 plugin system (3.E.1) handles extensibility. | Proposed |
| DQ6 | Should `debug admin` support POST (for `/admin/reconcile`)? | Yes — add `--method GET|POST` (default GET). POST body via `--json` / `--json-file`. | Proposed |

---

> **End of `dashctl debug` spec.** For the overall dashctl design see
> [`dashctl-hld.md`](../HLD/dashctl-hld.md) and
> [`dashctl-lld.md`](dashctl-lld.md). For implementation tracking see
> [`dashctl-impl-phases.md`](../Impl-Plan/dashctl-impl-phases.md).