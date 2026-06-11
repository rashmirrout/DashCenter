import { useState } from 'react';
import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { useSimulateFlow } from '@/queries/hooks';
import type { SimulateRequest } from '@/api/types';

export default function FlowTraceView() {
  const simulate = useSimulateFlow();
  const [form, setForm] = useState<SimulateRequest>({
    vnet_name: '', src_ip: '', dst_ip: '', protocol: 6, src_port: 0, dst_port: 80, direction: 'IN',
  });

  const update = (field: string, value: string | number) =>
    setForm((f) => ({ ...f, [field]: value }));

  return (
    <div className="animate-fade-in">
      <PageHeader title="Flow Trace" subtitle="Simulate packet flow through the pipeline" />
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <GlassCard>
          <p className="text-xs text-text-secondary uppercase mb-3">Trace Input</p>
          <div className="space-y-3">
            <input className="w-full px-2 py-1.5 bg-bg-primary border border-border rounded font-mono text-sm" placeholder="Vnet name" value={form.vnet_name} onChange={(e) => update('vnet_name', e.target.value)} />
            <div className="grid grid-cols-2 gap-2">
              <input className="px-2 py-1.5 bg-bg-primary border border-border rounded font-mono text-sm" placeholder="Source IP" value={form.src_ip} onChange={(e) => update('src_ip', e.target.value)} />
              <input className="px-2 py-1.5 bg-bg-primary border border-border rounded font-mono text-sm" placeholder="Dest IP" value={form.dst_ip} onChange={(e) => update('dst_ip', e.target.value)} />
            </div>
            <div className="grid grid-cols-3 gap-2">
              <input className="px-2 py-1.5 bg-bg-primary border border-border rounded font-mono text-sm" placeholder="Protocol" type="number" value={form.protocol} onChange={(e) => update('protocol', Number(e.target.value))} />
              <input className="px-2 py-1.5 bg-bg-primary border border-border rounded font-mono text-sm" placeholder="Src Port" type="number" value={form.src_port ?? 0} onChange={(e) => update('src_port', Number(e.target.value))} />
              <input className="px-2 py-1.5 bg-bg-primary border border-border rounded font-mono text-sm" placeholder="Dst Port" type="number" value={form.dst_port ?? 80} onChange={(e) => update('dst_port', Number(e.target.value))} />
            </div>
            <select className="w-full px-2 py-1.5 bg-bg-primary border border-border rounded text-sm" value={form.direction} onChange={(e) => update('direction', e.target.value)}>
              <option value="IN">Inbound</option>
              <option value="OUT">Outbound</option>
            </select>
            <button
              onClick={() => simulate.mutate(form)}
              disabled={simulate.isPending || !form.vnet_name || !form.src_ip || !form.dst_ip}
              className="w-full px-3 py-2 text-sm rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/30 hover:bg-accent-cyan/30 transition-colors disabled:opacity-50"
            >
              {simulate.isPending ? 'Simulating...' : 'Simulate'}
            </button>
          </div>
        </GlassCard>
        <GlassCard>
          <p className="text-xs text-text-secondary uppercase mb-3">Result</p>
          {simulate.data ? (
            <div className="space-y-3">
              <div className="flex items-center gap-2">
                <span className="text-sm">Verdict:</span>
                <span className={`font-bold ${simulate.data.verdict === 'ALLOW' ? 'text-accent-green' : 'text-accent-red'}`}>
                  {simulate.data.verdict}
                </span>
              </div>
              <div className="space-y-1">
                {simulate.data.stages.map((s, i) => (
                  <div key={i} className="flex items-center gap-2 text-sm py-1 border-b border-border last:border-0">
                    <span className={`w-2 h-2 rounded-full ${s.result === 'PASS' ? 'bg-[var(--accent-green)]' : s.result === 'DENY' ? 'bg-[var(--accent-red)]' : 'bg-[var(--text-muted)]'}`} />
                    <span className="font-mono">{s.name}</span>
                    <span className="ml-auto text-text-muted text-xs">{s.result}</span>
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <p className="text-sm text-text-muted">Run a simulation to see results</p>
          )}
        </GlassCard>
      </div>
    </div>
  );
}