# Referential Integrity Validation — Gap Analysis + Design Proposal

> **Audience**: dashd / dash-sim maintainers, operators managing
> multi-tenant DASH fleets, SDN controller integrators.
> **Scope**: write-time foreign-key validation across all 29 object
> kinds in both the northbound (dashd `dashcenter.v1` surface) and
> southbound (dash-sim `dashapi.v1` surface) layers; delete-side
> orphan protection; operational tooling.
> **Companion docs**:
> [sonic_dash_comparison_gap.md](sonic_dash_comparison_gap.md)
> (pipeline stage deviations),
> [dash-sim-on-par-with-sonic-audit.md](../dash-sim-on-par-with-sonic-audit.md)
> (sim parity roadmap),
> [features.md §5](features.md) (CRUD reference),
> [CLI_GUIDE.md](../CLI_GUIDE.md) (dashctl command reference).
> **Status**: ⏳ Phase 1 (dash-sim southbound) **implemented**.
> Phases 2–3 (dashd northbound, delete-side, tooling) are proposed.

---

## Table of contents

1. [Problem statement](#1-problem-statement)
2. [Gap analysis — current validation coverage](#2-gap-analysis--current-validation-coverage)
3. [Object dependency graph](#3-object-dependency-graph)
4. [Impact analysis — what breaks without validation](#4-impact-analysis--what-breaks-without-validation)
5. [Design proposal](#5-design-proposal)
6. [Approach options — pros/cons](#6-approach-options--proscons)
7. [Final verdict](#7-final-verdict)
8. [Implementation phases](#8-implementation-phases)
9. [Required creation order (operator-facing)](#9-required-creation-order-operator-facing)
10. [Future Scopes](#10-future-scopes)

---

## 1. Problem statement

The DASH pipeline is a **deeply interconnected object graph**. An ENI
is not functional unless its parent VNet exists, its ACL groups are
populated with rules, its outbound route chain (eni_route → route_group
→ routes → vnet_mappings) is wired, and its inbound route_rules are
configured. Every object kind references 1–5 other kinds via string-
typed foreign keys.

**Today, DashCenter validates only 5 of ~35 foreign-key relationships
at write time.** The remaining ~30 are silently accepted — dangling
references sit in the store and only surface as packet-time DROPs (in
the sim) or as silent wrong behavior (VNI=0, missing encap). Operators
discover config errors minutes or hours after the fact, via counter
spikes or flow traces, instead of at the `PUT` that caused them.

The delete path has **zero orphan protection**. Deleting a VNet while
10 ENIs reference it succeeds silently — those ENIs immediately start
dropping all traffic with no warning.

### Operator pain today

> "I created an ENI referencing `vnet-blue`, then realized I typo'd
> the vnet name as `vnet-bllue`. The PUT succeeded (200 OK). I only
> found out 20 minutes later when the ENI's counter sparkline flatlined
> and I traced a packet to see `DROP: vnet_mapping vnet-bllue/10.0.0.1:
> not found`. I lost 20 minutes of customer traffic."

> "I deleted `vnet-prod` thinking nothing was using it. 15 ENIs went
> dark. No error from dashd. The only signal was a spike in `drop_acl_in`
> across 3 DPUs."

---

## 2. Gap analysis — current validation coverage

### 2.1 dashd northbound (`namespace.Validator`)

| Object kind | FK field | Referenced kind | Validated today? | Location |
|---|---|---|---|---|
| **eni** | `vnet_name` | `vnet` | ✅ Yes | `CheckEni` |
| **eni** | `qos` | `qos` | ❌ No | — |
| **eni** | `v4_meter_policy_id` | `meter_policy` | ❌ No | — |
| **eni** | `v6_meter_policy_id` | `meter_policy` | ❌ No | — |
| **vnet_mapping** | `vnet_name` | `vnet` | ✅ Yes | `CheckVnetMapping` |
| **vnet_mapping** | `tunnel` | `service_tunnel` | ❌ No | — |
| **acl_policy** | `eni_names[]` | `eni` (each) | ✅ Yes | `CheckAclPolicy` |
| **route_policy** | `eni_names[]` | `eni` (each) | ✅ Yes | `CheckRoutePolicy` |
| **route_policy** | `routes[].next_hop_target` (type=vnet) | `vnet` | ✅ Yes | `CheckRoutePolicy` |
| **route_policy** | `routes[].next_hop_target` (type=service_tunnel) | `service_tunnel` | ❌ No | — |
| **route_policy** | `routes[].next_hop_target` (type=appliance) | (routing_appliance — sim-side) | ❌ No | — |
| **ha_set** | `member_dpu_ids[]` | inventory DPU | ❌ No | — |

**Score: 5 / 12 northbound FK relationships validated (42%).**

### 2.2 dash-sim southbound (`model.Store.Apply`)

| Object kind | FK field | Referenced kind | Validated? | Location |
|---|---|---|---|---|
| `acl_rule` | Key `group_id` | `acl_group` | ✅ Yes | `refs.go` fkRules |
| `acl_rule` | `src_tag[]`, `dst_tag[]` | `prefix_tag` | ✅ Yes | `refs.go` fkRules |
| `route` | Key `group_id` | `route_group` | ✅ Yes | `refs.go` fkRules |
| `route` | `vnet` / `vnet_direct.vnet` | `vnet` | ✅ Yes | `refs.go` fkRules |
| `route` | `appliance` | `routing_appliance` | ✅ Yes | `refs.go` fkRules |
| `route` | `tunnel` | `tunnel` | ✅ Yes | `refs.go` fkRules |
| `vnet_mapping` | Key `vnet` | `vnet` | ✅ Yes | `refs.go` fkRules |
| `vnet_mapping` | `tunnel` | `tunnel` | ✅ Yes | `refs.go` fkRules |
| `vnet_mapping` | `port_map` | `outbound_port_map` | ✅ Yes | `refs.go` fkRules |
| `eni` | `vnet` | `vnet` | ✅ Yes | `refs.go` fkRules |
| `eni` | `qos` | `qos` | ✅ Yes | `refs.go` fkRules |
| `eni_route` | Key `eni` | `eni` | ✅ Yes | `refs.go` fkRules |
| `eni_route` | `group_id` | `route_group` | ✅ Yes | `refs.go` fkRules |
| `acl_in` | Key `eni` | `eni` | ✅ Yes | `refs.go` fkRules |
| `acl_in` | `v4/v6_acl_group_id` | `acl_group` | ✅ Yes | `refs.go` fkRules |
| `acl_out` | Key `eni` | `eni` | ✅ Yes | `refs.go` fkRules |
| `acl_out` | `v4/v6_acl_group_id` | `acl_group` | ✅ Yes | `refs.go` fkRules |
| `route_rule` | Key `eni` | `eni` | ✅ Yes | `refs.go` fkRules |
| `route_rule` | `vnet` | `vnet` | ✅ Yes | `refs.go` fkRules |
| `meter_rule` | Key `meter_policy_id` | `meter_policy` | ✅ Yes | `refs.go` fkRules |
| `meter` | Key `eni` | `eni` | ✅ Yes | `refs.go` fkRules |
| `opm_range` | Key `map_id` | `outbound_port_map` | ✅ Yes | `refs.go` fkRules |
| `ha_scope` | `ha_set_id` | `ha_set` | ✅ Yes | `refs.go` fkRules |
| `ha_scope_config` | Key `ha_scope_id` + `ha_set_id` | `ha_scope` + `ha_set` | ✅ Yes | `refs.go` fkRules |
| `ha_scope_state` | Key `ha_scope_id` | `ha_scope` | ✅ Yes | `refs.go` fkRules |

**Score: 25 / 25 southbound FK relationships validated (100%).** ✅

> Implemented in `model/refs.go` via a declarative `fkRules` table.
> `Store.Apply()` calls `checkRefs()` under the write lock before
> persisting. Controlled by `--strict-refs` CLI flag (default `true`).

### 2.3 Delete-side orphan protection

| On delete of | Dependents that become orphaned | Protected? |
|---|---|---|
| `vnet` | ENIs, VnetMappings, Routes (type=vnet), RoutePolicies | ❌ No |
| `eni` | AclPolicies, RoutePolicies, eni_route, acl_in/out, route_rule, meter | ❌ No |
| `acl_group` | acl_rules, acl_in/out bindings | ❌ No |
| `route_group` | routes, eni_route bindings | ❌ No |
| `service_tunnel` | RoutePolicies, VnetMappings | ❌ No |
| `qos` | ENIs | ❌ No |
| `meter_policy` | meter_rules, ENIs | ❌ No |
| `ha_set` | ha_scope, ha_set_config, ha_set_state | ❌ No |
| `prefix_tag` | acl_rules (src_tag/dst_tag) | ❌ No |
| `tunnel` | routes, vnet_mappings | ❌ No |
| `outbound_port_map` | opm_ranges, vnet_mappings | ❌ No |
| `routing_appliance` | routes (type=appliance) | ❌ No |

**Score: 0 / 12 delete-side protections (0%).**

### 2.4 Summary

| Surface | Validated | Total | Coverage |
|---|---|---|---|
| dashd northbound (Put) | 5 | 12 | **42%** |
| dash-sim southbound (Apply) | **25** | 25 | **100%** ✅ |
| Delete orphan protection | 0 | 12 | **0%** |
| **Overall** | **30** | **~49** | **~61%** |

---

## 3. Object dependency graph

### 3.1 Tiered creation order

```
Tier 0 — Roots (no dependencies, create first):
  appliance, vnet, qos, acl_group, route_group, route_type,
  routing_appliance, prefix_tag, tunnel, meter_policy,
  outbound_port_map, pa_validation, ha_set

Tier 1 — References Tier 0 only:
  eni (→ vnet, qos, meter_policy)
  acl_rule (→ acl_group, prefix_tag)
  route (→ route_group, vnet, routing_appliance, tunnel)
  vnet_mapping (→ vnet, tunnel, outbound_port_map)
  meter_rule (→ meter_policy)
  outbound_port_map_range (→ outbound_port_map)
  ha_scope (→ ha_set)
  ha_set_config (→ ha_set)
  ha_set_state (→ ha_set)

Tier 2 — References Tier 0 + Tier 1:
  eni_route (→ eni, route_group)
  acl_in (→ eni, acl_group)
  acl_out (→ eni, acl_group)
  route_rule (→ eni, vnet, prefix_tag)
  meter (→ eni)
  ha_scope_config (→ ha_scope, ha_set)
  ha_scope_state (→ ha_scope)
```

### 3.2 Full FK map (compact)

```
appliance           → (nothing)
vnet                → (nothing)
qos                 → (nothing)
route_group         → (nothing)
route_type          → (nothing)
routing_appliance   → (nothing)
prefix_tag          → (nothing)
tunnel              → (nothing)
meter_policy        → (nothing)
outbound_port_map   → (nothing)
pa_validation       → (nothing)
ha_set              → (nothing)

eni                 → vnet(Eni.vnet), qos(Eni.qos),
                      meter_policy(Eni.v4/v6_meter_policy_id)
acl_rule            → acl_group(Key.group_id),
                      prefix_tag(src_tag[], dst_tag[])
route               → route_group(Key.group_id), vnet(Route.vnet),
                      routing_appliance(Route.appliance),
                      tunnel(Route.tunnel)
vnet_mapping        → vnet(Key.vnet), tunnel(VnetMapping.tunnel),
                      outbound_port_map(VnetMapping.port_map)
meter_rule          → meter_policy(Key.meter_policy_id)
opm_range           → outbound_port_map(Key.map_id)
ha_scope            → ha_set(HaScope.ha_set_id)
ha_set_config       → ha_set(Key.ha_set_id)
ha_set_state        → ha_set(Key.ha_set_id)

eni_route           → eni(Key.eni), route_group(EniRoute.group_id)
acl_in              → eni(Key.eni), acl_group(v4/v6_acl_group_id)
acl_out             → eni(Key.eni), acl_group(v4/v6_acl_group_id)
route_rule          → eni(Key.eni), vnet(RouteRule.vnet),
                      prefix_tag(Key.tag)
meter               → eni(Key.eni)
ha_scope_config     → ha_scope(Key.ha_scope_id), ha_set(ha_set_id)
ha_scope_state      → ha_scope(Key.ha_scope_id)
```

---

## 4. Impact analysis — what breaks without validation

### 4.1 Silent failures at packet-processing time

| Missing reference | Pipeline stage | Result | Severity |
|---|---|---|---|
| ENI's vnet doesn't exist | VNet lookup for VNI resolution | `vnetVNI()` returns 0 → wrong encap | **HIGH** — silent wrong behavior |
| acl_group for acl_in doesn't exist | ACL stage | **Silently skipped** — all traffic permitted | **CRITICAL** — security bypass |
| eni_route doesn't exist | Outbound step 3 | DROP: `"eni_route not found"` | HIGH |
| route_group empty | Outbound step 3 | DROP: `"no route matches"` | HIGH |
| vnet_mapping missing | VNET encap action | DROP: `"vnet_mapping not found"` | HIGH |
| routing_appliance missing | Appliance action | DROP: `"routing_appliance not found"` | MEDIUM |
| prefix_tag missing | ACL tag matching | **Not implemented** — silently ignored | MEDIUM |
| qos missing | ENI attrs | **Not enforced** — no QoS applied | LOW |

### 4.2 Delete-cascade disasters

| Deleted object | Orphaned dependents | Operator-visible symptom |
|---|---|---|
| `vnet` while ENIs reference it | ENIs → all traffic drops | Counter sparkline flatline on all affected DPUs |
| `acl_group` while acl_in binds to it | ACL stage → silently skipped → all traffic permitted | **Security incident** — no ACLs enforced |
| `route_group` while routes live in it | Routes become unreachable → DROPs | Flow trace shows `"no route matches"` |
| `eni` while acl_in/out/route_rule/meter reference it | Orphaned bindings → apply silently succeeds but no ENI to attach to | Silent config drift |

### 4.3 Operational cost of late-binding errors

| Metric | Without validation | With validation |
|---|---|---|
| Time to detect config error | Minutes to hours (when traffic hits) | **Immediate** (at PUT time, 400 response) |
| Blast radius of a typo | All traffic on affected ENIs | Zero (rejected before store write) |
| Root cause identification | Flow trace + counter analysis + cross-referencing object names | Error message names the exact missing reference |
| Rollback complexity | Undo the delete that orphaned 15 ENIs | Never happened — delete was rejected |

---

## 5. Design proposal

### 5.1 Core design

Extend the existing `namespace.Validator` pattern with a new
**`referential.Validator`** that checks every FK at write time.

```go
// internal/referential/validator.go
type Validator struct {
    store store.ReadStore  // Get + List for existence checks
    inv   *inventory.Inventory  // for DPU ID validation
    mode  Mode  // Strict | Warn | Off
}

type Mode int
const (
    ModeStrict Mode = iota  // reject on missing ref (default)
    ModeWarn                // log.Warn + accept
    ModeOff                 // skip all checks
)

func (v *Validator) CheckPut(ctx context.Context, ns, kind, name string, spec proto.Message) error
func (v *Validator) CheckDelete(ctx context.Context, ns, kind, name string) error
```

### 5.2 Wire integration

```
PUT /v1/{ns}/{kind}/{name}
  → auth middleware
  → namespace.Validator.CheckSpecNamespace()
  → namespace.Validator.Check{Kind}()          ← existing (5 checks)
  → referential.Validator.CheckPut()           ← NEW (all FKs)
  → store.Put()

DELETE /v1/{ns}/{kind}/{name}
  → auth middleware
  → referential.Validator.CheckDelete()        ← NEW (orphan scan)
  → store.Delete()
```

### 5.3 Error responses

**Put with missing reference:**
```json
{
  "error": "referential integrity violation: eni \"eni-001\" references vnet \"vnet-bllue\" which does not exist in namespace \"default\"; create the vnet first or fix the reference",
  "code": "FAILED_PRECONDITION",
  "details": {
    "kind": "eni",
    "name": "eni-001",
    "field": "vnet_name",
    "referenced_kind": "vnet",
    "referenced_name": "vnet-bllue",
    "namespace": "default"
  }
}
```
HTTP 400. gRPC `FailedPrecondition`.

**Delete with dependents:**
```json
{
  "error": "cannot delete vnet \"vnet-prod\": 3 dependents still reference it",
  "code": "FAILED_PRECONDITION",
  "dependents": [
    {"kind": "eni", "name": "eni-web-01"},
    {"kind": "eni", "name": "eni-web-02"},
    {"kind": "vnet_mapping", "name": "vnet-prod/10.0.0.1"}
  ]
}
```
HTTP 409 Conflict. gRPC `FailedPrecondition`.

**Force delete** (`?force=true`): bypasses orphan check, logs a WARN
audit entry. For emergency operations only.

### 5.4 Configuration

```yaml
# dashd.yaml
validation:
  referential_integrity: strict   # strict | warn | off
  # strict: reject puts with missing refs, reject deletes with orphans
  # warn:   log WARN but accept (migration mode)
  # off:    skip all referential checks (backward-compat escape hatch)
```

### 5.5 Batch / ApplyBatch handling

Within a batch, objects created earlier satisfy FKs for later objects.
The validator processes the batch in order, maintaining a "pending
creates" set that augments the store for lookups:

```
ApplyBatch([
  { PUT vnet/vnet-blue },           ← Tier 0, no FK
  { PUT eni/eni-001 (vnet=vnet-blue) },  ← Tier 1, FK to vnet-blue
                                          resolved from pending set ✅
])
```

---

## 6. Approach options — pros/cons

### Option A: Inline validation in each `Check{Kind}` method (extend existing pattern)

| Pros | Cons |
|---|---|
| Follows established pattern exactly | Each kind gets its own method → N methods to maintain |
| Easy to review (one kind at a time) | FK logic scattered across 12+ methods |
| Zero new abstractions | Adding a new kind requires remembering to add a Check method |
| Already proven for the 5 existing checks | |

### Option B: Declarative FK registry + generic validator

```go
var fkRules = []FKRule{
    {Kind: "eni", Field: "vnet_name", RefKind: "vnet", Required: true},
    {Kind: "eni", Field: "qos", RefKind: "qos", Required: false},
    // ... 35 more rules
}
```

| Pros | Cons |
|---|---|
| Single source of truth for ALL FKs | New abstraction to learn |
| Adding a kind = adding rows, not methods | Requires a generic spec-field extractor (reflection or codegen) |
| Delete-side orphan scan = reverse-query the registry | Harder to add kind-specific business logic (e.g., "only validate vnet ref when route type=VNET") |
| Easily testable (table-driven) | Conditional FKs (oneof, optional) need special handling |
| Self-documenting — the registry IS the dependency graph | |

### Option C: Hybrid — declarative registry for simple FKs + inline methods for conditional FKs

| Pros | Cons |
|---|---|
| Best of both: table-driven for 80% of cases | Two patterns to maintain |
| Inline methods for the 20% that need conditionals (route.vnet only when type=VNET) | Slightly more complex codebase |
| Delete-side orphan scan uses the registry (reverse lookup) | |
| Registry is the single source of truth for docs + tooling | |

### Option D: Validate only at the dashd northbound layer (skip sim-side)

| Pros | Cons |
|---|---|
| Smallest scope — only ~12 dashd-side FKs | dash-sim still accepts orphaned objects via direct gRPC |
| Fastest to ship | Operators using `dash-sim-client apply` directly bypass all validation |
| dashd is the control plane — validation belongs here | Sim integration tests can create invalid state |

---

## 7. Final verdict

**Option C (Hybrid) + sim-first, dashd-second (bottom-up).**

Rationale:

1. **dash-sim first (Phase 1)** because the sim is the lowest layer —
   the device that actually processes packets. If the sim accepts
   invalid objects, everything above it is undermined. `dash-sim-client
   apply` bypasses dashd entirely — the sim is the ONLY validation
   point for that path. Defense-in-depth starts at the bottom, not
   the top.

2. **Declarative registry** for the ~30 simple FKs (field X of kind Y
   must exist as kind Z). Table-driven, testable, self-documenting.

3. **Inline methods** (extend existing `Check{Kind}`) for the ~5
   conditional FKs: route.vnet only when type=VNET, route.appliance
   only when type=APPLIANCE, etc.

4. **dashd northbound second (Phase 2)** adds controller-level
   validation on top of the sim's device-level validation. dashd
   validates the dashcenter.v1 spec shapes + cross-namespace rules +
   inventory references (DPU IDs). Some FKs are only visible at this
   layer (e.g., HaSet → inventory DPUs).

5. **Delete-side orphan protection** (Phase 2b) because deleting a
   parent object is the highest-blast-radius operation and has zero
   protection today.

6. **Operational tooling** (Phase 3) for `dashctl validate` and
   `dash-sim-client validate` — scan existing stores for dangling
   references. Critical for migrating existing deployments to strict
   mode.

---

## 8. Implementation phases

### Phase 1: dash-sim + dash-sim-client Apply-side FK validation (~2-3 days)

**Rationale**: the sim is the lowest layer — the device that actually
processes packets. If the sim accepts invalid objects, everything above
it (dashd, dashctl, SPA) is undermined. Direct `dash-sim-client apply`
bypasses dashd entirely — the sim is the ONLY validation point for that
path. Defense-in-depth starts at the bottom.

**Scope**: extend `model.Store.Apply()` with FK checks for all 25
southbound relationships. Return gRPC `FailedPrecondition` with clear
error in `Ack.error` naming the missing referenced object.

| FK to validate | On Apply of | FK field | Referenced kind |
|---|---|---|---|
| acl_rule → acl_group | `acl_rule` | Key `group_id` | `acl_group` |
| acl_rule → prefix_tag | `acl_rule` | `src_tag[]`, `dst_tag[]` | `prefix_tag` |
| route → route_group | `route` | Key `group_id` | `route_group` |
| route → vnet | `route` | `vnet` / `vnet_direct.vnet` | `vnet` |
| route → routing_appliance | `route` | `appliance` | `routing_appliance` |
| route → tunnel | `route` | `tunnel` | `tunnel` |
| vnet_mapping → vnet | `vnet_mapping` | Key `vnet` | `vnet` |
| vnet_mapping → tunnel | `vnet_mapping` | `tunnel` | `tunnel` |
| vnet_mapping → port_map | `vnet_mapping` | `port_map` | `outbound_port_map` |
| eni → vnet | `eni` | `vnet` | `vnet` |
| eni → qos | `eni` | `qos` | `qos` |
| eni_route → eni | `eni_route` | Key `eni` | `eni` |
| eni_route → route_group | `eni_route` | `group_id` | `route_group` |
| acl_in → eni | `acl_in` | Key `eni` | `eni` |
| acl_in → acl_group | `acl_in` | `v4/v6_acl_group_id` | `acl_group` |
| acl_out → eni | `acl_out` | Key `eni` | `eni` |
| acl_out → acl_group | `acl_out` | `v4/v6_acl_group_id` | `acl_group` |
| route_rule → eni | `route_rule` | Key `eni` | `eni` |
| route_rule → vnet | `route_rule` | `vnet` | `vnet` |
| meter_rule → meter_policy | `meter_rule` | Key `meter_policy_id` | `meter_policy` |
| meter → eni | `meter` | Key `eni` | `eni` |
| opm_range → port_map | `opm_range` | Key `map_id` | `outbound_port_map` |
| ha_scope → ha_set | `ha_scope` | `ha_set_id` | `ha_set` |
| ha_scope_config → ha_scope+ha_set | `ha_scope_config` | Key `ha_scope_id` + `ha_set_id` | both |
| ha_scope_state → ha_scope | `ha_scope_state` | Key `ha_scope_id` | `ha_scope` |

**Config**: `--strict-refs` CLI flag on `dash-sim` (default `true`).
`--strict-refs=false` for backward-compat in tests that create objects
in arbitrary order.

**dash-sim-client additions**:
- `apply` gains `--validate` flag (default on) so operators see the
  error before it hits the wire.
- New `dash-sim-client validate <file>` subcommand that dry-runs FK
  checks against the sim's current store without actually applying.

**Tests**: ~20 UTs covering each FK family + the `--strict-refs=false`
escape + existing integration tests updated to create objects in
correct order.

#### Phase 1 — Implementation status: ✅ DONE

**Files added/modified:**

| File | Change |
|---|---|
| `dash-sim/internal/sim/model/refs.go` (new) | Declarative `fkRules` table (25 rules) + `checkRefs()` + helpers |
| `dash-sim/internal/sim/model/refs_test.go` (new) | 51 unit tests — 100% coverage on `checkRefs`, `nonEmpty`, `kindNameOf` |
| `dash-sim/internal/sim/model/store.go` | `strictRefs` field, `SetStrictRefs()`, FK check in `Apply()` |
| `dash-sim/cmd/dash-sim/main.go` | `--strict-refs` CLI flag (default `true`) |
| `dash-sim/test/integration/refs_test.go` (new) | 9 gRPC integration tests (reject, accept, error quality, fix-retry) |
| `dash-sim/internal/sim/server/dpu_counters_test.go` | `SetStrictRefs(false)` in counter test helpers |
| `dash-sim/test/integration/dpu_counters_test.go` | `SetStrictRefs(false)` in counter integration harness |

**Test results:**

```
dash-sim model:       51 tests PASS   (checkRefs 100%, nonEmpty 100%, kindNameOf 100%)
dash-sim integration: 13 tests PASS   (4 counter + 9 FK validation)
dash-sim total:        5 packages green
dashd total:          28 packages green (no regressions)
dashctl total:         9 packages green (no regressions)
```

**How it works — walkthrough with examples:**

Example 1 — **Wrong config: ENI references a typo'd vnet name**

```bash
# Start dash-sim with strict-refs (default)
dash-sim --strict-refs

# Try to create an ENI that references "vnet-bllue" (typo)
dash-sim-client --target localhost:50051 apply --kind eni --key eni-001 \
  --value '{"vnet": "vnet-bllue"}'
```

**Result: REJECTED**
```
Apply rejected: referential integrity: eni references vnet "vnet-bllue"
(field vnet) which does not exist; create it first
```

The error names the exact missing object, the field that references
it, and tells the operator what to do. The ENI is NOT stored — no
silent corruption.

Example 2 — **Right config: create objects in correct order**

```bash
# Step 1: Create the vnet first (Tier 0 — no dependencies)
dash-sim-client --target localhost:50051 apply --kind vnet --key vnet-blue \
  --value '{}'

# Step 2: Create the ENI (Tier 1 — references vnet)
dash-sim-client --target localhost:50051 apply --kind eni --key eni-001 \
  --value '{"vnet": "vnet-blue"}'
```

**Result: ACCEPTED** — the ENI is stored because `vnet-blue` exists.

Example 3 — **Wrong config: Tier 2 before Tier 1**

```bash
# vnet exists, but ENI does not
dash-sim-client --target localhost:50051 apply --kind eni_route --key eni-missing \
  --value '{"group_id": "rg-prod"}'
```

**Result: REJECTED**
```
Apply rejected: referential integrity: eni_route references eni "eni-missing"
(field key.eni) which does not exist; create it first
```

Example 4 — **Fix-then-retry workflow**

```bash
# Step 1: Attempt fails (vnet-prod doesn't exist yet)
dash-sim-client apply --kind eni --key eni-001 --value '{"vnet":"vnet-prod"}'
# → rejected: vnet "vnet-prod" does not exist

# Step 2: Create the missing vnet
dash-sim-client apply --kind vnet --key vnet-prod --value '{"vni":1001}'
# → accepted

# Step 3: Retry the ENI — now succeeds
dash-sim-client apply --kind eni --key eni-001 --value '{"vnet":"vnet-prod"}'
# → accepted
```

Example 5 — **Backward compatibility: disable strict refs**

```bash
# For legacy pipelines that create objects in arbitrary order:
dash-sim --strict-refs=false

# Now any Apply succeeds regardless of FK order
dash-sim-client apply --kind eni --key eni-001 --value '{"vnet":"nonexistent"}'
# → accepted (no FK check)
```

### Phase 2a: dashd Put-side FK validation (~2 days)

**Scope**: extend `namespace.Validator` + new `referential.Validator`
with FK registry. Reject with 400/FailedPrecondition on missing refs.

| FK to add | On Put of | Field | Ref kind | Condition |
|---|---|---|---|---|
| ENI → qos | `eni` | `qos` | `qos` | when non-empty |
| ENI → meter_policy v4 | `eni` | `v4_meter_policy_id` | `meter_policy` | when non-empty |
| ENI → meter_policy v6 | `eni` | `v6_meter_policy_id` | `meter_policy` | when non-empty |
| RoutePolicy route → service_tunnel | `route_policy` | `routes[].next_hop_target` | `service_tunnel` | when type=`service_tunnel` |
| VnetMapping → tunnel | `vnet_mapping` | `tunnel` | `service_tunnel` | when non-empty |
| HaSet → DPU IDs | `ha_set` | `member_dpu_ids[]` | inventory | each DPU must exist |

**Config**: `validation.referential_integrity: strict|warn|off`.

**Tests**: ~15 UTs covering each FK + the warn/off modes + batch handling.

### Phase 2b: dashd Delete-side orphan protection (~1 day)

**Scope**: `referential.Validator.CheckDelete()` scans for dependents
before allowing delete. Returns 409 Conflict with dependent list.

| On delete of | Scan for dependents in |
|---|---|
| `vnet` | enis, vnet_mappings, route_policies (routes with vnet ref) |
| `eni` | acl_policies, route_policies |
| `service_tunnel` | route_policies, vnet_mappings |
| `qos` | enis |
| `meter_policy` | enis |
| `ha_set` | ha_sets (spec.ha_set_id refs) |

**Escape hatch**: `DELETE ...?force=true` bypasses the check + logs
WARN audit entry.

**Tests**: ~10 UTs covering each delete-protection case + force flag.

### Phase 3: Operational tooling (~1 day)

**Scope**:
- `dashctl validate` subcommand — scans entire dashd store, reports orphans.
- `dash-sim-client validate` subcommand — scans sim store for orphans.
- `GET /v1/diagnostics/validate-refs` — REST equivalent.
- SPA `/diagnostics` page — "Referential Integrity" card.

**Tests**: ~5 UTs + live e2e.

### Summary

| Phase | Scope | Effort | Delivers |
|---|---|---|---|
| **1** | dash-sim + dash-sim-client Apply-side FK validation (25 FKs) | ~2-3 days | Bottom-layer defense; direct sim access validated |
| **2a** | dashd Put-side FK validation (7 missing FKs) | ~2 days | Reject bad config at controller write time |
| **2b** | dashd Delete-side orphan protection | ~1 day | Prevent cascade disasters |
| **3** | `dashctl validate` + `dash-sim-client validate` + REST + SPA | ~1 day | Operational tooling for existing deployments |
| **Total** | | **~6-7 days** | |

---

## 9. Required creation order (operator-facing)

For a fully functional ENI, objects must be created in this order:

```
Step  Kind                 Why first
────  ────                 ─────────
 1    vnet                 ENI's network identity (Tier 0)
 2    qos                  Traffic shaping profile (Tier 0, optional)
 3    meter_policy         Metering policy (Tier 0, optional)
 4    acl_group × N        ACL rule containers (Tier 0)
 5    route_group          Route container (Tier 0)
 6    prefix_tag × N       IP prefix sets for ACL matching (Tier 0)
 7    tunnel × N           Encap endpoints (Tier 0)
 8    acl_rule × N         Rules inside acl_groups (Tier 1)
 9    route × N            Routes inside route_groups (Tier 1)
10    vnet_mapping × N     CA→PA mappings for the vnet (Tier 1)
11    ENI                  ← NOW create the ENI (refs vnet+qos+meter) (Tier 1)
12    eni_route            Binds ENI → route_group (Tier 2)
13    acl_in/out × stages  Binds ENI → acl_groups (Tier 2)
14    route_rule × N       Inbound routing rules for ENI (Tier 2)
15    meter × N            Per-ENI metering entries (Tier 2)
```

`dashctl apply -R -f manifest/` already handles this naturally
because manifests are numbered `00-vnets.yaml`, `01-enis.yaml`, etc.
With referential validation, a misordered single PUT gets a clear
400 error instead of a silent success-then-drop.

---

## 10. Future Scopes

### 10.1 Cascading delete

- **Trigger**: operator wants `DELETE vnet-prod --cascade` to delete
  all ENIs + mappings that reference it.
- **Proposal**: `?cascade=true` query param; the validator computes
  the transitive closure, deletes leaf-first, returns the full list.
- **Risk**: accidental `--cascade` is catastrophic. Require
  `--cascade --confirm` double-flag in dashctl.

### 10.2 Dry-run validation mode

- **Trigger**: operator wants to check if a PUT would succeed without
  actually writing.
- **Proposal**: `?dry_run=true` query param; runs all validators,
  returns 200 (would succeed) or 400 (would fail) without store write.
- **Reuses**: the existing `POST /v1/simulate` infrastructure.

### 10.3 Graph visualization in SPA

- **Trigger**: operator wants to see "what depends on this vnet?"
  visually.
- **Proposal**: SPA page under Diagnostics showing the dependency
  graph as a DAG with Mermaid or D3 force-directed layout. Clicking
  an object highlights its dependents + dependencies.

### 10.4 Cross-namespace references (future multi-tenant)

- **Trigger**: shared-services vnet referenced from multiple tenant
  namespaces.
- **Proposal**: `namespace/name` qualified FK syntax; validator
  checks existence in the target namespace with a cross-ns read
  permission gate.
- **Currently**: cross-namespace references are forbidden by
  `CheckSpecNamespace`.

### 10.5 Eventual consistency mode for large-scale bulk loads

- **Trigger**: loading 10,000 objects in a batch; strict validation
  on every PUT is too slow (N² store lookups).
- **Proposal**: `validation.referential_integrity: deferred` mode
  that accepts all writes and runs a background scan after the batch
  completes, flagging violations as warnings.

---

> **Change Log**
>
> | Date | Change |
> |---|---|
> | 2026-06-15 | Initial: gap analysis + design proposal |
