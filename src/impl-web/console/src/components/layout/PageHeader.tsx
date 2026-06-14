import type { ReactNode } from "react";

interface PageHeaderProps {
  title: ReactNode;
  subtitle?: ReactNode;
  actions?: ReactNode;
}

export function PageHeader({ title, subtitle, actions }: PageHeaderProps) {
  return (
    <div className="flex items-center justify-between mb-2">
      <div className="min-w-0">
        <h1 className="text-xl font-bold text-[color:var(--text-primary)] truncate">
          {title}
        </h1>
        {subtitle && (
          <div className="text-sm text-[color:var(--text-secondary)] mt-0.5">
            {subtitle}
          </div>
        )}
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  );
}