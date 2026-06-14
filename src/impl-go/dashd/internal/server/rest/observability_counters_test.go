// observability_counters_test.go — PE-3c REST/SSE handler tests.
//
// Strategy: stand up an httptest.Server with a real handler, real
// Broadcaster, and a fake CounterReader. Exercise every branch.

package rest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/observability/broadcaster"
	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeCounterReader satisfies the rest.CounterReader interface.
type fakeCounterReader struct {
	mu      sync.Mutex
	reports map[string]*dashcenterv1.CounterReport
}

func newFakeReader(reps ...*dashcenterv1.CounterReport) *fakeCounterReader {
	r := &fakeCounterReader{reports: map[string]*dashcenterv1.CounterReport{}}
	for _, rep := range reps {
		r.reports[rep.GetDpuId()] = rep
	}
	return r
}

func (r *fakeCounterReader) ListReports() []DpuCounterEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(r.reports))
	for id := range r.reports {
		ids = append(ids, id)
	}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[j] < ids[i] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	out := make([]DpuCounterEntry, 0, len(ids))
	for _, id := range ids {
		out = append(out, DpuCounterEntry{DpuID: id, Report: r.reports[id]})
	}
	return out
}

func (r *fakeCounterReader) GetReport(id string) (*dashcenterv1.CounterReport, bool) {
	r.mu.Lock()
	rep, ok := r.reports[id]
	r.mu.Unlock()
	return rep, ok
}

func newCounterTestServer(t *testing.T, withWiring bool, reps ...*dashcenterv1.CounterReport) (*httptest.Server, *broadcaster.Broadcaster, *fakeCounterReader) {
	t.Helper()
	bcast := broadcaster.NewBroadcaster(broadcaster.Config{
		MaxSubscribers:       8,
		SubscriberBufferSize: 8,
		RingSize:             16,
		EventRatePerSec:      1000,
		BurstSize:            1000,
	}, nil)
	t.Cleanup(bcast.Stop)
	reader := newFakeReader(reps...)
	h := &handler{}
	if withWiring {
		h.cntBcast = bcast
		h.cntReader = reader
	}
	ts := httptest.NewServer(h.router())
	t.Cleanup(ts.Close)
	return ts, bcast, reader
}

func sampleRep(dpu string, decap int64) *dashcenterv1.CounterReport {
	return &dashcenterv1.CounterReport{
		DpuId:      dpu,
		SampledAt:  timestamppb.Now(),
		VxlanDecap: decap,
	}
}

// ── snapshot endpoint ────────────────────────────────────────────────────

func TestCountersSnapshot_HappyPath(t *testing.T) {
	t.Parallel()
	ts, _, _ := newCounterTestServer(t, true, sampleRep("dpu-a", 1), sampleRep("dpu-b", 2))
	resp, err := http.Get(ts.URL + "/v1/observability/counters")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var env struct {
		Reports []json.RawMessage `json:"reports"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, body)
	}
	if len(env.Reports) != 2 {
		t.Errorf("got %d reports, want 2", len(env.Reports))
	}
	if !strings.Contains(string(env.Reports[0]), `"dpu_id":"dpu-a"`) {
		t.Errorf("first report missing dpu-a: %s", env.Reports[0])
	}
}

func TestCountersSnapshot_FilterByDpu(t *testing.T) {
	t.Parallel()
	ts, _, _ := newCounterTestServer(t, true, sampleRep("dpu-a", 1), sampleRep("dpu-b", 2), sampleRep("dpu-c", 3))
	resp, _ := http.Get(ts.URL + "/v1/observability/counters?dpu=dpu-b")
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var env struct {
		Reports []json.RawMessage `json:"reports"`
	}
	_ = json.Unmarshal(body, &env)
	if len(env.Reports) != 1 {
		t.Errorf("got %d reports, want 1 (filtered)", len(env.Reports))
	}
}

func TestCountersSnapshot_FilterMultipleDpus(t *testing.T) {
	t.Parallel()
	ts, _, _ := newCounterTestServer(t, true, sampleRep("dpu-a", 1), sampleRep("dpu-b", 2), sampleRep("dpu-c", 3))
	resp, _ := http.Get(ts.URL + "/v1/observability/counters?dpu=dpu-a&dpu=dpu-c")
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var env struct {
		Reports []json.RawMessage `json:"reports"`
	}
	_ = json.Unmarshal(body, &env)
	if len(env.Reports) != 2 {
		t.Errorf("got %d reports, want 2", len(env.Reports))
	}
}

func TestCountersSnapshot_NotWired_503(t *testing.T) {
	t.Parallel()
	ts, _, _ := newCounterTestServer(t, false)
	resp, _ := http.Get(ts.URL + "/v1/observability/counters")
	defer resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// ── SSE stream ───────────────────────────────────────────────────────────

func TestCountersStream_SnapshotThenLive(t *testing.T) {
	t.Parallel()
	ts, bcast, _ := newCounterTestServer(t, true, sampleRep("dpu-a", 1))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/v1/observability/counters/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("content-type = %q", resp.Header.Get("Content-Type"))
	}

	// Use a goroutine to read the first 2 events: snapshot + live.
	doneCh := make(chan struct{})
	frames := []string{}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				frames = append(frames, string(buf[:n]))
			}
			if err != nil {
				close(doneCh)
				return
			}
			if len(frames) >= 2 {
				cancel()
			}
		}
	}()

	// give snapshot a moment to arrive, then publish a live event.
	time.Sleep(50 * time.Millisecond)
	bcast.Publish(sampleRep("dpu-a", 999))

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		cancel()
		<-doneCh
	}

	full := strings.Join(frames, "")
	if !strings.Contains(full, "event: snapshot") {
		t.Errorf("missing snapshot event in: %s", full)
	}
	if !strings.Contains(full, "event: report") {
		t.Errorf("missing report event in: %s", full)
	}
	if !strings.Contains(full, `"dpu_id":"dpu-a"`) {
		t.Errorf("missing dpu_id in: %s", full)
	}
}

func TestCountersStream_FilterByDpu(t *testing.T) {
	t.Parallel()
	ts, bcast, _ := newCounterTestServer(t, true, sampleRep("dpu-a", 1), sampleRep("dpu-b", 2))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/v1/observability/counters/stream?dpu=dpu-b", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	doneCh := make(chan struct{})
	frames := ""
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				frames += string(buf[:n])
			}
			if err != nil {
				close(doneCh)
				return
			}
		}
	}()
	time.Sleep(50 * time.Millisecond)
	bcast.Publish(sampleRep("dpu-a", 100)) // filtered out
	bcast.Publish(sampleRep("dpu-b", 200))
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-doneCh

	if strings.Contains(frames, "dpu-a") {
		t.Errorf("dpu-a should have been filtered out; saw it in: %s", frames)
	}
	if !strings.Contains(frames, "dpu-b") {
		t.Errorf("dpu-b missing: %s", frames)
	}
}

func TestCountersStream_LastEventIDHeader_TriggersResync(t *testing.T) {
	t.Parallel()
	ts, _, _ := newCounterTestServer(t, true, sampleRep("dpu-a", 1))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/v1/observability/counters/stream", nil)
	req.Header.Set("Last-Event-ID", "999") // cursor beyond any ring
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	doneCh := make(chan struct{})
	frames := ""
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				frames += string(buf[:n])
				if strings.Contains(frames, "event: resync") {
					cancel()
				}
			}
			if err != nil {
				close(doneCh)
				return
			}
		}
	}()
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		cancel()
		<-doneCh
	}
	if !strings.Contains(frames, "event: resync") {
		t.Errorf("missing resync sentinel in: %s", frames)
	}
	// Snapshot phase MUST be skipped when a cursor is supplied.
	if strings.Contains(frames, "event: snapshot") {
		t.Errorf("snapshot emitted with cursor present; should be skipped: %s", frames)
	}
}

func TestCountersStream_LastEventIDQuery(t *testing.T) {
	t.Parallel()
	ts, _, _ := newCounterTestServer(t, true, sampleRep("dpu-a", 1))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/v1/observability/counters/stream?last_event_id=999", nil)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	doneCh := make(chan struct{})
	frames := ""
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				frames += string(buf[:n])
				if strings.Contains(frames, "resync") {
					cancel()
				}
			}
			if err != nil {
				close(doneCh)
				return
			}
		}
	}()
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		cancel()
		<-doneCh
	}
	if !strings.Contains(frames, "resync") {
		t.Errorf("query param last_event_id not honoured: %s", frames)
	}
}

func TestCountersStream_NotWired_503(t *testing.T) {
	t.Parallel()
	ts, _, _ := newCounterTestServer(t, false)
	resp, _ := http.Get(ts.URL + "/v1/observability/counters/stream")
	defer resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func TestCountersStream_TooManySubscribers_429(t *testing.T) {
	t.Parallel()
	// Construct a server with cap=1 to force 429 on the 2nd connect.
	bcast := broadcaster.NewBroadcaster(broadcaster.Config{
		MaxSubscribers:       1,
		SubscriberBufferSize: 4,
		RingSize:             8,
		EventRatePerSec:      100,
		BurstSize:            100,
	}, nil)
	defer bcast.Stop()
	reader := newFakeReader(sampleRep("dpu-a", 1))
	h := &handler{cntBcast: bcast, cntReader: reader}
	ts := httptest.NewServer(h.router())
	defer ts.Close()

	// First subscriber holds the slot.
	ctx1, cancel1 := context.WithCancel(context.Background())
	req1, _ := http.NewRequestWithContext(ctx1, "GET", ts.URL+"/v1/observability/counters/stream", nil)
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("first GET: %v", err)
	}
	defer resp1.Body.Close()
	// Read at least one byte to ensure subscribe completed.
	buf := make([]byte, 256)
	_, _ = resp1.Body.Read(buf)

	// Second subscriber should get 429.
	resp2, err := http.Get(ts.URL + "/v1/observability/counters/stream")
	if err != nil {
		t.Fatalf("second GET: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", resp2.StatusCode)
	}
	if resp2.Header.Get("Retry-After") == "" {
		t.Errorf("missing Retry-After header")
	}
	cancel1()
}

func TestCountersStream_DropOnSlow_EmitsDroppedEvent(t *testing.T) {
	t.Parallel()
	// Tiny per-sub buffer + flood publishes → must see "event: dropped".
	bcast := broadcaster.NewBroadcaster(broadcaster.Config{
		MaxSubscribers:       4,
		SubscriberBufferSize: 2,
		RingSize:             32,
		EventRatePerSec:      10000,
		BurstSize:            10000,
	}, nil)
	defer bcast.Stop()
	reader := newFakeReader(sampleRep("dpu-a", 1))
	h := &handler{cntBcast: bcast, cntReader: reader}
	ts := httptest.NewServer(h.router())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/v1/observability/counters/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	// Drain snapshot to free up buffer briefly.
	buf := make([]byte, 4096)
	_, _ = resp.Body.Read(buf)
	// Stop reading. Flood publishes. Then read again.
	for i := 0; i < 200; i++ {
		bcast.Publish(sampleRep("dpu-a", int64(i)))
	}
	// Now read continuously and look for "event: dropped".
	doneCh := make(chan struct{})
	frames := ""
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				frames += string(buf[:n])
				if strings.Contains(frames, "event: dropped") {
					cancel()
				}
			}
			if err != nil {
				close(doneCh)
				return
			}
		}
	}()
	select {
	case <-doneCh:
	case <-time.After(3 * time.Second):
		cancel()
		<-doneCh
	}
	if !strings.Contains(frames, "event: dropped") {
		t.Errorf("never saw KIND_DROPPED sentinel in: %s", frames)
	}
}

func TestParseLastEventID_HeaderTakesPrecedence(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest("GET", "/x?last_event_id=7", nil)
	req.Header.Set("Last-Event-ID", "42")
	if got := parseLastEventID(req); got != 42 {
		t.Errorf("got %d, want 42 (header wins)", got)
	}
}

func TestParseLastEventID_QueryFallback(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest("GET", "/x?last_event_id=99", nil)
	if got := parseLastEventID(req); got != 99 {
		t.Errorf("got %d, want 99", got)
	}
}

func TestParseLastEventID_Invalid_ReturnsZero(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest("GET", "/x?last_event_id=junk", nil)
	req.Header.Set("Last-Event-ID", "garbage")
	if got := parseLastEventID(req); got != 0 {
		t.Errorf("got %d, want 0 (invalid → no cursor)", got)
	}
}

func TestParseLastEventID_None_ReturnsZero(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest("GET", "/x", nil)
	if got := parseLastEventID(req); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

// ── helper-coverage ──────────────────────────────────────────────────────

func TestCounterSSEEventName(t *testing.T) {
	t.Parallel()
	cases := map[dashcenterv1.CounterEvent_Kind]string{
		dashcenterv1.CounterEvent_KIND_SNAPSHOT:     "snapshot",
		dashcenterv1.CounterEvent_KIND_REPORT:       "report",
		dashcenterv1.CounterEvent_KIND_KEEPALIVE:    "keepalive",
		dashcenterv1.CounterEvent_KIND_DROPPED:      "dropped",
		dashcenterv1.CounterEvent_KIND_RATE_LIMITED: "rate_limited",
		dashcenterv1.CounterEvent_KIND_RESYNC:       "resync",
		dashcenterv1.CounterEvent_KIND_UNSPECIFIED:  "unknown",
	}
	for k, want := range cases {
		if got := counterSSEEventName(k); got != want {
			t.Errorf("counterSSEEventName(%v) = %q, want %q", k, got, want)
		}
	}
}

func TestDpuFilterSet_RestVariants(t *testing.T) {
	t.Parallel()
	if got := dpuFilterSet(nil); got != nil {
		t.Errorf("nil → %v", got)
	}
	if got := dpuFilterSet([]string{"", ""}); got != nil {
		t.Errorf("all-empty → %v", got)
	}
	got := dpuFilterSet([]string{"a", "", "b"})
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestWriteSSECounterFrame_NilNoop(t *testing.T) {
	t.Parallel()
	// nil frame → no panic, no write.
	w := httptest.NewRecorder()
	if err := writeSSECounterFrame(w, w, nil); err != nil {
		t.Errorf("nil frame returned err: %v", err)
	}
	if w.Body.Len() != 0 {
		t.Errorf("nil frame wrote bytes: %s", w.Body.String())
	}
	// frame with nil event → no write.
	if err := writeSSECounterFrame(w, w, &broadcaster.Frame{}); err != nil {
		t.Errorf("nil event returned err: %v", err)
	}
}

var _ = errors.Is // keep import used in case of further branches
