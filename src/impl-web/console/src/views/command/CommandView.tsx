import { useState } from 'react';
import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { api } from '@/api/client';

interface CommandResult { status: number; body: unknown }

export default function CommandView() {
  const [method, setMethod] = useState('GET');
  const [url, setUrl] = useState('/api/v1/default/vnets');
  const [result, setResult] = useState<CommandResult | null>(null);
  const [loading, setLoading] = useState(false);

  const execute = async () => {
    setLoading(true);
    try {
      const body = await api.get<unknown>(url);
      setResult({ status: 200, body });
    } catch (err: unknown) {
      const e = err as { status?: number; body?: unknown; message?: string };
      setResult({ status: e.status ?? 0, body: e.body ?? e.message });
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="animate-fade-in">
      <PageHeader title="Command" subtitle="Execute dashctl commands through the console" />
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <GlassCard>
          <p className="text-xs text-text-secondary uppercase mb-3">Command Builder</p>
          <div className="flex gap-2 mb-3">
            <select className="px-2 py-1.5 bg-bg-primary border border-border rounded text-sm" value={method} onChange={(e) => setMethod(e.target.value)}>
              <option>GET</option><option>PUT</option><option>POST</option><option>DELETE</option>
            </select>
            <input className="flex-1 px-2 py-1.5 bg-bg-primary border border-border rounded font-mono text-sm" value={url} onChange={(e) => setUrl(e.target.value)} />
          </div>
          <button
            onClick={execute}
            disabled={loading}
            className="w-full px-3 py-2 text-sm rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/30 hover:bg-accent-cyan/30 transition-colors disabled:opacity-50"
          >
            {loading ? 'Executing...' : 'Execute'}
          </button>
        </GlassCard>
        <GlassCard>
          <p className="text-xs text-text-secondary uppercase mb-3">Response {result && `(${result.status})`}</p>
          <pre className="text-xs font-mono text-text-primary bg-bg-primary p-3 rounded border border-border overflow-auto max-h-96">
            {result ? JSON.stringify(result.body, null, 2) : 'Execute a command to see output'}
          </pre>
        </GlassCard>
      </div>
    </div>
  );
}