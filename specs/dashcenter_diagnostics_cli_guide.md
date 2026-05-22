# DashCenter Diagnostics CLI Guide

## Purpose

This guide expands the DashCenter diagnostics design into a detailed operator-facing CLI manual. It explains how `dashctl` should be used to inspect DASH objects, visualize dependency graphs, explain packet and flow behavior, reconcile intended versus realized state, collect evidence, and troubleshoot one device or many devices through a central DashCenter service.[cite:15][cite:50][cite:181][cite:127][cite:130]

The goal is to make the CLI understandable as a production operations surface, not just as a list of commands. Each command in this guide explains what it does, what data it reads, how it visualizes its findings, what an example output looks like, and how an operator should interpret that output in a real troubleshooting workflow.[cite:143][cite:149][cite:151]

## CLI design philosophy

DashCenter is positioned as a kubectl-style operations plane for DASH-compliant devices, so the CLI should behave like an operator language with consistent nouns, verbs, scoping, and output modes across single-device and fleet-wide tasks.[cite:136][cite:141][cite:142]

The CLI therefore follows five principles:

- object-first navigation, because DASH operations revolve around ENIs, VNETs, ACLs, routes, mappings, and services.[cite:15][cite:181]
- cross-layer reasoning, because debugging requires more than config inspection.[cite:181][cite:50]
- decision-first output, because operators need the verdict before the raw data.[cite:151]
- terminal-native visualization, because not every investigation happens in a GUI.[cite:149][cite:151]
- identical mental model for one device or many devices, because fleet-scale troubleshooting should feel like a natural extension of node-local debugging.[cite:141][cite:142]

## Command model

The CLI is organized into seven diagnostic intents.

| Intent | Command families | Purpose |
|---|---|---|
| Discover | `get`, `list`, `show` | Inspect objects and topology |
| Explain | `explain` | Tell why a packet, flow, or object behaved a certain way |
| Trace | `trace` | Walk the path of traffic through objects and stages |
| Validate | `reconcile`, `verify`, `diff` | Compare intended and realized state |
| Observe | `health`, `top`, `watch`, `events` | View live operational posture |
| Collect | `bundle`, `logs`, `export` | Gather evidence for RCA and support |
| Scope | `--device`, `--cluster`, `--fleet`, `--selector` | Control where the command runs |

## Scope model

Every command should be usable against a single device, a selected group of devices, or an entire fleet managed by DashCenter. This is essential if DashCenter is to act as a central service rather than a host-local utility.[cite:50][cite:141][cite:142]

Examples:

```bash
dashctl get eni eni-100 --device dpu-a

dashctl health --selector role=edge-dpu

dashctl top findings --fleet
```

The scope flags should affect both the query source and the output style. Single-device views may show deeper details by default, while fleet views should summarize first and allow drill-down.

## Output modes

The CLI should support several output modes so the same command works for humans, automation, and support workflows.

| Mode | Purpose |
|---|---|
| default | Human-readable summary with decision first |
| `--wide` | Expanded terminal table |
| `--json` | Full machine-readable output |
| `--yaml` | Reviewable structured output |
| `--tree` | Dependency or hierarchy rendering |
| `--graph` | Graph-style relationship view |
| `--watch` | Periodic refresh for live observation |
| `--brief` | One-line summary for scripts or dashboards |

## Core nouns

The diagnostics CLI should treat the following as first-class nouns because they represent the main troubleshooting surface of DASH environments.[cite:15][cite:181]

- `eni`
- `vnet`
- `acl`
- `route`
- `mapping`
- `service`
- `policy`
- `flow`
- `device`
- `finding`
- `bundle`

Each noun should support at least `get`, `list`, and `show graph` where it makes sense.

## Discover commands

### `dashctl get`

`get` returns the normalized current state of an object. It should combine control-plane intent, realized state, references, findings, and health into one operator-friendly view.[cite:15][cite:181]

Example:

```bash
dashctl get eni eni-100 --device dpu-a
```

Example output:

```text
ENI: eni-100
Device: dpu-a
Tenant: tenant-prod
VNET: blue-prod
Admin State: up
Program State: partial
Health: degraded

Bindings:
  ACL Ingress: acl-prod-in
  ACL Egress:  acl-prod-out
  Route Group: rg-west
  Mapping:     vnet-blue-to-west
  Service:     svc-firewall-west

Key findings:
  WARN route-group rg-west unresolved on target table bank 2
  WARN 6 active stateful flows pinned to service chain

Counters:
  packets: 18.2M
  bytes:   24.7GB
  drops:   41
```

How to read it:

- `Program State` shows whether the object is fully realized, not just configured.
- `Health` is an operator-oriented rollup derived from findings and runtime evidence.
- `Bindings` answer the question, “what else should be inspected next?”

### `dashctl get --wide`

```bash
dashctl get eni eni-100 --device dpu-a --wide
```

Example output:

```text
FIELD                         VALUE
id                            eni-100
device                        dpu-a
tenant                        tenant-prod
vnet                          blue-prod
vni                           5001
admin_state                   up
config_generation             1482
appdb_state                   programmed
statedb_state                 partial
vendor_realization            partial
new_flow_owner                dpu-a
active_flow_count             324
drop_stage_last_5m            route_lookup
last_change                   2026-05-22T14:21:09Z
```

This expanded form is for expert investigation and should expose low-level operational fields directly.

### `dashctl list`

`list` is for collections, not single objects.

```bash
dashctl list eni --device dpu-a
```

Example output:

```text
ENI ID    VNET       PROGRAM STATE  HEALTH    ACTIVE FLOWS  PRIMARY FINDING
eni-100   blue-prod  partial        degraded  324           route unresolved
eni-101   blue-prod  realized       healthy    14           -
eni-102   red-prod   realized       warn       88           drop spike
```

`list` should be sortable and filterable.

```bash
dashctl list eni --device dpu-a --sort active_flows --filter health!=healthy
```

### `dashctl show graph`

This command renders relationships between objects.

```bash
dashctl show graph eni eni-100 --tree
```

Example output:

```text
eni/eni-100
├─ vnet/blue-prod
├─ acl/acl-prod-in
│  ├─ rule/allow-https
│  └─ rule/deny-any
├─ acl/acl-prod-out
├─ route-group/rg-west
│  ├─ route/10.2.2.0/24
│  └─ route/10.2.3.0/24
├─ mapping/vnet-blue-to-west
└─ service/svc-firewall-west
```

This is one of the most important commands in the platform because dependency visibility is the backbone of DASH diagnostics.[cite:15][cite:181]

### `dashctl show lineage`

This command shows source-of-truth lineage across control-plane and runtime layers.

```bash
dashctl show lineage eni eni-100
```

Example output:

```text
Intent Layer      present  generation=1482
APP_DB            present  generation=1482
STATE_DB          partial  generation=1482
Vendor Runtime    partial  generation=1482
Flow Ownership    source-owned
Last Drift Seen   2026-05-22T14:18:10Z
```

This view is critical because many issues in programmable networking come from divergence between layers rather than total absence of config.[cite:181][cite:50]

## Explain commands

### `dashctl explain match`

This command explains how a hypothetical or observed packet would be processed. It is one of the most important troubleshooting tools because operators often ask, “why did this packet get allowed, redirected, encapsulated, or dropped?”[cite:15][cite:181]

```bash
dashctl explain match --src 10.1.1.10 --dst 10.2.2.20 --proto tcp --dport 443 --device dpu-a
```

Example output:

```text
Packet explanation

Stage 1  ENI classification   PASS  eni-100
Stage 2  ACL ingress          PASS  acl-prod-in / allow-https
Stage 3  Route lookup         PASS  rg-west / 10.2.2.0/24
Stage 4  Mapping              PASS  vnet-blue-to-west
Stage 5  Service insertion    PASS  svc-firewall-west
Stage 6  Encap decision       PASS  vxlan vni=5001
Stage 7  Egress realization   PASS  programmed on dpu-a

Final action: FORWARD
Confidence: high
```

Why this matters:

- it translates control-plane objects into packet outcome;
- it identifies the winning rule or stage;
- it gives the operator a stage-by-stage proof path.[cite:15][cite:181]

### `dashctl explain drop`

```bash
dashctl explain drop --src 10.1.1.10 --dst 10.9.9.9 --proto tcp --dport 443 --device dpu-a
```

Example output:

```text
Drop explanation

Stage 1  ENI classification   PASS  eni-100
Stage 2  ACL ingress          PASS  acl-prod-in / allow-https
Stage 3  Route lookup         FAIL  no route in rg-west for 10.9.9.9/32

Final action: DROP
Primary cause: unresolved route for destination prefix
Suggested next commands:
  dashctl get route-group rg-west --device dpu-a
  dashctl reconcile eni eni-100 --device dpu-a
```

This command should always recommend the next useful commands.

### `dashctl explain object`

This command tells the operator why an object is unhealthy, degraded, blocked, or partial.

```bash
dashctl explain object eni eni-100 --device dpu-a
```

Example output:

```text
Object explanation: eni/eni-100

Current verdict: DEGRADED

Reason chain:
- ENI references route-group rg-west.
- Route-group rg-west contains prefix 10.2.2.0/24 expected by active flows.
- Target realization of rg-west is partial due to missing nexthop mapping.
- 41 packets dropped in the last 5 minutes at route lookup stage.

Suggested next commands:
  dashctl get route-group rg-west --wide
  dashctl diff eni eni-100 --device dpu-a --target-device dpu-b
```

## Trace commands

### `dashctl trace flow`

`trace flow` follows a real or hypothetical flow through the DASH pipeline.

```bash
dashctl trace flow --src 10.1.1.10 --dst 10.2.2.20 --proto tcp --dport 443 --device dpu-a
```

Example output:

```text
Flow trace

Input packet:
  10.1.1.10:45221 -> 10.2.2.20:443 tcp

Resolved path:
  eni/eni-100
  acl/acl-prod-in:allow-https
  route-group/rg-west:10.2.2.0/24
  mapping/vnet-blue-to-west
  service/svc-firewall-west
  encap/vxlan:vni-5001
  egress-port/overlay0

Observed evidence:
  source packets: yes
  target packets: no
  last-seen: 2026-05-22T14:28:19Z

Verdict: path resolved on source only
```

This command is especially useful when configuration looks correct but traffic still behaves incorrectly.

### `dashctl trace object`

This traces the downstream implications of an object.

```bash
dashctl trace object route-group rg-west --device dpu-a
```

Example output:

```text
Object trace: route-group/rg-west

Affects:
  ENIs:        12
  active flows: 844
  service paths: 3
  target issues: 1

Most impacted ENIs:
  eni-100  active_flows=324  drops=41
  eni-102  active_flows=188  drops=17
```

This is effectively a blast-radius tool for production incidents.

## Reconcile and validate commands

### `dashctl reconcile`

`reconcile` compares intended state with what is actually realized in the appliance or DPU runtime.

```bash
dashctl reconcile eni eni-100 --device dpu-a
```

Example output:

```text
Reconciliation result: eni/eni-100

Intent                PRESENT
APP_DB                PRESENT
STATE_DB              PARTIAL
Vendor Runtime        PARTIAL
Observed Traffic      ACTIVE

Drift summary:
  missing route-group member on target runtime
  service-chain object present but standby-only
  counters indicate active drops despite config parity

Verdict: DRIFT DETECTED
```

This is a core enterprise diagnostic because large outages often involve partial realization rather than total config absence.[cite:181][cite:50]

### `dashctl verify`

`verify` is a stricter policy gate. It answers whether the object or environment meets a declared policy or SLO.

```bash
dashctl verify eni eni-100 --policy migration-ready
```

Example output:

```text
Verification policy: migration-ready

Checks:
  dependency closure      PASS
  target parity           PASS
  active blocked flows    FAIL
  unknown flow count      PASS
  telemetry freshness     PASS

Decision: NOT READY
```

### `dashctl diff`

`diff` compares two scopes, devices, or timepoints.

```bash
dashctl diff eni eni-100 --device dpu-a --target-device dpu-b
```

Example output:

```text
Diff: eni/eni-100

Field                    source(dpu-a)     target(dpu-b)
program_state            realized          partial
route-group              rg-west           rg-west
route-realization        full              partial
service-chain            active            standby
active_flows             324               0
new_flow_owner           dpu-a             none
```

This is one of the fastest ways to find migration blockers or parity gaps.

## Observe commands

### `dashctl health`

This provides a fleet or device health summary.

```bash
dashctl health --fleet
```

Example output:

```text
Fleet Health Summary

Devices healthy:   18
Devices warning:    3
Devices degraded:   1
Critical findings:  2

Top issues:
  dpu-b route realization failures affecting 12 ENIs
  dpu-c telemetry lag exceeding threshold
```

### `dashctl health eni`

```bash
dashctl health eni eni-100 --device dpu-a
```

Example output:

```text
ENI Health: eni-100

Status: degraded
Score: 71/100

Breakdown:
  config parity        100
  runtime realization   60
  traffic health        72
  dependency health     75
  migration readiness   40
```

This score breakdown should be composable and explainable, not a black-box number.

### `dashctl top`

`top` ranks the most important problems or hottest objects.

```bash
dashctl top findings --fleet
```

Example output:

```text
RANK  FINDING TYPE              COUNT  TOP AFFECTED OBJECT
1     route_unresolved            12   route-group/rg-west
2     target_realization_partial   8   eni/eni-100
3     telemetry_lag                4   device/dpu-c
```

### `dashctl watch`

`watch` refreshes a command over time.

```bash
dashctl watch "dashctl health eni eni-100 --device dpu-a"
```

Example output:

```text
[14:31:01] status=degraded  score=71 drops=41 active_flows=324
[14:31:06] status=degraded  score=71 drops=43 active_flows=327
[14:31:11] status=warning   score=78 drops=43 active_flows=320
```

This is useful during active incident response or migration cutover windows.

### `dashctl events`

This shows time-ordered operational events.

```bash
dashctl events eni eni-100 --device dpu-a --since 30m
```

Example output:

```text
14:02:11  route-group rg-west updated generation=1482
14:02:13  target realization became partial
14:03:02  drop spike detected stage=route_lookup
14:08:27  migration validation requested
14:09:04  service-chain standby sync completed
```

Events are often the fastest way to build causal narrative during RCA.

## Flow commands

### `dashctl get flow`

This returns a detailed view of a single flow.

```bash
dashctl get flow fl-7812
```

Example output:

```text
Flow: fl-7812
Tuple: 10.1.1.10:45220 -> 10.2.2.20:443 tcp
ENI: eni-100
Owner: dpu-a
Owner State: source-owned
Association Confidence: direct
Statefulness: state-transfer-required
Service Chain: svc-firewall-west
Last Seen: 2026-05-22T14:32:41Z
Migration Recommendation: block
```

### `dashctl list flow`

```bash
dashctl list flow --eni eni-100 --group-by owner_state
```

Example output:

```text
source-owned   317
blocked          6
unknown          1
```

These commands connect diagnostics and migration by making flow state a readable part of everyday operations.[cite:127][cite:130]

## Logs and evidence commands

### `dashctl logs`

This command pulls normalized diagnostics logs relevant to an object or device.

```bash
dashctl logs eni eni-100 --device dpu-a --since 15m
```

Example output:

```text
14:24:10  INFO  eni programmed generation=1482
14:24:10  WARN  route-group rg-west partial realization
14:24:13  WARN  flow fl-7812 pinned to stateful service chain
14:25:11  INFO  validation policy migration-ready requested
```

### `dashctl bundle`

This creates a support bundle for an object, device, or fleet scope.

```bash
dashctl bundle eni eni-100 --device dpu-a --output eni-100-support.tar.gz
```

Example output:

```text
Bundle contents:
  object.json
  lineage.json
  graph.json
  findings.json
  flows.json
  logs.txt
  counters.json
  events.json

Result: eni-100-support.tar.gz written
```

This is essential for support, postmortems, and collaboration across teams.[cite:151][cite:181]

### `dashctl export`

`export` serializes normalized state for review or replay.

```bash
dashctl export eni eni-100 --format yaml
```

Example output:

```yaml
kind: ENIExport
metadata:
  id: eni-100
spec:
  device: dpu-a
  vnet: blue-prod
  bindings:
    aclIngress: acl-prod-in
    aclEgress: acl-prod-out
    routeGroup: rg-west
    mapping: vnet-blue-to-west
    service: svc-firewall-west
status:
  programState: partial
  health: degraded
  activeFlows: 324
```

Export is useful for review, diffing, and future migration workflows.

## Fleet commands

### `dashctl get device`

```bash
dashctl get device dpu-a
```

Example output:

```text
Device: dpu-a
Role: edge-dpu
Health: warning
Managed ENIs: 212
Realization Errors: 3
Telemetry Freshness: 102ms
Top Finding: partial route realization on rg-west
```

### `dashctl list device`

```bash
dashctl list device --selector role=edge-dpu
```

Example output:

```text
DEVICE  HEALTH    ENIS  ACTIVE FLOWS  PRIMARY FINDING
dpu-a   warning   212   8.2K          partial route realization
dpu-b   degraded  201   7.8K          target realization failures
dpu-c   healthy   198   6.9K          -
```

### `dashctl top eni`

```bash
dashctl top eni --fleet --sort drops
```

Example output:

```text
RANK  ENI      DEVICE  DROPS  ACTIVE FLOWS  HEALTH
1     eni-100  dpu-a     41     324         degraded
2     eni-102  dpu-a     17     188         warning
3     eni-201  dpu-b      9      71         warning
```

Fleet commands are what make DashCenter different from host-local debug tools.[cite:50][cite:141][cite:142]

## Suggested troubleshooting workflows

### Workflow 1: Why is traffic dropping?

1. `dashctl explain drop ...`
2. `dashctl get eni ...`
3. `dashctl get route-group ...`
4. `dashctl reconcile eni ...`
5. `dashctl bundle eni ...`

### Workflow 2: Why is an ENI degraded?

1. `dashctl explain object eni ...`
2. `dashctl show lineage eni ...`
3. `dashctl list flow --eni ... --group-by owner_state`
4. `dashctl events eni ... --since 1h`

### Workflow 3: Is this ENI migration-ready?

1. `dashctl get eni ...`
2. `dashctl list flow --eni ...`
3. `dashctl verify eni ... --policy migration-ready`
4. `dashctl diff eni ... --target-device ...`

These workflows should be documented directly in the product because operators think in questions, not in isolated commands.

## Command reference summary

| Command | Main question answered | Primary visualization |
|---|---|---|
| `get` | What is this object right now? | Summary panel |
| `list` | Which objects should I care about? | Ranked table |
| `show graph` | What depends on what? | Tree/graph |
| `show lineage` | Which layers disagree? | Layer stack |
| `explain match` | Why did this packet forward? | Stage pipeline |
| `explain drop` | Why did this packet drop? | Failure pipeline |
| `explain object` | Why is this object unhealthy? | Reason chain |
| `trace flow` | What path did traffic take? | Ordered path trace |
| `trace object` | What does this object affect? | Blast-radius summary |
| `reconcile` | Is runtime aligned with intent? | Drift report |
| `verify` | Does this meet a policy gate? | Scorecard |
| `diff` | What differs between scopes? | Side-by-side diff |
| `health` | What is the current health posture? | Health summary |
| `top` | What are the most severe issues? | Ranked list |
| `watch` | How is this changing live? | Refreshing one-line view |
| `events` | What happened over time? | Timeline |
| `logs` | What detailed evidence exists? | Time-ordered logs |
| `bundle` | How do I capture all evidence? | Bundle manifest |
| `export` | How do I serialize normalized state? | Structured file |

## Output quality standards

A production-grade diagnostics CLI should follow strict output rules.

- The first line should carry the decision or current verdict.
- Object identifiers and scope should be visible immediately.
- Structured evidence should come before free-form commentary.
- Confidence should be surfaced when evidence is inferred rather than direct.
- Suggested next commands should appear when a command identifies a likely next diagnostic step.
- Every view should be understandable in less than thirty seconds by an operator under incident pressure.[cite:151][cite:149]

## Final CLI position

The DashCenter diagnostics CLI should behave like a readable operational book for DASH systems: every command should expose the object model, the runtime truth, the traffic consequences, and the next likely question. That is how `dashctl` becomes an industry-grade control and visibility surface instead of a thin wrapper around raw APIs.[cite:15][cite:50][cite:181][cite:142]
