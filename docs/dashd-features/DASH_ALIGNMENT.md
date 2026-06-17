# DashCenter — DASH Alignment & Modern Diagnostics Proposal

> **Status:** Draft v1 (2026-06-17)
> **Purpose:** Research-grounded proposal for evolving DashCenter into the
> reference observability, configuration, and diagnostics platform for
> [sonic-net/DASH](https://github.com/sonic-net/DASH)-compliant DPU fleets.
> Built on a full read of upstream DASH HLDs, our vendored proto kinds,
> and the existing [dash-sim alignment audit](../dash-sim-on-par-with-sonic-audit.md).
> **Companion:** [`FEATURE_ASK.md`](./FEATURE_ASK.md) holds the general
> product backlog (F1–F60 + L1–L10). This document captures
> DASH-specific features (`D1`–`D32`) that complement that backlog.

---

## Table of Contents

1. [Executive summary](#1-executive-summary)
2. [DASH project overview](#2-dash-project-overview)
3. [DashCenter ↔ DASH alignment matrix](#3-dashcenter--dash-alignment-matrix)
4. [Gap analysis (5 dimensions)](#4-gap-analysis-5-dimensions)
5. [Feature proposals](#5-feature-proposals)
   - [Section A — Complete DASH object northbound (D1–D8)](#section-a--complete-dash-object-northbound)
   - [Section B — DASH HA native orchestration (D9–D14)](#section-b--dash-ha-native-orchestration)
   - [Section C — DASH pipeline observability (D15–D20)](#section-c--dash-pipeline-observability)
   - [Section D — Cross-fleet diagnostics (D21–D26)](#section-d--cross-fleet-diagnostics)
   - [Section E — DASH conformance and certification (D27–D30)](#section-e--dash-conformance-and-certification)
   - [Section F — Ultra-modern diagnostic platform (D31–D32)](#section-f--ultra-modern-diagnostic-platform)
6. [Reference architecture](#6-reference-architecture)
7. [Sequencing roadmap](#7-sequencing-roadmap)
8. [Appendix — Spec references](#8-appendix--spec-references)

---

## 📚 Reading along with the upstream DASH project

Open this document side-by-side with the upstream DASH repository — every
proposal cites the exact spec section it is grounded in. Use the links
below as your reading companion:

**Project root:**
- [`sonic-net/DASH`](https://github.com/sonic-net/DASH/tree/main) — main repository
- [`DASH/documentation/`](https://github.com/sonic-net/DASH/tree/main/documentation) — all HLDs (top-level table of contents)
- [`DASH/dash-pipeline/`](https://github.com/sonic-net/DASH/tree/main/dash-pipeline) — P4 + BMv2 reference dataplane
- [`DASH/test/`](https://github.com/sonic-net/DASH/tree/main/test) — PTF and SAI-Thrift conformance tests
- [`sonic-net/sonic-dash-api`](https://github.com/sonic-net/sonic-dash-api) — vendored protobuf schema (29 kinds)

**Per-section reading list:**

| This document | Upstream DASH companion |
|---|---|
| §3 alignment matrix | [`sonic-dash-api/proto/`](https://github.com/sonic-net/sonic-dash-api/tree/master/proto) (the 29 kinds) |
| §A object northbound | [`dash-acl.md`](https://github.com/sonic-net/DASH/blob/main/documentation/acl/dash-acl.md), [`dash-routing-actions.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-routing-actions.md), [`dash-metering.md`](https://github.com/sonic-net/DASH/blob/main/documentation/general/dash-metering.md), [`dash-private-link.md`](https://github.com/sonic-net/DASH/blob/main/documentation/general/dash-private-link.md) |
| §B HA orchestration | [`ha-api-hld.md`](https://github.com/sonic-net/DASH/blob/main/documentation/high-avail/ha-api-hld.md), [`high-availability-and-scale.md`](https://github.com/sonic-net/DASH/blob/main/documentation/high-avail/high-availability-and-scale.md) |
| §C pipeline observability | [`sdn-features-packet-transforms.md`](https://github.com/sonic-net/DASH/blob/main/documentation/general/sdn-features-packet-transforms.md), [`dash-routing-actions.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-routing-actions.md) |
| §D cross-fleet diagnostics | [`dash-flow-api.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-flow-api.md), [`dash-flow-resimulation.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-flow-resimulation.md) |
| §E conformance | [`dash-bmv2-data-plane-app.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-bmv2-data-plane-app.md), [`DASH/test/`](https://github.com/sonic-net/DASH/tree/main/test) |
| §F modern diagnostics | (DashCenter-native; no upstream DASH spec) |

> **Tip.** Each individual feature (D1–D32) ends with a `Spec refs:` line
> linking directly to the relevant upstream document anchor — click those
> while reading the feature, no need to scroll back here. The full
> citation list is in [§8 Appendix — Spec references](#8-appendix--spec-references).

---

## 1. Executive summary

DashCenter today is a strong **control-plane skeleton** with seven northbound
resource kinds (Vnet, ENI, VnetMapping, AclPolicy, RoutePolicy, HaSet,
ServiceTunnel) and a single-stage simulator pipeline. DASH itself defines
**29 object kinds**, a **multi-stage match-action pipeline**, a
**10-state HA machine**, ~**50 counter classes**, and a **conformance
test framework**. The delta between "we have control-plane plumbing"
and "we are the de-facto DASH operations platform" is the gap this
document closes.

The proposal is grouped into six strategic dimensions:

| Dimension | What we add | Why it matters |
|---|---|---|
| **A. Object northbound coverage** | 8 missing DASH kinds promoted to first-class managed resources | Operators today cannot manage metering, NAT, prefix tags, PA validation, port maps, trusted VNIs, route types, routing appliances via DashCenter |
| **B. HA native orchestration** | Full HA state-machine visibility, flow-sync metrics, DP probe health, switchover automation | SmartSwitch HA is the most discussed area of DASH; operators cannot reason about HA today without raw probes |
| **C. Pipeline observability** | Stage-by-stage tracer, ~50 counter classes, drop-reason attribution, fast/slow path classification | DASH telemetry has dozens of counters and pipeline stages today only inferable by hand |
| **D. Cross-fleet diagnostics** | Multi-DPU flow correlation, reachability analyzer, synthetic packet generator, drift heatmap | At fleet scale, single-DPU diagnostics are insufficient |
| **E. Conformance and certification** | DASH PTF runner, capability matrix, behavior-model integration | Customers buying a multi-vendor DPU fleet need objective proof of behavior parity |
| **F. Ultra-modern diagnostics** | AI-assisted policy diagnosis, natural-language query, ChatOps integration | Modern operations expects a Datadog-/Grafana-class diagnostic experience |

If delivered in the sequence proposed in §7, DashCenter becomes the
single management plane any DASH-compliant fleet runs against —
regardless of silicon vendor or deployment topology.

---

## 2. DASH project overview

DASH ("Disaggregated API for SONiC Hosts") is an OCP/SONiC initiative
that defines a vendor-neutral object model and gRPC/gNMI surface for
programming stateful DPU pipelines. The principal artifacts are:

- **`sonic-dash-api`** — the canonical 29-message protobuf schema for the
  object model and APP_DB tables.
- **`DASH/documentation/`** — HLDs covering pipeline behavior, ACL stages,
  routing actions, packet transforms, HA, metering, PA validation,
  prefix tags, fast-path, private link, flow tables, resimulation.
- **`DASH/dash-pipeline/`** — P4 + BMv2 reference dataplane.
- **`DASH/test/`** — PTF and SAI-Thrift conformance tests.
- **Active areas (2026 cadence):** flow tables, HA evolution (DPU-driven
  HA), trusted VNIs, ICMP fast-path redirection, outbound port-map
  ranges, dual-underlay encap.

The full upstream coverage matrix is captured in
[`dash-sim-on-par-with-sonic-audit.md`](../dash-sim-on-par-with-sonic-audit.md).
This document treats that audit as ground truth and focuses on
**operator-facing capabilities** DashCenter must expose to make DASH
actionable in production.

---

## 3. DashCenter ↔ DASH alignment matrix

The table below maps the 29 DASH proto kinds to DashCenter's current
northbound surface.

| DASH kind | Northbound today | Modeled in sim | Gap |
|---|---|---|---|
| `appliance` | indirect (inventory) | ✅ | minor |
| `vnet` | ✅ Vnet | ✅ | none |
| `eni` | ✅ Eni | ✅ | minor (FNIC ENI mode) |
| `eni_route` | indirect (RoutePolicy) | ✅ | minor |
| `route_group` | indirect (RoutePolicy) | ✅ | minor |
| `route` | indirect (RoutePolicy) | ✅ | minor (ECMP) |
| `route_rule` | indirect (RoutePolicy) | ✅ | minor (PA validation) |
| `route_type` | ❌ not surfaced | ✅ | **D1** |
| `vnet_mapping` | ✅ VnetMapping | ✅ | minor (port-map action) |
| `routing_appliance` | ❌ not surfaced | ✅ | **D2** |
| `tunnel` | ✅ ServiceTunnel | ✅ | minor |
| `acl_group` | ✅ AclPolicy | ✅ | minor |
| `acl_in` / `acl_out` | indirect (AclPolicy.stage) | ✅ | minor |
| `acl_rule` | indirect (AclPolicy.rules) | ✅ | partial (src_tag/dst_tag) |
| `prefix_tag` | ❌ not surfaced | partial | **D3** |
| `pa_validation` | ❌ not surfaced | partial | **D4** |
| `outbound_port_map` | ❌ not surfaced | partial | **D5** |
| `outbound_port_map_range` | ❌ not surfaced | partial | **D5** |
| `meter` | ❌ not surfaced | stored only | **D6** |
| `meter_policy` | ❌ not surfaced | stored only | **D6** |
| `meter_rule` | ❌ not surfaced | stored only | **D6** |
| `qos` | ❌ not surfaced | stored only | **D7** |
| `ha_set` | ✅ HaSet | partial | **B group** |
| `ha_set_config` | indirect (HaSet) | inert | **B group** |
| `ha_set_state` | indirect (HaSet) | inert | **B group** |
| `ha_scope` | ❌ not surfaced | inert | **D8 + B group** |
| `ha_scope_config` | ❌ not surfaced | inert | **D8 + B group** |
| `ha_scope_state` | ❌ not surfaced | inert | **D8 + B group** |
| `types` | (shared) | — | — |

Of the 29 DASH kinds, **only 7 (24%) are exposed as first-class
northbound resources**. The remaining 22 either live as embedded fields
inside existing kinds (acceptable) or are completely absent from the
operator-facing surface (Section A addresses this).

---

## 4. Gap analysis (5 dimensions)

### 4.1 Object northbound coverage
22 of 29 DASH kinds are not directly manageable through DashCenter.
Critical missing controls: metering (rate-limit, traffic-class
counters), NAT (encoded via routing-action params; today inaccessible),
prefix tags (referenced from ACL rules but cannot be CRUD'd), PA
validation (security baseline; cannot be configured), outbound port
maps (PrivateLink underpinning), trusted VNIs (inbound filtering),
QoS classes.

### 4.2 HA native orchestration
HA proto types are vendored but `dash-sim` ignores them and DashCenter
has only a thin `HaSet` wrapper. The DASH 10-state DPU HA machine, the
5-state flow-sync machine, DP-channel probing, perfect-sync bulk
algorithm, and the planned/unplanned/standalone failure-mode flows
have no operator-facing surface.

### 4.3 Diagnostics depth
`TraceFlow` exists for the simulator but does not include flow-table
state, fast-path/slow-path classification, HA flow-owner attribution,
or stage-by-stage timing. Operators cannot answer "why did this packet
go through stage X but not Y" without manual log correlation.

### 4.4 Observability — counters and telemetry
DASH defines ~50 ENI counter classes (TCP states, drops by reason, flow
lifecycle, DP probe stats, port-level misses, pipeline-stage misses).
The simulator emits 5 synthetic counters. The northbound counter
streaming (PE-3c) is present but the counter taxonomy is small;
high-value telemetry (drop-reason attribution, HA sync stats, per-stage
miss counters) is unavailable.

### 4.5 Conformance and certification
DASH has a PTF-based conformance suite. DashCenter has unit tests but
no operator-runnable certification flow. A multi-vendor DPU fleet needs
"my hardware passes" / "my hardware fails" answers, plus a capability
matrix to plan around partial implementations.

---

## 5. Feature proposals

All features below use the same requirements format as
[`FEATURE_ASK.md`](./FEATURE_ASK.md) (problem → use cases → capability →
functional/non-functional → business value → metrics → dependencies →
spec refs). Identifiers `D1`–`D32` are stable.

### Section A — Complete DASH object northbound

#### D1. Route-type catalog as first-class resource

- **Category:** dashd / DASH coverage
- **Problem:** DASH `route_type` defines the action set (DIRECT, VNET, VNET_DIRECT, SERVICETUNNEL, APPLIANCE, PRIVATELINK, PRIVATELINKNSG, DROP, TRAP) and transform parameters. Today operators cannot inspect or constrain the route-type catalog; mistakes in route policy default to silent fallback.
- **Use cases:** Lock down which action types a tenant may use; validate a manifest before apply; surface advertised actions per DPU capability.
- **Capability:** Read-only `RouteType` resource exposing the catalog from each DPU; admission rejects any `route.next_hop_type` not in the catalog.
- **Functional:** GET `/v1/route-types`, capability join with DPU inventory, surfacing in dashw.
- **Non-functional:** Read-cached; refreshed on inventory change.
- **Business value:** Predictable behavior across heterogeneous fleets; cleaner errors at admission.
- **Metrics:** Number of routes blocked by missing route-type capability.
- **Dependencies:** None.
- **Spec refs:** [`dash-routing-actions.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-routing-actions.md).

#### D2. Routing-appliance resource

- **Category:** dashd / DASH coverage
- **Problem:** DASH `routing_appliance` describes off-DPU appliances (NSG, NAT gateway, firewall) referenced from routes/tunnels. Without a managed resource, operators wire appliances by IP and lose lifecycle hooks.
- **Use cases:** Onboard a third-party NSG cluster as a named appliance; track health and version; rotate appliance fleets without rewriting routes.
- **Capability:** First-class `RoutingAppliance` kind with health probe, version, and capability fields.
- **Functional:** CRUD + watch; appliance referenced by name from routes/tunnels.
- **Non-functional:** Appliance health probed on inventory cadence.
- **Business value:** Decouples policy from appliance IP rotation; service-insertion lifecycle.
- **Metrics:** Number of routes pinned by name vs by IP.
- **Dependencies:** Inventory model.
- **Spec refs:** [`dash-routing-actions.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-routing-actions.md).

#### D3. Prefix-tag resource and ACL tag wiring

- **Category:** dashd / DASH coverage
- **Problem:** DASH `prefix_tag` lets ACL rules reference symbolic IP groups (e.g. `Vendors`, `CorpMgmt`) but DashCenter cannot CRUD tags today; ACLs must hardcode CIDRs.
- **Use cases:** Manage a "TrustedAdmins" tag once and reference it from many ACL rules; rotate trusted CIDRs in seconds.
- **Capability:** `PrefixTag` resource with versioned CIDR sets; ACL rules accept `src_tag`/`dst_tag` references.
- **Functional:** CRUD + dependency-graph integration (delete-protection); tag dereference at admission and audit.
- **Non-functional:** Tag size bounded; resolution latency budget enforced.
- **Business value:** Massive reduction in ACL churn for any org with security baselines.
- **Metrics:** Tag references in production ACLs; reduction in ACL rule count.
- **Dependencies:** AclPolicy schema extension.
- **Spec refs:** [`dash-acl.md`](https://github.com/sonic-net/DASH/blob/main/documentation/acl/dash-acl.md), [`dash-prefix-tag`](https://github.com/sonic-net/DASH/blob/main/documentation/) section.

#### D4. PA-validation list management

- **Category:** dashd / DASH coverage
- **Problem:** DASH `pa_validation` enforces that inbound packets carry a Provider Address from an approved list, blocking spoofing. Today operators have no surface to manage these lists per VNet.
- **Use cases:** Onboard a peer VNet and grant inbound PA acceptance; revoke a leaked PA in one command.
- **Capability:** `PaValidation` resource scoped per VNet; integrated with route-rule `pa_validation: true`.
- **Functional:** CRUD; rules link from route-rule; deletes protected if rules reference.
- **Non-functional:** List size bounded; lookup latency on the inbound path documented.
- **Business value:** Core security baseline expected in any multi-tenant deployment.
- **Metrics:** Inbound packets dropped by PA validation per VNet (counter).
- **Dependencies:** Inbound pipeline support (already partial).
- **Spec refs:** `dash-pa-validation` section.

#### D5. Outbound port-map and port-map-range for PrivateLink

- **Category:** dashd / DASH coverage
- **Problem:** PrivateLink redirect requires `outbound_port_map` and `outbound_port_map_range` to map overlay ports onto provider ports. The proto kinds are vendored but not surfaced as managed resources.
- **Use cases:** Set up a PrivateLink endpoint with a fixed port mapping; expose multiple service ports via range.
- **Capability:** Two resources (`OutboundPortMap`, `OutboundPortMapRange`) consumed by tunnels of action `privatelink`.
- **Functional:** CRUD; validation prevents overlapping ranges; admission joins with target tunnel capability.
- **Non-functional:** Validation O(log N) on rule count.
- **Business value:** Completes the PrivateLink story (a primary DASH selling point) end-to-end.
- **Metrics:** Number of PrivateLink endpoints provisioned through DashCenter.
- **Dependencies:** ServiceTunnel of action `privatelink`.
- **Spec refs:** [`dash-private-link.md`](https://github.com/sonic-net/DASH/blob/main/documentation/general/dash-private-link.md).

#### D6. Meter, MeterPolicy, MeterRule resources

- **Category:** dashd / DASH coverage
- **Problem:** DASH metering (token-bucket rate limiting + per-class counters) is foundational for QoS, billing, and security throttling. Three kinds exist (`meter`, `meter_policy`, `meter_rule`) but DashCenter exposes none.
- **Use cases:** Apply a per-tenant 10 Gbps cap; track per-meter-class throughput; rate-limit DDoS-implicated flows.
- **Capability:** Three new resources mirroring the DASH proto; admission ties meter classes to ACL rules and routes.
- **Functional:** CRUD + counter integration so meter hits stream via existing PE-3c pipeline.
- **Non-functional:** Per-meter counter cardinality documented; rollups via F6 dimensions.
- **Business value:** Unlocks rate-limit and per-class billing — a top operator ask.
- **Metrics:** Number of policies applied; bytes/packets metered.
- **Dependencies:** Counter streaming pipeline.
- **Spec refs:** [`dash-metering.md`](https://github.com/sonic-net/DASH/blob/main/documentation/general/dash-metering.md).

#### D7. QoS policy resource

- **Category:** dashd / DASH coverage
- **Problem:** DASH `qos` defines traffic-class queues and DSCP behavior. Today operators have no surface to manage this; per-tenant QoS guarantees are not enforceable.
- **Use cases:** Set DSCP preserve vs pipe on a per-tunnel basis; reserve queue bandwidth per tenant; surface oversubscription.
- **Capability:** `QosPolicy` resource scoped per ENI / VNet; capability-checked against DPU advertised classes.
- **Functional:** CRUD; visualization of per-class fill.
- **Non-functional:** Apply path latency budget unchanged.
- **Business value:** Removes a multi-tenant deal blocker (no fair sharing without QoS).
- **Metrics:** Queue-fill metric; QoS-induced drops counter.
- **Dependencies:** D6 meter integration helpful.
- **Spec refs:** [`dash-routing-actions.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-routing-actions.md) (DSCP), [`sdn-features-packet-transforms.md`](https://github.com/sonic-net/DASH/blob/main/documentation/general/sdn-features-packet-transforms.md).

#### D8. HA Scope and per-ENI HA binding

- **Category:** dashd / DASH coverage
- **Problem:** DASH separates `ha_set` (the pair) from `ha_scope` (the binding of ENIs to a pair) and exposes `ha_scope_state` / `ha_set_state`. DashCenter only models `HaSet`. Operators cannot reason about per-ENI HA membership or scope-level state.
- **Use cases:** Show which ENIs belong to which HA scope; query scope state (ACTIVE / STANDBY / STANDALONE) per scope; rebalance ENIs across scopes.
- **Capability:** First-class `HaScope` resource referencing a `HaSet`; ENIs gain `ha_scope_id` and `is_ha_flow_owner` fields exposed as read-only status.
- **Functional:** CRUD on `HaScope`; ENI binding via field on `Eni`; state visible via watch.
- **Non-functional:** Scope state propagation under failover within documented latency.
- **Business value:** Completes the HA model semantics required by the DASH HLD.
- **Metrics:** Number of scopes per cluster; rebalances per month.
- **Dependencies:** Existing HaSet; B group (HA visibility).
- **Spec refs:** [`ha-api-hld.md`](https://github.com/sonic-net/DASH/blob/main/documentation/high-avail/ha-api-hld.md).

---

### Section B — DASH HA native orchestration

#### D9. HA state-machine visualizer (10 DPU states + 5 flow-sync states)

- **Category:** dashw / DASH HA UX
- **Problem:** Operators have no way to see the DASH 10-state DPU HA machine (DEAD → CONNECTING → CONNECTED → INITIALIZING_TO_* → PENDING_* → ACTIVE / STANDBY → SWITCHING_TO_STANDALONE → STANDALONE) or the 5-state flow-sync machine.
- **Use cases:** Watch a switchover live; pinpoint where a stuck pair is parked; rehearse failover.
- **Capability:** Animated state diagram showing current and historical state per HA scope / set; flow-sync waterfall per ENI.
- **Functional:** Real-time via F17 WebSocket bridge; replay over the audit log; tooltips link to runbooks per state.
- **Non-functional:** Frame budget respected at fleet scale.
- **Business value:** Brings the most-talked-about DASH feature into the operator's first-pane experience.
- **Metrics:** Operator time-to-decision during a switchover.
- **Dependencies:** D8 HA scope; F17 streaming.
- **Spec refs:** [`ha-api-hld.md`§3 states](https://github.com/sonic-net/DASH/blob/main/documentation/high-avail/ha-api-hld.md).

#### D10. DP-channel probe health dashboard

- **Category:** dashw / DASH HA UX
- **Problem:** DP-channel BFD-like probes (`DP_PROBE_REQ`/`DP_PROBE_ACK`) gate HA correctness but are not surfaced anywhere today.
- **Use cases:** Detect a soft DP-channel degradation before it triggers failover; correlate probe loss with switchover events.
- **Capability:** Per-pair probe RTT and loss-rate gauge; alert if loss exceeds threshold.
- **Functional:** Counter source from DPU agent; visualization with thresholds.
- **Non-functional:** Counter cardinality bounded per pair.
- **Business value:** Detect and remediate HA degradation before customer impact.
- **Metrics:** Probe-RTT distribution; loss-rate alerts.
- **Dependencies:** Counter streaming; D9 visualizer.
- **Spec refs:** [`ha-api-hld.md`§4.7](https://github.com/sonic-net/DASH/blob/main/documentation/high-avail/ha-api-hld.md#47-counters).

#### D11. Inline + bulk flow-sync metrics and progress

- **Category:** dashd + dashw / HA
- **Problem:** DASH defines inline flow sync (per-flow) and bulk sync (Perfect Sync colour-flip). Today there is no visibility into either progress or correctness.
- **Use cases:** During pairing, watch bulk sync drain; alert if inline sync ACK rate degrades.
- **Capability:** Counters (`INLINE_FLOW_*_REQ/ACK`, `TIMED_FLOW_*`) streamed; bulk sync progress (% complete, colour iteration) exposed.
- **Functional:** Per-scope dashboard pane; ETA computation.
- **Non-functional:** Bulk-sync counters added with bounded cardinality.
- **Business value:** Confidence during failover and pairing — the highest-stakes HA operations.
- **Metrics:** Sync completion time; sync-failure rate.
- **Dependencies:** D9; counter pipeline.
- **Spec refs:** [`ha-api-hld.md`§Flow Sync](https://github.com/sonic-net/DASH/blob/main/documentation/high-avail/ha-api-hld.md).

#### D12. Planned switchover and unplanned failover workflows

- **Category:** dashctl + dashw / HA
- **Problem:** DASH defines distinct planned-switchover and unplanned-failover flows but DashCenter has no opinionated wrapper.
- **Use cases:** One-click planned switchover before maintenance; controlled standalone mode during peer outage; abort and roll back if pre-checks fail.
- **Capability:** Three commands (`dashctl ha switchover`, `failover`, `standalone`) with pre-checks, progress streaming, and forced-rollback gates.
- **Functional:** Each command emits saga-style audit; integrates with F18 HA Theater.
- **Non-functional:** Bounded operator time per phase; documented SLO.
- **Business value:** Codifies safe HA operations as repeatable workflows.
- **Metrics:** Switchover MTTR; failure-rate post-rollout.
- **Dependencies:** F18 HA Theater; D9.
- **Spec refs:** [`ha-api-hld.md`§Switchover](https://github.com/sonic-net/DASH/blob/main/documentation/high-avail/ha-api-hld.md).

#### D13. Split-brain detection and remediation

- **Category:** dashd / HA safety
- **Problem:** Two DPUs simultaneously believing they own the same flow set is the worst HA failure mode and has no first-party detector today.
- **Use cases:** Alert on split-brain; provide a guided remediation runbook; prevent further writes until resolved.
- **Capability:** Continuous cross-pair consistency probe; alert + automatic write-lockout on detection.
- **Functional:** Detection within bounded interval; admin endpoint exposes posture.
- **Non-functional:** No measurable steady-state regression.
- **Business value:** Prevents the worst-case outage class in any HA system.
- **Metrics:** Detection latency; false-positive rate.
- **Dependencies:** D9; D11.
- **Spec refs:** [`ha-api-hld.md`§Standalone](https://github.com/sonic-net/DASH/blob/main/documentation/high-avail/ha-api-hld.md).

#### D14. DPU-driven HA mode

- **Category:** dashd / HA topology
- **Problem:** Newer DASH variants delegate HA state-machine driving to the vendor SDK; DashCenter must model both control-plane-driven and DPU-driven HA.
- **Use cases:** Run a fleet where some vendors require operator-driven HA and others self-drive; surface the mode in dashw.
- **Capability:** Detection via DPU capability advertisement; mode-aware orchestration (the orchestrator becomes observer for DPU-driven scopes).
- **Functional:** Per-scope mode field; capability join; UI badge.
- **Non-functional:** Operator-driven path unchanged; DPU-driven path observed only.
- **Business value:** Multi-vendor fleet support — central to the DASH vision.
- **Metrics:** Number of scopes per mode.
- **Dependencies:** D8; D9.
- **Spec refs:** Upstream DASH community direction (DPU-driven HA).

---

### Section C — DASH pipeline observability

#### D15. Per-stage pipeline tracer

- **Category:** dashd diagnostics + dashw
- **Problem:** Operators see a binary "matched / dropped" answer from TraceFlow today. DASH defines a multi-stage pipeline (Direction → ENI → ACL stages 1–5 → routing → mapping → encap → metering); they need per-stage attribution.
- **Use cases:** "Why was this packet dropped" — show the exact stage and matched/missed rule; debug a new ACL by tracing five sample flows.
- **Capability:** Extended TraceFlow returning a per-stage trace with stage name, matched rule id, transform applied, and meter class.
- **Functional:** dashctl `trace flow --explain`; dashw waterfall view (Packet Anatomy Lab in vision doc).
- **Non-functional:** Trace overhead bounded; off the data path.
- **Business value:** First-pass diagnostic that resolves most "why did this drop" support tickets.
- **Metrics:** Time-to-diagnose for representative scenarios.
- **Dependencies:** F5 (TraceFlow parity), F17 streaming.
- **Spec refs:** [`dash-routing-actions.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-routing-actions.md).

#### D16. ~50-counter ENI telemetry coverage

- **Category:** dashd + dash-sim
- **Problem:** DASH defines ~50 ENI counters (TCP state, drop reasons, flow lifecycle, port-level miss). DashCenter exposes a thin subset.
- **Use cases:** Surface TCP SYN flood symptoms; pinpoint a DPU exhausting flow-table memory; trend connection setup rate per ENI.
- **Capability:** First-party support for the full DASH counter taxonomy in dash-sim, dashd ingest, and dashw visualization.
- **Functional:** Counter catalog versioned; UI groups by family (TCP / drop / flow / port-miss / pipeline-miss).
- **Non-functional:** Cardinality controlled via rollups (F6).
- **Business value:** Real DPU-grade telemetry, not toy counters.
- **Metrics:** Number of counter classes exposed; adoption in dashboards.
- **Dependencies:** F6 counter aggregation; F27 anomaly injection for testing.
- **Spec refs:** [`sdn-features-packet-transforms.md`§Counters](https://github.com/sonic-net/DASH/blob/main/documentation/general/sdn-features-packet-transforms.md#counters).

#### D17. Drop-reason attribution

- **Category:** dashd + dashw
- **Problem:** DASH defines >15 drop reasons (DropBlocked, DropMalformed, DropIPv4Spoof, DropFrag, DropNoRule, DroppedResourcesMemory, OUTBOUND_ROUTING_ENTRY_MISS_DROP, …). Today drops surface as a single bucket.
- **Use cases:** Triage a spike in drops by reason category; alert on `DroppedResourcesMemory` (capacity exhaustion); correlate `DropIPv4Spoof` events with PA-validation tightening.
- **Capability:** Per-reason counter stream + UI breakdown with drill-down to suspected ENIs / VNets / rules.
- **Functional:** Reason category surfaces in TraceFlow per stage.
- **Non-functional:** Per-reason cardinality bounded by DPU.
- **Business value:** Turns "drops are happening" into "drops are happening because X — here is what to do".
- **Metrics:** Per-reason drop rate; alerting accuracy.
- **Dependencies:** D16 counter taxonomy.
- **Spec refs:** [`sdn-features-packet-transforms.md`§Counters](https://github.com/sonic-net/DASH/blob/main/documentation/general/sdn-features-packet-transforms.md#counters).

#### D18. Fast-path vs slow-path classification

- **Category:** dashd + dash-sim
- **Problem:** DASH distinguishes slow-path (first-packet policy evaluation) from fast-path (subsequent packets via flow table). Operators today have no visibility into the split.
- **Use cases:** Detect a flow-table eviction storm forcing slow-path; detect a misconfigured fast-path-disable bit; trend fast-path hit rate per ENI.
- **Capability:** Per-ENI fast-path-hit / slow-path-hit counters; ratio surfaced in dashw with anomaly detection.
- **Functional:** dash-sim emits both counters; UI shows ratio with healthy-range bands.
- **Non-functional:** Counter cost bounded.
- **Business value:** Detects performance-affecting misconfigurations early.
- **Metrics:** Fast-path hit rate distribution.
- **Dependencies:** F25 sim flow-table; D16.
- **Spec refs:** [`dash-fast-path`](https://github.com/sonic-net/DASH/blob/main/documentation/general/) section.

#### D19. ECMP traffic distribution view

- **Category:** dashw / Routing
- **Problem:** DASH supports ECMP across equal-cost routes; today there's no visibility into actual distribution skew vs intent.
- **Use cases:** Detect a bad-hash distribution causing one PA to take 90% of traffic; rebalance an ECMP group; alert when skew exceeds threshold.
- **Capability:** Per-route counters + skew metric per ECMP group, visualized as a stacked area chart.
- **Functional:** Stream of `bytes_per_member`; computed skew percentage.
- **Non-functional:** Bounded cardinality per ECMP group.
- **Business value:** Cures a common silent failure mode in real fleets.
- **Metrics:** Skew distribution across ECMP groups.
- **Dependencies:** D16 counter taxonomy; ECMP routing.
- **Spec refs:** [`dash-routing-actions.md`§ECMP](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-routing-actions.md).

#### D20. Meter token-bucket visualization

- **Category:** dashw / QoS
- **Problem:** Meter policies (D6) need real-time visibility into bucket fill, drop rate, and burst capacity.
- **Use cases:** Tune a per-tenant rate-limit by watching bucket usage; alert on chronic exhaustion; demonstrate compliance with rate SLAs to customers.
- **Capability:** Live bucket-fill view per meter; rate-limit-induced drops cross-referenced with D17 attribution.
- **Functional:** Counter stream wired into a token-bucket-style gauge.
- **Non-functional:** Update frequency bounded by D16 cardinality budget.
- **Business value:** Operators tune rate limits with confidence; customer-facing SLA evidence.
- **Metrics:** Number of meters tuned via this view.
- **Dependencies:** D6 meter resource; D16 counters.
- **Spec refs:** [`dash-metering.md`](https://github.com/sonic-net/DASH/blob/main/documentation/general/dash-metering.md).

---

### Section D — Cross-fleet diagnostics

#### D21. Multi-DPU flow correlation

- **Category:** dashd / diagnostics
- **Problem:** A real packet traverses multiple DPUs (origin ENI → service tunnel → destination ENI). Today each DPU's view is siloed.
- **Use cases:** "Where did this flow die" — follow a 7-tuple across DPUs; correlate per-stage drops on the path.
- **Capability:** Flow-id correlation across DPUs using flow keys and time windows; visualization of the cross-DPU path.
- **Functional:** API returning a multi-DPU trace; dashw timeline view.
- **Non-functional:** Cross-DPU correlation bounded in latency.
- **Business value:** Eliminates the highest-effort class of network triage.
- **Metrics:** Time-to-RCA for cross-DPU incidents.
- **Dependencies:** F25 sim flow tables; D15 per-stage tracer.
- **Spec refs:** [`dash-flow-api.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-flow-api.md).

#### D22. Reachability analyzer ("can A reach B?")

- **Category:** dashd / diagnostics
- **Problem:** Operators routinely ask "given current policy, can ENI A reach ENI B over port P?" with no tooling answer.
- **Use cases:** Pre-flight any new tenant-network design; verify a security baseline; produce evidence for compliance.
- **Capability:** Static analyzer that walks ACLs, routes, tunnels, mappings, PA validation, and tags and returns ALLOW / DENY with rule attribution and counterexample paths.
- **Functional:** `dashctl reach <src> <dst> <port> [--proto tcp]` and dashw form.
- **Non-functional:** Analyzer bounded for representative fleet sizes.
- **Business value:** Differentiating diagnostic that no current DPU controller offers.
- **Metrics:** Adoption in compliance and onboarding flows.
- **Dependencies:** F22 dependency graph; admin APIs.
- **Spec refs:** [`dash-acl.md`](https://github.com/sonic-net/DASH/blob/main/documentation/acl/dash-acl.md).

#### D23. Synthetic packet generator and SLO probes

- **Category:** dashd + dash-sim / continuous testing
- **Problem:** TraceFlow is on-demand; production needs continuous correctness probes ("does Vnet A still reach Vnet B once per minute").
- **Use cases:** Catch a botched ACL deploy within minutes; trend reachability SLA per tenant; provide synthetic-test evidence for SLAs.
- **Capability:** A probe scheduler that generates synthetic packets, asserts expected verdicts, and exports counters / alerts on regression.
- **Functional:** Probes defined declaratively (`SloProbe` resource); results persisted; UI dashboard.
- **Non-functional:** Probe rate bounded; results retained per documented window.
- **Business value:** Continuous reachability SLO; objective evidence post-deploy.
- **Metrics:** Probe success rate per tenant; mean time to detect regression.
- **Dependencies:** F27 anomaly injection; D22 analyzer.
- **Spec refs:** [`dash-bmv2-data-plane-app.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-bmv2-data-plane-app.md).

#### D24. Configuration drift heatmap fleet-wide

- **Category:** dashw / observability
- **Problem:** Current drift signals are per-DPU and not visualised at fleet scale.
- **Use cases:** Detect a regional drift cluster; identify the DPU SKU with the highest drift rate; prioritise remediation.
- **Capability:** Heatmap of drift by DPU / zone / SKU / time, with drill-down to specific objects.
- **Functional:** Aggregated drift counters; tooltip → drift detail view.
- **Non-functional:** Aggregation cost bounded.
- **Business value:** Turns drift from a per-DPU annoyance into a fleet-management dashboard.
- **Metrics:** Mean drift count per DPU per day.
- **Dependencies:** F35 consistency audit; F6 aggregation.
- **Spec refs:** None (DashCenter-native feature applied to DASH).

#### D25. Per-tenant traffic-flow attribution

- **Category:** dashw / multi-tenant
- **Problem:** Cluster operators serving many tenants need traffic-by-tenant attribution from the existing telemetry (counters + flow tables).
- **Use cases:** Identify the noisy tenant during congestion; produce tenant billing inputs; SLA reporting.
- **Capability:** Counter rollups joined to tenant labels; per-tenant top-N flow view.
- **Functional:** Filter dashboards by tenant; export as report.
- **Non-functional:** Cost aligned with F6 rollups.
- **Business value:** Multi-tenant operability; foundation for F60 chargeback.
- **Metrics:** Adoption by platform teams.
- **Dependencies:** F6 rollups; F37 quotas; F60 cost allocation.
- **Spec refs:** None (DashCenter-native).

#### D26. Topology import from Netbox / Infoblox / external CMDB

- **Category:** dashd / integration
- **Problem:** Many ops teams already maintain network sources of truth (Netbox, Infoblox) that DashCenter should align with rather than duplicate.
- **Use cases:** Import DPU inventory from Netbox; reconcile IP allocations against Infoblox; pull rack/PSU metadata.
- **Capability:** Pluggable importers with periodic sync and divergence alerts.
- **Functional:** Importer config in `dashd.yaml`; per-source sync metrics; divergence detection.
- **Non-functional:** Source-of-truth conflict resolution policy documented.
- **Business value:** Removes a common dual-entry pain point for operators.
- **Metrics:** Number of sources synced; divergence count.
- **Dependencies:** F7 admission webhooks (for blocking on divergence).
- **Spec refs:** None (DashCenter-native).

---

### Section E — DASH conformance and certification

#### D27. DASH PTF conformance runner

- **Category:** dashctl / certification
- **Problem:** DASH ships PTF (Packet Test Framework) conformance tests but operators have no opinionated runner integrated with DashCenter.
- **Use cases:** Certify a new DPU SKU pre-deployment; validate firmware upgrades; gate fleet rollouts on conformance.
- **Capability:** `dashctl conformance run --target <dpu>` invoking upstream PTF tests, reporting pass/fail per scenario.
- **Functional:** Scenario catalog versioned; results stored; dashw certification view.
- **Non-functional:** Runner reproducible in CI environments.
- **Business value:** Enables informed vendor selection and upgrade validation.
- **Metrics:** Number of DPU SKUs certified; conformance pass rate.
- **Dependencies:** F5 hardware diagnostics parity.
- **Spec refs:** [`DASH/test/`](https://github.com/sonic-net/DASH/tree/main/test).

#### D28. DPU capability matrix visualization

- **Category:** dashw / certification
- **Problem:** Heterogeneous DPU fleets advertise different capabilities. Today operators learn this by trial and error.
- **Use cases:** Plan a deployment around partial PrivateLink support; route capacity-aware placement around capability differences; track capability drift across firmware versions.
- **Capability:** A matrix view (DPUs × features) sourced from capability advertisements; export to CSV.
- **Functional:** Real-time refresh; filter by tenant, zone, firmware.
- **Non-functional:** Matrix renders in defined time at fleet scale.
- **Business value:** Capacity and security planning across mixed fleets.
- **Metrics:** Adoption in planning workflows.
- **Dependencies:** Existing inventory and capability tracking.
- **Spec refs:** [`dash-bmv2-data-plane-app.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-bmv2-data-plane-app.md).

#### D29. Behavior-model integration (DASH P4 reference)

- **Category:** dash-sim / certification
- **Problem:** The DASH P4 reference model in `dash-pipeline` is the gold-standard ground truth. Our simulator currently lacks parity tests against it.
- **Use cases:** Continuous behavior parity testing in CI; flag any divergence between dash-sim and the reference model; build operator confidence.
- **Capability:** Parity test harness that drives both dash-sim and the P4 model with identical packets and asserts identical verdicts.
- **Functional:** PR-gating tests for each scenario in the upstream PTF suite.
- **Non-functional:** Test runtime bounded; failures reported with actionable diffs.
- **Business value:** Strong ground-truth alignment claim — major credibility signal.
- **Metrics:** Parity coverage percentage.
- **Dependencies:** F25 sim flow tables; F26 HA flow-owner.
- **Spec refs:** [`DASH/dash-pipeline`](https://github.com/sonic-net/DASH/tree/main/dash-pipeline).

#### D30. Continuous correctness verification (canary + reference model)

- **Category:** dashd / reliability
- **Problem:** Even after deployment, slow regressions can sneak in. Continuous verification across the fleet would surface them early.
- **Use cases:** Detect a firmware regression on a single DPU within hours; trend correctness across the fleet.
- **Capability:** Periodic synthetic packet runs (D23) compared against the behavior-model expected output (D29).
- **Functional:** Scheduled; results aggregated and alertable.
- **Non-functional:** Verification overhead bounded.
- **Business value:** Removes a large class of slow-onset regressions.
- **Metrics:** Number of regressions caught pre-impact.
- **Dependencies:** D23, D29.
- **Spec refs:** Industry SLO best practice; DASH test framework.

---

### Section F — Ultra-modern diagnostic platform

#### D31. AI-assisted diagnostic and natural-language query

- **Category:** dashw + dashd / modern diagnostics
- **Problem:** Operators spend most of their time turning natural questions ("why is tenant blue's east-west traffic dropping?") into queries across counters, logs, traces, and topology.
- **Use cases:** Natural-language entry returns a synthesized diagnosis (matched ACL rule, suspected meter, capacity headroom, recent change).
- **Capability:** An optional LLM-backed assistant trained on the DashCenter API surface and the DASH HLD corpus; reads counters, audit log, dependency graph, and trace results; produces a structured report with citations to underlying objects.
- **Functional:** Open-text input; output references concrete resource IDs and links; assistant cannot mutate state.
- **Non-functional:** Configurable model backend (local or hosted); zero PII leakage; rate-limited.
- **Business value:** Dramatic operator-productivity uplift; differentiating capability vs incumbent controllers.
- **Metrics:** Mean time-to-diagnosis; operator NPS.
- **Dependencies:** F22 dependency graph; D17 drop attribution; D15 stage tracer.
- **Spec refs:** None (DashCenter-native modern capability).

#### D32. ChatOps integration (Slack / Teams / PagerDuty bot)

- **Category:** Cross-cutting / integration
- **Problem:** Modern incident-response workflows live in chat tools, not browsers.
- **Use cases:** Query DashCenter ("@dashw reach eni-bank-web-01 → vnet-bank-db port 1433") from Slack; receive incident alerts and acknowledge in chat; trigger a planned switchover with approval.
- **Capability:** A first-party ChatOps bot integrating with Slack, Teams, and PagerDuty.
- **Functional:** Read queries via slash commands; mutating commands route through approval (F59); rich-card responses.
- **Non-functional:** Auditable; respects RBAC and quotas.
- **Business value:** Embeds DashCenter into modern incident-response workflows.
- **Metrics:** Adoption across incident war rooms.
- **Dependencies:** F4 OIDC; F59 approval workflows; D31 assistant.
- **Spec refs:** None (industry standard).

---

## 6. Reference architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  CLIENT EXPERIENCE                                              │
│  dashctl  •  dashw (incl. D9-D11, D15, D20-D22, D31, D32)       │
└─────────────────────────────────────────────────────────────────┘
                              ▲ ▼
┌─────────────────────────────────────────────────────────────────┐
│  CONTROL PLANE — dashd                                          │
│  • Northbound coverage: D1-D8 (route_type, appliance, prefix    │
│    tag, PA, port map, meter, qos, ha_scope)                     │
│  • HA orchestration: D12-D14 workflows                          │
│  • Diagnostics: D15 stage tracer, D17 attribution, D21 cross-   │
│    DPU correlation, D22 reachability, D23 SLO probes            │
│  • Observability: D16 counters, D18 fast/slow path, D19 ECMP    │
│  • Conformance: D27 PTF runner, D28 capability matrix           │
└─────────────────────────────────────────────────────────────────┘
                              ▲ ▼
┌─────────────────────────────────────────────────────────────────┐
│  SIMULATOR / REFERENCE — dash-sim                               │
│  • F25 flow tables, F26 HA flow-owner, D29 P4 parity tests      │
│  • F27 anomaly injection for D16 counter validation             │
└─────────────────────────────────────────────────────────────────┘
                              ▲ ▼
┌─────────────────────────────────────────────────────────────────┐
│  DPU FLEET (DASH-compliant agents)                              │
│  • Reports DASH counters → D16 telemetry                        │
│  • Returns DASH state machine → D9-D14 HA                       │
│  • Carries fleet-wide flow context → D21 correlation            │
└─────────────────────────────────────────────────────────────────┘
```

---

## 7. Sequencing roadmap

Three waves balance customer impact, dependency order, and team
focus. Each item is sized so a focused team can deliver it as one PR
series.

### Wave 1 — DASH-coverage foundation (weeks 1–8)

| Order | Item | Why |
|---|---|---|
| 1 | D8 HA Scope resource | Prerequisite for all HA visibility |
| 2 | D1 Route-type catalog | Cheap, immediate clarity in admission errors |
| 3 | D3 Prefix-tag resource | Most-requested ACL simplifier |
| 4 | D6 Meter / MeterPolicy / MeterRule | Unlocks QoS + rate-limit features |
| 5 | D4 PA-validation | Security baseline; needed for inbound parity |
| 6 | D5 Outbound port map | Completes PrivateLink end-to-end |
| 7 | D2 Routing-appliance | Cleaner appliance lifecycle |
| 8 | D7 QoS policy | Multi-tenant SLA enabler |

### Wave 2 — HA and pipeline observability (weeks 8–16)

| Order | Item | Why |
|---|---|---|
| 9 | D16 Counter taxonomy expansion | Foundation for every observability feature |
| 10 | D17 Drop-reason attribution | Highest-value diagnostic uplift |
| 11 | D15 Per-stage pipeline tracer | Operator's primary triage tool |
| 12 | D9 HA state-machine visualizer | Mission-critical HA visibility |
| 13 | D10 DP-channel probe health | HA degradation detector |
| 14 | D11 Inline + bulk flow-sync metrics | HA correctness signal |
| 15 | D18 Fast/slow path classification | Performance-affecting misconfig detector |
| 16 | D19 ECMP traffic distribution | Common silent-failure mode |
| 17 | D20 Meter token-bucket view | Completes meter feature loop |

### Wave 3 — Fleet diagnostics and modern UX (weeks 16–28)

| Order | Item | Why |
|---|---|---|
| 18 | D22 Reachability analyzer | Differentiating capability |
| 19 | D23 SLO probes / synthetic packet generator | Continuous SLA |
| 20 | D21 Multi-DPU flow correlation | Eliminates highest-effort triage class |
| 21 | D12 Planned/unplanned switchover wrappers | Codifies safe HA operations |
| 22 | D13 Split-brain detection | Worst-case-outage safety net |
| 23 | D27 PTF conformance runner | Multi-vendor fleet credibility |
| 24 | D28 Capability matrix | Planning and procurement |
| 25 | D24 Drift heatmap | Fleet-level operability |
| 26 | D25 Tenant traffic attribution | Multi-tenant ops + chargeback foundation |
| 27 | D29 Behavior-model parity tests | Ground-truth confidence claim |
| 28 | D30 Continuous correctness verification | Detect slow-onset regressions |
| 29 | D14 DPU-driven HA mode | Multi-vendor parity |
| 30 | D26 External CMDB import | Removes operator dual-entry |
| 31 | D31 AI-assisted diagnostics | Modern operator productivity |
| 32 | D32 ChatOps integration | Embeds DashCenter into incident workflows |

---

## 8. Appendix — Spec references

The proposals above are grounded in these upstream DASH documents:

- [`sonic-net/DASH/documentation`](https://github.com/sonic-net/DASH/tree/main/documentation) — top-level HLDs.
- [`dash-routing-actions.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-routing-actions.md) — multi-stage pipeline + transforms.
- [`dash-acl.md`](https://github.com/sonic-net/DASH/blob/main/documentation/acl/dash-acl.md) — ACL stages and tag references.
- [`dash-flow-api.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-flow-api.md) — flow tables and conntrack.
- [`dash-bmv2-data-plane-app.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-bmv2-data-plane-app.md) — behavioral reference.
- [`sdn-features-packet-transforms.md`](https://github.com/sonic-net/DASH/blob/main/documentation/general/sdn-features-packet-transforms.md) — transform catalog + counters.
- [`ha-api-hld.md`](https://github.com/sonic-net/DASH/blob/main/documentation/high-avail/ha-api-hld.md) — HA state machines, flow sync, switchover.
- [`high-availability-and-scale.md`](https://github.com/sonic-net/DASH/blob/main/documentation/high-avail/high-availability-and-scale.md) — HA topology.
- [`dash-metering.md`](https://github.com/sonic-net/DASH/blob/main/documentation/general/dash-metering.md) — meter / meter-policy / meter-rule semantics.
- [`dash-private-link.md`](https://github.com/sonic-net/DASH/blob/main/documentation/general/dash-private-link.md) — PrivateLink wiring.
- [`dash-flow-resimulation.md`](https://github.com/sonic-net/DASH/blob/main/documentation/dataplane/dash-flow-resimulation.md) — resimulation counters.
- [`DASH/dash-pipeline/`](https://github.com/sonic-net/DASH/tree/main/dash-pipeline) — P4 reference dataplane.
- [`DASH/test/`](https://github.com/sonic-net/DASH/tree/main/test) — PTF conformance suite.

Internal references:

- [`docs/dash-sim-on-par-with-sonic-audit.md`](../dash-sim-on-par-with-sonic-audit.md) — comprehensive simulator alignment audit.
- [`docs/roadmap.md`](../roadmap.md) §6.7 — F-series and L-series backlog.
- [`docs/dashd-features/FEATURE_ASK.md`](./FEATURE_ASK.md) — long-form feature requirements (F1–F60, L1–L10).
- [`docs/dashd-features/BUGS.md`](./BUGS.md) — tracked bugs (B1–B5).
- [`proto/vendor/sonic-dash-api/`](../../proto/vendor/sonic-dash-api/) — vendored DASH proto schema (29 kinds).
