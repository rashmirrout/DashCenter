# High-Level Design (HLD) Specification: DASHCenter Core Architecture

This document establishes the architectural layout and component interactions for **DASHCenter**. It reflects a strictly containerized, distributed approach where communication between the controller and the managed appliances relies exclusively on **gRPC and gNMI Protobuf channels**, eliminating any remote database tapping.

---

## 1. System Component Block Diagram

The block diagram below highlights the segregation between the **Stateless Query Plane**, the **Centralized Caching Plane**, and the **Protobuf-Exclusive Network Fabric**.

```
+-----------------------------------------------------------------------------------+
|                                  MANAGEMENT PLANE                                 |
|                                                                                   |
|   +-----------------------+                    +------------------------------+   |
|   |  dashctl CLI Client   |                    | Third-Party Web UI / Systems |   |[cite: 1]
|   +-----------+-----------+                    +--------------+---------------+   |
|               |                                               |                   |
|               +-----------------------+-----------------------+                   |
|                                       | REST / gRPC                               |
|                                       v                                           |
|   +---------------------------------------------------------------------------+   |
|   |                              clidemon Daemon                              |   |[cite: 1]
|   |                                                                           |   |
|   |   +----------------------------------+  +-----------------------------+   |   |
|   |   |        API Gateway Layer         |  |   Cache Bypass Engine       |   |   |
|   |   | - Handles standard cache reads   |  | - Handles synchronous live  |   |   |
|   |   | - Executes RediSearch queries    |  |   gRPC/gNMI routing switches|   |   |
|   |   +----------------+-----------------+  +--------------+--------------+   |   |
|   |                    |                                   |              |   |
|   |                    v Read Cache Reads                  |              |   |
|   |   +----------------------------------+                 |              |   |
|   |   |   High-Performance Redis Cache   |                 |              |   ||[cite: 1]
|   |   | - Hashes, Streams, TimeSeries    |                 |              |   |   |
|   |   +----------------^-----------------+                 |              |   |   |
|   |                    |                                   |              |   |   |
|   |                    | Standardized Populates            |              |   |   |
|   |   +----------------+-----------------+                 |              |   |   |
|   |   |  ConcurrentToken Pool Executors |                 |              |   |   |
|   |   | - Ingestion Worker Routines      |                 |              |   |   |
|   +---+----------------+-----------------+-----------------+--------------+---+   |
+------------------------|-----------------------------------|----------------------+
                         |                                   |
                         | Asynchronous gNMI Streams         | Forced Synchronous
                         | & Telemetry Polling Blocks        | gRPC / gNMI Bypass
                         v                                   v
+-----------------------------------------------------------------------------------+
|                             PROTOBUF-DRIVEN FABRIC TIER                           |
+------------------------+-----------------------------------|----------------------+
                         |                                   |
         +---------------+---------------+                   |
         |                               |                   |
         v (gRPC / gNMI Node Channel)    v                   v
+----------------------------------------+-------------------+----------------------+
|                           Managed DASH Appliance Fleet                            |
|                                                                                   |
|  +-----------------------------------+   +-------------------------------------+  |
|  |        DASH Appliance 01          |   |          DASH Appliance 10          |  |[cite: 1]
|  |                                   |   |                                     |  |
|  | +-------------------------------+ |   | +---------------------------------+ |  |
|  | |   Local Agent (gRPC Server)   | |   | |   Local Agent (gRPC Server)     | |  |
|  | +---------------+---------------+ |   | +---------------+-----------------+ |  |
|  |                 |                 |   |                 | inner execution |  |
|  |                 v Native API Calls|   |                 v                     |  |
|  | +-------------------------------+ |   | +---------------------------------+ |  |
|  | |   DASH-SAI / DPU P4 Pipeline  | |   | |   DASH-SAI / DPU P4 Pipeline    | |  |[cite: 1]
|  | +-------------------------------+ |   | +---------------------------------+ |  |
|  +-----------------------------------+   +-------------------------------------+  |
+-----------------------------------------------------------------------------------+

```

---

## 2. Component Design Details

### 2.1 Appliance Discovery and Configuration

DASHCenter tracks and registers network endpoints through two independent mechanisms:

#### Static Inventory Configuration

Operators define the target hardware landscape using a declarative manifest file (appliances.yaml) mounted directly by clidemon.

```yaml
apiVersion: dash.enterprise.io/v1
kind: ApplianceInventory
metadata:
  cluster_context: rack-04-east
items:
  - id: dpu-node-01
    management_ip: 10.100.50.11
    grpc_port: 50051
    gnmi_port: 50052
    namespace: tenant-prod
    labels:
      asic: bluefield3
      role: edge-router

```

#### Dynamic Registration Discovery

For elastically scaling environments, clidemon hosts a dedicated **Registration gRPC Service**.

1. When a new DPU or SmartNIC boots up, its local agent executes an outbound phone-home step targeting the primary DASHCenter VIP address.
2. The DPU issues a RegisterApplianceRequest(DeviceID, ManagementIP, HardwareCapabilities) payload.
3. clidemon validates the device credentials, dynamically provisions a new tracking profile context, updates the active asset index, and starts its background ingestion worker loops.

---

### 2.2 State Polling and Storage in the Redis Cache

DASHCenter translates high-frequency stateful Protobuf streams into structured, queryable cache formats inside a central Redis instance.

```
+-----------------------------------------------------------------------------------+
|                             STATE POLL & STORAGE PATH                             |
+-----------------------------------------------------------------------------------+
 [DASH DPU Hardware] 
        │
        ▼ (Protobuf Stream Engine)
 [gNMI Subscribe Response / gRPC Table Enums]
        │
        ▼
 [clidemon Normalizer Modules]
        │
        ├─► Configuration Models  ──► Redis Hashes [HSET] (RediSearch Indexed)
        ├─► High-Velocity Flows   ──► Redis Hashes + Short TTLs
        └─► Table Statistics/Drops──► RedisTimeSeries [TS.ADD]

```

#### A. Configuration Objects (ENIs, VNETs, Routing Rules)

* **Polling Mechanism:** On startup, clidemon establishes an asynchronous gNMI subscription (gnmi.Subscribe()) with a mode of TARGET_DEFINED or ON_CHANGE targeting configuration models like /dash/eni/ or /dash/vnet/.


* **Redis Target Schema:** Configuration objects map directly to **Redis Hashes**. The key space is explicitly segmented to enable fast lookups.


* *Key Format:* appliance:<appliance_id>:config:<object_type>:<object_id>
* *Example:* HSET appliance:dpu-node-01:config:eni:vnic-101 bandwidth_min 10G bandwidth_max 40G vnet_context vnet-prod
* *Indexing:* **RediSearch** continuously watches these namespaces, building secondary indexes over attributes like vnet_context or namespace to facilitate advanced multi-clause grouping.





#### B. Network Flows (Stateful Session Tracks)

* **Polling Mechanism:** Active network flows undergo high-velocity adjustments that are too volatile for standard gNMI paths. Instead, the daemon establishes a long-lived bidirectional gRPC stream link (rpc StreamFlows(FlowStreamRequest) returns (stream FlowStreamResponse)) directly with the hardware agent.
* **Redis Target Schema:** Active connections map to **Redis Hashes backed by fractional time-to-live values (TTLs)**.
* *Key Format:* appliance:<appliance_id>:flow:<5_tuple_hash>
* *Example Fields:* src_ip=10.0.0.4 dst_ip=20.0.0.8 proto=tcp state=ESTABLISHED bytes_transferred=412090
* *Eviction Strategy:* To protect memory from saturation, flow records feature an automatic aging window (e.g., a 30-second sliding expiration). If a flow update is not re-streamed before the window expires, it automatically drops out of the local cache.



#### C. Tables & Counters (ASIC Pipelines, Metrics, Drop Vectors)

* **Polling Mechanism:** Periodic gNMI polling tasks execute every 1000ms targeting operational data fields like /dash/counters/ or /dash/table-utilization/.


* **Redis Target Schema:** Numeric telemetry targets are fed straight into **RedisTimeSeries** structures to track performance history over time.


* *Key Format:* appliance:<appliance_id>:metric:<counter_name>
* *Example Usage:* TS.ADD appliance:dpu-node-01:metric:acl_drop_count 1774395210 412 (timestamp followed by raw counter metrics).



---

### 2.3 Multi-Appliance Management and Segregation

To scale horizontally up to 10 appliances without running into data corruption, lock-ups, or cross-node contamination, clidemon implements clear logical boundaries:

```
                     +---------------------------------------+
                     |    clidemon Multi-Channel Context     |
                     +-------------------+-------------------+
                                         |
               +-------------------------+-------------------------+
               |                                                   |
               v Core Worker Process 01                            v Core Worker Process 02
+---------------------------------------+           +---------------------------------------+
|  Thread Context: [dpu-node-01]        |           |  Thread Context: [dpu-node-02]        |
| - Dedicated gRPC Client Connection    |           | - Dedicated gRPC Client Connection    |
| - Isolated Ingestion Async Loop       |           | - Isolated Ingestion Async Loop       |
| - Writes to: `appliance:dpu-node-01:*`|           | - Writes to: `appliance:dpu-node-02:*`|
+---------------------------------------+           +---------------------------------------+

```

* **Thread Context Isolation:** The management software spawns an isolated execution workspace or async coroutine worker for every individual DPU entry registered in its active profile matrix. Network timeouts or channel breaks on one device cannot stall the ingestion processing loops of neighboring appliances.


* **Strict Key-Prefix Segmentation:** All stored properties require a hard-coded device identifier prefix (appliance::*) as the initial key token. Cross-node scanning tasks are strictly prohibited from modifying data outside their explicitly assigned namespace boundaries.
* **Aggregated Query Tag Filtering:** When building secondary query collections using RediSearch, every indexed property automatically inherits an implicit appliance_id tag field. This structural sorting lets the engine filter specific node states cleanly or group multi-node infrastructure views without needing complex data joins.



---

### 2.4 CLI Cache-First Presentation Mechanics (dashctl)

When an operator queries system data under standard workflows, the CLI operates on a high-speed, local cache-first read pathway:

```
 [User Terminal]                     [clidemon API Gateway]                 [Central Redis Cache]
        │                                      │                                       │
        │ 1. dashctl get enis --namespace=prod │                                       │
        ├─────────────────────────────────────►│                                       │
        │                                      │ 2. Execute RediSearch Query           │
        │                                      ├──────────────────────────────────────►│
        │                                      │                                       │
        │                                      │ 3. Return JSON Document Array         │
        │                                      │◄──────────────────────────────────────┤
        │ 4. Streams Formatted Output Array    │                                       │
        │◄─────────────────────────────────────┤                                       │

```

1. **The Request Pass:** The user fires a command such as dashctl get enis --namespace=production. The client sends a lightweight REST or gRPC read request to the central clidemon API endpoint.


2. **Cache Interception:** clidemon intercepts the query request parameters, skips any direct network polling to the remote appliances, and translates the client filtering conditions into a single RediSearch optimization statement:
FT.SEARCH idx:config "@namespace:{production} @object_type:{eni}"


3. **High-Speed Execution:** Redis executes the lookup across memory in microseconds, compiling the matching structural parameters into a flat JSON payload array.


4. **Formatting Output:** clidemon returns the cached structure array back to the active client shell interface. The dashctl binary converts the raw JSON array into clear, readable terminal tables, custom YAML layouts, or JSON arrays depending on the user's selected flags.



---

### 2.5 Cache Bypass and Forced Live Queries

When troubleshooting sensitive real-time issues, an operator can explicit bypass the central cache to force a live, synchronous read directly from the target hardware data plane.

```
 [User Terminal]               [clidemon API Gateway]           [Local DPU Agent]         [ASIC Pipeline]
        │                                │                              │                        │
        │ 1. dashctl get enis --live     │                              │                        │
        ├───────────────────────────────►│                              │                        │
        │                                │ 2. Synchronous gRPC Get()    │                        │
        │                                ├─────────────────────────────►│                        │
        │                                │                              │ 3. Fetch Hardware State│
        │                                │                              ├───────────────────────►│
        │                                │                              │                        │
        │                                │                              │ 4. Return Raw State    │
        │                                │                              │◄───────────────────────┤
        │                                │ 5. Local Agent Translates    │                        │
        │                                │    Protobuf State Array      │                        │
        │                                │◄─────────────────────────────┤                        │
        │ 6. Render Live Tabular Screen  │                              │                        │
        │◄───────────────────────────────┤                              │                        │
        │                                │                              │                        │
        │                                │ (Asynchronous Write-Through) │                        │
        │                                ├──────────────────────────────┼─────────────────────────► [Updates Cache]

```

1. **The Live Flag Trigger:** The user runs a command containing an explicit bypass instruction, such as dashctl get enis --live or dashctl get routes --no-cache.
2. **Bypass Routing Path:** The dashctl client embeds a custom routing flag metadata header (X-Bypass-Cache: true) into the outbound request envelope.
3. **Synchronous Direct Query:** Upon catching the bypass metadata flag, clidemon temporarily halts its default cache lookup path. It locates the target appliance's active channel context pool, opens a direct synchronous connection window, and passes a direct gRPC Get() or gNMI query down to the appliance's local agent.
4. **Hardware State Fetch:** The local DPU agent queries its underlying DASH-SAI table structures and operational registers, collecting fresh data straight from the physical hardware layer.


5. **Data Return & Sync Cache Update:** The live hardware parameters are packaged back up into a Protobuf message payload and returned up to clidemon.
6. **Delivery and Cache Refresh:** clidemon runs an inline schema translation and returns the fresh metrics back to the waiting user terminal screen within the target 500ms response window. Concurrently, a background process spawns an asynchronous **Write-Through update task** to refresh the stale data records within the central Redis instance, bringing the database cache back into alignment with the live hardware state.



---