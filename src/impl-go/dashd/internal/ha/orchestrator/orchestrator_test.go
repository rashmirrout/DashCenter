// PC-G1 / PC-G2 / PC-G3 orchestrator state-machine tests.
package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

func mkSet(name string, members ...string) *dashcenterv1.HaSetSpec {
	return &dashcenterv1.HaSetSpec{
		Namespace: "default", Name: name, Mode: "active_standby", MemberDpuIds: members,
	}
}

// --- PC-G1: switchover happy path ------------------------------------

func TestSwitchover_HappyPath_PC_G1(t *testing.T) {
	p := &NoOpPusher{}
	o := New(p)
	o.SyncFromSpec(mkSet("ha-1", "dpu-a", "dpu-b"))

	// dpu-a is auto-active (first member), dpu-b is STANDBY.
	set, err := o.GetSet("default", "ha-1")
	if err != nil {
		t.Fatal(err)
	}
	if set.Members[0].Role != dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_ACTIVE {
		t.Fatalf("dpu-a not ACTIVE; got %v", set.Members[0].Role)
	}

	ch, err := o.TriggerSwitchover(context.Background(), "default", "ha-1", "dpu-b", "maintenance")
	if err != nil {
		t.Fatalf("TriggerSwitchover: %v", err)
	}
	// Collect every streamed row.
	var rows []dashcenterv1.HaScopeStatus
	for r := range ch {
		rows = append(rows, r)
	}
	if len(rows) < 4 {
		t.Fatalf("expected at least 4 status rows, got %d: %+v", len(rows), rows)
	}
	// Final state: dpu-b ACTIVE, dpu-a STANDBY.
	final, _ := o.GetSet("default", "ha-1")
	for _, m := range final.Members {
		switch m.DpuID {
		case "dpu-a":
			if m.Role != dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_STANDBY {
				t.Errorf("dpu-a final role=%v; want STANDBY", m.Role)
			}
		case "dpu-b":
			if m.Role != dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_ACTIVE {
				t.Errorf("dpu-b final role=%v; want ACTIVE", m.Role)
			}
		}
	}
	// Switchover MUST have called all three Pusher methods.
	if p.DrainCalls != 1 || p.PromoteCalls != 1 || p.DemoteCalls != 1 {
		t.Errorf("pusher calls drain=%d promote=%d demote=%d; want 1/1/1",
			p.DrainCalls, p.PromoteCalls, p.DemoteCalls)
	}
	if p.LastDrainDpuID != "dpu-a" {
		t.Errorf("drain target=%q; want dpu-a (the old active)", p.LastDrainDpuID)
	}
}

func TestSwitchover_DrainFailure_RollsBack(t *testing.T) {
	p := &NoOpPusher{DrainErr: errors.New("DPU did not respond to drain")}
	o := New(p)
	o.SyncFromSpec(mkSet("ha-1", "dpu-a", "dpu-b"))

	ch, err := o.TriggerSwitchover(context.Background(), "default", "ha-1", "", "")
	if err != nil {
		t.Fatalf("TriggerSwitchover: %v", err)
	}
	for range ch {
	}
	// Roles must be back to pre-switchover.
	set, _ := o.GetSet("default", "ha-1")
	if set.Members[0].Role != dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_ACTIVE {
		t.Errorf("dpu-a role after rollback=%v; want ACTIVE", set.Members[0].Role)
	}
	if set.Members[1].Role != dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_STANDBY {
		t.Errorf("dpu-b role after rollback=%v; want STANDBY", set.Members[1].Role)
	}
	// Promote should NOT have been called (we aborted before it).
	if p.PromoteCalls != 0 {
		t.Errorf("PromoteCalls=%d; want 0 (aborted on drain failure)", p.PromoteCalls)
	}
}

func TestSwitchover_PromoteFailure_RollsBack(t *testing.T) {
	p := &NoOpPusher{PromoteErr: errors.New("promote rejected")}
	o := New(p)
	o.SyncFromSpec(mkSet("ha-1", "dpu-a", "dpu-b"))

	ch, _ := o.TriggerSwitchover(context.Background(), "default", "ha-1", "", "")
	for range ch {
	}
	set, _ := o.GetSet("default", "ha-1")
	if set.Members[0].Role != dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_ACTIVE {
		t.Errorf("dpu-a role=%v; want ACTIVE (promote failed; rollback)", set.Members[0].Role)
	}
}

func TestSwitchover_UnknownSet(t *testing.T) {
	o := New(&NoOpPusher{})
	_, err := o.TriggerSwitchover(context.Background(), "default", "no-such", "", "")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v; want ErrNotFound", err)
	}
}

func TestSwitchover_NoActive(t *testing.T) {
	o := New(&NoOpPusher{})
	o.SyncFromSpec(mkSet("ha-1", "dpu-a", "dpu-b"))
	// Force both to STANDBY.
	set := o.sets[key("default", "ha-1")]
	set.Members[0].Role = dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_STANDBY
	_, err := o.TriggerSwitchover(context.Background(), "default", "ha-1", "dpu-b", "")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("got %v; want ErrInvalidTransition", err)
	}
}

func TestSwitchover_TargetAlreadyActive(t *testing.T) {
	o := New(&NoOpPusher{})
	o.SyncFromSpec(mkSet("ha-1", "dpu-a", "dpu-b"))
	_, err := o.TriggerSwitchover(context.Background(), "default", "ha-1", "dpu-a", "")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("got %v; want ErrInvalidTransition (target is already active)", err)
	}
}

// --- PC-G2: failover does NOT contact old active ---------------------

func TestFailover_DoesNotInvokeDrain_PC_G2(t *testing.T) {
	p := &NoOpPusher{}
	o := New(p)
	o.SyncFromSpec(mkSet("ha-1", "dpu-a", "dpu-b"))

	ch, err := o.TriggerFailover(context.Background(), "default", "ha-1", "dpu-a", "dpu-b", "ICMP unreachable")
	if err != nil {
		t.Fatalf("TriggerFailover: %v", err)
	}
	for range ch {
	}
	// PC-G2 contract.
	if p.DrainCalls != 0 {
		t.Errorf("DrainCalls=%d; want 0 (failover must NOT contact old active)", p.DrainCalls)
	}
	if p.DemoteCalls != 0 {
		t.Errorf("DemoteCalls=%d; want 0 (failover skips demote too)", p.DemoteCalls)
	}
	if p.PromoteCalls != 1 {
		t.Errorf("PromoteCalls=%d; want 1 (target promote always happens)", p.PromoteCalls)
	}
	// Final: dpu-a DEAD, dpu-b ACTIVE.
	set, _ := o.GetSet("default", "ha-1")
	for _, m := range set.Members {
		switch m.DpuID {
		case "dpu-a":
			if m.Role != dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_DEAD {
				t.Errorf("dpu-a final=%v; want DEAD", m.Role)
			}
		case "dpu-b":
			if m.Role != dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_ACTIVE {
				t.Errorf("dpu-b final=%v; want ACTIVE", m.Role)
			}
		}
	}
}

func TestFailover_UnknownFailedDpu(t *testing.T) {
	o := New(&NoOpPusher{})
	o.SyncFromSpec(mkSet("ha-1", "dpu-a", "dpu-b"))
	_, err := o.TriggerFailover(context.Background(), "default", "ha-1", "dpu-typo", "", "")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("got %v; want ErrInvalidTransition", err)
	}
}

func TestFailover_NoStandbyTarget(t *testing.T) {
	o := New(&NoOpPusher{})
	// Single-member set — failover has no target.
	o.SyncFromSpec(mkSet("ha-1", "dpu-a"))
	_, err := o.TriggerFailover(context.Background(), "default", "ha-1", "dpu-a", "", "")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("got %v; want ErrInvalidTransition", err)
	}
}

// --- PC-G3: WatchHaEvents delivers role-changed events ---------------

func TestWatchHaEvents_DeliveryDuringSwitchover_PC_G3(t *testing.T) {
	o := New(&NoOpPusher{})
	o.SyncFromSpec(mkSet("ha-1", "dpu-a", "dpu-b"))

	ch, cancel := o.Broadcaster().Subscribe(Filter{})
	defer cancel()

	go func() {
		stream, _ := o.TriggerSwitchover(context.Background(), "default", "ha-1", "dpu-b", "PC-G3")
		for range stream {
		}
	}()

	// Collect events for up to 2 seconds; we expect SWITCHOVER_STARTED,
	// multiple ROLE_CHANGED, and SWITCHOVER_COMPLETED.
	deadline := time.After(2 * time.Second)
	got := map[dashcenterv1.HaEvent_Type]int{}
	for {
		select {
		case <-deadline:
			if got[dashcenterv1.HaEvent_TYPE_SWITCHOVER_STARTED] == 0 {
				t.Errorf("never observed SWITCHOVER_STARTED")
			}
			if got[dashcenterv1.HaEvent_TYPE_SWITCHOVER_COMPLETED] == 0 {
				t.Errorf("never observed SWITCHOVER_COMPLETED")
			}
			if got[dashcenterv1.HaEvent_TYPE_ROLE_CHANGED] < 2 {
				t.Errorf("ROLE_CHANGED count=%d; want >=2", got[dashcenterv1.HaEvent_TYPE_ROLE_CHANGED])
			}
			return
		case e := <-ch:
			got[e.Type]++
			if got[dashcenterv1.HaEvent_TYPE_SWITCHOVER_COMPLETED] > 0 {
				// Done.
				if got[dashcenterv1.HaEvent_TYPE_SWITCHOVER_STARTED] == 0 {
					t.Errorf("never observed SWITCHOVER_STARTED")
				}
				if got[dashcenterv1.HaEvent_TYPE_ROLE_CHANGED] < 2 {
					t.Errorf("ROLE_CHANGED count=%d; want >=2", got[dashcenterv1.HaEvent_TYPE_ROLE_CHANGED])
				}
				return
			}
		}
	}
}

func TestWatchHaEvents_FilterByType(t *testing.T) {
	o := New(&NoOpPusher{})
	o.SyncFromSpec(mkSet("ha-1", "dpu-a", "dpu-b"))
	ch, cancel := o.Broadcaster().Subscribe(Filter{
		Types: []dashcenterv1.HaEvent_Type{dashcenterv1.HaEvent_TYPE_SWITCHOVER_STARTED},
	})
	defer cancel()
	go func() {
		stream, _ := o.TriggerSwitchover(context.Background(), "default", "ha-1", "", "")
		for range stream {
		}
	}()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("never received the filtered SWITCHOVER_STARTED event")
		case e := <-ch:
			if e.Type != dashcenterv1.HaEvent_TYPE_SWITCHOVER_STARTED {
				t.Errorf("filter leaked event type %v", e.Type)
			}
			return
		}
	}
}

func TestWatchHaEvents_SlowSubscriberDoesNotBlockOrchestrator(t *testing.T) {
	o := New(&NoOpPusher{})
	o.SyncFromSpec(mkSet("ha-1", "dpu-a", "dpu-b"))
	// Subscribe but never read.
	_, cancel := o.Broadcaster().Subscribe(Filter{})
	defer cancel()
	// Publish way more events than the per-sub buffer (32) to prove
	// the broadcaster drops instead of blocking.
	for i := 0; i < 200; i++ {
		o.Broadcaster().Publish(dashcenterv1.HaEvent{Type: dashcenterv1.HaEvent_TYPE_ROLE_CHANGED, HaSetName: "ha-1"})
	}
	// If the broadcaster blocked, we would not reach this line in
	// reasonable time. Reaching it = pass.
}

// --- SyncFromSpec idempotency ----------------------------------------

func TestSyncFromSpec_PreservesExistingRoles(t *testing.T) {
	o := New(&NoOpPusher{})
	o.SyncFromSpec(mkSet("ha-1", "dpu-a", "dpu-b"))
	// Promote dpu-b via switchover.
	ch, _ := o.TriggerSwitchover(context.Background(), "default", "ha-1", "dpu-b", "")
	for range ch {
	}
	// Re-apply the same spec.
	o.SyncFromSpec(mkSet("ha-1", "dpu-a", "dpu-b"))
	set, _ := o.GetSet("default", "ha-1")
	for _, m := range set.Members {
		if m.DpuID == "dpu-b" && m.Role != dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_ACTIVE {
			t.Errorf("re-apply lost dpu-b ACTIVE role; got %v", m.Role)
		}
	}
}

func TestSyncFromSpec_AddedMemberStartsStandby(t *testing.T) {
	o := New(&NoOpPusher{})
	o.SyncFromSpec(mkSet("ha-1", "dpu-a", "dpu-b"))
	o.SyncFromSpec(mkSet("ha-1", "dpu-a", "dpu-b", "dpu-c"))
	set, _ := o.GetSet("default", "ha-1")
	for _, m := range set.Members {
		if m.DpuID == "dpu-c" && m.Role != dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_STANDBY {
			t.Errorf("new member dpu-c role=%v; want STANDBY", m.Role)
		}
	}
}

func TestRemove(t *testing.T) {
	o := New(&NoOpPusher{})
	o.SyncFromSpec(mkSet("ha-1", "dpu-a"))
	o.Remove("default", "ha-1")
	_, err := o.GetSet("default", "ha-1")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("after Remove: %v; want ErrNotFound", err)
	}
}

func TestListSets_Sorted(t *testing.T) {
	o := New(&NoOpPusher{})
	o.SyncFromSpec(mkSet("z", "a"))
	o.SyncFromSpec(mkSet("a", "b"))
	o.SyncFromSpec(mkSet("m", "c"))
	out := o.ListSets()
	if len(out) != 3 || out[0].Name != "a" || out[1].Name != "m" || out[2].Name != "z" {
		t.Errorf("ListSets order wrong: %v", out)
	}
}

// --- Broadcaster basics ----------------------------------------------

func TestBroadcaster_SubscribeUnsubscribe(t *testing.T) {
	b := NewBroadcaster()
	_, cancel := b.Subscribe(Filter{})
	if b.Count() != 1 {
		t.Errorf("Count=%d; want 1", b.Count())
	}
	cancel()
	if b.Count() != 0 {
		t.Errorf("Count after cancel=%d; want 0", b.Count())
	}
}

func TestBroadcaster_PublishToMultiSub(t *testing.T) {
	b := NewBroadcaster()
	ch1, c1 := b.Subscribe(Filter{})
	ch2, c2 := b.Subscribe(Filter{})
	defer c1()
	defer c2()
	b.Publish(dashcenterv1.HaEvent{Type: dashcenterv1.HaEvent_TYPE_ROLE_CHANGED, HaSetName: "x"})
	for _, ch := range []<-chan dashcenterv1.HaEvent{ch1, ch2} {
		select {
		case e := <-ch:
			if e.HaSetName != "x" {
				t.Errorf("got %v", e)
			}
		case <-time.After(time.Second):
			t.Fatal("subscriber never received event")
		}
	}
}

func TestFilter_AcceptCombos(t *testing.T) {
	cases := []struct {
		name   string
		filter Filter
		event  dashcenterv1.HaEvent
		want   bool
	}{
		{"empty accepts all", Filter{}, dashcenterv1.HaEvent{HaSetName: "x"}, true},
		{"ns match", Filter{Namespaces: []string{"a"}}, dashcenterv1.HaEvent{Namespace: "a"}, true},
		{"ns miss", Filter{Namespaces: []string{"a"}}, dashcenterv1.HaEvent{Namespace: "b"}, false},
		{"name match", Filter{HaSetNames: []string{"x"}}, dashcenterv1.HaEvent{HaSetName: "x"}, true},
		{"name miss", Filter{HaSetNames: []string{"x"}}, dashcenterv1.HaEvent{HaSetName: "y"}, false},
		{"type match", Filter{Types: []dashcenterv1.HaEvent_Type{dashcenterv1.HaEvent_TYPE_ROLE_CHANGED}}, dashcenterv1.HaEvent{Type: dashcenterv1.HaEvent_TYPE_ROLE_CHANGED}, true},
		{"type miss", Filter{Types: []dashcenterv1.HaEvent_Type{dashcenterv1.HaEvent_TYPE_ROLE_CHANGED}}, dashcenterv1.HaEvent{Type: dashcenterv1.HaEvent_TYPE_FAILOVER_STARTED}, false},
	}
	for _, c := range cases {
		if got := c.filter.accept(c.event); got != c.want {
			t.Errorf("%s: got=%v want=%v", c.name, got, c.want)
		}
	}
}

func TestNoOpPusher_DelayHonoursCtx(t *testing.T) {
	p := &NoOpPusher{DrainDelay: 200 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := p.DrainOldActive(ctx, "x", "v", "s")
	if err == nil {
		t.Error("ctx cancel should have aborted drain")
	}
}
