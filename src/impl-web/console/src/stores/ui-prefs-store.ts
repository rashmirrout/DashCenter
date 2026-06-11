import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export interface UiPrefsState {
  sidebarCollapsed: boolean;
  theme: 'dark'; // dark-only for v1
  tablePageSize: number;
  topologyLayout: 'logical' | 'physical';
  toggleSidebar: () => void;
  setTablePageSize: (size: number) => void;
  setTopologyLayout: (layout: 'logical' | 'physical') => void;
}

export const useUiPrefsStore = create<UiPrefsState>()(
  persist(
    (set) => ({
      sidebarCollapsed: false,
      theme: 'dark' as const,
      tablePageSize: 25,
      topologyLayout: 'logical' as const,
      toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
      setTablePageSize: (size) => set({ tablePageSize: size }),
      setTopologyLayout: (layout) => set({ topologyLayout: layout }),
    }),
    { name: 'dashw-ui-prefs' },
  ),
);