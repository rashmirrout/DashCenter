// hub_test.go — PE-3c counter Hub unit tests.
// Drives the Hub against a scriptable fake upstream.

package observability

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeUpstream is a controllable CountersClient.
type fakeUpstream struct {
	mu      sync.Mutex
	streams atomic.Int32      // # of active GetCounters calls
	pre     []*dashcenterv1.CounterEvent
	hold    bool
	failN   int32             // first N calls fail with retryable error
	calls   atomic.Int32
}

func (f *fakeUpstream) GetCounters(ctx context.Context, req *dashcenterv1.CounterRequest) (CounterStream, error) {
	f.calls.Add(1)
	if f.failN > 0 {
		f.failN--
		return nil, errors.New("upstream down")
	}
	f.streams.Add(1)
	return &fakeStream{ctx: ctx, pre: f.pre, hold: f.hold, parent: f}, nil
}

type fakeStream struct {
	ctx    context.Context
	pre    []*dashcenterv1.CounterEvent
	idx    int
	hold   bool
	parent *fakeUpstream
}

func (s *fakeStream) Recv() (*dashcenterv1.CounterEvent, error) {
	if s.idx < len(s.pre) {
		ev := s.pre[s.idx]
		s.idx++
		return ev, nil
	}
	if !s.hold {
		s.parent.streams.Add(-1)
		return nil, io.EOF
	}
	<-s.ctx.Done()
	s.parent.streams.Add(-1)
	return nil, s.ctx.Err()
}

func sampleEvent(dpu string, id uint64) *dashcenterv1.CounterEvent {
	return &dashcenterv1.CounterEvent{
		Kind:    dashcenterv1.CounterEvent_KIND_REPORT,
		EventId: id,
		Ts:      timestamppb.Now(),
		Body: &dashcenterv1.CounterEvent_Report{
			Report: &dashcenterv1.CounterReport{DpuId: dpu, VxlanDecap: int64(id) * 10},
		},
	}
}

func newTestHub(t *testing.T, up *fakeUpstream, tweak func(*HubConfig)) *Hub {
	t.Helper()
	cfg := HubConfig{
		MaxWatchers:          16,
		MaxWatchersPerIP:     4,
		WatcherBufferSize:    4,
		RingSize:             8,
		UpstreamReconnectMin: 10 * time.Millisecond,
		UpstreamReconnectMax: 50 * time.Millisecond,
		UpstreamIdleGC:       30 * time.Millisecond,
	}
	if tweak != nil {
		tweak(&cfg)
	}
	h := NewHub(up, cfg, nil)
	t.Cleanup(h.Stop)
	return h
}

func drain(t *testing.T, ch <-chan *Frame, d time.Duration) *Frame {
	t.Helper()
	select {
	case f := <-ch:
		return f
	case <-time.After(d):
		t.Fatalf("no frame within %v", d)
		return nil
	}
}

func TestDefaultHubConfig(t *testing.T) {
	t.Parallel()
	c := DefaultHubConfig()
	if c.MaxWatchers != 512 || c.RingSize != 1024 || c.UpstreamIdleGC != 30*time.Second {
		t.Errorf("DefaultHubConfig drifted: %+v", c)
	}
}

func TestNewHub_AppliesDefaults(t *testing.T) {
	t.Parallel()
	h := NewHub(&fakeUpstream{}, HubConfig{}, nil)
	defer h.Stop()
	if h.cfg.MaxWatchers != 512 || h.cfg.WatcherBufferSize != 128 || h.cfg.RingSize != 1024 {
		t.Errorf("defaults not applied: %+v", h.cfg)
	}
}

func TestHub_Subscribe_GlobalCap(t *testing.T) {
	t.Parallel()
	up := &fakeUpstream{}
	h := newTestHub(t, up, func(c *HubConfig) { c.MaxWatchers = 2 })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.Start(ctx)
	_, c1, _ := h.Subscribe(SubscribeOptions{ClientID: "1"})
	defer c1()
	_, c2, _ := h.Subscribe(SubscribeOptions{ClientID: "2"})
	defer c2()
	_, _, err := h.Subscribe(SubscribeOptions{ClientID: "3"})
	if !errors.Is(err, ErrTooManyWatchers) {
		t.Errorf("err = %v, want ErrTooManyWatchers", err)
	}
}

func TestHub_Subscribe_PerIPCap(t *testing.T) {
	t.Parallel()
	up := &fakeUpstream{}
	h := newTestHub(t, up, func(c *HubConfig) { c.MaxWatchersPerIP = 1 })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.Start(ctx)
	_, c1, _ := h.Subscribe(SubscribeOptions{ClientID: "ip-a"})
	defer c1()
	_, _, err := h.Subscribe(SubscribeOptions{ClientID: "ip-a"})
	if !errors.Is(err, ErrTooManyWatchers) {
		t.Errorf("err = %v, want ErrTooManyWatchers (per-IP cap)", err)
	}
}

func TestHub_PerIPCap_Zero_Disabled(t *testing.T) {
	t.Parallel()
	h := newTestHub(t, &fakeUpstream{}, func(c *HubConfig) { c.MaxWatchersPerIP = 0; c.MaxWatchers = 5 })
	h.Start(context.Background())
	for i := 0; i < 5; i++ {
		if _, c, err := h.Subscribe(SubscribeOptions{ClientID: "same"}); err != nil {
			t.Fatalf("Subscribe %d: %v", i, err)
		} else {
			defer c()
		}
	}
}

func TestHub_UpstreamLazyOpenForAllDPUs(t *testing.T) {
	t.Parallel()
	up := &fakeUpstream{pre: []*dashcenterv1.CounterEvent{sampleEvent("dpu-a", 1)}, hold: true}
	h := newTestHub(t, up, nil)
	h.Start(context.Background())
	w, cancel, err := h.Subscribe(SubscribeOptions{ClientID: "ip"})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()
	// Should receive 1 event from the pre-scripted upstream.
	f := drain(t, w.Recv(), 500*time.Millisecond)
	if f.Event.GetReport().GetDpuId() != "dpu-a" {
		t.Errorf("got dpu = %q", f.Event.GetReport().GetDpuId())
	}
	if up.streams.Load() != 1 {
		t.Errorf("expected 1 active upstream, got %d", up.streams.Load())
	}
}

func TestHub_UpstreamPerSubscribedDPU(t *testing.T) {
	t.Parallel()
	up := &fakeUpstream{hold: true}
	h := newTestHub(t, up, nil)
	h.Start(context.Background())
	_, c1, _ := h.Subscribe(SubscribeOptions{ClientID: "ip-a", DpuIDs: []string{"dpu-1"}})
	defer c1()
	_, c2, _ := h.Subscribe(SubscribeOptions{ClientID: "ip-b", DpuIDs: []string{"dpu-2"}})
	defer c2()
	// Give upstreams time to open.
	time.Sleep(50 * time.Millisecond)
	if got := up.streams.Load(); got != 2 {
		t.Errorf("active upstreams = %d, want 2 (one per DPU)", got)
	}
}

func TestHub_Filter_Sentinels_Pass(t *testing.T) {
	t.Parallel()
	// A KEEPALIVE from the "all DPUs" upstream should not reach a
	// dpu-filtered watcher because we can't say which DPU it applies
	// to. But a KEEPALIVE from the specific DPU's upstream should pass.
	h := newTestHub(t, &fakeUpstream{hold: true}, nil)
	h.Start(context.Background())
	w, cancel, _ := h.Subscribe(SubscribeOptions{DpuIDs: []string{"dpu-1"}})
	defer cancel()
	// Build a KEEPALIVE sentinel and publish via dpu-1 upstream key.
	ev := &dashcenterv1.CounterEvent{
		Kind: dashcenterv1.CounterEvent_KIND_KEEPALIVE,
		Body: &dashcenterv1.CounterEvent_Notice{Notice: &dashcenterv1.Notice{Message: "ka"}},
	}
	frame, _ := h.buildFrame(ev)
	h.publish(frame, "dpu-1")
	f := drain(t, w.Recv(), 200*time.Millisecond)
	if f.Event.GetKind() != dashcenterv1.CounterEvent_KIND_KEEPALIVE {
		t.Errorf("kind = %v", f.Event.GetKind())
	}
}

func TestHub_Filter_Report_ByDpu(t *testing.T) {
	t.Parallel()
	h := newTestHub(t, &fakeUpstream{hold: true}, nil)
	h.Start(context.Background())
	w, cancel, _ := h.Subscribe(SubscribeOptions{DpuIDs: []string{"dpu-1"}})
	defer cancel()
	// Publish reports for dpu-1 (match) and dpu-2 (drop).
	f1, _ := h.buildFrame(sampleEvent("dpu-1", 1))
	h.publish(f1, "")
	f2, _ := h.buildFrame(sampleEvent("dpu-2", 2))
	h.publish(f2, "")
	got := drain(t, w.Recv(), 100*time.Millisecond)
	if got.Event.GetReport().GetDpuId() != "dpu-1" {
		t.Errorf("got %v", got.Event.GetReport().GetDpuId())
	}
	select {
	case extra := <-w.Recv():
		t.Errorf("unexpected second frame: %+v", extra.Event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHub_Filter_AllEmptyDpuIDs_DegradesToAll(t *testing.T) {
	t.Parallel()
	h := newTestHub(t, &fakeUpstream{hold: true}, nil)
	h.Start(context.Background())
	w, cancel, _ := h.Subscribe(SubscribeOptions{DpuIDs: []string{"", ""}})
	defer cancel()
	if w.w.dpuSet != nil {
		t.Errorf("dpuSet should be nil (all-empty IDs degraded)")
	}
}

func TestHub_DropOnSlow(t *testing.T) {
	t.Parallel()
	h := newTestHub(t, &fakeUpstream{hold: true}, func(c *HubConfig) { c.WatcherBufferSize = 2 })
	h.Start(context.Background())
	w, cancel, _ := h.Subscribe(SubscribeOptions{})
	defer cancel()
	for i := 1; i <= 5; i++ {
		f, _ := h.buildFrame(sampleEvent("dpu-a", uint64(i)))
		h.publish(f, "")
	}
	// Drain the buffered 2.
	<-w.Recv()
	<-w.Recv()
	d := w.TakeDroppedCount()
	if d != 3 {
		t.Errorf("dropped = %d, want 3", d)
	}
	if again := w.TakeDroppedCount(); again != 0 {
		t.Errorf("TakeDroppedCount second call = %d, want 0 (atomic clear)", again)
	}
}

func TestHub_Resume_FromRing(t *testing.T) {
	t.Parallel()
	h := newTestHub(t, &fakeUpstream{hold: true}, nil)
	h.Start(context.Background())
	for i := 1; i <= 3; i++ {
		f, _ := h.buildFrame(sampleEvent("dpu-a", uint64(i)))
		h.publish(f, "")
	}
	w, cancel, _ := h.Subscribe(SubscribeOptions{ResumeAfterEventID: 1})
	defer cancel()
	// Should get id=2 and id=3.
	f1 := drain(t, w.Recv(), 100*time.Millisecond)
	f2 := drain(t, w.Recv(), 100*time.Millisecond)
	if f1.Event.GetEventId() != 2 || f2.Event.GetEventId() != 3 {
		t.Errorf("got %d, %d; want 2, 3", f1.Event.GetEventId(), f2.Event.GetEventId())
	}
}

func TestHub_Resume_StaleCursor_Resync(t *testing.T) {
	t.Parallel()
	h := newTestHub(t, &fakeUpstream{hold: true}, nil)
	h.Start(context.Background())
	w, cancel, _ := h.Subscribe(SubscribeOptions{ResumeAfterEventID: 999})
	defer cancel()
	f := drain(t, w.Recv(), 100*time.Millisecond)
	if f.Event.GetKind() != dashcenterv1.CounterEvent_KIND_RESYNC {
		t.Errorf("kind = %v, want RESYNC", f.Event.GetKind())
	}
}

func TestHub_Reconnect_FiresResync(t *testing.T) {
	t.Parallel()
	// First call fails, second succeeds. After failure the hub should
	// emit a KIND_RESYNC notice and then connect.
	up := &fakeUpstream{failN: 1, hold: true}
	h := newTestHub(t, up, nil)
	h.Start(context.Background())
	w, cancel, _ := h.Subscribe(SubscribeOptions{})
	defer cancel()
	// Wait for the resync sentinel to arrive.
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case f := <-w.Recv():
			if f.Event.GetKind() == dashcenterv1.CounterEvent_KIND_RESYNC {
				return
			}
		case <-deadline:
			t.Fatal("never saw resync after reconnect")
		}
	}
}

func TestHub_UpstreamGC_AfterIdle(t *testing.T) {
	t.Parallel()
	up := &fakeUpstream{hold: true}
	h := newTestHub(t, up, func(c *HubConfig) {
		c.UpstreamIdleGC = 50 * time.Millisecond
	})
	h.Start(context.Background())
	_, cancel, _ := h.Subscribe(SubscribeOptions{DpuIDs: []string{"dpu-1"}})
	time.Sleep(30 * time.Millisecond)
	if up.streams.Load() != 1 {
		t.Fatalf("expected 1 active upstream, got %d", up.streams.Load())
	}
	cancel()
	// Wait for GC to close the upstream.
	deadline := time.After(500 * time.Millisecond)
	for {
		if up.streams.Load() == 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("upstream not GC'd after idle; still %d active", up.streams.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestInjectSourceVia(t *testing.T) {
	t.Parallel()
	in := []byte(`{"kind":"KIND_REPORT","event_id":"7"}`)
	out := injectSourceVia(in, "dashd-1:9443", "dashw-a")
	if !strings.Contains(string(out), `"source":"dashd-1:9443"`) {
		t.Errorf("missing source in %s", out)
	}
	if !strings.Contains(string(out), `"via":"dashw-a"`) {
		t.Errorf("missing via in %s", out)
	}
	// Source-only.
	out = injectSourceVia(in, "x", "")
	if strings.Contains(string(out), `"via"`) {
		t.Errorf("unexpected via in %s", out)
	}
	// Via-only.
	out = injectSourceVia(in, "", "y")
	if strings.Contains(string(out), `"source"`) {
		t.Errorf("unexpected source in %s", out)
	}
	// Empty input is passed through.
	if got := injectSourceVia(nil, "x", "y"); got != nil {
		t.Errorf("nil input should pass through: %s", got)
	}
	// Non-object input passed through.
	non := []byte(`[1,2,3]`)
	if got := injectSourceVia(non, "x", "y"); string(got) != string(non) {
		t.Errorf("non-object should pass through: %s", got)
	}
	// Empty {} produces just labels (no leading comma).
	out = injectSourceVia([]byte(`{}`), "x", "y")
	if string(out) != `{"source":"x","via":"y"}` {
		t.Errorf("empty obj: %s", out)
	}
	// Both empty labels = passthrough.
	if got := injectSourceVia(in, "", ""); string(got) != string(in) {
		t.Errorf("both-empty should passthrough")
	}
}

func TestLatestPerDpu(t *testing.T) {
	t.Parallel()
	h := newTestHub(t, &fakeUpstream{hold: true}, nil)
	h.Start(context.Background())
	for i := 1; i <= 4; i++ {
		dpu := "dpu-a"
		if i%2 == 0 {
			dpu = "dpu-b"
		}
		f, _ := h.buildFrame(sampleEvent(dpu, uint64(i)))
		h.publish(f, "")
	}
	reps := h.LatestPerDpu()
	if len(reps) != 2 {
		t.Fatalf("got %d reports, want 2", len(reps))
	}
	// dpu-a most recent = id 3 → vxlan_decap 30; dpu-b id 4 → 40.
	for _, r := range reps {
		switch r.GetDpuId() {
		case "dpu-a":
			if r.GetVxlanDecap() != 30 {
				t.Errorf("dpu-a decap = %d, want 30 (latest)", r.GetVxlanDecap())
			}
		case "dpu-b":
			if r.GetVxlanDecap() != 40 {
				t.Errorf("dpu-b decap = %d, want 40 (latest)", r.GetVxlanDecap())
			}
		}
	}
}

func TestStats(t *testing.T) {
	t.Parallel()
	h := newTestHub(t, &fakeUpstream{hold: true}, nil)
	h.Start(context.Background())
	_, c, _ := h.Subscribe(SubscribeOptions{})
	defer c()
	f, _ := h.buildFrame(sampleEvent("dpu-a", 1))
	h.publish(f, "")
	time.Sleep(20 * time.Millisecond)
	s := h.Stats()
	if s.Watchers != 1 {
		t.Errorf("watchers = %d", s.Watchers)
	}
	if s.TotalPublished == 0 {
		t.Errorf("published = 0")
	}
	if s.NewestEventID != 1 {
		t.Errorf("newest = %d", s.NewestEventID)
	}
}

func TestKindLabel(t *testing.T) {
	t.Parallel()
	if kindLabel(nil) != "nil" {
		t.Errorf("nil kindLabel")
	}
	if kindLabel(&dashcenterv1.CounterEvent{Kind: dashcenterv1.CounterEvent_KIND_REPORT}) != "report" {
		t.Errorf("report kindLabel")
	}
}
