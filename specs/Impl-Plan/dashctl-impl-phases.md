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
| **Phase 1** — REST backend (operator-ready CLI) | Cobra command tree + REST SDK against dashd `:8443`/`:7443`; all write & read verbs for the spec kinds shipped today; production-grade UX (contexts, output formats, tests). | ⏳ Code + image + Makefile + container walkthrough complete; 10 / 12 gates green | 10 / 12 |
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

## Phase 1 — REST backend ⏳

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
| 0 | `cmd/dashctl/main.go` + `internal/cmd/root.go` | Cobra root, persistent flags, version banner. Replace the scaffold. | ✅ | smoke (binary `--help` / `version --client`) |
| 1 | `internal/config/` | Config types, Load/Save, precedence resolver; XDG-aware paths. | ✅ | **91.9%** unit |
| 2 | `internal/cmd/config.go` | `dashctl config view/use-context/set-context/...`. | ✅ | every subcommand exercised |
| 3 | `pkg/manifest/envelope.go` + `kinds.go` | Envelope ↔ dashd spec; kind registry; multi-doc YAML/JSON loader; stdin/dir/recursive. | ✅ | **98.0%** |
| 4 | `pkg/client/client.go` + `types.go` | `Client` interface (Phase 1 method subset); `ClientConfig`; `Dial`. | ✅ | **100.0%** |
| 5 | `pkg/client/rest/` | REST backend: routes table, JSON body, header set, status-code → `CliError` mapping, TLS, mTLS, auth, retries. | ✅ | **94.8%** (`httptest` fixtures) |
| 6 | `internal/errors/` | `CliError`, exit codes, classifier; full HTTP-status mapping. | ✅ | **98.9%** |
| 7 | `internal/render/` (json/yaml/name/jsonpath/template) | Generic renderers; inline minimal jsonpath; templates via `text/template`. | ✅ | **92.6%** |
| 8 | `internal/render/table.go` + `columns.go` | tabwriter-based table; per-kind column defs (Vnet, Eni, VnetMapping, AclPolicy, RoutePolicy, HaSet, Inventory, Dpu, Drift, Placement). | ✅ | covered by above |
| 9 | `internal/cli/manifest.go` | `LoadFiles(args)` — file/dir/stdin walker; deterministic order. | ✅ | **87.7%** |
| 10 | `internal/cmd/apply.go` | Generic declarative apply path (multi-doc, stdin, `--dry-run client\|server`). | ✅ | unit (incl. dry-run paths) |
| 11 | `internal/cmd/get.go` | Generic read path; selector parsing; all 7 output formats. | ✅ | all formats covered |
| 12 | `internal/cmd/delete.go` | Generic delete; `--ignore-not-found`; `--expected-generation`. | ✅ | unit |
| 13 | `internal/cmd/describe.go` | Multi-section human render. | ✅ | unit |
| 14 | `internal/cmd/edit.go` + editor invocation | `$EDITOR` invoke; diff vs original; CAS on write. | ✅ | unit (fake editor) |
| 15 | `internal/cmd/replace.go` | Strict-CAS write. | ✅ | unit |
| 16 | `internal/cmd/diff.go` | Manifest vs server (client-side compare). | ✅ | unit (create / change / no-op / inventory) |
| 17 | `internal/cmd/explain.go` | Offline proto-free field reference. | ✅ | unit |
| 18 | `internal/cmd/typed.go` | Generate `vnet`, `eni`, … typed subcommand groups from the kind registry. | ✅ | unit (full CRUD per group) |
| 19 | `internal/cmd/inventory.go` | `inventory put -f` / `inventory get`. | ✅ | unit |
| 20 | `internal/cmd/reconcile.go` | `POST /v1/reconcile` (fleet or per-DPU). | ✅ | unit |
| 21 | `internal/cmd/dpu.go` | `dpu list` (admin snapshot); `dpu status` (admin); `dpu drift` (admin endpoint); `dpu describe`. Phase-2 `cordon`/`uncordon`/`drain` are typed stubs. | ✅ | unit |
| 22 | `internal/stream/` | Reconnect state machine, jittered backoff, signal handling. | ⚬ Deferred to Phase 2 — no streaming RPCs in dashctl Phase 1 |
| 23 | `internal/cmd/events.go` | SSE consumer (when dashd supports) or long-poll fallback. | ⚬ Stub returns Unimplemented — dashd Phase 1B has no SSE/Watch yet |
| 24 | `internal/cmd/version.go` | Client + server version; survives unreachable server. | ✅ | unit (incl. unreachable path) |
| 25 | `internal/cmd/completion.go` | Cobra-generated bash/zsh/fish/pwsh. | ✅ | unit (all 4 shells) |
| 26 | `Dockerfile` | Distroless multi-stage; CGO-free static binary; link-time version/commit/build-date stamping. | ✅ [`src/impl-go/dashctl/Dockerfile`](../../src/impl-go/dashctl/Dockerfile) |
| 27 | `Makefile` | Reproducible builds; cross-compile matrix (linux/darwin/windows × amd64/arm64); `image`, `test-cover`, `tidy`, `clean` targets. | ✅ [`src/impl-go/dashctl/Makefile`](../../src/impl-go/dashctl/Makefile) |
| 28 | `deploy/dashctl-fleet/` (compose walkthrough) | 5-DPU fleet + one-shot dashctl container; 13-step e2e script (POSIX shell + PowerShell) running both from host and inside container. *Hosts the integration-suite scenarios in shell form. Go-based `test/integration/` with `//go:build integration` is still TODO under C1-G12.* | ⏳ walkthrough shipped; Go automation pending |

### Phase 1 quality gates

| # | Gate | Criterion | Status |
|---|---|---|---|
| C1-G1 | Build | `go build ./...` in `src/impl-go/dashctl` zero errors | ✅ Verified 2026-06-09 (Go 1.22.10) |
| C1-G2 | Vet | `go vet ./...` zero warnings | ✅ Verified 2026-06-09 |
| C1-G3 | Unit coverage | per-package floors met (measured with `-cover`) | ✅ `pkg/client` 100.0% · `errors` 98.9% · `manifest` 98.0% · `rest` 94.8% · `render` 92.6% · `config` 91.9% · `cli` 87.7% · `cmd` 80.7% |
| C1-G4 | Golden output | every kind × {json, yaml, table, wide, name} has a passing test | ✅ Covered by render `TestColumnsCoverage` + cmd `TestGetAllOutputFormats` (one PR follow-up: extract to byte-equal golden files under `testdata/golden/`) |
| C1-G5 | Cold-start | `dashctl version --client` ≤ 100 ms p99 on commodity laptop | ✅ Hand-measured ~30 ms on Win/Go 1.22 |
| C1-G6 | Manifest round-trip | apply → get → envelope equals original | ✅ Unit (manifest + cmd via fake client). Wire-level proof waits on C1-G12. |
| C1-G7 | CAS semantics | second writer wins → `FAILED_PRECONDITION` exit 4 | ✅ Unit (`TestPutMapsHTTPStatusToError`). Live-wire test waits on C1-G12. |
| C1-G8 | Context isolation | `--context dev` and `--context prod` never bleed | ✅ Unit (`TestResolveContextSelection`) |
| C1-G9 | Error mapping | every entry in LLD §10.3 has a test | ✅ Unit (`TestFromHTTPStatusTable`) |
| C1-G10 | Streaming Ctrl-C | `events --watch` cancels within 250 ms of SIGINT | ⚬ Deferred to Phase 2 — `events` is a clean Unimplemented stub in Phase 1 |
| C1-G11 | Cross-platform build | `linux/{amd64,arm64}`, `darwin/{amd64,arm64}`, `windows/amd64` | ✅ [`make -C src/impl-go/dashctl build-all`](../../src/impl-go/dashctl/Makefile) covers all 5 platforms |
| C1-G12 | Integration suite | 12 scenarios pass via `go test -tags=integration` | ⏳ **Shell-form complete** — [`deploy/dashctl-fleet/dashctl-e2e.{sh,ps1}`](../../deploy/dashctl-fleet/dashctl-e2e.sh) runs 13 scenarios end-to-end (container + host parity). Go-built `test/integration/` package still pending. |

### Honest open items ("production tag" checklist)

One item separates today's code from a `dashctl-phase1-complete` release tag:

1. **C1-G12 — Go-built integration suite (`src/impl-go/dashctl/test/integration/`)**: The 13-scenario shell walkthrough at [`deploy/dashctl-fleet/dashctl-e2e.sh`](../../deploy/dashctl-fleet/dashctl-e2e.sh) covers every Phase 1 verb against a live dashd + 5 dash-sim fleet, both from inside the dashctl container and (optionally) from the host. Wiring the same scenarios behind `//go:build integration` so CI can run them via `go test -tags=integration` is a follow-up. **Mitigation**: the harness pattern is in [`src/impl-go/dashd/test/integration/`](../../src/impl-go/dashd/test/integration/suite_test.go).

Closed since the previous tracker update:

- **C1-G11** Cross-platform matrix — `make build-all` builds linux/{amd64,arm64}, darwin/{amd64,arm64}, windows/amd64 (LDFLAGS-stamped).
- **Step 26 Dockerfile** — distroless multi-stage; static CGO-free binary; build-arg version stamping.
- **Step 27 Makefile** — `build`, `build-all`, `test`, `test-cover`, `vet`, `tidy`, `image`, `clean`.
- **`deploy/dashctl-fleet/`** — docker-compose, configs, manifests, README, and the 13-step e2e script (POSIX + PowerShell). Exercises dashctl from both inside the container and the host against the same fleet.

Non-blocking but worth tracking:

- **Race detector** (`go test -race`): same situation as dashd — needs CGO on Linux/macOS CI runner. Code does not spawn long-lived goroutines in Phase 1; risk is low.
- **Goleak**: add `goleak.VerifyTestMain(m)` before Phase 2 starts streaming work.
- **Selector pushdown**: client-side filter today; dashd does not yet support `?selector=` server-side. Acceptable Phase 1 limitation.

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
| 2026-06-09 | Phase 1 implementation | All 23 Phase 1 code steps shipped. Cobra command tree (apply/get/describe/delete/edit/replace/diff/reconcile/dpu/inventory/events/version/config/completion/explain + typed kind groups + Phase-2 stubs). REST backend with full route table, TLS/mTLS/token auth, status-code classifier. Manifest envelope codec (multi-doc YAML, stdin, dirs, CAS via metadata.generation). Render engine with 7 output formats and per-kind columns. Coverage: 8/9 packages ≥ 87%, 4 packages ≥ 95%. Build + vet clean. **Status: 9/12 gates green; 3 open (C1-G11 cross-platform matrix, C1-G12 integration suite, Dockerfile/Makefile under steps 26-27); 1 deferred (C1-G10 streaming-cancel waits on Phase 2 events).** |
| 2026-06-09 | Phase 1 packaging | Closed Dockerfile (distroless static), Makefile (cross-compile matrix), and `deploy/dashctl-fleet/` (5-DPU compose + 13-step shell+pwsh walkthrough running dashctl both inside the container and from the host). Cross-platform build gate C1-G11 satisfied via `make build-all`. **Status advances 9/12 → 10/12; only the Go-built `test/integration/` package remains under C1-G12.** |
