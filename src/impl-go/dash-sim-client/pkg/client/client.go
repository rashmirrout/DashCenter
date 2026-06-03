// Package client is the thin, transport-only Go SDK for talking to the
// dashsim.v1.DashSim service. It exists so that operators, tests, and other
// services can drive a simulator instance without re-implementing gRPC
// boilerplate.
//
// Design rules:
//
//   - This package depends ONLY on the generated proto stubs at
//     github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashsim/v1. It MUST
//     NOT import dash-sim's internal packages.
//   - It owns a *grpc.ClientConn for the caller; callers must Close().
//   - Insecure-by-default credentials so smoke tests "just work" on
//     localhost. Production callers can override via WithDialOptions.
package client

import (
	"context"
	"errors"
	"fmt"

	dashsimv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashsim/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client is a connected SDK handle. Goroutine-safe.
type Client struct {
	conn *grpc.ClientConn
	api  dashsimv1.DashSimClient
}

// Option mutates Dial behavior.
type Option func(*options)

type options struct {
	dialOpts []grpc.DialOption
}

// WithDialOptions appends raw grpc.DialOption values (e.g. for TLS or
// interceptors). Replaces nothing — your opts are appended after the default
// insecure credentials.
func WithDialOptions(opts ...grpc.DialOption) Option {
	return func(o *options) { o.dialOpts = append(o.dialOpts, opts...) }
}

// Dial connects to addr. addr is anything grpc.NewClient accepts, e.g.
// "localhost:50051" or "dns:///dash-sim.svc:50051".
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
	return &Client{
		conn: conn,
		api:  dashsimv1.NewDashSimClient(conn),
	}, nil
}

// Close releases the underlying gRPC connection.
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Raw exposes the generated client for use cases not covered by the typed
// helpers in this package.
func (c *Client) Raw() dashsimv1.DashSimClient { return c.api }

// -----------------------------------------------------------------------------
// VNETs
// -----------------------------------------------------------------------------

func (c *Client) CreateVnet(ctx context.Context, v *dashsimv1.Vnet) (*dashsimv1.Ack, error) {
	return c.api.CreateVnet(ctx, v)
}

func (c *Client) DeleteVnet(ctx context.Context, id string) (*dashsimv1.Ack, error) {
	return c.api.DeleteVnet(ctx, &dashsimv1.KeyRequest{Id: id})
}

func (c *Client) GetVnet(ctx context.Context, id string) (*dashsimv1.Vnet, error) {
	resp, err := c.api.GetVnet(ctx, &dashsimv1.KeyRequest{Id: id})
	if err != nil {
		return nil, err
	}
	return resp.GetItem(), nil
}

func (c *Client) ListVnets(ctx context.Context) ([]*dashsimv1.Vnet, error) {
	stream, err := c.api.ListVnets(ctx, &dashsimv1.ListRequest{})
	if err != nil {
		return nil, err
	}
	var out []*dashsimv1.Vnet
	for {
		msg, err := stream.Recv()
		if err != nil {
			if errors.Is(err, errEOF) || err.Error() == "EOF" {
				break
			}
			// io.EOF is the canonical end-of-stream marker for client streams.
			if isEOF(err) {
				break
			}
			return nil, err
		}
		out = append(out, msg.GetItem())
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// ENIs
// -----------------------------------------------------------------------------

func (c *Client) CreateEni(ctx context.Context, e *dashsimv1.Eni) (*dashsimv1.Ack, error) {
	return c.api.CreateEni(ctx, e)
}

func (c *Client) UpdateEni(ctx context.Context, e *dashsimv1.Eni) (*dashsimv1.Ack, error) {
	return c.api.UpdateEni(ctx, e)
}

func (c *Client) DeleteEni(ctx context.Context, id string) (*dashsimv1.Ack, error) {
	return c.api.DeleteEni(ctx, &dashsimv1.KeyRequest{Id: id})
}

func (c *Client) GetEni(ctx context.Context, id string) (*dashsimv1.Eni, error) {
	resp, err := c.api.GetEni(ctx, &dashsimv1.KeyRequest{Id: id})
	if err != nil {
		return nil, err
	}
	return resp.GetItem(), nil
}

func (c *Client) ListEnis(ctx context.Context) ([]*dashsimv1.Eni, error) {
	stream, err := c.api.ListEnis(ctx, &dashsimv1.ListRequest{})
	if err != nil {
		return nil, err
	}
	var out []*dashsimv1.Eni
	for {
		msg, err := stream.Recv()
		if err != nil {
			if isEOF(err) {
				break
			}
			return nil, err
		}
		out = append(out, msg.GetItem())
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// VNET mappings
// -----------------------------------------------------------------------------

func (c *Client) AddVnetMapping(ctx context.Context, m *dashsimv1.VnetMapping) (*dashsimv1.Ack, error) {
	return c.api.AddVnetMapping(ctx, m)
}

func (c *Client) DeleteVnetMapping(ctx context.Context, id string) (*dashsimv1.Ack, error) {
	return c.api.DeleteVnetMapping(ctx, &dashsimv1.KeyRequest{Id: id})
}

func (c *Client) ListVnetMappings(ctx context.Context) ([]*dashsimv1.VnetMapping, error) {
	stream, err := c.api.ListVnetMappings(ctx, &dashsimv1.ListRequest{})
	if err != nil {
		return nil, err
	}
	var out []*dashsimv1.VnetMapping
	for {
		msg, err := stream.Recv()
		if err != nil {
			if isEOF(err) {
				break
			}
			return nil, err
		}
		out = append(out, msg.GetItem())
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// Routes
// -----------------------------------------------------------------------------

func (c *Client) AddRoute(ctx context.Context, r *dashsimv1.Route) (*dashsimv1.Ack, error) {
	return c.api.AddRoute(ctx, r)
}

func (c *Client) DeleteRoute(ctx context.Context, id string) (*dashsimv1.Ack, error) {
	return c.api.DeleteRoute(ctx, &dashsimv1.KeyRequest{Id: id})
}

func (c *Client) ListRoutes(ctx context.Context) ([]*dashsimv1.Route, error) {
	stream, err := c.api.ListRoutes(ctx, &dashsimv1.ListRequest{})
	if err != nil {
		return nil, err
	}
	var out []*dashsimv1.Route
	for {
		msg, err := stream.Recv()
		if err != nil {
			if isEOF(err) {
				break
			}
			return nil, err
		}
		out = append(out, msg.GetItem())
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// ACLs
// -----------------------------------------------------------------------------

func (c *Client) AddAclGroup(ctx context.Context, g *dashsimv1.AclGroup) (*dashsimv1.Ack, error) {
	return c.api.AddAclGroup(ctx, g)
}

func (c *Client) DeleteAclGroup(ctx context.Context, id string) (*dashsimv1.Ack, error) {
	return c.api.DeleteAclGroup(ctx, &dashsimv1.KeyRequest{Id: id})
}

func (c *Client) ListAclGroups(ctx context.Context) ([]*dashsimv1.AclGroup, error) {
	stream, err := c.api.ListAclGroups(ctx, &dashsimv1.ListRequest{})
	if err != nil {
		return nil, err
	}
	var out []*dashsimv1.AclGroup
	for {
		msg, err := stream.Recv()
		if err != nil {
			if isEOF(err) {
				break
			}
			return nil, err
		}
		out = append(out, msg.GetItem())
	}
	return out, nil
}

func (c *Client) AddAclRule(ctx context.Context, r *dashsimv1.AclRule) (*dashsimv1.Ack, error) {
	return c.api.AddAclRule(ctx, r)
}

func (c *Client) DeleteAclRule(ctx context.Context, id string) (*dashsimv1.Ack, error) {
	return c.api.DeleteAclRule(ctx, &dashsimv1.KeyRequest{Id: id})
}

func (c *Client) ListAclRules(ctx context.Context) ([]*dashsimv1.AclRule, error) {
	stream, err := c.api.ListAclRules(ctx, &dashsimv1.ListRequest{})
	if err != nil {
		return nil, err
	}
	var out []*dashsimv1.AclRule
	for {
		msg, err := stream.Recv()
		if err != nil {
			if isEOF(err) {
				break
			}
			return nil, err
		}
		out = append(out, msg.GetItem())
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// Subscribe + counters
// -----------------------------------------------------------------------------

// Subscribe opens an Event stream. The returned channel is closed when the
// server closes the stream or ctx is cancelled. Errors (other than EOF) are
// returned via the error channel.
func (c *Client) Subscribe(ctx context.Context, kinds []dashsimv1.ObjectKind, snapshotFirst bool) (<-chan *dashsimv1.Event, <-chan error, error) {
	stream, err := c.api.Subscribe(ctx, &dashsimv1.SubscribeRequest{
		Kinds:         kinds,
		SnapshotFirst: snapshotFirst,
	})
	if err != nil {
		return nil, nil, err
	}
	evCh := make(chan *dashsimv1.Event, 64)
	errCh := make(chan error, 1)
	go func() {
		defer close(evCh)
		defer close(errCh)
		for {
			msg, err := stream.Recv()
			if err != nil {
				if isEOF(err) {
					return
				}
				errCh <- err
				return
			}
			select {
			case evCh <- msg:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}
	}()
	return evCh, errCh, nil
}

// GetCounters fetches the latest counter snapshot for the given object id.
func (c *Client) GetCounters(ctx context.Context, id string) (map[string]int64, error) {
	resp, err := c.api.GetCounters(ctx, &dashsimv1.KeyRequest{Id: id})
	if err != nil {
		return nil, err
	}
	return resp.GetCounters(), nil
}
