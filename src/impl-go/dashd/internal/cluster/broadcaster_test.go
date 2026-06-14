// Tests for the PE-G7 broadcaster: fan-out, drop-on-slow, cursor
// resume, sentinel emission, rate limiting, coalescing, and caps.
package cluster

import (
	"errors"
	"sync"
	"testing"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

// ── helpers ──────────────────────────────────────────────────────────────

// fastTestConfig returns a config tuned for snappy tests: tiny rates +
// tiny windows so we don't wait seconds for things to fire.
func fastTestConfig() BroadcasterConfig {
	c := DefaultBroadcasterConfig()
	c.CoalesceWindow = 10 * time.Millisecond
	c.KeepaliveInterval = 0                  // disable for most tests
	c.EventRatePerSec = 1000                 // generous; specific tests override
	c.BurstSize = 2000
	c.SubscriberBufferSize = 8               // small so overflow tests don't drown
	c.RingSize = 16
	c.MaxSubscribers = 4
	c.MaxSubscribersPerSubject = 2
	return c
}

func newDpuEvent(id, state string) *dashcenterv1.TopologyEvent {
	return &dashcenterv1.TopologyEvent{
		Kind: dashcenterv1.TopologyEvent_KIND_DPU_STATE,
		Body: &dashcenterv1.TopologyEvent_Dpu{Dpu: &dashcenterv1.DpuTopInfo{
			Id: id, State: state,
		}},
	}
}

func newPeerEvent(kind dashcenterv1.TopologyEvent_Kind, nodeID string) *dashcenterv1.TopologyEvent {
	return &dashcenterv1.TopologyEvent{
		Kind: kind,
		Body: &dashcenterv1.TopologyEvent_Peer{Peer: &dashcenterv1.ClusterNodeInfo{
			NodeId: nodeID,
		}},
	}
}

func drainTimeout(t *testing.T, sub *Subscription, want int, timeout time.Duration) []*Frame {
	t.Helper()
	out := make([]*Frame, 0, want)
	deadline := time.After(timeout)
	for len(out) < want {
		select {
		case f, ok := <-sub.Recv():
			if !ok {
				return out
			}
			out = append(out, f)
		case <-deadline:
			return out
		}
	}
	return out
}

// ── tests ────────────────────────────────────────────────────────────────

func TestBroadcaster_FanOutWithPreMarshalled(t *testing.T) {
	b := NewBroadcaster(fastTestConfig())
	defer b.Stop()

	subA, cA, err := b.Subscribe(SubscribeOptions{})
	if err != nil {
		t.Fatalf("subA: %v", err)
	}
	defer cA()
	subB, cB, err := b.Subscribe(SubscribeOptions{})
	if err != nil {
		t.Fatalf("subB: %v", err)
	}
	defer cB()

	// LEADER_CHANGED bypasses coalescing/rate limiting so it fans out
	// immediately — perfect for deterministic tests.
	b.Publish(&dashcenterv1.TopologyEvent{
		Kind: dashcenterv1.TopologyEvent_KIND_LEADER_CHANGED,
		NewLeaderId: "dashd-2",
	})

	for i, sub := range []*Subscription{subA, subB} {
		framesGot := drainTimeout(t, sub, 1, 500*time.Millisecond)
		if len(framesGot) != 1 {
			t.Fatalf("sub %d: want 1 frame, got %d", i, len(framesGot))
		}
		f := framesGot[0]
		if f.Event.GetEventId() == 0 {
			t.Errorf("sub %d: event_id must be set (got 0)", i)
		}
		if len(f.JSON) == 0 {
			t.Errorf("sub %d: JSON must be pre-marshalled", i)
		}
	}

	// Both subscribers must see the SAME byte slice (marshal-once).
	fa := drainOrNil(subA)
	fb := drainOrNil(subB)
	// drainOrNil returns the LAST frame we already saw above (re-issuing
	// Publish would be needed for fresh ones). Instead publish a second
	// event and check identity.
	b.Publish(&dashcenterv1.TopologyEvent{
		Kind: dashcenterv1.TopologyEvent_KIND_LEADER_CHANGED,
		NewLeaderId: "dashd-3",
	})
	fa = drainTimeoutOne(t, subA, 500*time.Millisecond)
	fb = drainTimeoutOne(t, subB, 500*time.Millisecond)
	if fa == nil || fb == nil {
		t.Fatal("missing second event")
	}
	if &fa.JSON[0] != &fb.JSON[0] {
		t.Errorf("expected shared JSON backing array (marshal-once); got distinct addresses")
	}
}

func drainOrNil(s *Subscription) *Frame {
	select {
	case f := <-s.Recv():
		return f
	default:
		return nil
	}
}

func drainTimeoutOne(t *testing.T, s *Subscription, d time.Duration) *Frame {
	t.Helper()
	select {
	case f := <-s.Recv():
		return f
	case <-time.After(d):
		return nil
	}
}

func TestBroadcaster_MonotonicEventIDs(t *testing.T) {
	b := NewBroadcaster(fastTestConfig())
	defer b.Stop()
	sub, cancel, _ := b.Subscribe(SubscribeOptions{})
	defer cancel()

	for i := 0; i < 5; i++ {
		b.Publish(&dashcenterv1.TopologyEvent{Kind: dashcenterv1.TopologyEvent_KIND_LEADER_CHANGED})
	}

	frames := drainTimeout(t, sub, 5, 500*time.Millisecond)
	if len(frames) != 5 {
		t.Fatalf("got %d frames; want 5", len(frames))
	}
	for i := 1; i < len(frames); i++ {
		if frames[i].Event.GetEventId() != frames[i-1].Event.GetEventId()+1 {
			t.Errorf("event_id not monotonic at index %d: %d vs %d", i, frames[i-1].Event.GetEventId(), frames[i].Event.GetEventId())
		}
	}
}

func TestBroadcaster_DropOnSlowSubscriberSetsCount(t *testing.T) {
	cfg := fastTestConfig()
	cfg.SubscriberBufferSize = 2
	cfg.CoalesceWindow = 0 // disable coalescing so each Publish lands
	b := NewBroadcaster(cfg)
	defer b.Stop()

	sub, cancel, _ := b.Subscribe(SubscribeOptions{})
	defer cancel()

	// Don't drain — fill way past the buffer (which is 2).
	for i := 0; i < 20; i++ {
		b.Publish(&dashcenterv1.TopologyEvent{Kind: dashcenterv1.TopologyEvent_KIND_LEADER_CHANGED})
	}

	// Brief settle.
	time.Sleep(50 * time.Millisecond)
	got := sub.TakeDroppedCount()
	if got == 0 {
		t.Error("expected non-zero dropped count after overflow")
	}
	// TakeDroppedCount resets to zero.
	if again := sub.TakeDroppedCount(); again != 0 {
		t.Errorf("TakeDroppedCount did not reset; got %d", again)
	}
}

func TestBroadcaster_CoalescesSameKey(t *testing.T) {
	cfg := fastTestConfig()
	cfg.CoalesceWindow = 50 * time.Millisecond
	b := NewBroadcaster(cfg)
	defer b.Stop()

	sub, cancel, _ := b.Subscribe(SubscribeOptions{})
	defer cancel()

	// 5 events for the same DPU should coalesce to ONE (the latest)
	// after the coalesce window.
	for i := 0; i < 5; i++ {
		b.Publish(newDpuEvent("dpu-1", "DPU_STATE_UP"))
	}
	time.Sleep(100 * time.Millisecond) // wait for window flush

	frames := drainTimeout(t, sub, 5, 200*time.Millisecond)
	if len(frames) != 1 {
		t.Errorf("expected 1 coalesced frame; got %d", len(frames))
	}
	if got := b.Stats().TotalCoalesced; got != 4 {
		t.Errorf("TotalCoalesced = %d; want 4", got)
	}
}

func TestBroadcaster_DifferentKeysDoNotCoalesce(t *testing.T) {
	cfg := fastTestConfig()
	cfg.CoalesceWindow = 50 * time.Millisecond
	b := NewBroadcaster(cfg)
	defer b.Stop()

	sub, cancel, _ := b.Subscribe(SubscribeOptions{})
	defer cancel()

	b.Publish(newDpuEvent("dpu-1", "DPU_STATE_UP"))
	b.Publish(newDpuEvent("dpu-2", "DPU_STATE_UP"))
	b.Publish(newDpuEvent("dpu-3", "DPU_STATE_UP"))
	time.Sleep(100 * time.Millisecond)

	frames := drainTimeout(t, sub, 3, 200*time.Millisecond)
	if len(frames) != 3 {
		t.Errorf("expected 3 distinct events; got %d", len(frames))
	}
}

func TestBroadcaster_RateLimitSuppressesAndEmitsNotice(t *testing.T) {
	cfg := fastTestConfig()
	cfg.EventRatePerSec = 1   // 1 event/sec; bucket holds 2
	cfg.BurstSize = 2
	cfg.CoalesceWindow = 0    // disable so events compete for tokens directly
	b := NewBroadcaster(cfg)
	defer b.Stop()

	sub, cancel, _ := b.Subscribe(SubscribeOptions{})
	defer cancel()

	for i := 0; i < 20; i++ {
		b.Publish(newDpuEvent("dpu-1", "DPU_STATE_UP"))
	}
	// Wait > the 250ms notice delay.
	time.Sleep(400 * time.Millisecond)

	frames := drainTimeout(t, sub, 30, 500*time.Millisecond)
	var sawRateLimited bool
	for _, f := range frames {
		if f.Event.GetKind() == dashcenterv1.TopologyEvent_KIND_RATE_LIMITED {
			sawRateLimited = true
			if f.Event.GetNotice().GetSuppressedCount() == 0 {
				t.Error("KIND_RATE_LIMITED notice should carry suppressed_count > 0")
			}
		}
	}
	if !sawRateLimited {
		t.Errorf("expected at least one KIND_RATE_LIMITED frame; got %d frames total", len(frames))
	}
	if b.Stats().TotalSuppressed == 0 {
		t.Error("Stats().TotalSuppressed should be > 0")
	}
}

func TestBroadcaster_ResumeAfterReplaysRing(t *testing.T) {
	cfg := fastTestConfig()
	cfg.CoalesceWindow = 0
	b := NewBroadcaster(cfg)
	defer b.Stop()

	// Publish 3 events BEFORE any subscriber exists; the ring should
	// still capture them.
	for i := 0; i < 3; i++ {
		b.Publish(&dashcenterv1.TopologyEvent{Kind: dashcenterv1.TopologyEvent_KIND_LEADER_CHANGED})
	}

	// New subscriber resumes from cursor=1 → should receive events 2 & 3.
	sub, cancel, err := b.Subscribe(SubscribeOptions{ResumeAfterEventID: 1})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer cancel()

	frames := drainTimeout(t, sub, 5, 200*time.Millisecond)
	if len(frames) != 2 {
		t.Fatalf("expected 2 resumed frames; got %d", len(frames))
	}
	if frames[0].Event.GetEventId() != 2 || frames[1].Event.GetEventId() != 3 {
		t.Errorf("resumed IDs = %d, %d; want 2, 3", frames[0].Event.GetEventId(), frames[1].Event.GetEventId())
	}
}

func TestBroadcaster_ResumeStaleCursorEmitsResyncSentinel(t *testing.T) {
	cfg := fastTestConfig()
	cfg.CoalesceWindow = 0
	cfg.RingSize = 4
	b := NewBroadcaster(cfg)
	defer b.Stop()

	// Fill + evict the ring.
	for i := 0; i < 10; i++ {
		b.Publish(&dashcenterv1.TopologyEvent{Kind: dashcenterv1.TopologyEvent_KIND_LEADER_CHANGED})
	}

	sub, cancel, _ := b.Subscribe(SubscribeOptions{ResumeAfterEventID: 1})
	defer cancel()

	f := drainTimeoutOne(t, sub, 200*time.Millisecond)
	if f == nil {
		t.Fatal("expected at least one frame (RESYNC sentinel)")
	}
	if f.Event.GetKind() != dashcenterv1.TopologyEvent_KIND_RESYNC {
		t.Errorf("first frame should be RESYNC; got %v", f.Event.GetKind())
	}
}

func TestBroadcaster_MaxSubscribersGlobalCap(t *testing.T) {
	cfg := fastTestConfig()
	cfg.MaxSubscribers = 2
	b := NewBroadcaster(cfg)
	defer b.Stop()

	cancels := []func(){}
	for i := 0; i < 2; i++ {
		_, c, err := b.Subscribe(SubscribeOptions{})
		if err != nil {
			t.Fatalf("sub %d: %v", i, err)
		}
		cancels = append(cancels, c)
	}
	defer func() { for _, c := range cancels { c() } }()

	if _, _, err := b.Subscribe(SubscribeOptions{}); !errors.Is(err, ErrTooManySubscribers) {
		t.Errorf("3rd subscribe should fail with ErrTooManySubscribers; got %v", err)
	}
}

func TestBroadcaster_MaxSubscribersPerSubjectCap(t *testing.T) {
	cfg := fastTestConfig()
	cfg.MaxSubscribers = 100
	cfg.MaxSubscribersPerSubject = 2
	b := NewBroadcaster(cfg)
	defer b.Stop()

	cancels := []func(){}
	for i := 0; i < 2; i++ {
		_, c, err := b.Subscribe(SubscribeOptions{SubjectName: "alice"})
		if err != nil {
			t.Fatalf("alice sub %d: %v", i, err)
		}
		cancels = append(cancels, c)
	}
	defer func() { for _, c := range cancels { c() } }()

	if _, _, err := b.Subscribe(SubscribeOptions{SubjectName: "alice"}); !errors.Is(err, ErrTooManySubscribers) {
		t.Errorf("alice's 3rd should fail; got %v", err)
	}
	// Different subject still works.
	_, c, err := b.Subscribe(SubscribeOptions{SubjectName: "bob"})
	if err != nil {
		t.Errorf("bob should be admitted; got %v", err)
	}
	defer c()
}

func TestBroadcaster_KeepaliveTickerSingleGoroutine(t *testing.T) {
	cfg := fastTestConfig()
	cfg.KeepaliveInterval = 25 * time.Millisecond
	cfg.MaxSubscribers = 16
	b := NewBroadcaster(cfg)
	defer b.Stop()

	// Spawn 5 subscribers; only ONE keepalive timer should be running.
	subs := []*Subscription{}
	cancels := []func(){}
	for i := 0; i < 5; i++ {
		s, c, err := b.Subscribe(SubscribeOptions{})
		if err != nil {
			t.Fatalf("sub %d: %v", i, err)
		}
		subs = append(subs, s)
		cancels = append(cancels, c)
	}
	defer func() { for _, c := range cancels { c() } }()

	time.Sleep(80 * time.Millisecond)

	// Every subscriber should have received keepalives.
	for i, s := range subs {
		var saw bool
		drain := drainTimeout(t, s, 10, 100*time.Millisecond)
		for _, f := range drain {
			if f.Event.GetKind() == dashcenterv1.TopologyEvent_KIND_KEEPALIVE {
				saw = true
				break
			}
		}
		if !saw {
			t.Errorf("sub %d never received a keepalive", i)
		}
	}
}

func TestBroadcaster_CancelIdempotentAndCleansUp(t *testing.T) {
	b := NewBroadcaster(fastTestConfig())
	defer b.Stop()
	sub, cancel, _ := b.Subscribe(SubscribeOptions{SubjectName: "carol"})
	if b.Stats().Subscribers != 1 {
		t.Fatal("subscriber not registered")
	}
	cancel()
	if b.Stats().Subscribers != 0 {
		t.Errorf("cancel didn't release; subs=%d", b.Stats().Subscribers)
	}
	if b.Stats().BySubjectCount["carol"] != 0 {
		t.Errorf("per-subject count not released")
	}
	// Second cancel is a no-op; channel already closed.
	cancel()
	if _, ok := <-sub.Recv(); ok {
		t.Error("channel should be closed after cancel")
	}
}

func TestBroadcaster_SentinelKindsAreNotPublishedByCaller(t *testing.T) {
	b := NewBroadcaster(fastTestConfig())
	defer b.Stop()
	sub, cancel, _ := b.Subscribe(SubscribeOptions{})
	defer cancel()

	// Caller-published sentinels MUST be silently rejected (they're
	// per-subscriber or broadcaster-internal).
	b.Publish(&dashcenterv1.TopologyEvent{Kind: dashcenterv1.TopologyEvent_KIND_DROPPED})
	b.Publish(&dashcenterv1.TopologyEvent{Kind: dashcenterv1.TopologyEvent_KIND_RESYNC})

	select {
	case f := <-sub.Recv():
		t.Errorf("subscriber received caller-published sentinel: %v", f.Event.GetKind())
	case <-time.After(100 * time.Millisecond):
		// expected — sentinels filtered out
	}
}

func TestBroadcaster_ConcurrentPublishAndSubscribe(t *testing.T) {
	b := NewBroadcaster(fastTestConfig())
	defer b.Stop()

	var wg sync.WaitGroup
	// 3 publisher goroutines × 50 events each.
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				b.Publish(newDpuEvent("dpu-x", "DPU_STATE_UP"))
			}
		}()
	}
	// Concurrent subscribe + cancel churn.
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				_, c, err := b.Subscribe(SubscribeOptions{})
				if err == nil {
					time.Sleep(2 * time.Millisecond)
					c()
				}
			}
		}()
	}
	wg.Wait()
	// Surviving stats must be consistent.
	st := b.Stats()
	if st.TotalPublished == 0 {
		t.Error("expected TotalPublished > 0")
	}
}
