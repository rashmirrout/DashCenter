# DashCenter — Known Bugs

> **Status:** Draft v1 (2026-06-17)
> **Purpose:** Tracked bugs and defects found via code review of dashd, dashctl,
> dashw, and dash-sim. Each entry includes severity, file/line evidence,
> reproduction notes, and a suggested fix.
> **Companion:** [`FEATURE_ASK.md`](./FEATURE_ASK.md) captures feature requests
> and logging/observability enhancements (`L1`–`L10`).

---

## How to read each bug

- **ID** — stable identifier (`B<N>`).
- **Severity** — HIGH (correctness/safety), MEDIUM (latent race/regression risk), LOW (cosmetic/operability).
- **Component** — sub-project and module owning the defect.
- **Evidence** — file path and line numbers.
- **Symptom** — what an operator or test observes.
- **Root cause** — what the code actually does versus what it claims.
- **Impact** — operational/security/correctness consequences.
- **Suggested fix** — concrete change.
- **Validation** — how a fix is verified.

---

## B1. DPU quarantine is dead code; log line falsely claims quarantine happened

- **Severity:** HIGH
- **Component:** dashd / `internal/dispatch`
- **Evidence:** [`src/impl-go/dashd/internal/dispatch/worker.go`](../../src/impl-go/dashd/internal/dispatch/worker.go) lines 213–220.
- **Symptom:** When a DPU's per-worker error counter exceeds the configured budget, dashd logs `dispatch: error budget exceeded — DPU quarantined` at ERROR but the DPU continues to be reconciled and serves traffic.
- **Root cause:**
  ```go
  func (w *worker) recordError() {
      n := atomic.AddInt32(&w.errCount, 1)
      if int(n) > w.budget && w.inv != nil {
          // Quarantine the DPU.
          slog.Error("dispatch: error budget exceeded — DPU quarantined", "dpu", w.id, "errors", n)
      }
  }
  ```
  The comment promises a quarantine but no method on `w.inv` is invoked, and `grep` for `Quarantine` across [`internal/inventory/`](../../src/impl-go/dashd/internal/inventory/) returns zero matches.
- **Impact:** Per-DPU isolation invariant from [`specs/HLD/dashd-hld.md`](../../specs/HLD/dashd-hld.md) §2 ("one bad DPU never stalls the fleet") is not enforced. A pathological DPU will keep failing Apply/Delete RPCs and never be removed from the reconciliation pool. Operators relying on the log message will believe an isolation action took place that did not.
- **Suggested fix:**
  1. Add an `Quarantine(dpuID string)` method to the inventory interface and a `QUARANTINED` state in the DPU model.
  2. Have `recordError` call `w.inv.Quarantine(w.id)` and stop the worker run loop.
  3. Expose the quarantined state via admin/health and dashw UI.
  4. Surface a `dpu_quarantine_total` Prometheus counter.
  5. Provide `dashctl dpu uncordon-quarantine <dpu>` (or similar) for manual recovery after remediation.
- **Validation:**
  - Unit test that simulates `budget+1` consecutive failures and asserts the inventory state transitions to `QUARANTINED` and the run loop exits.
  - Integration test against `dash-sim` that injects faults until quarantine triggers and confirms the rest of the fleet keeps converging.

---

## B2. Unsynchronized `w.client` field in dispatch worker

- **Severity:** MEDIUM
- **Component:** dashd / `internal/dispatch`
- **Evidence:** [`src/impl-go/dashd/internal/dispatch/worker.go`](../../src/impl-go/dashd/internal/dispatch/worker.go) lines 48–50, 158–169, 176–178.
- **Symptom:** None observed today (no documented multi-goroutine reproducer). Latent race risk surfaced by static read of the code.
- **Root cause:** `ensureClient`, `doApply`, `doDelete`, and `invalidateClient` all read and mutate `w.client` without any synchronization. No code comment asserts single-goroutine ownership.
- **Impact:** If any future change (probe loop, cordon path, periodic refresh) calls `invalidateClient()` from a goroutine different from the dispatch run loop, the result is a data race on a non-atomic pointer and possible nil dereference inside `doApply`/`doDelete`.
- **Suggested fix (pick one):**
  1. Add `mu sync.Mutex` to the worker struct and guard every read/write of `w.client`.
  2. Or formally restrict all `w.client` access to a single goroutine: add a code comment, assert the invariant in tests, and route invalidation requests through a channel handled in the run loop.
- **Validation:**
  - Add a `go test -race` scenario that calls `invalidateClient()` concurrently with `doApply()`/`doDelete()`.
  - Add a comment + lint to enforce the chosen invariant.

---

## B3. Version banner uses `fmt.Println` instead of structured logging

- **Severity:** LOW
- **Component:** dashd / `cmd/dashd`
- **Evidence:** [`src/impl-go/dashd/cmd/dashd/main.go`](../../src/impl-go/dashd/cmd/dashd/main.go) line 80.
- **Symptom:** When operators run `dashd --version` the output is plain text (`dashd 2.0.0-rc1`), which is correct for that case. However the same `fmt.Println` pattern is also present at boot time, polluting structured-log pipelines that expect every line to be JSON or slog text.
- **Root cause:** Single remaining `fmt.Println` in production code paths breaks the "all log output goes through `slog`" invariant.
- **Impact:** Log shippers that filter by handler format may drop or misclassify the banner. Inconsistency hurts log auditability.
- **Suggested fix:**
  - Keep `fmt.Println` only inside the `--version` short-circuit path that exits before any logger is initialized.
  - For boot-time banners (build info, version, mode), use `slog.Info("dashd starting", "version", version, "commit", commit, "build_date", buildDate)` consistently.
- **Validation:**
  - Add a smoke test that runs `dashd` with the JSON handler and asserts every emitted line parses as JSON.

---

## B4. "auth disabled" warning is too easy to lose

- **Severity:** LOW
- **Component:** dashd / Security
- **Evidence:** [`src/impl-go/dashd/cmd/dashd/main.go`](../../src/impl-go/dashd/cmd/dashd/main.go) line 105.
- **Symptom:** When dashd starts with `auth.mode=none` it logs a single WARN line. Production log pipelines often filter WARN, so the most security-critical posture decision a cluster can take is invisible to monitoring.
- **Root cause:** A single WARN log line is the only signal that authentication is disabled. There is no startup banner, no admin endpoint field, and no metric for monitoring to alert on.
- **Impact:** A misconfigured production deployment can run with auth disabled and operators may not notice for days.
- **Suggested fix:**
  1. Emit the banner at ERROR (not WARN) when `auth.mode=none` AND the listener is not bound to `127.0.0.1`.
  2. Add `security_posture` JSON to `/admin/health` returning `{auth_mode, tls_mode, mtls_required, insecure_listeners}` so monitoring can alert on it directly.
  3. Add a Prometheus gauge `dashd_security_posture_insecure` that flips to 1 when auth is disabled on a non-loopback listener.
- **Validation:**
  - Unit test that boots dashd with `auth.mode=none` bound to `0.0.0.0` and asserts the gauge is 1 and the admin endpoint reflects the posture.

---

## B5. Silent `Close` error swallowing in shutdown path

- **Severity:** LOW
- **Component:** dashd / `cmd/dashd`
- **Evidence:** [`src/impl-go/dashd/cmd/dashd/main.go`](../../src/impl-go/dashd/cmd/dashd/main.go) lines 286, 452, 453, 454.
- **Symptom:** Errors returned by `st.Close()`, `clusterReg.Close()`, and `electorProxy.Inner().Close()` are discarded with `_ =`. Store close failures (which can indicate disk-flush or fsync issues) are never reported.
- **Root cause:** Idiomatic but unconditional silencing of every shutdown error.
- **Impact:** A store that fails to flush during shutdown could corrupt on the next start without any warning in logs.
- **Suggested fix:** Log every shutdown `Close()` error at WARN with the component name:
  ```go
  if err := st.Close(); err != nil {
      slog.Warn("store close failed during shutdown", "component", "store", "backend", cfg.Storage.Backend, "error", err)
  }
  ```
  Apply the same pattern to `clusterReg.Close()` and `electorProxy.Inner().Close()`.
- **Validation:**
  - Inject a fake store whose `Close()` returns an error; assert the WARN is emitted with the correct keys.

---

## Reporting new bugs

Add a new section (`B<N+1>`) following the same template. Keep each entry self-contained and link to the exact file path + line numbers so the fix can be confirmed against the current `HEAD`.
