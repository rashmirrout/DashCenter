// Unit tests for the topology-v2 live store reducer.
//
// Covers each TopologyEvent kind the SSE/WS hook can apply, including
// the synthetic notice kinds (KIND_DROPPED, KIND_RATE_LIMITED,
// KIND_RESYNC) emitted by the dashw multiplexer and the dashd
// broadcaster. The store is the single source of truth for the
// /topology-v2 page so reducer correctness is critical — these tests
// pin the expected fan-out for every kind.

import { describe, it, expect, beforeEach } from 'vitest';
import {
  useTopologyV2Store,
  findEntity,
  type TopologyV2State,
} from '@/stores/topology-v2-store';
import type {
  TopologyEvent,
  TopologyV2Response,
} from '@/api/topology-v2-types';

function snap(): TopologyV2Response {
  return {
    computed_at: '2025-01-01T00:00:00Z',
    cluster: {
      healthy: true,
      leader_id: 'n1',
      node_count: 2,
      nodes: [
        { node_id: 'n1', is_leader: true, rest_addr: '10.0.0.1:8080' },
        { node_id: 'n2', is_leader: false, rest_addr: '10.0.0.2:8080' },
      ],
    },
    appliances: [
      {
        id: 'app-a',
        zone: 'z1',
        tier: 't0',
        dpus: [
          {
            id: 'dpu-1', slot: 0, state: 'DPU_STATE_UP', eni_count: 1,
            enis: [{ name: 'eni-1', namespace: 'ns1', mac_address: 'aa' }],
          },
          { id: 'dpu-2', slot: 1, state: 'DPU_STATE_UP', eni_count: 0 },
        ],
      },
    ],
    summary: {
      total_nodes: 2, total_appliances: 1, total_dpus: 2, total_enis: 1,
      healthy_dpus: 2, degraded_dpus: 0, offline_dpus: 0, cordoned_dpus: 0,
    },
  };
}

function state(): TopologyV2State {
  return useTopologyV2Store.getState();
}

describe('topology-v2 store · lifecycle', () => {
  beforeEach(() => state().reset());

  it('starts idle with zero counters', () => {
    const s = state();
    expect(s.connection).toBe('idle');
    expect(s.lastEventId).toBe(0);
    expect(s.droppedEvents).toBe(0);
    expect(s.suppressedEvents).toBe(0);
    expect(s.upstreamReconnects).toBe(0);
    expect(s.topology).toBeNull();
    expect(s.eventLog).toEqual([]);
  });

  it('setConnection updates state + records error', () => {
    state().setConnection('error', 'boom');
    expect(state().connection).toBe('error');
    expect(state().lastError).toBe('boom');
    state().setConnection('open');
    expect(state().connection).toBe('open');
  });

  it('applySnapshot bumps event id monotonically', () => {
    state().applySnapshot(snap(), 42);
    expect(state().lastEventId).toBe(42);
    state().applySnapshot(snap(), 7);          // older id — ignored
    expect(state().lastEventId).toBe(42);
    state().applySnapshot(snap(), 100);
    expect(state().lastEventId).toBe(100);
  });
});

describe('topology-v2 store · applyEvent · cluster deltas', () => {
  beforeEach(() => {
    state().reset();
    state().applySnapshot(snap(), 1);
  });

  it('KIND_PEER_ADDED appends + sorts + bumps node_count', () => {
    const ev: TopologyEvent = {
      kind: 'KIND_PEER_ADDED',
      event_id: 2,
      peer: { node_id: 'n3', rest_addr: '10.0.0.3:8080' },
    };
    state().applyEvent(ev);
    const c = state().topology!.cluster!;
    expect(c.node_count).toBe(3);
    expect(c.nodes.map((n) => n.node_id)).toEqual(['n1', 'n2', 'n3']);
    expect(state().lastEventId).toBe(2);
  });

  it('KIND_PEER_UPDATED replaces the matching node in-place', () => {
    state().applyEvent({
      kind: 'KIND_PEER_UPDATED',
      event_id: 3,
      peer: { node_id: 'n2', rest_addr: '10.0.0.99:8080', version: 'v2' },
    });
    const n2 = state().topology!.cluster!.nodes.find((n) => n.node_id === 'n2')!;
    expect(n2.rest_addr).toBe('10.0.0.99:8080');
    expect(n2.version).toBe('v2');
    expect(state().topology!.cluster!.node_count).toBe(2);
  });

  it('KIND_PEER_REMOVED drops the node + decrements node_count', () => {
    state().applyEvent({
      kind: 'KIND_PEER_REMOVED',
      event_id: 4,
      peer: { node_id: 'n2' },
    });
    const c = state().topology!.cluster!;
    expect(c.node_count).toBe(1);
    expect(c.nodes.map((n) => n.node_id)).toEqual(['n1']);
  });

  it('KIND_LEADER_CHANGED swaps is_leader flag on all nodes', () => {
    state().applyEvent({
      kind: 'KIND_LEADER_CHANGED',
      event_id: 5,
      old_leader_id: 'n1',
      new_leader_id: 'n2',
    });
    const nodes = state().topology!.cluster!.nodes;
    expect(nodes.find((n) => n.node_id === 'n1')!.is_leader).toBe(false);
    expect(nodes.find((n) => n.node_id === 'n2')!.is_leader).toBe(true);
    expect(state().topology!.cluster!.leader_id).toBe('n2');
  });
});

describe('topology-v2 store · applyEvent · DPU deltas', () => {
  beforeEach(() => {
    state().reset();
    state().applySnapshot(snap(), 1);
  });

  it('KIND_DPU_STATE mutates the matching DPU only', () => {
    state().applyEvent({
      kind: 'KIND_DPU_STATE',
      event_id: 2,
      dpu: { id: 'dpu-1', state: 'DPU_STATE_DEGRADED', eni_count: 1, cordoned: true },
    });
    const app = state().topology!.appliances![0];
    const d1 = app.dpus.find((d) => d.id === 'dpu-1')!;
    const d2 = app.dpus.find((d) => d.id === 'dpu-2')!;
    expect(d1.state).toBe('DPU_STATE_DEGRADED');
    expect(d1.cordoned).toBe(true);
    expect(d1.enis).toHaveLength(1);          // existing ENIs preserved
    expect(d2.state).toBe('DPU_STATE_UP');    // sibling untouched
  });

  it('KIND_DPU_REMOVED drops the DPU from the appliance', () => {
    state().applyEvent({
      kind: 'KIND_DPU_REMOVED',
      event_id: 3,
      dpu: { id: 'dpu-2', state: 'DPU_STATE_DOWN', eni_count: 0 },
    });
    const app = state().topology!.appliances![0];
    expect(app.dpus.map((d) => d.id)).toEqual(['dpu-1']);
  });
});

describe('topology-v2 store · applyEvent · notices', () => {
  beforeEach(() => {
    state().reset();
    state().applySnapshot(snap(), 1);
  });

  it('KIND_DROPPED accumulates dropped_count', () => {
    state().applyEvent({ kind: 'KIND_DROPPED', event_id: 2, notice: { dropped_count: 3 } });
    state().applyEvent({ kind: 'KIND_DROPPED', event_id: 3, notice: { dropped_count: 2 } });
    expect(state().droppedEvents).toBe(5);
  });

  it('KIND_RATE_LIMITED accumulates suppressed_count', () => {
    state().applyEvent({ kind: 'KIND_RATE_LIMITED', event_id: 2, notice: { suppressed_count: 7 } });
    expect(state().suppressedEvents).toBe(7);
  });

  it('KIND_RESYNC increments upstreamReconnects', () => {
    state().applyEvent({ kind: 'KIND_RESYNC', event_id: 2 });
    state().applyEvent({ kind: 'KIND_RESYNC', event_id: 3 });
    expect(state().upstreamReconnects).toBe(2);
  });

  it('KIND_KEEPALIVE updates lastEventAt without mutating topology', () => {
    const before = state().topology;
    state().applyEvent({ kind: 'KIND_KEEPALIVE', event_id: 2 });
    expect(state().topology).toBe(before);     // referential equality
    expect(state().lastEventAt).toBeDefined();
  });
});

describe('topology-v2 store · event log capacity', () => {
  beforeEach(() => state().reset());

  it('caps the log at eventLogCapacity', () => {
    state().applySnapshot(snap(), 1);
    const cap = state().eventLogCapacity;
    for (let i = 0; i < cap + 10; i++) {
      state().applyEvent({ kind: 'KIND_KEEPALIVE', event_id: i + 2 });
    }
    expect(state().eventLog.length).toBe(cap);
  });
});

describe('topology-v2 store · findEntity', () => {
  beforeEach(() => {
    state().reset();
    state().applySnapshot(snap(), 1);
  });

  it('finds nodes / appliances / dpus / enis', () => {
    const s = state();
    expect(findEntity(s, 'node', 'n1')).toMatchObject({ node_id: 'n1' });
    expect(findEntity(s, 'appliance', 'app-a')).toMatchObject({ id: 'app-a' });
    expect(findEntity(s, 'dpu', 'dpu-2')).toMatchObject({ id: 'dpu-2' });
    expect(findEntity(s, 'eni', 'eni-1')).toMatchObject({ name: 'eni-1' });
  });

  it('returns undefined for unknown ids', () => {
    const s = state();
    expect(findEntity(s, 'node', 'nope')).toBeUndefined();
    expect(findEntity(s, 'dpu', 'nope')).toBeUndefined();
    expect(findEntity(s, 'eni', 'nope')).toBeUndefined();
    expect(findEntity(s, 'appliance', 'nope')).toBeUndefined();
  });

  it('returns undefined when no id supplied or no topology', () => {
    expect(findEntity(state(), 'node')).toBeUndefined();
    state().reset();
    expect(findEntity(state(), 'node', 'n1')).toBeUndefined();
  });
});
