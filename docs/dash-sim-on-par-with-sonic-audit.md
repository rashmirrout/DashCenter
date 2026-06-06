# DashCenter `dash-sim` — Alignment Audit & Roadmap to "Best-in-Class"

> Date: 2026-06-07
> Inputs: this repo (`src/impl-go/dash-sim/`) +
> [`sonic-net/DASH`](https://github.com/sonic-net/DASH) (HLDs in
> `documentation/general/`, `documentation/dataplane/`,
> `documentation/high-avail/`) +
> [`sonic-net/sonic-dash-api`](https://github.com/sonic-net/sonic-dash-api/tree/master/proto)
> + `dash-pipeline` (P4/BMv2 reference) + `dash-bmv2-data-plane-app.md`.

This is an evidence-based assessment of how close our simulator is to the
real-world DASH DPU spec, what's missing, and a phased plan to close
every gap that matters.

---

## 1. Executive summary

| Layer | Our coverage | Spec maturity | Score |
|---|---|---|---|
| **APP_DB object model (29 kinds)** | All kinds wired, CRUD via `Apply/Get/List/Delete`, protojson values, Redis-compatible joined keys | Stable upstream; we track latest `master` (commit pinned in `proto/vendor/sonic-dash-api/VERSION`) | **9/10** ✅ |
| **Subscribe/event bus** | In-process pub/sub with snapshot-first | gNMI streaming (not in-process) | **6/10** ⚠ |
| **Outbound pipeline (single-stage)** | ENI → ACL_OUT (5 stages) → eni_route → route LPM → action (DIRECT/VNET/SVC_TUNNEL/APPLIANCE) | Multi-stage (lpmrouting/maprouting/portmaprouting transitions), per-stage metadata composition, 7+ packet-transform action types | **4/10** ⚠ |
| **Inbound pipeline** | ENI lookup → route_rule (priority) → ACL_IN | Symmetric multi-stage with decap + transposition + ACL | **5/10** ⚠ |
| **Packet transforms** | Returns a "Decision" with `out_underlay_ip`/`out_vni`/`out_eni` — does NOT produce a fully transformed packet | Full overlay/underlay rewrite (SMAC/DMAC, SIP/DIP + masks, dual-underlay encap, L3/L4 NAT, 4↔6 transposition, DSCP preserve/pipe, ECMP, reverse-tunnel) | **2/10** ❌ |
| **Flow table / conntrack** | **None** | Required (DASH Flow API HLD: CRUD on flow tables, bidirectional flow keys, sync-state state machine, bulk-get sessions) | **0/10** ❌ |
| **Flow lifecycle (slow/fast path, age-out, FIN/RST teardown, resimulation)** | **None** | Required (DASH flow resimulation HLD + BMv2 DPAPP HLD) | **0/10** ❌ |
| **High Availability** | HA proto types are present in `kinds` but **the simulator ignores them** (no role state machine, no DP/CP channels, no flow sync, no bulk sync, no probe protocol) | Full HA HLD: HA set + HA scope + ENI binding, 10-state HA machine, 5-state flow sync machine, DP probe protocol, perfect-sync bulk algorithm, switchover/failover workflows | **1/10** ❌ |
| **Counters** | 5 synthetic counters per object, deterministically derived from key hash | 50+ ENI counters (TCP state, drop reasons, flow lifecycle), DP-channel probe counters, port-level miss counters, CPS rates | **2/10** ❌ |
| **Admin/control surface** | Admin HTTP (health/dump/reset/faults/scenario/counters/kinds) + fault injector per RPC | gNMI northbound, OpenConfig telemetry, syslog/event notifications, SAI capability queries | **5/10** ⚠ |
| **Testability hooks** | Fault injector + scenarios + `simulate --trace` | DASH/PTF test suite, SAI-thrift harness, P4 conformance | **6/10** ✅ |
| **Multi-DPU topology** | Multi-process / multi-container test infra (we shipped this) | DPU appliance with 6× cards, smart switch with captive DPUs | **7/10** ✅ |

**Overall alignment: ~45%.** We are an **excellent APP_DB-shape simulator**
with a **toy single-stage pipeline**. To become "the best DPU simulator",
we need to graduate from "match-every-packet-from-scratch" into a real
**stateful flow engine** with HA, multi-stage transforms, and proper
counters. None of the gaps are conceptual blockers — they're all
implementation work.

---

## 2. What we got right (worth preserving)

These are architectural decisions that match the upstream model and we
should **not** rework:

1. **Use upstream `sonic-dash-api` protos verbatim.** Our service envelope
   (`dashapi.v1.DashApi`) is small and our own; every payload is the
   upstream type. This means the second the upstream protos change, we
   re-vendor and we're current.
2. **Joined string keys mapped to Redis APP_DB.** Our key encoding
   (`DASH_<KIND>_TABLE:<joined>`) is what `dash-redis-adapter` actually
   writes to APP_DB, which is the wire format SONiC's orchagent reads.
3. **Generic Apply/Get/List/Delete/Subscribe surface.** This is the right
   abstraction for an **APP_DB** layer (which is what an SDN agent would
   see). The fact that the same CLI drives sim + adapter + (future) real
   DPU is precisely the design intent of DASH.
4. **In-memory store for sim, real Redis for adapter.** Matches the
   upstream split between behavioral model and real APP_DB.
5. **Scenario loader (YAML → store).** Equivalent in spirit to the DASH
   PTF test fixtures + `dash-reference-config-example`.

Keep all of this.

---

## 3. The actual upstream architecture (one paragraph)

Real-world DASH = **SDN controller → gNMI → SONiC `dash` container →
APP_DB → `dashorch` (in SWSS) → ASIC_DB → DASH SAI (vendor lib) →
DPU pipeline**. The DPU pipeline is a **multi-stage match-action
processor** that does **one slow path per first packet of a flow** (full
policy evaluation, sets up flow state) and then a **fast path per
subsequent packet** (single flow-table lookup → apply pre-computed
transforms). **HA is a co-processor**: every flow is replicated to a
peer DPU over a DP channel; the local DPAPP serializes flow state into a
DASH-metadata header and tunnels it. **Counters** are per-ENI,
per-flow-state, per-drop-reason and per-DP-probe — there are dozens of
them and they're how observability actually works.

The behavioural model lives in
[`DASH/dash-pipeline`](https://github.com/sonic-net/DASH/tree/main/dash-pipeline)
(P4 + BMv2 + a VPP-based DPAPP). Our `dash-sim` is the **Go-native
equivalent**, optimised for ease of integration with `dash-sim-client`,
not for P4 conformance. That's a legitimate niche to occupy — but to be
**the best**, we need the same *behaviours*, even if the implementation
is different.

---

## 4. Detailed gap analysis (ranked by impact)

### Gap 1 — No flow table / no conntrack ❌ (the single biggest gap)

**Spec source:** [`dash-flow-api.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-flow-api.md),
[`dash-bmv2-data-plane-app.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-bmv2-data-plane-app.md).

The real pipeline maintains **flow tables** keyed by
`{eni_mac, vni, ip_proto, src_ip, dst_ip, src_port, dst_port}`, with
per-flow state:

- `version`, `direction`, `action`, `meter_class`, `is_unidirectional`
- `sync_state` ∈ {FLOW_MISS, FLOW_CREATED, FLOW_SYNCED, FLOW_PENDING_DELETE, FLOW_PENDING_RESIMULATION}
- Reverse flow key (so a packet hitting the *other* direction finds its bidirectional partner)
- Two underlay encaps (`underlay0` + `underlay1`) — VNI/SIP/DIP/SMAC/DMAC/encap_type
- Overlay rewrite: dst_mac, sip/dip with masks
- Per-flow vendor metadata + a protobuf form (`SaiDashFlowEntry`)

**Why this matters in a simulator:**
- Without flows, every `simulate` call is the slow path. We can't test
  the fast path at all (which is 99% of real DPU packet processing).
- Without flows, **HA cannot be modelled** (HA *is* flow replication).
- Without flows, **resimulation cannot be modelled** (resimulation is
  re-running policy *for an existing flow*).
- Without flows, **per-flow counters and aging cannot be modelled**.

### Gap 2 — Packet transforms are decisions, not transformed packets ❌

**Spec source:** [`sdn-features-packet-transforms.md`](https://github.com/sonic-net/DASH/blob/main/documentation/general/sdn-features-packet-transforms.md),
[`dash-routing-actions.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-routing-actions.md).

Our `Decision` carries `out_eni`, `out_underlay_ip`, `out_vni`,
`out_routing_type`. The real DPU produces a **transformed packet** with:

| Transform | We have it? |
|---|---|
| Static VXLAN/NVGRE encap | partial (just records underlay+VNI in decision) |
| VXLAN encap with mapping-table lookup (CA→PA + Mac rewrite + VNI) | partial (we look up the mapping but don't write out the inner/outer headers) |
| VXLAN encap with ECMP across multiple PAs | ❌ |
| Static decap | ❌ (we just decide DELIVER) |
| Mapping-based decap | ❌ |
| L3 SNAT/DNAT | ❌ |
| L4 SNAT/DNAT (with port-pool base) | ❌ |
| 4→6 / 6→4 (IPv4↔IPv6 transposition with bit encoding/masks) | ❌ |
| Source MAC stamping/override | ❌ |
| Service Tunnel encap (overlay SIP/DIP prefix manipulation) | partial (records underlay only) |
| Dual-underlay encap (underlay0 + underlay1 stacked) | ❌ |
| DSCP preserve / pipe modes | ❌ |
| Tunnel-from-encap (copy from existing tunnel) | ❌ |
| Reverse-tunnel (apply on the reverse flow) | ❌ |
| Up to 3 levels of routing transforms (transpose+encap+encap) | ❌ |

**Why this matters:** without producing the actual transformed packet,
we can't run **conformance tests** against the real DPU's expected
output. A test like "send packet X, expect packet Y at the wire" is the
only way to verify the simulator matches hardware. We can't do that today.

### Gap 3 — No HA implementation ❌

**Spec source:** [`ha-api-hld.md`](https://github.com/sonic-net/DASH/blob/main/documentation/high-avail/ha-api-hld.md),
[`high-availability-and-scale.md`](https://github.com/sonic-net/DASH/blob/main/documentation/high-avail/high-availability-and-scale.md).

We have the **proto types** (`ha_scope`, `ha_scope_config`,
`ha_scope_state`, `ha_set`, `ha_set_config`, `ha_set_state`) but they're
inert. Specifically, our pipeline doesn't:

- Bind ENIs to HA scopes (`SAI_ENI_ATTR_HA_SCOPE_ID`,
  `SAI_ENI_ATTR_IS_HA_FLOW_OWNER`).
- Run the **10-state DPU HA state machine** (DEAD → CONNECTING →
  CONNECTED → INITIALIZING_TO_ACTIVE/STANDBY → PENDING_* → ACTIVE/STANDBY → SWITCHING_TO_STANDALONE → ...).
- Run the **5-state flow sync state machine** (FLOW_MISS → FLOW_CREATED
  → FLOW_SYNCED → FLOW_PENDING_DELETE/RESIMULATION).
- Emit/consume the **HA packet types** (FLOW_SYNC_REQ, FLOW_SYNC_ACK,
  DP_PROBE_REQ, DP_PROBE_ACK).
- Run **inline flow sync** (Active recirculates new flow → sync packet
  to Standby → ack back → mark synced).
- Run **bulk sync** with the **"Perfect Sync" colour-flip algorithm**
  (when pairing is re-established, flip colour; sync only non-current
  colour; concurrent real-time sync of new flows).
- Run **DP-channel probing** (BFD-like).
- Implement **planned switchover** and **unplanned failover** workflows.

**Why this matters:** SmartSwitch HA is the most-discussed feature in
the DASH project right now. A simulator that can't model an HA pair, a
switchover, or a flow-reconcile is **not useful for SDN-controller
integration tests** — and those are the highest-value tests.

### Gap 4 — Pipeline is single-stage / hard-coded ⚠

**Spec source:** [`dash-routing-actions.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-routing-actions.md).

The real DASH pipeline is **N stages**, each match entry can `transition`
to a named next stage. We hard-code: `ENI → ACL_OUT 1..5 → eni_route →
route_group LPM → vnet_mapping`. We can't model:

- **Multiple LPM tables per ENI** with `stage_index`.
- **`portmaprouting`** (for Private Link redirect-map scenarios).
- **`maprouting`** chained after `lpmrouting` (which is exactly how
  Private Link / Service Tunnel composes).
- **Metadata composition across stages** (e.g.
  `tunnel_from_encap_underlay0_sip` overriding a tunnel's SIP, or 4to6
  encoding masks merging via `new_value = (old & !mask) | value`).

### Gap 5 — Counters ⚠

**Spec source:** [`sdn-features-packet-transforms.md`§Counters](https://github.com/sonic-net/DASH/blob/main/documentation/general/sdn-features-packet-transforms.md#counters),
[`ha-api-hld.md`§4.7](https://github.com/sonic-net/DASH/blob/main/documentation/high-avail/ha-api-hld.md#47-counters),
[`dash-flow-resimulation.md`§6.5](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-flow-resimulation.md#65-counters).

We tick 5 counters per arbitrary key from an FNV hash. The spec needs:

- ~50 **ENI traffic & TCP-state counters** (TotalPacket, TotalBytes,
  TcpSynPacket, TcpSynAckPacket, FINPackets, RSTPackets, TcpConnectionsVerified,
  TcpConnectionsTimedOut, TcpConnectionsReset, TcpHalfOpenTimeouts, ...).
- **Drop counters per reason** (DropPacket, DropBroadcastPacket,
  DropInvalidPacket, DropIPv4SpoofingPacket, DropIPv6SpoofingPacket,
  DropBlockedPacket, DroppedRedirectPackets, DroppedPADiscoveryPackets,
  DroppedResourcesMemory, DroppedPARouteRule, DroppedFragPacket,
  DroppedResourcesPacket, DroppedAclPacket, DroppedMalformedPacket,
  DroppedForwardingPacket, DroppedNoRuleMatchPacket,
  DroppedResourcesUnifiedFlowMaxFlowsLimit, NoENIMatch).
- **Flow lifecycle counters** (FLOW_CREATED, FLOW_CREATE_FAILED,
  FLOW_UPDATED, FLOW_UPDATE_FAILED, FLOW_DELETED, FLOW_DELETE_FAILED,
  FLOW_AGED, FLOW_UPDATED_BY_RESIMULATION).
- **HA set DP-channel counters** (DP_PROBE_REQ/ACK_RX/TX_BYTES/PACKETS,
  DP_PROBE_FAILED).
- **HA scope flow-sync counters** (INLINE_FLOW_CREATE_REQ_SENT/RECV/FAILED/IGNORED,
  INLINE_FLOW_*_ACK_RECV/FAILED/IGNORED, TIMED_FLOW_*).
- **Port-level miss counters** (ENI_MISS_DROP_PACKETS, VIP_MISS_DROP_PACKETS).
- **Pipeline-stage drop counters** (OUTBOUND_ROUTING_ENTRY_MISS_DROP,
  OUTBOUND_CA_PA_ENTRY_MISS_DROP, TUNNEL_MISS_DROP, INBOUND_ROUTING_MISS_DROP).

Our current counters are fine for "does the counter system work?" but
useless for "does this configuration produce the same telemetry shape as
a real DPU?".

### Gap 6 — No gNMI northbound shim ⚠

Real SDN controllers speak **gNMI** to a SONiC DASH container. We only
expose our own `dashapi.v1` gRPC. To be drop-in for a real controller,
we need a gNMI gateway that translates `Set`/`Get`/`Subscribe` onto our
Apply/Get/List/Delete/Subscribe.

This is mechanical but non-trivial — gNMI paths must follow the
OpenConfig/SONiC YANG models, which are themselves derived from the
proto schemas. Doable as a phase-3 add-on.

### Gap 7 — No `dashd` agent + STATE_DB feedback loop ⚠

In real SONiC the flow is:

```
SDN → gNMI → CONFIG_DB → orchagent → APP_DB → asic_db → SAI → DPU
                                                                ↓ (status)
                                  STATE_DB ← orchagent ← APP_STATE_DB
```

Our **dashd is a placeholder**
([src/impl-go/dashd](src/impl-go/dashd/)). To match the SONiC story, we'd
have `dashd` consume CONFIG_DB-style intent, translate to our APP_DB
calls, and emit STATE_DB feedback (e.g., "vnet-prod: applied=true,
last_applied_ts=...").

### Gap 8 — No conformance test harness ⚠

Spec source: `DASH/test/`. Real DASH has PTF + SAI-Thrift conformance
suites that exercise every transform. We have a few unit tests in
`pipeline_test.go`. We need a **conformance suite that mirrors the upstream
PTF tests** so that "our sim passes" gives someone confidence that "their
real DPU should also pass". Without this, the simulator is opinionated
software, not a reference.

---

## 5. Where the upstream DASH community is heading (so we don't bet on
the past)

- **Flow APIs** are the most active area (PRs landing in `sonic-dash-api`
  for HA proto changes within the last 3 weeks; key reorder in
  `ha_scope_config`, addition of trusted VNIs, etc.).
- **DPU-driven HA setup** (vendor SDK owns the HA state machine and
  drives transitions internally) is being added as a parallel model to
  the ENI-level HA. We need to model both.
- **DASH-SAI pipeline packet flow** is being formalised (the new
  `dash-sai-pipeline-packet-flow.md`).
- **Fast Path ICMP flow redirection** landed in `eni.proto` recently
  (`disable_fast_path_icmp_flow_redirection`).
- **Outbound port map / port-range** entries were added for Private Link
  service.

So: any roadmap we draw should be ready for **HA + flows + multi-stage
pipeline as the foundation** because that's where the rest of the
ecosystem is going.

---

## 6. The roadmap to "best-in-class"

Six phases. Each phase is independently shippable and adds clear value.
Estimated effort is in person-weeks for a single contributor; gating
assumes one engineer working full time. Adjust as needed.

### Phase 0 — Hardening (1–2 wk, sets up everything else)

- Move `pipeline_test.go` to a **table-driven conformance suite** that
  mirrors the upstream PTF test names (VNET-to-VNET, Private Link, etc.).
- Add a **golden-decision dump** for each scenario: every `simulate`
  call's expected `Decision` JSON is checked in. Run on every PR.
- Wire `dash-sim-client simulate` into the conformance run so the
  end-to-end is exercised, not just the engine internals.
- Pin coverage at 80% for `pipeline/` and `model/`.

### Phase 1 — Real packet transforms (3–5 wk)

This unblocks every later phase. Concretely:

1. **Introduce a `Packet` value object that's actually mutable.** Today
   our `Packet` proto is the input only. Add `TransformedPacket` to the
   `Decision` response, populated by every action.
2. **Implement the routing-action engine** from
   [`dash-routing-actions.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-routing-actions.md):
   - Transition actions: `drop`, `trap`, `lpmrouting` (with
     `stage_index`), `maprouting`, `portmaprouting`.
   - Transformation actions: `staticencap` (VXLAN+NVGRE; DSCP
     preserve/pipe), `tunnel`, `tunnel_from_encap`, `reverse_tunnel`,
     `4to6` (with encoding masks), `6to4`, `nat` (SNAT/DNAT L3/L4
     with port-base arithmetic).
   - Up to **3 levels of stacked transforms**.
3. **Multi-stage pipeline executor.** A small interpreter that, per
   match entry, reads `transition` + `routing_type` + metadata, applies
   the action, and routes to the next stage. Stages and their per-entry
   metadata are read from the same APP_DB tables we already have.
4. **Add `TunnelTable` and `RoutingApplianceTable` resolution** into the
   actual transform path (currently we look these up but only record an
   underlay IP).
5. **Reverse-flow transform computation.** Every transform must also
   produce its inverse for bidirectional flows — this is what flow
   tables will store.

**Deliverable:** `dash-sim-client simulate --emit-packet` returns the
exact bytes you'd see at the wire (inner + outer headers fully
populated) for every scenario in
[`sdn-features-packet-transforms.md`](https://github.com/sonic-net/DASH/blob/main/documentation/general/sdn-features-packet-transforms.md).

### Phase 2 — Flow table + slow/fast path (4–6 wk)

Now we have transforms; flows are just "cached pre-computed transforms
+ reverse key + sync state". Concretely:

1. **`FlowTable` object** (new APP_DB kind), keyed by `[flow_table_id]`,
   with attributes for `MAX_FLOW_COUNT`, `FLOW_TTL_IN_MILLISECONDS`, and
   `DASH_FLOW_ENABLED_KEY` mask (matches `sai_dash_flow_enabled_key_t`).
2. **`FlowEntry`** keyed by the 7-tuple, with the full `SaiDashFlowState`
   protobuf as the payload. Use the upstream proto verbatim (it exists
   in the HLD).
3. **Slow path** in `simulate`:
   - On first packet of a flow → run the full transform engine →
     **create** a flow entry with the computed transforms + reverse key →
     return the decision.
   - On subsequent packets → flow-table lookup → apply pre-computed
     transforms → return decision *without* re-running ACL/route.
4. **TCP state tracking.** Track flow state from `SYN → SYN_ACK →
   ESTABLISHED → FIN/RST`. On `FIN/RST`, mark flow `PENDING_DELETE`.
5. **Age-out timer.** Every `tickInterval`, scan flows; if
   `now - last_seen > flow_ttl`, mark `PENDING_DELETE` and emit
   `FLOW_AGED` event.
6. **Flow CRUD APIs.** Add `CreateFlow / GetFlow / SetFlowAttribute /
   DeleteFlow / BulkCreateFlows / BulkRemoveFlows / BulkGetFlows`
   (with the bulk-get-session filter shape from the HLD).
7. **Flow resimulation.** Add `SAI_ENI_ATTR_FULL_FLOW_RESIMULATION_REQUESTED`
   (toggle attribute on `eni`) and on next packet of every flow on that
   ENI, re-run policy with current config, replace the flow entry.

**Deliverable:** `dash-sim-client flow list --eni eni-001 -o table`
shows live flows; `dash-sim-client flow create / delete / bulk-get`
work; `simulate` shows fast-path vs slow-path in the trace
(`fast-path: matched flow=<key> ver=<n>`).

### Phase 3 — High availability (4–6 wk)

Building on flows. Concretely:

1. **HA state machine** per `HaScope`, implementing all 10 states.
   Driven by:
   - SDN config writes (e.g., `set_ha_scope_attribute(role=ACTIVE)`).
   - DP-channel probe results.
   - Flow-sync acks.
2. **Per-ENI HA scope binding** (`SAI_ENI_ATTR_HA_SCOPE_ID`,
   `SAI_ENI_ATTR_IS_HA_FLOW_OWNER`). Pipeline behaviour changes based
   on these.
3. **DP-channel probe protocol.** Periodic `DP_PROBE_REQ` from each DPU
   to its peer; if 3+ consecutive misses → mark peer dead → transition
   to `SWITCHING_TO_STANDALONE`.
4. **CP-channel** (gRPC, for bulk sync). One peer connects, one accepts.
5. **Inline flow sync.** Active flow creation → wraps flow into a
   `FLOW_SYNC_REQ` packet via the DASH-metadata header → tunnels to
   peer → peer creates flow + sends `FLOW_SYNC_ACK` → active marks flow
   `FLOW_SYNCED`.
6. **Bulk sync using "Perfect Sync" colour algorithm.** Flow has a
   colour bit; on re-pair, flip current colour; bulk-sync only
   non-current-colour flows; real-time sync continues in parallel.
7. **Switchover and failover workflows** as `sequenceDiagram`-driven
   tests.
8. **Flow reconcile** (on re-pair, re-run policy on existing flows to
   pick up any SDN changes that happened during disconnect).

**Deliverable:** topology-03 multi-docker fleet has two configurable
HA-paired DPUs. Switchover demo: write a flow on Active → see it appear
on Standby → kill Active → Standby goes Standalone → flow still
queryable.

### Phase 4 — Production-grade counters (2–3 wk)

1. **Replace the synthetic FNV counters** with **per-ENI, per-flow,
   per-stage real counters** that increment based on what actually
   happens in the pipeline.
2. **Categorise drops** by reason (ACL, no-route, no-mapping,
   admin-disabled, ENI-miss, VIP-miss, malformed, resources, ...).
3. **TCP state counters** driven from the flow state machine.
4. **HA counters** (DP-probe counts, bulk-sync flow counts, inline-sync
   req/ack counts).
5. **Counter export over gNMI** (after phase 5) and/or **OpenTelemetry
   metrics**.

### Phase 5 — gNMI northbound shim (3–4 wk)

1. Generate or hand-author a **gNMI YANG model** that mirrors the
   `dashapi.v1` API surface (or align with the upstream SONiC YANGs if
   they exist for DASH — `documentation/gnmi/dash-gnmi-design.md`).
2. **gNMI gateway** in a new module `dash-gnmi-shim` that translates
   `Set` → `Apply`, `Get`/`GetSubscribe` → `Get`/`List`, `Subscribe`
   STREAM/ON_CHANGE → our `Subscribe`.
3. Run inside the same process or as a sidecar.
4. Conformance: a real SONiC controller speaking gNMI can drive our
   sim.

### Phase 6 — Conformance harness + reference scenarios (continuous)

1. Mirror the **upstream DASH/test PTF cases** with our own runner that
   uses `dash-sim-client` + `--emit-packet`.
2. Add a **scenario library** covering all 7 DASH services
   (VNET-to-VNET, VNET Peering, HA, Load Balancer, Service Tunnel &
   Private Link, Encryption Gateway, ExpressRoute Gateway), each with
   expected per-stage decisions & wire-level packets.
3. Add a **fuzz mode** (random valid configs + random packets, assert
   no panics, no decision flips for unchanged config).
4. CI: PRs against the proto schema must include scenario coverage.

---

## 7. Sequenced deliverables (~5 months of focused work)

| Wk | Phase | Deliverable | New `dash-sim-client` capability |
|---:|---|---|---|
| 0 | — | This audit doc | — |
| 1–2 | P0 | Conformance table tests + golden decision dumps | `simulate --golden` |
| 3–5 | P1 | Routing-action engine + multi-stage pipeline | `simulate --emit-packet`, scenario library v2 |
| 6–7 | P1 | Reverse-flow transform; 4↔6, NAT L3/L4, dual-underlay | `simulate --reverse` |
| 8–13 | P2 | Flow tables, slow/fast path, age-out, FIN/RST teardown | `flow list / get / create / delete / resimulate` |
| 14–19 | P3 | HA state machine, DP probes, inline + bulk sync, switchover | `ha status / switchover / failover` |
| 20–22 | P4 | Real per-ENI/per-flow/per-stage counters with drop reasons | (richer counter shape) |
| 23–26 | P5 | gNMI shim | `gnmi-cli` interop demo |
| ∞ | P6 | Conformance scenarios for 7 DASH services | one suite per service |

---

## 8. What we should NOT do

To keep us focused:

- **Don't reimplement P4/BMv2.** That's already the upstream reference.
  We're a *Go-native behavioural model* — different niche.
- **Don't try to match real DPU performance** (10M+ CPS). That's
  pointless in a sim; the value is correctness + observability.
- **Don't add vendor SDK shims** in dash-sim itself. If we want to
  drive a real DPU, the right place is a separate adapter
  (`dash-vendor-adapter`), analogous to `dash-redis-adapter`.
- **Don't fork the proto schemas.** Every gap above is implementation;
  none requires schema changes. If we discover we need a field, **upstream
  it first** to `sonic-dash-api`, then re-vendor.
- **Don't build a UI/console.** The CLI + admin HTTP is enough for
  reference value.

---

## 9. Risks & open questions

| Risk | Mitigation |
|---|---|
| The upstream HLDs have gaps and unspecified behaviours (especially HA edge cases) | Where the HLD is silent, copy the BMv2 reference (`dash-pipeline/`); cite the source in our code. |
| Flow table memory could explode in long-running tests | Cap with `MAX_FLOW_COUNT`, drop oldest on overflow, log a counter. |
| Bulk-sync over gRPC adds operational complexity to topology 03 | Make HA opt-in via `fleet.yaml` (`ha.enabled: true`); default-off keeps the simple path simple. |
| gNMI YANG models for DASH are still WIP upstream | Track the upstream `documentation/gnmi/` folder; if they're not stable in 3 months, we author our own and contribute back. |
| 26 weeks is a lot for one person | Each phase is independently valuable — we can ship P0+P1 and have a much better sim even without P2-P6. |

**Open questions for you:**

1. **Scope: P1 only, or all 6 phases on the roadmap?** P0+P1 alone
   moves us from ~45% to ~70% alignment.
2. **Schema delta upstream:** are you OK with us contributing back to
   `sonic-dash-api` if we find proto-level gaps (e.g. flow API isn't
   currently in `sonic-dash-api` — it lives in the HLD only)?
3. **Multi-DPU testing of HA:** topology-03 (compose) already gives us
   2+ DPUs on one network — that's exactly the HA test setup we need.
   Are you OK with HA being a topology-03-only feature in phase 3?
4. **gNMI is the SDN integration story.** Skipping P5 means our sim
   can't be driven by stock SONiC tooling. Is that an acceptable
   limitation for the v1 audience (internal developers + dash-sim-client
   users)?

---

## 10. Bottom line

We have an **excellent APP_DB model + CRUD layer** and a **toy
single-shot pipeline**. To be the **best DPU simulator** in the SONiC
DASH ecosystem we need to add, in order:

1. **Real packet transforms** (so we can produce wire-correct packets).
2. **Flow tables + slow/fast path** (so we can model the actual
   stateful behaviour).
3. **HA** (so we can do SDN-controller failover tests).
4. **Production counters** (so observability matches real DPUs).
5. **gNMI shim** (so any SONiC tool drives us).
6. **Conformance suite** (so others can verify their DPU against us).

None of this is research — every gap maps to a published HLD. The
work is just engineering.
