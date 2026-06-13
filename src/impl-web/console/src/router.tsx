import { createBrowserRouter, Navigate } from 'react-router-dom';
import { lazy, Suspense, type ReactNode } from 'react';
import App from './App';
import { ErrorBoundary } from '@/components/feedback/ErrorBoundary';

/* ── Lazy-loaded views ─────────────────────────────────────── */
const DashboardView = lazy(() => import('./views/dashboard/DashboardView'));
const FleetView = lazy(() => import('./views/fleet/FleetView'));
const DpuView = lazy(() => import('./views/dpu/DpuView'));
const VnetView = lazy(() => import('./views/vnet/VnetView'));
const RoutingView = lazy(() => import('./views/routing/RoutingView'));
const TunnelView = lazy(() => import('./views/tunnel/TunnelView'));
const PolicyView = lazy(() => import('./views/policy/PolicyView'));
const HealthView = lazy(() => import('./views/health/HealthView'));
const AuditView = lazy(() => import('./views/audit/AuditView'));
const AdminOpsView = lazy(() => import('./views/admin-ops/AdminOpsView'));
const FlowTraceView = lazy(() => import('./views/flow-trace/FlowTraceView'));
const CommandView = lazy(() => import('./views/command/CommandView'));
const TopologyDashboardView = lazy(() => import('./views/topology/TopologyDashboardView'));
const TopologyV2View = lazy(() => import('./views/topology-v2/TopologyV2View'));
const DebugView = lazy(() => import('./views/debug/DebugView'));

/* ── Loading fallback + error boundary ─────────────────────── */
function ViewLoader({ children, name }: { children: ReactNode; name?: string }) {
  return (
    <ErrorBoundary viewName={name}>
      <Suspense
        fallback={
          <div className="flex items-center justify-center h-64">
            <div className="text-[color:var(--text-secondary)] animate-pulse">
              Loading view…
            </div>
          </div>
        }
      >
        {children}
      </Suspense>
    </ErrorBoundary>
  );
}

/* ── Route table (LLD §5) ──────────────────────────────────── */
/**
 * Routes per spec:
 *   /dashboard           → DashboardView
 *   /fleet               → FleetView
 *   /dpu/:dpuId          → DpuView          (NOT /fleet/dpu/:dpuId)
 *   /vnet/:vnetName      → VnetView         (singular)
 *   /routing             → RoutingView
 *   /tunnels             → TunnelView
 *   /policies            → PolicyView
 *   /flow-trace          → FlowTraceView
 *   /audit               → AuditView
 *   /health              → HealthView
 *   /admin               → AdminOpsView     (NOT /admin-ops)
 *   /commands            → CommandView      (plural)
 *   /debug               → DebugView
 */
export const router = createBrowserRouter([
  {
    path: '/',
    element: <App />,
    children: [
      { index: true, element: <Navigate to="/dashboard" replace /> },
      { path: 'dashboard', element: <ViewLoader name="Dashboard"><DashboardView /></ViewLoader> },
      { path: 'fleet', element: <ViewLoader name="Fleet"><FleetView /></ViewLoader> },
      { path: 'topology', element: <ViewLoader name="Topology"><TopologyDashboardView /></ViewLoader> },
      { path: 'topology-v2', element: <ViewLoader name="Topology v2"><TopologyV2View /></ViewLoader> },
      { path: 'dpu/:dpuId', element: <ViewLoader name="DPU Detail"><DpuView /></ViewLoader> },
      { path: 'vnet/:vnetName', element: <ViewLoader name="Vnet Detail"><VnetView /></ViewLoader> },
      { path: 'routing', element: <ViewLoader name="Routing"><RoutingView /></ViewLoader> },
      { path: 'tunnels', element: <ViewLoader name="Tunnels"><TunnelView /></ViewLoader> },
      { path: 'policies', element: <ViewLoader name="Policies"><PolicyView /></ViewLoader> },
      { path: 'flow-trace', element: <ViewLoader name="Flow Trace"><FlowTraceView /></ViewLoader> },
      { path: 'audit', element: <ViewLoader name="Audit"><AuditView /></ViewLoader> },
      { path: 'health', element: <ViewLoader name="Health"><HealthView /></ViewLoader> },
      { path: 'admin', element: <ViewLoader name="Admin Ops"><AdminOpsView /></ViewLoader> },
      { path: 'commands', element: <ViewLoader name="Commands"><CommandView /></ViewLoader> },
      { path: 'debug', element: <ViewLoader name="Debug"><DebugView /></ViewLoader> },
      /* Backward-compat redirects from old paths */
      { path: 'fleet/dpu/:dpuId', element: <Navigate to="/dpu/__redir__" replace /> },
      { path: 'vnets', element: <Navigate to="/fleet" replace /> },
      { path: 'vnets/:vnetName', element: <Navigate to="/vnet/__redir__" replace /> },
      { path: 'admin-ops', element: <Navigate to="/admin" replace /> },
      { path: 'command', element: <Navigate to="/commands" replace /> },
      /* 404 fallback */
      { path: '*', element: <Navigate to="/dashboard" replace /> },
    ],
  },
]);