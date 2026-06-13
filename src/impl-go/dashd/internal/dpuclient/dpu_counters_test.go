// dpu_counters_test.go covers the MockClient.GetDpuCounters path
// (PE-3b). The realClient.GetDpuCounters path is exercised by the
// dashd integration tests in test/integration/counters_test.go which
// stand up an in-process sim.

package dpuclient

import (
	"context"
	"errors"
	"testing"
	"time"

	dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
)

func TestMockClient_GetDpuCounters_StaticResp(t *testing.T) {
	m := NewMockClient()
	want := &dashapiv1.DpuCountersResponse{DeviceId: "sim-1", SampledAtNs: 42}
	m.CountersResp = want

	got, err := m.GetDpuCounters(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetDpuCounters: %v", err)
	}
	if got != want {
		t.Errorf("resp = %p, want %p (static)", got, want)
	}
	if m.GetDpuCountersCallCount() != 1 {
		t.Errorf("call count = %d, want 1", m.GetDpuCountersCallCount())
	}
}

func TestMockClient_GetDpuCounters_StaticErr(t *testing.T) {
	m := NewMockClient()
	m.CountersErr = errors.New("injected")
	_, err := m.GetDpuCounters(context.Background(), &dashapiv1.DpuCountersRequest{})
	if err == nil || err.Error() != "injected" {
		t.Errorf("err = %v, want injected", err)
	}
}

func TestMockClient_GetDpuCounters_PerCallFn(t *testing.T) {
	m := NewMockClient()
	m.CountersFn = func(n int, req *dashapiv1.DpuCountersRequest) (*dashapiv1.DpuCountersResponse, error) {
		if req == nil {
			t.Errorf("req should be non-nil from realClient adapter, mock receives caller's value")
		}
		return &dashapiv1.DpuCountersResponse{SampledAtNs: int64(n)}, nil
	}
	for i := 1; i <= 3; i++ {
		got, err := m.GetDpuCounters(context.Background(), &dashapiv1.DpuCountersRequest{})
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if got.GetSampledAtNs() != int64(i) {
			t.Errorf("call %d: SampledAtNs=%d, want %d", i, got.GetSampledAtNs(), i)
		}
	}
}

func TestMockClient_GetDpuCounters_ClosedErrors(t *testing.T) {
	m := NewMockClient()
	_ = m.Close()
	_, err := m.GetDpuCounters(context.Background(), nil)
	if err == nil {
		t.Errorf("expected error from closed mock")
	}
}

func TestMockClient_GetDpuCounters_CtxCancelled(t *testing.T) {
	m := NewMockClient()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := m.GetDpuCounters(ctx, nil)
	if err == nil {
		t.Errorf("expected ctx-cancelled error")
	}
}

func TestMockClient_Reset_ZeroesCounterCount(t *testing.T) {
	m := NewMockClient()
	m.CountersResp = &dashapiv1.DpuCountersResponse{}
	_, _ = m.GetDpuCounters(context.Background(), nil)
	_, _ = m.GetDpuCounters(context.Background(), nil)
	if m.GetDpuCountersCallCount() != 2 {
		t.Fatalf("setup: want 2 calls, got %d", m.GetDpuCountersCallCount())
	}
	m.Reset()
	if m.GetDpuCountersCallCount() != 0 {
		t.Errorf("after Reset: count = %d, want 0", m.GetDpuCountersCallCount())
	}
}

func TestMockClient_GetDpuCounters_NoBlock(t *testing.T) {
	// Confirms the mock is non-blocking even under contention; ensures
	// the poller goroutine cannot deadlock against the mock.
	m := NewMockClient()
	m.CountersResp = &dashapiv1.DpuCountersResponse{}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			_, _ = m.GetDpuCounters(context.Background(), nil)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("mock GetDpuCounters appears to block")
	}
}
