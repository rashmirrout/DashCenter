import { Link, useLocation } from "react-router-dom";
import { ChevronRight, Search, Menu } from "lucide-react";
import { cn } from "@/lib/cn";
import { useUiPrefsStore } from "@/stores/ui-prefs-store";
import { useDashdHealth } from "@/queries/hooks";

/** A single breadcrumb crumb. */
export interface Crumb {
  label: string;
  to?: string;
}

/**
 * Compute breadcrumb crumbs from a pathname.
 * Exported for unit tests.
 */
export function pathToCrumbs(pathname: string): Crumb[] {
  if (!pathname || pathname === "/" || pathname === "/dashboard") {
    return [{ label: "Dashboard" }];
  }
  const segs = pathname.split("/").filter(Boolean);
  const crumbs: Crumb[] = [{ label: "Dashboard", to: "/dashboard" }];
  let acc = "";
  for (let i = 0; i < segs.length; i++) {
    const seg = segs[i] ?? "";
    acc += "/" + seg;
    const label = humanize(seg);
    const isLast = i === segs.length - 1;
    crumbs.push(isLast ? { label } : { label, to: acc });
  }
  return crumbs;
}

export function humanize(seg: string): string {
  if (!seg) return "";
  // Keep ID-like values as-is.
  // An "ID-like" segment is any of:
  //  · contains a digit (e.g. "dpu-sim-01", "vnet-1234")
  //  · contains two or more dashes (e.g. "abc-xyz-foo")
  //  · is very long with lowercase chars (e.g. a hash)
  const dashCount = (seg.match(/-/g) || []).length;
  const hasDigit = /\d/.test(seg);
  const isLongHash = /^[a-z0-9]{16,}$/.test(seg);
  if (hasDigit || dashCount >= 2 || isLongHash) {
    return seg;
  }
  return seg
    .split("-")
    .map((w) => (w.length === 0 ? w : (w[0] ?? "").toUpperCase() + w.slice(1)))
    .join(" ");
}

export default function TopBar() {
  const { pathname } = useLocation();
  const toggleSidebar = useUiPrefsStore((s) => s.toggleSidebar);
  const health = useDashdHealth();

  const crumbs = pathToCrumbs(pathname);

  // Health status → indicator color
  const wsState = !health.data
    ? "unknown"
    : health.isError
      ? "offline"
      : health.data.leader
        ? "online"
        : "degraded";

  const wsColor =
    wsState === "online"
      ? "var(--accent-green)"
      : wsState === "degraded"
        ? "var(--accent-amber)"
        : wsState === "offline"
          ? "var(--accent-red)"
          : "var(--text-muted)";

  const wsLabel =
    wsState === "online"
      ? "dashd leader · live"
      : wsState === "degraded"
        ? "follower · live"
        : wsState === "offline"
          ? "dashd unreachable"
          : "connecting…";

  return (
    <header
      role="banner"
      className="flex items-center gap-3 h-14 px-4 border-b border-[color:var(--border-subtle)] bg-[color:var(--bg-secondary)]/70 backdrop-blur-md"
    >
      <button
        type="button"
        onClick={toggleSidebar}
        aria-label="Toggle sidebar"
        className="lg:hidden p-1.5 rounded-md text-[color:var(--text-secondary)] hover:bg-white/5"
      >
        <Menu size={18} />
      </button>

      <nav aria-label="Breadcrumb" className="flex-1 min-w-0">
        <ol className="flex items-center gap-1 text-sm text-[color:var(--text-secondary)] truncate">
          {crumbs.map((c, i) => (
            <li
              key={`${i}-${c.label}`}
              className="flex items-center gap-1 min-w-0"
            >
              {i > 0 && (
                <ChevronRight
                  size={14}
                  className="text-[color:var(--text-muted)] shrink-0"
                  aria-hidden
                />
              )}
              {c.to ? (
                <Link
                  to={c.to}
                  className="hover:text-[color:var(--text-primary)] transition-colors truncate"
                >
                  {c.label}
                </Link>
              ) : (
                <span className="text-[color:var(--text-primary)] font-medium truncate">
                  {c.label}
                </span>
              )}
            </li>
          ))}
        </ol>
      </nav>

      {/* Cmd+K trigger (placeholder — Phase 3 will wire to CommandPalette) */}
      <button
        type="button"
        onClick={() => {
          window.dispatchEvent(new CustomEvent("dashw:open-command-palette"));
        }}
        className={cn(
          "hidden md:flex items-center gap-2 px-3 py-1.5 rounded-md text-xs",
          "bg-white/5 border border-[color:var(--border-subtle)] text-[color:var(--text-secondary)]",
          "hover:bg-white/10 hover:text-[color:var(--text-primary)] transition-colors"
        )}
        title="Open command palette (Ctrl+K)"
      >
        <Search size={14} />
        <span>Search…</span>
        <kbd className="ml-2 px-1.5 py-0.5 rounded border border-[color:var(--border-default)] text-[10px] font-mono">
          ⌘K
        </kbd>
      </button>

      {/* WS / health status */}
      <div
        className="flex items-center gap-2 text-xs text-[color:var(--text-secondary)]"
        title={wsLabel}
        aria-label={wsLabel}
      >
        <span
          aria-hidden
          className={cn(
            "inline-block w-2 h-2 rounded-full",
            wsState === "online" && "animate-pulse-slow",
            wsState === "degraded" && "animate-pulse-medium",
            wsState === "offline" && "animate-pulse-fast"
          )}
          style={{
            backgroundColor: wsColor,
            boxShadow: `0 0 8px ${wsColor}`,
          }}
        />
        <span className="hidden sm:inline">{wsLabel}</span>
      </div>

      <span className="text-[10px] font-mono text-[color:var(--text-muted)] hidden lg:inline">
        v0.1.0
      </span>
    </header>
  );
}