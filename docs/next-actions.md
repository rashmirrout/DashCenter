# dashd — Next Actions Plan

> **Purpose**: Ordered, trackable plan for the post-PD-partial work. Companion to [`specs/Impl-Plan/impl-phases.md`](../specs/Impl-Plan/impl-phases.md) (the milestone-level tracker). This file is the *operator's worklist* — what to do next, in what order, and why.
> **Created**: 2026-06-11
> **Last updated**: 2026-06-11

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

**Latent work documented but not in this plan**:
- Denial auditing (4xx short-circuits never reach audit middleware) — 30-line cleanup, no PE/PF dependency, queued as a follow-up PR
- goleak (P1B-G10) — housekeeping pass, separate slice

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
| PE-1 Diagnostics | `internal/flow/` | `TraceFlow`, `ExplainMatch`, `ExplainDrift`, `GetAclHitStats`, `TriggerResimulation` | PE-G1, PE-G2 |
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
| 1 | Promote `tmp-fleet/` → `deploy/test-setup/04-ha-fleet/` (compose + configs + manifest + scripts + README + handson) | ✅ Done | 2026-06-11 | 2026-06-11 | 103-object pre-baked scenario (12 vnets, 4 service tunnels, 30 ENIs, 30 vnet mappings, 12 route policies, 12 ACL policies with 120 rules, 3 HA sets). Live smoke-test passed: leader elected, manifest applied, fan-out across 3 dashd verified, HA switchover SSE green, kill-leader failover preserves all 103 objects. |
| 2 | Add `test/integration/ha_fleet/` with `//go:build integration_ha_fleet` automation | ✅ Done | 2026-06-11 | 2026-06-11 | `src/impl-go/dashd/test/integration/ha_fleet/fleet_test.go` (~250 LOC) drives `start-fleet`, applies manifest via docker-run dashctl with bind-mount, asserts per-kind counts, exercises switchover SSE, kills the leader, asserts state survival on the new leader. `go vet -tags=integration_ha_fleet` clean; full run reserved for CI / on-demand. |
| 3 | Write `docs/tutorial/18-ha-and-migration-handson.md` + update tutorial index | ✅ Done | 2026-06-11 | 2026-06-11 | Slot 18 not 10 (10–17 were occupied). 8-step walkthrough: prereqs → fleet up → inspect leader election → apply 103 objects → switchover SSE → failover SSE → 10-phase migration → kill-leader-mid-migration survival → cleanup. Cross-linked from `04-ha-fleet/README.md` and `deploy/test-setup/README.md`. |
| 4 | PE-1 — `internal/flow/` Diagnostics (TraceFlow / ExplainMatch / ExplainDrift / GetAclHitStats / TriggerResimulation) | ⬜ Not started | — | — | PE-G1, PE-G2 |
| 5 | PE-2 — Saga + EventBroadcaster + gNMI Subscribe bridge | ⬜ Not started | — | — | PE-G5; saga was already partially landed under PC-G8 — confirm reuse |
| 6 | PE-3 — dash-sim per-ENI counter emission + dispatch.SnapshotCounters wiring | ⬜ Not started | — | — | unblocks #8 |
| 7 | PE-4 — MigrationService `ExportMigrationBundle` / `ImportMigrationBundle` (streaming gRPC) | ⬜ Not started | — | — | closes the two PC `Unimplemented` stubs |
| 8 | PD-G5 — `internal/observability/{counter_store,broadcaster}.go` + `GetCounters` follow mode + `dashctl counters --follow` | ⬜ Not started | — | — | depends on #6 |
| 9 | Denial auditing — write audit entry from `auth.interceptor` on 401/403 before short-circuit | ⬜ Not started | — | — | independent; can land any time |
| 10 | Tag `dashd-2.0.0` GA — CHANGELOG, README badge, green CI | ⬜ Not started | — | — | depends on #8, #9, #4–7 |

**Status legend**: ⬜ Not started · ⏳ In progress · ✅ Done · ❎ Deferred

---

## How to use this file

1. Pick the lowest-numbered ⬜ row in the tracker.
2. Flip it to ⏳ and fill in `Started`.
3. Land the work in a focused PR. Land code only — do not edit this file from the PR.
4. After merge, edit this file: flip to ✅, fill `Completed`, link the PR in `Notes / links`.
5. If you change scope, append a short note under the row (do not silently rewrite the description — the audit trail matters).

When all rows are ✅, archive this file by renaming it to `docs/next-actions-2026-Q2.md` and open a fresh one for the next horizon (PF / `dashd-3.x`).
