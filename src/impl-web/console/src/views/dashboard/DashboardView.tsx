import { Activity, Cpu, Globe, AlertTriangle, Shield, Route, Cable, Layers } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { StatsCard } from "@/components/feedback/StatsCard";
import { GlassCard } from "@/components/feedback/GlassCard";
import { CapacityGauge } from "@/components/visualization/CapacityGauge";
import { StatusBadge } from "@/components/feedback/StatusBadge";
import { CardSkeleton } from "@/components/feedback/LoadingSkeleton";
import { ErrorState } from "@/components/feedback/ErrorState";
import { useNavigate } from "react-router-dom";
import {
  useFleetSummary,
  useCapacityStats,
  useDashdHealth,
  useEniPlacement,
  useAclPolicies,
  useRoutePolicies,
  useServiceTunnels,
  useHaSets,
} from "@/queries/hooks";
import { formatNumber, formatDuration, stripStatePrefix } from "@/lib/format";
import {
  connectedDpuCount,
  eniPlacementCountsByDpu,
  fleetCapacity,
  fleetDegradedDpus,
  fleetDisconnectedDpus,
  fleetDpuCount,
  fleetDpuStates,
  fleetEniCount,
  fleetHealthyDpus,
  fleetVnetCount,
} from "@/lib/api-helpers";

export default function DashboardView() {
  const navigate = useNavigate();
  const fleet = useFleetSummary();
  const capacity = useCapacityStats();
  const health = useDashdHealth();
  const placements = useEniPlacement();
  const acls = useAclPolicies("default");
  const routes = useRoutePolicies("default");
  const tunnels = useServiceTunnels("default");
  const haSets = useHaSets("default");

  const placementCounts = eniPlacementCountsByDpu(placements.data?.items);

  if (fleet.isError) {
    return (
      <ErrorState
        message={fleet.error.message}
        onRetry={() => fleet.refetch()}
      />
    );
  }

  const fs = fleet.data;
  const cs = capacity.data;
  const hd = health.data;

  const cap = fleetCapacity(cs);
  const dpus = fleetDpuCount(fs);
  const enis = fleetEniCount(fs);
  const vnets = fleetVnetCount(fs);
  const healthy = fleetHealthyDpus(fs);
  const degraded = fleetDegradedDpus(fs);
  const disconnected = fleetDisconnectedDpus(fs);
  const states = fleetDpuStates(fs);
  const drift = fs?.drift_count ?? 0;

  return (
    <div className="animate-fade-in space-y-6">
      <PageHeader
        title="Dashboard"
        subtitle="Fleet health, capacity, and controller status at a glance"
      />

      {/* Stats row */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        {fleet.isLoading ? (
          Array.from({ length: 4 }, (_, i) => <CardSkeleton key={i} />)
        ) : (
          <>
            <StatsCard
              label="DPUs"
              value={dpus}
              icon={
                <Cpu size={20} className="text-[color:var(--accent-cyan)]" />
              }
            />
            <StatsCard
              label="ENIs"
              value={formatNumber(enis)}
              icon={
                <Layers
                  size={20}
                  className="text-[color:var(--accent-purple)]"
                />
              }
            />
            <StatsCard
              label="Vnets"
              value={vnets}
              icon={
                <Globe size={20} className="text-[color:var(--accent-green)]" />
              }
            />
            <StatsCard
              label="Drift Items"
              value={drift}
              icon={
                <AlertTriangle
                  size={20}
                  className={
                    drift > 0
                      ? "text-[color:var(--accent-amber)]"
                      : "text-[color:var(--text-muted)]"
                  }
                />
              }
            />
          </>
        )}
      </div>

      {/* Health + DPU States + dashd Controller */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        {/* Fleet health summary */}
        <GlassCard glow={disconnected > 0 ? "red" : degraded > 0 ? "amber" : "green"}>
          <div className="flex items-center justify-between mb-3">
            <p className="text-[10px] text-[color:var(--text-secondary)] uppercase tracking-[0.14em]">
              Fleet Health
            </p>
            <Activity
              size={14}
              className="text-[color:var(--text-muted)]"
            />
          </div>
          <div className="flex items-end justify-around gap-3">
            <HealthMetric label="Healthy" value={healthy} accent="green" />
            <HealthMetric label="Degraded" value={degraded} accent="amber" />
            <HealthMetric
              label="Offline"
              value={disconnected}
              accent="red"
            />
          </div>
        </GlassCard>

        {/* DPU State Breakdown */}
        <GlassCard>
          <p className="text-[10px] text-[color:var(--text-secondary)] uppercase tracking-[0.14em] mb-3">
            DPU States
          </p>
          {Object.keys(states).length === 0 ? (
            <span className="text-[color:var(--text-muted)] text-sm">
              No state data
            </span>
          ) : (
            <div className="flex flex-wrap gap-2">
              {Object.entries(states).map(([state, count]) => (
                <div
                  key={state}
                  className="flex items-center gap-1.5 px-2 py-1 rounded-md bg-white/5"
                >
                  <StatusBadge status={state} />
                  <span className="text-sm font-mono text-[color:var(--text-primary)]">
                    {count as number}
                  </span>
                </div>
              ))}
            </div>
          )}
        </GlassCard>

        {/* dashd Controller */}
        <GlassCard glow={hd?.leader ? "cyan" : undefined}>
          <p className="text-[10px] text-[color:var(--text-secondary)] uppercase tracking-[0.14em] mb-3">
            dashd Controller
          </p>
          {hd ? (
            <div className="space-y-2 text-sm">
              <Row
                label="Role"
                value={
                  <StatusBadge status={hd.leader ? "LEADER" : "FOLLOWER"} />
                }
              />
              <Row
                label="Connected DPUs"
                value={
                  <span className="font-mono text-[color:var(--text-primary)]">
                    {connectedDpuCount(hd)}
                  </span>
                }
              />
              {hd.uptime_seconds != null && (
                <Row
                  label="Uptime"
                  value={
                    <span className="font-mono text-[color:var(--text-primary)]">
                      {formatDuration(hd.uptime_seconds)}
                    </span>
                  }
                />
              )}
              {hd.cluster_size != null && (
                <Row
                  label="Cluster Size"
                  value={
                    <span className="font-mono text-[color:var(--text-primary)]">
                      {hd.cluster_size}
                    </span>
                  }
                />
              )}
              {hd.leader_id && (
                <Row
                  label="Leader ID"
                  value={
                    <span
                      className="font-mono text-[color:var(--text-primary)] truncate max-w-[160px]"
                      title={hd.leader_id}
                    >
                      {hd.leader_id.slice(0, 12)}…
                    </span>
                  }
                />
              )}
            </div>
          ) : health.isLoading ? (
            <CardSkeleton />
          ) : (
            <span className="text-[color:var(--text-muted)] text-sm">
              No health data
            </span>
          )}
        </GlassCard>
      </div>

      {/* Capacity gauges */}
      <GlassCard>
        <div className="flex items-center justify-between mb-4">
          <p className="text-[10px] text-[color:var(--text-secondary)] uppercase tracking-[0.14em]">
            Fleet Capacity
          </p>
          {capacity.isFetching && (
            <span className="text-[10px] text-[color:var(--text-muted)] animate-pulse">
              refreshing…
            </span>
          )}
        </div>
        {cs ? (
          <div className="grid grid-cols-2 md:grid-cols-4 gap-6">
            <CapacityGauge label="ENIs" used={cap.enisUsed} max={cap.enisMax} />
            <CapacityGauge
              label="Routes"
              used={cap.routesUsed}
              max={cap.routesMax}
            />
            <CapacityGauge
              label="ACL Rules"
              used={cap.aclRulesUsed}
              max={cap.aclRulesMax}
            />
            <CapacityGauge
              label="Flows"
              used={cap.flowsUsed}
              max={cap.flowsMax}
            />
          </div>
        ) : (
          <div className="flex justify-center py-4 text-[color:var(--text-muted)] text-sm">
            {capacity.isLoading
              ? "Loading capacity data…"
              : "No capacity data available"}
          </div>
        )}
      </GlassCard>

      {/* Per-DPU capacity table */}
      {cs && (cs.per_dpu?.length ?? 0) > 0 && (
        <GlassCard>
          <p className="text-[10px] text-[color:var(--text-secondary)] uppercase tracking-[0.14em] mb-3">
            Per-DPU Snapshot
          </p>
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="text-left text-[color:var(--text-muted)] border-b border-[color:var(--border-subtle)]">
                  <th className="font-medium px-2 py-2">DPU</th>
                  <th className="font-medium px-2 py-2">State</th>
                  <th className="font-medium px-2 py-2 text-right">ENIs</th>
                  <th className="font-medium px-2 py-2 text-right">Routes</th>
                </tr>
              </thead>
              <tbody>
                {(cs.per_dpu ?? []).map((row) => {
                  const id = row.dpu_id ?? row.id ?? "?";
                  const capEni = row.eni_count ?? row.enis_used ?? 0;
                  const placementEni = placementCounts[id] ?? 0;
                  const eniUsed = placementEni > 0 ? placementEni : capEni;
                  const eniMax = row.eni_max ?? row.enis_max ?? 0;
                  return (
                    <tr
                      key={id}
                      className="border-b border-[color:var(--border-subtle)] last:border-0 hover:bg-white/[0.02]"
                    >
                      <td className="px-2 py-2 font-mono text-[color:var(--text-primary)]">
                        {id}
                      </td>
                      <td className="px-2 py-2">
                        <StatusBadge status={String(row.state)} />
                      </td>
                      <td className="px-2 py-2 text-right font-mono">
                        {eniUsed}
                        <span className="text-[color:var(--text-muted)]">
                          {" / "}
                          {eniMax}
                        </span>
                      </td>
                      <td className="px-2 py-2 text-right font-mono">
                        {row.route_count ?? row.routes_used ?? 0}
                        <span className="text-[color:var(--text-muted)]">
                          {" / "}
                          {row.route_max ?? row.routes_max ?? 0}
                        </span>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </GlassCard>
      )}

      {/* Resource summary — counts derived from the live REST lists, clickable. */}
      <GlassCard>
        <p className="text-[10px] text-[color:var(--text-secondary)] uppercase tracking-[0.14em] mb-3">
          Resources
        </p>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
          <ResourceTile
            icon={<Shield size={16} className="text-[color:var(--accent-cyan)]" />}
            label="ACL Policies"
            value={
              acls.data?.items?.length ??
              fs?.acl_policy_count ??
              fs?.total_acl_policies
            }
            onClick={() => navigate("/policies")}
          />
          <ResourceTile
            icon={<Route size={16} className="text-[color:var(--accent-purple)]" />}
            label="Route Policies"
            value={
              routes.data?.items?.length ??
              fs?.route_policy_count ??
              fs?.total_route_policies
            }
            onClick={() => navigate("/routing")}
          />
          <ResourceTile
            icon={<Cable size={16} className="text-[color:var(--accent-green)]" />}
            label="Service Tunnels"
            value={
              tunnels.data?.items?.length ??
              fs?.service_tunnel_count ??
              fs?.total_service_tunnels
            }
            onClick={() => navigate("/tunnels")}
          />
          <ResourceTile
            icon={<Activity size={16} className="text-[color:var(--accent-amber)]" />}
            label="HA Sets"
            value={
              haSets.data?.items?.length ??
              fs?.ha_set_count ??
              fs?.total_ha_sets
            }
            onClick={() => navigate("/policies")}
          />
        </div>
      </GlassCard>

      {/* Last-seen DPU roster (compact) */}
      {fs?.dpus && fs.dpus.length > 0 && (
        <GlassCard>
          <p className="text-[10px] text-[color:var(--text-secondary)] uppercase tracking-[0.14em] mb-3">
            DPU Roster
          </p>
          <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-5 gap-2 text-xs">
            {fs.dpus.map((d) => (
              <div
                key={d.id}
                className="flex items-center gap-2 px-2 py-1.5 rounded-md bg-white/[0.02] border border-[color:var(--border-subtle)]"
              >
                <StatusBadge status={d.state} />
                <span className="font-mono text-[color:var(--text-primary)] truncate">
                  {d.id}
                </span>
              </div>
            ))}
          </div>
          <p className="text-[10px] text-[color:var(--text-muted)] mt-3">
            {fs.dpus.length} DPUs reporting · last sync{" "}
            {fs.timestamp
              ? new Date(fs.timestamp).toLocaleTimeString()
              : "—"}
          </p>
        </GlassCard>
      )}
    </div>
  );
}

/* ── Small internal helpers ──────────────────────────────── */

function HealthMetric({
  label,
  value,
  accent,
}: {
  label: string;
  value: number;
  accent: "green" | "amber" | "red";
}) {
  const colorVar =
    accent === "green"
      ? "var(--accent-green)"
      : accent === "amber"
        ? "var(--accent-amber)"
        : "var(--accent-red)";
  return (
    <div className="text-center">
      <span
        className="block text-3xl font-bold tabular-nums"
        style={{ color: colorVar }}
      >
        {value}
      </span>
      <span className="text-[10px] text-[color:var(--text-muted)] uppercase tracking-wider">
        {label}
      </span>
    </div>
  );
}

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex justify-between items-center">
      <span className="text-[color:var(--text-secondary)]">{label}</span>
      {value}
    </div>
  );
}

function ResourceTile({
  icon,
  label,
  value,
  onClick,
}: {
  icon: React.ReactNode;
  label: string;
  value: number | undefined;
  onClick?: () => void;
}) {
  const isInteractive = !!onClick && (value ?? 0) > 0;
  const Tag = isInteractive ? "button" : "div";
  return (
    <Tag
      type={isInteractive ? "button" : undefined}
      onClick={isInteractive ? onClick : undefined}
      className={
        isInteractive
          ? "flex items-center gap-3 text-left rounded-md p-1 -m-1 hover:bg-white/5 transition-colors"
          : "flex items-center gap-3"
      }
    >
      <div className="p-2 rounded-md bg-white/5">{icon}</div>
      <div className="min-w-0">
        <span className="block text-[10px] text-[color:var(--text-muted)] uppercase tracking-wider">
          {label}
        </span>
        <span
          className={
            isInteractive
              ? "block text-lg font-mono tabular-nums text-[color:var(--accent-cyan)] hover:underline"
              : "block text-lg font-mono tabular-nums text-[color:var(--text-primary)]"
          }
        >
          {value == null ? "—" : value}
        </span>
      </div>
    </Tag>
  );
}

// Re-export to silence unused-import warnings during incremental refactor.
export { stripStatePrefix };