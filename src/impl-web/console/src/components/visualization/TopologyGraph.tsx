import { useMemo } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  type Node,
  type Edge,
  Position,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import type { TopologyGraph as TopologyGraphData } from "@/api/types";

interface TopologyGraphProps {
  data: TopologyGraphData | undefined;
  height?: number;
  onNodeClick?: (nodeId: string, type: string) => void;
}

const NODE_STYLE: Record<string, { bg: string; border: string }> = {
  dpu: {
    bg: "rgba(0, 212, 255, 0.10)",
    border: "rgba(0, 212, 255, 0.6)",
  },
  vnet: {
    bg: "rgba(168, 85, 247, 0.10)",
    border: "rgba(168, 85, 247, 0.6)",
  },
  eni: {
    bg: "rgba(16, 185, 129, 0.10)",
    border: "rgba(16, 185, 129, 0.6)",
  },
};

/**
 * Lay nodes out in a simple grid by type.
 * Exported for tests.
 */
export function layoutNodes(data: TopologyGraphData | undefined): Node[] {
  if (!data?.nodes) return [];
  const byType: Record<string, typeof data.nodes> = {
    dpu: [],
    vnet: [],
    eni: [],
  };
  for (const n of data.nodes) {
    const t = n.type ?? "dpu";
    if (!byType[t]) byType[t] = [];
    byType[t]!.push(n);
  }

  const COL_WIDTH = 240;
  const ROW_HEIGHT = 90;
  const TYPE_ORDER = ["vnet", "dpu", "eni"] as const;

  const out: Node[] = [];
  TYPE_ORDER.forEach((type, colIdx) => {
    (byType[type] ?? []).forEach((n, rowIdx) => {
      const style = NODE_STYLE[type] ?? NODE_STYLE.dpu;
      out.push({
        id: n.id,
        position: { x: colIdx * COL_WIDTH, y: rowIdx * ROW_HEIGHT },
        data: { label: n.label ?? n.id },
        type: "default",
        sourcePosition: Position.Right,
        targetPosition: Position.Left,
        style: {
          background: style!.bg,
          border: `1px solid ${style!.border}`,
          borderRadius: 8,
          padding: 8,
          fontSize: 11,
          color: "#e8eef9",
          width: 200,
        },
      });
    });
  });
  return out;
}

/** Convert topology edges to xyflow Edge form. Exported for tests. */
export function buildEdges(data: TopologyGraphData | undefined): Edge[] {
  if (!data?.edges) return [];
  return data.edges.map((e, i) => ({
    id: `e-${i}-${e.source}-${e.target}`,
    source: e.source,
    target: e.target,
    label: e.label,
    animated: false,
    style: { stroke: "rgba(255,255,255,0.18)", strokeWidth: 1 },
    labelStyle: { fill: "#94a3b8", fontSize: 10 },
  }));
}

export function TopologyGraph({
  data,
  height = 480,
  onNodeClick,
}: TopologyGraphProps) {
  const nodes = useMemo(() => layoutNodes(data), [data]);
  const edges = useMemo(() => buildEdges(data), [data]);

  if (!data || nodes.length === 0) {
    return (
      <div
        className="flex items-center justify-center text-[color:var(--text-muted)] text-sm"
        style={{ height }}
        role="img"
        aria-label="Empty topology graph"
      >
        No topology data available
      </div>
    );
  }

  return (
    <div
      style={{ height }}
      data-testid="topology-graph"
      className="rounded-md overflow-hidden border border-[color:var(--border-subtle)]"
    >
      <ReactFlow
        nodes={nodes}
        edges={edges}
        fitView
        proOptions={{ hideAttribution: true }}
        onNodeClick={(_e, node) => {
          const topoNode = data.nodes.find((n) => n.id === node.id);
          if (topoNode && onNodeClick) onNodeClick(node.id, topoNode.type);
        }}
        nodesDraggable={false}
        nodesConnectable={false}
      >
        <Background color="rgba(255,255,255,0.04)" gap={16} />
        <Controls
          showInteractive={false}
          className="!bg-[color:var(--bg-elevated)] !border-[color:var(--border-subtle)]"
        />
      </ReactFlow>
    </div>
  );
}