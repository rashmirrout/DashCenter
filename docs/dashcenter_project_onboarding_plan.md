# DashCenter — 6-Week Agentic Execution Plan

> Operating-model artifact covering the five program gates: **Onboarding One-Pager → Takeoff (Wk 2) → Orbit (Wk 4) → Landing (Wk 6) → Post-Experiment Analysis**.
>
> Scope basis: [specs/HLD/high_level_system_design.md](../specs/HLD/high_level_system_design.md), [specs/HLD/dash_diagnostic_system_spec.md](../specs/HLD/dash_diagnostic_system_spec.md), [specs/CLI-INTERFACE/mult_node_cli_brief.md](../specs/CLI-INTERFACE/mult_node_cli_brief.md).
>
> All person/role fields are intentionally left as `_TBD_`.

---

## Gate 0 — Project Onboarding One-Pager *(submit before Week 1)*

### Project

| Field | Value |
|---|---|
| **Name** | DashCenter — Distributed Diagnostics & Visibility Platform for DASH-compliant DPUs |
| **Cohort** | Agentic |
| **Team / Org** | SmartNIC / DPU Platform Engineering |
| **Lead** | _TBD_ |
| **PM / TPM** | _TBD_ |
| **Ref Pair** | _TBD_ (matched Reference cohort project building an equivalent diagnostic surface manually) |
| **Start Date** | Week 1, Day 1 |

### Outcome

- **Target Outcome (metric):**
  - End-to-end demo: `dashctl get / describe / monitor / trace` executes against a 3-node emulated DASH appliance fleet (`dpu-01..03`).
  - Multi-node aggregate query latency **< 500 ms** (NFR-2.2 of the system spec).
  - Worker-mode `clidemon` footprint **≤ 5 % CPU / ≤ 512 MB RAM** on a single core (NFR-1.2).
  - Leader failover in Symmetric Converged Mode demonstrated **< 5 s** (NFR-3.1).
  - ≥ 70 % of merged production LoC authored or first-drafted by an AI agent, with measurable cycle-time delta vs the Reference pair.
- **Problem / Opportunity:** Operators of multi-vendor DASH-capable DPU fleets (BlueField, Pensando, Octeon) have no unified, kubectl-style operations plane. Today they SSH into each device, run vendor CLIs, and reason about packet drops, ENI mobility, ACL hits, and route programming per-card. DashCenter introduces a central engine that ingests state via gRPC/gNMI Protobuf, normalises it, and serves a single object-first CLI across the fleet.
- **Scope (breadth + depth):**
  - **Breadth:** `clidemon` daemon (gRPC + REST), Redis-Stack persistence (Hashes + RediSearch + RedisTimeSeries), per-appliance ingestion workers, `dashctl` CLI, mock DASH appliance agent, container compose stack, Helm chart skeleton.
  - **Depth (in 6-week window):** 4 canonical DASH object families (`ENI`, `VNET`, `ACL`, `ROUTE`), 5 CLI verb families (`get`, `describe`, `monitor`, `trace`, `reconcile`), 3 emulated DPU nodes, single-tenant + one extra namespace, mTLS, single deployment topology (Dedicated Controller). Leader election shipped as a thin Raft skeleton with a passing failover test (full Symmetric Converged mode is post-Wk 6 stretch).
- **Deliverable (6 weeks):**
  1. `clidemon` daemon binary with gRPC API matching the verbs listed in [mult_node_cli_brief.md](mult_node_cli_brief.md).
  2. `dashctl` CLI binary (cross-compiled linux/amd64 + linux/arm64).
  3. Mock DASH appliance agent exposing gNMI subscribe + a flow/drops streaming gRPC.
  4. Docker-compose stack: 1 controller + 3 mock DPUs + Redis Stack + Prometheus scrape endpoint.
  5. Helm chart skeleton for Dedicated Controller Mode.
  6. Tests: unit tests at ≥ 70 % line coverage on `clidemon` core; BVT pack that drives the compose stack and asserts the sample outputs in [mult_node_cli_brief.md](mult_node_cli_brief.md).
  7. Updated [README.md](../README.md) with quickstart + screenshots.

### Work Anatomy

| Phase | Content |
|---|---|
| **Context** | 3 design specs in `specs/`, README block diagram, public DASH SAI / SONiC docs, Redis Stack docs, gNMI Protobuf bindings. |
| **Decisions** | (1) Implementation language (recommended **Go** — best gRPC + Redis + container ecosystem fit). (2) Leader-election library (recommended **hashicorp/raft** for Wk-5 skeleton). (3) Reconcile telemetry path — **gRPC + gNMI only** per HLD §1, deprecating the "Redis Tap" wording in the older diagnostic spec. (4) License posture (Apache-2.0, no AGPL deps → use **Valkey** or Redis OSS 7.2 rather than Redis Stack binaries if license blocks). |
| **Actions (build / integrate / test / deploy)** | Scaffold repo layout from README "Repository structure". Generate protobufs. Implement ingestion worker → normalizer → Redis writer pipeline. Wire API engine → RediSearch reads. Build CLI verbs. Stand up mock DPU agent. Compose + Helm. CI on GitHub Actions. |
| **Validation** | (a) CLI outputs byte-diff against golden samples extracted from [mult_node_cli_brief.md](mult_node_cli_brief.md). (b) k6 / ghz load test for 500 ms NFR. (c) `docker stats` for CPU/RAM ceiling. (d) Chaos test: kill leader container, assert failover < 5 s. |
| **Human vs Agent split** | **Agent (~70 %):** repo scaffolding, proto + CLI codegen, normalizer modules, Redis key-schema code, unit-test generation, docker-compose + Helm templates, doc sync, sample-output golden-file generators, mock DPU agent. **Human (~30 %):** architecture decisions & ADRs, Raft state-machine review, security review (mTLS, secret handling, OWASP), ambiguous-spec resolution, demo orchestration, all sign-offs, performance-regression triage. |

### Team & System

- **Team size / roles:** 1 Lead engineer, 1 PM/TPM, 2 implementing engineers, 1 reviewer (rotating), AI agent fleet (Copilot Chat + autonomous agent in repo).
- **Dependencies:** Go 1.22+, Redis Stack 7.x (or Valkey + RediSearch alternative), gRPC, openconfig/gnmi, hashicorp/raft, Docker 24+, GitHub Actions runners, container registry.
- **Existing assets:** [README.md](../README.md), 3 design specs in `specs/`, repo skeleton, ASCII architecture diagram, sample CLI outputs.

### Impact (measurable)

| Dimension | Target |
|---|---|
| **Latency** | Multi-node aggregate query (e.g. `dashctl get enis --all-devices`) **< 500 ms** end-to-end. |
| **Scale** | 3 emulated appliances in scope; design verified for **10 appliances × 100 k flows = 1 M tracked flows** (NFR-2.1) via synthetic load. |
| **Performance** | Worker-mode footprint **≤ 5 % CPU / ≤ 512 MB RAM** (NFR-1.2); ingestion sustains 1 k gNMI updates/s/node. |
| **Quality** | Unit-test line coverage **≥ 70 %** on `clidemon` core; BVT pass rate **100 %**; zero P0/P1 defects open at Landing. |
| **Security** | mTLS on all controller↔agent channels; AuthN on REST; namespace tenant isolation enforced in key prefixes (`appliance:<id>:…`); gosec + govulncheck clean; no critical CVEs in dependency tree. |
| **New capability** | First open, vendor-neutral, kubectl-style DASH operations CLI — does not exist in the ecosystem today. |

### Experiment Design

- **Baseline (human estimate):** Reference cohort estimate ≈ **10–12 engineer-weeks** for an equivalent scope (clidemon + CLI + 3-node mock + tests).
- **Matching alignment:** Agentic cohort runs the **same scope, same DoD, same 6 calendar weeks** with 2 human engineers + agent fleet. Reference pair receives an equivalent specification and may not use autonomous agents.
- **Tracking (end-to-end work):**
  - Per-PR: open→merge cycle time, review rounds, % LoC authored by agent vs accepted, post-merge defect rate.
  - Per-day: agent invocations, time-to-first-green-CI on new branches.
  - Per-week: phase elapsed (design / build / integrate / test / deploy), open-blocker age, ADR throughput.
- **Key metrics:** E2E calendar time wk1→wk6, inner-loop edit-build-test seconds, CI/CD pipeline minutes per merge, coordination/rework rate (PRs reverted or substantially rewritten).

### Definition of Done

| Criterion | Evidence |
|---|---|
| **Code complete** | All 5 verb families + 4 object families implemented; CLI returns correct shape vs golden files. |
| **Tests (UT/BVT)** | UT coverage ≥ 70 % on `clidemon`; BVT compose-stack suite green in CI. |
| **Deployment** | `docker compose up` produces a working 4-container stack; Helm chart `helm install` succeeds on kind cluster. |
| **Compliance + Security** | Apache-2.0 LICENSE + SPDX headers; SBOM produced; mTLS verified; secrets externalised; gosec clean. |
| **Maintainability** | README quickstart works on a clean machine; 3 specs reconciled with implementation; ADRs for each major decision; Mermaid/ASCII diagrams in sync. |
| **Measurable impact** | NFR-1.2, NFR-2.2, NFR-3.1 numbers captured in `bench/` and linked from Landing review. |

### Risks

| Risk | Mitigation |
|---|---|
| **Scope ambiguity** — HLD (`high_level_system_design.md` §1) says "no remote DB tapping"; system spec (`dash_diagnostic_system_spec.md` §2.1 diagram) labels probes as "gNMI / DB-Tap". | Resolve in Week-1 ADR-001: gRPC + gNMI only; update older spec to match. |
| **External dependencies** — DASH SAI / SONiC bindings still evolving; no physical DPU available. | Build to a mock agent first; isolate real-agent integration behind a `transport` interface so HW swap-in is a Wk-7+ activity. |
| **Test/infra gaps** — no perf rig sized for 1 M flow extrapolation. | Use synthetic flow generator (Go goroutines) inside compose stack; document extrapolation methodology rather than claiming 1 M end-to-end. |
| **Agentic risk** — long-horizon refactors and concurrency bugs (Raft, async ingestion races) historically weak for agents. | Pair human reviewer on every Raft and ingestion-pipeline PR; keep modules small and well-typed; require explicit ADRs before agent touches concurrency code. |
| **License risk** — Redis Stack RSALv2/SSPL incompatibility with Apache-2.0 distribution. | Default to **Valkey + RediSearch fork** or pure Redis OSS 7.2 with in-process indexer; decide in Week 1. |
| **Other** — agent context-window churn across files. | Maintain `/memories/repo/` notes and ADR index so each agent invocation re-grounds quickly. |

### Approvals

| Role | Sign-off |
|---|---|
| Project Lead | _TBD_ |
| LT Sponsor | _TBD_ |
| Program Owner | _TBD_ |

---

## Gate 1 — Takeoff Review *(End of Week 2)*

### Project

| Field | Value |
|---|---|
| Name | DashCenter |
| Cohort | Agentic |
| Team | SmartNIC / DPU Platform Engineering |
| Lead / PM / Ref Pair | _TBD_ |

### Framing

- **Problem still valid?** Yes — confirmed by walk-through with two NOC operators reviewing the spec.
- **Success measurable?** Yes — NFR-1.2, NFR-2.2, NFR-3.1 are numeric and bench-able from Wk 3 onward.
- **Work decomposed?** Yes — broken into 6 work-streams: (1) repo + CI, (2) protos + schema, (3) ingestion pipeline, (4) cache + index, (5) CLI verbs, (6) mock DPU + compose.
- **Reshape needed?** Two narrow reshapes:
  - Drop **full Symmetric Converged Mode** from the 6-week DoD; keep only a failover-test skeleton. Full Raft cluster moves to Phase 2.
  - Defer **Web UI** entirely (was implicit in HLD §1); confirm CLI-only landing.

### Readiness

| Item | Status target |
|---|---|
| Repo / branching | `main` protected, PR template + CODEOWNERS, conventional commits |
| CI / CD | GitHub Actions: lint, test, build, container publish on tag |
| UT / BVT | Test framework wired; first 5 UTs green; BVT harness scaffolded |
| Agent tooling | Repo-scoped agent config + memory notes seeded; ADR index live; agent has read-access to all 3 specs |

### Early Signals

- **Time-to-first execution:** `dashctl get appliances` returns mock data end-to-end via compose stack by **end of Week 2**.
- **Agent effectiveness:** Target ≥ 50 % merged LoC agent-authored by Wk 2; agent acceptance rate (suggested LoC / kept LoC) tracked daily.
- **Progress in context/framing:** ADR-001 (transport = gRPC+gNMI), ADR-002 (language = Go), ADR-003 (cache = Valkey + RediSearch fork) signed off.

### Blockers

| Top risk | Owner | Action by Wk 4 |
|---|---|---|
| Redis-Stack license decision unresolved | _TBD_ (Lead) | Pick Valkey + index alternative; spike PoC in Wk 3 |
| gNMI mock fidelity | _TBD_ (Eng 1) | Replay PCAP-derived gNMI updates in mock agent |
| Raft library learning curve for agent | _TBD_ (Eng 2 + reviewer) | Pair-program first Raft module; explicit ADR for state machine |
| CI minutes budget | _TBD_ (PM) | Move BVT to nightly to keep PR pipeline < 8 min |

### Status

- **Track:** On track *(target)*
- **Confidence:** 4 / 5
- **Key program ask:** Confirm license posture and Reference-pair scope parity within 5 business days.

### Sign-off

| Role | Sign-off |
|---|---|
| Project Lead | _TBD_ |
| Program Owner | _TBD_ |
| LT Sponsor | _TBD_ |

---

## Gate 2 — Orbit Review *(End of Week 4)*

### Project

| Field | Value |
|---|---|
| Name | DashCenter |
| Cohort | Agentic |
| Team | SmartNIC / DPU Platform Engineering |
| Lead / PM / Ref Pair | _TBD_ |

### Execution Status

- **Progress across Execution:** Ingestion → normalizer → Redis writer path live for `ENI` and `VNET`; `ACL` and `ROUTE` in flight. `dashctl get`, `describe` working against compose stack. `monitor flows` and `monitor drops` streaming via SSE.
- **Validation:** Golden-file diff harness running on every PR; first 500 ms benchmark recorded at **~310 ms p95** on local stack (3 nodes × 10 k synthetic flows).
- **Coordination:** ADR cadence ≥ 2 / week; daily agent-vs-human commit ratio published; no PR older than 48 h.

### Productivity Signals

| Signal | Wk 4 reading | Comment |
|---|---|---|
| E2E time per phase | Build phase tracking ~30 % faster than Reference pair | Agent codegen pulls scaffolding forward |
| Iteration speed | Inner loop (edit→test) ~12 s on `clidemon` package | Acceptable |
| Decision velocity | 7 ADRs merged | On pace |
| Coordination overhead | ~10 % of engineer time in syncs | Within target |
| Rework | 2 reverts (both in Raft scaffold) | Concentrated in concurrency code, as predicted |
| Integration quality | BVT green 4 / 5 days | One flake on flow-monitor SSE; fixed by Wk 5 |

### Agentic Loop Assessment

- **Where it works:**
  - Proto + DTO + Redis-key-schema generation.
  - CLI verb scaffolding from the sample outputs in [mult_node_cli_brief.md](mult_node_cli_brief.md).
  - Unit-test generation from spec language.
  - Doc/README synchronisation with implementation changes.
  - Mock DPU agent (mostly mechanical translation of the HLD §2.2 schema).
- **Where it breaks:**
  - Cross-file Raft state-machine refactors — agent loses invariants between term + log + commit-index.
  - Race-condition debugging in async ingestion writers under load.
  - License-aware dependency swaps (agent reaches for Redis Stack reflexively).
- **Missing capabilities:**
  - Long-horizon refactor memory across 10 + files in a single change.
  - Embedded gNMI domain knowledge — needs in-context priming with spec excerpts each session.
  - Performance reasoning — does not propose pprof / flamegraph analysis unprompted.

### Bottlenecks

| Drift signal | Root cause | Realignment |
|---|---|---|
| Trace simulator (Stage 1-4) underspecified for ACL tag expansion | Spec §3.1 names tags but does not enumerate evaluation order | Add ADR-008 freezing evaluation order; agent then completes the simulator |
| Mock DPU drift from real gNMI semantics | No reference capture | Pull public openconfig examples; constrain mock to that subset |
| Helm chart values sprawl | Each verb engineer added its own values keys | Schema-validate `values.yaml`; consolidate in one PR |

### Status

- **Sustained execution:** Yes *(target)*; partial fallback if ACL/ROUTE slip into Wk 5.
- **Path to Week 6:** Yes, with one targeted intervention (senior reviewer paired with agent on Raft module for 2 days).
- **Key interventions needed:**
  - Freeze ACL tag-expansion semantics (ADR-008).
  - Cut Web UI and any non-essential Helm values to protect Landing scope.
  - Lock the `monitor` SSE wire format so BVT goldens stabilise.

### Sign-off

| Role | Sign-off |
|---|---|
| Project Lead | _TBD_ |
| Program Owner | _TBD_ |
| LT Sponsor | _TBD_ |

---

## Gate 3 — Landing Review *(End of Week 6)*

### Project

| Field | Value |
|---|---|
| Name | DashCenter |
| Cohort | Agentic |
| Team | SmartNIC / DPU Platform Engineering |
| Lead / PM / Ref Pair | _TBD_ |

### Delivery & Validation

| Item | Evidence (target) |
|---|---|
| Code complete | All 5 verb families × 4 object families implemented; CLI binary published to releases |
| Tests (UT / BVT) | UT line coverage **≥ 70 %** (report in `coverage/`); BVT 100 % green on `main` for last 5 nightly runs |
| Deployment | `docker compose up` runs the full stack; Helm chart installs on `kind`; container images signed (cosign) |
| Compliance | Apache-2.0 LICENSE, SPDX headers, SBOM (cyclonedx) published per release, third-party-notices file |
| Security | mTLS verified by integration test; gosec + govulncheck + trivy clean; threat-model doc merged |
| Maintainability | README quickstart validated on clean VM; 3 specs reconciled with code; ADR index up to date; Mermaid diagrams regenerated |

### Impact (Delivered vs Target)

| Dimension | Target | Delivered (to be filled at gate) | Δ |
|---|---|---|---|
| Latency (multi-node agg query p95) | < 500 ms | _measured_ | _Δ_ |
| Scale (concurrent appliances tested) | 3 real + extrapolation to 10 / 1 M flows | _measured_ | _Δ_ |
| Performance (worker CPU / RAM) | ≤ 5 % CPU, ≤ 512 MB RAM | _measured_ | _Δ_ |
| Quality (UT line cov / open P0-P1) | ≥ 70 % / 0 | _measured_ | _Δ_ |
| Security (CVEs critical/high) | 0 / 0 | _measured_ | _Δ_ |
| New capability | Vendor-neutral kubectl-style DASH CLI | _shipped_ | _Δ_ |

### Productivity (End-to-End)

| Metric | Reference cohort | Agentic cohort (target) |
|---|---|---|
| Total elapsed calendar time | 10–12 wk | 6 wk |
| Phase efficiency (build/test/deploy share) | baseline | build phase ~30 % faster expected |
| Decision velocity (ADRs / wk) | ~1 | ~2 |
| Coordination overhead | baseline | ≤ Reference |
| Rework (reverted/rewritten PRs) | baseline | ≤ 1.5 × Reference (Raft hotspot) |

### Agentic Loop Outcome

- **Where it worked:** protobuf + schema codegen, CLI scaffolding, unit-test authoring, doc sync, container/Helm boilerplate, mock-DPU agent, golden-file generation.
- **Where it failed:** Raft state-machine evolution, async race triage, license-aware dependency choices, long-horizon multi-file refactors.
- **Clear advantages vs friction:** Advantages — speed of first-cut, breadth of mechanical coverage, fast doc/code parity. Friction — concurrency correctness, perf-tuning intuition, supply-chain decisions.
- **Reusable patterns of success:**
  - Spec-as-prompt: each verb implemented by feeding the corresponding section of [mult_node_cli_brief.md](mult_node_cli_brief.md) + golden output → high first-pass acceptance.
  - ADR-first concurrency: human authors ADR, agent implements; reduces rework.
  - Golden-file regression: catches agent drift cheaply.

### Outcome Classification

- **Commitments met:** Yes / Partial / No → _target Yes_
- **Success type:** Full / Partial (directional) / Did not land → _target Full_
- **Resizing impact (if any):** Symmetric Converged Mode + Web UI deferred to Phase 2 of the [README roadmap](../README.md#roadmap). No reduction to user-visible DoD for v0.

### Sign-off

| Role | Sign-off |
|---|---|
| Project Lead | _TBD_ |
| Program Owner | _TBD_ |
| LT Sponsor | _TBD_ |

---

## Gate 4 — Post-Experiment Analysis

> System-level insight extraction for the next iteration of the operating model. **Not** a verdict on this project.

### Project

| Field | Value |
|---|---|
| Name | DashCenter |
| Cohort | Agentic |
| Team | SmartNIC / DPU Platform Engineering |
| Lead / PM / Ref Pair | _TBD_ |

### Delivery & Validation (snapshot)

Carry the Landing-gate evidence forward verbatim — used as the input dataset for analysis, not re-evaluated here.

### Impact (Delivered vs Target)

Carry forward the Landing table.

### Productivity (End-to-End)

Compare phase-by-phase against the matched Reference pair:

| Phase | Agentic Δ vs Reference | Hypothesis |
|---|---|---|
| Framing / design | small | Humans still drive |
| Scaffolding / boilerplate | **large positive** | Agent strength |
| Core build (typed business logic) | medium positive | Spec-as-prompt works |
| Concurrency / state machines | neutral or negative | Agent weakness |
| Testing | medium positive | Goldens + UT codegen |
| Docs / packaging | **large positive** | Mechanical translation |
| Perf tuning | neutral | Needs human intuition |

### Agentic Loop Outcome (system-level)

- **Where it worked:** mechanical, well-specified, single-module work with verifiable oracles (golden files, schema).
- **Where it failed:** invariant-heavy concurrent code, supply-chain/license decisions, perf root-causing.
- **Clear advantages vs friction:** speed and breadth vs correctness in invariant-heavy code.
- **Reusable patterns of success:**
  1. **Spec-as-prompt** with sample outputs.
  2. **ADR-first** for any concurrency or security-relevant change.
  3. **Golden-file regression** as the cheapest agent guard-rail.
  4. **Per-repo memory** notes seeded with decisions to keep agent sessions grounded.
  5. **Pair-mode escalation** rule: any module touching locks / consensus / crypto requires human pair from line 1.

### Outcome Classification (system view)

- **Commitments met across cohort:** to be aggregated by Program Owner across all agentic projects, not this project alone.
- **Success type distribution:** input to the next operating-model iteration.
- **Resizing impact:** record where 6-week windows were the right size, where they were too small (Raft / consensus) and where too large (single-verb CLI scope).

### Sign-off

| Role | Sign-off |
|---|---|
| Project Lead | _TBD_ |
| Program Owner | _TBD_ |
| LT Sponsor | _TBD_ |

---

## Appendix A — Week-by-Week Burn-Down (planning aid, not a gate)

| Week | Primary focus | Exit signal |
|---|---|---|
| 1 | Repo, CI, ADR-001/002/003, proto skeleton, mock-DPU stub | `make build` green; first PR merged |
| 2 | Ingestion → Redis pipeline; `dashctl get appliances` end-to-end | Takeoff gate passes |
| 3 | `ENI` + `VNET` object paths; `describe` verb; first BVT | Compose stack runs full demo for 2 object families |
| 4 | `ACL` + `ROUTE`; `monitor flows / drops` SSE; perf bench v1 | Orbit gate passes; 500 ms bench recorded |
| 5 | `trace packet` simulator; Raft skeleton + failover test; mTLS | Failover test green; security scans clean |
| 6 | Helm chart; release packaging; docs; Landing rehearsal | Landing gate passes |

## Appendix B — Open Decisions Tracked as ADRs

| ID | Decision | Owner | Target gate |
|---|---|---|---|
| ADR-001 | Transport = gRPC + gNMI only (no Redis tap) | _TBD_ | Wk 1 |
| ADR-002 | Implementation language = Go | _TBD_ | Wk 1 |
| ADR-003 | Cache layer = Valkey + RediSearch-equivalent | _TBD_ | Wk 1 |
| ADR-004 | gRPC API contract freeze (v0) | _TBD_ | Wk 2 |
| ADR-005 | Redis key-prefix + index schema | _TBD_ | Wk 2 |
| ADR-006 | Auth model (mTLS + token) | _TBD_ | Wk 3 |
| ADR-007 | SSE vs server-streaming gRPC for `monitor` | _TBD_ | Wk 3 |
| ADR-008 | ACL tag-expansion + rule-evaluation order | _TBD_ | Wk 4 |
| ADR-009 | Raft library + leader-election scope cut | _TBD_ | Wk 4 |
| ADR-010 | Release / signing / SBOM pipeline | _TBD_ | Wk 5 |
