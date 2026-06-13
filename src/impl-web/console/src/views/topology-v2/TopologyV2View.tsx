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
import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  Crown, Server, Cpu, HardDrive, Network, CheckCircle2,
  AlertTriangle, XCircle, Activity, Pause, Play, Radio, Wifi, WifiOff, X,
  Square, Trash2, Info, RefreshCw, Lock, Unlock, Loader2,
} from 'lucide-react';
import { cn } from '@/lib/cn';
import { PageHeader } from '@/components/layout/PageHeader';
import { Drawer } from '@/components/feedback/Drawer';
import { useTopologyStream } from '@/queries/useTopologyStream';
import {
  useTopologyV2Store, selectCluster, selectAppliances, selectSummary,
  selectStreamHealth, selectEventLog, findEntity,
} from '@/stores/topology-v2-store';
import type { TopologyEvent, TopologyV2Response } from '@/api/topology-v2-types';

/* ────────────────────────────────────────────────────────────── */

function ConnectionBadge({ streaming }: { streaming: boolean }) {
  const health = useTopologyV2Store(selectStreamHealth);
  const ageMs = health.lastEventAt ? Date.now() - health.lastEventAt : Infinity;
  const isStale = ageMs > 45_000 && health.connection === 'open';

  // Provenance line shown under the status badge whenever we have
  // either source or via (the dashw BFF stamps both on every frame).
  const provenance = (health.lastSource || health.lastVia) ? (
    <span
      className="text-[10px] text-[color:var(--text-muted)] font-mono tracking-tight"
      title="Source = upstream dashd that produced this event · Via = dashw replica that relayed it"
    >
      {health.lastSource || '?'} <span className="text-[color:var(--border-strong)]">→</span> {health.lastVia || '?'}
    </span>
  ) : null;

  // When the user has streaming OFF, show a clean "Off" badge instead
  // of the underlying ConnectionState (which would say "idle").
  if (!streaming) {
    return (
      <div className="flex flex-col items-end gap-0.5 px-3 py-1.5 rounded-md border border-[color:var(--border-subtle)] bg-[color:var(--bg-secondary)]">
        <div className="flex items-center gap-1.5">
          <WifiOff size={14} className="text-[color:var(--text-muted)]" />
          <span className="text-xs font-medium text-[color:var(--text-muted)]">Live: Off</span>
        </div>
      </div>
    );
  }

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
      <div className="grid grid-cols-[80px_140px_1fr_140px_60px] gap-2 px-4 py-1 border-b border-[color:var(--border-subtle)]/50 text-[10px] uppercase tracking-wider text-[color:var(--text-muted)]">
        <span>Time</span><span>Kind</span><span>Detail</span><span className="text-right">Source → Via</span><span className="text-right">ID</span>
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
  // Compact provenance string "dashd-1→dashw-0" rendered as a tooltip
  // hint on the kind cell, so the column-grid stays tight.
  const provenance = (ev.source || ev.via)
    ? `${ev.source ?? '?'} → ${ev.via ?? '?'}`
    : '';
  return (
    <div className="grid grid-cols-[80px_140px_1fr_140px_60px] gap-2 px-4 py-1 hover:bg-[color:var(--bg-primary)] border-b border-[color:var(--border-subtle)]/30">
      <span className="text-[color:var(--text-muted)]">{ts}</span>
      <span
        className={cn('uppercase tracking-wider', colorMap[kind] ?? 'text-[color:var(--text-secondary)]')}
        title={provenance ? `from ${provenance}` : undefined}
      >
        {kind}
      </span>
      <span className="text-[color:var(--text-primary)] truncate">{detail}</span>
      <span
        className="text-[color:var(--text-muted)] truncate text-right"
        title={provenance}
      >
        {provenance || '—'}
      </span>
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
        <div className="flex flex-col gap-3">
          {selectedKind === 'dpu' && selectedId && (
            <DpuActions dpuId={selectedId} cordoned={!!(entity as { cordoned?: boolean }).cordoned} />
          )}
          <pre className="text-xs font-mono overflow-auto text-[color:var(--text-primary)] p-3 rounded-md bg-[color:var(--bg-primary)] border border-[color:var(--border-subtle)]">
{JSON.stringify(entity, null, 2)}
          </pre>
        </div>
      ) : (
        <div className="text-sm text-[color:var(--text-muted)] flex items-center gap-2">
          <X size={14} /> Entity no longer present in topology (probably removed).
        </div>
      )}
    </Drawer>
  );
}

// DpuActions renders the cordon / uncordon toggle for a DPU.
//
// Wire path: browser → dashw → dashd (browser NEVER calls dashd direct).
// dashw proxies /api/v1/* through to dashd /v1/*, so POSTing to
// /api/v1/inventory/{id}/cordon hits the same dashd endpoint operators
// drive from dashctl + the REST docs.
//
// UX:
//   * disabled while inflight
//   * red-toned button when cordoned ("Uncordon")
//   * amber-toned button when uncordoned ("Cordon")
//   * banner shows the last result (success / error) until the next click
//   * does NOT optimistically update the store — we rely on the dashd
//     broadcaster's KIND_DPU_STATE event coming back through dashw and
//     reaching the topology-v2 store organically. If streaming is OFF,
//     the snapshot's 30s auto-refetch catches up. This matches the
//     dashd-is-source-of-truth invariant the rest of the page follows.
function DpuActions({ dpuId, cordoned }: { dpuId: string; cordoned: boolean }) {
  const [inflight, setInflight] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; msg: string } | null>(null);
  const verb: 'cordon' | 'uncordon' = cordoned ? 'uncordon' : 'cordon';
  const onClick = async () => {
    setInflight(true);
    setResult(null);
    try {
      const res = await fetch(`/api/v1/inventory/${encodeURIComponent(dpuId)}/${verb}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ reason: `operator action from /topology-v2` }),
      });
      if (!res.ok) {
        const text = await res.text().catch(() => '');
        setResult({ ok: false, msg: `HTTP ${res.status}${text ? `: ${text.slice(0, 160)}` : ''}` });
        return;
      }
      setResult({
        ok: true,
        msg: cordoned
          ? 'Uncordoned. dashd will resume scheduling onto this DPU; next dpu_state event will reflect the change.'
          : 'Cordoned. dashd will stop scheduling new workloads here; existing ENIs are unaffected.',
      });
    } catch (err) {
      setResult({ ok: false, msg: String((err as Error)?.message ?? err) });
    } finally {
      setInflight(false);
    }
  };

  const tone = cordoned
    ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-300 hover:bg-emerald-500/20'
    : 'border-amber-500/40 bg-amber-500/10 text-amber-300 hover:bg-amber-500/20';
  const Icon = cordoned ? Unlock : Lock;

  return (
    <div className="rounded-md border border-[color:var(--border-subtle)] bg-[color:var(--bg-secondary)] p-3">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="text-xs font-semibold text-[color:var(--text-secondary)] uppercase tracking-wider">DPU lifecycle</div>
          <div className="text-xs text-[color:var(--text-muted)] mt-1">
            Current state: {cordoned
              ? <span className="text-amber-300">cordoned (no new workloads)</span>
              : <span className="text-emerald-300">uncordoned (scheduling enabled)</span>}
          </div>
        </div>
        <button
          onClick={onClick}
          disabled={inflight}
          className={cn(
            'inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md border text-xs font-medium transition-colors disabled:opacity-60 disabled:cursor-wait',
            tone,
          )}
          title={cordoned
            ? `POST /api/v1/inventory/${dpuId}/uncordon`
            : `POST /api/v1/inventory/${dpuId}/cordon`}
        >
          {inflight
            ? <><Loader2 size={12} className="animate-spin" /> Working…</>
            : <><Icon size={12} /> {cordoned ? 'Uncordon DPU' : 'Cordon DPU'}</>}
        </button>
      </div>
      {result && (
        <div className={cn(
          'mt-3 text-xs px-2 py-1.5 rounded border',
          result.ok
            ? 'border-emerald-500/30 bg-emerald-500/5 text-emerald-200'
            : 'border-red-500/30 bg-red-500/5 text-red-200',
        )}>
          {result.msg}
        </div>
      )}
    </div>
  );
}

/* ────────────────────────────────────────────────────────────── */

const STREAM_PREF_KEY = 'topology-v2:streaming';
const ENIS_PREF_KEY = 'topology-v2:include-enis';

function loadBoolPref(key: string, defaultValue: boolean): boolean {
  if (typeof window === 'undefined') return defaultValue;
  try {
    const raw = window.localStorage.getItem(key);
    if (raw === null) return defaultValue;
    return raw === 'true';
  } catch {
    return defaultValue;
  }
}

function saveBoolPref(key: string, value: boolean): void {
  if (typeof window === 'undefined') return;
  try { window.localStorage.setItem(key, value ? 'true' : 'false'); } catch { /* no-op */ }
}

export default function TopologyV2View() {
  // Streaming is OPT-IN. The page does NOT auto-subscribe on mount
  // because the SSE channel sustains traffic the operator may not need
  // while idly navigating. We persist the preference in localStorage so
  // power-users who always want live data don't have to re-enable on
  // every page load.
  const [streaming, setStreaming] = useState(() => loadBoolPref(STREAM_PREF_KEY, false));
  const [includeEnis, setIncludeEnis] = useState(() => loadBoolPref(ENIS_PREF_KEY, false));
  useEffect(() => saveBoolPref(STREAM_PREF_KEY, streaming), [streaming]);
  useEffect(() => saveBoolPref(ENIS_PREF_KEY, includeEnis), [includeEnis]);

  const applySnapshot = useTopologyV2Store((s) => s.applySnapshot);
  const reset = useTopologyV2Store((s) => s.reset);

  // Always-load snapshot. This is a one-shot HTTP GET (deduped by
  // dashw's 1-second snapshot cache) so the page renders the
  // topology immediately on load — user can browse + click nodes
  // without enabling the live stream. When the stream IS on, the
  // stream owns the cache via setQueryData() and this query becomes a
  // no-op (staleTime: Infinity while streaming).
  const snap = useQuery<TopologyV2Response>({
    queryKey: ['topology-v2', includeEnis],
    queryFn: async () => {
      const params = new URLSearchParams();
      if (includeEnis) params.set('include_enis', 'true');
      const res = await fetch(`/api/console/topology-v2?${params.toString()}`);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      return (await res.json()) as TopologyV2Response;
    },
    staleTime: streaming ? Infinity : 30_000,
    refetchOnWindowFocus: !streaming,
  });

  // Push fetched snapshot into the store so all widgets share one source
  // of truth (the store is also what the SSE reducer mutates).
  useEffect(() => {
    if (snap.data) applySnapshot(snap.data, 0);
  }, [snap.data, applySnapshot]);

  // Hook is gated on the user's explicit Start. When `streaming` flips
  // false the hook closes its EventSource; the cached snapshot stays
  // in the store for inspection (see useTopologyStream cleanup note).
  useTopologyStream({ includeEnis, enabled: streaming });

  const hasData = !!useTopologyV2Store((s) => s.topology);

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Topology v2"
        subtitle="Browser ↔ dashw ↔ dashd. Snapshot loads on open; live stream is optional."
        actions={
          <div className="flex items-center gap-3">
            <button
              onClick={() => snap.refetch()}
              disabled={snap.isFetching}
              className="inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-md border border-[color:var(--border-subtle)] text-xs text-[color:var(--text-secondary)] hover:text-[color:var(--text-primary)] hover:border-[color:var(--border-strong)] disabled:opacity-50 transition-colors"
              title="Re-fetch the snapshot from dashw (uses the BFF's 1s cache)"
            >
              <RefreshCw size={12} className={snap.isFetching ? 'animate-spin' : ''} /> Refresh
            </button>
            <label
              className="flex items-center gap-2 text-xs text-[color:var(--text-secondary)] cursor-pointer"
              title="When ON, every snapshot + dpu_state/dpu_added event includes the per-DPU ENI list (name, namespace, MAC, admin state). When OFF, only the ENI count per DPU is sent—smaller payload, faster fan-out."
            >
              <input
                type="checkbox"
                checked={includeEnis}
                onChange={(e) => setIncludeEnis(e.target.checked)}
                className="rounded border-[color:var(--border-subtle)]"
              />
              Include ENIs
            </label>
            <button
              onClick={() => setStreaming((v) => !v)}
              className={cn(
                'inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md border text-xs font-medium transition-colors',
                streaming
                  ? 'border-red-500/40 bg-red-500/10 text-red-300 hover:bg-red-500/20'
                  : 'border-emerald-500/40 bg-emerald-500/10 text-emerald-300 hover:bg-emerald-500/20',
              )}
              title={streaming
                ? 'Stop the SSE stream. The cached snapshot stays visible.'
                : 'Open the SSE stream to dashw for live deltas.'}
            >
              {streaming ? <><Square size={12} /> Stop live</> : <><Play size={12} /> Start live</>}
            </button>
            {hasData && (
              <button
                onClick={reset}
                className="inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-md border border-[color:var(--border-subtle)] text-xs text-[color:var(--text-muted)] hover:text-[color:var(--text-primary)] hover:border-[color:var(--border-strong)] transition-colors"
                title="Clear the cached snapshot + event log"
              >
                <Trash2 size={12} />
              </button>
            )}
            <ConnectionBadge streaming={streaming} />
          </div>
        }
      />

      <InstructionBanner streaming={streaming} />

      {snap.isError && !hasData && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/5 p-4 text-sm text-red-300">
          Failed to load topology snapshot from dashw: {(snap.error as Error)?.message ?? 'unknown error'}.
          <button onClick={() => snap.refetch()} className="ml-3 underline">Retry</button>
        </div>
      )}

      {snap.isLoading && !hasData ? (
        <div className="rounded-lg border border-[color:var(--border-subtle)] bg-[color:var(--bg-secondary)] p-6 text-sm text-[color:var(--text-muted)] flex items-center gap-2">
          <RefreshCw size={14} className="animate-spin" /> Loading topology snapshot…
        </div>
      ) : (
        <>
          <SummaryStrip />
          <div className="grid grid-cols-1 xl:grid-cols-[360px_1fr] gap-3">
            <ClusterPanel />
            <AppliancesGrid />
          </div>
          {streaming && <EventTicker />}
        </>
      )}
      <InspectorDrawer />
    </div>
  );
}

function InstructionBanner({ streaming }: { streaming: boolean }) {
  // Tiny persistent helper text below the header. Tells the user the
  // page is interactive without the live stream + how to enable it.
  const health = useTopologyV2Store(selectStreamHealth);
  return (
    <div className="flex items-start gap-2 rounded-md border border-[color:var(--border-subtle)] bg-[color:var(--bg-secondary)] px-3 py-2 text-xs text-[color:var(--text-secondary)]">
      <Info size={14} className="text-cyan-400 mt-0.5 flex-shrink-0" />
      {streaming ? (
        <span>
          <strong className="text-emerald-300">Live stream is ON.</strong>{' '}
          Snapshot + deltas arrive via SSE from{' '}
          <code className="text-[color:var(--text-primary)]">{health.lastSource || 'dashd (pending first event)'}</code>{' '}
          relayed by{' '}
          <code className="text-[color:var(--text-primary)]">{health.lastVia || 'this dashw'}</code>.
          Click any node, appliance, or DPU to inspect. Use{' '}
          <strong>Stop live</strong> to pause without losing the cached view.
          Each event row shows its <code>source → via</code> path in the
          fourth column.
        </span>
      ) : (
        <span>
          Showing the latest snapshot — the page is fully interactive: click
          any node, appliance, or DPU to inspect. To see real-time changes
          (peer add/remove, DPU state, leader change) click{' '}
          <strong className="text-emerald-300">Start live</strong> at the top right.
          Every live event is stamped by dashw with its <code>source</code>
          (originating dashd) and <code>via</code> (relaying dashw replica)
          so you always know where it came from. Snapshots auto-refresh every
          30s; use <strong>Refresh</strong> for an on-demand fetch.
        </span>
      )}
    </div>
  );
}
