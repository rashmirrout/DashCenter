// CounterWidget tests — pure-helper coverage + component rendering.
//
// Stores per-DPU sample arrays directly into the Zustand store via
// setState (test-only state injection) so we don't need a real
// EventSource.

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import {
  CounterWidget,
  smooth,
  sparklinePath,
} from '../src/views/topology-v2/CounterWidget';
import {
  useCountersStore,
  type CounterSample,
  type CountersState,
} from '@/stores/counters-store';

function setupSeries(dpuId: string, samples: CounterSample[]): void {
  useCountersStore.setState((st: CountersState) => ({
    ...st,
    byDpu: {
      ...st.byDpu,
      [dpuId]: { dpuId, samples, latest: undefined },
    },
  }));
}

function resetStore(): void {
  useCountersStore.getState().reset();
}

// ── smooth() ────────────────────────────────────────────────────

describe('CounterWidget · smooth()', () => {
  it('returns [] for empty input', () => {
    expect(smooth([], 5)).toEqual([]);
  });

  it('returns input unchanged for single value', () => {
    expect(smooth([5], 3)).toEqual([5]);
  });

  it('returns input unchanged for single-value window', () => {
    expect(smooth([1, 2, 3], 1)).toEqual([1, 2, 3]);
  });

  it('computes rolling mean with window=2', () => {
    const result = smooth([1, 2, 3, 4, 5], 2);
    expect(result).toHaveLength(5);
    expect(result[0]).toBeCloseTo(1, 0);
    expect(result[1]).toBeCloseTo(1.5, 0);
    expect(result[2]).toBeCloseTo(2.5, 0);
  });

  it('computes rolling mean with window=5', () => {
    const result = smooth([1, 2, 3, 4, 5, 6], 5);
    expect(result).toHaveLength(6);
    expect(result[2]).toBeCloseTo(3, 0);
  });
});

// ── sparklinePath() ─────────────────────────────────────────────

describe('CounterWidget · sparklinePath()', () => {
  it('returns "" for empty input', () => {
    expect(sparklinePath([], 100, 20)).toBe('');
  });

  it('returns path starting with "M"', () => {
    const path = sparklinePath([1, 2, 3], 100, 20);
    expect(path).toMatch(/^M/);
  });

  it('produces 1 M + (n-1) L commands for n values', () => {
    const path = sparklinePath([1, 2, 3], 100, 20);
    const moveCount = (path.match(/M/g) || []).length;
    const lineCount = (path.match(/L/g) || []).length;
    expect(moveCount).toBe(1);
    expect(lineCount).toBe(2);
  });

  it('renders flat line for all-equal values (no division-by-zero)', () => {
    const path = sparklinePath([5, 5, 5, 5, 5], 100, 20);
    const yCoords = path.match(/[ML]\s\d+(?:\.\d+)?,(\d+(?:\.\d+)?)/g) || [];
    expect(yCoords.length).toBeGreaterThan(1);
    if (yCoords.length > 1) {
      const ys = yCoords.map((c) => parseFloat(c.split(',')[1]));
      for (let i = 1; i < ys.length; i++) {
        expect(ys[i]).toBeCloseTo(ys[0], 0);
      }
    }
    expect(path).not.toContain('NaN');
  });

  it('handles single value without NaN', () => {
    const path = sparklinePath([42], 100, 20);
    expect(path).toMatch(/^M/);
    expect(path).not.toContain('NaN');
  });

  it('scales higher values to lower y (visually higher on screen)', () => {
    const path = sparklinePath([1, 10], 100, 20);
    const coords = path.match(/(\d+(?:\.\d+)?),(\d+(?:\.\d+)?)/g) || [];
    expect(coords.length).toBe(2);
    const y1 = parseFloat(coords[0].split(',')[1]);
    const y2 = parseFloat(coords[1].split(',')[1]);
    expect(y1).toBeGreaterThan(y2);
  });
});

// ── component ───────────────────────────────────────────────────

describe('CounterWidget · component', () => {
  beforeEach(() => {
    resetStore();
  });

  it('renders "No counter data" when no series exists', () => {
    render(<CounterWidget dpuId="unknown-dpu" />);
    expect(screen.getByText(/No counter data yet for unknown-dpu/)).toBeInTheDocument();
  });

  it('renders "No counter data" when series is empty', () => {
    setupSeries('dpu-a', []);
    render(<CounterWidget dpuId="dpu-a" />);
    expect(screen.getByText(/No counter data yet for dpu-a/)).toBeInTheDocument();
  });

  it('renders top-line numbers with one sample', () => {
    const sample: CounterSample = {
      at: Date.now(),
      vxlan_decap: 1000,
      vxlan_encap: 2000,
      drop_acl_in: 500,
      flows_created_total: 0,
      flow_table_size: 42,
    };
    setupSeries('dpu-1', [sample]);
    render(<CounterWidget dpuId="dpu-1" />);

    expect(screen.getByText('1,000')).toBeInTheDocument();
    expect(screen.getByText('2,000')).toBeInTheDocument();
    expect(screen.getByText('500')).toBeInTheDocument();
    expect(screen.getByText('42')).toBeInTheDocument();
  });

  it('renders 4 sparklines when series has samples', () => {
    const samples: CounterSample[] = [
      { at: Date.now() - 1000, vxlan_decap: 100, vxlan_encap: 200, drop_acl_in: 50, flows_created_total: 0, flow_table_size: 10 },
      { at: Date.now(), vxlan_decap: 110, vxlan_encap: 210, drop_acl_in: 55, flows_created_total: 0, flow_table_size: 12 },
    ];
    setupSeries('dpu-2', samples);
    render(<CounterWidget dpuId="dpu-2" />);

    const sparklines = screen.getAllByTestId(/^sparkline-/);
    expect(sparklines).toHaveLength(4);
    expect(screen.getByTestId('sparkline-vxlan_decap')).toBeInTheDocument();
    expect(screen.getByTestId('sparkline-vxlan_encap')).toBeInTheDocument();
    expect(screen.getByTestId('sparkline-drop_acl_in')).toBeInTheDocument();
    expect(screen.getByTestId('sparkline-flow_table_size')).toBeInTheDocument();
  });

  it('renders labels for all 4 counters', () => {
    const sample: CounterSample = {
      at: Date.now(), vxlan_decap: 100, vxlan_encap: 200, drop_acl_in: 50, flows_created_total: 0, flow_table_size: 10,
    };
    setupSeries('dpu-3', [sample]);
    render(<CounterWidget dpuId="dpu-3" />);

    expect(screen.getByText('VXLAN Decap')).toBeInTheDocument();
    expect(screen.getByText('VXLAN Encap')).toBeInTheDocument();
    expect(screen.getByText('Drop ACL In')).toBeInTheDocument();
    expect(screen.getByText('Flow Table Size')).toBeInTheDocument();
  });

  it('re-renders with different dpuId and shows new data', () => {
    const sampleA: CounterSample = {
      at: Date.now(), vxlan_decap: 1000, vxlan_encap: 2000, drop_acl_in: 500, flows_created_total: 0, flow_table_size: 42,
    };
    const sampleB: CounterSample = {
      at: Date.now(), vxlan_decap: 5000, vxlan_encap: 6000, drop_acl_in: 1500, flows_created_total: 0, flow_table_size: 99,
    };
    setupSeries('dpu-a', [sampleA]);
    setupSeries('dpu-b', [sampleB]);

    const { rerender } = render(<CounterWidget dpuId="dpu-a" />);
    expect(screen.getByText('1,000')).toBeInTheDocument();
    expect(screen.getByText('42')).toBeInTheDocument();

    rerender(<CounterWidget dpuId="dpu-b" />);
    expect(screen.getByText('5,000')).toBeInTheDocument();
    expect(screen.getByText('99')).toBeInTheDocument();
  });

  it('uses toLocaleString() for large number formatting', () => {
    const sample: CounterSample = {
      at: Date.now(), vxlan_decap: 1234567, vxlan_encap: 9876543, drop_acl_in: 555555, flows_created_total: 0, flow_table_size: 100000,
    };
    setupSeries('dpu-large', [sample]);
    render(<CounterWidget dpuId="dpu-large" />);

    expect(screen.getByText('1,234,567')).toBeInTheDocument();
    expect(screen.getByText('9,876,543')).toBeInTheDocument();
    expect(screen.getByText('555,555')).toBeInTheDocument();
    expect(screen.getByText('100,000')).toBeInTheDocument();
  });

  it('displays latest sample values (not first sample)', () => {
    const samples: CounterSample[] = [
      { at: Date.now() - 5000, vxlan_decap: 100, vxlan_encap: 200, drop_acl_in: 50, flows_created_total: 0, flow_table_size: 10 },
      { at: Date.now(), vxlan_decap: 999, vxlan_encap: 888, drop_acl_in: 777, flows_created_total: 0, flow_table_size: 998 },
    ];
    setupSeries('dpu-latest', samples);
    render(<CounterWidget dpuId="dpu-latest" />);

    expect(screen.getByText('999')).toBeInTheDocument();
    expect(screen.getByText('888')).toBeInTheDocument();
    expect(screen.getByText('777')).toBeInTheDocument();
  });
});
