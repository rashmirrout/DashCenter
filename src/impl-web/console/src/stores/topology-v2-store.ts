// Topology-v2 live store. Holds the latest snapshot + applies deltas
// from the SSE/WS stream. Exposed via Zustand selectors so widgets
// re-render only when their slice changes.
//
// Why Zustand (and not React Context):
//   * 5+ widgets per page subscribe to slices — Context would re-render
//     the whole tree on every event.
//   * The hook is the single owner of the EventSource — store mutations
//     are funneled through one writer.
//
// Why not derive everything from TanStack Query:
//   * Query is great for snapshot fetches + invalidation. SSE deltas
//     aren't snapshots; we need a reducer.
//   * We do use TanStack Query for the *initial* GetSnapshot to dedupe
//     concurrent page mounts via the BFF's snapshot cache.

import { create } from 'zustand';
import type {
  ApplianceTop,
  ClusterNode,
  DpuTop,
  TopologyEvent,
  TopologyV2Response,
} from '@/api/topology-v2-types';

export type ConnectionState =
  | 'idle'           // before first connect attempt
  | 'connecting'     // EventSource opened, awaiting snapshot
  | 'open'           // snapshot received, live deltas flowing
  | 'reconnecting'   // dropped, backing off
  | 'paused'         // tab hidden / user-paused
  | 'error';         // unrecoverable

export interface TopologyV2State {
  // Connection lifecycle.
  connection: ConnectionState;
  lastError?: string;
  lastEventAt?: number;          // epoch ms; updated on every event including keepalive
  lastEventId: number;           // monotonic cursor from the broadcaster
  upstreamReconnects: number;    // count synthetic RESYNC events received
  droppedEvents: number;         // total dropped (server-reported)
  suppressedEvents: number;      // total rate-limited (server-reported)

  // Data.
  topology: TopologyV2Response | null;

  // Recent event log (capped for the timeline footer / time travel scrubber).
  eventLog: TopologyEvent[];
  eventLogCapacity: number;

  // Selection (clicked entity in the canvas → drawer).
  selectedKind?: 'node' | 'appliance' | 'dpu' | 'eni';
  selectedId?: string;

  // ── actions ──────────────────────────────────────────────
  setConnection: (s: ConnectionState, err?: string) => void;
  applySnapshot: (snap: TopologyV2Response, eventId: number) => void;
  applyEvent: (ev: TopologyEvent) => void;
  select: (kind: TopologyV2State['selectedKind'], id?: string) => void;
  reset: () => void;
}

export const useTopologyV2Store = create<TopologyV2State>((set, get) => ({
  connection: 'idle',
  lastEventId: 0,
  upstreamReconnects: 0,
  droppedEvents: 0,
  suppressedEvents: 0,
  topology: null,
  eventLog: [],
  eventLogCapacity: 200,

  setConnection: (s, err) => set({ connection: s, lastError: err }),

  applySnapshot: (snap, eventId) => set((st) => ({
    topology: snap,
    lastEventId: Math.max(st.lastEventId, eventId || 0),
    lastEventAt: Date.now(),
  })),

  applyEvent: (ev) => set((st) => {
    const log = [...st.eventLog, ev].slice(-st.eventLogCapacity);
    const next: Partial<TopologyV2State> = {
      eventLog: log,
      lastEventAt: Date.now(),
    };
    if (typeof ev.event_id === 'number' && ev.event_id > st.lastEventId) {
      next.lastEventId = ev.event_id;
    }
    if (!st.topology) {
      return next;
    }

    switch (ev.kind) {
      case 'KIND_SNAPSHOT':
        if (ev.snapshot) {
          next.topology = ev.snapshot;
        }
        return next;

      case 'KIND_PEER_ADDED':
      case 'KIND_PEER_UPDATED':
        if (ev.peer && st.topology.cluster) {
          const cluster = st.topology.cluster;
          const peers = cluster.nodes.filter((n) => n.node_id !== ev.peer!.node_id);
          peers.push(ev.peer);
          peers.sort((a, b) => a.node_id.localeCompare(b.node_id));
          next.topology = { ...st.topology, cluster: { ...cluster, nodes: peers, node_count: peers.length } };
        }
        return next;

      case 'KIND_PEER_REMOVED':
        if (ev.peer && st.topology.cluster) {
          const cluster = st.topology.cluster;
          const peers = cluster.nodes.filter((n) => n.node_id !== ev.peer!.node_id);
          next.topology = { ...st.topology, cluster: { ...cluster, nodes: peers, node_count: peers.length } };
        }
        return next;

      case 'KIND_LEADER_CHANGED':
        if (st.topology.cluster) {
          const cluster = st.topology.cluster;
          next.topology = {
            ...st.topology,
            cluster: {
              ...cluster,
              leader_id: ev.new_leader_id || '',
              nodes: cluster.nodes.map((n) => ({ ...n, is_leader: n.node_id === ev.new_leader_id })),
            },
          };
        }
        return next;

      case 'KIND_DPU_STATE':
      case 'KIND_DPU_ADDED':
      case 'KIND_DPU_REMOVED':
        if (ev.dpu) {
          next.topology = applyDpuMutation(st.topology, ev);
        }
        return next;

      case 'KIND_DROPPED':
        return {
          ...next,
          droppedEvents: st.droppedEvents + (ev.notice?.dropped_count ?? 0),
        };

      case 'KIND_RATE_LIMITED':
        return {
          ...next,
          suppressedEvents: st.suppressedEvents + (ev.notice?.suppressed_count ?? 0),
        };

      case 'KIND_RESYNC':
        return {
          ...next,
          upstreamReconnects: st.upstreamReconnects + 1,
        };

      default:
        return next;
    }
  }),

  select: (kind, id) => set({ selectedKind: kind, selectedId: id }),

  reset: () => set({
    connection: 'idle',
    lastError: undefined,
    lastEventAt: undefined,
    lastEventId: 0,
    upstreamReconnects: 0,
    droppedEvents: 0,
    suppressedEvents: 0,
    topology: null,
    eventLog: [],
    selectedKind: undefined,
    selectedId: undefined,
  }),
}));

// applyDpuMutation is split into a helper so the switch above stays
// readable. Pure function — returns a new TopologyV2Response.
function applyDpuMutation(t: TopologyV2Response, ev: TopologyEvent): TopologyV2Response {
  const dpu = ev.dpu!;
  const appliances = (t.appliances ?? []).map((a) => {
    let dpus: DpuTop[] = a.dpus;
    if (ev.kind === 'KIND_DPU_REMOVED') {
      dpus = dpus.filter((d) => d.id !== dpu.id);
    } else {
      const idx = dpus.findIndex((d) => d.id === dpu.id);
      if (idx >= 0) {
        dpus = [...dpus];
        dpus[idx] = { ...dpus[idx], ...dpu };
      }
    }
    return { ...a, dpus };
  });
  return { ...t, appliances };
}

// Selectors (memoised by Zustand's referential equality where possible).
export const selectConnection = (s: TopologyV2State) => s.connection;
export const selectTopology = (s: TopologyV2State) => s.topology;
export const selectCluster = (s: TopologyV2State) => s.topology?.cluster;
export const selectAppliances = (s: TopologyV2State) => s.topology?.appliances ?? [];
export const selectSummary = (s: TopologyV2State) => s.topology?.summary;
export const selectEventLog = (s: TopologyV2State) => s.eventLog;
export const selectSelected = (s: TopologyV2State) => ({ kind: s.selectedKind, id: s.selectedId });
export const selectStreamHealth = (s: TopologyV2State) => ({
  connection: s.connection,
  lastEventAt: s.lastEventAt,
  upstreamReconnects: s.upstreamReconnects,
  droppedEvents: s.droppedEvents,
  suppressedEvents: s.suppressedEvents,
  lastError: s.lastError,
});

// findEntity finds a peer / appliance / dpu / eni by id for the
// drawer's inspector. Returns the typed entry or undefined.
export function findEntity(s: TopologyV2State, kind: TopologyV2State['selectedKind'], id?: string) {
  if (!id || !s.topology) return undefined;
  switch (kind) {
    case 'node':
      return s.topology.cluster?.nodes.find((n) => n.node_id === id) as ClusterNode | undefined;
    case 'appliance':
      return (s.topology.appliances ?? []).find((a) => a.id === id) as ApplianceTop | undefined;
    case 'dpu':
      for (const a of s.topology.appliances ?? []) {
        const d = a.dpus.find((d) => d.id === id);
        if (d) return d as DpuTop;
      }
      return undefined;
    case 'eni':
      for (const a of s.topology.appliances ?? []) {
        for (const d of a.dpus) {
          const e = (d.enis ?? []).find((e) => e.name === id);
          if (e) return e;
        }
      }
      return undefined;
  }
  return undefined;
}
