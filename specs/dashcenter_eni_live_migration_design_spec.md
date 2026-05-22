# DashCenter ENI Live Migration Design Specification

## Overview

This document defines an industry-grade design for ENI live migration in DashCenter, the centralized visibility, troubleshooting, and fleet operations platform for DASH-compliant devices. It treats ENI migration as a controlled transfer of network identity, policy realization, flow ownership, and traffic steering between a source device and a target device, not as a simple object copy operation.[cite:15][cite:127][cite:130][cite:167]

The design assumes a DASH environment in which network intent is expressed through DASH objects, realized through appliance or DPU programming, and tracked through operational state in layers such as APP_DB, STATE_DB, and SAI or vendor runtime state. That layered model is exactly why ENI migration must be designed as a cross-layer transaction with explicit readiness, ownership, observability, cutover, and rollback semantics.[cite:15][cite:50][cite:181]

## What live migration means

Live migration is the controlled movement of an active endpoint from one execution location to another while keeping service disruption within a bounded operational objective. In a compute context, that often means transferring VM or device state with minimal downtime. In a DASH context, ENI live migration means transferring the active networking home of an ENI from one DASH-capable device to another while preserving correctness of identity, policy, forwarding, and service continuity.[cite:129][cite:168][cite:167]

The key distinction is that an ENI is not merely a configuration object. It is the anchor for network identity and forwarding behavior, and it is usually attached to a web of dependent objects such as VNETs, ACL groups, routes, QoS or metering policies, service insertion chains, encapsulation or mapping rules, and runtime flow state. Moving an ENI therefore means moving or recreating its full dependency closure and then shifting operational responsibility for traffic in a way that avoids ambiguity.[cite:15][cite:181]

## Why ENI migration matters

ENI live migration matters because modern DPU and smart-switch environments increasingly require maintenance, draining, rebalance, failover, and scale-out operations without tearing down workloads. The DASH high-availability direction publicly points to multi-appliance operation, overprovisioning, and flow-splitting ideas, which indicates that mobility of endpoint responsibility is not an edge case but a design expectation for resilient deployments.[cite:167][cite:127][cite:130]

In practice, operators need ENI migration for at least six recurring situations:

- Planned maintenance on a source DPU or appliance.[cite:167]
- Capacity rebalance across a fleet of DASH devices.[cite:50][cite:167]
- Hardware fault avoidance before a complete device failure.[cite:167]
- Software upgrade or controlled restart of a DPU data plane.[cite:15][cite:50]
- AZ, rack, or failure-domain reshaping in large environments.[cite:167]
- High-availability handoff where an alternate device must become authoritative for the same endpoint semantics.[cite:127][cite:130][cite:167]

## What an ENI is associated with

In operational terms, an ENI is associated with more than its identifier. It binds together endpoint identity, tenant or VNET membership, classification rules, routing and mapping context, policy evaluation points, and the state used to enforce or observe traffic behavior. DASH material consistently presents a structured object model whose value lies in that linkage across layers and services.[cite:15][cite:181]

A usable migration design therefore needs to reason about the ENI as the root of a dependency graph. That graph normally contains at least the following classes of attachment.

### Identity and tenancy

- ENI identifier and metadata.
- Tenant or network namespace context.
- VNET membership and VNI or equivalent virtual-network binding.[cite:15][cite:181]

### Policy attachments

- Ingress and egress ACL groups.
- Rule ordering and stage placement.
- Metering, rate limiting, and QoS policies.
- Security or service-chain references where present.[cite:15][cite:181]

### Forwarding dependencies

- Route groups or route entries used after classification.
- Mapping relationships, encapsulation parameters, or tunnel context.
- Appliance insertion or service path objects when traffic must traverse chained functions.[cite:15][cite:167][cite:181]

### Runtime and health dependencies

- Programmed state in APP_DB and STATE_DB.
- SAI or vendor runtime realization status.
- Counters, packet/byte statistics, and drops.
- Flow ownership state and flow inventory where supported.[cite:127][cite:130][cite:181]

### External steering dependencies

- Upstream selectors or VIP logic.
- TOR or fabric steering decision that determines whether new traffic lands on source or target.
- Registration state in any external controller or allocator that influences traffic placement.[cite:167]

## Why object copy is not enough

A naive design would copy the ENI object and its references to the target, then delete the source. That approach is operationally unsafe because it ignores the time dimension of traffic. During migration, packets continue to arrive, new flows are created, existing flows may be long-lived, and stateful processing may be split across multiple logical stages. If both devices believe they own the same traffic without an explicit ownership contract, the result can be duplication, asymmetry, stale path state, or hard blackholing.[cite:127][cite:130][cite:129]

The DASH and SAI public signals around HA strengthen this conclusion. The existence of work on an ENI HA operational flow-owner attribute implies that real HA behavior requires explicit declaration of ownership beyond mere object presence. The high-availability material also points to traffic splitting and overprovisioning, which again implies that traffic landing and traffic ownership are not automatically identical concepts.[cite:127][cite:130][cite:167]

## Design goals

The ENI live migration feature in DashCenter should satisfy the following goals.

- Move an ENI between devices with bounded loss and bounded ambiguity.[cite:127][cite:130]
- Preserve policy correctness before, during, and after cutover.[cite:15][cite:181]
- Support single-device, peer-pair, and fleet-scale operation through one control surface.[cite:50]
- Provide full operator visibility into readiness, dependencies, flow ownership, cutover state, and rollback state.[cite:127][cite:130]
- Degrade safely when advanced state transfer is unavailable by falling back to drain-based migration.[cite:129][cite:167]
- Produce deterministic evidence for debugging and RCA.[cite:50][cite:181]

## Non-goals

The initial design does not assume perfect state transfer for every stateful offload on every vendor implementation. It also does not assume that all upstream steering control is inside the DPU itself. Where platform support is partial, the design must prefer explicit visibility and safe rollback over pretending to provide universal seamlessness.[cite:129][cite:167][cite:181]

## Core concept: migration as a state machine

The migration must be implemented as a long-running, resumable state machine rather than as a single imperative API call. This is the only structure that allows strong guardrails, observability, pause and resume semantics, and deterministic rollback. Live migration in adjacent domains such as SR-IOV and VM mobility is also treated as a staged process for exactly this reason.[cite:129][cite:168]

The canonical state machine is shown below.

| Phase | Purpose | Must succeed before next phase |
|---|---|---|
| Admission | Validate source, target, and policy eligibility | Capability parity, health, scale, dependency resolution |
| Snapshot | Freeze a migration-consistent logical view | Source state version and dependency graph captured |
| Prepare | Create target shadow state | All target objects accepted and realized to standby state |
| Sync | Keep target current while source remains authoritative | Sync lag within policy threshold |
| Ready | Confirm dual-presence readiness | New-flow owner and standby roles explicit |
| Cutover | Shift new flows and steering | Traffic starts landing on target under guardrails |
| Drain or Transfer | Age out or rehome existing source-owned flows | Residual source activity below threshold |
| Commit | Make target sole authority | Rollback checkpoint retained until grace expires |
| Finalize | Deprogram source and close session | Safe cleanup complete |
| Rollback | Revert to source authority when required | Prior checkpoint restored |

## Architectural model in DashCenter

DashCenter should provide ENI live migration as a first-class service, not a best-effort script. The service is composed of the following internal roles.

### Migration planner

The planner builds a complete dependency graph rooted at the ENI, resolves the transitive closure of required objects, checks source-target capability parity, and emits a migration plan artifact. This is the analysis-heavy stage that determines whether migration is legal and which policy choices are available.[cite:15][cite:50][cite:181]

### Migration orchestrator

The orchestrator executes the state machine. It drives preparation, synchronization, cutover, drain, commit, and rollback. It must be idempotent, checkpointed, and generation-aware so that repeated requests do not corrupt state.[cite:129][cite:168]

### Flow ownership controller

This component decides which device owns new flows, which owns existing flows, and when ownership can change. The public SAI HA flow-owner work suggests that this concept is essential for real deployments and should be modeled explicitly in DashCenter.[cite:127][cite:130]

### Steering adapter

This component interfaces with the mechanism that actually determines where packets land. Depending on deployment, that could be an upstream VIP selector, TOR path preference, route/mapping change, or another traffic-steering control point.[cite:167]

### State synchronizer

This component keeps target shadow state aligned with source state while source remains authoritative. In V1, synchronization may be object and metadata oriented; in later versions, it may include stateful flow context and service state where supported.[cite:129][cite:127]

### Evidence and observability pipeline

Every phase produces findings, timestamps, diffs, health markers, and flow snapshots. These should be queryable live and exportable as a support bundle.[cite:50][cite:181]

## Data model

DashCenter should model migration with explicit objects so that the operation becomes inspectable, auditable, and automatable.

### MigrationPlan

A `MigrationPlan` is the immutable analytical artifact that describes what is intended.

Suggested fields:

| Field | Meaning |
|---|---|
| `plan_id` | Stable identifier for the plan |
| `eni_id` | ENI being migrated |
| `source_device` | Current authoritative host |
| `target_device` | Intended next authoritative host |
| `dependency_graph_ref` | Frozen graph snapshot used to derive actions |
| `policy_ref` | Cutover, drain, and rollback policy |
| `required_capabilities` | Features that must exist on both ends |
| `readiness_report_ref` | Validation evidence |
| `created_at` | Plan creation time |
| `created_by` | Principal initiating the plan |

### MigrationSession

A `MigrationSession` is the live execution record for a plan.

Suggested fields:

| Field | Meaning |
|---|---|
| `session_id` | Stable identifier for execution instance |
| `plan_id` | Plan being executed |
| `phase` | Current state-machine phase |
| `phase_generation` | Monotonic generation number |
| `source_owner_state` | Ownership role of source |
| `target_owner_state` | Ownership role of target |
| `sync_lag_ms` | Freshness lag between source and target |
| `new_flow_split` | Fraction or policy for new-flow landing |
| `residual_flow_count` | Remaining source-owned active flows |
| `rollback_checkpoint_ref` | Checkpoint for reversion |
| `status` | Running, blocked, aborted, committed, finalized |

### DependencyGraph

A `DependencyGraph` is a directed graph rooted at the ENI that captures required objects and external dependencies. It must preserve object kind, ID, version, source of truth, realization state, and edge semantics such as `binds_to`, `requires`, `programs_into`, and `steers_via`.[cite:15][cite:181]

### FlowOwnershipView

A `FlowOwnershipView` is a time-bounded operational snapshot that groups source-owned, target-owned, transitioning, unknown, and orphaned flows. This object is crucial for operator trust because it turns migration from a black box into a measurable activity.[cite:127][cite:130]

### MigrationReadinessReport

A `MigrationReadinessReport` summarizes parity checks, scale headroom, unresolved dependencies, telemetry health, expected cutover risk, and steering viability. It should be the required gate between planning and execution.[cite:50][cite:167]

## Dependency graph design

The dependency graph is the heart of the design. For a human operator, it answers the question, “What exactly must move with this ENI?” For the system, it determines the legal order of preparation and cleanup.

A representative graph shape is shown below.

```text
ENI eni-100
├── VNET blue-prod
│   ├── VNI 5001
│   └── RouteGroup rg-west
│       ├── Route 10.2.2.0/24
│       └── Route 10.2.3.0/24
├── ACLGroup acl-prod-in
│   ├── Rule allow-https
│   └── Rule deny-any
├── ACLGroup acl-prod-out
├── Meter meter-gold
├── ServiceChain svc-firewall-west
├── Mapping vnet-blue-to-west
├── RuntimeState source: realized
├── RuntimeState target: standby
└── SteeringDependency vip-selector-west
```

This graph must support three operator workflows:

- Visual inspection in CLI and UI.
- Export into a migration package for audit or offline review.
- Replay into a validator or simulator before actual cutover.

## Export, import, and apply model

A serious migration design needs portable artifacts. The operator should be able to export a plan, inspect it, sign off on it, import it in another control plane context, and apply it with policy overrides.

### Export

`dashctl migration export <plan>` should emit a signed archive or directory containing:

- `plan.yaml` with metadata and policy references.
- `graph.json` with the dependency graph.
- `source-snapshot.json` with consistent source-state snapshot.
- `readiness-report.json` with validation results.
- `actions.yaml` with ordered orchestration actions.
- `constraints.yaml` with required invariants and abort conditions.

### Import

`dashctl migration import <bundle>` should validate schema version, signatures, environment compatibility, and target inventory references before admitting the plan into DashCenter.

### Apply

`dashctl migration apply <plan>` should never mean “blindly start.” It should mean “execute the next legal phase under approved policy” and it should require explicit confirmation or automation token policy for cutover phases.

## Example plan file

A plan file should be readable by humans and deterministic for machines.

```yaml
apiVersion: dashcenter.io/v1alpha1
kind: MigrationPlan
metadata:
  name: eni-100-dpu-a-to-dpu-b
spec:
  eni: eni-100
  sourceDevice: dpu-a
  targetDevice: dpu-b
  strategy:
    mode: new-flows-first-drain
    maxSyncLagMs: 250
    residualFlowThreshold: 10
    drainTimeoutSeconds: 900
    rollbackWindowSeconds: 1800
  steering:
    type: vip-selector
    objectRef: vip-selector-west
  checks:
    requireCapabilityParity: true
    requireAclParity: true
    requireRouteParity: true
    requireTelemetryHealthy: true
    requireFlowOwnershipSupported: true
  rollback:
    enabled: true
    triggerOn:
      - target_drop_spike
      - source_target_asymmetry
      - sync_lag_exceeded
      - unresolved_unknown_flows
```

This form is useful because it makes strategy, thresholds, and abort triggers explicit. It also makes the migration policy reviewable before execution.

## Strategy modes

DashCenter should support explicit strategy selection.

| Strategy | Meaning | Best use |
|---|---|---|
| `new-flows-first-drain` | New flows switch to target, old flows drain on source | Recommended V1 default |
| `full-rehome` | Existing active flows are transferred where supported | Advanced environments with state transfer |
| `maintenance-fast-failover` | Fast ownership change with reduced continuity guarantee | Emergency maintenance or source risk |
| `canary-split` | Controlled partial steering to validate target before full cutover | Large or sensitive deployments |

The existence of DASH HA flow-splitting ideas makes canary and weighted strategies reasonable future extensions, but they should not replace the need for a clear authoritative owner for new flows.[cite:167][cite:127][cite:130]

## Readiness validation

Readiness validation must be exhaustive because migration failures become far more expensive after cutover than before it.

### Capability parity checks

- DASH object support parity.
- ACL stage support parity.
- Route and mapping capability parity.
- Metering and service-chain capability parity.
- Flow ownership capability parity.
- Observability parity sufficient to judge migration health.[cite:15][cite:50][cite:127]

### Resource headroom checks

- Target object table capacity.
- Route and ACL scale headroom.
- Memory and session/state capacity.
- CPU or control-plane pressure if relevant.
- Telemetry pipeline health.[cite:50][cite:181]

### Dependency closure checks

- Every transitive object exists or can be created on target.
- Every external steering dependency is reachable and mutable.
- Every policy reference resolves to a compatible target-side object.[cite:15][cite:167]

### Runtime health checks

- Source currently healthy enough to act as authoritative owner during sync.
- Target healthy enough to admit shadow state and become active.
- No severe unresolved drops for the ENI before migration begins.
- Time synchronization and telemetry freshness within threshold.

## Flow ownership model

The flow ownership model is the most important design element after dependency closure. The public SAI work around ENI HA flow-owner attribute indicates that ownership must be represented operationally and should not be inferred from object existence alone.[cite:127][cite:130]

The model should distinguish:

- **Config presence**: object exists on source or target.
- **Standby readiness**: target can accept ownership but is not yet authoritative.
- **New-flow ownership**: the device that should receive and own new flows.
- **Existing-flow ownership**: the device currently responsible for established flows.
- **Unknown flow state**: traffic observed without clear attribution, which should be treated as a risk condition.

In a V1 drain-based design, the default ownership transition is:

1. Source owns all flows.
2. Target receives shadow config and becomes standby-ready.
3. Cutover switches new-flow ownership to target.
4. Existing flows remain owned by source until they age out or hit policy threshold.
5. Final commit transitions sole ownership to target.

This is operationally safe because it minimizes ambiguity and avoids pretending that all active state can be transferred perfectly.

## Traffic steering model

A migration only succeeds when traffic lands on the intended device. That makes traffic steering a first-class problem, not a side effect. DASH HA material referring to VIPs, overprovisioning, and flow splitting suggests that steering may be managed above the DPU data plane, so DashCenter must abstract steering as an external dependency with explicit adapter support.[cite:167]

Steering adapters should support:

- Current steering owner discovery.
- Planned new-flow steering change.
- Weighted or canary steering where supported.
- Revert or rollback.
- Generation stamping so operators can correlate traffic behavior with steering decisions.

## Synchronization model

Before cutover, the target must be kept current with source intent and relevant runtime metadata. The synchronizer should run until commit completes.

### V1 synchronization scope

- ENI object version and admin state.
- All transitive dependency versions.
- Counters snapshot and time markers.
- Flow summary inventory, not necessarily per-flow state transfer.
- Readiness and findings deltas.

### V2 synchronization scope

- Active per-flow ownership data.
- Service-chain state where export/import exists.
- Session or offload state for supported vendors.[cite:129]

## Cutover sequence

The cutover sequence must be policy-driven and visible to operators.

Recommended order:

1. Confirm last successful sync within allowed lag.
2. Freeze plan inputs and increment session generation.
3. Change steering policy so new traffic lands on target.
4. Change new-flow ownership declaration to target.
5. Validate that target begins receiving expected new traffic.
6. Monitor source residual flow count, retransmits, drops, and asymmetry.
7. If thresholds are violated, trigger automated rollback.

This order avoids a dangerous gap in which ownership changes without traffic steering or vice versa.

## Drain and transfer phase

After cutover, DashCenter enters the drain or transfer phase.

### Drain-based migration

Drain-based migration means source continues serving existing flows while target serves new ones. The migration session remains open until residual source-owned flows reach the threshold or timeout. This is the most realistic V1 strategy because it does not require universal state transfer support.[cite:127][cite:130][cite:129]

### Full rehome migration

Where platform support exists, DashCenter can transfer active-flow state and rehome ownership to target more aggressively. This requires vendor or platform support for state export/import and stronger correctness validation. The SR-IOV live migration domain shows why this is materially harder and why platform support matters.[cite:129][cite:168]

## Rollback design

Rollback must be designed before commit, not after failure. Every migration session must retain a rollback checkpoint until the rollback window expires.

Rollback triggers should include:

- Target drop spike over threshold.
- New-flow admission failure on target.
- Sync lag beyond maximum policy.
- Source-target asymmetry detection.
- Unknown or orphaned flows above threshold.
- External steering mismatch.

Rollback should revert the following in order:

1. Restore steering so new traffic returns to source.
2. Restore new-flow ownership to source.
3. Keep target state available until rollback validation completes.
4. Reconcile source health and traffic recovery.
5. Mark the session aborted with preserved evidence.

## Failure scenarios and caveats

A credible design document must state its caveats clearly.

### Partial target realization

If the target accepts the object graph but fails to realize some subset of objects, the migration must not proceed to cutover. Partial realization is one of the highest-risk conditions because it creates false confidence from successful configuration admission.[cite:15][cite:181]

### Long-lived flows

Long-lived flows may prevent fast completion of drain-based migration. The design must support policy choices such as extended drain, operator-approved termination, or later introduction of stateful rehome capabilities.[cite:129]

### Stateful services

Any service-chain or offload that keeps significant per-flow state increases migration complexity. V1 should classify unsupported stateful dependencies clearly and either block migration or require an explicit degraded-continuity policy.[cite:129][cite:181]

### Telemetry blind spots

If DashCenter cannot determine flow ownership, target health, steering generation, or residual-flow progress, it should treat the migration as unsafe. In other words, observability is part of correctness, not just convenience.[cite:127][cite:130]

### Dual-presence ambiguity

During intermediate states, the ENI may exist on both source and target. That is acceptable only when the ownership model is explicit and measurable. Dual presence without explicit ownership is a design bug.[cite:127][cite:130]

## CLI design

DashCenter should expose migration through `dashctl` in a way that is operationally literate and review-friendly.

### Planning and validation

```bash
dashctl migration plan eni-100 --from dpu-a --to dpu-b

dashctl migration validate plan-342

dashctl migration graph plan-342 --view tree
```

### Export and import

```bash
dashctl migration export plan-342 --output bundle.tar.gz

dashctl migration import bundle.tar.gz
```

### Execution

```bash
dashctl migration prepare plan-342

dashctl migration sync session-991

dashctl migration cutover session-991 --new-flows-only

dashctl migration drain session-991

dashctl migration commit session-991
```

### Safety and rollback

```bash
dashctl migration rollback session-991

dashctl migration abort session-991
```

### Visibility

```bash
dashctl migration status session-991

dashctl migration flows session-991

dashctl migration explain session-991

dashctl migration bundle session-991
```

## CLI visualization model

The CLI should not return opaque blobs. It should visualize migration in multiple terminal-native forms.

### Readiness scorecard

`dashctl migration validate` should show a scorecard like this:

```text
Migration Readiness: 92/100

Capability parity      PASS
Dependency closure     PASS
Target headroom        PASS
Telemetry freshness    PASS
Flow ownership support PASS
Steering control       WARN  external selector latency high
Stateful service risk  WARN  firewall chain lacks state export
```

### Dependency tree

`dashctl migration graph` should render tree or graph mode.

```text
eni-100
├─ vnet/blue-prod
├─ route-group/rg-west
├─ acl-group/acl-prod-in
├─ acl-group/acl-prod-out
├─ meter/meter-gold
├─ mapping/vnet-blue-to-west
└─ steering/vip-selector-west
```

### Session timeline

`dashctl migration status` should show phase timeline.

```text
[12:40:02] Admission  PASS
[12:40:03] Snapshot   PASS
[12:40:05] Prepare    PASS
[12:40:10] Sync       PASS  lag=110ms
[12:41:00] Cutover    PASS  new_flows_owner=target
[12:41:20] Drain      RUN   residual_flows=128
```

### Flow ownership view

`dashctl migration flows` should show age buckets and ownership distribution.

```text
Owner      0-10s  10-60s  1-5m  5-30m  30m+
source        0      12    41     22     5
target      210      88    12      0     0
unknown       0       0     1      0     0
```

### Explain blockages

`dashctl migration explain` should translate findings into operator language, for example: “Cutover blocked because target realized ACL groups but route-group rg-west is unresolved; steering remains on source and rollback is still viable.”

## API design

Migration APIs should be long-running, generation-aware, and streaming-capable.

### Core RPCs or REST endpoints

- `CreateMigrationPlan`
- `GetMigrationPlan`
- `ValidateMigrationPlan`
- `StartMigrationSession`
- `PrepareMigration`
- `AdvanceMigrationPhase`
- `GetMigrationSession`
- `StreamMigrationSession`
- `RollbackMigration`
- `AbortMigration`
- `CommitMigration`
- `ExportMigrationBundle`
- `ImportMigrationBundle`

### API semantics

- Every mutating operation should require an idempotency key.
- Every phase transition should check the session generation.
- Every response should carry phase, status, findings, and next legal actions.
- Streaming APIs should emit changes in flow ownership, residual flows, readiness, and guardrail violations.

## Bundle and evidence design

The migration bundle is essential for postmortems, support, and compliance review.

The bundle should contain:

- Plan and session manifests.
- Dependency graph.
- Source and target snapshots.
- Per-phase findings and timestamps.
- Flow ownership snapshots.
- Steering actions and generation history.
- Health counters and drop reasons.
- Final outcome, including rollback if performed.

## Security and access control

Live migration is a high-impact operation and should be protected accordingly.

- Planning and validation may be granted more broadly than cutover and commit.
- Cutover, rollback, and commit should require elevated roles or explicit approval policy.
- Exported bundles should be signed and optionally encrypted.
- Every migration action should be audited with principal, timestamp, reason, and source IP or session identity.

## Reliability requirements

An industry-grade service must survive control-plane restarts and operator disconnects.

- Migration sessions must persist in durable storage.
- Checkpoints must be replayable after service restart.
- Orchestrator actions must be idempotent.
- Loss of observability must pause forward progress.
- A session must never silently jump phases after recovery.

## Recommended V1

The recommended V1 is a drain-based ENI live migration controller with explicit dual presence, explicit new-flow ownership transfer, deterministic steering control, residual-flow visibility, and rollback-first safety. This fits the clearest public DASH/SAI HA signals while avoiding dependency on universal state export/import support.[cite:127][cite:130][cite:167][cite:129]

V1 should include:

- Plan, validate, graph, export, import.
- Prepare target shadow state.
- New-flow steering cutover.
- Residual-flow drain monitoring.
- Readiness and session watch.
- Rollback with preserved checkpoint.
- Evidence bundle generation.

## Recommended V2 and V3

### V2

Add selective active-flow rehome where platform support exists, richer service-state adapters, canary steering, and policy simulation.[cite:129][cite:167]

### V3

Add predictive migration scoring, capacity-aware fleet rebalance, maintenance scheduling integration, and closed-loop autonomous migration under approved policies.[cite:50][cite:167]

## Final design position

The strongest enterprise design for DashCenter ENI live migration is: dual-programmed ENI presence, explicit dependency graph, explicit flow ownership, policy-driven steering, new-flows-first cutover, observable drain, deterministic rollback, and portable migration artifacts. That design matches the current public direction of DASH HA and SAI ENI HA work, while leaving room for deeper active-state transfer as vendor capabilities mature.[cite:127][cite:130][cite:167][cite:15]
