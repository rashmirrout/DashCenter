import { useState } from 'react';
import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { RESOURCE_KINDS, type ResourceKind } from '@/lib/constants';
import { useDeleteResource, useReconcile } from '@/queries/hooks';

export default function AdminOpsView() {
  const [selectedKind, setSelectedKind] = useState<ResourceKind>('vnets');
  const deleteMut = useDeleteResource();
  const reconcile = useReconcile();

  return (
    <div className="animate-fade-in">
      <PageHeader title="Admin Operations" subtitle="Create, edit, delete resources" />
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <GlassCard>
          <p className="text-xs text-text-secondary uppercase mb-3">Resource Type</p>
          <div className="space-y-1">
            {RESOURCE_KINDS.map((k) => (
              <button
                key={k}
                onClick={() => setSelectedKind(k)}
                className={`w-full text-left px-3 py-1.5 rounded text-sm ${
                  selectedKind === k ? 'bg-bg-elevated text-accent-cyan' : 'text-text-secondary hover:bg-bg-elevated'
                }`}
              >
                {k}
              </button>
            ))}
          </div>
        </GlassCard>
        <GlassCard className="lg:col-span-2">
          <p className="text-xs text-text-secondary uppercase mb-3">{selectedKind} Operations</p>
          <div className="space-y-3">
            <div className="p-3 border border-border rounded-lg">
              <p className="text-sm font-semibold mb-2">Create / Edit</p>
              <p className="text-xs text-text-muted">Paste YAML or JSON spec and submit via PUT endpoint</p>
              <textarea
                className="w-full mt-2 p-2 bg-bg-primary border border-border rounded font-mono text-xs text-text-primary resize-y h-32"
                placeholder={`{\n  "metadata": { "namespace": "default", "name": "..." },\n  ...\n}`}
              />
              <button className="mt-2 px-3 py-1.5 text-sm rounded bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/30 hover:bg-accent-cyan/30 transition-colors">
                Submit
              </button>
            </div>
            <div className="p-3 border border-border rounded-lg">
              <p className="text-sm font-semibold mb-2">Delete</p>
              <div className="flex gap-2">
                <input className="flex-1 px-2 py-1 bg-bg-primary border border-border rounded font-mono text-sm text-text-primary" placeholder="resource name" />
                <button
                  onClick={() => deleteMut.mutate({ kind: selectedKind, ns: 'default', name: '' })}
                  className="px-3 py-1 text-sm rounded bg-accent-red/20 text-accent-red border border-accent-red/30 hover:bg-accent-red/30 transition-colors"
                >
                  Delete
                </button>
              </div>
            </div>
            <button
              onClick={() => reconcile.mutate({})}
              disabled={reconcile.isPending}
              className="px-3 py-1.5 text-sm rounded bg-accent-amber/20 text-accent-amber border border-accent-amber/30 hover:bg-accent-amber/30 transition-colors disabled:opacity-50"
            >
              {reconcile.isPending ? 'Reconciling...' : 'Reconcile All'}
            </button>
          </div>
        </GlassCard>
      </div>
    </div>
  );
}