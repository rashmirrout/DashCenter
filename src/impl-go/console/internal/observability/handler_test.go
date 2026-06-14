// handler_test.go — PE-3c HTTP handler tests for the counter Hub.
//
// Uses a custom flushRecorder for SSE so handlers can be invoked
// synchronously and we never need httptest.NewServer for streaming.
// hub_test.go exercises the multiplexing + reconnect + GC under load;
// this file keeps handler-only logic tight.

package observability

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

func httpHub(t *testing.T) (*HTTPHandler, *Hub) {
	t.Helper()
	h := newTestHub(t, &fakeUpstream{hold: true}, func(c *HubConfig) {
		c.UpstreamIdleGC = 30 * time.Millisecond
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h.Start(ctx)
	return NewHTTPHandler(h), h
}

// flushRecorder is a minimal ResponseWriter + Flusher that captures
// the body progressively (unlike httptest.ResponseRecorder which only
// exposes the body after the handler returns; we need progressive
// reads to test SSE).
type flushRecorder struct {
	mu     sync.Mutex
	header http.Header
	body   []byte
	code   int
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{header: http.Header{}, code: 200}
}
func (r *flushRecorder) Header() http.Header { return r.header }
func (r *flushRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	r.body = append(r.body, p...)
	r.mu.Unlock()
	return len(p), nil
}
func (r *flushRecorder) WriteHeader(code int) { r.code = code }
func (r *flushRecorder) Flush()               {} // no-op; Write already accumulates
func (r *flushRecorder) Body() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.body)
}

// runSSEUntil invokes handler synchronously in a goroutine driven by
// req with a cancellable ctx, polls the flushRecorder body until
// predicate matches or deadline fires, then cancels ctx and waits
// for the handler goroutine to exit.
func runSSEUntil(t *testing.T, handler http.HandlerFunc, req *http.Request, deadline time.Duration, predicate func(string) bool) (status int, body string) {
	t.Helper()
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)
	w := newFlushRecorder()
	doneCh := make(chan struct{})
	go func() {
		handler(w, req)
		close(doneCh)
	}()
	t0 := time.Now()
	for time.Since(t0) < deadline {
		if predicate(w.Body()) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("handler did not return after ctx cancel; body=%s", w.Body())
	}
	return w.code, w.Body()
}

// ── snapshot tests ──────────────────────────────────────────────────────

func TestSnapshot_EmptyEnvelope(t *testing.T) {
	t.Parallel()
	hh, _ := httpHub(t)
	req := httptest.NewRequest("GET", "/api/console/counters", nil)
	w := httptest.NewRecorder()
	hh.Snapshot(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"reports":`) {
		t.Errorf("missing reports envelope: %s", w.Body)
	}
}

func TestSnapshot_ReturnsLatestPerDpu(t *testing.T) {
	t.Parallel()
	hh, hub := httpHub(t)
	for i := 1; i <= 3; i++ {
		f, _ := hub.buildFrame(sampleEvent("dpu-a", uint64(i)))
		hub.publish(f, "")
	}
	req := httptest.NewRequest("GET", "/api/console/counters", nil)
	w := httptest.NewRecorder()
	hh.Snapshot(w, req)
	var env struct {
		Reports []json.RawMessage `json:"reports"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	if len(env.Reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(env.Reports))
	}
	if !strings.Contains(string(env.Reports[0]), `"vxlan_decap":"30"`) {
		t.Errorf("expected latest decap=30: %s", env.Reports[0])
	}
}

func TestSnapshot_FilterByDpu(t *testing.T) {
	t.Parallel()
	hh, hub := httpHub(t)
	for _, d := range []string{"dpu-a", "dpu-b", "dpu-c"} {
		f, _ := hub.buildFrame(sampleEvent(d, 1))
		hub.publish(f, "")
	}
	req := httptest.NewRequest("GET", "/api/console/counters?dpu=dpu-b", nil)
	w := httptest.NewRecorder()
	hh.Snapshot(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "dpu-b") {
		t.Errorf("missing dpu-b: %s", body)
	}
	if strings.Contains(body, "dpu-a") || strings.Contains(body, "dpu-c") {
		t.Errorf("filter leaked: %s", body)
	}
}

// ── SSE tests (synchronous via flushRecorder) ───────────────────────────

func TestSSE_HeadersAndStream(t *testing.T) {
	t.Parallel()
	hh, hub := httpHub(t)
	// Publish snapshot data before subscribe so cold-start has content.
	f, _ := hub.buildFrame(sampleEvent("dpu-x", 7))
	hub.publish(f, "")

	req := httptest.NewRequest("GET", "/api/console/counters/stream", nil)
	// After ~50ms publish a live event so we see both snapshot + report.
	go func() {
		time.Sleep(50 * time.Millisecond)
		live, _ := hub.buildFrame(sampleEvent("dpu-x", 8))
		hub.publish(live, "")
	}()

	code, body := runSSEUntil(t, hh.SSE, req, 2*time.Second, func(s string) bool {
		return strings.Count(s, "dpu-x") >= 2
	})
	if code != 200 {
		t.Errorf("code = %d", code)
	}
	if !strings.Contains(body, "event: snapshot") {
		t.Errorf("missing snapshot in:\n%s", body)
	}
	if !strings.Contains(body, "event: report") {
		t.Errorf("missing report in:\n%s", body)
	}
}

func TestSSE_TooManyWatchers_429(t *testing.T) {
	t.Parallel()
	hub := newTestHub(t, &fakeUpstream{hold: true}, func(c *HubConfig) { c.MaxWatchers = 1 })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.Start(ctx)
	hh := NewHTTPHandler(hub)

	subCtx, subCancel := context.WithCancel(context.Background())
	defer subCancel()
	subReq := httptest.NewRequest("GET", "/api/console/counters/stream", nil).WithContext(subCtx)
	subW := newFlushRecorder()
	subDone := make(chan struct{})
	go func() {
		hh.SSE(subW, subReq)
		close(subDone)
	}()
	time.Sleep(50 * time.Millisecond) // let first Subscribe land

	req := httptest.NewRequest("GET", "/api/console/counters/stream", nil)
	w := httptest.NewRecorder()
	hh.SSE(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("code = %d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Errorf("missing Retry-After")
	}
	subCancel()
	select {
	case <-subDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first SSE handler did not exit after ctx cancel")
	}
}

func TestSSE_LastEventID_TriggersResync(t *testing.T) {
	t.Parallel()
	hh, hub := httpHub(t)
	for i := 1; i <= 3; i++ {
		f, _ := hub.buildFrame(sampleEvent("dpu-a", uint64(i)))
		hub.publish(f, "")
	}
	req := httptest.NewRequest("GET", "/api/console/counters/stream", nil)
	req.Header.Set("Last-Event-ID", "999")
	code, body := runSSEUntil(t, hh.SSE, req, 2*time.Second, func(s string) bool {
		return strings.Contains(s, "event: resync")
	})
	if code != 200 {
		t.Errorf("code = %d", code)
	}
	if !strings.Contains(body, "event: resync") {
		t.Errorf("missing resync: %s", body)
	}
}

// ── helpers + small handlers ────────────────────────────────────────────

func TestAdminStats(t *testing.T) {
	t.Parallel()
	hh, _ := httpHub(t)
	req := httptest.NewRequest("GET", "/api/console/counters/_stats", nil)
	w := httptest.NewRecorder()
	hh.AdminStats(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"Watchers"`) {
		t.Errorf("missing Watchers: %s", w.Body)
	}
}

func TestParseLastEventID_Variants(t *testing.T) {
	t.Parallel()
	r1 := httptest.NewRequest("GET", "/?last_event_id=7", nil)
	r1.Header.Set("Last-Event-ID", "42")
	if got := parseLastEventID(r1); got != 42 {
		t.Errorf("header wins: %d", got)
	}
	r2 := httptest.NewRequest("GET", "/?last_event_id=99", nil)
	if got := parseLastEventID(r2); got != 99 {
		t.Errorf("query fallback: %d", got)
	}
	r3 := httptest.NewRequest("GET", "/?last_event_id=junk", nil)
	r3.Header.Set("Last-Event-ID", "garbage")
	if got := parseLastEventID(r3); got != 0 {
		t.Errorf("invalid → 0: %d", got)
	}
	r4 := httptest.NewRequest("GET", "/", nil)
	if got := parseLastEventID(r4); got != 0 {
		t.Errorf("missing → 0: %d", got)
	}
}

func TestClientIPFor(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:54321"
	if got := clientIPFor(r); got != "10.0.0.1" {
		t.Errorf("RemoteAddr strip: %q", got)
	}
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.Header.Set("X-Real-IP", "1.2.3.4")
	if got := clientIPFor(r2); got != "1.2.3.4" {
		t.Errorf("X-Real-IP: %q", got)
	}
	r3 := httptest.NewRequest("GET", "/", nil)
	r3.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")
	if got := clientIPFor(r3); got != "1.1.1.1" {
		t.Errorf("XFF first: %q", got)
	}
	r4 := httptest.NewRequest("GET", "/", nil)
	r4.Header.Set("X-Forwarded-For", "3.3.3.3")
	if got := clientIPFor(r4); got != "3.3.3.3" {
		t.Errorf("XFF single: %q", got)
	}
}

func TestSSEEventName(t *testing.T) {
	t.Parallel()
	pairs := map[dashcenterv1.CounterEvent_Kind]string{
		dashcenterv1.CounterEvent_KIND_SNAPSHOT:     "snapshot",
		dashcenterv1.CounterEvent_KIND_REPORT:       "report",
		dashcenterv1.CounterEvent_KIND_KEEPALIVE:    "keepalive",
		dashcenterv1.CounterEvent_KIND_DROPPED:      "dropped",
		dashcenterv1.CounterEvent_KIND_RATE_LIMITED: "rate_limited",
		dashcenterv1.CounterEvent_KIND_RESYNC:       "resync",
		dashcenterv1.CounterEvent_KIND_UNSPECIFIED:  "unknown",
	}
	for k, want := range pairs {
		if got := sseEventName(k); got != want {
			t.Errorf("sseEventName(%v) = %q, want %q", k, got, want)
		}
	}
}

func TestDpuFilterSet(t *testing.T) {
	t.Parallel()
	if got := dpuFilterSet(nil); got != nil {
		t.Errorf("nil → %v", got)
	}
	if got := dpuFilterSet([]string{"", ""}); got != nil {
		t.Errorf("all-empty → %v", got)
	}
	got := dpuFilterSet([]string{"a", "", "b"})
	if len(got) != 2 {
		t.Errorf("len = %d", len(got))
	}
}

func TestWriteSSEFrame_Nil(t *testing.T) {
	t.Parallel()
	w := newFlushRecorder()
	if err := writeSSEFrame(w, w, nil); err != nil {
		t.Errorf("nil frame: %v", err)
	}
	if w.Body() != "" {
		t.Errorf("wrote bytes: %s", w.Body())
	}
}

func TestWriteJSONErr(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	writeJSONErr(w, 500, "oops")
	if !strings.Contains(w.Body.String(), `"error":"oops"`) {
		t.Errorf("body = %s", w.Body)
	}
}

func TestWriteSSE_Helper(t *testing.T) {
	t.Parallel()
	w := newFlushRecorder()
	writeSSE(w, w, "evt", map[string]string{"k": "v"})
	out := w.Body()
	if !strings.Contains(out, "event: evt") || !strings.Contains(out, `"k":"v"`) {
		t.Errorf("writeSSE output: %s", out)
	}
}

var _ = io.EOF
