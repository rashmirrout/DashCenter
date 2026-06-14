import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { Tabs, type TabDef } from "../src/components/layout/Tabs";

const sampleTabs: TabDef[] = [
  { id: "a", label: "Alpha", content: <div>alpha-content</div> },
  { id: "b", label: "Beta", badge: 3, content: <div>beta-content</div> },
  { id: "c", label: "Gamma", content: <div>gamma-content</div>, disabled: true },
];

describe("Tabs", () => {
  it("renders all tab labels", () => {
    render(<Tabs tabs={sampleTabs} />);
    expect(screen.getByText("Alpha")).toBeInTheDocument();
    expect(screen.getByText("Beta")).toBeInTheDocument();
    expect(screen.getByText("Gamma")).toBeInTheDocument();
  });

  it("renders the first tab body by default", () => {
    render(<Tabs tabs={sampleTabs} />);
    expect(screen.getByText("alpha-content")).toBeInTheDocument();
  });

  it("respects defaultTabId", () => {
    render(<Tabs tabs={sampleTabs} defaultTabId="b" />);
    expect(screen.getByText("beta-content")).toBeInTheDocument();
  });

  it("switches tabs on click and fires onChange", () => {
    const onChange = vi.fn();
    render(<Tabs tabs={sampleTabs} onChange={onChange} />);
    fireEvent.click(screen.getByText("Beta"));
    expect(screen.getByText("beta-content")).toBeInTheDocument();
    expect(onChange).toHaveBeenCalledWith("b");
  });

  it("renders badge counts", () => {
    render(<Tabs tabs={sampleTabs} />);
    expect(screen.getByText("3")).toBeInTheDocument();
  });

  it("disables tabs with disabled=true", () => {
    render(<Tabs tabs={sampleTabs} />);
    const gamma = screen.getByText("Gamma").closest("button");
    expect(gamma).toBeDisabled();
  });

  it("does not switch on click of a disabled tab", () => {
    const onChange = vi.fn();
    render(<Tabs tabs={sampleTabs} onChange={onChange} />);
    const gamma = screen.getByText("Gamma").closest("button")!;
    fireEvent.click(gamma);
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByText("alpha-content")).toBeInTheDocument();
  });

  it("supports controlled mode via activeTabId", () => {
    const { rerender } = render(
      <Tabs tabs={sampleTabs} activeTabId="a" onChange={() => {}} />
    );
    expect(screen.getByText("alpha-content")).toBeInTheDocument();
    rerender(<Tabs tabs={sampleTabs} activeTabId="b" onChange={() => {}} />);
    expect(screen.getByText("beta-content")).toBeInTheDocument();
  });

  it("ArrowRight navigates to next enabled tab", () => {
    const onChange = vi.fn();
    render(<Tabs tabs={sampleTabs} onChange={onChange} />);
    const alpha = screen.getByText("Alpha").closest("button")!;
    fireEvent.keyDown(alpha, { key: "ArrowRight" });
    expect(onChange).toHaveBeenCalledWith("b");
  });

  it("ArrowLeft wraps to last enabled tab", () => {
    const onChange = vi.fn();
    render(<Tabs tabs={sampleTabs} onChange={onChange} />);
    const alpha = screen.getByText("Alpha").closest("button")!;
    fireEvent.keyDown(alpha, { key: "ArrowLeft" });
    // Gamma is disabled, so last enabled is Beta
    expect(onChange).toHaveBeenCalledWith("b");
  });

  it("Home jumps to first tab", () => {
    const onChange = vi.fn();
    render(<Tabs tabs={sampleTabs} defaultTabId="b" onChange={onChange} />);
    const beta = screen.getByText("Beta").closest("button")!;
    fireEvent.keyDown(beta, { key: "Home" });
    expect(onChange).toHaveBeenCalledWith("a");
  });

  it("End jumps to last enabled tab", () => {
    const onChange = vi.fn();
    render(<Tabs tabs={sampleTabs} onChange={onChange} />);
    const alpha = screen.getByText("Alpha").closest("button")!;
    fireEvent.keyDown(alpha, { key: "End" });
    expect(onChange).toHaveBeenCalledWith("b");
  });

  it("uses tablist/tab/tabpanel ARIA roles", () => {
    render(<Tabs tabs={sampleTabs} />);
    expect(screen.getByRole("tablist")).toBeInTheDocument();
    expect(screen.getAllByRole("tab")).toHaveLength(3);
    expect(screen.getByRole("tabpanel")).toBeInTheDocument();
  });

  it("supports pill variant", () => {
    const { container } = render(<Tabs tabs={sampleTabs} variant="pill" />);
    const tablist = container.querySelector('[role="tablist"]')!;
    // pill variant should NOT add border-b on tablist
    expect(tablist.className).not.toContain("border-b");
  });
});