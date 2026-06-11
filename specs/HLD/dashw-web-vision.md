# DashCenter Web Console — Next-Generation Vision

> **Purpose.** This document goes beyond the existing HLD/LLD to define
> an **ultra-modern, innovative, next-level** web console that fully
> exploits every capability in the DashCenter API surface. It is
> informed by deep study of the actual proto contracts (7 files, ~2000
> lines), the Go implementation (dashd, dash-sim, dashctl), and the
> 29-kind southbound object model.
>
> The existing HLD (`dashw-web-hld.md`) defines a solid 13-view console.
> This document extends it with **15 additional innovative features**
> that transform dashw from a "management console" into a
> **network operations intelligence platform**.

---

## Table of Contents

1. [Design Philosophy: Beyond CRUD](#1-design-philosophy-beyond-crud)
2. [Visual Identity: Cinematic Network Dark](#2-visual-identity-cinematic-network-dark)
3. [Innovative Feature Catalog](#3-innovative-feature-catalog)
   - 3.1 [Command Nexus — AI-Assisted Operations](#31-command-nexus)
   - 3.2 [DPU Lifecycle State Machine Visualizer](#32-dpu-lifecycle-state-machine)
   - 3.3 [HA Orchestration Theater](#33-ha-orchestration-theater)
   - 3.4 [Migration Control Center](#34-migration-control-center)
   - 3.5 [Packet Anatomy Lab](#35-packet-anatomy-lab)
   - 3.6 [Capacity Planner & What-If Simulator](#36-capacity-planner)
   - 3.7 [Counter Correlation Matrix](#37-counter-correlation-matrix)
   - 3.8 [ACL Impact Analyzer](#38-acl-impact-analyzer)
   - 3.9 [Drift Remediation Workflow](#39-drift-remediation-workflow)
   - 3.10 [Physical-to-Logical Topology Map](#310-physical-to-logical-topology)
   - 3.11 [DPU Capability Compatibility Matrix](#311-capability-matrix)
   - 3.12 [Namespace Isolation Explorer](#312-namespace-isolation)
   - 3.13 [Event Causality Timeline](#313-event-causality-timeline)
   - 3.14 [Flow Deep Inspector](#314-flow-deep-inspector)
   - 3.15 [Policy Dependency Graph](#315-policy-dependency-graph)
4. [Enhanced View Architecture](#4-enhanced-view-architecture)
5. [Data Exploitation Map](#5-data-exploitation-map)
6. [Interaction Design Principles](#6-interaction-design-principles)
7. [Technology Stack Additions](#7-technology-stack-additions)

---

## 1. Design Philosophy: Beyond CRUD

The existing HLD designs a **console** — CRUD forms, data tables, basic
topology. What the DashCenter data model actually supports is a
**network operations intelligence platform**. The difference:

| Console (existing) | Intelligence Platform (this vision) |
|---|---|
| Show DPU state as a badge | Visualize the 9-state lifecycle FSM with live transitions and history |
| Table of flows | 7-tuple flow anatomy with fast/slow path visualization, sync state, and age heatmap |
| ACL rule list | Per-rule hit counter heatmap, dead-rule detection, candidate match explanation waterfall |
| Drift count badge | Interactive remediation workflow with field-by-field diff, suggested action, one-click fix |
| Static topology | Physics-based force-directed graph reflecting real appliance_id/slot physical placement |
| Capacity gauges | What-if simulator: "add 10 ENIs" → see per-DPU impact before committing |
| HA status table | HA Theater: animated role-flip timeline, flow-sync progress ring, split-brain alert |
| Migration phase text | 10-phase Gantt-style progress rail with per-phase timing, flow-sync waterfall |
| Counter numbers | Correlated multi-counter dashboard with anomaly detection highlighting |
| Namespace text field | Swimlane isolation explorer showing cross-namespace resource graph |

**Key insight:** dashd's API already exposes all the data needed for
these features. The current spec just doesn't visualize it.

---

## 2. Visual Identity: Cinematic Network Dark

The existing design tokens are good. This vision extends them:

### 2.1 Micro-interactions that communicate state

| Element | Micro-interaction |
|---|---|
| **DPU hexagon** | Breathes (scale 1.0→1.02→1.0, 3s) when HEALTHY. Jitters (translateX ±1px, 0.3s) when DEGRADED. Freezes + desaturates when UNREACHABLE. |
| **Tunnel edge** | Particle speed proportional to actual pps_out counter. Slows/stops when DPU is DOWN. |
| **Capacity gauge** | Arc fill animates with spring physics on data change. Exceeds 100% = arc glows red + shake. |
| **State transitions** | Morphs between shapes (e.g., REGISTERING circle → UP hexagon → DRAINING hourglass) |
| **Flow entry row** | Background gradient: recent (bright cyan) → aging (fading) → about to expire (amber). |
| **Policy change** | Ripple effect emanates from the changed resource outward through the dependency graph. |

### 2.2 Canvas backgrounds

| Canvas | Background treatment |
|---|---|
| Overlay plane | Subtle grid (`12px`, `#111827` lines on `#0A0E1A`). Slight parallax on scroll. |
| Underlay plane | Dot pattern (`20px` spacing, `#1F2937` dots). Denser grid suggests physical infrastructure. |
| HA Theater | Concentric rings (radar sweep animation) |
| Migration rail | Timeline gradient (left=past, right=future, current phase = bright band) |
| Flow inspector | Dark diagonal hatching (suggests packet flow direction) |

### 2.3 Color language extensions

| Semantic | Color | Usage |
|---|---|---|
| `--accent-blue` | `#3B82F6` | Informational, links, navigation |
| `--accent-teal` | `#14B8A6` | Flow sync, HA sync state |
| `--accent-pink` | `#EC4899` | Migration phases, canary traffic |
| `--accent-orange` | `#F97316` | Drain operations, cordoned state |
| `--gradient-tunnel` | `linear-gradient(135deg, #00D4FF, #A855F7)` | Overlay-to-underlay crossing |
| `--gradient-ha` | `linear-gradient(135deg, #00FF88, #14B8A6)` | HA healthy pair |

---

## 3. Innovative Feature Catalog

### 3.1 Command Nexus — AI-Assisted Operations

**What:** A single unified command interface that goes beyond the existing
Command View. Operators type natural-language intent, and the system
translates it to dashctl commands, validates via SimulateApply, shows the
blast radius, and executes — all in one flow.

**Why this is innovative:** No network console does this. Combines:
- `Cmd+K` palette (already designed)
- SimulateApply dry-run (proto: `SimulateApplyResult`)
- Capacity impact visualization (proto: `SimulatedDpuImpact`)

**Visual:**
```
┌────────────────────────────────────────────────────────────┐
│  ⌘ What do you want to do?                                 │
│  ┌──────────────────────────────────────────────────────┐  │
│  │ "move all enis from dpu-3 to dpu-5"                  │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                            │
│  INTERPRETED AS:                                           │
│  dashctl drain dpu-3 --target dpu-5 --parallelism 2        │
│                                                            │
│  DRY RUN RESULT:         (SimulateApply)                   │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ ✅ Would succeed                                    │   │
│  │ ENIs to move: 2 (eni-01, eni-02)                    │   │
│  │                                                     │   │
│  │ DPU-5 capacity impact:                              │   │
│  │   ENIs:    2/8 → 4/8  ██████░░ 50%                 │   │
│  │   Routes:  142 → 284  ████████ 28%                  │   │
│  │   Flows:   1204 → ~2400 (estimated)                 │   │
│  │                                                     │   │
│  │ ⚠️  Warning: dpu-5 lacks ipv6 capability           │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                            │
│  [Cancel]  [Modify]  [▶ Execute]                          │
└────────────────────────────────────────────────────────────┘
```

**Data source:** `POST /v1/simulate` → `SimulateApplyResult` with
`per_dpu_impact[]`, `validation_errors[]`, `would_issue_rpcs[]`.

---

### 3.2 DPU Lifecycle State Machine Visualizer

**What:** An animated FSM diagram showing all 9 DPU states and the
live transition of each DPU through them.

**Why:** DPU state is the most critical operational signal. The current
spec shows a badge. This shows the *journey*.

**Visual:**
```
┌──────────────────────────────────────────────────────────────────┐
│  DPU-1 Lifecycle                                                 │
│                                                                  │
│   ┌──────────┐    ┌────┐    ┌──────────┐    ┌─────────┐         │
│   │REGISTERING│───▶│ UP │───▶│ DEGRADED │───▶│CORDONED │         │
│   └──────────┘    └─┬──┘    └──────────┘    └────┬────┘         │
│                     │                            │               │
│                     │    ┌──────────┐    ┌───────▼───────┐       │
│                     │    │UNREACHABLE│───▶│   DRAINING    │       │
│                     │    └─────┬────┘    └───────┬───────┘       │
│                     │          │                  │               │
│                     │    ┌─────▼────┐    ┌───────▼──────────┐    │
│                     │    │  FAILED  │    │ DECOMMISSIONED   │    │
│                     │    └──────────┘    └──────────────────┘    │
│                                                                  │
│   Current: ● UP (since 14h 23m)                                  │
│   Transitions: REGISTERING→UP (14h23m ago) [auto: health OK]    │
│                                                                  │
│   Timeline: ──●────────────────────────────────────────── now    │
│              REG  UP                                             │
└──────────────────────────────────────────────────────────────────┘
```

**Implementation:** SVG state machine with nodes positioned in a
meaningful layout. The current state node glows + scales. Previous
transitions are shown as animated trails. Each node is clickable —
shows what triggers that transition.

**Data source:** `DpuStatusReport.state`, `DpuStatusReport.state_reason`,
`DpuRecord.state`, `PolicyEvent.TYPE_DPU_STATE_CHANGED`.

---

### 3.3 HA Orchestration Theater

**What:** A dedicated full-screen visualization for the HA subsystem:
the 12-state scope role machine, flow-sync progress, switchover/failover
animation, and split-brain detection.

**Why:** HA is the most complex operational domain in DASH. The proto
defines 12 role states, 5 flow-sync states, 9 event types, and
streaming switchover/failover progress. This is too rich for a table.

**Visual:**
```
┌────────────────────────────────────────────────────────────────────┐
│  HA THEATER                                                        │
│                                                                    │
│   HA Set: ha-set-prod (active-standby)     VIP: 10.0.0.100        │
│                                                                    │
│   ┌─────────────────┐         ┌─────────────────┐                 │
│   │   ⬡ DPU-1       │ ◀═══▶  │   ⬡ DPU-2       │                 │
│   │   ACTIVE ●      │  sync   │   STANDBY ○     │                 │
│   │                 │ ════╗   │                 │                 │
│   │   Flows: 12,430 │     ║   │   Flows: 12,428 │                 │
│   │   Synced: ✅    │     ║   │   Synced: ✅    │                 │
│   └─────────────────┘     ║   └─────────────────┘                 │
│                           ║                                        │
│   FLOW SYNC PROGRESS      ║                                        │
│   ┌───────────────────────╨────────────────────────────┐           │
│   │ ████████████████████████████████████████░░░░░ 94%  │           │
│   │ Synced: 12,428 / 12,430   Pending: 2   Failed: 0  │           │
│   │ Round-trip: 4ms   Colour: BLUE   Bandwidth: 2.1MB/s│           │
│   └────────────────────────────────────────────────────┘           │
│                                                                    │
│   ROLE TRANSITION TIMELINE                                         │
│   ──○──●────────────────────────────────────── now                │
│    INIT  ACTIVE   (stable 14h 23m)                                │
│                                                                    │
│   [▶ Trigger Switchover]   [⚡ Trigger Failover]                  │
│                                                                    │
│   LIVE EVENTS (WS /ws/ha-events)                                   │
│   │ 14:32:01 │ ROLE_CHANGED       │ DPU-1 STANDBY→ACTIVE │       │
│   │ 14:32:00 │ SWITCHOVER_STARTED │ ha-set-prod           │       │
│   │ 14:31:55 │ FLOW_SYNC_SYNCED   │ Round: BLUE           │       │
└────────────────────────────────────────────────────────────────────┘
```

**Key innovations:**
- **Animated switchover:** When operator clicks "Trigger Switchover", the
  `TriggerSwitchover` stream emits `HaScopeStatus` updates. The UI
  animates the role labels morphing: ACTIVE → SWITCHING_TO_STANDBY →
  STANDBY on DPU-1, STANDBY → SWITCHING_TO_ACTIVE → ACTIVE on DPU-2.
  The connecting line pulses during transition.
- **Flow sync ring:** Circular progress indicator showing
  `FlowSyncStats.flows_synced / (flows_synced + flows_pending)`.
  Color-coded by state (SYNCED=green, SYNCING=teal, FAILED=red).
- **Split-brain alert:** If `TYPE_SPLIT_BRAIN_DETECTED` event fires,
  the connecting line turns red and breaks apart with a zigzag animation.

**Data sources:**
- `HaService.GetHaSetState` → member roles
- `HaService.WatchHaEvents` stream → live events
- `HaService.GetFlowSyncStats` → sync progress
- `HaService.TriggerSwitchover` stream → role transition updates

---

### 3.4 Migration Control Center

**What:** A 10-phase Gantt-style progress rail for ENI live migrations
with real-time streaming updates, flow-sync waterfall, and rollback
controls.

**Why:** The migration state machine has 10 real phases + 2 terminal
states, 4 strategies, per-phase timestamps, and streaming progress.
This is a first-class operator workflow that deserves a dedicated view.

**Visual:**
```
┌────────────────────────────────────────────────────────────────────┐
│  MIGRATION: session-abc123                                         │
│  ENI: eni-01 | DPU-3 → DPU-5 | Strategy: NEW_FLOWS_FIRST_DRAIN   │
│                                                                    │
│  PHASE RAIL:                                                       │
│  ┌───┬───┬───┬───┬───┬───┬───┬───┬───┬───┐                       │
│  │ADM│SNP│PRE│SYN│RDY│CUT│DRN│CMT│FIN│CMP│                       │
│  │ ✅│ ✅│ ✅│ ▶ │   │   │   │   │   │   │                       │
│  └───┴───┴───┴───┴───┴───┴───┴───┴───┴───┘                       │
│  ▲               ▲                                                 │
│  00:00           04:23 (current: SYNC phase, 42% flow sync)        │
│                                                                    │
│  PHASE TIMING:                                                     │
│  ═══════════▓▓▓▓▓▓▓▓▓░░░░░░░░░░░░░░░░░░░░░░░░░░░░░              │
│  ADMISSION  SNAPSHOT  PREPARE  SYNC→→→→→→→→                       │
│  (2s)       (8s)      (1m 12s) (running 4m 23s)                   │
│                                                                    │
│  FLOW SYNC WATERFALL:                                              │
│  Flows to sync: 1,204                                              │
│  ████████████████████████████████░░░░░░░░░░░ 72%                  │
│  Rate: 14 flows/s    ETA: ~2m 30s                                  │
│                                                                    │
│  [⏸ Pause]  [⏪ Rollback]  [⏩ Advance to READY]  [❌ Abort]      │
│                                                                    │
│  PER-DPU DISPATCH:                                                 │
│  │ DPU-3 (source) │ ✅ policy snapshot captured    │               │
│  │ DPU-5 (target) │ ✅ policy pre-staged           │               │
└────────────────────────────────────────────────────────────────────┘
```

**Key innovations:**
- **Phase rail with timing** — each phase is a colored bar whose width
  is proportional to duration. Completed phases are green, current
  phase is animated cyan, future phases are gray.
- **Flow-sync waterfall** — `MigrationSession.detail_json` contains
  flow-sync progress percentage. Animated bar with ETA calculation.
- **Strategy-aware UX** — different strategies show different phase
  emphasis (CANARY_SPLIT shows a traffic-split slider at CUTOVER).
- **Live streaming** — `MigrationService.StreamMigrationSession` feeds
  real-time updates to the phase rail.

**Data sources:**
- `MigrationService.StreamMigrationSession` → live phase updates
- `MigrationSession.phase_started_at` → per-phase timing
- `MigrationSession.detail_json` → sync progress
- `MigrationService.AdvanceMigrationPhase` → advance controls
- `MigrationService.RollbackMigration` / `AbortMigration` → controls

---

### 3.5 Packet Anatomy Lab

**What:** An interactive packet dissection tool that goes beyond flow
trace. Operator constructs a synthetic packet visually, traces it
through the full DASH pipeline, and sees **why each candidate was or
was not selected** — not just the winner.

**Why:** The proto defines `ExplainMatch` which returns a ranked list
of every ACL rule, route, and Vnet mapping that was evaluated, with
per-candidate match/reject reasons. This is incredibly powerful for
debugging but the existing spec only shows "matched rule X".

**Visual:**
```
┌────────────────────────────────────────────────────────────────────┐
│  PACKET ANATOMY LAB                                                │
│                                                                    │
│  ┌── CONSTRUCT ──────────────────────────────────────────┐         │
│  │  Direction: [OUTBOUND ▼]    ENI: [eni-01 ▼]          │         │
│  │  Src: [10.0.0.5]:[8080]     Dst: [10.0.1.10]:[443]   │         │
│  │  Proto: [TCP ▼]  VNI: [10001]                         │         │
│  │                                    [🔍 Trace]         │         │
│  └───────────────────────────────────────────────────────┘         │
│                                                                    │
│  ┌── PIPELINE ───────────────────────────────────────────┐         │
│  │  ENI → ACL IN → ACL OUT → Route → VnetMap → Encap    │         │
│  │  [●]    [●]      [○]      [●]     [●]       [●]      │         │
│  │         ▲ selected                                    │         │
│  └───────────────────────────────────────────────────────┘         │
│                                                                    │
│  ┌── ACL CANDIDATE WATERFALL (ExplainMatch) ─────────────┐         │
│  │  Priority │ Policy     │ Action │ Match? │ Reason      │         │
│  │  ─────────┼────────────┼────────┼────────┼─────────── │         │
│  │  10       │ baseline   │ DENY   │ ❌     │ src_prefix  │         │
│  │           │            │        │        │ disjoint    │         │
│  │  50       │ web-allow  │ ALLOW  │ ✅     │ src 10.0.0/│         │
│  │           │            │        │ WINNER │ 24 ∧ dst   │         │
│  │           │            │        │        │ port 443   │         │
│  │  100      │ default    │ DENY   │ ⏭     │ skipped:   │         │
│  │           │            │        │        │ higher prio│         │
│  │           │            │        │        │ matched    │         │
│  └───────────────────────────────────────────────────────┘         │
│                                                                    │
│  VERDICT: ✅ ENCAP → DPU-5 (underlay 10.0.0.5, VNI 10001)         │
│  Fast path: YES (flow already in table)                            │
└────────────────────────────────────────────────────────────────────┘
```

**Data sources:**
- `DiagnosticsService.TraceFlow` → full pipeline trace
- `DiagnosticsService.ExplainMatch` → candidate waterfall
- `FlowTraceResult.verdict`, `.matched_acl_rule`, `.matched_route`,
  `.matched_vnet_mapping`, `.fast_path_hit`, `.trace[]`
- `MatchExplanation.candidates[]` → per-candidate match/reject reason

---

### 3.6 Capacity Planner & What-If Simulator

**What:** An interactive capacity planning tool where operators can
model changes before committing. "What if I add 10 ENIs to vnet-prod?"
→ see which DPUs would be selected, what the capacity impact would be,
and whether any would exceed limits — all visualized on the topology.

**Visual:**
```
┌────────────────────────────────────────────────────────────────────┐
│  CAPACITY PLANNER                                                  │
│                                                                    │
│  ┌── SCENARIO ───────────────────────────────────────────┐         │
│  │  Add: [10] ENIs to Vnet: [vnet-prod ▼]                │         │
│  │  Placement: [auto ▼]  (or specify DPU)                │         │
│  │                                    [📊 Simulate]      │         │
│  └───────────────────────────────────────────────────────┘         │
│                                                                    │
│  CURRENT → PROJECTED CAPACITY:                                     │
│  ┌─────────┬──────────┬──────────┬────────┬────────────┐          │
│  │ DPU     │ ENIs     │ Routes   │ Flows  │ Status     │          │
│  │─────────┼──────────┼──────────┼────────┼────────────│          │
│  │ DPU-1   │ 2→4(+2)  │ 142→284  │ ~2400  │ ✅ OK      │          │
│  │ DPU-2   │ 2→4(+2)  │ 142→284  │ ~2400  │ ✅ OK      │          │
│  │ DPU-3   │ 2→4(+2)  │ 142→284  │ ~2400  │ ✅ OK      │          │
│  │ DPU-4   │ 2→4(+2)  │ 142→284  │ ~2400  │ ⚠️ 85%    │          │
│  │ DPU-5   │ 2→4(+2)  │ 142→284  │ ~2400  │ ❌ EXCEED  │          │
│  └─────────┴──────────┴──────────┴────────┴────────────┘          │
│                                                                    │
│  TOPOLOGY (projected state overlaid):                              │
│  ⬡ DPU-1[OK]  ⬡ DPU-2[OK]  ⬡ DPU-3[OK]                         │
│       ⬡ DPU-4[⚠️]    ⬡ DPU-5[❌ would exceed max_enis]          │
│                                                                    │
│  RECOMMENDATION: Redistribute 2 ENIs from DPU-5 to DPU-1,DPU-2   │
│                                                                    │
│  [Cancel]  [Adjust]  [Apply (creates 10 ENIs)]                    │
└────────────────────────────────────────────────────────────────────┘
```

**Data sources:**
- `ControlPlane.SimulateApply` → `SimulateApplyResult` with
  `per_dpu_impact[]` and `would_succeed`
- `DpuCapacityLimits` (from `DpuRecord`)
- `DpuCapacityUsage` (from `DpuStatusReport`)
- `DpuCapabilities` → flag check (e.g., does target support IPv6?)

---

### 3.7 Counter Correlation Matrix

**What:** A multi-dimensional counter dashboard that shows TCP, drop,
flow, HA, encap, and service tunnel counters across all DPUs
simultaneously, with anomaly highlighting.

**Why:** `CounterReport` has 30+ named counters across 6 categories.
Showing them individually is meaningless. Correlated visualization
reveals patterns: "tcp_retransmits spiking on DPU-3 correlates with
drop_acl_in spike on the same DPU".

**Visual:**
```
┌────────────────────────────────────────────────────────────────────┐
│  COUNTER CORRELATION MATRIX                                        │
│                                                                    │
│  ┌── TCP ──────────────────────────────────────────────┐           │
│  │  syn_rx     ▁▂▃▅▇▅▃▂▁  │ 2,841/s │ ● DPU-1       │           │
│  │  established ▁▁▂▃▃▃▂▁▁ │ 1,204   │ ● DPU-2       │           │
│  │  retransmits ▁▁▁▁▅▇▅▁▁ │ 48/s    │ ⚠️ DPU-3 ↑   │           │
│  │  rst_rx      ▁▁▁▁▂▃▂▁▁ │ 12/s    │               │           │
│  └─────────────────────────────────────────────────────┘           │
│                                                                    │
│  ┌── DROPS ────────────────────────────────────────────┐           │
│  │  acl_in      ▁▁▁▁▃▇▅▂▁ │ 23/s    │ ⚠️ DPU-3 ↑↑  │           │
│  │  route_miss  ▁▁▁▁▁▁▁▁▁ │ 0       │               │           │
│  │  mapping_miss▁▁▁▁▁▁▁▁▁ │ 0       │               │           │
│  │  flow_full   ▁▁▁▁▁▁▁▁▁ │ 0       │               │           │
│  │  pa_valid    ▁▁▁▁▁▁▁▁▁ │ 0       │               │           │
│  └─────────────────────────────────────────────────────┘           │
│                                                                    │
│  ┌── CORRELATION DETECTED ─────────────────────────────┐           │
│  │  ⚠️ DPU-3: tcp_retransmits ↑ correlates with       │           │
│  │     drop_acl_in ↑ (r=0.94). Possible cause:        │           │
│  │     ACL policy change at 14:28:01 (txn: abc-123)    │           │
│  │     [View Policy Change] [View ACL Rules]           │           │
│  └─────────────────────────────────────────────────────┘           │
│                                                                    │
│  ┌── FLOW ─────────────────────────────────────────────┐           │
│  │  created/s   ▁▂▃▅▇▅▃▂▁ │ fast_path: 94.2%         │           │
│  │  table_size  ████████░░ │ 12,430 / 65,535           │           │
│  └─────────────────────────────────────────────────────┘           │
│                                                                    │
│  ┌── HA SYNC ──────────────────────────────────────────┐           │
│  │  sync_msg_tx ▁▂▂▂▂▂▂▂▁ │ 820/s   │                │           │
│  │  sync_msg_rx ▁▂▂▂▂▂▂▂▁ │ 818/s   │ ✅ balanced    │           │
│  │  split_brain ▁▁▁▁▁▁▁▁▁ │ 0       │                │           │
│  └─────────────────────────────────────────────────────┘           │
└────────────────────────────────────────────────────────────────────┘
```

**Data sources:**
- `ObservabilityService.GetCounters(follow=true)` → streaming counters
- `CounterReport` fields (30+ counters across 6 categories)
- `PolicyEvent` (correlate counter spikes with config changes)

---

### 3.8 ACL Impact Analyzer

**What:** A dedicated ACL analysis view that combines rule listing,
per-rule hit counters (including dead-rule detection), policy coverage
visualization, and one-click "what would this rule change affect?"
simulation.

**Visual:**
```
┌────────────────────────────────────────────────────────────────────┐
│  ACL IMPACT ANALYZER — policy: web-frontend-acl                    │
│  Stage: INBOUND | ENIs: eni-01, eni-02                             │
│                                                                    │
│  RULE HEATMAP (hit count → color intensity):                       │
│  ┌──────┬────────┬─────────┬──────────┬───────┬──────────────────┐│
│  │ Prio │ Action │ Hits    │ Bytes    │ Last  │ Status           ││
│  │──────┼────────┼─────────┼──────────┼───────┼──────────────────││
│  │ 10   │ ALLOW  │ 142,301 │ 48.2 GB  │ 2s    │ ✅ Active       ││
│  │ 20   │ ALLOW  │ 89,432  │ 12.1 GB  │ 5s    │ ✅ Active       ││
│  │ 30   │ DENY   │ 2,103   │ 840 KB   │ 1m    │ ✅ Active       ││
│  │ 40   │ ALLOW  │ 0       │ 0        │ never │ 💀 DEAD RULE    ││
│  │ 50   │ DENY   │ 0       │ 0        │ never │ 💀 DEAD RULE    ││
│  │ 999  │ DENY   │ 48      │ 19 KB    │ 30m   │ ⚠️ Low traffic  ││
│  └──────┴────────┴─────────┴──────────┴───────┴──────────────────┘│
│                                                                    │
│  DEAD RULES: 2 rules have never been hit. Safe to remove?          │
│  [Remove Dead Rules]  [Simulate Removal]                           │
│                                                                    │
│  COVERAGE MAP (which prefixes are covered?):                       │
│  10.0.0.0/8  ████████████████████████████  (rules 10,20,30)       │
│  10.1.0.0/16 ████████░░░░░░░░░░░░░░░░░░░  (rule 20 only)         │
│  0.0.0.0/0   ░░░░░░░░░░░░░░░░░░░░░░░░███  (rule 999 = default)   │
└────────────────────────────────────────────────────────────────────┘
```

**Data sources:**
- `DiagnosticsService.GetAclHitStats` → per-rule hit counters
- `AclRuleHit.hits`, `.bytes`, `.last_hit_at`
- `AclStatsRequest.zero_hits_only` → dead-rule detection
- `DiagnosticsService.ExplainMatch` → candidate analysis

---

### 3.9 Drift Remediation Workflow

**What:** An interactive drift remediation flow that goes beyond showing
drift counts. For each drift item, shows the field-by-field diff, the
suggested remediation (RECONCILE vs IMPORT_OBSERVED vs MANUAL), the
rationale, and one-click fix buttons.

**Visual:**
```
┌────────────────────────────────────────────────────────────────────┐
│  DRIFT REMEDIATION — DPU-3                                         │
│  Total drift items: 3                                              │
│                                                                    │
│  ┌── ITEM 1: ENI/default/eni-01 ─ FIELD_MISMATCH ────────────┐   │
│  │                                                             │   │
│  │  FIELD DIFF:                                                │   │
│  │  ┌─────────────┬──────────────┬──────────────┐             │   │
│  │  │ Field       │ Declared     │ Observed     │             │   │
│  │  │─────────────┼──────────────┼──────────────│             │   │
│  │  │ admin_state │ "up"         │ "down"       │             │   │
│  │  └─────────────┴──────────────┴──────────────┘             │   │
│  │                                                             │   │
│  │  SUGGESTED: ● RECONCILE (push declared → DPU)              │   │
│  │  RATIONALE: "admin_state is operator-controlled. Declared    │   │
│  │  'up' is the intent. DPU disagrees — likely transient."    │   │
│  │                                                             │   │
│  │  [⟲ Reconcile This] [📥 Import Observed] [⏭ Skip]        │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                    │
│  ┌── ITEM 2: Route/default/route-x ─ DECLARED_NOT_OBSERVED ──┐   │
│  │  dashd has it, DPU-3 doesn't. Likely failed dispatch.       │   │
│  │  SUGGESTED: ● RECONCILE                                     │   │
│  │  [⟲ Reconcile This]                                        │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                    │
│  [⟲ Reconcile All (3 items)]   [📊 View History]                 │
└────────────────────────────────────────────────────────────────────┘
```

**Data sources:**
- `DiagnosticsService.ExplainDrift` → `DriftExplanation` with
  `field_diffs[]`, `suggested` remediation, `rationale`
- `ObservabilityService.GetDrift` → `DriftReport.items[]`
- `ControlPlane.Reconcile` → execute fix

---

### 3.10 Physical-to-Logical Topology Map

**What:** A dual-layer topology that maps physical infrastructure
(appliance_id, slot) to logical constructs (Vnets, ENIs, tunnels).
The existing topology is logical-only. This adds the physical dimension.

**Why:** `DpuIdentity` carries `appliance_id` and `slot` — these
physically locate DPUs in racks. No existing network console overlays
physical and logical in the same view.

**Visual:**
```
┌────────────────────────────────────────────────────────────────────┐
│  PHYSICAL → LOGICAL TOPOLOGY                                       │
│                                                                    │
│  PHYSICAL LAYER (left):        LOGICAL LAYER (right):              │
│  ┌─ Appliance-1 ────────┐     ┌─ vnet-prod ──────────────┐       │
│  │  [Slot 0: DPU-1]─────│─────│──ENI-01, ENI-02           │       │
│  │  [Slot 1: DPU-2]─────│─────│──ENI-03, ENI-04           │       │
│  └───────────────────────┘     │                           │       │
│  ┌─ Appliance-2 ────────┐     │  VNI: 10001               │       │
│  │  [Slot 0: DPU-3]─────│─────│──ENI-05, ENI-06           │       │
│  │  [Slot 1: DPU-4]─────│─────│──ENI-07, ENI-08           │       │
│  └───────────────────────┘     └───────────────────────────┘       │
│  ┌─ Appliance-3 ────────┐     ┌─ vnet-staging ───────────┐       │
│  │  [Slot 0: DPU-5]─────│─────│──ENI-09, ENI-10           │       │
│  └───────────────────────┘     └───────────────────────────┘       │
│                                                                    │
│  Connection lines show ENI-to-Vnet associations crossing the       │
│  physical-logical boundary. Color = health state.                  │
└────────────────────────────────────────────────────────────────────┘
```

**Data sources:**
- `DpuIdentity.appliance_id`, `.slot` → physical placement
- `EniSpec.vnet_name`, `.placement_hint_dpu_ids` → logical mapping
- `DpuStatusReport.state` → health overlay

---

### 3.11 DPU Capability Compatibility Matrix

**What:** A matrix view showing which DPU capabilities each DPU
advertises, cross-referenced with which features each Vnet/ENI requires.

**Why:** `DpuCapabilities` has 12 boolean flags. Placement decisions
depend on capability matching. Operators need to see "can DPU-5 host
an ENI that needs IPv6 + service_tunnel + HA?"

**Visual:**
```
┌────────────────────────────────────────────────────────────────────┐
│  CAPABILITY MATRIX                                                 │
│                                                                    │
│          │ ipv6 │ svc_tunnel │ ecmp │ fast_path │ ha_a/a │ ha_a/s │
│  ────────┼──────┼────────────┼──────┼───────────┼────────┼────────│
│  DPU-1   │  ✅  │     ✅     │  ✅  │    ✅     │   ✅   │   ✅   │
│  DPU-2   │  ✅  │     ✅     │  ✅  │    ✅     │   ✅   │   ✅   │
│  DPU-3   │  ❌  │     ✅     │  ✅  │    ✅     │   ❌   │   ✅   │
│  DPU-4   │  ✅  │     ❌     │  ✅  │    ❌     │   ✅   │   ✅   │
│  DPU-5   │  ❌  │     ❌     │  ❌  │    ✅     │   ❌   │   ❌   │
│                                                                    │
│  ⚠️ DPU-5 is the least capable. 3 ENIs require capabilities it    │
│     doesn't have. Consider decommissioning or upgrading.           │
│                                                                    │
│  Schema version: DPU-1..4 = v2.1.0, DPU-5 = v1.8.0 (outdated)    │
└────────────────────────────────────────────────────────────────────┘
```

**Data sources:**
- `DpuCapabilities` (12 flags) from `DpuRecord`
- `DpuCapabilities.dash_api_schema_version` → version comparison

---

### 3.12 Namespace Isolation Explorer

**What:** A swimlane visualization showing resources grouped by namespace,
with cross-namespace relationships highlighted.

**Data sources:**
- Every spec message carries `namespace` field
- Cross-namespace references are validated by dashd

---

### 3.13 Event Causality Timeline

**What:** A connected timeline showing the causal chain:
operator action → policy event → DPU dispatch → audit entry → counter
change. Events are linked by `txn_id`.

**Visual:**
```
txn_id: abc-123
  │
  ├── 14:28:01.000  PUT /v1/default/acl-policies/web-acl (REST)
  ├── 14:28:01.012  PolicyEvent: TYPE_PUT, acl_policy/web-acl
  ├── 14:28:01.050  Dispatch → DPU-1 ✅, DPU-2 ✅, DPU-3 ✅
  ├── 14:28:01.080  AuditEntry: ControlPlane/PutAclPolicy, outcome=ok
  └── 14:28:02.000  CounterReport: DPU-3 drop_acl_in ↑ 23/s
                     (new rule took effect)
```

**Data sources:**
- `AuditEntry.txn_id` → links audit to event
- `PolicyEvent.txn_id` → links event to mutation
- `Ack.dispatch_results` → per-DPU dispatch result
- `CounterReport` → correlate counter changes with txn timing

---

### 3.14 Flow Deep Inspector

**What:** A rich flow table with per-flow deep inspection: 7-tuple,
sync state, fast/slow path indicator, packet/byte counters, age
heatmap, and ENI association.

**Why:** `FlowEntry` has 14 fields including `sync_state`, `fast_path`
boolean, `created_at`, `last_seen_at`. The existing spec shows a flat
table. This adds:
- **Age heatmap**: recent flows = bright cyan, aging = amber, stale = red
- **Fast/slow path badge**: green chip for fast_path=true, amber for slow
- **Sync state**: per-flow HA sync indicator
- **Flow sparklines**: per-flow packet/byte rate mini-charts

**Data sources:**
- `ObservabilityService.GetFlowList` stream
- `FlowEntry` (14 fields)

---

### 3.15 Policy Dependency Graph

**What:** A directed graph showing how policies depend on each other:
ENI → Vnet (via vnet_name), AclPolicy → ENI (via eni_names[]),
RoutePolicy → ENI (via eni_names[]), ServiceTunnel → Vnet (via VNI
match), VnetMapping → Vnet (via vnet_name). Shows "what breaks if I
delete this Vnet?"

**Data sources:**
- All spec messages' cross-references (vnet_name, eni_names[], etc.)
- `ControlPlane.SimulateApply` with ACTION_DELETE → see impact

---

## 4. Enhanced View Architecture

Updated view catalog (extending the 13 in the existing HLD):

| # | View | Route | Type | Key innovation |
|---|---|---|---|---|
| 1 | Dashboard | `/` | Existing | + correlation alerts from §3.7 |
| 2 | Fleet | `/fleet` | Existing | + physical topology toggle (§3.10) |
| 3 | DPU | `/dpu/:id` | Existing | + lifecycle FSM widget (§3.2) |
| 4 | Vnet | `/vnet/:name` | **Enhanced** | Dual-plane canvas (already done) |
| 5 | Routing | `/routing` | Existing | + prefix tree with coverage gaps |
| 6 | Tunnel | `/tunnels` | Existing | + particle speed ∝ pps |
| 7 | Policy | `/policies` | Existing | + ACL impact analyzer (§3.8) |
| 8 | Flow Trace | `/flow-trace` | **Enhanced** | Packet Anatomy Lab (§3.5) |
| 9 | Audit Log | `/audit` | Existing | + event causality timeline (§3.13) |
| 10 | Health | `/health` | Existing | + leader election timeline |
| 11 | Admin Ops | `/admin` | Existing | + Command Nexus (§3.1) |
| 12 | Command | `/commands` | Existing | Merged into Command Nexus |
| 13 | Debug | `/debug` | Existing | + raw counter matrix |
| **14** | **HA Theater** | `/ha` | **NEW** | §3.3 |
| **15** | **Migration Center** | `/migrations` | **NEW** | §3.4 |
| **16** | **Capacity Planner** | `/capacity` | **NEW** | §3.6 |
| **17** | **Counter Matrix** | `/counters` | **NEW** | §3.7 |
| **18** | **Drift Fixer** | `/drift` | **NEW** | §3.9 |
| **19** | **Capability Matrix** | `/capabilities` | **NEW** | §3.11 |
| **20** | **Dependency Graph** | `/dependencies` | **NEW** | §3.15 |

Total: **20 views** (13 existing + 7 new).

---

## 5. Data Exploitation Map

Every proto message → which views consume it:

| Proto Message | Fields | Consumed by |
|---|---|---|
| `DpuIdentity` | id, appliance_id, slot, software_version, labels | Fleet, DPU, Physical Topology, Capability Matrix |
| `DpuState` (9 states) | lifecycle state | DPU (FSM widget), Fleet, Dashboard |
| `DpuCapacityLimits` (18 limits) | max_* | Capacity Planner, DPU, Fleet |
| `DpuCapacityUsage` (14 usage fields) | *_used, pps_in/out, bps_in/out | Capacity Planner, DPU, Counter Matrix |
| `DpuCapabilities` (12 flags) | ipv6, service_tunnel, ecmp, ... | Capability Matrix, Capacity Planner |
| `CounterReport` (30+ counters) | TCP, drops(10), flow, HA, encap, svc_tunnel | Counter Correlation Matrix |
| `FlowEntry` (14 fields) | 7-tuple, sync_state, fast_path, bytes, packets | Flow Deep Inspector |
| `FlowTraceResult` | verdict, trace[], matched_* | Packet Anatomy Lab |
| `MatchExplanation` | candidates[], selected_candidate_id | Packet Anatomy Lab |
| `DriftItem` / `DriftExplanation` | kind, field_diffs[], remediation, rationale | Drift Fixer |
| `AclRuleHit` | priority, hits, bytes, last_hit_at | ACL Impact Analyzer |
| `HaSetStatus` / `HaScopeStatus` | members, roles, flow_sync | HA Theater |
| `HaEvent` (9 types) | role changes, split-brain, switchover | HA Theater |
| `FlowSyncStats` | synced, pending, failed, round_trip_ms | HA Theater |
| `MigrationSession` | phase, phase_started_at, detail_json | Migration Center |
| `MigrationPlan` | warnings, target_capacity_impact | Migration Center, Capacity Planner |
| `DrainProgress` | phase, enis_total/migrated/failed, links | DPU (drain widget) |
| `SimulateApplyResult` | would_succeed, per_dpu_impact | Command Nexus, Capacity Planner |
| `PolicyEvent` (8 types) | type, txn_id, target, detail_json | Dashboard, Event Timeline |
| `AuditEntry` | txn_id, principal, rpc, target, outcome | Audit, Event Timeline |
| `PolicyObject` | oneof 7 kinds + generation + last_modified_at | Admin Ops, Dependency Graph |

---

## 6. Interaction Design Principles

| Principle | Implementation |
|---|---|
| **Progressive disclosure** | Overview → click → detail → action. Never show all data at once. |
| **Contextual navigation** | Click a DPU in any view → navigate to DPU View with context preserved. Click an ENI → navigate to its Vnet View. Click a txn_id → navigate to audit. |
| **Action proximity** | Every diagnostic view has an action button next to the finding. Drift → "Reconcile". Dead rule → "Remove". Capacity exceed → "Redistribute". |
| **Temporal awareness** | Every panel shows "last updated Xs ago". Stale data fades. Real-time data glows. |
| **Correlation first** | Counter spikes link to policy changes. Drift items link to dispatch results. Migrations link to HA events. |
| **Zero-click insights** | Dashboard auto-generates alerts from data: "DPU-3 tcp_retransmits correlating with drop_acl_in since 14:28". No user action needed. |

---

## 7. Technology Stack Additions

| Addition | Why | Used by |
|---|---|---|
| **`@visx/visx`** | Low-level D3-powered React viz for custom charts (heatmaps, Gantt, waterfall) | Counter Matrix, Migration Center, ACL Analyzer |
| **`dagre`** | Directed graph layout for dependency graph | Policy Dependency Graph |
| **`elkjs`** | Layered graph layout for physical-logical topology | Physical Topology |
| **`@tanstack/react-virtual`** | Virtual scrolling for large flow tables (100K+ flows) | Flow Inspector |
| **`date-fns`** | Time formatting, relative time | Event Timeline, phase timing |
| **`simple-statistics`** | Correlation coefficient calculation for counter anomaly detection | Counter Correlation Matrix |

All are tree-shakeable and add < 50KB gzipped combined.

---

## 8. Existing View Enhancements — Making Every View Eye-Catching

The 13 Phase A views are functionally correct but visually standard.
These enhancements transform each from a "management console" into a
**visual experience** — using animation, spatial encoding, progressive
disclosure, and real-time data storytelling.

---

### 8.1 Dashboard — Fleet Heartbeat

**Current:** Static cards, gauges, event list.
**Enhanced:** A living, breathing fleet visualization.

#### 8.1.1 Fleet Heartbeat Ring

Replace the health donut with a **heartbeat ring** — a circular
visualization where each DPU is a segment of the ring:

```
         ┌───────────────────┐
         │    FLEET PULSE    │
         │                   │
         │    ╭──╮ ╭──╮     │
         │   ╱    ╲╱    ╲    │  ← Each arc segment = 1 DPU
         │  │  5 DPUs   │   │     Width ∝ ENI count
         │  │  12 ENIs   │   │     Color = health state
         │  │  99.2% ↑   │   │     Pulse animation = heartbeat
         │   ╲    ╱╲    ╱    │
         │    ╰──╯ ╰──╯     │
         │                   │
         └───────────────────┘
```

- Each ring segment represents one DPU
- Segment **width** ∝ ENI count (bigger DPU = bigger segment)
- Segment **color** = health state (green/amber/red)
- Entire ring **pulses** like a heartbeat (slow: healthy fleet, fast: issues)
- Hover segment → tooltip with DPU summary
- Click segment → navigate to `/dpu/{id}`
- Center text: total DPUs, ENIs, overall uptime percentage with trend arrow

#### 8.1.2 Smart Summary Cards with Trend Arrows

Each stats card shows not just the current number but a **trend sparkline
+ delta badge**:

```
┌─────────────────────────────┐
│  DPU Fleet                  │
│  ┌──┐                      │
│  │ 5│  ▁▁▂▂▃▃▃▃▅▅ ↗ +0    │
│  └──┘  (last 24h)          │
│  All healthy                │
└─────────────────────────────┘

┌─────────────────────────────┐
│  Active Flows               │
│  ┌──────┐                   │
│  │12,430│ ▁▂▃▅▇▅▃▂ ↗ +142  │
│  └──────┘  (last 1h)       │
│  94% fast path              │
└─────────────────────────────┘
```

- Delta badge: green `↗ +N` for increase, red `↘ −N` for decrease
- Sparkline: last 24h for slow metrics, last 1h for fast metrics
- Sub-text: key secondary insight (e.g., "94% fast path")

#### 8.1.3 Activity Heatmap (GitHub-style)

Below the summary cards, a **52-week activity heatmap** showing
operational activity intensity (mutations, reconciles, alerts) per day:

```
Activity (last 52 weeks):
Mon ░░░░░░▓▓░░░░░░░▓░░░░░░░░░▓▓▓░░░░░░░░░░░░▓░░░░░░░░
Wed ░░░░░░░▓▓▓░░░░░░░░░░░░░░░░▓▓░░░░░░░░░░░░░▓░░░░░░░░
Fri ░░░░░░░░▓░░░░░░░░░░░░░░░░░░▓▓▓░░░░░░░░░░░░░░░░░░░░
```

- Color intensity = number of mutations + events that day
- Click day → filter audit log to that date
- Tooltip: "23 mutations, 3 alerts, 1 reconcile on Jun 5"

#### 8.1.4 Mini Topology with Live Pulse

The mini topology doesn't just show nodes — it **pulses** them:

- Each DPU node pulses at a rate proportional to its pps
- Edges (tunnels) show directional particle flow
- New events cause a brief **ripple** emanating from the affected DPU
- The entire mini-topology subtly zooms to focus on whichever region has activity

---

### 8.2 Fleet View — Heatmap Overlay Modes

**Current:** Static topology + data table.
**Enhanced:** Multi-mode topology with switchable visualization layers.

#### 8.2.1 Overlay Mode Selector

A toolbar with toggle buttons switches the topology visualization:

| Mode | What it shows |
|---|---|
| **Health** (default) | Node color = health state (green/amber/red) |
| **Capacity** | Node size ∝ capacity usage %. Color: green→amber→red gradient. Over-capacity nodes throb. |
| **Traffic** | Edge thickness ∝ pps between endpoints. High-traffic edges glow brighter. Node size ∝ total throughput. |
| **Drift** | Nodes with drift items have a red dashed border + badge count. Clean nodes fade to 50% opacity. |
| **Age** | Node color = gradient from green (recently seen) to amber (minutes) to red (stale). Reveals connectivity issues. |

#### 8.2.2 Fleet Table with Inline Sparklines

Every numeric column in the fleet table gets an **inline sparkline** showing
the last 60 data points:

```
│ DPU   │ State   │ ENIs │ Capacity │ Flows        │ Last Seen │
│───────┼─────────┼──────┼──────────┼──────────────┼───────────│
│ DPU-1 │ ● UP    │ 2/8  │ ▓▓▓░ 38% │ ▁▂▃▅▇ 1,204 │ 2s ago   │
│ DPU-2 │ ● UP    │ 3/8  │ ▓▓▓▓ 50% │ ▁▁▂▃▃ 892   │ 1s ago   │
│ DPU-3 │ ⚠ DEGR │ 2/8  │ ▓▓▓░ 35% │ ▁▁▁▅▇ 1,891 │ 5s ago   │
```

- Capacity bar: inline colored bar with percentage
- Flow column: sparkline + current value
- Last Seen: relative time with color (green < 5s, amber < 30s, red > 30s)
- Row hover: entire row elevates with subtle shadow + glow

#### 8.2.3 Vnet Cards with Live Metrics

Each Vnet summary card shows a **micro-topology** of its DPU spread:

```
┌──────────────────────────────────┐
│  vnet-prod          VNI: 10001   │
│                                  │
│  ⬡─⬡─⬡     10 ENIs across 5 DPUs │
│  ⬡─⬡                             │
│                                  │
│  ▁▂▃▃▅▅▃▂  Flows: 6,230         │
│  Tunnels: 10  ● All active       │
└──────────────────────────────────┘
```

- Mini DPU cluster (dots in a grid showing DPU spread)
- Live flow sparkline
- Tunnel health indicator
- Click → Vnet dual-plane canvas view

---

### 8.3 DPU View — Digital Twin

**Current:** Header + panels + tables.
**Enhanced:** A visual "digital twin" of the DPU.

#### 8.3.1 DPU Hardware Schematic

Replace the header with an **isometric DPU schematic** showing ENI
slots as physical ports:

```
┌──────────────────────────────────────────────────┐
│         ⬡ DPU-1                                  │
│     ┌─────────────────────────────────────────┐  │
│     │  ┌───┐ ┌───┐ ┌───┐ ┌───┐ ┌───┐ ┌───┐  │  │
│     │  │ E1│ │ E2│ │   │ │   │ │   │ │   │  │  │  ← ENI "slots"
│     │  │ ●●│ │ ●●│ │   │ │   │ │   │ │   │  │  │    filled = in use
│     │  └───┘ └───┘ └───┘ └───┘ └───┘ └───┘  │  │    ● = active flows
│     │                                         │  │
│     │  State: UP ●    IP: 10.0.0.1            │  │
│     │  Uptime: 14h 23m  Version: v2.1.0       │  │
│     └─────────────────────────────────────────┘  │
│                                                  │
│  Capacity: ██████░░ 2/8 ENIs (25%)               │
└──────────────────────────────────────────────────┘
```

- ENI slots rendered as physical port rectangles
- Filled slots: colored by Vnet (cyan=vnet-prod, green=vnet-staging)
- Empty slots: gray/dashed border
- Each slot has tiny activity dots (pulsing when traffic active)
- Hover slot → ENI tooltip (name, MAC, Vnet, flows)
- Click slot → expand ENI detail panel

#### 8.3.2 ENI Pipe Visualization with Traffic Animation

Each ENI is visualized as a **pipe** with flowing traffic particles:

```
ENI-01 (aa:bb:cc:dd:ee:01)  vnet-prod
═══════════►════►════►═══════════
  142 flows   12K pps   4.2 MB/s
  ● up                  ▁▂▃▅▇▃▂
```

- Pipe width ∝ flow count (thicker pipe = more flows)
- Particle speed ∝ pps (faster particles = more traffic)
- Particle color: cyan for normal, amber for slow path, red for drops
- Below pipe: inline sparkline of traffic rate
- If ENI is `admin_state=down`: pipe turns gray, no particles, "ADMIN DOWN" badge

#### 8.3.3 Capacity Gauges with Prediction

Each capacity gauge shows not just current but **projected fill time**:

```
     ENIs                Routes
   ╭─────╮             ╭─────╮
  ╱  25%  ╲           ╱  72%  ╲
 │  2 / 8  │         │ 142/196 │
  ╲  ↗    ╱           ╲  ↗↗  ╱
   ╰─────╯             ╰─────╯
  ~32 days             ~3 days ⚠️
  until full           until full
```

- Arc color: threshold-based (green → amber → red)
- Below gauge: "estimated time until full" based on recent growth rate
- Growth rate calculated from rolling 24h delta
- Warning icon if projected to fill within 7 days

#### 8.3.4 Quick Action Floating Bar

A persistent floating action bar at the bottom of the DPU view:

```
┌────────────────────────────────────────────────────────┐
│ [⊕ Add ENI]  [🔄 Reconcile]  [🚫 Cordon]  [📤 Drain] │
│ [📊 Trace Flow]  [📋 View Observed]  [⋯ More]        │
└────────────────────────────────────────────────────────┘
```

- Context-aware: "Cordon" becomes "Uncordon" if already cordoned
- "Drain" disabled if no ENIs
- Each button has keyboard shortcut hint on hover

---

### 8.4 Routing View — Route Resolution Visualizer

**Current:** Prefix tree + table.
**Enhanced:** Interactive route resolution with animated path tracing.

#### 8.4.1 Animated Route Resolution

When clicking on a prefix in the tree, animate the **resolution path**:

```
10.0.1.5 resolves to:

10.0.0.0/8 (root)
    └─→ 10.0.0.0/16 (vnet)
        └─→ 10.0.1.0/24 (vnet-prod)      ← MATCH
            next-hop: vnet (vnet-prod)
            via DPU-1, DPU-3
```

- Tree path highlights step-by-step with animated traversal
- Each node shows match/miss with reason
- Final match node glows + shows next-hop detail
- "Test an IP" input: type any IP → watch it resolve in real-time

#### 8.4.2 Route Coverage Map

A horizontal bar visualization showing **which prefix ranges are
covered** vs gaps:

```
0.0.0.0 ─────────────────────────────── 255.255.255.255
         ████████░░░░████████████░░░░░░████████████████
         10.0.0/8     172.16/12         192.168/16
              └── vnet   └── svc_tunnel   └── direct

Gaps: 11.0.0.0 – 172.15.255.255 (DROP: no matching route)
```

- Colored by next-hop type
- Gaps highlighted in red with "no route" tooltip
- Click segment → jump to that route in the table

#### 8.4.3 ECMP Path Visualization

For routes with ECMP members, show a **fan-out diagram**:

```
10.0.1.0/24 → ECMP (3 paths):
    ├── 33% → vnet-prod (via DPU-1)  weight: 1
    ├── 33% → vnet-prod (via DPU-3)  weight: 1
    └── 34% → vnet-prod (via DPU-5)  weight: 1
```

- Visual fan-out from prefix to targets
- Width of each path ∝ weight
- Live traffic split shown if counters available

---

### 8.5 Tunnel View — Traffic-Weighted Mesh

**Current:** Map + table.
**Enhanced:** Live traffic-weighted tunnel mesh.

#### 8.5.1 Traffic-Proportional Edge Rendering

Tunnel edges have visual properties that encode live traffic data:

| Property | Encodes |
|---|---|
| **Line width** | Total pps through tunnel (1px → 6px) |
| **Particle speed** | Packets/sec (slow = low traffic, fast = high traffic) |
| **Particle size** | Average packet size (small = small packets, large = jumbo) |
| **Color saturation** | Utilization % (dim = low, bright = high) |
| **Dash pattern** | State: solid = ACTIVE, dashed = DOWN, dotted = UNKNOWN |

#### 8.5.2 Tunnel Pair Highlighting

Click a DPU → **highlight all its tunnel connections**, fade others:

```
           ⬡ DPU-1 ─────────── ⬡ DPU-2   (highlighted, bright)
                   ╲           ╱
                    ╲         ╱
       ⬡ DPU-5 ──── ╳ ────── ⬡ DPU-3    (highlighted, bright)
                    ╱         ╲
                   ╱           ╲
           ⬡ DPU-4             (faded, 20% opacity)
```

- Selected DPU: bright glow + all its tunnels highlighted
- Unrelated tunnels: fade to 20% opacity
- Shows total traffic to/from selected DPU in a summary badge

---

### 8.6 Policy View — Rule Blocks Visualization

**Current:** Expandable tables.
**Enhanced:** Visual "rule blocks" with inline testing.

#### 8.6.1 ACL Rule Blocks

Render ACL rules as **stacked colored blocks** (inspired by firewall
rule visualizers):

```
ACL: web-frontend-acl (INBOUND, eni-01, eni-02)

Priority ▼
┌─────────────────────────────────────────────────────┐
│ 10  ALLOW  10.0.0.0/8 → *  TCP 80,443              │ ████ 142K hits
├─────────────────────────────────────────────────────┤
│ 20  ALLOW  172.16.0.0/12 → *  ANY                   │ ███ 89K hits
├─────────────────────────────────────────────────────┤
│ 30  DENY   192.168.0.0/16 → *  ANY                  │ █ 2K hits
├─────────────────────────────────────────────────────┤
│ 40  ALLOW  0.0.0.0/0 → 10.0.1.0/24  UDP 53          │ ░ 0 hits 💀
├─────────────────────────────────────────────────────┤
│ 999 DENY   *  →  *  ANY                (default deny)│ ░ 48 hits
└─────────────────────────────────────────────────────┘
```

- Block height ∝ hit count (more hits = taller block)
- ALLOW = green background tint, DENY = red background tint
- Hit count bar on the right (proportional to max hits)
- Dead rules (0 hits): dimmed with 💀 badge
- Hover rule → expanded detail (prefixes, ports, description)
- **Inline "Test" button** per rule: enter an IP → show if this rule would match

#### 8.6.2 Policy Conflict Detector

Automatically detect rules that **shadow** each other:

```
⚠️ Conflict detected:
  Rule 20 (ALLOW 172.16.0.0/12 → * ANY) completely shadows
  Rule 40 (ALLOW 0.0.0.0/0 → 10.0.1.0/24 UDP 53)
  because 10.0.1.0/24 ⊂ 172.16.0.0/12 and rule 20 has higher priority.
  Rule 40 will NEVER be evaluated. [Remove Rule 40?]
```

---

### 8.7 Flow Trace View — Interactive Packet Builder

**Current:** Form + result.
**Enhanced:** Visual packet builder with protocol layer visualization.

#### 8.7.1 Visual Packet Constructor

Instead of plain input fields, show the packet as **protocol layers**:

```
┌──────────────────────────────────────────────────────┐
│  PACKET BUILDER                                      │
│                                                      │
│  ┌────────────────────────────────────────────────┐  │
│  │  L2: Ethernet                                  │  │
│  │  Src MAC: [aa:bb:cc:dd:ee:01]  ENI: [eni-01▼] │  │
│  ├────────────────────────────────────────────────┤  │
│  │  L3: IPv4                                      │  │
│  │  Src: [10.0.0.5]    Dst: [10.0.1.10]          │  │
│  │  VNI: [10001]        Direction: [OUTBOUND ▼]   │  │
│  ├────────────────────────────────────────────────┤  │
│  │  L4: TCP                                       │  │
│  │  Src Port: [8080]    Dst Port: [443]           │  │
│  └────────────────────────────────────────────────┘  │
│                                                      │
│  [🔍 Trace Packet]                                   │
└──────────────────────────────────────────────────────┘
```

- Protocol layers rendered as stacked cards (like Wireshark)
- L2/L3/L4 labels with protocol-appropriate coloring
- Auto-fill ENI's MAC when selecting from dropdown
- "Quick presets" button: common test packets (HTTP, DNS, ICMP)

#### 8.7.2 Trace Result Comparison

"Compare" button lets operators run two traces side-by-side:

```
TRACE A (before policy change)     TRACE B (after policy change)
ACL IN:  Rule 50 ALLOW  ✅        ACL IN:  Rule 30 DENY  ❌
Route:   10.0.1.0/24 vnet  ✅     Route:   (not reached)
Verdict: ENCAP ✅                  Verdict: DROP_ACL ❌

DIFF: Rule 30 (new) matches before Rule 50. Packet now denied.
```

- Side-by-side animated pipeline traces
- Diff highlighting: stages that differ glow red
- "What changed?" summary below

---

### 8.8 Audit Log View — Rich Activity Stream

**Current:** Flat table with filters.
**Enhanced:** Rich activity stream with actor avatars and impact badges.

#### 8.8.1 Activity Cards (not rows)

Instead of a flat table, render each audit entry as a **card**:

```
┌──────────────────────────────────────────────────────┐
│  🔵 PutAclPolicy                        14:28:01.012 │
│  operator@corp → acl_policy/default/web-acl          │
│  txn: abc-123  gen: 42                               │
│                                                      │
│  Impact: 3 DPUs dispatched  ✅✅✅                    │
│  Affected ENIs: eni-01, eni-02                       │
│  [View Diff] [View Policy] [Replay Change]           │
└──────────────────────────────────────────────────────┘
```

- Actor avatar (initials circle) with principal name
- RPC badge (color-coded: PUT=blue, DELETE=red, RECONCILE=amber)
- Impact line: how many DPUs were affected + their dispatch status
- Affected resources listed
- Quick action links: view diff, navigate to resource, replay

#### 8.8.2 Actor Swimlane View

Toggle to see audit entries grouped by **actor** (who did what):

```
operator@corp:     ──●──●────●──────────●───── 14 actions
ci-pipeline@bot:   ────────●──────●──────────── 4 actions
admin@oncall:      ──────────────────────●───── 1 action
                   10:00                   now
```

- Each actor is a horizontal swimlane
- Dots = actions, sized by impact (number of DPUs affected)
- Color = action type (blue=PUT, red=DELETE, amber=RECONCILE)
- Click dot → expand to full audit card

---

### 8.9 Health View — Live Cluster Vitals

**Current:** Panels with text.
**Enhanced:** Visual vital signs monitor.

#### 8.9.1 Cluster Vital Signs

```
┌────────────────────────────────────────────────────────┐
│  CLUSTER VITALS                                        │
│                                                        │
│  ♥ Heartbeat     ▁▁▁▁▁▁▁▁▁▁  Every 5s  ✅ On time    │
│  ⚡ Leader        node-01     Uptime: 14h 23m          │
│  🔄 Reconcile    Last: 2m ago  Pending: 0  ✅          │
│  📊 API Latency  p50: 4ms  p95: 12ms  p99: 42ms       │
│                  ▁▁▂▂▃▅▃▂▁▁  (last 1h)               │
│                                                        │
│  DPU CONNECTION STATUS:                                │
│  ●●●●●  5/5 connected    Last check: 2s ago           │
│  ● DPU-1 (2ms) ● DPU-2 (3ms) ● DPU-3 (4ms)          │
│  ● DPU-4 (3ms) ● DPU-5 (2ms)                         │
└────────────────────────────────────────────────────────┘
```

- Heartbeat line: ECG-style trace showing health check timing
- Leader with uptime and election history link
- Per-DPU connection latency as color-coded dots (green < 10ms, amber < 50ms)
- API latency percentiles with sparkline

#### 8.9.2 Leader Election Timeline

A horizontal timeline showing leader election history:

```
Leader History:
node-01 ████████████████████████████████████████ (14h 23m, current)
node-02 ███████                                  (2h, previous)
node-01 ████████████████████████████████         (8h)
        └────────────────────────────────────── now
```

- Each bar = a leadership term
- Color: current leader = green, previous = gray
- Click bar → show election details (reason for change)

---

### 8.10 Admin Ops View — Visual Resource Builder

**Current:** Forms.
**Enhanced:** Visual drag-and-drop resource builder.

#### 8.10.1 Visual ENI Placement

When creating ENIs, show a visual **DPU placement grid**:

```
Choose placement for new ENI:

  ⬡ DPU-1         ⬡ DPU-2         ⬡ DPU-3
  2/8 ENIs (25%)   3/8 ENIs (38%)   2/8 ENIs (25%)
  █░░░             ██░░             █░░░
  [Drop here]      [Drop here]      [Drop here] ← recommended ⭐

  ⬡ DPU-4         ⬡ DPU-5
  4/8 ENIs (50%)   1/8 ENIs (13%)
  ████             █░░░░░░░
  [Drop here]      [Drop here] ← most capacity
```

- DPU grid shows capacity bars
- Recommended DPU highlighted with star
- Drag ENI card to a DPU to set placement hint
- "Auto" button: let dashd decide (no placement hint)

#### 8.10.2 Live Diff Preview

Before submitting any mutation, show a **side-by-side diff** with
syntax highlighting:

```
┌─── Current ──────────────┬─── Proposed ─────────────┐
│ {                        │ {                         │
│   "name": "eni-01",      │   "name": "eni-01",      │
│   "vnet_name": "prod",   │   "vnet_name": "prod",   │
│   "admin_state": "up",   │   "admin_state": "down",  │ ← changed
│   "mac": "aa:bb:cc:..."  │   "mac": "aa:bb:cc:..."   │
│ }                        │ }                         │
└──────────────────────────┴───────────────────────────┘

Changes: 1 field modified (admin_state: up → down)
Impact: ENI will stop processing traffic on DPU-1.
[Cancel]  [Apply Change]
```

- Changed fields highlighted (green=added, red=removed, amber=modified)
- Impact summary auto-generated from diff
- "Simulate" button: dry-run before applying

---

### 8.11 Command View — Intelligent Shell

**Current:** Catalog + builder.
**Enhanced:** Context-aware intelligent shell.

#### 8.11.1 Context-Aware Autocomplete

As operators type in the command builder, show **context-aware suggestions**:

```
dashctl get en▊
              ├── eni          (ENI resources)
              ├── eni-01       (existing ENI)
              └── eni-02       (existing ENI)
```

- Suggestions ranked by: recently used → matching resources → all options
- Resource names auto-complete from live data (query dashd)
- Flag values auto-complete (e.g., `--dpu` suggests DPU IDs)

#### 8.11.2 Output Visualization

Command output is not just text — it's **rendered visually**:

```
$ dashctl get eni eni-01 -o wide

┌─────────────────────────────────────────────────┐
│  eni-01                                         │
│  Vnet: vnet-prod   MAC: aa:bb:cc:dd:ee:01       │
│  DPU: DPU-1        State: ● UP                  │
│  Underlay: 10.0.0.1                             │
│                                                  │
│  Capacity: ████████░░ 80%                        │
│  Flows: ▁▂▃▅▇ 1,204                            │
│                                                  │
│  [Open in DPU View]  [Edit]  [Delete]           │
└─────────────────────────────────────────────────┘
```

- Resource outputs rendered as rich cards (not raw JSON)
- List outputs rendered as interactive tables
- "Copy as JSON" / "Copy as YAML" toggle
- Action buttons on output cards for quick follow-up

---

### 8.12 Debug View — Network DevTools

**Current:** Raw API caller + sim inspector.
**Enhanced:** Chrome DevTools-inspired network debugger.

#### 8.12.1 Request Waterfall

Show all API requests in a **Chrome DevTools-style waterfall**:

```
GET  /api/console/fleet/summary     ████         42ms  200
GET  /api/admin/health              ██████       68ms  200
PUT  /api/v1/default/enis/eni-01    ████████     112ms 200
GET  /api/admin/drift?dpu=dpu-3     ███          28ms  200
WS   /ws/dpu-status                 ▓▓▓▓▓▓▓▓▓▓▓ open (14m)
```

- Horizontal bars proportional to response time
- Color: green (< 50ms), amber (50–200ms), red (> 200ms)
- Click request → expand to see full request/response
- WS connections shown as persistent bars
- Filter: method, status code, path pattern

#### 8.12.2 WebSocket Frame Inspector

For the WS tester, show frames with **JSON path highlighting**:

```
WS /ws/dpu-status Frame #42:
┌─────────────────────────────────────────────────┐
│ {                                               │
│   "type": "dpu_status",                         │
│   "seq": 42,                                    │
│   "data": {                                     │
│     "identity": {                               │
│       "dpu_id": "dpu-1",        ← click to copy │
│       "appliance_id": "app-1"                   │
│     },                                          │
│     "state": 2,           ← DPU_STATE_UP        │
│     "usage": {                                  │
│       "enis_used": 2      ← 2/8 = 25%          │
│     }                                           │
│   }                                             │
│ }                                               │
└─────────────────────────────────────────────────┘
```

- JSON syntax highlighted
- Enum values annotated with human-readable names
- Usage values annotated with percentages (from limits)
- Click any value → copy to clipboard
- Diff mode: highlight changes between consecutive frames

---

### 8.13 General Enhancements (All Views)

#### 8.13.1 Contextual Right-Click Menu

Every resource element (DPU badge, ENI name, Vnet card, policy name)
has a **right-click context menu**:

```
Right-click on "DPU-1":
┌─────────────────────┐
│ Open DPU View       │
│ View ENIs           │
│ View Flows          │
│ ─────────────────── │
│ Cordon              │
│ Drain               │
│ Reconcile           │
│ ─────────────────── │
│ Copy ID             │
│ Copy IP             │
│ View in JSON        │
└─────────────────────┘
```

#### 8.13.2 Cross-View Breadcrumb Trail

Navigation breadcrumbs show the **path the operator took**:

```
Dashboard → Fleet → DPU-1 → Flow Trace (from eni-01)
```

- Click any crumb to go back
- Preserves context (e.g., DPU filter carries to Flow Trace)

#### 8.13.3 Spotlight Search with Preview

The `Cmd+K` command palette shows **live preview** of the selected result:

```
⌘ K: Search
┌─────────────────────┬──────────────────────────────────────┐
│ > eni-01            │  eni-01                              │
│                     │  Vnet: vnet-prod  MAC: aa:bb:cc:..   │
│ ● eni-01 (ENI)     │  DPU: DPU-1  State: ● UP             │
│ ● eni-02 (ENI)     │  Flows: 1,204  Capacity: 80%         │
│ ○ /dpu/dpu-1 (view)│                                      │
│ ○ get eni (command) │  [Open] [Edit] [Delete]              │
└─────────────────────┴──────────────────────────────────────┘
```

- Left: search results with type icons
- Right: live preview panel showing resource details
- Quick action buttons in preview

#### 8.13.4 Keyboard Shortcuts Layer

A transparent keyboard shortcut overlay (toggle with `?`):

```
┌────────────────────────────────────────────────────────┐
│  KEYBOARD SHORTCUTS                                    │
│                                                        │
│  Navigation:                                           │
│  G D  → Dashboard      G F  → Fleet      G P  → Policy│
│  G T  → Tunnels        G A  → Admin      G C  → Commands
│                                                        │
│  Actions:                                              │
│  ⌘ K  → Search         ⌘ S  → Save       ⌘ R  → Refresh
│  N    → New Resource    E    → Edit        D    → Delete
│  ?    → This help                                      │
└────────────────────────────────────────────────────────┘
```

#### 8.13.5 Dark/Light Mode with Smooth Transition

While the primary theme is "Network Dark", offer a **light mode** for
operators who prefer it, with a smooth 300ms CSS transition:

- Toggle in TopBar (sun/moon icon)
- All CSS custom properties swap
- Saved in `ui-prefs-store` (persisted)
- Charts and visualizations adjust colors automatically

---

> **End of Vision.** This document is a blueprint for transforming
> dashw from a management console into a **network operations
> intelligence platform** — one that fully exploits the extraordinary
> richness of the DashCenter API surface. Every feature described here
> is backed by data that dashd already exposes. No new backend
> endpoints are needed beyond the BFF aggregation layer already designed
> in the existing LLD.
>
> **Implementation plan:** [`specs/Impl-Plan/dashw-web-impl-plan.md`](../Impl-Plan/dashw-web-impl-plan.md)
> tracks all phases (A through E) with gate-level detail.
