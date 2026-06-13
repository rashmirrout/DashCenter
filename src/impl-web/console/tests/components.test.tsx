import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  pulseClassForStatus,
  StatusBadge,
} from "../src/components/feedback/StatusBadge";
import { GlassCard } from "../src/components/feedback/GlassCard";
import Sidebar, {
  NAV_GROUPS,
  isPathActive,
} from "../src/components/layout/Sidebar";
import TopBar, {
  pathToCrumbs,
  humanize,
} from "../src/components/layout/TopBar";

// ── Mock react-query hooks consumed by TopBar ──────────────
vi.mock("../src/queries/hooks", () => ({
  useDashdHealth: () => ({
    data: { status: "ok", leader: true, dpus: [] },
    isError: false,
  }),
}));

function withRouter(node: React.ReactNode, initialPath = "/dashboard") {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initialPath]}>{node}</MemoryRouter>
    </QueryClientProvider>
  );
}

/* ──────────── StatusBadge ──────────── */

describe("StatusBadge · pulseClassForStatus", () => {
  it("returns slow pulse for healthy states", () => {
    expect(pulseClassForStatus("DPU_STATE_UP")).toBe("animate-pulse-slow");
    expect(pulseClassForStatus("READY")).toBe("animate-pulse-slow");
    expect(pulseClassForStatus("LEADER")).toBe("animate-pulse-slow");
    expect(pulseClassForStatus("ALLOW")).toBe("animate-pulse-slow");
  });
  it("returns medium pulse for degraded states", () => {
    expect(pulseClassForStatus("DPU_STATE_DEGRADED")).toBe(
      "animate-pulse-medium"
    );
    expect(pulseClassForStatus("DRAINING")).toBe("animate-pulse-medium");
    expect(pulseClassForStatus("FOLLOWER")).toBe("animate-pulse-medium");
  });
  it("returns fast pulse for offline states", () => {
    expect(pulseClassForStatus("DPU_STATE_DISCONNECTED")).toBe(
      "animate-pulse-fast"
    );
    expect(pulseClassForStatus("OFFLINE")).toBe("animate-pulse-fast");
    expect(pulseClassForStatus("ERROR")).toBe("animate-pulse-fast");
    expect(pulseClassForStatus("DENY")).toBe("animate-pulse-fast");
  });
  it("returns no pulse for unknown/null/empty", () => {
    expect(pulseClassForStatus("UNKNOWN")).toBe("");
    expect(pulseClassForStatus(null)).toBe("");
    expect(pulseClassForStatus(undefined)).toBe("");
    expect(pulseClassForStatus("")).toBe("");
    expect(pulseClassForStatus("FOOBAR")).toBe("");
  });
});

describe("StatusBadge · render", () => {
  it("renders prettified label by default", () => {
    render(<StatusBadge status="DPU_STATE_UP" />);
    expect(screen.getByTestId("status-badge")).toHaveTextContent("UP");
  });

  it("renders raw label when prettify=false", () => {
    render(<StatusBadge status="DPU_STATE_UP" prettify={false} />);
    expect(screen.getByTestId("status-badge")).toHaveTextContent(
      "DPU_STATE_UP"
    );
  });

  it("renders UNKNOWN for null status", () => {
    render(<StatusBadge status={null} />);
    expect(screen.getByTestId("status-badge")).toHaveTextContent("UNKNOWN");
  });

  it("exposes data-status attribute for tests", () => {
    render(<StatusBadge status="READY" />);
    expect(screen.getByTestId("status-badge")).toHaveAttribute(
      "data-status",
      "READY"
    );
  });
});

/* ──────────── GlassCard ──────────── */

describe("GlassCard", () => {
  it("renders children", () => {
    render(<GlassCard>Hello</GlassCard>);
    expect(screen.getByText("Hello")).toBeInTheDocument();
  });

  it("applies cyan glow when glow=true", () => {
    const { container } = render(<GlassCard glow>Hi</GlassCard>);
    expect(container.firstChild).toHaveClass("glow-cyan");
  });

  it.each([
    ["cyan", "glow-cyan"],
    ["purple", "glow-purple"],
    ["green", "glow-green"],
    ["amber", "glow-amber"],
    ["red", "glow-red"],
  ] as const)("applies %s glow class", (variant, cls) => {
    const { container } = render(<GlassCard glow={variant}>Hi</GlassCard>);
    expect(container.firstChild).toHaveClass(cls);
  });

  it("renders as button when onClick is provided", () => {
    const handle = vi.fn();
    render(
      <GlassCard onClick={handle} ariaLabel="open">
        Hi
      </GlassCard>
    );
    const card = screen.getByRole("button", { name: "open" });
    expect(card).toHaveAttribute("tabindex", "0");
    fireEvent.click(card);
    fireEvent.keyDown(card, { key: "Enter" });
    fireEvent.keyDown(card, { key: " " });
    fireEvent.keyDown(card, { key: "Escape" });
    expect(handle).toHaveBeenCalledTimes(3);
  });

  it("renders as semantic tag when `as` is provided", () => {
    const { container } = render(<GlassCard as="section">Hi</GlassCard>);
    expect(container.firstChild?.nodeName).toBe("SECTION");
  });

  it("does not set role when not clickable", () => {
    const { container } = render(<GlassCard>Hi</GlassCard>);
    expect(container.firstChild).not.toHaveAttribute("role");
  });
});

/* ──────────── Sidebar ──────────── */

describe("Sidebar · NAV_GROUPS", () => {
  it("has the five nav groups in order", () => {
    expect(NAV_GROUPS.map((g) => g.label)).toEqual([
      null,
      "Observe",
      "Diagnostics",
      "Operate",
      "Service",
    ]);
  });

  it("uses LLD paths exactly", () => {
    const flat = NAV_GROUPS.flatMap((g) => g.items.map((i) => i.path));
    expect(flat).toEqual([
      "/dashboard",
      "/fleet",
      "/routing",
      "/tunnels",
      "/policies",
      "/flow-trace",
      "/audit",
      "/health",
      "/admin",
      "/commands",
      "/debug",
      "/topology",
      "/topology-v2",
    ]);
  });
});

describe("Sidebar · isPathActive", () => {
  it("matches exact path", () => {
    expect(isPathActive("/fleet", "/fleet")).toBe(true);
    expect(isPathActive("/dashboard", "/dashboard")).toBe(true);
  });
  it("matches nested for non-dashboard routes", () => {
    expect(isPathActive("/fleet/details", "/fleet")).toBe(true);
    expect(isPathActive("/dpu/dpu-1", "/dpu")).toBe(true);
  });
  it("does not match nested for dashboard", () => {
    expect(isPathActive("/dashboard/x", "/dashboard")).toBe(false);
  });
  it("does not match unrelated paths", () => {
    expect(isPathActive("/health", "/fleet")).toBe(false);
  });
});

describe("Sidebar · render", () => {
  it("renders all top-level nav items", () => {
    render(withRouter(<Sidebar />));
    expect(screen.getByText("Dashboard")).toBeInTheDocument();
    expect(screen.getByText("Fleet")).toBeInTheDocument();
    expect(screen.getByText("Routing")).toBeInTheDocument();
    expect(screen.getByText("Health")).toBeInTheDocument();
    expect(screen.getByText("Admin Ops")).toBeInTheDocument();
  });

  it("renders group labels", () => {
    render(withRouter(<Sidebar />));
    expect(screen.getByText("Observe")).toBeInTheDocument();
    expect(screen.getByText("Diagnostics")).toBeInTheDocument();
    expect(screen.getByText("Operate")).toBeInTheDocument();
  });

  it("has a collapse button", () => {
    render(withRouter(<Sidebar />));
    expect(
      screen.getByRole("button", { name: /collapse sidebar|expand sidebar/i })
    ).toBeInTheDocument();
  });
});

/* ──────────── TopBar ──────────── */

describe("TopBar · pathToCrumbs", () => {
  it("returns single Dashboard crumb at root", () => {
    expect(pathToCrumbs("/")).toEqual([{ label: "Dashboard" }]);
    expect(pathToCrumbs("/dashboard")).toEqual([{ label: "Dashboard" }]);
  });

  it("builds crumbs for nested paths", () => {
    expect(pathToCrumbs("/fleet")).toEqual([
      { label: "Dashboard", to: "/dashboard" },
      { label: "Fleet" },
    ]);
  });

  it("builds crumbs for parameterized routes", () => {
    expect(pathToCrumbs("/dpu/dpu-sim-01")).toEqual([
      { label: "Dashboard", to: "/dashboard" },
      { label: "Dpu", to: "/dpu" },
      { label: "dpu-sim-01" },
    ]);
  });

  it("humanizes multi-word segments", () => {
    expect(pathToCrumbs("/flow-trace")).toEqual([
      { label: "Dashboard", to: "/dashboard" },
      { label: "Flow Trace" },
    ]);
  });
});

describe("TopBar · humanize", () => {
  it("title-cases dashed words", () => {
    expect(humanize("admin-ops")).toBe("Admin Ops");
    expect(humanize("flow-trace")).toBe("Flow Trace");
  });
  it("preserves long ID-like values", () => {
    expect(humanize("dpu-sim-01")).toBe("dpu-sim-01");
    expect(humanize("abc-123-xyz-9999")).toBe("abc-123-xyz-9999");
  });
  it("returns empty for empty input", () => {
    expect(humanize("")).toBe("");
  });
});

describe("TopBar · render", () => {
  it("shows breadcrumb leaf as text", () => {
    render(withRouter(<TopBar />, "/fleet"));
    const breadcrumb = screen.getByLabelText("Breadcrumb");
    expect(breadcrumb).toHaveTextContent("Fleet");
  });

  it("renders WS status indicator (leader → online)", () => {
    render(withRouter(<TopBar />, "/dashboard"));
    expect(screen.getByLabelText(/dashd leader · live/i)).toBeInTheDocument();
  });

  it("renders Cmd+K search trigger", () => {
    render(withRouter(<TopBar />, "/dashboard"));
    expect(screen.getByText("Search…")).toBeInTheDocument();
  });
});