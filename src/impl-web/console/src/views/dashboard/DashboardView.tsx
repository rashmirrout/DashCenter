/**
 * DashboardView — the operational landing page at `/dashboard`.
 *
 * Layout (top → bottom):
 *   1) PageHeader (unchanged, simple breadcrumb + subtitle)
 *   2) Hero grid: Fleet Connectivity Viz + the 4 primary KPIs
 *   3) Fleet Health · DPU States · dashd Controller (3-up)
 *   4) Fleet Capacity gauges
 *   5) Per-DPU "Snapshot" — REPLACED: was a plain table, now a
 *      responsive grid of clickable DPU cards with mini ENI/route
 *      utilization bars and a live heartbeat dot.
 *   6) Resources tiles (clickable deep-links)
 *   7) Recent Activity Feed — NEW: top-5 audit-log events with
 *     timestamp, action, target resource, animated reveal.
 *   8) DPU Roster (compact, unchanged)
 *
 * Animations:
 *   • framer-motion staggered reveal on every section as the page
 *     scrolls in (`viewport: { once: true }` so it only fires the
 *     first time each section enters view).
 *   • Numeric KPIs use AnimatedCounter (count-up).
 *   • CapacityGauge has gradient stroke + animated arc.
 *   • FleetConnectivityViz pulses heartbeat + particle-flow lines.
 *   • Floating gradient orbs in the page background (cyan/purple),
 *     much subtler than the HomeView hero.
 */
import { useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { motion, type Variants } from 'framer-motion';
import {
  Activity,
  AlertTriangle,
  Cable,
  Cpu,
  Globe,
  Layers,
  Route,
  Shield,
  Clock,
  ArrowRight,
} from 'lucide-react';
import { PageHeader } from '@/components/layout/PageHeader';
import { StatsCard } from '@/components/feedback/StatsCard';
import { GlassCard } from '@/components/feedback/GlassCard';
import { CapacityGauge } from '@/components/visualization/CapacityGauge';
import { StatusBadge } from '@/components/feedback/StatusBadge';
import { CardSkeleton } from '@/components/feedback/LoadingSkeleton';
import { ErrorState } from '@/components/feedback/ErrorState';
import { FleetConnectivityViz } from '@/components/visualization/FleetConnectivityViz';
import { AnimatedCounter } from '@/components/visualization/AnimatedCounter';
import {
  useFleetSummary,
  useCapacityStats,
  useDashdHealth,
  useEniPlacement,
  useAclPolicies,
  useRoutePolicies,
  useServiceTunnels,
  useHaSets,
  useAuditLog,
} from '@/queries/hooks';
import { formatDuration, stripStatePrefix } from '@/lib/format';
import {
  connectedDpuCount,
  eniPlacementCountsByDpu,
  fleetCapacity,
  fleetDegradedDpus,
  fleetDisconnectedDpus,
  fleetDpuCount,
  fleetDpuStates,
  fleetEniCount,
  fleetHealthyDpus,
  fleetVnetCount,
} from '@/lib/api-helpers';
import { cn } from '@/lib/cn';
import type { AuditEntry } from '@/api/types';

/* ── Animation presets ──────────────────────────────────────── */
const sectionVariants: Variants = {
  hidden: { opacity: 0, y: 14 },
  visible: { opacity: 1, y: 0, transition: { duration: 0.4, ease: 'easeOut' } },
};
const gridStagger: Variants = {
  hidden: { opacity: 0 },
  visible: { opacity: 1, transition: { staggerChildren: 0.06, delayChildren: 0.05 } },
};

/* ─────────────────────────────────────────────────────────── */

export default function DashboardView() {
  const navigate = useNavigate();
  const fleet = useFleetSummary();
  const capacity = useCapacityStats();
  const health = useDashdHealth();
  const placements = useEniPlacement();
  const acls = useAclPolicies('default');
  const routes = useRoutePolicies('default');
  const tunnels = useServiceTunnels('default');
  const haSets = useHaSets('default');
  const audit = useAuditLog();

  const placementCounts = useMemo(
    () => eniPlacementCountsByDpu(placements.data?.items),
    [placements.data?.items]
  );

  if (fleet.isError) {
    return <ErrorState message={fleet.error.message} onRetry={() => fleet.refetch()} />;
  }

  const fs = fleet.data;
  const cs = capacity.data;
  const hd = health.data;

  const cap = fleetCapacity(cs);
  const dpus = fleetDpuCount(fs);
  const enis = fleetEniCount(fs);
  const vnets = fleetVnetCount(fs);
  const healthy = fleetHealthyDpus(fs);
  const degraded = fleetDegradedDpus(fs);
  const disconnected = fleetDisconnectedDpus(fs);
  const states = fleetDpuStates(fs);
  const drift = fs?.drift_count ?? 0;

  // Top-5 most recent audit entries (the API returns newest-first).
  const recentAudit = (audit.data?.items ?? []).slice(0, 5);

  return (
    <div className="relative isolate animate-fade-in">
      {/* Decorative floating gradient orbs (very subtle on the dashboard) */}
      <div aria-hidden className="pointer-events-none absolute inset-0 -z-10 overflow-hidden">
        <div
          className="absolute -top-24 -left-32 w-[28rem] h-[28rem] rounded-full blur-3xl animate-pulse-slow opacity-50"
          style={{
            background:
              'radial-gradient(circle, rgba(0,212,255,0.10) 0%, transparent 70%)',
          }}
        />
        <div
          className="absolute top-1/4 -right-32 w-[32rem] h-[32rem] rounded-full blur-3xl animate-pulse-slow opacity-50"
          style={{
            background:
              'radial-gradient(circle, rgba(168,85,247,0.09) 0%, transparent 70%)',
            animationDelay: '1.5s',
          }}
        />
      </div>

      <div className="space-y-6">
        <PageHeader
          title="Dashboard"
          subtitle="Fleet health, capacity, and controller status — live"
        />

        {/* ═══════ HERO ═══════════════════════════════════ */}
        <motion.section
          initial="hidden"
          whileInView="visible"
          viewport={{ once: true }}
          variants={sectionVariants}
          className="grid grid-cols-1 lg:grid-cols-[420px_1fr] gap-4"
        >
          {/* Fleet Connectivity Map */}
          <GlassCard className="flex flex-col items-center justify-center min-h-[440px]">
            <div className="w-full flex items-center justify-between mb-2">
              <p className="text-[10px] uppercase tracking-[0.14em] text-[color:var(--text-secondary)]">
                Fleet Connectivity
              </p>
              {fleet.isFetching && (
                <span className="text-[10px] text-[color:var(--text-muted)] animate-pulse">
                  refreshing…
                </span>
              )}
            </div>
            <FleetConnectivityViz size={380} />
          </GlassCard>

          {/* Primary KPI grid (4 stats cards) */}
          <motion.div
            initial="hidden"
            whileInView="visible"
            viewport={{ once: true }}
            variants={gridStagger}
            className="grid grid-cols-1 sm:grid-cols-2 gap-4 content-start"
          >
            {fleet.isLoading ? (
              Array.from({ length: 4 }, (_, i) => (
                <motion.div key={i} variants={sectionVariants}>
                  <CardSkeleton />
                </motion.div>
              ))
            ) : (
              <>
                <motion.div variants={sectionVariants}>
                  <StatsCard
                    label="DPUs"
                    value={dpus}
                    accent="cyan"
                    icon={<Cpu size={20} className="text-[color:var(--accent-cyan)]" />}
                  />
                </motion.div>
                <motion.div variants={sectionVariants}>
                  <StatsCard
                    label="ENIs"
                    value={enis}
                    accent="purple"
                    icon={<Layers size={20} className="text-[color:var(--accent-purple)]" />}
                  />
                </motion.div>
                <motion.div variants={sectionVariants}>
                  <StatsCard
                    label="Vnets"
                    value={vnets}
                    accent="green"
                    icon={<Globe size={20} className="text-[color:var(--accent-green)]" />}
                  />
                </motion.div>
                <motion.div variants={sectionVariants}>
                  <StatsCard
                    label="Drift Items"
                    value={drift}
                    accent={drift > 0 ? 'amber' : 'cyan'}
                    icon={
                      <AlertTriangle
                        size={20}
                        className={
                          drift > 0
                            ? 'text-[color:var(--accent-amber)]'
                            : 'text-[color:var(--text-muted)]'
                        }
                      />
                    }
                  />
                </motion.div>
              </>
            )}
          </motion.div>
        </motion.section>

        {/* ═══════ HEALTH / STATES / CONTROLLER (3-up) ═══════ */}
        <motion.section
          initial="hidden"
          whileInView="visible"
          viewport={{ once: true }}
          variants={gridStagger}
          className="grid grid-cols-1 lg:grid-cols-3 gap-4"
        >
          {/* Fleet Health */}
          <motion.div variants={sectionVariants}>
            <GlassCard
              glow={disconnected > 0 ? 'red' : degraded > 0 ? 'amber' : 'green'}
              className="h-full"
            >
              <div className="flex items-center justify-between mb-3">
                <p className="text-[10px] uppercase tracking-[0.14em] text-[color:var(--text-secondary)]">
                  Fleet Health
                </p>
                <Activity size={14} className="text-[color:var(--text-muted)]" />
              </div>
              <div className="flex items-end justify-around gap-3">
                <HealthMetric label="Healthy" value={healthy} accent="green" pulse={healthy > 0} />
                <HealthMetric label="Degraded" value={degraded} accent="amber" pulse={degraded > 0} />
                <HealthMetric label="Offline" value={disconnected} accent="red" pulse={disconnected > 0} />
              </div>
            </GlassCard>
          </motion.div>

          {/* DPU States */}
          <motion.div variants={sectionVariants}>
            <GlassCard className="h-full">
              <p className="text-[10px] uppercase tracking-[0.14em] text-[color:var(--text-secondary)] mb-3">
                DPU States
              </p>
              {Object.keys(states).length === 0 ? (
                <span className="text-[color:var(--text-muted)] text-sm">No state data</span>
              ) : (
                <div className="flex flex-wrap gap-2">
                  {Object.entries(states).map(([state, count]) => (
                    <div
                      key={state}
                      className="flex items-center gap-1.5 px-2 py-1 rounded-md bg-white/5 hover:bg-white/10 transition-colors"
                    >
                      <StatusBadge status={state} />
                      <span className="text-sm font-mono text-[color:var(--text-primary)] tabular-nums">
                        <AnimatedCounter value={count as number} duration={0.8} />
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </GlassCard>
          </motion.div>

          {/* dashd Controller */}
          <motion.div variants={sectionVariants}>
            <GlassCard glow={hd?.leader ? 'cyan' : undefined} className="h-full">
              <p className="text-[10px] uppercase tracking-[0.14em] text-[color:var(--text-secondary)] mb-3">
                dashd Controller
              </p>
              {hd ? (
                <div className="space-y-2 text-sm">
                  <Row label="Role" value={<StatusBadge status={hd.leader ? 'LEADER' : 'FOLLOWER'} />} />
                  <Row
                    label="Connected DPUs"
                    value={
                      <span className="font-mono text-[color:var(--text-primary)] tabular-nums">
                        <AnimatedCounter value={connectedDpuCount(hd)} duration={0.8} />
                      </span>
                    }
                  />
                  {hd.uptime_seconds != null && (
                    <Row
                      label="Uptime"
                      value={
                        <span className="font-mono text-[color:var(--text-primary)]">
                          {formatDuration(hd.uptime_seconds)}
                        </span>
                      }
                    />
                  )}
                  {hd.cluster_size != null && (
                    <Row
                      label="Cluster Size"
                      value={
                        <span className="font-mono text-[color:var(--text-primary)] tabular-nums">
                          {hd.cluster_size}
                        </span>
                      }
                    />
                  )}
                  {hd.leader_id && (
                    <Row
                      label="Leader ID"
                      value={
                        <span
                          className="font-mono text-[color:var(--text-primary)] truncate max-w-[160px]"
                          title={hd.leader_id}
                        >
                          {hd.leader_id.slice(0, 12)}…
                        </span>
                      }
                    />
                  )}
                </div>
              ) : health.isLoading ? (
                <CardSkeleton />
              ) : (
                <span className="text-[color:var(--text-muted)] text-sm">No health data</span>
              )}
            </GlassCard>
          </motion.div>
        </motion.section>

        {/* ═══════ CAPACITY GAUGES ═══════════════════════ */}
        <motion.section
          initial="hidden"
          whileInView="visible"
          viewport={{ once: true }}
          variants={sectionVariants}
        >
          <GlassCard>
            <div className="flex items-center justify-between mb-4">
              <p className="text-[10px] uppercase tracking-[0.14em] text-[color:var(--text-secondary)]">
                Fleet Capacity
              </p>
              {capacity.isFetching && (
                <span className="text-[10px] text-[color:var(--text-muted)] animate-pulse">
                  refreshing…
                </span>
              )}
            </div>
            {cs ? (
              <div className="grid grid-cols-2 md:grid-cols-4 gap-6">
                <CapacityGauge label="ENIs" used={cap.enisUsed} max={cap.enisMax} />
                <CapacityGauge label="Routes" used={cap.routesUsed} max={cap.routesMax} />
                <CapacityGauge label="ACL Rules" used={cap.aclRulesUsed} max={cap.aclRulesMax} />
                <CapacityGauge label="Flows" used={cap.flowsUsed} max={cap.flowsMax} />
              </div>
            ) : (
              <div className="flex justify-center py-4 text-[color:var(--text-muted)] text-sm">
                {capacity.isLoading ? 'Loading capacity data…' : 'No capacity data available'}
              </div>
            )}
          </GlassCard>
        </motion.section>

        {/* ═══════ PER-DPU SNAPSHOT (cards, not table) ════ */}
        {cs && (cs.per_dpu?.length ?? 0) > 0 && (
          <motion.section
            initial="hidden"
            whileInView="visible"
            viewport={{ once: true }}
            variants={sectionVariants}
          >
            <GlassCard>
              <p className="text-[10px] uppercase tracking-[0.14em] text-[color:var(--text-secondary)] mb-3">
                Per-DPU Snapshot
              </p>
              <motion.div
                variants={gridStagger}
                initial="hidden"
                whileInView="visible"
                viewport={{ once: true }}
                className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 xl:grid-cols-4 gap-3"
              >
                {(cs.per_dpu ?? []).map((row) => {
                  const id = row.dpu_id ?? row.id ?? '?';
                  const capEni = row.eni_count ?? row.enis_used ?? 0;
                  const placementEni = placementCounts[id] ?? 0;
                  const eniUsed = placementEni > 0 ? placementEni : capEni;
                  const eniMax = row.eni_max ?? row.enis_max ?? 0;
                  const routeUsed = row.route_count ?? row.routes_used ?? 0;
                  const routeMax = row.route_max ?? row.routes_max ?? 0;
                  return (
                    <motion.div key={id} variants={sectionVariants}>
                      <DpuMiniCard
                        id={id}
                        state={String(row.state)}
                        eniUsed={eniUsed}
                        eniMax={eniMax}
                        routeUsed={routeUsed}
                        routeMax={routeMax}
                        onClick={() => navigate(`/dpu/${encodeURIComponent(id)}`)}
                      />
                    </motion.div>
                  );
                })}
              </motion.div>
            </GlassCard>
          </motion.section>
        )}

        {/* ═══════ RESOURCES (clickable tiles) ═══════════ */}
        <motion.section
          initial="hidden"
          whileInView="visible"
          viewport={{ once: true }}
          variants={sectionVariants}
        >
          <GlassCard>
            <p className="text-[10px] uppercase tracking-[0.14em] text-[color:var(--text-secondary)] mb-3">
              Resources
            </p>
            <motion.div
              variants={gridStagger}
              initial="hidden"
              whileInView="visible"
              viewport={{ once: true }}
              className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm"
            >
              <motion.div variants={sectionVariants}>
                <ResourceTile
                  icon={<Shield size={16} className="text-[color:var(--accent-cyan)]" />}
                  label="ACL Policies"
                  value={
                    acls.data?.items?.length ?? fs?.acl_policy_count ?? fs?.total_acl_policies
                  }
                  onClick={() => navigate('/policies')}
                />
              </motion.div>
              <motion.div variants={sectionVariants}>
                <ResourceTile
                  icon={<Route size={16} className="text-[color:var(--accent-purple)]" />}
                  label="Route Policies"
                  value={
                    routes.data?.items?.length ?? fs?.route_policy_count ?? fs?.total_route_policies
                  }
                  onClick={() => navigate('/routing')}
                />
              </motion.div>
              <motion.div variants={sectionVariants}>
                <ResourceTile
                  icon={<Cable size={16} className="text-[color:var(--accent-green)]" />}
                  label="Service Tunnels"
                  value={
                    tunnels.data?.items?.length ??
                    fs?.service_tunnel_count ??
                    fs?.total_service_tunnels
                  }
                  onClick={() => navigate('/tunnels')}
                />
              </motion.div>
              <motion.div variants={sectionVariants}>
                <ResourceTile
                  icon={<Activity size={16} className="text-[color:var(--accent-amber)]" />}
                  label="HA Sets"
                  value={haSets.data?.items?.length ?? fs?.ha_set_count ?? fs?.total_ha_sets}
                  onClick={() => navigate('/policies')}
                />
              </motion.div>
            </motion.div>
          </GlassCard>
        </motion.section>

        {/* ═══════ RECENT ACTIVITY FEED ═══════════════════ */}
        <motion.section
          initial="hidden"
          whileInView="visible"
          viewport={{ once: true }}
          variants={sectionVariants}
        >
          <GlassCard>
            <div className="flex items-center justify-between mb-3">
              <p className="text-[10px] uppercase tracking-[0.14em] text-[color:var(--text-secondary)]">
                Recent Activity
              </p>
              <button
                type="button"
                onClick={() => navigate('/audit')}
                className="text-[11px] text-[color:var(--accent-cyan)] hover:text-[color:var(--accent-purple)] inline-flex items-center gap-1 transition-colors"
              >
                View all <ArrowRight size={11} />
              </button>
            </div>
            {audit.isLoading ? (
              <CardSkeleton />
            ) : recentAudit.length === 0 ? (
              <span className="text-[color:var(--text-muted)] text-sm">No recent activity</span>
            ) : (
              <motion.ul
                variants={gridStagger}
                initial="hidden"
                whileInView="visible"
                viewport={{ once: true }}
                className="space-y-1.5"
              >
                {recentAudit.map((entry) => (
                  <motion.li key={entry.id} variants={sectionVariants}>
                    <AuditRow entry={entry} />
                  </motion.li>
                ))}
              </motion.ul>
            )}
          </GlassCard>
        </motion.section>

        {/* ═══════ DPU ROSTER (compact) ═══════════════════ */}
        {fs?.dpus && fs.dpus.length > 0 && (
          <motion.section
            initial="hidden"
            whileInView="visible"
            viewport={{ once: true }}
            variants={sectionVariants}
          >
            <GlassCard>
              <p className="text-[10px] uppercase tracking-[0.14em] text-[color:var(--text-secondary)] mb-3">
                DPU Roster
              </p>
              <motion.div
                variants={gridStagger}
                initial="hidden"
                whileInView="visible"
                viewport={{ once: true }}
                className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-5 gap-2 text-xs"
              >
                {fs.dpus.map((d) => (
                  <motion.button
                    key={d.id}
                    variants={sectionVariants}
                    onClick={() => navigate(`/dpu/${encodeURIComponent(d.id)}`)}
                    whileHover={{ y: -1 }}
                    className="flex items-center gap-2 px-2 py-1.5 rounded-md bg-white/[0.02] border border-[color:var(--border-subtle)] hover:bg-white/[0.05] hover:border-[color:var(--border-default)] transition-colors text-left"
                  >
                    <StatusBadge status={d.state} />
                    <span className="font-mono text-[color:var(--text-primary)] truncate">
                      {d.id}
                    </span>
                  </motion.button>
                ))}
              </motion.div>
              <p className="text-[10px] text-[color:var(--text-muted)] mt-3">
                {fs.dpus.length} DPUs reporting · last sync{' '}
                {fs.timestamp ? new Date(fs.timestamp).toLocaleTimeString() : '—'}
              </p>
            </GlassCard>
          </motion.section>
        )}
      </div>
    </div>
  );
}

/* ── Internal helper components ─────────────────────────────── */

function HealthMetric({
  label,
  value,
  accent,
  pulse,
}: {
  label: string;
  value: number;
  accent: 'green' | 'amber' | 'red';
  pulse?: boolean;
}) {
  const colorVar =
    accent === 'green'
      ? 'var(--accent-green)'
      : accent === 'amber'
        ? 'var(--accent-amber)'
        : 'var(--accent-red)';
  return (
    <div className="text-center">
      <div className="relative inline-flex items-center justify-center mb-1">
        {pulse && (
          <span
            aria-hidden
            className="absolute inset-0 rounded-full animate-ping"
            style={{ background: colorVar, opacity: 0.18 }}
          />
        )}
        <span
          className="relative inline-block w-2 h-2 rounded-full"
          style={{ background: colorVar, boxShadow: `0 0 8px ${colorVar}` }}
        />
      </div>
      <span className="block text-3xl font-semibold tabular-nums" style={{ color: colorVar }}>
        <AnimatedCounter value={value} duration={0.9} />
      </span>
      <span className="text-[10px] text-[color:var(--text-muted)] uppercase tracking-wider">
        {label}
      </span>
    </div>
  );
}

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex justify-between items-center">
      <span className="text-[color:var(--text-secondary)]">{label}</span>
      {value}
    </div>
  );
}

function ResourceTile({
  icon,
  label,
  value,
  onClick,
}: {
  icon: React.ReactNode;
  label: string;
  value: number | undefined;
  onClick?: () => void;
}) {
  const isInteractive = !!onClick && (value ?? 0) > 0;
  const baseCls = 'flex items-center gap-3 transition-all';
  const interactiveCls = isInteractive
    ? 'rounded-md p-1 -m-1 hover:bg-white/5 hover:translate-x-0.5 cursor-pointer text-left'
    : '';
  const numericValue = typeof value === 'number' ? value : 0;
  const showNumber = value != null;
  return (
    <motion.div
      whileHover={isInteractive ? { x: 2 } : undefined}
      transition={{ type: 'spring', stiffness: 260, damping: 22 }}
    >
      {isInteractive ? (
        <button type="button" onClick={onClick} className={cn(baseCls, interactiveCls)}>
          <ResourceTileBody icon={icon} label={label} numericValue={numericValue} showNumber={showNumber} isInteractive />
        </button>
      ) : (
        <div className={baseCls}>
          <ResourceTileBody icon={icon} label={label} numericValue={numericValue} showNumber={showNumber} isInteractive={false} />
        </div>
      )}
    </motion.div>
  );
}

function ResourceTileBody({
  icon,
  label,
  numericValue,
  showNumber,
  isInteractive,
}: {
  icon: React.ReactNode;
  label: string;
  numericValue: number;
  showNumber: boolean;
  isInteractive: boolean;
}) {
  return (
    <>
      <div className="p-2 rounded-md bg-white/5">{icon}</div>
      <div className="min-w-0">
        <span className="block text-[10px] text-[color:var(--text-muted)] uppercase tracking-wider">
          {label}
        </span>
        <span
          className={cn(
            'block text-lg font-mono tabular-nums',
            isInteractive
              ? 'text-[color:var(--accent-cyan)] hover:underline'
              : 'text-[color:var(--text-primary)]'
          )}
        >
          {showNumber ? <AnimatedCounter value={numericValue} duration={0.8} /> : '—'}
        </span>
      </div>
    </>
  );
}

/* ── DPU Mini Card (replaces the per-DPU table) ────────────── */

function DpuMiniCard({
  id,
  state,
  eniUsed,
  eniMax,
  routeUsed,
  routeMax,
  onClick,
}: {
  id: string;
  state: string;
  eniUsed: number;
  eniMax: number;
  routeUsed: number;
  routeMax: number;
  onClick: () => void;
}) {
  const stateUpper = state.toUpperCase();
  const isUp = stateUpper.includes('UP') || stateUpper === 'READY' || stateUpper === 'CONNECTED';
  const isDegraded = stateUpper.includes('DEGRADED') || stateUpper === 'DRAINING';
  const dotColor = isUp
    ? 'var(--accent-green)'
    : isDegraded
      ? 'var(--accent-amber)'
      : 'var(--accent-red)';

  return (
    <motion.button
      type="button"
      onClick={onClick}
      whileHover={{ y: -2 }}
      transition={{ type: 'spring', stiffness: 260, damping: 22 }}
      className="w-full text-left rounded-lg border border-[color:var(--border-subtle)] bg-white/[0.02] hover:bg-white/[0.05] hover:border-[color:var(--border-default)] p-3 transition-colors"
    >
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-2 min-w-0">
          <span
            aria-hidden
            className={cn(
              'inline-block w-2 h-2 rounded-full shrink-0',
              isUp && 'animate-heartbeat'
            )}
            style={{ background: dotColor, boxShadow: `0 0 8px ${dotColor}` }}
          />
          <span
            className="font-mono text-[12px] text-[color:var(--text-primary)] truncate"
            title={id}
          >
            {id}
          </span>
        </div>
        <StatusBadge status={state} />
      </div>
      <UtilBar label="ENIs" used={eniUsed} max={eniMax} accent="cyan" />
      <UtilBar label="Routes" used={routeUsed} max={routeMax} accent="purple" />
    </motion.button>
  );
}

function UtilBar({
  label,
  used,
  max,
  accent,
}: {
  label: string;
  used: number;
  max: number;
  accent: 'cyan' | 'purple';
}) {
  const pct = max > 0 ? Math.min(100, (used / max) * 100) : 0;
  const critical = pct >= 90;
  const warn = pct >= 70;
  const fromColor =
    critical ? '#ef4444' : warn ? '#f59e0b' : accent === 'cyan' ? '#00d4ff' : '#a855f7';
  const toColor =
    critical ? '#f87171' : warn ? '#fbbf24' : accent === 'cyan' ? '#a855f7' : '#ec4899';

  return (
    <div className="mt-1.5">
      <div className="flex justify-between text-[10px] text-[color:var(--text-muted)] mb-0.5">
        <span>{label}</span>
        <span className="tabular-nums">
          {used}/{max}
        </span>
      </div>
      <div className="relative h-1 rounded-full bg-white/[0.06] overflow-hidden">
        <motion.div
          className="absolute inset-y-0 left-0 rounded-full"
          style={{
            background: `linear-gradient(90deg, ${fromColor}, ${toColor})`,
            boxShadow: critical ? `0 0 6px ${fromColor}` : undefined,
          }}
          initial={{ width: 0 }}
          animate={{ width: `${pct}%` }}
          transition={{ duration: 0.9, ease: 'easeOut' }}
        />
      </div>
    </div>
  );
}

/* ── Audit Row (recent activity feed) ──────────────────────── */

function AuditRow({ entry }: { entry: AuditEntry }) {
  const navigate = useNavigate();
  const ts = entry.timestamp ? new Date(entry.timestamp) : null;
  const tsLabel = ts ? ts.toLocaleTimeString() : '—';
  const action = (entry.action || '').toLowerCase();
  const actionTier: 'create' | 'update' | 'delete' | 'other' = action.includes('delete')
    ? 'delete'
    : action.includes('create') || action.includes('put')
      ? 'create'
      : action.includes('update') || action.includes('patch')
        ? 'update'
        : 'other';
  const tierColor: Record<typeof actionTier, string> = {
    create: 'var(--accent-green)',
    update: 'var(--accent-cyan)',
    delete: 'var(--accent-red)',
    other: 'var(--text-muted)',
  };
  const dotColor = tierColor[actionTier];
  const result = (entry.result || '').toLowerCase();
  const failed = result === 'error' || result === 'failed' || result === 'fail';

  // Best-effort deep-link by resource kind.
  const kindLower = (entry.resource_kind || '').toLowerCase();
  const deepLink: string | null =
    kindLower === 'vnet'
      ? `/vnet/${encodeURIComponent(entry.resource_name)}`
      : kindLower === 'eni'
        ? `/eni/${encodeURIComponent(entry.namespace || 'default')}/${encodeURIComponent(entry.resource_name)}`
        : kindLower.includes('acl')
          ? '/policies'
          : kindLower.includes('route')
            ? '/routing'
            : kindLower.includes('tunnel')
              ? '/tunnels'
              : null;

  const handleClick = deepLink ? () => navigate(deepLink) : undefined;

  return (
    <button
      type="button"
      onClick={handleClick}
      className={cn(
        'group w-full flex items-center gap-3 px-2 py-1.5 rounded-md transition-colors text-left',
        handleClick
          ? 'cursor-pointer hover:bg-white/[0.04]'
          : 'cursor-default'
      )}
      title={entry.detail || `${entry.action} ${entry.resource_kind}/${entry.resource_name}`}
    >
      {/* Status dot */}
      <span
        aria-hidden
        className="shrink-0 inline-block w-1.5 h-1.5 rounded-full"
        style={{ background: dotColor, boxShadow: `0 0 6px ${dotColor}` }}
      />
      {/* Timestamp */}
      <span className="shrink-0 inline-flex items-center gap-1 text-[10px] font-mono text-[color:var(--text-muted)] tabular-nums w-[7.5rem]">
        <Clock size={10} />
        {tsLabel}
      </span>
      {/* Action + kind/name */}
      <span className="flex-1 min-w-0 text-xs truncate">
        <span className="uppercase tracking-wider text-[10px] mr-1.5" style={{ color: dotColor }}>
          {entry.action || 'event'}
        </span>
        <span className="text-[color:var(--text-secondary)]">{entry.resource_kind}</span>
        <span className="text-[color:var(--text-muted)] mx-1">/</span>
        <span className="font-mono text-[color:var(--text-primary)]">{entry.resource_name}</span>
        {failed && (
          <span className="ml-2 inline-flex items-center gap-1 text-[10px] text-[color:var(--accent-red)]">
            <AlertTriangle size={10} /> {entry.result}
          </span>
        )}
      </span>
      {handleClick && (
        <ArrowRight
          size={12}
          className="shrink-0 text-[color:var(--text-muted)] group-hover:text-[color:var(--accent-cyan)] transition-colors"
        />
      )}
    </button>
  );
}

/* Keep the legacy export so older imports don't break. */
export { stripStatePrefix };