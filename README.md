# DashCenter

> Centralized visibility, troubleshooting, and fleet operations for DASH-compliant devices.

DashCenter is an operations platform for environments that run one or many DASH-capable DPUs or appliances. It uses the same DASH object model and API vocabulary as the programming plane, then adds fleet-wide visibility, object inspection, packet/flow reasoning, state reconciliation, ENI mobility diagnostics, health analysis, and evidence collection in one place.

## Why DashCenter

**DASHCenter** is a distributed, multi-tenant state orchestrator and telemetry accumulator designed specifically for SmartNIC and Data Processing Unit (DPU) clusters running the **Disaggregated API for SONiC Hosts (DASH)**.

Think of it as the control room and diagnostic hub for your infrastructure edge. Instead of managing, debugging, and tracing stateful cloud network parameters on a per-card basis using disjointed commands, DASHCenter aggregates the operational states of your entire DPU fleet into a single unified dashboard.

---

### The Paradigm Shift: Why DASHCenter?

Traditional network tools are built for standard Layer 3 stateless routing. DPUs operating on the DASH pipeline handle massive, highly scaled stateful operations directly in hardware—managing millions of concurrent connections, massive Access Control List (ACL) tag matrices, and nested Elastic Network Interface (ENI) virtual routing topologies.

When a packet mysteriously vanishes or a hardware configuration drifts out of sync in production, traditional tools leave operators blind. DASHCenter addresses this challenge by providing a central engine to aggregate, validate, and analyze real-time packet transformations across all nodes simultaneously.

---

---

Good catch. The previous view compressed the appliance tier into a single block, which obscured how **DASHCenter** manages a scaled-out fleet of DPUs simultaneously.

To fix this, here is the diagram that explicitly shows the **1-to-Many distributed architecture**. It capture block diagram on how the system can manage multiple DASH appliances (DPU 01 through DPU 10).

```
==================================================================================================
                                      USER INTERACTION LAYER
==================================================================================================
                                    +---------------------------+
                                    |    dashctl CLI Client     |
                                    | (Configured Context Layer)|
                                    +-------------+-------------+
                                                  |
                                                  | Aggregated Multi-Node Queries
                                                  v (REST / gRPC API Calls)
==================================================================================================
                                    DASHCenter MANAGEMENT SUITE
==================================================================================================
+------------------------------------------------------------------------------------------------+
|                                      clidemon API Daemon                                       |
|                                                                                                |
|     +-------------------------------------+      +---------------------------------------+     |
|     |         API Request Engine          |      |        State Aggregator Core          |     |
|     |  - Resolves multi-device filters    |      |  - Thread Pool Manager                |     |
|     |  - Executes cross-plane traces      |      |  - Schema Normalizer                  |     |
|     +------------------+------------------+      +-------------------+-------------------+     |
|                        |                                             ^                         |
|                        | Fast Cache Reads                            | Standardized Writes     |
|                        v                                             |                         |
|     +--------------------------------------------------------------------------------------+   |
|     |           Centralized Persistence & TimeSeries Cache (Redis Stack)                   |   |
|     |   [Indexes by Appliance ID: dpu-01, dpu-02, ... dpu-10 to separate state tables]     |   |
|     +--------------------------------------------------------------------------------------+   |
|                                                  ^                                             |
|          +---------------------------------------+---------------------------------------+     |
|          |  Concurrent Ingestion Workers (Async Polling & Push Notification Streams)     |     |
|          |                                                                               |     |
|          v Channel 01                            v Channel 02                            v Channel 10
+----------+---------------------------------------+---------------------------------------+-----+
           | (gNMI / GRPC)                   | (gNMI / GRPC)                   | (gNMI / GRPC)
           |                                       |                                       |
==================================================================================================
                                  DISTRIBUTED DASH APPLIANCE FLEET
==================================================================================================
+-----------------------+              +-----------------------+              +-----------------------+
|  DASH Appliance 01    |              |  DASH Appliance 02    |              |  DASH Appliance 10    |
|                       |              |                       |              |                       |
| +-------------------+ |              | +-------------------+ |              | +-------------------+ |
| | Local Probe Agent | |              | | Local Probe Agent | |              | | Local Probe Agent | |
| +---------+---------+ |              | +---------+---------+ |              | +---------+---------+ |
|           |           |              |           |           |              |           |           |
|           v           |              |           v           |              |           v           |
| +-------------------+ |              | +-------------------+ |              | +-------------------+ |
| |  SONiC Databases  | |              | |  SONiC Databases  | |              | |  SONiC Databases  | |
| | (APP_/ASIC_/STATE)| |              | | (APP_/ASIC_/STATE)| |              | | (APP_/ASIC_/STATE)| |
| +---------+---------+ |              | +---------+---------+ |              | +---------+---------+ |
|           |           |              |           |           |              |           |           |
|           v           |              |           v           |              |           v           |
| +-------------------+ |              | +-------------------+ |              | +-------------------+ |
| | DASH SAI Hardware | |              | | DASH SAI Hardware | |              | | DASH SAI Hardware | |
| | P4 Packet Pipeline| |              | | P4 Packet Pipeline| |              | | P4 Packet Pipeline| |
| +-------------------+ |              | +-------------------+ |              | +-------------------+ |
+-----------------------+              +-----------------------+              +-----------------------+
```

---

### Key Multi-Node Management Mechanics

1. **Partitioned Storage:** Inside the central database, data is partitioned by append keys mapping to individual appliances (e.g., dpu-01:DASH_ENI_TABLE vs. dpu-10:DASH_ENI_TABLE). This prevents collision when different appliances use matching internal layout parameters.
2. **Decoupled Concurrency:** The Aggregator Core handles a dedicated pool of network channels. If DASH Appliance 02 encounters a slow control plane or drops offline, its corresponding worker thread handles timeouts independently, protecting the data collection flow from the remaining healthy nodes.


3. **Scatter-Gather API Logic:** When you run a cross-fabric query like dashctl get enis --all-devices, the API engine gathers data locally from the unified persistence cache instead of executing 10 individual live SSH or gNMI calls down to the hardware. This satisfies our 500ms responsiveness constraint.

---

### Core Structural Features

* **Kubernetes-Like Operations (dashctl):** Built to mimic the seamless declarative style of cloud-native systems. Operators log into the central suite and execute simple, unified commands like dashctl get enis --all-devices or dashctl monitor drops -w rather than jumping across fragmented individual devices.


* **Dual Deployment Versatility:** Designed to conform to your topology constraints. It can deploy as a **Dedicated Controller Appliance** on a standalone x86 server or as a **Symmetric Converged Cluster** running locally on the management cores of the DPUs themselves, utilizing an embedded consensus protocol to self-elect an active leader.


* **Asynchronous, Deep Diagnostics:** Leverages non-intrusive data extraction mechanisms—such as gNMI streams, local Redis database taps, and hardware event queues—to extract diagnostic telemetry with **zero performance impact** on the live network packet-forwarding plane.



---

### Signature Superpowers

#### 1. The Virtual Packet Tracer

Allows you to simulate complex packet journeys. You provide a mock network 5-tuple payload to the engine via a REST call or CLI, and DASHCenter steps it analytically through every single phase of the DASH pipeline (ENI, Flow-Table, Route, and ACL matrices), pinpointing exactly where a packet will be forwarded or dropped before sending real traffic.

#### 2. Cross-Plane State Auditing (Merkle-Trees)

Defends against silent state corruption. By organizing control-plane configuration layers and underlying ASIC hardware tables into local cryptographic Merkle trees, DASHCenter can continuously verify state consistency without heavy processing loops. If a root hash mismatches, it traces down the tree to isolate un-synchronized entries in milliseconds.

#### 3. Zero-Copy Drop Attribution

Monitors physical hardware error hooks dynamically. When an appliance drops an unexpected frame, DASHCenter’s local daemon catches the packet’s metadata from shared memory registers and streams back an automated explanation defining the precise failure context (e.g., *Dropped at Outbound ACL Stage due to rule violation*).


## Who it is for

DashCenter is aimed at platform teams, network engineers, SREs, NOC operators, and support teams operating DASH-compliant DPUs in production. It is especially useful when multiple devices, peers, or sites must be managed consistently through one operations layer.[cite:50][cite:141]

## Project goals

- Make DASH objects operationally visible across layers.[cite:36][cite:45]
- Provide a kubectl-like workflow for single-device and fleet-scale troubleshooting.[cite:136][cite:141]
- Improve incident response, ENI mobility safety, and change confidence.[cite:127][cite:130]
- Reduce dependence on one-off vendor-specific debug procedures by giving a stable DASH-native operations model.[cite:50][cite:45]

## Status

DashCenter is currently in design and architecture phase. The initial target is a gRPC-first service with a `dashctl` CLI, followed by fleet APIs, streaming health, historical snapshots, and a web console.[cite:43][cite:45]

## Repository structure

```text
/docs           Specifications, architecture, design docs
/proto          gRPC and protobuf APIs
/cmd/dashctl    CLI entrypoint
/pkg            Shared libraries and core types
/internal       Service implementation
/deploy         Deployment manifests and packaging
/web            Future console assets
```

## Roadmap

### Phase 1

- Canonical object model
- `dashctl get`, `list`, `show graph`
- `dashctl explain match`
- `dashctl trace flow`
- `dashctl reconcile`

### Phase 2

- ENI bundle, flows, readiness
- Fleet health and findings
- Support bundle generation
- Historical snapshots and diffing

### Phase 3

- HA parity and drain monitoring
- Streaming watch APIs
- Web console
- Policy simulation and advanced analytics

## Open source direction

DashCenter is intended to be an open source operations platform for DASH ecosystems, with a clean core, pluggable adapters, and a stable CLI/API model. The long-term goal is a shared visibility and diagnostics layer that can work across multiple DASH-compliant vendors and deployments while still allowing vendor-specific enrichments where available.[cite:50][cite:43]

## Contributing

The project should accept contributions in the following areas:

- object schemas and APIs
- CLI ergonomics
- DASH data-source adapters
- ENI mobility workflows
- health analysis and evidence collection
- documentation and examples

## License

Apache-2.0 is a strong fit for infrastructure tooling and vendor-neutral ecosystem projects.

