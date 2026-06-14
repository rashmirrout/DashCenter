// Unit tests for the counters store reducer.
//
// Covers each CounterEvent kind (KIND_SNAPSHOT, KIND_REPORT, KIND_KEEPALIVE,
// KIND_DROPPED, KIND_RATE_LIMITED, KIND_RESYNC) and the core behaviors:
// ring buffer capping, event_id ratcheting, provenance tracking, and
// summary aggregation.

import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
  useCountersStore,
  selectSeries,
  selectSummary,
  type CounterEvent,
  type CounterReport,
  type CountersState,
} from '@/stores/counters-store';

// ── helpers ──────────────────────────────────────────────────────

function state(): CountersState {
  return useCountersStore.getState();
}

function report(overrides: Partial<CounterReport> = {}): CounterReport {
  return {
    dpu_id: 'dpu-0',
    vxlan_decap: '10',
    vxlan_encap: '20',
    drop_acl_in: '5',
    flows_created_total: '100',
    flow_table_size: '50',
    ...overrides,
  };
}

function ev(kind: string, overrides: Partial<CounterEvent> = {}): CounterEvent {
  return {
    kind,
    ...overrides,
  };
}

// ── tests ────────────────────────────────────────────────────────

describe('counters store · lifecycle', () => {
  beforeEach(() => state().reset());

  it('starts idle with zero counters and empty byDpu', () => {
    const s = state();
    expect(s.connection).toBe('idle');
    expect(s.lastEventId).toBe(0);
    expect(s.droppedEvents).toBe(0);
    expect(s.suppressedEvents).toBe(0);
    expect(s.reconnects).toBe(0);
    expect(s.byDpu).toEqual({});
    expect(s.capacity).toBe(120);
    expect(s.lastError).toBeUndefined();
    expect(s.lastEventAt).toBeUndefined();
  });

  it('setConnection updates connection state', () => {
    state().setConnection('connecting');
    expect(state().connection).toBe('connecting');
    expect(state().lastError).toBeUndefined();
  });

  it('setConnection with error message records lastError', () => {
    state().setConnection('error', 'network timeout');
    expect(state().connection).toBe('error');
    expect(state().lastError).toBe('network timeout');
  });

  it('reset clears all state back to initial', () => {
    state().setConnection('error', 'boom');
    state().apply(ev('KIND_DROPPED', { notice: { dropped_count: '5' } }));
    state().apply(ev('KIND_REPORT', { report: report({ dpu_id: 'dpu-1' }) }));

    state().reset();

    const s = state();
    expect(s.connection).toBe('idle');
    expect(s.lastError).toBeUndefined();
    expect(s.lastEventId).toBe(0);
    expect(s.droppedEvents).toBe(0);
    expect(s.suppressedEvents).toBe(0);
    expect(s.reconnects).toBe(0);
    expect(s.byDpu).toEqual({});
    expect(s.lastSource).toBeUndefined();
    expect(s.lastVia).toBeUndefined();
    expect(s.lastEventAt).toBeUndefined();
  });
});

describe('counters store · apply · KIND_SNAPSHOT', () => {
  beforeEach(() => state().reset());

  it('KIND_SNAPSHOT creates a series with one sample', () => {
    const before = Date.now() - 10;
    state().apply(ev('KIND_SNAPSHOT', { report: report({ dpu_id: 'dpu-a' }) }));
    const after = Date.now() + 10;

    const series = selectSeries('dpu-a')(state());
    expect(series).toBeDefined();
    expect(series!.dpuId).toBe('dpu-a');
    expect(series!.samples).toHaveLength(1);
    expect(series!.samples[0].at).toBeGreaterThanOrEqual(before);
    expect(series!.samples[0].at).toBeLessThanOrEqual(after);
    expect(series!.samples[0].vxlan_decap).toBe(10);
    expect(series!.samples[0].vxlan_encap).toBe(20);
    expect(series!.samples[0].drop_acl_in).toBe(5);
    expect(series!.samples[0].flows_created_total).toBe(100);
    expect(series!.samples[0].flow_table_size).toBe(50);
    expect(series!.latest).toEqual(report({ dpu_id: 'dpu-a' }));
  });

  it('KIND_SNAPSHOT with empty dpu_id is skipped', () => {
    state().apply(ev('KIND_SNAPSHOT', { report: report({ dpu_id: '' }) }));
    expect(state().byDpu).toEqual({});
  });

  it('KIND_SNAPSHOT with null report is skipped', () => {
    state().apply(ev('KIND_SNAPSHOT', { report: undefined }));
    expect(state().byDpu).toEqual({});
  });
});

describe('counters store · apply · KIND_REPORT', () => {
  beforeEach(() => state().reset());

  it('KIND_REPORT appends a sample to an existing series', () => {
    state().apply(ev('KIND_SNAPSHOT', { report: report({ dpu_id: 'dpu-b' }) }));
    const firstAt = state().byDpu['dpu-b'].samples[0].at;

    state().apply(
      ev('KIND_REPORT', {
        report: report({
          dpu_id: 'dpu-b',
          vxlan_decap: '11',
          vxlan_encap: '21',
        }),
      })
    );

    const series = selectSeries('dpu-b')(state());
    expect(series!.samples).toHaveLength(2);
    expect(series!.samples[0].at).toBe(firstAt);
    expect(series!.samples[0].vxlan_decap).toBe(10);
    expect(series!.samples[1].vxlan_decap).toBe(11);
    expect(series!.samples[1].vxlan_encap).toBe(21);
  });

  it('ring buffer caps samples at capacity (120 by default)', () => {
    const capacity = state().capacity;
    for (let i = 0; i < capacity + 5; i++) {
      state().apply(
        ev('KIND_REPORT', {
          report: report({
            dpu_id: 'dpu-cap',
            vxlan_decap: String(i),
          }),
        })
      );
    }

    const series = selectSeries('dpu-cap')(state());
    expect(series!.samples).toHaveLength(capacity);
    expect(series!.samples[0].vxlan_decap).toBe(5);
    expect(series!.samples[capacity - 1].vxlan_decap).toBe(capacity + 4);
  });
});

describe('counters store · apply · event_id ratcheting', () => {
  beforeEach(() => state().reset());

  it('event_id ratchets upward only', () => {
    state().apply(ev('KIND_KEEPALIVE', { event_id: '100' }));
    expect(state().lastEventId).toBe(100);

    state().apply(ev('KIND_KEEPALIVE', { event_id: '50' }));
    expect(state().lastEventId).toBe(100);

    state().apply(ev('KIND_KEEPALIVE', { event_id: '150' }));
    expect(state().lastEventId).toBe(150);
  });

  it('parseIntSafe handles string, numeric, undefined correctly', () => {
    state().apply(ev('KIND_KEEPALIVE', { event_id: '42' }));
    expect(state().lastEventId).toBe(42);

    state().apply(ev('KIND_KEEPALIVE', { event_id: '0' }));
    expect(state().lastEventId).toBe(42);

    const before = state().lastEventId;
    state().apply(ev('KIND_KEEPALIVE', { event_id: undefined }));
    expect(state().lastEventId).toBe(before);

    state().apply(ev('KIND_KEEPALIVE', { event_id: 'abc' }));
    expect(state().lastEventId).toBe(42);
  });
});

describe('counters store · apply · provenance (source + via)', () => {
  beforeEach(() => state().reset());

  it('source + via are tracked from events', () => {
    state().apply(
      ev('KIND_KEEPALIVE', {
        source: 'dashd-primary',
        via: 'dashw-replica-1',
      })
    );
    expect(state().lastSource).toBe('dashd-primary');
    expect(state().lastVia).toBe('dashw-replica-1');
  });

  it('lastSource and lastVia update on each event', () => {
    state().apply(ev('KIND_KEEPALIVE', { source: 'dashd-1', via: 'dashw-1' }));
    state().apply(
      ev('KIND_KEEPALIVE', {
        source: 'dashd-2',
        via: 'dashw-2',
      })
    );
    expect(state().lastSource).toBe('dashd-2');
    expect(state().lastVia).toBe('dashw-2');
  });
});

describe('counters store · apply · KIND_KEEPALIVE', () => {
  beforeEach(() => state().reset());

  it('KIND_KEEPALIVE is a no-op for data but updates lastEventAt', () => {
    const before = state().lastEventAt;
    state().apply(ev('KIND_KEEPALIVE'));
    expect(state().lastEventAt).toBeGreaterThan(before ?? 0);
    expect(state().droppedEvents).toBe(0);
    expect(state().suppressedEvents).toBe(0);
    expect(state().byDpu).toEqual({});
  });
});

describe('counters store · apply · KIND_DROPPED', () => {
  beforeEach(() => state().reset());

  it('KIND_DROPPED increments droppedEvents by dropped_count', () => {
    state().apply(ev('KIND_DROPPED', { notice: { dropped_count: '3' } }));
    expect(state().droppedEvents).toBe(3);

    state().apply(ev('KIND_DROPPED', { notice: { dropped_count: '7' } }));
    expect(state().droppedEvents).toBe(10);
  });

  it('KIND_DROPPED with invalid dropped_count defaults to 0', () => {
    state().apply(ev('KIND_DROPPED', { notice: { dropped_count: 'invalid' } }));
    expect(state().droppedEvents).toBe(0);
  });
});

describe('counters store · apply · KIND_RATE_LIMITED', () => {
  beforeEach(() => state().reset());

  it('KIND_RATE_LIMITED increments suppressedEvents by suppressed_count', () => {
    state().apply(ev('KIND_RATE_LIMITED', { notice: { suppressed_count: '5' } }));
    expect(state().suppressedEvents).toBe(5);

    state().apply(
      ev('KIND_RATE_LIMITED', {
        notice: { suppressed_count: '10' },
      })
    );
    expect(state().suppressedEvents).toBe(15);
  });
});

describe('counters store · apply · KIND_RESYNC', () => {
  beforeEach(() => state().reset());

  it('KIND_RESYNC increments reconnects by 1', () => {
    state().apply(ev('KIND_RESYNC'));
    expect(state().reconnects).toBe(1);

    state().apply(ev('KIND_RESYNC'));
    expect(state().reconnects).toBe(2);
  });

  it('KIND_RESYNC does NOT clear byDpu', () => {
    state().apply(ev('KIND_SNAPSHOT', { report: report({ dpu_id: 'dpu-x' }) }));
    const byDpuBefore = state().byDpu;

    state().apply(ev('KIND_RESYNC'));

    expect(state().byDpu).toEqual(byDpuBefore);
    expect(state().byDpu['dpu-x']).toBeDefined();
  });
});

describe('counters store · apply · lastEventAt', () => {
  beforeEach(() => state().reset());

  it('lastEventAt is updated on every apply (including keepalive)', () => {
    expect(state().lastEventAt).toBeUndefined();

    const before = Date.now() - 10;
    state().apply(ev('KIND_KEEPALIVE'));
    const after = Date.now() + 10;

    expect(state().lastEventAt).toBeGreaterThanOrEqual(before);
    expect(state().lastEventAt).toBeLessThanOrEqual(after);
  });
});

describe('counters store · multi-DPU', () => {
  beforeEach(() => state().reset());

  it('two distinct dpu_ids create two distinct series', () => {
    state().apply(ev('KIND_SNAPSHOT', { report: report({ dpu_id: 'dpu-alpha' }) }));
    state().apply(
      ev('KIND_SNAPSHOT', {
        report: report({
          dpu_id: 'dpu-beta',
          vxlan_decap: '99',
        }),
      })
    );

    const seriesAlpha = selectSeries('dpu-alpha')(state());
    const seriesBeta = selectSeries('dpu-beta')(state());

    expect(seriesAlpha).toBeDefined();
    expect(seriesBeta).toBeDefined();
    expect(seriesAlpha!.dpuId).toBe('dpu-alpha');
    expect(seriesBeta!.dpuId).toBe('dpu-beta');
    expect(seriesAlpha!.samples[0].vxlan_decap).toBe(10);
    expect(seriesBeta!.samples[0].vxlan_decap).toBe(99);
  });
});

describe('counters store · selectors', () => {
  beforeEach(() => state().reset());

  it('selectSeries returns undefined for missing dpu_id', () => {
    const series = selectSeries('nonexistent')(state());
    expect(series).toBeUndefined();
  });

  it('selectSeries returns the series after apply', () => {
    state().apply(ev('KIND_SNAPSHOT', { report: report({ dpu_id: 'dpu-sel' }) }));
    const series = selectSeries('dpu-sel')(state());
    expect(series).toBeDefined();
    expect(series!.dpuId).toBe('dpu-sel');
    expect(series!.samples).toHaveLength(1);
  });

  it('selectSummary aggregates latest sample across all DPUs', () => {
    state().apply(ev('KIND_SNAPSHOT', { report: report({ dpu_id: 'dpu-1' }) }));
    state().apply(
      ev('KIND_SNAPSHOT', {
        report: report({
          dpu_id: 'dpu-2',
          vxlan_decap: '30',
          vxlan_encap: '40',
          drop_acl_in: '2',
          flows_created_total: '200',
        }),
      })
    );

    const summary = selectSummary(state());
    expect(summary.dpuCount).toBe(2);
    expect(summary.totalDecap).toBe(40);
    expect(summary.totalEncap).toBe(60);
    expect(summary.totalDrops).toBe(7);
    expect(summary.totalFlows).toBe(300);
  });

  it('selectSummary with no DPUs returns zero values', () => {
    const summary = selectSummary(state());
    expect(summary.dpuCount).toBe(0);
    expect(summary.totalDecap).toBe(0);
    expect(summary.totalEncap).toBe(0);
    expect(summary.totalDrops).toBe(0);
    expect(summary.totalFlows).toBe(0);
  });

  it('selectSummary with DPU having no samples returns partial aggregate', () => {
    state().apply(ev('KIND_SNAPSHOT', { report: report({ dpu_id: 'dpu-with-sample' }) }));
    useCountersStore.setState((st) => ({
      byDpu: {
        ...st.byDpu,
        'dpu-no-sample': { dpuId: 'dpu-no-sample', samples: [] },
      },
    }));

    const summary = selectSummary(state());
    expect(summary.dpuCount).toBe(2);
    expect(summary.totalDecap).toBe(10);
  });
});

describe('counters store · parseIntSafe coverage', () => {
  beforeEach(() => state().reset());

  it('parseIntSafe handles edge cases via apply', () => {
    state().apply(ev('KIND_DROPPED', { notice: { dropped_count: '' } }));
    expect(state().droppedEvents).toBe(0);

    state().apply(ev('KIND_DROPPED', { notice: { dropped_count: undefined } }));
    expect(state().droppedEvents).toBe(0);

    state().apply(ev('KIND_DROPPED', { notice: { dropped_count: 'abc' } }));
    expect(state().droppedEvents).toBe(0);

    state().apply(ev('KIND_DROPPED', { notice: { dropped_count: '42' } }));
    expect(state().droppedEvents).toBe(42);

    const before = state().droppedEvents;
    state().apply(ev('KIND_DROPPED', { notice: { dropped_count: '0' } }));
    expect(state().droppedEvents).toBe(before);
  });
});

describe('counters store · sample `at` field precision', () => {
  beforeEach(() => state().reset());

  it('each sample `at` is set to Date.now() at apply time', () => {
    const before = Date.now();
    state().apply(ev('KIND_SNAPSHOT', { report: report({ dpu_id: 'dpu-time' }) }));
    const after = Date.now();

    const sample = selectSeries('dpu-time')(state())!.samples[0];
    expect(sample.at).toBeGreaterThanOrEqual(before);
    expect(sample.at).toBeLessThanOrEqual(after);
  });

  it('consecutive applies produce non-decreasing `at` timestamps', () => {
    state().apply(ev('KIND_SNAPSHOT', { report: report({ dpu_id: 'dpu-seq' }) }));
    const first = selectSeries('dpu-seq')(state())!.samples[0].at;

    state().apply(ev('KIND_REPORT', { report: report({ dpu_id: 'dpu-seq' }) }));

    const second = selectSeries('dpu-seq')(state())!.samples[1].at;
    expect(second).toBeGreaterThanOrEqual(first);
  });
});

// Keep vitest import used.
void vi;
