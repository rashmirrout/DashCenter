# dashd — controller daemon (PLACEHOLDER)

| Field | Value |
|---|---|
| Source | [`src/impl-go/dashd/`](../../../src/impl-go/dashd/) |
| Binary | `dashd` |
| Status | **placeholder** (Phase 4) |

---

## What's here today

Only a scaffold:

```
dashd/
├── go.mod
├── cmd/dashd/main.go        -- prints "dashd 0.0.0-dev (scaffold)" and exits
├── internal/config/doc.go   -- empty package doc
├── configs/                 -- example YAML files (shape only)
└── Dockerfile               -- builds the scaffold binary
```

Running it today:
```bash
go run ./dashd/cmd/dashd
# dashd 0.0.0-dev (scaffold)
```

---

## What it will become (Phase 4)

A **fleet controller daemon** that sits one level above individual DPU
agents. Per the design sketches in [`specs/`](../../../specs/):

| Concern | Plan |
|---|---|
| Inputs | High-level **desired state** YAML (vnets, enis, mappings, ACL policies) + appliance inventory. |
| Outputs | Streams of `dashapi.Apply` / `Delete` calls fanned out to every DPU's `DashApi` server (sim or real). |
| State | Stores observed state in Redis (or pluggable backend). Reconciles desired vs observed continuously. |
| API | REST (control plane) + gRPC (machine-to-machine). |
| HA | Leader election + watched failover. |

The controller becomes the **only thing that talks to many DPUs at once**.
`dash-sim-client` remains the single-DPU operator CLI; `dashctl` will be
the controller-facing operator CLI.

---

## Why a placeholder right now?

The current scope (Phases 1–3) deliberately stops at the **single-DPU**
DashApi surface so that:
- Schema parity with upstream `sonic-dash-api` could be validated
- The behavioural pipeline could be exercised
- A SONiC-compatible adapter could be shipped

Standing up a fleet controller requires those three to be solid first.

---

## Roadmap

- [ ] Define `dashcenter.v1` proto (controller service definition).
- [ ] Move REST handler design out of [`specs/`](../../../specs/) into code.
- [ ] Implement reconciler loop (desired → observed; emit per-DPU Apply/Delete).
- [ ] HA + leader election (etcd or built-in Raft).
- [ ] CLI front (`dashctl`).
