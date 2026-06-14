import { useState, type ReactNode, type KeyboardEvent } from "react";
import { cn } from "@/lib/cn";

export interface TabDef {
  /** Stable id used to identify the active tab. */
  id: string;
  /** Visible label. */
  label: string;
  /** Optional badge / count next to the label. */
  badge?: number | string;
  /** Optional icon (e.g. lucide icon element). */
  icon?: ReactNode;
  /** Tab body. */
  content: ReactNode;
  /** Disable selection. */
  disabled?: boolean;
}

export interface TabsProps {
  tabs: TabDef[];
  defaultTabId?: string;
  /** Controlled active tab id. */
  activeTabId?: string;
  /** Called when the user switches tabs. */
  onChange?: (id: string) => void;
  /** Visual variant. */
  variant?: "underline" | "pill";
  className?: string;
}

export function Tabs({
  tabs,
  defaultTabId,
  activeTabId,
  onChange,
  variant = "underline",
  className,
}: TabsProps) {
  const [internalId, setInternalId] = useState<string>(
    () => defaultTabId ?? tabs[0]?.id ?? ""
  );
  const activeId = activeTabId ?? internalId;
  const activeTab = tabs.find((t) => t.id === activeId) ?? tabs[0];

  function selectTab(id: string) {
    if (activeTabId == null) setInternalId(id);
    onChange?.(id);
  }

  function handleKeyDown(e: KeyboardEvent<HTMLButtonElement>, idx: number) {
    const enabled = tabs.filter((t) => !t.disabled);
    const currentEnabledIdx = enabled.findIndex((t) => t.id === tabs[idx]?.id);
    if (currentEnabledIdx < 0) return;
    if (e.key === "ArrowRight") {
      e.preventDefault();
      const next = enabled[(currentEnabledIdx + 1) % enabled.length];
      if (next) selectTab(next.id);
    } else if (e.key === "ArrowLeft") {
      e.preventDefault();
      const prev =
        enabled[(currentEnabledIdx - 1 + enabled.length) % enabled.length];
      if (prev) selectTab(prev.id);
    } else if (e.key === "Home") {
      e.preventDefault();
      if (enabled[0]) selectTab(enabled[0].id);
    } else if (e.key === "End") {
      e.preventDefault();
      const last = enabled[enabled.length - 1];
      if (last) selectTab(last.id);
    }
  }

  return (
    <div className={cn("flex flex-col min-h-0", className)}>
      <div
        role="tablist"
        aria-orientation="horizontal"
        className={cn(
          "flex items-center gap-1",
          variant === "underline" &&
            "border-b border-[color:var(--border-subtle)] -mb-px"
        )}
      >
        {tabs.map((tab, idx) => {
          const isActive = tab.id === activeId;
          return (
            <button
              key={tab.id}
              role="tab"
              type="button"
              aria-selected={isActive}
              aria-controls={`tabpanel-${tab.id}`}
              id={`tab-${tab.id}`}
              disabled={tab.disabled}
              tabIndex={isActive ? 0 : -1}
              onClick={() => !tab.disabled && selectTab(tab.id)}
              onKeyDown={(e) => handleKeyDown(e, idx)}
              className={cn(
                "inline-flex items-center gap-2 px-3 py-2 text-sm transition-colors disabled:opacity-40 disabled:cursor-not-allowed",
                variant === "underline" &&
                  "border-b-2 -mb-px",
                variant === "underline" &&
                  (isActive
                    ? "border-[color:var(--accent-cyan)] text-[color:var(--accent-cyan)]"
                    : "border-transparent text-[color:var(--text-secondary)] hover:text-[color:var(--text-primary)]"),
                variant === "pill" &&
                  "rounded-md",
                variant === "pill" &&
                  (isActive
                    ? "bg-white/10 text-[color:var(--accent-cyan)]"
                    : "text-[color:var(--text-secondary)] hover:text-[color:var(--text-primary)] hover:bg-white/5")
              )}
            >
              {tab.icon}
              <span>{tab.label}</span>
              {tab.badge != null && (
                <span
                  className={cn(
                    "ml-1 px-1.5 py-0.5 rounded-full text-[10px] font-mono",
                    isActive
                      ? "bg-[color:var(--accent-cyan)]/15 text-[color:var(--accent-cyan)]"
                      : "bg-white/5 text-[color:var(--text-muted)]"
                  )}
                >
                  {tab.badge}
                </span>
              )}
            </button>
          );
        })}
      </div>

      <div
        key={activeId}
        role="tabpanel"
        id={`tabpanel-${activeId}`}
        aria-labelledby={`tab-${activeId}`}
        className="pt-4 animate-fade-in min-h-0 flex-1"
      >
        {activeTab?.content}
      </div>
    </div>
  );
}