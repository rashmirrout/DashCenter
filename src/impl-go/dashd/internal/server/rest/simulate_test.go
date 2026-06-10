// Simulate (PB-2) tests for the REST gateway.
package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/capacity"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
	filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

// setupSimServer wires a REST gateway against a real file store + a
// real capacity tracker with one DPU advertising MaxEnis=1 — small
// enough that the PB-G2 exceed-by-1 path is reachable in one POST.
func setupSimServer(t *testing.T) *httptest.Server {
	t.Helper()
	fs, err := filstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("filstore.Open: %v", err)
	}
	t.Cleanup(func() { fs.Close() })

	inv := inventory.New()
	if err := inv.Register(inventory.DpuEntry{ID: "dpu-1", Endpoint: "dpu-1:50051"}); err != nil {
		t.Fatalf("inv.Register: %v", err)
	}
	if err := inv.SetLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 1}); err != nil {
		t.Fatalf("inv.SetLimits: %v", err)
	}
	tr := capacity.NewTracker(inv)

	obs := model.NewObsCache()
	cpSvc := service.NewControlPlane(fs, inv, nil, tr, nil)
	obsSvc := service.NewObservability(inv, fs, obs)
	srv := New(cpSvc, obsSvc)
	return httptest.NewServer(srv.srv.Handler)
}

func TestSimulate_REST_EmptyBody(t *testing.T) {
	ts := setupSimServer(t)
	defer ts.Close()
	resp := doReq(t, ts, "POST", "/v1/simulate", "")
	if resp.StatusCode != 400 {
		t.Errorf("expected 400 for empty body; got %d", resp.StatusCode)
	}
}

func TestSimulate_REST_MalformedJSON(t *testing.T) {
	ts := setupSimServer(t)
	defer ts.Close()
	resp := doReq(t, ts, "POST", "/v1/simulate", "{not-json")
	if resp.StatusCode != 400 {
		t.Errorf("expected 400 for malformed body; got %d", resp.StatusCode)
	}
}

func TestSimulate_REST_PutEni_WithinCapacity(t *testing.T) {
	ts := setupSimServer(t)
	defer ts.Close()

	body := `{"ops":[{"action":"put","namespace":"default","kind":"eni","eni":{"name":"e1","placement_hint_dpu_ids":["dpu-1"]}}]}`
	resp := doReq(t, ts, "POST", "/v1/simulate", body)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200; got %d", resp.StatusCode)
	}
	var res struct {
		WouldSucceed     bool     `json:"would_succeed"`
		ValidationErrors []string `json:"validation_errors"`
		PerDpuImpact     []struct {
			DpuID     string `json:"dpu_id"`
			DeltaEnis int64  `json:"delta_enis"`
		} `json:"per_dpu_impact"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !res.WouldSucceed {
		t.Errorf("WouldSucceed=false; errors=%v", res.ValidationErrors)
	}
	if len(res.PerDpuImpact) != 1 || res.PerDpuImpact[0].DpuID != "dpu-1" || res.PerDpuImpact[0].DeltaEnis != 1 {
		t.Errorf("unexpected per_dpu_impact: %+v", res.PerDpuImpact)
	}
}

func TestSimulate_REST_PutEni_Exceeds_PB_G2(t *testing.T) {
	ts := setupSimServer(t)
	defer ts.Close()

	body := `{"ops":[
		{"action":"put","namespace":"default","kind":"eni","eni":{"name":"a","placement_hint_dpu_ids":["dpu-1"]}},
		{"action":"put","namespace":"default","kind":"eni","eni":{"name":"b","placement_hint_dpu_ids":["dpu-1"]}}
	]}`
	resp := doReq(t, ts, "POST", "/v1/simulate", body)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 (simulate is non-destructive); got %d", resp.StatusCode)
	}
	var res struct {
		WouldSucceed     bool     `json:"would_succeed"`
		ValidationErrors []string `json:"validation_errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.WouldSucceed {
		t.Fatal("WouldSucceed=true; want false (2nd ENI exceeds MaxEnis=1)")
	}
	if len(res.ValidationErrors) == 0 {
		t.Fatal("expected validation errors")
	}
	joined := strings.Join(res.ValidationErrors, " | ")
	if !strings.Contains(joined, "max_enis") {
		t.Errorf("expected max_enis in errors; got %q", joined)
	}
}

func TestSimulate_REST_DoesNotMutateStore(t *testing.T) {
	// Ensure a successful Simulate does NOT actually persist anything.
	ts := setupSimServer(t)
	defer ts.Close()

	body := `{"ops":[{"action":"put","namespace":"default","kind":"eni","eni":{"name":"shouldnotexist","placement_hint_dpu_ids":["dpu-1"]}}]}`
	resp := doReq(t, ts, "POST", "/v1/simulate", body)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200; got %d", resp.StatusCode)
	}
	// GET should still be 404.
	get := doReq(t, ts, "GET", "/v1/enis/shouldnotexist", "")
	if get.StatusCode != http.StatusNotFound {
		t.Errorf("Simulate should not persist; GET status=%d, want 404", get.StatusCode)
	}
}
