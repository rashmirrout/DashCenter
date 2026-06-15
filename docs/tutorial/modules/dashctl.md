# dashctl — controller operator CLI

| Field | Value |
|---|---|
| Source | [`src/impl-go/dashctl/`](../../../src/impl-go/dashctl/) |
| Binary | `dashctl` (or `dashctl.exe`) |
| Default endpoint | `http://localhost:8443` (dashd REST) |
| Status | stable |

---

## 1. Role

The **controller-facing operator CLI** for DashCenter. Where
`dash-sim-client` talks to a single DPU's `DashApi`, `dashctl` talks
to the central controller (`dashd`) to declare desired state across
many DPUs, inspect the fleet, trigger reconciliation, and validate
configurations.

```
        ┌──────────────────────┐
        │ dashctl  (operator)  │
        └──────────┬───────────┘
                   │  (dashcenter.v1 REST)
        ┌──────────▼───────────┐
        │ dashd    (controller)│
        └──────────┬───────────┘
                   │  fans out dashapi.v1.DashApi per DPU
       ┌───────────┼──────────────┐
       ▼           ▼              ▼
   dash-sim   dash-redis-adapter  real DPU agent
```

---

## 2. Command tree

```
dashctl
├── apply -f <manifest>         create or replace specs (--force to overwrite)
├── get <kind> [name]           read one or all specs
├── describe <kind> <name>      human-readable detail view
├── delete <kind> <name>        remove (with orphan protection)
├── replace -f <manifest>       CAS update with expected_generation
├── edit <kind> <name>          open in $EDITOR, PUT on save
├── diff -f <manifest>          preview what apply would change
├── validate -f <manifest>      pre-flight FK validation
├── reconcile                   fan-out desired state to all DPUs
├── simulate -f <ops.json>      dry-run validation (Phase 2)
├── dpu list|describe|drift     DPU inventory management
├── inventory get|put           fleet inventory operations
├── counters [--follow]         counter snapshot / SSE stream
├── events --watch              event stream (Phase 2 stub)
├── explain <kind>              offline schema documentation
├── config set-context|view     multi-cluster configuration
├── version                     client + server version
├── completion                  shell completions
├── topology                    cluster topology view
└── <kind> put|list|delete      typed-kind subgroups (vnet put, eni list, ...)
```

### EniBundle — full ENI config in one file

The `EniBundle` kind defines an ENI and its complete dependency chain
in a single YAML document. It auto-expands into individual specs in
the correct tier order (vnet → eni → mappings → policies) and
auto-wires FK references (`eni.vnet_name`, `route_policy.eni_names`).

### Other bundle kinds

| Bundle | Expands to | Use case |
|---|---|---|
| `EniBundle` | Vnet → ENI → VnetMappings → RoutePolicy → AclPolicies | Full ENI pipeline |
| `AclBundle` | Vnet → ENI → AclPolicy (with inline rules) | ACL policy + deps |
| `RouteBundle` | Vnet → ServiceTunnel → ENI → RoutePolicy (with inline routes) | Route policy + deps |
| `HaBundle` | HaSet | HA set config |

All bundles auto-wire FK references and expand in correct tier order.
Optional sections (vnet, eni, service_tunnel) can be omitted if the
dependency already exists.

See [CLI_GUIDE.md §6](../../CLI_GUIDE.md) for full bundle spec formats
and examples.

### Create vs. modify detection

`dashctl apply` checks whether objects already exist:
- **New** → applied normally (`CREATE`)
- **Existing** without `--force` → `BLOCKED` with warning
- **Existing** with `--force` → overwritten (`MODIFY`)

---

## 3. Referential integrity validation

### The problem: silent misconfiguration

Without FK validation, an operator can create an ENI that says
`vnet_name: "vnet-bllue"` (a typo for `"vnet-blue"`). dashd accepts
it — the YAML is valid, the kind is correct, the fields match the
proto. But on the DPU, the ENI's pipeline tries to look up
`vnet-bllue` for packet encapsulation and finds nothing. Traffic
drops. The operator discovers this 20 minutes later via counter
spikes, not at the PUT that caused it.

### The solution: validate on Put, protect on Delete

dashd enforces FK references at two points:

**Put-side** — when you create or update a spec, dashd checks that
every referenced object exists in the same namespace:

| Spec kind | FK field | Must reference | Example error |
|---|---|---|---|
| ENI | `vnet_name` | an existing vnet | `eni.vnet_name="vnet-bllue": not found in this namespace` |
| VnetMapping | `vnet_name` | an existing vnet | same pattern |
| AclPolicy | `eni_names[i]` | existing ENIs | `acl_policy.eni_names[0]="eni-x": not found` |
| RoutePolicy | `eni_names[i]` | existing ENIs | same pattern |
| RoutePolicy | `routes[i].next_hop_target` | vnet or service_tunnel | `routes[0].next_hop_target="tun-x" (type=service_tunnel): not found` |
| HaSet | `member_dpu_ids[i]` | inventory DPU | `ha_set.member_dpu_ids[0]="dpu-x" not found in inventory` |

**Delete-side** — when you delete an object, dashd checks that no
other object still references it:

| Deleting | Blocked when referenced by | Example error |
|---|---|---|
| vnet | any ENI or VnetMapping | `cannot delete vnet "vnet-prod" — eni "eni-001" still references it` |
| eni | any AclPolicy or RoutePolicy | `cannot delete eni "eni-001" — acl_policy "acl-web" still references it` |
| service_tunnel | any RoutePolicy | `cannot delete service_tunnel "tun-1" — route_policy "rp-prod" still references it` |

### Experiment: wrong config → error → understand → right config

**Experiment A — create an ENI with a missing vnet (FAIL):**

```bash
dashctl apply -f - <<'EOF'
apiVersion: dashcenter.v1
kind: Eni
metadata: { name: eni-orphan }
spec: { vnet_name: vnet-nonexistent, mac_address: "00:00:00:00:00:01" }
EOF
```

**Error:**

```
Error: invalid argument: eni.vnet_name="vnet-nonexistent":
  namespace: cross-namespace reference rejected
  (referenced default/vnet/vnet-nonexistent not found in this namespace)
```

**Why it failed**: dashd ran `CheckEni()` which calls
`refExists(ctx, "default", "vnet", "vnet-nonexistent")`. The store
returned `ErrNotFound`. No vnet by that name exists in the `default`
namespace. The ENI was not stored.

**Fix — create the vnet first:**

```bash
dashctl apply -f - <<'EOF'
apiVersion: dashcenter.v1
kind: Vnet
metadata: { name: vnet-nonexistent }
spec: { vni: 1001 }
---
apiVersion: dashcenter.v1
kind: Eni
metadata: { name: eni-orphan }
spec: { vnet_name: vnet-nonexistent, mac_address: "00:00:00:00:00:01" }
EOF
# → both accepted ✅ (vnet first, then ENI)
```

**Experiment B — delete a vnet that ENIs still reference (FAIL):**

```bash
dashctl delete vnet vnet-nonexistent
```

**Error:**

```
Error: failed precondition: referential integrity: object has dependents:
  cannot delete vnet "vnet-nonexistent" — eni "eni-orphan" still references it
```

**Why it failed**: dashd ran `CheckDelete()` which scanned all ENIs in
the namespace. It found `eni-orphan` with `vnet_name: "vnet-nonexistent"`.
Deleting the vnet would orphan the ENI, so the delete is blocked.

**Fix — delete top-down (dependents first):**

```bash
dashctl delete eni eni-orphan        # remove the dependent first
dashctl delete vnet vnet-nonexistent # now the vnet has no dependents
# → both succeed ✅
```

### `dashctl validate -f` — pre-flight validation

For manifest directories with many objects, use `validate` to check
them all and get a summary report:

```bash
dashctl validate -f manifest/ --endpoint http://localhost:8443 --insecure
# STATUS  KIND             NAMESPACE     NAME         ERROR
# ------  ---------------  ------------  ----------   -----
# ✅ OK   vnet             default       vnet-prod
# ✅ OK   eni              default       eni-001
# ❌ FAIL route_policy     default       rp-bad       ... cross-namespace reference ...
#
# Total: 3  Accepted: 2  Rejected: 1
```

---

## 4. Adding a new subcommand

1. Create `internal/cmd/<name>.go` with `func (a *Application) new<Name>Cmd() *cobra.Command`.
2. Register in `root.go` inside `NewRoot()`.
3. If the command needs a REST call, add a method to `pkg/client.Client` interface + implement in `pkg/client/rest/rest.go`.
4. Rebuild and test:
   ```bash
   go build ./...
   dashctl <name> --help
   ```

---

## 5. See also

- [CLI_GUIDE.md](../../CLI_GUIDE.md) — every command with examples + output
- [MANUAL-HANDSON.md](../../MANUAL-HANDSON.md) — 36-step operator walkthrough
- [explore-with-docker](../../explore-with-docker/manual-handson.md) — standalone tutorial
- [referential-integrity-validation.md](../../dashd-features/referential-integrity-validation.md) — FK design doc
