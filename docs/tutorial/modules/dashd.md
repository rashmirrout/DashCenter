# dashd — the fleet controller daemon

| Field | Value |
|---|---|
| Source | [`src/impl-go/dashd/`](../../../src/impl-go/dashd/) |
| Binary | `dashd` |
| Default ports | `:8443` (REST), `:7443` (admin), `:9443` (gRPC) |
| Status | stable |

---

## 1. Role

The **fleet controller daemon** for DashCenter. It manages desired state
for VNets, ENIs, VnetMappings, AclPolicies, RoutePolicies, HaSets, and
ServiceTunnels across a fleet of DPUs, reconciling desired vs observed
state continuously.

---

## 2. Internal layout

```
dashd/
├── cmd/dashd/main.go           -- entry point
├── Dockerfile                  -- Alpine runtime with dashctl bundled
└── internal/
    ├── audit/                  -- audit logging
    ├── capacity/               -- per-DPU admission control
    ├── cluster/                -- etcd cluster coordination
    ├── config/                 -- YAML configuration
    ├── counters/               -- counter tracking + SSE streaming
    ├── dispatch/               -- fan-out Apply/Delete to DPUs
    ├── dpuclient/              -- gRPC client to DPUs
    ├── flow/                   -- flow tracking
    ├── ha/                     -- HA leader election + orchestrator
    ├── inventory/              -- DPU registry
    ├── migration/              -- state migrations
    ├── model/                  -- observability cache
    ├── namespace/              -- namespace isolation + FK validation ⭐
    ├── observability/          -- metrics + broadcaster
    ├── operations/             -- cordon/drain
    ├── placement/              -- ENI placement logic
    ├── reconciler/             -- desired-vs-observed reconciliation
    ├── saga/                   -- atomic batch operations
    ├── schema/                 -- capability/schema gating
    ├── server/                 -- HTTP REST + gRPC + admin servers
    ├── service/                -- service layer (Put/Delete/Get handlers) ⭐
    ├── store/                  -- desired-state persistence (etcd/file)
    └── subscribe/              -- event subscription hub
```

---

## 3. Referential integrity validation

The `namespace.Validator` (in `internal/namespace/`) enforces FK
references on every spec write and delete:

**Put-side checks** (`Check*` methods):

| Check | FK field | Referenced kind |
|---|---|---|
| `CheckEni` | `vnet_name` | vnet (same namespace) |
| `CheckVnetMapping` | `vnet_name` | vnet (same namespace) |
| `CheckAclPolicy` | `eni_names[]` | eni (same namespace) |
| `CheckRoutePolicy` | `eni_names[]` | eni (same namespace) |
| `CheckRoutePolicy` | `routes[i].next_hop_target` | vnet or service_tunnel |
| `CheckHaSet` | `member_dpu_ids[]` | inventory DPU |

**Delete-side checks** (`CheckDelete` method):

| Deleting | Blocked by |
|---|---|
| vnet | ENIs, VnetMappings |
| eni | AclPolicies, RoutePolicies |
| service_tunnel | RoutePolicies |

See [referential-integrity-validation.md](../../dashd-features/referential-integrity-validation.md)
for the full FK map and design rationale.

---

## 4. Tests

```bash
go test ./dashd/... -count=1 -timeout 120s
# 28 packages green
```

The namespace validator tests (in `internal/namespace/namespace_test.go`)
cover every FK family: cross-namespace rejection, missing-ref rejection,
delete-with-dependents rejection, and force-delete bypass.
