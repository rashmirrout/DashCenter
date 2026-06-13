# Agent Operating Discipline — DashCenter

> **Audience**: Any AI agent (Claude / Copilot / human pair) working on
> this repo across sessions.
> **Purpose**: A single, authoritative rulebook for *how* we work on
> DashCenter, so quality and the audit trail survive across agent
> sessions, model swaps, and context-window compactions.
> **Status**: This is the project's operating contract. Treat it the
> same as `CONTRIBUTING.md` — if your work doesn't match what's here,
> ask before deviating.

---

## Table of contents

1. [The Prime Directive](#1-the-prime-directive)
2. [The Per-Feature Documentation Rule](#2-the-per-feature-documentation-rule)
3. [The Tracker & Phase Plan Rule](#3-the-tracker--phase-plan-rule)
4. [The Cleanup Capture Rule](#4-the-cleanup-capture-rule)
5. [The Future Scopes Rule](#5-the-future-scopes-rule)
6. [The Test & Live E2E Rule](#6-the-test--live-e2e-rule)
7. [The Browser ↔ dashw ↔ dashd Contract](#7-the-browser--dashw--dashd-contract)
8. [The "Marshal Once" Performance Contract](#8-the-marshal-once-performance-contract)
9. [The Definition of Done](#9-the-definition-of-done)
10. [Anti-patterns (what NOT to do)](#10-anti-patterns-what-not-to-do)
11. [Working session checklist](#11-working-session-checklist)
12. [Where everything lives](#12-where-everything-lives)
13. [Memory & cross-session continuity](#13-memory--cross-session-continuity)

---

## 1. The Prime Directive

**Every change preserves the project's audit trail.**

The audit trail is the union of:

- Code in git history
- Tests that exercise the code
- Per-feature design docs that explain *why*
- Tracker rows (`docs/next-actions.md`) that record *when* + *who*
- Impl-phase gates (`specs/Impl-Plan/impl-phases.md`) that record
  *what gate this work closed*
- Cleanup deferrals (`docs/recommended-postGA-cleanup.md`) that record
  *what we chose not to do now, and why*
- `Future Scopes` sections inside each feature doc that record
  *what we deliberately left for next time*

If a change breaks any of these, the change is incomplete — regardless
of whether the code compiles and passes tests.

---

## 2. The Per-Feature Documentation Rule

### 2.1 The Rule

> Every feature, slice, or non-trivial change MUST produce a
> descriptive design doc under `docs/<area>/<feature-slug>.md`.

This is non-negotiable. The doc is the contract with the next agent
session — without it, six months later nobody knows why the code does
what it does, and we ship a regression.

### 2.2 What counts as "non-trivial"

| Change type | Doc required? |
|---|---|
| New RPC / REST endpoint | ✅ Yes, full doc |
| New SPA page or major view | ✅ Yes, full doc |
| New CLI subcommand | ✅ Yes, full doc |
| Wire format extension (proto field added) | ✅ Yes, full doc |
| Performance hardening pass (e.g., PE-G7's D1-D7) | ✅ Yes, full doc |
| Operator UX polish slice (multiple small UX wins shipped together) | ✅ Yes, single doc covering the slice |
| 1-line bug fix | ❌ No — Change Log entry in the parent feature's doc |
| Refactor with no behaviour change | ❌ No — PR description is enough |
| Test-only addition | ❌ No — PR description is enough |
| Cleanup deletion | ❌ No — but MUST update the relevant cleanup doc row to ✅ |

### 2.3 Doc location

```
docs/dashd-features/<feature-slug>.md     ← dashd / dashw / SPA features
docs/concepts/<concept>.md                ← cross-cutting concepts
docs/tutorial/<NN>-<title>.md             ← operator-facing tutorials
docs/<top-level-topic>.md                 ← repo-wide topics
specs/HLD/                                ← high-level design (rare)
specs/LLD/                                ← low-level design (rare)
```

For a feature spanning dashd + dashw + SPA, the doc lives under
`docs/dashd-features/` and section-references the relevant package
paths. One doc per feature, NOT one doc per package.

### 2.4 Required doc skeleton

```markdown
# <Feature Name>

> **Audience**: <who reads this — be specific>
> **Scope**: <what this doc covers, and what it explicitly doesn't>
> **Companion docs**: <list other relevant docs with links>
> **Status**: <✅ Shipped <date> | ⏳ In progress | ❎ Deferred>

---

## Table of contents
…

## 1. Problem statement
What was broken / missing / painful BEFORE this change. Concrete
symptoms operators or developers hit. No solution language here.

## 2. Goals & non-goals
Bullet-point lists. Non-goals matter as much as goals — they tell the
next agent what's deliberately out of scope.

## 3. Architecture / Solution
Diagrams (mermaid), tier responsibilities, data flow.

## 4. Wire contract
Proto extensions, REST routes, SSE event shapes, JSON examples.

## 5. Implementation
File-by-file table: what changed and why. Link each file with the
markdown convention from the global instructions.

## 6. Configuration
Flags, env vars, defaults. Recommended production values.

## 7. Operator UX
Screenshots OR text-mockups of what the operator sees. CLI help text.

## 8. Test strategy
What's covered, what's NOT covered + why.

## 9. Live e2e
Exact commands that verified it works in the 05-full-console fleet (or
equivalent). Include expected output snippets.

## 10. Future Scopes        ← ALWAYS the last numbered section
Every deferred consideration, even tiny ones. See §5 of this rules
doc for what to capture.
```

Optional sections (use when relevant): Security model, Performance
envelope, Operational guide (alerts, sizing), Defect log (when fixing
multiple things at once like PE-G7 did with D1-D7), Migration / backward
compatibility.

### 2.5 Doc length guidance

- Tiny feature (1 endpoint, 1 page): 100-300 lines is fine.
- Medium feature (1 RPC + 1 UI + supporting infra): 400-800 lines.
- Major slice (PE-G7-class hardening pass): 1000+ lines is fine.

Bias toward longer. The next agent session has no prior context. A doc
that took 30 min extra to write saves 4 hours of code archaeology later.

### 2.6 The "Change Log" exception

For follow-up tweaks too small to deserve their own doc (a 1-line fix,
a tooltip tweak, a metric label rename), add an entry to the parent
feature's doc under a `## N. Change Log` section:

```markdown
## N. Change Log

| Date | Change | Why |
|---|---|---|
| 2026-06-13 | Tooltip on ConnectionBadge clarifies source vs. via | Operator confusion in support ticket #X |
| 2026-06-15 | KEEPALIVE interval bumped 30s → 60s | Reduce browser wake-ups |
```

Same audit trail, no doc-per-typo bloat.

---

## 3. The Tracker & Phase Plan Rule

### 3.1 The Two-File Discipline

Two files hold the project's "what's done / what's next" state:

| File | Purpose | Edited when |
|---|---|---|
| `docs/next-actions.md` | Linear tracker of work items with row numbers, status, dates, notes | Every PR (before merge) |
| `specs/Impl-Plan/impl-phases.md` | Phase milestones + acceptance gates (`PE-G6`, `PE-G7`, etc.) | When a gate flips ✅ or a new gate is added |

### 3.2 Tracker row workflow

1. **Pick** the lowest-numbered ⬜ row you'll work on.
2. **Flip to ⏳** and fill `Started: YYYY-MM-DD` BEFORE writing code.
   Lets concurrent agents see the row is claimed.
3. **Land** the work in a focused commit/PR. Don't bundle multiple
   tracker rows into one change unless they're literally inseparable.
4. **Flip to ✅** with `Completed: YYYY-MM-DD` and write the full Notes
   column entry: what shipped, key file paths, test counts, design-doc
   link, live-e2e summary. Mirror the structure of existing ✅ rows
   (rows 14-18 are good templates).
5. **If you discover follow-up work** that doesn't fit the row, ADD a
   new row at the end with status ⬜. Never smuggle scope into an
   existing row's notes — the audit trail loses the boundary.

### 3.3 Impl-phases gate workflow

When a tracker row closes a phase-acceptance gate (e.g., row #14
closed `PE-G7`):

1. Update the gate row in `specs/Impl-Plan/impl-phases.md` to ✅.
2. Update the parent phase progress count: e.g., `"3 / 6"` → `"4 / 7"`.
3. If the row adds a new gate (e.g., PE-G7.1 was a polish gate added
   on top of PE-G7), append it to the gate table with a one-line
   description.

### 3.4 Tracker row Notes column — required fields

Every closed (✅) row's Notes column MUST include:

- One opening sentence summarising **what shipped** in operator-visible terms.
- Key file paths (3-10 of them), each as a markdown link.
- Test counts: `N broadcaster tests + M hub tests + K SPA tests; all
  P packages green`.
- **Design doc link**: `Design doc: [path/to/doc.md](path/to/doc.md).`
- **Live e2e summary**: one sentence describing the manual verification.

Sample (from row #15):

> **Closes the PE-G6 known limitation** where follower nodes' `/admin/topology`
> returned `leader_id: ""` until somebody explicitly called `ObserveCurrentLeader`.
> Added an `observeLoop(ctx)` goroutine started in `NewEtcdElector`…
> **Tests** (`internal/ha/leader/etcd_test.go`): `TestLeaderObserver_FollowerSeesLeaderWithoutExplicitCall` + `TestLeaderObserver_FollowerSeesLeaderHandover`. **All 26 dashd packages still green**.
> **Live e2e** in 05-full-console fleet: dashd-1 + dashd-2 followers both serve `"leader_id":"dashd-3"` in `/admin/topology` (vs. `""` before).
> Design doc: [docs/dashd-features/topology-operator-polish.md](dashd-features/topology-operator-polish.md).

---

## 4. The Cleanup Capture Rule

### 4.1 The Rule

> If you find code, config, docs, or scripts that *should* be cleaned
> up but it's not the right time to do it now — capture it in
> [docs/recommended-postGA-cleanup.md](recommended-postGA-cleanup.md)
> immediately.

Never leave a `// TODO: clean this up` comment without a corresponding
row in the cleanup doc. Comments rot; the cleanup doc is the canonical
backlog.

### 4.2 What counts as cleanup (not feature work)

| Counts as cleanup ✅ | Doesn't count — file as new tracker row ⬜ |
|---|---|
| Delete duplicate code now superseded by a better implementation | Build a new endpoint |
| Remove a config knob no longer needed | Add a config knob |
| Consolidate two near-identical files | Add a new file |
| Extract shared code into a package | Add a new package because of a new feature |
| Drop a deprecated REST route after the grace period | Add a new REST route |
| Update / delete stale docs whose content is now wrong | Write a new doc |

If in doubt: **does this change deliver new user-visible value?**
Yes → tracker row. No → cleanup doc.

### 4.3 Cleanup row required fields

Use the existing T1/T2/T3 tiering in
[recommended-postGA-cleanup.md](recommended-postGA-cleanup.md):

| Field | Required content |
|---|---|
| What | One paragraph describing what to delete / change |
| Why | Why it's redundant or wrong NOW |
| Net LOC | Estimate of deletion (negative number) |
| Config breakage | Any flags / env vars that change |
| Wire breakage | Any user-visible REST / SSE / gRPC contract change |
| Risk | What could go wrong, with mitigation |
| Effort | Rough hours / days |
| Validation | Exact steps to confirm it's safe before merge |
| Cross-links | Other docs / tracker rows that motivated this |

### 4.4 Tiering

- **T1**: ≥200 LOC removed AND fixes an active footgun.
- **T2**: 50-200 LOC removed OR consolidates a duplicate concept.
- **T3**: < 50 LOC OR purely cosmetic.

### 4.5 When cleanups land

- **Don't** land cleanups during feature work (the cleanup loses
  reviewer attention).
- **Do** schedule a dedicated cleanup window after each major release.
  Tag the window in `impl-phases.md` (e.g., `PE-Cleanup-1`).
- **Do** order cleanup work by tier (T1 → T2 → T3) within the window.

### 4.6 Anti-patterns documented separately

`recommended-postGA-cleanup.md` has an "Anti-patterns to NOT clean up"
section. If you see something that *looks* like cleanup but is
deliberate, add it there — saves the next agent from re-discovering
why we kept it.

---

## 5. The Future Scopes Rule

### 5.1 The Rule

> Every feature doc MUST end with a numbered `## Future Scopes`
> section that captures every deferred consideration the design
> conversation surfaced — even the tiny ones.

### 5.2 Why exhaustive Future Scopes matter

A Future Scope entry is a gift to the next agent. It says:

- "We thought about this."
- "Here's the trigger condition that would make us take it up."
- "Here's the proposed treatment."
- "Here are the open design questions."

Without these, the next agent re-runs the same design conversation
from scratch — and may not even remember the rejected alternatives.

### 5.3 Required structure per entry

```markdown
### N.M <descriptive title>

- **Trigger**: <what condition would justify doing this>
- **Proposal**: <the rough approach>
- **Open Qs**: <design questions to resolve before building>
- **Backward-compat**: <how to ship without breaking existing consumers>
```

The `### 11.1 Bidirectional WebSocket` entries in
[topology-streaming-design.md](dashd-features/topology-streaming-design.md#11-future-scopes)
are the gold standard — copy that style.

### 5.4 When a Future Scope item gets implemented

DON'T delete the Future Scope entry. Add a status marker:

```markdown
### 11.9 Operator-facing canary controls

- **Status**: ✅ Shipped in PE-G7.1 — see [topology-operator-polish.md §4](topology-operator-polish.md#4-slice-d--cordon--uncordon-button-in-topology-v2-spa).
- **Original trigger**: …
- **What we built**: …
- **What's still future**: <if anything remains>
```

The audit trail matters.

### 5.5 Future Scopes vs. Cleanup

| Question | Answer |
|---|---|
| Adds new user-visible feature value? | Future Scope |
| Just deletes / consolidates existing code? | Cleanup |
| A bit of both? | Future Scope (the "feature" half wins the routing) |

### 5.6 Quantity guidance

- Tiny feature: 3-5 Future Scopes.
- Medium feature: 5-10.
- Major slice (PE-G7-class): 10-15.

If you struggle to find any Future Scopes for a feature, you probably
didn't think about it hard enough. Look at: failure modes, scale
ceilings, multi-tenancy, observability gaps, security hardening,
i18n, accessibility, mobile, federation, replay/debug, integration
with other DashCenter subsystems.

---

## 6. The Test & Live E2E Rule

### 6.1 Test layers required

| Layer | When required |
|---|---|
| Unit tests (Go) | Any new function / method with branching logic |
| Component tests (Go) | New REST handler, gRPC service, SSE / WS handler |
| Integration tests (Go, embedded etcd) | Anything that touches the cluster registry, leader election, or distributed state |
| SPA tests (vitest) | New Zustand store, new hook, new view |
| Live e2e (05-full-console fleet) | Anything that touches the wire format, the SSE stream, or operator-visible UI |

### 6.2 The "all green" bar

After your change, the test result MUST be:

- **dashd**: all 26 packages green
- **dashw (console)**: all 8 packages green
- **dashctl**: all 8 packages green
- **SPA**: 234/234 tests + clean `npm run build`

If your change adds new packages, update those numbers in tracker rows
+ design doc. The exact numbers shift; the "all green" requirement
doesn't.

### 6.3 Live e2e required when

| Touches | Live e2e required? |
|---|---|
| Wire format (proto, SSE event kinds, REST response shape) | ✅ Always |
| `/admin/topology` or `/v1/cluster/topology*` | ✅ Always |
| Broadcaster / Hub multiplexer | ✅ Always |
| SPA visible behaviour | ✅ Always |
| Auth / audit chain | ✅ Always |
| Internal refactor only | ❌ Tests suffice |

### 6.4 Live e2e recipe

The 05-full-console fleet is the canonical playground:

```powershell
# Rebuild + redeploy what you changed
cd c:\WorkSpace\PS\PublicRepo\DashCenter\deploy\test-setup\05-full-console
docker compose build dashd-1 dashw
docker compose up -d --force-recreate --no-deps dashd-1 dashd-2 dashd-3 dashw

# Verify
curl -s http://localhost:28443/v1/cluster/topology | jq .
curl -s -N --max-time 4 "http://localhost:3000/api/console/topology-v2/stream"
```

Capture the exact commands + expected output in your design doc §9.

### 6.5 Don't skip tests for "obvious" changes

If you find yourself thinking "this is too trivial to test", that's
usually the change that breaks something subtle six weeks later. Add
the test.

---

## 7. The Browser ↔ dashw ↔ dashd Contract

### 7.1 The Rule

> Browsers MUST NOT speak to dashd directly. Every browser request
> goes through dashw.

This is **structural**, not stylistic. It means:

- No direct `fetch('http://dashd-1:8443/...')` in the SPA, ever.
- All browser-side REST goes through `/api/v1/*` (proxied by dashw).
- All browser-side streaming goes through `/api/console/topology-v2/stream`
  (multiplexed by dashw's Hub).
- All browser-side admin goes through `/api/console/*` (proxied by dashw).

### 7.2 Why this matters

- **Multiplexing**: dashw's Hub collapses N browser tabs onto 1
  upstream stream — a ~150× reduction at fleet scale.
- **Per-IP caps**: dashw can rate-limit a runaway tab loop without
  affecting dashd's cluster work.
- **Auth**: bearer subjects can be enforced at the dashw boundary
  without each dashd seeing browser credentials.
- **Future-proofing**: lets us add WAF / CDN / OTel in front of dashw
  without touching dashd or the SPA.

### 7.3 dashctl is different

`dashctl` (operator CLI) DOES talk to dashd directly. The BFF
multiplexer adds latency the CLI doesn't need; CLI sessions are short,
single-stream, and authenticated at the CLI layer. See
[topology-operator-polish.md §2.2](dashd-features/topology-operator-polish.md#22-architecture).

### 7.4 What to do if dashd's surface doesn't cover something

1. Add the endpoint / RPC to dashd properly (with tests + doc).
2. Add a thin reverse-proxy route to dashw (no transformation; just
   pass through).
3. Use it from the SPA.

Never add a dashw-only "synthesised" endpoint that talks to dashd's
admin port to assemble a response — that's the path that gave us the
[dashw aggregator collapse](recommended-postGA-cleanup.md) cleanup
debt.

---

## 8. The "Marshal Once" Performance Contract

### 8.1 The Rule

> For any fan-out path (1 producer → N subscribers), serialise the
> payload ONCE at the producer and share the byte slice with every
> subscriber.

This is PE-G7 defect D2's invariant. Violating it is a ~50× CPU
regression at typical fleet scale.

### 8.2 What "marshal once" means in practice

```go
// ✅ Correct — one Marshal call shared across N subscribers
frame := &Frame{Event: ev, JSON: mustMarshal(ev)}
for _, sub := range subscribers {
    sub.Send(frame)  // shares frame.JSON
}

// ❌ Wrong — N Marshal calls
for _, sub := range subscribers {
    js, _ := protojson.Marshal(ev)
    sub.Send(js)
}
```

### 8.3 When you must add per-subscriber data

(e.g., the PE-G7.1 `source` + `via` stamping)

DON'T re-marshal. Splice the per-subscriber bytes into the shared
JSON buffer. See
[sse-event-provenance.md §4.2](dashd-features/sse-event-provenance.md#42-injectsourcevia--the-byte-splice)
for the canonical example.

### 8.4 If splicing isn't possible

(e.g., per-subscriber filtering changes the structure)

Then explicitly degrade to per-subscriber marshal AND:

- Document it in the feature doc.
- Add a Future Scope entry for a possible better approach.
- Measure the CPU impact and gate the feature behind a flag if it's
  significant.

---

## 9. The Definition of Done

A change is "done" when ALL of these are true:

- [ ] Code compiles and passes lint.
- [ ] All required test layers added (see §6.1).
- [ ] All test suites green (see §6.2).
- [ ] Live e2e verified in 05-full-console fleet, if required (§6.3).
- [ ] Per-feature design doc written under `docs/<area>/<feature-slug>.md` (§2).
- [ ] Design doc includes a `## Future Scopes` section with ≥3 entries (§5).
- [ ] `docs/next-actions.md` updated: existing row ✅'d OR new row added (§3.2).
- [ ] `specs/Impl-Plan/impl-phases.md` gate flipped, if applicable (§3.3).
- [ ] Cross-link added to `docs/dashd-features/features.md` "See also" footer (§3.4).
- [ ] Any noticed cleanups captured in `docs/recommended-postGA-cleanup.md` (§4).
- [ ] Any new browser-side calls go through dashw, not dashd (§7).
- [ ] Any new fan-out path preserves marshal-once (§8).

Missing any one of these = the change is not done. Don't merge.

---

## 10. Anti-patterns (what NOT to do)

### 10.1 Don't bundle scope

Each commit / PR / tracker row should land ONE coherent change. If
you find yourself writing "Also fixed X" in a tracker row, X needs
its own row.

### 10.2 Don't leave `TODO` without a tracker entry

`// TODO: fix this later` is a lie. Either:
- Fix it now, OR
- Open a tracker row OR a cleanup doc entry AND link to it in the
  comment: `// TODO(post-GA cleanup T2.5): foo`.

### 10.3 Don't optimise prematurely

PE-G7's D1-D7 hardening pass landed AFTER PE-G6's basic version was
in production for a day. The right shape was visible only after real
fan-out load. Don't pre-engineer.

### 10.4 Don't ship code without a doc and call it "minor"

Every change feels minor when you're writing it. Three months later,
the next agent doesn't know it ever existed. Write the doc.

### 10.5 Don't change wire format without bumping versions OR documenting backward-compat

Browsers, CLIs, and external integrations pin to wire formats. If you
add a proto field, it's additive (clients ignore unknown fields). If
you rename or remove one, that's a breaking change requiring a
deliberate version bump.

### 10.6 Don't trust pre-compaction context

When the conversation gets long enough that a compaction summary
appears, treat any non-tool-verified fact as suspect. Re-read the
relevant file / re-run the test before acting on remembered context.

### 10.7 Don't run cleanup as a side-effect of feature work

Cleanup deserves dedicated reviewer attention. See §4.5.

### 10.8 Don't skip the Future Scopes section because "nothing comes to mind"

If nothing comes to mind, you didn't push the design hard enough. See
§5.6 for the prompts.

### 10.9 Don't update one tracker file without the others

`next-actions.md` ↔ `impl-phases.md` ↔ `features.md` ↔ design doc
must all reference each other. A row that lands in only one place
fragments the audit trail.

### 10.10 Don't bypass dashw to "make the SPA faster"

If the SPA needs something dashw doesn't currently expose, expose it
through dashw. See §7.4.

---

## 11. Working session checklist

Before starting work in a new agent session:

1. **Read** the current state of `docs/next-actions.md` — what's ⏳?
   What's the lowest ⬜?
2. **Read** `specs/Impl-Plan/impl-phases.md` to know which phase + gate
   you're working toward.
3. **Read** any feature doc for the area you're about to touch.
4. **Check** session memory + repo memory for relevant notes.
5. **Decide** what tracker row your work targets. Flip it to ⏳ with
   your start date.

During work:

1. Run the relevant test suites BEFORE editing, to know the baseline.
2. Implement.
3. Run tests again — confirm all green.
4. Run live e2e if §6.3 says yes.

Before declaring done:

1. Walk through the Definition of Done checklist (§9).
2. Write / update the design doc.
3. Write the Future Scopes section.
4. Update tracker + impl-phases + cross-links.
5. Capture any cleanup deferrals.

---

## 12. Where everything lives

| Concern | File / folder |
|---|---|
| Operating rules (this doc) | `docs/agent-operating-discipline.md` |
| Linear tracker of work | `docs/next-actions.md` |
| Phase milestones + gates | `specs/Impl-Plan/impl-phases.md` |
| dashd / dashw / SPA features | `docs/dashd-features/` |
| Operator REST API reference | `docs/dashd-features/features.md` |
| CLI reference | `docs/CLI_GUIDE.md` |
| Cleanup backlog | `docs/recommended-postGA-cleanup.md` |
| Tutorials | `docs/tutorial/` |
| Cross-cutting concepts | `docs/concepts/` |
| High-level design specs | `specs/HLD/` |
| Low-level design specs | `specs/LLD/` |
| Test-setup scenarios | `deploy/test-setup/` |
| Live operator playground | `deploy/test-setup/05-full-console/` |
| Proto sources of truth | `proto/dashcenter/v1/` |
| Generated proto Go | `src/impl-go/gen/go/` |
| dashd source | `src/impl-go/dashd/` |
| dashw (console BFF) source | `src/impl-go/console/` |
| dashctl source | `src/impl-go/dashctl/` |
| SPA source | `src/impl-web/console/` |

---

## 13. Memory & cross-session continuity

### 13.1 What to write to repo memory

`/memories/repo/` should hold the *condensed* version of this file
plus any project-specific shortcuts the next agent benefits from
reading on every session start:

- Build commands (esp. the Go workspace + toolchain quirks).
- Test commands per module.
- Live e2e port mappings for the 05-full-console fleet.
- Known go.mod / GOTOOLCHAIN gotchas.

Keep entries SHORT — repo memory is loaded into context automatically.
Link to this doc for the full version.

### 13.2 What to write to session memory

`/memories/session/` should hold the in-flight task plan only:

- Current tracker row being worked.
- Files touched so far.
- Tests run + results.
- Outstanding "still to do" items in the slice.

### 13.3 What NOT to write to memory

- Long-form design rationale → goes in `docs/dashd-features/`.
- Tracker state → already in `docs/next-actions.md`.
- Build artifacts, secrets, ports for shared infra.

### 13.4 At session start

Read in this order:

1. `/memories/repo/` (always loaded).
2. `/memories/session/` if mid-task.
3. `docs/agent-operating-discipline.md` (this file) if returning to
   the project after a gap.
4. `docs/next-actions.md` for current status.

### 13.5 At session end

Update `/memories/session/` with the in-flight plan so the next
session can resume. Don't bloat repo memory unless the insight is
truly persistent.

---

## Appendix A — Quick Reference Card

| Need to… | File to update |
|---|---|
| Add a new endpoint | feature doc + features.md + next-actions.md row + impl-phases gate |
| Fix a bug | parent feature doc Change Log + next-actions.md row |
| Delete redundant code | recommended-postGA-cleanup.md row (NOT a tracker row) |
| Add a CLI command | CLI_GUIDE.md + feature doc + next-actions.md row |
| Add a new SPA view | feature doc + sidebar nav update + next-actions.md row |
| Capture a deferred consideration | Future Scopes section in the relevant feature doc |
| Note a cleanup opportunity for later | recommended-postGA-cleanup.md row |
| Document a new operating rule | this file |

---

> **If in doubt**: pause, read this file, ask before deviating.
> The discipline is the product as much as the code is.
