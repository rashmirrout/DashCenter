import { cn } from '@/lib/cn';
import { formatPercent } from '@/lib/format';

interface CapacityGaugeProps {
  label: string;
  used: number;
  max: number;
  size?: number;
  className?: string;
}

export function CapacityGauge({ label, used, max, size = 80, className }: CapacityGaugeProps) {
  const pct = max > 0 ? (used / max) * 100 : 0;
  const r = (size - 8) / 2;
  const circumference = 2 * Math.PI * r;
  const offset = circumference - (pct / 100) * circumference;

  const color =
    pct >= 90 ? 'var(--accent-red)' : pct >= 70 ? 'var(--accent-amber)' : 'var(--accent-cyan)';

  return (
    <div className={cn('flex flex-col items-center', className)}>
      <svg width={size} height={size} className="-rotate-90">
        <circle
          cx={size / 2}
          cy={size / 2}
          r={r}
          fill="none"
          stroke="var(--border)"
          strokeWidth={6}
        />
        <circle
          cx={size / 2}
          cy={size / 2}
          r={r}
          fill="none"
          stroke={color}
          strokeWidth={6}
          strokeDasharray={circumference}
          strokeDashoffset={offset}
          strokeLinecap="round"
          className="transition-all duration-500"
        />
      </svg>
      <span className="text-xs font-bold mt-1" style={{ color }}>
        {formatPercent(pct, 0)}
      </span>
      <span className="text-[10px] text-text-secondary">{label}</span>
      <span className="text-[10px] text-text-muted">
        {used}/{max}
      </span>
    </div>
  );
}