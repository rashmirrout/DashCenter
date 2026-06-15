/**
 * CapacityGauge — animated circular progress gauge.
 *
 * Visual upgrades over the original flat version:
 *   • Brand cyan→purple gradient stroke at low/medium utilization;
 *     transitions to amber and red as the gauge approaches its limit.
 *   • SVG glow filter behind the stroke for the "live" feel.
 *   • The stroke is rendered as a `motion.circle` that animates
 *     `strokeDashoffset` (0% → target) on mount and whenever the value
 *     changes — much more dynamic than the original CSS transition.
 *   • The percentage label uses `AnimatedCounter` for a count-up.
 *   • Above 90% utilization the whole ring softly pulses (warns the
 *     operator that this DPU is near capacity).
 *
 * Public API is backward compatible — `label`, `used`, `max`, `size`,
 * and `className` all work as before.
 */
import { useId } from 'react';
import { motion } from 'framer-motion';
import { cn } from '@/lib/cn';
import { AnimatedCounter } from './AnimatedCounter';

export interface CapacityGaugeProps {
  label: string;
  used: number;
  max: number;
  size?: number;
  className?: string;
}

export function CapacityGauge({
  label,
  used,
  max,
  size = 88,
  className,
}: CapacityGaugeProps) {
  const gradId = useId();
  const glowId = useId();

  const pct = max > 0 ? Math.min(100, (used / max) * 100) : 0;
  const stroke = 7;
  const r = (size - stroke) / 2;
  const circumference = 2 * Math.PI * r;
  const offset = circumference - (pct / 100) * circumference;

  // Color regime: cyan/purple (low/mid) → amber (warn) → red (critical).
  const tier: 'ok' | 'warn' | 'crit' = pct >= 90 ? 'crit' : pct >= 70 ? 'warn' : 'ok';
  const textColor =
    tier === 'crit'
      ? 'var(--accent-red)'
      : tier === 'warn'
        ? 'var(--accent-amber)'
        : 'var(--accent-cyan)';

  const isCritical = tier === 'crit';

  return (
    <div className={cn('flex flex-col items-center group', className)}>
      <div className="relative" style={{ width: size, height: size }}>
        <svg
          width={size}
          height={size}
          viewBox={`0 0 ${size} ${size}`}
          className={cn(
            '-rotate-90 transition-transform duration-300 group-hover:scale-[1.04]',
            isCritical && 'animate-pulse-slow'
          )}
        >
          <defs>
            <linearGradient id={gradId} x1="0%" y1="0%" x2="100%" y2="100%">
              {tier === 'ok' && (
                <>
                  <stop offset="0%" stopColor="#00d4ff" />
                  <stop offset="100%" stopColor="#a855f7" />
                </>
              )}
              {tier === 'warn' && (
                <>
                  <stop offset="0%" stopColor="#f59e0b" />
                  <stop offset="100%" stopColor="#fbbf24" />
                </>
              )}
              {tier === 'crit' && (
                <>
                  <stop offset="0%" stopColor="#ef4444" />
                  <stop offset="100%" stopColor="#f87171" />
                </>
              )}
            </linearGradient>
            <filter id={glowId} x="-50%" y="-50%" width="200%" height="200%">
              <feGaussianBlur stdDeviation="1.4" result="blur" />
              <feMerge>
                <feMergeNode in="blur" />
                <feMergeNode in="SourceGraphic" />
              </feMerge>
            </filter>
          </defs>

          {/* Track */}
          <circle
            cx={size / 2}
            cy={size / 2}
            r={r}
            fill="none"
            stroke="rgba(255,255,255,0.06)"
            strokeWidth={stroke}
          />
          {/* Animated progress arc */}
          <motion.circle
            cx={size / 2}
            cy={size / 2}
            r={r}
            fill="none"
            stroke={`url(#${gradId})`}
            strokeWidth={stroke}
            strokeLinecap="round"
            strokeDasharray={circumference}
            initial={{ strokeDashoffset: circumference }}
            animate={{ strokeDashoffset: offset }}
            transition={{ duration: 1.0, ease: 'easeOut' }}
            filter={`url(#${glowId})`}
          />
        </svg>
        {/* Centered percentage label */}
        <div className="absolute inset-0 flex flex-col items-center justify-center">
          <AnimatedCounter
            value={pct}
            duration={1.0}
            formatter={(n) => `${Math.round(n)}%`}
            className="text-sm font-semibold tabular-nums"
          />
        </div>
      </div>
      <span className="mt-1 text-[10px] uppercase tracking-[0.12em] text-[color:var(--text-secondary)]">
        {label}
      </span>
      <span
        className="text-[10px] font-mono tabular-nums"
        style={{ color: textColor }}
      >
        <AnimatedCounter value={used} duration={1.0} />
        <span className="text-[color:var(--text-muted)]"> / {max.toLocaleString()}</span>
      </span>
    </div>
  );
}