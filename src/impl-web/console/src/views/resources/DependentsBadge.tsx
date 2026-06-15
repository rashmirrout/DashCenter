/* ═══════════════════════════════════════════════════════════════
 * DependentsBadge — table-cell badge showing how many resources
 * depend on the row's resource.
 *
 * Renders a clickable pill like "2 ENIs · 1 mapping". On hover,
 * shows a tooltip listing each dependent's name + field path.
 * "0 dependents" renders as a muted dot (safe to delete).
 *
 * Used by the Resources tab tables (Vnets, ENIs, Service Tunnels)
 * to surface downstream impact at a glance.
 * ═══════════════════════════════════════════════════════════════ */

import { useState } from "react";
import type { ResourceKind } from "@/lib/constants";
import { KIND_LABELS, KIND_LABELS_PLURAL, type ResourceDepInfo } from "@/lib/resource-deps";

interface DependentsBadgeProps {
  info: ResourceDepInfo | undefined;
}

export function DependentsBadge({ info }: DependentsBadgeProps) {
  const [hover, setHover] = useState(false);

  if (!info || info.totalDependents === 0) {
    return (
      <span
        className="inline-flex items-center gap-1 text-[10px] text-[color:var(--text-muted)] font-mono"
        title="No dependents — safe to delete"
      >
        <span className="w-1.5 h-1.5 rounded-full bg-[color:var(--text-muted)] opacity-50" />
        none
      </span>
    );
  }

  const entries = Object.entries(info.byKind) as Array<[ResourceKind, number]>;

  return (
    <div
      className="relative inline-block"
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
    >
      <div className="flex flex-wrap gap-1">
        {entries.map(([kind, count]) => (
          <span
            key={kind}
            className="inline-flex items-center gap-1 text-[10px] font-mono px-1.5 py-0.5 rounded bg-[color:var(--accent-amber)]/15 text-[color:var(--accent-amber)] border border-[color:var(--accent-amber)]/30"
          >
            {count} {count === 1 ? KIND_LABELS[kind] : KIND_LABELS_PLURAL[kind]}
          </span>
        ))}
      </div>
      {hover && info.dependents && info.dependents.length > 0 && (
        <div
          className="absolute z-50 top-full left-0 mt-1 min-w-[220px] max-w-[320px] p-2 rounded-md border border-[color:var(--border-subtle)] bg-[color:var(--bg-elevated)] shadow-2xl text-[10px] font-mono"
          style={{ pointerEvents: "none" }}
        >
          <div className="text-[color:var(--text-secondary)] mb-1 not-italic uppercase tracking-wider text-[9px] font-sans">
            Dependents ({info.totalDependents})
          </div>
          <ul className="space-y-0.5">
            {info.dependents.slice(0, 8).map((d, i) => (
              <li key={i} className="flex items-baseline gap-1.5">
                <span className="text-[color:var(--accent-cyan)]">
                  {KIND_LABELS[d.kind]}
                </span>
                <span className="text-[color:var(--text-primary)]">{d.name}</span>
                <span className="text-[color:var(--text-muted)] text-[9px]">
                  · {d.field}
                </span>
              </li>
            ))}
            {info.dependents.length > 8 && (
              <li className="text-[color:var(--text-muted)] italic">
                …and {info.dependents.length - 8} more
              </li>
            )}
          </ul>
        </div>
      )}
    </div>
  );
}

/* ── DependenciesBadge — upstream references for a row ──────── */

interface DependenciesBadgeProps {
  refs: Array<{ kind: ResourceKind; name: string; field: string }>;
}

/** Renders the resources that THIS row references (upstream). */
export function DependenciesBadge({ refs }: DependenciesBadgeProps) {
  const [hover, setHover] = useState(false);

  if (refs.length === 0) {
    return (
      <span className="text-[10px] text-[color:var(--text-muted)] font-mono">
        —
      </span>
    );
  }

  // Group by kind so we render "Vnet: foo · ENI: bar, baz" compactly
  const byKind = new Map<ResourceKind, string[]>();
  for (const r of refs) {
    const arr = byKind.get(r.kind) ?? [];
    if (!arr.includes(r.name)) arr.push(r.name);
    byKind.set(r.kind, arr);
  }

  const entries = Array.from(byKind.entries());

  return (
    <div
      className="relative inline-block"
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
    >
      <div className="flex flex-wrap gap-1 max-w-[240px]">
        {entries.slice(0, 2).map(([kind, names]) => (
          <span
            key={kind}
            className="inline-flex items-center gap-1 text-[10px] font-mono px-1.5 py-0.5 rounded bg-[color:var(--accent-cyan)]/10 text-[color:var(--accent-cyan)] border border-[color:var(--accent-cyan)]/25"
          >
            <span className="uppercase tracking-wider text-[8px] opacity-70">
              {KIND_LABELS[kind]}
            </span>
            <span>{names[0]}{names.length > 1 ? ` +${names.length - 1}` : ""}</span>
          </span>
        ))}
        {entries.length > 2 && (
          <span className="text-[10px] text-[color:var(--text-muted)]">
            +{entries.length - 2}
          </span>
        )}
      </div>
      {hover && (
        <div
          className="absolute z-50 top-full left-0 mt-1 min-w-[220px] max-w-[320px] p-2 rounded-md border border-[color:var(--border-subtle)] bg-[color:var(--bg-elevated)] shadow-2xl text-[10px] font-mono"
          style={{ pointerEvents: "none" }}
        >
          <div className="text-[color:var(--text-secondary)] mb-1 not-italic uppercase tracking-wider text-[9px] font-sans">
            References ({refs.length})
          </div>
          <ul className="space-y-0.5">
            {refs.slice(0, 8).map((r, i) => (
              <li key={i} className="flex items-baseline gap-1.5">
                <span className="text-[color:var(--accent-purple)]">
                  {KIND_LABELS[r.kind]}
                </span>
                <span className="text-[color:var(--text-primary)]">{r.name}</span>
                <span className="text-[color:var(--text-muted)] text-[9px]">
                  · {r.field}
                </span>
              </li>
            ))}
            {refs.length > 8 && (
              <li className="text-[color:var(--text-muted)] italic">
                …and {refs.length - 8} more
              </li>
            )}
          </ul>
        </div>
      )}
    </div>
  );
}