import { describe, it, expect } from "vitest";
import {
  buildEdges,
  layoutNodes,
} from "../src/components/visualization/TopologyGraph";
import type { TopologyGraph as TopologyData } from "../src/api/types";

describe("TopologyGraph · layoutNodes", () => {
  it("returns [] for undefined data", () => {
    expect(layoutNodes(undefined)).toEqual([]);
  });

  it("returns [] for empty nodes", () => {
    expect(layoutNodes({ nodes: [], edges: [] })).toEqual([]);
  });

  it("places vnets in column 0, dpus in column 1, enis in column 2", () => {
    const data: TopologyData = {
      nodes: [
        { id: "v1", type: "vnet", label: "Vnet 1" },
        { id: "d1", type: "dpu", label: "DPU 1" },
        { id: "e1", type: "eni", label: "ENI 1" },
      ],
      edges: [],
    };
    const out = layoutNodes(data);
    expect(out).toHaveLength(3);
    const vnet = out.find((n) => n.id === "v1")!;
    const dpu = out.find((n) => n.id === "d1")!;
    const eni = out.find((n) => n.id === "e1")!;
    expect(vnet.position.x).toBe(0);
    expect(dpu.position.x).toBe(240);
    expect(eni.position.x).toBe(480);
  });

  it("stacks multiple nodes of same type vertically", () => {
    const data: TopologyData = {
      nodes: [
        { id: "v1", type: "vnet", label: "V1" },
        { id: "v2", type: "vnet", label: "V2" },
        { id: "v3", type: "vnet", label: "V3" },
      ],
      edges: [],
    };
    const out = layoutNodes(data);
    expect(out.map((n) => n.position.y)).toEqual([0, 90, 180]);
  });

  it("uses node id when label is missing", () => {
    const data: TopologyData = {
      nodes: [{ id: "x1", type: "dpu", label: "" }],
      edges: [],
    };
    const out = layoutNodes(data);
    expect((out[0]!.data as { label: string }).label).toBe("");
  });

  it("defaults to dpu type styling when type missing", () => {
    const data = {
      nodes: [{ id: "x", label: "X" } as unknown as TopologyData["nodes"][number]],
      edges: [],
    } as TopologyData;
    const out = layoutNodes(data);
    expect(out).toHaveLength(1);
    // dpu lands in col 1 (x=240)
    expect(out[0]!.position.x).toBe(240);
  });
});

describe("TopologyGraph · buildEdges", () => {
  it("returns [] for undefined data", () => {
    expect(buildEdges(undefined)).toEqual([]);
  });

  it("returns [] when edges absent", () => {
    expect(buildEdges({ nodes: [], edges: [] })).toEqual([]);
  });

  it("assigns unique ids", () => {
    const data: TopologyData = {
      nodes: [],
      edges: [
        { source: "a", target: "b" },
        { source: "a", target: "b", label: "second" },
        { source: "c", target: "d" },
      ],
    };
    const out = buildEdges(data);
    const ids = out.map((e) => e.id);
    expect(new Set(ids).size).toBe(ids.length);
    expect(out[1]!.label).toBe("second");
  });

  it("preserves source/target/label", () => {
    const data: TopologyData = {
      nodes: [],
      edges: [{ source: "x", target: "y", label: "link" }],
    };
    const out = buildEdges(data);
    expect(out[0]!.source).toBe("x");
    expect(out[0]!.target).toBe("y");
    expect(out[0]!.label).toBe("link");
  });
});