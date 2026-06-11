import { Outlet } from "react-router-dom";
import { Toaster } from "sonner";
import Sidebar from "@/components/layout/Sidebar";
import TopBar from "@/components/layout/TopBar";

export default function App() {
  return (
    <div className="flex h-screen w-screen overflow-hidden bg-[color:var(--bg-primary)] text-[color:var(--text-primary)] font-sans">
      <Sidebar />
      <div className="flex-1 flex flex-col min-w-0">
        <TopBar />
        <main className="flex-1 overflow-y-auto p-6">
          <Outlet />
        </main>
      </div>
      <Toaster
        theme="dark"
        position="top-right"
        toastOptions={{
          style: {
            background: "var(--bg-elevated)",
            border: "1px solid var(--border-default)",
            color: "var(--text-primary)",
          },
        }}
      />
    </div>
  );
}