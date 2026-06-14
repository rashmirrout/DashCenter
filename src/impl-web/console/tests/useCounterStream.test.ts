// Unit tests for useCounterStream. JSDOM has no EventSource so we
// install a minimal fake that records constructor params, lets us
// trigger named events, and supports close() lifecycle.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';

import { useCounterStream } from '@/queries/useCounterStream';
import { useCountersStore, type CounterEvent } from '@/stores/counters-store';

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

  close() {
    this.closed = true;
  }

  emit(name: string, data: unknown) {
    const ev = { data: JSON.stringify(data) } as MessageEvent;
    for (const fn of this.listeners.get(name) ?? []) {
      fn(ev as unknown as Event);
    }
  }

  triggerError() {
    this.onerror?.call(this as unknown as EventSource, new Event('error'));
  }
}

function report(dpuId: string, eventId?: number): CounterEvent {
  return {
    kind: 'KIND_REPORT',
    event_id: eventId?.toString(),
    report: {
      dpu_id: dpuId,
      vxlan_decap: '100',
      vxlan_encap: '200',
      drop_acl_in: '5',
      flows_created_total: '1000',
      flow_table_size: '500',
    },
  };
}

function snapshot(eventId?: number): CounterEvent {
  return {
    kind: 'KIND_SNAPSHOT',
    event_id: eventId?.toString(),
    report: {
      dpu_id: 'dpu-1',
      vxlan_decap: '150',
    },
  };
}

function dropNotice(count: number): CounterEvent {
  return {
    kind: 'KIND_DROPPED',
    notice: { dropped_count: String(count) },
  };
}

function resyncNotice(): CounterEvent {
  return { kind: 'KIND_RESYNC' };
}

beforeEach(() => {
  vi.useFakeTimers();
  FakeEventSource.instances = [];
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
  useCountersStore.getState().reset();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('useCounterStream · lifecycle', () => {
  it('enabled: false → no EventSource constructed', () => {
    renderHook(() => useCounterStream({ enabled: false }));
    expect(FakeEventSource.instances).toHaveLength(0);
  });

  it('mount with default opts → exactly 1 EventSource at /api/console/counters/stream', () => {
    renderHook(() => useCounterStream());

    expect(FakeEventSource.instances).toHaveLength(1);
    const es = FakeEventSource.instances[0];
    expect(es.url).toBe('/api/console/counters/stream');
    expect(useCountersStore.getState().connection).toBe('connecting');
  });

  it('mount with dpuIds → URL contains ?dpu=dpu-a&dpu=dpu-b', () => {
    renderHook(() => useCounterStream({ dpuIds: ['dpu-a', 'dpu-b'] }));

    const es = FakeEventSource.instances[0];
    expect(es.url).toContain('dpu=dpu-a');
    expect(es.url).toContain('dpu=dpu-b');
  });

  it('mount with dpuIds containing empty entry → only non-empty included', () => {
    renderHook(() => useCounterStream({ dpuIds: ['', 'dpu-a'] }));

    const es = FakeEventSource.instances[0];
    expect(es.url).toContain('dpu=dpu-a');
    expect(es.url).not.toContain('dpu=&');
  });

  it('after lastEventId=42 in store, mount includes last_event_id=42 in URL', () => {
    useCountersStore.setState({ lastEventId: 42 });
    renderHook(() => useCounterStream());

    const es = FakeEventSource.instances[0];
    expect(es.url).toContain('last_event_id=42');
  });

  it('tears down EventSource on unmount', () => {
    const { unmount } = renderHook(() => useCounterStream());
    const es = FakeEventSource.instances[0];
    expect(es.closed).toBe(false);
    unmount();
    expect(es.closed).toBe(true);
  });
});

describe('useCounterStream · event listeners', () => {
  it('registers listeners for all 6 event kinds', () => {
    renderHook(() => useCounterStream());

    const es = FakeEventSource.instances[0];
    expect(es.listeners.has('snapshot')).toBe(true);
    expect(es.listeners.has('report')).toBe(true);
    expect(es.listeners.has('keepalive')).toBe(true);
    expect(es.listeners.has('dropped')).toBe(true);
    expect(es.listeners.has('rate_limited')).toBe(true);
    expect(es.listeners.has('resync')).toBe(true);
    expect(es.listeners.size).toBe(6);
  });
});

describe('useCounterStream · event handling', () => {
  it('emit report event → store.apply invoked → lastEventId reflects event_id', () => {
    renderHook(() => useCounterStream());

    const es = FakeEventSource.instances[0];
    act(() => es.emit('report', report('dpu-1', 99)));

    expect(useCountersStore.getState().lastEventId).toBe(99);
    expect(useCountersStore.getState().byDpu['dpu-1']).toBeDefined();
  });

  it('emit snapshot event → store updates byDpu', () => {
    renderHook(() => useCounterStream());
    const es = FakeEventSource.instances[0];
    act(() => es.emit('snapshot', snapshot(100)));

    const series = useCountersStore.getState().byDpu['dpu-1'];
    expect(series).toBeDefined();
    expect(series?.samples).toHaveLength(1);
  });

  it('emit dropped notice → droppedEvents increments', () => {
    renderHook(() => useCounterStream());
    const es = FakeEventSource.instances[0];
    act(() => es.emit('dropped', dropNotice(5)));

    expect(useCountersStore.getState().droppedEvents).toBe(5);
  });

  it('emit resync notice → reconnects increments', () => {
    renderHook(() => useCounterStream());
    const es = FakeEventSource.instances[0];

    expect(useCountersStore.getState().reconnects).toBe(0);
    act(() => es.emit('resync', resyncNotice()));
    expect(useCountersStore.getState().reconnects).toBe(1);
  });
});

describe('useCounterStream · error & reconnect', () => {
  it('onerror fires → reconnecting; after backoff new EventSource opens', () => {
    renderHook(() => useCounterStream());

    const first = FakeEventSource.instances[0];
    act(() => first.triggerError());

    expect(first.closed).toBe(true);
    expect(useCountersStore.getState().connection).toBe('reconnecting');
    expect(FakeEventSource.instances).toHaveLength(1);

    act(() => vi.advanceTimersByTime(1500));

    expect(FakeEventSource.instances).toHaveLength(2);
    expect(useCountersStore.getState().connection).toBe('connecting');
  });

  it('multiple consecutive errors increase backoff delay', () => {
    renderHook(() => useCounterStream());

    const first = FakeEventSource.instances[0];
    act(() => first.triggerError());
    act(() => vi.advanceTimersByTime(1500));
    expect(FakeEventSource.instances).toHaveLength(2);

    const second = FakeEventSource.instances[1];
    act(() => second.triggerError());
    act(() => vi.advanceTimersByTime(2000));
    expect(FakeEventSource.instances).toHaveLength(3);

    const third = FakeEventSource.instances[2];
    act(() => third.triggerError());
    act(() => vi.advanceTimersByTime(4000));
    expect(FakeEventSource.instances).toHaveLength(4);
  });
});

describe('useCounterStream · visibility pause', () => {
  it('tab hidden → after grace period EventSource closes; connection paused', () => {
    renderHook(() => useCounterStream());

    const es = FakeEventSource.instances[0];
    Object.defineProperty(document, 'hidden', { value: true, configurable: true });
    act(() => { document.dispatchEvent(new Event('visibilitychange')); });

    act(() => vi.advanceTimersByTime(30000));
    expect(es.closed).toBe(false);

    act(() => vi.advanceTimersByTime(31000));
    expect(es.closed).toBe(true);
    expect(useCountersStore.getState().connection).toBe('paused');
  });

  it('tab visible after pause → new EventSource constructed', () => {
    renderHook(() => useCounterStream());

    Object.defineProperty(document, 'hidden', { value: true, configurable: true });
    act(() => { document.dispatchEvent(new Event('visibilitychange')); });
    act(() => vi.advanceTimersByTime(61000));

    expect(FakeEventSource.instances).toHaveLength(1);
    expect(FakeEventSource.instances[0].closed).toBe(true);

    Object.defineProperty(document, 'hidden', { value: false, configurable: true });
    act(() => { document.dispatchEvent(new Event('visibilitychange')); });

    expect(FakeEventSource.instances).toHaveLength(2);
    expect(FakeEventSource.instances[1].closed).toBe(false);
  });
});

describe('useCounterStream · error handling', () => {
  it('JSON parse error → connection error with SyntaxError message', () => {
    renderHook(() => useCounterStream());

    const es = FakeEventSource.instances[0];
    const handler = es.listeners.get('keepalive')?.[0];
    expect(handler).toBeDefined();

    act(() => {
      handler!({ data: '{not-valid-json' } as unknown as Event);
    });

    expect(useCountersStore.getState().connection).toBe('error');
    expect(useCountersStore.getState().lastError).toMatch(/SyntaxError|JSON/i);
  });
});

describe('useCounterStream · cleanup', () => {
  it('unmount with pending reconnect timer → no zombie EventSource', () => {
    const { unmount } = renderHook(() => useCounterStream());

    const first = FakeEventSource.instances[0];
    act(() => first.triggerError());
    expect(FakeEventSource.instances).toHaveLength(1);

    unmount();

    act(() => vi.advanceTimersByTime(2000));

    expect(FakeEventSource.instances).toHaveLength(1);
  });
});
