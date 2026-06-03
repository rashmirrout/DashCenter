# DASH Diagnostics System and CLI Design Specification

## Executive Summary

This document specifies an industry-grade diagnostics and troubleshooting platform for production DPUs that support full DASH-based programming. The system is designed as a read-mostly, out-of-band operational plane that uses the same DASH APIs and object model as the programming plane, then adds cross-layer visibility, troubleshooting workflows, packet/flow reasoning, object reconciliation, ENI mobility diagnostics, health analysis, evidence collection, and automation-friendly interfaces.[cite:36][cite:43][cite:45][cite:50]

The design assumes that the appliance already supports DASH programming and that the new platform must improve observability and debugging without replacing the controller or vendor dataplane implementation. DASH already defines object processing across northbound APIs, DASH APP_DB, DashOrch, SAI realization, and STATE_DB programmed status, which makes a correlated diagnostics layer technically feasible and operationally valuable.[cite:36][cite:45][cite:50]

## Goals and Non-Goals

### Goals

- Provide production-grade diagnostics for DASH-programmed DPUs using the same object vocabulary used by operators and controllers.[cite:43][cite:45]
- Expose strong operational workflows: object inspection, explain-match, trace-flow, reconcile-state, ENI mobility diagnostics, health analysis, and support-bundle generation.[cite:36][cite:45]
- Correlate intent, software state, runtime state, dataplane realization, and traffic evidence into one operator-facing model.[cite:36][cite:45]
- Support CLI, gRPC, and REST interfaces with consistent semantics and output models.[cite:43][cite:45]
- Be vendor-integrable but not vendor-locked, using adapter interfaces for platform-specific realization and telemetry.[cite:50][cite:36]
- Be suitable for production operations, incident response, NOC workflows, change validation, and migration readiness.[cite:36][cite:50]

### Non-Goals

- Replacing the vendor’s DASH programming plane or SDN controller.[cite:43][cite:50]
- Acting as the sole source of configuration truth for the DPU fleet.[cite:36][cite:45]
- Requiring deep vendor internals for all features; advanced features should improve when vendor adapters expose more data, but the core model must still work with standard DASH-accessible state.[cite:43][cite:50]

## Operational Problem Statement

A production DPU that supports DASH can be correctly configured at the controller layer and still fail operationally due to broken object dependencies, partial realization, inconsistent peer state, resource pressure, or unexpected traffic behavior. DASH’s architecture already spans northbound objects, APP_DB state, orchestration, SAI translation, and STATE_DB programmed status, but operators often do not have a single diagnostic tool that explains failures across those layers.[cite:36][cite:45]

The system defined here solves that gap by answering the operator’s core questions directly:

- What objects exist for this workload, ENI, VNET, or flow?[cite:45]
- What object dependencies and bindings exist, and what is their current health?[cite:36][cite:45]
- Why did this packet match, miss, redirect, or drop?[cite:36]
- What path did this traffic take, and where did that path diverge from expectation?[cite:36]
- Is the configured state fully realized on the source DPU, peer DPU, or target DPU?[cite:45]
- What rules, flows, sessions, and dependencies are associated with an ENI for HA or migration operations?[cite:127][cite:130]

## High-Level Architecture

The diagnostics platform is composed of six logical layers.

### 1. Acquisition Layer

This layer reads state from all relevant control and runtime planes:[cite:36][cite:45][cite:50]

- DASH northbound API or controller-facing protobuf/gRPC interfaces.[cite:43]
- DASH APP_DB objects representing accepted software state.[cite:45]
- STATE_DB programmed status and operational object state.[cite:45]
- ASIC/SAI realization if accessible through standard SONiC/SAI mechanisms or vendor adapters.[cite:45]
- Vendor-specific hardware telemetry, flow/session state, counters, exception reasons, and mobility metadata where available.[cite:50]
- Platform health, process state, logs, and resource telemetry, which are essential for production troubleshooting.[cite:50]

### 2. Normalization Layer

This layer converts all sources into canonical records with stable identities. One logical object may appear as a northbound DASH object, an APP_DB record, a STATE_DB record, a SAI object, and one or more vendor runtime representations, so the system must maintain explicit lineage between those forms rather than flattening them into one record.[cite:45][cite:36]

### 3. Correlation Graph

The correlation graph is the core of the system. It represents relationships such as:

- ENI belongs to VNET.[cite:45]
- ENI is protected by ACL group or policy.[cite:45]
- Route rule depends on mapping or appliance binding.[cite:36][cite:45]
- APP_DB object realizes into one or more SAI objects.[cite:45]
- Programmed status for a configured object is reflected in STATE_DB.[cite:45]
- A flow or packet was classified under a given ENI and affected by a specific rule, route, meter, or service object.[cite:36]
- A flow is owned by a source or target DPU for HA or ENI mobility scenarios.[cite:127][cite:130]

### 4. Analysis Engine

This layer executes the major diagnostics functions:

- Object graph resolution.[cite:45]
- Match explanation using DASH packet processing logic.[cite:36]
- Trace-flow reconstruction using expected and observed path information.[cite:36]
- Reconcile-state comparison across intent, software state, and runtime realization.[cite:45]
- ENI flow aggregation and mobility readiness analysis.[cite:127][cite:130]
- Health scoring and blast-radius analysis.[cite:36][cite:45]

### 5. Interfaces Layer

The same engine must serve multiple operator surfaces:

- CLI for operations engineers and automation.[cite:43]
- gRPC API for structured integrations and streaming workflows.[cite:43]
- REST API for portals, support tools, and ecosystem integrations.[cite:43]
- Optional UI for graph exploration, incident review, and NOC workflows.[cite:36]

### 6. Persistence and History Layer

A production system must keep snapshots, timelines, evidence artifacts, and health histories, because troubleshooting often depends on understanding what changed before impact. Historical diffing and evidence-pack generation are core enterprise features, not optional extras.[cite:36][cite:45]

## Core Domain Model

The platform should define a canonical domain model centered on DASH-native entities.

### Primary Object Families

| Object family | Examples | Purpose |
|---|---|---|
| Attachment objects | ENI, endpoint identity, NIC association | Identify workload attachment and ingress/egress context.[cite:45] |
| Network objects | VNET, VNI mapping, peering, route groups | Define overlay connectivity and routing scope.[cite:36][cite:45] |
| Policy objects | ACL group, ACL rule, route rule, PA validation, meter policy | Control access, validation, and treatment.[cite:45] |
| Service objects | Appliance/service-chain, private-link style redirection, tunnel/service constructs | Define non-direct path behavior.[cite:36] |
| Realization objects | APP_DB record, STATE_DB status, SAI object, vendor hardware state | Represent implementation state.[cite:45][cite:43] |
| Observation objects | Flow, packet evidence, counters, health state, logs, support artifacts | Represent what is actually happening in production.[cite:36][cite:50] |
| Mobility objects | ENI migration bundle, flow ownership, drain state, peer readiness | Support HA and mobility operations.[cite:127][cite:130] |

### Canonical Relationships

The system must model at least these relationship types:

- `BELONGS_TO`
- `DEPENDS_ON`
- `REALIZED_AS`
- `PROGRAMMED_IN`
- `BOUND_TO`
- `MATCHED_BY`
- `PROTECTED_BY`
- `FORWARDED_VIA`
- `REDIRECTED_TO`
- `METERED_BY`
- `OWNED_BY`
- `MIGRATABLE_TO`

These relations are required to implement explain-match, trace-flow, blast-radius, and mobility readiness in a uniform way.[cite:36][cite:45][cite:127]

## System Capabilities

### 1. Get Object

This capability returns a complete normalized view of a DASH object. It must show identity, object type, source records, dependent objects, bindings, realization state, current health, relevant counters, and known findings. For ENIs specifically, it must include VNET association, ACL bindings, route/mapping dependencies, service bindings, meter policy, flow summary, and mobility metadata.[cite:45][cite:36][cite:127]

### 2. Explain Match

This capability explains why a packet or flow matched a specific set of DASH objects. The engine uses DASH processing stages such as direction determination, ENI selection, ACL stages, route and mapping lookup, metering, and service-path selection to show the winning logic and the first failure point when traffic is dropped or misrouted.[cite:36][cite:45]

### 3. Trace Flow

This capability reconstructs the flow path through the appliance and related service objects. It must show expected path, observed path where available, all logical hops and transforms, the first divergence, and evidence supporting the trace. For encapsulated traffic, it must show the chosen VNET/VNI, encap decision, and outer tunnel metadata when those are available.[cite:36]

### 4. Reconcile State

This capability compares desired or configured DASH state against APP_DB, STATE_DB, SAI, and vendor runtime state. It identifies partial convergence, stale realization, missing dependencies, asymmetric peer state, and runtime drift. The result must be machine-readable and operator-readable, with explicit severity and remediation hints.[cite:45][cite:36]

### 5. ENI Mobility Diagnostics

This is a first-class capability for production DPU operations. It creates a migration bundle for an ENI, enumerates all associated rules and dependent objects, measures active flows and ownership state, verifies destination readiness, and monitors drain or cutover progress. This capability is directly motivated by DASH ENI operational semantics and SAI work related to ENI HA flow ownership.[cite:127][cite:130][cite:45]

### 6. Health and Visibility

The system must provide per-object, per-DPU, per-tenant, per-VNET, and per-ENI health views. Health must not be a generic green/yellow/red indicator only; it must be backed by findings such as programming failure, counter anomaly, stale flow ownership, peer asymmetry, missing dependency, route gap, or service-path misconfiguration.[cite:36][cite:45][cite:50]

### 7. Evidence and Supportability

For incidents and escalations, the system must generate support bundles that contain object snapshots, diffs, logs, counters, flow summaries, flow ownership state, and collected evidence for the incident window. This is critical for an enterprise product because RCA and vendor escalation are core operational workflows.[cite:36][cite:50]

## User Roles

| Role | Primary usage |
|---|---|
| NOC Operator | High-level health, alarm triage, ENI/VNET impact view |
| SRE / Operations Engineer | CLI-based debug, explain-match, trace-flow, reconcile-state |
| Network Engineer | Policy, route, mapping, and packet path analysis |
| Platform Engineer | DPU lifecycle, HA readiness, ENI mobility validation |
| Support Engineer | Evidence collection, diffing, support-bundle export |
| Automation / CI system | API-based checks, rollout validation, drift detection |

## Interfaces

### CLI

The CLI is the primary operator tool. It must be consistent, scriptable, composable, and capable of returning both human-readable and machine-readable outputs. Every command must support:

- `--output table|json|yaml|graph|wide`
- `--scope dpu|tenant|vnet|eni|flow|global`
- `--since`, `--until`
- `--evidence`
- `--watch`
- `--severity`
- `--explain`
- `--no-color`
- `--profile` for multi-environment support

### API

The system should be gRPC-first with REST parity for core operations. gRPC aligns well with protobuf-oriented DASH data handling and supports streaming health, watch operations, and deeply structured responses.[cite:43][cite:45]

## CLI Command Design

The command namespace must be clean and stable. The recommended binary name is `dashdiag`.

## Global CLI Principles

- Commands are verb-first and object-aware.
- Every command returns a deterministic exit code.
- Human output is concise by default but expandable with `--wide`, `--json`, and `--explain`.
- Graph-like commands support terminal tree mode and JSON graph mode.
- Long-running views use `watch` and streaming mode.
- Commands support explicit DPU targeting and multi-DPU fleet selection.

## Command Reference

### Object Inspection Commands

| Command | Purpose | Output / visualization |
|---|---|---|
| `dashdiag get <kind> <id>` | Fetch a single normalized object with identity, bindings, dependencies, realization, and health. | Default table view; `--wide` adds source layers; `--json` emits full canonical object.[cite:45] |
| `dashdiag list <kind>` | List objects of a given kind, optionally filtered by health, tenant, VNET, ENI, DPU, or tag. | Table view with sortable columns; `--json` for automation.[cite:45] |
| `dashdiag show graph <kind> <id>` | Display dependency/dependent graph for an object. | Tree view in terminal; JSON graph or Graphviz export in advanced mode.[cite:36][cite:45] |
| `dashdiag show lineage <kind> <id>` | Show controller, APP_DB, STATE_DB, SAI, and vendor lineage for an object. | Layered path visualization from intent to realization.[cite:45] |
| `dashdiag show bindings <kind> <id>` | Show what is attached to or protected by the object. | Relationship table grouped by binding type.[cite:45] |

#### `dashdiag get`

This is the operator’s starting point. For an ENI, it must show VNET, compute identity, attached ACL groups, route and mapping objects, service bindings, meter policies, current realization state, flow summary, and current health findings. For an ACL or route rule, it must show the objects it affects and how it participates in forwarding behavior.[cite:45][cite:36][cite:127]

#### `dashdiag show graph`

This command visualizes relationships as a dependency tree. The default output should be a layered graph, for example: `ENI -> VNET -> ACL Groups -> Routes -> Mappings -> Service Bindings -> Runtime State -> Flow Ownership`. This is one of the most important visibility commands because it turns raw configuration into an operational model.[cite:36][cite:45][cite:127]

### Match and Path Commands

| Command | Purpose | Output / visualization |
|---|---|---|
| `dashdiag explain match ...` | Explain which objects matched a packet/flow and why. | Step-by-step evaluation trace with winning and losing candidates.[cite:36] |
| `dashdiag trace flow ...` | Trace expected and observed path for a packet/flow. | Hop-by-hop pipeline view with divergence points.[cite:36] |
| `dashdiag explain drop ...` | Return first-failure analysis for a dropped flow. | Root-cause summary plus failed stage details.[cite:36] |
| `dashdiag trace object <kind> <id>` | Show how an object influences traffic paths. | Object-to-path impact graph.[cite:36][cite:45] |

#### `dashdiag explain match`

This command accepts packet and context fields such as source IP, destination IP, protocol, ports, ingress source, and optional ENI or DPU hints. It should return the inferred direction, matched ENI, matched ACL stage/rule, selected route or route rule, mapping result, meter decision, and final forwarding or drop action. When ambiguity exists, the tool must show alternate candidates and why they lost.[cite:36][cite:45]

Example:

```bash
dashdiag explain match --src 10.1.1.10 --dst 10.2.2.20 --proto tcp --dport 443 --dpu dpu-a
```

#### `dashdiag trace flow`

This command visualizes the packet journey. The view should be stage-oriented, for example:

1. Ingress classification
2. ENI selection
3. Direction decision
4. ACL evaluation
5. Route selection
6. Mapping or service redirect
7. Metering
8. Encapsulation or direct forwarding
9. Egress realization

When possible, the output must include evidence markers such as counter hits, runtime state confirmations, or vendor dataplane observations.[cite:36]

### Reconciliation and State Commands

| Command | Purpose | Output / visualization |
|---|---|---|
| `dashdiag reconcile <kind> <id>` | Compare desired and observed state for one object. | Layer-by-layer diff summary with severity.[cite:45] |
| `dashdiag reconcile batch --file targets.yaml` | Reconcile many objects in one job. | Tabular compliance view plus exportable report.[cite:45] |
| `dashdiag diff snapshot <before> <after>` | Compare historical snapshots. | Change matrix and blast-radius summary.[cite:36] |
| `dashdiag verify dpu <id>` | Run standard convergence and health checks against one DPU. | Multi-check status dashboard in terminal.[cite:50][cite:45] |
| `dashdiag verify fleet` | Run fleet-wide checks. | Fleet summary with drill-down identifiers.[cite:50] |

#### `dashdiag reconcile`

This command is essential for troubleshooting controller-vs-runtime drift. It must compare at least northbound object state, APP_DB state, STATE_DB programmed status, and any accessible SAI or vendor realization state. It should classify findings into categories such as `MISSING`, `STALE`, `PARTIAL`, `ASYMMETRIC`, `FAILED`, and `UNKNOWN`, and must show the first broken layer.[cite:45][cite:36]

### ENI Mobility and HA Commands

| Command | Purpose | Output / visualization |
|---|---|---|
| `dashdiag eni bundle <eni-id>` | Export all objects and state associated with an ENI. | Layered bundle summary with object counts and dependencies.[cite:127][cite:45] |
| `dashdiag eni flows <eni-id>` | Show active and recent flows associated with an ENI. | Flow table, top talkers, rate summary, ownership summary.[cite:127][cite:130] |
| `dashdiag eni readiness <eni-id> --target-dpu <id>` | Check whether ENI can be moved to a target DPU. | Readiness verdict, blockers, warnings, missing objects.[cite:127][cite:130] |
| `dashdiag eni drain <eni-id>` | Monitor flow drain during migration/cutover. | Streaming flow-count and ownership dashboard.[cite:127][cite:130] |
| `dashdiag eni ownership <eni-id>` | Show operational flow ownership state. | Source vs target ownership summary.[cite:127][cite:130] |
| `dashdiag ha parity <group-id>` | Compare peer DPU state for symmetry. | Side-by-side diff of object realization and ownership.[cite:130] |

#### `dashdiag eni bundle`

This command creates the migration bundle for an ENI. It must include all dependent rules, routes, mappings, services, realization state, current health, flow summary, and ownership metadata. This is the core “what moves with this NIC” command and should be treated as a flagship enterprise feature.[cite:127][cite:130][cite:45]

#### `dashdiag eni readiness`

This command must answer whether the destination DPU is ready for the ENI. It checks whether all required dependent objects exist or can be realized, whether capability mismatches exist, whether ownership can move safely, and whether long-lived or critical flows create migration risk.[cite:127][cite:130][cite:50]

### Health and Visibility Commands

| Command | Purpose | Output / visualization |
|---|---|---|
| `dashdiag health dpu <id>` | Show full health state for one DPU. | Health scorecard by subsystem and object family.[cite:50] |
| `dashdiag health eni <eni-id>` | Show ENI-specific health. | Findings list plus flow and dependency summary.[cite:45] |
| `dashdiag health vnet <id>` | Show VNET-wide health and affected ENIs. | Scope summary and blast-radius table.[cite:36][cite:45] |
| `dashdiag top flows` | Show hottest flows across fleet or scope. | Ranked table with filters by ENI/VNET/DPU. |
| `dashdiag top findings` | Show most severe or frequent findings. | Ranked findings view with drill-down IDs. |
| `dashdiag watch health` | Stream health and state changes. | Real-time terminal dashboard. |

### Evidence and Support Commands

| Command | Purpose | Output / visualization |
|---|---|---|
| `dashdiag bundle create ...` | Create a support bundle for an incident scope. | Bundle ID and artifact summary. |
| `dashdiag bundle inspect <bundle-id>` | Inspect collected artifacts. | Indexed evidence list and metadata. |
| `dashdiag events <scope>` | Show operational events and state transitions. | Timeline view. |
| `dashdiag logs <scope>` | Aggregate relevant logs for a scope. | Filtered log output with timestamps. |
| `dashdiag export report <scope>` | Export troubleshooting report. | Markdown/JSON artifact with findings and evidence. |

### Administrative Commands

| Command | Purpose | Output / visualization |
|---|---|---|
| `dashdiag profile list` | List configured environments or clusters. | Table |
| `dashdiag target list` | List DPUs, peers, roles, and labels. | Table |
| `dashdiag schema show` | Show supported object schemas and versions. | Version matrix |
| `dashdiag doctor` | Validate CLI connectivity, credentials, adapters, and capabilities. | Readiness checklist |

## CLI Output and Visualization Standards

An industry-grade CLI must make complex state readable without turning every command into JSON-only output.

### Required visualization modes

- **Table mode** for standard object and flow listing.
- **Wide mode** for detailed operator inspection.
- **Tree mode** for dependency graphs.
- **Pipeline mode** for match and trace outputs.
- **Timeline mode** for events and state changes.
- **Diff mode** for reconciliation and historical comparisons.
- **JSON/YAML mode** for automation and APIs.
- **Streaming watch mode** for drain, health, and active investigations.

### Example: `trace flow` visualization

```text
FLOW TRACE
Ingress: dpu-a / eni-100
Direction: outbound
Stage 1  ENI classify        PASS   eni-100
Stage 2  ACL outbound v4     PASS   rule allow-https
Stage 3  Route lookup        PASS   route-group rg-prod / prefix 10.2.2.0/24
Stage 4  Mapping             PASS   map-vnet-blue-to-remote-west
Stage 5  Meter               PASS   class-id 17
Stage 6  Encap decision      PASS   vxlan vni 5001
Stage 7  Tunnel endpoint     PASS   192.0.2.10 -> 198.51.100.40
Stage 8  Egress realization  PASS   programmed
```

### Example: `reconcile eni eni-100`

```text
RECONCILIATION SUMMARY
Target: ENI eni-100
Controller / NB API      PRESENT
APP_DB                   PRESENT
STATE_DB                 PARTIAL
SAI / Vendor runtime     MISSING route-group binding
Peer parity              WARNING
Result                   DEGRADED
Primary finding          ENI realized without full route dependency on target DPU
```

## API Surface

The API must mirror CLI semantics closely.

### Core API methods

| Method | Description |
|---|---|
| `GetObject` | Return canonical object with lineage and findings |
| `ListObjects` | Return filtered object list |
| `GetGraph` | Return dependency/dependent graph |
| `ExplainMatch` | Return packet classification and match explanation |
| `TraceFlow` | Return expected and observed path |
| `ReconcileObject` | Return state reconciliation result |
| `CreateSnapshot` | Persist a scoped snapshot |
| `DiffSnapshots` | Compare snapshots |
| `GetEniBundle` | Return full ENI migration bundle |
| `GetEniFlows` | Return flow summaries for an ENI |
| `CheckEniReadiness` | Evaluate ENI migration readiness |
| `WatchEniDrain` | Stream ENI flow drain events |
| `CheckHaParity` | Compare source and peer/target parity |
| `GetHealth` | Return scoped health findings |
| `CreateSupportBundle` | Build support artifact package |

## Troubleshooting Use Cases

### Use case 1: Packet is unexpectedly dropped

1. `dashdiag explain drop --src ... --dst ... --proto ...`
2. `dashdiag get eni <eni-id>`
3. `dashdiag trace flow ...`
4. `dashdiag reconcile eni <eni-id>`
5. `dashdiag bundle create --scope eni/<eni-id>`

This workflow identifies the first failure stage, shows affected object dependencies, confirms whether the object is fully realized, and captures incident evidence.[cite:36][cite:45]

### Use case 2: ENI live migration

1. `dashdiag eni bundle eni-100`
2. `dashdiag eni flows eni-100`
3. `dashdiag eni readiness eni-100 --target-dpu dpu-b`
4. `dashdiag ha parity pair-a`
5. `dashdiag eni drain eni-100`

This workflow answers what must move, what flows are active, whether the target is ready, whether peer parity exists, and when the cutover/drain is safe.[cite:127][cite:130][cite:50]

### Use case 3: Controller says config applied but traffic still fails

1. `dashdiag reconcile eni eni-100`
2. `dashdiag show lineage eni eni-100`
3. `dashdiag explain match ...`
4. `dashdiag trace flow ...`

This workflow exposes partial or stale realization and ties it to actual packet behavior.[cite:45][cite:36]

## Data Collection and Processing Requirements

### Collection methods

- Polling for static and slowly changing object state.
- Subscriptions or streams for health/state changes where supported.
- Vendor adapter queries for flow/session ownership and runtime evidence.
- On-demand packet or flow reasoning requests.
- Snapshotting at operator-defined or policy-defined intervals.

### Data quality requirements

- Every record must include source provenance and collection timestamp.
- Every analysis result must reference the exact source records used.
- Stale data must be clearly marked.
- Unknown results must not be presented as healthy results.
- Multi-source disagreements must be surfaced explicitly.

## Security and Access Control

An enterprise-grade diagnostics platform must implement RBAC and scoped access. Not all users should see all tenants, flows, or support bundles. Access should be enforceable at least by environment, DPU, tenant, VNET, ENI, and bundle scope.

The platform should support:

- Read-only incident roles.
- Engineering roles with bundle export capability.
- Automation roles with restricted API scopes.
- Audit logging for all data access and export operations.

## Reliability and Scale Requirements

### Availability

The service should be deployable in an HA configuration and must tolerate collector restarts and temporary target unavailability. Collection gaps must degrade gracefully without invalidating previously collected evidence.

### Performance

- Single-object lookup should be low-latency.
- Match explanation should complete quickly for interactive use.
- Trace and reconcile must be bounded and stream progress when operating on large scopes.
- Fleet-wide operations should support fan-out concurrency and result pagination.

### Scalability

The system must support:

- Multi-DPU environments
- Large numbers of ENIs and VNETs
- Large active-flow populations per ENI
- Historical snapshot retention
- Concurrent operator sessions and automation calls

## Observability of the Diagnostics Platform

The diagnostics platform itself must be observable. It should expose:

- Collector health
- Source freshness
- Adapter failures
- Query latency
- Cache hit rate
- Snapshot success/failure
- Bundle generation time
- Per-command usage metrics
- Audit trails

## Failure Model and Troubleshooting Semantics

Every diagnostic result should distinguish among:

- **Healthy**: object exists and is fully realized.
- **Degraded**: object exists but dependent or runtime state is impaired.
- **Failed**: object or critical dependency is absent or not realized.
- **Unknown**: data source unavailable or insufficient evidence.
- **Asymmetric**: source and peer/target disagree.
- **Stale**: data is too old to trust.

This semantic model is critical for operator trust and automation safety.[cite:45][cite:36][cite:130]

## Implementation Roadmap

### Phase 1: Foundation

- Canonical data model
- Object readers for northbound API, APP_DB, STATE_DB
- `get`, `list`, `show graph`, `show lineage`
- Basic health and findings model

### Phase 2: Reasoning

- `explain match`
- `trace flow`
- `reconcile`
- snapshot and diff

### Phase 3: Enterprise Operations

- ENI bundle and readiness
- ENI flow and ownership views
- HA parity
- support bundle generation
- watch/streaming health

### Phase 4: Advanced Operations

- historical analytics
- change intelligence
- policy simulation
- predictive migration risk scoring
- deeper vendor hardware evidence integrations

## Recommended V1 and V2 Command Set

### V1 mandatory

- `get`
- `list`
- `show graph`
- `show lineage`
- `explain match`
- `trace flow`
- `reconcile`
- `health`
- `eni bundle`
- `eni flows`
- `eni readiness`
- `bundle create`
- `doctor`

### V2 expansion

- `explain drop`
- `diff snapshot`
- `eni drain`
- `eni ownership`
- `ha parity`
- `watch health`
- `top flows`
- `top findings`
- `export report`

## Final Design Position

The correct enterprise design is a DASH-native diagnostics plane that speaks the same object language as the production programming plane and augments it with cross-layer visibility, match/path reasoning, reconciliation, ENI mobility diagnostics, health analytics, and evidence export. The strongest product differentiator is not raw telemetry collection but the ability to explain production behavior in terms of DASH objects, flows, dependencies, realization state, and migration safety for ENI-centric workloads.[cite:36][cite:43][cite:45][cite:50][cite:127][cite:130]
