/**
 * StatsCard — animated metric tile used on the Dashboard.
 *
 * Visual upgrades over the original flat version:
 *   • Gradient left-border accent strip whose color is keyed to a
 *     semantic `accent` prop (cyan / purple / green / amber / red).
 *   • Numeric values use `AnimatedCounter` for a count-up reveal.
 *   • Hover micro-interaction: subtle lift + glow halo.
 *   • Optional `delta` trend pill (positive=green, negative=red).
 *
 * Public API is BACKWARD COMPATIBLE — `label`, `value`, `icon`, `trend`,
 * `className` all still work. New optional props: `accent`, `delta`,
 * `animate`.
 */
import { motion } from 'framer-motion';
import type { ReactNode } from 'react';
import { cn } from '@/lib/cn';
import { AnimatedCounter } from '@/components/visualization/AnimatedCounter';

export type StatsCardAccent = 'cyan' | 'purple' | 'green' | 'amber' | 'red';

export interface StatsCardProps {
  /** Short uppercase label rendered above the value. */
  label: string;
  /** Primary value — numbers animate, strings render as-is. */
  value: string | number;
  /** Optional leading icon (typically a lucide-react icon). */
  icon?: ReactNode;
  /** @deprecated use `delta` for richer trend display. */
  trend?: { value: number; label: string };
  /** Color accent for the left border + icon halo. Default: cyan. */
  accent?: StatsCardAccent;
  /** Optional delta vs previous period — adds a trend pill. */
  delta?: { value: number; label?: string };
  /** When false, numbers are rendered without count-up animation. */
  animate?: boolean;
  /** Extra Tailwind classes for the outer card. */
  className?: string;
}

const ACCENT_TO_BORDER: Record<StatsCardAccent, string> = {
  cyan: 'before:bg-gradient-to-b before:from-cyan-400 before:to-cyan-600',
  purple: 'before:bg-gradient-to-b before:from-purple-400 before:to-purple-600',
  green: 'before:bg-gradient-to-b before:from-emerald-400 before:to-emerald-600',
  amber: 'before:bg-gradient-to-b before:from-amber-400 before:to-amber-600',
  red: 'before:bg-gradient-to-b before:from-rose-400 before:to-rose-600',
};

const ACCENT_TO_HOVER_GLOW: Record<StatsCardAccent, string> = {
  cyan: 'hover:shadow-[0_0_24px_rgba(0,212,255,0.20)]',
  purple: 'hover:shadow-[0_0_24px_rgba(168,85,247,0.20)]',
  green: 'hover:shadow-[0_0_24px_rgba(16,185,129,0.20)]',
  amber: 'hover:shadow-[0_0_24px_rgba(245,158,11,0.20)]',
  red: 'hover:shadow-[0_0_24px_rgba(239,68,68,0.24)]',
};

export function StatsCard({
  label,
  value,
  icon,
  trend,
  accent = 'cyan',
  delta,
  animate = true,
  className,
}: StatsCardProps) {
  const isNumeric = typeof value === 'number';
  const showDelta = !!delta;
  const showLegacyTrend = !!trend && !showDelta;

  return (
    <motion.div
      whileHover={{ y: -3 }}
      transition={{ type: 'spring', stiffness: 260, damping: 22 }}
      className={cn(
        // Base glass surface (mimics GlassCard but with an extra left-bar slot)
        'relative glass-surface p-4 overflow-hidden transition-shadow',
        // ::before is the gradient accent strip on the left edge
        'before:content-[""] before:absolute before:top-3 before:bottom-3 before:left-0 before:w-[3px] before:rounded-r',
        ACCENT_TO_BORDER[accent],
        ACCENT_TO_HOVER_GLOW[accent],
        className
      )}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-[10px] uppercase tracking-[0.14em] text-[color:var(--text-secondary)]">
            {label}
          </p>
          <p className="mt-1 text-3xl font-semibold tabular-nums tracking-tight text-[color:var(--text-primary)]">
            {isNumeric && animate ? (
              <AnimatedCounter value={value as number} duration={1.0} />
            ) : (
              value
            )}
          </p>
          {showDelta && <DeltaPill {...delta} />}
          {showLegacyTrend && (
            <p
              className={cn(
                'text-xs mt-1',
                trend!.value >= 0 ? 'text-[color:var(--accent-green)]' : 'text-[color:var(--accent-red)]'
              )}
            >
              {trend!.value >= 0 ? '↑' : '↓'} {Math.abs(trend!.value)} {trend!.label}
            </p>
          )}
        </div>
        {icon && (
          <div className="shrink-0 p-2 rounded-lg bg-white/[0.04] border border-white/[0.06]">
            {icon}
          </div>
        )}
      </div>
    </motion.div>
  );
}

function DeltaPill({ value, label }: { value: number; label?: string }) {
  if (value === 0) {
    return (
      <span className="mt-2 inline-flex items-center gap-1 text-[11px] text-[color:var(--text-muted)]">
        → 0 {label}
      </span>
    );
  }
  const positive = value > 0;
  const cls = positive
    ? 'bg-emerald-500/10 text-[color:var(--accent-green)] border-emerald-500/20'
    : 'bg-rose-500/10 text-[color:var(--accent-red)] border-rose-500/20';
  return (
    <span
      className={cn(
        'mt-2 inline-flex items-center gap-1 px-1.5 py-0.5 rounded-md border text-[11px] font-medium tabular-nums',
        cls
      )}
    >
      {positive ? '↑' : '↓'} {Math.abs(value).toLocaleString()} {label}
    </span>
  );
}