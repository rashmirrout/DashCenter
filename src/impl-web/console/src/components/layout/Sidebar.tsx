import { useLocation, Link } from 'react-router-dom';
import { cn } from '@/lib/cn';
import { useUiPrefsStore } from '@/stores/ui-prefs-store';

interface NavItem {
  label: string;
  path: string;
  icon: string;
}

interface NavGroup {
  title: string;
  items: NavItem[];
}

const NAV_GROUPS: NavGroup[] = [
  {
    title: 'Overview',
    items: [
      { label: 'Dashboard', path: '/dashboard', icon: '⬡' },
      { label: 'Fleet', path: '/fleet', icon: '◎' },
    ],
  },
  {
    title: 'Resources',
    items: [
      { label: 'Vnets', path: '/vnets', icon: '◈' },
      { label: 'Routing', path: '/routing', icon: '⇢' },
      { label: 'Tunnels', path: '/tunnels', icon: '⤨' },
      { label: 'Policies', path: '/policies', icon: '⛨' },
    ],
  },
  {
    title: 'Operations',
    items: [
      { label: 'Admin Ops', path: '/admin-ops', icon: '⚙' },
      { label: 'Flow Trace', path: '/flow-trace', icon: '⟿' },
      { label: 'Command', path: '/command', icon: '⌘' },
    ],
  },
  {
    title: 'System',
    items: [
      { label: 'dashd Health', path: '/health', icon: '♥' },
      { label: 'Audit', path: '/audit', icon: '📋' },
      { label: 'Debug', path: '/debug', icon: '🔧' },
    ],
  },
];

export function Sidebar() {
  const location = useLocation();
  const collapsed = useUiPrefsStore((s) => s.sidebarCollapsed);
  const toggleSidebar = useUiPrefsStore((s) => s.toggleSidebar);

  return (
    <aside
      className={cn(
        'bg-bg-surface border-r border-border flex flex-col transition-all duration-200',
        collapsed ? 'w-16' : 'w-56',
      )}
    >
      {/* Logo */}
      <div className="p-3 border-b border-border flex items-center gap-2">
        <button
          onClick={toggleSidebar}
          className="text-accent-cyan text-xl hover:opacity-80 transition-opacity"
          aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        >
          ⬡
        </button>
        {!collapsed && (
          <div>
            <span className="text-lg font-bold text-accent-cyan">dashw</span>
            <p className="text-[10px] text-text-muted leading-none">DashCenter Console</p>
          </div>
        )}
      </div>

      {/* Navigation */}
      <nav className="flex-1 overflow-y-auto py-2" aria-label="Main navigation">
        {NAV_GROUPS.map((group) => (
          <div key={group.title} className="mb-2">
            {!collapsed && (
              <p className="px-3 py-1 text-[10px] font-semibold uppercase tracking-widest text-text-muted">
                {group.title}
              </p>
            )}
            {group.items.map((item) => {
              const active = location.pathname === item.path ||
                (item.path !== '/dashboard' && location.pathname.startsWith(item.path));
              return (
                <Link
                  key={item.path}
                  to={item.path}
                  title={collapsed ? item.label : undefined}
                  aria-current={active ? 'page' : undefined}
                  className={cn(
                    'flex items-center gap-2 mx-1 px-2 py-1.5 rounded text-sm transition-colors focus:outline-none focus:ring-2 focus:ring-accent-cyan/50',
                    active
                      ? 'bg-bg-elevated text-accent-cyan'
                      : 'text-text-secondary hover:bg-bg-elevated hover:text-text-primary',
                  )}
                >
                  <span className="text-base w-5 text-center flex-shrink-0" aria-hidden="true">{item.icon}</span>
                  {!collapsed && <span className="truncate">{item.label}</span>}
                </Link>
              );
            })}
          </div>
        ))}
      </nav>
    </aside>
  );
}