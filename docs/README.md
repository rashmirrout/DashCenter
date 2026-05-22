# DashCenter

> Centralized visibility, troubleshooting, and fleet operations for DASH-compliant devices.

DashCenter is an operations platform for environments that run one or many DASH-capable DPUs or appliances. It uses the same DASH object model and API vocabulary as the programming plane, then adds fleet-wide visibility, object inspection, packet/flow reasoning, state reconciliation, ENI mobility diagnostics, health analysis, and evidence collection in one place.[cite:36][cite:43][cite:45][cite:50]

## Why DashCenter

DASH already defines a structured model spanning northbound APIs, DASH APP_DB, orchestration, SAI realization, and STATE_DB programmed status, which makes it possible to build a true cross-layer diagnostics and operations plane instead of relying on disconnected logs, counters, and vendor-specific debug tools.[cite:36][cite:45]

DashCenter is built for production operators who need to answer questions like:

- What objects exist for this ENI, VNET, tenant, or workload?[cite:45]
- Why did this packet match, redirect, or drop?[cite:36]
- What path did this flow take through the DPU?[cite:36]
- Is the configured state fully realized on the device?[cite:45]
- Is an ENI ready to migrate to another DPU, and what flows are still active?[cite:127][cite:130]
- Which DASH devices in the fleet are degraded right now?[cite:50]

## What it provides

### Central service

DashCenter is designed as a central service that manages one or many DASH-compliant devices from a single control point. Operators connect to DashCenter instead of logging into each device independently, which makes it suitable for both single-node operations and fleet-scale environments.[cite:50][cite:141]

### `dashctl` CLI

The companion CLI, `dashctl`, provides a kubectl-style operator experience for querying and debugging objects, flows, and fleet state from a terminal. Commands are object-aware, scriptable, and work consistently across one device or many devices.[cite:136][cite:149]

### Cross-layer diagnostics

DashCenter correlates DASH northbound objects, APP_DB, STATE_DB, SAI or vendor realization, packet/flow evidence, and platform health into one normalized view. This enables explain-match, trace-flow, reconcile-state, blast-radius analysis, and ENI mobility diagnostics using the same object model as the production programming plane.[cite:36][cite:43][cite:45][cite:50]

## Core capabilities

| Capability | What it does |
|---|---|
| Get object | Inspect ENIs, VNETs, ACLs, routes, mappings, services, and runtime state in one normalized view.[cite:45] |
| Explain match | Show why a packet or flow matched a given DASH policy/path, including winning rule and failure point.[cite:36] |
| Trace flow | Show the expected and observed path of traffic through the DPU and related DASH objects.[cite:36] |
| Reconcile state | Compare intended/configured state against APP_DB, STATE_DB, and runtime realization.[cite:45] |
| ENI mobility | Build ENI migration bundles, show flow ownership, check readiness, and monitor drain state.[cite:127][cite:130] |
| Fleet visibility | View health, findings, and degradation across many DASH-compliant devices from one place.[cite:50][cite:141] |
| Evidence collection | Generate support bundles with snapshots, findings, logs, and flow/path context for RCA.[cite:50][cite:151] |

## Architecture at a glance

DashCenter is built around a central analysis service with pluggable collectors and adapters. The service reads DASH-facing APIs and runtime data sources, normalizes them into a canonical object graph, then serves diagnostics workflows to CLI, API, and UI consumers.[cite:36][cite:43][cite:45][cite:50]

```text
+------------------------------+
|          dashctl CLI         |
+--------------+---------------+
               |
               v
+------------------------------+
|         DashCenter API       |
|   gRPC / REST / streaming    |
+--------------+---------------+
               |
               v
+------------------------------+
|  Correlation + Analysis Core |
|  - object graph              |
|  - explain match             |
|  - trace flow                |
|  - reconcile state           |
|  - ENI mobility checks       |
+--------------+---------------+
               |
      +--------+---------+-------------------+
      |                  |                   |
      v                  v                   v
+-----------+     +-------------+     +-------------+
| DASH API  |     | APP/STATEDB |     | Vendor / HW |
+-----------+     +-------------+     +-------------+
```

## CLI examples

### Inspect an object

```bash
dashctl get eni eni-100
```

Returns the ENI, its VNET, attached ACLs, route dependencies, meter/service bindings, realization status, and current health findings.[cite:45]

### Explain a packet match

```bash
dashctl explain match --src 10.1.1.10 --dst 10.2.2.20 --proto tcp --dport 443 --device dpu-a
```

Shows inferred ENI, matched ACL stage/rule, selected route or mapping, meter decision, and final forwarding or drop action.[cite:36]

### Trace a flow

```bash
dashctl trace flow --src 10.1.1.10 --dst 10.2.2.20 --proto tcp --dport 443
```

Shows the packet journey across classification, ENI selection, policy, routing, mapping, service path, and egress realization.[cite:36]

### Reconcile runtime state

```bash
dashctl reconcile eni eni-100
```

Compares intended/configured state to APP_DB, STATE_DB, and runtime realization and highlights drift, missing dependencies, or partial programming.[cite:45]

### Check ENI migration readiness

```bash
dashctl eni readiness eni-100 --target-dpu dpu-b
```

Shows whether the target DPU is ready to host the ENI, what rules and dependencies must move, and whether active flows or ownership state make cutover risky.[cite:127][cite:130]

## Command groups

| Command group | Purpose |
|---|---|
| `get`, `list`, `show` | Object visibility and topology inspection |
| `explain`, `trace` | Packet and flow reasoning |
| `reconcile`, `verify`, `diff` | State validation and drift detection |
| `eni bundle`, `eni flows`, `eni readiness`, `eni drain` | ENI mobility and HA workflows |
| `health`, `top`, `watch` | Fleet visibility and live operations |
| `bundle`, `logs`, `events`, `export` | Supportability and evidence collection |

## Who it is for

DashCenter is aimed at platform teams, network engineers, SREs, NOC operators, and support teams operating DASH-compliant DPUs in production. It is especially useful when multiple devices, peers, or sites must be managed consistently through one operations layer.[cite:50][cite:141]

## Project goals

- Make DASH objects operationally visible across layers.[cite:36][cite:45]
- Provide a kubectl-like workflow for single-device and fleet-scale troubleshooting.[cite:136][cite:141]
- Improve incident response, ENI mobility safety, and change confidence.[cite:127][cite:130]
- Reduce dependence on one-off vendor-specific debug procedures by giving a stable DASH-native operations model.[cite:50][cite:45]

## Status

DashCenter is currently in design and architecture phase. The initial target is a gRPC-first service with a `dashctl` CLI, followed by fleet APIs, streaming health, historical snapshots, and a web console.[cite:43][cite:45]

## Repository structure

```text
/docs           Specifications, architecture, design docs
/proto          gRPC and protobuf APIs
/cmd/dashctl    CLI entrypoint
/pkg            Shared libraries and core types
/internal       Service implementation
/deploy         Deployment manifests and packaging
/web            Future console assets
```

## Roadmap

### Phase 1

- Canonical object model
- `dashctl get`, `list`, `show graph`
- `dashctl explain match`
- `dashctl trace flow`
- `dashctl reconcile`

### Phase 2

- ENI bundle, flows, readiness
- Fleet health and findings
- Support bundle generation
- Historical snapshots and diffing

### Phase 3

- HA parity and drain monitoring
- Streaming watch APIs
- Web console
- Policy simulation and advanced analytics

## Open source direction

DashCenter is intended to be an open source operations platform for DASH ecosystems, with a clean core, pluggable adapters, and a stable CLI/API model. The long-term goal is a shared visibility and diagnostics layer that can work across multiple DASH-compliant vendors and deployments while still allowing vendor-specific enrichments where available.[cite:50][cite:43]

## Contributing

The project should accept contributions in the following areas:

- object schemas and APIs
- CLI ergonomics
- DASH data-source adapters
- ENI mobility workflows
- health analysis and evidence collection
- documentation and examples

## License

Apache-2.0 is a strong fit for infrastructure tooling and vendor-neutral ecosystem projects.

