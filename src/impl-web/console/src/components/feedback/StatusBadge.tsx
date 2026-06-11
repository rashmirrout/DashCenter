import { cn } from '@/lib/cn';
import { STATUS_COLORS } from '@/lib/constants';

interface StatusBadgeProps {
  status: string;
  size?: 'sm' | 'md';
  className?: string;
}

export function StatusBadge({ status, size = 'sm', className }: StatusBadgeProps) {
  const color = STATUS_COLORS[status] ?? 'var(--text-muted)';
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full font-medium uppercase tracking-wider',
        size === 'sm' ? 'px-2 py-0.5 text-[10px]' : 'px-2.5 py-1 text-xs',
        className,
      )}
      style={{ color, borderColor: color, border: '1px solid' }}
    >
      <span
        className="inline-block rounded-full"
        style={{
          width: size === 'sm' ? 6 : 8,
          height: size === 'sm' ? 6 : 8,
          backgroundColor: color,
        }}
      />
      {status}
    </span>
  );
}