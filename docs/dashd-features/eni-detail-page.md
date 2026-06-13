# ENI Detail Page (dashw web console)

> **Status:** Phase A · shipped
> **Owner:** dashw / dashd-aggregation
> **See also:** [`docs/concepts/dashd-configuration-concepts.md`](../concepts/dashd-configuration-concepts.md)
> **Source:**
> - Backend handler: `src/impl-go/console/internal/aggregation/aggregator.go` → `EniDetail()`
> - Frontend view: `src/impl-web/console/src/views/eni/EniView.tsx`
> - Frontend list view: `src/impl-web/console/src/views/eni/EniListView.tsx`

---

## What it is

A dedicated, "everything-about-this-ENI" page in the dashw web console.

The concepts doc explains how an ENI sits at the centre of the dashd object
graph — it inherits its **VNI** from the parent Vnet, lives on one or more
DPUs, is referenced by ACL policies (per stage), is referenced by Route
policies, can reach Service tunnels through both routes and vnet-mappings,
and may be hosted by a DPU that participates in an HA set. When an operator
is debugging an ENI, all of that context normally lives in different views.

The **ENI Detail Page** brings it onto one URL.

```
/eni/:namespace/:name      ← the comprehensive detail page
/enis                      ← top-level list page (sidebar entry)
```

---

## What you get on the page

Backed by a single HTTP fetch to the new BFF aggregator endpoint:

```http
GET /api/console/eni/{namespace}/{name}/detail
```

| Section | Source field | Notes |
|---|---|---|
| **Identity card** | `identity` | name, namespace, vnet name, MAC, underlay IP, admin state, generation, labels |
| **VNI badge** | `vnet.vni` | Surfaced prominently in the header AND in the identity card — the inheritance from the parent Vnet that the concepts doc makes explicit |
| **Placement / HA-active-active** | `placement.dpu_ids`, `placement.ha_active_active` | All placement DPUs as clickable chips; `HA · active-active` badge when present on >1 DPU |
| **Vnet Mappings tab** | `vnet_mappings_reachable` | Mappings in this ENI's vnet, with overlay → underlay flow |
| **ACL Inbound tab** | `acls_inbound` | Policies whose `eni_names` contains this ENI AND `stage = "inbound"` |
| **ACL Outbound tab** | `acls_outbound` | Same but `stage = "outbound"` |
| **Routes tab** | `route_policies` | Policies whose `eni_names` contains this ENI; route entries expanded with next-hop type/target and ECMP members |
| **Tunnels tab** | `service_tunnels` | Tunnels referenced from the ENI's routes or vnet-mappings (others filtered out) |
| **HA tab** | `ha_set` | When any placement DPU is a member of an HaSet — full set with peer DPUs, virtual IP, roles |
| **Trace flow button** | header | One click into `/flow-trace?eni_name=...&vni=...` pre-filled |

---

## Architecture

### Server-side aggregation (Option B)

The handler at `aggregator.EniDetail()` fans out to dashd in parallel:

| # | Goroutine | Endpoint | Purpose |
|---|---|---|---|
| 1 | identity | `GET /v1/{ns}/enis/{name}` | **FATAL** — 404 here → 404 to browser |
| 2 | placement | `GET /admin/eni-placement` | HA-aware (`placements[]` shape) |
| 3 | mappings | `GET /v1/{ns}/vnet-mappings` | filtered server-side to ENI's vnet |
| 4 | acls | `GET /v1/{ns}/acl-policies` | filtered by `eni_names`, split by `stage` |
| 5 | routes | `GET /v1/{ns}/route-policies` | filtered by `eni_names` |
| 6 | tunnels | `GET /v1/{ns}/service-tunnels` | filtered to refs from routes/mappings |
| 7 | ha-sets | `GET /v1/{ns}/ha` | attached when any placement DPU is a member |
| 8 | vnet | `GET /v1/{ns}/vnets/{vnet_name}` | **deferred** — runs only after #1 returns the vnet name |

All non-ENI fetch failures **degrade gracefully**: the field is omitted and a
human-readable string is appended to `warnings[]`. The UI shows a yellow
"Partial data" banner so the operator knows what's missing.

### Why server-side and not a client-side join?

Both options were on the table. We picked the server-side aggregator (Option B)
because:

- The 8 fetches happen in parallel and the BFF can singleflight repeats.
- The wire payload is shaped exactly for this page — no client-side projection.
- Reverse-index logic (which ACL/Route references this ENI? which tunnel is
  reachable?) lives in **one** place (`aggregator.go` helpers), not duplicated
  in the React view.
- Mirrors the existing `DpuDetail` / `VnetDetail` / `ServiceTopology` patterns.

The TS view consumes the joined shape directly through `useEniDetail()`, which
is just a `useQuery` against `consoleApi.eniDetail()`.

---

## Deep linking

The page is reachable from multiple places:

| From | How |
|---|---|
| Sidebar | New **ENIs** entry in the **Observe** group |
| `/enis` list page | Click any row |
| `/dpu/:dpuId` | Click the ENI name in the ENIs table |
| `/vnet/:vnetName` | Click the ENI name in the ENIs table |
| `/fleet` (ENIs tab) | Click the ENI name in the table |
| Direct URL | `/eni/{namespace}/{name}` — fully shareable |

The reverse — going **from** the page — is wired everywhere:
- DPU chips → `/dpu/:id`
- Vnet name → `/vnet/:name`
- Next-hop tunnels in route tables → `/tunnels`
- HA peer DPUs → `/dpu/:id`

---

## VNI inheritance — UI reinforcement

The page **deliberately surfaces the parent Vnet's VNI in four places** so
operators see at a glance where the ENI's overlay identity comes from:

1. The page header chip: `VNI 100`
2. The Identity card row: `VNI (inherited from vnet)`
3. The Parent Vnet card on the Overview tab: large `VNI 100` value
4. The italic note on the Parent Vnet card: *"ENIs inherit their VNI from
   the parent Vnet. There is no `vni` field on the ENI spec itself."*

This is the UI corollary to the explicit callouts added in §2.3, §3, and §6
of the [concepts doc](../concepts/dashd-configuration-concepts.md).

---

## Files touched in this feature

**Backend (Go)**
- `src/impl-go/console/internal/aggregation/types.go` — `EniDetail` + supporting structs
- `src/impl-go/console/internal/aggregation/aggregator.go` — `EniDetail()` handler + helpers
- `src/impl-go/console/internal/server/router.go` — registers the route
- `src/impl-go/console/internal/aggregation/aggregator_test.go` — 3 new tests (happy path, ENI 404, vnet-down degrade)

**Frontend (TypeScript)**
- `src/impl-web/console/src/api/types.ts` — `EniDetail` interface
- `src/impl-web/console/src/api/console-api.ts` — `consoleApi.eniDetail()`
- `src/impl-web/console/src/queries/keys.ts` — `queryKeys.eni.detail()`
- `src/impl-web/console/src/queries/hooks.ts` — `useEniDetail()`
- `src/impl-web/console/src/views/eni/EniView.tsx` — the page itself
- `src/impl-web/console/src/views/eni/EniListView.tsx` — sidebar entry list
- `src/impl-web/console/src/router.tsx` — `/enis` and `/eni/:namespace/:name`
- `src/impl-web/console/src/components/layout/Sidebar.tsx` — sidebar entry
- `src/impl-web/console/src/views/dpu/DpuView.tsx` — ENI cells clickable
- `src/impl-web/console/src/views/vnet/VnetView.tsx` — ENI cells clickable
- `src/impl-web/console/src/views/fleet/FleetView.tsx` — ENI cells clickable
- `src/impl-web/console/tests/eni-view.test.tsx` — 12 new Vitest cases
- `src/impl-web/console/tests/components.test.tsx` — sidebar nav-paths snapshot updated to include `/enis`

---

## Out of scope for Phase A

These are intentional deferrals, not bugs:

- **Per-rule hit counters** (`counters.rule_hits`) — wire field reserved; populated in Phase B alongside the WebSocket counter stream.
- **Edit / Delete actions** — this page is read-only for debug. Mutations stay in the kind-specific views (`/policies`, `/routing`, etc.).
- **Cross-namespace ACL / Route references** — the aggregator warns about them; full UX for cross-namespace will come with the namespace-picker design (Phase B).

---

## Smoke test

Against the dev fleet:

```bash
curl http://localhost:8080/api/console/eni/default/eni-blue-1/detail | jq .

# Expect:
#  - identity, vnet, placement, ha_set blocks populated
#  - acls_inbound + acls_outbound split correctly
#  - service_tunnels filtered to ones referenced from routes/mappings
#  - counters match array lengths
```

Then open `http://localhost:8080/eni/default/eni-blue-1` in a browser —
the page renders all 7 tabs with real data, deep-link works from a cold tab,
and the Trace-flow button takes you to `/flow-trace?eni_name=eni-blue-1&vni=100`.

---

## Verification gate (what was checked before declaring done)

- ✅ `go vet ./...` clean (no new warnings)
- ✅ `go build ./...` clean
- ✅ `go test ./internal/aggregation/...` → **8/8 pass** (5 existing + 3 new `TestEniDetail_*`)
- ✅ `npx tsc --noEmit` → exit 0
- ✅ `npm test` → **246/246 pass** (234 existing + 12 new `eni-view.test.tsx`)
- ✅ ENI 404 path returns 404, not a half-rendered page
- ✅ Multi-DPU HA placement renders all chips + the `HA · active-active` badge
- ✅ Trace-flow button URL contains `eni_name=` and `vni=`
- ✅ Sidebar `NAV_GROUPS` snapshot test updated to reflect new structural intent