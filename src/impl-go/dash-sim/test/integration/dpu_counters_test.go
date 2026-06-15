// Package integration spins a dash-sim gRPC server in-process and drives
// it via the dash-sim-client SDK to verify the PE-3a (PE-G8) standalone
// contract: operators MUST be able to inspect counter behaviour using
// just `dash-sim` + `dash-sim-client` — no dashd, no fleet, no extra
// infrastructure.
//
// This test is the operator-experience smoke test from the discipline
// doc's "Live e2e" requirement, run in a pure-Go in-process harness so
// CI executes it on every commit (no docker, no port binding flakes
// beyond the one localhost:0 listener we open per test).
//
// What's exercised end-to-end:
//
//   1. dashapi.v1 wire path: proto-encoded request/response across gRPC.
//   2. dash-sim Apply + counter Tick loop populating per-(kind,key) rows.
//   3. Server-side rollup: DPU total / per-ENI / per-VNET aggregations.
//   4. Client SDK: GetDpuCounters returning the typed response shape.
//   5. Optional filter flags (eni_names / vnet_keys) propagating end-to-end.
//
// What is NOT covered here (deferred to PE-3b / PE-3c by design):
//
//   - dashd ingestion + mapping to dashcenter.v1.CounterReport.
//   - Streaming with follow=true (dashd-side broadcaster pattern).
//   - dashw multiplexer + browser SPA widget.

package integration

import (
	"context"
	"net"
	"testing"
	"time"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	dash_eni "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/eni"
	dash_vnet "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/vnet"
	dash_vnet_mapping "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/vnet_mapping"
	dashsimclient "github.com/rashmirrout/DashCenter/src/impl-go/dash-sim-client/pkg/client"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/counters"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/events"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/faults"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/model"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/server"
	"google.golang.org/grpc"
)

// harness owns the in-process sim server + connected client + counters
// registry handle. Created via newHarness, torn down via Close (called
// by t.Cleanup).
type harness struct {
	srv     *server.Server
	store   *model.Store
	reg     *counters.Registry
	cli     *dashsimclient.Client
	gsrv    *grpc.Server
	lis     net.Listener
}

func newHarness(t *testing.T, deviceID string) *harness {
	t.Helper()
	bus := events.New()
	store := model.New(bus)
	// Disable strict FK refs — integration counter tests create objects
	// without their parents. FK validation is tested in model/refs_test.go.
	store.SetStrictRefs(false)
	reg := counters.New()
	fi := faults.New()
	srv := server.New(store, bus, fi, reg).WithDeviceID(deviceID)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := grpc.NewServer()
	dashapi.RegisterDashApiServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()

	cli, err := dashsimclient.Dial(lis.Addr().String())
	if err != nil {
		gs.Stop()
		_ = lis.Close()
		t.Fatalf("dial: %v", err)
	}

	h := &harness{
		srv: srv, store: store, reg: reg, cli: cli,
		gsrv: gs, lis: lis,
	}
	t.Cleanup(h.Close)
	return h
}

func (h *harness) Close() {
	_ = h.cli.Close()
	h.gsrv.GracefulStop()
	_ = h.lis.Close()
}

// apply is a small helper that uses the sim's Apply RPC (over the loopback
// gRPC) to populate the store and then ticks counters `times` times so
// rollups produce non-zero values.
func (h *harness) apply(t *testing.T, kind dashapi.ObjectKind, key []string, payload any, ticks int) {
	t.Helper()
	obj := &dashapi.Object{Kind: kind, Key: key}
	switch p := payload.(type) {
	case *dash_eni.Eni:
		obj.Payload = &dashapi.Object_Eni{Eni: p}
	case *dash_vnet.Vnet:
		obj.Payload = &dashapi.Object_Vnet{Vnet: p}
	case *dash_vnet_mapping.VnetMapping:
		obj.Payload = &dashapi.Object_VnetMapping{VnetMapping: p}
	default:
		t.Fatalf("apply: unsupported payload type %T", p)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := h.cli.Apply(ctx, obj); err != nil {
		t.Fatalf("Apply(%v, %v): %v", kind, key, err)
	}
	joined := counters.JoinKey(key)
	for i := 0; i < ticks; i++ {
		h.reg.Tick(joined)
	}
}

// ── tests ────────────────────────────────────────────────────────────────

// TestIntegration_EndToEnd_DefaultRequest covers the operator's most
// common smoke-test workflow: spin a sim, populate a couple of ENIs +
// one VNET with mappings, ask for DPU rollup (no scope flags), assert
// non-zero counters.
func TestIntegration_EndToEnd_DefaultRequest(t *testing.T) {
	h := newHarness(t, "dpu-sim-int-01")
	h.apply(t, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-001"}, &dash_eni.Eni{Vnet: "vnet-prod"}, 3)
	h.apply(t, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-002"}, &dash_eni.Eni{Vnet: "vnet-prod"}, 5)
	h.apply(t, dashapi.ObjectKind_OBJECT_KIND_VNET, []string{"vnet-prod"}, &dash_vnet.Vnet{Vni: 1001}, 1)
	h.apply(t, dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING, []string{"vnet-prod", "10.0.0.10"}, &dash_vnet_mapping.VnetMapping{}, 2)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := h.cli.GetDpuCounters(ctx, &dashapi.DpuCountersRequest{})
	if err != nil {
		t.Fatalf("GetDpuCounters: %v", err)
	}
	if resp.GetDeviceId() != "dpu-sim-int-01" {
		t.Errorf("device_id wrong: %q", resp.GetDeviceId())
	}
	if resp.GetDpu().GetPacketsIn() == 0 {
		t.Error("dpu bucket empty after 11 ticks")
	}
	if len(resp.GetEnis()) != 0 {
		t.Errorf("default request must NOT include enis: %d", len(resp.GetEnis()))
	}
	if len(resp.GetVnets()) != 0 {
		t.Errorf("default request must NOT include vnets: %d", len(resp.GetVnets()))
	}
}

// TestIntegration_EndToEnd_IncludeEnisAndVnets covers the next-most
// common workflow: per-scope inspection, both opt-ins enabled.
func TestIntegration_EndToEnd_IncludeEnisAndVnets(t *testing.T) {
	h := newHarness(t, "dpu-sim-int-02")
	h.apply(t, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-001"}, &dash_eni.Eni{Vnet: "vnet-prod"}, 1)
	h.apply(t, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-002"}, &dash_eni.Eni{Vnet: "vnet-prod"}, 1)
	h.apply(t, dashapi.ObjectKind_OBJECT_KIND_VNET, []string{"vnet-prod"}, &dash_vnet.Vnet{Vni: 1001}, 1)
	h.apply(t, dashapi.ObjectKind_OBJECT_KIND_VNET, []string{"vnet-stage"}, &dash_vnet.Vnet{Vni: 1002}, 1)
	h.apply(t, dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING, []string{"vnet-prod", "10.0.0.10"}, &dash_vnet_mapping.VnetMapping{}, 4)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := h.cli.GetDpuCounters(ctx, &dashapi.DpuCountersRequest{
		IncludeEnis: true, IncludeVnets: true,
	})
	if err != nil {
		t.Fatalf("GetDpuCounters: %v", err)
	}
	if len(resp.GetEnis()) != 2 {
		t.Errorf("want 2 enis, got %d: %+v", len(resp.GetEnis()), resp.GetEnis())
	}
	if len(resp.GetVnets()) != 2 {
		t.Errorf("want 2 vnets, got %d", len(resp.GetVnets()))
	}
	// Sorted alphabetically: eni-001, eni-002.
	if resp.GetEnis()[0].GetScopeKey() != "eni-001" || resp.GetEnis()[1].GetScopeKey() != "eni-002" {
		t.Errorf("eni order wrong: %s, %s", resp.GetEnis()[0].GetScopeKey(), resp.GetEnis()[1].GetScopeKey())
	}
	// vnet-prod (with child mapping ticked 4 times) > vnet-stage.
	prod, stage := resp.GetVnets()[0], resp.GetVnets()[1]
	if prod.GetScopeKey() != "vnet-prod" {
		t.Fatalf("expected vnet-prod first (sorted), got %s", prod.GetScopeKey())
	}
	if prod.GetBucket().GetPacketsIn() <= stage.GetBucket().GetPacketsIn() {
		t.Errorf("vnet-prod (with child mapping) should outpace vnet-stage: prod=%+v stage=%+v",
			prod.GetBucket(), stage.GetBucket())
	}
}

// TestIntegration_EndToEnd_FilterFlagsPropagate covers operator drill-down:
// "show me only these two ENIs" — important when the device has 100s of
// scopes and the operator wants targeted output.
func TestIntegration_EndToEnd_FilterFlagsPropagate(t *testing.T) {
	h := newHarness(t, "dpu-sim-int-03")
	h.apply(t, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-001"}, &dash_eni.Eni{Vnet: "vnet-prod"}, 1)
	h.apply(t, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-002"}, &dash_eni.Eni{Vnet: "vnet-prod"}, 1)
	h.apply(t, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-003"}, &dash_eni.Eni{Vnet: "vnet-prod"}, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := h.cli.GetDpuCounters(ctx, &dashapi.DpuCountersRequest{
		IncludeEnis: true,
		EniNames:    []string{"eni-002", "eni-missing"},
	})
	if err != nil {
		t.Fatalf("GetDpuCounters: %v", err)
	}
	if len(resp.GetEnis()) != 2 {
		t.Fatalf("filter must return exactly the requested scopes (incl. missing): got %d", len(resp.GetEnis()))
	}
	// Sorted: eni-002, eni-missing.
	if resp.GetEnis()[0].GetScopeKey() != "eni-002" {
		t.Errorf("first eni scope = %q want eni-002", resp.GetEnis()[0].GetScopeKey())
	}
	if resp.GetEnis()[1].GetScopeKey() != "eni-missing" {
		t.Errorf("second eni scope = %q want eni-missing", resp.GetEnis()[1].GetScopeKey())
	}
	// eni-missing must have a zero bucket (no data).
	if resp.GetEnis()[1].GetBucket().GetPacketsIn() != 0 {
		t.Errorf("unknown scope bucket non-zero: %+v", resp.GetEnis()[1].GetBucket())
	}
}

// TestIntegration_EndToEnd_LegacyGetCountersStillWorks confirms the
// pre-PE-3a per-object GetCounters RPC continues to function — back-compat
// is a hard requirement for the existing dash-sim-client `counters`
// subcommand that ships today.
func TestIntegration_EndToEnd_LegacyGetCountersStillWorks(t *testing.T) {
	h := newHarness(t, "dpu-sim-int-04")
	h.apply(t, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-legacy"}, &dash_eni.Eni{Vnet: "vnet-prod"}, 7)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got, err := h.cli.GetCounters(ctx, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-legacy"})
	if err != nil {
		t.Fatalf("legacy GetCounters: %v", err)
	}
	if got["packets_in"] == 0 {
		t.Errorf("legacy counters empty: %v", got)
	}
}
