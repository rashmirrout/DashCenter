import { useEffect, type ReactNode } from "react";
import { X } from "lucide-react";
import { cn } from "@/lib/cn";

interface DrawerProps {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  subtitle?: ReactNode;
  /** Right-edge slide-in width. */
  width?: "sm" | "md" | "lg" | "xl";
  children: ReactNode;
}

const WIDTH_MAP: Record<NonNullable<DrawerProps["width"]>, string> = {
  sm: "w-[400px]",
  md: "w-[560px]",
  lg: "w-[720px]",
  xl: "w-[920px]",
};

export function Drawer({
  open,
  onClose,
  title,
  subtitle,
  width = "lg",
  children,
}: DrawerProps) {
  useEffect(() => {
    if (!open) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKey);
    // lock body scroll
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = prev;
    };
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div
      role="dialog"
      aria-modal="true"
      data-testid="drawer"
      className="fixed inset-0 z-50 flex"
    >
      {/* Backdrop */}
      <button
        type="button"
        aria-label="Close drawer"
        onClick={onClose}
        className="flex-1 bg-black/60 backdrop-blur-[2px] animate-fade-in"
      />
      {/* Panel */}
      <aside
        className={cn(
          "h-full flex flex-col bg-[color:var(--bg-secondary)] border-l border-[color:var(--border-default)] shadow-2xl animate-slide-up overflow-hidden",
          WIDTH_MAP[width],
          "max-w-full"
        )}
      >
        <header className="flex items-start justify-between gap-3 p-4 border-b border-[color:var(--border-subtle)]">
          <div className="min-w-0">
            <h2 className="text-base font-semibold text-[color:var(--text-primary)] truncate">
              {title}
            </h2>
            {subtitle && (
              <p className="text-xs text-[color:var(--text-secondary)] mt-0.5 truncate">
                {subtitle}
              </p>
            )}
          </div>
          <button
            type="button"
            onClick={onClose}
            className="p-1.5 rounded-md text-[color:var(--text-secondary)] hover:text-[color:var(--text-primary)] hover:bg-white/5"
            aria-label="Close"
          >
            <X size={16} />
          </button>
        </header>
        <div className="flex-1 overflow-y-auto p-4">{children}</div>
      </aside>
    </div>
  );
}

/* ── Reusable property rows / sections ────────────────── */

export function KeyValueRow({
  label,
  value,
}: {
  label: ReactNode;
  value: ReactNode;
}) {
  return (
    <div className="grid grid-cols-[140px_1fr] gap-2 items-baseline text-sm border-b border-[color:var(--border-subtle)] py-1.5 last:border-0">
      <span className="text-[color:var(--text-muted)] text-xs uppercase tracking-wider">
        {label}
      </span>
      <span className="text-[color:var(--text-primary)] break-all">{value}</span>
    </div>
  );
}

export function DrawerSection({
  title,
  children,
}: {
  title: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="mb-5">
      <h3 className="text-[10px] font-semibold uppercase tracking-[0.14em] text-[color:var(--text-secondary)] mb-2">
        {title}
      </h3>
      {children}
    </section>
  );
}

export function LabelChips({ labels }: { labels: Record<string, string> | undefined }) {
  const entries = Object.entries(labels ?? {});
  if (entries.length === 0)
    return <span className="text-xs text-[color:var(--text-muted)]">—</span>;
  return (
    <div className="flex flex-wrap gap-1">
      {entries.map(([k, v]) => (
        <span
          key={k}
          className="text-[10px] px-1.5 py-0.5 rounded bg-white/5 font-mono"
        >
          {k}={v}
        </span>
      ))}
    </div>
  );
}