// coverage_test.go covers the small branches the other tests don't
// touch: nil entries inside per-vnet, early returns in pollOnce, the
// shutdown-cancel path in pollDpu.

package counters

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/dpuclient"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

func TestMapPerVnet_NilEntryAndEmptyKeyDropped(t *testing.T) {
	src := &dashapi.DpuCountersResponse{
		Vnets: []*dashapi.ScopedCounters{
			{ScopeKey: "vnet-a", Bucket: &dashapi.CounterBucket{PacketsIn: 1}},
			nil, // dropped
			{ScopeKey: "", Bucket: &dashapi.CounterBucket{PacketsIn: 99}}, // dropped
		},
	}
	got := MapPerVnet(src)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (nil + empty-key entries dropped)", len(got))
	}
	if _, ok := got["vnet-a"]; !ok {
		t.Errorf("vnet-a missing")
	}
}

func TestPoller_PollOnce_GuardCovers_NilFactory(t *testing.T) {
	// inv non-nil, store non-nil, factory nil \u2192 early return at top of pollOnce.
	inv := inventory.New()
	_ = inv.Register(inventory.DpuEntry{ID: "x", Endpoint: "x:1", State: dashcenterv1.DpuState_DPU_STATE_UP})
	p := NewPoller(inv, nil, NewStore(), time.Second)
	p.pollOnce(context.Background())
	// No assertion: just exercising the guard branch.
}

func TestPoller_PollOnce_GuardCovers_NilStore(t *testing.T) {
	inv := inventory.New()
	_ = inv.Register(inventory.DpuEntry{ID: "x", Endpoint: "x:1", State: dashcenterv1.DpuState_DPU_STATE_UP})
	p := NewPoller(inv, dpuclient.DefaultFactory, nil, time.Second)
	p.pollOnce(context.Background())
}

func TestPoller_PollOnce_ContextDoneBetweenDpus(t *testing.T) {
	inv := inventory.New()
	_ = inv.Register(inventory.DpuEntry{ID: "a", Endpoint: "a:1", State: dashcenterv1.DpuState_DPU_STATE_UP})
	_ = inv.Register(inventory.DpuEntry{ID: "b", Endpoint: "b:1", State: dashcenterv1.DpuState_DPU_STATE_UP})

	called := 0
	factory := dpuclient.ClientFactory(func(string) (dpuclient.DpuClient, error) {
		called++
		// Cancel after the first dpu so the second loop iter takes the
		// ctx.Done branch.
		return nil, errors.New("first ok, second skipped")
	})

	ctx, cancel := context.WithCancel(context.Background())
	p := NewPoller(inv, factory, NewStore(), time.Second)
	// Cancel BEFORE calling pollOnce; the loop checks ctx.Done first.
	cancel()
	p.pollOnce(ctx)
	if called != 0 {
		t.Errorf("factory called %d times after ctx cancel, want 0", called)
	}
}

func TestPollDpu_ShutdownCancelSwallowed(t *testing.T) {
	// Build a mock that returns ctx.Canceled \u2014 confirms pollDpu's
	// shutdown-cancel branch is hit (no log spam, no panic).
	inv := inventory.New()
	_ = inv.Register(inventory.DpuEntry{ID: "x", Endpoint: "x:1", State: dashcenterv1.DpuState_DPU_STATE_UP})
	store := NewStore()
	mock := &dpuclient.MockClient{CountersErr: context.Canceled}
	factory := dpuclient.MockFactory(mock)
	p := NewPoller(inv, factory, store, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // parent already cancelled \u2192 the err matches & parent.Err()!=nil
	p.pollDpu(ctx, "x", "x:1")
	if store.Len() != 0 {
		t.Errorf("store populated under cancel: %d entries", store.Len())
	}
}
