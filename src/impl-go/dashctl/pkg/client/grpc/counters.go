// Package grpcclient is the dashctl-side gRPC backend.
//
// Scope (PE-3c): currently only the counter streaming surface
// (ObservabilityService.GetCounters). The full Client interface
// (Health, ServerInfo, Put, Get, List, etc.) remains REST-only; a
// future dashctl-gRPC slice will fill that in. This package exists
// today so operators can opt into the gRPC stream for `dashctl
// counters --follow --backend grpc` without dragging in a full
// gRPC-vs-REST backend choice for every other command.
//
// The CountersClient is constructed lazily by the counters command
// when the operator selects backend=grpc; it dials, runs the stream,
// and tears down on ctx cancel. No global Dial registry change.

package grpcclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
)

// CountersClient is the dashctl-side gRPC wrapper for
// ObservabilityService.GetCounters.
type CountersClient struct {
	conn   *grpc.ClientConn
	client dashcenterv1.ObservabilityServiceClient
}

// DialOptions narrows NewCountersClient. tls=nil ⇒ insecure transport
// (matches dashd's plaintext default + DASHCTL_INSECURE escape hatch).
type DialOptions struct {
	Endpoint string                              // host:port, required
	TLS      credentials.TransportCredentials    // nil = insecure
	Token    string                              // optional bearer
	DialTimeout time.Duration                    // default 5s
}

// NewCountersClient dials the endpoint and returns a ready client.
// Caller MUST call Close().
func NewCountersClient(ctx context.Context, opts DialOptions) (*CountersClient, error) {
	if opts.Endpoint == "" {
		return nil, errors.New("grpcclient: endpoint required")
	}
	if opts.DialTimeout == 0 {
		opts.DialTimeout = 5 * time.Second
	}
	creds := opts.TLS
	if creds == nil {
		creds = insecure.NewCredentials()
	}
	dialCtx, cancel := context.WithTimeout(ctx, opts.DialTimeout)
	defer cancel()
	// gRPC 1.65: NewClient is the preferred constructor (replaces DialContext).
	conn, err := grpc.NewClient(opts.Endpoint, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("grpcclient: dial %s: %w", opts.Endpoint, err)
	}
	_ = dialCtx // dial is async with NewClient; we don't block here
	return &CountersClient{conn: conn, client: dashcenterv1.NewObservabilityServiceClient(conn)}, nil
}

// Close releases the underlying gRPC connection.
func (c *CountersClient) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// StreamCounters opens the GetCounters server-stream and invokes
// opts.OnEvent for every CounterEvent frame until ctx cancel / EOF /
// OnEvent returns a non-nil sentinel.
func (c *CountersClient) StreamCounters(ctx context.Context, opts client.CountersWatchOptions) error {
	if opts.OnEvent == nil {
		return errors.New("grpcclient: StreamCounters requires OnEvent")
	}
	req := &dashcenterv1.CounterRequest{
		DpuIds:             opts.DpuIDs,
		Follow:             true,
		ResumeAfterEventId: opts.LastEventID,
	}
	stream, err := c.client.GetCounters(ctx, req)
	if err != nil {
		return fmt.Errorf("grpcclient: open stream: %w", err)
	}
	for {
		ev, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("grpcclient: stream recv: %w", err)
		}
		ce, cerr := protoToCounterEvent(ev)
		if cerr != nil {
			return fmt.Errorf("grpcclient: decode event: %w", cerr)
		}
		if cberr := opts.OnEvent(ce); cberr != nil {
			return cberr
		}
	}
}

// GetCountersSnapshot calls GetCounters with follow=false and accumulates
// the SNAPSHOT frames into a CountersSnapshot envelope.
func (c *CountersClient) GetCountersSnapshot(ctx context.Context, dpuIDs []string) (*client.CountersSnapshot, error) {
	req := &dashcenterv1.CounterRequest{
		DpuIds: dpuIDs,
		Follow: false,
	}
	stream, err := c.client.GetCounters(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("grpcclient: open snapshot: %w", err)
	}
	out := &client.CountersSnapshot{}
	for {
		ev, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return nil, fmt.Errorf("grpcclient: snapshot recv: %w", err)
		}
		if ev.GetKind() != dashcenterv1.CounterEvent_KIND_SNAPSHOT {
			continue // ignore non-snapshot frames in one-shot mode
		}
		ce, cerr := protoToCounterEvent(ev)
		if cerr != nil {
			return nil, fmt.Errorf("grpcclient: decode snapshot: %w", cerr)
		}
		if ce.Report != nil {
			out.Reports = append(out.Reports, ce.Report)
		}
	}
}

// protoToCounterEvent marshals the proto frame through protojson and
// re-decodes into the dashctl-side JSON struct. This keeps the wire
// shape identical to the SSE path so cmd/counters.go can render either
// backend identically.
func protoToCounterEvent(ev *dashcenterv1.CounterEvent) (client.CounterEvent, error) {
	js, err := protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: false}.Marshal(ev)
	if err != nil {
		return client.CounterEvent{}, err
	}
	var ce client.CounterEvent
	if err := jsonUnmarshalCounterEvent(js, &ce); err != nil {
		return client.CounterEvent{}, err
	}
	return ce, nil
}
