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
import { useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { motion } from 'framer-motion';
import { Maximize2 } from 'lucide-react';
import { useFleetSummary, useEniPlacement } from '@/queries/hooks';
import { eniPlacementCountsByDpu, fleetHealthyDpus } from '@/lib/api-helpers';
import { LogoMark } from '@/components/brand/LogoMark';

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
  /** Square SVG size in pixels (default 380). */
  size?: number;
  /** Extra Tailwind classes for the wrapper. */
  className?: string;
}

export function FleetConnectivityViz({ size = 380, className }: FleetConnectivityVizProps) {
  const navigate = useNavigate();
  const fleet = useFleetSummary();
  const placements = useEniPlacement();

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

  if (fleet.isLoading) {
    return (
      <div
        className={className}
        style={{ width: size, height: size }}
        aria-busy="true"
        aria-label="Loading fleet connectivity map"
      >
        <div className="w-full h-full rounded-full skeleton-shimmer opacity-40" />
      </div>
    );
  }

  return (
    <div className={className} style={{ width: size, height: size }}>
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