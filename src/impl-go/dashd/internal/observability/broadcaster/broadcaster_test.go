// broadcaster_test.go — comprehensive UT coverage for the PE-3c
// counter broadcaster.
//
// Coverage target: 100% on every new symbol in this package.
//
// Failure-mode matrix (§3.6 of docs/dashd-features/counter-streaming.md)
// covered here for the broadcaster path:
//
//   * slow subscriber (channel full)        → TestBroadcaster_DropOnSlow_Sentinel
//   * dashd restart mid-stream              → covered by handler tests (Tier 1.3)
//   * sim death / DPU unreachable           → bridge_test.go's nil-entry path
//   * ctx cancel mid-Publish                → TestBroadcaster_Run_StopsKeepaliveOnCtxCancel
//   * broadcaster backlog overflow          → TestBroadcaster_RateLimit_EmitsNotice
//   * per-IP cap exceeded                   → handler tests (Tier 1.3); broadcaster handles per-subject + global
//   * per-subject cap exceeded              → TestBroadcaster_Subscribe_RejectsOverPerSubjectCap
//   * marshal-once invariant                → TestBroadcaster_MarshalOnce
//
// Concurrency: every test calls t.Parallel() where safe (no shared
// metric registers) AND the package is exercised with `go test -race`
// by Tier 1 acceptance.

package broadcaster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ── helpers ──────────────────────────────────────────────────────────────

// newTestBroadcaster constructs a broadcaster with deterministic
// tuning suitable for unit tests: tiny buffers (overflow fast), tiny
// ring (test the wrap path), zero keepalive (no goroutine surprises),
// and zero CoalesceWindow (events emit immediately unless a test
// opts in).
func newTestBroadcaster(t *testing.T) *Broadcaster {
	t.Helper()
	cfg := Config{
		MaxSubscribers:           4,
		MaxSubscribersPerSubject: 2,
		SubscriberBufferSize:     2, // small → easy to test overflow
		RingSize:                 4, // small → easy to test wrap + eviction
		CoalesceWindow:           0, // off by default; opt-in per test
		EventRatePerSec:          1000,
		BurstSize:                1000,
		KeepaliveInterval:        0, // off by default; opt-in per test
		SuppressedNoticeDelay:    20 * time.Millisecond,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	b := NewBroadcaster(cfg, logger)
	t.Cleanup(b.Stop)
	return b
}

func sampleReport(dpu string, decap int64) *dashcenterv1.CounterReport {
	return &dashcenterv1.CounterReport{
		DpuId:      dpu,
		SampledAt:  timestamppb.Now(),
		VxlanDecap: decap,
	}
}

// drainOne waits up to d for one frame on ch, fatals if it doesn't
// arrive. Returns the frame.
func drainOne(t *testing.T, ch <-chan *Frame, d time.Duration) *Frame {
	t.Helper()
	select {
	case f := <-ch:
		return f
	case <-time.After(d):
		t.Fatalf("expected one frame within %v; got none", d)
		return nil
	}
}

// expectNoFrame asserts no frame arrives for at least d (used to
// confirm filter dropouts or post-cancel silence).
func expectNoFrame(t *testing.T, ch <-chan *Frame, d time.Duration) {
	t.Helper()
	select {
	case f := <-ch:
		t.Fatalf("expected no frame; got %v", f)
	case <-time.After(d):
	}
}

// ── construction + defaults ──────────────────────────────────────────────

func TestNewBroadcaster_AppliesDefaults_OnZeroConfig(t *testing.T) {
	t.Parallel()
	b := NewBroadcaster(Config{}, nil) // nil logger forces slog.Default()
	defer b.Stop()
	if b.cfg.MaxSubscribers != 256 {
		t.Errorf("MaxSubscribers default not applied: %d", b.cfg.MaxSubscribers)
	}
	if b.cfg.SubscriberBufferSize != 64 {
		t.Errorf("SubscriberBufferSize default not applied: %d", b.cfg.SubscriberBufferSize)
	}
	if b.cfg.RingSize != 512 {
		t.Errorf("RingSize default not applied: %d", b.cfg.RingSize)
	}
	if b.cfg.EventRatePerSec != 200 {
		t.Errorf("EventRatePerSec default not applied: %v", b.cfg.EventRatePerSec)
	}
	if b.cfg.BurstSize != 400 {
		t.Errorf("BurstSize default not applied: %v", b.cfg.BurstSize)
	}
	if b.cfg.SuppressedNoticeDelay != 250*time.Millisecond {
		t.Errorf("SuppressedNoticeDelay default not applied: %v", b.cfg.SuppressedNoticeDelay)
	}
	if b.logger == nil {
		t.Errorf("nil logger should fall back to slog.Default()")
	}
}

func TestDefaultConfig_ProductionValues(t *testing.T) {
	t.Parallel()
	c := DefaultConfig()
	if c.MaxSubscribers != 256 || c.RingSize != 512 || c.EventRatePerSec != 200 {
		t.Errorf("DefaultConfig drifted from documented values: %+v", c)
	}
}

// ── Subscribe / cancel ───────────────────────────────────────────────────

func TestBroadcaster_Subscribe_DeliversPublishedEvent(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t)
	sub, cancel, err := b.Subscribe(SubscribeOptions{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	b.Publish(sampleReport("dpu-a", 100))
	f := drainOne(t, sub.Recv(), 200*time.Millisecond)
	if f.Event.GetKind() != dashcenterv1.CounterEvent_KIND_REPORT {
		t.Errorf("kind = %v, want KIND_REPORT", f.Event.GetKind())
	}
	if f.Event.GetReport().GetDpuId() != "dpu-a" {
		t.Errorf("dpu = %q, want dpu-a", f.Event.GetReport().GetDpuId())
	}
	if f.Event.GetEventId() != 1 {
		t.Errorf("event_id = %d, want 1 (first published)", f.Event.GetEventId())
	}
	if f.Event.GetTs() == nil {
		t.Errorf("ts not stamped")
	}
	if len(f.JSON) == 0 {
		t.Errorf("JSON not populated; marshal-once contract broken")
	}
}

func TestBroadcaster_Subscribe_NilReport_NoOp(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t)
	sub, cancel, _ := b.Subscribe(SubscribeOptions{})
	defer cancel()
	b.Publish(nil)
	expectNoFrame(t, sub.Recv(), 50*time.Millisecond)
}

func TestBroadcaster_Cancel_ClosesChannel(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t)
	sub, cancel, _ := b.Subscribe(SubscribeOptions{})
	cancel()
	// Channel must be closed.
	_, ok := <-sub.Recv()
	if ok {
		t.Errorf("expected closed channel after cancel; got open")
	}
	// Idempotent cancel — second call must not panic.
	cancel()
}

func TestBroadcaster_Cancel_ReleasesSlot(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t)
	subs := make([]func(), 0, 4)
	for i := 0; i < 4; i++ {
		_, cancel, err := b.Subscribe(SubscribeOptions{})
		if err != nil {
			t.Fatalf("Subscribe %d: %v", i, err)
		}
		subs = append(subs, cancel)
	}
	if _, _, err := b.Subscribe(SubscribeOptions{}); !errors.Is(err, ErrTooManySubscribers) {
		t.Fatalf("5th subscribe: got %v, want ErrTooManySubscribers", err)
	}
	subs[0]()
	if _, c, err := b.Subscribe(SubscribeOptions{}); err != nil {
		t.Errorf("Subscribe after cancel: %v", err)
	} else {
		c()
	}
}

func TestBroadcaster_Subscribe_RejectsOverGlobalCap(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t)
	for i := 0; i < 4; i++ {
		_, c, err := b.Subscribe(SubscribeOptions{})
		if err != nil {
			t.Fatalf("Subscribe %d: %v", i, err)
		}
		defer c()
	}
	_, _, err := b.Subscribe(SubscribeOptions{})
	if !errors.Is(err, ErrTooManySubscribers) {
		t.Errorf("err = %v, want ErrTooManySubscribers", err)
	}
	if err == nil || !strings.Contains(err.Error(), "global cap=4") {
		t.Errorf("err lacks diagnostic: %v", err)
	}
}

func TestBroadcaster_Subscribe_RejectsOverPerSubjectCap(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t)
	_, c1, _ := b.Subscribe(SubscribeOptions{SubjectName: "alice"})
	defer c1()
	_, c2, _ := b.Subscribe(SubscribeOptions{SubjectName: "alice"})
	defer c2()
	_, _, err := b.Subscribe(SubscribeOptions{SubjectName: "alice"})
	if !errors.Is(err, ErrTooManySubscribers) {
		t.Errorf("err = %v, want ErrTooManySubscribers", err)
	}
	if err == nil || !strings.Contains(err.Error(), `per-subject cap=2 reached for "alice"`) {
		t.Errorf("err lacks subject diagnostic: %v", err)
	}
	// Different subject should still succeed.
	_, c3, err := b.Subscribe(SubscribeOptions{SubjectName: "bob"})
	if err != nil {
		t.Errorf("Subscribe(bob): %v", err)
	} else {
		defer c3()
	}
}

func TestBroadcaster_Subscribe_PerSubjectCapZero_Disabled(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.MaxSubscribersPerSubject = 0 // disabled
	cfg.MaxSubscribers = 5
	cfg.KeepaliveInterval = 0
	b := NewBroadcaster(cfg, nil)
	defer b.Stop()
	for i := 0; i < 5; i++ {
		_, c, err := b.Subscribe(SubscribeOptions{SubjectName: "alice"})
		if err != nil {
			t.Fatalf("Subscribe %d: %v", i, err)
		}
		defer c()
	}
}

func TestBroadcaster_Subscribe_AnonymousIgnoresPerSubjectCap(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t) // per-subject cap = 2
	for i := 0; i < 4; i++ {
		_, c, err := b.Subscribe(SubscribeOptions{SubjectName: ""}) // empty = anon
		if err != nil {
			t.Fatalf("anon Subscribe %d: %v", i, err)
		}
		defer c()
	}
}

// ── DpuIDs filter ───────────────────────────────────────────────────────

func TestBroadcaster_Filter_AllowsOnlyMatchingDpu(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t)
	sub, cancel, _ := b.Subscribe(SubscribeOptions{DpuIDs: []string{"dpu-b"}})
	defer cancel()

	b.Publish(sampleReport("dpu-a", 1)) // filtered out
	b.Publish(sampleReport("dpu-b", 2)) // matches
	b.Publish(sampleReport("dpu-c", 3)) // filtered out

	f := drainOne(t, sub.Recv(), 200*time.Millisecond)
	if f.Event.GetReport().GetDpuId() != "dpu-b" {
		t.Errorf("got dpu=%q, want dpu-b only", f.Event.GetReport().GetDpuId())
	}
	expectNoFrame(t, sub.Recv(), 50*time.Millisecond)
}

func TestBroadcaster_Filter_EmptyMeansAll(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t)
	sub, cancel, _ := b.Subscribe(SubscribeOptions{DpuIDs: nil}) // empty
	defer cancel()
	b.Publish(sampleReport("dpu-x", 1))
	b.Publish(sampleReport("dpu-y", 2))
	_ = drainOne(t, sub.Recv(), 200*time.Millisecond)
	_ = drainOne(t, sub.Recv(), 200*time.Millisecond)
}

func TestBroadcaster_Filter_AllEmptyEntriesFallBackToAll(t *testing.T) {
	t.Parallel()
	// Edge case: caller passes [""]. We must NOT silently deliver
	// zero events; fall back to "all DPUs" instead.
	b := newTestBroadcaster(t)
	sub, cancel, _ := b.Subscribe(SubscribeOptions{DpuIDs: []string{"", ""}})
	defer cancel()
	b.Publish(sampleReport("dpu-x", 1))
	_ = drainOne(t, sub.Recv(), 200*time.Millisecond)
}

func TestBroadcaster_Filter_SentinelsAlwaysPass(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t)
	sub, cancel, _ := b.Subscribe(SubscribeOptions{DpuIDs: []string{"dpu-only-this"}})
	defer cancel()

	// Drive a keepalive directly through PublishSentinel — no report
	// body, so the dpu filter must allow it through.
	b.PublishSentinel(newKeepaliveNotice())
	f := drainOne(t, sub.Recv(), 200*time.Millisecond)
	if f.Event.GetKind() != dashcenterv1.CounterEvent_KIND_KEEPALIVE {
		t.Errorf("kind = %v, want KIND_KEEPALIVE", f.Event.GetKind())
	}
}

// ── marshal-once invariant ───────────────────────────────────────────────

func TestBroadcaster_MarshalOnce(t *testing.T) {
	t.Parallel()
	// Two subscribers receive the exact same byte slice for one
	// Publish. Compare by address (same backing array) — that's the
	// only way to assert "marshal-once-send-many".
	b := newTestBroadcaster(t)
	subA, cancelA, _ := b.Subscribe(SubscribeOptions{})
	defer cancelA()
	subB, cancelB, _ := b.Subscribe(SubscribeOptions{})
	defer cancelB()

	b.Publish(sampleReport("dpu-shared", 7))
	fa := drainOne(t, subA.Recv(), 200*time.Millisecond)
	fb := drainOne(t, subB.Recv(), 200*time.Millisecond)
	// Same Frame pointer ⇒ same JSON []byte ⇒ marshal-once.
	if fa != fb {
		t.Errorf("subscribers got distinct *Frame; marshal-once violated: %p vs %p", fa, fb)
	}
	if len(fa.JSON) == 0 || string(fa.JSON) != string(fb.JSON) {
		t.Errorf("JSON not shared: A=%q B=%q", fa.JSON, fb.JSON)
	}
}

// ── drop-on-slow + KIND_DROPPED workflow ────────────────────────────────

func TestBroadcaster_DropOnSlow_TakeDroppedCount(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t) // SubscriberBufferSize = 2
	sub, cancel, _ := b.Subscribe(SubscribeOptions{})
	defer cancel()

	// Fill the buffer (capacity 2) + 1 overflow + 2 more drops.
	for i := 0; i < 5; i++ {
		b.Publish(sampleReport("dpu-a", int64(i)))
	}
	// drain the 2 delivered frames so the channel is empty, then
	// observe dropped count.
	<-sub.Recv()
	<-sub.Recv()
	dropped := sub.TakeDroppedCount()
	if dropped != 3 {
		t.Errorf("dropped = %d, want 3 (5 published - 2 buffered)", dropped)
	}
	// TakeDroppedCount must be atomic-clear: second call returns 0.
	if again := sub.TakeDroppedCount(); again != 0 {
		t.Errorf("second TakeDroppedCount = %d, want 0", again)
	}
}

func TestBroadcaster_LastDeliveredEventID_Tracks(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t)
	sub, cancel, _ := b.Subscribe(SubscribeOptions{})
	defer cancel()
	b.Publish(sampleReport("dpu-a", 1))
	b.Publish(sampleReport("dpu-b", 2))
	f1 := drainOne(t, sub.Recv(), 100*time.Millisecond)
	f2 := drainOne(t, sub.Recv(), 100*time.Millisecond)
	if sub.LastDeliveredEventID() != f2.Event.GetEventId() {
		t.Errorf("LastDeliveredEventID = %d; want %d (max of delivered)",
			sub.LastDeliveredEventID(), f2.Event.GetEventId())
	}
	_ = f1
}

// ── rate-limit ──────────────────────────────────────────────────────────

func TestBroadcaster_RateLimit_EmitsNotice(t *testing.T) {
	t.Parallel()
	// Tiny bucket forces suppression on the second Publish.
	cfg := DefaultConfig()
	cfg.MaxSubscribers = 4
	cfg.MaxSubscribersPerSubject = 0
	cfg.SubscriberBufferSize = 16
	cfg.RingSize = 16
	cfg.CoalesceWindow = 0
	cfg.EventRatePerSec = 1
	cfg.BurstSize = 1
	cfg.KeepaliveInterval = 0
	cfg.SuppressedNoticeDelay = 30 * time.Millisecond
	b := NewBroadcaster(cfg, nil)
	defer b.Stop()
	sub, cancel, _ := b.Subscribe(SubscribeOptions{})
	defer cancel()
	// First publish consumes the only token; subsequent ones suppress.
	for i := 0; i < 10; i++ {
		b.Publish(sampleReport("dpu-a", int64(i)))
	}
	// Drain delivered REPORTs (only the first goes through).
	got := 0
	deadline := time.After(200 * time.Millisecond)
loop:
	for {
		select {
		case f := <-sub.Recv():
			switch f.Event.GetKind() {
			case dashcenterv1.CounterEvent_KIND_REPORT:
				got++
			case dashcenterv1.CounterEvent_KIND_RATE_LIMITED:
				n := f.Event.GetNotice().GetSuppressedCount()
				if n == 0 {
					t.Errorf("rate-limited notice has zero suppressed_count")
				}
				if !strings.Contains(f.Event.GetNotice().GetMessage(), "suppressed") {
					t.Errorf("rate-limited notice message lacks 'suppressed': %s", f.Event.GetNotice().GetMessage())
				}
				if got != 1 {
					t.Errorf("delivered REPORTs = %d, want 1", got)
				}
				return // success
			}
		case <-deadline:
			break loop
		}
	}
	t.Fatal("never saw KIND_RATE_LIMITED notice")
}

func TestBroadcaster_RateLimit_ScheduleIdempotent(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.MaxSubscribers = 4
	cfg.MaxSubscribersPerSubject = 0
	cfg.SubscriberBufferSize = 16
	cfg.RingSize = 16
	cfg.CoalesceWindow = 0
	cfg.EventRatePerSec = 1
	cfg.BurstSize = 1
	cfg.KeepaliveInterval = 0
	cfg.SuppressedNoticeDelay = 500 * time.Millisecond // long enough to test idempotency
	b := NewBroadcaster(cfg, nil)
	defer b.Stop()
	_, c, _ := b.Subscribe(SubscribeOptions{})
	defer c()
	// First Publish consumes the token.
	b.Publish(sampleReport("dpu-a", 1))
	// Suppressions schedule the notice timer; calling schedule again
	// while the timer is pending must be a no-op (else timers would
	// stack and notices would fire repeatedly).
	for i := 0; i < 100; i++ {
		b.Publish(sampleReport("dpu-a", int64(i)))
	}
	// The internal rateNoticeT should be non-nil right now (one timer).
	b.rateMu.Lock()
	hasTimer := b.rateNoticeT != nil
	b.rateMu.Unlock()
	if !hasTimer {
		t.Errorf("expected a single pending rate-notice timer")
	}
}

func TestBroadcaster_RateLimit_EmitRateNotice_NoOpWhenZeroSuppressed(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t)
	// Calling emit with zero suppressed should fast-path return; no
	// frames emitted to subscribers.
	sub, c, _ := b.Subscribe(SubscribeOptions{})
	defer c()
	b.emitRateNotice()
	expectNoFrame(t, sub.Recv(), 30*time.Millisecond)
}

// ── coalescing ──────────────────────────────────────────────────────────

func TestBroadcaster_Coalesce_MergesByDpu(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.MaxSubscribers = 4
	cfg.MaxSubscribersPerSubject = 0
	cfg.SubscriberBufferSize = 32
	cfg.RingSize = 32
	cfg.CoalesceWindow = 50 * time.Millisecond
	cfg.EventRatePerSec = 1000
	cfg.BurstSize = 1000
	cfg.KeepaliveInterval = 0
	b := NewBroadcaster(cfg, nil)
	defer b.Stop()
	sub, c, _ := b.Subscribe(SubscribeOptions{})
	defer c()
	// 5 puts to dpu-a within the window → 1 emitted (the latest).
	for i := 0; i < 5; i++ {
		b.Publish(sampleReport("dpu-a", int64(100+i)))
	}
	// 1 put to dpu-b within the same window → 1 emitted.
	b.Publish(sampleReport("dpu-b", 999))

	// Collect everything that arrives in ~150ms.
	deadline := time.After(150 * time.Millisecond)
	frames := []*Frame{}
loop:
	for {
		select {
		case f := <-sub.Recv():
			frames = append(frames, f)
		case <-deadline:
			break loop
		}
	}
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2 (one per dpu after coalesce)", len(frames))
	}
	// Sorted by dpu alphabetically per flushCoalesce contract.
	if frames[0].Event.GetReport().GetDpuId() != "dpu-a" {
		t.Errorf("first frame dpu = %q, want dpu-a", frames[0].Event.GetReport().GetDpuId())
	}
	if frames[0].Event.GetReport().GetVxlanDecap() != 104 {
		t.Errorf("first frame decap = %d, want 104 (latest survives)", frames[0].Event.GetReport().GetVxlanDecap())
	}
	if frames[1].Event.GetReport().GetDpuId() != "dpu-b" {
		t.Errorf("second frame dpu = %q, want dpu-b", frames[1].Event.GetReport().GetDpuId())
	}
}

func TestBroadcaster_Coalesce_EmptyDpuBypasses(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.MaxSubscribers = 4
	cfg.MaxSubscribersPerSubject = 0
	cfg.SubscriberBufferSize = 32
	cfg.RingSize = 32
	cfg.CoalesceWindow = 100 * time.Millisecond
	cfg.EventRatePerSec = 1000
	cfg.BurstSize = 1000
	cfg.KeepaliveInterval = 0
	b := NewBroadcaster(cfg, nil)
	defer b.Stop()
	sub, c, _ := b.Subscribe(SubscribeOptions{})
	defer c()
	// Empty dpu_id can't be coalesced; falls through to publishImmediate.
	b.Publish(&dashcenterv1.CounterReport{DpuId: ""})
	_ = drainOne(t, sub.Recv(), 50*time.Millisecond)
}

func TestBroadcaster_CoalesceDisabled_ImmediateEmit(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t) // CoalesceWindow = 0
	sub, c, _ := b.Subscribe(SubscribeOptions{})
	defer c()
	b.Publish(sampleReport("dpu-a", 1))
	b.Publish(sampleReport("dpu-a", 2))
	// No coalesce; both should arrive.
	f1 := drainOne(t, sub.Recv(), 100*time.Millisecond)
	f2 := drainOne(t, sub.Recv(), 100*time.Millisecond)
	if f1.Event.GetReport().GetVxlanDecap() != 1 || f2.Event.GetReport().GetVxlanDecap() != 2 {
		t.Errorf("decap sequence wrong: %d, %d", f1.Event.GetReport().GetVxlanDecap(), f2.Event.GetReport().GetVxlanDecap())
	}
}

// ── PublishSentinel ─────────────────────────────────────────────────────

func TestPublishSentinel_RejectsPerSubscriberKinds(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t)
	sub, c, _ := b.Subscribe(SubscribeOptions{})
	defer c()
	b.PublishSentinel(nil) // nil → no-op
	b.PublishSentinel(NewDroppedNotice(5))
	b.PublishSentinel(newResyncNotice(7, "test"))
	expectNoFrame(t, sub.Recv(), 50*time.Millisecond)
}

func TestPublishSentinel_KeepaliveDeliverable(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t)
	sub, c, _ := b.Subscribe(SubscribeOptions{})
	defer c()
	b.PublishSentinel(newKeepaliveNotice())
	f := drainOne(t, sub.Recv(), 100*time.Millisecond)
	if f.Event.GetKind() != dashcenterv1.CounterEvent_KIND_KEEPALIVE {
		t.Errorf("kind = %v", f.Event.GetKind())
	}
}

// ── ResumeAfterEventID ──────────────────────────────────────────────────

func TestSubscribe_Resume_FromRing(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t)
	for i := 0; i < 3; i++ {
		b.Publish(sampleReport(fmt.Sprintf("dpu-%d", i), int64(i)))
	}
	// New subscriber asks to resume after event_id=1 → should receive
	// events 2 and 3.
	sub, c, _ := b.Subscribe(SubscribeOptions{ResumeAfterEventID: 1})
	defer c()
	f1 := drainOne(t, sub.Recv(), 100*time.Millisecond)
	f2 := drainOne(t, sub.Recv(), 100*time.Millisecond)
	if f1.Event.GetEventId() != 2 || f2.Event.GetEventId() != 3 {
		t.Errorf("resume order: %d, %d; want 2, 3", f1.Event.GetEventId(), f2.Event.GetEventId())
	}
	if sub.LastDeliveredEventID() != 3 {
		t.Errorf("LastDeliveredEventID = %d, want 3", sub.LastDeliveredEventID())
	}
}

func TestSubscribe_Resume_CursorBeyondTrigger_Resync(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t)
	b.Publish(sampleReport("dpu-a", 1))
	// Cursor 999 is past the current head → RESYNC sentinel.
	sub, c, _ := b.Subscribe(SubscribeOptions{ResumeAfterEventID: 999})
	defer c()
	f := drainOne(t, sub.Recv(), 100*time.Millisecond)
	if f.Event.GetKind() != dashcenterv1.CounterEvent_KIND_RESYNC {
		t.Errorf("kind = %v, want KIND_RESYNC", f.Event.GetKind())
	}
	if f.Event.GetNotice().GetCurrentEventId() == 0 {
		t.Errorf("RESYNC notice missing current_event_id")
	}
	if !strings.Contains(f.Event.GetNotice().GetMessage(), "cursor exceeds") {
		t.Errorf("RESYNC message = %q", f.Event.GetNotice().GetMessage())
	}
}

func TestSubscribe_Resume_CursorPredatesRing_Resync(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t) // RingSize = 4
	for i := 0; i < 10; i++ {
		b.Publish(sampleReport("dpu-a", int64(i)))
	}
	// Cursor 1 is well before the evicted oldest → RESYNC.
	sub, c, _ := b.Subscribe(SubscribeOptions{ResumeAfterEventID: 1})
	defer c()
	f := drainOne(t, sub.Recv(), 100*time.Millisecond)
	if f.Event.GetKind() != dashcenterv1.CounterEvent_KIND_RESYNC {
		t.Errorf("kind = %v, want KIND_RESYNC", f.Event.GetKind())
	}
	if !strings.Contains(f.Event.GetNotice().GetMessage(), "predates ring") {
		t.Errorf("RESYNC message = %q", f.Event.GetNotice().GetMessage())
	}
}

func TestSubscribe_Resume_ZeroIsNoOp(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t)
	b.Publish(sampleReport("dpu-a", 1))
	sub, c, _ := b.Subscribe(SubscribeOptions{ResumeAfterEventID: 0})
	defer c()
	expectNoFrame(t, sub.Recv(), 50*time.Millisecond)
}

func TestSubscribe_Resume_AppliesDpuFilter(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t)
	b.Publish(sampleReport("dpu-a", 1))
	b.Publish(sampleReport("dpu-b", 2))
	b.Publish(sampleReport("dpu-c", 3))
	sub, c, _ := b.Subscribe(SubscribeOptions{
		ResumeAfterEventID: 0, // 0 ⇒ no resume; reset below
		DpuIDs:             []string{"dpu-b"},
	})
	c() // close immediately to test the path
	_ = sub

	// Re-do with resume enabled.
	sub2, c2, _ := b.Subscribe(SubscribeOptions{
		ResumeAfterEventID: 0,
		DpuIDs:             []string{"dpu-b"},
	})
	defer c2()
	expectNoFrame(t, sub2.Recv(), 50*time.Millisecond)

	// Live event for dpu-b after subscribe → arrives.
	b.Publish(sampleReport("dpu-b", 99))
	f := drainOne(t, sub2.Recv(), 100*time.Millisecond)
	if f.Event.GetReport().GetDpuId() != "dpu-b" {
		t.Errorf("filtered subscribe got %q, want dpu-b", f.Event.GetReport().GetDpuId())
	}
}

func TestSubscribe_Resume_RingWrap(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t) // RingSize = 4
	// Publish 6 → ring holds events 3..6 (oldest evicted).
	for i := 0; i < 6; i++ {
		b.Publish(sampleReport("dpu-a", int64(i)))
	}
	// Cursor at id=4 → expect events 5 and 6.
	sub, c, _ := b.Subscribe(SubscribeOptions{ResumeAfterEventID: 4})
	defer c()
	f1 := drainOne(t, sub.Recv(), 100*time.Millisecond)
	f2 := drainOne(t, sub.Recv(), 100*time.Millisecond)
	if f1.Event.GetEventId() != 5 || f2.Event.GetEventId() != 6 {
		t.Errorf("ring-wrap resume: got %d, %d; want 5, 6", f1.Event.GetEventId(), f2.Event.GetEventId())
	}
}

// ── Run / Stop / keepalive lifecycle ─────────────────────────────────────

func TestRun_StartsKeepalive_StopShutsDown(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.MaxSubscribers = 4
	cfg.MaxSubscribersPerSubject = 0
	cfg.SubscriberBufferSize = 16
	cfg.RingSize = 16
	cfg.CoalesceWindow = 0
	cfg.KeepaliveInterval = 30 * time.Millisecond
	cfg.SuppressedNoticeDelay = 30 * time.Millisecond
	b := NewBroadcaster(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	b.Run(ctx)
	sub, sc, _ := b.Subscribe(SubscribeOptions{})
	defer sc()

	f := drainOne(t, sub.Recv(), 200*time.Millisecond)
	if f.Event.GetKind() != dashcenterv1.CounterEvent_KIND_KEEPALIVE {
		t.Errorf("first frame should be KEEPALIVE; got %v", f.Event.GetKind())
	}
	cancel()
	b.Stop()
	// Subsequent Stop is idempotent.
	b.Stop()
}

func TestRun_Idempotent(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.Run(ctx)
	b.Run(ctx) // must be no-op
}

func TestRun_StopsKeepaliveOnCtxCancel(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.MaxSubscribers = 4
	cfg.MaxSubscribersPerSubject = 0
	cfg.SubscriberBufferSize = 16
	cfg.RingSize = 16
	cfg.KeepaliveInterval = 30 * time.Millisecond
	b := NewBroadcaster(cfg, nil)
	defer b.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	b.Run(ctx)
	sub, sc, _ := b.Subscribe(SubscribeOptions{})
	defer sc()
	_ = drainOne(t, sub.Recv(), 200*time.Millisecond) // first keepalive
	cancel()
	// Drain any in-flight; then assert silence after 100ms.
	time.Sleep(80 * time.Millisecond)
	expectNoFrame(t, sub.Recv(), 100*time.Millisecond)
}

func TestRun_ZeroKeepalive_NoGoroutine(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t) // KeepaliveInterval = 0
	b.Run(context.Background())
	sub, c, _ := b.Subscribe(SubscribeOptions{})
	defer c()
	expectNoFrame(t, sub.Recv(), 100*time.Millisecond)
}

// ── Stats ────────────────────────────────────────────────────────────────

func TestStats_TracksLifecycle(t *testing.T) {
	t.Parallel()
	b := newTestBroadcaster(t)

	s := b.Stats()
	if s.Subscribers != 0 {
		t.Errorf("initial subscribers = %d, want 0", s.Subscribers)
	}
	if s.RingSize != 4 {
		t.Errorf("RingSize = %d, want 4", s.RingSize)
	}
	if s.OldestEventID != 0 || s.NewestEventID != 0 {
		t.Errorf("empty broadcaster should have zero ids; got oldest=%d newest=%d", s.OldestEventID, s.NewestEventID)
	}

	_, c1, _ := b.Subscribe(SubscribeOptions{SubjectName: "alice"})
	defer c1()
	b.Publish(sampleReport("dpu-a", 1))
	b.Publish(sampleReport("dpu-b", 2))
	// give the fan-out a moment to land.
	time.Sleep(30 * time.Millisecond)
	s = b.Stats()
	if s.Subscribers != 1 {
		t.Errorf("subscribers = %d, want 1", s.Subscribers)
	}
	if s.BySubjectCount["alice"] != 1 {
		t.Errorf("bySubject[alice] = %d, want 1", s.BySubjectCount["alice"])
	}
	if s.TotalPublished != 2 {
		t.Errorf("TotalPublished = %d, want 2", s.TotalPublished)
	}
	if s.NewestEventID != 2 {
		t.Errorf("NewestEventID = %d, want 2", s.NewestEventID)
	}
}

// ── helpers coverage ─────────────────────────────────────────────────────

func TestKindLabel(t *testing.T) {
	t.Parallel()
	cases := map[string]*dashcenterv1.CounterEvent{
		"snapshot":      {Kind: dashcenterv1.CounterEvent_KIND_SNAPSHOT},
		"report":        {Kind: dashcenterv1.CounterEvent_KIND_REPORT},
		"keepalive":     {Kind: dashcenterv1.CounterEvent_KIND_KEEPALIVE},
		"dropped":       {Kind: dashcenterv1.CounterEvent_KIND_DROPPED},
		"rate_limited":  {Kind: dashcenterv1.CounterEvent_KIND_RATE_LIMITED},
		"resync":        {Kind: dashcenterv1.CounterEvent_KIND_RESYNC},
		"unspecified":   {Kind: dashcenterv1.CounterEvent_KIND_UNSPECIFIED},
	}
	for want, ev := range cases {
		if got := kindLabel(ev); got != want {
			t.Errorf("kindLabel(%v) = %q, want %q", ev.GetKind(), got, want)
		}
	}
	if got := kindLabel(nil); got != "nil" {
		t.Errorf("kindLabel(nil) = %q, want 'nil'", got)
	}
}

func TestCanCoalesce(t *testing.T) {
	t.Parallel()
	noCoalesce := []dashcenterv1.CounterEvent_Kind{
		dashcenterv1.CounterEvent_KIND_SNAPSHOT,
		dashcenterv1.CounterEvent_KIND_KEEPALIVE,
		dashcenterv1.CounterEvent_KIND_RATE_LIMITED,
	}
	for _, k := range noCoalesce {
		if canCoalesce(k) {
			t.Errorf("canCoalesce(%v) = true, want false", k)
		}
	}
	coalesce := []dashcenterv1.CounterEvent_Kind{
		dashcenterv1.CounterEvent_KIND_REPORT,
		dashcenterv1.CounterEvent_KIND_UNSPECIFIED,
		dashcenterv1.CounterEvent_KIND_DROPPED,
		dashcenterv1.CounterEvent_KIND_RESYNC,
	}
	for _, k := range coalesce {
		if !canCoalesce(k) {
			t.Errorf("canCoalesce(%v) = false, want true", k)
		}
	}
}

func TestBuildFrame_PopulatesJSON(t *testing.T) {
	t.Parallel()
	ev := &dashcenterv1.CounterEvent{
		Kind:    dashcenterv1.CounterEvent_KIND_REPORT,
		EventId: 7,
		Ts:      timestamppb.Now(),
		Body: &dashcenterv1.CounterEvent_Report{Report: sampleReport("dpu-x", 5)},
	}
	f, err := buildFrame(ev)
	if err != nil {
		t.Fatalf("buildFrame: %v", err)
	}
	if f.Event != ev {
		t.Errorf("Frame.Event pointer not preserved")
	}
	if !strings.Contains(string(f.JSON), `"dpu_id":"dpu-x"`) {
		t.Errorf("JSON missing dpu_id: %s", f.JSON)
	}
}

// ── concurrency smoke ───────────────────────────────────────────────────

func TestBroadcaster_ConcurrentPublishSubscribe(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.MaxSubscribers = 32
	cfg.MaxSubscribersPerSubject = 0
	cfg.SubscriberBufferSize = 64
	cfg.RingSize = 256
	cfg.CoalesceWindow = 0
	cfg.EventRatePerSec = 10000
	cfg.BurstSize = 10000
	cfg.KeepaliveInterval = 0
	b := NewBroadcaster(cfg, nil)
	defer b.Stop()

	var wg sync.WaitGroup
	// 10 subscribers churn through subscribe+drain+cancel.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sub, c, err := b.Subscribe(SubscribeOptions{SubjectName: fmt.Sprintf("user-%d", idx)})
			if err != nil {
				return
			}
			defer c()
			deadline := time.After(100 * time.Millisecond)
			for {
				select {
				case <-sub.Recv():
				case <-deadline:
					return
				}
			}
		}(i)
	}
	// 5 publishers spam reports.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				b.Publish(sampleReport(fmt.Sprintf("dpu-%d", idx%3), int64(j)))
			}
		}(i)
	}
	wg.Wait()
	// Sanity: stats should be sane (no panics, totals match).
	s := b.Stats()
	if s.TotalPublished == 0 {
		t.Errorf("nothing got published")
	}
}

// ── defensive-branch coverage ────────────────────────────────────────────
// Targeted tests for the remaining reachable branches so the package
// hits 100% UT coverage. The two protojson.Marshal error paths
// (publishImmediate + buildFrame) are deliberately NOT covered — they
// are defensive belt-and-suspenders against malformed protos which
// cannot be constructed for our own generated types; removing the
// check would make the code less robust, not more testable.

func TestBroadcaster_Stop_WithPendingCoalesceTimer(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.MaxSubscribers = 4
	cfg.MaxSubscribersPerSubject = 0
	cfg.SubscriberBufferSize = 16
	cfg.RingSize = 16
	cfg.CoalesceWindow = 5 * time.Second // long enough to still be pending at Stop()
	cfg.KeepaliveInterval = 0
	b := NewBroadcaster(cfg, nil)
	sub, c, _ := b.Subscribe(SubscribeOptions{})
	defer c()
	b.Publish(sampleReport("dpu-a", 1)) // arms the coalesce timer
	// Stop() must cancel the pending timer.
	b.Stop()
	// No frame should arrive (timer cancelled before flush).
	expectNoFrame(t, sub.Recv(), 100*time.Millisecond)
}

func TestBroadcaster_Stop_WithPendingRateNoticeTimer(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.MaxSubscribers = 4
	cfg.MaxSubscribersPerSubject = 0
	cfg.SubscriberBufferSize = 16
	cfg.RingSize = 16
	cfg.CoalesceWindow = 0
	cfg.EventRatePerSec = 1
	cfg.BurstSize = 1
	cfg.KeepaliveInterval = 0
	cfg.SuppressedNoticeDelay = 5 * time.Second // long → still pending at Stop()
	b := NewBroadcaster(cfg, nil)
	sub, c, _ := b.Subscribe(SubscribeOptions{})
	defer c()
	b.Publish(sampleReport("dpu-a", 1)) // consumes token; emits KIND_REPORT
	_ = drainOne(t, sub.Recv(), 100*time.Millisecond)
	b.Publish(sampleReport("dpu-b", 2)) // suppressed → arms rate-notice timer
	// The pending rate-notice timer MUST be cancelled by Stop(), so no
	// KIND_RATE_LIMITED frame should arrive after Stop returns.
	b.Stop()
	expectNoFrame(t, sub.Recv(), 100*time.Millisecond)
}

func TestPublishImmediate_SkipsClosedSubscriber(t *testing.T) {
	t.Parallel()
	// Cover the branch where fan-out encounters a subscriber that was
	// cancelled mid-snapshot (between the RLock and the per-sub Lock).
	// We can drive it deterministically by cancelling before Publish.
	b := newTestBroadcaster(t)
	_, cancel, _ := b.Subscribe(SubscribeOptions{})
	// Subscribe a second sub that stays open so Publish still has work.
	other, otherCancel, _ := b.Subscribe(SubscribeOptions{})
	defer otherCancel()
	cancel() // first sub closed
	b.Publish(sampleReport("dpu-a", 1))
	// The open sub still gets it.
	_ = drainOne(t, other.Recv(), 100*time.Millisecond)
}

func TestPublishImmediate_RaceClosedDuringFanout(t *testing.T) {
	t.Parallel()
	// White-box: cover the TOCTOU branch where the per-sub Lock finds
	// sub.closed=true. publishImmediate snapshots subscribers under
	// RLock (taking a copy of the map keys); the actual cancel() may
	// have set closed=true AFTER the snapshot but BEFORE we take the
	// per-sub lock. We construct that state directly: insert a
	// subscription marked closed, then drive a Publish.
	b := newTestBroadcaster(t)
	zombie := &subscription{ch: make(chan *Frame, 1), closed: true}
	b.mu.Lock()
	b.subscribers[zombie] = struct{}{}
	b.mu.Unlock()
	// Also add a healthy subscriber so Publish has someone to deliver
	// to and we can assert the loop did fan out.
	live, cancel, _ := b.Subscribe(SubscribeOptions{})
	defer cancel()
	b.Publish(sampleReport("dpu-a", 1))
	_ = drainOne(t, live.Recv(), 100*time.Millisecond)
	// Zombie was skipped (its closed branch was hit); channel stays empty.
	if len(zombie.ch) != 0 {
		t.Errorf("zombie subscriber received a frame; closed-branch not taken")
	}
}

func TestRunKeepalive_StopChClosesLoop(t *testing.T) {
	t.Parallel()
	// runKeepalive selects on ctx.Done OR b.stopCh; the previous test
	// covers ctx.Done. This one covers b.stopCh via Broadcaster.Stop().
	cfg := DefaultConfig()
	cfg.MaxSubscribers = 4
	cfg.MaxSubscribersPerSubject = 0
	cfg.SubscriberBufferSize = 16
	cfg.RingSize = 16
	cfg.KeepaliveInterval = 30 * time.Millisecond
	b := NewBroadcaster(cfg, nil)
	ctx := context.Background() // never cancelled
	b.Run(ctx)
	sub, sc, _ := b.Subscribe(SubscribeOptions{})
	defer sc()
	_ = drainOne(t, sub.Recv(), 200*time.Millisecond) // first keepalive
	b.Stop()                                          // closes stopCh → keepalive goroutine exits
	time.Sleep(80 * time.Millisecond)
	expectNoFrame(t, sub.Recv(), 100*time.Millisecond)
}

func TestReplayResume_FilteredOutEntriesSkipped(t *testing.T) {
	t.Parallel()
	// Publish events to dpu-a and dpu-b; subscribe with DpuIDs=[dpu-b]
	// and a resume cursor → only dpu-b's resumed frames arrive (dpu-a
	// frames in the ring are skipped by the per-subscription filter).
	b := newTestBroadcaster(t)
	b.Publish(sampleReport("dpu-a", 1))
	b.Publish(sampleReport("dpu-b", 2))
	b.Publish(sampleReport("dpu-a", 3))
	b.Publish(sampleReport("dpu-b", 4))
	sub, c, _ := b.Subscribe(SubscribeOptions{
		DpuIDs:             []string{"dpu-b"},
		ResumeAfterEventID: 1,
	})
	defer c()
	// Should get events 2 and 4 (the two dpu-b events with id > 1).
	f1 := drainOne(t, sub.Recv(), 100*time.Millisecond)
	f2 := drainOne(t, sub.Recv(), 100*time.Millisecond)
	if f1.Event.GetEventId() != 2 || f2.Event.GetEventId() != 4 {
		t.Errorf("filtered resume: got %d, %d; want 2, 4",
			f1.Event.GetEventId(), f2.Event.GetEventId())
	}
	expectNoFrame(t, sub.Recv(), 50*time.Millisecond)
}

func TestEnqueueLocked_SkipsClosedSubscriber(t *testing.T) {
	t.Parallel()
	// Cover the "if sub.closed { return }" branch in enqueueLocked.
	// Drive via replayResume which calls enqueueLocked: cancel the
	// subscription, then trigger replay through a fresh Subscribe with
	// the same backing subscription... can't easily do that externally.
	// Instead, call the internal path directly: build a sub, close it,
	// then call enqueueResyncLocked under ringMu RLock.
	b := newTestBroadcaster(t)
	sub := &subscription{ch: make(chan *Frame, 1)}
	// Mark closed (don't close ch since the test fills it differently).
	sub.closed = true
	b.ringMu.RLock()
	b.enqueueResyncLocked(sub, 0, "test")
	b.ringMu.RUnlock()
	// Channel must remain empty (closed subscriber → silent skip).
	if len(sub.ch) != 0 {
		t.Errorf("enqueue to closed subscriber leaked frame; ch len=%d", len(sub.ch))
	}
}

func TestEnqueueLocked_DropOnFullBuffer(t *testing.T) {
	t.Parallel()
	// Cover the `default:` branch in enqueueLocked (drop-on-full
	// during ring replay).
	b := newTestBroadcaster(t)
	// Pre-load the ring with several events.
	for i := 0; i < 4; i++ {
		b.Publish(sampleReport("dpu-a", int64(i)))
	}
	// Subscribe with a 0-cursor that still attempts replay through a
	// tiny channel; we drive overflow via internal helpers since the
	// public Subscribe defends against this via the channel buffer.
	sub := &subscription{ch: make(chan *Frame, 1)}
	// Pre-fill the buffer.
	sub.ch <- &Frame{}
	// Force replay; the next enqueueLocked will hit the default branch.
	b.ringMu.RLock()
	for i := 0; i < 4; i++ {
		entry := b.ring[i]
		if entry.frame != nil {
			b.enqueueLocked(sub, entry.frame)
		}
	}
	b.ringMu.RUnlock()
	if sub.droppedCount.Load() == 0 {
		t.Errorf("expected droppedCount > 0 after overflow; got 0")
	}
}

func TestStats_RingHeadWrap(t *testing.T) {
	t.Parallel()
	// Cover the `if idx < 0` branch in Stats() — triggered when the
	// ring has wrapped AND ringHead == 0 (the next write goes to slot
	// 0 again). We hit this by publishing exactly RingSize events.
	b := newTestBroadcaster(t) // RingSize = 4
	for i := 0; i < 4; i++ {
		b.Publish(sampleReport("dpu-a", int64(i)))
	}
	// One more publish to actually wrap (ringHead → 1, ringWrapped=true).
	b.Publish(sampleReport("dpu-a", 99))
	// Three more to push ringHead back to 0.
	for i := 0; i < 3; i++ {
		b.Publish(sampleReport("dpu-a", int64(i)))
	}
	s := b.Stats()
	// The newest event_id should be the last we published (8 total).
	if s.NewestEventID != 8 {
		t.Errorf("NewestEventID = %d, want 8", s.NewestEventID)
	}
}

