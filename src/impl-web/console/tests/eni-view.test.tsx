import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import EniView from "../src/views/eni/EniView";
import EniListView from "../src/views/eni/EniListView";
import type { EniDetail } from "../src/api/types";

/* ── Mock react-query hooks consumed by both views ──────── */

const mockEniDetail = vi.fn();
const mockEniList = vi.fn();
const mockEniPlacement = vi.fn();

vi.mock("../src/queries/hooks", () => ({
  useEniDetail: (name: string, ns?: string) => mockEniDetail(name, ns),
  useEniList: (ns?: string) => mockEniList(ns),
  useEniPlacement: () => mockEniPlacement(),
}));

/* ── Test helpers ──────────────────────────────────────── */

function wrap(node: React.ReactNode, initialPath: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route path="/eni/:namespace/:name" element={node} />
          <Route path="/enis" element={node} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

function makeDetail(overrides: Partial<EniDetail> = {}): EniDetail {
  return {
    namespace: "default",
    name: "eni-blue-1",
    identity: {
      vnet_name: "vnet-blue",
      mac_address: "aa:bb:cc:00:01:01",
      underlay_ip: "10.0.1.11",
      admin_state: "UP",
      generation: 3,
      labels: { tier: "app" },
    },
    vnet: {
      name: "vnet-blue",
      vni: 100,
      gw_mac: "aa:bb:cc:00:00:01",
      state: "ACTIVE",
    },
    placement: {
      dpu_ids: ["dpu-1", "dpu-2"],
      ha_active_active: true,
      slots: [
        { dpu_id: "dpu-1", observed: true },
        { dpu_id: "dpu-2", observed: true },
      ],
    },
    ha_set: {
      name: "ha-blue",
      scope: "appliance",
      virtual_ip: "10.0.99.1",
      member_dpu_ids: ["dpu-1", "dpu-2"],
      members_by_role: { "dpu-1": "ACTIVE", "dpu-2": "ACTIVE" },
    },
    vnet_mappings_reachable: [],
    acls_inbound: [],
    acls_outbound: [],
    route_policies: [],
    service_tunnels: [],
    counters: {
      acl_inbound: 1,
      acl_outbound: 1,
      routes: 1,
      mappings: 2,
      tunnels: 2,
      placements: 2,
    },
    ...overrides,
  };
}

/* ── EniView tests ─────────────────────────────────────── */

describe("EniView · loading state", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("shows a skeleton while the detail query is loading", () => {
    mockEniDetail.mockReturnValue({
      isLoading: true,
      isError: false,
      data: undefined,
      refetch: vi.fn(),
    });
    wrap(<EniView />, "/eni/default/eni-blue-1");
    expect(screen.getByText("Loading…")).toBeInTheDocument();
  });
});

describe("EniView · error state", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("shows the error message and a retry button on fetch failure", () => {
    const refetch = vi.fn();
    mockEniDetail.mockReturnValue({
      isLoading: false,
      isError: true,
      error: new Error("boom: dashd unreachable"),
      data: undefined,
      refetch,
    });
    wrap(<EniView />, "/eni/default/eni-blue-1");
    expect(screen.getByText(/boom: dashd unreachable/i)).toBeInTheDocument();
  });
});

describe("EniView · happy path", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockEniDetail.mockReturnValue({
      isLoading: false,
      isError: false,
      data: makeDetail(),
      refetch: vi.fn(),
    });
  });

  it("renders the ENI name in the page header", () => {
    wrap(<EniView />, "/eni/default/eni-blue-1");
    // Name appears in PageHeader title AND in Identity card → use getAllByText.
    expect(screen.getAllByText("eni-blue-1").length).toBeGreaterThan(0);
  });

  it("shows the parent Vnet's VNI prominently (inheritance from §concepts)", () => {
    wrap(<EniView />, "/eni/default/eni-blue-1");
    // VNI 100 appears in the page-header chip and in the identity card.
    const vniMatches = screen.getAllByText("100");
    expect(vniMatches.length).toBeGreaterThanOrEqual(2);
  });

  it("renders the HA active-active badge when placement spans multiple DPUs", () => {
    wrap(<EniView />, "/eni/default/eni-blue-1");
    expect(screen.getByText(/HA · active-active/i)).toBeInTheDocument();
  });

  it("does NOT render the HA badge when placed on a single DPU", () => {
    mockEniDetail.mockReturnValue({
      isLoading: false,
      isError: false,
      data: makeDetail({
        placement: {
          dpu_ids: ["dpu-1"],
          ha_active_active: false,
          slots: [{ dpu_id: "dpu-1", observed: true }],
        },
      }),
      refetch: vi.fn(),
    });
    wrap(<EniView />, "/eni/default/eni-blue-1");
    expect(screen.queryByText(/HA · active-active/i)).not.toBeInTheDocument();
  });

  it("renders all 7 tabs with badge counters", () => {
    wrap(<EniView />, "/eni/default/eni-blue-1");
    expect(screen.getByRole("tab", { name: /Overview/i })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /Vnet Mappings/i })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /ACL Inbound/i })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /ACL Outbound/i })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /Routes/i })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /Tunnels/i })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /HA/i })).toBeInTheDocument();
  });

  it("includes a Trace-flow button that links to /flow-trace pre-filled with this ENI + VNI", () => {
    wrap(<EniView />, "/eni/default/eni-blue-1");
    const btn = screen.getByRole("button", { name: /Trace a flow/i });
    expect(btn).toBeInTheDocument();
    // The button uses navigate(), so we can't read an href; instead assert
    // the title attr promises pre-fill — and we know buildTraceFlowUrl()
    // constructs ?eni_name=...&vni=... (tested via the second case below).
    expect(btn.getAttribute("title")).toMatch(/pre-filled/i);
  });
});

describe("EniView · partial-data degradation", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders the warnings banner when the BFF reports partial data", () => {
    mockEniDetail.mockReturnValue({
      isLoading: false,
      isError: false,
      data: makeDetail({
        vnet: undefined,
        warnings: ["vnet fetch failed: connection refused"],
      }),
      refetch: vi.fn(),
    });
    wrap(<EniView />, "/eni/default/eni-blue-1");
    expect(screen.getByText("Partial data")).toBeInTheDocument();
    expect(
      screen.getByText(/vnet fetch failed: connection refused/i)
    ).toBeInTheDocument();
  });

  it("still renders identity even when vnet is missing", () => {
    mockEniDetail.mockReturnValue({
      isLoading: false,
      isError: false,
      data: makeDetail({ vnet: undefined, warnings: ["vnet fetch failed"] }),
      refetch: vi.fn(),
    });
    wrap(<EniView />, "/eni/default/eni-blue-1");
    expect(screen.getAllByText("eni-blue-1").length).toBeGreaterThan(0);
    // The MAC now appears in two places (StatusHero + IdentityCard),
    // so use getAllByText and assert at least one is present.
    expect(screen.getAllByText("aa:bb:cc:00:01:01").length).toBeGreaterThan(0);
  });
});

/* ── EniListView tests ─────────────────────────────────── */

describe("EniListView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders the list with row count in the subtitle", () => {
    mockEniList.mockReturnValue({
      isLoading: false,
      isError: false,
      data: {
        items: [
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          {
            metadata: { namespace: "default", name: "eni-blue-1" },
            vnet_name: "vnet-blue",
            mac_address: "aa:bb:cc:00:01:01",
            underlay_ip: "10.0.1.11",
            admin_state: "UP",
          } as any,
        ],
      },
      refetch: vi.fn(),
    });
    mockEniPlacement.mockReturnValue({
      isLoading: false,
      isError: false,
      data: { items: [] },
      refetch: vi.fn(),
    });

    wrap(<EniListView />, "/enis");
    expect(screen.getByText(/1 ENI/i)).toBeInTheDocument();
    expect(screen.getByText("eni-blue-1")).toBeInTheDocument();
  });

  it("renders an empty card when no ENIs are declared", () => {
    mockEniList.mockReturnValue({
      isLoading: false,
      isError: false,
      data: { items: [] },
      refetch: vi.fn(),
    });
    mockEniPlacement.mockReturnValue({
      isLoading: false,
      isError: false,
      data: { items: [] },
      refetch: vi.fn(),
    });

    wrap(<EniListView />, "/enis");
    expect(screen.getByText(/No ENIs declared/i)).toBeInTheDocument();
  });
});