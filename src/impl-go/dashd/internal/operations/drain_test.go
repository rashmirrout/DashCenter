// PC-G7 drain tests: cordon-then-rehome happy path, no-destination,
// per-ENI failure, parallelism cap, cancellation.
package operations

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeMover is a scriptable Mover backed by plain maps.
type fakeMover struct {
	mu sync.Mutex
	// enisByDpu[dpuID] -> ENIs on that DPU.
	enisByDpu map[string][]EniRef
	// rehomes records every Rehome call (eni, dst).
	rehomes []rehomeCall
	// pickFn lets tests inject custom destination selection.
	pickFn func(eni EniRef, excluded []string) string
	// rehomeFn lets tests inject per-call success/failure.
	rehomeFn func(eni EniRef, dst string) error
	// rehomeDelay is added to every Rehome call to test parallelism.
	rehomeDelay time.Duration
	// concurrentPeak captures the max in-flight Rehomes seen at once.
	inFlight        atomic.Int32
	concurrentPeak  atomic.Int32
}

type rehomeCall struct {
	eni EniRef
	dst string
}

func (f *fakeMover) EnisOn(dpu string) []EniRef {
	f.mu.Lock()
	defer f.mu.Unlock()
	src := f.enisByDpu[dpu]
	out := make([]EniRef, len(src))
	copy(out, src)
	return out
}

func (f *fakeMover) PickDestination(eni EniRef, excluded []string) string {
	if f.pickFn != nil {
		return f.pickFn(eni, excluded)
	}
	// Default: pick the first DPU we know about that isn't excluded.
	excl := map[string]struct{}{}
	for _, e := range excluded {
		excl[e] = struct{}{}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for dpu := range f.enisByDpu {
		if _, skip := excl[dpu]; !skip {
			return dpu
		}
	}
	return ""
}

func (f *fakeMover) Rehome(ctx context.Context, eni EniRef, dst string) error {
	cur := f.inFlight.Add(1)
	defer f.inFlight.Add(-1)
	for {
		peak := f.concurrentPeak.Load()
		if cur <= peak {
			break
		}
		if f.concurrentPeak.CompareAndSwap(peak, cur) {
			break
		}
	}
	if f.rehomeDelay > 0 {
		select {
		case <-time.After(f.rehomeDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if f.rehomeFn != nil {
		if err := f.rehomeFn(eni, dst); err != nil {
			return err
		}
	}
	f.mu.Lock()
	f.rehomes = append(f.rehomes, rehomeCall{eni: eni, dst: dst})
	f.mu.Unlock()
	return nil
}

// --- PC-G7 happy path: 5 ENIs migrate, source ends cordoned -----------

func TestDrain_5ENIs_HappyPath_PC_G7(t *testing.T) {
	mgr := New(newFakeInv("dpu-1", "dpu-2", "dpu-3"))
	mover := &fakeMover{
		enisByDpu: map[string][]EniRef{
			"dpu-1": {
				{Namespace: "default", Name: "eni-a"},
				{Namespace: "default", Name: "eni-b"},
				{Namespace: "default", Name: "eni-c"},
				{Namespace: "default", Name: "eni-d"},
				{Namespace: "default", Name: "eni-e"},
			},
			// dpu-2 and dpu-3 are empty destinations.
			"dpu-2": {},
			"dpu-3": {},
		},
	}
	res, err := mgr.Drain(context.Background(), "dpu-1", DrainOpts{Parallelism: 2, Reason: "PC-G7 test"}, mover)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if !res.Cordoned {
		t.Error("Cordoned=false; want true")
	}
	if res.TotalEnis != 5 {
		t.Errorf("TotalEnis=%d; want 5", res.TotalEnis)
	}
	if len(res.Migrated) != 5 {
		t.Errorf("Migrated=%d; want 5", len(res.Migrated))
	}
	if len(res.Failed) != 0 {
		t.Errorf("Failed=%v; want none", res.Failed)
	}
	if !mgr.IsCordoned("dpu-1") {
		t.Error("dpu-1 should remain cordoned after drain")
	}
	// Every ENI should have a non-source destination.
	for _, m := range res.Migrated {
		if m.From != "dpu-1" || m.To == "" || m.To == "dpu-1" {
			t.Errorf("bad migration row %+v", m)
		}
	}
}

func TestDrain_ParallelismCap(t *testing.T) {
	// 8 ENIs, parallelism=3, rehome takes 30ms. Concurrent peak must
	// be exactly 3 (no more, ideally not fewer when there's enough
	// work).
	mover := &fakeMover{enisByDpu: map[string][]EniRef{
		"src": {{"ns", "a"}, {"ns", "b"}, {"ns", "c"}, {"ns", "d"}, {"ns", "e"}, {"ns", "f"}, {"ns", "g"}, {"ns", "h"}},
		"dst": {},
	}, rehomeDelay: 30 * time.Millisecond, pickFn: func(_ EniRef, _ []string) string { return "dst" }}
	mgr := New(newFakeInv("src", "dst"))
	res, err := mgr.Drain(context.Background(), "src", DrainOpts{Parallelism: 3}, mover)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(res.Migrated) != 8 {
		t.Fatalf("migrated=%d; want 8", len(res.Migrated))
	}
	peak := mover.concurrentPeak.Load()
	if peak < 1 || peak > 3 {
		t.Errorf("concurrent peak=%d; want in [1, 3]", peak)
	}
	// At parallelism=3 with 8 ops × 30ms, the peak should be ≥2 to
	// prove we're actually running concurrent workers.
	if peak < 2 {
		t.Errorf("concurrent peak=%d; want >=2 to prove parallelism", peak)
	}
}

func TestDrain_NoDestination_AllFail(t *testing.T) {
	mgr := New(newFakeInv("only-dpu"))
	mover := &fakeMover{
		enisByDpu: map[string][]EniRef{
			"only-dpu": {{Namespace: "default", Name: "lonely"}},
		},
		pickFn: func(_ EniRef, _ []string) string { return "" },
	}
	res, err := mgr.Drain(context.Background(), "only-dpu", DrainOpts{Parallelism: 1}, mover)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(res.Migrated) != 0 {
		t.Errorf("Migrated=%d; want 0", len(res.Migrated))
	}
	if len(res.Failed) != 1 {
		t.Fatalf("Failed=%d; want 1", len(res.Failed))
	}
	if !mgr.IsCordoned("only-dpu") {
		t.Error("only-dpu should still be cordoned even with no destinations")
	}
}

func TestDrain_PerEniFailure_OthersContinue(t *testing.T) {
	mgr := New(newFakeInv("src", "dst"))
	mover := &fakeMover{
		enisByDpu: map[string][]EniRef{
			"src": {{Namespace: "default", Name: "a"}, {Namespace: "default", Name: "b"}, {Namespace: "default", Name: "c"}},
			"dst": {},
		},
		pickFn: func(_ EniRef, _ []string) string { return "dst" },
		rehomeFn: func(eni EniRef, _ string) error {
			if eni.Name == "b" {
				return errors.New("destination at capacity")
			}
			return nil
		},
	}
	res, err := mgr.Drain(context.Background(), "src", DrainOpts{Parallelism: 1}, mover)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(res.Migrated) != 2 {
		t.Errorf("Migrated=%d; want 2 (a and c)", len(res.Migrated))
	}
	if len(res.Failed) != 1 {
		t.Fatalf("Failed=%d; want 1 (b)", len(res.Failed))
	}
	if res.Failed[0].Name != "b" || res.Failed[0].Reason == "" {
		t.Errorf("Failed[0]=%+v; want {Name: b, Reason: ...}", res.Failed[0])
	}
}

func TestDrain_UnknownDPU(t *testing.T) {
	mgr := New(newFakeInv("dpu-1"))
	mover := &fakeMover{enisByDpu: map[string][]EniRef{}}
	_, err := mgr.Drain(context.Background(), "dpu-typo", DrainOpts{}, mover)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v; want ErrNotFound", err)
	}
}

func TestDrain_NilMover(t *testing.T) {
	mgr := New(newFakeInv("dpu-1"))
	_, err := mgr.Drain(context.Background(), "dpu-1", DrainOpts{}, nil)
	if err == nil {
		t.Error("want error for nil mover")
	}
}

func TestDrain_EmptyDPU_OK(t *testing.T) {
	mgr := New(newFakeInv("dpu-1", "dpu-2"))
	mover := &fakeMover{enisByDpu: map[string][]EniRef{"dpu-1": {}, "dpu-2": {}}}
	res, err := mgr.Drain(context.Background(), "dpu-1", DrainOpts{}, mover)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if res.TotalEnis != 0 || len(res.Migrated) != 0 || len(res.Failed) != 0 {
		t.Errorf("unexpected result on empty DPU: %+v", res)
	}
	if !mgr.IsCordoned("dpu-1") {
		t.Error("empty DPU should still end cordoned")
	}
}

func TestDrain_CtxCancelled_StopsForwardWork(t *testing.T) {
	mgr := New(newFakeInv("src", "dst"))
	mover := &fakeMover{
		enisByDpu: map[string][]EniRef{
			"src": {
				{Namespace: "default", Name: "a"},
				{Namespace: "default", Name: "b"},
				{Namespace: "default", Name: "c"},
			},
			"dst": {},
		},
		rehomeDelay: 100 * time.Millisecond,
		pickFn:      func(_ EniRef, _ []string) string { return "dst" },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	res, err := mgr.Drain(ctx, "src", DrainOpts{Parallelism: 1}, mover)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	// At least one ENI should have failed due to ctx cancellation.
	if len(res.Failed) == 0 {
		t.Errorf("Failed=0; want >=1 (ctx cancelled mid-drain). res=%+v", res)
	}
}

func TestDrain_DefaultParallelism(t *testing.T) {
	// opts.Parallelism=0 should default to 4 (D5). With 10 ENIs and
	// 20ms each, peak should be ≤4.
	mover := &fakeMover{enisByDpu: map[string][]EniRef{
		"src": func() []EniRef {
			out := make([]EniRef, 10)
			for i := range out {
				out[i] = EniRef{Namespace: "ns", Name: fmt.Sprintf("e%d", i)}
			}
			return out
		}(),
		"dst": {},
	}, rehomeDelay: 20 * time.Millisecond, pickFn: func(_ EniRef, _ []string) string { return "dst" }}
	mgr := New(newFakeInv("src", "dst"))
	res, err := mgr.Drain(context.Background(), "src", DrainOpts{}, mover)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(res.Migrated) != 10 {
		t.Errorf("Migrated=%d; want 10", len(res.Migrated))
	}
	if mover.concurrentPeak.Load() > 4 {
		t.Errorf("peak=%d; default parallelism cap is 4", mover.concurrentPeak.Load())
	}
}
