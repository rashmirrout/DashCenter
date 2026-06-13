// Tests for cluster.Broadcaster.
package cluster

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

func TestBroadcaster_FanOut(t *testing.T) {
	b := NewBroadcaster()
	chA, cancelA := b.Subscribe()
	chB, cancelB := b.Subscribe()
	defer cancelA()
	defer cancelB()

	b.Publish(&dashcenterv1.TopologyEvent{Kind: dashcenterv1.TopologyEvent_KIND_PEER_ADDED})
	b.Publish(&dashcenterv1.TopologyEvent{Kind: dashcenterv1.TopologyEvent_KIND_LEADER_CHANGED})

	for i, ch := range []<-chan *dashcenterv1.TopologyEvent{chA, chB} {
		for j := 0; j < 2; j++ {
			select {
			case ev := <-ch:
				if ev == nil {
					t.Errorf("sub %d got nil event", i)
				}
			case <-time.After(500 * time.Millisecond):
				t.Errorf("sub %d did not receive event %d", i, j)
			}
		}
	}

	stats := b.Stats()
	if stats.Subscribers != 2 || stats.TotalSent != 4 || stats.TotalDrop != 0 {
		t.Errorf("stats = %+v; want subs=2 sent=4 drop=0", stats)
	}
}

func TestBroadcaster_DropOnSlowSubscriber(t *testing.T) {
	b := NewBroadcaster()
	_, cancel := b.Subscribe() // never drain
	defer cancel()

	// Publish way more than the buffer (64).
	const N = 200
	for i := 0; i < N; i++ {
		b.Publish(&dashcenterv1.TopologyEvent{Kind: dashcenterv1.TopologyEvent_KIND_PEER_ADDED})
	}
	stats := b.Stats()
	if stats.TotalDrop == 0 {
		t.Error("expected drops on stuck subscriber")
	}
	if stats.TotalSent+stats.TotalDrop != N {
		t.Errorf("sent+drop = %d+%d; want %d", stats.TotalSent, stats.TotalDrop, N)
	}
}

func TestBroadcaster_CancelCleansUp(t *testing.T) {
	b := NewBroadcaster()
	ch, cancel := b.Subscribe()
	if b.Stats().Subscribers != 1 {
		t.Fatal("subscribe didn't register")
	}
	cancel()
	if b.Stats().Subscribers != 0 {
		t.Errorf("cancel didn't release: subs = %d", b.Stats().Subscribers)
	}
	// Second cancel should be a safe no-op.
	cancel()
	// Channel is closed.
	if _, ok := <-ch; ok {
		t.Error("channel should be closed after cancel")
	}
}

func TestBroadcaster_Concurrent(t *testing.T) {
	b := NewBroadcaster()
	const (
		nSubs    = 10
		perPub   = 100
	)
	var wg sync.WaitGroup
	var received atomic.Uint64

	for i := 0; i < nSubs; i++ {
		ch, cancel := b.Subscribe()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			for ev := range ch {
				_ = ev
				received.Add(1)
			}
		}()
	}

	// One publisher per goroutine.
	var pubWG sync.WaitGroup
	for i := 0; i < 4; i++ {
		pubWG.Add(1)
		go func() {
			defer pubWG.Done()
			for j := 0; j < perPub; j++ {
				b.Publish(&dashcenterv1.TopologyEvent{Kind: dashcenterv1.TopologyEvent_KIND_DPU_STATE})
			}
		}()
	}
	pubWG.Wait()

	// Drain — wait for in-flight events to be received, then cancel all
	// subs (the deferred cancels close the channels, ending the
	// consumer goroutines).
	time.Sleep(100 * time.Millisecond)
	stats := b.Stats()
	if stats.TotalSent+stats.TotalDrop != 4*perPub*nSubs {
		t.Errorf("sent+drop = %d+%d; want %d", stats.TotalSent, stats.TotalDrop, 4*perPub*nSubs)
	}
	// Note: we don't assert received == TotalSent because the
	// consumer goroutines may still be draining; this test is about
	// thread-safety + accounting, not delivery determinism.
}

func TestBroadcaster_NilPublishIgnored(t *testing.T) {
	b := NewBroadcaster()
	ch, cancel := b.Subscribe()
	defer cancel()
	b.Publish(nil)
	select {
	case <-ch:
		t.Error("nil event should not fan out")
	case <-time.After(100 * time.Millisecond):
		// expected: nothing arrived
	}
}
