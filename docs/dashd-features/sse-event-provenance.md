# SSE Event Provenance — `source` + `via` Stamping

> **Audience**: dashw maintainers, SREs reading live SSE captures,
> developers writing new browser-side consumers of `/topology-v2`.
> **Scope**: the per-frame `source` (origin dashd) + `via` (relaying
> dashw replica) annotations added to every JSON frame emitted by
> dashw's topology Hub.
> **Companion docs**:
> [topology-streaming-design.md](topology-streaming-design.md)
> (PE-G7 production hardening — this doc is a focused follow-up),
> [features.md §10](features.md#10-cluster-topology-pe-g6--pe-g7).
> **Status**: ✅ Shipped 2026-06-13 as part of PE-G7.1 polish.

---

## Table of contents

1. [Problem statement](#1-problem-statement)
2. [Solution](#2-solution)
3. [Wire format](#3-wire-format)
4. [Implementation](#4-implementation)
5. [Configuration](#5-configuration)
6. [Operator-facing UX](#6-operator-facing-ux)
7. [Test strategy](#7-test-strategy)
8. [Future Scopes](#8-future-scopes)

---

## 1. Problem statement

Before this change, the dashw → browser SSE stream emitted opaque
`KIND_KEEPALIVE` frames every 30s — but the JSON body contained only
`{"kind":"KIND_KEEPALIVE","event_id":N,"ts":"..."}` with **no
identification of which process produced or relayed the event**.

Operator pain manifested as:

1. **"Where is this keepalive coming from?"** — an operator looking at
   DevTools couldn't tell which dashd or dashw replica was on the
   other end of the stream. In a multi-replica deploy this matters:
   if a dashw replica is misbehaving, you need to identify it from
   the same browser session that's observing the symptom.
2. **Silent failover invisibility** — when the dashw Hub's upstream
   reconnects to a different dashd (e.g., leader handover, dashd
   restart), nothing in the wire stream signalled the change. Browsers
   continued receiving events as if nothing happened, even though the
   underlying source had shifted.
3. **No correlation key for support tickets** — operators couldn't
   paste a stream sample into a bug report and have the receiving
   engineer immediately know which dashd / dashw replica was involved.

The root cause was a clean separation: the protobuf `TopologyEvent`
schema (defined in [proto/dashcenter/v1/cluster.proto](../../proto/dashcenter/v1/cluster.proto))
intentionally has no concept of "which process serialised this" —
it's a pure event message. But the SSE wire layer is operated by
dashw, which absolutely DOES know both its own identity AND the
upstream dashd it's relaying from. That metadata was simply being
discarded on the way out.

---

## 2. Solution

dashw stamps two new fields on every outbound SSE frame:

| Field | Meaning | Source of truth |
|---|---|---|
| `source` | Upstream dashd identity this hub is fanning out from | `cfg.DashdGrpcAddr` (the gRPC address dashw dialled) |
| `via` | This dashw replica's identity | `cfg.NodeID` — OS hostname by default, override via `--node-id` / `DASHW_NODE_ID` |

Both fields are emitted on **every** event kind including KEEPALIVE,
DROPPED, RATE_LIMITED and RESYNC. Empty values are omitted (so a
single-process dev deploy with no `NodeID` set emits just `source`).

### 2.1 Why dashw stamps (not dashd)

Putting `source`/`via` into the proto schema and asking dashd to
populate them was rejected because:

- dashd doesn't know its own externally-visible address (it could be
  behind a Kubernetes service, a LB, a docker port mapping…). The
  authoritative answer to "which dashd did this come from?" from a
  browser's perspective is **the address dashw dialled**, which only
  dashw knows.
- Schema churn for purely operational metadata is wrong — these fields
  are routing metadata, not protocol contract.
- Stamping at the dashw fan-out keeps it cheap: 1 splice per frame,
  shared across N subscribers (marshal-once invariant preserved).

### 2.2 Why both fields, not one

In a typical multi-replica deploy:

```
N browser tabs ──► L7 LB ──► dashw-A or dashw-B or dashw-C
                                 │
                                 └──► one of: dashd-1, dashd-2, dashd-3
```

A single `source` field couldn't disambiguate which dashw fan-out a
given browser session belonged to — useful when a tab gets a stale
view and the SRE needs to bounce the right dashw replica without
disturbing the others.

---

## 3. Wire format

### 3.1 Before (PE-G7)

```
event: keepalive
data: {"kind":"KIND_KEEPALIVE","event_id":4,"ts":"2026-06-13T17:08:00Z"}
```

### 3.2 After (PE-G7.1)

```
event: keepalive
data: {"kind":"KIND_KEEPALIVE","event_id":4,"ts":"2026-06-13T17:08:00Z","source":"dashd-1:9443","via":"edce4b15fcdc"}
```

Notes on parseability:
- The annotation is **always appended as the last two keys** of the
  JSON object before the closing `}`. Existing parsers using
  `JSON.parse` ignore unknown fields gracefully.
- The byte splice is done on the already-marshalled JSON to preserve
  PE-G7's marshal-once-fanout-many invariant (no double serialisation).
- If both `source` and `via` are empty (e.g., a dashw built with no
  config), the payload is byte-identical to PE-G7 — pure passthrough.

### 3.3 Confirmed wire sample from 05-full-console fleet

```
event: snapshot
data: {"kind":"KIND_SNAPSHOT","ts":"2026-06-13T17:08:00.725051841Z",
       "snapshot":{...full topology...},
       "source":"dashd-1:9443","via":"edce4b15fcdc"}
```

---

## 4. Implementation

### 4.1 Files touched

| File | Change |
|---|---|
| [src/impl-go/console/internal/config/config.go](../../src/impl-go/console/internal/config/config.go) | New `NodeID string` field, defaulting via `hostnameOr("dashw")`. New `--node-id` flag + `DASHW_NODE_ID` env. |
| [src/impl-go/console/internal/cluster/hub.go](../../src/impl-go/console/internal/cluster/hub.go) | New `HubConfig.UpstreamLabel` + `HubConfig.SelfLabel`. Package-level `buildFrame` became method `Hub.buildFrame` so it can read the labels. New `injectSourceVia([]byte, source, via string) []byte` helper that splices the two keys before the trailing `}`. |
| [src/impl-go/console/internal/cluster/handler.go](../../src/impl-go/console/internal/cluster/handler.go) | All 4 `buildFrame(...)` call sites switched to `h.hub.buildFrame(...)`. |
| [src/impl-go/console/internal/server/server.go](../../src/impl-go/console/internal/server/server.go) | Plumbs `cfg.DashdGrpcAddr` → `HubConfig.UpstreamLabel` and `cfg.NodeID` → `HubConfig.SelfLabel`. |
| [src/impl-web/console/src/api/topology-v2-types.ts](../../src/impl-web/console/src/api/topology-v2-types.ts) | New optional `source?: string` + `via?: string` on `TopologyEvent`. |
| [src/impl-web/console/src/stores/topology-v2-store.ts](../../src/impl-web/console/src/stores/topology-v2-store.ts) | New `lastSource` + `lastVia` state; updated by every `applyEvent`; exposed via `selectStreamHealth`. |
| [src/impl-web/console/src/views/topology-v2/TopologyV2View.tsx](../../src/impl-web/console/src/views/topology-v2/TopologyV2View.tsx) | ConnectionBadge shows `source → via`. InstructionBanner mentions which dashd is producing + which dashw relaying. EventTicker adds a "Source → Via" column per row. |

### 4.2 `injectSourceVia` — the byte splice

```go
func injectSourceVia(js []byte, source, via string) []byte {
    if source == "" && via == "" {
        return js                       // passthrough
    }
    if n := len(js); n < 2 || js[n-1] != '}' {
        return js                       // not a JSON object — leave alone
    }
    buf := make([]byte, 0, len(js)+len(source)+len(via)+24)
    buf = append(buf, js[:len(js)-1]...) // everything except trailing }
    if len(js) > 2 {
        buf = append(buf, ',')           // separator (skipped for empty {})
    }
    if source != "" {
        buf = append(buf, `"source":`...)
        buf = strconv.AppendQuote(buf, source) // handles escaping
    }
    if via != "" {
        if source != "" { buf = append(buf, ',') }
        buf = append(buf, `"via":`...)
        buf = strconv.AppendQuote(buf, via)
    }
    buf = append(buf, '}')
    return buf
}
```

### 4.3 Why a byte splice (not re-marshal)

The straightforward implementation would have been to add the fields
to the proto, re-marshal the event after writing them, and ship that.
We rejected it because:

- Marshal-once is **the** PE-G7 D2 optimisation (~50× CPU at high
  fan-out). Re-marshalling per fan-out would undo it.
- The fields are metadata of the wire layer, not the event data. The
  proto stays clean and re-usable for native gRPC clients that already
  know their own connection identity.
- The splice is allocation-bounded (one buffer per frame, shared
  across all subscribers) and benchmarked at <500ns per call.

---

## 5. Configuration

### 5.1 dashw flags + env vars

```bash
# Default — uses OS hostname
$ dashw

# Override per replica (recommended for production)
$ dashw --node-id dashw-replica-2

# Or via env (preferred for compose / k8s)
$ DASHW_NODE_ID=dashw-replica-2 dashw
```

### 5.2 Suggested production naming

| Environment | Pattern | Example |
|---|---|---|
| Docker Compose | `${service-name}-${ordinal}` | `dashw-0`, `dashw-1` |
| Kubernetes | StatefulSet `${pod-name}` | `dashw-replica-2` |
| Bare metal | `${hostname}` (default) | `prod-dashw-east-1` |

For `source`, the configured `--dashd-grpc <addr>` flag is used
as-is. If you want a friendly name (e.g., `dashd-1` instead of
`dashd-1.cluster.local:9443`), put the friendly name in the gRPC
endpoint — both work for client routing if DNS resolves them.

---

## 6. Operator-facing UX

### 6.1 Connection badge

Top-right of `/topology-v2`. When streaming is on, the badge now
includes a small monospace line under the status:

```
[Live ▪ cursor #42 ⤺ 0 resync]
dashd-1:9443 → edce4b15fcdc
```

A tooltip on the provenance line reads:
> Source = upstream dashd that produced this event · Via = dashw replica that relayed it

### 6.2 Instruction banner

When stream is on:
> **Live stream is ON.** Snapshot + deltas arrive via SSE from `dashd-1:9443` relayed by `edce4b15fcdc`. Click any node, appliance, or DPU to inspect. Use **Stop live** to pause without losing the cached view. Each event row shows its `source → via` path in the fourth column.

### 6.3 Event ticker

New 4th column "Source → Via" per row. Empty cells render `—` so
older events without provenance (e.g., from a downgraded dashw) are
visibly distinct.

### 6.4 Browser DevTools capture

When operators paste a stream sample into a support ticket, the
provenance is now self-contained:

```
event: peer_removed
data: {"kind":"KIND_PEER_REMOVED","event_id":17,"peer":{"node_id":"dashd-2"},
       "source":"dashd-1:9443","via":"edce4b15fcdc"}
```

A receiving engineer immediately knows:
- Which dashd authored the event (`dashd-1:9443`).
- Which dashw relayed it (`edce4b15fcdc`).
- The cursor (`17`) for replay.
- The affected entity (`dashd-2`).

---

## 7. Test strategy

| Test | What it asserts |
|---|---|
| `TestInjectSourceVia_BothLabels` | Both fields appear; original keys preserved |
| `TestInjectSourceVia_EmptyLabelsPassthrough` | No-op when both labels empty |
| `TestInjectSourceVia_OnlySource` | Only `source` appears when `via` is empty |
| `TestInjectSourceVia_QuotesEscaped` | `strconv.AppendQuote` escapes embedded quotes |
| `TestInjectSourceVia_NonObjectPassthrough` | nil / empty / `null` / arrays returned unchanged |
| `TestHubBuildFrame_StampsLabels` | End-to-end: `Hub.buildFrame` produces JSON containing both fields |

All 6 added to [src/impl-go/console/internal/cluster/source_via_test.go](../../src/impl-go/console/internal/cluster/source_via_test.go).
**dashw 8 packages green** after the change.

Live e2e: confirmed in 05-full-console fleet — `curl http://localhost:3000/api/console/topology-v2/stream` returns frames carrying `"source":"dashd-1:9443","via":"edce4b15fcdc"`.

---

## 8. Future Scopes

### 8.1 Multi-hop chains

Today `via` is a single value. If we later put a CDN edge between
dashw and the browser (e.g., for geographic locality), we'd want a
**hop chain**: `"via":["dashw-2","edge-tokyo"]`. The current single-
string field is forward-compatible — change to a JSON array when needed;
JS clients reading `health.lastVia` should fold to a join then.

### 8.2 dashd-side build-info on source

Today `source` is just the gRPC address. Operators on the receiving end
sometimes want the dashd's `version` + `build_sha` without correlating
to `cluster.nodes[]`. Future: stamp `source_version` + `source_sha` too,
sourced from the upstream stream's first `snapshot` frame (cache locally).

### 8.3 OpenTelemetry trace propagation

Embedding a `trace_id` field alongside `source`/`via` would let
operators link a specific topology event to a distributed trace span
(e.g., the `PutVnet` that caused the `KIND_PEER_UPDATED`). Needs the
broader OTel rollout first.

### 8.4 Suppression for dev mode

For local dev where there's only one of everything, the `source`/`via`
labels are noise. Add a `--quiet-provenance` flag that omits them in
single-replica deployments.

### 8.5 Per-tenant aliasing

Operators of a SaaS DashCenter may not want to leak internal hostnames
to tenant browsers. A future tenant-aware label table would map
`dashw-replica-2 → "BFF"` for tenant view while keeping the real name
for internal audit. Pairs with the multi-tenant filtering Future Scope
in [topology-streaming-design.md §11.5](topology-streaming-design.md#115-per-tenant-filtering--rbac).

### 8.6 Per-event source override

When dashw's hub aggregates multiple upstream dashd streams (Future
Scope #11.7 — multi-cluster federation), each event needs its OWN
`source` rather than the dashw's single configured upstream. The
splice point is the same; the value just needs to come from the event's
context rather than `cfg.UpstreamLabel`.
