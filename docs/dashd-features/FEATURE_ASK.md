# DashCenter — Feature Requirements Specification

> **Status:** Draft v1 (2026-06-17)
> **Purpose:** Capture 30 production-grade feature requests across dashd,
> dashctl, dashw, dash-sim, and cross-cutting platform capabilities, written
> as formal feature requirements with problem, use cases, value, and success
> criteria.
> **Companion:** [`../roadmap.md`](../roadmap.md) §6 lists the
> same items in priority/tracking-table form. This document is the long-form
> requirements source-of-truth used for design intake.

---

## How to read each feature

Every feature below uses the same structure so product, engineering, and
operations can review it consistently:

- **Title** — short, intent-revealing name.
- **Category** — which sub-project owns the change.
- **Problem statement** — the operational or business pain the feature exists to remove.
- **Primary use cases** — concrete scenarios where the feature is invoked.
- **Proposed capability** — what the system will do once the feature ships.
- **Functional requirements** — must-have behaviors expressed as testable statements.
- **Non-functional requirements** — performance, security, observability, compatibility.
- **Business value** — why this matters to operators, platform teams, and customers.
- **Success metrics** — measurable indicators of "shipped well".
- **Dependencies** — features, specs, or external systems required first.
- **Spec references** — pointers to existing HLD/LLD/impl-plan documents.

---

## Section 1 — `dashd` (control plane)

### F1. Atomic cross-DPU `ApplyBatch` with saga rollback

- **Category:** dashd / Operations
- **Problem statement:** Today each spec apply is independent. A multi-object change that touches several DPUs can leave the fleet in a partially-applied state when a single DPU rejects its write, requiring manual cleanup and increasing outage risk for large rollouts.
- **Primary use cases:**
  - Rolling out a tenant onboarding bundle (VNet + ENIs + mappings + ACLs + routes) where any failure must revert all objects.
  - Migrating a service tunnel across 10 DPUs where a half-applied change would split traffic.
  - GitOps reconciliation jobs that need transactional commits to support safe Git revert.
- **Proposed capability:** A single `ApplyBatch` API and CLI command that commits the full set of operations or rolls back every object on partial failure, using a saga coordinator persisted in the same store as the operation.
- **Functional requirements:**
  - Server accepts a batch of typed operations (Put/Delete) with a single CAS expectation per object.
  - On any operation failure the saga executes compensating actions for already-applied operations.
  - Final outcome (committed | rolled_back | stuck) is durable, queryable, and audited.
  - Idempotent retry with a client-supplied batch id.
- **Non-functional requirements:**
  - Worst-case rollback completes within a documented bounded time for ≤ N objects (target N=500).
  - Concurrent batches do not deadlock; conflicting CAS surfaces as a structured error.
- **Business value:** Removes a major class of partial-failure incidents and is a prerequisite for safe, scriptable fleet automation and GitOps adoption.
- **Success metrics:** Zero observed partial-applies in soak tests; rollback completion p95 within SLO; reduction in operator-driven cleanup tickets.
- **Dependencies:** Existing reconciler, audit log, store CAS, observability events.
- **Spec references:** [`specs/Impl-Plan/impl-plan-advanced.md`](../../specs/Impl-Plan/impl-plan-advanced.md) P2-M12 saga coordinator.

---

### F2. gNMI Subscribe bridge for DashCenter events

- **Category:** dashd / Telemetry
- **Problem statement:** Many operator stacks already speak gNMI/OpenConfig for telemetry. They cannot consume DashCenter `WatchEvents` natively, forcing custom shims for every integration.
- **Primary use cases:**
  - Sending DashCenter object/state events into existing gNMI collectors (gnmic, Cisco, Arista, internal SREs).
  - Building cross-vendor dashboards without learning the DashCenter proto.
  - Compliance reporting that already standardizes on gNMI for change capture.
- **Proposed capability:** A read-only gNMI Subscribe bridge embedded in dashd that maps `WatchEvents` payloads to a stable OpenConfig-like path schema.
- **Functional requirements:**
  - gNMI Subscribe (STREAM mode, ON_CHANGE) supported for inventory, resource lifecycle, and reconcile drift events.
  - Path schema and timestamp semantics documented and versioned.
  - Backpressure: slow consumers do not block dashd; lagging streams are disconnected with diagnostics.
- **Non-functional requirements:**
  - Mapping latency p95 ≤ 100 ms relative to native `WatchEvents`.
  - Schema additions are backward-compatible.
- **Business value:** Unlocks immediate integration with existing telemetry investments, accelerating enterprise adoption.
- **Success metrics:** Number of supported gNMI clients (gnmic, telegraf-gnmi plugin) passing conformance tests; reduction in custom-integration effort reported by adopters.
- **Dependencies:** `WatchEvents` (already implemented), proto event schema stability.
- **Spec references:** [`specs/HLD/dashd-hld.md`](../../specs/HLD/dashd-hld.md) §15 future work; [`specs/Impl-Plan/impl-plan-advanced.md`](../../specs/Impl-Plan/impl-plan-advanced.md) P2-M12.

---

### F3. Controllerless mode (embedded gossip + Raft)

- **Category:** dashd / Topology
- **Problem statement:** Edge and constrained deployments cannot afford a separate controller tier with external etcd. The HLD specifies dual-mode operation but only controller mode is implemented.
- **Primary use cases:**
  - Edge POPs and ROBO sites that need DashCenter without dedicated control hardware.
  - Self-contained appliances where the same DPUs cooperatively own state.
  - Air-gapped environments where running external coordinators is prohibited.
- **Proposed capability:** Single binary started in `mode: controllerless` joins a gossip mesh, elects a leader through embedded Raft, and serves the same northbound API as controller mode.
- **Functional requirements:**
  - Membership discovery via gossip with seed list and optional encryption key.
  - Raft-backed state with snapshotting and configurable thresholds.
  - Identical northbound API surface and behavior as controller mode.
  - Documented failover convergence target under N-1 node loss.
- **Non-functional requirements:**
  - Disk and memory footprint quantified for embedded deployment classes.
  - Upgrade compatibility with controller-mode clusters' object semantics.
- **Business value:** Expands addressable deployments to edge and self-contained classes, removing a major adoption blocker for non-data-center environments.
- **Success metrics:** Failover convergence within SLO; certified operating envelope; successful soak in reference edge deployment.
- **Dependencies:** Storage abstraction already in place; configuration contract for `mode/ha/storage` already frozen.
- **Spec references:** [`specs/HLD/dashd-hld.md`](../../specs/HLD/dashd-hld.md) §10 controllerless mode; [`specs/Impl-Plan/impl-phases.md`](../../specs/Impl-Plan/impl-phases.md) PF.

---

### F4. Enterprise identity integration (OIDC / AAD)

- **Category:** dashd / Security
- **Problem statement:** Production deployments require centrally managed identity, token rotation, and audited access. The current auth stack only supports static tokens and mTLS; the OIDC interface in the implementation plan is an explicit stub.
- **Primary use cases:**
  - SSO login for operator personas via corporate IdP (Azure AD, Okta, Google).
  - Short-lived token issuance with automatic rotation for CI pipelines.
  - Role/claim mapping to viewer/operator/admin per business unit or tenant.
- **Proposed capability:** Pluggable identity provider with OIDC PKCE flow, JWT validation, introspection support, and configurable claim-to-role mapping.
- **Functional requirements:**
  - dashctl and dashw can authenticate via browser-based OIDC flow; dashd validates tokens.
  - Role mappings configurable via `dashd.yaml` and reloadable without restart.
  - Token revocation honored within configured propagation window.
- **Non-functional requirements:**
  - Token validation latency p95 ≤ 20 ms.
  - No plaintext secrets persisted; key rotation supported.
- **Business value:** Removes the largest blocker to enterprise/production deployment; satisfies common compliance and audit requirements.
- **Success metrics:** Successful integration with at least two major IdPs; audit logs include resolved subject and role.
- **Dependencies:** Existing auth interface; PD security gates landed.
- **Spec references:** [`specs/Impl-Plan/impl-plan-advanced.md`](../../specs/Impl-Plan/impl-plan-advanced.md) P2-M9; [`docs/dashd-features/features.md`](./features.md) §2.

---

### F5. TraceFlow + ExplainMatch parity on real DPU agents

- **Category:** dashd / Diagnostics
- **Problem statement:** Diagnostics (TraceFlow, ExplainMatch) work end-to-end against the simulator but require parity work on real DPU agents via `dash-redis-adapter`. Without parity, operators cannot trust diagnostic output during real-world incidents.
- **Primary use cases:**
  - Post-incident root-cause analysis on production DPUs.
  - Customer-support engineers reproducing reported issues against live fleets.
  - Pre-deployment validation of policy changes on real hardware.
- **Proposed capability:** Diagnostics RPCs return equivalent semantically-correct results regardless of whether the target is sim or real DPU agent.
- **Functional requirements:**
  - Capability matrix documents which diagnostic features each agent supports.
  - Where a feature is unsupported, the response includes a clear `Unimplemented` reason rather than a misleading partial answer.
  - Conformance tests run against both sim and adapter targets.
- **Non-functional requirements:**
  - Diagnostic round-trip latency p95 ≤ existing sim performance + bounded overhead.
- **Business value:** Production-grade troubleshooting capability that closes the simulator-vs-hardware gap and reduces mean-time-to-resolution.
- **Success metrics:** Conformance suite passes on adapter; reduction in time-to-diagnosis in incident postmortems.
- **Dependencies:** PE-1 diagnostics in sim (done), PE-3 counters (done).
- **Spec references:** [`specs/Impl-Plan/impl-phases.md`](../../specs/Impl-Plan/impl-phases.md) PE-4.

---

### F6. Server-side counter aggregation and rollups

- **Category:** dashd / Observability
- **Problem statement:** Raw per-DPU counter streams produce high-cardinality time-series that overload downstream metrics stores and dashboards. Operators currently must run their own aggregation pipelines.
- **Primary use cases:**
  - Per-VNet, per-tenant, and per-ENI utilization views in dashboards.
  - Capacity planning queries over rollups instead of raw counters.
  - Alerts on aggregate (not per-DPU) thresholds.
- **Proposed capability:** dashd performs configurable server-side aggregation across DPUs by tenant/VNet/ENI dimensions with selectable retention windows.
- **Functional requirements:**
  - Aggregation interval and dimensions are configurable per cluster.
  - Rollups are queryable through the existing counter API surface with a stable schema.
  - Raw and rolled-up streams can be subscribed independently.
- **Non-functional requirements:**
  - Memory and CPU cost grow sub-linearly with active rollup dimensions.
  - Backpressure does not drop raw counters silently.
- **Business value:** Reduces observability cost, improves dashboard quality, and removes a common downstream pipeline burden.
- **Success metrics:** Cardinality reduction ratio vs raw; query latency improvements in dashw and external dashboards.
- **Dependencies:** PE-3c counter streaming end-to-end (done).
- **Spec references:** [`docs/dashd-features/features.md`](./features.md) §10A; [`specs/Impl-Plan/impl-phases.md`](../../specs/Impl-Plan/impl-phases.md) PE-3c.

---

### F7. Pluggable admission webhook framework

- **Category:** dashd / Extensibility
- **Problem statement:** Organizations want to enforce custom policies (IPAM coordination, naming conventions, capacity caps per tenant, compliance gates) without forking dashd. The admission gate is currently hard-coded.
- **Primary use cases:**
  - Reject ENIs whose underlay IPs collide with an external IPAM allocation.
  - Enforce per-tenant ACL rule budgets.
  - Add organization-specific naming and tag policies.
- **Proposed capability:** Configurable admission webhook contract invoked synchronously during admission; pass/deny/mutate semantics defined.
- **Functional requirements:**
  - Webhooks configured per cluster with timeout, fail-open/fail-closed policy.
  - Structured deny responses with reason codes surfaced to client and audit log.
  - Mutating webhooks return a normalized object; conflicts are deterministic.
- **Non-functional requirements:**
  - Webhook timeout default ≤ 1 s; configurable cap.
  - Webhook outage cannot crash dashd; circuit breaker prevents cascading failures.
- **Business value:** Enables enterprise customization without forking; supports IPAM/governance integrations that block adoption today.
- **Success metrics:** Successful integration with at least one external IPAM in a reference deployment; webhook latency stays within SLO.
- **Dependencies:** Existing admission gate; auth (F4) for webhook authentication.
- **Spec references:** [`specs/HLD/dashd-hld.md`](../../specs/HLD/dashd-hld.md) §6.

---

### F8. Multi-region federation (read aggregation)

- **Category:** dashd / Platform
- **Problem statement:** Operators running DashCenter in multiple regions have no unified view; they must query each cluster separately and stitch results manually.
- **Primary use cases:**
  - Global capacity dashboards spanning regions.
  - Cross-region security/compliance reports.
  - Single console pane that lists DPUs from all clusters with status.
- **Proposed capability:** A federation read API that aggregates inventory and key status from multiple dashd clusters into one queryable surface.
- **Functional requirements:**
  - Read-only by design; writes still go to the regional cluster.
  - Per-region partial failures degrade the aggregated response with explicit status.
  - Namespace isolation preserved end-to-end.
- **Non-functional requirements:**
  - Aggregator latency p95 ≤ slowest healthy region + bounded fan-out overhead.
- **Business value:** Reduces operator toil at multi-region scale and improves governance.
- **Success metrics:** Mean time to produce cross-region report; adoption by ops dashboards.
- **Dependencies:** Stable inventory APIs; auth model for cross-cluster identity (F4).
- **Spec references:** [`specs/HLD/dashd-hld.md`](../../specs/HLD/dashd-hld.md) §15.

---

### F9. Drift auto-remediation mode

- **Category:** dashd / Reliability
- **Problem statement:** Reconciliation is operator-triggered today. At fleet scale, undetected drift accumulates between reconciles, increasing risk and complicating debugging.
- **Primary use cases:**
  - Continuous remediation of well-known categories of drift on production fleets.
  - Dry-run mode for safe rollout of auto-remediation policies.
  - Per-tenant policies (e.g., remediate ACLs automatically, never remediate routes automatically).
- **Proposed capability:** Configurable continuous reconcile mode with category-aware policies, dry-run vs enforce, back-off, and audit.
- **Functional requirements:**
  - Policies expressed declaratively; reloadable.
  - Dry-run emits the same audit/observability events as enforce, without mutating state.
  - Back-off prevents thrash on flapping resources.
- **Non-functional requirements:**
  - No measurable steady-state CPU regression vs on-demand reconcile when policies are disabled.
- **Business value:** Improves configuration hygiene and reduces operator toil; enables a safer "operations as code" posture.
- **Success metrics:** Reduction in time-to-converge for known drift categories; zero remediation-induced incidents in soak.
- **Dependencies:** Idempotent reconciler (in place); audit log; observability events.
- **Spec references:** [`specs/HLD/dashd-hld.md`](../../specs/HLD/dashd-hld.md) §9.

---

## Section 2 — `dashctl` (operator CLI)

### F10. gRPC transport (Phase 2) for dashctl

- **Category:** dashctl / Transport
- **Problem statement:** dashctl currently uses REST and lacks first-class server streaming, batching, and exact-once semantics that gRPC provides.
- **Primary use cases:**
  - `dashctl events --watch` and `dashctl migration stream` with native server-stream behavior.
  - Long-running operations with stable reconnect/replay behavior.
  - High-throughput CI usage with reduced round-trip overhead.
- **Proposed capability:** Full gRPC backend behind the existing `Client` interface, selectable per context.
- **Functional requirements:**
  - All northbound RPCs available over gRPC with parity to REST.
  - Streaming commands reconnect cleanly and surface cancellation within a documented window.
  - Cross-platform builds remain CGO-free and statically linked.
- **Non-functional requirements:**
  - Cold-start latency unchanged; binary size growth within budget.
- **Business value:** Unlocks streaming workflows and future-proofs CLI tooling for advanced server features.
- **Success metrics:** Full Phase 2 gate matrix passes; user-reported workflow latency drops in streaming commands.
- **Dependencies:** dashd Phase 2 streaming RPCs.
- **Spec references:** [`specs/Impl-Plan/dashctl-impl-phases.md`](../../specs/Impl-Plan/dashctl-impl-phases.md) Phase 2.

---

### F11. `dashctl debug` low-level command suite

- **Category:** dashctl / Support tooling
- **Problem statement:** Support engineers need raw inspection and request reproduction tools during incidents. Today they fall back to ad-hoc curl/grpcurl and lose context.
- **Primary use cases:**
  - Reproduce a customer issue with a minimal repro payload.
  - Inspect raw server response without envelope decoration.
  - Generate a redacted `curl`/`grpcurl` command for handoff to other teams.
- **Proposed capability:** `dashctl debug put-raw`, `debug get-raw`, `debug curl`, `debug admin` sub-commands.
- **Functional requirements:**
  - Sensitive auth headers are redacted by default in generated commands.
  - Raw modes bypass envelope codec while preserving auth and TLS context.
  - All commands have unit and integration tests including auth-redaction.
- **Non-functional requirements:**
  - No new dependency footprint beyond existing client.
- **Business value:** Faster incident triage and reduced support escalation overhead.
- **Success metrics:** Reduction in mean time to gather repro evidence in support cases.
- **Dependencies:** None.
- **Spec references:** [`specs/LLD/dashctl-debug.md`](../../specs/LLD/dashctl-debug.md).

---

### F12. `dashctl diff` with three-way and live modes

- **Category:** dashctl / Change safety
- **Problem statement:** Operators reviewing GitOps PRs or staged manifests cannot easily see the precise difference between "manifest in PR", "manifest in main", and "live cluster state".
- **Primary use cases:**
  - Pre-merge review of GitOps PRs.
  - Post-incident comparison between snapshot and current state.
  - Promotion of manifests between environments with explicit diff evidence.
- **Proposed capability:** Three-way diff (`--base` vs `--target` vs server) and a `--live` flag to anchor against current cluster state.
- **Functional requirements:**
  - Output clearly distinguishes add/modify/delete/conflict.
  - Works for files, directories, and multi-doc YAML.
  - Stable, machine-readable output mode for CI consumption.
- **Non-functional requirements:**
  - Diff completes within bounded time for representative manifest sizes.
- **Business value:** Reduces accidental drift and increases reviewer confidence during change management.
- **Success metrics:** Adoption by GitOps PR templates; reduction in revert frequency.
- **Dependencies:** Existing manifest codec.
- **Spec references:** [`specs/HLD/dashctl-hld.md`](../../specs/HLD/dashctl-hld.md) §11.

---

### F13. `dashctl rollout` staged delivery

- **Category:** dashctl / Operations
- **Problem statement:** Large fleet updates need progressive delivery (canary, batches, automatic rollback) but the CLI today applies everything at once.
- **Primary use cases:**
  - Roll out a new ACL policy to 10% of ENIs first, then 50%, then 100%.
  - Pause on first detected regression and resume after fix.
  - Automatic rollback when a configurable health signal breaches threshold.
- **Proposed capability:** A `rollout` command that wraps `ApplyBatch` (F1) with progress, pause/resume, health gates, and rollback.
- **Functional requirements:**
  - Plans support percentage and explicit-batch strategies.
  - Health gates are pluggable: reconcile drift, custom Prometheus query, dashctl-supplied probe.
  - Rollback triggers and thresholds are configurable per plan.
- **Non-functional requirements:**
  - Rollout state survives CLI restarts; resumable from server-side saga state.
- **Business value:** Reduces blast radius of changes and brings progressive delivery norms to network operations.
- **Success metrics:** Reduction in incidents caused by configuration rollouts; adoption by CI pipelines.
- **Dependencies:** F1 (ApplyBatch), F10 (streaming).
- **Spec references:** [`specs/HLD/dashctl-hld.md`](../../specs/HLD/dashctl-hld.md) §7; [`specs/Impl-Plan/impl-plan-advanced.md`](../../specs/Impl-Plan/impl-plan-advanced.md) P2-M8.

---

### F14. dashctl plugin framework

- **Category:** dashctl / Extensibility
- **Problem statement:** Ecosystem extensions and team-local tools cannot be distributed without modifying the upstream CLI.
- **Primary use cases:**
  - Internal team CLIs that wrap policy checks specific to one org.
  - Vendor-supplied adapters (IPAM, ticketing, custom audit exporters).
  - Distribution of community plugins by name resolution.
- **Proposed capability:** kubectl-krew-style plugin discovery: any executable named `dashctl-<name>` on PATH appears as `dashctl <name>`.
- **Functional requirements:**
  - Plugin discovery is opt-in and documented.
  - Plugin invocation receives standard environment (current context, endpoint, token).
  - Help text aggregates plugin metadata.
- **Non-functional requirements:**
  - No mandatory runtime dependency for plugin authors beyond an executable contract.
- **Business value:** Enables platform extensibility and ecosystem growth without core changes.
- **Success metrics:** Number of distinct plugins discovered in surveys; documentation completeness.
- **Dependencies:** None.
- **Spec references:** [`specs/HLD/dashctl-hld.md`](../../specs/HLD/dashctl-hld.md) §17.

---

### F15. `dashctl simulate --file` batch dry-run

- **Category:** dashctl / Pre-flight safety
- **Problem statement:** Operators want to know whether an entire manifest will succeed (FK validation, capacity, capability matrix) before any state is written.
- **Primary use cases:**
  - CI gate that fails the build if any object would be rejected.
  - Pre-promotion preview when moving manifests between environments.
  - Capacity planning before tenant onboarding.
- **Proposed capability:** Run the entire manifest through `SimulateApply` in a throwaway namespace and emit a per-object verdict report.
- **Functional requirements:**
  - Report includes each object's verdict, reason, and dependency context.
  - Non-zero exit on any projected failure (configurable to warn-only).
  - Output formats include human-readable and machine-readable.
- **Non-functional requirements:**
  - Simulation does not leave residue in real namespaces.
- **Business value:** Catches mistakes before any state change and improves change confidence.
- **Success metrics:** Adoption in CI pipelines; reduction in failed real applies.
- **Dependencies:** `SimulateApply` RPC (implemented).
- **Spec references:** [`specs/HLD/dashctl-hld.md`](../../specs/HLD/dashctl-hld.md) §7.

---

### F16. NDJSON streaming output for dashctl

- **Category:** dashctl / Automation
- **Problem statement:** Large list responses are awkward to process in shell pipelines because JSON arrays must be buffered before parsing.
- **Primary use cases:**
  - `dashctl get eni | jq ...` over thousands of objects.
  - Stream-processing exports for backup/audit pipelines.
  - Memory-bounded automation in constrained environments.
- **Proposed capability:** Add `-o ndjson` for read/list commands so each object is emitted as a single JSON line.
- **Functional requirements:**
  - Stable schema per kind, identical fields to `-o json`.
  - Works with streaming and snapshot commands.
- **Non-functional requirements:**
  - Memory footprint flat regardless of response size.
- **Business value:** Improves CI/CD ergonomics and downstream automation patterns.
- **Success metrics:** Adoption in tutorial examples; user-reported memory-related issues in large lists trend to zero.
- **Dependencies:** None.
- **Spec references:** [`specs/HLD/dashctl-hld.md`](../../specs/HLD/dashctl-hld.md) §10.

---

## Section 3 — `dashw` (web console)

### F17. WebSocket-to-gRPC real-time bridge (Phase B)

- **Category:** dashw / Real-time
- **Problem statement:** dashw uses REST polling for live views, causing latency and unnecessary load. The HLD specifies a Phase B bridge that has not been built.
- **Primary use cases:**
  - DPU status changes appear within seconds, not on next poll.
  - Live audit log, counters, and event tail in views that need them.
  - Migration/HA progress views update without manual refresh.
- **Proposed capability:** BFF bridges dashd's gRPC server-streaming RPCs to browser-friendly WebSocket channels consumed by SPA hooks.
- **Functional requirements:**
  - Reconnect and replay semantics are deterministic and documented.
  - Backpressure surfaces clearly in the UI rather than silently dropping events.
  - Auth and rate limiting apply consistently to streamed channels.
- **Non-functional requirements:**
  - End-to-end event latency p95 ≤ defined SLO; CPU/memory impact bounded.
- **Business value:** Single biggest UX upgrade for the console; reduces dashd load created by polling.
- **Success metrics:** Polling traffic reduction; user-perceived responsiveness in usability tests.
- **Dependencies:** dashd Phase 2 streaming RPCs.
- **Spec references:** [`specs/HLD/dashw-web-hld.md`](../../specs/HLD/dashw-web-hld.md) §8; [`specs/Impl-Plan/dashw-web-impl-plan.md`](../../specs/Impl-Plan/dashw-web-impl-plan.md) Phase B.

---

### F18. HA Orchestration Theater

- **Category:** dashw / HA UX
- **Problem statement:** HA transitions are hard to reason about via tables and logs alone; operators need a focused visualization for confidence and incident response.
- **Primary use cases:**
  - Planned switchover with single-click trigger and live timeline.
  - Visualizing flow-sync progress and split-brain alerts.
  - Post-incident review of role transitions.
- **Proposed capability:** A dedicated view animating HA state transitions, flow-sync rings, and alert banners using existing HA RPCs.
- **Functional requirements:**
  - Planned vs unplanned transitions clearly differentiated.
  - Split-brain detection surfaces prominently with remediation guidance.
  - History/timeline replayable for postmortems.
- **Non-functional requirements:**
  - Frame-rate stays smooth under load typical of large fleets.
- **Business value:** Reduces HA operational risk and improves operator trust in HA features.
- **Success metrics:** Reduction in HA-related support tickets; operator satisfaction scores in usability tests.
- **Dependencies:** F17 (WebSocket bridge) for live progress; existing HA RPCs.
- **Spec references:** [`specs/HLD/dashw-web-vision.md`](../../specs/HLD/dashw-web-vision.md) §3.3; [`specs/Impl-Plan/dashw-web-impl-plan.md`](../../specs/Impl-Plan/dashw-web-impl-plan.md) D1.

---

### F19. Migration Control Center

- **Category:** dashw / Migration UX
- **Problem statement:** ENI live migration is a 10-phase state machine; operators currently parse text status to understand progress and blockers.
- **Primary use cases:**
  - Driving a planned migration with phase-by-phase progress and pause/rollback controls.
  - Diagnosing stuck migrations with per-phase timing and dependency view.
  - Bulk migration during DPU drain with aggregated progress.
- **Proposed capability:** Gantt-style migration cockpit with per-phase progress rails, flow-drain waterfall, and integrated rollback controls.
- **Functional requirements:**
  - Full migration lifecycle visible in one page; per-session and aggregate views.
  - One-click rollback with confirmation and audit context.
  - Failure reasons surfaced with link to relevant diagnostic actions.
- **Non-functional requirements:**
  - Responsive at the scale of typical operator drain operations.
- **Business value:** Reduces migration failure rate and operator cognitive load during long-running operations.
- **Success metrics:** Reduction in operator time per migration; lower rollback rate due to clearer guidance.
- **Dependencies:** F17 (WebSocket bridge), migration RPCs (implemented).
- **Spec references:** [`specs/HLD/dashw-web-vision.md`](../../specs/HLD/dashw-web-vision.md) §3.4; [`specs/Impl-Plan/dashw-web-impl-plan.md`](../../specs/Impl-Plan/dashw-web-impl-plan.md) D2.

---

### F20. Capacity Planner and What-If Simulator

- **Category:** dashw / Planning
- **Problem statement:** Capacity overruns are discovered after a failed apply rather than during planning.
- **Primary use cases:**
  - Plan tenant onboarding by previewing per-DPU and per-tenant headroom impact.
  - Compare alternative placements before committing.
  - Detect tipping points where additional ENIs require new DPUs.
- **Proposed capability:** Interactive what-if planner that computes projected impact from a proposed change set using existing capacity APIs.
- **Functional requirements:**
  - Input supports add/move/delete operations on ENIs and policies.
  - Output shows per-DPU, per-tenant headroom impact and any admission violations.
  - Plans can be exported as manifests for execution.
- **Non-functional requirements:**
  - Calculation completes in seconds for representative fleet sizes.
- **Business value:** Supports proactive capacity planning and avoids capacity-related incidents.
- **Success metrics:** Reduction in capacity-related failed applies; adoption by planning workflows.
- **Dependencies:** Existing capacity APIs.
- **Spec references:** [`specs/HLD/dashw-web-vision.md`](../../specs/HLD/dashw-web-vision.md) §3.6; [`specs/Impl-Plan/dashw-web-impl-plan.md`](../../specs/Impl-Plan/dashw-web-impl-plan.md) D3.

---

### F21. ACL Impact Analyzer

- **Category:** dashw / Policy quality
- **Problem statement:** Large ACL stacks accumulate dead, redundant, or risky rules that are hard to detect manually.
- **Primary use cases:**
  - Identify rules with zero hits over a configurable window.
  - Trace why a packet matched the wrong rule via `ExplainMatch`.
  - Prioritize cleanup of risky overlapping rules.
- **Proposed capability:** A visual ACL analyzer that combines counter data, dead-rule detection, and match-explanation waterfall.
- **Functional requirements:**
  - Heatmap and filter by ENI/tenant/window.
  - Suggested cleanups exportable as a manifest diff or PR.
  - Drill-down to per-rule explanations.
- **Non-functional requirements:**
  - Analyzer responsive at the scale of representative policy sets.
- **Business value:** Improves security quality and reduces operator effort in policy maintenance.
- **Success metrics:** Number of dead rules removed in early adopter deployments; reduction in misconfigured ACL incidents.
- **Dependencies:** Counter streaming (PE-3c done), `ExplainMatch` (PE-1 done).
- **Spec references:** [`specs/HLD/dashw-web-vision.md`](../../specs/HLD/dashw-web-vision.md) §3.8.

---

### F22. Policy Dependency Graph

- **Category:** dashw / Change safety
- **Problem statement:** Operators struggle to predict the blast radius of deleting or modifying a resource that has many dependents.
- **Primary use cases:**
  - Pre-delete preview showing dependents and severity.
  - Onboarding new operators by visualizing relationships.
  - Impact analysis during incident response.
- **Proposed capability:** Interactive force-directed graph of relationships (ENI ↔ VNet ↔ RoutePolicy ↔ ServiceTunnel ↔ ACLPolicy ↔ HaSet) with filter, search, and impact preview.
- **Functional requirements:**
  - Delete pre-check surfaces blocking dependencies; suggests safe order.
  - Graph supports filter by tenant, label, and resource kind.
  - Performance acceptable on large datasets via virtualization.
- **Non-functional requirements:**
  - Initial render under defined budget on representative fleets.
- **Business value:** Prevents accidental service impact and shortens operator onboarding.
- **Success metrics:** Reduction in failed deletes; faster operator ramp-up.
- **Dependencies:** Existing list APIs.
- **Spec references:** [`specs/HLD/dashw-web-vision.md`](../../specs/HLD/dashw-web-vision.md) §3.15.

---

### F23. Production hardening pack for dashw (A-PH)

- **Category:** dashw / Reliability + Security
- **Problem statement:** dashw is functionally complete but the 26-gate production-hardening sub-phase is unstarted, blocking enterprise deployment.
- **Primary use cases:**
  - Customer security review checklist (CSP, CSRF, SameSite cookies, input sanitization).
  - Reliability under partial-backend failure (BFF health-degraded states).
  - Accessibility compliance (WCAG 2.1 AA).
  - Frontend error reporting and BFF distributed tracing.
- **Proposed capability:** Deliver all 26 A-PH gates: security headers, request limits, sanitization, rate-limit UX feedback, BFF degraded mode, connection pooling, Lighthouse budget, a11y pass, error reporting, BFF tracing.
- **Functional requirements:**
  - Every gate has a passing test and is enforced in CI.
  - Operator documentation captures the security model.
- **Non-functional requirements:**
  - Performance budgets enforced via CI Lighthouse run.
- **Business value:** Removes the largest blocker to enterprise deployment of dashw.
- **Success metrics:** All 26 A-PH gates green; security scan results pass.
- **Dependencies:** None hard; benefits from F17.
- **Spec references:** [`specs/Impl-Plan/dashw-web-impl-plan.md`](../../specs/Impl-Plan/dashw-web-impl-plan.md) A-PH.

---

### F24. Multi-cluster context switcher in dashw

- **Category:** dashw / Productivity
- **Problem statement:** Each dashw instance targets one dashd cluster; ops teams managing several clusters must redeploy or run separate consoles.
- **Primary use cases:**
  - Switch between environments (dev/stage/prod) from one URL.
  - Compare same view across clusters during incident response.
  - Visual differentiation prevents accidental cross-cluster actions.
- **Proposed capability:** A cluster picker reading dashctl-style contexts; per-session selection with explicit visual indicators.
- **Functional requirements:**
  - Contexts persisted per user session; explicit confirmation on destructive operations.
  - BFF remains stateless; selection passed per request.
  - Visual cues (color band, name) prevent confusion.
- **Non-functional requirements:**
  - No additional dependencies; performance unchanged.
- **Business value:** Improves operator productivity and reduces operational error in multi-cluster environments.
- **Success metrics:** Reduction in operator time for multi-cluster workflows; zero cross-cluster mistakes in usability tests.
- **Dependencies:** F4 (consistent identity across clusters) is helpful but not blocking.
- **Spec references:** [`specs/HLD/dashw-web-hld.md`](../../specs/HLD/dashw-web-hld.md) §16 (currently non-goal flagged as post-v1).

---

## Section 4 — `dash-sim` and `dash-redis-adapter`

### F25. Simulator flow-table and fast/slow-path semantics

- **Category:** dash-sim / Fidelity
- **Problem statement:** The simulator covers object lifecycle and counter rollups, but not flow-table dynamics or fast/slow path distinction. This limits how realistically diagnostics and HA features can be tested without hardware.
- **Primary use cases:**
  - Diagnostic conformance tests that include flow aging and ownership.
  - Pre-hardware validation of advanced diagnostics.
  - Test environments for HA behavior tied to flow lifecycle.
- **Proposed capability:** Add a flow-table model with create/age/delete/sync-state, plus fast vs slow path classification consistent with real DPU behavior.
- **Functional requirements:**
  - Behaviors are deterministic and replayable.
  - Trace outputs identify the path classification correctly.
  - Tests pass on both sim and adapter via a conformance suite.
- **Non-functional requirements:**
  - Simulation overhead bounded and documented.
- **Business value:** Higher confidence in pre-production testing; less reliance on scarce hardware time.
- **Success metrics:** Conformance suite expansion; HA tests no longer need real hardware for routine validation.
- **Dependencies:** None hard; aligns with F5.
- **Spec references:** [`specs/Impl-Plan/impl-phases.md`](../../specs/Impl-Plan/impl-phases.md) PE-4; [`specs/LLD/dash-sim.md`](../../specs/LLD/dash-sim.md) §9.

---

### F26. Simulator HA flow-owner attribute

- **Category:** dash-sim / HA fidelity
- **Problem statement:** HA flow-owner attribute is not modeled in the simulator, so flow-drain progress metrics cannot be validated end-to-end without hardware.
- **Primary use cases:**
  - End-to-end test of HA switchover including flow-owner transition.
  - Validation of drain progress metrics in CI.
  - Reference behavior for adapter implementations.
- **Proposed capability:** Per-ENI `flow_owner` attribute tracked in sim HA state machine, with role-switch updates.
- **Functional requirements:**
  - Role switch atomically updates flow ownership for affected ENIs.
  - Failover tests pass deterministically.
- **Non-functional requirements:**
  - No regression in non-HA flows.
- **Business value:** Removes a critical sim-vs-hardware gap and enables automated HA testing.
- **Success metrics:** Automated HA validation runs in CI without hardware.
- **Dependencies:** F25 helpful for full realism.
- **Spec references:** [`specs/Impl-Plan/impl-phases.md`](../../specs/Impl-Plan/impl-phases.md) PE-4 sim parity.

---

### F27. Simulator counter anomaly injection

- **Category:** dash-sim / Test enablement
- **Problem statement:** Analytics and anomaly-detection features (e.g., F21) need realistic synthetic anomalies to validate, which the simulator cannot produce today.
- **Primary use cases:**
  - Inject spikes, drops, and stale counters for analyzer testing.
  - Reproducible test scenarios for alerting rules.
  - Demo data for product validation and stakeholder reviews.
- **Proposed capability:** Admin endpoint on the simulator to inject scripted counter anomalies with replay/reset controls.
- **Functional requirements:**
  - Scenario templates supported; events traceable and resettable.
  - Anomaly emissions are clearly tagged in counter streams.
- **Non-functional requirements:**
  - Injection does not interfere with non-test counters.
- **Business value:** Enables strong validation of observability features before any hardware is involved.
- **Success metrics:** Coverage of analyzer features measured via injected scenarios.
- **Dependencies:** Counter streaming end-to-end (done).
- **Spec references:** [`specs/HLD/dashw-web-vision.md`](../../specs/HLD/dashw-web-vision.md) §3.7 (consumer side).

---

## Section 5 — Cross-cutting

### F28. GitOps reconciliation controller

- **Category:** Platform integration
- **Problem statement:** Many platform teams standardize on Git-driven change workflows; today they must wrap dashctl manually to integrate.
- **Primary use cases:**
  - Continuous reconciliation of a Git repo of DashCenter manifests into a cluster.
  - Drift detection between cluster state and Git source of truth.
  - PR-driven promotion across environments.
- **Proposed capability:** A standalone controller that polls a Git repo, applies manifests, and exposes sync/drift status.
- **Functional requirements:**
  - Configurable safety policies (manual gate, dry-run, schedule).
  - Drift and sync status exported via metrics and API.
  - Compatible with the same manifest format used by dashctl.
- **Non-functional requirements:**
  - Stable under intermittent Git outages with clear backoff.
- **Business value:** Brings Git-driven governance and traceability to DashCenter changes.
- **Success metrics:** Adoption by platform teams; reduction in out-of-band changes.
- **Dependencies:** F1 (ApplyBatch) preferred for atomic reconciliation.
- **Spec references:** [`specs/HLD/dashctl-hld.md`](../../specs/HLD/dashctl-hld.md) §2 (downstream concern).

---

### F29. Terraform provider for DashCenter

- **Category:** Platform integration
- **Problem statement:** IaC users prefer Terraform-native workflows; CLI-only access is a friction point that blocks adoption.
- **Primary use cases:**
  - Manage tenants/VNets/ENIs/policies as Terraform resources alongside other infrastructure.
  - Import existing DashCenter state into Terraform.
  - Plan-then-apply workflows familiar to platform engineers.
- **Proposed capability:** A Terraform provider exposing first-class resources for each DashCenter kind with optimistic concurrency via generation-based CAS.
- **Functional requirements:**
  - CRUD with `terraform import` support.
  - Documented schemas, examples, and migration guidance.
  - Provider authenticates via tokens, mTLS, and (with F4) OIDC.
- **Non-functional requirements:**
  - Provider releases align with REST API version and stability guarantees.
- **Business value:** Significantly broadens adoption among platform-engineering teams.
- **Success metrics:** Provider downloads and registry adoption; reduced custom-wrapper code in observed pipelines.
- **Dependencies:** Stable REST API contracts.
- **Spec references:** [`docs/dashd-features/features.md`](./features.md) §5.

---

### F30. Prometheus alerting rules and Grafana dashboard pack

- **Category:** Observability
- **Problem statement:** Operators get a metrics endpoint but no shipped guidance for actionable alerts and dashboards, leaving teams to invent their own.
- **Primary use cases:**
  - First-day alerting on leader-loss, capacity headroom, drift backlog, reconcile lag.
  - Per-tenant and per-resource drill-downs in Grafana.
  - Reference posture for SRE on-call rotations.
- **Proposed capability:** A curated, version-controlled alert ruleset and Grafana dashboard bundle aligned with the metrics exposed at `/admin/metrics`.
- **Functional requirements:**
  - Alerts come with runbook links and severity guidance.
  - Dashboards include fleet overview, per-tenant, per-resource, and reconciliation panels.
  - Bundle ships with installation docs for common stacks (Prometheus, Alertmanager, Grafana).
- **Non-functional requirements:**
  - Rules and dashboards versioned in repo with CI lint.
- **Business value:** Reduces operator setup time and standardizes monitoring posture across deployments.
- **Success metrics:** Adoption across reference deployments; reduction in escalations missing actionable alerts.
- **Dependencies:** Existing `/admin/metrics` endpoint.
- **Spec references:** [`specs/HLD/dashd-hld.md`](../../specs/HLD/dashd-hld.md) §13; [`docs/dashd-features/features.md`](./features.md) §11.

---

## Section 6 — Quality and Reliability

### F31. Chaos engineering harness with programmable failure injection

- **Category:** Cross-cutting / Reliability engineering
- **Problem statement:** dashd's resilience (leader failover, partition tolerance, DPU isolation) is claimed by design but not continuously proven. Production-grade software must validate these properties on every release through automated fault injection.
- **Primary use cases:**
  - CI runs that kill the current leader during a long-running `ApplyBatch` and assert convergence.
  - Network-partition tests between dashd and etcd, between dashd and DPUs, between dashw BFF and dashd.
  - Slow-disk and high-latency injections to verify timeouts and back-off.
  - Random crash recovery to validate state durability and idempotency.
- **Proposed capability:** A first-party chaos harness (separate binary + Go SDK) that defines and executes named failure scenarios against a running stack and asserts steady-state properties.
- **Functional requirements:**
  - Scenario library covers: leader kill, etcd partition, DPU partition, slow disk, packet loss, clock skew, OOM, restart loop.
  - Steady-state probes evaluate pre/post invariants (object counts, reconcile lag, leader liveness).
  - Scenarios integrate with the existing `04-ha-fleet` and `07-full-experiment` topologies.
  - Results emit JUnit XML for CI dashboards.
- **Non-functional requirements:**
  - Harness adds no runtime dependency on dashd itself.
  - Per-scenario runtime budget documented and bounded.
- **Business value:** Turns "we believe it's resilient" into "we prove it on every PR". Major adoption signal for SRE-conscious customers.
- **Success metrics:** Number of resilience invariants asserted in CI; regressions caught before release.
- **Dependencies:** None.
- **Spec references:** [`specs/HLD/next_gen_dpu_fleet_management_platform.md`](../../specs/HLD/next_gen_dpu_fleet_management_platform.md) §14, §23 (Failure Mode Analysis).

---

### F32. Disaster recovery: backup, restore, point-in-time recovery

- **Category:** dashd / Data safety
- **Problem statement:** etcd snapshots are the only recovery artifact today, and restore drills are not part of release validation. Operators have no documented PITR (point-in-time recovery) story.
- **Primary use cases:**
  - Restore a cluster after accidental mass-delete or bad GitOps reconciliation.
  - Move state between staging and prod for forensic investigation.
  - Cross-region cold standby recovery in a regional outage.
- **Proposed capability:** A backup/restore tool with periodic snapshots, integrity verification, encrypted storage targets, and a documented PITR procedure tied to audit-log replay.
- **Functional requirements:**
  - Scheduled snapshots with configurable retention and target (S3-compatible, local disk).
  - Integrity check (signature + checksum) on every snapshot.
  - Restore command validates snapshot, target cluster, and version compatibility before proceeding.
  - PITR: replay audit log from a snapshot forward to a chosen timestamp.
- **Non-functional requirements:**
  - Restore time for documented reference dataset within RTO budget.
  - Backup encryption keys integrate with KMS (see F53).
- **Business value:** Removes a top-3 production blocker for risk-averse customers and is a prerequisite for any compliance discussion.
- **Success metrics:** Successful restore drill on representative dataset; RTO/RPO numbers published.
- **Dependencies:** Existing audit log; F53 (KMS) optional.
- **Spec references:** [`specs/HLD/dashd-hld.md`](../../specs/HLD/dashd-hld.md) §11.

---

### F33. Zero-downtime dashd upgrade (rolling + canary)

- **Category:** dashd / Lifecycle
- **Problem statement:** dashd upgrades today require a maintenance window because there is no canary or rolling-upgrade contract for an HA cluster.
- **Primary use cases:**
  - Roll out a new dashd minor version one replica at a time without service interruption.
  - Canary a new version against a small percentage of traffic and auto-promote on health.
  - Skew tolerance: leader on N-1 while followers on N during the rollout window.
- **Proposed capability:** Documented N/N-1 compatibility contract, explicit rolling-upgrade procedure, and tooling for canary promotion driven by health signals.
- **Functional requirements:**
  - Version skew between any two replicas is supported within an N+1 window.
  - Health gates use existing leader/health endpoints plus reconcile lag.
  - Rollback path is one command and tested on every release.
- **Non-functional requirements:**
  - No reconciliation regression during upgrade window in soak tests.
- **Business value:** Higher deploy frequency, smaller change-failure rate, and improved customer SLA.
- **Success metrics:** Mean time between deploys; percentage of deploys with zero error budget consumption.
- **Dependencies:** F34 versioned schema.
- **Spec references:** [`specs/HLD/dashd-hld.md`](../../specs/HLD/dashd-hld.md) §14.

---

### F34. Versioned API schema with formal deprecation policy

- **Category:** Cross-cutting / API governance
- **Problem statement:** The proto API has evolved organically. Production customers need a formal compatibility and deprecation contract so they can plan multi-quarter upgrades.
- **Primary use cases:**
  - Customer pinning a specific minor and upgrading on a known cadence.
  - Deprecation warnings surfaced in dashctl and dashw for fields scheduled for removal.
  - Long-term-support (LTS) version offering for risk-averse adopters.
- **Proposed capability:** Documented SemVer-style compatibility, a deprecation taxonomy (warn → error → remove), and tooling that emits deprecation warnings in clients.
- **Functional requirements:**
  - Each proto field/RPC carries a stability annotation (alpha/beta/stable/deprecated).
  - dashd surfaces deprecation warnings via response headers and audit log.
  - CI lints prevent removing or repurposing a stable field without going through the deprecation window.
- **Non-functional requirements:**
  - No runtime overhead beyond a small header per response.
- **Business value:** Predictable upgrades for customers; lower support burden.
- **Success metrics:** Number of fields formally graduated; zero accidental breaking changes after policy adoption.
- **Dependencies:** None.
- **Spec references:** [`specs/HLD/dashctl-hld.md`](../../specs/HLD/dashctl-hld.md) §15; [`specs/HLD/dashd-hld.md`](../../specs/HLD/dashd-hld.md) §2.

---

### F35. Cluster-wide state consistency audit (Merkle-tree based)

- **Category:** dashd / Correctness
- **Problem statement:** With many writes flowing through HA replicas and N DPUs, operators have no proof that desired state matches observed state across the entire fleet at a point in time.
- **Primary use cases:**
  - Pre/post-upgrade integrity audit.
  - Compliance evidence that policy and configuration are coherent fleet-wide.
  - Forensic investigation after a suspected partial-apply or drift event.
- **Proposed capability:** A Merkle-tree-style audit that hashes namespaces, kinds, and objects and compares to per-DPU observed-state hashes; produces a diff report.
- **Functional requirements:**
  - On-demand audit returns a signed report with the consistency status per object.
  - Audit is incremental and cheap (sub-tree hashing).
  - Integrates with audit log so anyone can verify a past report later.
- **Non-functional requirements:**
  - Cost bounded; does not block reconciliation.
- **Business value:** Strong compliance and trust artifact, often required by enterprise procurement.
- **Success metrics:** Audit runs nightly in CI; consistency status displayed in dashw.
- **Dependencies:** Existing inventory and observed-state cache.
- **Spec references:** [`specs/HLD/next_gen_dpu_fleet_management_platform.md`](../../specs/HLD/next_gen_dpu_fleet_management_platform.md) §18.

---

### F36. Dead-letter queue and poison-message handling for reconciliation

- **Category:** dashd / Operational resilience
- **Problem statement:** A single repeatedly-failing object can monopolise a worker and degrade throughput for an entire DPU. Today the error budget is per-DPU but there is no DLQ for individual objects.
- **Primary use cases:**
  - Quarantine a single ENI whose backing DPU keeps returning a permanent error.
  - Operator runbook: inspect, remediate, requeue from DLQ.
  - Alert when DLQ exceeds threshold.
- **Proposed capability:** Per-object DLQ with quarantine policy, reason capture, manual requeue command, and metrics.
- **Functional requirements:**
  - Quarantine after configurable consecutive permanent failures.
  - DLQ entries include cause, last attempt, and original spec hash.
  - Manual `dashctl quarantine list / inspect / requeue / drop` commands.
- **Non-functional requirements:**
  - DLQ size bounded; oldest entries evicted with audit.
- **Business value:** Prevents a single bad object from degrading fleet throughput and gives operators a clear path to fix-it-or-skip-it.
- **Success metrics:** Reduced reconcile lag during poison events in soak tests.
- **Dependencies:** Existing reconciler.
- **Spec references:** [`specs/HLD/dashd-hld.md`](../../specs/HLD/dashd-hld.md) §9.

---

### F37. Per-tenant resource quotas with hard/soft limits and fair scheduling

- **Category:** dashd / Multi-tenancy
- **Problem statement:** Multi-tenant clusters today have no defence against one tenant exhausting fleet capacity or starving others under load.
- **Primary use cases:**
  - Enforce max ENIs per tenant.
  - Throttle reconciliation for a noisy tenant so others get fair share.
  - Bill on quotas (see F60).
- **Proposed capability:** Tenant-scoped quotas (objects, mutation rate, capacity), soft warnings before hard limits, and a fair-share scheduler in the reconciler.
- **Functional requirements:**
  - Quotas configurable at namespace level; visible in dashw.
  - Quota breaches return `RESOURCE_EXHAUSTED` with the offending limit named.
  - Fair-share scheduler weights tenants; documented behavior under contention.
- **Non-functional requirements:**
  - Scheduler does not regress single-tenant throughput.
- **Business value:** Essential for serving multiple internal teams or external customers from the same cluster.
- **Success metrics:** Demonstrated isolation under stress test; zero noisy-neighbor incidents in soak.
- **Dependencies:** Existing capacity admission; F4 OIDC for per-tenant identity.
- **Spec references:** [`specs/HLD/next_gen_dpu_fleet_management_platform.md`](../../specs/HLD/next_gen_dpu_fleet_management_platform.md) §16.

---

### F38. Deep health checks (write probe, dependency probe, leader probe)

- **Category:** dashd / Observability
- **Problem statement:** `/healthz` returns "process up". That does not detect "process up but cannot write to etcd" or "process up but leader has lost lease" — common failure modes in real deployments.
- **Primary use cases:**
  - Kubernetes readiness gating that excludes degraded replicas from traffic.
  - Operator dashboards that surface "soft down" states.
  - Load-balancer health checks aligned with actual user-facing capability.
- **Proposed capability:** Layered health endpoints: `/livez` (process), `/readyz` (serving), `/healthz/deep` (write + dependency probes).
- **Functional requirements:**
  - Deep probe writes a synthetic keep-alive object and reads it back.
  - Dependency probes report etcd, DPU agents, and admission webhooks individually.
  - Endpoint returns structured JSON with per-component status.
- **Non-functional requirements:**
  - Deep probe runs at most every N seconds (configurable); does not amplify load.
- **Business value:** Operations teams can route traffic away from degraded replicas long before they hard-fail.
- **Success metrics:** Reduction in user-visible errors during partial-failure events.
- **Dependencies:** None.
- **Spec references:** [`specs/HLD/dashd-hld.md`](../../specs/HLD/dashd-hld.md) §13.

---

## Section 7 — Scalability and Performance

### F39. Sharded reconciliation workers (per-DPU shard groups)

- **Category:** dashd / Scale
- **Problem statement:** The reconciler today is one worker per DPU. At 10,000 DPUs that becomes a goroutine and scheduling bottleneck; a shard-group model is needed to scale linearly.
- **Primary use cases:**
  - 10k-DPU clusters where each shard owns 100–500 DPUs.
  - Horizontal scaling by adding dashd replicas that adopt shards.
  - Failure isolation between shards.
- **Proposed capability:** Shard-group abstraction with consistent hashing across replicas, lease-based ownership, and graceful shard transfer.
- **Functional requirements:**
  - Shard count configurable; default sized for 1k DPU clusters.
  - Shard ownership change is graceful and audited.
  - Per-shard metrics for lag, backlog, and error rates.
- **Non-functional requirements:**
  - No reconciliation regression at small (100 DPU) scale.
- **Business value:** Validated linear scale story to 10k+ DPUs, removing the largest architectural objection from hyperscale prospects.
- **Success metrics:** Sustained 10k-DPU benchmark with steady-state lag SLO.
- **Dependencies:** Existing leader-election; capacity admission.
- **Spec references:** [`specs/HLD/next_gen_dpu_fleet_management_platform.md`](../../specs/HLD/next_gen_dpu_fleet_management_platform.md) §15.

---

### F40. Watch bookmarks, resume tokens, and server-side pagination

- **Category:** Cross-cutting / API
- **Problem statement:** Long-running watches reconnect from scratch and re-deliver state; list APIs return everything. Both are unacceptable at fleet scale.
- **Primary use cases:**
  - Reconnect a watch and continue without re-sending unchanged objects.
  - Page through 100k ENIs without OOMing the client.
  - dashw and dashctl resume streams across BFF restarts.
- **Proposed capability:** Kubernetes-style resourceVersion + bookmarks for watches; cursor-based pagination on list APIs.
- **Functional requirements:**
  - `?continue=<token>` and `?limit=N` on list APIs.
  - Watch responses include bookmarks; resume omits already-delivered objects.
  - Compatible REST + gRPC behavior; documented semantics.
- **Non-functional requirements:**
  - Token compact; opaque to clients; signed so it cannot be forged.
- **Business value:** Foundation for large-fleet operation and a major performance win for both UI and CLI.
- **Success metrics:** Memory footprint reduction in dashw under large lists; watch reconnect storms shrink in chaos tests.
- **Dependencies:** Store cursor support; F10 for streaming.
- **Spec references:** [`specs/HLD/dashd-hld.md`](../../specs/HLD/dashd-hld.md) §7.

---

### F41. ETag / If-None-Match read optimization

- **Category:** dashd / API performance
- **Problem statement:** Many client reads are no-op (object unchanged) but still transfer the full payload. HTTP caching primitives close this loop.
- **Primary use cases:**
  - dashw view refresh that should be a 304 when nothing changed.
  - CI pipelines that poll for drift and need cheap diffs.
  - Mobile/low-bandwidth operator tools.
- **Proposed capability:** Add `ETag` response and `If-None-Match` request support on all read endpoints; map to existing generation/version.
- **Functional requirements:**
  - 304 Not Modified returned when ETag matches.
  - dashctl and dashw clients send `If-None-Match` automatically.
  - Cache documentation and integration guidance for third-party clients.
- **Non-functional requirements:**
  - Negligible server CPU overhead.
- **Business value:** Lower bandwidth, reduced dashd CPU, better UX in slow links.
- **Success metrics:** Measured byte and CPU reduction in dashw under steady-state browsing.
- **Dependencies:** Generation-based CAS (already in place).
- **Spec references:** [`docs/dashd-features/features.md`](./features.md) §5.

---

### F42. Compressed gRPC and HTTP streaming

- **Category:** dashd / Transport
- **Problem statement:** Large list responses and high-volume event streams are uncompressed, wasting bandwidth and CPU at fleet scale.
- **Primary use cases:**
  - Cross-region replication of audit/event streams.
  - Large list responses to UIs and CLIs.
  - Compressed counter rollups.
- **Proposed capability:** Negotiated compression (gzip, snappy, zstd) on both REST and gRPC paths.
- **Functional requirements:**
  - Server advertises supported encodings; clients negotiate.
  - Per-endpoint policy lets operators disable compression for sensitive flows.
- **Non-functional requirements:**
  - CPU cost measured and documented; no regression for small payloads.
- **Business value:** Lower bandwidth bills, faster cross-region replication, better mobile/edge experience.
- **Success metrics:** Measured payload size reduction; latency unchanged or improved.
- **Dependencies:** None.
- **Spec references:** [`specs/HLD/dashd-hld.md`](../../specs/HLD/dashd-hld.md) §6.

---

### F43. Read-replica routing in HA (followers serve reads)

- **Category:** dashd / Scale
- **Problem statement:** The leader serves all reads today, becoming the bottleneck on read-heavy clusters even though followers hold the same state.
- **Primary use cases:**
  - dashw read-heavy traffic served by followers.
  - dashctl read commands routed to nearest replica.
  - Bursty dashboards that should not affect write throughput.
- **Proposed capability:** Followers expose a read endpoint with documented staleness bounds; clients opt in via header or context.
- **Functional requirements:**
  - Staleness reported in response so callers can decide.
  - Per-endpoint policy for read-only vs leader-only.
  - Smart-routing in dashctl/dashw clients.
- **Non-functional requirements:**
  - Reads from followers within configurable staleness SLO.
- **Business value:** Big throughput unlock without architectural change; better latency in multi-region deployments.
- **Success metrics:** Read QPS scales with follower count; leader CPU drops correspondingly.
- **Dependencies:** Existing leader election.
- **Spec references:** [`specs/HLD/dashd-hld.md`](../../specs/HLD/dashd-hld.md) §11.

---

### F44. Coalesced write windows

- **Category:** dashd / Throughput
- **Problem statement:** Many small writes from different clients hit the store individually, when they could be coalesced into batched writes for higher throughput.
- **Primary use cases:**
  - GitOps reconciler producing many small updates.
  - Bulk tenant onboarding with hundreds of objects.
  - Migration cleanup tasks.
- **Proposed capability:** Server-side write coalescing window that bundles concurrent writes targeting the same shard into one storage transaction.
- **Functional requirements:**
  - Configurable max-window and max-batch-size to bound latency.
  - Per-batch durability semantics documented.
- **Non-functional requirements:**
  - Single-write latency not regressed beyond a defined budget.
- **Business value:** Higher sustained write throughput without changing client behavior.
- **Success metrics:** Measured write QPS lift; storage CPU reduction.
- **Dependencies:** Store contract; F40 helpful for read pipeline.
- **Spec references:** [`specs/HLD/dashd-hld.md`](../../specs/HLD/dashd-hld.md) §9.

---

## Section 8 — UI and UX

### F45. Command palette (Cmd+K) and keyboard-shortcut system

- **Category:** dashw / UX
- **Problem statement:** Power operators navigate slowly through menus; modern tools (VS Code, Linear, GitHub) set the expectation of a global command palette.
- **Primary use cases:**
  - Jump to any resource by name in one keystroke.
  - Execute commands (apply, delete, drain, simulate) by typing the verb.
  - Discover and learn shortcuts incrementally.
- **Proposed capability:** Cmd+K palette indexed across resources, commands, and views; complete keyboard navigation across the console.
- **Functional requirements:**
  - Fuzzy search across resources, kinds, and command verbs.
  - Customizable shortcuts per user.
  - Documented shortcut map with a cheatsheet overlay.
- **Non-functional requirements:**
  - Palette open latency under defined budget regardless of fleet size.
- **Business value:** Power-user productivity and modern UX expectations met.
- **Success metrics:** Adoption rate measured by analytics; reduced clicks per workflow in usability tests.
- **Dependencies:** F23 hardening pack (a11y interactions).
- **Spec references:** [`specs/HLD/dashw-web-vision.md`](../../specs/HLD/dashw-web-vision.md) §6.

---

### F46. Customizable dashboards with drag-and-drop widgets

- **Category:** dashw / UX
- **Problem statement:** The fixed Dashboard view does not match every team's priorities. Operators want to assemble their own at-a-glance panels.
- **Primary use cases:**
  - NOC team building a wall-mounted dashboard.
  - SRE assembling capacity + reconcile + audit widgets.
  - Tenant-specific dashboards.
- **Proposed capability:** A widget library (capacity gauge, drift list, HA status, counter chart, audit tail, event log) composable via drag-and-drop with per-user persistence.
- **Functional requirements:**
  - Layout persists per user; sharable across teams.
  - Widgets are responsive and respect a11y.
  - Read-only embed mode for status walls.
- **Non-functional requirements:**
  - Rendering performance budget on representative dashboards.
- **Business value:** Higher operator engagement and clearer team-specific operational focus.
- **Success metrics:** Number of saved dashboards per active user; NPS uptick.
- **Dependencies:** F17 streaming preferred.
- **Spec references:** [`specs/HLD/dashw-web-vision.md`](../../specs/HLD/dashw-web-vision.md) §4.

---

### F47. Saved views, bookmarks, and per-user workspace

- **Category:** dashw / UX
- **Problem statement:** Operators repeatedly re-construct the same filtered views; teams cannot share standard "go-to" investigation links.
- **Primary use cases:**
  - Save a filter like "all ENIs in tenant=blue with drift" and share with team.
  - Browser-tab bookmarks that survive cluster restarts.
  - Per-user recent activity for quick return.
- **Proposed capability:** Saved-view system with shareable URLs, recent-activity history, and explicit favourites.
- **Functional requirements:**
  - URLs encode the full state so links are shareable.
  - Saved views are scoped (private, team, public).
  - Recent and favourites surfaced in sidebar.
- **Non-functional requirements:**
  - State storage minimal; no server-side per-user DB unless needed.
- **Business value:** Significant operator productivity boost; encourages knowledge sharing.
- **Success metrics:** Number of saved views per active user; reduction in time-to-first-action.
- **Dependencies:** F23 hardening pack.
- **Spec references:** [`specs/HLD/dashw-web-vision.md`](../../specs/HLD/dashw-web-vision.md) §6.

---

### F48. Time-machine: point-in-time state and audit replay

- **Category:** dashw / UX + Compliance
- **Problem statement:** Postmortems and audits need to answer "what did the cluster look like at time T?" Today operators must hand-correlate audit logs.
- **Primary use cases:**
  - Postmortem: show fleet state immediately before and after the incident.
  - Compliance: produce historical configuration snapshots on demand.
  - "Diff this view between yesterday and today" investigations.
- **Proposed capability:** Time-machine view that replays audit log and snapshots to render the cluster state at any chosen timestamp.
- **Functional requirements:**
  - Timeline scrubber that updates the current view live.
  - Diff mode between two timestamps.
  - Export of point-in-time view for audit evidence.
- **Non-functional requirements:**
  - Reconstruction performance bounded for representative audit volumes.
- **Business value:** Massive analyst and auditor productivity gain; differentiating capability for compliance-heavy customers.
- **Success metrics:** Time-to-conclusion for postmortems; audit demand metrics.
- **Dependencies:** F32 backup/restore; F35 consistency audit; existing audit log.
- **Spec references:** [`specs/HLD/next_gen_dpu_fleet_management_platform.md`](../../specs/HLD/next_gen_dpu_fleet_management_platform.md) §18.

---

### F49. Bulk actions: multi-select and batch operations

- **Category:** dashw / UX
- **Problem statement:** Operators today must apply changes object-by-object in the UI for routine bulk tasks (cordon a group of DPUs, label many ENIs, force-reconcile a tenant).
- **Primary use cases:**
  - Select 20 DPUs and cordon them.
  - Apply a label to all ENIs matching a filter.
  - Force-reconcile a tenant's full set.
- **Proposed capability:** Multi-select in lists and tables, plus a batch-action menu wired to existing APIs (and to `ApplyBatch` once F1 lands).
- **Functional requirements:**
  - Selection persists across pagination.
  - Bulk-action preview shows exactly what will change.
  - Audit log captures the bulk action and its components.
- **Non-functional requirements:**
  - UI remains responsive on large selections via virtualization.
- **Business value:** Dramatic reduction in operator time for routine bulk operations.
- **Success metrics:** Time-to-complete for representative tasks reduces by an order of magnitude.
- **Dependencies:** F1 (ApplyBatch) recommended.
- **Spec references:** [`specs/HLD/dashw-web-vision.md`](../../specs/HLD/dashw-web-vision.md) §6.

---

### F50. Inline diff visualization for apply preview

- **Category:** dashw / UX
- **Problem statement:** Today's "apply" UI shows the post-state but not the diff vs current state, so operators cannot quickly validate they are doing exactly what they intend.
- **Primary use cases:**
  - Preview a YAML upload with side-by-side diff before commit.
  - Visualize cascading effects (changes that ripple through dependent resources).
  - Show explicit "no-op" when nothing changes.
- **Proposed capability:** Side-by-side and unified diff modes in the apply flow, with optional dependency-aware impact view.
- **Functional requirements:**
  - Diff computed client-side from the current server state.
  - Supports YAML, JSON, and form-based input.
  - Highlights field-level changes and any policy violations.
- **Non-functional requirements:**
  - Diff performance bounded for large manifests.
- **Business value:** Reduces change-related incidents and improves operator confidence.
- **Success metrics:** Reduction in unintended changes; user feedback during usability testing.
- **Dependencies:** Existing get/list APIs.
- **Spec references:** [`specs/HLD/dashw-web-vision.md`](../../specs/HLD/dashw-web-vision.md) §6.

---

## Section 9 — Industry-Demand and Ecosystem

### F51. Policy-as-Code engine (OPA / Rego integration)

- **Category:** dashd / Governance
- **Problem statement:** Customers expect declarative policy enforcement (naming, ownership, security baselines, tenant isolation rules) without writing custom Go code or managing admission webhooks.
- **Primary use cases:**
  - Enforce naming and label conventions.
  - Block production resources missing an owner label.
  - Enforce per-tenant network-policy baselines.
- **Proposed capability:** Embedded OPA/Rego evaluator at the admission gate with bundle loading, policy authoring tooling, and structured deny output.
- **Functional requirements:**
  - Rego policies versioned and reloadable.
  - Policy decisions logged with rule and evidence.
  - Local dry-run via `dashctl policy test`.
- **Non-functional requirements:**
  - Evaluation latency budget enforced per request.
- **Business value:** Aligns with the de-facto industry standard for policy-as-code; major checkbox for regulated industries.
- **Success metrics:** Number of customer policies deployed; reduction in custom-webhook adoption.
- **Dependencies:** F7 admission webhook architecture.
- **Spec references:** [`specs/HLD/next_gen_dpu_fleet_management_platform.md`](../../specs/HLD/next_gen_dpu_fleet_management_platform.md) §17.

---

### F52. Compliance reporting pack (SOC2 / FedRAMP / PCI-DSS / ISO 27001)

- **Category:** Cross-cutting / Compliance
- **Problem statement:** Compliance teams ask repeatable questions ("show me all admin actions in the last 90 days", "prove ENIs in tenant T are in approved VNets"). Operators piece this together manually today.
- **Primary use cases:**
  - Auditor report bundles delivered with one click.
  - Continuous evidence collection (control daily proof).
  - Mapping to common compliance controls (SOC2 CC, NIST 800-53).
- **Proposed capability:** A compliance reporting pack with curated report definitions, evidence collectors, and scheduled exports.
- **Functional requirements:**
  - Reports versioned; outputs reproducible.
  - Export targets include S3, SFTP, SIEM (F58).
  - Each control mapped to specific dashd data sources.
- **Non-functional requirements:**
  - Report generation bounded in time; large clusters supported.
- **Business value:** Major sales accelerator for regulated customers; reduces audit prep effort by weeks.
- **Success metrics:** Time-to-audit-prep metrics from reference customers.
- **Dependencies:** F35 consistency audit; existing audit log.
- **Spec references:** [`specs/HLD/next_gen_dpu_fleet_management_platform.md`](../../specs/HLD/next_gen_dpu_fleet_management_platform.md) §17.

---

### F53. KMS / HashiCorp Vault integration for secrets

- **Category:** Cross-cutting / Security
- **Problem statement:** Secrets (auth tokens, TLS keys, backup encryption keys) live in files today. Enterprise customers require centrally managed, audited secret stores.
- **Primary use cases:**
  - Rotate dashd serving cert from Vault without restart.
  - Encrypt backups using customer-managed keys.
  - Per-tenant secret namespaces.
- **Proposed capability:** Pluggable secret-store interface with implementations for HashiCorp Vault, AWS KMS, Azure Key Vault, and GCP KMS.
- **Functional requirements:**
  - Hot reload of secrets without restart.
  - Key rotation supported with overlap window.
  - Audited access to secrets.
- **Non-functional requirements:**
  - Secret access latency bounded; cached locally with TTL.
- **Business value:** Removes a common security-review blocker for enterprise sales.
- **Success metrics:** Successful integration with at least two major KMS systems.
- **Dependencies:** F4 OIDC for identity.
- **Spec references:** [`specs/HLD/next_gen_dpu_fleet_management_platform.md`](../../specs/HLD/next_gen_dpu_fleet_management_platform.md) §17.

---

### F54. SBOM and signed container images (sigstore / cosign)

- **Category:** Cross-cutting / Supply chain
- **Problem statement:** Modern enterprise procurement requires Software Bill of Materials (SBOM) and image signatures (cosign/Notary) to assess and verify the dependency chain.
- **Primary use cases:**
  - Customer security scan that requires CycloneDX or SPDX SBOM.
  - Admission controllers that verify image signature.
  - CVE response: list all customers running impacted versions.
- **Proposed capability:** CI pipeline generates SBOMs (SPDX + CycloneDX), signs images with cosign, publishes attestations.
- **Functional requirements:**
  - SBOM published for every release artifact.
  - Image signatures verifiable with public keys.
  - Documented verification procedure for customers.
- **Non-functional requirements:**
  - Build time impact bounded.
- **Business value:** Required by many enterprise procurement checklists today; absence is a deal blocker.
- **Success metrics:** Coverage of all release artifacts; documented customer verifications.
- **Dependencies:** Existing CI.
- **Spec references:** [`specs/HLD/next_gen_dpu_fleet_management_platform.md`](../../specs/HLD/next_gen_dpu_fleet_management_platform.md) §17.

---

### F55. Air-gapped deployment and offline update bundles

- **Category:** Cross-cutting / Deployment
- **Problem statement:** Government, defense, and certain telco customers operate fully air-gapped clusters. Today there is no documented offline-install or update story.
- **Primary use cases:**
  - Initial install into an air-gapped data center.
  - Quarterly updates delivered via signed media.
  - Vulnerability hot-patch in an isolated environment.
- **Proposed capability:** A versioned offline bundle containing images, manifests, signatures, and install scripts; documented installation procedure.
- **Functional requirements:**
  - Bundles are self-contained and signed.
  - Install procedure validates signatures before applying.
  - Update flow includes rollback artifact.
- **Non-functional requirements:**
  - Bundle size bounded and documented per version.
- **Business value:** Unlocks government/defense markets that are otherwise inaccessible.
- **Success metrics:** Successful install in reference air-gapped environment; documented adopters.
- **Dependencies:** F54 signed images; F32 backups.
- **Spec references:** [`specs/HLD/next_gen_dpu_fleet_management_platform.md`](../../specs/HLD/next_gen_dpu_fleet_management_platform.md) §21.

---

### F56. ARM64 first-class support

- **Category:** Cross-cutting / Platform
- **Problem statement:** DPUs (BlueField, Octeon) and modern cloud nodes (Graviton, Ampere) are ARM64. The platform must be a first-class ARM64 citizen, not an afterthought.
- **Primary use cases:**
  - Run controllerless dashd directly on the DPU control CPU.
  - Cloud deployments on Graviton instances for cost savings.
  - Edge appliances on ARM64 hardware.
- **Proposed capability:** Officially supported ARM64 binaries and container images, with CI matrix coverage equivalent to x86_64.
- **Functional requirements:**
  - Multi-arch container manifests for every release.
  - ARM64 in CI matrix for dashd, dashctl, dashw, dash-sim.
  - Documented performance characteristics on representative ARM64 platforms.
- **Non-functional requirements:**
  - ARM64 performance within documented envelope of x86.
- **Business value:** Aligns with DPU hardware reality and modern cost-optimized cloud deployments.
- **Success metrics:** ARM64 download share; ARM64 issues at parity with x86.
- **Dependencies:** None.
- **Spec references:** [`specs/HLD/next_gen_dpu_fleet_management_platform.md`](../../specs/HLD/next_gen_dpu_fleet_management_platform.md) §21.

---

### F57. Webhook subscription framework for external integrations

- **Category:** dashd / Integration
- **Problem statement:** External systems (ticketing, ChatOps, CMDB, ITSM) need to react to DashCenter events but today must implement gNMI or proto consumers.
- **Primary use cases:**
  - Post a Slack/Teams message when a DPU degrades.
  - Open a ServiceNow ticket on persistent drift.
  - Push reconcile failures into PagerDuty.
- **Proposed capability:** A subscription API where external endpoints register filtered webhook destinations; dashd delivers events with retries and dead-letter handling.
- **Functional requirements:**
  - Filters on kind, namespace, severity, labels.
  - Delivery with signed payloads, retries, and DLQ.
  - Subscription management via API and dashw UI.
- **Non-functional requirements:**
  - Backpressure does not impact core event pipeline.
- **Business value:** Removes integration friction; meets common enterprise event-driven workflows.
- **Success metrics:** Number of active subscriptions in reference deployments.
- **Dependencies:** Existing event pipeline; F36 DLQ patterns.
- **Spec references:** [`specs/HLD/dashd-hld.md`](../../specs/HLD/dashd-hld.md) §15.

---

### F58. SIEM integration for audit log shipping

- **Category:** Cross-cutting / Security operations
- **Problem statement:** Enterprise SOCs require all security-relevant logs in their SIEM (Splunk, Elastic, Datadog, QRadar). Today audit log access is API-only.
- **Primary use cases:**
  - Stream audit log to Splunk HEC.
  - Stream to Elastic via Logstash/OpenTelemetry.
  - Forward to a generic syslog target.
- **Proposed capability:** Pluggable audit-shipper component with first-party support for common SIEMs and an OTEL-based generic target.
- **Functional requirements:**
  - At-least-once delivery; tamper-evident (signed) payloads.
  - Buffering and backoff under SIEM outage.
  - Mapping documentation for common detection rules.
- **Non-functional requirements:**
  - Shipper does not block audit writes; degraded mode documented.
- **Business value:** Required for SOC integration in most regulated industries.
- **Success metrics:** Documented integrations; reference detection content shared.
- **Dependencies:** Audit log (in place).
- **Spec references:** [`docs/dashd-features/features.md`](./features.md) §2.

---

### F59. Approval workflows and change windows

- **Category:** Cross-cutting / Change management
- **Problem statement:** Many enterprises require multi-stage approvals and scheduled change windows for production network changes; DashCenter has no native support today.
- **Primary use cases:**
  - "Two-person rule" for sensitive changes.
  - Block changes during business-critical windows.
  - Scheduled apply at a future maintenance window.
- **Proposed capability:** Approval policy + change-window framework integrated into the apply flow; configurable per namespace and per kind.
- **Functional requirements:**
  - Pending-approval state for changes; approval auditable.
  - Change windows configurable with timezone awareness.
  - Out-of-window or unapproved changes blocked with clear messages.
- **Non-functional requirements:**
  - Approval state durable; survives failover.
- **Business value:** Aligns with enterprise change-management norms (ITIL, SOX); enables low-risk production usage.
- **Success metrics:** Adoption of approval policies; reduction in unauthorized changes.
- **Dependencies:** F4 OIDC; audit log.
- **Spec references:** [`specs/HLD/next_gen_dpu_fleet_management_platform.md`](../../specs/HLD/next_gen_dpu_fleet_management_platform.md) §17.

---

### F60. Cost allocation and chargeback per tenant

- **Category:** Cross-cutting / FinOps
- **Problem statement:** Multi-tenant operators must allocate platform cost back to tenants but lack the usage data and reporting to do so.
- **Primary use cases:**
  - Monthly chargeback report per tenant.
  - Internal cost transparency for engineering teams.
  - Forecast cost based on growth trends.
- **Proposed capability:** Per-tenant usage metering (ENIs, mutations, capacity used, audit volume) and exportable cost-allocation reports.
- **Functional requirements:**
  - Metering aligned with quotas (F37) for consistency.
  - Reports exportable as CSV, JSON, and via API.
  - Tagging/labelling for further breakdown.
- **Non-functional requirements:**
  - Metering overhead bounded; pre-aggregated where possible.
- **Business value:** Important for internal platform teams charging back to product groups, and for SaaS providers reselling DashCenter.
- **Success metrics:** Adoption by reference platform teams; reduction in custom-billing pipelines.
- **Dependencies:** F37 quotas; F6 counter aggregation.
- **Spec references:** [`specs/HLD/next_gen_dpu_fleet_management_platform.md`](../../specs/HLD/next_gen_dpu_fleet_management_platform.md) §16.

---

## Appendix — Priority view

The priority order below is reproduced from
[`docs/roadmap.md`](../roadmap.md) §6.6 so requirements reviewers can
see the same intended sequencing without leaving this document.

| Priority | Feature | Why first |
|---|---|---|
| 1 | F11 dashctl `debug` suite | Zero new deps; immediate support value |
| 2 | F10 dashctl gRPC (Phase 2) | Unblocks F17 streaming |
| 3 | F17 WebSocket bridge | Largest dashw UX upgrade; APIs ready |
| 4 | F23 dashw production hardening | 26 reliability/security gates |
| 5 | F1 ApplyBatch + saga | Enables F13 rollouts |
| 6 | F13 dashctl rollout | Direct fleet-ops value on top of F1 + F10 |
| 7 | F18 + F19 HA Theater + Migration UI | Server-side complete; UI work |
| 8 | F6 + F21 counter aggregation + ACL analyzer | Counter infra ready |
| 9 | F22 policy dependency graph | High operator safety; no new APIs |
| 10 | F4 OIDC | Required before production outside trusted LAN |

### Phase-2 priorities (industry hardening — F31–F60)

| Priority | Feature | Why now |
|---|---|---|
| 11 | F38 deep health checks | Smallest investment, immediate ops value |
| 12 | F40 watch bookmarks + pagination | Unblocks scale and reduces UI memory |
| 13 | F32 backup / restore / PITR | Top customer ask; compliance prerequisite |
| 14 | F33 zero-downtime upgrades | Direct effect on deploy frequency |
| 15 | F35 consistency audit | Trust and compliance signal |
| 16 | F39 sharded reconciliation | Validates 10k-DPU scale story |
| 17 | F54 SBOM + signed images | Common procurement blocker; low effort |
| 18 | F58 SIEM integration | High-leverage security checkbox |
| 19 | F51 OPA policy-as-code | Aligns with industry standard for governance |
| 20 | F37 tenant quotas + fair share | Multi-tenancy readiness |
| 21 | F45 + F47 command palette + saved views | Visible UX win for power operators |
| 22 | F48 time-machine | Differentiating capability for compliance / forensics |
| 23 | F53 KMS / Vault | Common enterprise security requirement |
| 24 | F59 approval workflows | Enables regulated production use |
| 25 | F60 cost allocation | FinOps maturity; enables internal chargeback |
| 26 | F31 chaos harness | Proves resilience claims continuously |
| 27 | F52 compliance pack | Sales accelerator for regulated industries |
| 28 | F55 air-gapped bundles | Unlocks government markets |
| 29 | F56 ARM64 support | DPU-native + cloud-cost alignment |
| 30 | F36 dead-letter queue | Operational polish for fleet ops |

