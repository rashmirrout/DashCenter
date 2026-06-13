# PE-G7.1 — Operator Polish (`dashctl topology` + Leader Observer + Cordon Button)

> **Audience**: dashd / dashw / dashctl maintainers, SREs running the
> fleet, operators using the live topology surface from the CLI or
> the browser.
> **Scope**: three small, independent operator-facing improvements
> shipped together as a single polish pass on top of PE-G6/PE-G7.
> **Companion docs**:
> [cluster-topology-design.md](cluster-topology-design.md) (PE-G6
> ClusterService design),
> [topology-streaming-design.md](topology-streaming-design.md) (PE-G7
> production hardening + dashw multiplexer),
> [sse-event-provenance.md](sse-event-provenance.md) (PE-G7.1 SSE
> source/via stamping),
> [features.md §10](features.md#10-cluster-topology-pe-g6--pe-g7).
> **Status**: ✅ Shipped 2026-06-13.

---

## Table of contents

1. [Why this slice](#1-why-this-slice)
2. [Slice A — `dashctl topology [--follow]`](#2-slice-a--dashctl-topology---follow)
3. [Slice C — `EtcdElector` background leader observer](#3-slice-c--etcdelector-background-leader-observer)
4. [Slice D — Cordon / Uncordon button in `/topology-v2` SPA](#4-slice-d--cordon--uncordon-button-in-topology-v2-spa)
5. [Test summary](#5-test-summary)
6. [Live e2e in the 05-full-console fleet](#6-live-e2e-in-the-05-full-console-fleet)
7. [Future Scopes](#7-future-scopes)

---

## 1. Why this slice

PE-G6 landed the `ClusterService` server. PE-G7 hardened the streaming
path + introduced the dashw multiplexer + shipped the `/topology-v2`
SPA. After running both for a day in the 05-full-console fleet, three
visible operator pain points remained:

1. **No CLI parity.** Operators on a server with no browser couldn't
   inspect the live topology. The wire format was there
   (`/v1/cluster/topology` + `/v1/cluster/topology/watch`) but no
   ergonomic `dashctl` command consumed it.
2. **Followers reported `leader_id: ""`.** PE-G6 acknowledged this as
   a known limitation: `EtcdElector.LeaderID()` only knew the leader
   after the local node explicitly called `ObserveCurrentLeader`. So
   a follower's `/admin/topology` showed the cluster with no leader,
   even though one was clearly elected. Confusing for operators reading
   the JSON.
3. **No one-click cordon from the live view.** Operators inspecting a
   DPU in the `/topology-v2` drawer could see its state but had to
   leave the page (or open a terminal) to cordon it. A common, simple
   operation was buried behind a context switch.

Each fix is small enough to fit in one PR but operator-visible enough
to warrant its own design + test pass. Shipped together because
they're naturally a "live topology polish" set.

---

## 2. Slice A — `dashctl topology [--follow]`

### 2.1 Goals

- One-shot pretty-tree view of the fleet topology from the command line.
- `--follow` mode that opens the SSE stream and prints every event live.
- Match the browser's `/topology-v2` semantics: same wire format, same
  resume cursor, same opt-in ENI list.
- `-o json` / `--json` for machine consumption (scripting, pipelines).

### 2.2 Architecture

```
dashctl ──REST──► dashd /v1/cluster/topology              (one-shot)
        ──SSE───► dashd /v1/cluster/topology/watch        (--follow)
```

**dashctl talks to dashd directly, NOT through dashw.** This matches
the project's design contract: dashw is the BFF for browsers (it
multiplexes N tabs onto 1 upstream stream and adds per-IP caps,
snapshot dedup, etc.); the operator CLI doesn't need any of that and
shouldn't add latency to its own queries. CLI sessions are short,
single-stream, and authenticated at the CLI layer — exactly what dashd's
REST surface was designed for.

### 2.3 Wire types — hand-rolled, not codegen

A new section in [src/impl-go/dashctl/pkg/client/client.go](../../src/impl-go/dashctl/pkg/client/client.go)
defines `TopologySnapshot`, `TopologyEvent`, `TopologyClusterInfo`,
`TopologyAppliance`, `TopologyDpu`, `TopologyEni`, `TopologyZone`,
`TopologySummary`, `TopologyNamespaceObjectCounts`, `TopologyNotice`,
`TopologyClusterNode`, and `TopologyWatchOptions`. All have explicit
`json:"snake_case"` tags matching the protojson wire shape.

We **deliberately did not** import `gen/go/dashcenter/v1`. Reasons:

- Keeps dashctl's binary slim (no proto runtime overhead).
- Matches the existing pattern for `PutResult`, `StoredItem`, etc. —
  every other dashctl wire type is hand-rolled too.
- Decouples the CLI release cadence from the proto schema cadence:
  adding a field to the proto doesn't break older dashctls (they just
  ignore the unknown JSON keys).

### 2.4 REST client extension

[src/impl-go/dashctl/pkg/client/rest/rest.go](../../src/impl-go/dashctl/pkg/client/rest/rest.go)
gains two methods (interface-required so future gRPC backends must
implement them too):

```go
GetTopology(ctx, includeEnis bool) (*TopologySnapshot, error)
StreamTopology(ctx, opts TopologyWatchOptions) error
```

`StreamTopology` is the interesting one — it's a self-contained SSE
parser:

- Uses a **separate `http.Client`** that shares the configured TLS
  transport but has no `ResponseHeaderTimeout`. The default REST
  client's 30s ceiling would kill the long-lived stream after that.
- Sends `Last-Event-ID: N` HTTP header AND `?last_event_id=N` query
  param. Belt + suspenders for reverse proxies that strip one (some
  L7 LBs treat the header as a custom header and rewrite it).
- Parses SSE field-by-field: accumulates multi-line `data:` payloads
  until a blank line, then dispatches one event. Skips `:keepalive`
  comments and `id:`/`event:` metadata (the JSON body already carries
  the equivalent fields).
- Invokes `opts.OnEvent(ev)` for each parsed event. A non-nil return
  from `OnEvent` is a sentinel — `StreamTopology` exits cleanly with
  that error. Useful for "stop after first KIND_LEADER_CHANGED" style
  consumers.

### 2.5 Cobra command surface

[src/impl-go/dashctl/internal/cmd/topology.go](../../src/impl-go/dashctl/internal/cmd/topology.go):

```
dashctl topology                                 # pretty tree (default)
dashctl topology -o json                         # raw JSON (uses --output)
dashctl topology --json                          # raw JSON (per-cmd flag)
dashctl topology --include-enis                  # include per-DPU ENI list
dashctl topology --follow                        # SSE stream until Ctrl-C
dashctl topology --follow --since-id 42          # resume from cursor 42
dashctl topology --follow --json                 # one JSON object per line
```

### 2.6 Pretty tree renderer

The output is a 3-section view:

```
CLUSTER  nodes=3  leader=dashd-3  status=healthy
   dashd-1   rest=:8443  grpc=:9443  ver=0.2.0-phase1b
   dashd-2   rest=:8443  grpc=:9443  ver=0.2.0-phase1b
 * dashd-3   rest=:8443  grpc=:9443  ver=0.2.0-phase1b

SUMMARY  appliances=5  dpus=10  enis=41  healthy=10  degraded=0  offline=0  cordoned=0

APPLIANCES
  zone=us-west-2a
    appliance-1  tier=gold  dpus=2
      dpu-sim-01   slot=0  state=DPU_STATE_UP  enis=4
      dpu-sim-02   slot=1  state=DPU_STATE_UP  enis=4
    appliance-2  tier=silver  dpus=2
      dpu-sim-03   slot=0  state=DPU_STATE_UP  enis=4
      dpu-sim-04   slot=1  state=DPU_STATE_UP  enis=4
  ...

OBJECTS (per namespace)
  default    vnets=14  enis=41  mappings=40  acls=18  routes=17  ha_sets=4  tunnels=6
  edge       vnets=2  enis=3  mappings=3  acls=1  routes=1  ha_sets=0  tunnels=0
```

The leading `*` marker on the leader node mirrors the browser's
Crown icon. Cordoned DPUs get a trailing `CORDONED` marker.

### 2.7 `--follow` event renderer

Each event prints on one line in the form:

```
#42      2026-06-13T18:00:49Z  PEER_REMOVED     peer=dashd-2 leader=false addr=:8443
#43      2026-06-13T18:00:51Z  KEEPALIVE
#44      2026-06-13T18:00:55Z  DPU_STATE        dpu=dpu-sim-03 state=DPU_STATE_DOWN enis=4
#45      2026-06-13T18:01:02Z  DROPPED          slow subscriber dropped=4
#46      2026-06-13T18:01:05Z  RESYNC           cursor predates ring; refetch GetTopology current_id=46
```

In `--json` mode, every event is one JSON object per line (jq-friendly).

---

## 3. Slice C — `EtcdElector` background leader observer

### 3.1 Problem (PE-G6 known limitation)

`EtcdElector.LeaderID()` returned an empty string on follower nodes
until somebody called `ObserveCurrentLeader(ctx)` explicitly. The
issue:

- The campaign loop only updates `currentLeader` to `e.nodeID` **when
  this node wins** the campaign.
- `ObserveCurrentLeader` is a one-shot lookup — it caches the value
  but doesn't keep it fresh.
- Operators reading a follower's `/admin/topology` or
  `ClusterService.GetTopology` saw `"leader_id": ""` despite a
  perfectly healthy elected leader.

This wasn't a correctness bug (the leader was elected, all writes
went through it) but it was a visible UX wart — operators couldn't
tell from a follower's perspective who the leader was.

### 3.2 Fix

Added a background `observeLoop(ctx)` goroutine started in
`NewEtcdElector`:

```go
func (e *EtcdElector) observeLoop(ctx context.Context) {
    backoff := 200 * time.Millisecond
    const maxBackoff = 5 * time.Second
    for {
        if ctx.Err() != nil { return }
        ch := e.election.Observe(ctx)         // etcd v3 leader-change stream
        for resp := range ch {
            leader := ""
            if len(resp.Kvs) > 0 {
                leader = string(resp.Kvs[0].Value)
            }
            e.mu.Lock()
            e.currentLeader = leader
            e.mu.Unlock()
            backoff = 200 * time.Millisecond  // healthy stream resets backoff
        }
        // Channel closed; re-observe with exponential backoff.
        if ctx.Err() != nil { return }
        select {
        case <-ctx.Done(): return
        case <-time.After(backoff):
        }
        if backoff < maxBackoff { backoff *= 2 }
    }
}
```

`concurrency.Election.Observe(ctx)` is a streaming watch on the
leader key — it emits the current value whenever it changes
(campaign / resign / lease expiry). With this loop running, the
follower's `currentLeader` cache is always within ~50ms of the actual
elected leader.

A new `observeCancel context.CancelFunc` field on `EtcdElector` lets
`Close()` stop the loop deterministically.

### 3.3 Capped exponential backoff

Why backoff at all? `Observe` streams die on transient etcd network
blips. Without backoff, a flapping connection would burn CPU
re-observing in a tight loop. The 200ms → 5s cap matches the existing
pattern used by the dashw Hub's upstream reconnect (PE-G7).

### 3.4 What changed in observable behaviour

Before:

```bash
$ curl -s http://localhost:27453/admin/topology | jq .cluster.leader_id
""
```

After:

```bash
$ curl -s http://localhost:27453/admin/topology | jq .cluster.leader_id
"dashd-3"
```

…even from a follower node. `is_leader: true` is correctly set on the
elected node from every observer's perspective.

### 3.5 Tests

Both new tests live in [src/impl-go/dashd/internal/ha/leader/etcd_test.go](../../src/impl-go/dashd/internal/ha/leader/etcd_test.go):

| Test | Scenario | Asserts |
|---|---|---|
| `TestLeaderObserver_FollowerSeesLeaderWithoutExplicitCall` | Follower never calls `ObserveCurrentLeader`. Another node wins the campaign. | Follower's `LeaderID()` returns `"node-leader"` within 5s. |
| `TestLeaderObserver_FollowerSeesLeaderHandover` | `nodeA` wins, follower observes `node-a`. `nodeA.Close()` (resigns). `nodeB` wins. | Follower's `LeaderID()` flips `node-a` → `node-b` within 5s. |

Both pass against the embedded etcd test fixture; ~1.2s total
runtime.

---

## 4. Slice D — Cordon / Uncordon button in `/topology-v2` SPA

### 4.1 Goal

When an operator clicks a DPU card on `/topology-v2`, the inspector
drawer should let them cordon (stop scheduling new workloads) or
uncordon the DPU with one click — without context-switching to
`dashctl` or another browser page.

### 4.2 UX contract

- Context-aware button: amber **Cordon DPU** when uncordoned,
  emerald **Uncordon DPU** when cordoned.
- Button disabled + spinner while the POST is in flight.
- Inline result banner (success / error) under the button. Success
  message explicitly tells the operator the change is *pending* until
  the next `dpu_state` event arrives.
- The drawer does **NOT** optimistically update — see §4.4 for why.

### 4.3 Wire path

```
Browser ──POST /api/v1/inventory/{id}/cordon──► dashw reverse proxy ──► dashd /v1/inventory/{id}/cordon
                                                       ▲
                                                       │
                                                  (no new endpoint;
                                                   PB-1 path reused)
```

The browser **still never talks to dashd directly**. dashw's existing
`/api/v1/*` → `/v1/*` reverse proxy carries the POST through with no
new routes, no new auth wiring. The reason endpoint
`/api/v1/inventory/{id}/cordon` is the same path PB-1 wired in 2026-Q1.

Request body:
```json
{"reason": "operator action from /topology-v2"}
```

The reason is recorded by dashd's audit chain (PD-G4 denial auditing
+ PD-G3 audit log) — so every cordon/uncordon click leaves an audit
row tied to the auth subject.

### 4.4 Non-optimistic update — why

The button does **not** flip the cordoned flag locally on click. It
waits for dashd to broadcast a `KIND_DPU_STATE` event back through
the broadcaster → dashw Hub → SPA reducer chain.

Rationale:

- **Single source of truth.** dashd is the authority. If for some
  reason the cordon fails server-side (e.g., placement engine refuses
  due to in-flight migrations), the optimistic flip would lie to the
  operator.
- **Free verification.** When the operator sees the card turn amber,
  they know the change *actually committed* — not just that the POST
  was 200.
- **Snapshot fallback.** If streaming is OFF, the 30s snapshot
  refresh catches up. Operators using the page without live stream
  still see the change, just slower.

The trade-off is a 100-200ms delay between click and visual feedback
when streaming is on. Acceptable — the inline "Cordoned. dashd will
stop scheduling…" banner appears immediately so the operator knows
the click registered.

### 4.5 Implementation

[src/impl-web/console/src/views/topology-v2/TopologyV2View.tsx](../../src/impl-web/console/src/views/topology-v2/TopologyV2View.tsx):

```tsx
function DpuActions({ dpuId, cordoned }: { dpuId: string; cordoned: boolean }) {
  const [inflight, setInflight] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; msg: string } | null>(null);
  const verb: 'cordon' | 'uncordon' = cordoned ? 'uncordon' : 'cordon';
  const onClick = async () => {
    setInflight(true);
    setResult(null);
    try {
      const res = await fetch(`/api/v1/inventory/${encodeURIComponent(dpuId)}/${verb}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ reason: 'operator action from /topology-v2' }),
      });
      if (!res.ok) { /* … */ return; }
      setResult({ ok: true, msg: /* "Cordoned. dashd will stop…" / "Uncordoned…" */ });
    } catch (err) { /* … */ }
    finally { setInflight(false); }
  };
  // … render
}
```

The component is mounted from `InspectorDrawer` only when
`selectedKind === 'dpu'`. No new state in the Zustand store — the
button reads `cordoned` from the existing entity and triggers a fresh
event flow rather than mutating.

### 4.6 No new SPA tests required

The component is a UI bridge over an existing, well-tested REST
endpoint. The 234 existing SPA tests cover the store reducer +
event flow + page lifecycle. Adding a UT for the button would assert
"calls fetch with these args" which gives near-zero confidence vs.
the live e2e verification we did (§6.3).

If we add complex state (e.g., a confirmation dialog, batch cordon),
that earns its own UT then.

---

## 5. Test summary

| Slice | Tests added | Package result |
|---|---|---|
| A (dashctl) | 5 cmd-layer + 4 REST-wire = **9 new** | **8/8 dashctl packages green** |
| C (leader observer) | 2 new etcd-backed | **26/26 dashd packages green** |
| D (cordon button) | 0 (relies on existing 234 + live e2e) | **234/234 SPA tests green** |

Zero regressions across all three modules.

---

## 6. Live e2e in the 05-full-console fleet

All three slices verified end-to-end:

### 6.1 dashctl topology

```bash
$ dashctl --endpoint http://localhost:28443 --admin-endpoint http://localhost:27443 topology
CLUSTER  nodes=3  leader=dashd-3  status=healthy
   dashd-1   rest=:8443  grpc=:9443  ver=0.2.0-phase1b
   dashd-2   rest=:8443  grpc=:9443  ver=0.2.0-phase1b
 * dashd-3   rest=:8443  grpc=:9443  ver=0.2.0-phase1b
(… snipped …)
```

### 6.2 Leader observer

```bash
# dashd-1 (follower)
$ curl -s http://localhost:27443/admin/topology | jq '.cluster.leader_id, .cluster.nodes[] | select(.is_leader).node_id'
"dashd-3"
"dashd-3"

# dashd-2 (follower) — same answer
$ curl -s http://localhost:27463/admin/topology | jq '.cluster.leader_id'
"dashd-3"
```

Before the fix, both returned `""`.

### 6.3 Cordon button

Manual: opened `http://localhost:3000/topology-v2`, clicked the
appliance-2 / dpu-sim-04 card → inspector drawer → "Cordon DPU"
button → amber → `result.ok=true` banner with "Cordoned. dashd will
stop scheduling…" → DPU card turned amber + `CORDONED` label within
~150ms via the live stream's `KIND_DPU_STATE` event.

---

## 7. Future Scopes

### 7.1 dashctl topology — `--watch-resource`

Today `--follow` shows every event. Operators sometimes want only
events about a specific DPU. Future: `--watch-resource dpu:dpu-sim-03`
applies a client-side filter on the parsed event before printing.

### 7.2 dashctl topology — `--graph` ASCII tree

The 3-section view is fine for inventory but not for relationships
(e.g., "which appliances host the most cordoned DPUs?"). A future
`--graph` mode could render a tiny ASCII / `graph-easy`-style tree.

### 7.3 dashctl topology — `--diff` snapshot comparison

`dashctl topology --diff snapshot.json` would compare the current live
topology against a saved JSON snapshot and print the deltas. Pairs well
with the existing deterministic-sort guarantee from PE-G6.

### 7.4 Leader observer — gRPC-based observation for non-etcd backends

The observer goroutine uses `concurrency.Election.Observe` which is
etcd-specific. When we add a Raft / Consul / Zookeeper elector
backend, each will need its own equivalent. The `Elector` interface
should grow an `ObserveLeaderID() <-chan string` method that every
backend implements; the existing `LeaderID()` becomes a thin wrapper.

### 7.5 Leader observer — observer health metric

Add `dashd_cluster_leader_observer_stream_alive` (gauge 0/1) so
operators can alert on the observer dying silently. Today a failed
observer is invisible until somebody notices `LeaderID()` is stale.

### 7.6 Cordon button — drain action

Cordon stops *new* placements; **drain** also rehomes existing ENIs
off the DPU. Today drain is `POST /v1/inventory/{id}/drain` (PC-G7).
Future: add a "Drain DPU" button next to Cordon with a confirmation
dialog ("Will rehome N ENIs to ${count} other DPUs"). Needs a
preview-of-impact UX before committing.

### 7.7 Cordon button — bulk action

Today the operator must click each DPU individually. A future
multi-select on the appliances grid → "Cordon selected" bulk action
would speed up zone-wide maintenance prep. Needs a batch POST endpoint
(today's `/v1/inventory/{id}/cordon` is single-DPU).

### 7.8 Cordon button — scheduled cordon (maintenance window)

"Cordon dpu-sim-04 at 02:00 UTC for the next 4h" → integration with
the upcoming `internal/maintenance/` subsystem. Out of scope for
PE-G7.1 but pairs with the Push notification Future Scope in
[topology-streaming-design.md §11.6](topology-streaming-design.md#116-push-notification-on-critical-events).

### 7.9 Cordon button — optimistic mode toggle

For operators on slow networks who want immediate visual feedback,
add a "Optimistic UI" preference (localStorage). When on, the button
flips the cordoned flag locally on click and reverts if the event
doesn't arrive within 5s.

### 7.10 dashctl ↔ SPA divergence

Today the dashctl `--follow` renderer and the SPA's EventTicker have
slightly different formats for the same data. A future cleanup could
extract a shared "topology event one-liner" formatter (Go template ↔
TS template) so both stay aligned automatically as new event kinds
are added.
