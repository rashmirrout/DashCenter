# dashctl — controller CLI (PLACEHOLDER)

| Field | Value |
|---|---|
| Source | [`src/impl-go/dashctl/`](../../../src/impl-go/dashctl/) |
| Binary | `dashctl` |
| Status | **placeholder** (Phase 4) |

---

## What's here today

```
dashctl/
├── go.mod
├── cmd/dashctl/main.go      -- prints "dashctl 0.0.0-dev (scaffold)"
└── pkg/client/doc.go        -- empty package doc
```

Running it today:
```bash
go run ./dashctl/cmd/dashctl
# dashctl 0.0.0-dev (scaffold)
```

---

## What it will become

The **controller-facing operator CLI**. Where `dash-sim-client` talks to a
**single DPU's** `DashApi`, `dashctl` will talk to the **central
controller** (`dashd`) to:
- declare desired state across many DPUs
- inspect the controller's view of the fleet
- trigger reconciliation, drain, HA failover, scenario rollout

Layer separation:

```
        ┌──────────────────────┐
        │ dashctl  (operator)  │   ← Phase 4, here
        └──────────┬───────────┘
                   │  (dashcenter.v1 REST + gRPC)
        ┌──────────▼───────────┐
        │ dashd     (controller)│  ← Phase 4
        └──────────┬───────────┘
                   │  fans out  dashapi.v1.DashApi  per DPU
       ┌───────────┼──────────────┐
       ▼           ▼              ▼
   dash-sim   dash-redis-adapter  real DPU agent
       ▲           ▲
       │           │
       └─────  dash-sim-client  ───   (today, for single-DPU work)
```

---

## Until Phase 4 lands

Use [`dash-sim-client`](dash-sim-client.md) to talk to DPUs directly.
There's no controller yet, so there's nothing for `dashctl` to do.
