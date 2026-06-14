// bridge_test.go — UT coverage for the counters.Store → Broadcaster
// adapter goroutine.
//
// Coverage target: 100% of bridge.go. The bridge has three branches:
//
//   1. happy path: notify ch fires → GetReport returns (report, true)
//      → Broadcaster.Publish is called with the report.
//   2. nil-entry race: notify fires but the entry was deleted between
//      notification and read → bridge skips the cycle silently
//      (admin endpoint will surface the absence; observability is
//      best-effort).
//   3. lifecycle: ctx cancel → bridge returns cleanly; store channel
//      close → bridge returns cleanly; nil store/broadcaster → bridge
//      logs a warning and exits.

package broadcaster

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

// fakeStore is a controllable CounterStore for bridge tests. Callers
// drive notifications via Notify; GetReport returns whatever the test
// pre-loaded.
type fakeStore struct {
	mu       sync.Mutex
	reports  map[string]*dashcenterv1.CounterReport
	ch       chan<- string
	subOnce  sync.Once
	cancelN  atomic.Int32
}

func newFakeStore() *fakeStore {
	return &fakeStore{reports: map[string]*dashcenterv1.CounterReport{}}
}

func (f *fakeStore) Subscribe(ch chan<- string) func() {
	// Match counters.Store contract: track exactly one subscriber for
	// the bridge.
	f.subOnce.Do(func() {
		f.ch = ch
	})
	return func() {
		f.cancelN.Add(1)
	}
}

func (f *fakeStore) GetReport(dpuID string) (*dashcenterv1.CounterReport, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.reports[dpuID]
	return r, ok && r != nil
}

func (f *fakeStore) Put(report *dashcenterv1.CounterReport) {
	f.mu.Lock()
	f.reports[report.GetDpuId()] = report
	f.mu.Unlock()
}

func (f *fakeStore) Delete(dpuID string) {
	f.mu.Lock()
	delete(f.reports, dpuID)
	f.mu.Unlock()
}

// Notify drives a change notification to whatever channel the bridge
// registered via Subscribe. Blocks briefly if the bridge is slow.
func (f *fakeStore) Notify(t *testing.T, dpuID string) {
	t.Helper()
	select {
	case f.ch <- dpuID:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("bridge notify deadlocked")
	}
}

// ── tests ────────────────────────────────────────────────────────────────

func TestBridge_NewBridge_NilLoggerFallsBackToDefault(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	b := newTestBroadcaster(t)
	br := NewBridge(store, b, nil)
	if br.logger == nil {
		t.Errorf("nil logger should fall back to slog.Default()")
	}
}

func TestBridge_NotifyToPublish(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.Put(sampleReport("dpu-a", 42))

	bcast := newTestBroadcaster(t)
	sub, cancel, _ := bcast.Subscribe(SubscribeOptions{})
	defer cancel()

	br := NewBridge(store, bcast, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancelCtx := context.WithCancel(context.Background())
	defer cancelCtx()
	go br.Run(ctx)
	// Give the goroutine a moment to register the subscription.
	time.Sleep(10 * time.Millisecond)

	store.Notify(t, "dpu-a")
	f := drainOne(t, sub.Recv(), 200*time.Millisecond)
	if f.Event.GetReport().GetDpuId() != "dpu-a" {
		t.Errorf("bridged dpu = %q, want dpu-a", f.Event.GetReport().GetDpuId())
	}
	if f.Event.GetReport().GetVxlanDecap() != 42 {
		t.Errorf("bridged decap = %d, want 42", f.Event.GetReport().GetVxlanDecap())
	}
}

func TestBridge_NotifyForMissingDpu_Skipped(t *testing.T) {
	t.Parallel()
	store := newFakeStore() // no entries
	bcast := newTestBroadcaster(t)
	sub, cancel, _ := bcast.Subscribe(SubscribeOptions{})
	defer cancel()

	br := NewBridge(store, bcast, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancelCtx := context.WithCancel(context.Background())
	defer cancelCtx()
	go br.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	store.Notify(t, "nonexistent")
	// Bridge must NOT panic, must NOT publish (store has no entry).
	expectNoFrame(t, sub.Recv(), 80*time.Millisecond)
}

func TestBridge_NotifyForDeletedDpu_Skipped(t *testing.T) {
	t.Parallel()
	// Race: notify fires for dpu-a, but dpu-a was deleted between
	// notify and read. Bridge calls GetReport → (nil, false) → skip.
	store := newFakeStore()
	store.Put(sampleReport("dpu-a", 1))
	bcast := newTestBroadcaster(t)
	sub, cancel, _ := bcast.Subscribe(SubscribeOptions{})
	defer cancel()

	br := NewBridge(store, bcast, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancelCtx := context.WithCancel(context.Background())
	defer cancelCtx()
	go br.Run(ctx)
	time.Sleep(10 * time.Millisecond)
	store.Delete("dpu-a")
	store.Notify(t, "dpu-a")
	expectNoFrame(t, sub.Recv(), 80*time.Millisecond)
}

func TestBridge_CtxCancel_ReturnsCleanly(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	bcast := newTestBroadcaster(t)
	br := NewBridge(store, bcast, nil)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		br.Run(ctx)
		close(done)
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("bridge did not return after ctx cancel")
	}
	if got := store.cancelN.Load(); got != 1 {
		t.Errorf("store.Subscribe cancel invoked %d times; want 1", got)
	}
}

func TestBridge_StoreChannelClosed_ReturnsCleanly(t *testing.T) {
	t.Parallel()
	// If the store closes the notify channel out from under us, the
	// bridge must exit without panic.
	store := newFakeStore()
	bcast := newTestBroadcaster(t)
	br := NewBridge(store, bcast, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		br.Run(ctx)
		close(done)
	}()
	time.Sleep(10 * time.Millisecond)
	close(br.notifyCh) // simulate store closing the channel
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("bridge did not return on channel close")
	}
}

func TestBridge_NilStore_ExitsImmediately(t *testing.T) {
	t.Parallel()
	br := NewBridge(nil, newTestBroadcaster(t), nil)
	done := make(chan struct{})
	go func() {
		br.Run(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("bridge with nil store should exit immediately")
	}
}

func TestBridge_NilBroadcaster_ExitsImmediately(t *testing.T) {
	t.Parallel()
	br := NewBridge(newFakeStore(), nil, nil)
	done := make(chan struct{})
	go func() {
		br.Run(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("bridge with nil broadcaster should exit immediately")
	}
}

// Sentinel-constructor coverage (sentinels.go).

func TestNewKeepaliveNotice(t *testing.T) {
	t.Parallel()
	ev := newKeepaliveNotice()
	if ev.GetKind() != dashcenterv1.CounterEvent_KIND_KEEPALIVE {
		t.Errorf("kind = %v", ev.GetKind())
	}
	if ev.GetNotice().GetMessage() != "keepalive" {
		t.Errorf("message = %q", ev.GetNotice().GetMessage())
	}
}

func TestNewRateLimitedNotice(t *testing.T) {
	t.Parallel()
	ev := newRateLimitedNotice(17, 200)
	if ev.GetKind() != dashcenterv1.CounterEvent_KIND_RATE_LIMITED {
		t.Errorf("kind = %v", ev.GetKind())
	}
	if ev.GetNotice().GetSuppressedCount() != 17 {
		t.Errorf("suppressed_count = %d, want 17", ev.GetNotice().GetSuppressedCount())
	}
}

func TestNewResyncNotice(t *testing.T) {
	t.Parallel()
	ev := newResyncNotice(99, "stale")
	if ev.GetKind() != dashcenterv1.CounterEvent_KIND_RESYNC {
		t.Errorf("kind = %v", ev.GetKind())
	}
	if ev.GetNotice().GetCurrentEventId() != 99 {
		t.Errorf("current_event_id = %d, want 99", ev.GetNotice().GetCurrentEventId())
	}
	if ev.GetNotice().GetMessage() != "stale" {
		t.Errorf("message = %q", ev.GetNotice().GetMessage())
	}
	if ev.GetTs() == nil {
		t.Errorf("ts should be stamped at construction")
	}
}

func TestNewDroppedNotice(t *testing.T) {
	t.Parallel()
	ev := NewDroppedNotice(5)
	if ev.GetKind() != dashcenterv1.CounterEvent_KIND_DROPPED {
		t.Errorf("kind = %v", ev.GetKind())
	}
	if ev.GetNotice().GetDroppedCount() != 5 {
		t.Errorf("dropped_count = %d, want 5", ev.GetNotice().GetDroppedCount())
	}
	if ev.GetTs() == nil {
		t.Errorf("ts should be stamped at construction")
	}
}
