// Package flow implements the dashd Diagnostics computation primitives
// (PE-1): TraceFlow, ExplainMatch, ExplainDrift, GetAclHitStats,
// TriggerResimulation. All operations are pure functions over a
// point-in-time snapshot of dashd's desired state plus (for stats) a
// counter store. No goroutines, no network I/O — diagnostics are
// deterministic and sub-millisecond by construction.
//
// The package boundary keeps the algorithms testable without spinning
// up a server. The transport-agnostic service wrapper lives in
// internal/service/diagnostics.go; gRPC and REST adapters live in
// internal/server/{grpc,rest}/diagnostics.go.
package flow

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/placement"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// HitStatsSource is the optional dependency used by GetAclHitStats to
// return real per-rule counters. Production wires the PD-G5
// observability.CounterStore here once it lands; the diagnostics layer
// otherwise reports rules with `hits=0 last_hit_at=<zero>`, which is
// the correct "never observed" default.
//
// PE-1 ships with a NilHitStats so the contract is stable; PD-G5
// swaps in the real implementation.
type HitStatsSource interface {
	// AclHits returns hit + byte counters for the (dpu, namespace,
	// policy, stage, priority) tuple. Returns (0, 0, 0, false) when
	// the rule was never observed.
	AclHits(dpuID, ns, policy, stage string, priority uint32) (hits int64, bytes int64, lastHitUnixNanos int64, ok bool)
}

// NilHitStats is a HitStatsSource that always reports "never observed".
// Used as the safe default until PD-G5 wires the live counter store.
type NilHitStats struct{}

// AclHits implements HitStatsSource by returning the zero result.
func (NilHitStats) AclHits(_, _, _, _ string, _ uint32) (int64, int64, int64, bool) {
	return 0, 0, 0, false
}

// Resimulator is the dispatch-side hook the Engine calls when
// TriggerResimulation needs to push a re-evaluation request to one or
// more DPUs. Production wires reconciler.ForceReconcile-style fan-out
// here; unit tests provide a recording stub.
type Resimulator interface {
	// Resimulate marks the named ENIs (or all ENIs on the named DPUs
	// when eniNames is empty) for a slow-path re-evaluation against
	// current policy. dropAllFlows mirrors ResimRequest.drop_all_flows:
	// when true, every existing flow is evicted; when false, only
	// flows whose verdict would change are re-evaluated.
	//
	// Returns the txn id so callers can correlate with downstream
	// dispatch logs.
	Resimulate(ctx context.Context, dpuIDs, eniNames []string, namespace string, dropAllFlows bool) (txnID string, err error)
}

// NopResimulator is a Resimulator that records the most recent call
// but performs no work. Used as the safe default until dispatch wires
// the real fan-out and by unit tests.
type NopResimulator struct {
	LastDpus    []string
	LastEnis    []string
	LastNS      string
	LastDropAll bool
	LastTxnID   string
	NextErr     error // when non-nil, Resimulate returns it and skips bookkeeping
	NextTxnID   string
}

// Resimulate stores the call args on the receiver and returns either
// NextErr or NextTxnID (defaulting to a synthesized value).
func (n *NopResimulator) Resimulate(_ context.Context, dpus, enis []string, ns string, dropAll bool) (string, error) {
	n.LastDpus = dpus
	n.LastEnis = enis
	n.LastNS = ns
	n.LastDropAll = dropAll
	if n.NextErr != nil {
		return "", n.NextErr
	}
	if n.NextTxnID != "" {
		n.LastTxnID = n.NextTxnID
	} else {
		n.LastTxnID = fmt.Sprintf("resim-%s", ns)
	}
	return n.LastTxnID, nil
}

// Engine is the diagnostics evaluator. It is stateless across calls;
// each request loads a fresh snapshot of DesiredSpecs from the store
// (cheap — the store backs onto file or etcd, both deliver the snapshot
// in microseconds for the manifest sizes dashd handles today).
//
// Construction is intentionally constructor-only so the dependencies
// stay explicit; the zero value is unusable.
type Engine struct {
	store store.DesiredStore
	inv   *inventory.Inventory
	hits  HitStatsSource
	resim Resimulator
}

// New returns an Engine wired to the production stack. inv may be nil
// for diagnostics that don't need fleet awareness (TraceFlow infers
// the target from the request). hits/resim default to NilHitStats /
// NopResimulator when nil.
func New(st store.DesiredStore, inv *inventory.Inventory, hits HitStatsSource, resim Resimulator) *Engine {
	if hits == nil {
		hits = NilHitStats{}
	}
	if resim == nil {
		resim = &NopResimulator{}
	}
	return &Engine{store: st, inv: inv, hits: hits, resim: resim}
}

// loadView is the shared spec loader. Errors are wrapped with the
// diagnostics package prefix so handlers can wrap them with gRPC
// codes uniformly.
func (e *Engine) loadView(ctx context.Context) (*placement.DesiredSpecs, error) {
	if e.store == nil {
		return &placement.DesiredSpecs{}, nil
	}
	specs, err := placement.LoadDesiredSpecs(ctx, e.store)
	if err != nil {
		return nil, fmt.Errorf("flow: load desired specs: %w", err)
	}
	return specs, nil
}

// sentinel errors surfaced through the service layer.
var (
	// ErrInvalidArgument signals a malformed diagnostic request.
	// Mapped to codes.InvalidArgument by the gRPC handler and HTTP
	// 400 by the REST handler.
	ErrInvalidArgument = fmt.Errorf("flow: invalid argument")

	// ErrNotFound signals that a referenced spec (ENI / policy / DPU)
	// does not exist in the current desired state.
	ErrNotFound = fmt.Errorf("flow: not found")
)

// invArgf wraps an inline-formatted error with ErrInvalidArgument.
func invArgf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidArgument, fmt.Sprintf(format, args...))
}

// notFoundf wraps an inline-formatted error with ErrNotFound.
func notFoundf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrNotFound, fmt.Sprintf(format, args...))
}

// nowTS is overrideable in tests for deterministic timestamps. The
// production default returns time.Now().UTC().
var nowTS = func() *timestamppb.Timestamp { return timestamppb.New(time.Now().UTC()) }

// tsFromUnixNanos converts a unix-nanos value (0 = "never") to a proto
// Timestamp, returning nil for the never sentinel so the JSON marshaller
// omits the field instead of emitting "1970-01-01T00:00:00Z".
func tsFromUnixNanos(unixNanos int64) *timestamppb.Timestamp {
	if unixNanos == 0 {
		return nil
	}
	return timestamppb.New(time.Unix(0, unixNanos).UTC())
}
