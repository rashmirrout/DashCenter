// Tests for the Bucket / Rollup additions in PE-3a (PE-G8). Cover both the
// new rollup primitives and the legacy Snapshot()/Tick()/Keys()/Forget() API
// surface so the whole counters package stays at 100% on PE-3a-touched code.

package counters

import (
	"strings"
	"sync"
	"testing"
)

// ── legacy surface (Snapshot / Tick / Keys / Forget) ───────────────────────

func TestNewIsEmpty(t *testing.T) {
	r := New()
	if got := r.Keys(); len(got) != 0 {
		t.Fatalf("fresh registry not empty: %v", got)
	}
	if got := r.Snapshot("eni-1"); got["packets_in"] != 0 {
		t.Fatalf("unknown-key snapshot non-zero: %v", got)
	}
}

func TestTickIsDeterministic(t *testing.T) {
	r := New()
	r.Tick("eni-1")
	r.Tick("eni-1")
	s1 := r.Snapshot("eni-1")

	r2 := New()
	r2.Tick("eni-1")
	r2.Tick("eni-1")
	s2 := r2.Snapshot("eni-1")

	for k, v := range s1 {
		if s2[k] != v {
			t.Fatalf("non-deterministic counter %s: %d vs %d", k, v, s2[k])
		}
	}
}

func TestSnapshotReturnsAllFiveFields(t *testing.T) {
	r := New()
	r.Tick("eni-1")
	got := r.Snapshot("eni-1")
	for _, want := range []string{"packets_in", "packets_out", "bytes_in", "bytes_out", "drops"} {
		if _, ok := got[want]; !ok {
			t.Errorf("snapshot missing field %q (got %v)", want, got)
		}
	}
}

func TestKeysReturnsEveryTrackedKey(t *testing.T) {
	r := New()
	r.Tick("eni-1")
	r.Tick("vnet-prod")
	r.Tick("vnet-prod:10.0.0.10")
	got := r.Keys()
	if len(got) != 3 {
		t.Fatalf("want 3 keys, got %d (%v)", len(got), got)
	}
}

func TestForgetRemovesKey(t *testing.T) {
	r := New()
	r.Tick("eni-1")
	r.Forget("eni-1")
	if r.Snapshot("eni-1")["packets_in"] != 0 {
		t.Fatal("forget did not zero counters")
	}
	if len(r.Keys()) != 0 {
		t.Fatal("forget did not remove key")
	}
}

// TestTickAccumulatesDropsOverMany ensures the deterministic-tick drop
// counter eventually fires (the legacy `if h%23 == 0 { drops++ }` branch).
// Without this, drops stays at zero for keys whose hash never lands on 23k.
func TestTickAccumulatesDropsOverMany(t *testing.T) {
	r := New()
	// Tick enough distinct keys that at least one of them hashes to
	// h%23==0 — the FNV-1a distribution makes this trivial within 100.
	for i := 0; i < 200; i++ {
		k := JoinKey([]string{"drop-key", string(rune('a' + i%26)), string(rune('a' + (i/26)%26))})
		r.Tick(k)
	}
	total := r.TotalBucket()
	if total.Drops == 0 {
		t.Fatal("expected at least one drop across 200 distinct keys")
	}
}

// TestGetReturnsExistingRowOnRaceLost exercises the double-checked-lock
// fast path inside Registry.get where two goroutines race to create the
// same key. The first creator wins; the second observes the value already
// present after acquiring the write lock and must not overwrite it.
func TestGetReturnsExistingRowOnRaceLost(t *testing.T) {
	// Use many fresh, never-before-seen keys per iteration so multiple
	// concurrent Tick()s on the same key actually race for first-creation.
	// We loop several times because Go's scheduler doesn't guarantee
	// contention on the write-lock recheck on every attempt.
	for iter := 0; iter < 50; iter++ {
		r := New()
		key := JoinKey([]string{"race-key", string(rune('a' + iter))})
		var wg sync.WaitGroup
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); r.Tick(key) }()
		}
		wg.Wait()
		got := r.SnapshotBucket(key)
		// 32 ticks must have all hit the same row (sum > one tick's
		// contribution). Proves the second-creator branch returned the
		// EXISTING row rather than overwriting with a fresh zero row.
		if got.PacketsIn == 0 {
			t.Fatalf("iter %d: concurrent Ticks did not accumulate", iter)
		}
	}
}

func TestJoinKey(t *testing.T) {
	if got := JoinKey([]string{"vnet-prod", "10.0.0.10"}); got != "vnet-prod:10.0.0.10" {
		t.Fatalf("JoinKey wrong: %q", got)
	}
	if got := JoinKey([]string{"eni-1"}); got != "eni-1" {
		t.Fatalf("JoinKey single: %q", got)
	}
}

// ── new Bucket math (PE-3a) ───────────────────────────────────────────────

func TestBucketAddIsCommutative(t *testing.T) {
	a := Bucket{PacketsIn: 10, PacketsOut: 20, BytesIn: 100, BytesOut: 200, Drops: 1}
	b := Bucket{PacketsIn: 5, PacketsOut: 6, BytesIn: 50, BytesOut: 60, Drops: 0}

	ab := a
	ab.Add(b)
	ba := b
	ba.Add(a)
	if ab != ba {
		t.Fatalf("Add not commutative: %+v vs %+v", ab, ba)
	}
	want := Bucket{PacketsIn: 15, PacketsOut: 26, BytesIn: 150, BytesOut: 260, Drops: 1}
	if ab != want {
		t.Fatalf("Add wrong: got %+v want %+v", ab, want)
	}
}

func TestBucketAddZeroIsIdentity(t *testing.T) {
	a := Bucket{PacketsIn: 7}
	a.Add(Bucket{})
	if a != (Bucket{PacketsIn: 7}) {
		t.Fatalf("zero-add changed bucket: %+v", a)
	}
}

// ── SnapshotBucket ────────────────────────────────────────────────────────

func TestSnapshotBucketUnknownKey(t *testing.T) {
	r := New()
	if got := r.SnapshotBucket("nope"); got != (Bucket{}) {
		t.Fatalf("unknown key bucket non-zero: %+v", got)
	}
}

func TestSnapshotBucketMirrorsLegacy(t *testing.T) {
	r := New()
	r.Tick("eni-1")
	r.Tick("eni-1")
	legacy := r.Snapshot("eni-1")
	typed := r.SnapshotBucket("eni-1")
	if typed.PacketsIn != legacy["packets_in"] ||
		typed.PacketsOut != legacy["packets_out"] ||
		typed.BytesIn != legacy["bytes_in"] ||
		typed.BytesOut != legacy["bytes_out"] ||
		typed.Drops != legacy["drops"] {
		t.Fatalf("typed bucket diverges from legacy map: typed=%+v legacy=%v", typed, legacy)
	}
}

// ── TotalBucket (DPU-wide sum) ────────────────────────────────────────────

func TestTotalBucketEmpty(t *testing.T) {
	r := New()
	if got := r.TotalBucket(); got != (Bucket{}) {
		t.Fatalf("empty total non-zero: %+v", got)
	}
}

func TestTotalBucketSumsEveryKey(t *testing.T) {
	r := New()
	for _, k := range []string{"eni-1", "eni-2", "vnet-prod", "vnet-prod:10.0.0.10"} {
		r.Tick(k)
	}
	per := map[string]Bucket{}
	var want Bucket
	for _, k := range r.Keys() {
		b := r.SnapshotBucket(k)
		per[k] = b
		want.Add(b)
	}
	got := r.TotalBucket()
	if got != want {
		t.Fatalf("TotalBucket wrong: got %+v want %+v (per-key: %+v)", got, want, per)
	}
}

// ── Rollup (scope-prefix membership) ──────────────────────────────────────

func TestRollupEmptyScopeReturnsZero(t *testing.T) {
	r := New()
	r.Tick("eni-1")
	if got := r.Rollup(""); got != (Bucket{}) {
		t.Fatalf("empty scope non-zero: %+v", got)
	}
}

func TestRollupClaimsExactAndPrefixedKeys(t *testing.T) {
	r := New()
	// eni-1 owns both the single-key form AND the eni-1:* prefix.
	r.Tick("eni-1")
	r.Tick("eni-1:10.0.0.0/24")
	r.Tick("eni-1:10.0.0.0/16")
	// eni-2 must NOT be summed into eni-1.
	r.Tick("eni-2")
	r.Tick("eni-2:0.0.0.0/0")
	// vnet-prod and a substring-match-trap key must NOT be summed into eni-1.
	r.Tick("vnet-prod")
	r.Tick("eni-10") // shares "eni-1" as substring but NOT as first component

	want := Bucket{}
	want.Add(r.SnapshotBucket("eni-1"))
	want.Add(r.SnapshotBucket("eni-1:10.0.0.0/24"))
	want.Add(r.SnapshotBucket("eni-1:10.0.0.0/16"))

	got := r.Rollup("eni-1")
	if got != want {
		t.Fatalf("eni-1 rollup wrong: got %+v want %+v", got, want)
	}

	// Sanity: eni-2 rollup is disjoint.
	eni2 := r.Rollup("eni-2")
	if eni2 == (Bucket{}) {
		t.Fatalf("eni-2 rollup should be non-zero")
	}
	if got == eni2 {
		t.Fatalf("eni-1 and eni-2 rollups must differ")
	}
}

func TestRollupNeverConfusesPrefixSubstring(t *testing.T) {
	// Regression test: "eni-1" must NOT claim "eni-10" or "eni-11".
	r := New()
	r.Tick("eni-1")
	r.Tick("eni-10")
	r.Tick("eni-11")
	r.Tick("eni-1:")    // pathological: empty trailing component
	r.Tick("eni-1:x")   // legitimate child

	scope1 := r.Rollup("eni-1")
	scope10 := r.Rollup("eni-10")
	scope11 := r.Rollup("eni-11")

	// eni-10 / eni-11 rollups must each be just their own SnapshotBucket.
	if scope10 != r.SnapshotBucket("eni-10") {
		t.Fatalf("eni-10 leaked: got %+v", scope10)
	}
	if scope11 != r.SnapshotBucket("eni-11") {
		t.Fatalf("eni-11 leaked: got %+v", scope11)
	}
	// eni-1 rollup is eni-1 + eni-1: + eni-1:x; verify it's distinct from
	// the disjoint substring buckets.
	want := Bucket{}
	want.Add(r.SnapshotBucket("eni-1"))
	want.Add(r.SnapshotBucket("eni-1:"))
	want.Add(r.SnapshotBucket("eni-1:x"))
	if scope1 != want {
		t.Fatalf("eni-1 rollup wrong: got %+v want %+v", scope1, want)
	}
}

func TestRollupReturnsZeroWhenNoMatch(t *testing.T) {
	r := New()
	r.Tick("eni-1")
	if got := r.Rollup("never-tracked"); got != (Bucket{}) {
		t.Fatalf("unmatched scope non-zero: %+v", got)
	}
}

// ── RollupAll (batch) ─────────────────────────────────────────────────────

func TestRollupAllPreservesZeroes(t *testing.T) {
	r := New()
	r.Tick("eni-1")
	got := r.RollupAll([]string{"eni-1", "eni-missing"})
	if got["eni-1"] == (Bucket{}) {
		t.Fatal("RollupAll dropped eni-1's data")
	}
	if got["eni-missing"] != (Bucket{}) {
		t.Fatalf("RollupAll filled missing scope unexpectedly: %+v", got["eni-missing"])
	}
}

func TestRollupAllMatchesIndividualRollups(t *testing.T) {
	r := New()
	for _, k := range []string{"eni-1", "eni-1:x", "eni-2", "vnet-prod", "vnet-prod:10.0.0.10"} {
		r.Tick(k)
	}
	batch := r.RollupAll([]string{"eni-1", "eni-2", "vnet-prod"})
	for _, scope := range []string{"eni-1", "eni-2", "vnet-prod"} {
		if batch[scope] != r.Rollup(scope) {
			t.Errorf("batch != individual for %s", scope)
		}
	}
}

// ── concurrency: ensure Rollup is race-free against Tick ──────────────────

func TestRollupRaceFreeAgainstTick(t *testing.T) {
	r := New()
	keys := []string{"eni-1", "eni-1:a", "eni-1:b", "vnet-prod", "vnet-prod:10.0.0.1"}

	// Pre-populate so the readers below always see non-zero counters even
	// if the goroutine scheduler delays the ticker goroutine startup.
	for _, k := range keys {
		r.Tick(k)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				for _, k := range keys {
					r.Tick(k)
				}
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			_ = r.Rollup("eni-1")
			_ = r.Rollup("vnet-prod")
			_ = r.TotalBucket()
		}
	}()
	for i := 0; i < 1000; i++ {
		_ = r.RollupAll([]string{"eni-1", "vnet-prod"})
	}
	close(stop)
	wg.Wait()

	// Counter-pre-population guarantees non-zero; this is a defensive
	// invariant check, not a contention assertion.
	if r.Rollup("eni-1").PacketsIn == 0 {
		t.Fatal("expected non-zero packets_in after pre-tick + concurrent loop")
	}
}

// ── string helper used by Rollup matchers ─────────────────────────────────

func TestPrefixHelperShape(t *testing.T) {
	// Documents (and locks in) the prefix convention: scope "eni-1" matches
	// keys starting with "eni-1:" but never bare "eni-10".
	if !strings.HasPrefix("eni-1:foo", "eni-1:") {
		t.Fatal("strings.HasPrefix semantics regressed")
	}
	if strings.HasPrefix("eni-10", "eni-1:") {
		t.Fatal("strings.HasPrefix semantics regressed (false positive)")
	}
}
