import { Outlet } from 'react-router-dom';
import { Toaster } from 'sonner';
import { Sidebar } from '@/components/layout/Sidebar';

export default function App() {
  return (
    <div className="flex h-screen bg-bg-primary text-text-primary font-sans">
      {/* Skip link for keyboard users */}
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-50 focus:px-4 focus:py-2 focus:bg-accent-cyan focus:text-bg-primary focus:rounded-lg focus:text-sm focus:font-medium"
      >
        Skip to main content
      </a>

      <Sidebar />

      {/* Main content */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {/* TopBar */}
        <header
          className="h-11 bg-bg-surface border-b border-border flex items-center justify-between px-4"
          role="banner"
        >
          <span className="text-sm text-text-secondary">DashCenter Web Console</span>
          <span className="text-xs text-text-muted font-mono" aria-label="Version 0.1.0">v0.1.0</span>
        </header>

        {/* Route outlet */}
        <main id="main-content" className="flex-1 overflow-auto p-6" role="main">
          <Outlet />
        </main>
      </div>

      <Toaster position="bottom-right" theme="dark" richColors />
    </div>
  );
}
