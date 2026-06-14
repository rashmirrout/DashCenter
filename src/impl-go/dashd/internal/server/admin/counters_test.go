// counters_test.go covers the PE-3b admin endpoints. Uses a stub
// CountersStore + CountersPoller via SetCountersWiring so the tests
// don't depend on the polling goroutine.

package admin

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	dccnt "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/counters"
	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

// --- stubs ---

type stubStore struct {
	get  func(string) (*dccnt.Entry, bool)
	list func() []*dccnt.Entry
}

func (s *stubStore) Get(id string) (*dccnt.Entry, bool) { return s.get(id) }
func (s *stubStore) List() []*dccnt.Entry               { return s.list() }

type stubPoller struct {
	enabled atomic.Bool
	intNs   atomic.Int64
}

func (p *stubPoller) Enabled() bool              { return p.enabled.Load() }
func (p *stubPoller) Interval() time.Duration    { return time.Duration(p.intNs.Load()) }
func (p *stubPoller) SetEnabled(b bool)          { p.enabled.Store(b) }
func (p *stubPoller) SetInterval(d time.Duration) { p.intNs.Store(int64(d)) }

// dccntStore / dccntPoller aliases match the public interface names
// declared in counters.go so the stubs satisfy them statically.
type dccntStore = CountersStore
type dccntPoller = CountersPoller

// postJSON sends a JSON-encoded body to the fixture's admin server.
func (f *adminFixture) postJSON(t *testing.T, path string, body any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	resp, err := http.Post(f.ts.URL+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// setupAdminWithCounters mirrors setupAdmin but exposes the *Server
// so we can wire the counters stubs after construction.
func setupAdminWithCounters(t *testing.T, store CountersStore, poller CountersPoller) *adminFixture {
	t.Helper()
	f := setupAdmin(t)
	// Rebuild a parallel server so we can keep the test deps tight.
	srv := New(f.inv, f.store, f.obs, nil)
	srv.SetCountersWiring(store, poller)
	// f.ts already running on the old handler from setupAdmin; replace.
	f.ts.Close()
	ts := httptest.NewServer(srv.srv.Handler)
	t.Cleanup(ts.Close)
	f.ts = ts
	return f
}

// --- tests ---

func TestCounters_NotWired_503(t *testing.T) {
	f := setupAdmin(t)
	for _, path := range []string{"/admin/counters", "/admin/counters?dpu=x"} {
		code, body := f.get(t, path)
		if code != http.StatusServiceUnavailable {
			t.Errorf("%s: code=%d, want 503; body=%s", path, code, body)
		}
	}
}

func TestCountersList_Empty(t *testing.T) {
	store := &stubStore{
		get:  func(string) (*dccnt.Entry, bool) { return nil, false },
		list: func() []*dccnt.Entry { return nil },
	}
	f := setupAdminWithCounters(t, store, &stubPoller{})

	code, body := f.get(t, "/admin/counters")
	if code != http.StatusOK {
		t.Fatalf("code=%d, body=%s", code, body)
	}
	var resp struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Entries) != 0 {
		t.Errorf("entries=%d, want 0", len(resp.Entries))
	}
}

func TestCountersList_DpuMissing_404(t *testing.T) {
	store := &stubStore{
		get:  func(string) (*dccnt.Entry, bool) { return nil, false },
		list: func() []*dccnt.Entry { return nil },
	}
	f := setupAdminWithCounters(t, store, &stubPoller{})
	code, body := f.get(t, "/admin/counters?dpu=unknown")
	if code != http.StatusNotFound {
		t.Errorf("code=%d, want 404; body=%s", code, body)
	}
}

func TestCountersList_DpuPresent(t *testing.T) {
	now := time.Now().UTC()
	entry := &dccnt.Entry{
		DpuID:    "dpu-a",
		UpdateAt: now,
		Report: &dashcenterv1.CounterReport{
			DpuId:      "dpu-a",
			SampledAt:  timestamppb.New(now),
			VxlanDecap: 123, VxlanEncap: 456, DropAclIn: 7,
		},
		PerEni: map[string]*dashcenterv1.CounterReport{
			"eni-001": {VxlanDecap: 5},
		},
		PerVnet: map[string]*dashcenterv1.CounterReport{
			"vnet-prod": {VxlanDecap: 9},
		},
	}
	store := &stubStore{
		get: func(id string) (*dccnt.Entry, bool) {
			if id == "dpu-a" {
				return entry, true
			}
			return nil, false
		},
		list: func() []*dccnt.Entry { return []*dccnt.Entry{entry} },
	}
	f := setupAdminWithCounters(t, store, &stubPoller{})

	code, body := f.get(t, "/admin/counters?dpu=dpu-a")
	if code != http.StatusOK {
		t.Fatalf("code=%d, body=%s", code, body)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["dpu_id"] != "dpu-a" {
		t.Errorf("dpu_id = %v, want dpu-a", got["dpu_id"])
	}
	report, _ := got["report"].(map[string]any)
	if report == nil {
		t.Fatalf("missing report block; full=%v", got)
	}
	// protojson encodes int64 as JSON string to preserve precision.
	// UseProtoNames=true gives snake_case field names.
	if gotV, want := report["vxlan_decap"], "123"; gotV != want {
		t.Errorf("vxlan_decap = %v (%T), want %v (string)", gotV, gotV, want)
	}
	perEni, _ := got["per_eni"].(map[string]any)
	if perEni == nil || perEni["eni-001"] == nil {
		t.Errorf("per_eni missing entry; got %v", perEni)
	}
}

func TestCountersList_All_Sorted(t *testing.T) {
	entries := []*dccnt.Entry{
		{DpuID: "dpu-a", Report: &dashcenterv1.CounterReport{DpuId: "dpu-a"}},
		{DpuID: "dpu-b", Report: &dashcenterv1.CounterReport{DpuId: "dpu-b"}},
	}
	store := &stubStore{
		get:  func(id string) (*dccnt.Entry, bool) { return nil, false },
		list: func() []*dccnt.Entry { return entries },
	}
	f := setupAdminWithCounters(t, store, &stubPoller{})

	code, body := f.get(t, "/admin/counters")
	if code != http.StatusOK {
		t.Fatalf("code=%d body=%s", code, body)
	}
	var resp struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("len = %d, want 2", len(resp.Entries))
	}
	if resp.Entries[0]["dpu_id"] != "dpu-a" || resp.Entries[1]["dpu_id"] != "dpu-b" {
		t.Errorf("order = %v / %v, want dpu-a/dpu-b", resp.Entries[0]["dpu_id"], resp.Entries[1]["dpu_id"])
	}
}

func TestCountersPollInterval_Roundtrip(t *testing.T) {
	p := &stubPoller{}
	p.SetInterval(time.Second)
	store := &stubStore{
		get:  func(string) (*dccnt.Entry, bool) { return nil, false },
		list: func() []*dccnt.Entry { return nil },
	}
	f := setupAdminWithCounters(t, store, p)

	code, body := f.postJSON(t, "/admin/counters/poll-interval", map[string]any{"interval": "3s"})
	if code != http.StatusOK {
		t.Fatalf("code=%d body=%s", code, body)
	}
	var got map[string]any
	_ = json.Unmarshal(body, &got)
	if got["interval"] != "3s" {
		t.Errorf("interval echo = %v, want 3s", got["interval"])
	}
	if p.Interval() != 3*time.Second {
		t.Errorf("poller.Interval = %v, want 3s", p.Interval())
	}
}

func TestCountersPollInterval_BadInputs(t *testing.T) {
	p := &stubPoller{}
	f := setupAdminWithCounters(t,
		&stubStore{get: func(string) (*dccnt.Entry, bool) { return nil, false }, list: func() []*dccnt.Entry { return nil }},
		p,
	)

	cases := []struct {
		name string
		body map[string]any
		code int
		hint string
	}{
		{"empty", map[string]any{}, http.StatusBadRequest, "interval is required"},
		{"unparseable", map[string]any{"interval": "soon"}, http.StatusBadRequest, "parse interval"},
		{"negative", map[string]any{"interval": "-1s"}, http.StatusBadRequest, `must be \u003e 0`},
		{"zero", map[string]any{"interval": "0s"}, http.StatusBadRequest, `must be \u003e 0`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := f.postJSON(t, "/admin/counters/poll-interval", tc.body)
			if code != tc.code {
				t.Errorf("code=%d, want %d; body=%s", code, tc.code, body)
			}
			if !bytes.Contains(body, []byte(tc.hint)) {
				t.Errorf("body=%s, want hint %q", body, tc.hint)
			}
		})
	}
}

func TestCountersEnable_Roundtrip(t *testing.T) {
	p := &stubPoller{}
	p.SetEnabled(false)
	f := setupAdminWithCounters(t,
		&stubStore{get: func(string) (*dccnt.Entry, bool) { return nil, false }, list: func() []*dccnt.Entry { return nil }},
		p,
	)

	code, body := f.postJSON(t, "/admin/counters/enable", map[string]any{"enabled": true})
	if code != http.StatusOK {
		t.Fatalf("code=%d body=%s", code, body)
	}
	if !p.Enabled() {
		t.Errorf("poller still disabled after POST")
	}

	code, body = f.postJSON(t, "/admin/counters/enable", map[string]any{"enabled": false})
	if code != http.StatusOK {
		t.Fatalf("code=%d body=%s", code, body)
	}
	if p.Enabled() {
		t.Errorf("poller still enabled after disable POST")
	}
}

func TestCountersEnable_RequiresField(t *testing.T) {
	p := &stubPoller{}
	f := setupAdminWithCounters(t,
		&stubStore{get: func(string) (*dccnt.Entry, bool) { return nil, false }, list: func() []*dccnt.Entry { return nil }},
		p,
	)
	code, body := f.postJSON(t, "/admin/counters/enable", map[string]any{})
	if code != http.StatusBadRequest {
		t.Errorf("code=%d, want 400; body=%s", code, body)
	}
	if !bytes.Contains(body, []byte("enabled is required")) {
		t.Errorf("body=%s, want hint 'enabled is required'", body)
	}
}

func TestCountersEndpoints_NotWired_PostsReturn503(t *testing.T) {
	f := setupAdmin(t)
	for _, path := range []string{"/admin/counters/poll-interval", "/admin/counters/enable"} {
		code, body := f.postJSON(t, path, map[string]any{"x": 1})
		if code != http.StatusServiceUnavailable {
			t.Errorf("%s: code=%d want 503; body=%s", path, code, body)
		}
	}
}
