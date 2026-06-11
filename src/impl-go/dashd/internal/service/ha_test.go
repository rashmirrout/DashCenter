// PC-G1..G3 HaService integration tests at the service layer.
package service

import (
	"context"
	"testing"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/ha/orchestrator"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

func newHa(t *testing.T) (HaService, *orchestrator.Orchestrator, *orchestrator.NoOpPusher) {
	t.Helper()
	p := &orchestrator.NoOpPusher{}
	orch := orchestrator.New(p)
	return NewHa(orch), orch, p
}

// newServiceWithOrch wires a minimal ControlPlaneService that knows
// about a non-nil orchestrator. Used by TestHa_PutHaSetWiring.
func newServiceWithOrch(t *testing.T, orch *orchestrator.Orchestrator) ControlPlaneService {
	t.Helper()
	fs, err := filstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("filstore.Open: %v", err)
	}
	t.Cleanup(func() { fs.Close() })
	inv := inventory.New()
	return NewControlPlane(fs, inv, nil, nil, nil, nil, orch)
}

func TestHa_GetSet_Empty(t *testing.T) {
	h, _, _ := newHa(t)
	_, err := h.GetHaSetState(context.Background(), "default", "no-such")
	if err == nil {
		t.Error("want ErrInvalidArgument; got nil")
	}
}

func TestHa_SyncAndGet(t *testing.T) {
	h, orch, _ := newHa(t)
	orch.SyncFromSpec(&dashcenterv1.HaSetSpec{Name: "ha-1", Mode: "active_standby", MemberDpuIds: []string{"a", "b"}})
	v, err := h.GetHaSetState(context.Background(), "default", "ha-1")
	if err != nil {
		t.Fatalf("GetHaSetState: %v", err)
	}
	if len(v.Members) != 2 {
		t.Errorf("members=%d; want 2", len(v.Members))
	}
}

func TestHa_Switchover_PC_G1(t *testing.T) {
	h, orch, p := newHa(t)
	orch.SyncFromSpec(&dashcenterv1.HaSetSpec{Name: "ha-1", MemberDpuIds: []string{"a", "b"}})
	ch, err := h.TriggerSwitchover(context.Background(), "default", "ha-1", "b", "test")
	if err != nil {
		t.Fatalf("TriggerSwitchover: %v", err)
	}
	for range ch {
	}
	if p.DrainCalls != 1 || p.PromoteCalls != 1 {
		t.Errorf("drain/promote = %d/%d; want 1/1", p.DrainCalls, p.PromoteCalls)
	}
}

func TestHa_Failover_PC_G2_NoDrain(t *testing.T) {
	h, orch, p := newHa(t)
	orch.SyncFromSpec(&dashcenterv1.HaSetSpec{Name: "ha-1", MemberDpuIds: []string{"a", "b"}})
	ch, err := h.TriggerFailover(context.Background(), "default", "ha-1", "a", "b", "presumed dead")
	if err != nil {
		t.Fatalf("TriggerFailover: %v", err)
	}
	for range ch {
	}
	if p.DrainCalls != 0 {
		t.Errorf("DrainCalls=%d; want 0", p.DrainCalls)
	}
}

func TestHa_WatchEvents_PC_G3(t *testing.T) {
	h, orch, _ := newHa(t)
	orch.SyncFromSpec(&dashcenterv1.HaSetSpec{Name: "ha-1", MemberDpuIds: []string{"a", "b"}})
	ch, cancel, err := h.WatchHaEvents(HaEventFilter{})
	if err != nil {
		t.Fatalf("WatchHaEvents: %v", err)
	}
	defer cancel()
	go func() {
		stream, _ := h.TriggerSwitchover(context.Background(), "default", "ha-1", "", "")
		for range stream {
		}
	}()
	deadline := time.After(2 * time.Second)
	saw := false
	for !saw {
		select {
		case <-deadline:
			t.Fatal("never observed any HaEvent")
		case e := <-ch:
			if e.HaSetName == "ha-1" {
				saw = true
			}
		}
	}
}

func TestHa_FlowSyncStats(t *testing.T) {
	h, orch, _ := newHa(t)
	orch.SyncFromSpec(&dashcenterv1.HaSetSpec{Name: "ha-1", MemberDpuIds: []string{"a", "b"}})
	v, err := h.GetFlowSyncStats(context.Background(), "default", "ha-1")
	if err != nil {
		t.Fatalf("GetFlowSyncStats: %v", err)
	}
	if v.State != dashcenterv1.FlowSyncState_FLOW_SYNC_STATE_SYNCED {
		t.Errorf("state=%v; want SYNCED", v.State)
	}
	// Fleet-wide.
	all, err := h.GetFlowSyncStats(context.Background(), "default", "")
	if err != nil {
		t.Fatalf("GetFlowSyncStats fleet: %v", err)
	}
	if all.State != dashcenterv1.FlowSyncState_FLOW_SYNC_STATE_SYNCED {
		t.Errorf("fleet state=%v; want SYNCED", all.State)
	}
}

func TestHa_PutHaSetWiring(t *testing.T) {
	// PutHaSet through the full service layer should populate the
	// orchestrator automatically (real integration check).
	p := &orchestrator.NoOpPusher{}
	orch := orchestrator.New(p)
	svc := newServiceWithOrch(t, orch)
	_, err := svc.PutHaSet(context.Background(), "default", &dashcenterv1.HaSetSpec{
		Name: "ha-wired", Mode: "active_standby", MemberDpuIds: []string{"x", "y"},
	})
	if err != nil {
		t.Fatalf("PutHaSet: %v", err)
	}
	set, err := orch.GetSet("default", "ha-wired")
	if err != nil {
		t.Fatalf("orch.GetSet: %v", err)
	}
	if len(set.Members) != 2 {
		t.Errorf("members=%d; want 2", len(set.Members))
	}
	// Now delete it.
	if err := svc.Delete(context.Background(), "default", "ha_set", "ha-wired"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := orch.GetSet("default", "ha-wired"); err == nil {
		t.Error("orchestrator should have forgotten the deleted set")
	}
}
