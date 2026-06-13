// Unit tests for useTopologyStream. JSDOM has no EventSource so we
// install a minimal fake that records constructor params, lets us
// trigger named events, and supports close() lifecycle. The hook's
// invariants under test:
//   * opens a single EventSource on mount
//   * forwards `snapshot` + delta events into the Zustand store
//   * appends `?last_event_id=N` on subsequent opens once the cursor moves
//   * tears down EventSource + listeners on unmount
//   * does nothing when enabled=false (used by tests that don't want
//     a live network)

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createElement } from 'react';

import { useTopologyStream } from '@/queries/useTopologyStream';
import { useTopologyV2Store } from '@/stores/topology-v2-store';
import type { TopologyEvent, TopologyV2Response } from '@/api/topology-v2-types';

// ── fake EventSource ────────────────────────────────────────────
class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  closed = false;
  onerror: ((this: EventSource, ev: Event) => unknown) | null = null;
  listeners = new Map<string, EventListener[]>();
  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }
  addEventListener(name: string, fn: EventListener) {
    const arr = this.listeners.get(name) ?? [];
    arr.push(fn);
    this.listeners.set(name, arr);
  }
  removeEventListener(name: string, fn: EventListener) {
    const arr = (this.listeners.get(name) ?? []).filter((f) => f !== fn);
    this.listeners.set(name, arr);
  }
  close() { this.closed = true; }
  emit(name: string, data: unknown) {
    const ev = { data: JSON.stringify(data) } as MessageEvent;
    for (const fn of this.listeners.get(name) ?? []) fn(ev as unknown as Event);
  }
  triggerError() {
    this.onerror?.call(this as unknown as EventSource, new Event('error'));
  }
}

function snap(): TopologyV2Response {
  return {
    cluster: {
      healthy: true, leader_id: 'n1', node_count: 1,
      nodes: [{ node_id: 'n1', is_leader: true }],
    },
    appliances: [],
    summary: {
      total_nodes: 1, total_appliances: 0, total_dpus: 0, total_enis: 0,
      healthy_dpus: 0, degraded_dpus: 0, offline_dpus: 0, cordoned_dpus: 0,
    },
  };
}

function withQc() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return {
    qc,
    wrapper: ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client: qc }, children),
  };
}

// ── shared lifecycle ────────────────────────────────────────────
beforeEach(() => {
  vi.useFakeTimers();
  FakeEventSource.instances = [];
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
  useTopologyV2Store.getState().reset();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('useTopologyStream · lifecycle', () => {
  it('opens an EventSource on mount + tears it down on unmount', () => {
    const { wrapper } = withQc();
    const { unmount } = renderHook(() => useTopologyStream(), { wrapper });

    expect(FakeEventSource.instances).toHaveLength(1);
    const es = FakeEventSource.instances[0];
    expect(es.url).toContain('/api/console/topology-v2/stream');
    expect(useTopologyV2Store.getState().connection).toBe('connecting');

    unmount();
    expect(es.closed).toBe(true);
  });

  it('does nothing when enabled=false', () => {
    const { wrapper } = withQc();
    renderHook(() => useTopologyStream({ enabled: false }), { wrapper });
    expect(FakeEventSource.instances).toHaveLength(0);
  });

  it('appends ?include_enis=true when requested', () => {
    const { wrapper } = withQc();
    renderHook(() => useTopologyStream({ includeEnis: true }), { wrapper });
    expect(FakeEventSource.instances[0].url).toContain('include_enis=true');
  });
});

describe('useTopologyStream · events', () => {
  it('applies snapshot + flips connection to open + caches with query client', () => {
    const { wrapper, qc } = withQc();
    renderHook(() => useTopologyStream(), { wrapper });

    const es = FakeEventSource.instances[0];
    const ev: TopologyEvent = { kind: 'KIND_SNAPSHOT', event_id: 42, snapshot: snap() };

    act(() => es.emit('snapshot', ev));

    const s = useTopologyV2Store.getState();
    expect(s.connection).toBe('open');
    expect(s.topology?.cluster?.leader_id).toBe('n1');
    expect(s.lastEventId).toBe(42);
    expect(qc.getQueryData<TopologyV2Response>(['topology-v2', false])).toBeDefined();
  });

  it('applies a peer_added delta into the store', () => {
    const { wrapper } = withQc();
    renderHook(() => useTopologyStream(), { wrapper });
    const es = FakeEventSource.instances[0];

    act(() => es.emit('snapshot', { kind: 'KIND_SNAPSHOT', event_id: 1, snapshot: snap() }));
    act(() => es.emit('peer_added', {
      kind: 'KIND_PEER_ADDED',
      event_id: 2,
      peer: { node_id: 'n2', is_leader: false },
    } satisfies TopologyEvent));

    const c = useTopologyV2Store.getState().topology!.cluster!;
    expect(c.node_count).toBe(2);
    expect(c.nodes.map((n) => n.node_id).sort()).toEqual(['n1', 'n2']);
  });

  it('ignores malformed event JSON without throwing', () => {
    const { wrapper } = withQc();
    renderHook(() => useTopologyStream(), { wrapper });
    const es = FakeEventSource.instances[0];

    expect(() => {
      const handler = es.listeners.get('keepalive')?.[0];
      if (handler) handler({ data: '{not-json' } as MessageEvent);
    }).not.toThrow();
  });
});

describe('useTopologyStream · reconnect', () => {
  it('on error: closes ES, sets reconnecting, schedules a new open', () => {
    const { wrapper } = withQc();
    renderHook(() => useTopologyStream(), { wrapper });

    const first = FakeEventSource.instances[0];
    act(() => first.triggerError());

    expect(first.closed).toBe(true);
    expect(useTopologyV2Store.getState().connection).toBe('reconnecting');
    expect(FakeEventSource.instances).toHaveLength(1);

    // backoff window: <= initial 500ms + jitter. Advance generously.
    act(() => { vi.advanceTimersByTime(1500); });

    expect(FakeEventSource.instances).toHaveLength(2);
    expect(useTopologyV2Store.getState().connection).toBe('connecting');
  });

  it('attaches ?last_event_id=N on reconnect once cursor has moved', () => {
    const { wrapper } = withQc();
    renderHook(() => useTopologyStream(), { wrapper });

    const first = FakeEventSource.instances[0];
    act(() => first.emit('snapshot', { kind: 'KIND_SNAPSHOT', event_id: 123, snapshot: snap() }));
    act(() => first.triggerError());
    act(() => { vi.advanceTimersByTime(1500); });

    const second = FakeEventSource.instances[1];
    expect(second.url).toContain('last_event_id=123');
  });
});
