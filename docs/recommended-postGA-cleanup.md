# Recommended Post-GA Cleanup

> **Audience**: DashCenter maintainers planning the post-`dashd-2.0.0`
> cleanup window.
> **Scope**: refactors, deletions, and consolidations that are pure
> tech-debt reductions — no new user-facing feature value. Scheduled
> deliberately AFTER GA so they don't compete with feature work for
> review attention.
> **Status**: ⬜ Planning. Each row owns its own PR; no batching.

---

## Why a dedicated cleanup window?

DashCenter shipped Phase 1A → Phase 2 (PE) in roughly six months. Along
the way we landed:

- "Workaround" code that solved a problem dashd couldn't solve at the
  time. dashd has since grown the proper solution → the workaround
  is now duplication.
- Config knobs that exist only because of those workarounds.
- Test fixtures + scripts that ran against pre-GA wire formats.
- Documentation files that were created during exploration and got
  superseded by later, more comprehensive specs.

If we land cleanups during feature work, reviewers split attention
between "is this cleanup correct?" and "is this feature correct?" — and
the cleanup loses. A dedicated 1-2 week post-GA window lets each
deletion get focused review.

**Rule**: nothing in this file should change user-visible behaviour
unless explicitly called out. Wire format, CLI surface, REST routes,
config flag names — all stable. Internal-only changes welcome.

---

## Priority tiers

- **T1 (high value)**: ≥200 LOC removed AND fixes an active footgun.
- **T2 (medium value)**: 50-200 LOC removed OR consolidates a duplicate
  concept.
- **T3 (nice to have)**: < 50 LOC OR purely cosmetic.

---

## T1 — High value

### T1.1 — `dashw` aggregator collapse

| Field | Detail |
|---|---|
| **What** | Delete [src/impl-go/console/internal/aggregation/aggregator.go](../src/impl-go/console/internal/aggregation/aggregator.go) (~400 LOC) and rewrite `/api/console/service-topology` as a thin proxy over `cluster.Hub.GetTopology()`. |
| **Why redundant** | Pre-PE-G6 workaround: dashw needed a fleet-wide view but dashd had no such RPC, so dashw built one client-side by sharding `/admin/*` calls across every dashd in `DashdClusterAddrs[]`. PE-G6 added `ClusterService.GetTopology`; PE-G7 added `Hub.GetTopology` with a 1s dedup cache. The aggregator now duplicates the controller's authoritative view with worse semantics (no resume, no caps, no Prom metrics, no deterministic sort). |
| **Net LOC** | -400 in `internal/aggregation/aggregator.go` + -200 in its test suite + -20 in `internal/config` (delete `DashdClusterAddrs` field + flag) + new 20-LOC thin proxy. **Net: -600 LOC**. |
| **Config breakage** | Delete `--dashd-cluster` CLI flag + `DASHD_CLUSTER_ADDRS` env var. Operators with these set will get a `flag provided but not defined` error on start; they can simply remove the line. The `DashdGrpcAddr` (single dialled dashd) takes over. |
| **Wire breakage** | None for browsers — `/api/console/service-topology` keeps responding with the same shape (the new proxy serves `TopologyResponse` which is the same shape the SPA already consumes via `/topology-v2`). |
| **Risk** | Behavioural drift: the aggregator's "newest-wins" merge sometimes differs from any single dashd's deterministic snapshot. Run a side-by-side diff in the 05-full-console fleet for 1 hour before deletion. |
| **Effort** | ~3 hours. |
| **Validation** | Diff the JSON output of `GET /api/console/service-topology` before and after under: (a) all 3 dashd healthy, (b) one dashd killed, (c) leader changeover. All three must produce semantically equivalent output. |
| **Cross-links** | [topology-streaming-design.md §3.2](dashd-features/topology-streaming-design.md#32-why-a-multiplexer-dashw-hub) (why a multiplexer is the right answer), [next-actions.md row #12](next-actions.md). |

### T1.2 — Saga partial-landing dedupe (PC-G8 vs. PE-G5)

| Field | Detail |
|---|---|
| **What** | Saga coordinator was partially landed under PC-G8 (Phase 1C). The pending PE-G5 work was originally scoped as "implement Saga". Audit the actual `internal/saga/` package vs. PE-G5's spec; either close PE-G5 as "already done" or scope it to the residual delta. |
| **Why** | Avoid building a parallel implementation when ~70% of the work is already in production. |
| **Effort** | ~1 day (audit + scope adjustment) + however much residual work remains. |
| **Cross-links** | [next-actions.md row #5](next-actions.md). |

### T1.3 — Extract shared `broadcaster` package (PROMOTED from T2.1)

| Field | Detail |
|---|---|
| **What** | Two near-identical broadcaster implementations now ship: `dashd/internal/cluster/broadcaster.go` (~620 LOC, PE-G7) and `dashd/internal/observability/broadcaster/broadcaster.go` (~800 LOC, PE-3c / PD-G5). Both: marshal-once Frame, ring buffer + `ResumeAfterEventID`, drop-on-slow + sentinel synth, leaky-bucket rate limit, per-subject + global caps, single global keepalive. Differences: payload type (`TopologyEvent` vs. `CounterEvent`), per-dpu coalesce key (counters only), sentinel constructors. Extract a generic `internal/broadcaster/Broadcaster[T proto.Message]` (Go 1.22 generics) with hooks for `coalesceKey(T)`, `newKeepalive()`, etc. |
| **Why now** | PROMOTED to T1 because the second instance shipped in PD-G5 (2026-06-14) — the abstraction is now justified by lived experience, not speculation. Future streaming endpoints (e.g., the alerter from Future Scope 10.3) would be a third copy. |
| **Effort** | ~2 days + a UT migration. Existing 60+14 = 74 tests must keep passing. |
| **Risk** | Refactoring well-tested production code. Mitigation: extract gradually, keep both surfaces' existing tests pointing at the same surface via type-parameter shim. |
| **Order** | Do this BEFORE the next streaming endpoint (Future Scopes 10.1 / 10.3 / 10.4). |
| **Cross-links** | [counter-streaming.md §3.2](dashd-features/counter-streaming.md#32-why-a-separate-broadcaster-not-just-storesubscribe), [topology-streaming-design.md](dashd-features/topology-streaming-design.md). |

### T1.4 — Consolidate `Notice` into `dashcenter.v1.types.proto`

| Field | Detail |
|---|---|
| **What** | `Notice` (sentinel-body message with `dropped_count`, `suppressed_count`, `current_event_id`, etc.) lives in `cluster.proto` and is cross-package-imported by `observability.proto` (PE-3c). Move to `proto/dashcenter/v1/types.proto`; both services import from there; delete the cross-package import. |
| **Wire impact** | **Zero.** `dashcenter.v1.Notice` is the same fully-qualified protobuf name regardless of which `.proto` file declares it; protoc-gen-go emits by package, not by file. Existing Go imports update to the new file; no client-side regen required for downstream consumers pinning to the package alias. |
| **Effort** | ~30 min: move the `message Notice {...}` block, fix one import line in each `.proto`, regen. |
| **Order** | Any time post-GA. Trivially low-risk — do alongside T1.3 if convenient. |
| **Cross-links** | [counter-streaming.md Future Scope 10.6](dashd-features/counter-streaming.md#106-consolidate-notice-into-dashcenterv1typesproto), [proto/dashcenter/v1/cluster.proto](../proto/dashcenter/v1/cluster.proto), [proto/dashcenter/v1/observability.proto](../proto/dashcenter/v1/observability.proto). |

---

## T2 — Medium value

### T2.1 — ~~Extract shared `broadcaster` package~~ (PROMOTED to T1.3)

Moved to **T1.3** above on 2026-06-14 after PD-G5 shipped the second instance and justified the abstraction by lived experience. See T1.3 for current status.

### T2.2 — Hand-rolled wire types vs. proto codegen

| Field | Detail |
|---|---|
| **What** | dashctl and the SPA both hand-roll JSON wire types that mirror `dashcenter.v1.*`. Today this is deliberate (decouples release cadence, avoids proto runtime in CLI binary). But the manual sync is starting to drift — e.g., `TopologyEvent.event_id` was added to one and we had to remember to add it to the other. Generate a thin codegen step that produces matching Go + TS types from proto, and switch each package to the generated version. |
| **Effort** | ~2 days. Worth it once we have 4+ proto messages mirrored in both dashctl + SPA. Currently 3 — wait until #4 lands. |
| **Risk** | Codegen breakage in CI. |

### T2.3 — Consolidate redundant `cluster.proto` SSE doc

| Field | Detail |
|---|---|
| **What** | [docs/dashd-features/cluster-topology-design.md](dashd-features/cluster-topology-design.md) (PE-G6 v1 design) is superseded by [topology-streaming-design.md](dashd-features/topology-streaming-design.md) (PE-G7 v2). Today both exist + cross-link; the v1 file confuses new readers ("which one is current?"). Add a banner to v1 directing readers to v2, then DELETE v1 once nobody's PR references it for 30 days. |
| **Effort** | 30 min. |

### T2.4 — Trim `command-registry.ts` stale paths

| Field | Detail |
|---|---|
| **What** | [src/impl-web/console/src/lib/command-registry.ts](../src/impl-web/console/src/lib/command-registry.ts) references API paths like `/api/v1/operations/cordon/{dpu-id}` that were never wired (the real path is `/api/v1/inventory/{id}/cordon`). Audit every `apiPath` in the registry against `internal/proxy/proxy_test.go`'s ground-truth route table; delete or fix mismatches. |
| **Effort** | ~2 hours. |

---

## T3 — Nice to have

### T3.1 — `safeWriter` micro-helper in `dashctl topology.go`

| Field | Detail |
|---|---|
| **What** | The renderer in [src/impl-go/dashctl/internal/cmd/topology.go](../src/impl-go/dashctl/internal/cmd/topology.go) uses a 1-method `safeWriter` shim around `io.Writer`. Replace with direct `io.Writer` once we audit no caller needs the no-error semantic. |
| **Effort** | 15 min. |

### T3.2 — `/admin/topology` deprecation path

| Field | Detail |
|---|---|
| **What** | The admin port's `/admin/topology` predates PE-G6's `/v1/cluster/topology`. Both return the same payload. Long-term: deprecate `/admin/topology` (operator scripts still pin it). Add a `Deprecation: <date>` HTTP header pointing at the v1 path for a release, then remove. |
| **Effort** | 30 min code + 1 release cycle communication. |

### T3.3 — Remove `eslint-disable react-hooks/exhaustive-deps` in `useTopologyStream.ts`

| Field | Detail |
|---|---|
| **What** | The disable was added to suppress a false positive (`reset` was referenced but not actually called any more). Now that `reset` is removed entirely, the disable is unnecessary. |
| **Effort** | 2 min. |

### T3.4 — Drop `Pause`, `Play`, `Wifi`, `Activity` import on `TopologyV2View.tsx` if unused after refactors

| Field | Detail |
|---|---|
| **What** | Some lucide icons imported during the iterative SPA UX changes may not be referenced any more. Run a `ts-unused-exports` pass on the file. |
| **Effort** | 5 min. |

### T3.5 — Audit `Future Scopes` lists for items now in-flight

| Field | Detail |
|---|---|
| **What** | [topology-streaming-design.md §11](dashd-features/topology-streaming-design.md#11-future-scopes) lists 14 future scopes. Some (e.g., #11.9 operator-facing cordon controls) just shipped as part of PE-G7.1. Mark them as "✅ shipped in PE-G7.1" rather than removing — the audit trail matters. |
| **Effort** | 15 min. |

---

## Anti-patterns to NOT clean up

These look like cleanup candidates but should be left alone:

- **Hand-rolled wire types per binary** — keeps dashctl small and lets
  binary releases diverge in cadence from proto schema releases.
  Cleanup only when drift becomes routine (T2.2 above), not pre-emptively.
- **dashw's `/api/v1/*` reverse proxy passthrough** — looks like "just
  forwarding"; deleting it would break the browser ↔ dashw contract
  (browsers must NOT speak to dashd directly). Keep it.
- **The `Notice` message having both `dropped_count` and
  `suppressed_count`** — looks redundant; they're semantically distinct
  (per-subscriber drops vs. broadcaster-wide suppressions).
- **Per-event protojson `UseProtoNames: true`** — deliberate to match
  the SPA's snake_case expectations. Switching to camelCase would
  silently break every browser-side selector.

---

## Acceptance criteria for "cleanup window done"

- All T1 rows: ✅ or ❎ (deferred with reason).
- All T2 rows: ✅, ❎, or moved to a future cleanup window.
- T3 rows: best-effort.
- All 26 dashd + 8 dashw + 8 dashctl + 234 SPA tests still green.
- 05-full-console fleet stands up cleanly with no `--dashd-cluster`
  flag (the most operator-visible deletion).
- `docs/dashd-features/` has no orphan v1 design docs without a
  superseded-by banner.

---

## When to schedule

- **After**: Tier-2 GA path lands (E PE-3 + F PD-G5 + G dashd-2.0.0 tag).
- **Before**: Tier-3 federation / multi-cluster work (those are
  fresh features that should land on a *clean* foundation).
- **Duration**: 1-2 sprints with no feature work landing in parallel
  on the touched packages.
