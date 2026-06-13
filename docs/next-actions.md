# dashd — Next Actions Plan

> **Purpose**: Ordered, trackable plan for the post-PD-partial work. Companion to [`specs/Impl-Plan/impl-phases.md`](../specs/Impl-Plan/impl-phases.md) (the milestone-level tracker). This file is the *operator's worklist* — what to do next, in what order, and why. **This is the source of truth for next steps.**
> **Created**: 2026-06-11
> **Last updated**: 2026-06-11 — rows 1–3 refreshed with full delivered scope (HA fleet grew to 120 objects × 2 namespaces, dashctl context auto-setup, lab-task appendices)

---

## Current state (snapshot)

| Slice | Status |
|---|---|
| Phase 1A · Core | ✅ tagged |
| Phase 1B · Hardening | ✅ 15/16 (goleak deferred) |
| Phase 2 · PA · Infra (etcd + leader election + namespace) | ✅ `dashd-2.0.0-alpha` |
| Phase 2 · PB · Admission (capacity + schema + simulate) | ✅ `dashd-2.0.0-beta` |
| Phase 2 · PC · Operations (HA + migration + cordon/drain/saga) | ✅ `dashd-2.0.0-rc1` + `dashd-2.0.0-beta-ENI-Live-Migration` |
| Phase 2 · PD · Security & Observability | ⏳ 4/5 (PD-G5 counter-polling deferred) |
| Phase 2 · PE · Diagnostics & gNMI | ❌ not started |
| Phase 2 · PF · Controllerless | ❌ not started (intentional last per Strategy B) |

**Known unblockers shipped this session (post-tagging)**:
- HA admin-leader status (admin handler was hardcoded `leader=true`) — fixed in [`src/impl-go/dashd/internal/server/admin/server.go`](../src/impl-go/dashd/internal/server/admin/server.go)
- Stale config validator rejecting `ha.controller.elector.backend=etcd` — fixed in [`src/impl-go/dashd/internal/config/ha.go`](../src/impl-go/dashd/internal/config/ha.go)
- `start-fleet.{ps1,sh}` IPv6 stall on Windows when probing `http://localhost:27443` — IPv6 lookup hits `::1` first, dashd binds IPv4-only, the connect blocks past the 2s timeout. Fixed in 04-ha-fleet by probing `127.0.0.1` directly with a 3s timeout.
- `dashctl --endpoint` mandatory on the HA fleet — fixed by having `start-fleet` auto-configure a saved `ha-fleet` dashctl context (kubectl-style `config set-context` + `use-context`).
- Mermaid diagrams failing to render — edge labels containing `.` `/` `(` `)` `:` `+` must be double-quoted; multi-word `subgraph` IDs need explicit aliases. Fixed in all 3 diagrams across README + manual-handson + tutorial 18.

**Latent work documented but not in this plan**:
- Denial auditing (4xx short-circuits never reach audit middleware) — 30-line cleanup, no PE/PF dependency. **Captured as row 9 below.**
- goleak (P1B-G10) — housekeeping pass; separate slice, not tracked here.
- `dashctl` survives losing its pinned dashd instance — documented as the **Lab task** in [`04-ha-fleet/manual-handson.md`](../deploy/test-setup/04-ha-fleet/manual-handson.md#lab-task--make-dashctl-survive-losing-its-target-dashd) with 3 solution options (HAProxy LB / multi-endpoint context / leader-aware routing anti-pattern). Production fix is solution 1; deferred until someone needs it.

---

## Recommendation

**Do operator-facing artifacts first, *then* PE.** Rationale:

1. We just built and tore down a fully working 9-container HA fleet (5 dash-sim × 3 dashd × etcd). Two real bugs surfaced from it (admin.health, ha-elector validator). That is evidence we **need** a permanent HA fixture — `01/02/03` topologies under [`deploy/test-setup/`](../deploy/test-setup/) all assume single-dashd.
2. Every PE/PF PR from here benefits from one-command HA reproducibility. CI gets a real multi-node smoke. Operators get the demo for free.
3. PE then unblocks PD-G5 (counter polling needs sim counters) → tag `dashd-2.0.0` GA cleanly.

### What we explicitly defer

| Option | Why later |
|---|---|
| dash-sim enhancement as a standalone task | PE delivers it as a byproduct (sim counter emission) |
| dashctl next phase | dashctl is feature-complete for current dashd surface — no proto RPC is unwired |
| Controllerless (PF) | Locked last per Strategy B; needs PD-stable + PE-tested fleet to validate against |
| Pure design docs (etcd / HA / election principles) | Already covered in [`specs/HLD/high_level_system_design.md`](../specs/HLD/high_level_system_design.md), [`specs/HLD/high_level_system_design_controllerless.md`](../specs/HLD/high_level_system_design_controllerless.md), [`specs/LLD/dashd-lld.md`](../specs/LLD/dashd-lld.md). Add **operational** docs (the tutorial below) rather than re-stating design |

---

## The plan — 5 ordered steps

```
Step 1 ──► Step 2 ──► Step 3 ──► Step 4 ──► Step 5
04-ha-fleet  tutorial 10  PE         PD-G5     dashd-2.0.0
fixture     ha+migration  diag+gNMI  + denial  GA tag
                                     audit
```

---

### Step 1 — Promote the throw-away fleet to `deploy/test-setup/04-ha-fleet/`

**Why first**: lowest cost, highest leverage. Locks in the multi-dashd HA topology as a repeatable fixture so every PE/PF PR validates against it. Surfaces follower-vs-leader bugs early (we already found two this week).

**Deliverables**:
- [`deploy/test-setup/04-ha-fleet/docker-compose.yml`](../deploy/test-setup/04-ha-fleet/docker-compose.yml) — 1 etcd + 5 dash-sim + 3 dashd, host ports `28443/28453/28463` + `27443/27453/27463`
- `dashd-1.yaml` / `dashd-2.yaml` / `dashd-3.yaml` — controller-mode, etcd elector, 8s lease TTL
- `inventory.yaml` — 5 sim DPUs
- `manifest.yaml` — 2 vnets (`tenant-blue`, `tenant-red`) + 5 ENIs (pinned via `placement_hint_dpu_ids`) + 2 VnetMappings + 2 RoutePolicies
- `start-fleet.ps1` / `start-fleet.sh` + `stop-fleet.ps1` / `stop-fleet.sh` (parity with `01-host-multi-port/`)
- `README.md` — operator reference (topology diagram, port map, common ops)
- `manual-handson.md` — step-by-step apply + kill-leader + verify-failover walkthrough
- `test/integration/ha_fleet/` with `//go:build integration_ha_fleet` — automation that brings the compose up, applies the manifest, asserts state on all 3 dashd, kills the leader, asserts succession + state survival, tears down

**Quality bar**:
- `docker compose up -d` brings 9 containers to ready in ≤ 30s
- Exactly one dashd reports `/admin/leader → leader=true` after ≤ 10s
- Killing the leader → succession within `lease_ttl + 5s` (so ≤ 13s with our 8s TTL)
- Applied state survives leader change
- Integration test green in ≤ 90s

**Estimated scope**: ~250 LOC YAML + ~150 LOC Go test + ~300 lines of docs. Roughly half a day.

---

### Step 2 — `docs/tutorial/18-ha-and-migration-handson.md`

**Why second**: the tutorial sequence stops at `17-full-fleet-experiments.md` (HA + migration are PC's headline features but were untaught). Writing the tutorial is also adversarial review — it surfaces operator-facing rough edges (this is how we found the `leader=true` bug and the IPv6 stall).

> **Note on slot numbering**: slots 0–9 (Part I, dash-sim) and 10–17 (Part II, dashd) are already taken. The HA tutorial took slot **18**, not 10 as originally written in this plan.

**Deliverables**:
- New tutorial page [`docs/tutorial/18-ha-and-migration-handson.md`](tutorial/18-ha-and-migration-handson.md) covering, end-to-end:
  1. Bring up `04-ha-fleet/` (from Step 1)
  2. Inspect leader election (etcd keyspace + `/admin/leader` on each dashd)
  3. Apply an `HaSet` + walk a planned `switchover` via SSE
  4. Walk an unplanned `failover` and observe `DrainOldActive` count stays zero
  5. Walk a 10-phase ENI live migration via SSE
  6. **Kill the dashd leader mid-migration** → resume on new leader (operator-visible proof of PC-G6)
  7. Cleanup
- Update [`docs/tutorial/README.md`](tutorial/README.md) index to include the new page
- Cross-link from `04-ha-fleet/README.md` → this tutorial

**Quality bar**: every command in the tutorial copy-pastes and produces the documented output on a fresh checkout.

**Estimated scope**: ~600 lines of markdown + small fixture tweaks. Roughly half a day.

---

### Step 3 — Milestone PE (Diagnostics + sim counters + gNMI bridge)

**Why third**: the next code milestone in Strategy B. Strictly required to unblock PD-G5 (counter polling has no upstream until dash-sim emits counters). All three sub-deliverables are well-scoped and independently testable.

**Scope** (mirrors [`specs/Impl-Plan/impl-phases.md`](../specs/Impl-Plan/impl-phases.md) § Milestone PE):

| Sub-milestone | Package | Headline RPCs | Gate |
|---|---|---|---|
| PE-1 Diagnostics | `internal/flow/` | `TraceFlow`, `ExplainMatch`, `ExplainDrift`, `GetAclHitStats`, `TriggerResimulation` | PE-G1, PE-G6 |
| PE-2 Saga + EventBroadcaster + gNMI | `internal/api/gnmi/`, `internal/observability/` | gNMI `Subscribe` (ON_CHANGE) | PE-G5 |
| PE-3 dash-sim counters | `src/impl-go/dash-sim/` + `internal/dispatch/` | per-ENI rx/tx bytes + pkts + drops on packet/flow events | unblocks PD-G5 |
| PE-4 Bundle export/import | `internal/migration/bundle.go` | `ExportMigrationBundle`, `ImportMigrationBundle` (streaming gRPC) | closes the two `Unimplemented` MigrationService RPCs deferred from PC |

**Quality bar**: PE-G1..G5 all green; `internal/flow` ≥ 90% coverage; sim counters visible via existing `GET /admin/observed`; gNMI Subscribe receives a `Notification` within 2s of a `PutVnet`.

**Estimated scope**: largest of the five steps. Will be broken into a PE-tracker analogous to PA/PB/PC in [`specs/Impl-Plan/impl-phases.md`](../specs/Impl-Plan/impl-phases.md) when started.

---

### Step 4 — Close PD-G5 + denial-audit cleanup

**Why fourth**: PE-3 (sim counters) unblocks PD-G5. Pairing them keeps the audit subsystem cohesive.

**Deliverables**:
- `internal/observability/counter_store.go` — ring buffer per `(dpu, eni, counter_name)` with configurable retention (default 5 min @ 500ms = 600 samples)
- `internal/observability/broadcaster.go` — fan-out hub for `GetCounters(follow=true)` subscribers; drop-on-overflow with `CounterStreamLagged` audit event
- `internal/dispatch/manager.go` — extend with `SnapshotCounters(ctx, dpuID)`; ticker (default 500ms, configurable via `cfg.Observability.CounterPollInterval`)
- `internal/server/grpc/observability.go` — implement `GetCounters` server-stream (first frame = snapshot, subsequent = deltas; honour `req.Dpus` / `req.Counters` filters and `req.Follow=false` one-shot mode)
- `dashctl/internal/cmd/counters.go` — wire `--follow`
- **Denial auditing** (separate ~30-LOC patch): when `internal/auth/interceptor.go` denies, call `auditWriter.Write(Entry{OK:false, ...})` directly before returning the error; pass writer in via `auth.Options`
- Tests: ~8 cases counters (snapshot-only, follow with synthetic ticker, filter by DPU, filter by counter name, lagged-subscriber drop, broadcaster cleanup on client cancel, retention eviction, integration against dash-sim) + 2 cases denial audit

**Quality bar**: PD-G5 green; `internal/observability` ≥ 90% coverage; live e2e: `dashctl counters --follow` tails a synthetic packet flood from dash-sim.

**Estimated scope**: ~500 LOC + tests. Roughly 1–2 days.

---

### Step 5 — Tag `dashd-2.0.0` GA

**Deliverables**:
- All 5 PD gates green (G1..G5 + denial audit)
- All 5 PE gates green
- `specs/Impl-Plan/impl-phases.md` Phase 2 · PD row → 5/5; Phase 2 · PE row → 5/5
- `CHANGELOG.md` entry summarising PA → PE
- `git tag dashd-2.0.0` on a green main
- README front-matter "Status" badge bumped from `rc1` → `2.0.0`

**Quality bar**: `go test -count=1 ./...` clean across all Go modules; `04-ha-fleet/` integration test green; full tutorial walkthrough (`docs/tutorial/00..10`) copy-pastes end-to-end.

---

## Tracker

| # | Action | Status | Started | Completed | Notes / links |
|---|---|---|---|---|---|
| 1 | Promote `tmp-fleet/` → `deploy/test-setup/04-ha-fleet/` (compose + configs + manifest + scripts + README + handson) | ✅ Done | 2026-06-11 | 2026-06-11 | **120-object pre-baked scenario across 2 namespaces** (12 vnets, 4 service tunnels, 31 ENIs incl. `eni-quarantine-01`, 30 vnet mappings, 15 route policies, 15 ACL policies with **~130 ACL rules total**, 3 HA sets in `default`; 2 vnets + 3 ENIs + 3 mappings + 1 route + 1 ACL in `edge`). Manifest split into 11 files under `manifest/` exercising every spec kind + every behavioural knob (admin_state up/down, resimulate_flows, 4 vnet-mapping actions, all 4 next-hop types including direct, 2/3-way ECMP with weights, numeric protocols `6`/`17`/`1`/`58`, port ranges, src_port matchers, allow_and_continue chains, active_standby + active_active HA modes). Also added `show-leader.{ps1,sh}` helper (uses `dashctl version` against each node). `start-fleet.{ps1,sh}` auto-configures a saved `ha-fleet` dashctl context so `--endpoint` is no longer required. Live smoke-test passed end-to-end: leader elected, 130 objects applied + verified in etcd (120 state keys + 10 edge ns), fan-out across 3 dashd verified, HA switchover SSE green, HA failover SSE green, kill-leader failover preserves all 120 objects. |
| 2 | Add `test/integration/ha_fleet/` with `//go:build integration_ha_fleet` automation | ✅ Done | 2026-06-11 | 2026-06-11 | `src/impl-go/dashd/test/integration/ha_fleet/fleet_test.go` (~250 LOC) drives `start-fleet`, applies manifest via docker-run dashctl with bind-mount, asserts per-kind counts, exercises switchover SSE, kills the leader, asserts state survival on the new leader. `go vet -tags=integration_ha_fleet` clean; full run reserved for CI / on-demand. |
| 3 | Write `docs/tutorial/18-ha-and-migration-handson.md` + update tutorial index + add Appendix + Lab task to manual-handson | ✅ Done | 2026-06-11 | 2026-06-11 | Slot 18 not 10 (10–17 were occupied). Tutorial 18: 8-step walkthrough prereqs → fleet up → leader election → apply objects → switchover SSE → failover SSE → 10-phase migration → kill-leader-mid-migration survival → cleanup. `04-ha-fleet/manual-handson.md` rewritten with **Objective / Run / Expected output** per step (15 steps) + lab topology Mermaid + **Appendix A** "How dashctl finds dashd" (A.1 endpoint resolution, A.2 architecture diagram, A.3 always-dashd-1, A.4 switchover effects) + **Lab task** with reproducible failure recipe and 3 collapsible solutions (HAProxy LB / multi-endpoint context / leader-aware routing anti-pattern). Cross-linked from `04-ha-fleet/README.md` and `deploy/test-setup/README.md`. Mermaid label-escaping bug fixed in all 3 diagrams (manual-handson ×2, README ×1, tutorial ×1). |
| 4 | PE-1 — `internal/flow/` Diagnostics (TraceFlow / ExplainMatch / ExplainDrift / GetAclHitStats / TriggerResimulation) | ✅ Done | 2026-06-11 | 2026-06-11 | PE-G1 + PE-G6 green. New `internal/flow/` package (~1050 LOC, 7 source files + 23 unit tests at **91.2% coverage**) implements all 5 RPCs as pure-cache functions over `placement.DesiredSpecs`. `TraceFlow` walks ACL chain (allow/deny/allow_and_continue) → longest-prefix route (4 next-hop types: vnet/service_tunnel/direct/drop, metric tie-break) → vnet-mapping, emitting a per-stage trace[] + MatchedAclRule/MatchedRoute/MatchedVnetMapping protos. `ExplainMatch` returns per-candidate `MatchCandidate` rows for SUBJECT_ACL / SUBJECT_ROUTE / SUBJECT_VNET_MAPPING. `ExplainDrift` returns presence + remediation hint (RECONCILE / MANUAL). `GetAclHitStats` with `ZeroHitsOnly=true` filter is dead-rule-detection ready against the NilHitStats source (PD-G5 will swap in the live counter store). `TriggerResimulation` validates non-empty scope then delegates to the injected Resimulator. Service-layer wrapper in `internal/service/diagnostics.go` maps flow.* sentinels to service+store sentinels. Wired into both transports: gRPC `DiagnosticsServiceServer` in `internal/server/grpc/diagnostics.go` (streaming `GetAclHitStats` adapter), REST in `internal/server/rest/diagnostics.go` (5 `POST /v1/diagnostics/*` endpoints accepting the proto request shapes verbatim). main.go step 8b constructs the engine + service and threads them through both server Options. All 25 dashd + 8 dashctl packages green; vet of new code clean. |
| 5 | PE-2 — Saga + EventBroadcaster + gNMI Subscribe bridge | ⬜ Not started | — | — | PE-G5; saga was already partially landed under PC-G8 — confirm reuse |
| 6 | PE-3 — dash-sim per-ENI counter emission + dispatch.SnapshotCounters wiring | ⬜ Not started | — | — | unblocks #8 |
| 7 | PE-4 — MigrationService `ExportMigrationBundle` / `ImportMigrationBundle` (streaming gRPC) | ⬜ Not started | — | — | closes the two PC `Unimplemented` stubs |
| 8 | PD-G5 — `internal/observability/{counter_store,broadcaster}.go` + `GetCounters` follow mode + `dashctl counters --follow` | ⬜ Not started | — | — | depends on #6 |
| 9 | Denial auditing — write audit entry from `auth.interceptor` on 401/403 before short-circuit | ✅ Done | 2026-06-11 | 2026-06-11 | **Callback pattern** avoids circular import (`audit` already imports `auth`; `auth` MUST NOT import `audit`). New `auth.DenyAuditor func(method string, code int, actor string, err error)` type + variadic functional options (`MiddlewareOption`, `WithDenyAuditor(fn)`) thread the callback through `NewHTTPMiddleware`, `NewUnaryServerInterceptor`, `NewStreamServerInterceptor` (zero-opt callers stay no-op). Helper `audit.DenyAuditor(*audit.Writer)` returns the bound closure that writes `Entry{OK:false, Code: httpCodeString(code), Actor, Method, Error}`; nil writer returns nil so middleware's nil-guard skips. REST + gRPC server constructors auto-wire the callback when `opts.AuditWriter != nil`. Actor identity is best-effort: `cn:<CN>` (mTLS) → `bearer:<8-char prefix>…` (token; never logs full secret) → `anonymous`. **9 unit tests added** (3 audit `deny_auditor_test.go` cases incl. nil-writer guard + nil-error guard; 6 auth `deny_auditor_test.go` cases covering HTTP 401/403 + allow-path no-fire + gRPC unary 401/403 + allow-path no-fire) — all green. **Live e2e**: spun token-auth dashd on `127.0.0.1:18094` (state_dir = `$TEMP\dashd-pd-g4-deny`); curl-test produced 4 expected status codes (401 anonymous, 401 wrong token, 403 viewer write, 200 admin read) and `audit.jsonl` recorded all three deny rows with correct `actor`/`code`/`error` plus the admin allow rows — denial path now visible to operators. 05-full-console fleet remained up the whole time. |
| 10 | Tag `dashd-2.0.0` GA — CHANGELOG, README badge, green CI | ⬜ Not started | — | — | depends on #8, #9, #4–7, #11 |
| 11 | PE-G6 — `ClusterService` (cluster.Registry + Aggregator + Broadcaster) | ✅ Done | 2026-06-12 | 2026-06-12 | **First-class fleet-topology RPC.** New `internal/cluster/` package (~1000 LOC, 16 unit tests): `Registry` (~400 LOC) publishes self under its OWN etcd lease (decoupled from elector — leadership loss does NOT depublish) and watches `/peers/` for membership; `Aggregator` (~330 LOC) is a pure function over `(registry, inventory, store, elector)` returning a deterministic `TopologyResponse` (every list stable-sorted for diff-friendly clients); `Broadcaster` (~120 LOC) drop-on-slow-subscriber for streaming. Wire contract: `proto/dashcenter/v1/cluster.proto` adds `ClusterService` with `GetTopology` (unary) + `WatchTopology` (server-stream). REST (`GET /v1/cluster/topology` + `/watch` SSE) + gRPC + `GET /admin/topology` convenience. Auth: both methods are `registerR(...)` (viewer+) on the same auth+audit chain as every other RPC. Self-only fallback when `storage.backend != etcd` keeps single-node deployments unchanged. Design spec: [docs/dashd-features/cluster-topology-design.md](dashd-features/cluster-topology-design.md). **Live e2e against 05-full-console fleet**: all 3 dashd nodes self-published into etcd under `/dashd/console/state/peers/`, every node's `/admin/topology` returns 3-node `cluster.nodes` + 10 DPUs across 5 appliances + 3 zones + per-namespace object counts (default 14 vnets/41 ENIs/18 ACLs/17 routes, edge/staging ns also enumerated); `/v1/cluster/topology/watch` SSE delivers `event: snapshot` with full payload on connect. **26-package full test suite green**, zero regressions. **Known limitation** (deferred follow-up): `EtcdElector.LeaderID()` returns "" on followers until they call `ObserveCurrentLeader` — leader-node responses have `leader_id` populated, follower responses currently don't. Acceptable for PE-G6 v1; will be addressed when the elector adds a background observer goroutine. |
| 12 | dashw aggregator collapse — replace `console/internal/aggregation/aggregator.go::ServiceTopology` (~400 LOC fan-out) with a thin `GET /api/console/service-topology` → `dashd.ClusterService.GetTopology` proxy; delete `DashdClusterAddrs` config knob | ⬜ Not started | — | — | depends on #11 (PE-G6 landed) |
| 13 | `dashctl topology [--follow]` — CLI client over `ClusterService` (gRPC) | ⬜ Not started | — | — | depends on #11 |
| 14 | PE-G7 — Topology streaming hardening + dashw multiplexer + `/topology-v2` SPA | ✅ Done | 2026-06-12 | 2026-06-12 | **Production-grade live topology end-to-end.** Closes 7 dashd broadcaster defects (D1-D7) + introduces dashw multiplexer so browsers NEVER hit dashd directly. **dashd** (`internal/cluster/broadcaster.go` rewritten ~620 LOC): per-event monotonic `event_id`, `Frame{Event,JSON}` pre-marshalled once via protojson (~50× CPU win), `KIND_DROPPED`/`KIND_RATE_LIMITED`/`KIND_RESYNC` synthetic Notice frames, coalescing window (50ms by `(kind,entity-id)`), leaky-bucket rate limit (100/s + burst 200), ring replay with stale-cursor RESYNC sentinel, global cap (64) + per-subject cap (4) returning `ResourceExhausted`/429, single global keepalive ticker (30s), full Prom metrics (`dashd_cluster_broadcaster_*`). Proto adds `WatchTopologyRequest.resume_after_event_id` + `TopologyEvent.event_id` + 4 new kinds + `Notice` message. REST `/v1/cluster/topology/stream` honours `Last-Event-ID` header + `?last_event_id=` query. **dashw** (NEW `internal/cluster/` ~860 LOC): `Hub` multiplexes N browser tabs onto 1 upstream dashd gRPC stream with snapshot dedup cache (1s TTL), per-IP cap (8), global cap (512), upstream auto-reconnect (500ms→15s exp backoff), fan-out RESYNC on reconnect. New SPA-facing endpoints `/api/console/topology-v2[/stream|/ws|/_stats]`. **SPA** (NEW `src/views/topology-v2/`): `topology-v2-store.ts` Zustand reducer over all event kinds, `useTopologyStream.ts` EventSource owner with Last-Event-ID resume + tab-visibility pause + exp-backoff reconnect, `TopologyV2View.tsx` with ConnectionBadge (dropped/suppressed/resync counters + 45s stale warning), SummaryStrip, ClusterPanel (leader Crown pulse), AppliancesGrid (color-coded DPU state), EventTicker (last 12 events), InspectorDrawer. Sidebar `/topology-v2 · Live` entry. **Tests**: 14 dashd broadcaster tests + 10 dashw hub tests + 17 SPA store tests + 8 SPA hook tests; **dashd 26 packages green**, **dashw 8 packages green**, **SPA 234/234 tests + Vite build clean**. Comprehensive design + **Future Scopes (14 entries)** spec at [docs/dashd-features/topology-streaming-design.md](dashd-features/topology-streaming-design.md). Browser ↔ dashw contract enforced — operators cannot bypass the BFF. |

**Status legend**: ⬜ Not started · ⏳ In progress · ✅ Done · ❎ Deferred

---

## How to use this file

**This is the source of truth for next steps.** Anyone landing PRs should:

1. Pick the lowest-numbered ⬜ row in the tracker.
2. Flip it to ⏳ and fill in `Started`.
3. Land the work in a focused PR. Land code only — do not edit this file from the PR.
4. After merge, edit this file: flip to ✅, fill `Completed`, link the PR in `Notes / links`. Keep the original description; **append** scope changes rather than rewriting them — the audit trail matters.
5. If you discover follow-up work that doesn't fit the existing 10 rows, add a new row at the end with status ⬜. Don't smuggle it into an existing row's notes.

When all rows are ✅, archive this file by renaming it to `docs/next-actions-2026-Q2.md` and open a fresh one for the next horizon (PF / `dashd-3.x`).

### Immediate next step (as of 2026-06-11 evening)

**Pick row 5 — PE-2** (Saga + EventBroadcaster + gNMI Subscribe bridge). Saga was already partially landed under PC-G8 — first task is to confirm reuse and only add what's still missing (durable `Resume()` on dashd restart for in-flight rollbacks per the original PE-G3/G4 contract). gNMI Subscribe is a fresh code path; PE-G5 wants a single `Notification` delivered to a gNMI client within 2s of a `PutVnet`. Estimated 1–2 days; ~400 LOC + tests.
