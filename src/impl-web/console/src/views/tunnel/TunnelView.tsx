import { useState } from "react";
import { Cable } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { GlassCard } from "@/components/feedback/GlassCard";
import { DataTable, type Column } from "@/components/data/DataTable";
import { StatusBadge } from "@/components/feedback/StatusBadge";
import { ErrorState } from "@/components/feedback/ErrorState";
import { CardSkeleton } from "@/components/feedback/LoadingSkeleton";
import {
  Drawer,
  DrawerSection,
  KeyValueRow,
  LabelChips,
} from "@/components/feedback/Drawer";
import { useServiceTunnels } from "@/queries/hooks";
import type { ServiceTunnelSpec } from "@/api/types";

export default function TunnelView() {
  const tunnels = useServiceTunnels("default");
  const [open, setOpen] = useState<ServiceTunnelSpec | null>(null);

  const columns: Column<ServiceTunnelSpec>[] = [
    {
      key: "name",
      header: "Name",
      accessor: (t) => t.metadata?.name,
      cell: (t) => (
        <span className="font-mono text-[color:var(--text-primary)]">
          {t.metadata?.name}
        </span>
      ),
    },
    {
      key: "action",
      header: "Action",
      accessor: (t) => t.params?.action ?? t.tunnel_type ?? "",
      cell: (t) => {
        const a = t.params?.action ?? t.tunnel_type;
        return a ? (
          <StatusBadge status={a.toUpperCase()} />
        ) : (
          <span className="text-[color:var(--text-muted)]">—</span>
        );
      },
      width: "w-32",
    },
    {
      key: "local",
      header: "Local Underlay",
      accessor: (t) => t.local_underlay_ip ?? "",
      cell: (t) => (
        <span className="font-mono text-xs">{t.local_underlay_ip ?? "—"}</span>
      ),
    },
    {
      key: "remote",
      header: "Remote Underlay",
      accessor: (t) => t.remote_underlay_ip ?? "",
      cell: (t) => (
        <span className="font-mono text-xs">{t.remote_underlay_ip ?? "—"}</span>
      ),
    },
    {
      key: "vni",
      header: "VNI",
      accessor: (t) => t.vni ?? 0,
      align: "right",
      cell: (t) => (
        <span className="font-mono">{t.vni ?? "—"}</span>
      ),
    },
    {
      key: "labels",
      header: "Labels",
      accessor: (t) => Object.keys(t.labels ?? {}).join(","),
      cell: (t) => <LabelChips labels={t.labels} />,
    },
  ];

  if (tunnels.isError) {
    return (
      <ErrorState
        message={tunnels.error?.message ?? "Failed to load service tunnels"}
        onRetry={() => tunnels.refetch()}
      />
    );
  }

  return (
    <div className="animate-fade-in space-y-6">
      <PageHeader
        title="Tunnels"
        subtitle="Service tunnels (NAT, VPN, scrub, private-link, peering)"
      />
      {tunnels.isLoading ? (
        <CardSkeleton />
      ) : (
        <GlassCard className="p-0">
          <DataTable
            columns={columns}
            data={tunnels.data?.items ?? []}
            rowKey={(t) => `${t.metadata?.namespace}/${t.metadata?.name}`}
            onRowClick={(t) => setOpen(t)}
            defaultSort={{ key: "name", direction: "asc" }}
            emptyMessage="No service tunnels defined."
            filterPlaceholder="Filter tunnels…"
          />
        </GlassCard>
      )}

      <Drawer
        open={!!open}
        onClose={() => setOpen(null)}
        title={
          <span className="flex items-center gap-2">
            <Cable size={18} className="text-[color:var(--accent-amber)]" />
            <span className="font-mono">{open?.metadata?.name ?? "tunnel"}</span>
          </span>
        }
        subtitle={
          open?.params?.action ? `action: ${open.params.action}` : null
        }
      >
        {open && <TunnelDetail t={open} />}
      </Drawer>
    </div>
  );
}

function TunnelDetail({ t }: { t: ServiceTunnelSpec }) {
  const params = t.params ?? {};
  return (
    <>
      <DrawerSection title="Identity">
        <KeyValueRow label="Name" value={<code>{t.metadata?.name}</code>} />
        <KeyValueRow
          label="Namespace"
          value={<code>{t.metadata?.namespace}</code>}
        />
        <KeyValueRow label="VNI" value={<code>{t.vni ?? "—"}</code>} />
      </DrawerSection>

      <DrawerSection title="Endpoints">
        <KeyValueRow
          label="Local Underlay"
          value={<code>{t.local_underlay_ip ?? "—"}</code>}
        />
        <KeyValueRow
          label="Remote Underlay"
          value={<code>{t.remote_underlay_ip ?? "—"}</code>}
        />
      </DrawerSection>

      {Object.keys(params).length > 0 && (
        <DrawerSection title="Params">
          {Object.entries(params).map(([k, v]) => (
            <KeyValueRow
              key={k}
              label={k}
              value={
                k === "action" ? (
                  <StatusBadge status={String(v).toUpperCase()} />
                ) : (
                  <code>{String(v)}</code>
                )
              }
            />
          ))}
        </DrawerSection>
      )}

      <DrawerSection title="Labels">
        <LabelChips labels={t.labels} />
      </DrawerSection>
    </>
  );
}