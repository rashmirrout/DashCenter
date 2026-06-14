// EniStatisticsView tests — covers DPU tree, ENI selection, Pull
// button, streaming toggle, and counter-panel data flow.
//
// Strategy: stub `fetch` to feed both the topology snapshot and the
// counter-details endpoint. No real network. Uses MemoryRouter +
// fresh QueryClient per test (retry: false, no caching across tests).

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import EniStatisticsView from '../src/views/eni-stats/EniStatisticsView';

type StubResponses = {
  topology?: unknown;
  details?: Record<string, unknown>;
  detailsStatus?: Record<string, number>; // dpuId → status code
};

let stubs: StubResponses = {};
const detailsCallCount = { n: 0 };

let deleteCallLog: string[] = [];

function installFetchStub() {
  detailsCallCount.n = 0;
  deleteCallLog = [];
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString();
    const method = (init?.method ?? 'GET').toUpperCase();

    // ── DELETE /api/v1/observability/counters[/{id}][?reset_sim=true] ──
    if (method === 'DELETE' && url.startsWith('/api/v1/observability/counters')) {
      const dpuMatch = url.match(/\/api\/v1\/observability\/counters\/([^/?]+)/);
      if (dpuMatch) {
        const id = decodeURIComponent(dpuMatch[1]!);
        deleteCallLog.push(id);
        return new Response(JSON.stringify({ cleared: true, dpu_id: id }), {
          status: 200,
          headers: { 'content-type': 'application/json' },
        });
      }
      deleteCallLog.push('__ALL__');
      return new Response(JSON.stringify({ cleared: 5 }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      });
    }

    if (url.startsWith('/api/console/topology-v2')) {
      return new Response(JSON.stringify(stubs.topology ?? { appliances: [] }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      });
    }
    const m = url.match(/^\/api\/v1\/observability\/counters\/([^/]+)\/details/);
    if (m) {
      detailsCallCount.n++;
      const dpu = decodeURIComponent(m[1]!);
      const status = stubs.detailsStatus?.[dpu] ?? 200;
      if (status === 404) {
        return new Response(JSON.stringify({ error: 'not found' }), {
          status: 404,
          headers: { 'content-type': 'application/json' },
        });
      }
      const body = stubs.details?.[dpu] ?? { dpu_id: dpu };
      return new Response(JSON.stringify(body), {
        status,
        headers: { 'content-type': 'application/json' },
      });
    }
    return new Response('not found', { status: 404 });
  }) as unknown as typeof globalThis.fetch;
}

function wrap(node: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{node}</MemoryRouter>
    </QueryClientProvider>,
  );
}

const sampleTopology = {
  appliances: [
    {
      id: 'app-1',
      dpus: [
        {
          id: 'dpu-sim-01',
          state: 'DPU_STATE_UP',
          eni_count: 2,
          enis: [
            { name: 'eni-bank-web-01', namespace: 'default', vnet_name: 'bank-prod-web', mac_address: 'aa:bb:cc:00:01:01' },
            { name: 'eni-bank-web-02', namespace: 'default', vnet_name: 'bank-prod-web' },
          ],
        },
        {
          id: 'dpu-sim-02',
          state: 'DPU_STATE_UP',
          eni_count: 1,
          enis: [
            { name: 'eni-other-01', namespace: 'default' },
          ],
        },
      ],
    },
  ],
};

const sampleDetails = {
  'dpu-sim-01': {
    dpu_id: 'dpu-sim-01',
    update_at: '2026-06-14T07:50:00Z',
    report: { dpu_id: 'dpu-sim-01', vxlan_decap: '12345', vxlan_encap: '23456', drop_acl_in: '7', flow_table_size: '4' },
    per_eni: {
      'eni-bank-web-01': { vxlan_decap: '5000', vxlan_encap: '6000', drop_acl_in: '2', flow_table_size: '2' },
      'eni-bank-web-02': { vxlan_decap: '3000', vxlan_encap: '4000', drop_acl_in: '5', flow_table_size: '1' },
    },
    per_vnet: { 'bank-prod-web': { vxlan_decap: '8000' } },
  },
};

beforeEach(() => {
  installFetchStub();
  stubs = {};
  try {
    window.localStorage.clear();
  } catch {
    /* ignore */
  }
});

afterEach(() => {
  vi.useRealTimers();
});

describe('EniStatisticsView · topology tree', () => {
  it('renders empty state when topology has no appliances', async () => {
    stubs.topology = { appliances: [] };
    wrap(<EniStatisticsView />);
    expect(await screen.findByTestId('topology-empty')).toBeInTheDocument();
  });

  it('renders DPU rows from the topology snapshot', async () => {
    stubs.topology = sampleTopology;
    wrap(<EniStatisticsView />);
    expect(await screen.findByTestId('dpu-row-dpu-sim-01')).toBeInTheDocument();
    expect(screen.getByTestId('dpu-row-dpu-sim-02')).toBeInTheDocument();
  });

  it('does NOT show ENIs until DPU is expanded', async () => {
    stubs.topology = sampleTopology;
    wrap(<EniStatisticsView />);
    await screen.findByTestId('dpu-row-dpu-sim-01');
    expect(screen.queryByTestId('eni-row-dpu-sim-01-eni-bank-web-01')).toBeNull();
  });

  it('clicking a DPU row expands to show its ENIs', async () => {
    stubs.topology = sampleTopology;
    wrap(<EniStatisticsView />);
    const dpuRow = await screen.findByTestId('dpu-row-dpu-sim-01');
    fireEvent.click(dpuRow);
    expect(await screen.findByTestId('eni-row-dpu-sim-01-eni-bank-web-01')).toBeInTheDocument();
    expect(screen.getByTestId('eni-row-dpu-sim-01-eni-bank-web-02')).toBeInTheDocument();
  });
});

describe('EniStatisticsView · ENI selection & panel', () => {
  it('shows the "select an ENI" hint when nothing is selected', async () => {
    stubs.topology = sampleTopology;
    wrap(<EniStatisticsView />);
    expect(await screen.findByTestId('no-eni-selected')).toBeInTheDocument();
  });

  it('clicking an ENI opens the panel with metadata + per-ENI counters', async () => {
    stubs.topology = sampleTopology;
    stubs.details = sampleDetails;
    wrap(<EniStatisticsView />);
    fireEvent.click(await screen.findByTestId('dpu-row-dpu-sim-01'));
    fireEvent.click(await screen.findByTestId('eni-row-dpu-sim-01-eni-bank-web-01'));

    // MAC address is only rendered in the panel — unique anchor.
    expect(await screen.findByText('aa:bb:cc:00:01:01')).toBeInTheDocument();

    // Per-ENI counters (formatted with toLocaleString) appear only in
    // the panel; both rollup and per-ENI render numbers but with
    // different values so we can pick per-ENI-specific ones.
    expect(await screen.findByText('5,000')).toBeInTheDocument();
    expect(screen.getByText('6,000')).toBeInTheDocument();

    // The ENI name is in BOTH the tree row and the panel heading;
    // assert >= 2 instances exist (one in tree, one in panel).
    const matches = screen.getAllByText('eni-bank-web-01');
    expect(matches.length).toBeGreaterThanOrEqual(2);
  });

  it('shows empty-state when /details has no per_eni for the selected ENI', async () => {
    stubs.topology = sampleTopology;
    stubs.details = { 'dpu-sim-02': { dpu_id: 'dpu-sim-02', per_eni: {}, report: {} } };
    wrap(<EniStatisticsView />);
    fireEvent.click(await screen.findByTestId('dpu-row-dpu-sim-02'));
    fireEvent.click(await screen.findByTestId('eni-row-dpu-sim-02-eni-other-01'));
    expect(await screen.findByTestId('counter-panel-empty')).toBeInTheDocument();
  });

  it('selecting an ENI auto-expands its parent DPU', async () => {
    stubs.topology = sampleTopology;
    stubs.details = sampleDetails;
    wrap(<EniStatisticsView />);
    fireEvent.click(await screen.findByTestId('dpu-row-dpu-sim-01'));
    fireEvent.click(await screen.findByTestId('eni-row-dpu-sim-01-eni-bank-web-01'));
    // dpu-sim-02 is still collapsed (auto-expand only touches selected parent)
    expect(screen.queryByTestId('eni-row-dpu-sim-02-eni-other-01')).toBeNull();
  });
});

describe('EniStatisticsView · streaming toggle + Pull button', () => {
  it('Pull button is disabled until an ENI is selected', async () => {
    stubs.topology = sampleTopology;
    wrap(<EniStatisticsView />);
    const pull = await screen.findByTestId('pull-button');
    expect(pull).toBeDisabled();
  });

  it('Pull button is enabled after ENI selection', async () => {
    stubs.topology = sampleTopology;
    stubs.details = sampleDetails;
    wrap(<EniStatisticsView />);
    fireEvent.click(await screen.findByTestId('dpu-row-dpu-sim-01'));
    fireEvent.click(await screen.findByTestId('eni-row-dpu-sim-01-eni-bank-web-01'));
    const pull = await screen.findByTestId('pull-button');
    await waitFor(() => expect(pull).not.toBeDisabled());
  });

  it('streaming badge starts "Off" and toggles to "Live streaming"', async () => {
    stubs.topology = sampleTopology;
    wrap(<EniStatisticsView />);
    const badge = await screen.findByTestId('streaming-badge');
    expect(badge.textContent).toContain('Off');

    fireEvent.click(screen.getByTestId('streaming-toggle'));
    expect(badge.textContent).toContain('Live streaming');
  });

  it('streaming toggle preference persists in localStorage', async () => {
    stubs.topology = sampleTopology;
    wrap(<EniStatisticsView />);
    await screen.findByTestId('streaming-toggle');
    fireEvent.click(screen.getByTestId('streaming-toggle'));
    expect(window.localStorage.getItem('eni-stats:streaming')).toBe('1');

    fireEvent.click(screen.getByTestId('streaming-toggle'));
    expect(window.localStorage.getItem('eni-stats:streaming')).toBe('0');
  });

  it('with streaming OFF the /details endpoint is hit once per selection (no auto-refresh)', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    stubs.topology = sampleTopology;
    stubs.details = sampleDetails;
    wrap(<EniStatisticsView />);
    fireEvent.click(await screen.findByTestId('dpu-row-dpu-sim-01'));
    fireEvent.click(await screen.findByTestId('eni-row-dpu-sim-01-eni-bank-web-01'));

    // Wait for initial fetch.
    await waitFor(() => expect(detailsCallCount.n).toBe(1));

    // Advance 10s — no refetch.
    await act(async () => {
      vi.advanceTimersByTime(10_000);
    });
    expect(detailsCallCount.n).toBe(1);
  });
});

describe('EniStatisticsView · error paths', () => {
  it('shows topology error when /api/console/topology-v2 fails', async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response('boom', { status: 500 }),
    ) as unknown as typeof globalThis.fetch;
    wrap(<EniStatisticsView />);
    expect(await screen.findByTestId('topology-error')).toBeInTheDocument();
  });

  it('treats 404 from /details as an empty-state, not an error', async () => {
    stubs.topology = sampleTopology;
    stubs.detailsStatus = { 'dpu-sim-01': 404 };
    wrap(<EniStatisticsView />);
    fireEvent.click(await screen.findByTestId('dpu-row-dpu-sim-01'));
    fireEvent.click(await screen.findByTestId('eni-row-dpu-sim-01-eni-bank-web-01'));
    expect(await screen.findByTestId('counter-panel-empty')).toBeInTheDocument();
  });
});

describe('EniStatisticsView · clear buttons', () => {
  it('Clear DPU button is disabled until an ENI is selected', async () => {
    stubs.topology = sampleTopology;
    wrap(<EniStatisticsView />);
    const btn = await screen.findByTestId('clear-selected-button');
    expect(btn).toBeDisabled();
  });

  it('Clear DPU calls DELETE /api/v1/observability/counters/{dpu_id} for the selected ENI\'s parent DPU', async () => {
    stubs.topology = sampleTopology;
    stubs.details = sampleDetails;
    wrap(<EniStatisticsView />);
    fireEvent.click(await screen.findByTestId('dpu-row-dpu-sim-01'));
    fireEvent.click(await screen.findByTestId('eni-row-dpu-sim-01-eni-bank-web-01'));
    // Wait for the details panel.
    await screen.findByText('aa:bb:cc:00:01:01');
    const btn = screen.getByTestId('clear-selected-button');
    await waitFor(() => expect(btn).not.toBeDisabled());

    await act(async () => {
      fireEvent.click(btn);
    });

    expect(deleteCallLog).toContain('dpu-sim-01');
    // A message banner should appear.
    expect(await screen.findByTestId('clear-msg')).toBeInTheDocument();
  });

  it('Clear all button calls DELETE /api/v1/observability/counters (after confirm)', async () => {
    stubs.topology = sampleTopology;
    stubs.details = sampleDetails;
    vi.stubGlobal('confirm', () => true);
    wrap(<EniStatisticsView />);
    // Can click Clear all without selecting an ENI.
    const btn = await screen.findByTestId('clear-all-button');
    expect(btn).not.toBeDisabled();

    await act(async () => {
      fireEvent.click(btn);
    });

    expect(deleteCallLog).toContain('__ALL__');
    expect(await screen.findByTestId('clear-msg')).toBeInTheDocument();
    vi.unstubAllGlobals();
  });

  it('Clear all does NOT call DELETE when user cancels the confirm', async () => {
    stubs.topology = sampleTopology;
    vi.stubGlobal('confirm', () => false);
    wrap(<EniStatisticsView />);
    const btn = await screen.findByTestId('clear-all-button');

    await act(async () => {
      fireEvent.click(btn);
    });

    expect(deleteCallLog).toHaveLength(0);
    vi.unstubAllGlobals();
  });
});
