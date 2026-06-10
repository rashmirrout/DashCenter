# dashw — Implementation Phase Tracker

> **Purpose**: Single source of truth for `dashw` (DashCenter Web Console)
> implementation progress.
> **Ground truth**: [`specs/HLD/dashw-web-hld.md`](../HLD/dashw-web-hld.md) and
> [`specs/LLD/dashw-web-lld.md`](../LLD/dashw-web-lld.md).
> **Companion**: dashctl phase tracker is [`dashctl-impl-phases.md`](dashctl-impl-phases.md);
> dashd phase tracker is [`impl-phases.md`](impl-phases.md).
> **Last updated**: 2026-06-11

---

## Legend

| Symbol | Meaning |
|--------|---------|
| ✅ | Complete — code written, tests pass, gate verified |
| ⏳ | In progress |
| ❌ | Not started |
| ⬜ | Deferred — intentionally skipped for this phase |

---

## Overall progress

| Phase | Objective | Status | Gates |
|---|---|---|---|
| **Phase A** — REST-only (functional console) | BFF proxy + aggregation + SPA with all 13 views using REST polling. No WebSocket, no gRPC. | ❌ Not started | 0 / 18 |
| **Phase B** — gRPC streaming (real-time) | WebSocket ↔ gRPC bridge in BFF; real-time DPU status, events, flows, counters, audit in SPA. | ❌ Not started | 0 / 12 |
| **Phase C** — Diagnostics & advanced (full fidelity) | TraceFlow animation, ACL hit stats streaming, HA/Migration stream UIs, fault injection, E2E tests. | ❌ Not started | 0 / 10 |

> **Dependency**: Phase A can ship against dashd Phase 1B (REST is feature-complete).
> Phase B requires dashd gRPC streaming RPCs (dashd Phase 2 PA/PB).
> Phase C requires dashd Diagnostics RPCs (dashd Phase 2 PE).

---

## Implementation order

```
dashd 1B ✅ ──────────────► dashw Phase A (REST-only)
                                    │
dashd 2 PA (Observability) ────────►├──► dashw Phase B (WS streams)
dashd 2 PB (Operations)  ──────────┘        │
                                            │
dashd 2 PC–PE (Diag/HA/Migration) ─────────►└──► dashw Phase C (Diagnostics)
```

---

## Phase A — REST-only (functional console)

### Objective

Deliver a **fully functional web console** with all 13 views, BFF
proxy + aggregation, Admin CRUD operations, Command View, and Debug
tools. All data fetched via REST polling (TanStack Query). No
WebSocket, no gRPC. The console is independently valuable at this phase.

### Scope

| In scope | Out of scope (Phase B/C) |
|---|---|
| BFF Go binary with `go:embed` SPA serving | WebSocket ↔ gRPC bridge |
| REST proxy to dashd `:8443` and Admin `:7443` | Real-time streaming |
| Aggregation endpoints (fleet/summary, dpu/detail, topology, vnet/detail, capacity) | Live sparkline charts (use static snapshots) |
| All 13 SPA views with REST polling | Packet counter streaming |
| Admin Ops: Create, Edit, Delete, Batch upload, Reconcile | Audit log streaming |
| Command View with execute-through-BFF | Drain/Migration/HA stream UI |
| Flow Trace via `POST /v1/simulate` (static result) | Animated trace (show result only, no animation) |
| Debug View: Raw API, Admin endpoints, Sim inspector | WebSocket tester |
| Design system: dark theme, glass cards, status badges | |
| Responsive layout (desktop-first, mobile-degraded) | |
| Docker Compose (console + dashd + 5 sims) | |
| BFF + SPA unit/integration tests | E2E tests |
| Lighthouse CI performance validation | |

### Sub-phases

#### A1 — Project scaffolding & BFF core

| Gate | Task | Status |
|---|---|---|
| **A1-G1** | Initialize `src/impl-go/console/` Go module: `go.mod`, `cmd/dashw/main.go`, `internal/config/config.go` | ❌ |
| **A1-G2** | Initialize `src/impl-web/console/` SPA: `package.json`, Vite config, TypeScript config, Tailwind config, `index.html`, `main.tsx` | ❌ |
| **A1-G3** | BFF server core: `server.go`, `router.go`, `middleware.go` (logging, recovery, CORS, request-id) | ❌ |
| **A1-G4** | SPA `go:embed` handler: `spa.go`, `embed.go`, serves `index.html` for all non-API paths | ❌ |
| **A1-G5** | BFF health endpoint: `GET /healthz` with dashd connectivity check | ❌ |
| **A1-G6** | Dockerfile multi-stage build: node → go → distroless | ❌ |
| **A1-G7** | Makefile: `web-build`, `bff-build`, `docker-console` targets | ❌ |

**Gate verification:** `make bff-build` produces `dashw` binary; `./dashw` serves blank SPA on `:8080`; `/healthz` returns JSON.

#### A2 — REST proxy layer

| Gate | Task | Status |
|---|---|---|
| **A2-G1** | `proxy/rest.go`: reverse-proxy `/api/v1/*` → dashd `:8443` with path rewrite, error handler, timeout | ❌ |
| **A2-G2** | `proxy/admin.go`: reverse-proxy `/api/admin/*` → dashd `:7443` | ❌ |
| **A2-G3** | `proxy/sim.go`: dynamic reverse-proxy `/api/sim/{simId}/admin/*` → dash-sim | ❌ |
| **A2-G4** | Integration test: start BFF + mock dashd; verify PUT/GET/DELETE pass through with correct path rewrite | ❌ |

**Gate verification:** `curl http://localhost:8080/api/v1/default/vnets` returns dashd response; `curl http://localhost:8080/api/admin/health` returns admin health.

#### A3 — Aggregation endpoints

| Gate | Task | Status |
|---|---|---|
| **A3-G1** | `aggregation/types.go`: all response types (FleetSummary, DpuDetail, TopologyGraph, VnetDetail, CapacityStats) | ❌ |
| **A3-G2** | `aggregation/fleet.go`: `GET /api/console/fleet/summary` with parallel fan-out + singleflight | ❌ |
| **A3-G3** | `aggregation/dpu_detail.go`: `GET /api/console/dpu/{dpuId}/detail` | ❌ |
| **A3-G4** | `aggregation/topology.go`: `GET /api/console/topology` with graph computation | ❌ |
| **A3-G5** | `aggregation/vnet_detail.go`: `GET /api/console/vnet/{vnetName}/detail` | ❌ |
| **A3-G6** | `aggregation/capacity.go`: `GET /api/console/stats/capacity` | ❌ |
| **A3-G7** | Unit tests for merge logic + integration tests with mock dashd responses | ❌ |

**Gate verification:** All 5 aggregation endpoints return valid JSON with merged data from dashd; singleflight coalesces concurrent requests.

#### A4 — SPA design system & shared components

| Gate | Task | Status |
|---|---|---|
| **A4-G1** | Design tokens: `globals.css` (CSS custom properties), `fonts.css` (@font-face Inter + JetBrains Mono), `animations.css` (pulse, glow, fade-in) | ❌ |
| **A4-G2** | Tailwind config: colors, fonts, border-radius, shadows mapped to CSS vars | ❌ |
| **A4-G3** | shadcn/ui primitives installed: Button, Card, Dialog, Input, Select, Table, Tabs, Toast, Badge, Sheet, Skeleton, Tooltip | ❌ |
| **A4-G4** | Layout components: `AppShell.tsx`, `Sidebar.tsx` (with nav groups), `TopBar.tsx`, `Breadcrumb.tsx`, `PageHeader.tsx` | ❌ |
| **A4-G5** | Feedback components: `GlassCard.tsx`, `StatusBadge.tsx`, `StatsCard.tsx`, `EmptyState.tsx`, `ErrorState.tsx`, `LoadingSkeleton.tsx`, `StalenessIndicator.tsx` | ❌ |
| **A4-G6** | Data components: `DataTable.tsx` (with sorting, filtering, pagination, column visibility), `VirtualizedTable.tsx` | ❌ |
| **A4-G7** | Visualization components: `CapacityGauge.tsx` (SVG radial), `HealthDonut.tsx`, `SparklineChart.tsx` (Recharts) | ❌ |
| **A4-G8** | Topology components: `TopologyGraph.tsx` (React Flow wrapper), `DpuNode.tsx` (hexagon), `EniNode.tsx`, `VnetNode.tsx` | ❌ |
| **A4-G9** | Form components: `IpInput.tsx`, `MacInput.tsx`, `CidrInput.tsx`, `PortRangeInput.tsx`, `NamespaceSelector.tsx`, `LabelEditor.tsx` | ❌ |
| **A4-G10** | Common components: `CommandPalette.tsx` (Cmd+K), `JsonViewer.tsx`, `CodeEditor.tsx`, `CopyButton.tsx`, `TimeAgo.tsx`, `ExportButton.tsx` | ❌ |
| **A4-G11** | Component unit tests (GlassCard, StatusBadge states, CapacityGauge math, DataTable sorting) | ❌ |

**Gate verification:** Storybook-like visual check (or component test screenshots); all component tests pass.

#### A5 — SPA data layer

| Gate | Task | Status |
|---|---|---|
| **A5-G1** | `api/client.ts`: base fetch wrapper with `ApiError`, `api.get/put/post/delete` | ❌ |
| **A5-G2** | `api/types.ts`: complete TypeScript types mirroring proto (VnetSpec, EniSpec, AclPolicySpec, etc. + BFF aggregation types + WSFrame) | ❌ |
| **A5-G3** | `api/dashd-rest.ts`: per-kind CRUD (vnetApi, eniApi, aclPolicyApi, routePolicyApi, haSetApi, serviceTunnelApi, inventoryApi, opsApi) | ❌ |
| **A5-G4** | `api/dashd-admin.ts`: admin API calls (health, leader, inventory, drift, observed, eniPlacement) | ❌ |
| **A5-G5** | `api/console-api.ts`: BFF aggregation calls (fleetSummary, dpuDetail, topology, vnetDetail, capacityStats) | ❌ |
| **A5-G6** | `queries/keys.ts`: query key factory (fleet, dpu, vnet, eni, policy, mapping, tunnel, health, capacity, inventory) | ❌ |
| **A5-G7** | Query hooks: `useFleetSummary`, `useFleetTopology`, `useDpuDetail`, `useVnetDetail`, `useVnetList`, `useEniList`, `useAclPolicies`, `useRoutePolicies`, `useServiceTunnels`, `useDashdHealth`, `useLeader` — with correct staleTime + refetchInterval | ❌ |
| **A5-G8** | Mutation hooks: `usePutVnet`, `usePutEni`, `usePutAclPolicy`, `usePutRoutePolicy`, `useDeleteResource`, `useReconcile`, `useSimulateFlow` — with toast + cache invalidation | ❌ |
| **A5-G9** | Zustand stores: `fleet-store`, `dpu-store`, `event-store`, `ws-connection-store`, `ui-prefs-store` (persisted), `trace-history-store`, `command-store` | ❌ |
| **A5-G10** | Form validation schemas (zod): all 8 resource schemas + simulateRequestSchema + RESOURCE_SCHEMAS registry | ❌ |
| **A5-G11** | `lib/constants.ts`: POLL_INTERVALS, WS_ENDPOINTS | ❌ |
| **A5-G12** | `lib/format.ts`: formatIp, formatMac, formatBytes, formatDuration, formatPercent | ❌ |
| **A5-G13** | `lib/cn.ts`: clsx + tailwind-merge utility | ❌ |
| **A5-G14** | MSW mock handlers + test data factories | ❌ |
| **A5-G15** | Unit tests: stores (ring buffer, connection status), format utils, schema validation | ❌ |

**Gate verification:** All query/mutation hooks work against MSW mocks; store tests pass; schema validation catches invalid input.

#### A6 — SPA routing & views (read-only)

| Gate | Task | Status |
|---|---|---|
| **A6-G1** | `router.tsx`: route definitions with lazy imports for all 13 views | ❌ |
| **A6-G2** | Dashboard View: FleetHealthPanel (donut), StatsCards (3), CapacityPanel (4 gauges), AlertBanner, MiniTopology | ❌ |
| **A6-G3** | Fleet View: FleetTopology (full graph), FleetTable (DPU data table with row click → navigate), VnetCardGrid, FleetFilters | ❌ |
| **A6-G4** | DPU View: DpuHeader (status badge), EniPipePanel, DpuCapacityPanel (4 gauges), DriftPanel, DpuPolicyPanel, FlowTablePanel (polling) | ❌ |
| **A6-G5** | Vnet View: VnetHeader, VnetTopology, tabs (ENIs, Mappings, Routes, Tunnels) | ❌ |
| **A6-G6** | Routing View: PrefixTreePanel (D3.js), RouteTable, RouteAssociation | ❌ |
| **A6-G7** | Tunnel View: TunnelMapPanel, TunnelTable, OverlayUnderlayToggle | ❌ |
| **A6-G8** | Policy View: AclPolicyList (expandable rules), RoutePolicyList | ❌ |
| **A6-G9** | dashd Health View: LeaderPanel, ClusterHealthPanel, ConnectedDpuTable, ReconcilePanel | ❌ |
| **A6-G10** | Audit View (Phase A): polling-based, basic table with filters (no streaming yet) | ❌ |

**Gate verification:** All 10 read-only views render correctly with data from REST polling; navigation works; responsive at 1440px and 1024px.

#### A7 — SPA views (write + interactive)

| Gate | Task | Status |
|---|---|---|
| **A7-G1** | Admin Ops View: ResourceTypeSelector, CreateResourceForm (dynamic zod form), EditResourceForm (pre-fill + diff), DeleteDialog, BatchUploader (YAML/JSON), ReconcileButton | ❌ |
| **A7-G2** | Flow Trace View: TraceInputForm, useSimulateFlow → TraceResultPanel (verdict, matched rules). Static result display (no animation in Phase A). TraceHistory (session storage). | ❌ |
| **A7-G3** | Command View: CommandCatalog (sidebar), CommandDetail, CommandBuilder (interactive flag form), CommandPreview (copy), Execute button → API call → CommandOutput | ❌ |
| **A7-G4** | Debug View: RawApiCaller (method/URL/body/send), AdminEndpoints (quick buttons), SimInspector (per-sim dropdown) | ❌ |
| **A7-G5** | `lib/command-registry.ts`: metadata for all dashctl commands (verb, kind, flags, types, examples, category) | ❌ |
| **A7-G6** | Integration tests: Admin create ENI flow, Command View execute GET, Flow Trace simulate | ❌ |

**Gate verification:** Create/edit/delete resources through Admin Ops; simulate flow and see result; execute commands through Command View; raw API calls through Debug.

#### A8 — Deployment & polish

| Gate | Task | Status |
|---|---|---|
| **A8-G1** | Docker Compose: `deploy/compose/dashw.yml` with console + dashd + 5 sims | ❌ |
| **A8-G2** | `README.md` for `src/impl-go/console/` and `src/impl-web/console/` | ❌ |
| **A8-G3** | Accessibility pass: focus indicators, aria labels, skip link, keyboard nav, color+text status | ❌ |
| **A8-G4** | Performance validation: Lighthouse CI (LCP < 2s, TTI < 3s), bundle size check (< 500KB gzip initial) | ❌ |
| **A8-G5** | Error boundary: per-view error boundary, query error states, 403 handling | ❌ |
| **A8-G6** | End-to-end smoke test: `docker compose up` → browser → navigate all views → create ENI → verify | ❌ |

**Gate verification:** `docker compose up` starts console at `:8080`; Lighthouse passes; all views accessible; create/delete ENI works end-to-end.

### Phase A exit criteria

- [ ] All 18 gates (A1-G1 through A8-G6) verified
- [ ] BFF binary builds cleanly (`CGO_ENABLED=0`)
- [ ] SPA builds cleanly (`npm run build`)
- [ ] Docker image builds and runs
- [ ] All 13 views render with data from dashd
- [ ] Admin CRUD operations work
- [ ] Flow trace returns results
- [ ] Command View executes commands
- [ ] BFF tests pass (`go test ./...`)
- [ ] SPA tests pass (`npm test`)
- [ ] Lighthouse LCP < 2s, bundle < 500KB gzip
- [ ] No accessibility violations (axe-core)

---

## Phase B — gRPC streaming (real-time)

### Objective

Add **real-time data streaming** via WebSocket ↔ gRPC bridge in the BFF.
DPU status, events, flows, counters, and audit stream continuously to
the browser. Views upgrade from REST polling to live updates with
animated indicators.

### Prerequisites

- dashd Phase 2 PA (ObservabilityService gRPC streaming RPCs)
- dashd Phase 2 PB (OperationsService: DrainDpu stream)
- Phase A complete

### Sub-phases

#### B1 — WebSocket ↔ gRPC bridge

| Gate | Task | Status |
|---|---|---|
| **B1-G1** | BFF gRPC client connection: dial dashd `:9443` at startup, graceful fallback if unavailable | ❌ |
| **B1-G2** | `ws/frame.go`: WSFrame + WSError types | ❌ |
| **B1-G3** | `ws/bridge.go`: generic Bridge engine (WS upgrade → gRPC stream → JSON pump → ping/pong keepalive → graceful close) | ❌ |
| **B1-G4** | `ws/handler.go`: per-stream handlers (DpuStatus, Events, Flows, Counters, Audit, Drain, Migration, HaEvents) | ❌ |
| **B1-G5** | Unit test: Bridge with mock gRPC stream → verify WSFrame output, sequence numbering, error frames | ❌ |
| **B1-G6** | Integration test: BFF + mock dashd gRPC → browser WebSocket client receives JSON frames | ❌ |

**Gate verification:** Open WS to `/ws/dpu-status` → receive JSON frames; close browser → gRPC stream cancels.

#### B2 — SPA WebSocket hooks

| Gate | Task | Status |
|---|---|---|
| **B2-G1** | `hooks/useWebSocket.ts`: generic hook with auto-reconnect (1s→30s jittered backoff), connection status tracking | ❌ |
| **B2-G2** | Specialized hooks: `useDpuStatus`, `useEvents`, `useFlows`, `useCounters`, `useAudit`, `useDrain`, `useMigration`, `useHaEvents` | ❌ |
| **B2-G3** | `stores/ws-connection-store.ts`: per-URL connection tracking, `overallStatus` derivation | ❌ |
| **B2-G4** | `ConnectionIndicator.tsx` in TopBar: green/amber/red dot based on `overallStatus` | ❌ |
| **B2-G5** | Hook unit tests: connect/reconnect/close lifecycle, message parsing, store updates | ❌ |

**Gate verification:** `useDpuStatus` connects, receives data, updates dpuStore; auto-reconnects on disconnect.

#### B3 — View upgrades (real-time)

| Gate | Task | Status |
|---|---|---|
| **B3-G1** | Dashboard: RecentEventsPanel switches from poll to `useEvents()` WS; live event feed with fade-in animation | ❌ |
| **B3-G2** | DPU View: PacketStatsPanel switches to `useCounters(dpuId)` WS; live sparkline charts with rolling 60-sample window | ❌ |
| **B3-G3** | DPU View: FlowTablePanel switches to `useFlows(dpuId)` WS; live flow table with new-row highlight | ❌ |
| **B3-G4** | Fleet/Dashboard: DPU status indicators pulse live from `useDpuStatus()` WS; StatusBadge state transitions animate | ❌ |
| **B3-G5** | Audit View: AuditFeed switches to `useAudit()` WS; live streaming feed with virtual scroll | ❌ |
| **B3-G6** | EniPipe component: animated flow particles when WS counter data available | ❌ |

**Gate verification:** Open DPU View → see live sparklines updating; open Audit → see new entries appear in real-time; reconnect indicator works.

### Phase B exit criteria

- [ ] All 12 gates (B1-G1 through B3-G6) verified
- [ ] WebSocket connections auto-reconnect with backoff
- [ ] Connection indicator in TopBar reflects status
- [ ] DPU status, events, flows, counters, audit stream in real-time
- [ ] Sparkline charts update live with 60-sample rolling window
- [ ] Audit feed streams with virtual scroll
- [ ] No memory leaks on view navigation (WS cleanup)
- [ ] All existing Phase A tests still pass
- [ ] New WS hook tests pass

---

## Phase C — Diagnostics & advanced (full fidelity)

### Objective

Add **diagnostic visualizations** (animated flow trace, ACL hit stats),
**operational stream UIs** (HA switchover/failover, migration sessions,
DPU drain progress), and **advanced debug** (WebSocket tester). Deliver
E2E test framework.

### Prerequisites

- dashd Phase 2 PC (OperationsService: cordon/uncordon)
- dashd Phase 2 PD (HaService, MigrationService)
- dashd Phase 2 PE (DiagnosticsService: TraceFlow, ExplainMatch, GetAclHitStats)
- Phase B complete

### Sub-phases

#### C1 — Flow Trace animation

| Gate | Task | Status |
|---|---|---|
| **C1-G1** | `FlowAnimator.tsx`: animated SVG pipeline (6 stages), Framer Motion dot traversal, stage highlight on entry | ❌ |
| **C1-G2** | `FlowPipelineStage.tsx`: individual stage rendering with matched rule detail | ❌ |
| **C1-G3** | Flow Trace View upgrade: on simulate result, play animation → then show result panel. Replay from history. | ❌ |
| **C1-G4** | `prefers-reduced-motion` support: skip animation, show result instantly | ❌ |
| **C1-G5** | TraceFlow via gRPC (when dashd PE available): `POST /api/v1/simulate` → gRPC `TraceFlow` RPC with richer result | ❌ |

**Gate verification:** Simulate flow → watch animated dot traverse pipeline → see verdict + matched rules at each stage.

#### C2 — Diagnostics streaming

| Gate | Task | Status |
|---|---|---|
| **C2-G1** | `WS /ws/acl-hits/{eniName}` bridge + `useAclHits` hook | ❌ |
| **C2-G2** | Policy View: ACL hit count overlay on rule rows (real-time from WS) | ❌ |
| **C2-G3** | Debug View: WebSocket tester tab (connect to any `/ws/*`, raw frame log, send custom) | ❌ |

**Gate verification:** Open Policy View → ACL rules show real-time hit counts; WS tester connects and displays raw frames.

#### C3 — HA & Migration stream UI

| Gate | Task | Status |
|---|---|---|
| **C3-G1** | HA View: switchover/failover trigger forms, `useHaEvents()` WS live feed, progress indicators | ❌ |
| **C3-G2** | Migration View: create plan, start session, `useMigration(sessionId)` WS progress stream, phase advance, rollback/abort/commit controls | ❌ |
| **C3-G3** | DPU View: drain button → `useDrain(dpuId)` WS → progress bar + ENI evacuation timeline | ❌ |
| **C3-G4** | Operations: cordon/uncordon buttons on DPU View with confirmation | ❌ |

**Gate verification:** Trigger switchover → see HA events stream; start migration → stream progress; drain DPU → see progress.

#### C4 — Polish & E2E

| Gate | Task | Status |
|---|---|---|
| **C4-G1** | E2E test framework: Playwright or Cypress against Docker Compose stack | ❌ |
| **C4-G2** | E2E test suite: navigate all views, create/delete resources, simulate flow, verify WS streams | ❌ |
| **C4-G3** | Performance re-validation: Lighthouse CI with all features enabled | ❌ |
| **C4-G4** | Documentation: operator guide (how to run, configure, use each view) | ❌ |

**Gate verification:** Full E2E test suite passes against Docker Compose; Lighthouse scores maintained; docs published.

### Phase C exit criteria

- [ ] All 10 gates (C1-G1 through C4-G4) verified
- [ ] Flow trace animation plays correctly
- [ ] ACL hit stats stream to Policy View
- [ ] HA/Migration/Drain stream UIs functional
- [ ] WebSocket tester works in Debug View
- [ ] E2E test suite passes
- [ ] All Phase A + B tests still pass
- [ ] Lighthouse scores maintained (LCP < 2s)
- [ ] Operator documentation complete

---

## Cross-phase dependency matrix

| dashw phase | Requires dashd | Requires dashctl |
|---|---|---|
| Phase A | dashd Phase 1B (REST ✅) | None (dashctl command metadata embedded in SPA) |
| Phase B | dashd Phase 2 PA (ObservabilityService streaming) + PB (OperationsService.DrainDpu) | None |
| Phase C | dashd Phase 2 PC (cordon/uncordon) + PD (HA/Migration) + PE (DiagnosticsService) | None |

---

## Risk register

| Risk | Impact | Mitigation |
|---|---|---|
| **dashd gRPC streaming not ready** | Phase B blocked | Phase A is independently valuable; ship and iterate. Phase B WS hooks degrade gracefully to polling. |
| **Large topology graph performance** | Slow render (>100 DPUs) | React Flow virtual rendering; BFF pre-computes layout; limit visible nodes with zoom levels. |
| **WebSocket memory leaks** | Browser tab memory grows | Strict cleanup on navigation (useEffect cleanup); ring buffers in stores (max 1000 events, 60 counter samples). |
| **SPA bundle too large** | Slow initial load | Manual chunks, lazy loading, tree shaking. Monitor with Vite bundle analyzer in CI. |
| **dashd API changes** | Type mismatches | TypeScript types mirror proto; update types when proto changes. BFF uses proto-generated stubs. |
| **Accessibility regressions** | WCAG violations | axe-core in CI; component-level a11y tests. |

---

## File deliverables per phase

### Phase A

| Layer | New files |
|---|---|
| BFF | `src/impl-go/console/` (entire directory): `go.mod`, `cmd/dashw/main.go`, `internal/{config,server,proxy,aggregation,health,metrics}/*.go`, `embed.go`, `Dockerfile`, `Makefile` |
| SPA | `src/impl-web/console/` (entire directory): `package.json`, `vite.config.ts`, `tailwind.config.ts`, `tsconfig.json`, `index.html`, `src/{api,hooks,stores,queries,components,views,lib,styles}/**/*.{ts,tsx,css}`, `tests/**` |
| Deploy | `deploy/compose/dashw.yml` |

### Phase B

| Layer | New/modified files |
|---|---|
| BFF | `internal/ws/{bridge,handler,frame,reconnect}.go` (new); `internal/server/router.go` (add WS routes) |
| SPA | `hooks/useWebSocket.ts` (new), `hooks/use{DpuStatus,Events,Flows,Counters,Audit,Drain,Migration,HaEvents}.ts` (new); `stores/ws-connection-store.ts` (new); `components/feedback/ConnectionIndicator.tsx` (new); view files modified for WS integration |

### Phase C

| Layer | New/modified files |
|---|---|
| BFF | `internal/ws/handler.go` (add AclHits handler) |
| SPA | `components/visualization/{FlowAnimator,FlowPipelineStage}.tsx` (new); `views/flow-trace/TraceAnimation.tsx` (new); `views/debug/WsTester.tsx` (new); HA/Migration view components (new) |
| Tests | `tests/e2e/` (new Playwright/Cypress test suite) |

---

## Definition of done (all phases)

- [ ] BFF builds cleanly (`CGO_ENABLED=0 go build`)
- [ ] SPA builds cleanly (`npm run build`)
- [ ] Docker image builds and starts
- [ ] All 13 views render with data
- [ ] Admin CRUD operations work end-to-end
- [ ] Real-time streams (Phase B+) function with auto-reconnect
- [ ] Flow trace animation plays (Phase C)
- [ ] All unit/integration tests pass
- [ ] E2E test suite passes (Phase C)
- [ ] Lighthouse: LCP < 2s, TTI < 3s, bundle < 500KB gzip
- [ ] Accessibility: no axe-core critical violations
- [ ] Operator documentation published

---

> **End of implementation plan.** For the architecture and design decisions
> behind these phases, see [`specs/HLD/dashw-web-hld.md`](../HLD/dashw-web-hld.md).
> For implementable-grade module/component specs, see
> [`specs/LLD/dashw-web-lld.md`](../LLD/dashw-web-lld.md).