// Wire-level tests for client.Client.GetDpuCounters using an in-process
// gRPC server. The fake DashApi server only implements the methods this
// client surface needs — everything else returns Unimplemented.

package client

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeDashApiServer is the in-process backend the client dials. Each
// test installs its own GetDpuCounters handler via the handler field to
// produce the response (or error) it wants to verify.
type fakeDashApiServer struct {
	dashapi.UnimplementedDashApiServer
	mu              sync.Mutex
	calls           int
	lastReq         *dashapi.DpuCountersRequest
	handler         func(req *dashapi.DpuCountersRequest) (*dashapi.DpuCountersResponse, error)
}

func (f *fakeDashApiServer) GetDpuCounters(_ context.Context, req *dashapi.DpuCountersRequest) (*dashapi.DpuCountersResponse, error) {
	f.mu.Lock()
	f.calls++
	f.lastReq = req
	h := f.handler
	f.mu.Unlock()
	if h != nil {
		return h(req)
	}
	return &dashapi.DpuCountersResponse{DeviceId: "fake", SampledAtNs: 42}, nil
}

// spinServer starts a real grpc.Server on a localhost ephemeral port and
// returns the dialled Client + a cleanup closure. Cancellation propagates
// through to the server's Stop().
func spinServer(t *testing.T, fake *fakeDashApiServer) (*Client, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gs := grpc.NewServer()
	dashapi.RegisterDashApiServer(gs, fake)
	go func() { _ = gs.Serve(lis) }()
	cl, err := Dial(lis.Addr().String())
	if err != nil {
		gs.Stop()
		_ = lis.Close()
		t.Fatalf("dial: %v", err)
	}
	cleanup := func() {
		_ = cl.Close()
		gs.GracefulStop()
		_ = lis.Close()
	}
	return cl, cleanup
}

// ── happy paths ───────────────────────────────────────────────────────────

func TestGetDpuCounters_NilReqDefaultsToEmpty(t *testing.T) {
	fake := &fakeDashApiServer{}
	cl, cleanup := spinServer(t, fake)
	defer cleanup()
	resp, err := cl.GetDpuCounters(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetDeviceId() != "fake" {
		t.Errorf("device_id=%q", resp.GetDeviceId())
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.lastReq == nil {
		t.Fatal("server saw nil request")
	}
	if fake.lastReq.GetIncludeEnis() || fake.lastReq.GetIncludeVnets() {
		t.Errorf("default request must not opt in to scopes: %+v", fake.lastReq)
	}
}

func TestGetDpuCounters_PassesFlagsThrough(t *testing.T) {
	fake := &fakeDashApiServer{}
	cl, cleanup := spinServer(t, fake)
	defer cleanup()

	req := &dashapi.DpuCountersRequest{
		IncludeEnis:  true,
		IncludeVnets: true,
		EniNames:     []string{"eni-001", "eni-002"},
		VnetKeys:     []string{"vnet-prod"},
	}
	if _, err := cl.GetDpuCounters(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	got := fake.lastReq
	if !got.GetIncludeEnis() || !got.GetIncludeVnets() {
		t.Errorf("flags not propagated: %+v", got)
	}
	if len(got.GetEniNames()) != 2 || got.GetEniNames()[1] != "eni-002" {
		t.Errorf("eni_names not propagated: %+v", got.GetEniNames())
	}
	if len(got.GetVnetKeys()) != 1 || got.GetVnetKeys()[0] != "vnet-prod" {
		t.Errorf("vnet_keys not propagated: %+v", got.GetVnetKeys())
	}
}

// ── error paths ──────────────────────────────────────────────────────────

func TestGetDpuCounters_PropagatesServerError(t *testing.T) {
	fake := &fakeDashApiServer{
		handler: func(_ *dashapi.DpuCountersRequest) (*dashapi.DpuCountersResponse, error) {
			return nil, status.Error(codes.Unavailable, "injected")
		},
	}
	cl, cleanup := spinServer(t, fake)
	defer cleanup()
	_, err := cl.GetDpuCounters(context.Background(), nil)
	if err == nil {
		t.Fatal("want error from server")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unavailable {
		t.Errorf("want Unavailable status, got %v", err)
	}
}

func TestGetDpuCounters_HonoursContextCancel(t *testing.T) {
	fake := &fakeDashApiServer{
		handler: func(_ *dashapi.DpuCountersRequest) (*dashapi.DpuCountersResponse, error) {
			time.Sleep(2 * time.Second)
			return &dashapi.DpuCountersResponse{}, nil
		},
	}
	cl, cleanup := spinServer(t, fake)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := cl.GetDpuCounters(ctx, nil)
	if err == nil {
		t.Fatal("want context error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		st, ok := status.FromError(err)
		if !ok || st.Code() != codes.DeadlineExceeded {
			t.Errorf("want DeadlineExceeded, got %v", err)
		}
	}
}
