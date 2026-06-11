// HaService surface (PC-G1..G3). Keeps internal/service free of
// proto streaming concerns — the gRPC handler in internal/server/grpc
// adapts these to the streaming wire RPCs.
package service

import (
	"context"
	"errors"
	"fmt"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/ha/orchestrator"
)

// HaService is the transport-agnostic HA surface. dashd's gRPC and REST
// adapters delegate here; tests can drive it directly without spinning
// up a server.
type HaService interface {
	// GetHaSetState returns the live aggregate view of one HA set.
	GetHaSetState(ctx context.Context, ns, name string) (*HaSetView, error)

	// GetHaScopeState returns the per-scope view (one per role-holder).
	// vdpuID/scopeID are synthesized from the HA set name by the
	// orchestrator (PC-G1..G3 scope; PE wires real DASH IDs).
	GetHaScopeState(ctx context.Context, ns, vdpuID, scopeID string) ([]*HaScopeView, error)

	// TriggerSwitchover starts a planned drain-first role flip from the
	// current ACTIVE to `targetDpuID` (or picked highest-priority STANDBY
	// if empty). Returns a stream channel of per-phase status rows; the
	// channel closes when the switchover terminates. Error is non-nil
	// only when the switchover could not start.
	TriggerSwitchover(ctx context.Context, ns, name, targetDpuID, reason string) (<-chan dashcenterv1.HaScopeStatus, error)

	// TriggerFailover starts an unplanned failover. PC-G2 contract:
	// MUST NOT contact the presumed-dead DPU. Same stream shape as
	// TriggerSwitchover.
	TriggerFailover(ctx context.Context, ns, name, failedDpuID, targetDpuID, reason string) (<-chan dashcenterv1.HaScopeStatus, error)

	// WatchHaEvents subscribes to fleet-wide HA events. The returned
	// cancel function MUST be called by the caller to release the
	// subscription; otherwise the broadcaster slot leaks.
	WatchHaEvents(filter HaEventFilter) (<-chan dashcenterv1.HaEvent, func(), error)

	// GetFlowSyncStats returns per-HA-set flow-sync state aggregates.
	// PC-G1..G3 scope: derived from the orchestrator's model
	// (FLOW_SYNC_STATE_SYNCED for any set with >=2 members). PE swaps
	// in real DASH telemetry.
	GetFlowSyncStats(ctx context.Context, ns, name string) (*FlowSyncStatsView, error)

	// ListHaSets returns every HA set the orchestrator knows about,
	// sorted by (namespace, name). Used by /admin/health and by
	// dashctl ha list.
	ListHaSets(ctx context.Context) []*HaSetView
}

// HaEventFilter mirrors orchestrator.Filter without leaking the orch
// package across the service boundary.
type HaEventFilter struct {
	Namespaces []string                       `json:"namespaces,omitempty"`
	HaSetNames []string                       `json:"ha_set_names,omitempty"`
	Types      []dashcenterv1.HaEvent_Type    `json:"types,omitempty"`
}

// HaSetView is the JSON-friendly aggregate state of one HA set.
type HaSetView struct {
	Namespace string                       `json:"namespace"`
	Name      string                       `json:"name"`
	Mode      string                       `json:"mode"`
	VirtualIP string                       `json:"virtual_ip,omitempty"`
	FlowSync  dashcenterv1.FlowSyncState   `json:"flow_sync"`
	Members   []*HaMemberView              `json:"members"`
}

// HaMemberView is one DPU's role within an HA set.
type HaMemberView struct {
	DpuID  string                       `json:"dpu_id"`
	Role   dashcenterv1.HaScopeRole     `json:"role"`
	Reason string                       `json:"reason,omitempty"`
}

// HaScopeView is the per-scope row exposed by GetHaScopeState.
type HaScopeView struct {
	Namespace    string                       `json:"namespace"`
	VdpuID       string                       `json:"vdpu_id"`
	ScopeID      string                       `json:"scope_id"`
	DpuID        string                       `json:"dpu_id"`
	Role         dashcenterv1.HaScopeRole     `json:"role"`
	IsRoleHolder bool                         `json:"is_role_holder"`
	FlowSync     dashcenterv1.FlowSyncState   `json:"flow_sync"`
	Reason       string                       `json:"reason,omitempty"`
}

// FlowSyncStatsView is the JSON aggregate the gRPC/REST adapters return.
type FlowSyncStatsView struct {
	Namespace string                       `json:"namespace,omitempty"`
	HaSetName string                       `json:"ha_set_name,omitempty"`
	State     dashcenterv1.FlowSyncState   `json:"state"`
	Detail    string                       `json:"detail,omitempty"`
}

// --- impl backed by orchestrator -------------------------------------

// haService implements HaService against an orchestrator.Orchestrator.
// service.NewHa returns it once main.go wires the orchestrator; gRPC
// and REST adapters call into it.
type haService struct {
	orch *orchestrator.Orchestrator
}

// NewHa returns the HaService impl. Panics if orch is nil — production
// always wires one; tests that don't want HA should not call this.
func NewHa(orch *orchestrator.Orchestrator) HaService {
	if orch == nil {
		panic("service.NewHa: orchestrator is nil")
	}
	return &haService{orch: orch}
}

func (h *haService) GetHaSetState(_ context.Context, ns, name string) (*HaSetView, error) {
	if name == "" {
		return nil, fmt.Errorf("%w: ha_set name is required", ErrInvalidArgument)
	}
	ns = resolveNS(ns)
	set, err := h.orch.GetSet(ns, name)
	if err != nil {
		if errors.Is(err, orchestrator.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s/%s", ErrInvalidArgument, ns, name)
		}
		return nil, err
	}
	return setToView(&set), nil
}

func (h *haService) GetHaScopeState(_ context.Context, ns, vdpuID, scopeID string) ([]*HaScopeView, error) {
	ns = resolveNS(ns)
	// PC-G1..G3 model: a scope's (vdpu_id, scope_id) is synthesised
	// from the HA set name (orchestrator.SyncFromSpec sets vdpu = name+"-vdpu",
	// scope = name+"-scope"). Reverse that here to find the set.
	for _, set := range h.orch.ListSets() {
		if set.Namespace != ns {
			continue
		}
		if vdpuID != "" && set.VdpuID != vdpuID {
			continue
		}
		if scopeID != "" && set.ScopeID != scopeID {
			continue
		}
		out := make([]*HaScopeView, 0, len(set.Members))
		for _, m := range set.Members {
			out = append(out, &HaScopeView{
				Namespace: set.Namespace, VdpuID: set.VdpuID, ScopeID: set.ScopeID,
				DpuID: m.DpuID, Role: m.Role,
				IsRoleHolder: m.Role == dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_ACTIVE,
				FlowSync:     set.FlowSync, Reason: m.Reason,
			})
		}
		return out, nil
	}
	return nil, fmt.Errorf("%w: no HA scope matches vdpu=%q scope=%q in %s", ErrInvalidArgument, vdpuID, scopeID, ns)
}

func (h *haService) TriggerSwitchover(ctx context.Context, ns, name, targetDpuID, reason string) (<-chan dashcenterv1.HaScopeStatus, error) {
	if name == "" {
		return nil, fmt.Errorf("%w: ha_set name is required", ErrInvalidArgument)
	}
	ns = resolveNS(ns)
	ch, err := h.orch.TriggerSwitchover(ctx, ns, name, targetDpuID, reason)
	if err != nil {
		if errors.Is(err, orchestrator.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s/%s", ErrInvalidArgument, ns, name)
		}
		if errors.Is(err, orchestrator.ErrInvalidTransition) {
			return nil, fmt.Errorf("%w: %w", ErrFailedPrecondition, err)
		}
		return nil, err
	}
	return ch, nil
}

func (h *haService) TriggerFailover(ctx context.Context, ns, name, failedDpuID, targetDpuID, reason string) (<-chan dashcenterv1.HaScopeStatus, error) {
	if name == "" {
		return nil, fmt.Errorf("%w: ha_set name is required", ErrInvalidArgument)
	}
	if failedDpuID == "" {
		return nil, fmt.Errorf("%w: failed_dpu_id is required for failover", ErrInvalidArgument)
	}
	ns = resolveNS(ns)
	ch, err := h.orch.TriggerFailover(ctx, ns, name, failedDpuID, targetDpuID, reason)
	if err != nil {
		if errors.Is(err, orchestrator.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s/%s", ErrInvalidArgument, ns, name)
		}
		if errors.Is(err, orchestrator.ErrInvalidTransition) {
			return nil, fmt.Errorf("%w: %w", ErrFailedPrecondition, err)
		}
		return nil, err
	}
	return ch, nil
}

func (h *haService) WatchHaEvents(filter HaEventFilter) (<-chan dashcenterv1.HaEvent, func(), error) {
	ch, cancel := h.orch.Broadcaster().Subscribe(orchestrator.Filter{
		Namespaces: filter.Namespaces,
		HaSetNames: filter.HaSetNames,
		Types:      filter.Types,
	})
	return ch, cancel, nil
}

func (h *haService) GetFlowSyncStats(_ context.Context, ns, name string) (*FlowSyncStatsView, error) {
	ns = resolveNS(ns)
	if name != "" {
		set, err := h.orch.GetSet(ns, name)
		if err != nil {
			return nil, fmt.Errorf("%w: %s/%s", ErrInvalidArgument, ns, name)
		}
		return &FlowSyncStatsView{
			Namespace: set.Namespace, HaSetName: set.Name, State: set.FlowSync,
			Detail: fmt.Sprintf("members=%d", len(set.Members)),
		}, nil
	}
	// Fleet-wide aggregate: report SYNCED if every set is synced; else
	// FAILED if any set is failed; else the lowest non-zero state.
	state := dashcenterv1.FlowSyncState_FLOW_SYNC_STATE_NONE
	for _, s := range h.orch.ListSets() {
		switch s.FlowSync {
		case dashcenterv1.FlowSyncState_FLOW_SYNC_STATE_FAILED:
			return &FlowSyncStatsView{State: dashcenterv1.FlowSyncState_FLOW_SYNC_STATE_FAILED, Detail: "at least one HA set has failed sync"}, nil
		case dashcenterv1.FlowSyncState_FLOW_SYNC_STATE_SYNCED:
			state = dashcenterv1.FlowSyncState_FLOW_SYNC_STATE_SYNCED
		case dashcenterv1.FlowSyncState_FLOW_SYNC_STATE_SYNCING, dashcenterv1.FlowSyncState_FLOW_SYNC_STATE_INITIATING:
			if state != dashcenterv1.FlowSyncState_FLOW_SYNC_STATE_SYNCED {
				state = s.FlowSync
			}
		}
	}
	return &FlowSyncStatsView{State: state}, nil
}

func (h *haService) ListHaSets(_ context.Context) []*HaSetView {
	sets := h.orch.ListSets()
	out := make([]*HaSetView, 0, len(sets))
	for i := range sets {
		out = append(out, setToView(&sets[i]))
	}
	return out
}

// setToView projects an orchestrator.Set into the JSON-friendly view.
func setToView(s *orchestrator.Set) *HaSetView {
	out := &HaSetView{
		Namespace: s.Namespace, Name: s.Name, Mode: s.Mode, VirtualIP: s.VirtualIP, FlowSync: s.FlowSync,
		Members: make([]*HaMemberView, 0, len(s.Members)),
	}
	for _, m := range s.Members {
		out.Members = append(out.Members, &HaMemberView{DpuID: m.DpuID, Role: m.Role, Reason: m.Reason})
	}
	return out
}
