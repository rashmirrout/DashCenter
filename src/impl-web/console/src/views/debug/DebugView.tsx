import { useState } from 'react';
import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { api } from '@/api/client';
import { useAclHitStats, useTriggerResimulation } from '@/queries/hooks';

export default function DebugView() {
  const [url, setUrl] = useState('');
  const [method, setMethod] = useState('GET');
  const [body, setBody] = useState('');
  const [result, setResult] = useState<string>('');
  const [loading, setLoading] = useState(false);
  const [zeroHitsOnly, setZeroHitsOnly] = useState(true);
  const [resimDpus, setResimDpus] = useState('');

  const aclHitStats = useAclHitStats();
  const triggerResim = useTriggerResimulation();

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
    { label: 'Audit Log', url: '/api/admin/audit?limit=50' },
    { label: 'Fleet Summary', url: '/api/console/fleet/summary' },
    { label: 'Topology', url: '/api/console/topology' },
    { label: 'Service Topology', url: '/api/console/service-topology' },
    { label: 'Capacity', url: '/api/console/stats/capacity' },
    { label: 'Vnets', url: '/api/v1/default/vnets' },
    { label: 'ENIs', url: '/api/v1/default/enis' },
    { label: 'ACL Policies', url: '/api/v1/default/acl-policies' },
    { label: 'Route Policies', url: '/api/v1/default/route-policies' },
    { label: 'VNet Mappings', url: '/api/v1/default/vnet-mappings' },
    { label: 'Service Tunnels', url: '/api/v1/default/service-tunnels' },
    { label: 'HA Sets', url: '/api/v1/default/ha' },
    { label: 'Inventory', url: '/api/v1/inventory' },
  ];

  const diagEndpoints = [
    { label: 'Trace Flow', method: 'POST', url: '/api/v1/diagnostics/trace-flow', body: '{"flow":{"direction":1,"eni_name":"eni-bank-web-04","src_ip":"203.0.113.10","dst_ip":"192.168.11.4","dst_port":443,"protocol":"tcp"}}' },
    { label: 'Explain Match (Route)', method: 'POST', url: '/api/v1/diagnostics/explain-match', body: '{"subject":2,"flow":{"direction":1,"eni_name":"eni-spark-01","src_ip":"10.4.1.11","dst_ip":"10.200.5.5","dst_port":9092,"protocol":"tcp"}}' },
    { label: 'ACL Hit Stats', method: 'POST', url: '/api/v1/diagnostics/acl-hit-stats', body: '{"zero_hits_only":true}' },
    { label: 'Trigger Resimulation', method: 'POST', url: '/api/v1/diagnostics/trigger-resimulation', body: '{"dpu_ids":["dpu-sim-01"]}' },
  ];

  return (
    <div className="animate-fade-in">
      <PageHeader title="Debug" subtitle="Raw API caller, diagnostics tools, and endpoint inspector" />
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <div className="space-y-4">
          <GlassCard>
            <p className="text-xs text-[color:var(--text-secondary)] uppercase mb-3">Quick Endpoints</p>
            <div className="space-y-1 max-h-[300px] overflow-auto">
              {quickEndpoints.map((ep) => (
                <button
                  key={ep.url}
                  onClick={() => { setUrl(ep.url); setMethod('GET'); setBody(''); }}
                  className="w-full text-left px-2 py-1 rounded text-sm text-[color:var(--text-secondary)] hover:bg-white/5 hover:text-[color:var(--text-primary)] transition-colors"
                >
                  <span className="text-[color:var(--accent-cyan)] font-mono text-xs">GET</span>{' '}
                  {ep.label}
                </button>
              ))}
            </div>
          </GlassCard>
          <GlassCard>
            <p className="text-xs text-[color:var(--text-secondary)] uppercase mb-3">PE-1 Diagnostics</p>
            <div className="space-y-1">
              {diagEndpoints.map((ep) => (
                <button
                  key={ep.label}
                  onClick={() => { setUrl(ep.url); setMethod(ep.method); setBody(ep.body); }}
                  className="w-full text-left px-2 py-1 rounded text-sm text-[color:var(--text-secondary)] hover:bg-white/5 hover:text-[color:var(--text-primary)] transition-colors"
                >
                  <span className="text-amber-400 font-mono text-xs">{ep.method}</span>{' '}
                  {ep.label}
                </button>
              ))}
            </div>
          </GlassCard>
          <GlassCard>
            <p className="text-xs text-[color:var(--text-secondary)] uppercase mb-3">Quick Actions</p>
            <div className="space-y-2">
              <div>
                <div className="flex items-center gap-2 mb-1">
                  <label className="text-xs text-[color:var(--text-secondary)]">ACL Hit Stats</label>
                  <label className="flex items-center gap-1 text-xs text-[color:var(--text-muted)]">
                    <input type="checkbox" checked={zeroHitsOnly} onChange={(e) => setZeroHitsOnly(e.target.checked)} className="rounded" />
                    Zero hits only
                  </label>
                </div>
                <button
                  onClick={() => aclHitStats.mutate({ zero_hits_only: zeroHitsOnly }, { onSuccess: (d) => setResult(JSON.stringify(d, null, 2)) })}
                  disabled={aclHitStats.isPending}
                  className="w-full px-2 py-1.5 text-xs rounded bg-violet-500/20 text-violet-400 border border-violet-500/30 hover:bg-violet-500/30 transition-colors disabled:opacity-50"
                >
                  {aclHitStats.isPending ? 'Loading…' : 'Fetch ACL Hit Stats'}
                </button>
              </div>
              <div>
                <label className="block text-xs text-[color:var(--text-secondary)] mb-1">Trigger Resimulation</label>
                <input
                  className="w-full px-2 py-1 bg-[color:var(--bg-primary)] border border-[color:var(--border-subtle)] rounded font-mono text-xs text-[color:var(--text-primary)] mb-1"
                  placeholder="dpu-sim-01,dpu-sim-02"
                  value={resimDpus}
                  onChange={(e) => setResimDpus(e.target.value)}
                />
                <button
                  onClick={() => {
                    const ids = resimDpus.split(',').map(s => s.trim()).filter(Boolean);
                    triggerResim.mutate({ dpu_ids: ids.length ? ids : undefined }, { onSuccess: (d) => setResult(JSON.stringify(d, null, 2)) });
                  }}
                  disabled={triggerResim.isPending}
                  className="w-full px-2 py-1.5 text-xs rounded bg-amber-500/20 text-amber-400 border border-amber-500/30 hover:bg-amber-500/30 transition-colors disabled:opacity-50"
                >
                  {triggerResim.isPending ? 'Triggering…' : 'Trigger Resimulation'}
                </button>
              </div>
            </div>
          </GlassCard>
        </div>
        <GlassCard className="lg:col-span-2">
          <p className="text-xs text-[color:var(--text-secondary)] uppercase mb-3">Raw API Caller</p>
          <div className="flex gap-2 mb-3">
            <select className="px-2 py-1.5 bg-[color:var(--bg-primary)] border border-[color:var(--border-subtle)] rounded text-sm text-[color:var(--text-primary)]" value={method} onChange={(e) => setMethod(e.target.value)}>
              <option>GET</option><option>PUT</option><option>POST</option><option>DELETE</option>
            </select>
            <input
              className="flex-1 px-2 py-1.5 bg-[color:var(--bg-primary)] border border-[color:var(--border-subtle)] rounded font-mono text-sm text-[color:var(--text-primary)]"
              placeholder="/api/..."
              value={url}
              onChange={(e) => setUrl(e.target.value)}
            />
            <button
              onClick={send}
              disabled={loading || !url}
              className="px-4 py-1.5 text-sm rounded bg-[color:var(--accent-cyan)]/20 text-[color:var(--accent-cyan)] border border-[color:var(--accent-cyan)]/30 hover:bg-[color:var(--accent-cyan)]/30 transition-colors disabled:opacity-50"
            >
              Send
            </button>
          </div>
          {(method === 'PUT' || method === 'POST') && (
            <textarea
              className="w-full mb-3 p-2 bg-[color:var(--bg-primary)] border border-[color:var(--border-subtle)] rounded font-mono text-xs text-[color:var(--text-primary)] resize-y h-24"
              placeholder="Request body (JSON)"
              value={body}
              onChange={(e) => setBody(e.target.value)}
            />
          )}
          <pre className="text-xs font-mono text-[color:var(--text-primary)] bg-[color:var(--bg-primary)] p-3 rounded border border-[color:var(--border-subtle)] overflow-auto max-h-[500px]">
            {result || 'Send a request to see the response'}
          </pre>
        </GlassCard>
      </div>
    </div>
  );
}
