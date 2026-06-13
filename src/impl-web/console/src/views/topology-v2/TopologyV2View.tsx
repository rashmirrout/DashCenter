/**
 * TopologyV2View — live fleet topology powered by the dashw SSE hub.
 *
 * Architecture (see docs/dashd-features/topology-streaming-design.md):
 *
 *   useTopologyStream() ──► dashw /api/console/topology-v2/stream (SSE)
 *           │
 *           ▼
 *   useTopologyV2Store (Zustand)
 *           │ shallow selectors
 *           ▼
 *   ┌─────────────────────────────────────────────┐
 *   │  StatusBand  (live | paused | reconnecting) │
 *   │  ────────────────────────────────────────── │
 *   │  ┌──────────┬─────────────┬──────────────┐  │
 *   │  │ Cluster  │  Appliance  │   Drawer     │  │
 *   │  │ panel    │  cards grid │   inspector  │  │
 *   │  └──────────┴─────────────┴──────────────┘  │
 *   │  Recent events ticker (footer)              │
 *   └─────────────────────────────────────────────┘
 *
 * Browser → ONLY dashw. Never dashd directly.
 */
import { useState } from 'react';
import {
  Crown, Server, Cpu, HardDrive, Network, CheckCircle2,
  AlertTriangle, XCircle, Activity, Pause, Play, Radio, Wifi, WifiOff, X,
} from 'lucide-react';
import { cn } from '@/lib/cn';
import { PageHeader } from '@/components/layout/PageHeader';
import { Drawer } from '@/components/feedback/Drawer';
import { useTopologyStream } from '@/queries/useTopologyStream';
import {
  useTopologyV2Store, selectCluster, selectAppliances, selectSummary,
  selectStreamHealth, selectEventLog, findEntity,
} from '@/stores/topology-v2-store';
import type { TopologyEvent } from '@/api/topology-v2-types';

/* ────────────────────────────────────────────────────────────── */

function ConnectionBadge() {
  const health = useTopologyV2Store(selectStreamHealth);
  const ageMs = health.lastEventAt ? Date.now() - health.lastEventAt : Infinity;
  const isStale = ageMs > 45_000 && health.connection === 'open';

  const map: Record<typeof health.connection, { label: string; icon: any; color: string }> = {
    idle:         { label: 'Idle',         icon: WifiOff,  color: 'text-[color:var(--text-muted)]' },
    connecting:   { label: 'Connecting…',  icon: Activity, color: 'text-amber-400' },
    open:         { label: isStale ? `Stale (${Math.round(ageMs / 1000)}s)` : 'Live', icon: Radio, color: isStale ? 'text-amber-400' : 'text-emerald-400' },
    reconnecting: { label: 'Reconnecting…', icon: Activity, color: 'text-amber-400' },
    paused:       { label: 'Paused (tab hidden)', icon: Pause, color: 'text-slate-400' },
    error:        { label: 'Error',        icon: XCircle,  color: 'text-red-400' },
  };
  const e = map[health.connection];
  const Icon = e.icon;

  return (
    <div className="flex items-center gap-3 px-3 py-1.5 rounded-md border border-[color:var(--border-subtle)] bg-[color:var(--bg-secondary)]">
      <div className="flex items-center gap-1.5">
        <Icon size={14} className={e.color} />
        <span className={cn('text-xs font-medium', e.color)}>{e.label}</span>
      </div>
      <div className="h-3 w-px bg-[color:var(--border-subtle)]" />
      <span className="text-[10px] text-[color:var(--text-muted)] uppercase tracking-wider">
        cursor #{health.lastEventAt ? useTopologyV2Store.getState().lastEventId : 0}
      </span>
      {health.upstreamReconnects > 0 && (
        <span className="text-[10px] text-amber-400 uppercase tracking-wider">
          ⤺ {health.upstreamReconnects} resync
        </span>
      )}
      {health.droppedEvents > 0 && (
        <span className="text-[10px] text-red-400 uppercase tracking-wider">
          ⚠ {health.droppedEvents} dropped
        </span>
      )}
    </div>
  );
}

function SummaryStrip() {
  const summary = useTopologyV2Store(selectSummary);
  const cluster = useTopologyV2Store(selectCluster);
  if (!summary || !cluster) return null;

  const cards = [
    { label: 'Controllers', value: cluster.node_count, icon: Server, color: 'text-cyan-400' },
    { label: 'Appliances',  value: summary.total_appliances, icon: HardDrive, color: 'text-purple-400' },
    { label: 'DPUs',        value: summary.total_dpus, icon: Cpu, color: 'text-blue-400' },
    { label: 'ENIs',        value: summary.total_enis, icon: Network, color: 'text-teal-400' },
    { label: 'Healthy',     value: summary.healthy_dpus, icon: CheckCircle2, color: 'text-emerald-400' },
    { label: 'Issues',
      value: summary.degraded_dpus + summary.offline_dpus + summary.cordoned_dpus,
      icon: AlertTriangle,
      color: (summary.degraded_dpus + summary.offline_dpus) > 0 ? 'text-amber-400' : 'text-[color:var(--text-muted)]' },
  ];

  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3">
      {cards.map((c) => {
        const Icon = c.icon;
        return (
          <div key={c.label} className="rounded-lg border border-[color:var(--border-subtle)] bg-[color:var(--bg-secondary)] p-3">
            <div className="flex items-center gap-2 mb-1">
              <Icon size={14} className={c.color} />
              <span className="text-[11px] text-[color:var(--text-muted)] uppercase tracking-wider">{c.label}</span>
            </div>
            <p className="text-2xl font-semibold text-[color:var(--text-primary)]">{c.value}</p>
          </div>
        );
      })}
    </div>
  );
}

function ClusterPanel() {
  const cluster = useTopologyV2Store(selectCluster);
  const select = useTopologyV2Store((s) => s.select);
  if (!cluster) return null;

  return (
    <div className="rounded-lg border border-[color:var(--border-subtle)] bg-[color:var(--bg-secondary)]">
      <div className="flex items-center justify-between px-4 py-3 border-b border-[color:var(--border-subtle)]">
        <div className="flex items-center gap-2">
          <Server size={16} className="text-cyan-400" />
          <h3 className="text-sm font-semibold text-[color:var(--text-primary)]">Controller Cluster</h3>
        </div>
        <span className={cn('text-xs', cluster.healthy ? 'text-emerald-400' : 'text-amber-400')}>
          {cluster.healthy ? '● healthy' : '⚠ degraded'}
        </span>
      </div>
      <div className="p-3 grid gap-2">
        {cluster.nodes.map((n) => (
          <button
            key={n.node_id}
            onClick={() => select('node', n.node_id)}
            className={cn(
              'group flex items-center justify-between rounded-md border px-3 py-2 text-left transition-all',
              n.is_leader
                ? 'border-yellow-500/40 bg-yellow-500/5 hover:bg-yellow-500/10'
                : 'border-[color:var(--border-subtle)] bg-[color:var(--bg-primary)] hover:border-cyan-500/40 hover:bg-cyan-500/5',
            )}
          >
            <div className="flex items-center gap-2 min-w-0">
              {n.is_leader
                ? <Crown size={14} className="text-yellow-400 animate-pulse flex-shrink-0" />
                : <Server size={14} className="text-cyan-400 flex-shrink-0" />}
              <span className="text-sm font-medium text-[color:var(--text-primary)] truncate">{n.node_id}</span>
              {n.is_leader && (
                <span className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded bg-yellow-500/15 text-yellow-300 border border-yellow-500/30">leader</span>
              )}
            </div>
            <span className="text-[11px] text-[color:var(--text-muted)] font-mono truncate ml-3">
              {n.rest_addr || '—'}
            </span>
          </button>
        ))}
      </div>
    </div>
  );
}

function AppliancesGrid() {
  const appliances = useTopologyV2Store(selectAppliances);
  const select = useTopologyV2Store((s) => s.select);

  if (appliances.length === 0) {
    return (
      <div className="rounded-lg border border-[color:var(--border-subtle)] bg-[color:var(--bg-secondary)] p-6 text-center text-sm text-[color:var(--text-muted)]">
        No appliances reported. Waiting for snapshot…
      </div>
    );
  }

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
      {appliances.map((a) => (
        <div
          key={a.id}
          className="rounded-lg border border-[color:var(--border-subtle)] bg-[color:var(--bg-secondary)] overflow-hidden"
        >
          <button
            onClick={() => select('appliance', a.id)}
            className="w-full flex items-center justify-between px-3 py-2 border-b border-[color:var(--border-subtle)] hover:bg-[color:var(--bg-primary)] transition-colors text-left"
          >
            <div className="flex items-center gap-2">
              <HardDrive size={14} className="text-purple-400" />
              <span className="text-sm font-medium text-[color:var(--text-primary)]">{a.id}</span>
            </div>
            <div className="flex items-center gap-2">
              {a.zone && <span className="text-[10px] text-[color:var(--text-muted)]">{a.zone}</span>}
              {a.tier && (
                <span className={cn(
                  'text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded border',
                  a.tier === 'gold' && 'bg-yellow-500/15 text-yellow-400 border-yellow-500/30',
                  a.tier === 'silver' && 'bg-slate-400/15 text-slate-300 border-slate-400/30',
                  a.tier === 'bronze' && 'bg-orange-500/15 text-orange-400 border-orange-500/30',
                )}>{a.tier}</span>
              )}
            </div>
          </button>
          <div className="p-2 grid grid-cols-2 gap-2">
            {a.dpus.map((d) => (
              <button
                key={d.id}
                onClick={() => select('dpu', d.id)}
                className={cn(
                  'group rounded-md border px-2 py-1.5 text-left transition-all',
                  d.cordoned
                    ? 'border-amber-500/40 bg-amber-500/5'
                    : d.state === 'DPU_STATE_UP'
                      ? 'border-emerald-500/30 bg-emerald-500/5 hover:bg-emerald-500/10'
                      : 'border-red-500/30 bg-red-500/5 hover:bg-red-500/10',
                )}
              >
                <div className="flex items-center gap-1.5">
                  <Cpu size={11} className={d.state === 'DPU_STATE_UP' ? 'text-emerald-400' : 'text-red-400'} />
                  <span className="text-xs font-mono text-[color:var(--text-primary)] truncate">{d.id}</span>
                </div>
                <div className="flex items-center justify-between mt-0.5">
                  <span className="text-[10px] text-[color:var(--text-muted)]">
                    {d.state.replace('DPU_STATE_', '').toLowerCase()}
                  </span>
                  <span className="text-[10px] text-[color:var(--text-muted)]">
                    {d.eni_count} ENI{d.eni_count === 1 ? '' : 's'}
                  </span>
                </div>
              </button>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function EventTicker() {
  const log = useTopologyV2Store(selectEventLog);
  const recent = log.slice(-12).reverse();

  if (recent.length === 0) return null;
  return (
    <div className="rounded-lg border border-[color:var(--border-subtle)] bg-[color:var(--bg-secondary)]">
      <div className="flex items-center gap-2 px-4 py-2 border-b border-[color:var(--border-subtle)]">
        <Activity size={14} className="text-cyan-400" />
        <h3 className="text-xs font-semibold uppercase tracking-wider text-[color:var(--text-secondary)]">
          Live events
        </h3>
        <span className="text-[10px] text-[color:var(--text-muted)] ml-auto">
          showing last {recent.length} of {log.length}
        </span>
      </div>
      <div className="max-h-48 overflow-y-auto font-mono text-[11px]">
        {recent.map((ev, idx) => (
          <EventRow key={`${ev.event_id ?? 0}-${idx}`} ev={ev} />
        ))}
      </div>
    </div>
  );
}

function EventRow({ ev }: { ev: TopologyEvent }) {
  const ts = ev.ts ? new Date(ev.ts).toLocaleTimeString() : '—';
  const kind = ev.kind.replace('KIND_', '').toLowerCase();
  const colorMap: Record<string, string> = {
    snapshot: 'text-blue-400',
    peer_added: 'text-emerald-400',
    peer_removed: 'text-red-400',
    peer_updated: 'text-cyan-400',
    leader_changed: 'text-yellow-400',
    dpu_state: 'text-amber-400',
    dpu_added: 'text-emerald-400',
    dpu_removed: 'text-red-400',
    keepalive: 'text-[color:var(--text-muted)]',
    dropped: 'text-red-400',
    rate_limited: 'text-amber-400',
    resync: 'text-purple-400',
  };
  const detail = ev.peer?.node_id
    ?? ev.dpu?.id
    ?? ev.new_leader_id
    ?? ev.notice?.message
    ?? '';
  return (
    <div className="grid grid-cols-[80px_140px_1fr_60px] gap-2 px-4 py-1 hover:bg-[color:var(--bg-primary)] border-b border-[color:var(--border-subtle)]/30">
      <span className="text-[color:var(--text-muted)]">{ts}</span>
      <span className={cn('uppercase tracking-wider', colorMap[kind] ?? 'text-[color:var(--text-secondary)]')}>{kind}</span>
      <span className="text-[color:var(--text-primary)] truncate">{detail}</span>
      <span className="text-right text-[color:var(--text-muted)]">#{ev.event_id ?? '–'}</span>
    </div>
  );
}

function InspectorDrawer() {
  const selectedKind = useTopologyV2Store((s) => s.selectedKind);
  const selectedId = useTopologyV2Store((s) => s.selectedId);
  const select = useTopologyV2Store((s) => s.select);
  const entity = useTopologyV2Store((s) => findEntity(s, s.selectedKind, s.selectedId));

  return (
    <Drawer
      open={!!selectedKind && !!selectedId}
      onClose={() => select(undefined, undefined)}
      title={selectedKind ? `${selectedKind.toUpperCase()}: ${selectedId}` : ''}
      subtitle="Live inspector"
      width="lg"
    >
      {entity ? (
        <pre className="text-xs font-mono overflow-auto text-[color:var(--text-primary)] p-3 rounded-md bg-[color:var(--bg-primary)] border border-[color:var(--border-subtle)]">
{JSON.stringify(entity, null, 2)}
        </pre>
      ) : (
        <div className="text-sm text-[color:var(--text-muted)] flex items-center gap-2">
          <X size={14} /> Entity no longer present in topology (probably removed).
        </div>
      )}
    </Drawer>
  );
}

/* ────────────────────────────────────────────────────────────── */

export default function TopologyV2View() {
  const [includeEnis, setIncludeEnis] = useState(false);
  useTopologyStream({ includeEnis });

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Topology v2 — Live"
        subtitle="Browser ↔ dashw ↔ dashd. Streaming via SSE with last-event-id resume + per-tenant caps."
        actions={
          <div className="flex items-center gap-3">
            <label className="flex items-center gap-2 text-xs text-[color:var(--text-secondary)]">
              <input
                type="checkbox"
                checked={includeEnis}
                onChange={(e) => setIncludeEnis(e.target.checked)}
                className="rounded border-[color:var(--border-subtle)]"
              />
              Include ENIs
            </label>
            <ConnectionBadge />
          </div>
        }
      />

      <SummaryStrip />

      <div className="grid grid-cols-1 xl:grid-cols-[360px_1fr] gap-3">
        <ClusterPanel />
        <AppliancesGrid />
      </div>

      <EventTicker />
      <InspectorDrawer />
    </div>
  );
}
