package leader

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestNoneElector_ZeroValueUsable verifies the zero value works without an
// explicit constructor — important because cmd/dashd/main.go uses a
// composite literal for clarity.
func TestNoneElector_ZeroValueUsable(t *testing.T) {
	var e NoneElector

	if err := e.AwaitLeadership(context.Background()); err != nil {
		t.Fatalf("AwaitLeadership on zero value: %v; want nil", err)
	}
	if !e.IsLeader() {
		t.Fatal("zero-value NoneElector reports IsLeader=false")
	}
	if e.LeaderID() != "" {
		t.Fatalf("zero-value LeaderID=%q; want \"\"", e.LeaderID())
	}
	if e.LostLeadership() == nil {
		t.Fatal("LostLeadership returned nil channel")
	}
}

// TestNoneElector_AwaitImmediate proves AwaitLeadership returns nil without
// blocking when the context is alive. This is the property that makes the
// leaderLoop in main.go a no-op for single-node dashd.
func TestNoneElector_AwaitImmediate(t *testing.T) {
	e := &NoneElector{NodeID: "dashd-test"}
	start := time.Now()
	if err := e.AwaitLeadership(context.Background()); err != nil {
		t.Fatalf("AwaitLeadership: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Millisecond {
		t.Fatalf("AwaitLeadership blocked for %s; should be ~immediate", elapsed)
	}
	if e.LeaderID() != "dashd-test" {
		t.Fatalf("LeaderID=%q; want dashd-test", e.LeaderID())
	}
}

// TestNoneElector_AwaitRespectsContextCancel guards against a future
// refactor accidentally hard-coding context.Background.
func TestNoneElector_AwaitRespectsContextCancel(t *testing.T) {
	e := &NoneElector{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel BEFORE the call so we test the select path

	err := e.AwaitLeadership(ctx)
	if err != context.Canceled {
		t.Fatalf("AwaitLeadership with cancelled ctx returned %v; want context.Canceled", err)
	}
}

// TestNoneElector_LostNeverFiresUntilClose is the central correctness
// property of NoneElector — the leaderLoop in main.go select-waits on this
// channel, and for single-node dashd it must never wake until shutdown.
func TestNoneElector_LostNeverFiresUntilClose(t *testing.T) {
	e := &NoneElector{}
	lost := e.LostLeadership()

	select {
	case <-lost:
		t.Fatal("LostLeadership fired without Close")
	case <-time.After(20 * time.Millisecond):
		// Expected — channel must not fire spontaneously.
	}

	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-lost:
		// Expected — Close must wake the channel so leaderLoop can tear down.
	case <-time.After(50 * time.Millisecond):
		t.Fatal("LostLeadership did not fire after Close")
	}
}

// TestNoneElector_CloseFlipsIsLeader documents the post-Close contract.
func TestNoneElector_CloseFlipsIsLeader(t *testing.T) {
	e := &NoneElector{}
	if !e.IsLeader() {
		t.Fatal("IsLeader=false before Close")
	}
	_ = e.Close()
	if e.IsLeader() {
		t.Fatal("IsLeader=true after Close; want false")
	}
}

// TestNoneElector_CloseAwaitsReturnCanceled — a caller that calls
// AwaitLeadership AFTER Close gets context.Canceled, not a spurious nil.
func TestNoneElector_CloseAwaitsReturnCanceled(t *testing.T) {
	e := &NoneElector{}
	_ = e.Close()
	if err := e.AwaitLeadership(context.Background()); err != context.Canceled {
		t.Fatalf("AwaitLeadership after Close returned %v; want context.Canceled", err)
	}
}

// TestNoneElector_CloseIsIdempotent guards against panic-on-double-close.
func TestNoneElector_CloseIsIdempotent(t *testing.T) {
	e := &NoneElector{}
	if err := e.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestNoneElector_ConcurrentInit proves the lazy-init race guard works:
// many goroutines calling AwaitLeadership / LostLeadership simultaneously
// on a fresh zero-value elector must all succeed without panic or a nil
// channel.
func TestNoneElector_ConcurrentInit(t *testing.T) {
	const n = 64
	var e NoneElector
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e.LostLeadership() == nil {
				t.Errorf("LostLeadership returned nil")
				return
			}
			if err := e.AwaitLeadership(context.Background()); err != nil {
				t.Errorf("AwaitLeadership: %v", err)
			}
			if !e.IsLeader() {
				t.Errorf("IsLeader=false")
			}
		}()
	}
	wg.Wait()
}

// TestNoneElector_SatisfiesInterface ensures NoneElector implements the
// Elector interface; this catches a missing method at compile time before
// main.go fails to build.
func TestNoneElector_SatisfiesInterface(t *testing.T) {
	var _ Elector = (*NoneElector)(nil)
}
