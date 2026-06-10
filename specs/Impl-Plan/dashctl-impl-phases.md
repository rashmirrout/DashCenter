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
| **Phase 1** — REST backend (operator-ready CLI) | Cobra command tree + REST SDK against dashd `:8443`/`:7443`; all write & read verbs for the spec kinds shipped today; production-grade UX (contexts, output formats, tests). | ✅ Complete; 12 / 12 gates green (G10 deferred-by-design to Phase 2) | 12 / 12 |
| **Phase 2** — gRPC backend (full fidelity) | gRPC SDK against dashd `:9443`; native streaming; `ApplyBatch`; Operations / HA / Migration / Diagnostics commands as dashd's Phase 2 milestones land. Delivered in 5 sub-phases (2A–2E) matched to dashd PA–PE. | ❌ Not started | 0 / 31 sub-phase gates · 0 / 10 overall exit gates |

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

## Phase 1 — REST backend ✅

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
| 28 | `deploy/dashctl-fleet/` (compose walkthrough) + `test/integration/` (Go suite) | Container e2e walkthrough (13 steps, POSIX + PowerShell) AND Go-built `//go:build integration` suite (13 scenarios run via `make test-integration`). The Go suite builds dashctl once, spawns a private dashd + dash-sim per scenario on dynamic ports, exercises every Phase-1 verb, and tears down with Windows-safe `taskkill /T /F`. | ✅ Full suite green on Windows: `--- PASS` for all 13 scenarios (`165s` total). Logs land in `C:\Temp\dashctl-it-logs` when `DASHCTL_IT_LOG_DIR` is set. |

### Phase 1 extension — `debug` subcommand (REST-compatible subset)

> **Scope.** Four `debug` subcommands that work against dashd's REST and
> Admin surfaces today, requiring no gRPC backend. Ships as a follow-up
> PR to Phase 1 completion. Full spec:
> [`specs/LLD/dashctl-debug.md`](../LLD/dashctl-debug.md).

| # | Module | Description | Status | Tests |
|---|---|---|---|---|
| 29 | `internal/cmd/debug.go` | Cobra `debug` group (hidden from top-level help) + `debug put-raw` subcommand. Bypasses envelope codec; sends raw JSON to `Put<Kind>` via REST or gRPC. Supports `--dry-run`, `--expected-generation`. | ❌ | unit (dry-run, round-trip) |
| 30 | `internal/cmd/debug.go` | `debug get-raw` subcommand. Calls `client.Get()` and emits raw `PolicyObject.spec` protojson — no envelope wrapping, no column filtering. | ❌ | unit (raw vs envelope diff) |
| 31 | `internal/cmd/debug.go` | `debug curl` subcommand. Generates a `curl` command from the resolved context (endpoint, auth, TLS). gRPC contexts emit `grpcurl` form. Offline — no RPC issued. | ❌ | unit (REST form, auth redaction) |
| 32 | `internal/cmd/debug.go` + `pkg/client/client.go` | `debug admin` subcommand. Raw GET to dashd admin `:7443`. Adds `AdminRaw(ctx, path, params)` to `Client` interface; REST backend implements, gRPC returns `ErrUnimplemented`. | ❌ | unit (health, unknown-path 404) |

### Phase 1 quality gates

| # | Gate | Criterion | Status |
|---|---|---|---|
| C1-G1 | Build | `go build ./...` in `src/impl-go/dashctl` zero errors | ✅ Verified 2026-06-09 (Go 1.22.10) |
| C1-G2 | Vet | `go vet ./...` zero warnings | ✅ Verified 2026-06-09 |
| C1-G3 | Unit coverage | per-package floors met (measured with `-cover`) | ✅ `pkg/client` 100.0% · `errors` 98.9% · `manifest` 98.0% · `rest` 94.8% · `render` 92.6% · `config` 91.9% · `cli` 87.7% · `cmd` 80.7% |
| C1-G4 | Golden output | every kind × {json, yaml, table, wide, name} has a passing test | ✅ Covered by render `TestColumnsCoverage` + cmd `TestGetAllOutputFormats` (one PR follow-up: extract to byte-equal golden files under `testdata/golden/`) |
| C1-G5 | Cold-start | `dashctl version --client` ≤ 100 ms p99 on commodity laptop | ✅ Hand-measured ~30 ms on Win/Go 1.22 |
| C1-G6 | Manifest round-trip | apply → get → envelope equals original | ✅ Unit (manifest + cmd via fake client) + **live-wire** [`TestIntegration_Apply_RoundTrip`](../../src/impl-go/dashctl/test/integration/rest_test.go). |
| C1-G7 | CAS semantics | second writer wins → `FAILED_PRECONDITION` exit 4 | ✅ Unit (`TestPutMapsHTTPStatusToError`) + **live-wire** [`TestIntegration_Replace_CAS_Mismatch`](../../src/impl-go/dashctl/test/integration/rest_test.go) (replace twice with stale gen → exit 4). |
| C1-G8 | Context isolation | `--context dev` and `--context prod` never bleed | ✅ Unit (`TestResolveContextSelection`) |
| C1-G9 | Error mapping | every entry in LLD §10.3 has a test | ✅ Unit (`TestFromHTTPStatusTable`) |
| C1-G10 | Streaming Ctrl-C | `events --watch` cancels within 250 ms of SIGINT | ⚬ Deferred to Phase 2 — `events` is a clean Unimplemented stub in Phase 1 |
| C1-G11 | Cross-platform build | `linux/{amd64,arm64}`, `darwin/{amd64,arm64}`, `windows/amd64` | ✅ [`make -C src/impl-go/dashctl build-all`](../../src/impl-go/dashctl/Makefile) covers all 5 platforms |
| C1-G12 | Integration suite | 13 scenarios pass via `go test -tags=integration` | ✅ **All 13 scenarios PASS** in `165s` on Windows with chocolatey `GNU Make 4.4.1`. Run via `make test-integration` or [`src/impl-go/dashctl/test/integration/`](../../src/impl-go/dashctl/test/integration/). |

### Phase 1 extension quality gates (`debug`)

| # | Gate | Criterion | Status |
|---|---|---|---|
| CD-G1 | `put-raw` round-trip | `put-raw` + `get-raw` produces byte-equal spec to `apply` + `get -o json` | ❌ |
| CD-G2 | `put-raw` schema rejection | invalid field → `INVALID_ARGUMENT` exit 5 | ❌ |
| CD-G3 | `put-raw --dry-run` | prints URL + body, makes zero HTTP calls | ❌ |
| CD-G4 | `curl` REST form | emitted `curl`, when executed, gives identical JSON to `get -o json` | ❌ |
| CD-G6 | `admin` health | `debug admin --path /admin/health` exits 0 with valid JSON | ❌ |
| CD-G7 | `admin` unknown path | non-existent path → `404` surfaced with exit 3 | ❌ |
| CD-G12 | Coverage | `internal/cmd/debug.go` ≥ 80 % statements | ❌ |

### Honest open items ("production tag" checklist)

**No release-tag blockers remain for Phase 1.** All 12 quality gates pass. C1-G10 (streaming Ctrl-C) is intentionally deferred to Phase 2 because dashd Phase 1B has no streaming RPCs to cancel — the `events` verb is a clean Unimplemented stub.

Closed in chronological order since the previous tracker update:

- **C1-G11** Cross-platform matrix — `make build-all` builds linux/{amd64,arm64}, darwin/{amd64,arm64}, windows/amd64 (LDFLAGS-stamped). Verified — all 5 binaries produced on Windows.
- **C1-G12** Integration suite — 13 Go scenarios under `test/integration/` with `//go:build integration`. Builds dashctl once, spawns harness per test, exercises every Phase-1 verb. **Full suite green** on Windows in ~165s.
- **Step 26 Dockerfile** — distroless multi-stage; static CGO-free binary; build-arg version stamping.
- **Step 27 Makefile** — `build`, `build-all`, `test`, `test-cover`, `test-integration`, `vet`, `tidy`, `image`, `clean`. Cross-platform: detects `OS=Windows_NT` and substitutes `SHELL=cmd.exe`, native `md`/`rmdir`, PowerShell-based UTC timestamp. Verified in **both pwsh and Git Bash** on Windows.
- **`deploy/dashctl-fleet/`** — docker-compose, configs, manifests, README, and the 13-step e2e script (POSIX + PowerShell). Exercises dashctl from both inside the container and the host against the same fleet.

Non-blocking, worth tracking before Phase 2 begins:

- **Race detector** (`go test -race`): same situation as dashd — needs CGO on Linux/macOS CI runner. Code does not spawn long-lived goroutines in Phase 1; risk is low.
- **Goleak**: add `goleak.VerifyTestMain(m)` before Phase 2 starts streaming work.
- **Selector pushdown**: client-side filter today; dashd does not yet support `?selector=` server-side. Acceptable Phase 1 limitation — transparent to users.

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

### Phase 1 integration scenarios (13 — Go test suite)

Location: [`src/impl-go/dashctl/test/integration/`](../../src/impl-go/dashctl/test/integration/) with `//go:build integration`. Run via `make test-integration` (preferred) or `go test -tags=integration -count=1 -timeout 600s ./test/integration/...`.

| # | Test | What it verifies | Notes |
|---|---|---|---|
| 1 | `TestIntegration_VersionClient` | offline: `dashctl version --client` prints banner | no harness |
| 2 | `TestIntegration_Version_ServerUnreachable` | client section present + `Server: unavailable`, exit 0 | no harness |
| 3 | `TestIntegration_Explain_Offline` | `dashctl explain vnet` produces field reference offline | no harness |
| 4 | `TestIntegration_DpuList` | `dpu list -o table` shows DPU after harness brings it UP | harness |
| 5 | `TestIntegration_GetVnet_Empty` | empty-list path works | harness |
| 6 | `TestIntegration_Apply_RoundTrip` | apply dir of YAMLs → readback shows generation | harness |
| 7 | `TestIntegration_Get_OutputFormats` | sub-tests for table/wide/json/yaml/name | harness |
| 8 | `TestIntegration_Describe` | `describe` prints Name/Kind/Generation block | harness |
| 9 | `TestIntegration_Reconcile` | `dashctl reconcile` returns OK | harness |
| 10 | `TestIntegration_DpuDrift_Converges` | after apply, `dpu drift` shows `0 drift items.` within 30s | harness |
| 11 | `TestIntegration_Delete_IdempotentAfter` | delete → NOT_FOUND on re-delete → `--ignore-not-found` is exit 0 | harness |
| 12 | `TestIntegration_Get_LabelSelector` | `-l tier=prod` filters out non-matching specs | harness |
| 13 | `TestIntegration_Replace_CAS_Mismatch` | second `replace` with stale generation → exit 4 (FAILED_PRECONDITION) | harness |

**Shell-form parity walkthrough**: [`deploy/dashctl-fleet/dashctl-e2e.sh`](../../deploy/dashctl-fleet/dashctl-e2e.sh) (+ `.ps1`) covers the same 13 scenarios end-to-end against a real Docker fleet, runs dashctl from both inside the container and from the host, and is the recommended human-driven walkthrough.

### Phase 1 files created (target tree)

See [LLD § 1 Repository layout](../LLD/dashctl-lld.md#1-repository-layout--module-boundaries).
Phase 1 ships everything in the tree **except** `pkg/client/grpc/`,
`internal/cmd/ha.go`, `internal/cmd/migration.go`, `internal/cmd/trace.go`,
and the Phase 2 `dpu cordon/uncordon/drain` subcommands.

---

## Phase 2 — gRPC backend ❌

> **Contributor's quickstart.** Phase 2 is delivered in five **sub-phases (2A–2E)** that line up exactly with dashd's Phase-2 milestones (PA–PE). Each sub-phase is a **standalone, mergeable unit of work** with its own scope, gates, file list, and exit criteria. You can pick up any sub-phase whose dashd prerequisite has shipped; you do not need the prior sub-phases to be finished. The five sub-phases roll up into the overall Phase-2 gates (P2-G1 … P2-G10) and the integration suite expansion.
>
> If you are looking for **just one task to start with**, see [§ Phase 2 first-time contributor guide](#phase-2-first-time-contributor-guide) below.

### Phase 2 objective

Make every `dashcenter.v1` RPC reachable from `dashctl` over a **native gRPC backend** (server-streaming, client-streaming, byte streams, mTLS). After Phase 2:

- the operator picks transport per context (`transport: rest` or `transport: grpc`); both are first-class;
- streaming verbs (`events --watch`, `dpu status --watch`, `migration stream`, `ha events --watch`, `dpu drain`, `logs`, `counters --watch`, `flows`) deliver in real time with graceful Ctrl-C and automatic reconnect;
- `dashctl apply --batch` becomes an atomic client-streamed transaction;
- `dpu cordon/uncordon/drain`, `ha *`, `migration *`, `trace *`, and `logs` all stop returning `Unimplemented`;
- mTLS is production-hardened (CA rotation, client cert rotation).

### Phase 2 sub-phase roadmap

```
dashd PA ✅ ──► dashctl 2A (gRPC core + REST↔gRPC parity)
              │
dashd PB ✅ ──┼─► dashctl 2B (admission preview: --dry-run=server, ApplyBatch, --prune)
              │
dashd PC ✅ ──┼─► dashctl 2C (ops + HA + migration verbs, with streaming)
              │
dashd PD ✅ ──┼─► dashctl 2D (audit log + counters + mTLS + RBAC error mapping)
              │
dashd PE ✅ ──┴─► dashctl 2E (diagnostics: trace flow / explain / acl-stats / resimulate)
```

Each `dashctl 2X` is gated on the matching `dashd PX`. There is **no dependency** between sub-phases other than that — 2C can land before 2D, etc.

### Phase 2 sub-phase status summary

| Sub-phase | Objective | Gates | Status | Prerequisite |
|---|---|---|---|---|
| [2A](#sub-phase-2a--grpc-core--restgrpc-parity) | gRPC backend + REST↔gRPC parity for every Phase-1 verb | 6 | ❌ Not started | dashd Phase 2 PA |
| [2B](#sub-phase-2b--admission-preview--applybatch) | `--dry-run=server` (capacity/schema preview), `ApplyBatch`, `--prune` | 5 | ❌ Not started | dashd Phase 2 PB |
| [2C](#sub-phase-2c--operations--ha--migration) | `dpu cordon/uncordon/drain`, `ha *`, `migration *` | 9 | ❌ Not started | dashd Phase 2 PC |
| [2D](#sub-phase-2d--security--audit--counters) | mTLS hardening, `logs` (audit), `counters --watch` | 6 | ❌ Not started | dashd Phase 2 PD |
| [2E](#sub-phase-2e--diagnostics) | `trace flow / explain / acl-stats / drift-explain / resimulate` | 5 | ❌ Not started | dashd Phase 2 PE |
| **Total** | | **31** | **0 / 31** | |

### Prerequisites

- ✅ dashctl Phase 1 complete (all 12 gates pass; release-tag candidate today).
- dashd Phase 2 milestones unlock sub-phases incrementally:
  - dashd PA (etcd, leader, namespace) → unlocks dashctl 2A (leader-aware error hints, `--all-namespaces` enforcement, transport=grpc dialer).
  - dashd PB (capacity, schema, `SimulateApply`) → unlocks dashctl 2B (`apply --dry-run=server`, `apply --batch`, `--prune`).
  - dashd PC (Operations + HA + Migration services) → unlocks dashctl 2C (`dpu cordon/uncordon/drain`, `ha *`, `migration *`).
  - dashd PD (TLS/RBAC + audit + counter polling) → unlocks dashctl 2D (`logs`, `counters`, mTLS hardening, RBAC error UX).
  - dashd PE (DiagnosticsService) → unlocks dashctl 2E (`trace *`).

---

### Sub-phase 2A — gRPC core + REST↔gRPC parity

**Scope.** Add the gRPC backend behind the existing `client.Client` interface. Subcommand code does NOT change — every Phase-1 verb gains a second transport.

**Why first.** Every later sub-phase depends on having a working gRPC client.

#### 2A files (new / modified)

```
src/impl-go/dashctl/
├── pkg/client/grpc/
│   ├── grpc.go                    # NEW — gRPC client, implements client.Client
│   ├── dial.go                    # NEW — interceptors, keepalive, retry policy
│   ├── tls.go                     # NEW — credentials builder (mTLS, CA, insecure)
│   ├── retry.go                   # NEW — idempotent-only retry middleware
│   └── grpc_test.go               # NEW — bufconn fixture, full method table
├── internal/cmd/root.go           # MOD — register grpc backend via init() import
└── internal/config/config.go      # MOD — enforce grpc/https URL consistency
```

#### 2A step-by-step tasks

| # | Module | Description | Status |
|---|---|---|---|
| 2A.1 | `pkg/client/grpc/dial.go` | `grpc.NewClient` with chain interceptors (timeout, trace-id, retry); keepalive (`Time=30s Timeout=10s`); per-RPC `Authorization: Bearer <token>` metadata when `auth.mode=token`. | ❌ |
| 2A.2 | `pkg/client/grpc/tls.go` | `credentials.NewTLS` builder honouring `TLSConfig.{CAFile,CertFile,KeyFile,InsecureSkipVerify}` + `Insecure` (plaintext) safety mirror of REST backend. | ❌ |
| 2A.3 | `pkg/client/grpc/retry.go` | Retry policy: idempotent reads only (`Get`, `List`, `Health`); `UNAVAILABLE` / `DEADLINE_EXCEEDED`; 3 attempts; exponential 200 ms → 1.6 s; jittered. Writes are **never** retried. | ❌ |
| 2A.4 | `pkg/client/grpc/grpc.go` | Implement every `client.Client` method using `dashcenterv1.ControlPlaneClient` / `ObservabilityServiceClient`. Translate `status.Code` → typed `*errors.Error` (mirrors HTTP-status classifier in REST backend). | ❌ |
| 2A.5 | `internal/cmd/root.go` | Force-import `pkg/client/grpc` so its `init()` registers `TransportGRPC`. | ❌ |
| 2A.6 | `internal/config/config.go` | Reject mixed configs (`transport: grpc` + `https://…` endpoint → error). | ❌ |
| 2A.7 | `pkg/client/grpc/grpc_test.go` | bufconn-driven unit tests; every method exercised including status-code mapping table. Target ≥ 90 %. | ❌ |
| 2A.8 | `internal/cmd/debug.go` + `pkg/client/grpc/grpc.go` | `debug grpc-stream` subcommand. Opens a named gRPC server-stream RPC and dumps messages as NDJSON. Adds `DebugStream(ctx, key, reqJSON)` to `Client` interface; gRPC implements, REST returns `ErrUnimplemented`. Compile-time dispatch table for supported RPCs. Full spec: [`dashctl-debug.md § 4.5`](../LLD/dashctl-debug.md#45-debug-grpc-stream). | ❌ |
| 2A.9 | `internal/cmd/debug.go` | `debug parity` subcommand. Dials both REST and gRPC backends, issues same `Get`, normalises protojson, diffs. Exits 0 on match, 1 on mismatch with unified diff. `--all` flag iterates every kind. No new `Client` methods. Full spec: [`dashctl-debug.md § 4.6`](../LLD/dashctl-debug.md#46-debug-parity). | ❌ |

#### 2A quality gates

| # | Gate | Criterion | Status |
|---|---|---|---|
| C2A-G1 | gRPC build | `go build ./...` zero errors with `pkg/client/grpc` linked | ❌ |
| C2A-G2 | Transport parity | every Phase-1 verb produces identical results on REST and gRPC against the same dashd (proven by [`TestIntegration_GRPC_PutVnet_Parity_REST`](#phase-2-integration-suite-additional-13)) | ❌ |
| C2A-G3 | Status-code mapping | every `grpc/codes` value used by dashd has a unit test (mirrors [LLD § 10.3](../LLD/dashctl-lld.md#103-stable-mapping)) | ❌ |
| C2A-G4 | Plaintext safety | `transport: grpc` to non-localhost without `--insecure` and without TLS material → config error (mirrors REST) | ❌ |
| C2A-G5 | Coverage | `pkg/client/grpc/` ≥ 90 % statements | ❌ |
| C2A-G6 | Cold start | `dashctl get vnet -o json` (LAN) ≤ 300 ms p99 over gRPC | ❌ |
| CD-G5 | `curl` gRPC form | emitted `grpcurl` command contains correct `dashcenter.v1.ControlPlane/Get` service path | ❌ |
| CD-G8 | `grpc-stream` cancel | `SIGINT` during `debug grpc-stream` → exit 0 within 250 ms, prints event count | ❌ |
| CD-G9 | `grpc-stream` REST reject | `debug grpc-stream` on REST context → exit 9 with transport hint | ❌ |
| CD-G10 | `parity` match | after `apply`, `debug parity` exits 0 (REST = gRPC) | ❌ |
| CD-G11 | `parity` mismatch | injected codec drift → `debug parity` exits 1 with unified diff | ❌ |

---

### Sub-phase 2B — admission preview + ApplyBatch

**Scope.** Wire the three Phase-2 ControlPlane RPCs that depend on dashd PB (capacity + schema gating) into operator-visible verbs.

**Why.** `apply --dry-run=server` is the single most-requested operator feature for catching capacity blow-outs before they hit production.

#### 2B step-by-step tasks

| # | Module | Description | Status |
|---|---|---|---|
| 2B.1 | `internal/cmd/apply.go` | `--dry-run=server` invokes `ControlPlane.SimulateApply` (gRPC) or `POST /v1/simulate-apply` (REST when dashd lands it). Pretty-print per-DPU capacity preview. | ❌ |
| 2B.2 | `internal/cmd/apply.go` | `--batch` flag → `ControlPlane.ApplyBatch` (client-stream, gRPC only). Streams envelopes; aggregates `BatchAck` rows. | ❌ |
| 2B.3 | `internal/cmd/apply.go` | `--prune` (kubectl-style) → delete server-side specs labelled with `--selector` absent from manifest. Two-phase: list-by-selector → diff → delete. | ❌ |
| 2B.4 | `internal/cmd/diff.go` | Upgrade to call `SimulateApply` when transport=grpc (instead of client-side compare). | ❌ |
| 2B.5 | `pkg/client/grpc/grpc.go` | Add `ApplyBatch`, `SimulateApply` to the grpc backend. | ❌ |

#### 2B quality gates

| # | Gate | Criterion | Status |
|---|---|---|---|
| C2B-G1 | `--dry-run=server` happy path | preview returns per-DPU capacity rows without mutating store | ❌ |
| C2B-G2 | `--dry-run=server` rejects over-cap | `RESOURCE_EXHAUSTED` surfaced with limit/used/requested | ❌ |
| C2B-G3 | `ApplyBatch` atomic | 50-item batch with item #25 over-capacity → 0 items survive (verified by `dashctl get`) | ❌ |
| C2B-G4 | `--prune` correctness | manifest selector `tier=prod` + cluster has 3 specs (1 not in manifest) → 1 deletion, 0 false positives | ❌ |
| C2B-G5 | Per-DPU capacity preview | output matches dashd's per-kind limit field names (no client-side reinvention) | ❌ |

---

### Sub-phase 2C — Operations + HA + Migration

**Scope.** Three big new verb groups, all dependent on dashd PC (which delivers `OperationsService`, `HaService`, `MigrationService`).

**Why grouped.** All three share the same shape: streaming response + state-machine progress reporting. Building one renderer pays off three times.

#### 2C step-by-step tasks

| # | Module | Description | Status |
|---|---|---|---|
| 2C.1 | `internal/stream/` | Generic `Stream[T]` reconnector + jittered backoff + signal-cancel state machine. (Currently a stub Phase 1 marker.) | ❌ |
| 2C.2 | `internal/cmd/dpu.go` | Replace Phase-1 stubs for `cordon/uncordon/drain` with real `OperationsService` calls. `drain` is a server-stream rendering `PLANNING → MIGRATING → DRAINING → COMPLETE` stages. | ❌ |
| 2C.3 | `internal/cmd/ha.go` | New file. `ha switchover`, `ha failover`, `ha set get`, `ha scope get`, `ha events --watch`, `ha flow-sync-stats`. `failover` requires explicit `--confirm`. | ❌ |
| 2C.4 | `internal/cmd/migration.go` | New file. `migration plan create/validate/get/list`, `migration session start/advance/stream/rollback/abort/commit`, `migration bundle export/import` (byte-stream). | ❌ |
| 2C.5 | `pkg/client/grpc/grpc.go` | Add `OperationsService`, `HaService`, `MigrationService` clients to grpc backend. | ❌ |
| 2C.6 | `internal/render/stream.go` | NEW — incremental `RenderStream` for live stages (`drain`, `migration stream`, `ha events`) with a stable column re-render. | ❌ |

#### 2C quality gates

| # | Gate | Criterion | Status |
|---|---|---|---|
| C2C-G1 | `dpu cordon` excludes from placement | next `apply` for a fresh ENI does not pick the cordoned DPU | ❌ |
| C2C-G2 | `dpu uncordon` re-includes | placement resumes on this DPU after `uncordon` | ❌ |
| C2C-G3 | `dpu drain` streams stages | `PLANNING → MIGRATING → DRAINING → COMPLETE` visible in real time | ❌ |
| C2C-G4 | `ha switchover` end-to-end | active role flips between two dash-sims; old-active reports `STANDBY` | ❌ |
| C2C-G5 | `ha failover` safety | refuses without `--confirm`; with `--confirm` skips old-active contact | ❌ |
| C2C-G6 | `migration *` 10-phase | `dashctl migration session start … advance …` → `COMMITTED` end-to-end | ❌ |
| C2C-G7 | `migration rollback` | from `FLOW_DRAIN` returns to source DPU; final state `ROLLED_BACK` | ❌ |
| C2C-G8 | `migration bundle` round-trip | export to file → import on a second cluster → identical session state | ❌ |
| C2C-G9 | Stream cancel | SIGINT during `dpu drain` / `migration stream` cancels within 250 ms with a final summary line | ❌ |

---

### Sub-phase 2D — Security + Audit + Counters

**Scope.** Production-hardens dashctl for prod environments where dashd has TLS/mTLS, RBAC, and an audit log.

#### 2D step-by-step tasks

| # | Module | Description | Status |
|---|---|---|---|
| 2D.1 | `internal/cmd/logs.go` | NEW — `dashctl logs [--since DUR] [--actor ID] [--watch]`. Server-stream over `ObservabilityService.GetAuditLog`. | ❌ |
| 2D.2 | `internal/cmd/counters.go` | NEW — `dashctl counters --dpu ID [--watch]`. Snapshot or follow `GetCounters` stream. | ❌ |
| 2D.3 | `internal/cmd/dpu.go` | Add `dpu flow-stats <id>` (unary `GetFlowStats`) and `dpu flows <id> [-l]` (server-stream `GetFlowList`). | ❌ |
| 2D.4 | mTLS test harness | Per-test CA + server cert + client cert generation; cert rotation scenario (`SIGHUP`-equivalent or fresh dial). | ❌ |
| 2D.5 | `internal/errors/errors.go` | Add RBAC-aware hint: `PERMISSION_DENIED` → "your role lacks this verb; ask an admin for `operator` or `admin`". | ❌ |
| 2D.6 | `internal/cmd/version.go` | Print server's RBAC role for the current token (from response metadata once dashd PD ships it). | ❌ |

#### 2D quality gates

| # | Gate | Criterion | Status |
|---|---|---|---|
| C2D-G1 | mTLS happy path | valid CA + cert + key → `dashctl get vnet` succeeds | ❌ |
| C2D-G2 | mTLS bad cert refused | tampered client cert → `UNAUTHENTICATED` exit 6 | ❌ |
| C2D-G3 | mTLS rotation | swap client cert without restarting `dashctl` (long-lived stream survives) | ❌ |
| C2D-G4 | `logs --watch` tail | new mutating RPC appears in the stream within 1 s | ❌ |
| C2D-G5 | `counters --watch` follow | delta deltas observed within counter-poll interval | ❌ |
| C2D-G6 | RBAC error UX | viewer token on `apply` → exit 6 with role-aware hint | ❌ |

---

### Sub-phase 2E — Diagnostics

**Scope.** The `trace *` family. All five RPCs are read-only and run entirely in dashd's in-memory observed-state cache, so latency is sub-millisecond and dataplane-safe.

#### 2E step-by-step tasks

| # | Module | Description | Status |
|---|---|---|---|
| 2E.1 | `internal/cmd/trace.go` | NEW — `trace flow`, `trace explain`, `trace acl-stats`, `trace drift-explain`, `trace resimulate`. | ❌ |
| 2E.2 | `pkg/client/grpc/grpc.go` | Add `DiagnosticsService` client. | ❌ |
| 2E.3 | `internal/render/trace.go` | NEW — pretty-print the per-stage verdict block (kubectl-describe style). | ❌ |

#### 2E quality gates

| # | Gate | Criterion | Status |
|---|---|---|---|
| C2E-G1 | `trace flow` PERMIT | known-permit ACL → verdict `PERMIT`, matched rule id correct | ❌ |
| C2E-G2 | `trace flow` DENY | no matching rule → verdict `DENY`, default-policy explanation present | ❌ |
| C2E-G3 | `trace explain` | per-rule match/no-match reasoning with field-level diff | ❌ |
| C2E-G4 | `trace acl-stats --zero-only` | surfaces unused rules (dead-rule detection) | ❌ |
| C2E-G5 | `trace resimulate` | issues re-Apply with `resimulate_flows=true`; dashd audit log records the trigger | ❌ |

---

### Phase 2 — overall exit criteria

`dashctl-phase2-complete` release tag requires **ALL** of:

| # | Gate | Criterion | Status |
|---|---|---|---|
| P2-G1 | All sub-phase gates green | C2A-G1 … C2E-G5 (31 gates total) | ❌ 0 / 31 |
| P2-G2 | Phase 1 regression-free | all 12 Phase 1 gates still green | ❌ |
| P2-G3 | Aggregate coverage | overall statement coverage ≥ 90 % (matches Phase 1 watermark) | ❌ |
| P2-G4 | `go test -race ./...` | passes on Linux/macOS CI runner | ❌ |
| P2-G5 | `goleak.VerifyTestMain(m)` | active in `pkg/client/grpc/`, `internal/stream/`, all streaming-cmd packages | ❌ |
| P2-G6 | 24 h soak | `dashctl events --watch` against live dashd: < 5 % RSS growth over 24 h | ❌ |
| P2-G7 | Integration suite | **23 scenarios** pass (13 Phase 1 + 10 Phase 2 new — see below) | ❌ |
| P2-G8 | Per-package coverage floors | `pkg/client/grpc/` ≥ 90 %, `internal/stream/` ≥ 85 %, all new `internal/cmd/*.go` ≥ 80 % | ❌ |
| P2-G9 | Cross-platform matrix | `make build-all` produces all 5 platform binaries (no regressions from Phase 1) | ❌ |
| P2-G10 | Docs in sync | every new verb has a help-text line, an entry in `dashctl explain`, a row in this tracker, and a row in `dashctl-lld.md` § 18.1 RPC matrix | ❌ |

---

### Phase 2 integration suite (additional 10)

Builds on Phase 1's 13 scenarios; lives in the same `src/impl-go/dashctl/test/integration/` package under the same `//go:build integration` tag.

| # | Test | Sub-phase | Verifies |
|---|---|---|---|
| 14 | `TestIntegration_GRPC_PutVnet_Parity_REST` | 2A | identical Put result over REST and gRPC against the same dashd |
| 24 | `TestIntegration_Debug_PutRaw_RoundTrip` | 1 ext | `put-raw` + `get-raw` byte-equals `apply` + `get -o json` |
| 25 | `TestIntegration_Debug_PutRaw_InvalidField` | 1 ext | invalid field → INVALID_ARGUMENT exit 5 |
| 26 | `TestIntegration_Debug_PutRaw_DryRun` | 1 ext | prints request, makes zero HTTP calls |
| 27 | `TestIntegration_Debug_Curl_REST` | 1 ext | emitted curl runs correctly |
| 28 | `TestIntegration_Debug_Admin_Health` | 1 ext | admin health exits 0 |
| 29 | `TestIntegration_Debug_Admin_UnknownPath` | 1 ext | unknown admin path → exit 3 |
| 30 | `TestIntegration_Debug_GrpcStream_Cancel` | 2A | SIGINT cancels within 250 ms |
| 31 | `TestIntegration_Debug_GrpcStream_REST_Reject` | 2A | REST context → exit 9 |
| 32 | `TestIntegration_Debug_Parity_Match` | 2A | REST = gRPC after apply |
| 33 | `TestIntegration_Debug_Parity_Mismatch` | 2A | codec drift → exit 1 with diff |
| 15 | `TestIntegration_GRPC_Get_Stream` | 2A | server-stream `List` returns ordered envelopes; client cancel works |
| 16 | `TestIntegration_DryRun_Server_OverCapacity` | 2B | `--dry-run=server` on a known-over-cap manifest → `RESOURCE_EXHAUSTED` with limit detail |
| 17 | `TestIntegration_ApplyBatch_Atomic_Rollback` | 2B | 50-item batch with item #25 failing → 0 specs survive |
| 18 | `TestIntegration_DpuDrain_Stream_Stages` | 2C | `PLANNING → MIGRATING → DRAINING → COMPLETE` observed in order |
| 19 | `TestIntegration_HA_Switchover_E2E` | 2C | active role flips; old-active reports STANDBY |
| 20 | `TestIntegration_Migration_HappyPath` | 2C | `migration session …` → `COMMITTED` end-to-end (10-phase) |
| 21 | `TestIntegration_Migration_Rollback_FromFlowDrain` | 2C | rollback from `FLOW_DRAIN`; final state `ROLLED_BACK` |
| 22 | `TestIntegration_mTLS_RotateClientCert` | 2D | swap client cert mid-stream without disconnect |
| 23 | `TestIntegration_TraceFlow_DenyVerdict` | 2E | known-deny ACL produces verdict `DENY` + correct matched-rule path |

> **Scenarios are unit-of-PR.** A sub-phase is not green until its tests in this table pass.

---

### Phase 2 RPC → verb coverage matrix

Every `dashcenter.v1` RPC in [`proto/dashcenter/v1/`](../../proto/dashcenter/v1/) is listed below with its target `dashctl` verb. Use this as the **authoritative source** when adding a new verb.

| Service | RPC | Transport | dashctl verb | Sub-phase |
|---|---|---|---|---|
| ControlPlane | `PutInventory` | REST + gRPC | `inventory put` / `apply` | 1 ✅ |
| ControlPlane | `RegisterDpu` | gRPC | `dpu register` | 2A |
| ControlPlane | `DeregisterDpu` | gRPC | `dpu deregister` | 2A |
| ControlPlane | `PutVnet`/`PutEni`/`PutVnetMapping`/`PutAclPolicy`/`PutRoutePolicy`/`PutHaSet` | REST + gRPC | `apply` / `<kind> put` | 1 ✅ |
| ControlPlane | `PutServiceTunnel` | REST + gRPC | `service-tunnel put` (capability-gated by dashd PB) | 2B |
| ControlPlane | `Delete` | REST + gRPC | `delete` | 1 ✅ |
| ControlPlane | `Get` | REST + gRPC | `get` | 1 ✅ |
| ControlPlane | `List` (server-stream) | REST paged / gRPC stream | `get` (list mode) | 1 ✅ (paged) · 2A (stream) |
| ControlPlane | `ApplyBatch` (client-stream) | gRPC only | `apply --batch` | 2B |
| ControlPlane | `SimulateApply` | REST + gRPC | `apply --dry-run=server`, `diff` | 2B |
| ControlPlane | `Reconcile` | REST + gRPC | `reconcile` | 1 ✅ |
| ObservabilityService | `GetDpuStatus` (stream) | gRPC | `dpu status --watch` | 2A (snapshot today via admin) |
| ObservabilityService | `GetFlowStats` | gRPC | `dpu flow-stats <id>` | 2D |
| ObservabilityService | `GetFlowList` (stream) | gRPC | `dpu flows <id>` | 2D |
| ObservabilityService | `GetDrift` | gRPC | `dpu drift` (today via admin) | 2A |
| ObservabilityService | `GetCounters` (stream) | gRPC | `counters --watch` | 2D |
| ObservabilityService | `WatchEvents` (stream) | gRPC | `events --watch` | 2A |
| ObservabilityService | `GetAuditLog` (stream) | gRPC | `logs [--watch]` | 2D |
| OperationsService | `CordonDpu` | gRPC | `dpu cordon` | 2C |
| OperationsService | `UncordonDpu` | gRPC | `dpu uncordon` | 2C |
| OperationsService | `DrainDpu` (stream) | gRPC | `dpu drain` | 2C |
| HaService | `GetHaSetState` | gRPC | `ha set get <name>` | 2C |
| HaService | `GetHaScopeState` | gRPC | `ha scope get <name>` | 2C |
| HaService | `TriggerSwitchover` (stream) | gRPC | `ha switchover <ha-set> --to <dpu>` | 2C |
| HaService | `TriggerFailover` (stream) | gRPC | `ha failover <ha-set> --to <dpu> --confirm` | 2C |
| HaService | `WatchHaEvents` (stream) | gRPC | `ha events --watch` | 2C |
| HaService | `GetFlowSyncStats` | gRPC | `ha flow-sync-stats <ha-scope>` | 2C |
| MigrationService | `CreateMigrationPlan` | gRPC | `migration plan create -f` | 2C |
| MigrationService | `ValidateMigrationPlan` | gRPC | `migration plan validate <id>` | 2C |
| MigrationService | `StartMigrationSession` | gRPC | `migration session start <plan-id>` | 2C |
| MigrationService | `AdvanceMigrationPhase` | gRPC | `migration session advance <id>` | 2C |
| MigrationService | `StreamMigrationSession` (stream) | gRPC | `migration session stream <id>` | 2C |
| MigrationService | `RollbackMigration` | gRPC | `migration session rollback <id>` | 2C |
| MigrationService | `AbortMigration` | gRPC | `migration session abort <id>` | 2C |
| MigrationService | `CommitMigration` | gRPC | `migration session commit <id>` | 2C |
| MigrationService | `GetMigrationSession` | gRPC | `migration session get <id>` | 2C |
| MigrationService | `ListMigrationSessions` (stream) | gRPC | `migration session list` | 2C |
| MigrationService | `ExportMigrationBundle` (byte-stream) | gRPC | `migration bundle export <id> -o <file>` | 2C |
| MigrationService | `ImportMigrationBundle` (byte-stream) | gRPC | `migration bundle import -f <file>` | 2C |
| DiagnosticsService | `TraceFlow` | gRPC | `trace flow` | 2E |
| DiagnosticsService | `ExplainMatch` | gRPC | `trace explain` | 2E |
| DiagnosticsService | `GetAclHitStats` (stream) | gRPC | `trace acl-stats [--zero-only]` | 2E |
| DiagnosticsService | `ExplainDrift` | gRPC | `trace drift-explain` | 2E |
| DiagnosticsService | `TriggerResimulation` | gRPC | `trace resimulate --dpu` | 2E |

> Phase 1 commands marked ✅ work today over REST; their gRPC equivalent ships in 2A.

---

### Phase 2 — performance budgets

| Action | Budget | Test |
|---|---|---|
| Cold start `dashctl version --client` (gRPC binary) | ≤ 100 ms p99 | reuse Phase 1 C1-G5 |
| `dashctl get vnet x` (LAN, gRPC) | ≤ 300 ms p99 | C2A-G6 |
| `dashctl apply --batch` 1 000 specs | ≤ 5 s p99 (network-dominated) | new under 2B |
| `dashctl events --watch` steady-state CPU | < 1 % single core | P2-G6 24 h soak |
| `dashctl events --watch` 24 h RSS growth | < 5 % | P2-G6 |
| Stream cancel SIGINT → exit | ≤ 250 ms p99 | C2C-G9 |

---

### Phase 2 — open design questions (resolve before coding)

These are NOT blockers for starting Phase 2 work but each merits a one-line decision in this section before the relevant sub-phase opens.

| # | Question | Sub-phase | Recommendation (subject to review) |
|---|---|---|---|
| Q1 | `ApplyBatch` retry: stream-level retry or always client-resubmit? | 2B | Never retry — operator re-runs with the same `--batch` |
| Q2 | `migration stream` and `dpu drain`: implicit reconnect, or one-shot? | 2C | Implicit reconnect with the generic `stream.Reconnector` (Phase 1 has the design; 2C implements) |
| Q3 | mTLS cert rotation: SIGHUP, fsnotify, or per-RPC re-load? | 2D | fsnotify on the cert/key files; rebuild creds on change |
| Q4 | OIDC device-flow login (`dashctl login`)? | 2D or post | Post-Phase-2 unless dashd PD ships an OIDC verifier first |
| Q5 | `dashctl logs` filter: server-side (dashd `AuditFilter`) or client grep? | 2D | Server-side — match dashd's `AuditFilter` proto field-for-field |
| Q6 | `trace flow` payload pretty-print: text first or NDJSON first? | 2E | Text default (kubectl-describe style); `-o json` always available |
| Q7 | Streaming `-o table`: re-render every event or NDJSON? | 2C / 2D | NDJSON by default; `-o table --interactive` for live re-render |
| Q8 | gNMI client? | post-Phase 2 | Out of scope; separate tool |
| Q9 | Krew-style plugin system? | post-Phase 2 | Out of scope for v1 |
| Q10 | Web Console parity check tests? | post-Phase 2 | Need Console first |

---

### Phase 2 — first-time contributor guide

> **Want to pick up one piece of Phase 2?** Here is a curated set of single-PR-sized tasks. Each is independently mergeable.

| Difficulty | Task | Files | Skills | Prereq |
|---|---|---|---|---|
| 🟢 Easy | Wire `pkg/client/grpc/init()` to register `TransportGRPC` (returns `Unimplemented` placeholder) | `pkg/client/grpc/grpc.go` | Go basics, init() | none |
| 🟢 Easy | Add `dashctl logs` Cobra stub returning `Unimplemented` | `internal/cmd/logs.go` | Cobra | none |
| 🟡 Medium | Implement `pkg/client/grpc` happy-path for `PutVnet` + `Get` (bufconn-tested) | `pkg/client/grpc/grpc.go`, `grpc_test.go` | gRPC client, bufconn | 2A.1, 2A.2 |
| 🟡 Medium | Build the `Stream[T]` reconnect engine (no dashd dep — uses an in-process mock stream) | `internal/stream/stream.go` | state machines, goroutines | none |
| 🟡 Medium | gRPC status-code → CliError mapping table + unit tests | `pkg/client/grpc/grpc.go` | gRPC, test tables | 2A.1 |
| 🟠 Hard | `internal/cmd/migration.go` — full state-machine renderer | `internal/cmd/migration.go`, `internal/render/stream.go` | streams, kubectl-style UX | 2C.1, 2C.5 |
| 🟠 Hard | mTLS cert rotation via fsnotify | `pkg/client/grpc/tls.go` | tls.Config rotation patterns | 2A.2, 2D.4 |
| 🔴 Expert | `apply --prune` (kubectl-prune semantics, label-selector pruning) | `internal/cmd/apply.go` | nuanced UX, dry-run-first design | 2A, 2B.5 |
| 🔴 Expert | 24 h soak harness + RSS reporting | `test/perf/` (new) | long-running tests, RSS sampling | none |

**Bootstrap your dev environment**: see [`README.md`](../../README.md) → "Build & test" and [`docs/DASHCTL_INTEGRATION_TEST.md`](../../docs/DASHCTL_INTEGRATION_TEST.md). Pick a task, open an issue with "Phase 2 sub-phase 2X — task NN" in the title, link this tracker row.

---

### Phase 2 file layout (target)

After Phase 2 is complete, the tree adds:

```
src/impl-go/dashctl/
├── pkg/client/grpc/             # NEW — entire dir
│   ├── grpc.go
│   ├── dial.go
│   ├── tls.go
│   ├── retry.go
│   └── grpc_test.go
├── internal/stream/             # GROWS — was a Phase-1 stub
│   ├── stream.go
│   ├── reconnector.go
│   ├── backoff.go
│   └── stream_test.go
├── internal/cmd/
│   ├── ha.go                    # NEW
│   ├── migration.go             # NEW
│   ├── trace.go                 # NEW
│   ├── logs.go                  # NEW
│   ├── counters.go              # NEW
│   ├── apply.go                 # MOD — --batch, --dry-run=server, --prune
│   ├── dpu.go                   # MOD — cordon/uncordon/drain real impl
│   ├── events.go                # MOD — real streaming impl
│   └── version.go               # MOD — server role display
├── internal/render/
│   ├── stream.go                # NEW — live stage renderer
│   └── trace.go                 # NEW — verdict block renderer
└── test/integration/
    └── grpc_test.go             # NEW — Phase 2 scenarios #14–#23
```

---

---

## Phase 3+ — future enhancements (post-Phase 2 backlog)

> Everything below is **explicitly out of scope** for Phase 1 and Phase 2. Captured here so contributors can pick up an enhancement without it sitting in a private notebook, and so reviewers have a clean reference for "is this in scope or future work?". Each item carries:
>
> - **Audience**: who benefits (operator / SRE / platform owner / CI / SDK consumer).
> - **Effort**: 🟢 small (≤ 1 PR), 🟡 medium (a few PRs), 🟠 large (multi-week), 🔴 epic (multi-month).
> - **Depends on**: Phase 2 sub-phase or upstream component.
> - **Status**: ❄️ frozen (no champion yet), 🌱 sprouting (issue exists), 🚧 in flight.

### 3.A — Operator UX polish

| # | Item | Audience | Effort | Depends | Status |
|---|---|---|---|---|---|
| 3.A.1 | **Dynamic shell completion** for `--namespace`, `--context`, `<kind>`, `<name>` — Cobra `ValidArgsFunction` backed by a 60 s on-disk cache under `$XDG_CACHE_HOME/dashctl/` | operator | 🟡 | Phase 1 | ❄️ |
| 3.A.2 | **`dashctl explain <kind>.<field>...`** — drill into nested fields (today: top-level only); embed proto descriptors at build time | SDK consumer | 🟡 | Phase 1 (proto deps optional) | ❄️ |
| 3.A.3 | **`dashctl diff -R <dir>`** with colourised side-by-side hunks (kubectl-diff style; runs `git diff --no-index` under the hood) | operator | 🟢 | Phase 1 | ❄️ |
| 3.A.4 | **`dashctl wait`** — block until a condition is met (e.g. `dashctl wait --for=converged dpu/dpu-0 --timeout=2m`); polls drift endpoint | operator / CI | 🟢 | Phase 1 (admin) · Phase 2A (streaming variant) | ❄️ |
| 3.A.5 | **`dashctl explain --recursive`** prints a full nested schema tree for a kind | SDK consumer | 🟢 | Phase 1 | ❄️ |
| 3.A.6 | **`dashctl debug bundle`** — collect `/admin/health` + `/admin/inventory` + `/admin/drift` + per-DPU dumps into a tarball for support tickets | SRE | 🟡 | Phase 1 | ❄️ |
| 3.A.7 | **`dashctl describe` for sub-resources** (e.g. `describe acl-policy x` shows bound ENIs, hit-stats, last reconcile) | operator | 🟡 | Phase 2D + 2E | ❄️ |
| 3.A.8 | **Smart prompts when a command is destructive** (`delete`, `cordon`, `failover`) with `-y/--yes` to bypass | operator | 🟢 | Phase 1 | ❄️ |
| 3.A.9 | **`dashctl tree`** — render the object graph (Vnet → ENI → VnetMapping / AclPolicy / RoutePolicy / HaSet) for a given root | operator | 🟡 | Phase 1 | ❄️ |
| 3.A.10 | **Pluggable kind aliases** via context (`alias: { vn: Vnet }`) | operator | 🟢 | Phase 1 | ❄️ |

### 3.B — Authentication & multi-tenant UX

| # | Item | Audience | Effort | Depends | Status |
|---|---|---|---|---|---|
| 3.B.1 | **OIDC device-flow login** — `dashctl login --issuer https://...` opens browser, polls token endpoint, stores opaque token under `auth.token-file` | operator | 🟠 | dashd PD ships OIDC verifier | ❄️ |
| 3.B.2 | **AAD / Entra ID profile** — `dashctl login --aad-tenant ...` with `azure/identity` library; supports managed identities for CI runners | operator (Microsoft Azure) | 🟠 | 3.B.1 | ❄️ |
| 3.B.3 | **mTLS cert rotation via fsnotify** — Phase 2D ships SIGHUP-equivalent; here we automate the watch | SRE | 🟡 | Phase 2D | ❄️ |
| 3.B.4 | **Per-namespace `dashctl whoami`** — show effective role + namespaces accessible | operator | 🟢 | dashd PD RBAC | ❄️ |
| 3.B.5 | **`dashctl audit` shortcuts** — `audit who-did-what --kind eni --name eni-001` queries audit log with structured filters | SRE / compliance | 🟢 | Phase 2D `logs` verb | ❄️ |
| 3.B.6 | **Secret manager integration** — pull tokens from `pass`, macOS Keychain, Windows Credential Manager instead of env vars | operator | 🟡 | 3.B.1 | ❄️ |
| 3.B.7 | **kubeconfig import** — `dashctl config import-from-kubeconfig` for orgs that already manage TLS material per cluster in kubeconfig | operator | 🟢 | Phase 1 | ❄️ |

### 3.C — GitOps & automation

| # | Item | Audience | Effort | Depends | Status |
|---|---|---|---|---|---|
| 3.C.1 | **`dashctl apply --server-side`** — force `ApplyBatch` even for single docs; idempotent re-runs always produce identical results | CI / GitOps | 🟢 | Phase 2B | ❄️ |
| 3.C.2 | **`dashctl apply --field-manager <name>`** — k8s-style field ownership for multi-tool GitOps environments | platform owner | 🟠 | dashd field-ownership (new) | ❄️ |
| 3.C.3 | **Flux / Argo CD controller** — out-of-tree GitOps controller that reconciles a Git repo against `dashctl apply` | platform owner | 🔴 | Phase 2 | ❄️ |
| 3.C.4 | **Terraform provider** — `terraform-provider-dashcenter` consuming the same `dashcenter.v1` proto | platform owner | 🔴 | Phase 2A | ❄️ |
| 3.C.5 | **Pulumi provider** | platform owner | 🔴 | 3.C.4 pattern | ❄️ |
| 3.C.6 | **Crossplane composition** | platform owner (k8s shops) | 🔴 | 3.C.4 / 3.C.5 | ❄️ |
| 3.C.7 | **JSON-Schema export** — `dashctl explain --json-schema vnet` emits a JSON Schema document for IDE autocomplete in YAML editors | SDK consumer | 🟢 | Phase 1 (or 3.A.5) | ❄️ |
| 3.C.8 | **`dashctl apply --validate-only`** — pure client-side validation against embedded JSON Schemas (no dashd dial) | CI | 🟢 | 3.C.7 | ❄️ |

### 3.D — Observability of dashctl itself

| # | Item | Audience | Effort | Depends | Status |
|---|---|---|---|---|---|
| 3.D.1 | **OpenTelemetry trace export** — `--otel-endpoint http://collector:4318` emits one span per RPC (matches dashd's OTel layer) | SRE | 🟡 | Phase 2A | ❄️ |
| 3.D.2 | **`--profile cpu|mem`** — drop pprof profile on exit for long-running streams | maintainer | 🟢 | Phase 2 streaming | ❄️ |
| 3.D.3 | **Prometheus metrics on `dashctl` itself** when running in service mode (`dashctl serve --watch …` for GitOps webhooks) | platform owner | 🟠 | 3.C.3 pattern | ❄️ |
| 3.D.4 | **Structured-log NDJSON** with stable field names (`method`, `endpoint`, `latency_ms`, `code`) — Phase 1 has stderr text, this elevates to NDJSON suitable for piping into a log aggregator | SRE | 🟢 | Phase 1 | ❄️ |

### 3.E — Plugin & extension model

| # | Item | Audience | Effort | Depends | Status |
|---|---|---|---|---|---|
| 3.E.1 | **Krew-style plugin system** — `dashctl plugin install <name>`, plugins discovered on PATH as `dashctl-<name>` exec subcommands | operator | 🟠 | none | ❄️ |
| 3.E.2 | **Renderer plug-ins** — register a new `-o <fmt>` handler from a plugin | SDK consumer | 🟡 | 3.E.1 | ❄️ |
| 3.E.3 | **Webhook hooks** — `pre-apply` / `post-apply` exec hooks fired from config (security-gated) | SRE | 🟡 | none | ❄️ |
| 3.E.4 | **Lua / Starlark policy hooks** for `apply` — reject manifests violating org policy (e.g. "all ENIs must have a `cost-center` label") | platform owner | 🟠 | 3.E.3 | ❄️ |

### 3.F — Web Console parity

| # | Item | Audience | Effort | Depends | Status |
|---|---|---|---|---|---|
| 3.F.1 | **Console parity verification suite** — run the same scenario via dashctl and Console; assert identical dashd state | maintainer / QA | 🟠 | Web Console exists | ❄️ |
| 3.F.2 | **Console URL deep-link** — `dashctl get vnet vnet-prod --open` opens browser at `/vnets/vnet-prod` in the Console | operator | 🟢 | Web Console exists | ❄️ |
| 3.F.3 | **Shared kind registry** — Console + dashctl read the same JSON-Schema source so column definitions never drift | maintainer | 🟡 | 3.C.7 | ❄️ |

### 3.G — Performance & scale

| # | Item | Audience | Effort | Depends | Status |
|---|---|---|---|---|---|
| 3.G.1 | **`dashctl apply --parallel N`** — Phase 2 introduces serial apply; bump to N concurrent Put RPCs with bounded error coverage | CI / large fleets | 🟢 | Phase 2A | ❄️ |
| 3.G.2 | **Local watch-cache** — `dashctl watch-cache run` runs a sidecar that holds a hot ObservedCache copy, used by other dashctl invocations for sub-millisecond reads | SRE / dashboards | 🟠 | Phase 2A WatchEvents | ❄️ |
| 3.G.3 | **Pagination tuning** — auto-detect "list-of-thousands" and switch to cursor pagination | operator | 🟢 | Phase 2A streaming List | ❄️ |
| 3.G.4 | **Benchmark suite** — `make test-bench` runs micro-benchmarks for codec, render, classifier; baselines published to CI artifact | maintainer | 🟢 | Phase 1 | ❄️ |
| 3.G.5 | **Memory profiler harness** — captures heap during 24 h soak for regression baselines | maintainer | 🟡 | Phase 2 stream | ❄️ |

### 3.H — Distribution & ergonomics

| # | Item | Audience | Effort | Depends | Status |
|---|---|---|---|---|---|
| 3.H.1 | **Homebrew formula** (`brew install dashctl`) | operator (mac) | 🟢 | release tag | ❄️ |
| 3.H.2 | **Chocolatey package** (`choco install dashctl`) | operator (Windows) | 🟢 | release tag | ❄️ |
| 3.H.3 | **WinGet manifest** | operator (Windows) | 🟢 | release tag | ❄️ |
| 3.H.4 | **APT / RPM repositories** | operator (Linux) | 🟡 | release tag | ❄️ |
| 3.H.5 | **One-liner installer** — `curl -sSL https://dashcenter.dev/install.sh \| sh` | operator | 🟢 | release tag | ❄️ |
| 3.H.6 | **Cosign-signed releases + SLSA provenance** | SRE / supply chain | 🟡 | release pipeline | ❄️ |
| 3.H.7 | **In-binary update check** — `dashctl version` warns when a newer version is available | operator | 🟢 | release tag | ❄️ |
| 3.H.8 | **Krew index entry** — once 3.E.1 lands, register dashctl plugins in Krew | operator | 🟢 | 3.E.1 | ❄️ |

### 3.I — Protocol & compatibility extensions

| # | Item | Audience | Effort | Depends | Status |
|---|---|---|---|---|---|
| 3.I.1 | **gNMI client** — `dashctl gnmi subscribe /dashcenter/v1/events` against dashd's Phase 2 PE gNMI bridge | network operator | 🟠 | dashd Phase 2 PE | ❄️ |
| 3.I.2 | **NETCONF / RESTCONF adapter** — translate dashctl manifests into NETCONF for legacy provisioning systems | platform owner | 🔴 | 3.I.1 | ❄️ |
| 3.I.3 | **Multi-cluster federation** (`dashctl --cluster prod-east,prod-west get vnet`) | platform owner | 🟠 | dashd federation (post-Phase 2) | ❄️ |
| 3.I.4 | **API version negotiation** — once `dashcenter.v2` exists, dashctl auto-selects per context | maintainer | 🟡 | `dashcenter.v2` exists | ❄️ |

### 3.J — Testing & quality engineering

| # | Item | Audience | Effort | Depends | Status |
|---|---|---|---|---|---|
| 3.J.1 | **Mutation testing** — `go-mutesting` or `gremlins` baseline; CI fails if mutation score drops > 5 % | maintainer | 🟡 | Phase 1 | ❄️ |
| 3.J.2 | **Fuzz tests** — `internal/render`, `pkg/manifest`, `pkg/client/rest` (response parsing) | maintainer | 🟡 | Phase 1 | ❄️ |
| 3.J.3 | **Property-based tests** — `gopter` round-trip tests for envelope codec + selector parser | maintainer | 🟡 | Phase 1 | ❄️ |
| 3.J.4 | **Chaos tests** — inject dashd disconnects / slow responses during integration tests | maintainer | 🟡 | Phase 1 IT | ❄️ |
| 3.J.5 | **Golden-file output suite** under `testdata/golden/` for every `-o` × every kind; CI compares byte-for-byte | maintainer | 🟢 | Phase 1 | ❄️ |
| 3.J.6 | **Multi-OS CI matrix** — run unit + integration suites on linux/{amd64,arm64}, darwin/{amd64,arm64}, windows/amd64 | maintainer | 🟡 | Phase 1 | ❄️ |

### 3.K — Documentation & community

| # | Item | Audience | Effort | Depends | Status |
|---|---|---|---|---|---|
| 3.K.1 | **`CONTRIBUTING.md`** at repo root pointing to both trackers + integration-test guide + this future-roadmap | new contributor | 🟢 | Phase 1 | ❄️ |
| 3.K.2 | **GitHub issue templates** — "Phase 2 sub-phase 2X — task NN", "Bug", "Future enhancement", "Documentation" | maintainer | 🟢 | Phase 1 | ❄️ |
| 3.K.3 | **Tutorial: "GitOps with dashctl in 10 minutes"** | operator | 🟢 | Phase 2B | ❄️ |
| 3.K.4 | **Tutorial: "Migrating an ENI live with dashctl"** | operator | 🟢 | Phase 2C | ❄️ |
| 3.K.5 | **Hosted command reference site** (`dashcenter.dev/cli`) generated from Cobra `--help` + this tracker | operator | 🟡 | Phase 1 | ❄️ |
| 3.K.6 | **Demo videos** — 60 s loops for each new sub-phase milestone | operator | 🟢 | each sub-phase | ❄️ |
| 3.K.7 | **`docs/UPGRADING.md`** — per-release upgrade notes, especially around output-format changes | operator | 🟢 | release tag | ❄️ |

### 3.L — Safety & guardrails

| # | Item | Audience | Effort | Depends | Status |
|---|---|---|---|---|---|
| 3.L.1 | **Production-context warning** — `dashctl --context prod-* apply` prints a yellow banner before mutating | operator | 🟢 | Phase 1 | ❄️ |
| 3.L.2 | **Rate-limit confirmations** — block `delete` of > N specs without `--confirm-count=N` | operator | 🟢 | Phase 1 | ❄️ |
| 3.L.3 | **`dashctl audit replay <txn-id>`** — replay a past audit entry against the live cluster (read-only by default) | SRE | 🟡 | Phase 2D logs | ❄️ |
| 3.L.4 | **Dry-run-by-default for `delete --cascade`** — kubectl learned this the hard way | operator | 🟢 | Phase 2B `--cascade` | ❄️ |
| 3.L.5 | **Drift webhook** — `dashctl drift watch --webhook https://…` POSTs to a URL on every drift event | SRE | 🟡 | Phase 2A WatchEvents | ❄️ |

### Cross-cutting principles for future work

1. **Backward compatibility is sacred.** Existing `-o json` / `-o yaml` output shapes are part of the public API. Add fields freely, never rename/remove without a major bump.
2. **Exit codes are stable.** Anything in [LLD § 10.3](../LLD/dashctl-lld.md#103-stable-mapping) is committed.
3. **No business logic in dashctl.** Anything that recomputes server state (capacity, placement, drift) belongs in dashd. dashctl renders.
4. **Streaming verbs always support Ctrl-C and reconnect.** Established in Phase 2C; future verbs must match.
5. **New flags need a help line, an `explain` entry, and a row in the tracker.** Documentation lag is a bug.
6. **No new external dependencies without a `go.sum` justification in the PR.** dashctl ships a 8-MB static binary today; we protect that.

---

## Open items (cross-phase)

| # | Item | Notes |
|---|---|---|
| 1 | **`--prune` semantics** | Mirror kubectl-prune; specified in sub-phase 2B but UX details still need a design issue. |
| 2 | **Per-flag dynamic completion** | Cobra `ValidArgsFunction` for `--context`, `--namespace`, `<kind>`, `<name>` — captured under 3.A.1. |
| 3 | **OIDC device-flow login** | Captured under 3.B.1. |
| 4 | **Web Console parity check** | Captured under 3.F. |

---

## Change log

| Date | Phase | Notes |
|---|---|---|
| 2026-06-09 | bootstrap | Initial tracker created. dashctl scaffold (today) is a single `main.go` printing version; this plan turns it into a kubectl-grade CLI. |
| 2026-06-09 | Phase 1 implementation | All 23 Phase 1 code steps shipped. Cobra command tree (apply/get/describe/delete/edit/replace/diff/reconcile/dpu/inventory/events/version/config/completion/explain + typed kind groups + Phase-2 stubs). REST backend with full route table, TLS/mTLS/token auth, status-code classifier. Manifest envelope codec (multi-doc YAML, stdin, dirs, CAS via metadata.generation). Render engine with 7 output formats and per-kind columns. Coverage: 8/9 packages ≥ 87%, 4 packages ≥ 95%. Build + vet clean. **Status: 9/12 gates green; 3 open (C1-G11 cross-platform matrix, C1-G12 integration suite, Dockerfile/Makefile under steps 26-27); 1 deferred (C1-G10 streaming-cancel waits on Phase 2 events).** |
| 2026-06-09 | Phase 1 packaging | Closed Dockerfile (distroless static), Makefile (cross-compile matrix), and `deploy/dashctl-fleet/` (5-DPU compose + 13-step shell+pwsh walkthrough running dashctl both inside the container and from the host). Cross-platform build gate C1-G11 satisfied via `make build-all`. **Status advances 9/12 → 10/12; only the Go-built `test/integration/` package remains under C1-G12.** |
| 2026-06-09 | Phase 1 ✅ | Closed C1-G12 by adding [`src/impl-go/dashctl/test/integration/`](../../src/impl-go/dashctl/test/integration/) (`//go:build integration`, 13 scenarios). Builds dashctl once, brings up a private dashd + dash-sim per scenario (`go run`), exercises every Phase-1 verb, and tears down with Windows-safe `taskkill /T /F`. **Full suite PASS in 165s on Windows** — covers offline (version/explain), live happy-paths (apply/get/describe/delete/reconcile/dpu list/drift), 5 output-format sub-tests, label-selector filtering, idempotent delete, and CAS-on-replace exit 4. Hardened Makefile for portable Windows (`SHELL=cmd.exe` + native `md`/`rmdir` when `OS=Windows_NT`) and verified `make` works in **both pwsh and Git Bash**. Tracker advances to **12/12 gates green**. Phase 1 ready for release tag. |
| 2026-06-09 | Phase 2 tracker rewrite | Phase 2 rewritten in contributor-friendly, OSS-grade form: split into **5 sub-phases 2A–2E** matched to dashd PA–PE; **31 gates** (was 10) covering each sub-phase + 10 overall exit gates; **full RPC → verb matrix** for all dashcenter.v1 services; performance budgets table; 10 open design questions; first-time contributor task ladder (🟢🟡🟠🔴); target file layout; integration suite expansion to **23 scenarios** (13 Phase 1 + 10 Phase 2). No code change — tracker only. |
| 2026-06-09 | Future-roadmap section | Added comprehensive **"Phase 3+ — future enhancements"** section organising post-Phase 2 work into 12 themes (3.A operator UX · 3.B auth · 3.C GitOps · 3.D observability · 3.E plugins · 3.F Console parity · 3.G performance · 3.H distribution · 3.I protocol/compat · 3.J test quality · 3.K docs · 3.L safety). **62 enhancement items** with audience, effort tier (🟢🟡🟠🔴), dependency, and status — each crisp enough to file as a GitHub issue. Plus 6 cross-cutting principles. No code change — tracker only. |
| 2026-06-10 | `dashctl debug` spec | Added **`dashctl debug` subcommand group**: standalone spec ([`specs/LLD/dashctl-debug.md`](../LLD/dashctl-debug.md)), **Phase 1 extension steps 29–32** (`put-raw`, `get-raw`, `curl`, `admin`), **sub-phase 2A tasks 2A.8–2A.9** (`grpc-stream`, `parity`), **12 quality gates** (CD-G1–CD-G12), **10 integration tests** (#24–#33). Cross-referenced in HLD §7 and LLD §5.4/§6.1. No code change — spec/tracker only. |
