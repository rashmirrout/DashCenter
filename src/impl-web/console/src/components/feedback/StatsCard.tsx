import { GlassCard } from './GlassCard';
import { cn } from '@/lib/cn';
import type { ReactNode } from 'react';

interface StatsCardProps {
  label: string;
  value: string | number;
  icon?: ReactNode;
  trend?: { value: number; label: string };
  className?: string;
}

export function StatsCard({ label, value, icon, trend, className }: StatsCardProps) {
  return (
    <GlassCard className={cn('flex items-start justify-between', className)}>
      <div>
        <p className="text-xs text-text-secondary uppercase tracking-wider">{label}</p>
        <p className="text-2xl font-bold mt-1">{value}</p>
        {trend && (
          <p className={cn('text-xs mt-1', trend.value >= 0 ? 'text-accent-green' : 'text-accent-red')}>
            {trend.value >= 0 ? '↑' : '↓'} {Math.abs(trend.value)} {trend.label}
          </p>
        )}
      </div>
      {icon && <div className="text-accent-cyan opacity-60">{icon}</div>}
    </GlassCard>
  );
}