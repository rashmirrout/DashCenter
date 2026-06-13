// Unit tests for dashapi.v1.DashApi.GetDpuCounters (PE-3a / PE-G8).
//
// The handler is exercised in three layers of breadth:
//
//   1. The DPU-wide bucket is always populated; per-ENI / per-VNET
//      sections are opt-in.
//   2. Rollup attribution by first-component matches the model.Store
//      key convention for every kind we care about (ENI + ENI_ROUTE +
//      ACL_IN attribute to ENI; VNET + VNET_MAPPING attribute to VNET).
//   3. Filters honour caller-requested scope keys (including ones not
//      present in the store — return empty bucket, never silently drop).
//
// Fault injection on the new op name `"GetDpuCounters"` is exercised
// to confirm operator-controlled chaos works.

package server

import (
	"context"
	"strings"
	"testing"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	dash_acl_in "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_in"
	dash_eni "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/eni"
	dash_vnet "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/vnet"
	dash_vnet_mapping "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/vnet_mapping"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/counters"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/events"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/faults"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/model"
)

// newTestServer builds a self-contained Server + Store with the supplied
// device ID and a fresh counters Registry. Tests load the store via the
// real Apply RPC so the wire path is exercised end-to-end (no internal
// store-mutation shortcuts that could mask bugs in object validation).
func newTestServer(t *testing.T, deviceID string) (*Server, *model.Store, *counters.Registry) {
	t.Helper()
	bus := events.New()
	store := model.New(bus)
	reg := counters.New()
	fi := faults.New()
	srv := New(store, bus, fi, reg).WithDeviceID(deviceID)
	return srv, store, reg
}

// applyAndTick is a test helper: Apply the object via the gRPC handler
// (so the same path tests-and-production use), then Tick the counters
// registry `times` times so the row has non-zero values.
func applyAndTick(t *testing.T, srv *Server, kind dashapi.ObjectKind, key []string, payload any, times int) {
	t.Helper()
	obj := &dashapi.Object{Kind: kind, Key: key}
	switch p := payload.(type) {
	case *dash_eni.Eni:
		obj.Payload = &dashapi.Object_Eni{Eni: p}
	case *dash_vnet.Vnet:
		obj.Payload = &dashapi.Object_Vnet{Vnet: p}
	case *dash_vnet_mapping.VnetMapping:
		obj.Payload = &dashapi.Object_VnetMapping{VnetMapping: p}
	case *dash_acl_in.AclIn:
		obj.Payload = &dashapi.Object_AclIn{AclIn: p}
	default:
		t.Fatalf("applyAndTick: unsupported payload type %T", p)
	}
	if _, err := srv.Apply(context.Background(), &dashapi.ApplyRequest{Object: obj}); err != nil {
		t.Fatalf("Apply(%v, %v): %v", kind, key, err)
	}
	joined := strings.Join(key, ":")
	for i := 0; i < times; i++ {
		srv.counters.Tick(joined)
	}
}

// ── DPU-wide bucket (always populated) ────────────────────────────────────

func TestGetDpuCounters_DeviceIDEcho(t *testing.T) {
	srv, _, _ := newTestServer(t, "dpu-test-42")
	resp, err := srv.GetDpuCounters(context.Background(), &dashapi.DpuCountersRequest{})
	if err != nil {
		t.Fatalf("GetDpuCounters: %v", err)
	}
	if resp.GetDeviceId() != "dpu-test-42" {
		t.Errorf("device_id=%q want dpu-test-42", resp.GetDeviceId())
	}
	if resp.GetSampledAtNs() == 0 {
		t.Errorf("sampled_at_ns must be set")
	}
	if resp.GetDpu() == nil {
		t.Fatal("dpu bucket must always be populated (even when empty)")
	}
}

func TestGetDpuCounters_EmptyStoreReturnsZeroBucket(t *testing.T) {
	srv, _, _ := newTestServer(t, "dpu-empty")
	resp, err := srv.GetDpuCounters(context.Background(), &dashapi.DpuCountersRequest{
		IncludeEnis: true, IncludeVnets: true,
	})
	if err != nil {
		t.Fatalf("GetDpuCounters: %v", err)
	}
	if got := resp.GetDpu(); got.GetPacketsIn() != 0 || got.GetDrops() != 0 {
		t.Errorf("empty store dpu bucket non-zero: %+v", got)
	}
	if len(resp.GetEnis()) != 0 {
		t.Errorf("empty store enis len=%d want 0", len(resp.GetEnis()))
	}
	if len(resp.GetVnets()) != 0 {
		t.Errorf("empty store vnets len=%d want 0", len(resp.GetVnets()))
	}
}

func TestGetDpuCounters_DpuBucketSumsEveryKey(t *testing.T) {
	srv, _, reg := newTestServer(t, "dpu-x")
	applyAndTick(t, srv, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-001"}, &dash_eni.Eni{Vnet: "vnet-prod"}, 5)
	applyAndTick(t, srv, dashapi.ObjectKind_OBJECT_KIND_VNET, []string{"vnet-prod"}, &dash_vnet.Vnet{Vni: 1001}, 3)
	applyAndTick(t, srv, dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING, []string{"vnet-prod", "10.0.0.10"}, &dash_vnet_mapping.VnetMapping{}, 2)

	resp, err := srv.GetDpuCounters(context.Background(), &dashapi.DpuCountersRequest{})
	if err != nil {
		t.Fatalf("GetDpuCounters: %v", err)
	}
	want := reg.TotalBucket()
	if resp.GetDpu().GetPacketsIn() != want.PacketsIn {
		t.Errorf("dpu.packets_in=%d want %d", resp.GetDpu().GetPacketsIn(), want.PacketsIn)
	}
	if resp.GetDpu().GetDrops() != want.Drops {
		t.Errorf("dpu.drops=%d want %d", resp.GetDpu().GetDrops(), want.Drops)
	}
}

// ── Per-ENI rollup (include_enis) ────────────────────────────────────────

func TestGetDpuCounters_IncludeEnisDefaultEnumerates(t *testing.T) {
	srv, _, _ := newTestServer(t, "dpu-x")
	applyAndTick(t, srv, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-001"}, &dash_eni.Eni{Vnet: "vnet-prod"}, 2)
	applyAndTick(t, srv, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-002"}, &dash_eni.Eni{Vnet: "vnet-prod"}, 4)
	// ACL_IN ["eni-001", "1"] joins to "eni-001:1" and must roll up under eni-001.
	applyAndTick(t, srv, dashapi.ObjectKind_OBJECT_KIND_ACL_IN, []string{"eni-001", "1"}, &dash_acl_in.AclIn{V4AclGroupId: "g1"}, 7)

	resp, err := srv.GetDpuCounters(context.Background(), &dashapi.DpuCountersRequest{IncludeEnis: true})
	if err != nil {
		t.Fatalf("GetDpuCounters: %v", err)
	}
	if len(resp.GetEnis()) != 2 {
		t.Fatalf("want 2 enis, got %d: %+v", len(resp.GetEnis()), resp.GetEnis())
	}
	// Sorted: eni-001, eni-002.
	if resp.GetEnis()[0].GetScopeKey() != "eni-001" || resp.GetEnis()[1].GetScopeKey() != "eni-002" {
		t.Errorf("eni order wrong: %s, %s", resp.GetEnis()[0].GetScopeKey(), resp.GetEnis()[1].GetScopeKey())
	}
	// eni-001 must include the ACL_IN row's contribution.
	if resp.GetEnis()[0].GetBucket().GetPacketsIn() == 0 {
		t.Error("eni-001 bucket empty; ACL_IN child key not attributed")
	}
}

func TestGetDpuCounters_IncludeEnisFilterByName(t *testing.T) {
	srv, _, _ := newTestServer(t, "dpu-x")
	applyAndTick(t, srv, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-001"}, &dash_eni.Eni{Vnet: "vnet-prod"}, 1)
	applyAndTick(t, srv, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-002"}, &dash_eni.Eni{Vnet: "vnet-prod"}, 1)

	resp, err := srv.GetDpuCounters(context.Background(), &dashapi.DpuCountersRequest{
		IncludeEnis: true, EniNames: []string{"eni-002"},
	})
	if err != nil {
		t.Fatalf("GetDpuCounters: %v", err)
	}
	if len(resp.GetEnis()) != 1 {
		t.Fatalf("want 1 eni, got %d", len(resp.GetEnis()))
	}
	if resp.GetEnis()[0].GetScopeKey() != "eni-002" {
		t.Errorf("filter returned wrong scope: %s", resp.GetEnis()[0].GetScopeKey())
	}
}

func TestGetDpuCounters_FilterUnknownScopeReturnsEmptyBucket(t *testing.T) {
	srv, _, _ := newTestServer(t, "dpu-x")
	applyAndTick(t, srv, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-real"}, &dash_eni.Eni{Vnet: "vnet-prod"}, 1)

	resp, err := srv.GetDpuCounters(context.Background(), &dashapi.DpuCountersRequest{
		IncludeEnis: true, EniNames: []string{"eni-missing"},
	})
	if err != nil {
		t.Fatalf("GetDpuCounters: %v", err)
	}
	if len(resp.GetEnis()) != 1 {
		t.Fatalf("want 1 eni placeholder, got %d", len(resp.GetEnis()))
	}
	got := resp.GetEnis()[0]
	if got.GetScopeKey() != "eni-missing" {
		t.Errorf("scope_key=%q want eni-missing", got.GetScopeKey())
	}
	if got.GetBucket().GetPacketsIn() != 0 {
		t.Errorf("unknown scope bucket should be zero: %+v", got.GetBucket())
	}
}

func TestGetDpuCounters_FilterDedupesAndSkipsEmpty(t *testing.T) {
	srv, _, _ := newTestServer(t, "dpu-x")
	applyAndTick(t, srv, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-001"}, &dash_eni.Eni{Vnet: "vnet-prod"}, 1)

	resp, err := srv.GetDpuCounters(context.Background(), &dashapi.DpuCountersRequest{
		IncludeEnis: true,
		EniNames:    []string{"eni-001", "", "eni-001", "eni-002"},
	})
	if err != nil {
		t.Fatalf("GetDpuCounters: %v", err)
	}
	if len(resp.GetEnis()) != 2 {
		t.Fatalf("want 2 enis after dedup+empty-skip, got %d: %v", len(resp.GetEnis()), resp.GetEnis())
	}
	if resp.GetEnis()[0].GetScopeKey() != "eni-001" || resp.GetEnis()[1].GetScopeKey() != "eni-002" {
		t.Errorf("sorted order broken: %+v", resp.GetEnis())
	}
}

// ── Per-VNET rollup (include_vnets) ──────────────────────────────────────

func TestGetDpuCounters_IncludeVnetsEnumeratesAndSums(t *testing.T) {
	srv, _, _ := newTestServer(t, "dpu-x")
	applyAndTick(t, srv, dashapi.ObjectKind_OBJECT_KIND_VNET, []string{"vnet-prod"}, &dash_vnet.Vnet{Vni: 1001}, 1)
	applyAndTick(t, srv, dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING, []string{"vnet-prod", "10.0.0.10"}, &dash_vnet_mapping.VnetMapping{}, 3)
	applyAndTick(t, srv, dashapi.ObjectKind_OBJECT_KIND_VNET, []string{"vnet-stage"}, &dash_vnet.Vnet{Vni: 1002}, 1)

	resp, err := srv.GetDpuCounters(context.Background(), &dashapi.DpuCountersRequest{IncludeVnets: true})
	if err != nil {
		t.Fatalf("GetDpuCounters: %v", err)
	}
	if len(resp.GetVnets()) != 2 {
		t.Fatalf("want 2 vnets, got %d", len(resp.GetVnets()))
	}
	prod := resp.GetVnets()[0]
	if prod.GetScopeKey() != "vnet-prod" {
		t.Errorf("expected vnet-prod first (sorted), got %s", prod.GetScopeKey())
	}
	// vnet-prod gets 1 tick on itself + 3 ticks on vnet-prod:10.0.0.10 → packets_in > vnet-stage
	if prod.GetBucket().GetPacketsIn() <= resp.GetVnets()[1].GetBucket().GetPacketsIn() {
		t.Errorf("vnet-prod (with child mapping) should outpace vnet-stage: prod=%+v stage=%+v",
			prod.GetBucket(), resp.GetVnets()[1].GetBucket())
	}
}

func TestGetDpuCounters_IncludeBoth(t *testing.T) {
	srv, _, _ := newTestServer(t, "dpu-x")
	applyAndTick(t, srv, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-001"}, &dash_eni.Eni{Vnet: "vnet-prod"}, 1)
	applyAndTick(t, srv, dashapi.ObjectKind_OBJECT_KIND_VNET, []string{"vnet-prod"}, &dash_vnet.Vnet{Vni: 1001}, 1)

	resp, err := srv.GetDpuCounters(context.Background(), &dashapi.DpuCountersRequest{
		IncludeEnis: true, IncludeVnets: true,
	})
	if err != nil {
		t.Fatalf("GetDpuCounters: %v", err)
	}
	if len(resp.GetEnis()) != 1 || len(resp.GetVnets()) != 1 {
		t.Fatalf("expected one of each: enis=%d vnets=%d", len(resp.GetEnis()), len(resp.GetVnets()))
	}
}

// ── Back-compat: existing GetCounters keeps working ──────────────────────

func TestGetCounters_LegacyPerObjectStillWorks(t *testing.T) {
	srv, _, _ := newTestServer(t, "dpu-x")
	applyAndTick(t, srv, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-001"}, &dash_eni.Eni{Vnet: "vnet-prod"}, 2)

	resp, err := srv.GetCounters(context.Background(), &dashapi.CountersRequest{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ENI,
		Key:  []string{"eni-001"},
	})
	if err != nil {
		t.Fatalf("GetCounters: %v", err)
	}
	if resp.GetCounters()["packets_in"] == 0 {
		t.Errorf("legacy per-object counter map should have packets_in: %v", resp.GetCounters())
	}
}

// ── Fault injection ──────────────────────────────────────────────────────

func TestGetDpuCounters_FaultInjectionPropagates(t *testing.T) {
	srv, _, _ := newTestServer(t, "dpu-x")
	if err := srv.faults.Add(faults.Spec{Op: "GetDpuCounters", Mode: faults.ModeError, Count: 1, Message: "injected failure"}); err != nil {
		t.Fatalf("faults.Add: %v", err)
	}
	_, err := srv.GetDpuCounters(context.Background(), &dashapi.DpuCountersRequest{})
	if err == nil {
		t.Fatal("want error from injected fault, got nil")
	}
	if !strings.Contains(err.Error(), "injected failure") {
		t.Errorf("error %q missing injected message", err)
	}
	// Second call (Count=1 exhausted) succeeds.
	if _, err := srv.GetDpuCounters(context.Background(), &dashapi.DpuCountersRequest{}); err != nil {
		t.Errorf("second call should pass after fault exhausted: %v", err)
	}
}

// ── No-device-id deployment ──────────────────────────────────────────────

func TestGetDpuCounters_WithoutDeviceIDLeavesFieldEmpty(t *testing.T) {
	// Server constructed via New() WITHOUT WithDeviceID — verifies the
	// optional setter contract.
	bus := events.New()
	store := model.New(bus)
	reg := counters.New()
	srv := New(store, bus, faults.New(), reg)
	resp, err := srv.GetDpuCounters(context.Background(), &dashapi.DpuCountersRequest{})
	if err != nil {
		t.Fatalf("GetDpuCounters: %v", err)
	}
	if resp.GetDeviceId() != "" {
		t.Errorf("device_id should be empty when WithDeviceID not called: %q", resp.GetDeviceId())
	}
}
