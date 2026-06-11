# dashw SPA — DashCenter Web Console Frontend

React 18 + TypeScript + Vite 5 single-page application for the DashCenter
Web Console. Served by the dashw BFF via `go:embed`.

## Quick Start

```bash
# Install dependencies
npm install

# Development server (hot reload, proxied to BFF on :8080)
npm run dev

# Type check
npx tsc --noEmit

# Run tests
npm test

# Production build (output → dist/)
npm run build
```

## Architecture

```
src/
├── api/            # API client, TypeScript types, CRUD functions
│   ├── client.ts       # Base fetch wrapper with ApiError
│   ├── types.ts        # 40+ interfaces mirroring proto + BFF types
│   ├── dashd-rest.ts   # Per-kind CRUD (vnetApi, eniApi, etc.)
│   ├── dashd-admin.ts  # Admin API (health, leader, drift, audit)
│   └── console-api.ts  # BFF aggregation (fleet, dpu, topology, capacity)
│
├── queries/        # TanStack Query layer
│   ├── keys.ts         # Query key factory (13 domains)
│   └── hooks.ts        # 20 query hooks + 9 mutation hooks
│
├── stores/         # Zustand state
│   └── ui-prefs-store.ts  # Persisted UI preferences (sidebar, pageSize)
│
├── lib/            # Shared utilities
│   ├── cn.ts               # clsx + tailwind-merge
│   ├── constants.ts        # POLL_INTERVALS, WS_ENDPOINTS, DPU_STATES
│   ├── format.ts           # 10 formatters (IP, MAC, bytes, duration, etc.)
│   ├── schemas.ts          # Zod schemas for 8 resource types
│   └── command-registry.ts # 31 dashctl command metadata entries
│
├── components/     # Design system
│   ├── layout/         # Sidebar, PageHeader
│   ├── feedback/       # GlassCard, StatusBadge, StatsCard, ErrorBoundary, etc.
│   ├── visualization/  # CapacityGauge (SVG radial)
│   ├── data/           # DataTable (sort, filter, paginate)
│   └── form/           # IpInput, MacInput, CidrInput, PortRangeInput, etc.
│
├── views/          # 13 route views (all lazy-loaded)
│   ├── dashboard/      # Fleet health, stats, capacity gauges
│   ├── fleet/          # DPU card grid with status
│   ├── dpu/            # DPU detail: capacity, ENIs, drift
│   ├── vnet/           # Vnet list + detail
│   ├── routing/        # Route policies with rules
│   ├── tunnel/         # Service tunnels
│   ├── policy/         # ACL policies with expandable rules
│   ├── health/         # dashd health, leader, reconcile
│   ├── audit/          # Timestamped event log
│   ├── admin-ops/      # CRUD operations, reconcile
│   ├── flow-trace/     # Simulate packet → verdict + stages
│   ├── command/        # Interactive dashctl command builder
│   └── debug/          # 12 quick endpoints + raw API caller
│
├── styles/
│   └── globals.css     # CSS custom properties, dark theme tokens
│
├── App.tsx         # Root layout (Sidebar + TopBar + Outlet + Toaster)
├── router.tsx      # Route table with lazy imports + ErrorBoundary
└── main.tsx        # React entry point (QueryClient + RouterProvider)
```

## Views

| View | Route | Description |
|------|-------|-------------|
| Dashboard | `/dashboard` | Fleet health overview, stats cards, capacity gauges |
| Fleet | `/fleet` | DPU card grid with status badges |
| DPU Detail | `/fleet/dpu/:dpuId` | Per-DPU capacity, ENI list, drift items |
| Vnets | `/vnets`, `/vnets/:name` | Vnet list and detail |
| Routing | `/routing` | Route policies with rules |
| Tunnels | `/tunnels` | Service tunnel list |
| Policies | `/policies` | ACL policies with expandable rules |
| Health | `/health` | dashd controller health, leader, reconcile |
| Audit | `/audit` | Timestamped audit event log |
| Admin Ops | `/admin-ops` | Create/edit/delete resources, batch upload |
| Flow Trace | `/flow-trace` | Simulate packet flow → verdict + matched rules |
| Command | `/command` | Interactive dashctl command builder + executor |
| Debug | `/debug` | Raw API caller + 12 quick admin endpoints |

## Data Flow

```
Browser ──REST──► dashw BFF (:8080) ──REST──► dashd (:8443, :7443)
                      │
                      ├── /api/v1/*     → proxy to dashd REST
                      ├── /api/admin/*  → proxy to dashd Admin
                      └── /api/console/* → BFF aggregation endpoints
```

- **TanStack Query** manages all server state with configurable polling
  intervals (5s–30s depending on data volatility)
- **Zustand** manages client-only UI state (sidebar, page size)
- **Zod** validates resource forms before submission

## Design System

- **Dark theme**: CSS custom properties in `globals.css`
- **Glass morphism**: `GlassCard` with backdrop blur + border glow
- **Status colors**: green/amber/red/cyan mapped via `STATUS_COLORS`
- **Capacity gauges**: SVG radial with threshold-based colors
- **Responsive**: Desktop-first, works at 1440px and 1024px

## Testing

```bash
npm test               # 71 tests (format, schemas, command-registry)
npm run test:watch     # Watch mode
npm run test:coverage  # Coverage report
```

## Build

```bash
npm run build          # tsc + vite → dist/
```

Output: ~101KB gzip initial load, 26 chunks, all views lazy-loaded.

## Key Dependencies

| Package | Purpose |
|---------|---------|
| `react` 18 | UI framework |
| `react-router-dom` 6 | Client-side routing |
| `@tanstack/react-query` 5 | Server state + polling |
| `zustand` 4 | Client state |
| `zod` 3 | Schema validation |
| `tailwindcss` 4 | Utility-first CSS |
| `lucide-react` | Icon library |
| `sonner` | Toast notifications |
| `recharts` | Charts (Phase B+) |
| `@xyflow/react` | Topology graph (Phase B+) |
| `framer-motion` | Animations (Phase B+) |