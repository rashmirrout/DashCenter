# DashCenter — Feature Requirements Specification

> **Status:** Draft v1 (2026-06-17)
> **Purpose:** Capture 30 production-grade feature requests across dashd,
> dashctl, dashw, dash-sim, and cross-cutting platform capabilities, written
> as formal feature requirements with problem, use cases, value, and success
> criteria.
> **Companion:** [`../docs/roadmap.md`](../docs/roadmap.md) §6 lists the
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
- **Spec references:** [`specs/Impl-Plan/impl-plan-advanced.md`](../specs/Impl-Plan/impl-plan-advanced.md) P2-M12 saga coordinator.

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
- **Spec references:** [`specs/HLD/dashd-hld.md`](../specs/HLD/dashd-hld.md) §15 future work; [`specs/Impl-Plan/impl-plan-advanced.md`](../specs/Impl-Plan/impl-plan-advanced.md) P2-M12.

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
- **Spec references:** [`specs/HLD/dashd-hld.md`](../specs/HLD/dashd-hld.md) §10 controllerless mode; [`specs/Impl-Plan/impl-phases.md`](../specs/Impl-Plan/impl-phases.md) PF.

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
- **Spec references:** [`specs/Impl-Plan/impl-plan-advanced.md`](../specs/Impl-Plan/impl-plan-advanced.md) P2-M9; [`docs/dashd-features/features.md`](../docs/dashd-features/features.md) §2.

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
- **Spec references:** [`specs/Impl-Plan/impl-phases.md`](../specs/Impl-Plan/impl-phases.md) PE-4.

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
- **Spec references:** [`docs/dashd-features/features.md`](../docs/dashd-features/features.md) §10A; [`specs/Impl-Plan/impl-phases.md`](../specs/Impl-Plan/impl-phases.md) PE-3c.

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
- **Spec references:** [`specs/HLD/dashd-hld.md`](../specs/HLD/dashd-hld.md) §6.

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
- **Spec references:** [`specs/HLD/dashd-hld.md`](../specs/HLD/dashd-hld.md) §15.

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
- **Spec references:** [`specs/HLD/dashd-hld.md`](../specs/HLD/dashd-hld.md) §9.

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
- **Spec references:** [`specs/Impl-Plan/dashctl-impl-phases.md`](../specs/Impl-Plan/dashctl-impl-phases.md) Phase 2.

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
- **Spec references:** [`specs/LLD/dashctl-debug.md`](../specs/LLD/dashctl-debug.md).

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
- **Spec references:** [`specs/HLD/dashctl-hld.md`](../specs/HLD/dashctl-hld.md) §11.

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
- **Spec references:** [`specs/HLD/dashctl-hld.md`](../specs/HLD/dashctl-hld.md) §7; [`specs/Impl-Plan/impl-plan-advanced.md`](../specs/Impl-Plan/impl-plan-advanced.md) P2-M8.

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
- **Spec references:** [`specs/HLD/dashctl-hld.md`](../specs/HLD/dashctl-hld.md) §17.

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
- **Spec references:** [`specs/HLD/dashctl-hld.md`](../specs/HLD/dashctl-hld.md) §7.

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
- **Spec references:** [`specs/HLD/dashctl-hld.md`](../specs/HLD/dashctl-hld.md) §10.

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
- **Spec references:** [`specs/HLD/dashw-web-hld.md`](../specs/HLD/dashw-web-hld.md) §8; [`specs/Impl-Plan/dashw-web-impl-plan.md`](../specs/Impl-Plan/dashw-web-impl-plan.md) Phase B.

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
- **Spec references:** [`specs/HLD/dashw-web-vision.md`](../specs/HLD/dashw-web-vision.md) §3.3; [`specs/Impl-Plan/dashw-web-impl-plan.md`](../specs/Impl-Plan/dashw-web-impl-plan.md) D1.

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
- **Spec references:** [`specs/HLD/dashw-web-vision.md`](../specs/HLD/dashw-web-vision.md) §3.4; [`specs/Impl-Plan/dashw-web-impl-plan.md`](../specs/Impl-Plan/dashw-web-impl-plan.md) D2.

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
- **Spec references:** [`specs/HLD/dashw-web-vision.md`](../specs/HLD/dashw-web-vision.md) §3.6; [`specs/Impl-Plan/dashw-web-impl-plan.md`](../specs/Impl-Plan/dashw-web-impl-plan.md) D3.

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
- **Spec references:** [`specs/HLD/dashw-web-vision.md`](../specs/HLD/dashw-web-vision.md) §3.8.

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
- **Spec references:** [`specs/HLD/dashw-web-vision.md`](../specs/HLD/dashw-web-vision.md) §3.15.

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
- **Spec references:** [`specs/Impl-Plan/dashw-web-impl-plan.md`](../specs/Impl-Plan/dashw-web-impl-plan.md) A-PH.

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
- **Spec references:** [`specs/HLD/dashw-web-hld.md`](../specs/HLD/dashw-web-hld.md) §16 (currently non-goal flagged as post-v1).

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
- **Spec references:** [`specs/Impl-Plan/impl-phases.md`](../specs/Impl-Plan/impl-phases.md) PE-4; [`specs/LLD/dash-sim.md`](../specs/LLD/dash-sim.md) §9.

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
- **Spec references:** [`specs/Impl-Plan/impl-phases.md`](../specs/Impl-Plan/impl-phases.md) PE-4 sim parity.

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
- **Spec references:** [`specs/HLD/dashw-web-vision.md`](../specs/HLD/dashw-web-vision.md) §3.7 (consumer side).

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
- **Spec references:** [`specs/HLD/dashctl-hld.md`](../specs/HLD/dashctl-hld.md) §2 (downstream concern).

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
- **Spec references:** [`docs/dashd-features/features.md`](../docs/dashd-features/features.md) §5.

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
- **Spec references:** [`specs/HLD/dashd-hld.md`](../specs/HLD/dashd-hld.md) §13; [`docs/dashd-features/features.md`](../docs/dashd-features/features.md) §11.

---

## Appendix — Priority view

The priority order below is reproduced from
[`docs/roadmap.md`](../docs/roadmap.md) §6.6 so requirements reviewers can
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
