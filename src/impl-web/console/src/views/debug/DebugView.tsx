import { useState } from 'react';
import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { api } from '@/api/client';

export default function DebugView() {
  const [url, setUrl] = useState('');
  const [method, setMethod] = useState('GET');
  const [body, setBody] = useState('');
  const [result, setResult] = useState<string>('');
  const [loading, setLoading] = useState(false);

  const send = async () => {
    setLoading(true);
    try {
      let data: unknown;
      if (method === 'GET') data = await api.get<unknown>(url);
      else if (method === 'DELETE') data = await api.delete<unknown>(url);
      else if (method === 'POST') data = await api.post<unknown>(url, body ? JSON.parse(body) : undefined);
      else data = await api.put<unknown>(url, body ? JSON.parse(body) : {});
      setResult(JSON.stringify(data, null, 2));
    } catch (err: unknown) {
      setResult(JSON.stringify(err, null, 2));
    } finally {
      setLoading(false);
    }
  };

  const quickEndpoints = [
    { label: 'Health', url: '/api/admin/health' },
    { label: 'Leader', url: '/api/admin/leader' },
    { label: 'DPU Health', url: '/api/admin/health/dpus' },
    { label: 'Drift', url: '/api/admin/drift' },
    { label: 'ENI Placement', url: '/api/admin/eni-placement' },
    { label: 'Fleet Summary', url: '/api/console/fleet/summary' },
    { label: 'Topology', url: '/api/console/topology' },
    { label: 'Capacity', url: '/api/console/stats/capacity' },
    { label: 'Vnets', url: '/api/v1/default/vnets' },
    { label: 'ENIs', url: '/api/v1/default/enis' },
    { label: 'ACL Policies', url: '/api/v1/default/acl-policies' },
    { label: 'Inventory', url: '/api/v1/inventory' },
  ];

  return (
    <div className="animate-fade-in">
      <PageHeader title="Debug" subtitle="Raw API caller and endpoint inspector" />
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <GlassCard>
          <p className="text-xs text-text-secondary uppercase mb-3">Quick Endpoints</p>
          <div className="space-y-1">
            {quickEndpoints.map((ep) => (
              <button
                key={ep.url}
                onClick={() => { setUrl(ep.url); setMethod('GET'); }}
                className="w-full text-left px-2 py-1 rounded text-sm text-text-secondary hover:bg-bg-elevated hover:text-text-primary transition-colors"
              >
                <span className="text-accent-cyan font-mono text-xs">GET</span>{' '}
                {ep.label}
              </button>
            ))}
          </div>
        </GlassCard>
        <GlassCard className="lg:col-span-2">
          <p className="text-xs text-text-secondary uppercase mb-3">Raw API Caller</p>
          <div className="flex gap-2 mb-3">
            <select className="px-2 py-1.5 bg-bg-primary border border-border rounded text-sm" value={method} onChange={(e) => setMethod(e.target.value)}>
              <option>GET</option><option>PUT</option><option>POST</option><option>DELETE</option>
            </select>
            <input
              className="flex-1 px-2 py-1.5 bg-bg-primary border border-border rounded font-mono text-sm"
              placeholder="/api/..."
              value={url}
              onChange={(e) => setUrl(e.target.value)}
            />
            <button
              onClick={send}
              disabled={loading || !url}
              className="px-4 py-1.5 text-sm rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/30 hover:bg-accent-cyan/30 transition-colors disabled:opacity-50"
            >
              Send
            </button>
          </div>
          {(method === 'PUT' || method === 'POST') && (
            <textarea
              className="w-full mb-3 p-2 bg-bg-primary border border-border rounded font-mono text-xs text-text-primary resize-y h-24"
              placeholder="Request body (JSON)"
              value={body}
              onChange={(e) => setBody(e.target.value)}
            />
          )}
          <pre className="text-xs font-mono text-text-primary bg-bg-primary p-3 rounded border border-border overflow-auto max-h-[500px]">
            {result || 'Send a request to see the response'}
          </pre>
        </GlassCard>
      </div>
    </div>
  );
}