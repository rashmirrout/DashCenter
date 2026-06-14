// observability_counters_test.go — bufconn integration tests for the
// PE-3c gRPC GetCounters handler.
//
// Coverage strategy:
//   * Spin a real grpc.Server over bufconn with the actual
//     observabilityHandler wired to a real Broadcaster + a stub
//     CounterReader (no counters package import needed).
//   * Exercise every branch in observability_counters.go: nil wiring,
//     snapshot-only (follow=false), follow + live, DPU filter on
//     snapshot, DPU filter on follow, ResumeAfterEventID, KIND_DROPPED
//     synthesis, ctx cancel, ErrTooManySubscribers → ResourceExhausted.

package grpcserver

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/observability/broadcaster"
	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeCounterReader is a minimal CounterReader fake: an in-memory map
// keyed by dpu_id. Sorted iteration matches the production
// counters.Store contract.
type fakeCounterReader struct {
	mu      sync.Mutex
	reports map[string]*dashcenterv1.CounterReport
}

func newFakeReader(reports ...*dashcenterv1.CounterReport) *fakeCounterReader {
	r := &fakeCounterReader{reports: map[string]*dashcenterv1.CounterReport{}}
	for _, rep := range reports {
		r.reports[rep.GetDpuId()] = rep
	}
	return r
}

func (r *fakeCounterReader) ListReports() []DpuCounterEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(r.reports))
	for id := range r.reports {
		ids = append(ids, id)
	}
	// Stable test output: sort alphabetically (matches counters.Store.List).
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[j] < ids[i] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	out := make([]DpuCounterEntry, 0, len(ids))
	for _, id := range ids {
		out = append(out, DpuCounterEntry{DpuID: id, Report: r.reports[id]})
	}
	return out
}

func (r *fakeCounterReader) GetReport(id string) (*dashcenterv1.CounterReport, bool) {
	r.mu.Lock()
	rep, ok := r.reports[id]
	r.mu.Unlock()
	return rep, ok
}

// counterServer pairs a bufconn-served observabilityHandler with its
// broadcaster, reader, and a pre-dialed client.
type counterServer struct {
	bcast  *broadcaster.Broadcaster
	reader *fakeCounterReader
	gs     *grpc.Server
	conn   *grpc.ClientConn
}

// newCounterServer constructs the harness. SetCounterWiring is called
// here so all tests start with a wired handler unless they explicitly
// re-test the unwired path.
func newCounterServer(t *testing.T, reports ...*dashcenterv1.CounterReport) *counterServer {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	bcast := broadcaster.NewBroadcaster(broadcaster.Config{
		MaxSubscribers:           8,
		MaxSubscribersPerSubject: 2,
		SubscriberBufferSize:     8,
		RingSize:                 16,
		CoalesceWindow:           0,
		EventRatePerSec:          1000,
		BurstSize:                1000,
		KeepaliveInterval:        0,
		SuppressedNoticeDelay:    20 * time.Millisecond,
	}, nil)
	t.Cleanup(bcast.Stop)
	reader := newFakeReader(reports...)

	handler := &observabilityHandler{obs: &richObservabilityStub{}}
	handler.SetCounterWiring(bcast, reader)

	gs := grpc.NewServer()
	dashcenterv1.RegisterObservabilityServiceServer(gs, handler)
	serveErr := make(chan error, 1)
	go func() { serveErr <- gs.Serve(lis) }()

	dialer := func(context.Context, string) (net.Conn, error) { return lis.Dial() }
	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		gs.Stop()
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		gs.Stop()
		select {
		case <-serveErr:
		case <-time.After(2 * time.Second):
			t.Logf("counterServer: Serve goroutine did not exit within 2s")
		}
	})
	return &counterServer{bcast: bcast, reader: reader, gs: gs, conn: conn}
}

func report(dpu string, decap int64) *dashcenterv1.CounterReport {
	return &dashcenterv1.CounterReport{
		DpuId:      dpu,
		SampledAt:  timestamppb.Now(),
		VxlanDecap: decap,
	}
}

// ── tests ────────────────────────────────────────────────────────────────

func TestGetCounters_Snapshot_NoFollow(t *testing.T) {
	t.Parallel()
	s := newCounterServer(t, report("dpu-a", 1), report("dpu-b", 2))
	client := dashcenterv1.NewObservabilityServiceClient(s.conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := client.GetCounters(ctx, &dashcenterv1.CounterRequest{Follow: false})
	if err != nil {
		t.Fatalf("GetCounters: %v", err)
	}
	got := []*dashcenterv1.CounterEvent{}
	for {
		ev, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		got = append(got, ev)
	}
	if len(got) != 2 {
		t.Fatalf("got %d frames, want 2", len(got))
	}
	for i, ev := range got {
		if ev.GetKind() != dashcenterv1.CounterEvent_KIND_SNAPSHOT {
			t.Errorf("frame %d kind = %v, want KIND_SNAPSHOT", i, ev.GetKind())
		}
	}
	if got[0].GetReport().GetDpuId() != "dpu-a" || got[1].GetReport().GetDpuId() != "dpu-b" {
		t.Errorf("dpu order: %q, %q; want dpu-a, dpu-b", got[0].GetReport().GetDpuId(), got[1].GetReport().GetDpuId())
	}
}

func TestGetCounters_Snapshot_FilterByDpu(t *testing.T) {
	t.Parallel()
	s := newCounterServer(t, report("dpu-a", 1), report("dpu-b", 2), report("dpu-c", 3))
	client := dashcenterv1.NewObservabilityServiceClient(s.conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := client.GetCounters(ctx, &dashcenterv1.CounterRequest{
		Follow:  false,
		DpuIds:  []string{"dpu-b"},
	})
	if err != nil {
		t.Fatalf("GetCounters: %v", err)
	}
	got := []*dashcenterv1.CounterEvent{}
	for {
		ev, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		got = append(got, ev)
	}
	if len(got) != 1 {
		t.Fatalf("got %d frames, want 1 (filtered to dpu-b)", len(got))
	}
	if got[0].GetReport().GetDpuId() != "dpu-b" {
		t.Errorf("dpu = %q, want dpu-b", got[0].GetReport().GetDpuId())
	}
}

func TestGetCounters_Snapshot_FilterIgnoresEmptyEntry(t *testing.T) {
	t.Parallel()
	s := newCounterServer(t, report("dpu-a", 1))
	client := dashcenterv1.NewObservabilityServiceClient(s.conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Filter ["", ""] should reduce to no filter → all DPUs returned.
	stream, err := client.GetCounters(ctx, &dashcenterv1.CounterRequest{
		Follow: false,
		DpuIds: []string{"", ""},
	})
	if err != nil {
		t.Fatalf("GetCounters: %v", err)
	}
	count := 0
	for {
		_, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		count++
	}
	if count != 1 {
		t.Errorf("got %d frames, want 1 (filter reduced to no-op)", count)
	}
}

func TestGetCounters_Follow_DeliversLiveAfterSnapshot(t *testing.T) {
	t.Parallel()
	s := newCounterServer(t, report("dpu-a", 1))
	client := dashcenterv1.NewObservabilityServiceClient(s.conn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := client.GetCounters(ctx, &dashcenterv1.CounterRequest{Follow: true})
	if err != nil {
		t.Fatalf("GetCounters: %v", err)
	}
	// Snapshot first.
	snap, err := stream.Recv()
	if err != nil {
		t.Fatalf("snapshot Recv: %v", err)
	}
	if snap.GetKind() != dashcenterv1.CounterEvent_KIND_SNAPSHOT {
		t.Errorf("first frame = %v, want KIND_SNAPSHOT", snap.GetKind())
	}
	// Publish a delta; the handler should fan it out.
	s.bcast.Publish(report("dpu-a", 999))
	live, err := stream.Recv()
	if err != nil {
		t.Fatalf("live Recv: %v", err)
	}
	if live.GetKind() != dashcenterv1.CounterEvent_KIND_REPORT {
		t.Errorf("second frame = %v, want KIND_REPORT", live.GetKind())
	}
	if live.GetReport().GetVxlanDecap() != 999 {
		t.Errorf("live decap = %d, want 999", live.GetReport().GetVxlanDecap())
	}
}

func TestGetCounters_Follow_FilterByDpu(t *testing.T) {
	t.Parallel()
	s := newCounterServer(t, report("dpu-a", 1), report("dpu-b", 2))
	client := dashcenterv1.NewObservabilityServiceClient(s.conn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := client.GetCounters(ctx, &dashcenterv1.CounterRequest{
		Follow: true,
		DpuIds: []string{"dpu-b"},
	})
	if err != nil {
		t.Fatalf("GetCounters: %v", err)
	}
	// Snapshot phase delivers only dpu-b.
	snap, err := stream.Recv()
	if err != nil || snap.GetReport().GetDpuId() != "dpu-b" {
		t.Fatalf("snapshot = %v (%v), want dpu-b", snap, err)
	}
	// Publish a dpu-a event; filtered out.
	s.bcast.Publish(report("dpu-a", 100))
	// Publish a dpu-b event; should arrive.
	s.bcast.Publish(report("dpu-b", 200))
	live, err := stream.Recv()
	if err != nil {
		t.Fatalf("live Recv: %v", err)
	}
	if live.GetReport().GetDpuId() != "dpu-b" {
		t.Errorf("live dpu = %q, want dpu-b (dpu-a should be filtered)", live.GetReport().GetDpuId())
	}
}

func TestGetCounters_NoWiring_FailedPrecondition(t *testing.T) {
	t.Parallel()
	// Construct a server WITHOUT calling SetCounterWiring; expect
	// codes.FailedPrecondition.
	lis := bufconn.Listen(1 << 20)
	handler := &observabilityHandler{obs: &richObservabilityStub{}}
	gs := grpc.NewServer()
	dashcenterv1.RegisterObservabilityServiceServer(gs, handler)
	go func() { _ = gs.Serve(lis) }()
	defer gs.Stop()
	dialer := func(context.Context, string) (net.Conn, error) { return lis.Dial() }
	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	client := dashcenterv1.NewObservabilityServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := client.GetCounters(ctx, &dashcenterv1.CounterRequest{})
	if err != nil {
		t.Fatalf("GetCounters open: %v", err)
	}
	_, err = stream.Recv()
	if status.Code(err) != codes.FailedPrecondition {
		t.Errorf("code = %v, want FailedPrecondition; err=%v", status.Code(err), err)
	}
}

func TestGetCounters_Resume_SkipsSnapshot(t *testing.T) {
	t.Parallel()
	// With resume_after_event_id > 0, snapshot phase is skipped.
	// The broadcaster will emit a RESYNC sentinel because the cursor
	// is from a previous (nonexistent) ring.
	s := newCounterServer(t, report("dpu-a", 1))
	client := dashcenterv1.NewObservabilityServiceClient(s.conn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := client.GetCounters(ctx, &dashcenterv1.CounterRequest{
		Follow:              true,
		ResumeAfterEventId:  99,
	})
	if err != nil {
		t.Fatalf("GetCounters: %v", err)
	}
	ev, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if ev.GetKind() != dashcenterv1.CounterEvent_KIND_RESYNC {
		t.Errorf("first frame = %v, want KIND_RESYNC", ev.GetKind())
	}
}

func TestGetCounters_TooManySubscribers_ResourceExhausted(t *testing.T) {
	t.Parallel()
	// Construct with cap=1 to force ResourceExhausted on the 2nd open.
	lis := bufconn.Listen(1 << 20)
	bcast := broadcaster.NewBroadcaster(broadcaster.Config{
		MaxSubscribers:       1,
		SubscriberBufferSize: 4,
		RingSize:             4,
		EventRatePerSec:      100,
		BurstSize:            100,
	}, nil)
	defer bcast.Stop()
	reader := newFakeReader(report("dpu-a", 1))
	handler := &observabilityHandler{obs: &richObservabilityStub{}}
	handler.SetCounterWiring(bcast, reader)
	gs := grpc.NewServer()
	dashcenterv1.RegisterObservabilityServiceServer(gs, handler)
	go func() { _ = gs.Serve(lis) }()
	defer gs.Stop()
	dialer := func(context.Context, string) (net.Conn, error) { return lis.Dial() }
	conn, _ := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	defer conn.Close()
	client := dashcenterv1.NewObservabilityServiceClient(conn)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// First subscribe holds the slot.
	s1, err := client.GetCounters(ctx, &dashcenterv1.CounterRequest{Follow: true})
	if err != nil {
		t.Fatalf("first GetCounters: %v", err)
	}
	if _, err := s1.Recv(); err != nil {
		t.Fatalf("first Recv: %v", err)
	}
	// Second subscribe should be rejected.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	s2, err := client.GetCounters(ctx2, &dashcenterv1.CounterRequest{Follow: true})
	if err != nil {
		t.Fatalf("second GetCounters open: %v", err)
	}
	_, err = s2.Recv()
	if status.Code(err) != codes.ResourceExhausted {
		t.Errorf("code = %v, want ResourceExhausted; err=%v", status.Code(err), err)
	}
}

func TestGetCounters_CtxCancel_ReturnsCleanly(t *testing.T) {
	t.Parallel()
	s := newCounterServer(t, report("dpu-a", 1))
	client := dashcenterv1.NewObservabilityServiceClient(s.conn)
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.GetCounters(ctx, &dashcenterv1.CounterRequest{Follow: true})
	if err != nil {
		t.Fatalf("GetCounters: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("snapshot Recv: %v", err)
	}
	cancel()
	_, err = stream.Recv()
	if err == nil {
		t.Errorf("expected error after ctx cancel; got nil")
	}
	if status.Code(err) != codes.Canceled && err != context.Canceled {
		t.Logf("ctx cancel surfaced as %v (acceptable: gRPC may translate it)", err)
	}
}

func TestGetCounters_DropOnSlow_SynthesisesDroppedNotice(t *testing.T) {
	t.Parallel()
	// Force buffer overflow by publishing faster than the client reads.
	lis := bufconn.Listen(1 << 20)
	bcast := broadcaster.NewBroadcaster(broadcaster.Config{
		MaxSubscribers:       4,
		SubscriberBufferSize: 2, // tiny → easy to overflow
		RingSize:             32,
		EventRatePerSec:      10000,
		BurstSize:            10000,
	}, nil)
	defer bcast.Stop()
	reader := newFakeReader(report("dpu-a", 1))
	handler := &observabilityHandler{obs: &richObservabilityStub{}}
	handler.SetCounterWiring(bcast, reader)
	gs := grpc.NewServer()
	dashcenterv1.RegisterObservabilityServiceServer(gs, handler)
	go func() { _ = gs.Serve(lis) }()
	defer gs.Stop()
	dialer := func(context.Context, string) (net.Conn, error) { return lis.Dial() }
	conn, _ := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	defer conn.Close()
	client := dashcenterv1.NewObservabilityServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := client.GetCounters(ctx, &dashcenterv1.CounterRequest{Follow: true})
	if err != nil {
		t.Fatalf("GetCounters: %v", err)
	}
	// Drain snapshot.
	_, _ = stream.Recv()
	// Publish a flood without reading — broadcaster will drop after
	// the per-sub buffer fills. The handler synthesises a KIND_DROPPED
	// notice before the next live frame is sent.
	for i := 0; i < 50; i++ {
		bcast.Publish(report("dpu-a", int64(i)))
	}
	// Drain everything; find at least one KIND_DROPPED.
	sawDropped := false
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("never saw KIND_DROPPED notice")
		default:
		}
		ev, err := stream.Recv()
		if err != nil {
			break
		}
		if ev.GetKind() == dashcenterv1.CounterEvent_KIND_DROPPED {
			sawDropped = true
			if ev.GetNotice().GetDroppedCount() == 0 {
				t.Errorf("KIND_DROPPED carried zero count")
			}
			break
		}
	}
	if !sawDropped {
		t.Errorf("expected at least one KIND_DROPPED notice")
	}
}

// dpuFilterSet coverage (the helper has its own small branches).

func TestDpuFilterSet(t *testing.T) {
	t.Parallel()
	if got := dpuFilterSet(nil); got != nil {
		t.Errorf("nil → %v, want nil", got)
	}
	if got := dpuFilterSet([]string{}); got != nil {
		t.Errorf("empty → %v, want nil", got)
	}
	if got := dpuFilterSet([]string{"", ""}); got != nil {
		t.Errorf("all-empty → %v, want nil (degraded to no-op)", got)
	}
	got := dpuFilterSet([]string{"a", "", "b", "a"})
	if len(got) != 2 {
		t.Errorf("len = %d, want 2 (a, b deduped)", len(got))
	}
	if _, ok := got["a"]; !ok {
		t.Errorf("a missing")
	}
	if _, ok := got["b"]; !ok {
		t.Errorf("b missing")
	}
}
