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
| Phase 2 · PD · Security & Observability | ✅ 5/5 (PD-G5 closed 2026-06-14 via PE-3c counter streaming end-to-end — see row #22) |
| Phase 2 · PE · Diagnostics & gNMI | ⏳ PE-1 diagnostics ✅, PE-3a/b/c counters ✅, **RI Phase 1 ✅** (25/25 sim FK validation); **PE-4 sim parity planned** (rows #23–#25: flow table + transforms + HA) |
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
| 6 | PE-3 — dash-sim per-ENI counter emission + dispatch.SnapshotCounters wiring | ❎ Split | — | — | **Superseded by rows #20 (PE-3a), #21 (PE-3b), #22 (PE-3c)** — split into three independently-shippable phases on 2026-06-14 per [agent-operating-discipline.md](agent-operating-discipline.md) discipline (each phase delivers operator value standalone + matches the dash-sim / dash-sim-client parity rule). |
| 7 | PE-4 — MigrationService `ExportMigrationBundle` / `ImportMigrationBundle` (streaming gRPC) | ⬜ Not started | — | — | closes the two PC `Unimplemented` stubs |
| 8 | PD-G5 — `internal/observability/{counter_store,broadcaster}.go` + `GetCounters` follow mode + `dashctl counters --follow` | ❎ Split | — | — | **Superseded by row #22 (PE-3c)** — streaming + dashctl + dashw + SPA widget land together as PE-3c; row title preserved for the GA tag history. |
| 9 | Denial auditing — write audit entry from `auth.interceptor` on 401/403 before short-circuit | ✅ Done | 2026-06-11 | 2026-06-11 | **Callback pattern** avoids circular import (`audit` already imports `auth`; `auth` MUST NOT import `audit`). New `auth.DenyAuditor func(method string, code int, actor string, err error)` type + variadic functional options (`MiddlewareOption`, `WithDenyAuditor(fn)`) thread the callback through `NewHTTPMiddleware`, `NewUnaryServerInterceptor`, `NewStreamServerInterceptor` (zero-opt callers stay no-op). Helper `audit.DenyAuditor(*audit.Writer)` returns the bound closure that writes `Entry{OK:false, Code: httpCodeString(code), Actor, Method, Error}`; nil writer returns nil so middleware's nil-guard skips. REST + gRPC server constructors auto-wire the callback when `opts.AuditWriter != nil`. Actor identity is best-effort: `cn:<CN>` (mTLS) → `bearer:<8-char prefix>…` (token; never logs full secret) → `anonymous`. **9 unit tests added** (3 audit `deny_auditor_test.go` cases incl. nil-writer guard + nil-error guard; 6 auth `deny_auditor_test.go` cases covering HTTP 401/403 + allow-path no-fire + gRPC unary 401/403 + allow-path no-fire) — all green. **Live e2e**: spun token-auth dashd on `127.0.0.1:18094` (state_dir = `$TEMP\dashd-pd-g4-deny`); curl-test produced 4 expected status codes (401 anonymous, 401 wrong token, 403 viewer write, 200 admin read) and `audit.jsonl` recorded all three deny rows with correct `actor`/`code`/`error` plus the admin allow rows — denial path now visible to operators. 05-full-console fleet remained up the whole time. |
| 10 | Tag `dashd-2.0.0` GA — CHANGELOG, README badge, green CI | ⬜ Not started | — | — | depends on #8, #9, #4–7, #11 |
| 11 | PE-G6 — `ClusterService` (cluster.Registry + Aggregator + Broadcaster) | ✅ Done | 2026-06-12 | 2026-06-12 | **First-class fleet-topology RPC.** New `internal/cluster/` package (~1000 LOC, 16 unit tests): `Registry` (~400 LOC) publishes self under its OWN etcd lease (decoupled from elector — leadership loss does NOT depublish) and watches `/peers/` for membership; `Aggregator` (~330 LOC) is a pure function over `(registry, inventory, store, elector)` returning a deterministic `TopologyResponse` (every list stable-sorted for diff-friendly clients); `Broadcaster` (~120 LOC) drop-on-slow-subscriber for streaming. Wire contract: `proto/dashcenter/v1/cluster.proto` adds `ClusterService` with `GetTopology` (unary) + `WatchTopology` (server-stream). REST (`GET /v1/cluster/topology` + `/watch` SSE) + gRPC + `GET /admin/topology` convenience. Auth: both methods are `registerR(...)` (viewer+) on the same auth+audit chain as every other RPC. Self-only fallback when `storage.backend != etcd` keeps single-node deployments unchanged. Design spec: [docs/dashd-features/cluster-topology-design.md](dashd-features/cluster-topology-design.md). **Live e2e against 05-full-console fleet**: all 3 dashd nodes self-published into etcd under `/dashd/console/state/peers/`, every node's `/admin/topology` returns 3-node `cluster.nodes` + 10 DPUs across 5 appliances + 3 zones + per-namespace object counts (default 14 vnets/41 ENIs/18 ACLs/17 routes, edge/staging ns also enumerated); `/v1/cluster/topology/watch` SSE delivers `event: snapshot` with full payload on connect. **26-package full test suite green**, zero regressions. **Known limitation** (deferred follow-up): `EtcdElector.LeaderID()` returns "" on followers until they call `ObserveCurrentLeader` — leader-node responses have `leader_id` populated, follower responses currently don't. Acceptable for PE-G6 v1; will be addressed when the elector adds a background observer goroutine. |
| 12 | dashw aggregator collapse — replace `console/internal/aggregation/aggregator.go::ServiceTopology` (~400 LOC fan-out) with a thin `GET /api/console/service-topology` → `dashd.ClusterService.GetTopology` proxy; delete `DashdClusterAddrs` config knob | ⬜ Not started | — | — | depends on #11 (PE-G6 landed) |
| 13 | `dashctl topology [--follow]` — CLI client over `ClusterService` (gRPC) | ✅ Done | 2026-06-13 | 2026-06-13 | **Operator CLI parity with the `/topology-v2` browser page.** New `dashctl topology` subcommand goes through dashd's REST surface (`/v1/cluster/topology` snapshot + `/v1/cluster/topology/watch` SSE) — NOT through dashw (dashw is the BFF for browsers; CLI talks straight to the cluster). One-shot mode renders a 3-section pretty tree (cluster header with leader `*` marker, appliances grouped by zone with cordoned flag, per-namespace object counts) + `--json` for machine consumption. `--follow` mode opens the SSE stream and prints one event per line; `--include-enis` flag toggles per-DPU ENI payload; `--since-id N` resumes via `Last-Event-ID` header + `?last_event_id=N` query (both sent to survive any reverse proxy that strips one). New `pkg/client.TopologySnapshot` + `TopologyEvent` hand-rolled wire types mirror dashcenter.v1 protojson without dragging the proto runtime into dashctl. REST backend gains `GetTopology` (unary) + `StreamTopology` (SSE parser handling multi-line `data:`, skipping `:keepalive` comments + `id:`/`event:` meta) and an SSE-specific `http.Client` with no response-header timeout so long-lived streams survive. **Tests**: 5 new `internal/cmd/topology_test.go` cases (pretty / JSON / include-enis / --follow with three frame kinds / sentinel-stop) + 4 new `pkg/client/rest/topology_test.go` cases (snapshot decode, Last-Event-ID sent both header+query, multi-line data parse, OnEvent-required guard). **All 8 dashctl packages green**. |
| 14 | PE-G7 — Topology streaming hardening + dashw multiplexer + `/topology-v2` SPA | ✅ Done | 2026-06-12 | 2026-06-12 | **Production-grade live topology end-to-end.** Closes 7 dashd broadcaster defects (D1-D7) + introduces dashw multiplexer so browsers NEVER hit dashd directly. **dashd** (`internal/cluster/broadcaster.go` rewritten ~620 LOC): per-event monotonic `event_id`, `Frame{Event,JSON}` pre-marshalled once via protojson (~50× CPU win), `KIND_DROPPED`/`KIND_RATE_LIMITED`/`KIND_RESYNC` synthetic Notice frames, coalescing window (50ms by `(kind,entity-id)`), leaky-bucket rate limit (100/s + burst 200), ring replay with stale-cursor RESYNC sentinel, global cap (64) + per-subject cap (4) returning `ResourceExhausted`/429, single global keepalive ticker (30s), full Prom metrics (`dashd_cluster_broadcaster_*`). Proto adds `WatchTopologyRequest.resume_after_event_id` + `TopologyEvent.event_id` + 4 new kinds + `Notice` message. REST `/v1/cluster/topology/stream` honours `Last-Event-ID` header + `?last_event_id=` query. **dashw** (NEW `internal/cluster/` ~860 LOC): `Hub` multiplexes N browser tabs onto 1 upstream dashd gRPC stream with snapshot dedup cache (1s TTL), per-IP cap (8), global cap (512), upstream auto-reconnect (500ms→15s exp backoff), fan-out RESYNC on reconnect. New SPA-facing endpoints `/api/console/topology-v2[/stream|/ws|/_stats]`. **SPA** (NEW `src/views/topology-v2/`): `topology-v2-store.ts` Zustand reducer over all event kinds, `useTopologyStream.ts` EventSource owner with Last-Event-ID resume + tab-visibility pause + exp-backoff reconnect, `TopologyV2View.tsx` with ConnectionBadge (dropped/suppressed/resync counters + 45s stale warning), SummaryStrip, ClusterPanel (leader Crown pulse), AppliancesGrid (color-coded DPU state), EventTicker (last 12 events), InspectorDrawer. Sidebar `/topology-v2 · Live` entry. **Tests**: 14 dashd broadcaster tests + 10 dashw hub tests + 17 SPA store tests + 8 SPA hook tests; **dashd 26 packages green**, **dashw 8 packages green**, **SPA 234/234 tests + Vite build clean**. Comprehensive design + **Future Scopes (14 entries)** spec at [docs/dashd-features/topology-streaming-design.md](dashd-features/topology-streaming-design.md). Browser ↔ dashw contract enforced — operators cannot bypass the BFF. |
| 15 | PE-G6 follow-up — `EtcdElector` background leader-observer goroutine | ✅ Done | 2026-06-13 | 2026-06-13 | **Closes the PE-G6 known limitation** where follower nodes' `/admin/topology` (and `ClusterService.GetTopology`) returned `leader_id: ""` until somebody explicitly called `ObserveCurrentLeader`. Added an `observeLoop(ctx)` goroutine started in `NewEtcdElector` that watches `concurrency.Election.Observe(ctx)` for leader-key changes and updates the cached `currentLeader` atomically — handover events propagate within ~50ms in tests. Capped exponential backoff (200ms → 5s) re-Observes on transient stream loss. `observeCancel` stops the loop deterministically from `Close()`. **Tests** (`internal/ha/leader/etcd_test.go`): `TestLeaderObserver_FollowerSeesLeaderWithoutExplicitCall` + `TestLeaderObserver_FollowerSeesLeaderHandover` (after `nodeA.Close()` + `nodeB.AwaitLeadership`, the follower's `LeaderID()` flips from `node-a` → `node-b` within 5s). **All 26 dashd packages still green**. **Live e2e** in 05-full-console fleet: dashd-1 + dashd-2 followers both serve `"leader_id":"dashd-3"` in `/admin/topology` (vs. `""` before), and `is_leader:true` set correctly on dashd-3's node from every follower's perspective. Design doc: [docs/dashd-features/topology-operator-polish.md](dashd-features/topology-operator-polish.md). |
| 16 | PE-G7 follow-up — Operator-facing Cordon / Uncordon button in `/topology-v2` SPA drawer | ✅ Done | 2026-06-13 | 2026-06-13 | **One-click DPU lifecycle from the live topology page.** Inspector drawer shows a context-aware button: amber "Cordon DPU" when uncordoned, emerald "Uncordon DPU" when cordoned. POSTs to `/api/v1/inventory/{id}/cordon` (or `/uncordon`) through dashw's existing reverse proxy — browser still **never** talks to dashd directly, preserving the PE-G7 contract. Body `{"reason":"operator action from /topology-v2"}` is audited by dashd. Result banner renders inline; drawer does NOT optimistically update — waits for dashd's `KIND_DPU_STATE` event to come back through the broadcaster → hub → SPA reducer (or the 30s snapshot refresh when streaming is off). **234 SPA tests still green**, Vite build clean. Design doc: [docs/dashd-features/topology-operator-polish.md §4](dashd-features/topology-operator-polish.md#4-slice-d--cordon--uncordon-button-in-topology-v2-spa). |
| 17 | PE-G7.1 — SSE event provenance (`source` + `via` stamping by dashw on every fan-out frame) | ✅ Done | 2026-06-13 | 2026-06-13 | **Every SSE frame now identifies which dashd produced it and which dashw relayed it.** New `cfg.NodeID` field on dashw (defaults to OS hostname, override via `--node-id` / `DASHW_NODE_ID`). New `HubConfig.UpstreamLabel` (set to `cfg.DashdGrpcAddr`) + `HubConfig.SelfLabel` (set to `cfg.NodeID`) thread through to `Hub.buildFrame` which byte-splices `,"source":"X","via":"Y"` before the trailing `}` of the protojson — preserves PE-G7 D2 marshal-once-fanout-many. Empty labels passthrough cleanly. SPA reads via `lastSource` / `lastVia` on the store and surfaces in: ConnectionBadge sub-line (`dashd-1:9443 → edce4b15fcdc`), InstructionBanner ("deltas arrive via SSE from `X` relayed by `Y`"), EventTicker (new 4th column "Source → Via"). **6 new tests** in `internal/cluster/source_via_test.go` (both-labels / empty-passthrough / source-only / quote-escaping / non-object-passthrough / end-to-end `Hub.buildFrame`). **All 8 dashw packages green**. Live wire sample confirmed: `data: {"kind":"KIND_SNAPSHOT",...,"source":"dashd-1:9443","via":"edce4b15fcdc"}`. Design doc: [docs/dashd-features/sse-event-provenance.md](dashd-features/sse-event-provenance.md). |
| 18 | PE-G7.1 — `dashctl topology [--follow]` CLI parity | ✅ Done | 2026-06-13 | 2026-06-13 | Cross-link of #13 — see row #13 above. Design doc: [docs/dashd-features/topology-operator-polish.md §2](dashd-features/topology-operator-polish.md#2-slice-a--dashctl-topology---follow). |
| 19 | Post-GA cleanup window (T1/T2/T3 — dashw aggregator collapse, shared broadcaster extraction, etc.) | ⏳ Scheduled | — | — | **Pure tech-debt reductions**, no user-visible feature value. Scheduled deliberately AFTER `dashd-2.0.0` GA so it doesn't compete with feature work for review attention. Full backlog with priority tiers + risk + effort estimates: [docs/recommended-postGA-cleanup.md](recommended-postGA-cleanup.md). |
| 20 | **PE-3a** — dash-sim per-DPU + per-ENI + per-VNET counter rollups; `dash-sim-client dpu-counters` operator subcommand | ✅ Done | 2026-06-14 | 2026-06-14 | **Closes gate PE-G8.** New `dashapi.v1.GetDpuCounters` RPC + `CounterBucket` / `ScopedCounters` / `DpuCountersRequest` / `DpuCountersResponse` messages (legacy per-object `GetCounters` preserved verbatim for back-compat). Sim's `counters.Registry` gains typed `Bucket` + `SnapshotBucket`/`TotalBucket`/`Rollup`/`RollupAll` aggregators that walk `model.Store.AllKeys()` and sum by first-component scope (ENI rollup over `OBJECT_KIND_ENI` + `OBJECT_KIND_ENI_ROUTE` + `OBJECT_KIND_ACL_IN` joined keys; VNET rollup over `OBJECT_KIND_VNET` + `OBJECT_KIND_VNET_MAPPING`). Sim `Server` gains `WithDeviceID` setter so `DpuCountersResponse.device_id` is populated. `dash-sim-client` ships new `dpu-counters` subcommand with `--include-enis`, `--include-vnets`, `--eni-names`, `--vnet-keys`, `--watch`, `--interval`, and four render formats (`-o table\|json\|yaml\|csv` — CSV new for spreadsheet workflows). Fault injection wires `"GetDpuCounters"` op. **No dashd involvement** — fully standalone. **Tests**: 60 new UTs across 6 packages (`counters` 18, sim `server` 11, sim-client `client` 4, sim-client `render` 16, sim-client `cmd` 7) + **4 in-process integration tests** spinning a real sim+client wire path through localhost loopback gRPC. **100% coverage** on every new function (5 sim funcs incl. `GetDpuCounters`/`scopesForKind`/`scopedRollups`/`bucketToProto`/`WithDeviceID`; client SDK `GetDpuCounters`; render `ParseFormatExt`/`DpuCounters`/`envelopeDpu`/`envelopeBucket`/`writeDpuTable`/`writeDpuCSV`/`bucketCols`/`bucketRow`/`csvBucketRow`/`orDash`; `oneWatchTick`). Counter pkg as a whole hit **100.0%**. Module-wide test results: **4/4 dash-sim packages green**, **3/3 dash-sim-client packages green**, **4/4 integration tests green** in <2s. Design doc with **10 Future Scopes** (per-flow + caps, per-namespace, top-N by rate, decimal-friendly fields, diff snapshots, SAI-canonical names, payload-introspection scope walk, streaming from sim, reset/baseline, histograms): [docs/dashd-features/dash-sim-counter-rollups.md](dashd-features/dash-sim-counter-rollups.md). Per-flow + scalability work deferred to PE-3c with explicit Future-Scope rationale documenting the scale constraints (1M+ flows on real DPUs). |
| 21 | **PE-3b** — dashd ingests sim counters via `GetDpuCounters`; `counter_mapper.go` (Option B) translates generic sim shape → typed `dashcenter.v1.CounterReport`; per-DPU `CounterStore`; runtime config knobs (enable, poll interval, per-DPU override); admin endpoint `GET /admin/counters/{dpu_id}` + `POST /admin/counters/poll-interval` | ✅ Done | 2026-06-14 | 2026-06-14 | **Closes gate PE-G9.** New `internal/counters/` package (mapper + store + poller, ~520 LOC + ~720 LOC tests, **99.3% line coverage**). Mapper is the Option B translator: DPU-wide `packets_in/out/drops` → typed `vxlan_decap/vxlan_encap/drop_acl_in` + `flow_table_size = len(enis)+len(vnets)` as the "DPU is busy" signal; per-ENI + per-VNET sub-rollups returned as `map[scope_key]*CounterReport` for the admin endpoint. Caller-supplied `dpuID` always wins over `src.device_id` (sims can be misconfigured; dashd inventory is the source of truth). Store is a per-DPU snapshot cache with drop-on-slow fan-out broadcaster (PE-3c consumer-ready). Poller: 5s default, 100ms min clamp, atomic `SetInterval` + `SetEnabled`, per-DPU `pollTimeout=5s` so one slow DPU doesn't stall the round, transient errors logged at WARN and swallowed (counters are best-effort observability). DpuClient interface gained `GetDpuCounters(ctx,req)`; production `realClient` wraps the gRPC call, `MockClient` gains `CountersResp/CountersErr/CountersFn/GetDpuCountersCallCount`. New `cfg.Observability.Counters{Enabled,PollInterval}` block with full defaults + validation (rejects `<100ms` when enabled, allows `0` when disabled). Three new admin HTTP endpoints: `GET /admin/counters[?dpu=ID]` dumps cached entries with per-ENI + per-VNET sub-blocks via protojson, `POST /admin/counters/poll-interval {"interval":"3s"}` flips cadence at runtime, `POST /admin/counters/enable {"enabled":true|false}` toggles polling. `Server.SetCountersWiring(store,poller)` is the late-injection seam (mirrors `SetClusterService` pattern from PE-G2). main.go wires the always-on poller after the admin server build, stops it cleanly in shutdown order. **Tests**: 60 unit + 4 in-process gRPC e2e using a stub `dashapi.UnimplementedDashApiServer` so the real `realClient.GetDpuCounters` wire path is exercised without inverting the dashd→dash-sim module layering. **All 34 packages green** (27 dashd + 4 dash-sim + 3 dash-sim-client) — zero regressions. Per-DPU poll-interval override (Future Scope) explicitly deferred to PE-3c per design doc §11. |
| 22 | **PE-3c / PD-G5** — `ObservabilityService.GetCounters` server-streaming impl + `dashd_observability_broadcaster_*` Prom metrics + REST/SSE `/v1/observability/counters` + dashctl `counters [--follow]` (REST **and** gRPC backends; gRPC backend lands here) + dashw counter Hub multiplexer (`/api/console/counters[/stream|/_stats]`) + SPA counter widget (top-line numbers + 60s sparklines) in `/topology-v2` DPU inspector drawer | ✅ Done | 2026-06-14 | 2026-06-14 | **Closes existing gate PD-G5 → Phase PD now 5/5.** Proto: new `CounterEvent` wrapper (Pattern A, mirrors PE-G7 `TopologyEvent` exactly — `Kind` enum + `oneof body {report\|notice}` + monotonic `event_id` + 6 sentinel kinds incl. `KIND_DROPPED/RATE_LIMITED/RESYNC/KEEPALIVE`); `CounterRequest.resume_after_event_id` added. **Pattern Reconnaissance catch** (recorded as §10.11 anti-pattern in [agent-operating-discipline.md](agent-operating-discipline.md)): first draft proposed `_meta.*` sentinel-in-payload encoding to avoid a proto change; user caught it by asking how other streams are implemented — established PE-G7 envelope convention won. Discipline doc gained §0.2.1 Pattern Reconnaissance non-negotiable. **dashd** (28/28 pkg green): new `internal/observability/broadcaster/` package (~800 LOC, **98.7% coverage, 60 tests**) — marshal-once Frame, ring buffer + ResumeAfterEventID replay, drop-on-slow + `KIND_DROPPED` synth, leaky-bucket rate limit + per-dpu coalesce window, single global keepalive, per-subject + global caps; `bridge.go` adapts `counters.Store.Subscribe` → `Broadcaster.Publish`; `GetCounters` gRPC + `/v1/observability/counters[/stream]` REST/SSE handlers; main.go wires Broadcaster lifecycle before server construction; ultra-flexible config block `observability.counters.stream.*` + `per_dpu_overrides` map with cross-field validation (`min_grpc ≤ default_interval`, `burst ≥ rate`, `per_subject ≤ global`). **dashctl** (9/9 pkg green): new `pkg/client/rest/observability.go` + `pkg/client/grpc/counters.go` (CountersClient with `StreamCounters`/`GetCountersSnapshot`); new `counters` subcommand with `--follow --dpu --since-id --json --csv --backend rest\|grpc --grpc-endpoint`; auto-derive `grpc-endpoint = restPort + 1000`; protojson `int64` strings + custom `EventID` JSON type accepting either string or number. **dashw** (9/9 pkg green): new `internal/observability/hub.go` (~600 LOC, 20 tests) — Q2 decision: one lazy upstream gRPC stream per **subscribed** DPU id, refcounted, GC'd after `upstream_idle_gc=30s` of refcount=0; PE-G7.1 byte-splice `source` + `via` mirrored; `replayResume`, `fanoutResyncForDpu`, exp backoff per-stream; new `internal/observability/handler.go` (16 tests via custom `flushRecorder` to avoid `httptest` SSE deadlocks); routes `/api/console/counters[/stream\|/_stats]` plumbed into router + server. **SPA** (311/311 tests green, build clean — **77 net-new tests**): new `stores/counters-store.ts` Zustand reducer (29 tests, ring cap=120/DPU, 6-kind reducer, event_id ratchet, provenance), new `queries/useCounterStream.ts` (17 tests — FakeEventSource pattern, tab-visibility pause grace=60s, exp backoff 500ms→15s), new `views/topology-v2/CounterWidget.tsx` (19 tests — pure helpers `smooth(window=5)` + `sparklinePath()` exported; 2×2 grid; auto-scale; SVG sparklines, no external chart lib; flat-line case + division-by-zero guarded); slotted into `TopologyV2View.tsx` `InspectorDrawer` when `selectedKind === 'dpu'`. **Live e2e in 05-full-console**: dashd REST snapshot returns 10 DPU reports; SSE stream emits 10 `event: snapshot` then live `event: report id:N`; dashw stream shows PE-G7.1 provenance `"source":"dashd-1:9443","via":"5177b98fd853"`; lazy upstream activated on first watcher (0→1 Watchers, 0→1 UpstreamCount); `dashctl counters` table + `--json --dpu` + `--follow` + `--backend=grpc` all work; kill-a-sim freezes that DPU's `sampled_at` while others advance (graceful degradation by design). Feature doc: [counter-streaming.md](dashd-features/counter-streaming.md) — full §1-§9 + 6 Future Scopes; cross-linked in [features.md](dashd-features/features.md). Post-GA cleanup: T2.1 promoted to **T1.3** (broadcaster extraction now justified by 2 instances), new **T1.4** filed (`Notice` consolidation `cluster.proto`→`types.proto`, zero wire impact). Slice plan in `/memories/session/pe-3c-pd-g5-plan.md`. |

**Status legend**: ⬜ Not started · ⏳ In progress · ✅ Done · ❎ Deferred

### Planned — dash-sim parity roadmap

| # | What | Status | Started | Done | Notes |
|---|---|---|---|---|---|
| 23 | **PE-4 Phase 1** — dash-sim flow table + slow/fast path + flow CRUD APIs + flow lifecycle counters + TCP state tracking + age-out + resimulation. Closes the **#1 gap** in the [dash-sim-on-par-with-sonic-audit](dash-sim-on-par-with-sonic-audit.md) (Gap 1: "No flow table / no conntrack"). | ⬜ Not started | — | — | See row details above. |
| 24 | **PE-4 Phase 2** — dash-sim full packet transforms + ~50 real per-ENI counters + drop-reason counters. Closes audit Gaps 2, 3, 5. | ⬜ Not started | — | — | Depends on #23. |
| 25 | **PE-4 Phase 3** — dash-sim HA modelling. Closes audit Gaps 4, 6. | ⬜ Not started | — | — | Depends on #23. |

### Planned — referential integrity validation

| # | What | Status | Started | Done | Notes |
|---|---|---|---|---|---|
| 26 | **RI Phase 1** — dash-sim + dash-sim-client Apply-side FK validation for all 25 southbound `dashapi.v1` FK relationships. Bottom-layer defense: the sim is the lowest layer and the ONLY validation point for direct `dash-sim-client apply`. Config `--strict-refs` flag (default true). New `dash-sim-client validate <file>` subcommand for pre-flight FK checks. | ✅ Done | 2026-06-14 | 2026-06-15 | **Delivered**: `model/refs.go` (25-rule FK table + `checkRefs()`), `model/refs_test.go` (51 unit tests, 100% coverage on core functions), `test/integration/refs_test.go` (9 gRPC integration tests), `--strict-refs` CLI flag, `dash-sim-client validate -f` subcommand. Updated docs: RI design doc §2.2/§2.4/§8.1, tutorials (dash-sim §3/§10a, dash-sim-client §7a, RUN_AND_TEST §9c), labs (01 Step 14b, 02 Step 10a, 03 Step 14a, 05 Lab 14, 06 Lab 15). All 42 packages green (dash-sim 5 + dashd 28 + dashctl 9). |
| 27 | **RI Phase 2a** — dashd Put-side FK validation for the northbound FKs: RoutePolicy→service_tunnel, HaSet→DPU IDs (inventory). Extended `namespace.Validator` with FK checks. | ✅ Done | 2026-06-15 | 2026-06-15 | **Delivered**: `CheckRoutePolicy` now validates `service_tunnel` next_hop_target exists; `CheckHaSet` validates `member_dpu_ids[]` against inventory; `Validator.WithInventory()` method; new tests. Note: ENI→qos and ENI→meter_policy don't exist at the dashcenter.v1 proto level (sim-only); VnetMapping→tunnel also sim-only. |
| 28 | **RI Phase 2b** — dashd Delete-side orphan protection. `CheckDelete()` rejects DELETE when dependents still reference the target. `force=true` bypass for emergency. | ✅ Done | 2026-06-15 | 2026-06-15 | **Delivered**: `CheckDelete(ctx, ns, kind, name, force)` in namespace.Validator; scans for vnet→(eni, vnet_mapping), eni→(acl_policy, route_policy), service_tunnel→(route_policy) dependents; wired into `controlPlaneService.Delete()`; 12 new tests (reject+ok+force bypass for each kind). |
| 29 | **RI Phase 3** — Operational tooling: `dashctl validate -f` + `dash-sim-client validate -f` subcommands. CLI pre-flight validation against live stores. | ✅ Done | 2026-06-15 | 2026-06-15 | **Delivered**: `dashctl validate -f <manifest>` (loads manifests, PUTs each to dashd, reports summary table with pass/fail per object); `dash-sim-client validate -f <file>` (same for dash-sim). REST endpoint + SPA card deferred to future. All 45 packages green. |

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
