import type { ReactNode } from 'react';
import { cn } from '@/lib/cn';

interface GlassCardProps {
  children: ReactNode;
  className?: string;
  glow?: boolean;
  onClick?: () => void;
}

export function GlassCard({ children, className, glow, onClick }: GlassCardProps) {
  return (
    <div
      className={cn(
        'rounded-xl border border-border bg-bg-surface/80 backdrop-blur-sm p-4',
        glow && 'shadow-[var(--shadow-glow-cyan)]',
        onClick && 'cursor-pointer hover:border-[var(--border-focus)] transition-colors',
        className,
      )}
      onClick={onClick}
      role={onClick ? 'button' : undefined}
      tabIndex={onClick ? 0 : undefined}
      onKeyDown={onClick ? (e) => e.key === 'Enter' && onClick() : undefined}
    >
      {children}
    </div>
  );
}