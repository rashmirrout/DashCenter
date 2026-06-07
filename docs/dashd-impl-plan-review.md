# Architectural Review — `dashd` Implementation Plans

> Reviewer's lens: a distributed-systems architect with hands-on Kubernetes /
> kubectl background. The bar I'm holding the plans to is "could a team
> ship this and run it for years without rewriting it?"
> Documents reviewed:
> [`specs/Impl-Plan/impl-plan-basic.md`](../specs/Impl-Plan/impl-plan-basic.md)
> (1,535 lines) + [`specs/Impl-Plan/impl-plan-advanced.md`](../specs/Impl-Plan/impl-plan-advanced.md)
> (1,884 lines) +
> [`specs/LLD/dashd.md`](../specs/LLD/dashd.md) +
> [`proto/dashcenter/v1/`](../proto/dashcenter/v1/).
> Date: 2026-06-07.

## TL;DR

These are **unusually high-quality** implementation plans. The structure
(per-package contracts, table-driven tests called out by name, exit
gates with copy-pasteable verification scripts, an explicit "rules"
section that pins dependency direction) is what you'd expect from a
seasoned platform team — not a first draft. The Phase 1 / Phase 2 split
is correctly drawn: the basic plan ships a usable single-node controller;
the advanced plan layers on the things you'd want before you ran
multi-tenant or production.

**What I'd change:** seven design concerns (one is structural and worth
fixing now, the rest are calibration), nine smaller improvements, three
documentation cleanups. None of them invalidate the overall shape.

The hardest issue is **§A1 (the namespace breaking change)** — Phase 2
currently mutates the `store.ObjectKey` struct, which forces a forklift
on every consumer. I show a non-breaking alternative.

The most important non-obvious gap is **§A4 (the missing "controller
runtime" abstraction)** — multiple modules end up re-implementing the
same control-loop / informer / work-queue / leader-gate pattern. Picking
that abstraction now (≈400 LoC) saves an order of magnitude of
boilerplate in P2-M6/M7/M8.

---

## 1. What the plans get right

These are not nits — they're decisions to *keep* as the plans evolve:

| Decision | Why it's correct |
|---|---|
| **Pure `placement` package** with explicit "no I/O, no goroutines, no global state" rule | Mirrors Kubernetes `scheduler/framework` — placement is the most-tested, easiest-to-reason-about layer when it's pure. The plan even guards this with a check in §4 of the basic plan. |
| **`reconciler` never calls `dashapi` directly** — it only signals dispatch | This is the same separation as `kube-controller-manager` reconcile loops + `kubelet` apply. Without it, you cannot rate-limit / quarantine per DPU. |
| **One worker goroutine per DPU**, capacity-1 inbox = coalescing | Exactly the pattern in client-go's `workqueue`. The plan correctly calls out that 100 Syncs collapse to 1 reconcile. |
| **Optimistic concurrency via `expected_generation`** | The right call vs. distributed locks. Etcd's `ModRevision` for the etcd backend (§5 of advanced) is the *correct* choice — it's globally monotonic and free. |
| **`snapshot-first` semantics on `Subscribe` + `Watch`** | This is the single most-overlooked thing in informer designs. The plan mandates it for both the store's `Watch()` and the dispatch pump. Reconnect re-syncs without races. ✓ |
| **Leader election as a *gate* around the reconciler**, not a permission check on every RPC | Right: followers can serve reads (etcd is strongly consistent) but can't run reconcile. The HA loop pattern in §6 of the advanced plan is textbook. |
| **Dependency-tier ordering** for Apply vs Delete (§10 of basic) | Mirrors how `kubectl apply` orders CRDs / namespaces / RBAC before workloads. Without tiers, you get spurious failures on a fresh DPU. |
| **Exit criteria as copy-pasteable bash** | Every plan should do this. Without runnable acceptance gates, "Phase 1 is done" becomes opinion. |
| **Per-module proto contracts + test names enumerated** | An engineer can `go test ./...` and have the right assertions to write before reading any other code. |
| **Explicit list of what the design does NOT do** in both plans | This is what saves PR review cycles. ✓ |
| **Conventions enforced across the proto surface** (every `Put*` has `expected_generation`, every spec has `namespace`, every mutation emits an `AuditEntry`, enums are closed-additive) | This is the Kubernetes API conventions playbook applied correctly. |

If we shipped exactly Phase 1 as written, we'd already have a perfectly
defensible controller. The Phase 2 plan turns it into a production
system. Everything below is *delta* on top of that.

---

## A. Design concerns (impact-ordered)

### A1 — The namespace introduction in P2-M3 is a forklift breaking change ⚠

`impl-plan-advanced.md` §7 introduces multi-tenancy by **mutating the
Phase 1 `store.ObjectKey`**:

```go
// Before (Phase 1)
type ObjectKey struct {
    Kind string
    Name string
}

// After (Phase 2-M3)
type ObjectKey struct {
    Namespace string   // NEW
    Kind      string
    Name      string
}
```

…with a follow-on: `store.List(ctx, namespace, kind string)` (signature
change), file-store path layout change, etcd key layout change, and a
migration tool to move `<state_dir>/<kind>/...` → `<state_dir>/default/<kind>/...`.

**Why this is bad:** every package that touches the store
(`reconciler/`, `dispatch/`, `placement/`, `inventory/`, `server/grpc/`,
`server/rest/`, `cmd/dashd/`) gets a coordinated edit. The advanced plan
explicitly lists this in its "Files modified in Phase 2" table — that's
8+ files whose tests have to be rewritten in lockstep.

The Phase 1 plan even *says* the contract is forward-compatible
("`internal/store/store.go` — No interface change (Phase 1 contract is
forward-compatible)") — but the M3 section then breaks that promise.

**Fix (non-breaking):** introduce namespace as a Phase 1 field with
default `"default"`. Add it to `ObjectKey` from day one. Persist it from
day one. Make `Validate()` reject the value `""`. The file store layout
is `<state_dir>/<namespace>/<kind>/<name>.json` from day one. Phase 1
clients writing without a namespace just default to `"default"` —
exactly the Kubernetes pattern.

This costs Phase 1 a single line of struct change and ~5 lines of
default-handling in the REST gateway. Phase 2-M3 then becomes "enforce
cross-namespace validation" instead of "rewrite the storage layout".

> **Recommendation:** move "namespace is a field on every key, defaulting
> to `default`" into Phase 1, P0. Make the Phase 2 work
> *behavior* (validation, RBAC binding) rather than *structure*.

### A2 — Strategy: "Phase 1 then forklift to etcd" is the wrong default ⚠

Phase 1 mandates `Storage.Backend == "file"`, validated to *reject*
`etcd` at parse time:

```
* Storage.Backend == "etcd" → error: "storage.backend=etcd is not supported in Phase 1"
```

Phase 2 then introduces the etcd backend with a different snapshot-first
implementation, a different concurrency model (etcd `ModRevision` vs
in-memory generation counter), and a different watcher contract (etcd
emits a `Compacted` error you must recover from; the file store never
does).

**Why this is a concern:** any code written against the Phase 1
file-store contract that assumes "generation values are stable and small
integers", "Watch never returns Compacted", or "channel is buffered 64"
will subtly break when swapped. The "the interface is forward-compatible"
claim isn't fully true once etcd's failure modes leak in.

**Fix:** add to the Phase 1 `DesiredStore` *contract* (godoc on the
interface) the **strictest** semantics the backend must provide:

```go
// Watch returns a channel receiving a snapshot of current state
// followed by live mutations. The channel MAY be closed without warning
// (compaction, store restart, etc.); the caller MUST be prepared to
// re-Subscribe.
//
// Generation values are strictly monotonic *per key* and globally
// unique only for the etcd backend; callers SHOULD NOT compare
// generations across keys.
```

This documents the behavior we want both backends to enforce, and forces
the file backend to *also* simulate compaction-style closure (e.g. on
internal slow-subscriber drop). Switching backends becomes mechanical.

**Better still:** ship both backends in Phase 1. The etcd backend
without HA is a 1-day add on top of M1 (you skip M2 leader election;
the single-node controller is always leader). This buys you:
- An integration-test target (`docker-compose up etcd`) for free.
- A way to dogfood the contract before phase 2 starts.

### A3 — The "what runs only on the leader" set is implicit ⚠

The advanced plan says:

> Start leader-only goroutines: `mgr.Start(leaderCtx)`, `pumpSet.Start(...)`, `rec.Run(leaderCtx)`

But there are many *other* things in Phase 2 that should also only run
on the leader — and the plan doesn't enumerate them:

- The **capacity tracker's Recount on Put** (P2-M4) — only the leader writes.
- The **audit log writer** (P2-M10) — must be single-writer.
- The **HA orchestrator's switchover/failover** (P2-M6) — only one entity should drive HA state.
- The **migration coordinator** (P2-M7) — same.
- The **drain coordinator** (P2-M8) — same.
- The **counter polling per DPU** (added to dispatch in P2-M10) — same.

Followers are described only as "serve read-only `Get` / `List`". But
what about server-streaming `WatchEvents`, `GetDpuStatus`,
`StreamMigrationSession`? Those are reads, but they piggyback on
in-memory broadcasters that only exist if the orchestrator/migration
coordinator is *running locally*. A follower would return empty streams.

**Fix:** add a single "**Leader-Only Components**" table to the Phase 2
plan, listing every package and whether it runs on leader, follower, or
both. Refuse `git push` if any new package isn't classified. Mirror
this in the gRPC interceptor (one decorator: `@RequiresLeader`,
`@ReadOnly`).

```go
// In server/grpc/interceptors.go (Phase 2):
type leaderRole int
const (
    leaderOnly leaderRole = iota // mutating + orchestration
    leaderOrFollower             // pure read
    followerStream               // read but requires local cache; reject if no broadcaster
)

var rpcRoles = map[string]leaderRole{
    "/dashcenter.v1.ControlPlane/PutEni":               leaderOnly,
    "/dashcenter.v1.ControlPlane/GetEni":               leaderOrFollower,
    "/dashcenter.v1.HaService/TriggerSwitchover":       leaderOnly,
    "/dashcenter.v1.MigrationService/AdvanceMigrationPhase": leaderOnly,
    "/dashcenter.v1.ObservabilityService/WatchEvents":  followerStream,
    // ...
}
```

This eliminates 30+ lines of repeated `if !s.elector.IsLeader() { ... }`
checks scattered across handlers.

### A4 — Missing: a "controller-runtime" abstraction ⚠ (biggest non-obvious gap)

The dispatch/reconciler design in Phase 1 is a small handcrafted
controller. By Phase 2 we have:

- HA orchestrator (subscribes to `ha_scope_state` events, runs state machine, fans out events)
- Migration coordinator (loads sessions from store, advances phases, emits events)
- Drain coordinator (lists ENIs, spawns parallel migrations, streams progress)
- Audit log writer (subscribes to RPC interceptor events, appends to log)
- Capacity tracker (recomputes on store changes)
- Counter poller (per-DPU, periodic)

**Every one of these reimplements the same pattern**: watch a source
(store, obs cache, time), maintain a local state, emit to a fan-out,
respect leader/non-leader, handle graceful shutdown, expose metrics for
queue depth and event lag.

In Kubernetes, this is **controller-runtime**:
- `Source` (a watch on something — store, informer, channel, ticker)
- `Predicate` (filter)
- `Handler` (enqueues work to a typed work queue)
- `Reconciler` (the pure function that goes from key → desired state)
- `Manager` (knows the leader status, starts/stops controllers atomically)

**Recommendation:** add an `internal/controller/` package in Phase 1
that codifies this:

```go
package controller

// Reconciler is the function called for each work-queue item.
type Reconciler func(ctx context.Context, key Key) (Result, error)

// Result lets the reconciler request a retry.
type Result struct {
    RequeueAfter time.Duration // 0 = do not requeue
}

// Source is anything that emits Keys onto a workqueue.
type Source interface {
    Start(ctx context.Context, queue Workqueue) error
}

// Controller wires a Source + Reconciler with a workqueue and leader gate.
type Controller struct {
    Name        string
    Sources     []Source
    Reconciler  Reconciler
    Workers     int          // default 1
    RateLimiter ratelimit.Limiter
    LeaderOnly  bool
}

func (c *Controller) Run(ctx context.Context, elector leader.Elector) error
```

Phase 1's `dispatch.Manager` and `reconciler.Reconciler` become *one
instance* of this abstraction (Source = store.Watch + dirty channel +
ticker; Reconciler = the existing reconcilePass). Phase 2's M6/M7/M8
each become *one more instance* — not 400 LoC of bespoke loop each.

The cost is ~400 LoC + tests in Phase 1. The benefit by end of Phase 2
is **at least 2,000 LoC of duplicated control-loop boilerplate avoided**,
plus uniform metrics, plus a single place to fix bugs in shutdown
sequencing.

This is the single highest-leverage change I'd make to the plans.

### A5 — Saga is added in P2-M12 (last), but `ApplyBatch` is part of `ControlPlane` from Phase 1 ⚠

The Phase 1 plan says `ControlPlane` is implemented; the Phase 2 plan
says "saga-backed `ApplyBatch`" is M12 (last). What happens in the gap?

If Phase 1 ships `ApplyBatch` without saga, partial failures leave the
fleet in an inconsistent state — and clients have no way to detect it
short of comparing pre/post `Get` for every key. That's worse than
returning `Unimplemented`.

**Fix:** explicitly stub `ApplyBatch` as `Unimplemented` in Phase 1
(it's in the basic plan's "explicitly does NOT deliver" list already —
just make sure §14 actually stubs it, not silently implements it
non-transactionally). Don't promise it from REST either.

The same concern applies to `Reconcile` (single-key forced reconcile)
in P1 — that one is safe to implement as "trigger the per-DPU sync, no
transactional guarantee".

### A6 — Observability is afterthought-shaped ⚠

Phase 1 mentions `log/slog` and the admin HTTP `/admin/health`. Phase 2
mentions `audit_v2` capability and the audit log module — but neither
plan calls out:

| Telemetry primitive | Where it lives | Missing? |
|---|---|---|
| **Prometheus metrics** (or OpenTelemetry) | global registry | ❌ no plan |
| **gRPC interceptor for latency histograms** | server/grpc | ❌ |
| **Per-DPU worker metrics** (apply rate, error rate, queue depth) | dispatch | ❌ |
| **Reconcile lag** (time from store.Put to obs reflect) | reconciler | ❌ |
| **HA state metrics** (leader since, last election ts) | ha/leader | ❌ |
| **Trace context propagation** (W3C tracestate / OTel) | every interceptor | ❌ |

Without these, you cannot debug a misbehaving Phase 2 deployment. The
audit log answers "who did what when" but not "is the reconciler keeping
up". You'll find this out three months after first deployment.

**Fix:** add to **Phase 1 P0** (hardening): a single
`internal/metrics/` package that exposes a Prometheus registry,
registers gRPC server interceptors (latency / total / in-flight), and
defines counters/gauges for:

- `dashd_reconcile_total{dpu, outcome}`
- `dashd_apply_total{dpu, kind, outcome}`
- `dashd_apply_duration_seconds{dpu, kind}` (histogram)
- `dashd_dpu_state{dpu, state}` (gauge)
- `dashd_store_objects{kind}` (gauge)
- `dashd_reconcile_lag_seconds{dpu}` (gauge — push from subscribe pump)
- `dashd_leader_role` (gauge: 1=leader, 0=follower)
- `dashd_subscribe_events_dropped_total{dpu, reason}`

Expose on `:7443/metrics` (same as the admin HTTP). Once you have these,
*everything else gets easier*: SLOs, alerting, capacity planning,
debugging.

### A7 — Auth design needs sharpening (P2-M9) ⚠

Phase 2-M9 says "token-based RBAC (viewer/operator/admin)" — but doesn't
specify:

- **Token format** — opaque vs JWT?
- **Token validation** — local secret? Sidecar? PSP?
- **Token refresh / rotation** — how?
- **Three roles, but how do they map to namespaces?** Does "operator"
  mean "can write in any namespace" or "can write in the namespaces
  associated with their token"? The plan doesn't say.
- **mTLS vs token** — both? either? both required for some RPCs?
- **RBAC enforcement model** — is it per-RPC (table-based, like A3)
  or per-resource (annotation-based, like Kubernetes RBAC)?

Three-role static RBAC is well-known to be insufficient in production
(every customer asks for "read namespace X, write namespace Y" within
6 weeks). The fix isn't to design full OIDC/Casbin in Phase 2 — but
**the auth interceptor's interface should be designed for it from day
one**:

```go
type Subject struct {
    Principal string            // user or service account
    Roles     []string
    Namespaces []string          // subject scope (empty = all)
    Attributes map[string]string // for future ABAC
}

type Authorizer interface {
    Authorize(ctx context.Context, sub Subject, verb, namespace, kind string) error
}
```

Phase 2-M9 implements `StaticTokenAuthorizer` with viewer/operator/admin.
External operators replace with `OIDCAuthorizer` etc. by config-swap.

**Recommendation:** specify the `Authorizer` interface in §M9; mark
OIDC/AAD as "stub interface only — production replaces via
implementation swap" (which the plan already says — just make sure the
*interface* survives the swap, not just the package).

---

## B. Smaller improvements (numbered for PR commentary)

### B1 — Two persistent stores, two `expected_generation` semantics ✏

Phase 1 file store: generation = "monotonic per key, starts at 1".
Phase 2 etcd store: generation = "etcd `ModRevision` — monotonic
globally, may jump by large amounts".

Clients comparing generations across keys (e.g. for "show me everything
modified since revision N") will break when switching backends.

**Fix:** add to the `StoredSpec` struct a separate `EtcdRevision int64`
that's only populated by the etcd backend; deprecate cross-key
generation comparisons; clearly mark `Generation` as a *per-key
opaque token*.

### B2 — Watch channel back-pressure: file store drops, etcd watch needs explicit re-list ✏

File store says "dropped — subscriber too slow; they re-read on next
tick". Etcd watch can return `ErrCompacted`. Phase 2 reconciler code
should handle both uniformly — but Phase 1 introduces the dropping
semantic without surfacing it (no log, no counter).

**Fix:** when a subscriber would-be-dropped, also push a
`DesiredEvent{Type: EventResync}` (sentinel) so the consumer knows it
needs to re-list. Currently the consumer has no signal at all.

### B3 — The `dirty` channel is shared between subscribe pump and dispatch — coupling ✏

`subscribe.Pump` writes to `dirty <- dpuID`; `reconciler` reads it and
calls `mgr.Sync(dpuID)`. This works in Phase 1 but couples three
packages through one channel.

**Better:** introduce a small `internal/eventbus/` (or fold into the
controller-runtime A4) that fans out typed events:
- `DpuObjectChanged{dpuID, kind, key}`
- `DesiredSpecChanged{key}`
- `DpuStateChanged{dpuID, oldState, newState}`

This is the same pattern as the Kubernetes informer's shared event
multiplexer, and it scales to Phase 2's audit log, capacity tracker,
HA orchestrator — all of which want to *observe* the same events
without being wired together pairwise.

### B4 — `placement.Resolve` rebuilds `DesiredSpecs` on every reconcile ✏

The plan loads *all* specs from the store on every `reconcilePass`. For
a fleet with 10k ENIs this is wasteful (parse 10k JSON files per
reconcile per DPU).

**Fix:** add `placement.Snapshot` cached in `model/`, refreshed by the
reconciler whenever a `DesiredEvent` arrives. Per-DPU
`Resolve(dpuID, snapshot, inv)` then becomes O(N_eni_on_dpu) instead of
O(N_total_specs).

This is the equivalent of Kubernetes' shared informer cache — the
plans should have it from Phase 1.

### B5 — Tier 5 (HA + outbound port map) applied last is *probably* wrong ✏

The tier table puts `ha_set`, `ha_scope`, `ha_scope_config` at tier 5
(applied after `vnet_mapping` / `acl_in` / `route_rule`). But HA
configuration on a DPU usually has to be set up **before** ENIs that
should participate (so that on first ENI creation, the DPU already
knows its HA peer).

Worth a re-check against the upstream LLD/HLD; the dependency may flow
the other way.

### B6 — Migration `AdvanceMigrationPhase` advances one step but doesn't *retry* ✏

Phase 2-M7 says: "Run the phase action … Don't persist on error —
caller can retry." But the same plan says migrations survive restart
("Restart dashd mid-migration: session recovered from store; can
resume"). If `dashd` crashes between *executing* the side effect
(DPU calls) and *persisting* the phase advance, the session resumes in
the old phase but the DPU is already in the new state.

**Fix:** the phase-action must be either:
- **Idempotent on retry** (the plan should document this for each phase action), or
- **Persisted before execution** as `phase=X_IN_PROGRESS`, then transitioned to `X` on success / `X_FAILED` on error.

The current plan implies the first but doesn't enforce it; missing
idempotency is the #1 source of migration bugs.

### B7 — `Saga` rollback is described, but the saga compensation order is not ✏

P2-M12 says `ApplyBatch` is saga-backed, but the compensation logic
("if step 5 fails, undo 1-4") needs:
- **Compensation tier ordering** (delete in reverse of apply order)
- **Best-effort vs. guaranteed compensation** (if a Delete fails, do you retry forever, give up, or mark the saga "stuck for operator"?)
- **Compensation logging** (audit log must distinguish "user did X" from "saga undid X")

Without these, you have a saga **name** but not a saga **algorithm**.

### B8 — Drain timeout is per-DPU but migration timeout is per-phase ✏

`OperationsService.DrainDpu` takes `req.Timeout`; `MigrationService.AdvanceMigrationPhase` takes `req.DrainTimeoutSec` (only for the FLOW_DRAIN phase). What happens when `drain.Timeout` is reached but 3 of 5 ENIs are mid-migration?

Spec needs to say:
- Drain stops accepting new migrations.
- In-flight migrations get the *remaining* time as their per-phase timeout.
- Operator gets a `DrainProgress{state: TIMEOUT, succeeded:[...], inflight:[...], queued:[...]}` so they can decide rollback.

### B9 — REST gateway design isn't specified ✏

The plan repeatedly mentions REST on `:8443` but never specifies:
- Hand-written handlers or `grpc-gateway`?
- URL conventions (`/v1/vnets/{name}` looks REST-like — but is it singular/plural? `/v1/vnet/{name}` or `/v1/vnets/{name}`? Picks one and commits).
- Body format: JSON only, or protojson? (They differ on enum casing!)
- Field naming: snake_case (Go) or camelCase (REST)?

**Recommendation:** use `grpc-gateway` with `proto/dashcenter/v1/*.proto`
annotated with HTTP rules. Zero hand-written handlers; binding rules
are version-controlled with the proto. This is what Kubernetes does
(`apiregistration.k8s.io`) and what every modern gRPC service does.

---

## C. Documentation cleanups

### C1 — `internal/README.md` (old) is still referenced

The retire-list in §5 of the basic plan correctly identifies the old
Redis scaffold. But the **current `src/impl-go/dashd/README.md`** (top
level) *still describes* that old design (15 packages, Redis store,
Raft+memberlist, WebSocket, telemetry). Anyone landing on the repo
reads that first.

**Fix:** Step 0 of Phase 1 must include "**rewrite
`src/impl-go/dashd/README.md`** to point at the new design + the
impl-plan-basic.md as the source of truth".

### C2 — Phase 1 doc says "Phase 2 will cover X"; some Xs are not in Phase 2 plan

E.g. Phase 1 says "TLS / mTLS / RBAC / audit log" are Phase 2. Phase 2
covers TLS/mTLS/RBAC under M9 and audit under M10 ✓. But Phase 1 also
defers "schema/capability gating" — Phase 2's M5 covers it ✓. **But**
Phase 1 also defers "diagnostic RPCs (TraceFlow, ExplainMatch — stubbed)"
— Phase 2 says they're in M11. Some words say "TriggerResimulation",
others say "ExplainMatch". Make sure the deferred-feature list in basic
exactly matches the delivered-feature list in advanced. Currently 2 RPCs
are mentioned in one but not the other.

### C3 — Both plans link to `dash-sim-alignment-audit.md`; we just renamed it

Phase 2 plan references
`docs/dash-sim-alignment-audit.md`. We renamed it to
[`docs/dash-sim-on-par-with-sonic-audit.md`](dash-sim-on-par-with-sonic-audit.md).
Update the reference in both plans.

---

## D. The one thing I'd insist on before merge

Add a **"Lessons from Kubernetes / kube-controller-manager"** annex to
the design docs, calling out the four patterns that *both plans
already use* but don't credit:

1. **Reconcile is level-driven, not edge-driven.** Spec says: dirty
   channel may drop events; 30s tick re-reconciles. Document this as
   *the* invariant.
2. **Reconcile is idempotent.** Spec says: optimistic cache update +
   subscribe push (no-op on equal). Document the requirement on every
   reconciler.
3. **Reconcile is per-DPU isolated.** Spec says: one worker per DPU,
   rate-limited, error budget per DPU. Document that no per-DPU failure
   affects another DPU.
4. **Reconcile is observable.** *This* one is the gap (§A6). Fix it.

Once those four are stated as **the controller invariants**, every
future package gets reviewed against them. It's the difference between
"a controller that works" and "a controller that survives 18 months of
production".

---

## E. Closing scorecard

| Dimension | Basic plan | Advanced plan |
|---|---|---|
| **Scope clarity** | 10/10 | 9/10 |
| **Per-module contracts** | 10/10 | 9/10 |
| **Test specification** | 10/10 | 8/10 (M7/M9/M12 lighter) |
| **Failure-mode reasoning** | 8/10 | 7/10 |
| **Concurrency model** | 9/10 | 8/10 |
| **API ergonomics** | 8/10 | 7/10 (saga rollback, drain timeout fuzzy) |
| **Observability** | 4/10 ⚠ | 5/10 ⚠ |
| **Forward-compat hygiene** | 9/10 | 6/10 ⚠ (namespace forklift) |
| **Operator experience** | 8/10 | 8/10 |
| **Aggregate** | **9/10** | **7.5/10** |

The basic plan is essentially ready to implement; my recommendation is
to **apply §A1 + §A4 + §A6 (the three structural items) to it before
Phase 1 starts**, then evolve the advanced plan against §A2 / §A3 /
§A5 / §A7 as you go.

If you'd like, I can produce concrete PR-shaped edits to the basic plan
that fold these in. Say the word.
