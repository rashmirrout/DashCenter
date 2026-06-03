# DashCenter Live Migration CLI Guide

## Purpose

This guide defines the operator-facing CLI for ENI live migration in DashCenter. It goes deeper than the core migration design by explaining what a flow is, how flows are associated with an ENI, how migration artifacts represent flows, how exported plans are translated during apply, and how every migration command should behave and visualize its result in a production-grade terminal workflow.[cite:127][cite:130][cite:167][cite:15]

The goal is not only to list commands, but to make the CLI itself understandable as an operational language. A reader should be able to understand the mental model, know what data the command is using, see what the output looks like, and trust how the operation will behave under real migration conditions.[cite:129][cite:168]

## Reading model

The CLI is designed around three questions that operators repeatedly ask during ENI migration:

- What exists and what depends on it?[cite:15][cite:181]
- What traffic is active and who owns it?[cite:127][cite:130]
- What will happen if migration proceeds now?[cite:167][cite:129]

Every command in this guide is therefore grouped into one of five intents: discover, validate, plan, execute, and explain.

## What is a flow

In DashCenter, a **flow** is the operational record of traffic that is treated as belonging to the same communication unit for policy, steering, accounting, and migration purposes. A flow is usually keyed from the packet tuple and context that identify the conversation, such as source address, destination address, protocol, source port, destination port, direction, VNET or tunnel context, and sometimes additional metadata like service-chain stage or appliance path.[cite:181][cite:129]

A flow is not just a packet count. It is the smallest practical unit at which the migration system can answer questions like “is this traffic still active,” “which device owns it,” “can it be drained safely,” and “would rehoming it require state transfer.” That is why flow awareness becomes central to ENI live migration instead of being a secondary telemetry feature.[cite:127][cite:130][cite:129]

## Why flows matter in ENI migration

An ENI can be pre-created on a target device before it becomes active, but that alone does not move active traffic safely. What matters during migration is the relationship between packets, flow state, and ownership. Some traffic will belong to new flows created after cutover, while some will belong to already-established flows that may continue to depend on the source for some time. The migration controller must distinguish those categories explicitly.[cite:127][cite:130][cite:167]

That is why the system needs a flow model even when the operator thinks in terms of objects like ENI, ACL, or route group. Objects describe intent and realization, but flows reveal whether the traffic plane has actually converged on the intended outcome.[cite:181][cite:127]

## How flows are associated with an ENI

The association of a flow with an ENI is not guessed at random. DashCenter should determine it through a layered attribution process that follows the same traffic logic used by the DASH-capable appliance.

### Primary attribution

A flow is primarily associated with an ENI when traffic classification selects that ENI as the relevant endpoint context for packet handling. That association may be derived from the packet's ingress classification, attached vport or interface context, tenant or VNET mapping, host-side identity, or other control-plane signals that bind traffic to the ENI.[cite:15][cite:181]

### Supporting evidence

The system should strengthen or challenge that association through additional evidence:

- counters or flow tables keyed under the ENI;
- route, mapping, or ACL evaluation already recorded against that ENI;
- source or target ownership metadata for established flows;
- service-chain or tunnel context tied to that endpoint;
- packet trace or telemetry sample showing ENI classification result.[cite:127][cite:130][cite:181]

### Confidence model

DashCenter should assign one of four confidence levels when reporting flow association:

| Confidence | Meaning |
|---|---|
| `direct` | Flow came from a source that explicitly keyed it to the ENI |
| `derived-strong` | Flow was inferred from classification and matching runtime context |
| `derived-weak` | Flow was inferred from partial evidence such as counters plus path context |
| `unknown` | Flow may relate to the ENI but association is not trustworthy |

This matters because migration automation should not treat `unknown` flow attribution as safe evidence for commit.[cite:127][cite:130]

## Flow record model

DashCenter should represent each flow as an operational record with stable identity and migration-specific metadata.

A practical flow record should contain at least:

| Field | Meaning |
|---|---|
| `flow_id` | Stable DashCenter identifier for the flow record |
| `eni_id` | Associated ENI |
| `tuple` | Source/destination/protocol/ports or equivalent tuple |
| `direction` | Ingress, egress, bidirectional, or service-stage scoped |
| `vnet` | VNET or segmentation context |
| `owner_device` | Device currently responsible for the flow |
| `owner_state` | Source-owned, target-owned, transitioning, unknown |
| `first_seen` | Timestamp of first observation |
| `last_seen` | Timestamp of most recent observation |
| `idle_age_ms` | Time since last packet |
| `byte_count` | Observed bytes |
| `packet_count` | Observed packets |
| `statefulness` | Stateless, drain-safe, state-transfer-required, unknown |
| `association_confidence` | Direct, derived-strong, derived-weak, unknown |
| `migration_recommendation` | Drain, rehome, observe, block |

## Flow classes used by the CLI

For operator readability, the CLI should group flows into migration-relevant classes.

- **New flows**: created after cutover generation and expected to land on target.[cite:127][cite:130]
- **Residual source flows**: established before cutover and still active on source.[cite:127][cite:130]
- **Transferred flows**: flows that have been explicitly rehomed to target where supported.[cite:129]
- **Unknown flows**: flows with insufficient attribution or ownership evidence.[cite:127][cite:130]
- **Blocked flows**: flows whose dependencies or statefulness make them unsafe for migration.

These groupings are more useful in practice than raw per-flow dumps, because they tell the operator what kind of migration risk remains.

## How DashCenter discovers flows for an ENI

The system should build the flow set using a merge pipeline rather than a single source. Public DASH and SAI signals show the importance of operational ownership and runtime state, so the best design is to fuse multiple data sources into a normalized view.[cite:127][cite:130][cite:181]

### Source candidates

- Flow tables or runtime ownership tables exposed by the source device.[cite:127][cite:130]
- Counters and telemetry scoped by ENI or dependent object.[cite:181]
- Packet trace or sampled telemetry that records ENI classification.[cite:15][cite:181]
- Service-chain records when traffic is pinned to stateful processing.[cite:181]
- External steering generation and target landing evidence.[cite:167]

### Correlation process

1. Collect all candidate records from source and target.
2. Normalize tuple, timestamps, device identity, and VNET context.
3. Attach ENI using direct classification data where possible.
4. Infer ENI using route, mapping, and path context where direct linkage is absent.
5. Merge duplicates into a canonical flow record.
6. Score confidence and mark ownership state.
7. Publish a `FlowOwnershipView` for the session.[cite:127][cite:130]

## Flow inventory commands

The flow discovery commands are the foundation of all migration work. They answer the question, “what traffic is actually live for this ENI?”

### `dashctl eni flows`

This command lists current flows associated with an ENI. It is an operator's first step before creating or validating a migration plan.

```bash
dashctl eni flows eni-100
```

Example output:

```text
ENI: eni-100
Association summary:
  direct:         284
  derived-strong:  31
  derived-weak:     8
  unknown:          1

Flow classes:
  new-flows:              0
  residual-source-flows: 324
  transferred-flows:       0
  blocked-flows:           6

Top findings:
  WARN 6 flows pinned to firewall service-chain; state transfer unsupported
  WARN 1 flow has unknown ownership attribution
```

What it does:

- queries source and target flow candidates;
- resolves ENI association;
- groups flows into migration-relevant classes;
- highlights risk categories and confidence levels.[cite:127][cite:130][cite:181]

### `dashctl eni flows --wide`

This expands into a table of individual flows.

```bash
dashctl eni flows eni-100 --wide
```

Example output:

```text
FLOW ID     OWNER   STATE         DIR   PROTO  SRC                 DST                 AGE   BYTES     STATEFUL  RECOMMEND
fl-7812     dpu-a   source-owned  bi    tcp    10.1.1.10:45220     10.2.2.20:443       3m    14.2MB    yes       block
fl-7813     dpu-a   source-owned  bi    tcp    10.1.1.10:45221     10.2.2.20:443       8s    98KB      no        drain
fl-7814     dpu-a   source-owned  bi    udp    10.1.1.10:53001     10.3.4.9:53         1s    2KB       no        drain
fl-7815     ?       unknown       bi    tcp    10.1.1.10:45222     10.2.2.21:8443      40s   120KB     unknown   observe
```

What the reader should infer:

- `OWNER` is the currently authoritative device for the flow.
- `STATEFUL` indicates migration sensitivity.
- `RECOMMEND` is a migration-oriented recommendation, not a generic network verdict.

### `dashctl eni flows --group-by`

Operators often need summarized views, not raw flow dumps.

```bash
dashctl eni flows eni-100 --group-by owner_state

dashctl eni flows eni-100 --group-by age_bucket

dashctl eni flows eni-100 --group-by service_chain
```

Example output:

```text
Group: owner_state
source-owned   324
unknown          1
blocked          6
```

## Planning commands

Planning commands convert ENI and flow state into a migration artifact.

### `dashctl migration plan`

This command creates a migration plan rooted at the ENI and enriched by flow analysis.

```bash
dashctl migration plan eni-100 --from dpu-a --to dpu-b --strategy new-flows-first-drain
```

Example output:

```text
Plan: plan-342
ENI: eni-100
Source: dpu-a
Target: dpu-b
Strategy: new-flows-first-drain

Dependency summary:
  objects: 18
  external steering refs: 1
  unresolved: 0

Flow summary:
  total associated: 324
  drain-safe: 317
  transfer-required: 6
  unknown: 1

Initial decision:
  ADMIT WITH WARNINGS

Warnings:
  6 flows pinned to firewall service-chain may delay final commit
  1 flow lacks strong ownership attribution
```

What it does:

- builds dependency graph from the ENI;
- attaches live flow inventory;
- calculates initial migration strategy fitness;
- emits a stable `MigrationPlan` record.[cite:15][cite:127][cite:130][cite:167]

### `dashctl migration graph`

This command renders the dependency graph, optionally with flow overlays.

```bash
dashctl migration graph plan-342 --view flow-overlay
```

Example output:

```text
eni/eni-100
├─ vnet/blue-prod
├─ acl-group/acl-prod-in
├─ acl-group/acl-prod-out
├─ route-group/rg-west
├─ mapping/vnet-blue-to-west
├─ service-chain/svc-firewall-west   [6 transfer-required flows]
└─ steering/vip-selector-west        [new-flow switch point]
```

This makes the graph operationally useful by connecting objects to live traffic classes instead of showing a sterile config tree.

## Validation commands

### `dashctl migration validate`

This command decides whether the plan should proceed.

```bash
dashctl migration validate plan-342
```

Example output:

```text
Migration Readiness: 88/100

Capability parity       PASS
Dependency closure      PASS
Target realization      PASS
Telemetry freshness     PASS
Flow ownership support  PASS
Steering control        PASS
Drain viability         WARN 6 flows may exceed standard timeout
Unknown flow risk       WARN 1 unknown flow requires observation

Decision: PROCEED WITH POLICY REVIEW
```

What it does:

- validates target capability and dependency parity;
- checks target headroom and source health;
- folds in flow drainability and unknown-flow risk;
- emits a formal readiness report.[cite:127][cite:130][cite:167]

### `dashctl migration validate --explain`

```bash
dashctl migration validate plan-342 --explain
```

Example output:

```text
Reasoning trace:
- Capability parity passed because target supports all required object kinds.
- Drain viability warning triggered because 6 active flows depend on service-chain state.
- Unknown flow warning triggered because one flow lacks direct ENI attribution and target ownership evidence.
- Commit safety remains possible if residual threshold and timeout policy are expanded.
```

This style matters because enterprise operators need reasoning, not just a red/green gate.

## Export format

### What export contains

The migration export must include the flow view, because migration correctness is not determined only by object dependencies. A useful bundle contains both graph and traffic state.[cite:127][cite:130]

Recommended files:

```text
bundle/
  plan.yaml
  graph.json
  flows.json
  readiness-report.json
  source-snapshot.json
  target-template.json
  actions.yaml
  constraints.yaml
  signatures.json
```

### Example `flows.json`

```json
{
  "apiVersion": "dashcenter.io/v1alpha1",
  "kind": "FlowOwnershipView",
  "eni": "eni-100",
  "plan": "plan-342",
  "generatedAt": "2026-05-22T14:01:04Z",
  "summary": {
    "total": 324,
    "sourceOwned": 317,
    "targetOwned": 0,
    "transferRequired": 6,
    "unknown": 1
  },
  "flows": [
    {
      "flowId": "fl-7812",
      "tuple": {
        "srcIp": "10.1.1.10",
        "srcPort": 45220,
        "dstIp": "10.2.2.20",
        "dstPort": 443,
        "protocol": "tcp"
      },
      "eni": "eni-100",
      "ownerDevice": "dpu-a",
      "ownerState": "source-owned",
      "associationConfidence": "direct",
      "statefulness": "state-transfer-required",
      "serviceChain": "svc-firewall-west",
      "migrationRecommendation": "block"
    },
    {
      "flowId": "fl-7813",
      "tuple": {
        "srcIp": "10.1.1.10",
        "srcPort": 45221,
        "dstIp": "10.2.2.20",
        "dstPort": 443,
        "protocol": "tcp"
      },
      "eni": "eni-100",
      "ownerDevice": "dpu-a",
      "ownerState": "source-owned",
      "associationConfidence": "direct",
      "statefulness": "drain-safe",
      "migrationRecommendation": "drain"
    }
  ]
}
```

### Why export flows

Exporting flows serves four purposes:

- captures a migration-consistent baseline;
- makes policy review possible before cutover;
- gives auditability to later RCA;
- allows apply-time translation and delta checks.[cite:127][cite:130][cite:129]

## How apply translates exported flows

This is the part that often stays vague in weak designs. The export file is **not** replayed as a blind list of flows to recreate. Instead, flows are translated into migration intent and risk classes during apply.

### Translation rules

When `dashctl migration apply` consumes a bundle, DashCenter should process `flows.json` like this:

1. Revalidate each exported flow against current live observations.
2. Classify whether the flow still exists, has moved, or has aged out.
3. Map the flow into one of four apply-time buckets:
   - `historical-only`
   - `drain-on-source`
   - `eligible-for-target-ownership`
   - `block-or-review`
4. Update the session's live `FlowOwnershipView`.
5. Use the translated buckets to decide whether prepare, cutover, or commit can proceed.

### Example translation

An exported flow with `migrationRecommendation=drain` does **not** get installed on the target as a flow object. Instead, it becomes a live rule in the session logic: after cutover, source remains owner until the flow ages out or policy threshold says otherwise.[cite:127][cite:130]

An exported flow with `migrationRecommendation=block` becomes a gating condition. If that flow is still active at apply time and no state-transfer adapter exists, the session should not enter commit-ready state.[cite:129]

### `dashctl migration apply`

```bash
dashctl migration apply bundle.tar.gz
```

Example output:

```text
Bundle admitted: plan-342

Object translation:
  dependency graph loaded: 18 objects
  target template generated: yes
  target realizable now: yes

Flow translation:
  historical-only:             3
  drain-on-source:           314
  eligible-for-target-owner:   0
  block-or-review:             7

Apply decision:
  SESSION CREATED: session-991
  phase: prepare
  commit blocked until 7 flows are resolved or policy override is approved
```

That output makes it obvious that the bundle is translated into live migration policy state, not blindly replayed as stale telemetry.

## Prepare commands

### `dashctl migration prepare`

```bash
dashctl migration prepare plan-342
```

Example output:

```text
Session: session-991
Phase: PREPARE

Target object realization:
  eni/eni-100                    READY
  vnet/blue-prod                 READY
  acl-group/acl-prod-in          READY
  acl-group/acl-prod-out         READY
  route-group/rg-west            READY
  mapping/vnet-blue-to-west      READY
  service-chain/svc-firewall-west READY (standby)

Flow posture at prepare:
  source-owned active: 324
  target-owned active:   0
  unknown:               1

Decision: PREPARE COMPLETE
```

What it does:

- creates shadow objects on target;
- confirms target realization;
- snapshots current flow posture to compare with future phases.

## Synchronization commands

### `dashctl migration sync`

```bash
dashctl migration sync session-991
```

Example output:

```text
Session: session-991
Phase: SYNC

Last sync: 2026-05-22T14:03:09Z
Sync lag: 118ms

Object deltas applied:
  acl-group/acl-prod-in      0
  route-group/rg-west        1
  mapping/vnet-blue-to-west  0

Flow deltas:
  new source-owned flows:        4
  aged-out source flows:        11
  unknown flows resolved:        1
  blocked flows unchanged:       6

Status: SYNC HEALTHY
```

This command should be safe to run repeatedly and should always make the live delta visible.

## Cutover commands

### `dashctl migration cutover`

```bash
dashctl migration cutover session-991 --new-flows-only
```

Example output:

```text
Session: session-991
Phase: CUTOVER
Generation: 14

Pre-cutover checks:
  sync lag:                    PASS (122ms)
  steering adapter reachable:  PASS
  target realization healthy:  PASS
  unknown flows under limit:   PASS

Actions:
  steering switched to target for new flows
  target marked authoritative for new flows
  source retained ownership for residual flows

Immediate landing check:
  target new flows observed: PASS (12 within 3s)
  source unexpected new flows: WARN (1)

Result: CUTOVER ACTIVE
```

The output matters because it shows both the control action and the first data-plane proof.

### `dashctl migration cutover --canary`

```bash
dashctl migration cutover session-991 --canary 10
```

This should route a bounded fraction of new flows to the target where supported, useful in large environments or when service-chain statefulness increases risk.[cite:167]

## Drain commands

### `dashctl migration drain`

```bash
dashctl migration drain session-991
```

Example output:

```text
Session: session-991
Phase: DRAIN
Elapsed: 4m20s

Residual source flows:
  total: 81
  drain-safe: 75
  transfer-required: 6

Age buckets:
  0-10s:   0
  10-60s: 12
  1-5m:   43
  5-30m:  20
  30m+:    6

Decision:
  commit not yet allowed
  6 flows remain blocked on service-chain state
```

This is the operational heart of V1 migration. It tells the operator exactly why the session is not yet finished.[cite:127][cite:130][cite:129]

### `dashctl migration flows`

```bash
dashctl migration flows session-991
```

Example output:

```text
Owner State Distribution
source-owned   81
target-owned  223
unknown         0
blocked         6

Top blocked flows:
fl-7812 tcp 10.1.1.10:45220 -> 10.2.2.20:443  service-chain=svc-firewall-west  reason=no-state-transfer
fl-7901 tcp 10.1.1.10:46011 -> 10.2.8.11:443  service-chain=svc-firewall-west  reason=no-state-transfer
```

## Commit commands

### `dashctl migration commit`

```bash
dashctl migration commit session-991
```

Example output:

```text
Commit check:
  residual source flows below threshold: PASS
  blocked flows remaining:               FAIL (6)
  override policy present:               NO

Decision: COMMIT DENIED
Reason: active blocked flows require operator action or policy override
```

This is intentionally conservative. Commit must be a policy-governed action, not just the next button.

### Commit with override

```bash
dashctl migration commit session-991 --override blocked-stateful-flows --reason "approved maintenance window"
```

Example output:

```text
Override accepted by policy: yes
Rollback window: 1800s

Commit actions:
  target becomes sole authoritative owner for new flows
  source retained as rollback checkpoint
  source cleanup deferred until rollback window expires

Result: COMMITTED WITH OVERRIDE
```

## Rollback commands

### `dashctl migration rollback`

```bash
dashctl migration rollback session-991
```

Example output:

```text
Session: session-991
Phase: ROLLBACK

Actions:
  steering restored to source
  source restored as new-flow owner
  target shadow state retained for diagnosis

Post-rollback checks:
  source new flows observed: PASS
  target unexpected new flows: 0

Result: ROLLBACK COMPLETE
```

Rollback output should be crisp, because operators invoke it under stress.

## Explain commands

### `dashctl migration explain`

This is the most human-facing command in the set. It should narrate the migration state in plain operator language.

```bash
dashctl migration explain session-991
```

Example output:

```text
Migration session-991 is in DRAIN phase.

Why it is not committed:
- 6 active flows are attached to service-chain svc-firewall-west.
- The current target adapter does not support state transfer for that chain.
- These flows are still source-owned and have not yet aged out.

What is safe now:
- New flows are already landing on target.
- Target realization is healthy.
- Rollback remains available for 23m.

What can happen next:
- wait for drain,
- terminate blocked flows,
- or commit with approved override policy.
```

This command should be available at every phase and should answer the operator's real question: “what is happening and what should be done now?”

## Bundle commands

### `dashctl migration bundle`

```bash
dashctl migration bundle session-991 --output support-bundle.tar.gz
```

Example output:

```text
Bundle contents:
  session.json
  plan.yaml
  graph.json
  flows-before.json
  flows-now.json
  readiness-report.json
  target-realization.json
  steering-history.json
  findings.log

Result: support-bundle.tar.gz written
```

The support bundle should make a failed or slow migration completely reconstructable offline.

## Command reference summary

| Command | Main purpose | Primary output form |
|---|---|---|
| `dashctl eni flows <eni>` | Discover ENI-associated traffic | Summary table |
| `dashctl eni flows --wide` | Inspect individual flows | Detailed flow table |
| `dashctl migration plan` | Create migration plan | Plan summary |
| `dashctl migration graph` | Visualize dependency graph and flow overlays | Tree/graph view |
| `dashctl migration validate` | Gate migration readiness | Readiness scorecard |
| `dashctl migration export` | Export plan and flow baseline | Bundle files |
| `dashctl migration import` | Admit bundle into DashCenter | Admission report |
| `dashctl migration apply` | Translate bundle into live session state | Translation summary |
| `dashctl migration prepare` | Realize shadow state on target | Target realization table |
| `dashctl migration sync` | Refresh object and flow deltas | Delta report |
| `dashctl migration cutover` | Shift new-flow steering | Action log + landing proof |
| `dashctl migration drain` | Watch residual source activity | Age buckets + block reasons |
| `dashctl migration flows` | Inspect session ownership | Ownership summary |
| `dashctl migration commit` | Finalize migration | Policy decision report |
| `dashctl migration rollback` | Restore source authority | Rollback action log |
| `dashctl migration explain` | Human explanation of state | Narrative explanation |
| `dashctl migration bundle` | Export evidence for RCA | Bundle manifest |

## Output design principles

Every migration command should follow these principles:

- show decision first, details second;
- separate facts from recommendations;
- surface confidence when flow attribution is imperfect;
- preserve generation and timestamp context;
- describe next legal actions explicitly.

That design discipline is what makes the CLI suitable for enterprise operations instead of becoming a collection of opaque debug commands.

## Final CLI position

The Live Migration CLI should make flows first-class citizens of the operator experience. Without flow awareness, migration commands only describe configuration. With flow awareness, the CLI can describe what traffic is live, who owns it, how export captures it, how apply translates it, why cutover is safe or unsafe, and what remains before commit. That is the difference between a hobby-grade migration tool and a production-grade migration control surface.[cite:127][cite:130][cite:129][cite:167]
