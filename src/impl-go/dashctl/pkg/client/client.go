// Package client is dashctl's transport-agnostic SDK. Phase 1 ships the
// REST backend under pkg/client/rest; Phase 2 will add pkg/client/grpc.
// Subcommand code never sees a concrete backend — it depends only on the
// Client interface defined here.
package client

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/config"
)

// PutResult is the typed shape of a dashd Ack on Put*.
type PutResult struct {
	Accepted   bool   `json:"accepted"`
	Generation uint64 `json:"generation"`
	TxnID      string `json:"txn_id,omitempty"`
}

// StoredItem mirrors dashd's REST GET / LIST shape.
type StoredItem struct {
	Kind       string          `json:"kind"`
	Name       string          `json:"name"`
	Namespace  string          `json:"namespace"`
	Generation uint64          `json:"generation"`
	Spec       json.RawMessage `json:"spec"`
}

// ListOptions tunes List queries.
type ListOptions struct {
	Selector string // label selector — applied client-side in Phase 1
	Limit    int    // 0 = unlimited
}

// DeleteOptions tunes Delete.
type DeleteOptions struct {
	IgnoreNotFound     bool
	ExpectedGeneration uint64 // 0 = no CAS
}

// DpuInput is the inventory write payload.
type DpuInput struct {
	ID       string            `json:"id"`
	Endpoint string            `json:"endpoint"`
	Labels   map[string]string `json:"labels,omitempty"`
}

// DpuStatus is the runtime view of one DPU (admin + observability sources).
type DpuStatus struct {
	ID       string            `json:"id"`
	Endpoint string            `json:"endpoint,omitempty"`
	State    string            `json:"state"`
	LastSeen string            `json:"last_seen,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
}

// DriftItem is one declared-vs-observed delta on a DPU.
type DriftItem struct {
	DpuID  string `json:"dpu_id"`
	Op     string `json:"op"` // "add" | "update" | "remove"
	Kind   string `json:"kind"`
	Key    string `json:"key,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// HealthReport is the admin /health shape.
type HealthReport struct {
	Status string      `json:"status"`
	Leader bool        `json:"leader"`
	Dpus   []DpuStatus `json:"dpus"`
}

// EniPlacementRow is the admin /eni-placement row.
type EniPlacementRow struct {
	Namespace string `json:"namespace,omitempty"`
	EniName   string `json:"eni_name"`
	VnetName  string `json:"vnet_name,omitempty"`
	DpuID     string `json:"dpu_id"`
	Observed  bool   `json:"observed"`
}

// ServerInfo is the (best-effort) server-side version + health used by
// `dashctl version`.
type ServerInfo struct {
	Version string
	Leader  bool
	OK      bool
}

// Client is the contract every backend (REST today; gRPC in Phase 2) implements.
type Client interface {
	Close() error

	// Identity / liveness.
	Health(ctx context.Context) (HealthReport, error)
	ServerInfo(ctx context.Context) (ServerInfo, error)

	// Inventory.
	PutInventory(ctx context.Context, dpus []DpuInput) error
	GetInventory(ctx context.Context) ([]DpuStatus, error)

	// Per-kind Put (spec is a JSON object — Phase 1 is proto-free).
	Put(ctx context.Context, ns, kind, name string, specJSON []byte) (*PutResult, error)

	// CRUD on any kind.
	Get(ctx context.Context, ns, kind, name string) (*StoredItem, error)
	List(ctx context.Context, ns, kind string, opts ListOptions) ([]*StoredItem, error)
	Delete(ctx context.Context, ns, kind, name string, opts DeleteOptions) error

	// Reconcile.
	Reconcile(ctx context.Context, dpuIDs []string) error

	// Admin views.
	AdminDrift(ctx context.Context, dpuID string) ([]DriftItem, error)
	AdminEniPlacement(ctx context.Context) ([]EniPlacementRow, error)
}

// Factory builds a Client from a resolved config. Phase 1 only registers
// the REST factory; Phase 2 will register the gRPC factory.
type Factory func(ctx context.Context, rc *config.ResolvedConfig) (Client, error)

var factories = map[config.Transport]Factory{}

// Register is called by backend packages in init() (or by tests) to plug
// themselves into Dial.
func Register(t config.Transport, f Factory) {
	factories[t] = f
}

// Dial selects the backend per rc.Transport and returns a Client.
func Dial(ctx context.Context, rc *config.ResolvedConfig) (Client, error) {
	if rc == nil {
		return nil, errInvalidArgument("client: nil ResolvedConfig")
	}
	f, ok := factories[rc.Transport]
	if !ok {
		return nil, errUnimplemented("client: transport " + string(rc.Transport) + " not registered")
	}
	return f(ctx, rc)
}

// Helpers used in tests to provide synthetic timeouts without dragging the
// errors package into the public client API.
func newContext(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, d)
}

// --- internal helpers — kept here to avoid a cyclic dep with internal/errors ---

type clientError struct {
	code int
	msg  string
}

func (e *clientError) Error() string { return e.msg }
func (e *clientError) Code() int     { return e.code }

func errInvalidArgument(msg string) error { return &clientError{code: 5, msg: msg} }
func errUnimplemented(msg string) error   { return &clientError{code: 9, msg: msg} }
