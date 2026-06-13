// Tests for the dpu-counters CLI subcommand. The cobra plumbing is
// covered indirectly (cmd construction + flag parsing); the watch loop
// is exercised with a fake client to verify multi-tick behaviour without
// spinning a gRPC server.

package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim-client/internal/render"
)

// fakeDpuCountersClient implements the minimal interface watchDpuCounters
// needs (so we don't need a real gRPC connection).
type fakeDpuCountersClient struct {
	mu        sync.Mutex
	calls     int32
	respFn    func(req *dashapi.DpuCountersRequest) (*dashapi.DpuCountersResponse, error)
}

func (f *fakeDpuCountersClient) GetDpuCounters(_ context.Context, req *dashapi.DpuCountersRequest) (*dashapi.DpuCountersResponse, error) {
	atomic.AddInt32(&f.calls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.respFn != nil {
		return f.respFn(req)
	}
	return &dashapi.DpuCountersResponse{DeviceId: "fake", SampledAtNs: 1}, nil
}

// pipeStdout redirects os.Stdout into a buffer for the duration of fn.
// Restored on return. Used because watchDpuCounters writes to os.Stdout
// directly (the same shape the cobra subcommands use in production).
func pipeStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() {
		os.Stdout = orig
	}()
	doneCh := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		defer close(doneCh)
		_, _ = io.Copy(&buf, r)
	}()

	runErr := fn()
	_ = w.Close()
	<-doneCh
	return buf.String(), runErr
}

// ── command construction ──────────────────────────────────────────────────

func TestNewDpuCountersCmd_FlagsRegistered(t *testing.T) {
	c := newDpuCountersCmd()
	for _, name := range []string{"include-enis", "include-vnets", "eni-names", "vnet-keys", "watch", "interval"} {
		if c.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s not registered", name)
		}
	}
}

func TestNewDpuCountersCmd_PreRunImpliesIncludeEnis(t *testing.T) {
	c := newDpuCountersCmd()
	if err := c.ParseFlags([]string{"--eni-names", "eni-001"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if err := c.PreRunE(c, nil); err != nil {
		t.Fatalf("PreRunE: %v", err)
	}
	// The implicit-enable behaviour is on locals captured in the cmd's
	// closure; this test verifies the PreRunE accepted the implicit
	// combination without erroring (no positive assertion possible
	// without exposing the locals).
}

func TestNewDpuCountersCmd_PreRunRejectsBadInterval(t *testing.T) {
	c := newDpuCountersCmd()
	_ = c.ParseFlags([]string{"--watch", "--interval", "0s"})
	err := c.PreRunE(c, nil)
	if err == nil {
		t.Fatal("want error for --interval <= 0")
	}
	if !strings.Contains(err.Error(), "interval") {
		t.Errorf("unhelpful error: %v", err)
	}
}

// ── oneWatchTick ──────────────────────────────────────────────────────────

func TestOneWatchTick_HappyPath(t *testing.T) {
	fc := &fakeDpuCountersClient{
		respFn: func(_ *dashapi.DpuCountersRequest) (*dashapi.DpuCountersResponse, error) {
			return &dashapi.DpuCountersResponse{
				DeviceId: "d1", SampledAtNs: 1,
				Dpu: &dashapi.CounterBucket{PacketsIn: 42},
			}, nil
		},
	}
	// Set flagTimeout for the duration of this test (oneWatchTick uses it).
	orig := flagTimeout
	flagTimeout = 2 * time.Second
	defer func() { flagTimeout = orig }()

	out, err := pipeStdout(t, func() error {
		return oneWatchTick(context.Background(), fc, &dashapi.DpuCountersRequest{}, render.FormatTable, os.Stdout)
	})
	if err != nil {
		t.Fatalf("oneWatchTick: %v", err)
	}
	if !strings.Contains(out, "----") {
		t.Errorf("missing separator: %s", out)
	}
	if !strings.Contains(out, "DEVICE  d1") {
		t.Errorf("missing device line: %s", out)
	}
}

func TestOneWatchTick_PropagatesError(t *testing.T) {
	fc := &fakeDpuCountersClient{
		respFn: func(_ *dashapi.DpuCountersRequest) (*dashapi.DpuCountersResponse, error) {
			return nil, errors.New("boom")
		},
	}
	flagTimeout = 2 * time.Second
	err := oneWatchTick(context.Background(), fc, &dashapi.DpuCountersRequest{}, render.FormatTable, os.Stdout)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want propagated error, got %v", err)
	}
}

// ── watchDpuCounters ──────────────────────────────────────────────────────

func TestWatchDpuCounters_TicksUntilContextCancelled(t *testing.T) {
	fc := &fakeDpuCountersClient{}
	flagTimeout = 2 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Allow at least 3 ticks at 30ms interval, then cancel.
		time.Sleep(120 * time.Millisecond)
		cancel()
	}()

	out, err := pipeStdout(t, func() error {
		return watchDpuCounters(ctx, fc, &dashapi.DpuCountersRequest{}, render.FormatTable, 30*time.Millisecond, os.Stdout)
	})
	if err != nil {
		t.Fatalf("watch returned err on cancel: %v", err)
	}
	if atomic.LoadInt32(&fc.calls) < 2 {
		t.Errorf("expected at least 2 ticks, got %d", fc.calls)
	}
	if !strings.Contains(out, "DEVICE") {
		t.Errorf("watch output empty: %s", out)
	}
}

func TestWatchDpuCounters_KeepsGoingOnTransientErrors(t *testing.T) {
	// First call errors, subsequent calls succeed. Watch loop must NOT
	// exit on the first failure.
	var attempts int32
	fc := &fakeDpuCountersClient{
		respFn: func(_ *dashapi.DpuCountersRequest) (*dashapi.DpuCountersResponse, error) {
			n := atomic.AddInt32(&attempts, 1)
			if n == 1 {
				return nil, errors.New("transient")
			}
			return &dashapi.DpuCountersResponse{DeviceId: "ok", SampledAtNs: 1}, nil
		},
	}
	flagTimeout = 2 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(120 * time.Millisecond)
		cancel()
	}()
	out, err := pipeStdout(t, func() error {
		return watchDpuCounters(ctx, fc, &dashapi.DpuCountersRequest{}, render.FormatTable, 30*time.Millisecond, os.Stdout)
	})
	if err != nil {
		t.Fatalf("watch returned err: %v", err)
	}
	if !strings.Contains(out, "DEVICE  ok") {
		t.Errorf("watch should print successful frames after transient: %s", out)
	}
}
