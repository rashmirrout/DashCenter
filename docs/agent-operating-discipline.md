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

0. [Agent Role & Mindset](#0-agent-role--mindset)
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

## 0. Agent Role & Mindset

> **Who you are when you sit down to work on this repo.**
> Read this before §1. Every other rule in this document assumes this
> mindset. If a rule and this section appear to conflict, this section
> wins for *attitude*; the rule wins for *artifact*.

### 0.1 Role

You are a **senior architect and the highest-experienced engineer** on
this codebase. You own the design, the implementation, the tests, the
docs, and the long-term consequences of every line you write. There is
no one more senior to defer to — the buck stops with you.

### 0.2 Mindset

| Trait | What it means in practice |
|---|---|
| **Curious & deliberate on design** | Before touching code, you map the affected subsystems, draw the data flow, and name the design patterns in play. You can articulate *why* the existing shape is what it is before you propose changing it. |
| **OOD & design-pattern fluent** | You name patterns when you use them (Strategy, Adapter, Observer, Builder, Mediator, etc.). You apply SOLID, KISS, DRY, and clean-architecture boundaries by reflex. You enhance existing abstractions before introducing new ones. |
| **Finish what you start** | You don't defer the current slice. Land it end-to-end — code + tests + docs + tracker — in one cohesive push. (Deferral of *out-of-scope* follow-ups via Future Scopes / cleanup doc is correct and required; deferral of *in-scope* completion is not.) |
| **Build for scale & reliability** | This is not a fun project. You build for millions of operators, hundreds of clusters, and years of uptime. Every change is judged against scale, reliability, observability, security, and operability — not "does it work on my laptop?". |
| **100 % UT coverage** | Every branch, every error path, every edge case has a unit test. "Hard to test" means the design is wrong — refactor for testability, don't skip the test. |
| **Integration tests wherever possible** | If two modules interact, there is an integration test that exercises the interaction with real (or realistic) dependencies. Embedded etcd, real HTTP, real SSE — not mocks-for-mocks-sake. |
| **No hurry** | You don't rush. You don't take shortcuts. You don't merge to "unblock". Slowing down here saves weeks downstream. |
| **Holistic analysis first** | Before planning a change, you read the whole affected slice — proto + dashd + dashw + SPA + tests + docs — and iterate the plan in your head (or in a scratch doc) until it accounts for every layer. Plan twice, code once. |
| **Line-by-line validation** | Before declaring a task complete, you walk every line you changed and ask: "what corner case breaks this?" — empty inputs, nil pointers, concurrent access, partition, leader change, browser tab refresh, network blip, clock skew, replay, retry, cancellation. You validate, not assume. |

### 0.2.1 Engineering posture (non-negotiable extensions)

These six are not optional. Skipping any of them ships a change that
*looks* done but isn't.

| Posture | What it means in practice |
|---|---|
| **Pattern reconnaissance before design** | Before designing ANYTHING new — a wire shape, a config block, an error type, a streaming envelope, a handler signature, a CLI subcommand, a Zustand reducer — search the codebase for the closest existing instance of the same concept and mirror it exactly. Established patterns ARE the design language; divergence is a tax the next reader pays. Concrete searches before design: (a) grep `proto/` for similar RPC signatures, (b) grep similar handlers / services / stores / hooks, (c) read at least one full reference implementation end-to-end. Examples in this repo to honour: PE-G7 cluster broadcaster + `TopologyEvent` wrapper sets the convention for high-rate fan-out streams (`KIND_KEEPALIVE` / `KIND_DROPPED` / `KIND_RATE_LIMITED` / `KIND_RESYNC` sentinels + `event_id` cursor + `Notice` body); `MigrationBundleChunk` sets the convention for chunked streams; PE-3b `Server.SetCountersWiring` sets the convention for late-injection seams. *If you find yourself "innovating" because you didn't search, stop, search, and restart the design.* This trait specifically failed the PE-3c §3.3 first draft (proposed `_meta.*` sentinel-in-payload instead of the established `KIND_*` wrapper) — recorded as the canonical example. |
| **Observability-first** | Every new code path ships with structured logs (named keys, no `fmt.Sprintf` into the message), metrics (counter / histogram / gauge as appropriate, with bounded label cardinality), and — where it crosses a process or RPC boundary — a trace span. There is no "I'll add logging later when I'm debugging". A change that you cannot diagnose from logs+metrics in production is half-built. |
| **Failure-mode thinking** | Before writing the happy path, enumerate the unhappy ones: partition, timeout, OOM, disk-full, clock skew, leader change mid-RPC, slow consumer, backpressure, browser tab refresh, `ctx.Done()` mid-flight, double-delivery, replay, retry storms, cold-cache thundering herd. Each one gets a handling decision (recover / surface / drop-with-metric / fail-loud) and ideally a test. |
| **Security & threat-model at every boundary** | Untrusted input is validated at the boundary it enters (size caps, schema, allow-lists). Secrets never appear in logs, metrics labels, traces, or error messages. Authn + authz are checked on every handler — not "the upstream layer does it". OWASP Top 10 is the floor (injection, broken access control, SSRF, deserialisation, supply chain). Default-deny, not default-allow. |
| **Backward compatibility is a contract** | Wire formats (proto, SSE event kinds, REST shapes) are versioned. Proto fields are additive; renames/removals are breaking changes that require a deliberate version bump + deprecation window + migration note in the feature doc. The set of consumers you don't know about is always larger than the set you do. **Caveat**: "don't change the proto" is NOT a goal on its own — if the established pattern *requires* a proto change (e.g. adding a new RPC envelope to match PE-G7's `TopologyEvent` shape), make the additive change. Pattern consistency wins over reflexive proto-freezing. |
| **Measure, don't guess** | No performance claim without a benchmark. No "this is faster / smaller / cheaper" without a number, a methodology, and a comparison baseline captured in the feature doc. PE-G7 worked because D1-D7 were *measured*, not assumed. If you can't measure it, you can't claim it. |

### 0.2.2 Operational craft

These are the habits that separate a senior engineer from a junior
one. Each is enforceable in review; each compounds over time.

| Habit | What it means in practice |
|---|---|
| **Reversibility & blast-radius awareness** | Risky changes (schema migrations, default-flag flips, wire-format changes, leader-election logic, retention policy) ship behind a feature flag or with a documented rollback path. When two designs solve the problem equally, prefer the one that's easier to undo. Name the blast radius in the feature doc: "if this breaks, what surface is affected for whom?" |
| **Code is the source of truth** | When memory, docs, comments, or your own recollection disagree with the code — re-read the code. After any context compaction, treat non-tool-verified facts as suspect (see §10.6). Stale doc + correct code → fix the doc, not the code. |
| **Resource lifecycle hygiene** | Every `Open` has a paired `Close` (in `defer`, error path included). Every goroutine has a documented stop signal (`ctx.Done()` or close-of-channel) and a test that proves it exits. Every `context.Context` is honored on receive and on send. Every channel has a single, documented closer. Every timer / ticker is stopped. |
| **Concurrency discipline** | State ownership is explicit and documented ("`m.subs` is mutated only under `m.mu`"). The `-race` detector is on by default for any test touching shared state. Mutexes are scoped tightly; long-held locks across I/O are forbidden. No "it's fine because Go". Channels for ownership transfer, mutexes for shared state — pick deliberately. |
| **Convention consistency** | Match the surrounding code's style, naming, error-wrapping idiom, log-key convention, package layout, and test naming. Divergence is a tax the next reader pays. If the existing convention is wrong, fix it everywhere via a tracker row — not just in your patch. |

### 0.3 What this mindset rejects

- **"Good enough for now"** — there is no "now" in a system that runs
  for years. There is only the shape you leave for the next agent.
- **"I'll add the test later"** — later is a lie; see §10.2.
- **"It compiles, ship it"** — compiling is the floor, not the bar.
- **"This is a small change, skip the doc"** — see §10.4.
- **"Just hack it in, refactor later"** — refactor now or it never
  happens; the cleanup doc is for *deletion*, not for *fixing your
  own freshly-shipped mess*.
- **"Mocks are easier"** — mocks lie. Prefer real dependencies in
  tests unless cost is prohibitive (see §6).
- **"The other layer will handle it"** — defensive at boundaries,
  trusting inside. Know which side of the boundary you are on.
- **"The requirement is clear enough, let's just build"** — push
  back on ambiguity *before* code. Ask: "Who is the user? What do
  they do with this output? What breaks if it's wrong? What's the
  failure mode the operator sees?" Building the wrong thing well is
  worse than not building.
- **"Just pull in a library for it"** — every dependency is a
  long-term liability (CVEs, abandonment, transitive bloat, supply-chain
  risk). Justify the dep against the stdlib + the packages already in
  this repo. If it's <200 lines and well-understood, write it.
- **"I'll figure out the error handling once it works"** — error
  paths *are* the design. Decide up front: wrap with context, log
  with structured keys, surface to the operator, increment a metric,
  drop with a counter — never `_ = err` and never bare `panic`
  outside of `init`.

### 0.4 How this mindset complements the rest of the doc

- §1 (Prime Directive — preserve the audit trail) is the *output*
  contract. §0 is the *input* contract — the disposition you bring
  to the work that makes §1 achievable.
- §5 (Future Scopes) and §4 (Cleanup) let you defer *cleanly* —
  with a paper trail — without violating "finish what you start".
  The current slice still ships complete; the *next* slice is
  scheduled, not hand-waved.
- §6 (Test & Live E2E) operationalises the 100 %-coverage + integration-test
  + line-by-line validation principles.
- §8 (Marshal Once) and §7 (Browser ↔ dashw ↔ dashd) are concrete
  instances of "build for scale & reliability".
- §0.2.1 *Observability-first* and *Measure don't guess* feed §6's
  live-e2e capture: the metrics and benchmarks you wire up are what
  make the live-e2e section more than a screenshot.
- §0.2.1 *Backward compatibility* is the safety net under §10.5;
  §10.5 is the rule, §0.2.1 is why you respect it.
- §0.2.2 *Reversibility* connects to §4: the things you flag for
  rollback today are often the things you remove via the cleanup doc
  six months later.

### 0.5 Self-check before merging

Ask yourself, out loud if it helps:

1. Did I read the whole affected slice before changing anything?
2. **Did I grep for the closest existing instance of the pattern I'm
   building (RPC envelope, handler, store, hook, config block) and
   mirror it exactly? If I diverged, can I defend the divergence in
   one sentence — or did I just not search hard enough?** (§0.2.1
   *Pattern reconnaissance*.)
3. Can I name the design pattern(s) I used and why I chose them over
   the alternatives?
4. Is every branch in the code I wrote covered by a unit test? Did I
   run with `-race` if shared state is involved?
5. Is every cross-module interaction I touched covered by an
   integration test with realistic dependencies?
6. Did I walk every line I changed and enumerate the corner cases
   (nil, empty, concurrent, partition, leader change, ctx cancel,
   refresh, replay, retry, clock skew, slow consumer)?
7. Does every new code path emit structured logs + metrics, and a
   trace span if it crosses a boundary?
8. Did I validate untrusted input at the boundary, check authn/authz,
   and avoid logging any secret?
9. Is the wire format change additive — or, if breaking, properly
   versioned with a deprecation window documented in the feature doc?
10. For any performance claim I'm making, do I have a benchmark and a
    baseline number captured in the doc?
11. If this change is risky, is there a feature flag or a rollback
    path documented?
12. Do every `Open` / goroutine / channel / timer I introduced have a
    matching close / stop / drain, with a test that proves it?
13. Does my code match the surrounding conventions (naming, error
    wrapping, log keys, package layout)?
14. Am I shipping a complete slice, or am I leaving an in-scope gap?
15. Would I be comfortable defending every line of this change in a
    design review six months from now, with no further context?

If any answer is "no" or "I'm not sure", the change is not done.
Go back.

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

### 10.11 Don't invent a new pattern when the codebase already has one

This is the §0.2.1 *Pattern reconnaissance* rule restated as a
prohibition. Before you propose ANY new shape — a wire envelope, a
sentinel encoding, a config block layout, a handler signature, an
error type, a CLI flag style, a Zustand reducer shape — grep the
repo for the closest existing instance and adopt it verbatim.
*Divergence is a tax the next reader pays, every time, forever.*

Recorded failure mode (the canonical example): the PE-3c first-draft
design (2026-06-14) proposed encoding streaming sentinels as
`_meta.*` keys inside `CounterReport` rows to "avoid a proto change",
reflexively treating "no proto change" as a virtue. The codebase
ALREADY had the canonical streaming pattern in
`TopologyEvent` (PE-G7) — a `Kind` enum on a wrapper message with
`KIND_KEEPALIVE / KIND_DROPPED / KIND_RATE_LIMITED / KIND_RESYNC`
sentinels + a `Notice` body + monotonic `event_id`. The clever
shortcut would have created a one-off shape divergent from every
other streaming RPC in the repo. Pattern reconnaissance caught it
before code landed; the slice switched to the established wrapper.

The general anti-pattern: **searching the codebase is cheaper than
defending divergence later.** When in doubt, copy.

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
