/**
 * HomeView — DashCenter landing / hero page.
 *
 * This is the first thing a user sees at "/". It is intentionally
 * marketing-grade (animated hero, gradient orbs, staggered feature
 * cards) and is the only view that is NOT a dense operational console
 * — those live one click away under /dashboard, /fleet, etc.
 *
 * Sections (top → bottom):
 *   1) Animated hero: rotating LogoMark + product name + tagline
 *      + 2 CTAs (Open Dashboard, Explore Fleet).
 *   2) Live Fleet Pulse strip: 4 small live metrics polled at the
 *      fleet cadence (graceful "—" when offline). Proof-of-life
 *      that the BFF is connected without forcing a dense table.
 *   3) Three feature pillars: Observe / Diagnose / Operate.
 *   4) Quick-nav tile grid: 6 deep-link cards into the main app.
 *
 * Animation:
 *   - framer-motion staggered reveal on mount (no scroll trigger;
 *     the hero is short enough to fit in a single viewport).
 *   - Floating gradient orbs via pure-CSS keyframes so they keep
 *     drifting after the initial reveal.
 *   - Honors `prefers-reduced-motion` via the global CSS rule that
 *     clamps all animation-duration values to 1ms.
 */
import { Link } from 'react-router-dom';
import { motion } from 'framer-motion';
import {
  ArrowRight,
  Activity,
  Eye,
  Sliders,
  LayoutDashboard,
  Network,
  LayoutGrid,
  Workflow,
  Waypoints,
  HeartPulse,
  type LucideIcon,
} from 'lucide-react';
import { LogoMark } from '@/components/brand/LogoMark';
import { useFleetSummary } from '@/queries/hooks';
import {
  fleetDpuCount,
  fleetEniCount,
  fleetVnetCount,
  fleetHealthyDpus,
} from '@/lib/api-helpers';

/* ── Animation presets ──────────────────────────────────────── */
const containerStagger = {
  hidden: { opacity: 0 },
  visible: {
    opacity: 1,
    transition: { staggerChildren: 0.08, delayChildren: 0.05 },
  },
};
const fadeUp = {
  hidden: { opacity: 0, y: 18 },
  visible: { opacity: 1, y: 0, transition: { duration: 0.45, ease: 'easeOut' as const } },
};

/* ── Feature pillars ────────────────────────────────────────── */
interface Pillar {
  icon: LucideIcon;
  title: string;
  body: string;
  accent: 'cyan' | 'purple' | 'green';
}
const PILLARS: Pillar[] = [
  {
    icon: Eye,
    title: 'Observe',
    body: 'Topology, ENIs, Vnets, routes, tunnels, ACLs — all surfaced live across your fleet with cross-resource linking and capacity gauges.',
    accent: 'cyan',
  },
  {
    icon: Activity,
    title: 'Diagnose',
    body: 'Trace flows end-to-end, explain ACL matches, watch ENI counters, audit every dashd transition — answer "why did this packet drop?" in seconds.',
    accent: 'purple',
  },
  {
    icon: Sliders,
    title: 'Operate',
    body: 'Validate, simulate, and apply CRUD against the dashd control plane through guided forms or the raw YAML/JSON Admin Ops surface.',
    accent: 'green',
  },
];

/* ── Quick-nav tiles ────────────────────────────────────────── */
interface QuickTile {
  to: string;
  icon: LucideIcon;
  label: string;
  hint: string;
}
const QUICK_TILES: QuickTile[] = [
  { to: '/dashboard', icon: LayoutDashboard, label: 'Dashboard', hint: 'Fleet health at a glance' },
  { to: '/fleet', icon: Network, label: 'Fleet', hint: 'DPU inventory & state' },
  { to: '/resources', icon: LayoutGrid, label: 'Resources', hint: 'Guided CRUD forms' },
  { to: '/flow-trace', icon: Workflow, label: 'Flow Trace', hint: 'Diagnose a packet path' },
  { to: '/topology', icon: Waypoints, label: 'Topology', hint: 'Live cluster graph' },
  { to: '/health', icon: HeartPulse, label: 'Health', hint: 'Per-DPU diagnostics' },
];

/* ── Pulse strip metric ─────────────────────────────────────── */
function PulseMetric({
  label,
  value,
  loading,
}: {
  label: string;
  value: number | string;
  loading: boolean;
}) {
  return (
    <div className="flex flex-col items-center gap-0.5 px-4 py-2 min-w-[6.5rem]">
      <div className="text-2xl font-semibold tabular-nums tracking-tight bg-gradient-to-br from-[color:var(--accent-cyan)] to-[color:var(--accent-purple)] bg-clip-text text-transparent">
        {loading ? '—' : value}
      </div>
      <div className="text-[10px] uppercase tracking-[0.14em] text-[color:var(--text-muted)]">
        {label}
      </div>
    </div>
  );
}

/* ─────────────────────────────────────────────────────────── */

export default function HomeView() {
  const fleet = useFleetSummary();
  const summary = fleet.data;
  const loading = fleet.isLoading;

  return (
    <motion.div
      data-testid="home-view"
      initial="hidden"
      animate="visible"
      variants={containerStagger}
      className="relative isolate min-h-full overflow-hidden"
    >
      {/* ── Floating background orbs (decorative) ───────────── */}
      <div aria-hidden className="pointer-events-none absolute inset-0 -z-10">
        <div className="absolute -top-32 -left-24 w-[28rem] h-[28rem] rounded-full bg-[radial-gradient(circle,rgba(0,212,255,0.18)_0%,transparent_70%)] blur-3xl animate-pulse-slow" />
        <div
          className="absolute top-1/3 -right-32 w-[32rem] h-[32rem] rounded-full bg-[radial-gradient(circle,rgba(168,85,247,0.15)_0%,transparent_70%)] blur-3xl animate-pulse-slow"
          style={{ animationDelay: '1.2s' }}
        />
        <div
          className="absolute bottom-0 left-1/3 w-[24rem] h-[24rem] rounded-full bg-[radial-gradient(circle,rgba(16,185,129,0.10)_0%,transparent_70%)] blur-3xl animate-pulse-slow"
          style={{ animationDelay: '2.4s' }}
        />
      </div>

      {/* ── HERO ───────────────────────────────────────────── */}
      <section className="px-6 pt-16 pb-12 md:pt-24 md:pb-16 max-w-6xl mx-auto text-center">
        <motion.div variants={fadeUp} className="flex justify-center mb-6">
          <div className="relative">
            <div aria-hidden className="absolute inset-0 rounded-full blur-2xl bg-gradient-to-br from-cyan-400/40 to-purple-500/40" />
            <LogoMark size={104} animated className="relative" ariaLabel="DashCenter logo" />
          </div>
        </motion.div>

        <motion.h1
          variants={fadeUp}
          className="text-5xl md:text-6xl font-semibold tracking-tight leading-[1.05]"
        >
          <span className="bg-gradient-to-br from-[color:var(--text-primary)] via-[color:var(--accent-cyan)] to-[color:var(--accent-purple)] bg-clip-text text-transparent">
            DashCenter
          </span>
        </motion.h1>

        <motion.p
          variants={fadeUp}
          className="mt-4 text-lg md:text-xl text-[color:var(--text-secondary)] max-w-2xl mx-auto"
        >
          Network Operations Intelligence for <span className="text-[color:var(--text-primary)] font-medium">DPU Fleets</span>.
        </motion.p>

        <motion.p
          variants={fadeUp}
          className="mt-3 text-sm md:text-base text-[color:var(--text-muted)] max-w-3xl mx-auto"
        >
          One pane of glass to observe, diagnose, and operate your SONiC DASH data plane —
          from a single DPU to a fleet of hundreds.
        </motion.p>

        <motion.div
          variants={fadeUp}
          className="mt-8 flex flex-wrap items-center justify-center gap-3"
        >
          <Link
            to="/dashboard"
            className="group inline-flex items-center gap-2 px-5 py-2.5 rounded-lg text-sm font-medium text-[color:var(--text-inverse)] bg-gradient-to-br from-[color:var(--accent-cyan)] to-[color:var(--accent-purple)] shadow-[0_0_24px_rgba(0,212,255,0.35)] hover:shadow-[0_0_32px_rgba(0,212,255,0.55)] transition-shadow"
          >
            Open Dashboard
            <ArrowRight size={16} className="transition-transform group-hover:translate-x-0.5" />
          </Link>
          <Link
            to="/fleet"
            className="inline-flex items-center gap-2 px-5 py-2.5 rounded-lg text-sm font-medium text-[color:var(--text-primary)] glass-surface glass-surface-hover transition-colors"
          >
            Explore Fleet
            <ArrowRight size={16} />
          </Link>
        </motion.div>

        {/* ── Pulse strip ─────────────────────────────────── */}
        <motion.div
          variants={fadeUp}
          className="mt-10 inline-flex flex-wrap items-center justify-center divide-x divide-[color:var(--border-subtle)] glass-surface px-2"
          aria-label="Live fleet pulse"
        >
          <PulseMetric label="DPUs" value={fleetDpuCount(summary)} loading={loading} />
          <PulseMetric label="Healthy" value={fleetHealthyDpus(summary)} loading={loading} />
          <PulseMetric label="ENIs" value={fleetEniCount(summary)} loading={loading} />
          <PulseMetric label="Vnets" value={fleetVnetCount(summary)} loading={loading} />
        </motion.div>
      </section>

      {/* ── PILLARS ────────────────────────────────────────── */}
      <section className="px-6 pb-12 max-w-6xl mx-auto">
        <motion.div
          variants={containerStagger}
          className="grid grid-cols-1 md:grid-cols-3 gap-4"
        >
          {PILLARS.map((p) => {
            const Icon = p.icon;
            const glow =
              p.accent === 'cyan'
                ? 'glow-cyan'
                : p.accent === 'purple'
                  ? 'glow-purple'
                  : 'glow-green';
            const iconColor =
              p.accent === 'cyan'
                ? 'text-[color:var(--accent-cyan)]'
                : p.accent === 'purple'
                  ? 'text-[color:var(--accent-purple)]'
                  : 'text-[color:var(--accent-green)]';
            return (
              <motion.article
                key={p.title}
                variants={fadeUp}
                whileHover={{ y: -4 }}
                transition={{ type: 'spring', stiffness: 240, damping: 20 }}
                className={`glass-surface glass-surface-hover ${glow} p-6 flex flex-col gap-3`}
              >
                <div className={`inline-flex items-center justify-center w-10 h-10 rounded-lg bg-white/5 ${iconColor}`}>
                  <Icon size={20} aria-hidden />
                </div>
                <h2 className="text-lg font-semibold tracking-tight text-[color:var(--text-primary)]">
                  {p.title}
                </h2>
                <p className="text-sm text-[color:var(--text-secondary)] leading-relaxed">
                  {p.body}
                </p>
              </motion.article>
            );
          })}
        </motion.div>
      </section>

      {/* ── QUICK-NAV TILES ───────────────────────────────── */}
      <section className="px-6 pb-20 max-w-6xl mx-auto">
        <motion.h3
          variants={fadeUp}
          className="text-xs font-semibold tracking-[0.16em] uppercase text-[color:var(--text-muted)] mb-3"
        >
          Jump straight in
        </motion.h3>
        <motion.div
          variants={containerStagger}
          className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-3"
        >
          {QUICK_TILES.map((t) => {
            const Icon = t.icon;
            return (
              <motion.div key={t.to} variants={fadeUp}>
                <Link
                  to={t.to}
                  className="group flex flex-col items-start gap-2 p-4 rounded-xl glass-surface glass-surface-hover h-full transition-transform hover:-translate-y-0.5"
                >
                  <Icon
                    size={18}
                    aria-hidden
                    className="text-[color:var(--accent-cyan)] group-hover:text-[color:var(--accent-purple)] transition-colors"
                  />
                  <div className="text-sm font-medium text-[color:var(--text-primary)]">
                    {t.label}
                  </div>
                  <div className="text-[11px] text-[color:var(--text-muted)] leading-snug">
                    {t.hint}
                  </div>
                </Link>
              </motion.div>
            );
          })}
        </motion.div>
      </section>
    </motion.div>
  );
}