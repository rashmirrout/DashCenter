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

// SimulateDpuImpact is the per-DPU row of a SimulateResult (PB-2).
type SimulateDpuImpact struct {
	DpuID             string `json:"dpu_id"`
	DeltaEnis         int64  `json:"delta_enis"`
	DeltaVnetMappings int64  `json:"delta_vnet_mappings"`
	DeltaAclRules     int64  `json:"delta_acl_rules"`
	ExceedsCapacity   bool   `json:"exceeds_capacity,omitempty"`
	Reason            string `json:"reason,omitempty"`
}

// ── Cluster topology (PE-G6 / PE-G7) ─────────────────────────────────────
//
// The shapes below mirror the protojson wire format of
// dashcenter.v1.{TopologyResponse,TopologyEvent,...} as served by dashd
// REST `/v1/cluster/topology` + `/v1/cluster/topology/watch`. They are
// kept hand-rolled here (rather than pulled from gen/go) to avoid
// dragging the proto runtime into the dashctl binary — matches the same
// pattern as PutResult / StoredItem above.

// TopologyClusterNode is one dashd controller in the cluster.
type TopologyClusterNode struct {
	NodeID     string            `json:"node_id"`
	RestAddr   string            `json:"rest_addr,omitempty"`
	GrpcAddr   string            `json:"grpc_addr,omitempty"`
	AdminAddr  string            `json:"admin_addr,omitempty"`
	Version    string            `json:"version,omitempty"`
	BuildSha   string            `json:"build_sha,omitempty"`
	StartedAt  string            `json:"started_at,omitempty"`
	IsLeader   bool              `json:"is_leader,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// TopologyClusterInfo mirrors dashcenter.v1.ClusterInfo.
type TopologyClusterInfo struct {
	Healthy   bool                  `json:"healthy"`
	LeaderID  string                `json:"leader_id,omitempty"`
	NodeCount int                   `json:"node_count"`
	Nodes     []TopologyClusterNode `json:"nodes"`
}

// TopologyEni mirrors dashcenter.v1.EniTopInfo.
type TopologyEni struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
	VnetName   string `json:"vnet_name,omitempty"`
	MacAddress string `json:"mac_address,omitempty"`
	AdminState string `json:"admin_state,omitempty"`
}

// TopologyDpu mirrors dashcenter.v1.DpuTopInfo.
type TopologyDpu struct {
	ID       string        `json:"id"`
	Slot     int32         `json:"slot,omitempty"`
	State    string        `json:"state"`
	LastSeen string        `json:"last_seen,omitempty"`
	EniCount int           `json:"eni_count"`
	Cordoned bool          `json:"cordoned,omitempty"`
	Enis     []TopologyEni `json:"enis,omitempty"`
}

// TopologyAppliance mirrors dashcenter.v1.ApplianceInfo.
type TopologyAppliance struct {
	ID   string        `json:"id"`
	Zone string        `json:"zone,omitempty"`
	Tier string        `json:"tier,omitempty"`
	Dpus []TopologyDpu `json:"dpus"`
}

// TopologyZone mirrors dashcenter.v1.ZoneInfo.
type TopologyZone struct {
	Zone           string `json:"zone"`
	ApplianceCount int    `json:"appliance_count"`
	DpuCount       int    `json:"dpu_count"`
	EniCount       int    `json:"eni_count"`
}

// TopologySummary mirrors dashcenter.v1.TopologySummary.
type TopologySummary struct {
	TotalNodes      int `json:"total_nodes"`
	TotalAppliances int `json:"total_appliances"`
	TotalDpus       int `json:"total_dpus"`
	TotalEnis       int `json:"total_enis"`
	HealthyDpus     int `json:"healthy_dpus"`
	DegradedDpus    int `json:"degraded_dpus"`
	OfflineDpus     int `json:"offline_dpus"`
	CordonedDpus    int `json:"cordoned_dpus"`
}

// TopologyNamespaceObjectCounts mirrors dashcenter.v1.NamespaceObjectCounts.
type TopologyNamespaceObjectCounts struct {
	Vnets          int `json:"vnets,omitempty"`
	Enis           int `json:"enis,omitempty"`
	VnetMappings   int `json:"vnet_mappings,omitempty"`
	AclPolicies    int `json:"acl_policies,omitempty"`
	RoutePolicies  int `json:"route_policies,omitempty"`
	HaSets         int `json:"ha_sets,omitempty"`
	ServiceTunnels int `json:"service_tunnels,omitempty"`
}

// TopologySnapshot is the unary GetTopology reply (also the body of the
// first SSE event with kind=KIND_SNAPSHOT).
type TopologySnapshot struct {
	ComputedAt  string                                   `json:"computed_at,omitempty"`
	Cluster     *TopologyClusterInfo                     `json:"cluster,omitempty"`
	Appliances  []TopologyAppliance                      `json:"appliances,omitempty"`
	Zones       []TopologyZone                           `json:"zones,omitempty"`
	Summary     *TopologySummary                         `json:"summary,omitempty"`
	Objects     map[string]TopologyNamespaceObjectCounts `json:"objects,omitempty"`
}

// TopologyNotice mirrors dashcenter.v1.Notice (payload for KIND_KEEPALIVE
// / KIND_DROPPED / KIND_RATE_LIMITED / KIND_RESYNC events).
type TopologyNotice struct {
	DroppedCount    uint64 `json:"dropped_count,omitempty"`
	SuppressedCount uint64 `json:"suppressed_count,omitempty"`
	Message         string `json:"message,omitempty"`
	CurrentEventID  uint64 `json:"current_event_id,omitempty"`
}

// TopologyEvent mirrors dashcenter.v1.TopologyEvent. One frame per SSE
// event. Kind values are the proto enum names with the KIND_ prefix:
//
//	KIND_SNAPSHOT      KIND_PEER_ADDED   KIND_PEER_REMOVED   KIND_PEER_UPDATED
//	KIND_LEADER_CHANGED KIND_DPU_ADDED   KIND_DPU_REMOVED    KIND_DPU_STATE
//	KIND_KEEPALIVE     KIND_DROPPED      KIND_RATE_LIMITED   KIND_RESYNC
type TopologyEvent struct {
	Kind        string               `json:"kind"`
	Ts          string               `json:"ts,omitempty"`
	EventID     uint64               `json:"event_id,omitempty"`
	Snapshot    *TopologySnapshot    `json:"snapshot,omitempty"`
	Peer        *TopologyClusterNode `json:"peer,omitempty"`
	Dpu         *TopologyDpu         `json:"dpu,omitempty"`
	Notice      *TopologyNotice      `json:"notice,omitempty"`
	OldLeaderID string               `json:"old_leader_id,omitempty"`
	NewLeaderID string               `json:"new_leader_id,omitempty"`
}

// TopologyWatchOptions narrows a StreamTopology call.
type TopologyWatchOptions struct {
	IncludeEnis     bool
	LastEventID     uint64 // resume cursor
	OnEvent         func(TopologyEvent) error // return non-nil to stop
}

// SimulateResult is the dashd reply to POST /v1/simulate (PB-2).
type SimulateResult struct {
	WouldSucceed     bool                 `json:"would_succeed"`
	ValidationErrors []string             `json:"validation_errors,omitempty"`
	PerDpuImpact     []*SimulateDpuImpact `json:"per_dpu_impact,omitempty"`
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

	// Simulate runs a dry-run admission check against dashd. The ops
	// are JSON-encoded service.SimulateOp records; the server returns
	// a SimulateResult (would_succeed, validation_errors[], per_dpu_impact[]).
	// PB-2 supports eni|vnet_mapping|acl_policy.
	Simulate(ctx context.Context, opsJSON []byte) (*SimulateResult, error)

	// Admin views.
	AdminDrift(ctx context.Context, dpuID string) ([]DriftItem, error)
	AdminEniPlacement(ctx context.Context) ([]EniPlacementRow, error)

	// Cluster topology (PE-G6 / PE-G7).
	GetTopology(ctx context.Context, includeEnis bool) (*TopologySnapshot, error)
	// StreamTopology opens an SSE stream and invokes opts.OnEvent for
	// every received frame until the context is cancelled, the server
	// closes the stream, or OnEvent returns a non-nil sentinel error.
	StreamTopology(ctx context.Context, opts TopologyWatchOptions) error
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
