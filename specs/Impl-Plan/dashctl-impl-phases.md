# dashctl — Implementation Phase Tracker

> **Purpose**: Single source of truth for `dashctl` implementation progress.
> **Ground truth**: [`specs/HLD/dashctl-hld.md`](../HLD/dashctl-hld.md) and
> [`specs/LLD/dashctl-lld.md`](../LLD/dashctl-lld.md).
> **Companion**: dashd phase tracker is [`impl-phases.md`](impl-phases.md).
> **Last updated**: 2026-06-09

---

## Legend

| Symbol | Meaning |
|--------|---------|
| ✅ | Complete — code written, tests pass, gate verified |
| ⏳ | In progress — partially done or blocked |
| ❌ | Not started |
| ⬜ | Deferred — intentionally skipped for this phase |

---

## Overall progress

| Phase | Objective | Status | Gates Passed |
|---|---|---|---|
| **Phase 1** — REST backend (operator-ready CLI) | Cobra command tree + REST SDK against dashd `:8443`/`:7443`; all write & read verbs for the spec kinds shipped today; production-grade UX (contexts, output formats, tests). | ❌ Not started | 0 / 12 |
| **Phase 2** — gRPC backend (full fidelity) | gRPC SDK against dashd `:9443`; native streaming; `ApplyBatch`; Operations / HA / Migration / Diagnostics commands as dashd's Phase 2 milestones land. | ❌ Not started | 0 / 10 |

> **Dependency**: Phase 1 of dashctl can ship today against dashd Phase 1B
> (REST is feature-complete). Phase 2 unlocks incrementally as dashd's
> Phase 2 milestones land (PA → PB → PC → PD → PE).

---

## Implementation order

```
dashd 1B ✅ ──► dashctl Phase 1 (REST) ──┐
                                          ├─► dashctl Phase 2 (gRPC) — incremental as dashd Phase 2 lands
dashd 2 (PA→PE) ─────────────────────────┘
```

---

## Phase 1 — REST backend ❌

### Objective

Deliver a **production-grade operator CLI** that drives a running dashd
end-to-end via its REST surface (`:8443`) and admin surface (`:7443`).
Phase 1 covers every spec kind dashd ships today, every read endpoint,
contexts, output formats, completion, and a full test pyramid. There is
**no** gRPC code in this phase — no proto stubs in the dashctl binary,
no native streaming, no `ApplyBatch`.

### Scope

| In scope | Out of scope (Phase 2) |
|---|---|
| Cobra command tree, persistent global flags | gRPC backend |
| Kubeconfig-style contexts | `ApplyBatch` (client-stream) |
| REST SDK (`pkg/client/rest`) | Native server-streaming (events/status) |
| Apply / Get / List / Delete / Describe / Edit / Replace / Diff | Operations service (cordon/drain) |
| Inventory put/get | HA service |
| Reconcile | Migration service |
| `dpu list`, `dpu drift`, `dpu status` (snapshot + polling) | Diagnostics service (`trace flow`) |
| `events --watch` via SSE/long-poll where available | `--prune`, `--cascade`, `--parallel` |
| `-o json/yaml/table/wide/name/jsonpath/template` | Krew-style plugins |
| Manifest envelope codec (multi-doc YAML/JSON, dirs, stdin) | OIDC login |
| Token + TLS / TLS-CA / mTLS auth (mTLS optional, only if REST listener supports it) | |
| Shell completion (bash/zsh/fish/pwsh) | |
| `version`, `config *`, `completion`, `explain` (offline) | |

### Deliverables

- Binary: `dashctl` produced by `go build ./cmd/dashctl` on every supported OS/arch.
- `pkg/client.Client` interface + `pkg/client/rest` implementation backing it.
- Persistent contexts file at `$XDG_CONFIG_HOME/dashctl/config` (resp. `%APPDATA%\dashctl\config` on Windows).
- Embedded `FileDescriptorSet` for offline `dashctl explain`.
- 6+ container image (distroless multi-arch).
- Test pyramid as per [LLD § 15](../LLD/dashctl-lld.md#15-testing-strategy).

### Step-by-step tasks

| # | Module | Description | Status | Tests |
|---|---|---|---|---|
| 0 | `cmd/dashctl/main.go` + `internal/cmd/root.go` | Cobra root, persistent flags, version banner. Replace the scaffold. | ❌ | smoke |
| 1 | `internal/config/` | Config types, Load/Save, precedence resolver; XDG-aware paths. | ❌ | ≥ 90% (table tests) |
| 2 | `internal/cmd/config.go` | `dashctl config view/use-context/set-context/...`. | ❌ | each subcommand |
| 3 | `pkg/manifest/envelope.go` + `kinds.go` | Envelope ↔ `dashcenter.v1.*Spec` codec; kind registry; multi-doc YAML/JSON loader; stdin/dir/recursive. | ❌ | round-trip per kind |
| 4 | `pkg/client/client.go` + `types.go` | `Client` interface (Phase 1 method subset); `ClientConfig`; `Dial`. | ❌ | unit (mock backend) |
| 5 | `pkg/client/rest/` | REST backend: routes table, protojson body, header set, status-code → `CliError` mapping, retry policy for idempotent reads. | ❌ | httptest fixtures; full route table |
| 6 | `internal/errors/` | `CliError`, exit codes, classifier; full HTTP-status mapping. | ❌ | full table |
| 7 | `internal/render/` (json/yaml/name/jsonpath/template) | Generic renderers; jsonpath via `k8s.io/client-go`; templates via `text/template`. | ❌ | golden files |
| 8 | `internal/render/table.go` + `columns/*.go` | tabwriter-based table; per-kind column defs (Vnet, Eni, VnetMapping, AclPolicy, RoutePolicy, HaSet, Inventory, Dpu, Drift). | ❌ | golden files |
| 9 | `internal/cli/manifest.go` | `LoadFiles(args)` — file/dir/stdin walker; deterministic order. | ❌ | unit |
| 10 | `internal/cmd/apply.go` | Generic declarative apply path. | ❌ | unit + golden |
| 11 | `internal/cmd/get.go` | Generic read path; selector parsing; `-A`. | ❌ | unit + golden |
| 12 | `internal/cmd/delete.go` | Generic delete; `--ignore-not-found`; `--expected-generation`. | ❌ | unit + golden |
| 13 | `internal/cmd/describe.go` | Multi-section human render: spec + drift snapshot + placement. | ❌ | golden |
| 14 | `internal/cmd/edit.go` + `internal/cli/editor.go` | `$EDITOR` invoke; diff vs original; CAS on write. | ❌ | unit (fake editor) |
| 15 | `internal/cmd/replace.go` | Strict-CAS write. | ❌ | unit |
| 16 | `internal/cmd/diff.go` | Manifest vs server via `SimulateApply` (when available) or local compare. | ❌ | unit |
| 17 | `internal/cmd/explain.go` | Offline proto descriptor walker. | ❌ | unit |
| 18 | `internal/cmd/resource_typed.go` | Generate `vnet`, `eni`, … typed subcommand groups from the kind registry. | ❌ | golden |
| 19 | `internal/cmd/inventory.go` | `inventory put -f` / `inventory get`. | ❌ | unit + integration |
| 20 | `internal/cmd/reconcile.go` | `POST /v1/reconcile` (or admin endpoint). | ❌ | integration |
| 21 | `internal/cmd/dpu.go` | `dpu list` (admin snapshot); `dpu status` (polling); `dpu drift` (admin endpoint). | ❌ | integration |
| 22 | `internal/stream/` | Reconnect state machine, jittered backoff, signal handling. | ❌ | unit + integration |
| 23 | `internal/cmd/events.go` | SSE consumer (where dashd supports) or long-poll fallback; NDJSON / table. | ❌ | unit (fake SSE server) |
| 24 | `internal/cmd/version.go` | Client + server version; survives unreachable server. | ❌ | unit |
| 25 | `internal/cmd/completion.go` | Cobra-generated; per-flag custom completers (`--context`, `--namespace`, `<kind>`, `<name>`). | ❌ | smoke |
| 26 | `Dockerfile` | Distroless multi-stage; `linux/amd64` + `linux/arm64`. | ❌ | image build |
| 27 | `Makefile` + `scripts/build-dashctl.ps1` | Reproducible builds; release artifacts. | ❌ | CI |
| 28 | `test/integration/` (`//go:build integration`) | E2E suite against live dashd + dash-sim from `deploy/compose/`. | ❌ | 12 scenarios (table 1B-i) |

### Phase 1 quality gates

| # | Gate | Criterion | Status |
|---|---|---|---|
| C1-G1 | Build | `go build ./...` in `src/impl-go/dashctl` zero errors | ❌ |
| C1-G2 | Vet | `go vet ./...` zero warnings | ❌ |
| C1-G3 | Unit coverage | per-package floors: `config/` ≥ 90%, `manifest/` ≥ 90%, `render/` ≥ 90%, `errors/` ≥ 95%, `cli/` ≥ 85%, `pkg/client/rest` ≥ 85%, `internal/cmd/` ≥ 75% | ❌ |
| C1-G4 | Golden output | every kind × {json, yaml, table, wide, name} has a passing golden test | ❌ |
| C1-G5 | Cold-start | `dashctl version --client` ≤ 100 ms p99 on commodity laptop | ❌ |
| C1-G6 | Manifest round-trip | apply → get → diff = ∅ for every kind | ❌ |
| C1-G7 | CAS semantics | edit then modify externally then save → exit 4 with `FAILED_PRECONDITION` | ❌ |
| C1-G8 | Context isolation | `--context dev` and `--context prod` never bleed; concurrent invocations safe | ❌ |
| C1-G9 | Error mapping | every entry in [LLD § 10.3](../LLD/dashctl-lld.md#103-stable-mapping) has a test that exercises the path | ❌ |
| C1-G10 | Streaming Ctrl-C | `events --watch` cancels within 250 ms of SIGINT | ❌ |
| C1-G11 | Cross-platform build | matrix builds: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`. All produce a working `--help`. | ❌ |
| C1-G12 | Integration suite | 12/12 scenarios pass with `go test -tags=integration` | ❌ |

### Phase 1 integration scenarios (12)

| # | Test | What it verifies | Transport |
|---|---|---|---|
| 1 | `Test_Apply_Vnet_RoundTrip` | apply → get → values match | REST |
| 2 | `Test_Apply_MultiDoc` | multi-doc YAML applies all kinds in order | REST |
| 3 | `Test_Apply_DirRecursive` | `-R` walks subtree | REST |
| 4 | `Test_Get_LabelSelector` | `-l tier=prod` filters server-side | REST |
| 5 | `Test_Delete_IgnoreNotFound` | exit 0 when missing | REST |
| 6 | `Test_Edit_CAS_Mismatch` | second writer wins → first edit returns exit 4 | REST |
| 7 | `Test_Reconcile_All` | `dashctl reconcile` returns ack | REST |
| 8 | `Test_DpuList_Snapshot` | non-empty inventory from admin endpoint | Admin |
| 9 | `Test_DpuDrift_Empty_AfterConverge` | drift = [] after `apply` converges | Admin |
| 10 | `Test_Events_Watch_Cancel` | SIGINT → graceful exit 0 | SSE / long-poll |
| 11 | `Test_OutputGoldens_AllKinds` | every kind × {json,yaml,table,wide,name} matches golden | n/a |
| 12 | `Test_Version_ServerUnreachable` | client version printed, server section reports `unavailable`, exit 0 | n/a |

### Phase 1 files created (target tree)

See [LLD § 1 Repository layout](../LLD/dashctl-lld.md#1-repository-layout--module-boundaries).
Phase 1 ships everything in the tree **except** `pkg/client/grpc/`,
`internal/cmd/ha.go`, `internal/cmd/migration.go`, `internal/cmd/trace.go`,
and the Phase 2 `dpu cordon/uncordon/drain` subcommands.

---

## Phase 2 — gRPC backend ❌

### Objective

Add the **native gRPC backend** with full streaming and the Phase-2-only
verbs (Operations, HA, Migration, Diagnostics). After Phase 2, every
`dashcenter.v1` RPC is reachable from `dashctl`. The user picks transport
per context; both REST and gRPC remain first-class.

### Prerequisites

- ✅ dashctl Phase 1 complete (all 12 gates pass).
- dashd Phase 2 milestones unlock subcommands incrementally:
  - dashd PA (etcd, leader, namespace) → `dashctl` gains leader-aware error messages and `--all-namespaces` enforcement.
  - dashd PB (capacity, schema) → `apply --dry-run=server` returns capacity preview.
  - dashd PC (operations, HA, migration) → `dashctl dpu cordon/uncordon/drain`, `dashctl ha *`, `dashctl migration *` unlock.
  - dashd PD (auth, audit) → `dashctl logs` (audit stream), mTLS hardening.
  - dashd PE (diagnostics, gNMI) → `dashctl trace *`.

### Scope

| In scope | Out of scope |
|---|---|
| `pkg/client/grpc/` backend | gNMI client (post-Phase 2; out-of-band tool) |
| Native streaming: `events`, `dpu status`, `counters`, `logs`, `flows` | OIDC login flow |
| `ApplyBatch` client-streaming | Plugin system (`dashctl plugin`) |
| `dpu cordon / uncordon / drain` | Web Console integration |
| `ha set/scope get`, `ha switchover/failover`, `ha events`, `ha flow-sync-stats` | |
| `migration plan/start/advance/stream/rollback/abort/commit`, `migration bundle export/import` | |
| `trace flow / explain / acl-stats / drift-explain / resimulate` | |
| `--prune`, `--cascade`, `--parallel N` | |
| mTLS production hardening (cert rotation tested) | |

### Step-by-step tasks

| # | Module | Description | Status |
|---|---|---|---|
| 1 | `pkg/client/grpc/dial.go` | Dial, interceptors (timeout, retry idempotent only, trace), keepalive | ❌ |
| 2 | `pkg/client/grpc/grpc.go` | Implement every `Client` method using `dashcenterv1.*Client` stubs | ❌ |
| 3 | Backend selection | `transport: grpc` makes `Dial` route to gRPC; conflict-detect against http/https URLs | ❌ |
| 4 | `internal/stream/` (gRPC variants) | Concrete `Stream[T]` wrappers per RPC (`DpuStatusStream`, `EventStream`, `AuditStream`, `DrainStream`, `MigrationStream`, `HaEventStream`) | ❌ |
| 5 | `internal/cmd/apply.go` upgrade | `--batch` flag → `client.ApplyBatch` client-stream | ❌ |
| 6 | `internal/cmd/events.go` upgrade | Switch to native server-stream when `transport: grpc` | ❌ |
| 7 | `internal/cmd/dpu.go` upgrade | `status --watch` uses native stream; add `cordon/uncordon/drain` | ❌ |
| 8 | `internal/cmd/ha.go` (new) | All HA subcommands | ❌ |
| 9 | `internal/cmd/migration.go` (new) | All migration subcommands; byte-stream bundle export/import to file | ❌ |
| 10 | `internal/cmd/trace.go` (new) | All diagnostics subcommands | ❌ |
| 11 | `internal/cmd/logs.go` (new) | `GetAuditLog` stream | ❌ |
| 12 | mTLS test harness | TLS fixtures (CA, server cert, client cert); cert rotation test | ❌ |
| 13 | Performance harness | 24h `events --watch` no-leak test; `apply --batch` 1k specs latency | ❌ |
| 14 | Integration suite expansion | +10 scenarios for gRPC + Phase 2 verbs | ❌ |

### Phase 2 quality gates

| # | Gate | Criterion | Status |
|---|---|---|---|
| C2-G1 | gRPC build | `go build` with `pkg/client/grpc` zero errors | ❌ |
| C2-G2 | Transport parity | every `Client` method has identical behaviour on REST and gRPC for the kinds dashd supports on both (REST is the truth set; Phase-2-only RPCs are gRPC-only) | ❌ |
| C2-G3 | Native streaming | `events`, `dpu status`, `migration stream`, `ha events`, `dpu drain`, `logs` work end-to-end | ❌ |
| C2-G4 | `ApplyBatch` | client-stream of 100 mixed kinds commits atomically; rollback on injected failure | ❌ |
| C2-G5 | mTLS | mTLS handshake against dashd PD; bad client cert refused | ❌ |
| C2-G6 | Stream resilience | 3 forced disconnects in 30s → no event loss; stream summary line accurate | ❌ |
| C2-G7 | Migration end-to-end | 10-phase happy path completes; rollback from FLOW_DRAIN restores | ❌ |
| C2-G8 | HA end-to-end | switchover happy path between two dash-sims; failover refuses without `--confirm` | ❌ |
| C2-G9 | TraceFlow | deny verdict for known-deny ACL returns correct matched-rule path | ❌ |
| C2-G10 | Long-running RSS | `events --watch` 24h: < 5% RSS growth | ❌ |

### Phase 2 integration scenarios (additional 10)

| # | Test | Verifies |
|---|---|---|
| 13 | `Test_GRPC_PutVnet_Parity_REST` | same result over REST and gRPC |
| 14 | `Test_GRPC_Events_Stream` | native stream delivers events on PutVnet |
| 15 | `Test_GRPC_DpuStatus_Stream` | snapshot + deltas; reconnect resumes |
| 16 | `Test_GRPC_ApplyBatch_Atomic` | 1 failure in 50-item batch → 0 specs survive |
| 17 | `Test_DpuDrain_Stream_Stages` | PLANNING → MIGRATING → DRAINING → COMPLETE observed |
| 18 | `Test_Migration_HappyPath` | PLANNING → COMMITTED via `dashctl migration *` |
| 19 | `Test_Migration_Rollback` | rollback from FLOW_DRAIN, final state ROLLED_BACK |
| 20 | `Test_HA_Switchover` | `ha switchover` flips active role |
| 21 | `Test_TraceFlow_DenyVerdict` | matched-rule path correct |
| 22 | `Test_mTLS_Handshake` | valid cert accepted; rotated cert accepted; tampered rejected |

---

## Open items (cross-phase)

| # | Item | Notes |
|---|---|---|
| 1 | **`--prune` semantics** | Mirror kubectl-prune: delete server-side specs labelled with `--selector` absent from manifest. Phase 2. |
| 2 | **Plugin system** | Out of scope v1; possible future `dashctl plugin install <name>`. |
| 3 | **Bash/zsh completion polish** | Phase 1 includes Cobra-generated; per-flag dynamic completion (live `--namespace`, `<name>`) requires cache strategy. |
| 4 | **OIDC device-flow login** | Future `dashctl login`. Phase 2 if dashd PD lands an OIDC verifier; else post-Phase 2. |
| 5 | **Web Console parity check** | Once Console exists, validate Console + dashctl produce identical effects for the same intent. |

---

## Change log

| Date | Phase | Notes |
|---|---|---|
| 2026-06-09 | bootstrap | Initial tracker created. dashctl scaffold (today) is a single `main.go` printing version; this plan turns it into a kubectl-grade CLI. |
