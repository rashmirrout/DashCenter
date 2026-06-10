# Contributing to DashCenter

> One-page reference for everyone opening a PR against this repo. Pairs
> with the per-component design specs under [`specs/`](../specs/) and the
> phase tracker at
> [`specs/Impl-Plan/impl-phases.md`](../specs/Impl-Plan/impl-phases.md).
>
> If you're adding a tutorial page, see also
> [`docs/tutorial/CONTRIBUTING-TO-TUTORIAL.md`](tutorial/CONTRIBUTING-TO-TUTORIAL.md).

## 1. Quick start

```powershell
# build
cd src/impl-go/dashd && go build ./...

# unit tests
go test -count=1 ./...

# unit tests with coverage
go test -count=1 -cover ./...

# integration suite (spawns dashd + dash-sim per test)
go test -tags=integration -count=1 -timeout 600s ./test/integration/...

# vet + race (race needs CGO on Linux/macOS CI)
go vet ./...
```

Repeat for `src/impl-go/dashctl/`. The whole pyramid must be green before
PR open.

## 2. Per-PR checklist

The reviewer reads this list top-to-bottom. Self-check before requesting
review.

```text
[ ] Branch builds:                go build ./...
[ ] Vet clean:                    go vet ./...
[ ] Unit tests pass:              go test -count=1 ./...
[ ] Touched-package coverage:     >= existing (>= 85% if new)
[ ] Race detector (Linux CI):     go test -race ./<touched-pkg>/
[ ] Live fleet regression:        docker compose -f deploy/dashctl-fleet/...
                                  apply manifests + drift = 0 on all DPUs
[ ] Tracker updated:              specs/Impl-Plan/impl-phases.md row(s)
                                  match the PR's status
[ ] Auth contract (AC-1..AC-10):  see § 3
[ ] Controllerless contract (MC-1..MC-5): see § 4
[ ] If new tutorial page:         see tutorial/CONTRIBUTING-TO-TUTORIAL.md
```

If three or more items fail, the PR is changes-requested as a block. Fix
all of them in one push.

## 3. Auth forward-compatibility contract (AC-1..AC-10)

Authentication itself ships in **PD**. To guarantee PD lands without
rework, every PR in PA / PB / PC / PE / PF must satisfy ALL ten rules
below. Frozen knob design is in
[`specs/Impl-Plan/impl-phases.md`](../specs/Impl-Plan/impl-phases.md)
§ Configuration & forward-compatibility contract.

| # | Rule | One-line summary |
|---|---|---|
| **AC-1** | RPC handlers take `ctx context.Context` first | actor flows via `ctx`; never look up actor from globals |
| **AC-2** | New RPC registered in `internal/auth/roles.go` | one-line `auth.DefaultRoleMap.Register(...)` in the package's `init()` |
| **AC-3** | Listeners go through `internal/auth/listener.go` | call `auth.NewListener(...)` — never `net.Listen` directly |
| **AC-4** | New gRPC server registrations use the shared interceptor chain | in `internal/server/grpc/server.go` — no ad-hoc `grpc.NewServer(...)` |
| **AC-5** | New REST handlers use the shared middleware chain | in `internal/server/rest/server.go` — no raw `http.HandlerFunc` registrations |
| **AC-6** | No plaintext credentials in env/yaml that PD couldn't override later | placeholder values + documented eventual secrets path |
| **AC-7** | Integration tests run under `auth.mode: none` (the default) | don't depend on a special "unauthenticated" mode |
| **AC-8** | Mutating actions log via `slog` with `{actor, namespace, kind, name, op, result}` | PD-late audit writer just copies these into JSONL |
| **AC-9** | `internal/auth/` package skeleton stays current | PRs add one-line `roles.go` entries for their new RPCs |
| **AC-10** | This page is the published checklist | new contributors find it on day one |

### How to add a Phase-2 RPC and satisfy the contract

```go
// internal/server/grpc/<service>.go

// AC-1: ctx is the first parameter.
// AC-4: handler registered via the shared chain in server.go (not here).
func (h *opsHandler) TriggerSwitchover(ctx context.Context, req *pb.SwitchoverRequest) (*pb.Ack, error) {
    // AC-8: stable slog fields.
    slog.Info("ops.switchover",
        "actor", auth.FromContext(ctx).Name,
        "namespace", req.GetNamespace(),
        "kind", "ha_set",
        "name", req.GetHaSetName(),
        "op", "switchover",
        "result", "started",
    )
    // ... handler body ...
}

// AC-2: register the RPC in roles.go via an init() in this file.
// AC-9: keep the role map current.
func init() {
    auth.DefaultRoleMap.Register(auth.RPCInfo{
        Method:       "/dashcenter.v1.HaService/TriggerSwitchover",
        Access:       auth.AccessWrite, // MC-3: declare read-vs-write
        AllowedRoles: []string{auth.RoleAdmin},
    })
}
```

That's it. Until PD ships, `auth.FromContext(ctx).Name` returns
`"anonymous"` and `auth.DefaultRoleMap.Allow` returns `true` for every
caller — your code is correct under both PA-1 (today) and PD (future)
behaviour.

## 4. Controllerless forward-compatibility contract (MC-1..MC-5)

Controllerless mode (gossip + raft + proxy embedded on every DPU) ships
in **PF**. Every PR until then satisfies these rules so PF lands without
a controller-vs-controllerless refactor.

| # | Rule | One-line summary |
|---|---|---|
| **MC-1** | No direct `etcd.Client` calls outside `internal/store/etcd/` and `internal/ha/leader/etcd.go` | use `store.DesiredStore` and `leader.Elector` interfaces |
| **MC-2** | No single-writer assumptions outside leader-only goroutines | reconciler / dispatch / subscribe are leader-only; REST / gRPC / admin can run on followers |
| **MC-3** | Every RPC declares its read-vs-write nature in `roles.go` (`auth.AccessRead` / `auth.AccessWrite`) | PF-4 proxy needs this metadata to forward writes to the leader |
| **MC-4** | Process-local state on disk goes only under `<state_dir>/` | controllerless raft replicates all durable state through the FSM |
| **MC-5** | State-mutating long-lived goroutines start inside `leaderLoop` (from PA-0) | pure-read goroutines run unconditionally; `RaftElector` reuses the same loop |

## 5. Code style

- Go 1.22+. `gofmt -s` + `goimports` clean. No `// nolint` without a one-line justification.
- Public APIs documented with full sentences. Private helpers may be terse if names are self-explanatory.
- One package per `internal/<dir>/`. No god-packages.
- Errors: wrap with `%w`; never lose the underlying cause.
- Logging: `log/slog` only; stable field set per AC-8.
- Concurrency: prefer channels over mutexes for coordination, mutexes for state. Every goroutine has a documented exit condition.

## 6. Commit + PR hygiene

- One logical change per PR. Refactors land separately from feature work.
- Commit messages: imperative present tense; first line ≤ 72 chars; body wraps at 80.
- PR title: `<area>: <imperative summary>` — e.g. `dashd/store: add etcd backend (PA-1b)`.
- Link the tracker row in the PR description.
- Squash on merge unless the history is genuinely informative.

## 7. Where to ask for help

- Design discussions: PR comments + the matching spec under `specs/`.
- Architectural questions: open an issue tagged `design`.
- Operational questions about the dev fleet: `docs/explore-with-docker/manual-handson.md`.

Thanks for contributing.
