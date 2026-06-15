/* ═══════════════════════════════════════════════════════════════
 * DependencyMiniMap — visual DAG of the 7 resource kinds.
 *
 * Renders a compact, clickable graph showing how resource kinds
 * depend on each other. The currently-active kind is highlighted
 * with a glow; clicking any node switches to that tab.
 *
 * Layout: 4 columns
 *   col 0: Roots (vnets, service-tunnels, ha-sets)
 *   col 1: First-level (enis, vnet-mappings)
 *   col 2: Second-level (acl-policies)
 *   col 3: Sinks (route-policies)
 *
 * Edges are drawn from upstream (left) to downstream (right) using
 * cubic-bezier paths. Edges incident to the active kind are
 * highlighted in cyan.
 *
 * ═══════════════════════════════════════════════════════════════ */

import { motion } from "framer-motion";
import type { ResourceKind } from "@/lib/constants";
import {
  DEPENDENCY_GRAPH,
  KIND_LABELS_PLURAL,
} from "@/lib/resource-deps";

/* ── Layout config ─────────────────────────────────────────── */

/** Position of each kind in the mini-map (col, row). */
const POSITIONS: Record<ResourceKind, { col: number; row: number }> = {
  vnets: { col: 0, row: 0 },
  "service-tunnels": { col: 0, row: 1 },
  "ha-sets": { col: 0, row: 2 },
  enis: { col: 1, row: 0 },
  "vnet-mappings": { col: 1, row: 1 },
  "acl-policies": { col: 2, row: 0 },
  "route-policies": { col: 3, row: 0 },
};

/** Accent color per kind for the node fill. */
const KIND_ACCENT: Record<ResourceKind, string> = {
  vnets: "#a855f7",            // purple
  "service-tunnels": "#10b981", // green
  "ha-sets": "#f59e0b",         // amber
  enis: "#00d4ff",              // cyan
  "vnet-mappings": "#ec4899",   // pink
  "acl-policies": "#ef4444",    // red
  "route-policies": "#8b5cf6",  // violet
};

/* Geometry — sized so the natural viewBox is wide (~1060px) so the
 * graph fills wide containers via `width="100%"` + default
 * `preserveAspectRatio="xMidYMid meet"`. The previous compact
 * geometry (~560px viewBox) caused the SVG to be centered & shrunk
 * inside wide cards. */
const COL_WIDTH = 250;
const ROW_HEIGHT = 88;
const NODE_W = 200;
const NODE_H = 56;
const PAD_X = 30;
const PAD_Y = 24;

function nodeXY(kind: ResourceKind): { x: number; y: number } {
  const { col, row } = POSITIONS[kind];
  return {
    x: PAD_X + col * COL_WIDTH,
    y: PAD_Y + row * ROW_HEIGHT,
  };
}

function nodeCenter(kind: ResourceKind): { cx: number; cy: number } {
  const { x, y } = nodeXY(kind);
  return { cx: x + NODE_W / 2, cy: y + NODE_H / 2 };
}

/* ── Edges ─────────────────────────────────────────────────── */

interface Edge {
  from: ResourceKind;
  to: ResourceKind;
}

/** Build all edges from the dependency graph. */
function buildEdges(): Edge[] {
  const edges: Edge[] = [];
  for (const [k, deps] of Object.entries(DEPENDENCY_GRAPH)) {
    for (const dep of deps) {
      // Edge from upstream (dep) → downstream (k)
      edges.push({ from: dep as ResourceKind, to: k as ResourceKind });
    }
  }
  return edges;
}

const EDGES = buildEdges();

/** Build a cubic-bezier path from upstream to downstream node. */
function edgePath(edge: Edge): string {
  const from = nodeCenter(edge.from);
  const to = nodeCenter(edge.to);
  // Start at the right edge of `from`, end at the left edge of `to`
  const x1 = from.cx + NODE_W / 2;
  const y1 = from.cy;
  const x2 = to.cx - NODE_W / 2;
  const y2 = to.cy;
  // Control points push the curve horizontally so it looks like a smooth arc
  const dx = (x2 - x1) * 0.5;
  return `M ${x1} ${y1} C ${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`;
}

/* ── Component ─────────────────────────────────────────────── */

interface DependencyMiniMapProps {
  /** The currently-selected kind, highlighted in the map. */
  activeKind: ResourceKind;
  /** Callback when the user clicks a node. */
  onSelectKind: (kind: ResourceKind) => void;
  /** Live counts per kind (rendered as a small badge in each node). */
  counts: Partial<Record<ResourceKind, number | null>>;
}

export function DependencyMiniMap({
  activeKind,
  onSelectKind,
  counts,
}: DependencyMiniMapProps) {
  const maxCol = Math.max(...Object.values(POSITIONS).map((p) => p.col));
  const maxRow = Math.max(...Object.values(POSITIONS).map((p) => p.row));
  const svgWidth = PAD_X * 2 + (maxCol + 1) * COL_WIDTH;
  const svgHeight = PAD_Y * 2 + (maxRow + 1) * ROW_HEIGHT;

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <h3 className="text-xs font-semibold uppercase tracking-wider text-[color:var(--text-secondary)]">
          Dependency Graph
        </h3>
        <span className="text-[10px] text-[color:var(--text-muted)] italic">
          Arrows show resource references · click a node to switch tabs
        </span>
      </div>
      <div className="relative">
        <svg
          width="100%"
          height={svgHeight}
          viewBox={`0 0 ${svgWidth} ${svgHeight}`}
          className="overflow-visible"
          role="img"
          aria-label="Resource dependency graph"
        >
          <defs>
            {/* Arrowhead markers */}
            <marker
              id="arrow-dim"
              markerWidth="8"
              markerHeight="8"
              refX="7"
              refY="4"
              orient="auto"
              markerUnits="strokeWidth"
            >
              <path d="M0,0 L8,4 L0,8 z" fill="rgba(255,255,255,0.25)" />
            </marker>
            <marker
              id="arrow-active"
              markerWidth="8"
              markerHeight="8"
              refX="7"
              refY="4"
              orient="auto"
              markerUnits="strokeWidth"
            >
              <path d="M0,0 L8,4 L0,8 z" fill="#00d4ff" />
            </marker>
          </defs>

          {/* Edges */}
          {EDGES.map((e, i) => {
            const isActive = e.from === activeKind || e.to === activeKind;
            return (
              <motion.path
                key={`${e.from}->${e.to}`}
                d={edgePath(e)}
                fill="none"
                stroke={isActive ? "#00d4ff" : "rgba(255,255,255,0.18)"}
                strokeWidth={isActive ? 1.5 : 1}
                markerEnd={isActive ? "url(#arrow-active)" : "url(#arrow-dim)"}
                initial={{ pathLength: 0, opacity: 0 }}
                animate={{ pathLength: 1, opacity: 1 }}
                transition={{
                  duration: 0.8,
                  delay: 0.1 + i * 0.05,
                  ease: "easeOut",
                }}
              />
            );
          })}

          {/* Nodes */}
          {(Object.keys(POSITIONS) as ResourceKind[]).map((kind, idx) => {
            const { x, y } = nodeXY(kind);
            const isActive = kind === activeKind;
            const accent = KIND_ACCENT[kind];
            const count = counts[kind];
            return (
              <motion.g
                key={kind}
                onClick={() => onSelectKind(kind)}
                style={{ cursor: "pointer" }}
                initial={{ opacity: 0, scale: 0.8 }}
                animate={{ opacity: 1, scale: 1 }}
                transition={{ duration: 0.4, delay: idx * 0.05 }}
                role="button"
                aria-label={`${KIND_LABELS_PLURAL[kind]} — ${count ?? "loading"} items`}
              >
                {/* Glow halo for active node */}
                {isActive && (
                  <rect
                    x={x - 3}
                    y={y - 3}
                    width={NODE_W + 6}
                    height={NODE_H + 6}
                    rx={8}
                    fill="none"
                    stroke={accent}
                    strokeWidth={2}
                    opacity={0.6}
                    style={{
                      filter: `drop-shadow(0 0 8px ${accent})`,
                    }}
                  />
                )}
                {/* Node body */}
                <rect
                  x={x}
                  y={y}
                  width={NODE_W}
                  height={NODE_H}
                  rx={6}
                  fill={isActive ? `${accent}30` : "rgba(255,255,255,0.04)"}
                  stroke={isActive ? accent : "rgba(255,255,255,0.15)"}
                  strokeWidth={isActive ? 1.5 : 1}
                  className="transition-colors"
                />
                {/* Left accent bar */}
                <rect
                  x={x}
                  y={y}
                  width={3}
                  height={NODE_H}
                  rx={1.5}
                  fill={accent}
                  opacity={isActive ? 1 : 0.6}
                />
                {/* Label */}
                <text
                  x={x + 14}
                  y={y + 22}
                  fontSize="13"
                  fontWeight="600"
                  fill={isActive ? "#fff" : "rgba(255,255,255,0.9)"}
                  style={{ pointerEvents: "none" }}
                >
                  {KIND_LABELS_PLURAL[kind]}
                </text>
                {/* Count badge */}
                <text
                  x={x + 14}
                  y={y + 42}
                  fontSize="11"
                  fontFamily="monospace"
                  fill={isActive ? accent : "rgba(255,255,255,0.55)"}
                  style={{ pointerEvents: "none" }}
                >
                  {count == null ? "…" : `${count} item${count === 1 ? "" : "s"}`}
                </text>
              </motion.g>
            );
          })}
        </svg>
      </div>
    </div>
  );
}