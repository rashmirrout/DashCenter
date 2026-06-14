// observability_test.go — PE-3c REST client tests for
// GetCountersSnapshot + StreamCounters.

package rest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
)

// newRESTClientFor builds a Client pointed at ts.URL. Uses the
// direct-struct construction pattern from topology_test.go so tests
// stay independent of factory wiring.
func newRESTClientFor(t *testing.T, ts *httptest.Server) *Client {
	t.Helper()
	return &Client{baseURL: ts.URL, httpc: &http.Client{Transport: ts.Client().Transport}}
}

func TestGetCountersSnapshot_DecodesEnvelope(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/observability/counters" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		// protojson encodes int64 fields as strings — matches dashd's wire format.
		_, _ = w.Write([]byte(`{"reports":[{"dpu_id":"dpu-a","vxlan_decap":"42"},{"dpu_id":"dpu-b","vxlan_decap":"99"}]}`))
	}))
	defer ts.Close()
	c := newRESTClientFor(t, ts)
	snap, err := c.GetCountersSnapshot(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetCountersSnapshot: %v", err)
	}
	if len(snap.Reports) != 2 {
		t.Fatalf("got %d reports, want 2", len(snap.Reports))
	}
	if snap.Reports[0].DpuId != "dpu-a" || snap.Reports[0].VxlanDecap != "42" {
		t.Errorf("first report = %+v", snap.Reports[0])
	}
}

func TestGetCountersSnapshot_PassesDpuFilter(t *testing.T) {
	t.Parallel()
	var seen []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Query()["dpu"]
		_, _ = w.Write([]byte(`{"reports":[]}`))
	}))
	defer ts.Close()
	c := newRESTClientFor(t, ts)
	_, err := c.GetCountersSnapshot(context.Background(), []string{"dpu-a", "dpu-b"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(seen) != 2 || seen[0] != "dpu-a" || seen[1] != "dpu-b" {
		t.Errorf("dpu params = %v", seen)
	}
}

func TestGetCountersSnapshot_SkipsEmptyDpuIds(t *testing.T) {
	t.Parallel()
	var seen []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Query()["dpu"]
		_, _ = w.Write([]byte(`{"reports":[]}`))
	}))
	defer ts.Close()
	c := newRESTClientFor(t, ts)
	_, _ = c.GetCountersSnapshot(context.Background(), []string{"", "dpu-a", ""})
	if len(seen) != 1 || seen[0] != "dpu-a" {
		t.Errorf("dpu params = %v (empty entries should be skipped)", seen)
	}
}

func TestStreamCounters_RequiresOnEvent(t *testing.T) {
	t.Parallel()
	c := &Client{baseURL: "http://x"}
	err := c.StreamCounters(context.Background(), client.CountersWatchOptions{})
	if err == nil || !strings.Contains(err.Error(), "requires OnEvent") {
		t.Errorf("err = %v", err)
	}
}

func TestStreamCounters_ParsesEventsAndSendsLastEventID(t *testing.T) {
	t.Parallel()
	var (
		mu          sync.Mutex
		sawLastID   string
		sawDpu      []string
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sawLastID = r.Header.Get("Last-Event-ID")
		sawDpu = r.URL.Query()["dpu"]
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		// 3 events: snapshot, report, keepalive. Hold open until ctx
		// done so OnEvent's sentinel can stop the loop deterministically.
		fmt.Fprint(w, "event: snapshot\nid: 1\ndata: {\"kind\":\"KIND_SNAPSHOT\",\"event_id\":1,\"report\":{\"dpu_id\":\"dpu-a\",\"vxlan_decap\":\"5\"}}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "event: report\nid: 2\ndata: {\"kind\":\"KIND_REPORT\",\"event_id\":2,\"report\":{\"dpu_id\":\"dpu-a\",\"vxlan_decap\":\"6\"}}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "event: keepalive\nid: 3\ndata: {\"kind\":\"KIND_KEEPALIVE\",\"event_id\":3,\"notice\":{\"message\":\"keepalive\"}}\n\n")
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer ts.Close()
	c := newRESTClientFor(t, ts)

	got := []client.CounterEvent{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := c.StreamCounters(ctx, client.CountersWatchOptions{
		LastEventID: 42,
		DpuIDs:      []string{"dpu-a"},
		OnEvent: func(ev client.CounterEvent) error {
			got = append(got, ev)
			if len(got) == 3 {
				return errStopSentinel{}
			}
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected sentinel error to stop the stream")
	}
	if _, ok := err.(errStopSentinel); !ok {
		t.Fatalf("err = %v (want sentinel)", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3", len(got))
	}
	if got[0].Kind != "KIND_SNAPSHOT" || got[1].Kind != "KIND_REPORT" || got[2].Kind != "KIND_KEEPALIVE" {
		t.Errorf("event kinds = %v %v %v", got[0].Kind, got[1].Kind, got[2].Kind)
	}
	if got[0].Report == nil || got[0].Report.VxlanDecap != "5" {
		t.Errorf("snapshot report wrong: %+v", got[0].Report)
	}
	if got[2].Notice == nil || got[2].Notice.Message != "keepalive" {
		t.Errorf("keepalive notice wrong: %+v", got[2].Notice)
	}
	mu.Lock()
	defer mu.Unlock()
	if sawLastID != "42" {
		t.Errorf("Last-Event-ID header = %q, want 42", sawLastID)
	}
	if len(sawDpu) != 1 || sawDpu[0] != "dpu-a" {
		t.Errorf("dpu query = %v", sawDpu)
	}
}

func TestStreamCounters_MultiLineDataReassembled(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher := w.(http.Flusher)
		// Split JSON across two data: lines.
		fmt.Fprint(w, "event: report\ndata: {\"kind\":\"KIND_REPORT\",\n")
		fmt.Fprint(w, "data: \"event_id\":7,\"report\":{\"dpu_id\":\"dpu-x\"}}\n\n")
		flusher.Flush()
	}))
	defer ts.Close()
	c := newRESTClientFor(t, ts)
	var got *client.CounterEvent
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.StreamCounters(ctx, client.CountersWatchOptions{
		OnEvent: func(ev client.CounterEvent) error {
			got = &ev
			return errStopSentinel{}
		},
	})
	if got == nil || got.EventID != 7 || got.Report == nil || got.Report.DpuId != "dpu-x" {
		t.Errorf("multi-line not reassembled: %+v", got)
	}
}

func TestStreamCounters_HTTPError(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad", http.StatusInternalServerError)
	}))
	defer ts.Close()
	c := newRESTClientFor(t, ts)
	err := c.StreamCounters(context.Background(), client.CountersWatchOptions{
		OnEvent: func(client.CounterEvent) error { return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("err = %v, want HTTP 500", err)
	}
}

func TestStreamCounters_CtxCancel(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "event: keepalive\ndata: {\"kind\":\"KIND_KEEPALIVE\"}\n\n")
		flusher.Flush()
		// hang forever
		<-r.Context().Done()
	}))
	defer ts.Close()
	c := newRESTClientFor(t, ts)
	ctx, cancel := context.WithCancel(context.Background())
	gotCh := make(chan client.CounterEvent, 1)
	go func() {
		_ = c.StreamCounters(ctx, client.CountersWatchOptions{
			OnEvent: func(ev client.CounterEvent) error {
				gotCh <- ev
				return nil
			},
		})
	}()
	select {
	case ev := <-gotCh:
		if ev.Kind != "KIND_KEEPALIVE" {
			t.Errorf("got %v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("never got first event")
	}
	cancel()
	// Give the goroutine a moment to exit.
	time.Sleep(100 * time.Millisecond)
}

// errStopSentinel — local sentinel to stop a stream in tests.
type errStopSentinel struct{}

func (errStopSentinel) Error() string { return "stop" }
