// PE-3c / PD-G5: counter streaming store.
//
// Holds a per-DPU ring buffer of the last 120 samples (~60s at 500ms
// poll cadence) so CounterWidget can render sparklines. The hook
// useCounterStream is the singleton owner of the EventSource; widgets
// read via this store's selectors.
//
// Pattern mirrors topology-v2-store.ts exactly (§0.2.1 pattern
// reconnaissance).

import { create } from 'zustand';

export type ConnectionState =
  | 'idle'
  | 'connecting'
  | 'open'
  | 'reconnecting'
  | 'paused'
  | 'error';

/** Mirrors dashcenter.v1.CounterReport (protojson encodes int64 as
 *  string, so all counter fields are strings on the wire). */
export interface CounterReport {
  dpu_id: string;
  sampled_at?: string;
  vxlan_decap?: string;
  vxlan_encap?: string;
  drop_acl_in?: string;
  drop_acl_out?: string;
  drop_other?: string;
  flows_created_total?: string;
  flow_table_size?: string;
}

/** Mirrors dashcenter.v1.Notice. */
export interface CounterNotice {
  dropped_count?: string;
  suppressed_count?: string;
  message?: string;
  current_event_id?: string;
}

/** Mirrors dashcenter.v1.CounterEvent. */
export interface CounterEvent {
  kind: string;
  ts?: string;
  event_id?: string;
  report?: CounterReport;
  notice?: CounterNotice;
  /** PE-G7.1 stamps added by dashw's Hub. */
  source?: string;
  via?: string;
}

/** A single sample point in the per-DPU ring buffer. */
export interface CounterSample {
  /** Wall-clock arrival time on the browser side (avoids server-clock
   *  skew on sparkline x-axis). */
  at: number;
  /** Parsed numeric counter values. */
  vxlan_decap: number;
  vxlan_encap: number;
  drop_acl_in: number;
  flows_created_total: number;
  flow_table_size: number;
}

/** Per-DPU buffer of samples. */
export interface DpuCounterSeries {
  dpuId: string;
  samples: CounterSample[]; // ring; oldest first
  latest?: CounterReport;
}

export interface CountersState {
  connection: ConnectionState;
  lastError?: string;
  lastEventAt?: number;
  lastEventId: number;
  droppedEvents: number;
  suppressedEvents: number;
  reconnects: number;

  /** Provenance stamps from PE-G7.1. */
  lastSource?: string;
  lastVia?: string;

  /** Map: dpu_id → series. */
  byDpu: Record<string, DpuCounterSeries>;
  /** Ring buffer cap per DPU (default 120 samples ~60s @ 500ms). */
  capacity: number;

  // ── actions ────────────────────────────────────────────────
  setConnection: (s: ConnectionState, err?: string) => void;
  /** Idempotent ingest of a CounterEvent frame. */
  apply: (ev: CounterEvent) => void;
  /** Test-only: replace state with a fresh empty store. */
  reset: () => void;
  /**
   * Drop the local sample ring for one DPU. The browser-side
   * "clear sparklines" affordance — wipes the 60-sample history so
   * the widget re-bootstraps from the next inbound frame. Does NOT
   * touch dashd's server-side cache; for that, operators use
   * dashctl `counters clear` or DELETE /v1/observability/counters.
   */
  clearDpu: (dpuId: string) => void;
}

export const useCountersStore = create<CountersState>((set) => ({
  connection: 'idle',
  lastEventId: 0,
  droppedEvents: 0,
  suppressedEvents: 0,
  reconnects: 0,
  byDpu: {},
  capacity: 120,

  setConnection: (s, err) => set({ connection: s, lastError: err }),

  apply: (ev) => set((st) => {
    const next: Partial<CountersState> = { lastEventAt: Date.now() };
    if (ev.event_id) {
      const id = parseIntSafe(ev.event_id);
      if (id > st.lastEventId) next.lastEventId = id;
    }
    if (ev.source) next.lastSource = ev.source;
    if (ev.via) next.lastVia = ev.via;

    switch (ev.kind) {
      case 'KIND_DROPPED':
        next.droppedEvents = st.droppedEvents + parseIntSafe(ev.notice?.dropped_count);
        break;
      case 'KIND_RATE_LIMITED':
        next.suppressedEvents = st.suppressedEvents + parseIntSafe(ev.notice?.suppressed_count);
        break;
      case 'KIND_RESYNC':
        next.reconnects = st.reconnects + 1;
        // Don't blow away byDpu — a snapshot will follow and overwrite.
        break;
      case 'KIND_KEEPALIVE':
        // No-op beyond lastEventAt + provenance.
        break;
      case 'KIND_SNAPSHOT':
      case 'KIND_REPORT': {
        const rep = ev.report;
        if (!rep || !rep.dpu_id) break;
        const sample: CounterSample = {
          at: Date.now(),
          vxlan_decap: parseIntSafe(rep.vxlan_decap),
          vxlan_encap: parseIntSafe(rep.vxlan_encap),
          drop_acl_in: parseIntSafe(rep.drop_acl_in),
          flows_created_total: parseIntSafe(rep.flows_created_total),
          flow_table_size: parseIntSafe(rep.flow_table_size),
        };
        const series = st.byDpu[rep.dpu_id] ?? { dpuId: rep.dpu_id, samples: [] };
        const samples = [...series.samples, sample].slice(-st.capacity);
        next.byDpu = {
          ...st.byDpu,
          [rep.dpu_id]: { dpuId: rep.dpu_id, samples, latest: rep },
        };
        break;
      }
    }
    return next;
  }),

  reset: () => set({
    connection: 'idle',
    lastError: undefined,
    lastEventAt: undefined,
    lastEventId: 0,
    droppedEvents: 0,
    suppressedEvents: 0,
    reconnects: 0,
    lastSource: undefined,
    lastVia: undefined,
    byDpu: {},
  }),

  clearDpu: (dpuId) => set((st) => {
    if (!dpuId || !st.byDpu[dpuId]) return {};
    const next = { ...st.byDpu };
    delete next[dpuId];
    return { byDpu: next };
  }),
}));

/** Selector: get the series for a specific DPU. */
export const selectSeries = (dpuId: string) => (st: CountersState): DpuCounterSeries | undefined =>
  st.byDpu[dpuId];

/** Selector: get a summary across all DPUs. */
export interface CountersSummary {
  dpuCount: number;
  totalDecap: number;
  totalEncap: number;
  totalDrops: number;
  totalFlows: number;
}
export const selectSummary = (st: CountersState): CountersSummary => {
  let dpuCount = 0;
  let totalDecap = 0;
  let totalEncap = 0;
  let totalDrops = 0;
  let totalFlows = 0;
  for (const id in st.byDpu) {
    const s = st.byDpu[id];
    if (!s) continue;
    dpuCount++;
    const last = s.samples[s.samples.length - 1];
    if (last) {
      totalDecap += last.vxlan_decap;
      totalEncap += last.vxlan_encap;
      totalDrops += last.drop_acl_in;
      totalFlows += last.flows_created_total;
    }
  }
  return { dpuCount, totalDecap, totalEncap, totalDrops, totalFlows };
};

function parseIntSafe(v: string | undefined | null): number {
  if (v == null || v === '') return 0;
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}
