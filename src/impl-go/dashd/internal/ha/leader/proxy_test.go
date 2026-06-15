package leader

import (
	"context"
	"sync"
	"testing"
)

func TestProxy_InitialState(t *testing.T) {
	el := &NoneElector{NodeID: "node-1"}
	p := NewProxy(el)

	if !p.IsLeader() {
		t.Error("proxy should delegate IsLeader to NoneElector (always true)")
	}
	if got := p.LeaderID(); got != "node-1" {
		t.Errorf("LeaderID = %q, want %q", got, "node-1")
	}
	if got := p.Inner(); got != el {
		t.Error("Inner should return the initial elector")
	}
}

func TestProxy_NilInner(t *testing.T) {
	p := &LeaderProxy{}

	if p.IsLeader() {
		t.Error("nil inner should return IsLeader=false")
	}
	if got := p.LeaderID(); got != "" {
		t.Errorf("nil inner LeaderID = %q, want empty", got)
	}
	if p.Inner() != nil {
		t.Error("nil inner should return nil from Inner()")
	}
}

func TestProxy_Swap(t *testing.T) {
	el1 := &NoneElector{NodeID: "node-1"}
	el2 := &NoneElector{NodeID: "node-2"}
	p := NewProxy(el1)

	if got := p.LeaderID(); got != "node-1" {
		t.Errorf("before swap LeaderID = %q, want %q", got, "node-1")
	}

	p.Swap(el2)

	if got := p.LeaderID(); got != "node-2" {
		t.Errorf("after swap LeaderID = %q, want %q", got, "node-2")
	}
	if got := p.Inner(); got != el2 {
		t.Error("Inner should return the swapped elector")
	}
}

func TestProxy_SwapToNil(t *testing.T) {
	el := &NoneElector{NodeID: "node-1"}
	p := NewProxy(el)
	p.Swap(nil)

	if p.IsLeader() {
		t.Error("swap to nil should return IsLeader=false")
	}
	if got := p.LeaderID(); got != "" {
		t.Errorf("swap to nil LeaderID = %q, want empty", got)
	}
}

func TestProxy_IsLeaderDelegates(t *testing.T) {
	el := &NoneElector{NodeID: "node-1"}
	p := NewProxy(el)

	if !p.IsLeader() {
		t.Error("should be leader via NoneElector")
	}

	// Close the NoneElector — IsLeader should flip.
	_ = el.Close()

	if p.IsLeader() {
		t.Error("should not be leader after NoneElector closed")
	}
}

func TestProxy_ConcurrentAccess(t *testing.T) {
	p := NewProxy(&NoneElector{NodeID: "a"})
	var wg sync.WaitGroup

	// Concurrent readers.
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.IsLeader()
			_ = p.LeaderID()
			_ = p.Inner()
		}()
	}

	// Concurrent swaps.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			p.Swap(&NoneElector{NodeID: "node-" + string(rune('a'+n%26))})
		}(i)
	}

	wg.Wait()
}

func TestProxy_AwaitAndLostViaInner(t *testing.T) {
	el := &NoneElector{NodeID: "node-1"}
	p := NewProxy(el)

	// AwaitLeadership on Inner() works.
	ctx := context.Background()
	inner := p.Inner()
	if err := inner.AwaitLeadership(ctx); err != nil {
		t.Errorf("AwaitLeadership via Inner: %v", err)
	}

	// LostLeadership channel is accessible.
	ch := inner.LostLeadership()
	select {
	case <-ch:
		t.Error("NoneElector LostLeadership should not fire before Close")
	default:
		// expected
	}
}
