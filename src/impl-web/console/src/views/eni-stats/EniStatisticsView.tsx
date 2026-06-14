// EniStatisticsView — Diagnostics subpage for per-ENI counter
// statistics (PE-3c add-on).
//
// Layout:
//   ┌──────────────────────────────┬──────────────────────────────┐
//   │  DPU tree (left, 40%)        │  Counter panel (right, 60%)  │
//   │   ▾ dpu-sim-01               │   ENI: eni-bank-web-01       │
//   │       eni-bank-web-01  ◀──   │   namespace=default          │
//   │       eni-bank-web-02        │   vnet=bank-prod-web         │
//   │   ▸ dpu-sim-02               │   mac=00:11:22:33:44:55      │
//   │   ▸ dpu-sim-03               │   ────────                   │
//   │                              │   vxlan_decap = 25,360       │
//   │                              │   vxlan_encap =  4,755       │
//   │                              │   drop_acl_in =     0        │
//   │                              │   (per-ENI sub-rollup)       │
//   └──────────────────────────────┴──────────────────────────────┘
//
// Streaming model:
//   * "Pull" button → one-shot GET /api/v1/observability/counters/
//     {dpu_id}/details (proxied through dashw → dashd REST).
//   * "Start streaming" → react-query refetchInterval = 5s while the
//     toggle is ON; per-DPU SSE stream (useCounterStream) drives the
//     rollup tile via useCountersStore.
//   * The page is the only place per-ENI sub-rollups are surfaced
//     in the SPA today. The streaming surface is per-DPU only;
//     per-ENI live deltas are filed as Future Scope 10.7.

import { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  ChevronDown,
  ChevronRight,
  RefreshCw,
  Activity,
  Pause,
  Plug,
  Server,
  Trash2,
} from 'lucide-react';
import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { useCounterStream } from '@/queries/useCounterStream';
import { useCountersStore } from '@/stores/counters-store';
import { clearCounterTarget, clearAllCounters } from '@/api/observability';
import type { TopologyV2Response } from '@/api/topology-v2-types';
import type { DpuTop, EniTop } from '@/api/topology-v2-types';

/* ── types matching dashd CounterDetails envelope ───────────── */

interface CounterReportLite {
  dpu_id?: string;
  sampled_at?: string;
  vxlan_decap?: string;
  vxlan_encap?: string;
  drop_acl_in?: string;
  drop_acl_out?: string;
  flows_created_total?: string;
  flow_table_size?: string;
}

interface CounterDetails {
  dpu_id: string;
  update_at?: string;
  report?: CounterReportLite;
  per_eni?: Record<string, CounterReportLite>;
  per_vnet?: Record<string, CounterReportLite>;
}

const STREAM_PREF_KEY = 'eni-stats:streaming';
const REFETCH_INTERVAL_MS = 5_000;

function loadBoolPref(key: string, fallback: boolean): boolean {
  try {
    const v = window.localStorage.getItem(key);
    return v === null ? fallback : v === '1';
  } catch {
    return fallback;
  }
}

function saveBoolPref(key: string, v: boolean): void {
  try {
    window.localStorage.setItem(key, v ? '1' : '0');
  } catch {
    /* private mode etc — best-effort */
  }
}

function fmt(value: string | undefined): string {
  if (!value) return '—';
  const n = Number(value);
  if (Number.isNaN(n)) return value;
  return n.toLocaleString();
}

/* ── data hooks ─────────────────────────────────────────────── */

function useTopologySnapshot(enabled = true) {
  return useQuery<TopologyV2Response>({
    enabled,
    queryKey: ['eni-stats', 'topology-v2', true],
    queryFn: async () => {
      const res = await fetch('/api/console/topology-v2?include_enis=true');
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      return (await res.json()) as TopologyV2Response;
    },
    staleTime: 30_000,
  });
}

function useCounterDetails(dpuId: string | null, streaming: boolean) {
  return useQuery<CounterDetails>({
    enabled: !!dpuId,
    queryKey: ['counter-details', dpuId],
    queryFn: async () => {
      if (!dpuId) throw new Error('dpuId required');
      const res = await fetch(
        `/api/v1/observability/counters/${encodeURIComponent(dpuId)}/details`,
      );
      if (res.status === 404) {
        return { dpu_id: dpuId } as CounterDetails;
      }
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      return (await res.json()) as CounterDetails;
    },
    refetchInterval: streaming ? REFETCH_INTERVAL_MS : false,
    refetchIntervalInBackground: false,
    staleTime: streaming ? 0 : 60_000,
  });
}

/* ── helpers ────────────────────────────────────────────────── */

function flattenDpus(snap: TopologyV2Response | undefined): DpuTop[] {
  if (!snap?.appliances) return [];
  const dpus: DpuTop[] = [];
  for (const app of snap.appliances) {
    for (const dpu of app.dpus ?? []) {
      dpus.push(dpu);
    }
  }
  return dpus.sort((a, b) => a.id.localeCompare(b.id));
}

/* ── components ─────────────────────────────────────────────── */

interface DpuRowProps {
  dpu: DpuTop;
  expanded: boolean;
  selectedEni: { dpuId: string; eniName: string } | null;
  onToggle: () => void;
  onSelectEni: (eniName: string) => void;
}

function DpuRow({ dpu, expanded, selectedEni, onToggle, onSelectEni }: DpuRowProps) {
  const enis = dpu.enis ?? [];
  return (
    <div className="border-b border-[color:var(--border-subtle)] last:border-b-0">
      <button
        type="button"
        onClick={onToggle}
        className="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-[color:var(--bg-secondary)]"
        data-testid={`dpu-row-${dpu.id}`}
      >
        {expanded ? (
          <ChevronDown className="h-4 w-4 text-[color:var(--text-muted)]" />
        ) : (
          <ChevronRight className="h-4 w-4 text-[color:var(--text-muted)]" />
        )}
        <Server className="h-4 w-4 text-[color:var(--text-secondary)]" />
        <span className="font-mono text-sm text-[color:var(--text-primary)] flex-1 truncate">
          {dpu.id}
        </span>
        <span className="text-xs text-[color:var(--text-muted)] tabular-nums">
          {enis.length} ENI{enis.length === 1 ? '' : 's'}
        </span>
      </button>
      {expanded && (
        <ul className="bg-[color:var(--bg-primary)] py-1">
          {enis.length === 0 && (
            <li className="px-9 py-1.5 text-xs italic text-[color:var(--text-muted)]">
              no ENIs landed on this DPU
            </li>
          )}
          {enis.map((eni) => {
            const isSelected =
              selectedEni?.dpuId === dpu.id && selectedEni?.eniName === eni.name;
            return (
              <li key={eni.name}>
                <button
                  type="button"
                  onClick={() => onSelectEni(eni.name)}
                  className={
                    'w-full flex items-center gap-2 pl-9 pr-3 py-1.5 text-left text-xs hover:bg-[color:var(--bg-secondary)] ' +
                    (isSelected ? 'bg-[color:var(--bg-secondary)] font-semibold' : '')
                  }
                  data-testid={`eni-row-${dpu.id}-${eni.name}`}
                >
                  <Plug className="h-3 w-3 text-[color:var(--text-muted)]" />
                  <span className="font-mono text-[color:var(--text-primary)] flex-1 truncate">
                    {eni.name}
                  </span>
                  {eni.vnet_name && (
                    <span className="text-[10px] text-[color:var(--text-muted)] truncate max-w-[12em]">
                      {eni.vnet_name}
                    </span>
                  )}
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

interface EniPanelProps {
  dpuId: string;
  eni: EniTop;
  details: CounterDetails | undefined;
  loading: boolean;
  error: Error | null;
}

function EniPanel({ dpuId, eni, details, loading, error }: EniPanelProps) {
  const sub = details?.per_eni?.[eni.name];
  const dpuRollup = details?.report;

  return (
    <div className="flex flex-col gap-3">
      <GlassCard>
        <div className="flex items-center gap-2 mb-2">
          <Plug className="h-4 w-4 text-[color:var(--accent-primary)]" />
          <h2 className="text-lg font-semibold text-[color:var(--text-primary)] truncate">
            {eni.name}
          </h2>
        </div>
        <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm">
          <dt className="text-[color:var(--text-muted)]">DPU</dt>
          <dd className="font-mono text-[color:var(--text-primary)] truncate">{dpuId}</dd>
          <dt className="text-[color:var(--text-muted)]">Namespace</dt>
          <dd className="font-mono text-[color:var(--text-primary)] truncate">{eni.namespace ?? 'default'}</dd>
          {eni.vnet_name && (
            <>
              <dt className="text-[color:var(--text-muted)]">VNET</dt>
              <dd className="font-mono text-[color:var(--text-primary)] truncate">{eni.vnet_name}</dd>
            </>
          )}
          {eni.mac_address && (
            <>
              <dt className="text-[color:var(--text-muted)]">MAC</dt>
              <dd className="font-mono text-[color:var(--text-primary)] truncate">{eni.mac_address}</dd>
            </>
          )}
          {eni.admin_state && (
            <>
              <dt className="text-[color:var(--text-muted)]">Admin state</dt>
              <dd className="font-mono text-[color:var(--text-primary)] truncate">{eni.admin_state}</dd>
            </>
          )}
        </dl>
      </GlassCard>

      <GlassCard>
        <h3 className="text-xs font-semibold uppercase tracking-wide text-[color:var(--text-muted)] mb-2">
          Per-ENI counter sub-rollup
        </h3>
        {error && (
          <div className="text-sm text-[color:var(--status-error)]" data-testid="counter-panel-error">
            Error: {error.message}
          </div>
        )}
        {!error && loading && !details && (
          <div className="text-sm text-[color:var(--text-muted)]">Loading…</div>
        )}
        {!error && !loading && !sub && (
          <div className="text-sm text-[color:var(--text-muted)]" data-testid="counter-panel-empty">
            No per-ENI counters cached for <code>{eni.name}</code>. Hit <b>Pull</b> or wait for the next poll round (default 5s).
          </div>
        )}
        {sub && (
          <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-1 text-sm font-mono">
            <dt className="text-[color:var(--text-muted)]">vxlan_decap</dt>
            <dd className="text-[color:var(--text-primary)] text-right">{fmt(sub.vxlan_decap)}</dd>
            <dt className="text-[color:var(--text-muted)]">vxlan_encap</dt>
            <dd className="text-[color:var(--text-primary)] text-right">{fmt(sub.vxlan_encap)}</dd>
            <dt className="text-[color:var(--text-muted)]">drop_acl_in</dt>
            <dd className="text-[color:var(--text-primary)] text-right">{fmt(sub.drop_acl_in)}</dd>
            <dt className="text-[color:var(--text-muted)]">drop_acl_out</dt>
            <dd className="text-[color:var(--text-primary)] text-right">{fmt(sub.drop_acl_out)}</dd>
            <dt className="text-[color:var(--text-muted)]">flows_created_total</dt>
            <dd className="text-[color:var(--text-primary)] text-right">{fmt(sub.flows_created_total)}</dd>
            <dt className="text-[color:var(--text-muted)]">flow_table_size</dt>
            <dd className="text-[color:var(--text-primary)] text-right">{fmt(sub.flow_table_size)}</dd>
          </dl>
        )}
        {details?.update_at && (
          <div className="text-[10px] text-[color:var(--text-muted)] mt-2" data-testid="counter-panel-update-at">
            Last updated: {details.update_at}
          </div>
        )}
      </GlassCard>

      {dpuRollup && (
        <GlassCard>
          <h3 className="text-xs font-semibold uppercase tracking-wide text-[color:var(--text-muted)] mb-2">
            Parent DPU rollup ({dpuId})
          </h3>
          <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-1 text-sm font-mono">
            <dt className="text-[color:var(--text-muted)]">vxlan_decap</dt>
            <dd className="text-[color:var(--text-primary)] text-right">{fmt(dpuRollup.vxlan_decap)}</dd>
            <dt className="text-[color:var(--text-muted)]">vxlan_encap</dt>
            <dd className="text-[color:var(--text-primary)] text-right">{fmt(dpuRollup.vxlan_encap)}</dd>
            <dt className="text-[color:var(--text-muted)]">drop_acl_in</dt>
            <dd className="text-[color:var(--text-primary)] text-right">{fmt(dpuRollup.drop_acl_in)}</dd>
            <dt className="text-[color:var(--text-muted)]">flow_table_size</dt>
            <dd className="text-[color:var(--text-primary)] text-right">{fmt(dpuRollup.flow_table_size)}</dd>
          </dl>
        </GlassCard>
      )}
    </div>
  );
}

/* ── main view ──────────────────────────────────────────────── */

export default function EniStatisticsView() {
  // Per-page streaming toggle, persisted in localStorage. Independent
  // of the topology-v2 page's Start/Stop so operators can leave
  // topology streaming off while flipping this page on.
  const [streaming, setStreaming] = useState(() => loadBoolPref(STREAM_PREF_KEY, false));
  useEffect(() => saveBoolPref(STREAM_PREF_KEY, streaming), [streaming]);

  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [selectedEni, setSelectedEni] = useState<{ dpuId: string; eniName: string } | null>(null);
  const [clearing, setClearing] = useState(false);
  const [clearMsg, setClearMsg] = useState<string | null>(null);

  // Topology snapshot — always pull once on mount (and refresh on
  // window focus); used for the DPU list + ENI children.
  const topology = useTopologySnapshot();
  const dpus = useMemo(() => flattenDpus(topology.data), [topology.data]);

  // Counter details for the DPU containing the selected ENI.
  const detailsQuery = useCounterDetails(selectedEni?.dpuId ?? null, streaming);

  // When streaming is on, also subscribe to the per-DPU SSE for the
  // selected DPU so any global Hub-level reconnects update the
  // shared counters-store (consistency with the topology-v2 widget).
  useCounterStream({
    enabled: streaming && !!selectedEni,
    dpuIds: selectedEni ? [selectedEni.dpuId] : [],
  });

  function toggleDpu(id: string) {
    setExpanded((m) => ({ ...m, [id]: !m[id] }));
  }

  function selectEni(dpuId: string, eniName: string) {
    setSelectedEni({ dpuId, eniName });
    // Auto-expand for visual continuity.
    setExpanded((m) => ({ ...m, [dpuId]: true }));
  }

  // Clear counter cache on dashd for the selected DPU AND wipe the
  // shared local sparkline ring. Local wipe runs first so the panel
  // visibly resets even if the network round-trip is slow.
  async function handleClearSelected() {
    if (!selectedEni || clearing) return;
    setClearing(true);
    setClearMsg(null);
    useCountersStore.getState().clearDpu(selectedEni.dpuId);
    try {
      const r = await clearCounterTarget(selectedEni.dpuId);
      setClearMsg(
        r.cleared
          ? `cleared ${selectedEni.dpuId} on dashd; repopulates at next poll`
          : `no cached entry for ${selectedEni.dpuId} (already clear); repopulates at next poll`,
      );
      detailsQuery.refetch();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      setClearMsg(`local cleared; server error: ${msg}`);
    } finally {
      setClearing(false);
      window.setTimeout(() => setClearMsg(null), 6_000);
    }
  }

  // Bulk: wipe every cached counter entry on dashd.
  async function handleClearAll() {
    if (clearing) return;
    if (!window.confirm('Clear cached counters for every DPU on dashd? Entries refill at the next poll round (~5s).')) {
      return;
    }
    setClearing(true);
    setClearMsg(null);
    // Local: drop every DPU from the shared ring.
    const st = useCountersStore.getState();
    for (const id of Object.keys(st.byDpu)) {
      st.clearDpu(id);
    }
    try {
      const r = await clearAllCounters();
      setClearMsg(`cleared ${r.cleared} cached counter entr${r.cleared === 1 ? 'y' : 'ies'} on dashd; refill at next poll`);
      detailsQuery.refetch();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      setClearMsg(`local cleared; server error: ${msg}`);
    } finally {
      setClearing(false);
      window.setTimeout(() => setClearMsg(null), 6_000);
    }
  }

  const selectedEniObj: EniTop | null = useMemo(() => {
    if (!selectedEni) return null;
    const dpu = dpus.find((d) => d.id === selectedEni.dpuId);
    if (!dpu?.enis) return null;
    return dpu.enis.find((e) => e.name === selectedEni.eniName) ?? null;
  }, [dpus, selectedEni]);

  const totalEnis = useMemo(
    () => dpus.reduce((n, d) => n + (d.enis?.length ?? 0), 0),
    [dpus],
  );

  return (
    <div className="flex flex-col gap-4 h-full">
      <PageHeader
        title="ENI Statistics"
        subtitle={
          <>
            Per-ENI counter sub-rollups from{' '}
            <code className="text-xs">GET /v1/observability/counters/&#123;dpu_id&#125;/details</code>.{' '}
            Streaming surface is per-DPU only; this page polls the snapshot every {REFETCH_INTERVAL_MS / 1000}s while streaming is on.
          </>
        }
        actions={
          <div className="flex items-center gap-2">
            <span
              className={
                'inline-flex items-center gap-1 px-2 py-1 rounded text-xs font-medium ' +
                (streaming
                  ? 'bg-[color:var(--status-success)]/10 text-[color:var(--status-success)]'
                  : 'bg-[color:var(--bg-secondary)] text-[color:var(--text-muted)]')
              }
              data-testid="streaming-badge"
            >
              {streaming ? <Activity className="h-3 w-3" /> : <Pause className="h-3 w-3" />}
              {streaming ? 'Live streaming' : 'Off'}
            </span>
            <button
              type="button"
              onClick={() => setStreaming((s) => !s)}
              className="px-3 py-1.5 text-sm font-medium rounded border border-[color:var(--border-subtle)] hover:bg-[color:var(--bg-secondary)]"
              data-testid="streaming-toggle"
            >
              {streaming ? 'Stop streaming' : 'Start streaming'}
            </button>
            <button
              type="button"
              onClick={() => detailsQuery.refetch()}
              disabled={!selectedEni}
              className="px-3 py-1.5 text-sm font-medium rounded border border-[color:var(--border-subtle)] hover:bg-[color:var(--bg-secondary)] disabled:opacity-50 disabled:cursor-not-allowed inline-flex items-center gap-1"
              data-testid="pull-button"
              title={
                selectedEni
                  ? 'Pull a fresh /details snapshot for the selected DPU'
                  : 'Select an ENI first'
              }
            >
              <RefreshCw className="h-3.5 w-3.5" />
              Pull
            </button>
            <button
              type="button"
              onClick={handleClearSelected}
              disabled={!selectedEni || clearing}
              className="px-3 py-1.5 text-sm font-medium rounded border border-[color:var(--border-subtle)] hover:bg-[color:var(--bg-secondary)] disabled:opacity-50 disabled:cursor-not-allowed inline-flex items-center gap-1"
              data-testid="clear-selected-button"
              title={
                selectedEni
                  ? 'DELETE the cached counter entry for this DPU on dashd; wipe the local sparkline ring. Refills at next poll (~5s).'
                  : 'Select an ENI first'
              }
            >
              <Trash2 className="h-3.5 w-3.5" />
              {clearing ? 'Clearing…' : 'Clear DPU'}
            </button>
            <button
              type="button"
              onClick={handleClearAll}
              disabled={clearing}
              className="px-3 py-1.5 text-sm font-medium rounded border border-[color:var(--border-subtle)] hover:bg-[color:var(--bg-secondary)] disabled:opacity-50 disabled:cursor-not-allowed inline-flex items-center gap-1"
              data-testid="clear-all-button"
              title="DELETE every cached counter entry on dashd (asks first). Refills at next poll."
            >
              <Trash2 className="h-3.5 w-3.5" />
              Clear all
            </button>
          </div>
        }
      />

      {clearMsg && (
        <div
          className="text-xs text-[color:var(--text-muted)] px-3 py-2 rounded bg-[color:var(--bg-secondary)] border border-[color:var(--border-subtle)]"
          data-testid="clear-msg"
        >
          {clearMsg}
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-[2fr_3fr] gap-4 min-h-0 flex-1">
        {/* ── DPU/ENI tree ───────────────────────────────── */}
        <GlassCard className="p-0 overflow-hidden">
          <div className="px-3 py-2 border-b border-[color:var(--border-subtle)] flex items-center justify-between">
            <h2 className="text-xs font-semibold uppercase tracking-wide text-[color:var(--text-muted)]">
              Fleet ({dpus.length} DPU{dpus.length === 1 ? '' : 's'}, {totalEnis} ENI{totalEnis === 1 ? '' : 's'})
            </h2>
          </div>
          {topology.isLoading && (
            <div className="p-3 text-sm text-[color:var(--text-muted)]">Loading topology…</div>
          )}
          {topology.isError && (
            <div className="p-3 text-sm text-[color:var(--status-error)]" data-testid="topology-error">
              Failed to load topology: {(topology.error as Error)?.message ?? 'unknown'}
            </div>
          )}
          {!topology.isLoading && !topology.isError && dpus.length === 0 && (
            <div className="p-3 text-sm text-[color:var(--text-muted)]" data-testid="topology-empty">
              No DPUs in inventory yet.
            </div>
          )}
          <div className="overflow-y-auto" style={{ maxHeight: '70vh' }}>
            {dpus.map((dpu) => (
              <DpuRow
                key={dpu.id}
                dpu={dpu}
                expanded={!!expanded[dpu.id]}
                selectedEni={selectedEni}
                onToggle={() => toggleDpu(dpu.id)}
                onSelectEni={(eniName) => selectEni(dpu.id, eniName)}
              />
            ))}
          </div>
        </GlassCard>

        {/* ── Counter panel ──────────────────────────────── */}
        <div>
          {!selectedEni && (
            <GlassCard>
              <div className="text-sm text-[color:var(--text-muted)]" data-testid="no-eni-selected">
                <Activity className="h-5 w-5 mb-2 text-[color:var(--text-muted)]" />
                Select an ENI from the tree on the left. Hit <b>Pull</b> for a one-shot
                snapshot of its per-ENI counter sub-rollup, or click <b>Start streaming</b>
                {' '}to auto-refresh every {REFETCH_INTERVAL_MS / 1000}s.
              </div>
            </GlassCard>
          )}
          {selectedEni && selectedEniObj && (
            <EniPanel
              dpuId={selectedEni.dpuId}
              eni={selectedEniObj}
              details={detailsQuery.data}
              loading={detailsQuery.isLoading || detailsQuery.isFetching}
              error={(detailsQuery.error as Error | null) ?? null}
            />
          )}
          {selectedEni && !selectedEniObj && (
            <GlassCard>
              <div className="text-sm text-[color:var(--status-warning)]" data-testid="eni-vanished">
                The selected ENI ({selectedEni.eniName}) is no longer in the topology snapshot.
              </div>
            </GlassCard>
          )}
        </div>
      </div>
    </div>
  );
}
