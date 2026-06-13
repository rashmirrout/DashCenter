// Package dpuclient provides the southbound client abstraction used by
// dashd to talk to DPU agents (or dash-sim) via the dashapi.v1.DashApi
// gRPC service.
//
// The DpuClient interface is the single seam between the daemon's
// orchestration code (dispatch workers + subscribe pumps) and the
// underlying transport. The real implementation wraps grpc.ClientConn
// and dashapi.DashApiClient; the test mock implements the same surface
// without needing a process or socket.
//
// This separation is what allows 100% unit-test coverage of the pump
// and worker reconcile loops: tests inject a MockClient that records
// every Apply/Delete and replays canned Subscribe events.
package dpuclient

import (
	"context"
	"fmt"

	dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// DpuClient is the southbound abstraction for one DPU connection.
//
// The interface intentionally exposes the small subset of dashapi
// surface that dashd consumes: Apply + Delete (write path) and
// Subscribe (observed-state pump). Get/List/Counters/SimulatePacket
// are reachable through the underlying RawClient escape hatch when
// debug tooling needs them, but reconcile code MUST go through the
// typed methods to remain mockable.
type DpuClient interface {
	// Apply pushes a single object (kind+key+payload) to the DPU.
	// Errors include transport failures and Ack.error from the server.
	Apply(ctx context.Context, obj *dashapiv1.Object) error

	// Delete removes a single object by kind+key on the DPU.
	// A NotFound Ack.error is reported as nil (idempotent delete).
	Delete(ctx context.Context, kind dashapiv1.ObjectKind, key []string) error

	// Subscribe opens a server-streaming Subscribe RPC and returns the
	// stream. The caller drains it until ctx is cancelled or the stream
	// terminates with error. snapshotFirst=true requests a SNAPSHOT
	// prelude of every existing object before live events.
	Subscribe(ctx context.Context, snapshotFirst bool) (grpc.ServerStreamingClient[dashapiv1.Event], error)

	// GetDpuCounters fetches the typed per-DPU + per-ENI + per-VNET
	// rollup (PE-3a wire contract). nil req is treated as the empty
	// request — the DPU-wide bucket is always returned regardless.
	// Wraps transport + Ack errors in a single error value.
	GetDpuCounters(ctx context.Context, req *dashapiv1.DpuCountersRequest) (*dashapiv1.DpuCountersResponse, error)

	// Close releases the underlying transport. Idempotent.
	Close() error
}

// ClientFactory builds a DpuClient for the given endpoint. It is the
// injection seam used by dispatch.Manager and subscribe.PumpSet so
// tests can substitute MockFactory.
type ClientFactory func(endpoint string) (DpuClient, error)

// realClient is the production implementation backed by gRPC.
type realClient struct {
	cc  *grpc.ClientConn
	api dashapiv1.DashApiClient
}

// New dials endpoint with insecure credentials (Phase 1 dev mode) and
// returns a DpuClient. The connection is lazy — gRPC defers the actual
// network handshake until the first RPC. Callers MUST defer Close().
//
// TODO(security): swap to TLS + SPIFFE when Phase 2 wires mTLS.
func New(endpoint string) (DpuClient, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("dpuclient: endpoint is empty")
	}
	cc, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dpuclient: dial %s: %w", endpoint, err)
	}
	return &realClient{
		cc:  cc,
		api: dashapiv1.NewDashApiClient(cc),
	}, nil
}

// DefaultFactory is the production ClientFactory wired into main.go.
var DefaultFactory ClientFactory = New

// Apply implements DpuClient.
func (c *realClient) Apply(ctx context.Context, obj *dashapiv1.Object) error {
	if obj == nil {
		return fmt.Errorf("dpuclient: Apply nil object")
	}
	ack, err := c.api.Apply(ctx, &dashapiv1.ApplyRequest{Object: obj})
	if err != nil {
		return fmt.Errorf("dpuclient: Apply rpc: %w", err)
	}
	if ack != nil && ack.GetError() != "" {
		return fmt.Errorf("dpuclient: Apply ack: %s", ack.GetError())
	}
	return nil
}

// Delete implements DpuClient.
func (c *realClient) Delete(ctx context.Context, kind dashapiv1.ObjectKind, key []string) error {
	ack, err := c.api.Delete(ctx, &dashapiv1.DeleteRequest{
		Kind: kind,
		Key:  append([]string(nil), key...),
	})
	if err != nil {
		return fmt.Errorf("dpuclient: Delete rpc: %w", err)
	}
	if ack != nil && ack.GetError() != "" {
		// NotFound is treated as success — desired-state convergence is
		// idempotent and the object is already gone.
		if isNotFound(ack.GetError()) {
			return nil
		}
		return fmt.Errorf("dpuclient: Delete ack: %s", ack.GetError())
	}
	return nil
}

// Subscribe implements DpuClient.
func (c *realClient) Subscribe(ctx context.Context, snapshotFirst bool) (grpc.ServerStreamingClient[dashapiv1.Event], error) {
	req := &dashapiv1.SubscribeRequest{
		SnapshotFirst: snapshotFirst,
	}
	stream, err := c.api.Subscribe(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("dpuclient: Subscribe rpc: %w", err)
	}
	return stream, nil
}

// GetDpuCounters implements DpuClient.
func (c *realClient) GetDpuCounters(ctx context.Context, req *dashapiv1.DpuCountersRequest) (*dashapiv1.DpuCountersResponse, error) {
	if req == nil {
		req = &dashapiv1.DpuCountersRequest{}
	}
	resp, err := c.api.GetDpuCounters(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("dpuclient: GetDpuCounters rpc: %w", err)
	}
	return resp, nil
}

// Close implements DpuClient. Idempotent — repeated calls return nil.
func (c *realClient) Close() error {
	if c == nil || c.cc == nil {
		return nil
	}
	err := c.cc.Close()
	c.cc = nil
	c.api = nil
	if err != nil {
		return fmt.Errorf("dpuclient: close: %w", err)
	}
	return nil
}

// isNotFound is a loose match — dash-sim and real DPUs both surface
// NotFound as a string containing "not found" / "NotFound".
func isNotFound(msg string) bool {
	const (
		l = "not found"
		u = "NotFound"
	)
	return containsFold(msg, l) || containsFold(msg, u)
}

// containsFold is a tiny ASCII case-insensitive substring check; we
// avoid pulling in strings.EqualFold loops on hot paths.
func containsFold(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(s) < len(sub) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if eqFold(s[i:i+len(sub)], sub) {
			return true
		}
	}
	return false
}

func eqFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}