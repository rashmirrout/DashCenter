import {
  Server,
  Crown,
  Cpu,
  HardDrive,
  Network,
  Activity,
  CheckCircle2,
  AlertTriangle,
  XCircle,
  ChevronDown,
  ChevronRight,
  MapPin,
} from 'lucide-react';
import { useState } from 'react';
import { cn } from '@/lib/cn';
import { useServiceTopology } from '@/queries/hooks';
import { PageHeader } from '@/components/layout/PageHeader';
import { LoadingSkeleton } from '@/components/feedback/LoadingSkeleton';
import { ErrorState } from '@/components/feedback/ErrorState';
import type {
  ClusterNodeInfo,
  ApplianceTopInfo,
  DpuTopInfo,
  ZoneTopInfo,
  TopologySummary,
} from '@/api/types';

/* ── Status helpers ──────────────────────────────────────────── */

function nodeStatusColor(status: string): string {
  if (status === 'ok') return 'text-emerald-400';
  if (status === 'degraded') return 'text-amber-400';
  return 'text-red-400';
}

function nodeStatusIcon(status: string) {
  if (status === 'ok') return <CheckCircle2 size={14} className="text-emerald-400" />;
  if (status === 'degraded') return <AlertTriangle size={14} className="text-amber-400" />;
  return <XCircle size={14} className="text-red-400" />;
}

function dpuStateColor(state: string): string {
  if (state === 'DPU_STATE_UP') return 'text-emerald-400';
  if (state === 'DPU_STATE_REGISTERING' || state === 'DPU_STATE_RECONCILING')
    return 'text-amber-400';
  return 'text-red-400';
}

function dpuStateBadge(state: string) {
  const label = state.replace('DPU_STATE_', '').toLowerCase();
  const color =
    state === 'DPU_STATE_UP'
      ? 'bg-emerald-500/15 text-emerald-400 border-emerald-500/30'
      : state === 'DPU_STATE_REGISTERING' || state === 'DPU_STATE_RECONCILING'
        ? 'bg-amber-500/15 text-amber-400 border-amber-500/30'
        : 'bg-red-500/15 text-red-400 border-red-500/30';
  return (
    <span className={cn('inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border', color)}>
      {label}
    </span>
  );
}

function tierBadge(tier: string) {
  const colors: Record<string, string> = {
    gold: 'bg-yellow-500/15 text-yellow-400 border-yellow-500/30',
    silver: 'bg-slate-400/15 text-slate-300 border-slate-400/30',
    bronze: 'bg-orange-500/15 text-orange-400 border-orange-500/30',
  };
  return (
    <span className={cn('inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border', colors[tier] ?? 'bg-white/5 text-[color:var(--text-secondary)] border-white/10')}>
      {tier}
    </span>
  );
}

/* ── Summary Cards ──────────────────────────────────────────── */

function SummaryCards({ summary, nodeCount }: { summary: TopologySummary; nodeCount: number }) {
  const cards = [
    { label: 'Controller Nodes', value: nodeCount, icon: Server, color: 'text-cyan-400' },
    { label: 'Appliances', value: summary.total_appliances, icon: HardDrive, color: 'text-purple-400' },
    { label: 'DPUs', value: summary.total_dpus, icon: Cpu, color: 'text-blue-400' },
    { label: 'ENIs', value: summary.total_enis, icon: Network, color: 'text-teal-400' },
    { label: 'Healthy DPUs', value: summary.healthy_dpus, icon: CheckCircle2, color: 'text-emerald-400' },
    { label: 'Degraded', value: summary.degraded_dpus + summary.offline_dpus, icon: AlertTriangle, color: summary.degraded_dpus + summary.offline_dpus > 0 ? 'text-amber-400' : 'text-[color:var(--text-muted)]' },
  ];

  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3">
      {cards.map((c) => {
        const Icon = c.icon;
        return (
          <div
            key={c.label}
            className="rounded-lg border border-[color:var(--border-subtle)] bg-[color:var(--bg-secondary)] p-3"
          >
            <div className="flex items-center gap-2 mb-1">
              <Icon size={14} className={c.color} />
              <span className="text-[11px] text-[color:var(--text-muted)] uppercase tracking-wider">
                {c.label}
              </span>
            </div>
            <p className="text-2xl font-semibold text-[color:var(--text-primary)]">{c.value}</p>
          </div>
        );
      })}
    </div>
  );
}

/* ── Cluster Nodes Panel ─────────────────────────────────────── */

function ClusterPanel({ nodes, leaderId, healthy }: { nodes: ClusterNodeInfo[]; leaderId: string; healthy: boolean }) {
  return (
    <div className="rounded-lg border border-[color:var(--border-subtle)] bg-[color:var(--bg-secondary)]">
      <div className="flex items-center justify-between px-4 py-3 border-b border-[color:var(--border-subtle)]">
        <div className="flex items-center gap-2">
          <Server size={16} className="text-cyan-400" />
          <h3 className="text-sm font-semibold text-[color:var(--text-primary)]">
            Controller Cluster
          </h3>
        </div>
        <div className="flex items-center gap-2">
          {healthy ? (
            <span className="flex items-center gap-1 text-xs text-emerald-400">
              <CheckCircle2 size={12} /> Healthy
            </span>
          ) : (
            <span className="flex items-center gap-1 text-xs text-amber-400">
              <AlertTriangle size={12} /> Degraded
            </span>
          )}
        </div>
      </div>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-0 divide-y md:divide-y-0 md:divide-x divide-[color:var(--border-subtle)]">
        {nodes.map((node) => {
          const isLeader = node.is_leader;
          return (
            <div
              key={node.addr}
              className={cn(
                'p-4 relative',
                isLeader && 'bg-cyan-500/5'
              )}
            >
              {isLeader && (
                <div className="absolute top-2 right-2">
                  <Crown size={14} className="text-yellow-400" />
                </div>
              )}
              <div className="flex items-center gap-2 mb-2">
                {nodeStatusIcon(node.status)}
                <span className="text-sm font-medium text-[color:var(--text-primary)]">
                  {node.node_id}
                </span>
                {isLeader && (
                  <span className="text-[10px] px-1.5 py-0.5 rounded bg-yellow-500/15 text-yellow-400 border border-yellow-500/30 font-medium">
                    LEADER
                  </span>
                )}
              </div>
              <div className="space-y-1 text-xs text-[color:var(--text-secondary)]">
                <div className="flex justify-between">
                  <span>Address</span>
                  <span className="font-mono text-[color:var(--text-primary)] text-[11px]">
                    {node.addr.replace(/^https?:\/\//, '')}
                  </span>
                </div>
                <div className="flex justify-between">
                  <span>Status</span>
                  <span className={nodeStatusColor(node.status)}>{node.status}</span>
                </div>
                <div className="flex justify-between">
                  <span>DPUs</span>
                  <span className="text-[color:var(--text-primary)]">{node.dpu_count}</span>
                </div>
                <div className="flex justify-between">
                  <span>Latency</span>
                  <span className="text-[color:var(--text-primary)]">{node.latency_ms} ms</span>
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

/* ── Zone Summary ────────────────────────────────────────────── */

function ZoneSummary({ zones }: { zones: ZoneTopInfo[] }) {
  if (zones.length === 0) return null;
  return (
    <div className="rounded-lg border border-[color:var(--border-subtle)] bg-[color:var(--bg-secondary)]">
      <div className="flex items-center gap-2 px-4 py-3 border-b border-[color:var(--border-subtle)]">
        <MapPin size={16} className="text-purple-400" />
        <h3 className="text-sm font-semibold text-[color:var(--text-primary)]">
          Availability Zones
        </h3>
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-0 divide-y sm:divide-y-0 sm:divide-x divide-[color:var(--border-subtle)]">
        {zones.map((z) => (
          <div key={z.zone} className="p-4">
            <p className="text-sm font-medium text-[color:var(--text-primary)] mb-2">{z.zone}</p>
            <div className="grid grid-cols-3 gap-2 text-xs">
              <div>
                <span className="text-[color:var(--text-muted)]">Appliances</span>
                <p className="text-[color:var(--text-primary)] font-medium">{z.appliance_count}</p>
              </div>
              <div>
                <span className="text-[color:var(--text-muted)]">DPUs</span>
                <p className="text-[color:var(--text-primary)] font-medium">{z.dpu_count}</p>
              </div>
              <div>
                <span className="text-[color:var(--text-muted)]">ENIs</span>
                <p className="text-[color:var(--text-primary)] font-medium">{z.eni_count}</p>
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

/* ── DPU Row ─────────────────────────────────────────────────── */

function DpuRow({ dpu }: { dpu: DpuTopInfo }) {
  const [expanded, setExpanded] = useState(false);
  const hasEnis = dpu.enis && dpu.enis.length > 0;

  return (
    <div className="border-t border-[color:var(--border-subtle)]">
      <button
        type="button"
        onClick={() => hasEnis && setExpanded(!expanded)}
        disabled={!hasEnis}
        className={cn(
          'w-full flex items-center gap-3 px-4 py-2.5 text-left transition-colors',
          hasEnis && 'hover:bg-white/3 cursor-pointer',
          !hasEnis && 'cursor-default'
        )}
      >
        <div className="w-4">
          {hasEnis ? (
            expanded ? <ChevronDown size={14} className="text-[color:var(--text-muted)]" /> : <ChevronRight size={14} className="text-[color:var(--text-muted)]" />
          ) : (
            <span className="w-3.5" />
          )}
        </div>
        <Cpu size={14} className={dpuStateColor(dpu.state)} />
        <span className="text-sm font-mono text-[color:var(--text-primary)] min-w-[120px]">
          {dpu.id}
        </span>
        <span className="text-xs text-[color:var(--text-muted)] min-w-[60px]">
          slot {dpu.slot}
        </span>
        {dpuStateBadge(dpu.state)}
        <span className="ml-auto text-xs text-[color:var(--text-secondary)]">
          {dpu.eni_count} ENI{dpu.eni_count !== 1 ? 's' : ''}
        </span>
      </button>
      {expanded && hasEnis && (
        <div className="ml-12 mr-4 mb-2">
          <table className="w-full text-xs">
            <thead>
              <tr className="text-[color:var(--text-muted)] border-b border-[color:var(--border-subtle)]">
                <th className="text-left py-1 font-medium">ENI Name</th>
                <th className="text-left py-1 font-medium">Vnet</th>
                <th className="text-left py-1 font-medium">MAC</th>
                <th className="text-left py-1 font-medium">State</th>
              </tr>
            </thead>
            <tbody>
              {dpu.enis!.map((eni) => (
                <tr
                  key={eni.name}
                  className="border-b border-[color:var(--border-subtle)] last:border-b-0"
                >
                  <td className="py-1.5 font-mono text-[color:var(--text-primary)]">{eni.name}</td>
                  <td className="py-1.5 text-[color:var(--text-secondary)]">{eni.vnet_name ?? '—'}</td>
                  <td className="py-1.5 font-mono text-[color:var(--text-secondary)]">{eni.mac_address ?? '—'}</td>
                  <td className="py-1.5">
                    {eni.admin_state === 'enabled' ? (
                      <span className="text-emerald-400">enabled</span>
                    ) : (
                      <span className="text-[color:var(--text-muted)]">{eni.admin_state ?? '—'}</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

/* ── Appliance Card ──────────────────────────────────────────── */

function ApplianceCard({ appliance }: { appliance: ApplianceTopInfo }) {
  const [expanded, setExpanded] = useState(true);
  const totalEnis = appliance.dpus.reduce((sum, d) => sum + d.eni_count, 0);

  return (
    <div className="rounded-lg border border-[color:var(--border-subtle)] bg-[color:var(--bg-secondary)] overflow-hidden">
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-3 px-4 py-3 hover:bg-white/3 transition-colors text-left"
      >
        {expanded ? (
          <ChevronDown size={16} className="text-[color:var(--text-muted)]" />
        ) : (
          <ChevronRight size={16} className="text-[color:var(--text-muted)]" />
        )}
        <HardDrive size={16} className="text-purple-400" />
        <span className="text-sm font-semibold text-[color:var(--text-primary)]">
          {appliance.id}
        </span>
        {appliance.zone && (
          <span className="text-[10px] px-1.5 py-0.5 rounded bg-blue-500/15 text-blue-400 border border-blue-500/30">
            {appliance.zone}
          </span>
        )}
        {appliance.tier && tierBadge(appliance.tier)}
        <span className="ml-auto flex items-center gap-3 text-xs text-[color:var(--text-secondary)]">
          <span>{appliance.dpus.length} DPU{appliance.dpus.length !== 1 ? 's' : ''}</span>
          <span>{totalEnis} ENI{totalEnis !== 1 ? 's' : ''}</span>
        </span>
      </button>
      {expanded && (
        <div>
          {appliance.dpus
            .sort((a, b) => a.slot - b.slot)
            .map((dpu) => (
              <DpuRow key={dpu.id} dpu={dpu} />
            ))}
        </div>
      )}
    </div>
  );
}

/* ── Main View ───────────────────────────────────────────────── */

export default function TopologyDashboardView() {
  const { data, isLoading, error } = useServiceTopology();

  if (isLoading) {
    return (
      <div className="p-6 space-y-6">
        <PageHeader
          title="Service Topology"
          subtitle="Controller cluster, appliances, DPUs, and ENI endpoints"
        />
        <LoadingSkeleton lines={8} />
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="p-6 space-y-6">
        <PageHeader
          title="Service Topology"
          subtitle="Controller cluster, appliances, DPUs, and ENI endpoints"
        />
        <ErrorState
          title="Failed to load topology"
          message={error?.message ?? 'No data received from BFF'}
        />
      </div>
    );
  }

  const sortedAppliances = [...data.appliances].sort((a, b) =>
    a.id.localeCompare(b.id)
  );

  return (
    <div className="p-6 space-y-6">
      <PageHeader
        title="Service Topology"
        subtitle="Controller cluster, appliances, DPUs, and ENI endpoints"
        actions={
          <div className="flex items-center gap-1.5 text-xs text-[color:var(--text-muted)]">
            <Activity size={12} />
            <span>Live · 10s poll</span>
          </div>
        }
      />

      {/* Summary cards */}
      <SummaryCards summary={data.summary} nodeCount={data.cluster.node_count} />

      {/* Controller cluster */}
      <ClusterPanel
        nodes={data.cluster.nodes}
        leaderId={data.cluster.leader_id}
        healthy={data.cluster.healthy}
      />

      {/* Zone summary */}
      <ZoneSummary zones={data.zones} />

      {/* Appliances */}
      <div className="space-y-3">
        <div className="flex items-center gap-2">
          <HardDrive size={16} className="text-purple-400" />
          <h3 className="text-sm font-semibold text-[color:var(--text-primary)]">
            Appliances ({sortedAppliances.length})
          </h3>
        </div>
        {sortedAppliances.map((app) => (
          <ApplianceCard key={app.id} appliance={app} />
        ))}
      </div>
    </div>
  );
}