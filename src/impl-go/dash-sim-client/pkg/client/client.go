// Package client is the thin, transport-only Go SDK for the dashapi.v1.DashApi
// gRPC service. It exists so that operators, tests, and other services can
// drive any DashApi server (sim or real-hardware adapter) without rebuilding
// gRPC plumbing.
//
// Design rules:
//
//   - Depends ONLY on the generated proto stubs at
//     github.com/rashmirrout/DashCenter/src/impl-go/gen/go/... — no
//     simulator-internal imports.
//   - Insecure-by-default credentials so smoke tests "just work" on
//     localhost. Production callers override via WithDialOptions.
//   - Methods are 1:1 with the gRPC service (Apply/Get/Delete/List/Subscribe/
//     GetCounters) plus convenience helpers that take a kind name + typed
//     payload and pack it into an *Object for you.
package client

import (
	"context"
	"errors"
	"fmt"
	"io"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

// Client wraps a *grpc.ClientConn + dashapi.DashApiClient.
type Client struct {
	conn *grpc.ClientConn
	api  dashapi.DashApiClient
}

// Option mutates Dial behavior.
type Option func(*options)

type options struct {
	dialOpts []grpc.DialOption
}

// WithDialOptions appends raw grpc.DialOption values.
func WithDialOptions(opts ...grpc.DialOption) Option {
	return func(o *options) { o.dialOpts = append(o.dialOpts, opts...) }
}

// Dial connects to addr (e.g. "localhost:50051").
func Dial(addr string, opts ...Option) (*Client, error) {
	o := &options{
		dialOpts: []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
	}
	for _, fn := range opts {
		fn(o)
	}
	conn, err := grpc.NewClient(addr, o.dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return &Client{conn: conn, api: dashapi.NewDashApiClient(conn)}, nil
}

// Close releases the underlying gRPC connection.
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Raw exposes the generated gRPC client for ad-hoc use cases.
func (c *Client) Raw() dashapi.DashApiClient { return c.api }

// Apply creates or replaces obj.
func (c *Client) Apply(ctx context.Context, obj *dashapi.Object) (*dashapi.Ack, error) {
	return c.api.Apply(ctx, &dashapi.ApplyRequest{Object: obj})
}

// Delete removes (kind, key).
func (c *Client) Delete(ctx context.Context, kind dashapi.ObjectKind, key []string) (*dashapi.Ack, error) {
	return c.api.Delete(ctx, &dashapi.DeleteRequest{Kind: kind, Key: key})
}

// Get reads (kind, key).
func (c *Client) Get(ctx context.Context, kind dashapi.ObjectKind, key []string) (*dashapi.Object, error) {
	resp, err := c.api.Get(ctx, &dashapi.GetRequest{Kind: kind, Key: key})
	if err != nil {
		return nil, err
	}
	return resp.GetObject(), nil
}

// List streams every object of kind (optionally filtered by joined-key prefix).
func (c *Client) List(ctx context.Context, kind dashapi.ObjectKind, keyPrefix string) ([]*dashapi.Object, error) {
	stream, err := c.api.List(ctx, &dashapi.ListRequest{Kind: kind, KeyPrefix: keyPrefix})
	if err != nil {
		return nil, err
	}
	var out []*dashapi.Object
	for {
		item, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		out = append(out, item.GetObject())
	}
	return out, nil
}

// Subscribe opens an Event stream. The returned channel is closed when the
// server closes the stream or ctx is cancelled.
func (c *Client) Subscribe(ctx context.Context, kinds []dashapi.ObjectKind, snapshotFirst bool) (<-chan *dashapi.Event, <-chan error, error) {
	stream, err := c.api.Subscribe(ctx, &dashapi.SubscribeRequest{
		Kinds: kinds, SnapshotFirst: snapshotFirst,
	})
	if err != nil {
		return nil, nil, err
	}
	evCh := make(chan *dashapi.Event, 64)
	errCh := make(chan error, 1)
	go func() {
		defer close(evCh)
		defer close(errCh)
		for {
			ev, err := stream.Recv()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					errCh <- err
				}
				return
			}
			select {
			case evCh <- ev:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}
	}()
	return evCh, errCh, nil
}

// GetCounters fetches the counter snapshot for (kind, key).
func (c *Client) GetCounters(ctx context.Context, kind dashapi.ObjectKind, key []string) (map[string]int64, error) {
	resp, err := c.api.GetCounters(ctx, &dashapi.CountersRequest{Kind: kind, Key: key})
	if err != nil {
		return nil, err
	}
	return resp.GetCounters(), nil
}

// GetDpuCounters fetches the typed per-DPU rollup added in PE-3a (PE-G8).
// The response always contains the DPU-wide bucket; per-ENI and per-VNET
// sections are populated only when the respective IncludeEnis / IncludeVnets
// flag is set on req. Returning the full *DpuCountersResponse (rather than
// flattening to a map like GetCounters) gives the CLI render layer full
// access to scope_key + bucket pairs and to the device_id / sampled_at_ns
// metadata required by --watch mode.
func (c *Client) GetDpuCounters(ctx context.Context, req *dashapi.DpuCountersRequest) (*dashapi.DpuCountersResponse, error) {
	if req == nil {
		req = &dashapi.DpuCountersRequest{}
	}
	return c.api.GetDpuCounters(ctx, req)
}

// Compile-time check.
var _ proto.Message = (*dashapi.Object)(nil)
