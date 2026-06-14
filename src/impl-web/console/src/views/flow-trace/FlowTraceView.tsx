import { useState, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import { PageHeader } from "@/components/layout/PageHeader";
import { GlassCard } from "@/components/feedback/GlassCard";
import { useTraceFlow, useExplainMatch, useEniList } from "@/queries/hooks";
import {
  TRACE_VERDICTS,
  EXPLAIN_SUBJECTS,
  type TraceFlowInput,
  type TraceFlowResponse,
  type ExplainMatchResponse,
} from "@/api/types";
import { cn } from "@/lib/cn";
import {
  Search,
  Play,
  CheckCircle2,
  XCircle,
  AlertTriangle,
  ArrowRight,
  Shield,
  Route,
  Network,
  ChevronDown,
  ChevronRight,
} from "lucide-react";

/* ── Constants ─────────────────────────────────────────────── */

const PROTOCOLS = [
  { value: "tcp", label: "TCP" },
  { value: "udp", label: "UDP" },
  { value: "icmp", label: "ICMP" },
];

const DIRECTIONS = [
  { value: 1, label: "Inbound" },
  { value: 2, label: "Outbound" },
];

const SUBJECT_OPTIONS = [
  { value: EXPLAIN_SUBJECTS.ACL, label: "ACL Rules", icon: Shield },
  { value: EXPLAIN_SUBJECTS.ROUTE, label: "Routes", icon: Route },
  { value: EXPLAIN_SUBJECTS.VNET_MAPPING, label: "VNet Mappings", icon: Network },
];

/* ── Shared Input Form ─────────────────────────────────────── */

function FlowInputForm({
  flow,
  onChange,
  eniNames,
}: {
  flow: TraceFlowInput;
  onChange: (f: TraceFlowInput) => void;
  eniNames: string[];
}) {
  const update = (field: string, value: string | number) =>
    onChange({ ...flow, [field]: value });

  return (
    <div className="space-y-3">
      {/* ENI selector */}
      <div>
        <label className="block text-[10px] uppercase tracking-wider text-[color:var(--text-muted)] mb-1">
          ENI
        </label>
        {eniNames.length > 0 ? (
          <select
            className="w-full px-2 py-1.5 bg-[color:var(--bg-primary)] border border-[color:var(--border-subtle)] rounded font-mono text-sm text-[color:var(--text-primary)]"
            value={flow.eni_name}
            onChange={(e) => update("eni_name", e.target.value)}
          >
            <option value="">Select ENI…</option>
            {eniNames.map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
        ) : (
          <input
            className="w-full px-2 py-1.5 bg-[color:var(--bg-primary)] border border-[color:var(--border-subtle)] rounded font-mono text-sm text-[color:var(--text-primary)]"
            placeholder="eni-bank-web-04"
            value={flow.eni_name}
            onChange={(e) => update("eni_name", e.target.value)}
          />
        )}
      </div>

      {/* Direction + Protocol */}
      <div className="grid grid-cols-2 gap-2">
        <div>
          <label className="block text-[10px] uppercase tracking-wider text-[color:var(--text-muted)] mb-1">
            Direction
          </label>
          <select
            className="w-full px-2 py-1.5 bg-[color:var(--bg-primary)] border border-[color:var(--border-subtle)] rounded text-sm text-[color:var(--text-primary)]"
            value={flow.direction}
            onChange={(e) => update("direction", Number(e.target.value))}
          >
            {DIRECTIONS.map((d) => (
              <option key={d.value} value={d.value}>
                {d.label}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label className="block text-[10px] uppercase tracking-wider text-[color:var(--text-muted)] mb-1">
            Protocol
          </label>
          <select
            className="w-full px-2 py-1.5 bg-[color:var(--bg-primary)] border border-[color:var(--border-subtle)] rounded text-sm text-[color:var(--text-primary)]"
            value={flow.protocol}
            onChange={(e) => update("protocol", e.target.value)}
          >
            {PROTOCOLS.map((p) => (
              <option key={p.value} value={p.value}>
                {p.label}
              </option>
            ))}
          </select>
        </div>
      </div>

      {/* Src / Dst IP */}
      <div className="grid grid-cols-2 gap-2">
        <div>
          <label className="block text-[10px] uppercase tracking-wider text-[color:var(--text-muted)] mb-1">
            Source IP
          </label>
          <input
            className="w-full px-2 py-1.5 bg-[color:var(--bg-primary)] border border-[color:var(--border-subtle)] rounded font-mono text-sm text-[color:var(--text-primary)]"
            placeholder="203.0.113.10"
            value={flow.src_ip}
            onChange={(e) => update("src_ip", e.target.value)}
          />
        </div>
        <div>
          <label className="block text-[10px] uppercase tracking-wider text-[color:var(--text-muted)] mb-1">
            Dest IP
          </label>
          <input
            className="w-full px-2 py-1.5 bg-[color:var(--bg-primary)] border border-[color:var(--border-subtle)] rounded font-mono text-sm text-[color:var(--text-primary)]"
            placeholder="192.168.11.4"
            value={flow.dst_ip}
            onChange={(e) => update("dst_ip", e.target.value)}
          />
        </div>
      </div>

      {/* Ports */}
      <div className="grid grid-cols-2 gap-2">
        <div>
          <label className="block text-[10px] uppercase tracking-wider text-[color:var(--text-muted)] mb-1">
            Src Port
          </label>
          <input
            className="w-full px-2 py-1.5 bg-[color:var(--bg-primary)] border border-[color:var(--border-subtle)] rounded font-mono text-sm text-[color:var(--text-primary)]"
            type="number"
            placeholder="0"
            value={flow.src_port ?? 0}
            onChange={(e) => update("src_port", Number(e.target.value))}
          />
        </div>
        <div>
          <label className="block text-[10px] uppercase tracking-wider text-[color:var(--text-muted)] mb-1">
            Dst Port
          </label>
          <input
            className="w-full px-2 py-1.5 bg-[color:var(--bg-primary)] border border-[color:var(--border-subtle)] rounded font-mono text-sm text-[color:var(--text-primary)]"
            type="number"
            placeholder="443"
            value={flow.dst_port ?? 443}
            onChange={(e) => update("dst_port", Number(e.target.value))}
          />
        </div>
      </div>
    </div>
  );
}

/* ── Preset Scenarios ──────────────────────────────────────── */

const PRESETS: Array<{ label: string; desc: string; flow: Partial<TraceFlowInput> }> = [
  {
    label: "HTTPS Allow",
    desc: "TCP/443 inbound to bank-web",
    flow: {
      direction: 1,
      eni_name: "eni-bank-web-04",
      src_ip: "203.0.113.10",
      dst_ip: "192.168.11.4",
      dst_port: 443,
      protocol: "tcp",
    },
  },
  {
    label: "SSH Deny",
    desc: "TCP/22 blocked by ACL",
    flow: {
      direction: 1,
      eni_name: "eni-bank-web-04",
      src_ip: "203.0.113.10",
      dst_ip: "192.168.11.4",
      dst_port: 22,
      protocol: "tcp",
    },
  },
  {
    label: "No Mapping",
    desc: "Valid route, no VnetMapping",
    flow: {
      direction: 1,
      eni_name: "eni-bank-web-04",
      src_ip: "203.0.113.10",
      dst_ip: "192.168.11.99",
      dst_port: 443,
      protocol: "tcp",
    },
  },
  {
    label: "Gaming UDP",
    desc: "UDP/7780 from lobby subnet",
    flow: {
      direction: 1,
      eni_name: "eni-gaming-match-01",
      src_ip: "192.168.61.50",
      dst_ip: "192.168.62.1",
      dst_port: 7780,
      protocol: "udp",
    },
  },
];

/* ── Verdict Badge ─────────────────────────────────────────── */

function VerdictBadge({ verdict }: { verdict: number }) {
  const info = TRACE_VERDICTS[verdict] ?? { label: `VERDICT_${verdict}`, color: "gray" };
  const colorMap: Record<string, string> = {
    green:
      "bg-emerald-500/20 text-emerald-400 border-emerald-500/30 shadow-[0_0_12px_rgba(16,185,129,0.3)]",
    red: "bg-red-500/20 text-red-400 border-red-500/30 shadow-[0_0_12px_rgba(239,68,68,0.3)]",
    amber:
      "bg-amber-500/20 text-amber-400 border-amber-500/30 shadow-[0_0_12px_rgba(245,158,11,0.3)]",
    gray: "bg-white/10 text-[color:var(--text-secondary)] border-white/20",
  };

  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg border font-mono text-sm font-bold",
        colorMap[info.color] ?? colorMap.gray
      )}
    >
      {info.color === "green" && <CheckCircle2 size={14} />}
      {info.color === "red" && <XCircle size={14} />}
      {info.color === "amber" && <AlertTriangle size={14} />}
      {info.label}
    </span>
  );
}

/* ── Trace Step Renderer ───────────────────────────────────── */

function TraceSteps({ trace }: { trace: string[] }) {
  return (
    <div className="space-y-1">
      {trace.map((line, i) => {
        const isAllow = /ACL ALLOW|VNET_MAPPING:.*→/.test(line);
        const isDeny = /ACL DENY|DROP/.test(line);
        const isSkip = /ACL skip/.test(line);
        const isInput = line.startsWith("INPUT:");
        const isRoute = line.startsWith("ROUTE:");
        const isMapping = line.startsWith("VNET_MAPPING:");

        let icon = <ArrowRight size={12} className="shrink-0 text-[color:var(--text-muted)]" />;
        let textColor = "text-[color:var(--text-secondary)]";

        if (isAllow) {
          icon = <CheckCircle2 size={12} className="shrink-0 text-emerald-400" />;
          textColor = "text-emerald-400";
        } else if (isDeny) {
          icon = <XCircle size={12} className="shrink-0 text-red-400" />;
          textColor = "text-red-400";
        } else if (isSkip) {
          icon = <ChevronRight size={12} className="shrink-0 text-[color:var(--text-muted)]" />;
          textColor = "text-[color:var(--text-muted)]";
        } else if (isInput) {
          icon = <Play size={12} className="shrink-0 text-[color:var(--accent-cyan)]" />;
          textColor = "text-[color:var(--accent-cyan)]";
        } else if (isRoute) {
          icon = <Route size={12} className="shrink-0 text-violet-400" />;
          textColor = "text-violet-400";
        } else if (isMapping) {
          icon = <Network size={12} className="shrink-0 text-amber-400" />;
          textColor = "text-amber-400";
        }

        return (
          <div key={i} className="flex items-start gap-2">
            <span className="mt-0.5">{icon}</span>
            <span className={cn("font-mono text-xs leading-relaxed", textColor)}>{line}</span>
          </div>
        );
      })}
    </div>
  );
}

/* ── Matched Objects Panel ─────────────────────────────────── */

function MatchedObjects({ result }: { result: TraceFlowResponse }) {
  const items = [
    result.matched_acl_rule && {
      label: "ACL Rule",
      name: result.matched_acl_rule.policy_name,
      detail: `priority=${result.matched_acl_rule.priority} action=${result.matched_acl_rule.action}`,
      icon: Shield,
      color: "text-emerald-400",
    },
    result.matched_route && {
      label: "Route",
      name: result.matched_route.policy_name,
      detail: `${result.matched_route.prefix} → ${result.matched_route.next_hop_type}/${result.matched_route.next_hop_target}`,
      icon: Route,
      color: "text-violet-400",
    },
    result.matched_vnet_mapping && {
      label: "VNet Mapping",
      name: result.matched_vnet_mapping.vnet_name,
      detail: `${result.matched_vnet_mapping.ip_address} → ${result.matched_vnet_mapping.action}`,
      icon: Network,
      color: "text-amber-400",
    },
  ].filter(Boolean);

  if (items.length === 0) return null;

  return (
    <div className="space-y-2 mt-4">
      <p className="text-[10px] uppercase tracking-wider text-[color:var(--text-muted)]">
        Matched Objects
      </p>
      {items.map((item) => {
        if (!item) return null;
        const Icon = item.icon;
        return (
          <div
            key={item.label}
            className="flex items-center gap-2 px-3 py-2 rounded-md bg-[color:var(--bg-primary)] border border-[color:var(--border-subtle)]"
          >
            <Icon size={14} className={cn("shrink-0", item.color)} />
            <div className="min-w-0 flex-1">
              <div className="text-xs font-medium text-[color:var(--text-primary)]">
                {item.label}: <span className="font-mono">{item.name}</span>
              </div>
              <div className="text-[10px] font-mono text-[color:var(--text-muted)] truncate">
                {item.detail}
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}

/* ── Explain Match Results ─────────────────────────────────── */

function ExplainResults({ result }: { result: ExplainMatchResponse }) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  const toggle = (id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  return (
    <div className="space-y-1">
      <div className="flex items-center gap-2 mb-2">
        <p className="text-[10px] uppercase tracking-wider text-[color:var(--text-muted)]">
          Winner
        </p>
        <span className="font-mono text-xs text-[color:var(--accent-cyan)] bg-[color:var(--accent-cyan)]/10 px-2 py-0.5 rounded border border-[color:var(--accent-cyan)]/25">
          {result.selected_candidate_id}
        </span>
      </div>
      {result.candidates.map((c) => {
        const isWinner = c.candidate_id === result.selected_candidate_id;
        const isExpanded = expanded.has(c.candidate_id);
        return (
          <div
            key={c.candidate_id}
            className={cn(
              "rounded-md border px-3 py-2 cursor-pointer transition-colors",
              isWinner
                ? "border-[color:var(--accent-cyan)]/40 bg-[color:var(--accent-cyan)]/5 shadow-[0_0_8px_rgba(0,212,255,0.15)]"
                : "border-[color:var(--border-subtle)] bg-[color:var(--bg-primary)] hover:bg-white/5"
            )}
            onClick={() => toggle(c.candidate_id)}
          >
            <div className="flex items-center gap-2">
              {isExpanded ? (
                <ChevronDown size={12} className="text-[color:var(--text-muted)]" />
              ) : (
                <ChevronRight size={12} className="text-[color:var(--text-muted)]" />
              )}
              {c.matched ? (
                <CheckCircle2 size={12} className="text-emerald-400 shrink-0" />
              ) : (
                <XCircle size={12} className="text-[color:var(--text-muted)] shrink-0" />
              )}
              <span className="font-mono text-xs text-[color:var(--text-primary)] truncate flex-1">
                {c.candidate_id}
              </span>
              {c.priority !== undefined && (
                <span className="text-[10px] text-[color:var(--text-muted)] font-mono">
                  p={c.priority}
                </span>
              )}
            </div>
            {isExpanded && (
              <div className="mt-2 pl-6 text-xs font-mono text-[color:var(--text-secondary)] leading-relaxed">
                {c.reason}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

/* ── Main View ─────────────────────────────────────────────── */

type TabId = "trace" | "explain";

export default function FlowTraceView() {
  const [tab, setTab] = useState<TabId>("trace");

  // Seed the form from URL query params so deep-links like
  //   /flow-trace?eni_name=eni-bank-web-04&vni=2001
  // (used by the ENI detail page's "Trace a flow…" button) land
  // with the relevant fields already filled in. Reading once on
  // mount via useState's lazy initializer keeps the form fully
  // controllable afterwards — we don't fight user edits.
  const [searchParams] = useSearchParams();
  const [flow, setFlow] = useState<TraceFlowInput>(() => {
    const eniName = searchParams.get("eni_name") ?? "";
    const vniRaw = searchParams.get("vni");
    return {
      direction: 1,
      eni_name: eniName,
      src_ip: "",
      dst_ip: "",
      src_port: 0,
      dst_port: 443,
      protocol: "tcp",
      vni: vniRaw ?? undefined,
    };
  });
  const [subject, setSubject] = useState<number>(EXPLAIN_SUBJECTS.ACL);

  const traceFlow = useTraceFlow();
  const explainMatch = useExplainMatch();
  const { data: eniData } = useEniList("default");

  const eniNames = useMemo(
    () =>
      (eniData?.items ?? [])
        .map((e) => e.metadata?.name)
        .filter((n): n is string => !!n)
        .sort(),
    [eniData]
  );

  const canSubmit = !!flow.eni_name && !!flow.src_ip && !!flow.dst_ip;
  const isPending = traceFlow.isPending || explainMatch.isPending;

  const handleSubmit = () => {
    if (!canSubmit) return;
    if (tab === "trace") {
      traceFlow.mutate({ flow });
    } else {
      explainMatch.mutate({ subject, flow });
    }
  };

  const applyPreset = (preset: Partial<TraceFlowInput>) => {
    setFlow((prev) => ({ ...prev, ...preset }));
  };

  return (
    <div className="animate-fade-in">
      <PageHeader
        title="Flow Trace"
        subtitle="Simulate packet flow through the ACL → Route → Mapping pipeline"
      />

      {/* ── Tab bar ──────────────────────────────────────────── */}
      <div className="flex gap-1 mb-4 bg-[color:var(--bg-secondary)] p-1 rounded-lg w-fit border border-[color:var(--border-subtle)]">
        {(
          [
            { id: "trace" as TabId, label: "Trace Flow", icon: Play },
            { id: "explain" as TabId, label: "Explain Match", icon: Search },
          ] as const
        ).map((t) => {
          const Icon = t.icon;
          return (
            <button
              key={t.id}
              onClick={() => setTab(t.id)}
              className={cn(
                "flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm transition-colors",
                tab === t.id
                  ? "bg-[color:var(--accent-cyan)]/15 text-[color:var(--accent-cyan)] border border-[color:var(--accent-cyan)]/25"
                  : "text-[color:var(--text-secondary)] hover:text-[color:var(--text-primary)] hover:bg-white/5"
              )}
            >
              <Icon size={14} />
              {t.label}
            </button>
          );
        })}
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        {/* ── Left: Input Panel ──────────────────────────────── */}
        <div className="space-y-4">
          <GlassCard>
            <p className="text-xs text-[color:var(--text-secondary)] uppercase mb-3">
              Flow Definition
            </p>
            <FlowInputForm flow={flow} onChange={setFlow} eniNames={eniNames} />

            {/* Subject selector (explain tab only) */}
            {tab === "explain" && (
              <div className="mt-3">
                <label className="block text-[10px] uppercase tracking-wider text-[color:var(--text-muted)] mb-1">
                  Explain Subject
                </label>
                <div className="flex gap-1">
                  {SUBJECT_OPTIONS.map((s) => {
                    const Icon = s.icon;
                    return (
                      <button
                        key={s.value}
                        onClick={() => setSubject(s.value)}
                        className={cn(
                          "flex items-center gap-1 px-2 py-1 rounded text-xs transition-colors",
                          subject === s.value
                            ? "bg-[color:var(--accent-cyan)]/15 text-[color:var(--accent-cyan)] border border-[color:var(--accent-cyan)]/25"
                            : "text-[color:var(--text-secondary)] border border-[color:var(--border-subtle)] hover:bg-white/5"
                        )}
                      >
                        <Icon size={12} />
                        {s.label}
                      </button>
                    );
                  })}
                </div>
              </div>
            )}

            <button
              onClick={handleSubmit}
              disabled={isPending || !canSubmit}
              className="w-full mt-4 px-3 py-2 text-sm rounded-md bg-[color:var(--accent-cyan)]/20 text-[color:var(--accent-cyan)] border border-[color:var(--accent-cyan)]/30 hover:bg-[color:var(--accent-cyan)]/30 transition-colors disabled:opacity-50 flex items-center justify-center gap-2"
            >
              {isPending ? (
                "Processing…"
              ) : tab === "trace" ? (
                <>
                  <Play size={14} /> Trace Flow
                </>
              ) : (
                <>
                  <Search size={14} /> Explain Match
                </>
              )}
            </button>
          </GlassCard>

          {/* Presets */}
          <GlassCard>
            <p className="text-xs text-[color:var(--text-secondary)] uppercase mb-2">
              Quick Presets
            </p>
            <div className="space-y-1">
              {PRESETS.map((p) => (
                <button
                  key={p.label}
                  onClick={() => applyPreset(p.flow)}
                  className="w-full text-left px-2 py-1.5 rounded text-sm text-[color:var(--text-secondary)] hover:bg-white/5 hover:text-[color:var(--text-primary)] transition-colors"
                >
                  <span className="text-[color:var(--accent-cyan)] font-medium">{p.label}</span>
                  <span className="text-[color:var(--text-muted)] text-xs ml-2">{p.desc}</span>
                </button>
              ))}
            </div>
          </GlassCard>
        </div>

        {/* ── Right: Result Panel ────────────────────────────── */}
        <div className="lg:col-span-2">
          <GlassCard>
            <p className="text-xs text-[color:var(--text-secondary)] uppercase mb-3">
              {tab === "trace" ? "Trace Result" : "Explain Match Result"}
            </p>

            {/* Trace Flow results */}
            {tab === "trace" && traceFlow.data && (
              <div className="space-y-4">
                <div className="flex items-center gap-3">
                  <VerdictBadge verdict={traceFlow.data.verdict} />
                  <span className="text-xs font-mono text-[color:var(--text-muted)]">
                    verdict={traceFlow.data.verdict}
                  </span>
                </div>

                <div>
                  <p className="text-[10px] uppercase tracking-wider text-[color:var(--text-muted)] mb-2">
                    Pipeline Trace ({traceFlow.data.trace.length} steps)
                  </p>
                  <div className="bg-[color:var(--bg-primary)] rounded-md border border-[color:var(--border-subtle)] p-3 max-h-[400px] overflow-auto">
                    <TraceSteps trace={traceFlow.data.trace} />
                  </div>
                </div>

                <MatchedObjects result={traceFlow.data} />
              </div>
            )}

            {/* Explain Match results */}
            {tab === "explain" && explainMatch.data && (
              <div className="space-y-4">
                <div className="bg-[color:var(--bg-primary)] rounded-md border border-[color:var(--border-subtle)] p-3 max-h-[500px] overflow-auto">
                  <ExplainResults result={explainMatch.data} />
                </div>
              </div>
            )}

            {/* Empty states */}
            {tab === "trace" && !traceFlow.data && !traceFlow.isPending && (
              <div className="flex flex-col items-center justify-center py-12 text-[color:var(--text-muted)]">
                <Play size={32} className="mb-2 opacity-30" />
                <p className="text-sm">
                  Configure a flow and click <strong>Trace Flow</strong>
                </p>
                <p className="text-xs mt-1">
                  Simulates the packet through the ACL → Route → VNet Mapping pipeline
                </p>
              </div>
            )}

            {tab === "explain" && !explainMatch.data && !explainMatch.isPending && (
              <div className="flex flex-col items-center justify-center py-12 text-[color:var(--text-muted)]">
                <Search size={32} className="mb-2 opacity-30" />
                <p className="text-sm">
                  Configure a flow and click <strong>Explain Match</strong>
                </p>
                <p className="text-xs mt-1">
                  Shows every candidate rule/route and why each matched or didn&apos;t
                </p>
              </div>
            )}

            {/* Error states */}
            {traceFlow.error && tab === "trace" && (
              <div className="p-3 rounded-md bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
                {traceFlow.error.message}
              </div>
            )}
            {explainMatch.error && tab === "explain" && (
              <div className="p-3 rounded-md bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
                {explainMatch.error.message}
              </div>
            )}
          </GlassCard>
        </div>
      </div>
    </div>
  );
}