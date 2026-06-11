import { createBrowserRouter, Navigate } from 'react-router-dom';
import { lazy, Suspense } from 'react';
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
const DebugView = lazy(() => import('./views/debug/DebugView'));

/* ── Loading fallback + error boundary ─────────────────────── */
function ViewLoader({ children, name }: { children: React.ReactNode; name?: string }) {
  return (
    <ErrorBoundary viewName={name}>
      <Suspense
        fallback={
          <div className="flex items-center justify-center h-64">
            <div className="text-text-secondary animate-pulse">Loading view...</div>
          </div>
        }
      >
        {children}
      </Suspense>
    </ErrorBoundary>
  );
}

/* ── Route table ───────────────────────────────────────────── */
export const router = createBrowserRouter([
  {
    path: '/',
    element: <App />,
    children: [
      { index: true, element: <Navigate to="/dashboard" replace /> },
      { path: 'dashboard', element: <ViewLoader name="Dashboard"><DashboardView /></ViewLoader> },
      { path: 'fleet', element: <ViewLoader name="Fleet"><FleetView /></ViewLoader> },
      { path: 'fleet/dpu/:dpuId', element: <ViewLoader name="DPU Detail"><DpuView /></ViewLoader> },
      { path: 'vnets', element: <ViewLoader name="Vnets"><VnetView /></ViewLoader> },
      { path: 'vnets/:vnetName', element: <ViewLoader name="Vnet Detail"><VnetView /></ViewLoader> },
      { path: 'routing', element: <ViewLoader name="Routing"><RoutingView /></ViewLoader> },
      { path: 'tunnels', element: <ViewLoader name="Tunnels"><TunnelView /></ViewLoader> },
      { path: 'policies', element: <ViewLoader name="Policies"><PolicyView /></ViewLoader> },
      { path: 'health', element: <ViewLoader name="Health"><HealthView /></ViewLoader> },
      { path: 'audit', element: <ViewLoader name="Audit"><AuditView /></ViewLoader> },
      { path: 'admin-ops', element: <ViewLoader name="Admin Ops"><AdminOpsView /></ViewLoader> },
      { path: 'flow-trace', element: <ViewLoader name="Flow Trace"><FlowTraceView /></ViewLoader> },
      { path: 'command', element: <ViewLoader name="Command"><CommandView /></ViewLoader> },
      { path: 'debug', element: <ViewLoader name="Debug"><DebugView /></ViewLoader> },
    ],
  },
]);