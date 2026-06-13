import { NavLink, useLocation } from "react-router-dom";
import {
  LayoutDashboard,
  Network,
  Waypoints,
  Route,
  Cable,
  Shield,
  Workflow,
  ScrollText,
  HeartPulse,
  Settings,
  Terminal,
  Bug,
  Radio,
  Plug,
  PanelLeftClose,
  PanelLeftOpen,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@/lib/cn";
import { useUiPrefsStore } from "@/stores/ui-prefs-store";

/** A single sidebar navigation item. */
export interface NavItem {
  path: string;
  label: string;
  icon: LucideIcon;
}

/** A grouped block of items. `label` is null for the top, ungrouped row. */
export interface NavGroup {
  label: string | null;
  items: NavItem[];
}

/**
 * Navigation groups defined per LLD §6.2 (Sidebar).
 * Exported so tests can assert structure.
 */
export const NAV_GROUPS: readonly NavGroup[] = [
  {
    label: null,
    items: [
      { path: "/dashboard", label: "Dashboard", icon: LayoutDashboard },
    ],
  },
  {
    label: "Observe",
    items: [
      { path: "/fleet", label: "Fleet", icon: Network },
      { path: "/enis", label: "ENIs", icon: Plug },
      { path: "/routing", label: "Routing", icon: Route },
      { path: "/tunnels", label: "Tunnels", icon: Cable },
      { path: "/policies", label: "Policies", icon: Shield },
    ],
  },
  {
    label: "Diagnostics",
    items: [
      { path: "/flow-trace", label: "Flow Trace", icon: Workflow },
      { path: "/audit", label: "Audit Log", icon: ScrollText },
      { path: "/health", label: "Health", icon: HeartPulse },
    ],
  },
  {
    label: "Operate",
    items: [
      { path: "/admin", label: "Admin Ops", icon: Settings },
      { path: "/commands", label: "Commands", icon: Terminal },
      { path: "/debug", label: "Debug", icon: Bug },
    ],
  },
  {
    label: "Service",
    items: [
      { path: "/topology", label: "Topology", icon: Waypoints },
      { path: "/topology-v2", label: "Topology v2 · Live", icon: Radio },
    ],
  },
] as const;

/**
 * `true` when `pathname` matches `itemPath` (or is nested under it for
 * non-dashboard routes). Exported for unit tests.
 */
export function isPathActive(pathname: string, itemPath: string): boolean {
  if (pathname === itemPath) return true;
  if (itemPath === "/dashboard") return pathname === "/dashboard";
  return pathname.startsWith(itemPath + "/");
}

export default function Sidebar() {
  const collapsed = useUiPrefsStore((s) => s.sidebarCollapsed);
  const toggleCollapsed = useUiPrefsStore((s) => s.toggleSidebar);
  const { pathname } = useLocation();

  return (
    <aside
      data-testid="sidebar"
      aria-label="Primary navigation"
      className={cn(
        "flex flex-col shrink-0 h-full",
        "bg-[color:var(--bg-secondary)] border-r border-[color:var(--border-subtle)]",
        "transition-[width] duration-200 ease-out",
        collapsed ? "w-16" : "w-60"
      )}
    >
      {/* Brand / collapse toggle */}
      <div
        className={cn(
          "flex items-center gap-2 h-14 px-3 border-b border-[color:var(--border-subtle)]",
          collapsed ? "justify-center" : "justify-between"
        )}
      >
        {!collapsed && (
          <div className="flex items-center gap-2">
            <span
              aria-hidden
              className="inline-block w-6 h-6 rounded-md bg-gradient-to-br from-cyan-400 to-purple-500 shadow-[0_0_12px_rgba(0,212,255,0.45)]"
            />
            <span className="font-semibold tracking-tight text-[color:var(--text-primary)]">
              dashw
            </span>
          </div>
        )}
        <button
          type="button"
          onClick={toggleCollapsed}
          aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          className="p-1.5 rounded-md text-[color:var(--text-secondary)] hover:text-[color:var(--text-primary)] hover:bg-white/5 transition-colors"
        >
          {collapsed ? (
            <PanelLeftOpen size={16} />
          ) : (
            <PanelLeftClose size={16} />
          )}
        </button>
      </div>

      {/* Groups */}
      <nav
        className="flex-1 overflow-y-auto py-3 space-y-4"
        aria-label="Sidebar navigation"
      >
        {NAV_GROUPS.map((group, gi) => (
          <div key={group.label ?? `__top_${gi}`}>
            {group.label && !collapsed && (
              <p className="px-3 mb-1 text-[10px] font-semibold tracking-[0.14em] uppercase text-[color:var(--text-muted)]">
                {group.label}
              </p>
            )}
            <ul className="space-y-0.5">
              {group.items.map((item) => {
                const active = isPathActive(pathname, item.path);
                const Icon = item.icon;
                return (
                  <li key={item.path}>
                    <NavLink
                      to={item.path}
                      aria-current={active ? "page" : undefined}
                      title={collapsed ? item.label : undefined}
                      className={cn(
                        "group relative flex items-center gap-3 mx-2 px-2.5 py-2 rounded-md text-sm transition-colors",
                        collapsed && "justify-center",
                        active
                          ? "bg-white/5 text-[color:var(--accent-cyan)] shadow-[inset_0_0_0_1px_rgba(0,212,255,0.25)]"
                          : "text-[color:var(--text-secondary)] hover:text-[color:var(--text-primary)] hover:bg-white/5"
                      )}
                    >
                      {active && (
                        <span
                          aria-hidden
                          className="absolute left-0 top-1.5 bottom-1.5 w-0.5 rounded-r bg-[color:var(--accent-cyan)] shadow-[0_0_8px_rgba(0,212,255,0.6)]"
                        />
                      )}
                      <Icon size={16} className="shrink-0" aria-hidden />
                      {!collapsed && (
                        <span className="truncate">{item.label}</span>
                      )}
                    </NavLink>
                  </li>
                );
              })}
            </ul>
          </div>
        ))}
      </nav>

      {/* Version footer */}
      <div
        className={cn(
          "border-t border-[color:var(--border-subtle)] px-3 py-2 text-[10px] text-[color:var(--text-muted)]",
          collapsed && "text-center"
        )}
      >
        {collapsed ? "v0.1" : "v0.1.0 · Phase A"}
      </div>
    </aside>
  );
}