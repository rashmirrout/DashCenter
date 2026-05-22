# System Specification: Enterprise Diagnostic & Visibility System for DASH
## Document Control
* **System Identifier:** DASHCenter Engine & `dashctl` CLI Tool suite
* **Target Environment:** Distributed SONiC SmartNIC/DPU Platforms (Mellanox/BlueField, AMD/Pensando, Marvell/Octeon)
* **Status:** Complete Engineering Specification

---

## 1. System Requirements Document (SRD)

This section establishes the functional and non-functional requirements for the DASH distributed diagnostic platform, ensuring flexibility across single-node deployments, cluster form-factors, and massive scale data-plane parsing.

### 1.1 Functional Requirements (FR)

#### FR-1: Flexible Deployment Topologies
* **FR-1.1 (Dedicated Controller Mode):** The system must support installation as a standalone containerized software stack on an external x86 bare-metal server or VM (`DASHCenter`). This centralized host acts as the dedicated orchestrator and telemetry accumulator for all target DPUs.
* **FR-1.2 (Symmetric Converged Mode):** The system binary must be uniform and deployable directly onto the control-plane CPU (Arm/x86 Linux environment) of any individual DASH-compliant appliance or DPU. 
* **FR-1.3 (Leader Election):** In Symmetric Converged Mode, the platform must dynamically elect a single master `DASHCenter` instance among a pool of up to 10 appliances using a consensus protocol. The remaining 9 instances must drop into worker/forwarder mode, feeding local metrics to the elected leader.

#### FR-2: Declarative Control & API Plane (`clidemon`)
* **FR-2.1 (Stateless Core):** The centralized engine must expose an asynchronous, stateless service (`clidemon`) handling REST/HTTPS and gRPC endpoints.
* **FR-2.2 (Context Abstraction):** The API plane must support Multi-Tenancy and Namespace boundaries, mapping distinct infrastructure segments or tenant VNETs identically to Kubernetes namespaces (e.g., `dashctl get enis --namespace=tenant-blue`).
* **FR-2.3 (Unified Object Inventory):** `clidemon` must maintain an updated view of all DASH structural entities (`ENIs`, `VNETs`, `Routes`, `ACLs`) discovered across the appliance fleet.

#### FR-3: Kubectl-Like User Experience (`dashctl`)
* **FR-3.1 (Command Taxonomy Consistency):** The CLI syntax must leverage noun-verb pairs directly mirroring Kubernetes design conventions (`dashctl get`, `dashctl describe`, `dashctl logs`, `dashctl monitor`).
* **FR-3.2 (Serialization & Output Formats):** All resource discovery paths must natively support `-o json`, `-o yaml`, and `-o wide` switches to enable programmatic consumption by external tools.
* **FR-3.3 (Stream Watch Capabilities):** Implement a global watch flag (`-w` / `--watch`) using long-lived HTTP Server-Sent Events (SSE) or gRPC server streams to output configuration changes or performance counter fluctuations instantly without client-side polling loops.

#### FR-4: Distributed Probing & State Consolidation
* **FR-4.1 (Heterogeneous Extraction Hooks):** The telemetry consumer layer must read from both local and remote nodes using gRPC/gNMI Protobuf channels, eliminating remote database tapping.
* **FR-4.2 (State Normalization):** Independent vendor object layouts must be standardized into an internal canonical data model before entering the persistence layer.

---

### 1.2 Non-Functional Requirements (NFR)

#### NFR-1: Datapath Isolation & Control Overhead
* **NFR-1.1 (Zero Interruption):** Diagnostic lookups, tree checks, and table discoveries must execute strictly asynchronously from the data plane. The tool must never inject latency or compete for packet-processing pipeline cycles on the ASIC/P4 threads.
* **NFR-1.2 (Resource Ceilings):** When operating on a worker DPU, the local daemon footprint must not exceed 5% CPU consumption on a single core and must remain under 512MB RAM utilization.

#### NFR-2: Ingestion Performance & Scalability
* **NFR-2.1 (Scale Thresholds):** The central state accumulator must easily support 10 concurrent appliances, where each appliance maintains a scale limit of up to 100,000 active stateful connections, resulting in a minimum processing capability of 1,000,000 active tracking lines.
* **NFR-2.2 (Read Performance):** Aggregate multi-node queries (e.g., fetching whole-fabric drop counters or checking ACL hit rates across all 10 devices) must complete execution and stream out text to the user shell within 500 milliseconds.

#### NFR-3: High Availability & Network Partitioning
* **NFR-3.1 (Failover Speed):** In a converged mesh topology, if the node hosting the primary `DASHCenter` engine fails or is cut off from the network, the remaining nodes must elect a backup master and sync global topologies within 5 seconds.
* **NFR-3.2 (Split-Brain Mitigation):** The state engine must enforce strict quorum rules (N/2 + 1). If an appliance gets partitioned from the majority cluster, it must disable its API write mechanics and report read-only local views to avoid localized state drift.

---

## 2. High-Level Design (HLD)

### 2.1 Architectural Topology Diagrams

The architecture accommodates two deployment footprints via an identical software package.

#### Topology A: Dedicated Controller Mode
```
+-------------------------------------------------------------+
|                      Management Workstation                 |
|                        [ dashctl CLI ]                      |
+------------------------------+------------------------------+
                               | REST / gRPC
                               v
+-------------------------------------------------------------+
|               Central Appliance / VM Engine                 |
|                       [ DASHCenter ]                        |
|   +-----------------------------------------------------+   |
|   |                      clidemon                       |   |
|   |  +------------------+         +------------------+  |   |
|   |  |   API Engine     |         | Aggregator Core  |  |   |
|   |  +--------+---------+         +--------+---------+  |   |
|   +-----------|----------------------------|------------+   |
|               v                            v                |
|   +-----------------------------------------------------+   |
|   |       High-Performance Storage Layer (Redis Stack)  |   |
|   +-----------------------------------------------------+   |
+---------------+----------------------------+----------------+
                |                            |
                | gNMI / Redis Protobuf      | gNMI / Redis Protobuf
                v                            v
+-------------------------------+ +---------------------------+
|          Worker DPU 01        | |       Worker DPU 10       |
| +---------------------------+ | | +-----------------------+ |
| | Local Probe (gNMI/DB-Tap) | | | | Local Probe           | |
| +---------------------------+ | | +-----------------------+ |
+-------------------------------+ +---------------------------+
```

#### Topology B: Symmetric Converged Mode (Self-Electing Cluster)
```
+---------------------------------------------------------------------------------+
|                                 dashctl CLI                                     |
+---------------------------------------+-----------------------------------------+
                                        | REST / gRPC (Targets Cluster IP)
                                        v
       +-------------------------------------------------------------------+
       |                      Virtual IP (Cluster VIP)                     |
       +--------------------------------+----------------------------------+
                                        |
       +────────────────────────────────┴───────────────────────────+
       |                                                            |
       v [Active Leader Role]                                       v [Follower Role]
+-----------------------------------------+   +-----------------------------------------+
|              Appliance 01               |   |              Appliance 02               |
| +-------------------------------------+ |   | +-------------------------------------+ |
| |              clidemon               | |   | |              clidemon               | |
| |  +------------+     +------------+  | |   | |  +------------+     +------------+  | |
| |  | API Engine |     | Aggregator |  | |   | |  | API Engine |     | Aggregator |  | |
| |  +------------+     +------------+  | |   | |  +------------+     +------------+  | |
| +-------------------------------------+ |   | +-------------------------------------+ |
| |   Redis Stack (Active Replication)  | |<--| |   Redis Stack (Passive Replica)     | |
| +-------------------------------------+ |Raft| +-------------------------------------+ |
| | Local Probe (Loops to Local Shared) | |Msg| | Local Probe (Forwards to Leader VIP)| |
| +-------------------------------------+ |   | +-------------------------------------+ |
+-----------------------------------------+   +-----------------------------------------+
```

### 2.2 DASHCenter Internal Core Engine Architecture
The internal engine consists of three decoupled layers:
1. **The Ingestion Pipeline:** Spawns asynchronous worker loops dedicated to every connected appliance. It handles incoming gNMI subscription streams and captures changes from the target DPU's local control state using strict protobuf interfaces.
2. **The Normalization Module:** Converts disparate vendor schema definitions into deterministic JSON data definitions structured around standard upstream DASH parameters.
3. **The Core Data Engine (Redis Stack):** Leverages **RedisTimeSeries** to index high-frequency counters (packet rates, drops, flow creation metrics) alongside **RediSearch** to enable multi-clause filtering across thousands of active infrastructure rules.

### 2.3 `clidemon` REST/gRPC API Layer
`clidemon` handles authentication, command processing, and real-time event distribution. It features built-in middleware for mapping incoming HTTP requests directly to internal search tasks over the data layer. 

To achieve sub-second execution performance, `clidemon` avoids synchronous calls to the DPUs during a query; instead, it serves reads directly from the data engine cache, which is kept updated by the ingestion threads.

### 2.4 Leader Election Mechanism
When deployed in Symmetric Converged Mode, the platform relies on an embedded Raft implementation within `clidemon`:
* **Heartbeat Intervals:** Nodes broadcast keeping-alive heartbeats over port `8989` every 150ms.
* **Timeout Thresholds:** If the primary leader drops out for over 1000ms, follower nodes transition to candidate status, increment their term IDs, and request peer cluster votes.
* **Virtual IP Bindings:** The newly elected master takes control of a shared Cluster Virtual IP via an internal gratuitous ARP execution, maintaining a single destination endpoint for `dashctl`.

---

## 3. DASH Object Models & Diagnostic Intelligence

### 3.1 Mapping Core Objects & Diagnostic Target Values

The diagnostic system continuously monitors the following primary DASH objects, mapping out their technical dependencies and key failure modes:

```
+--------------------+       1:M       +---------------------+
|   DASH_ENI_TABLE   |---------------->|   DASH_VNET_TABLE   |
|   (PCIe/Host Port) |                 |    (Overlay Domain) |
+---------+----------+                 +----------+----------+
          |                                       |
          | 1:M                                   | 1:M
          v                                       v
+--------------------+                 +---------------------+
| DASH_ACL_GROUP_TBL |                 | DASH_ROUTE_TABLE    |
| (Firewall Matrices)|                 | (LPM Prefix Matches)|
+--------------------+                 +---------------------+
```

* **`DASH_ENI_TABLE` (Elastic Network Interface)**
  * *Functionality:* Maps the raw host attachment interface (PCIe physical/virtual function) to a tenant profile.
  * *Diagnostic Targets:* Operational link status, allocated bandwidth caps, active connection counts, and High-Availability roles.

* **`DASH_VNET_TABLE` (Virtual Network Domain)**
  * *Functionality:* Provides isolation boundaries for tenant overlay configurations.
  * *Diagnostic Targets:* VXLAN/NVGRE VNI configurations, inner-to-outer header configuration states, and cross-tenant peering rules.

* **`DASH_ACL_GROUP_TABLE` & `DASH_ACL_RULE_TABLE`**
  * *Functionality:* Implements high-scale security policies using Prefix Tags to group thousands of rules together efficiently.
  * *Diagnostic Targets:* Rule evaluation priorities, tag expansion matrices, and exact tracking of rule hit counters.

* **`DASH_ROUTE_TABLE`**
  * *Functionality:* Governs Longest Prefix Match (LPM) routing for overlay networks.
  * *Diagnostic Targets:* Next-hop reachability, encapsulation parameters, and tunnel endpoint resolution.

---

### 3.2 Deep Packet Flow Match-and-Trace Engine Mechanics

The `trace` framework simulates the packet path by mapping simulated structures sequentially through the physical data-plane pipeline stages:

```
                  [ Inbound / Outbound Packet Payload ]
                                    │
                                    v
   Stage 1: ENI Context Lookup ───────────────────────────────┐
     - Resolves PCIe physical/virtual origin identity         │
     - Error State: Unknown Interface -> Drop (Code 0x01)     │
                                    │                         │
                                    v                         │
   Stage 2: Stateful Flow Table Lookup ───────────────────┐   │
     - Tracks active 5-tuple tracking structures          │   │
     - Hit Path: Fast-path bypass to encapsulation        │   │
     - Miss Path: Fallback to slow-path evaluation        │   │
                                    │                     │   │
                                    v                     │   │
   Stage 3: Policy & LPM Route Resolution ────────────────┼───┼─► Fast-Path Bypass
     - Identifies target VNET transit rules               │   │   (Direct Encap)
     - Error State: No Route Found -> Drop (Code 0x04)    │   │
                                    │                     │   │
                                    v                     │   │
   Stage 4: Inbound/Outbound ACL Evaluation ──────────────┼───┘
     - Checks firewall permissions via expanded tags      │
     - Error State: Security Violations -> Drop (Code 0x08)│
                                    │                     │
                                    v                     v
                [ Packet Rewrite, VXLAN Encap, and Egress ]
```

When an operator issues a request via `dashctl trace`, the engine queries the active configuration models to compute a precise analytical trace of the matching rules across every pipeline stage.

---

### 3.3 Inline Drop Attribution using In-Band Network Telemetry (INT)

To handle production drops without impacting performance, `DASHCenter` leverages hardware-assisted error-queue captures. When a packet drops, the DPU hardware populates metadata registers detailing the drop context.

The local agent monitors these error frames using zero-copy shared memory rings and packages them as normalized telemetry events.

#### Normalized Drop Schema Example:
```json
{
  "timestamp": "2026-05-22T16:40:12.891Z",
  "appliance_id": "dpu-node-03",
  "packet_metadata": {
    "src_ip": "10.240.12.44",
    "dst_ip": "192.168.100.2",
    "protocol": 6,
    "src_port": 54322,
    "dst_port": 443
  },
  "drop_diagnostics": {
    "stage": "STAGE_4_ACL_EVALUATION",
    "hardware_code": "0x000002AC",
    "reason_string": "DASH_DROP_REASON_ACL_EGRESS_DENY",
    "associated_object": "DASH_ACL_RULE_TABLE:tenant-prod-block-ssh"
  }
}
```

---

### 3.4 Cross-Plane State Integrity Auditing (Merkle-Tree Engine)

To detect silent state drift—where software databases think a rule is applied but the underlying hardware tables are out of sync—`DASHCenter` implements an inline Merkle-Tree audit mechanism.

```
       Control Plane (Intent)                      Data Plane (Reality)
    +--------------------------+               +--------------------------+
    |     gNMI Protobuf State  |               |    DASH Hardware Tables  |
    +-------------+------------+               +-------------+------------+
                  |                                          |
                  v                                          v
          [ Root Hash: A9B2 ]                       [ Root Hash: F8C3 ]
                 /    \                                    /    \
                /      \             MISMATCH!            /      \
               v        v          A9B2 != F8C3          v        v
           [Hash 1]  [Hash 2]  ──────────────────────> [Hash 1]  [Hash 3]
             /  \      /  \                                      /  \
            v    v    v    v                                    v    v
          ObjA ObjB ObjC ObjD                                 ObjC  Drifted_Obj!
```

1. **Tree Synthesis:** The agent structures local database parameters into a binary cryptographic tree. Every leaf represents an object configuration state, while parent nodes hold hashes of their children.
2. **Root Comparison:** The orchestrator checks only the Root Hash from each DPU. If the control-plane root matches the data-plane root, the table is verified as 100% consistent.
3. **Rapid Isolation:** If the roots do not match, the auditor traverses down the mismatched branches, isolating the specific corrupted or missing object ID within milliseconds, avoiding the need for a full, resource-heavy linear table dump.

---

## 4. Interface Blueprint (CLI & API Spec)

### 4.1 `dashctl` CLI Command Taxonomy

The CLI mimics `kubectl` mechanics, implementing a unified, resource-centric syntax.

```bash
# 1. Global View Operations
dashctl get appliances
dashctl get enis --all-devices -o wide
dashctl get vnets --namespace=production -o yaml

# 2. Detailed Inspection Operations
dashctl describe eni eni-vnic-202 --device=dpu-node-01
dashctl describe acl-group sec-group-web --namespace=frontend

# 3. Live Diagnostics & Multi-Node Verification
dashctl trace packet --src=10.0.0.5 --dst=20.100.2.10 --proto=tcp --dport=80
dashctl monitor drops --device=dpu-node-04 --follow
dashctl audit consistency --table=DASH_ACL_RULE_TABLE --repair
```

---

### 4.2 REST API Endpoint Schemas & Payload Examples

The backend `clidemon` application exposes structured endpoints to back both `dashctl` and external custom dashboards.

#### Endpoint 1: Fetch Unified Appliance Inventories
* **HTTP Method / Path:** `GET /api/v1/appliances`
* **Response Payload (`200 OK`):**
```json
{
  "kind": "ApplianceList",
  "apiVersion": "dash.enterprise.io/v1",
  "metadata": {
    "total_nodes": 2,
    "cluster_status": "HEALTHY"
  },
  "items": [
    {
      "id": "dpu-node-01",
      "role": "LEADER",
      "management_ip": "10.50.100.11",
      "platform": "Mellanox_BlueField_3",
      "uptime_seconds": 1204550,
      "metrics": {
        "cpu_utilization_percent": 2.4,
        "memory_utilization_bytes": 214748364,
        "active_flows": 43220
      }
    },
    {
      "id": "dpu-node-02",
      "role": "FOLLOWER",
      "management_ip": "10.50.100.12",
      "platform": "Mellanox_BlueField_3",
      "uptime_seconds": 1204548,
      "metrics": {
        "cpu_utilization_percent": 1.8,
        "memory_utilization_bytes": 198180220,
        "active_flows": 39100
      }
    }
  ]
}
```

#### Endpoint 2: Execute Virtual Packet Trace Across Distributed Pipeline Configurations
* **HTTP Method / Path:** `POST /api/v1/diagnostics/trace`
* **Request Payload:**
```json
{
  "namespace": "tenant-gold",
  "packet_header": {
    "src_ip": "10.1.10.5",
    "dst_ip": "10.200.50.20",
    "protocol": 6,
    "src_port": 49152,
    "dst_port": 80
  }
}
```
* **Response Payload (`200 OK`):**
```json
{
  "kind": "PacketTraceResult",
  "apiVersion": "dash.enterprise.io/v1",
  "trace_summary": {
    "verdict": "ALLOWED",
    "egress_interface": "vnet-tunnel-09",
    "total_stages_traversed": 4
  },
  "pipeline_stages": [
    {
      "stage_index": 1,
      "stage_name": "ENI_LOOKUP",
      "status": "MATCH",
      "matched_object": "DASH_ENI_TABLE:eni-host-vnic-01",
      "details": "Resolved source interface successfully."
    },
    {
      "stage_index": 2,
      "stage_name": "FLOW_LOOKUP",
      "status": "MISS",
      "matched_object": null,
      "details": "No existing tracking state found; falling back to full routing evaluation."
    },
    {
      "stage_index": 3,
      "stage_name": "ROUTE_LOOKUP",
      "status": "MATCH",
      "matched_object": "DASH_ROUTE_TABLE:prefix-10.200.0.0_16",
      "details": "Resolved next-hop tunnel target VNI 600102."
    },
    {
      "stage_index": 4,
      "stage_name": "ACL_EVALUATION",
      "status": "MATCH",
      "matched_object": "DASH_ACL_RULE_TABLE:rule-allow-http-global",
      "details": "Packet matches explicit allow rule conditions."
    }
  ]
}
```
