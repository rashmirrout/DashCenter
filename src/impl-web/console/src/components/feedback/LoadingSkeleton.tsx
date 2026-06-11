import { cn } from '@/lib/cn';

interface LoadingSkeletonProps {
  className?: string;
  lines?: number;
}

export function LoadingSkeleton({ className, lines = 3 }: LoadingSkeletonProps) {
  return (
    <div className={cn('space-y-3 animate-pulse', className)}>
      {Array.from({ length: lines }, (_, i) => (
        <div
          key={i}
          className="h-4 rounded bg-bg-elevated"
          style={{ width: `${85 - i * 15}%` }}
        />
      ))}
    </div>
  );
}

export function CardSkeleton({ className }: { className?: string }) {
  return (
    <div className={cn('rounded-xl border border-border bg-bg-surface p-4 animate-pulse', className)}>
      <div className="h-3 w-20 rounded bg-bg-elevated mb-3" />
      <div className="h-6 w-16 rounded bg-bg-elevated mb-2" />
      <div className="h-3 w-28 rounded bg-bg-elevated" />
    </div>
  );
}