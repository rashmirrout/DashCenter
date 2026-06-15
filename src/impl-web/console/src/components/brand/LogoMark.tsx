/**
 * LogoMark — the DashCenter brand mark.
 *
 * Stylized "network hub" SVG: a central node with six spokes radiating
 * to outer satellite nodes, evoking the DPU-fleet product concept.
 * Rendered as a single inline SVG so it inherits color, sizes crisply
 * at any DPI, and can be embedded next to text without layout glitches.
 *
 * Design tokens:
 *   - linear gradient `accent-cyan (#00d4ff) → accent-purple (#a855f7)`
 *   - 32×32 viewBox (uniform across all consumers)
 *
 * Each instance defines its own `<defs>` with a unique gradient id so
 * multiple LogoMarks can coexist on the same page (e.g. sidebar + hero)
 * without DOM-id collisions.
 */
import { useId } from 'react';
import { cn } from '@/lib/cn';

export interface LogoMarkProps {
  /** Square size in pixels (default: 24). */
  size?: number;
  /** Extra Tailwind classes (e.g. for drop-shadow). */
  className?: string;
  /** When true, slowly rotates the outer ring (hero use). */
  animated?: boolean;
  /** Accessible label; pass null to mark as decorative. */
  ariaLabel?: string | null;
}

export function LogoMark({
  size = 24,
  className,
  animated = false,
  ariaLabel = 'DashCenter',
}: LogoMarkProps) {
  const gradId = useId();
  const glowId = useId();

  // Six outer satellite nodes positioned on a circle of radius 12,
  // centered at (16,16) within a 32×32 viewBox.
  const satellites = Array.from({ length: 6 }, (_, i) => {
    const angle = (i * 60 - 90) * (Math.PI / 180); // start at top
    return {
      x: 16 + 12 * Math.cos(angle),
      y: 16 + 12 * Math.sin(angle),
    };
  });

  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 32 32"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      role={ariaLabel ? 'img' : 'presentation'}
      aria-label={ariaLabel ?? undefined}
      aria-hidden={ariaLabel === null ? true : undefined}
      className={cn('shrink-0', className)}
    >
      <defs>
        <linearGradient id={gradId} x1="0" y1="0" x2="32" y2="32" gradientUnits="userSpaceOnUse">
          <stop offset="0%" stopColor="#00d4ff" />
          <stop offset="100%" stopColor="#a855f7" />
        </linearGradient>
        <filter id={glowId} x="-50%" y="-50%" width="200%" height="200%">
          <feGaussianBlur stdDeviation="0.8" result="blur" />
          <feMerge>
            <feMergeNode in="blur" />
            <feMergeNode in="SourceGraphic" />
          </feMerge>
        </filter>
      </defs>

      {/* Outer ring (rotates when animated) */}
      <g
        className={animated ? 'animate-spin-slow' : undefined}
        style={animated ? { transformOrigin: '16px 16px' } : undefined}
      >
        <circle
          cx="16"
          cy="16"
          r="13"
          stroke={`url(#${gradId})`}
          strokeOpacity="0.35"
          strokeWidth="0.75"
          strokeDasharray="2 2"
        />
        {/* Spokes from center to each satellite */}
        {satellites.map((s, i) => (
          <line
            key={`spoke-${i}`}
            x1="16"
            y1="16"
            x2={s.x}
            y2={s.y}
            stroke={`url(#${gradId})`}
            strokeOpacity="0.45"
            strokeWidth="0.6"
          />
        ))}
        {/* Satellite nodes */}
        {satellites.map((s, i) => (
          <circle
            key={`sat-${i}`}
            cx={s.x}
            cy={s.y}
            r="1.6"
            fill={`url(#${gradId})`}
          />
        ))}
      </g>

      {/* Central hub (always visible, gently glowing) */}
      <circle
        cx="16"
        cy="16"
        r="4.5"
        fill={`url(#${gradId})`}
        filter={`url(#${glowId})`}
      />
      <circle cx="16" cy="16" r="2.2" fill="#0a0e1a" opacity="0.55" />
    </svg>
  );
}

export default LogoMark;