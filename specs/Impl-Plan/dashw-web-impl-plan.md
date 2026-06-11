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
| **Phase A** — REST-only (functional console) | BFF proxy + aggregation + SPA with all 13 views using REST polling. In-process cache, circuit breaker, rate limiter, readiness probe. No WebSocket, no gRPC. | ❌ Not started | 0 / 24 |
| **Phase B** — gRPC streaming (real-time) | WebSocket ↔ gRPC bridge in BFF; real-time DPU status, events, flows, counters, audit in SPA. | ❌ Not started | 0 / 12 |
| **Phase C** — Diagnostics & advanced (full fidelity) | TraceFlow animation, ACL hit stats streaming, basic HA/Migration stream UIs, E2E tests. | ❌ Not started | 0 / 10 |
| **Phase D** — HA Theater + Migration Center + Capacity | Full HA orchestration theater, 10-phase migration control center, capacity planner with what-if simulator. 3 new views. | ❌ Not started | 0 / 16 |
| **Phase E** — Intelligence & Analytics | Counter correlation matrix, drift remediation workflow, packet anatomy lab, ACL impact analyzer, capability matrix, policy dependency graph, event causality timeline. 4 new views + 6 enhanced views. | ❌ Not started | 0 / 20 |

> **Dependency**: Phase A ships against dashd Phase 1B (REST is feature-complete).
> Phase B requires dashd gRPC streaming RPCs (dashd Phase 2 PA/PB).
> Phase C requires dashd Diagnostics RPCs (dashd Phase 2 PE).
> Phase D requires dashd HA/Migration RPCs (dashd Phase 2 PD) + Phase C.
> Phase E requires Phase D + all dashd Phase 2 RPCs. Can be parallelized with Phase D for independent features.

---

## Implementation order

```
dashd 1B ✅ ──────────────► dashw Phase A (REST-only, 13 views)
                                    │
dashd 2 PA (Observability) ────────►├──► dashw Phase B (WS streams)
dashd 2 PB (Operations)  ──────────┘        │
                                            │
dashd 2 PC–PE (Diag/HA/Migration) ─────────►├──► dashw Phase C (Diagnostics)
                                            │
dashd 2 PD (HA + Migration RPCs)  ─────────►├──► dashw Phase D (HA Theater + Migration + Capacity)
                                            │         3 new views: /ha, /migrations, /capacity
                                            │
                                            └──► dashw Phase E (Intelligence + Analytics)
                                                      4 new views: /counters, /drift, /capabilities, /dependencies
                                                      6 enhanced views: DPU, Fleet, Policy, Flow Trace, Audit, Dashboard
```

**Total views at Phase E completion: 20** (13 Phase A + 7 new in D/E).

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

#### A1b — BFF resilience & caching infrastructure

| Gate | Task | Status | AI Agent Instructions |
|---|---|---|---|
| **A1b-G1** | `internal/cache/cache.go`: In-process TTL cache with stale-while-revalidate. Types: `CacheEntry{Data, ExpiresAt, StaleAt}`, `Cache` struct with `sync.RWMutex` + `map[string]*CacheEntry`. Methods: `Get(key) → ([]byte, CacheStatus)`, `Set(key, data, ttl, staleWindow)`, `Invalidate(key)`, `InvalidatePattern(prefix)`, `Flush()`. `CacheStatus` enum: `HIT`, `MISS`, `STALE`. On STALE: return data + trigger background refresh goroutine. | ❌ | Use stdlib only (`sync.RWMutex`, `map`, `time`). Background refresh: `go func() { data := fetch(); cache.Set(key, data, ttl, stale) }()`. Periodic cleanup goroutine every 60s removes expired+stale entries. Response headers: `X-Cache: hit|miss|stale`, `X-Cache-Age: Ns`. See `dashw-web-scale-design-req-analysis.md §3` for full design. |
| **A1b-G2** | `internal/cache/invalidation.go`: Mutation-aware cache invalidation. Map of URL pattern → cache keys to flush. BFF proxy middleware intercepts successful PUT/POST/DELETE responses and calls `cache.InvalidatePattern()`. | ❌ | Invalidation map: PUT vnets → flush `fleet/summary,topology,vnet/detail:*,vnet/canvas:*,dependencies`. PUT enis → flush `fleet/summary,topology,dpu/detail:*,vnet/*,capacity,dependencies`. POST reconcile → `cache.Flush()` (full). See analysis doc §3.4 for complete map. |
| **A1b-G3** | `internal/resilience/circuit_breaker.go`: Circuit breaker for dashd calls. States: CLOSED→OPEN→HALF_OPEN. Threshold: 5 failures in 30s → OPEN for 30s. When OPEN: return `ErrCircuitOpen` (caller serves stale cache). HALF_OPEN: try one request, success → CLOSED. | ❌ | Use `sync/atomic` for state + failure counter. Thread-safe. One circuit breaker per dashd target (REST, Admin, gRPC). Wrap all aggregation fan-out calls with `cb.Call(func() error { ... })`. |
| **A1b-G4** | `internal/resilience/rate_limiter.go`: Write rate limiter. `golang.org/x/time/rate` token bucket per source IP. 10/s burst, 100/min sustained. Middleware: check on PUT/POST/DELETE methods only. Return `429 Too Many Requests` with `Retry-After` header. | ❌ | IP extraction: `X-Real-IP` header (from chi `RealIP` middleware), fallback to `RemoteAddr`. Per-IP limiter map with cleanup (remove idle entries after 5 min). Ignore reads (GET) — cache handles read load. |
| **A1b-G5** | `internal/health/readiness.go`: `GET /readyz` readiness probe. Checks: 1) dashd REST reachable (GET /admin/health, timeout 2s), 2) at least 1 cache entry populated. Returns 200 if both pass, 503 otherwise. | ❌ | Separate from `/healthz` (liveness). K8s uses `/readyz` for load balancer routing — new instances don't receive traffic until cache is warm. `/healthz` only checks "process alive" (always 200 unless deadlocked). |
| **A1b-G6** | Unit tests: cache (TTL expiry, stale-while-revalidate, invalidation, pattern flush), circuit breaker (state transitions, threshold, timeout), rate limiter (burst, sustained, per-IP isolation), readiness (warm/cold cache). | ❌ | Test cache: `Set(key, data, 1s, 5s)` → immediate `Get` = HIT → sleep 1.1s → `Get` = STALE → sleep 5.1s → `Get` = MISS. Test CB: trigger 5 errors → state = OPEN → wait 30s → HALF_OPEN → success → CLOSED. |

**Gate verification:** Cache reduces dashd calls by 50×+ under multi-user load. Circuit breaker opens on dashd failure, serves stale. Rate limiter rejects excessive writes. `/readyz` returns 503 until cache warm.

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
| **A6-G5** | Vnet View: **Dual-plane interactive canvas** (`VnetCanvas` with overlay/underlay planes, `DpuHexNode` hexagons in pentagon ring, `TunnelEdge` with animated particles crossing layer divider, `DpuTooltip` on hover, `TunnelDetailDrawer` on click, `VnetPropertyFlash` entry animation, `EniConnectorEdge` dashed lines, `LayerDividerEdge`). Below canvas: tabs (ENIs, Mappings, Routes, Tunnels). BFF endpoint `GET /api/console/vnet/{name}/canvas` for pre-computed canvas data. See LLD §21 for full component tree, types, layout algorithm, and animation specs. | ❌ |
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
| **A8-G1** | Docker Compose: `deploy/test-setup/05-full-console/docker-compose.yml` — full stack with 10 DPU sims + 3 HA dashd (etcd-elected) + dashw console. Single `docker compose up -d --build` → `http://localhost:3000`. Configs: `configs/dashd-{1,2,3}.yaml` (controller mode, etcd backend), `configs/inventory.yaml` (10 DPUs across 5 appliances with zone/tier labels). See `deploy/test-setup/05-full-console/README.md` for topology diagram and quick start. | ❌ |
| **A8-G2** | `README.md` for `src/impl-go/console/`, `src/impl-web/console/`, and `deploy/test-setup/05-full-console/manual-handson.md` — 12-step hands-on lab tutorial (deploy → explore → create resources → Vnet canvas → flow trace → HA failover → command view → debug → batch → reconcile → clean up) | ❌ |
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

## Phase D — HA Theater + Migration Center + Capacity Planner

### Objective

Deliver **3 new purpose-built views** that transform dashw from a
monitoring console into an **operational control center** for the three
most complex DashCenter workflows: HA orchestration, ENI live migration,
and capacity planning. Each view is a rich, interactive, real-time
visualization — not a table with buttons.

### Prerequisites

- dashd Phase 2 PD (HaService: GetHaSetState, TriggerSwitchover/Failover streams, WatchHaEvents, GetFlowSyncStats)
- dashd Phase 2 PD (MigrationService: full 12-RPC surface)
- dashd ControlPlane.SimulateApply (already available in Phase 1B REST)
- Phase C complete (WS bridge, streaming hooks, E2E framework)

### Design reference

- [`specs/HLD/dashw-web-vision.md` §3.3](../HLD/dashw-web-vision.md) — HA Orchestration Theater
- [`specs/HLD/dashw-web-vision.md` §3.4](../HLD/dashw-web-vision.md) — Migration Control Center
- [`specs/HLD/dashw-web-vision.md` §3.6](../HLD/dashw-web-vision.md) — Capacity Planner & What-If Simulator

### Sub-phases

#### D1 — HA Orchestration Theater (`/ha`)

| Gate | Task | Status | AI Agent Instructions |
|---|---|---|---|
| **D1-G1** | **BFF**: `aggregation/ha.go` — `GET /api/console/ha/sets` aggregation endpoint. Fan-out: `GET /v1/ha` (list HA sets) + `GET /admin/health` (per-DPU state). Return `HaSetOverview[]` with members, roles, flow_sync state. | ❌ | Proto source: `ha.proto:HaSetStatus`, `HaSetMember`, `FlowSyncState`. Go types mirror these. Use `errgroup` parallel fan-out + `singleflight`. |
| **D1-G2** | **BFF**: `aggregation/ha.go` — `GET /api/console/ha/{ns}/{name}/detail` aggregation endpoint. Fan-out: `GET /v1/ha/{ns}/{name}` + `GET /v1/ha/flow-sync-stats?ha_set_name={name}`. Return `HaSetDetail` with members, roles, sync stats, VIP. | ❌ | Proto source: `ha.proto:HaSetStatus` + `FlowSyncStats`. Merge into single response. |
| **D1-G3** | **SPA types**: `api/types.ts` — add `HaSetOverview`, `HaSetDetail`, `HaSetMember`, `HaScopeRole` (enum as string union), `FlowSyncState`, `FlowSyncStats`, `HaEvent` types. | ❌ | Mirror proto `ha.proto` enums as TypeScript string union types. `HaScopeRole` has 12 values. `FlowSyncState` has 5 values. `HaEvent.Type` has 9 values. |
| **D1-G4** | **SPA query hooks**: `queries/ha.ts` — `useHaSetList()`, `useHaSetDetail(ns, name)`. Polling: 10s list, 5s detail. | ❌ | Use `queryKeys.ha = { all: ['ha'], list: () => [...], detail: (ns,name) => [...] }`. |
| **D1-G5** | **SPA component**: `views/ha/HaTheater.tsx` — main view. Two DPU hexagons side-by-side with connecting sync line. Role labels (`ACTIVE ●` / `STANDBY ○`), flow count, sync state badge. | ❌ | Use `DpuHexNode` from shared components (reuse from Vnet View). Connecting line: SVG `<line>` with animated dash (`stroke-dashoffset`). Role badge: `StatusBadge` with custom HA colors (teal for sync, green for active). |
| **D1-G6** | **SPA component**: `views/ha/FlowSyncRing.tsx` — circular progress indicator. `FlowSyncStats.flows_synced / (flows_synced + flows_pending)`. Color: SYNCED=green, SYNCING=teal, FAILED=red. Inner text: percentage + pending count. | ❌ | SVG `<circle>` with `stroke-dasharray` and `stroke-dashoffset` animated via Framer Motion. Radius 60px. Background ring: `--border`. Foreground: state-colored. |
| **D1-G7** | **SPA component**: `views/ha/HaEventFeed.tsx` — live event stream from `useHaEvents()` WS hook. Each event: timestamp, type badge (color-coded), DPU ID, role change arrows. Virtual scroll. | ❌ | Reuse `VirtualizedTable`. Event type color map: `ROLE_CHANGED`=cyan, `SPLIT_BRAIN_DETECTED`=red, `SWITCHOVER_*`=amber, `FAILOVER_*`=red, `FLOW_SYNC_*`=teal. |
| **D1-G8** | **SPA component**: `views/ha/SwitchoverControls.tsx` — "Trigger Switchover" and "Trigger Failover" buttons with confirmation dialog. Switchover: target DPU selector + reason input. Failover: failed DPU selector + target DPU selector + reason. On submit: `POST /v1/ha/{ns}/{name}/switchover` or `/failover`. | ❌ | Use `Dialog` (shadcn). Validate: HA set must have ≥2 members. Show warning if flow_sync ≠ SYNCED. Disable Failover if no STANDBY member. On submit success: toast + invalidate ha queries. |
| **D1-G9** | **SPA**: Animated switchover sequence — when switchover is in progress (`HaEvent.TYPE_SWITCHOVER_STARTED` received), animate: role labels morph (ACTIVE→SWITCHING_TO_STANDBY→STANDBY), connecting line pulses faster, flow sync ring resets. Complete on `TYPE_SWITCHOVER_COMPLETED`. | ❌ | Use Framer Motion `AnimatePresence` for label transitions. Pulse animation: increase `stroke-dashoffset` animation speed 3x during transition. State machine: `idle → switching → complete`. |
| **D1-G10** | **SPA**: Split-brain alert — when `TYPE_SPLIT_BRAIN_DETECTED` event fires, connecting line turns red + zigzag SVG animation + toast alert. Dashboard integration: add HA alert to `AlertBanner`. | ❌ | SVG path with `<animate>` zigzag on the connecting line. Toast: `toast.error("Split-brain detected on ha-set-prod!")`. Dashboard: add to `AlertBanner` condition check. |
| **D1-G11** | **Tests**: Unit tests for HaTheater (render with mock data), FlowSyncRing (math), SwitchoverControls (form validation), HaEventFeed (event rendering). Integration test: switchover flow. | ❌ | MSW handlers: `GET /api/console/ha/sets`, `GET /api/console/ha/default/ha-set-prod/detail`, `POST /v1/ha/default/ha-set-prod/switchover`. Test data factory: `createHaSetDetail()`. |

**Gate verification:** Navigate to `/ha` → see HA sets, click one → see theater with DPU pair, sync ring, event feed. Trigger switchover → animated sequence. Split-brain → alert.

#### D2 — Migration Control Center (`/migrations`)

| Gate | Task | Status | AI Agent Instructions |
|---|---|---|---|
| **D2-G1** | **BFF**: `aggregation/migration.go` — `GET /api/console/migrations/active` endpoint. Calls `GET /v1/migrations/sessions` (active only). Returns enriched `MigrationSessionOverview[]` with plan details inlined. | ❌ | Proto source: `migration.proto:MigrationSession`, `MigrationPlan`. Strip `detail_json` from list response for performance. |
| **D2-G2** | **SPA types**: `api/types.ts` — add `MigrationSession`, `MigrationPlan`, `MigrationPhase` (enum: 12 values as string union), `MigrationStrategy` (4 values), `SimulatedCapacityImpact`, `DrainProgress`, `EniMigrationLink`. | ❌ | `MigrationPhase` values: UNSPECIFIED, ADMISSION, SNAPSHOT, PREPARE, SYNC, READY, CUTOVER, DRAIN, COMMIT, FINALIZE, COMPLETED, ROLLBACK, ABORTED. Phase ordinals are contractual (0-12). |
| **D2-G3** | **SPA query hooks**: `queries/migration.ts` — `useMigrationList()`, `useMigrationSession(id)`, `useCreateMigrationPlan()` mutation, `useStartMigrationSession()` mutation, `useAdvanceMigrationPhase()` mutation, `useRollbackMigration()` mutation, `useAbortMigration()` mutation. | ❌ | Mutations invalidate `['migration']` queries on success. `useAdvanceMigrationPhase` takes `{ sessionId, expectedGeneration, toPhase }`. |
| **D2-G4** | **SPA component**: `views/migration/MigrationPhaseRail.tsx` — 10-phase horizontal rail. Each phase: colored block (green=done, cyan=current animated, gray=future, red=failed). Phase label below. Current phase indicator (arrow/badge). | ❌ | Flexbox row of `<div>` blocks. Width proportional to phase timing (from `phase_started_at` map). Animation: current phase has `animate-pulse-slow` + `border-accent-cyan`. Use `motion.div` for transitions. |
| **D2-G5** | **SPA component**: `views/migration/MigrationTimingBar.tsx` — Gantt-style horizontal bar showing elapsed time per phase. Each phase segment: colored proportional to its duration. Total elapsed + ETA. | ❌ | Calculate duration per phase from `phase_started_at` timestamps. Render as stacked horizontal bar. Colors match phase rail. Current phase grows in real-time (timer). |
| **D2-G6** | **SPA component**: `views/migration/FlowSyncWaterfall.tsx` — progress bar for flow sync during SYNC phase. Parse `detail_json` for `{ flows_synced, flows_total, sync_rate }`. Show: bar + percentage + rate + ETA. | ❌ | `detail_json` is a JSON string — parse with `JSON.parse()`. Bar: `CapacityGauge`-style but horizontal. Rate: calculate from delta over time. ETA: `remaining / rate`. |
| **D2-G7** | **SPA component**: `views/migration/MigrationControls.tsx` — action buttons: "Advance" (next phase), "Rollback", "Abort", "Commit". Each gated by current phase. Confirmation dialogs with reason input. | ❌ | Phase gating rules: Advance only if `toPhase = currentPhase + 1`. Rollback available in non-terminal phases. Commit only in FINALIZE. Abort always available (with strong warning). All carry `expectedGeneration` for optimistic concurrency. |
| **D2-G8** | **SPA component**: `views/migration/CreateMigrationWizard.tsx` — 3-step wizard: 1) Select ENIs + source DPU, 2) Select strategy + target DPU (or auto), 3) Review plan (show `MigrationPlan.warnings[]`, `target_capacity_impact`). "Start Session" button. | ❌ | Step 1: multi-select ENIs from source DPU's ENI list. Step 2: strategy dropdown (4 options with description). Auto-target: leave blank. Step 3: call `POST /v1/migrations/plans` → show response (warnings, estimated_flow_count, capacity impact). "Start" calls `POST /v1/migrations/sessions`. |
| **D2-G9** | **SPA**: `views/migration/MigrationView.tsx` — main view. Left panel: active session list. Right panel: selected session detail (PhaseRail + TimingBar + FlowSyncWaterfall + Controls + DispatchResults). Real-time via `useMigration(sessionId)` WS hook. | ❌ | Layout: 2-column (30%/70%). Left: `DataTable` of sessions (session_id, eni, source→target, phase, strategy). Click → select. Right: detail view. WS hook provides real-time phase updates. |
| **D2-G10** | **Tests**: Unit tests for PhaseRail (all phase states), TimingBar (timing math), FlowSyncWaterfall (parse detail_json), Controls (phase gating). Integration: create plan → start → advance. | ❌ | MSW handlers for all migration endpoints. Test data: `createMigrationSession({ phase: 'SYNC', detail_json: '{"flows_synced":500,"flows_total":1000}' })`. |

**Gate verification:** Navigate to `/migrations` → see active sessions. Create migration plan → review warnings → start → watch phase rail advance in real-time. Rollback works. Controls gate correctly by phase.

#### D3 — Capacity Planner & What-If Simulator (`/capacity`)

| Gate | Task | Status | AI Agent Instructions |
|---|---|---|---|
| **D3-G1** | **BFF**: `aggregation/capacity_planner.go` — `GET /api/console/capacity/fleet` endpoint. Aggregate: per-DPU `DpuCapacityLimits` + `DpuCapacityUsage` + `DpuCapabilities`. Return `FleetCapacity` with per-DPU used/max/percent for all 18 capacity dimensions. | ❌ | Proto: `DpuCapacityLimits` (18 fields), `DpuCapacityUsage` (14 fields). Compute `pct = used/max * 100` for each. Include `DpuCapabilities` (12 flags) for compatibility check. |
| **D3-G2** | **SPA types**: `api/types.ts` — add `FleetCapacity`, `DpuCapacityRow` (id, state, all used/max/pct fields, capabilities), `SimulateApplyResult`, `SimulatedDpuImpact`. | ❌ | `SimulateApplyResult` has: `would_succeed: boolean`, `validation_errors: string[]`, `per_dpu_impact: SimulatedDpuImpact[]`, `would_issue_rpcs: string[]`. |
| **D3-G3** | **SPA component**: `views/capacity/CapacityFleetTable.tsx` — sortable table: DPU, ENIs (bar), Routes (bar), ACL Rules (bar), Flows (bar), pps, bps. Each cell: inline capacity bar (cyan/amber/red by threshold). Sort by any metric. | ❌ | Reuse `DataTable`. Custom cell renderer: inline `<div>` bar with `width: ${pct}%`. Color: >90% red, >70% amber, else cyan. Hover: tooltip with `used/max`. |
| **D3-G4** | **SPA component**: `views/capacity/WhatIfSimulator.tsx` — scenario builder form: "Add [N] ENIs to Vnet [dropdown]", "Move ENI [name] to DPU [dropdown]", "Delete Vnet [dropdown]". Translates to `SimulateOp[]`, calls `POST /v1/simulate`. Shows `SimulateApplyResult`. | ❌ | Scenario types: ADD_ENIS, MOVE_ENI, DELETE_RESOURCE. Each type has its own form fields. On "Simulate": construct `{ ops: [...] }` body matching `service.SimulateOp` shape. Map result `per_dpu_impact` to capacity table delta columns. |
| **D3-G5** | **SPA component**: `views/capacity/CapacityDeltaOverlay.tsx` — after simulate, overlay projected changes on fleet table: current→projected columns, delta badges (+2, −1), status change (OK→⚠️→❌). | ❌ | Add columns to `CapacityFleetTable`: "Projected ENIs", "Δ", "Projected Status". Highlight rows with `exceeds_capacity = true` in red. Show `capacity_failure_reason` in tooltip. |
| **D3-G6** | **SPA component**: `views/capacity/CapacityTopologyOverlay.tsx` — optional topology view (reuse `TopologyGraph`) with DPU nodes colored by projected capacity state. OK=green, warning=amber, exceed=red. | ❌ | Reuse `TopologyGraph` + `DpuNode`. Override node color based on `SimulatedDpuImpact.exceeds_capacity`. Overlay: show delta badges on DPU nodes. |
| **D3-G7** | **SPA**: `views/capacity/CapacityView.tsx` — main view. Top: fleet capacity table. Middle: what-if simulator. Bottom: result (delta overlay on table + optional topology). "Apply" button: execute the scenario for real. | ❌ | Layout: vertical stack. Fleet table always visible. Simulator panel: collapsible. Result: appears after simulation. "Apply" button: calls the actual PUT/DELETE APIs for each op in the scenario. Confirmation dialog with diff preview. |
| **D3-G8** | **Tests**: CapacityFleetTable (bar rendering, sorting), WhatIfSimulator (form→SimulateOp translation), DeltaOverlay (delta math, exceed detection). | ❌ | MSW: `POST /v1/simulate` returns mock `SimulateApplyResult`. Test: simulate adding 10 ENIs → verify delta overlay shows correct per-DPU impact. |

**Gate verification:** Navigate to `/capacity` → see fleet capacity table. Enter "add 5 ENIs to vnet-prod" → simulate → see per-DPU capacity impact with delta overlay. "Apply" executes.

### Phase D exit criteria

- [ ] All 16 gates (D1-G1 through D3-G8) verified
- [ ] `/ha` view: HA theater renders, switchover/failover trigger works, event feed streams, split-brain alerts
- [ ] `/migrations` view: session list, phase rail, timing bar, flow-sync waterfall, controls, create wizard
- [ ] `/capacity` view: fleet table, what-if simulator, simulate → delta overlay, apply
- [ ] All Phase A + B + C tests still pass
- [ ] 3 new lazy-loaded route chunks (< 200KB gzip each)
- [ ] Lighthouse scores maintained
- [ ] New view tests pass (unit + integration)

---

## Phase E — Intelligence & Analytics

### Objective

Transform dashw into a **network operations intelligence platform** by
adding **4 new views** and **enhancing 6 existing views** with analytics,
correlation, and remediation capabilities. Every feature exploits data
dashd already exposes but was previously under-visualized.

### Prerequisites

- All dashd Phase 2 RPCs (DiagnosticsService, ObservabilityService, HaService)
- Phase D complete (Phase E builds on D's infrastructure)
- `@visx/visx`, `dagre`, `simple-statistics` npm dependencies added

### Design reference

- [`specs/HLD/dashw-web-vision.md`](../HLD/dashw-web-vision.md) — sections §3.1, §3.2, §3.5, §3.7, §3.8, §3.9, §3.10, §3.11, §3.13, §3.14, §3.15

### Sub-phases

#### E1 — Counter Correlation Matrix (`/counters`)

| Gate | Task | Status | AI Agent Instructions |
|---|---|---|---|
| **E1-G1** | **SPA types**: `api/types.ts` — add `CounterCategory` (TCP, Drops, Flow, HA, Encap, ServiceTunnel), `CounterTimeSeries` (name, samples[], dpuId). | ❌ | Group `CounterReport` fields into 6 categories. TCP: fields 10-15. Drops: fields 20-29 (10 drop reasons). Flow: fields 40-45. HA: fields 50-55. Encap: fields 60-61. ServiceTunnel: fields 70-71. |
| **E1-G2** | **Zustand store**: `stores/counter-store.ts` — per-DPU counter time-series ring buffer (120 samples, 1s interval). Methods: `addSample(dpuId, CounterReport)`, `getSeries(dpuId, counterName)`. Cross-DPU aggregation: `getFleetSeries(counterName)`. | ❌ | Ring buffer: fixed-size array, write pointer, wrap on overflow. Each sample: `{ timestamp, value }`. Derive `delta/s` from consecutive samples for rate counters (tcp_syn_rx, drops, etc). |
| **E1-G3** | **SPA component**: `views/counters/CounterCategoryPanel.tsx` — one panel per counter category. Per-counter row: name, sparkline (120 samples), current value, per-DPU breakdown badges. Anomaly highlight: if delta exceeds 2σ from rolling mean, row glows amber/red. | ❌ | Sparkline: `SparklineChart` component (reuse). Anomaly detection: compute rolling mean + stddev over last 60 samples. If latest delta > mean + 2*stddev, mark as anomaly. Use `simple-statistics` for mean/stddev. |
| **E1-G4** | **SPA component**: `views/counters/CorrelationAlert.tsx` — auto-generated alerts when two counters on the same DPU spike simultaneously. Compute Pearson correlation coefficient between counter pairs over 60-sample window. If |r| > 0.8, show alert with linked cause hypothesis. | ❌ | Use `simple-statistics.sampleCorrelation(x[], y[])`. Counter pairs to check: (tcp_retransmits, drop_acl_in), (tcp_retransmits, drop_route_miss), (flow_created, slow_path_packets), (ha_sync_failed, ha_split_brain_detected). Link to `PolicyEvent` by timestamp proximity. |
| **E1-G5** | **SPA**: `views/counters/CounterMatrixView.tsx` — main view. Collapsible category panels + correlation alerts at top. DPU selector (filter to one DPU or "fleet"). Auto-refresh from `useCounters()` WS hook. | ❌ | Layout: correlation alerts banner → category panels (accordion). Each panel: expand/collapse. DPU filter: dropdown at top. Fleet mode: show aggregated counters. |
| **E1-G6** | **Tests**: counter-store (ring buffer, delta calc, anomaly detection), CorrelationAlert (Pearson math), CounterCategoryPanel (rendering). | ❌ | Test: add 120 samples with known pattern → verify anomaly detection triggers at correct sample. Test: two perfectly correlated series → r=1.0, alert shown. |

**Gate verification:** Navigate to `/counters` → see live counter sparklines grouped by category. Anomaly highlights appear on spikes. Correlation alerts link to policy changes.

#### E2 — Drift Remediation Workflow (`/drift`)

| Gate | Task | Status | AI Agent Instructions |
|---|---|---|---|
| **E2-G1** | **BFF**: `aggregation/drift_detail.go` — `GET /api/console/drift/{dpuId}/explain` endpoint. For each drift item from `GET /admin/drift?dpu={dpuId}`, call dashd gRPC `DiagnosticsService.ExplainDrift` to get `DriftExplanation` (field_diffs, suggested remediation, rationale). Return `DriftExplanationList`. | ❌ | Proto: `diagnostics.proto:DriftExplanation`, `FieldDiff`, `Remediation` enum (4 values: UNSPECIFIED, RECONCILE, IMPORT_OBSERVED, MANUAL). Fan-out: one ExplainDrift call per drift item (bounded parallelism, max 10). |
| **E2-G2** | **SPA types**: `api/types.ts` — add `DriftExplanation`, `FieldDiff` (field, declared, observed), `DriftRemediation` enum. | ❌ | `Remediation` values: RECONCILE, IMPORT_OBSERVED, MANUAL. Map to UI labels: "Push Declared → DPU", "Adopt Observed → Declared", "Manual Intervention Required". |
| **E2-G3** | **SPA component**: `views/drift/DriftItemCard.tsx` — expandable card per drift item. Header: target ref + kind badge + DPU ID. Expanded: `FieldDiff` table (field, declared value, observed value with color diff), suggested remediation badge, rationale text, action buttons. | ❌ | Field diff table: green for declared, red for observed, strikethrough on mismatched values. Remediation badge: RECONCILE=cyan, IMPORT_OBSERVED=amber, MANUAL=red. Action buttons: "Reconcile This", "Import Observed", "Skip". |
| **E2-G4** | **SPA component**: `views/drift/DriftView.tsx` — main view. DPU selector (dropdown or tabs). Drift item count badge. List of `DriftItemCard` components. "Reconcile All" bulk action button. | ❌ | DPU selector: tabs for each DPU with drift count badge. Items: sorted by remediation urgency (MANUAL first, then RECONCILE, then IMPORT). Bulk reconcile: `POST /v1/reconcile` with `dpu_ids: [selected]`. |
| **E2-G5** | **Tests**: DriftItemCard (field diff rendering, action buttons), DriftView (bulk reconcile flow). | ❌ | MSW: `GET /api/console/drift/dpu-3/explain` returns 3 items with mixed remediation types. Test: click "Reconcile This" → mutation fires → item removed from list. |

**Gate verification:** Navigate to `/drift` → see per-DPU drift items with field-by-field diffs, suggested remediation, one-click fix. "Reconcile All" works.

#### E3 — Enhanced views (DPU, Fleet, Policy, Flow Trace, Audit, Dashboard)

| Gate | Task | Status | AI Agent Instructions |
|---|---|---|---|
| **E3-G1** | **DPU View enhancement**: Add `DpuLifecycleFsm.tsx` widget — SVG state machine diagram showing all 9 DPU states with the current state highlighted + glowing. State history timeline below. | ❌ | 9 states from `DpuState` enum in `types.proto`. Layout: predefined SVG positions for each state. Transitions: arrows between valid state pairs. Current state: fill=accent color + scale animation. History: horizontal timeline with state change events from `PolicyEvent.TYPE_DPU_STATE_CHANGED`. |
| **E3-G2** | **Fleet View enhancement**: Add physical-logical topology toggle. "Physical" view: group DPU nodes by `appliance_id`, show slot numbers. "Logical" view: existing topology. Toggle button in toolbar. | ❌ | Physical layout: use `DpuIdentity.appliance_id` + `slot` to group. Appliance = container node. Slots = positioned DPU hexagons inside. No existing endpoint exposes appliance_id — use `GET /v1/inventory` which returns `DpuRecord.identity.appliance_id`. |
| **E3-G3** | **Policy View enhancement**: ACL Impact Analyzer tab. For each ACL policy: expand rules → show hit count column (from `useAclHits` WS), dead rule badge (hits=0), coverage map (prefix ranges visualized). | ❌ | Proto: `AclRuleHit` from `diagnostics.proto`. Hit count column: value from WS stream. Dead rule: `hits === 0 && last_hit_at === null`. Coverage map: for each rule's `src_prefixes[]`, render horizontal bar proportional to prefix size (/8 = full, /32 = tiny). |
| **E3-G4** | **Flow Trace View enhancement**: Packet Anatomy Lab upgrade. After `TraceFlow`, call `ExplainMatch` for the ACL stage to get candidate waterfall. Show every candidate rule evaluated with match/reject reason. | ❌ | Proto: `diagnostics.proto:MatchExplanation`, `MatchCandidate`. New BFF endpoint or direct gRPC: `POST /api/console/explain-match` wrapping `DiagnosticsService.ExplainMatch`. Render as sorted table: priority → matched (✅/❌/⏭) → reason. Highlight winner row. |
| **E3-G5** | **Audit View enhancement**: Event Causality Timeline. When clicking an audit entry, show the causal chain: mutation → policy event → dispatch results → counter changes. Link by `txn_id`. | ❌ | `AuditEntry.txn_id` links to `PolicyEvent.txn_id` links to `Ack.dispatch_results`. Show as vertical timeline. Counter correlation: find `CounterReport` samples within 2s of the event timestamp and highlight deltas. |
| **E3-G6** | **Dashboard enhancement**: Add correlation alert banner (from Counter Matrix E1-G4). Auto-generated alerts: "DPU-3 tcp_retransmits correlating with drop_acl_in since 14:28". Link to `/counters`. | ❌ | Reuse `CorrelationAlert` component from E1-G4. Dashboard subscribes to counter-store anomaly events. Show top 3 alerts in banner. Click → navigate to `/counters?dpu=dpu-3`. |

**Gate verification:** DPU View shows lifecycle FSM. Fleet View toggles physical/logical. Policy View shows ACL hit counts and dead rules. Flow Trace shows candidate waterfall. Audit shows causal timeline. Dashboard shows correlation alerts.

#### E4 — Capability Matrix + Policy Dependency Graph

| Gate | Task | Status | AI Agent Instructions |
|---|---|---|---|
| **E4-G1** | **SPA component**: `views/capabilities/CapabilityMatrixView.tsx` — grid: rows=DPUs, columns=12 capabilities. Cell: ✅/❌. Footer: software version comparison. Alerts for outdated DPUs. Route: `/capabilities`. | ❌ | Data: `DpuRecord.capabilities` from `GET /v1/inventory`. 12 capability fields: ipv6, service_tunnel, ecmp, fast_path, fast_path_icmp_redirection, trusted_vni, ha_active_active, ha_active_standby, flow_sync, gnmi_telemetry, flow_resimulation, eni_live_migration. Version: `DpuCapabilities.dash_api_schema_version`. |
| **E4-G2** | **BFF**: `aggregation/dependency.go` — `GET /api/console/dependencies` endpoint. Build dependency graph from all specs: ENI→Vnet (via vnet_name), AclPolicy→ENI (via eni_names[]), RoutePolicy→ENI (via eni_names[]), ServiceTunnel→Vnet (via VNI match), VnetMapping→Vnet (via vnet_name). Return `DependencyGraph { nodes[], edges[] }`. | ❌ | Fan-out: list all vnets, enis, acl-policies, route-policies, service-tunnels. Build adjacency. Node types: vnet, eni, acl_policy, route_policy, service_tunnel, ha_set. Edge types: "belongs_to", "targets", "routes_for", "tunnels_to". |
| **E4-G3** | **SPA component**: `views/dependencies/DependencyGraphView.tsx` — directed graph using React Flow + dagre layout. Node types: colored by kind (vnet=cyan, eni=green, acl=red, route=amber, tunnel=purple). Click node → side panel showing details + "What breaks if I delete this?" (call SimulateApply with ACTION_DELETE). Route: `/dependencies`. | ❌ | Use `dagre` for layout (`rankdir: 'TB'`). Custom node types per kind. Edge labels: relationship type. Delete impact: on node click → "Impact Analysis" button → call `POST /v1/simulate` with `{ ops: [{ action: 'DELETE', object: { ... } }] }` → show `validation_errors[]` in side panel. |
| **E4-G4** | **Tests**: CapabilityMatrix (render, version comparison), DependencyGraph (graph building, layout, delete impact). | ❌ | Test: 5 DPUs with varied capabilities → verify matrix renders correctly. Test: graph with Vnet→ENI→AclPolicy chain → delete Vnet → show "2 ENIs and 1 ACL policy would be orphaned". |

**Gate verification:** `/capabilities` shows capability matrix with version alerts. `/dependencies` shows policy dependency graph with delete-impact analysis.

#### E5 — Polish & integration

| Gate | Task | Status | AI Agent Instructions |
|---|---|---|---|
| **E5-G1** | **Sidebar update**: Add nav items for 7 new views. New groups: "Operations" (HA Theater, Migrations, Capacity), "Analytics" (Counter Matrix, Drift, Capabilities, Dependencies). | ❌ | Update `NAV_GROUPS` in `Sidebar.tsx`. Icons: HA=`ShieldCheck`, Migrations=`ArrowRightLeft`, Capacity=`BarChart3`, Counters=`Activity`, Drift=`GitCompare`, Capabilities=`Puzzle`, Dependencies=`Network`. |
| **E5-G2** | **Router update**: Add lazy routes for 7 new views. Update route table in `router.tsx`. | ❌ | Add: `/ha`, `/migrations`, `/capacity`, `/counters`, `/drift`, `/capabilities`, `/dependencies`. All lazy-loaded. |
| **E5-G3** | **npm dependencies**: Add `@visx/visx`, `dagre`, `simple-statistics`, `elkjs`. Update `vite.config.ts` manual chunks: add `analytics-vendor` chunk. | ❌ | `analytics-vendor` chunk: `['@visx/visx', 'dagre', 'simple-statistics']`. Verify total initial bundle stays < 500KB gzip. |
| **E5-G4** | **E2E tests**: Add Playwright tests for all 7 new views + 6 enhanced views. | ❌ | Test scenarios: HA switchover flow, migration create→advance, capacity simulate, counter anomaly detection, drift remediation, capability check, dependency impact. |
| **E5-G5** | **Performance validation**: Lighthouse CI with all 20 views. Bundle analysis: verify each lazy chunk < 200KB gzip. | ❌ | Run `npx vite-bundle-visualizer` and verify. If analytics-vendor chunk too large, split further. |
| **E5-G6** | **Documentation update**: Operator guide updated with all 20 views, HA Theater workflow, Migration workflow, Capacity planning workflow. | ❌ | Update existing Phase C operator guide. Add sections for each new view with screenshots/descriptions. |

**Gate verification:** All 20 views accessible from sidebar. E2E tests pass. Lighthouse maintained. Docs complete.

### Phase E exit criteria

- [ ] All 20 gates (E1-G1 through E5-G6) verified
- [ ] `/counters` view: live counter matrix with anomaly detection and correlation alerts
- [ ] `/drift` view: per-item remediation with field-by-field diff and one-click fix
- [ ] `/capabilities` view: DPU capability matrix with version alerts
- [ ] `/dependencies` view: policy dependency graph with delete-impact analysis
- [ ] DPU View: lifecycle FSM widget
- [ ] Fleet View: physical/logical topology toggle
- [ ] Policy View: ACL hit counts + dead rule detection
- [ ] Flow Trace: candidate waterfall from ExplainMatch
- [ ] Audit: event causality timeline linked by txn_id
- [ ] Dashboard: correlation alert banner
- [ ] All 20 views render, navigate, and function
- [ ] E2E test suite covers all 20 views
- [ ] Bundle: initial < 500KB, lazy chunks < 200KB each
- [ ] Lighthouse: LCP < 2s maintained

---

## Cross-phase dependency matrix

| dashw phase | Requires dashd | Requires dashctl |
|---|---|---|
| Phase A | dashd Phase 1B (REST ✅) | None (dashctl command metadata embedded in SPA) |
| Phase B | dashd Phase 2 PA (ObservabilityService streaming) + PB (OperationsService.DrainDpu) | None |
| Phase C | dashd Phase 2 PC (cordon/uncordon) + PD (HA/Migration) + PE (DiagnosticsService) | None |
| Phase D | dashd Phase 2 PD (HaService full, MigrationService full) + Phase 1B (SimulateApply) | None |
| Phase E | dashd Phase 2 PE (DiagnosticsService.ExplainDrift, ExplainMatch, GetAclHitStats) + all prior | None |

### Parallelization opportunities

| Feature | Can start before Phase D/E completion? | Dependency |
|---|---|---|
| E1 Counter Matrix | Yes — after Phase B (uses existing `useCounters` WS) | Phase B complete |
| E2 Drift Remediation | Yes — after Phase C (uses DiagnosticsService) | Phase C + dashd PE |
| E4-G1 Capability Matrix | Yes — after Phase A (uses REST `GET /v1/inventory`) | Phase A complete |
| D3 Capacity Planner | Yes — after Phase A (uses REST `POST /v1/simulate`) | Phase A complete |
| E4-G3 Dependency Graph | Yes — after Phase A (uses REST list endpoints) | Phase A complete |

---

## Risk register

| Risk | Impact | Mitigation |
|---|---|---|
| **dashd gRPC streaming not ready** | Phase B blocked | Phase A is independently valuable; ship and iterate. Phase B WS hooks degrade gracefully to polling. |
| **Large topology graph performance** | Slow render (>100 DPUs) | React Flow virtual rendering; BFF pre-computes layout; limit visible nodes with zoom levels. |
| **WebSocket memory leaks** | Browser tab memory grows | Strict cleanup on navigation (useEffect cleanup); ring buffers in stores (max 1000 events, 60/120 counter samples). |
| **SPA bundle too large** | Slow initial load | Manual chunks, lazy loading, tree shaking. Monitor with Vite bundle analyzer in CI. Phase E adds `analytics-vendor` chunk (~40KB gzip). |
| **dashd API changes** | Type mismatches | TypeScript types mirror proto; update types when proto changes. BFF uses proto-generated stubs. |
| **Accessibility regressions** | WCAG violations | axe-core in CI; component-level a11y tests. |
| **Counter correlation false positives** | Noisy alerts | Require |r| > 0.8 AND both counters exceed 2σ anomaly threshold simultaneously. Tunable in `ui-prefs-store`. |
| **ExplainDrift/ExplainMatch latency** | Slow drift/flow-trace views | BFF bounded parallelism (max 10 concurrent ExplainDrift calls). Loading skeletons. Cached for 30s via `singleflight`. |
| **Phase D/E scope creep** | Delayed delivery | Each gate is independently testable. Ship D1 (HA) before D2 (Migration) if needed. E1-E4 sub-phases are independent. |
| **HA switchover animation complexity** | Hard to implement + test | Use Framer Motion state machine (3 states: idle/switching/complete). E2E test drives switchover and verifies state transitions. |

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

### Phase D

| Layer | New/modified files |
|---|---|
| BFF | `internal/aggregation/{ha,migration,capacity_planner}.go` (new); `internal/server/router.go` (add 3 new aggregation routes) |
| SPA | `views/ha/{HaTheater,FlowSyncRing,HaEventFeed,SwitchoverControls}.tsx` (new); `views/migration/{MigrationView,MigrationPhaseRail,MigrationTimingBar,FlowSyncWaterfall,MigrationControls,CreateMigrationWizard}.tsx` (new); `views/capacity/{CapacityView,CapacityFleetTable,WhatIfSimulator,CapacityDeltaOverlay,CapacityTopologyOverlay}.tsx` (new); `queries/{ha,migration}.ts` (new); `api/types.ts` (extend); `router.tsx` (add 3 routes); `Sidebar.tsx` (add nav items) |
| Tests | `tests/views/{ha,migration,capacity}/*.test.tsx` (new); MSW handlers for HA/migration/simulate endpoints |

### Phase E

| Layer | New/modified files |
|---|---|
| BFF | `internal/aggregation/{drift_detail,dependency}.go` (new); `internal/server/router.go` (add 2 new routes) |
| SPA | `stores/counter-store.ts` (new); `views/counters/{CounterMatrixView,CounterCategoryPanel,CorrelationAlert}.tsx` (new); `views/drift/{DriftView,DriftItemCard}.tsx` (new); `views/capabilities/CapabilityMatrixView.tsx` (new); `views/dependencies/DependencyGraphView.tsx` (new); `views/dpu/DpuLifecycleFsm.tsx` (new widget); `views/fleet/PhysicalTopology.tsx` (new toggle); `views/policy/AclImpactAnalyzer.tsx` (new tab); `views/flow-trace/CandidateWaterfall.tsx` (new panel); `views/audit/CausalityTimeline.tsx` (new panel); `components/layout/Sidebar.tsx` (update nav); `router.tsx` (add 4 routes); `api/types.ts` (extend) |
| Deps | `package.json`: add `@visx/visx`, `dagre`, `simple-statistics`; `vite.config.ts`: add `analytics-vendor` chunk |
| Tests | `tests/stores/counter-store.test.ts` (new); `tests/views/{counters,drift,capabilities,dependencies}/*.test.tsx` (new); E2E: extend suite for 7 new views |

---

## Definition of done (all phases)

- [ ] BFF builds cleanly (`CGO_ENABLED=0 go build`)
- [ ] SPA builds cleanly (`npm run build`)
- [ ] Docker image builds and starts
- [ ] All **20 views** render with data (13 Phase A + 7 Phase D/E)
- [ ] Admin CRUD operations work end-to-end
- [ ] Real-time streams (Phase B+) function with auto-reconnect
- [ ] Flow trace animation plays with candidate waterfall (Phase C + E)
- [ ] HA Theater: switchover/failover works with animated sequence (Phase D)
- [ ] Migration Center: create/advance/rollback/abort with real-time phase rail (Phase D)
- [ ] Capacity Planner: what-if simulation with delta overlay (Phase D)
- [ ] Counter Correlation Matrix: anomaly detection + correlation alerts (Phase E)
- [ ] Drift Remediation: field-by-field diff with one-click fix (Phase E)
- [ ] Capability Matrix: fleet-wide capability grid (Phase E)
- [ ] Dependency Graph: delete-impact analysis (Phase E)
- [ ] All unit/integration tests pass
- [ ] E2E test suite passes (all 20 views)
- [ ] Lighthouse: LCP < 2s, TTI < 3s, bundle < 500KB gzip initial
- [ ] Lazy chunks: < 200KB gzip each
- [ ] Accessibility: no axe-core critical violations
- [ ] Operator documentation covers all 20 views

---

> **End of implementation plan.** For the architecture and design decisions
> behind these phases, see [`specs/HLD/dashw-web-hld.md`](../HLD/dashw-web-hld.md).
> For the next-generation vision behind Phases D and E, see
> [`specs/HLD/dashw-web-vision.md`](../HLD/dashw-web-vision.md).
> For implementable-grade module/component specs, see
> [`specs/LLD/dashw-web-lld.md`](../LLD/dashw-web-lld.md).
