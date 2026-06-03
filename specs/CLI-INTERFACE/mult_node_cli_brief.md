
---

## 1. Appliance & Cluster Commands

### dashctl get appliances

* **Signature:** dashctl get appliances [-o wide | json | yaml]
* **Description:** Retrieves the high-level status, management IPs, platform type, and active session scale across the entire monitored fleet.


* **Data Path:** Fetches aggregated runtime metrics from the local Redis data cache.



#### Sample Output:

```text
ID           ROLE      MANAGEMENT-IP   PLATFORM              UPTIME      ACTIVE-FLOWS   STATUS
dpu-node-01  LEADER    10.50.100.11    Mellanox_BlueField_3  13d:22h:45m 43,220         HEALTHY
dpu-node-02  FOLLOWER  10.50.100.12    Mellanox_BlueField_3  13d:22h:45m 39,100         HEALTHY
dpu-node-03  FOLLOWER  10.50.100.13    AMD_Pensando_Elba     4d:02h:11m  82,400         HEALTHY
dpu-node-04  FOLLOWER  10.50.100.14    Marvell_Octeon_10     0s          0              UNREACHABLE

```

---

### dashctl describe appliance 

* **Signature:** dashctl describe appliance 
* **Description:** Inspects a single DPU node deeply, breaking down localized hardware limits, memory utilization, and active gRPC/gNMI stream channels.


* **Data Path:** Combines configuration metadata hashes and real-time operational metrics from the cache.



#### Sample Output:

```text
Name:             dpu-node-01
Role:             LEADER[cite: 1]
Management IP:    10.50.100.11[cite: 1]
Platform:         Mellanox_BlueField_3[cite: 1]
Status:           HEALTHY[cite: 1]
Namespace Scope:  tenant-prod[cite: 1]

Hardware Table Utilization:
  DASH_ENI_TABLE:         4 / 128         (3.1%)
  DASH_VNET_TABLE:        12 / 1024       (1.1%)
  DASH_ACL_RULE_TABLE:    45,210 / 500,000(9.0%)
  DASH_ROUTE_TABLE:       120,442 / 1,000,000 (12.0%)

Inbound Subscriptions:
  gNMI Subscription:      ACTIVE (Target-Defined Streaming)[cite: 1]
  Flows gRPC Stream:      ACTIVE (Bidirectional Fast-Path)[cite: 1]

```

---

## 2. DASH Core Object Commands

### dashctl get enis

* **Signature:** dashctl get enis [--all-devices] [-n ] [--live]
* **Description:** Lists the Elastic Network Interfaces mapped across host compute boundaries.


* **Data Path:** Defaults to a RediSearch scan over Redis hashes. If --live is passed, it forces a synchronous gRPC Get() down to the raw hardware agents, bypassing the cache.



#### Sample Output:

```text
DEVICE       ENI-ID         MAC-ADDRESS       VNET-CONTEXT   PPS-LIMIT   BPS-LIMIT   STATUS
dpu-node-01  eni-vnic-202   00:1A:4A:16:01:A2 vnet-prod-east 1,000,000   40 Gbps     PROVISIONED
dpu-node-01  eni-vnic-203   00:1A:4A:16:01:A3 vnet-prod-west 500,000     10 Gbps     PROVISIONED
dpu-node-02  eni-vnic-401   00:1A:4A:22:99:FF vnet-dmz        2,500,000   100 Gbps    DEGRADED

```

---

### dashctl describe acl-group

* **Signature:** dashctl describe acl-group  [-n ] [--live]
* **Description:** Dissects a high-scale security policy matrix, displaying the exact rule evaluation hierarchy and real-time hit counters.



#### Sample Output:

```text
Group ID:    sec-group-web[cite: 1]
Namespace:   frontend[cite: 1]
Appliance:   dpu-node-01

Rules Matrix:
PRIORITY   RULE-ID            ACTION   MATCH-CRITERIA                  HIT-COUNTER
10         allow-http-global  ALLOW    dst_ip: Tags(WebServers), dport: 80 1,422,904[cite: 1]
20         allow-https-global ALLOW    dst_ip: Tags(WebServers), dport: 443 8,911,022
30         deny-ssh-management DENY    src_ip: 0.0.0.0/0, dport: 22    412[cite: 1]
100        default-deny       DENY    any                             0

```

---

## 3. High-Velocity Telemetry & Stream Commands

### dashctl monitor flows

* **Signature:** dashctl monitor flows --device= [--src=] [--dst=]
* **Description:** Establishes a streaming monitor console tracking volatile stateful network connection additions, changes, and teardowns inside the DPU connection tracker (conntrack).


* **Data Path:** Hooks into long-lived Server-Sent Events (SSE) or a gRPC server stream on clidemon, reading from active Redis short-TTL hash mutations.



#### Sample Output (Real-time Stream):

```text
TIMESTAMP                 ACTION      5-TUPLE                                     STATE        PKTS/BYTES
2026-05-22T17:31:02.102Z  FLOW_NEW    10.0.1.5:49210 -> 192.168.10.2:443 [TCP]    SYN_SENT     1 / 64 B
2026-05-22T17:31:02.105Z  FLOW_UPD    10.0.1.5:49210 -> 192.168.10.2:443 [TCP]    ESTABLISHED  2 / 128 B
2026-05-22T17:31:05.890Z  FLOW_STATS  10.24.90.11:80 -> 10.100.2.4:55122 [TCP]    ESTABLISHED  44.1k / 62 MB
2026-05-22T17:31:09.411Z  FLOW_DEL    192.168.1.2:53 -> 8.8.8.8:53 [UDP]          CLOSED       2 / 142 B
^C
Stream terminated by operator.

```

---

### dashctl monitor drops

* **Signature:** dashctl monitor drops [--device=] [--follow]
* **Description:** Displays inline hardware drop attributions captured directly from the DPU's exception or copy-to-CPU queues, exposing exact pipeline-level reason codes.


* **Data Path:** Emits updates from a Redis stream populated by the local agent's zero-copy memory ring reader.



#### Sample Output:

```text
TIMESTAMP                 DEVICE       SOURCE-IP    DEST-IP      PROTO  STAGE       REASON                             OBJECT-LINEAGE
2026-05-22T17:32:00.121Z  dpu-node-01  10.1.10.5    10.200.50.20 TCP    ACL_STAGE   DASH_DROP_REASON_ACL_EGRESS_DENY   DASH_ACL_RULE_TABLE:rule-deny-ssh-management[cite: 1]
2026-05-22T17:32:04.891Z  dpu-node-03  192.168.2.4  172.16.0.100 UDP    ROUTE_STAGE DASH_DROP_REASON_NO_ROUTE_FOUND    DASH_ROUTE_TABLE:LPM_MISS
2026-05-22T17:32:05.412Z  dpu-node-01  10.0.0.4     20.0.0.8     TCP    FLOW_STAGE  DASH_DROP_REASON_CONNTRACK_MISS    STATEFUL_FLOW_EVAL:TCP_WINDOW_VIOLATION[cite: 1]

```

---

## 4. Advanced Diagnostic Engine Commands

### dashctl trace packet

* **Signature:** dashctl trace packet --src= --dst= --proto=<tcp|udp> --dport= [--device=]
* **Description:** Runs an analytical pipeline dry-run simulation. It maps a virtual packet 5-tuple payload sequentially through all configuration models to show exactly how it will traverse the hardware tables.


* **Data Path:** Handled entirely by the clidemon Cache Simulator Engine using the structured JSON object array stored inside Redis.



#### Sample Output:

```text
Trace Analysis Request
----------------------------------------------------------------------
Simulated 5-Tuple:  10.1.10.5:49152 -> 10.200.50.20:80 [TCP][cite: 1]
Target Namespace:   tenant-gold[cite: 1]
Target Scope:       All Active Cluster Devices[cite: 1]

Calculated Pipeline Journey:
[STAGE 1]: ENI_LOOKUP[cite: 1]
  - Result:   MATCH[cite: 1]
  - Object:   DASH_ENI_TABLE:eni-host-vnic-01[cite: 1]
  - Metadata: Resolved source anchor virtual interface context safely.[cite: 1]

[STAGE 2]: FLOW_LOOKUP[cite: 1]
  - Result:   MISS[cite: 1]
  - Object:   None[cite: 1]
  - Metadata: No active session match found in connection tracker; routing to policy evaluation.[cite: 1]

[STAGE 3]: ROUTE_LOOKUP[cite: 1]
  - Result:   MATCH[cite: 1]
  - Object:   DASH_ROUTE_TABLE:prefix-10.200.0.0_16[cite: 1]
  - Metadata: Longest Prefix Match resolved. Next-hop tunnel target VNI mapped to 600102.[cite: 1]

[STAGE 4]: ACL_EVALUATION[cite: 1]
  - Result:   MATCH (ALLOW)[cite: 1]
  - Object:   DASH_ACL_RULE_TABLE:rule-allow-http-global[cite: 1]
  - Metadata: Packet explicitly matches firewall whitelist parameters.[cite: 1]

----------------------------------------------------------------------
FINAL VERDICT: ALLOWED[cite: 1]
Action:        Packet will be rewritten, encapsulated via VXLAN (VNI: 600102), and forwarded out.[cite: 1]

```

---

### dashctl audit consistency

* **Signature:** dashctl audit consistency [--table=] [--repair]
* **Description:** Performs a rapid cross-plane synchronization audit using local cryptographic Merkle trees to detect silent state drift between the software intent layer and physical ASIC state.


* **Data Path:** Queries the central cache for software object hashes and executes a high-speed gRPC check to compare root hashes with the physical appliances.



#### Sample Output:

```text
Executing Cross-Plane State Verification Audit...
Structuring localized Merkle trees for comparative validation.[cite: 1]

TABLE_NAME             APPLIANCE    INTENT_HASH  HARDWARE_HASH  STATUS    DRIFTED_OBJECT_IDS
DASH_ENI_TABLE         dpu-node-01  A9B267F1     A9B267F1       MATCHED   None
DASH_VNET_TABLE        dpu-node-01  E812CC44     E812CC44       MATCHED   None
DASH_ACL_RULE_TABLE    dpu-node-01  BC11402E     FF42A119       MISMATCH  DASH_ACL_RULE:gold-allow-ssh-temp
DASH_ROUTE_TABLE       dpu-node-02  39A11202     39A11202       MATCHED   None

[!] WARN: Found 1 drifted configuration object on dpu-node-01.
    The software control plane considers 'gold-allow-ssh-temp' deleted, but it remains active in the hardware ASIC table.[cite: 1]

Tip: Execute 'dashctl audit consistency --table=DASH_ACL_RULE_TABLE --repair' to force-reconcile hardware tables.[cite: 1]

```

---

### Summary of Special CLI Flag Behaviors

* -n : Limits the search context to specific isolated tenants or workloads, directly translating into query constraints inside RediSearch.


* --live / --no-cache: Forces clidemon to bypass Redis entirely. It opens a live gRPC connection window down to the selected DPU, pulls fresh data directly from the vendor's DASH-SAI layer, and streams it back before triggering an async write-through cache update.


* -w / --watch / --follow: Keeps the terminal line open, appending incoming metrics dynamically as new chunk packets arrive from the clidemon event loop.