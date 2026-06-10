# 🔍 DashCenter vs SONiC DASH Spec — Full Alignment Audit

> Date: 2026-06-10
> Inputs: this repo (`src/impl-go/dash-sim/`, `src/impl-go/dashd/`) +
> [`sonic-net/DASH`](https://github.com/sonic-net/DASH) (P4 behavioral model, SAI specs, HLDs) +
> [`sonic-net/sonic-dash-api`](https://github.com/sonic-net/sonic-dash-api/tree/master/proto)

---

## Executive Summary

**Overall alignment: ~45% (simulator) / ~60% (daemon/management plane)**

Our **APP_DB object model** is excellently aligned. Our **management plane (dashd)** is architecturally sound but is a fleet orchestrator (not a SONiC component). Our **simulator pipeline** covers the basic outbound/inbound happy path but is missing the majority of the DASH behavioral model's stateful features.

---

## 1. OBJECT MODEL ALIGNMENT

### ✅ ALIGNED (9/10)

| Area | DashCenter | SONIC DASH Spec | Status |
|------|-----------|-----------------|--------|
| **29 object kinds registered** | All 29 in `kinds.go` | All present in `sonic-dash-api` protos | ✅ Exact match |
| **Upstream protos used verbatim** | Vendored from `sonic-dash-api` | — | ✅ Best practice |
| **Key encoding** | `DASH_<KIND>_TABLE:<joined-key>` | SONiC APP_DB convention | ✅ Wire-compatible |
| **ENI** | Key=eni name, admin_state, vnet, mac | Key=MAC, admin_state, vnet_id, 40+ attrs | ✅ Proto aligned |
| **VNET** | vni, guid, address_space, peer_list | Same | ✅ |
| **ACL Group/Rule/In/Out** | All present with correct key structure | Same | ✅ |
| **Route/RouteGroup/EniRoute** | group_id + prefix LPM, routing_type enum | Same | ✅ |
| **RouteRule** | eni+vni+prefix+priority | Same | ✅ |
| **VnetMapping** | vnet+ip, underlay_ip, use_dst_vni, mac | Same + tunnel, port_map, metering | ✅ |
| **HA objects** | ha_scope, ha_scope_config/state, ha_set, ha_set_config/state | Same | ✅ Shape match |
| **Tunnel** | Present in kinds | endpoints[], encap_type, vni | ✅ |
| **Meter/MeterPolicy/MeterRule** | Present in kinds | Same | ✅ |
| **PA Validation** | Present in kinds | Same | ✅ |
| **QoS** | Present in kinds | Same | ✅ |
| **PrefixTag** | Present in kinds | Same | ✅ |
| **OutboundPortMap/Range** | Present in kinds | Same | ✅ |
| **RoutingAppliance** | Present in kinds | Same | ✅ |

### ⚠️ MISSING SAI OBJECTS (not in DashCenter at all)

| SONIC DASH Object | Description | Impact |
|---|---|---|
| **VIP** (`dash_vip`) | VIP table for pre-pipeline check | No VIP validation in simulator |
| **Direction Lookup** (`dash_direction_lookup`) | VNI → direction (OUTBOUND/INBOUND) | Simulator uses explicit `direction` field in Packet instead |
| **ENI Ether Address Map** (`eni_ether_address_map`) | MAC → ENI ID mapping table | Simulator does linear scan of all ENIs |
| **Flow Table** (`dash_flow_table`) | Flow table config (max_flow_count, TTL, enabled_key) | ❌ No flow tables |
| **Flow Entry** (`dash_flow`) | Per-flow state with 8-tuple key | ❌ No flows |
| **Flow Bulk Get Session/Filter** | Bulk flow export sessions | ❌ No flows |
| **Trusted VNI** (`dash_trusted_vni`) | Global + per-ENI trusted VNI ranges | ❌ Not modeled |
| **Underlay Route** (`route`) | Underlay LPM routing | ❌ Not modeled |

---

## 2. PACKET PROCESSING PIPELINE ALIGNMENT

### SONIC DASH Full Pipeline (20+ stages):
```
PACKET → VIP check → Direction Lookup (VNI) → ENI Lookup (MAC) →
ENI Attrs → Admin State check → Counter update → Tunnel Decap →
[Flow Table lookup → Flow Entry lookup] →
HA Stage (scope/set/role) →
[If flow miss]: Trusted VNI → ACL Group validate →
  OUTBOUND: Conntrack → ACL stages 1-3 → Outbound Routing (LPM) →
            CA-to-PA Mapping → Port Map → Pre-routing action apply
  INBOUND:  Conntrack → ACL stages 1-3 → Inbound Routing (VNI+SIP) →
            PA Validation → Tunnel Encap to VM
→ Routing Action Apply (deferred bitmask) →
Underlay Routing (LPM) → DSCP handling → Metering → Counter update
```

### DashCenter `dash-sim` Pipeline:
```
OUTBOUND: ENI lookup (by name) → ACL_OUT stages 1-5 →
          eni_route → route_group LPM → route action →
          [VNET: vnet_mapping lookup → ENCAP decision]
INBOUND:  ENI lookup (by name or MAC scan) → route_rule (priority) →
          ACL_IN stages 1-5 → FORWARD
```

### Stage-by-Stage Deviation Matrix

| Pipeline Stage | SONIC DASH | DashCenter dash-sim | Deviation |
|---|---|---|---|
| **VIP Lookup** | `vip` table, exact match on underlay DIP, drops on miss | ❌ Not implemented | Missing: no VIP validation |
| **Direction Lookup** | `direction_lookup` table, VNI → direction | ❌ Uses explicit `Packet.direction` field | Missing: real direction is derived from VNI |
| **ENI Lookup** | `eni_ether_address_map`, exact MAC match, O(1) | Linear scan of all ENIs by MAC, or explicit `Packet.eni` name | Deviation: O(n) scan vs O(1) table; key is name not MAC |
| **ENI Attrs Load** | Sets 40+ attrs into metadata (ACL groups, HA scope, flow table, metering, DSCP, PL, vm_underlay_dip, vm_vni, mode) | Loads proto; only reads `admin_state`, `vnet`, `mac_address`, ACL bindings via separate `acl_in/acl_out` objects | Missing: HA scope binding, flow table ID, metering policy, DSCP mode, PL fields, vm_vni, vm_underlay_dip, outbound_routing_group_id |
| **Admin State Check** | Drop if `admin_state == 0` | ✅ Checks `STATE_ENABLED` | ✅ Aligned |
| **ENI RX Counter** | `eni_rx`, `eni_outbound_rx`/`eni_inbound_rx` incremented | ❌ Only synthetic FNV-hash counters on encap | Missing: real per-direction traffic counters |
| **Tunnel Decap** | Strips outer VXLAN/NvGRE headers, resets tunnel pointer | ❌ Not implemented | Missing: no decapsulation modeled |
| **Flow Table Lookup** | Reads flow_table config (max_flow_count, TTL, enabled_key) | ❌ Not implemented | **CRITICAL GAP** |
| **Flow Entry Lookup** | 8-tuple exact match → cached transforms/encap/overlay rewrite | ❌ Not implemented | **CRITICAL GAP** |
| **HA Stage** | ha_scope → ha_set, populates role/peer/DP-channel metadata | ❌ Not implemented | **CRITICAL GAP** |
| **Trusted VNI** | Global + per-ENI VNI range check, drop untrusted | ❌ Not implemented | Missing |
| **ACL Group Validate** | Validates packet IP version matches group's `ip_addr_family` | ✅ Uses `isIPv4()` to select v4/v6 group ID | ✅ Functionally aligned |
| **ACL Stages** | P4 instantiates stages 1-3 (metadata has 1-5) | ✅ Implements stages 1-5 | ✅ Actually exceeds bmv2 (which only does 1-3) |
| **ACL Actions** | `permit`, `permit_and_continue`, `deny`, `deny_and_continue` | `ALLOW` (with `terminating` flag), `DENY` | ⚠️ Partial: no `deny_and_continue` |
| **ACL Tag Matching** | `src_tag[]`, `dst_tag[]` fields match against `prefix_tag` objects | ❌ Not implemented (code comment: "tag matching not implemented") | Missing: prefix tag resolution |
| **Outbound Routing** | Uses `outbound_routing_group_id` from ENI attrs, then LPM with `is_overlay_ip_v6` as key | Uses `eni_route` → `route_group` → LPM on dst_ip only | ⚠️ Deviation: different indirection (eni_route vs outbound_routing_group_id); no IPv6 overlay discrimination |
| **Routing Types** | DROP, VNET, VNET_DIRECT, DIRECT, SERVICETUNNEL, VNET_ENCAP, PRIVATELINK | DROP, VNET, VNET_DIRECT, VNET_ENCAP, DIRECT, SERVICETUNNEL, APPLIANCE | ⚠️ Missing: PRIVATELINK; APPLIANCE lookup simplified |
| **CA→PA Mapping** | Exact match on (dst_vnet_id, is_v6, lookup_ip), sets tunnel/overlay/metering | Exact match on (vnet, ip), reads underlay_ip + use_dst_vni | ⚠️ Partial: no overlay MAC rewrite, no metering, no tunnel ref, no PL mapping |
| **VNI Resolution** | `vnet` table lookup for VNI; `use_dst_vnet_vni` logic | `vnetVNI()` is **stubbed — always returns 0** | ❌ Broken: VNI never resolved |
| **Outbound Port Map** | Private Link SNAT port remapping | ❌ Not implemented | Missing |
| **Inbound Routing** | `inbound_routing` table: (eni_id, vni, underlay_sip ternary) → tunnel_decap/pa_validate/drop | `route_rule` lookup: (eni, vni, src_ip prefix) → action_type (DECAP/DROP) | ⚠️ Partial: no PA validation trigger, no ternary SIP match |
| **PA Validation** | `pa_validation` table: (vnet_id, underlay_sip exact) → permit/drop | ❌ Not implemented in pipeline | Missing: no PA validation |
| **Inbound Encap to VM** | Always VXLAN encap with `vm_underlay_dip` + `vm_vni` from ENI | Returns `FORWARD` decision with no encap | ❌ Missing: no re-encapsulation to VM |
| **Routing Action Apply** | Deferred bitmask: ENCAP_U0, ENCAP_U1, SET_SMAC, SET_DMAC, SNAT, DNAT, NAT46, NAT64, SNAT_PORT, DNAT_PORT | No deferred action model; immediate decision | ❌ Missing: entire transform engine |
| **Underlay Routing** | LPM on outer dst_ip → next_hop | ❌ Not modeled | Missing |
| **DSCP Handling** | PRESERVE or PIPE mode from ENI attrs | ❌ Not modeled | Missing |
| **Metering** | meter_class computation (OR/AND), policy/rule fallback, bucket update | ❌ Not modeled | Missing |
| **TX Counters** | `eni_tx`, direction-specific TX counters | ❌ Synthetic only | Missing |

---

## 3. PACKET TRANSFORMS

| Transform | SONIC DASH Spec | DashCenter | Status |
|---|---|---|---|
| **VXLAN Encap (U0)** | Full outer headers: Eth(SMAC/DMAC) + IPv4(SIP/DIP) + UDP(4789) + VXLAN(VNI) | Records `out_underlay_ip` + `out_vni` in Decision; no actual packet | ❌ No real transform |
| **NvGRE Encap** | Eth + IPv4 + GRE(0x2f) + NvGRE(VSID) | ❌ Not supported | ❌ |
| **Dual-layer Encap (U0+U1)** | Two encap layers via tunnel_pointer increment | ❌ Not supported | ❌ |
| **VXLAN Decap** | Strip outer Eth/IP/UDP/VXLAN headers | ❌ Not modeled | ❌ |
| **Overlay MAC Rewrite** | `push_action_set_dmac(dmac)` from CA→PA mapping | ❌ Not modeled | ❌ |
| **NAT46 (4→6)** | SIP/DIP bitmask encoding: `ipv6 = (ipv4 & ~mask) \| (prefix & mask)` | ❌ Not modeled | ❌ |
| **NAT64 (6→4)** | Reverse decode | ❌ Not modeled | ❌ |
| **L3 SNAT/DNAT** | IP address rewrite | ❌ Not modeled | ❌ |
| **L4 SNAT/DNAT (port)** | Port rewrite with port-base arithmetic | ❌ Not modeled | ❌ |
| **Service Tunnel Encode** | IPv4 inner → IPv6 outer with bitmask | Records underlay DIP only | ❌ Partial |
| **DSCP Preserve/Pipe** | Copy or override DSCP in outer header | ❌ Not modeled | ❌ |
| **Reverse Tunnel** | Compute inverse transform for return traffic | ❌ Not modeled | ❌ |
| **Transformed packet output** | Full wire-format packet with all headers | Returns `Decision` struct only | ❌ |

---

## 4. FLOW TABLE / CONNECTION TRACKING

| Feature | SONIC DASH Spec | DashCenter | Status |
|---|---|---|---|
| **Flow Table object** | Configurable: max_flow_count, TTL, enabled_key bitmask | ❌ Not implemented | ❌ |
| **Flow Entry** | 8-tuple key, cached transforms, reverse flow key, sync state | ❌ Not implemented | ❌ |
| **Slow Path** | First packet → full policy eval → create flow | Every packet = slow path | ❌ |
| **Fast Path** | Subsequent packets → flow lookup → apply cached transforms | ❌ Not implemented | ❌ |
| **Flow Sync State Machine** | 5 states: MISS→CREATED→SYNCED→PENDING_DELETE→PENDING_RESIMULATION | ❌ Not implemented | ❌ |
| **TCP State Tracking** | SYN/SYN_ACK/FIN/RST → flow lifecycle | ❌ Not implemented | ❌ |
| **Flow Age-out** | TTL-based expiry | ❌ Not implemented | ❌ |
| **Flow CRUD APIs** | Create/Get/Set/Delete/BulkCreate/BulkRemove/BulkGet | ❌ Not implemented | ❌ |
| **Flow Resimulation** | `full_flow_resimulation_requested` on ENI; per-mapping resim trigger | ❌ Not implemented | ❌ |
| **Flow Bulk Export** | Sessions with filters, GRPC/EVENT modes | ❌ Not implemented | ❌ |

**Impact: This is the single largest gap.** Without flows, HA can't work (HA = flow replication), resimulation can't work, fast-path can't be tested, per-flow counters can't exist.

---

## 5. HIGH AVAILABILITY

| Feature | SONIC DASH Spec | DashCenter | Status |
|---|---|---|---|
| **HA Scope binding to ENI** | `SAI_ENI_ATTR_HA_SCOPE_ID`, `is_ha_flow_owner` | ❌ Proto types exist but pipeline ignores them | ❌ |
| **HA State Machine** | 13 states (DEAD→CONNECTING→...→ACTIVE/STANDBY→SWITCHING_TO_STANDALONE) | ❌ Not implemented | ❌ |
| **DP-channel probe protocol** | BFD-like: DP_PROBE_REQ/ACK, configurable interval + fail threshold | ❌ Not implemented | ❌ |
| **Inline flow sync** | FLOW_SYNC_REQ/ACK via DASH metadata header + tunnel | ❌ Not implemented | ❌ |
| **Bulk sync** | CP data channel (TCP), "Perfect Sync" colour-flip algorithm | ❌ Not implemented | ❌ |
| **Switchover** (planned) | SDN-driven role transition, flow ownership transfer | ❌ Not implemented | ❌ |
| **Failover** (unplanned) | DP-probe timeout → SWITCHING_TO_STANDALONE | ❌ Not implemented | ❌ |
| **Flow reconciliation** | On re-pair, re-evaluate all flows against current policy | ❌ Not implemented | ❌ |
| **HA Counters** | dp_probe_req/ack rx/tx, bulk_sync counts, inline sync counts | ❌ Not implemented | ❌ |

---

## 6. COUNTERS

| Feature | SONIC DASH Spec | DashCenter | Status |
|---|---|---|---|
| **Per-ENI traffic** | `eni_rx/tx`, `eni_outbound_rx/tx`, `eni_inbound_rx/tx` | Synthetic FNV-hash: `packetsIn/Out, bytesIn/Out, drops` | ❌ Not real |
| **Per-ENI flow lifecycle** | flow_created/updated/deleted/aged/resimulated (11 counters) | ❌ | ❌ |
| **Per-ENI flow sync** | 24+ inline/timed sync req/ack counters | ❌ | ❌ |
| **Per-ENI drop reasons** | outbound_routing_miss, ca_pa_miss, inbound_routing_miss, routing_group_miss/disabled, port_map_miss, trusted_vni_miss | ❌ | ❌ |
| **Port-level counters** | vip_miss_drop, eni_miss_drop | ❌ | ❌ |
| **Metering buckets** | Per (eni, meter_class) byte counters, inbound/outbound split | ❌ | ❌ |
| **HA set counters** | dp_probe counts, cp_data_channel counts, bulk_sync counts | ❌ | ❌ |

---

## 7. DASHD (DAEMON) ALIGNMENT

The daemon (`dashd`) is **not a SONiC component** — it's a fleet management controller. It doesn't need to align with the DASH behavioral model. However, here's how it relates:

| Area | Status | Notes |
|---|---|---|
| **Desired-state → DPU reconciliation** | ✅ Well designed | Level-driven, per-DPU workers, diff-based |
| **Southbound via dashapi.v1** | ✅ Correct | Apply/Delete/Subscribe to dash-sim |
| **Northbound proto (dashcenter.v1)** | ✅ Distinct API | Appropriate separation from SAI/DASH API |
| **HA/Leader election** | ⚠️ Stub only | `NoneElector` (always leader); etcd backend blocked |
| **ENI Migration** | ⚠️ Proto-only | Rich migration state machine in proto but no implementation |
| **DPU Inventory/Probing** | ✅ Functional | Lifecycle: REGISTERING→UP→UNREACHABLE, exponential backoff |
| **Drift Detection** | ✅ Functional | Observed vs desired diff per DPU |
| **Diagnostics** | ⚠️ Proto-only | TraceFlow, ExplainMatch, AclHitStats defined but not implemented |
| **Batch Apply / Dry-run** | ⚠️ Proto-only | `ApplyBatch`, `SimulateApply` defined but not implemented |
| **gNMI bridge** | ❌ Not implemented | Required for real SONiC controller integration |

---

## 8. CRITICAL DEVIATIONS SUMMARY (Ranked by Impact)

### 🔴 CRITICAL (blocks production use / integration testing)

1. **No flow table / conntrack** — Every packet is slow-path. Can't test fast-path (99% of real traffic). Can't model HA. Can't do resimulation. Score: **0/10**

2. **No packet transforms** — Returns a `Decision` struct, not a transformed packet. Can't run conformance tests ("send X, expect Y at wire"). Score: **2/10**

3. **No HA implementation** — Proto types exist but are inert. No state machine, no flow sync, no DP probes, no switchover/failover. This is the #1 feature in the DASH community right now. Score: **1/10**

4. **VNI resolution stubbed** — `vnetVNI()` at `pipeline.go:512-517` always returns 0. Every VXLAN encap has VNI=0. Score: **0/10** for this specific feature.

### 🟡 SIGNIFICANT (affects accuracy / completeness)

5. **No VIP/Direction Lookup/ENI MAC map** — Skips 3 pre-pipeline stages. Direction is explicit, not derived from VNI. ENI is by name, not MAC table lookup.

6. **No PA Validation** — Inbound routing doesn't validate source PA against VNET mapping.

7. **No metering** — No meter_class computation, no policy/rule lookup, no bucket accounting.

8. **No trusted VNI checking** — Untrusted VNIs are not dropped.

9. **Pipeline is single-stage / hard-coded** — No multi-stage transitions, no `stage_index`, no deferred action bitmask model.

10. **Counters are synthetic** — FNV-hash derived, not event-driven. Wrong shape entirely (5 vs 50+).

11. **ACL tag matching not implemented** — `src_tag[]` / `dst_tag[]` fields ignored.

12. **No inbound re-encapsulation** — Inbound returns FORWARD without VXLAN encap to VM (needs `vm_underlay_dip` + `vm_vni`).

### 🟢 MINOR / ACCEPTABLE

13. **ACL implements 5 stages** — Exceeds bmv2 (which only does 1-3). Matches SAI spec intent.

14. **`deny_and_continue` ACL action missing** — Rare edge case.

15. **No NvGRE** — Less common than VXLAN.

16. **No DSCP handling** — Cosmetic for simulation.

---

## 9. WHAT IS WELL ALIGNED ✅

1. **29 object kinds** — Exact match with upstream `sonic-dash-api`
2. **Upstream protos used verbatim** — Best practice, no forking
3. **APP_DB key encoding** — Wire-compatible with SONiC orchagent
4. **Generic CRUD + Subscribe** — Correct APP_DB abstraction
5. **ACL evaluation logic** — 5-tuple matching, priority ordering, terminating semantics
6. **Route LPM** — Longest-prefix-match on dst_ip, correct sorting
7. **Outbound route action dispatch** — DROP/DIRECT/VNET/VNET_DIRECT/SERVICETUNNEL/APPLIANCE
8. **Inbound route_rule** — Priority-ordered matching on (eni, vni, src_prefix)
9. **Scenario loader** — YAML → store, equivalent to DASH PTF fixtures
10. **Fault injection** — Per-RPC fault injection (unique to this simulator)
11. **Admin HTTP** — Health/dump/reset/faults/counters/kinds
12. **dashd reconciliation model** — Sound desired-state → observed-state diff architecture
13. **Multi-DPU topology support** — Already ships multi-container test infra

---

## 10. RECOMMENDATION

The existing audit doc (`docs/dash-sim-on-par-with-sonic-audit.md`) is **accurate and comprehensive**. The scores it assigns match this independent analysis. The phased roadmap it proposes (P0: conformance hardening → P1: transforms → P2: flows → P3: HA → P4: counters → P5: gNMI → P6: conformance suite) is the right order.

**The most impactful single improvement** would be implementing the flow table + slow/fast path split (Phase 2 in the roadmap), because it unblocks HA, resimulation, real counters, and fast-path testing. But it depends on real packet transforms (Phase 1).

The daemon (`dashd`) is architecturally sound for its role as a fleet orchestrator and doesn't need to mirror the DASH behavioral model — that's `dash-sim`'s job.

---

## Appendix: Score Card

| Layer | Score | Notes |
|---|---|---|
| APP_DB object model (29 kinds) | **9/10** ✅ | All kinds, correct keys, upstream protos |
| Subscribe/event bus | **6/10** ⚠️ | In-process pub/sub vs gNMI streaming |
| Outbound pipeline | **4/10** ⚠️ | Single-stage, no transforms, no multi-stage |
| Inbound pipeline | **5/10** ⚠️ | No decap, no PA validation, no re-encap |
| Packet transforms | **2/10** ❌ | Decision struct only, no wire-format output |
| Flow table / conntrack | **0/10** ❌ | Not implemented |
| Flow lifecycle | **0/10** ❌ | Not implemented |
| High Availability | **1/10** ❌ | Proto types only, no behavior |
| Counters | **2/10** ❌ | Synthetic FNV, wrong shape |
| Admin/control surface | **5/10** ⚠️ | HTTP admin, no gNMI |
| Testability hooks | **6/10** ✅ | Fault injector + scenarios + simulate --trace |
| Multi-DPU topology | **7/10** ✅ | Multi-container infra ships |
| **Overall** | **~45%** | Excellent APP_DB sim, toy pipeline |
