// Package integration — gRPC integration tests for referential integrity
// validation in dash-sim. These tests verify that FK violations are
// correctly rejected through the full client → gRPC → server → store
// stack, and that valid object creation order succeeds.
//
// What's exercised end-to-end:
//
//   1. gRPC wire path: proto-encoded Apply request/response with FK check.
//   2. dash-sim Store.Apply FK validation returning gRPC errors to the client.
//   3. Error messages propagating across the wire with field + kind info.
//   4. Correct creation order (Tier 0 → Tier 1 → Tier 2) succeeding.
//   5. StrictRefs=true (default) enforcement.

package integration

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	dash_acl_group "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_group"
	dash_acl_in "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_in"
	dash_acl_rule "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_rule"
	dash_eni "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/eni"
	dash_eni_route "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/eni_route"
	dash_route "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route"
	dash_route_group "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route_group"
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

// refsHarness mirrors the counter-test harness but keeps strictRefs=true.
type refsHarness struct {
	cli  *dashsimclient.Client
	gsrv *grpc.Server
	lis  net.Listener
}

func newRefsHarness(t *testing.T) *refsHarness {
	t.Helper()
	bus := events.New()
	store := model.New(bus)
	// strictRefs defaults to true — keep it on for FK validation testing
	reg := counters.New()
	fi := faults.New()
	srv := server.New(store, bus, fi, reg).WithDeviceID("dpu-ri-test")

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

	h := &refsHarness{cli: cli, gsrv: gs, lis: lis}
	t.Cleanup(h.Close)
	return h
}

func (h *refsHarness) Close() {
	_ = h.cli.Close()
	h.gsrv.GracefulStop()
	_ = h.lis.Close()
}

func (h *refsHarness) applyObj(t *testing.T, obj *dashapi.Object) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ack, err := h.cli.Apply(ctx, obj)
	if err != nil {
		t.Fatalf("Apply(%v, %v): gRPC error: %v", obj.GetKind(), obj.GetKey(), err)
	}
	if !ack.GetAccepted() {
		t.Fatalf("Apply(%v, %v): rejected: %s", obj.GetKind(), obj.GetKey(), ack.GetError())
	}
}

func (h *refsHarness) applyExpectError(t *testing.T, obj *dashapi.Object, wantSubstr string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ack, err := h.cli.Apply(ctx, obj)
	if err != nil {
		t.Fatalf("Apply(%v, %v): unexpected gRPC error: %v", obj.GetKind(), obj.GetKey(), err)
	}
	if ack.GetAccepted() {
		t.Fatalf("expected Apply rejection containing %q, but was accepted", wantSubstr)
	}
	if !strings.Contains(ack.GetError(), wantSubstr) {
		t.Fatalf("ack.Error %q does not contain %q", ack.GetError(), wantSubstr)
	}
}

// ── Integration test: wrong order is rejected over gRPC ───────────────

func TestIntegration_Refs_ENI_WithoutVnet_IsRejected(t *testing.T) {
	h := newRefsHarness(t)
	// Attempt to create ENI without first creating its vnet — should fail.
	h.applyExpectError(t, &dashapi.Object{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ENI,
		Key:  []string{"eni-001"},
		Payload: &dashapi.Object_Eni{Eni: &dash_eni.Eni{
			Vnet: "vnet-prod",
		}},
	}, "vnet")
}

func TestIntegration_Refs_Route_WithoutRouteGroup_IsRejected(t *testing.T) {
	h := newRefsHarness(t)
	h.applyExpectError(t, &dashapi.Object{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ROUTE,
		Key:  []string{"rg-nonexistent", "10.0.0.0/8"},
		Payload: &dashapi.Object_Route{Route: &dash_route.Route{
			RoutingTypeData: &dash_route.Route_Vnet{Vnet: "vnet-prod"},
		}},
	}, "route_group")
}

func TestIntegration_Refs_AclRule_WithoutAclGroup_IsRejected(t *testing.T) {
	h := newRefsHarness(t)
	h.applyExpectError(t, &dashapi.Object{
		Kind:    dashapi.ObjectKind_OBJECT_KIND_ACL_RULE,
		Key:     []string{"no-group", "100"},
		Payload: &dashapi.Object_AclRule{AclRule: &dash_acl_rule.AclRule{}},
	}, "acl_group")
}

func TestIntegration_Refs_EniRoute_WithoutENI_IsRejected(t *testing.T) {
	h := newRefsHarness(t)
	// Create the route_group but not the ENI
	h.applyObj(t, &dashapi.Object{
		Kind:    dashapi.ObjectKind_OBJECT_KIND_ROUTE_GROUP,
		Key:     []string{"rg-prod"},
		Payload: &dashapi.Object_RouteGroup{RouteGroup: &dash_route_group.RouteGroup{}},
	})
	h.applyExpectError(t, &dashapi.Object{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ENI_ROUTE,
		Key:  []string{"no-eni"},
		Payload: &dashapi.Object_EniRoute{EniRoute: &dash_eni_route.EniRoute{
			GroupId: "rg-prod",
		}},
	}, "eni")
}

func TestIntegration_Refs_AclIn_WithoutENI_IsRejected(t *testing.T) {
	h := newRefsHarness(t)
	h.applyObj(t, &dashapi.Object{
		Kind:    dashapi.ObjectKind_OBJECT_KIND_ACL_GROUP,
		Key:     []string{"acl-grp-1"},
		Payload: &dashapi.Object_AclGroup{AclGroup: &dash_acl_group.AclGroup{}},
	})
	h.applyExpectError(t, &dashapi.Object{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ACL_IN,
		Key:  []string{"no-eni", "1"},
		Payload: &dashapi.Object_AclIn{AclIn: &dash_acl_in.AclIn{
			V4AclGroupId: "acl-grp-1",
		}},
	}, "eni")
}

func TestIntegration_Refs_VnetMapping_WithoutVnet_IsRejected(t *testing.T) {
	h := newRefsHarness(t)
	h.applyExpectError(t, &dashapi.Object{
		Kind:    dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING,
		Key:     []string{"no-vnet", "10.0.0.1"},
		Payload: &dashapi.Object_VnetMapping{VnetMapping: &dash_vnet_mapping.VnetMapping{}},
	}, "vnet")
}

// ── Integration test: correct Tier-0 → Tier-1 → Tier-2 order works ───

func TestIntegration_Refs_CorrectCreationOrder_Succeeds(t *testing.T) {
	h := newRefsHarness(t)

	// Tier 0 — roots
	h.applyObj(t, &dashapi.Object{
		Kind: dashapi.ObjectKind_OBJECT_KIND_VNET, Key: []string{"vnet-prod"},
		Payload: &dashapi.Object_Vnet{Vnet: &dash_vnet.Vnet{Vni: 1001}},
	})
	h.applyObj(t, &dashapi.Object{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ACL_GROUP, Key: []string{"acl-grp-1"},
		Payload: &dashapi.Object_AclGroup{AclGroup: &dash_acl_group.AclGroup{}},
	})
	h.applyObj(t, &dashapi.Object{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ROUTE_GROUP, Key: []string{"rg-prod"},
		Payload: &dashapi.Object_RouteGroup{RouteGroup: &dash_route_group.RouteGroup{}},
	})

	// Tier 1 — references only Tier 0
	h.applyObj(t, &dashapi.Object{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ENI, Key: []string{"eni-001"},
		Payload: &dashapi.Object_Eni{Eni: &dash_eni.Eni{Vnet: "vnet-prod"}},
	})
	h.applyObj(t, &dashapi.Object{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ACL_RULE, Key: []string{"acl-grp-1", "100"},
		Payload: &dashapi.Object_AclRule{AclRule: &dash_acl_rule.AclRule{}},
	})
	h.applyObj(t, &dashapi.Object{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ROUTE, Key: []string{"rg-prod", "10.0.0.0/8"},
		Payload: &dashapi.Object_Route{Route: &dash_route.Route{
			RoutingTypeData: &dash_route.Route_Vnet{Vnet: "vnet-prod"},
		}},
	})
	h.applyObj(t, &dashapi.Object{
		Kind: dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING, Key: []string{"vnet-prod", "10.0.0.1"},
		Payload: &dashapi.Object_VnetMapping{VnetMapping: &dash_vnet_mapping.VnetMapping{}},
	})

	// Tier 2 — references Tier 0 + Tier 1
	h.applyObj(t, &dashapi.Object{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ENI_ROUTE, Key: []string{"eni-001"},
		Payload: &dashapi.Object_EniRoute{EniRoute: &dash_eni_route.EniRoute{
			GroupId: "rg-prod",
		}},
	})
	h.applyObj(t, &dashapi.Object{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ACL_IN, Key: []string{"eni-001", "1"},
		Payload: &dashapi.Object_AclIn{AclIn: &dash_acl_in.AclIn{
			V4AclGroupId: "acl-grp-1",
		}},
	})
}

// ── Integration test: error message quality over gRPC ─────────────────

func TestIntegration_Refs_ErrorMessage_ContainsMissingRefAndKind(t *testing.T) {
	h := newRefsHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ack, err := h.cli.Apply(ctx, &dashapi.Object{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ENI,
		Key:  []string{"eni-typo"},
		Payload: &dashapi.Object_Eni{Eni: &dash_eni.Eni{
			Vnet: "vnet-bllue", // intentional typo
		}},
	})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if ack.GetAccepted() {
		t.Fatal("expected FK rejection")
	}
	msg := ack.GetError()
	if !strings.Contains(msg, "vnet-bllue") {
		t.Errorf("error should name the missing ref: %s", msg)
	}
	if !strings.Contains(msg, "vnet") {
		t.Errorf("error should name the ref kind: %s", msg)
	}
	if !strings.Contains(msg, "create it first") {
		t.Errorf("error should suggest the fix: %s", msg)
	}
}

// ── Integration test: fix-then-retry workflow ─────────────────────────

func TestIntegration_Refs_CreateMissing_ThenRetry_Succeeds(t *testing.T) {
	h := newRefsHarness(t)

	// Step 1: Try to create ENI with missing vnet — should be rejected
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	ack, err := h.cli.Apply(ctx, &dashapi.Object{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ENI, Key: []string{"eni-001"},
		Payload: &dashapi.Object_Eni{Eni: &dash_eni.Eni{Vnet: "vnet-prod"}},
	})
	cancel()
	if err != nil {
		t.Fatalf("step 1: unexpected gRPC error: %v", err)
	}
	if ack.GetAccepted() {
		t.Fatal("step 1: expected FK rejection")
	}

	// Step 2: Create the missing vnet
	h.applyObj(t, &dashapi.Object{
		Kind: dashapi.ObjectKind_OBJECT_KIND_VNET, Key: []string{"vnet-prod"},
		Payload: &dashapi.Object_Vnet{Vnet: &dash_vnet.Vnet{Vni: 1001}},
	})

	// Step 3: Retry ENI creation — should succeed now
	h.applyObj(t, &dashapi.Object{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ENI, Key: []string{"eni-001"},
		Payload: &dashapi.Object_Eni{Eni: &dash_eni.Eni{Vnet: "vnet-prod"}},
	})
}
