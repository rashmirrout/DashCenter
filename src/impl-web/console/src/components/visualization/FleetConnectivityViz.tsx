/**
 * FleetConnectivityViz — animated live SVG map of the DPU fleet.
 *
 * The dashboard's hero visualization. Arranges every DPU as a glowing
 * node on a circle (with a central "controller" hub) and animates
 * dashed "particle-flow" lines between the controller and each DPU
 * to convey live network connectivity.
 *
 * Design:
 *   • Pure inline SVG so it inherits CSS colors and scales crisply at
 *     any DPI.
 *   • The dashed connector lines use `stroke-dasharray` + an animated
 *     `stroke-dashoffset` (CSS keyframe `particle-flow`) so the dashes
 *     "march" along the line — no JS frame timer required.
 *   • Each DPU node colors itself by `state` (green=up, amber=degraded,
 *     red=disconnected, gray=unknown) and pulses subtly via the
 *     `heartbeat` CSS keyframe when healthy.
 *   • The central hub uses the brand cyan→purple gradient and a soft
 *     SVG glow filter — it represents the dashd controller.
 *   • Clicking a DPU node navigates to `/dpu/:id`.
 *
 * Layout:
 *   • Up to ~24 DPUs render comfortably on a single ring.
 *   • For larger fleets we automatically scale the node radius down so
 *     they don't overlap. Above 36 DPUs the labels are hidden (still
 *     visible on hover via the `<title>` element).
 *
 * Accessibility:
 *   • The SVG has `role="img"` and an `aria-label` describing fleet
 *     totals.
 *   • Each DPU node has a `<title>` with its id and state.
 *   • Honors `prefers-reduced-motion` (the global CSS rule clamps
 *     all animation durations).
 *   • The "View topology" deep-link is a real focusable button.
 *
 * Data sources:
 *   • `useFleetSummary()` for the DPU list + state.
 *   • `useEniPlacement()` for per-DPU ENI counts (rendered as a small
 *     badge on each node).
 */
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { motion } from 'framer-motion';
import { Maximize2 } from 'lucide-react';
import { useFleetSummary, useEniPlacement } from '@/queries/hooks';
import { eniPlacementCountsByDpu, fleetHealthyDpus } from '@/lib/api-helpers';
import { LogoMark } from '@/components/brand/LogoMark';

// SSR-safe variant of useLayoutEffect — fall back to useEffect when there
// is no `window` (e.g. server rendering / test runtimes without jsdom).
const useIsomorphicLayoutEffect =
  typeof window !== 'undefined' ? useLayoutEffect : useEffect;

type DpuLite = { id: string; state: string };

/** Map a DPU state string → accent color css variable. */
function colorForState(state: string | undefined): string {
  if (!state) return 'var(--text-muted)';
  const s = state.toUpperCase();
  if (s.includes('UP') || s === 'READY' || s === 'CONNECTED') {
    return 'var(--accent-green)';
  }
  if (s.includes('DEGRADED') || s === 'DRAINING' || s === 'CORDONED') {
    return 'var(--accent-amber)';
  }
  if (s.includes('DISCONNECTED') || s.includes('DOWN') || s === 'OFFLINE' || s === 'ERROR') {
    return 'var(--accent-red)';
  }
  return 'var(--text-muted)';
}

/** True for states we'd consider "live" (the heartbeat & particle flow run). */
function isLive(state: string | undefined): boolean {
  if (!state) return false;
  const s = state.toUpperCase();
  return s.includes('UP') || s === 'READY' || s === 'CONNECTED';
}

/**
 * Compute polar layout for `n` nodes on a circle of `radius` centered
 * at `(cx, cy)`. Start at the top (`-π/2`) and walk clockwise.
 */
function ringLayout(n: number, cx: number, cy: number, radius: number) {
  if (n <= 0) return [];
  return Array.from({ length: n }, (_, i) => {
    const angle = (i / n) * Math.PI * 2 - Math.PI / 2;
    return {
      x: cx + radius * Math.cos(angle),
      y: cy + radius * Math.sin(angle),
      angle,
    };
  });
}

export interface FleetConnectivityVizProps {
  /** Square SVG size in pixels (default 380). Ignored when `fill` is true. */
  size?: number;
  /** Extra Tailwind classes for the wrapper. */
  className?: string;
  /**
   * Auto-fit the viz to the parent container's width (capped at
   * `maxFillSize`). When true, `size` is computed as
   * `min(parentClientWidth, maxFillSize)` via a `ResizeObserver`.
   */
  fill?: boolean;
  /** Hard cap (px) on the viz when `fill` is true. Default 720. */
  maxFillSize?: number;
}

export function FleetConnectivityViz({
  size: sizeProp = 380,
  className,
  fill = false,
  maxFillSize = 720,
}: FleetConnectivityVizProps) {
  const navigate = useNavigate();
  const fleet = useFleetSummary();
  const placements = useEniPlacement();

  // --- Auto-fill sizing ---------------------------------------------------
  // When `fill` is true we measure BOTH dimensions of the parent container
  // (width AND height) and render the SVG as the largest square that fits
  // inside, leaving a small reserve for the legend strip below it.
  //
  // This is the key design choice: the viz is intrinsically square (circular
  // ring layout), so we cannot stretch it to fill a non-square box. Instead
  // we let the *card* control its own dimensions (typically a fixed height
  // like `h-[540px]`) and have the viz fill the largest square area the
  // card offers, centered. This keeps the box at a sensible size while the
  // animation/graph inside occupies as much of it as the geometry allows.
  //
  // `maxFillSize` is an absolute hard cap (default 720 px) so on ultra-wide
  // monitors the viz never becomes absurdly large.
  const LEGEND_RESERVE = 32; // px of vertical space reserved for the legend strip
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [autoSize, setAutoSize] = useState<number | null>(null);

  useIsomorphicLayoutEffect(() => {
    if (!fill) return;
    const el = containerRef.current;
    if (!el) return;
    const update = () => {
      const w = el.clientWidth;
      const h = el.clientHeight - LEGEND_RESERVE;
      // Largest square that fits inside the container (after reserving
      // vertical space for the legend strip beneath the SVG).
      const dim = Math.min(w, h);
      if (dim > 0) {
        setAutoSize(Math.min(Math.floor(dim), maxFillSize));
      }
    };
    update();
    if (typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, [fill, maxFillSize]);

  // Effective render size — falls back to the explicit `sizeProp` if auto-fill
  // is off, or if the first measurement hasn't completed yet.
  const size = fill && autoSize != null ? autoSize : sizeProp;

  const dpus: DpuLite[] = useMemo(() => {
    const list = fleet.data?.dpus ?? [];
    // Stable order by id so the layout doesn't reshuffle on each refresh.
    return [...list].sort((a, b) => a.id.localeCompare(b.id));
  }, [fleet.data?.dpus]);

  const eniCounts = useMemo(
    () => eniPlacementCountsByDpu(placements.data?.items),
    [placements.data?.items]
  );

  const cx = size / 2;
  const cy = size / 2;
  // Auto-shrink the ring radius (and node radius) when many DPUs.
  const ringR = Math.min(size * 0.4, size / 2 - 36);
  const nodeR = dpus.length > 24 ? 8 : dpus.length > 12 ? 11 : 14;
  const showLabels = dpus.length <= 36;

  const points = useMemo(() => ringLayout(dpus.length, cx, cy, ringR), [dpus.length, cx, cy, ringR]);

  const healthy = fleetHealthyDpus(fleet.data);
  const total = dpus.length;
  const ariaLabel = `Fleet connectivity map. ${healthy} of ${total} DPUs healthy.`;

  // Wrapper style: in `fill` mode the outer container takes 100% of both
  // dimensions of its parent so the ResizeObserver can measure available
  // width AND height; the SVG + legend are centered inside.
  // In fixed-size mode we preserve the original layout exactly (size×size box).
  const wrapperStyle: React.CSSProperties = fill
    ? {
        width: '100%',
        height: '100%',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
      }
    : { width: size, height: size };

  if (fleet.isLoading) {
    return (
      <div
        ref={containerRef}
        className={className}
        style={wrapperStyle}
        aria-busy="true"
        aria-label="Loading fleet connectivity map"
      >
        <div
          className="rounded-full skeleton-shimmer opacity-40"
          style={{ width: size, height: size }}
        />
      </div>
    );
  }

  return (
    <div ref={containerRef} className={className} style={wrapperStyle}>
      <svg
        width={size}
        height={size}
        viewBox={`0 0 ${size} ${size}`}
        role="img"
        aria-label={ariaLabel}
        className="overflow-visible"
      >
        <defs>
          <linearGradient id="fcv-link-grad" x1="0%" y1="0%" x2="100%" y2="100%">
            <stop offset="0%" stopColor="#00d4ff" stopOpacity="0.9" />
            <stop offset="100%" stopColor="#a855f7" stopOpacity="0.9" />
          </linearGradient>
          <radialGradient id="fcv-hub-glow" cx="50%" cy="50%" r="50%">
            <stop offset="0%" stopColor="#00d4ff" stopOpacity="0.35" />
            <stop offset="60%" stopColor="#a855f7" stopOpacity="0.10" />
            <stop offset="100%" stopColor="#a855f7" stopOpacity="0" />
          </radialGradient>
          <filter id="fcv-node-glow" x="-50%" y="-50%" width="200%" height="200%">
            <feGaussianBlur stdDeviation="2" result="blur" />
            <feMerge>
              <feMergeNode in="blur" />
              <feMergeNode in="SourceGraphic" />
            </feMerge>
          </filter>
        </defs>

        {/* Outer guide ring (very subtle) */}
        <circle
          cx={cx}
          cy={cy}
          r={ringR}
          fill="none"
          stroke="rgba(255,255,255,0.06)"
          strokeWidth={1}
          strokeDasharray="2 4"
        />

        {/* Soft halo behind the hub */}
        <circle cx={cx} cy={cy} r={42} fill="url(#fcv-hub-glow)" />

        {/* Connector lines (drawn BEFORE nodes so nodes sit on top) */}
        {points.map((p, i) => {
          const dpu = dpus[i]!;
          const live = isLive(dpu.state);
          return (
            <line
              key={`link-${dpu.id}`}
              x1={cx}
              y1={cy}
              x2={p.x}
              y2={p.y}
              stroke="url(#fcv-link-grad)"
              strokeOpacity={live ? 0.55 : 0.18}
              strokeWidth={1.25}
              strokeDasharray="4 6"
              className={live ? 'animate-particle-flow' : undefined}
            />
          );
        })}

        {/* Central hub (controller) */}
        <g style={{ cursor: 'pointer' }} onClick={() => navigate('/topology')}>
          <title>dashd controller — click to open Topology view</title>
          <circle cx={cx} cy={cy} r={22} fill="rgba(255,255,255,0.04)" stroke="rgba(255,255,255,0.18)" />
          {/* Render the brand LogoMark on top, centered. */}
          <foreignObject x={cx - 18} y={cy - 18} width={36} height={36}>
            <div style={{ width: 36, height: 36, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
              <LogoMark size={32} ariaLabel={null} />
            </div>
          </foreignObject>
        </g>

        {/* DPU nodes */}
        {points.map((p, i) => {
          const dpu = dpus[i]!;
          const color = colorForState(dpu.state);
          const live = isLive(dpu.state);
          const eniCount = eniCounts[dpu.id] ?? 0;
          // Label sits just outside the node, along the same angle.
          const labelDist = nodeR + 14;
          const lx = cx + (ringR + labelDist) * Math.cos(p.angle);
          const ly = cy + (ringR + labelDist) * Math.sin(p.angle);
          const labelAnchor: 'start' | 'middle' | 'end' =
            Math.abs(Math.cos(p.angle)) < 0.3
              ? 'middle'
              : Math.cos(p.angle) > 0
                ? 'start'
                : 'end';
          return (
            <g
              key={dpu.id}
              style={{ cursor: 'pointer' }}
              onClick={() => navigate(`/dpu/${encodeURIComponent(dpu.id)}`)}
            >
              <title>{`${dpu.id} · ${dpu.state}${eniCount ? ` · ${eniCount} ENI${eniCount === 1 ? '' : 's'}` : ''}`}</title>
              {/* Outer halo (only for live nodes) */}
              {live && (
                <circle
                  cx={p.x}
                  cy={p.y}
                  r={nodeR + 6}
                  fill={color}
                  opacity={0.12}
                  className="animate-pulse-slow"
                />
              )}
              {/* The node body */}
              <circle
                cx={p.x}
                cy={p.y}
                r={nodeR}
                fill={color}
                opacity={live ? 0.85 : 0.45}
                filter="url(#fcv-node-glow)"
                className={live ? 'animate-heartbeat' : undefined}
                style={{ transformOrigin: `${p.x}px ${p.y}px` }}
              />
              {/* Dark center dot for visual depth */}
              <circle cx={p.x} cy={p.y} r={nodeR * 0.45} fill="#0a0e1a" opacity={0.55} />
              {/* ENI-count badge (only when there are ENIs and we have room) */}
              {eniCount > 0 && nodeR >= 10 && (
                <text
                  x={p.x}
                  y={p.y + 3}
                  textAnchor="middle"
                  fontSize={9}
                  fontWeight={600}
                  fill="#e8eef9"
                  pointerEvents="none"
                >
                  {eniCount}
                </text>
              )}
              {/* Node label */}
              {showLabels && (
                <text
                  x={lx}
                  y={ly}
                  textAnchor={labelAnchor}
                  dominantBaseline="middle"
                  fontSize={10}
                  fill="var(--text-secondary)"
                  pointerEvents="none"
                  style={{ fontFamily: 'JetBrains Mono, Consolas, monospace' }}
                >
                  {dpu.id.length > 14 ? `${dpu.id.slice(0, 12)}…` : dpu.id}
                </text>
              )}
            </g>
          );
        })}

        {/* Empty-state when no DPUs */}
        {total === 0 && (
          <text
            x={cx}
            y={cy + 56}
            textAnchor="middle"
            fontSize={11}
            fill="var(--text-muted)"
          >
            No DPUs reporting yet
          </text>
        )}
      </svg>

      {/* Bottom-overlay legend + deep-link */}
      <div className="-mt-3 flex items-center justify-center gap-4 text-[10px] text-[color:var(--text-muted)]">
        <Legend color="var(--accent-green)" label="Healthy" />
        <Legend color="var(--accent-amber)" label="Degraded" />
        <Legend color="var(--accent-red)" label="Offline" />
        <motion.button
          type="button"
          onClick={() => navigate('/topology')}
          whileHover={{ x: 2 }}
          className="ml-2 inline-flex items-center gap-1 text-[color:var(--accent-cyan)] hover:text-[color:var(--accent-purple)] transition-colors"
        >
          Open Topology <Maximize2 size={10} />
        </motion.button>
      </div>
    </div>
  );
}

function Legend({ color, label }: { color: string; label: string }) {
  return (
    <span className="inline-flex items-center gap-1">
      <span
        aria-hidden
        className="inline-block w-2 h-2 rounded-full"
        style={{ background: color, boxShadow: `0 0 6px ${color}` }}
      />
      {label}
    </span>
  );
}

export default FleetConnectivityViz;