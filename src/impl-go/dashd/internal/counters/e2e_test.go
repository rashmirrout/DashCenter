// e2e_test.go is the PE-3b end-to-end smoke. It stands up a minimal
// in-process gRPC server that implements dashapi.v1.DashApi.GetDpuCounters
// (the only RPC the poller uses), points the dashd counter poller at
// that server through the real dpuclient.DefaultFactory, and asserts
// the store fills with typed CounterReports translated through the
// Option B mapper.
//
// What's exercised end-to-end through real production code:
//
//   1. inventory.Inventory \u2192 Poller \u2192 dpuclient.DefaultFactory
//   2. real dpuclient.realClient.GetDpuCounters \u2192 gRPC unary RPC
//   3. counters.MapReport (Option B translator)
//   4. counters.Store.Put + Get
//   5. Runtime SetInterval applied to the in-flight poller
//   6. Disabled-poller toggle path covered by /admin/counters/enable
//
// The dash-sim's own response shape is asserted by its own PE-3a
// integration tests; we deliberately use a stub server here to avoid
// inverting the module dependency (dashd \u2192 dash-sim is a layering
// violation in the impl-go go.work workspace).

package counters

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/dpuclient"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

// stubDashApiServer implements only GetDpuCounters; every other RPC
// returns Unimplemented (the poller never calls them).
type stubDashApiServer struct {
	dashapi.UnimplementedDashApiServer
	callCount atomic.Int64
	respFn    func(int64, *dashapi.DpuCountersRequest) *dashapi.DpuCountersResponse
}

func (s *stubDashApiServer) GetDpuCounters(_ context.Context, req *dashapi.DpuCountersRequest) (*dashapi.DpuCountersResponse, error) {
	n := s.callCount.Add(1)
	return s.respFn(n, req), nil
}

// e2eHarness owns the gRPC server + poller + store + inventory.
type e2eHarness struct {
	addr  string
	stub  *stubDashApiServer
	store *Store
	pol   *Poller
	gs    *grpc.Server
	lis   net.Listener
}

func newE2eHarness(t *testing.T, dpuID string, pollEvery time.Duration, respFn func(int64, *dashapi.DpuCountersRequest) *dashapi.DpuCountersResponse) *e2eHarness {
	t.Helper()

	stub := &stubDashApiServer{respFn: respFn}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := grpc.NewServer()
	dashapi.RegisterDashApiServer(gs, stub)
	go func() { _ = gs.Serve(lis) }()

	addr := lis.Addr().String()
	inv := inventory.New()
	if err := inv.Register(inventory.DpuEntry{
		ID: dpuID, Endpoint: addr, State: dashcenterv1.DpuState_DPU_STATE_UP,
	}); err != nil {
		gs.Stop()
		_ = lis.Close()
		t.Fatalf("inv.Register: %v", err)
	}

	store := NewStore()
	pol := NewPoller(inv, dpuclient.DefaultFactory, store, pollEvery)

	h := &e2eHarness{addr: addr, stub: stub, store: store, pol: pol, gs: gs, lis: lis}
	t.Cleanup(func() {
		pol.Stop()
		gs.GracefulStop()
		_ = lis.Close()
	})
	return h
}

// waitForEntry blocks until the store has an entry for dpuID, or fails
// after budget. Returns the entry so the caller can assert on it.
func (h *e2eHarness) waitForEntry(t *testing.T, dpuID string, budget time.Duration) *Entry {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if e, ok := h.store.Get(dpuID); ok {
			return e
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("e2e: no store entry for %q within %v (calls=%d)", dpuID, budget, h.stub.callCount.Load())
	return nil
}

func TestE2E_PollerToStore_DefaultBucket(t *testing.T) {
	resp := &dashapi.DpuCountersResponse{
		DeviceId:    "sim-x",
		SampledAtNs: time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC).UnixNano(),
		Dpu: &dashapi.CounterBucket{
			PacketsIn: 7, PacketsOut: 14, Drops: 1,
		},
	}
	h := newE2eHarness(t, "dpu-e2e-1", MinInterval, func(int64, *dashapi.DpuCountersRequest) *dashapi.DpuCountersResponse {
		return resp
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.pol.Start(ctx)

	entry := h.waitForEntry(t, "dpu-e2e-1", 3*time.Second)
	if entry.Report == nil {
		t.Fatalf("entry.Report is nil")
	}
	if got := entry.Report.GetVxlanDecap(); got != 7 {
		t.Errorf("vxlan_decap = %d, want 7", got)
	}
	if got := entry.Report.GetVxlanEncap(); got != 14 {
		t.Errorf("vxlan_encap = %d, want 14", got)
	}
	if got := entry.Report.GetDropAclIn(); got != 1 {
		t.Errorf("drop_acl_in = %d, want 1", got)
	}
	if got := entry.Report.GetDpuId(); got != "dpu-e2e-1" {
		t.Errorf("dpu_id = %q, want dpu-e2e-1 (caller, not sim device_id %q)", got, "sim-x")
	}
}

func TestE2E_PollerToStore_PerEniPropagates(t *testing.T) {
	resp := &dashapi.DpuCountersResponse{
		Dpu: &dashapi.CounterBucket{PacketsIn: 1},
		Enis: []*dashapi.ScopedCounters{
			{ScopeKey: "eni-001", Bucket: &dashapi.CounterBucket{PacketsIn: 11, PacketsOut: 22}},
			{ScopeKey: "eni-002", Bucket: &dashapi.CounterBucket{PacketsIn: 33}},
		},
		Vnets: []*dashapi.ScopedCounters{
			{ScopeKey: "vnet-prod", Bucket: &dashapi.CounterBucket{PacketsIn: 5}},
		},
	}
	h := newE2eHarness(t, "dpu-e2e-2", MinInterval, func(int64, *dashapi.DpuCountersRequest) *dashapi.DpuCountersResponse {
		return resp
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.pol.Start(ctx)

	entry := h.waitForEntry(t, "dpu-e2e-2", 3*time.Second)
	if got := entry.Report.GetFlowTableSize(); got != 3 {
		t.Errorf("flow_table_size = %d, want 3 (2 enis + 1 vnet)", got)
	}
	if got := len(entry.PerEni); got != 2 {
		t.Errorf("len(PerEni) = %d, want 2", got)
	}
	if e := entry.PerEni["eni-001"]; e == nil || e.GetVxlanDecap() != 11 {
		t.Errorf("PerEni[eni-001] = %+v, want decap=11", e)
	}
	if v := entry.PerVnet["vnet-prod"]; v == nil || v.GetVxlanDecap() != 5 {
		t.Errorf("PerVnet[vnet-prod] = %+v, want decap=5", v)
	}
}

func TestE2E_PollerToStore_SetIntervalSpeedsUp(t *testing.T) {
	resp := &dashapi.DpuCountersResponse{Dpu: &dashapi.CounterBucket{PacketsIn: 3}}
	h := newE2eHarness(t, "dpu-e2e-3", 2*time.Second, func(int64, *dashapi.DpuCountersRequest) *dashapi.DpuCountersResponse {
		return resp
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.pol.Start(ctx)

	h.pol.SetInterval(MinInterval)
	entry := h.waitForEntry(t, "dpu-e2e-3", 3*time.Second)
	if entry.Report.GetVxlanDecap() != 3 {
		t.Errorf("vxlan_decap = %d, want 3", entry.Report.GetVxlanDecap())
	}
}

func TestE2E_DisabledPoller_NeverPolls_UntilEnabled(t *testing.T) {
	resp := &dashapi.DpuCountersResponse{Dpu: &dashapi.CounterBucket{PacketsIn: 5}}
	h := newE2eHarness(t, "dpu-e2e-4", MinInterval, func(int64, *dashapi.DpuCountersRequest) *dashapi.DpuCountersResponse {
		return resp
	})
	h.pol.SetEnabled(false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.pol.Start(ctx)

	time.Sleep(400 * time.Millisecond)
	if h.store.Len() != 0 {
		t.Fatalf("store populated while disabled: len=%d, calls=%d", h.store.Len(), h.stub.callCount.Load())
	}

	h.pol.SetEnabled(true)
	entry := h.waitForEntry(t, "dpu-e2e-4", 3*time.Second)
	if entry.Report.GetVxlanDecap() != 5 {
		t.Errorf("vxlan_decap = %d, want 5", entry.Report.GetVxlanDecap())
	}
}
