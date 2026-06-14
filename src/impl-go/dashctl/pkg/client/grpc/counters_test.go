// counters_test.go — UT for dashctl gRPC counter client.
//
// Uses bufconn to stand a real ObservabilityServiceServer that serves
// either a few snapshot frames or a live follow stream, then drives
// CountersClient against it.

package grpcclient

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// fakeObsServer hosts a controllable GetCounters implementation.
type fakeObsServer struct {
	dashcenterv1.UnimplementedObservabilityServiceServer
	mu     sync.Mutex
	events []*dashcenterv1.CounterEvent
	hold   bool // when true, stream stays open after sending events
}

func (f *fakeObsServer) GetCounters(req *dashcenterv1.CounterRequest, stream dashcenterv1.ObservabilityService_GetCountersServer) error {
	f.mu.Lock()
	evs := append([]*dashcenterv1.CounterEvent(nil), f.events...)
	hold := f.hold
	f.mu.Unlock()
	for _, ev := range evs {
		if err := stream.Send(ev); err != nil {
			return err
		}
	}
	if hold {
		<-stream.Context().Done()
	}
	return nil
}

func startFakeServer(t *testing.T, srv *fakeObsServer) *CountersClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	dashcenterv1.RegisterObservabilityServiceServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &CountersClient{conn: conn, client: dashcenterv1.NewObservabilityServiceClient(conn)}
}

func TestNewCountersClient_RejectsEmptyEndpoint(t *testing.T) {
	t.Parallel()
	_, err := NewCountersClient(context.Background(), DialOptions{Endpoint: ""})
	if err == nil || !errors.Is(err, errors.Unwrap(err)) && err.Error() == "" {
		t.Errorf("err = %v, want non-nil", err)
	}
}

func TestCountersClient_Close_NilSafe(t *testing.T) {
	t.Parallel()
	c := &CountersClient{}
	if err := c.Close(); err != nil {
		t.Errorf("Close on nil conn: %v", err)
	}
}

func TestCountersClient_GetCountersSnapshot_HappyPath(t *testing.T) {
	t.Parallel()
	srv := &fakeObsServer{
		events: []*dashcenterv1.CounterEvent{
			{
				Kind:    dashcenterv1.CounterEvent_KIND_SNAPSHOT,
				EventId: 1,
				Body: &dashcenterv1.CounterEvent_Report{Report: &dashcenterv1.CounterReport{DpuId: "dpu-a", VxlanDecap: 7}},
			},
			{
				Kind:    dashcenterv1.CounterEvent_KIND_SNAPSHOT,
				EventId: 2,
				Body: &dashcenterv1.CounterEvent_Report{Report: &dashcenterv1.CounterReport{DpuId: "dpu-b", VxlanDecap: 99}},
			},
			// Non-snapshot frame should be ignored in one-shot mode.
			{
				Kind:    dashcenterv1.CounterEvent_KIND_REPORT,
				EventId: 3,
				Body: &dashcenterv1.CounterEvent_Report{Report: &dashcenterv1.CounterReport{DpuId: "dpu-a", VxlanDecap: 8}},
			},
		},
	}
	c := startFakeServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	snap, err := c.GetCountersSnapshot(ctx, []string{"dpu-a", "dpu-b"})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snap.Reports) != 2 {
		t.Errorf("got %d reports, want 2 (non-snapshot frame filtered)", len(snap.Reports))
	}
	if snap.Reports[0].DpuId != "dpu-a" || snap.Reports[0].VxlanDecap != "7" {
		t.Errorf("first report = %+v", snap.Reports[0])
	}
}

func TestCountersClient_StreamCounters_HappyPath(t *testing.T) {
	t.Parallel()
	srv := &fakeObsServer{
		events: []*dashcenterv1.CounterEvent{
			{Kind: dashcenterv1.CounterEvent_KIND_SNAPSHOT, EventId: 1, Body: &dashcenterv1.CounterEvent_Report{Report: &dashcenterv1.CounterReport{DpuId: "dpu-a"}}},
			{Kind: dashcenterv1.CounterEvent_KIND_REPORT, EventId: 2, Body: &dashcenterv1.CounterEvent_Report{Report: &dashcenterv1.CounterReport{DpuId: "dpu-a", VxlanDecap: 11}}},
			{Kind: dashcenterv1.CounterEvent_KIND_KEEPALIVE, EventId: 3, Body: &dashcenterv1.CounterEvent_Notice{Notice: &dashcenterv1.Notice{Message: "kpa"}}},
		},
		hold: true,
	}
	c := startFakeServer(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := []client.CounterEvent{}
	err := c.StreamCounters(ctx, client.CountersWatchOptions{
		OnEvent: func(ev client.CounterEvent) error {
			got = append(got, ev)
			if len(got) == 3 {
				return errSentinel
			}
			return nil
		},
	})
	if !errors.Is(err, errSentinel) {
		t.Fatalf("err = %v, want sentinel", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3", len(got))
	}
	if got[1].Report == nil || got[1].Report.VxlanDecap != "11" {
		t.Errorf("report = %+v", got[1].Report)
	}
	if got[2].Kind != "KIND_KEEPALIVE" || got[2].Notice == nil || got[2].Notice.Message != "kpa" {
		t.Errorf("keepalive = %+v", got[2])
	}
}

func TestCountersClient_StreamCounters_RequiresOnEvent(t *testing.T) {
	t.Parallel()
	c := startFakeServer(t, &fakeObsServer{})
	err := c.StreamCounters(context.Background(), client.CountersWatchOptions{})
	if err == nil {
		t.Fatal("expected error for nil OnEvent")
	}
}

func TestCountersClient_StreamCounters_CtxCancel(t *testing.T) {
	t.Parallel()
	srv := &fakeObsServer{hold: true}
	c := startFakeServer(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- c.StreamCounters(ctx, client.CountersWatchOptions{
			OnEvent: func(client.CounterEvent) error { return nil },
		})
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-doneCh:
		if err == nil {
			t.Errorf("expected error after ctx cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not return after cancel")
	}
}

var errSentinel = errors.New("test stop")
