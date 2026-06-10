// Package service provides the transport-agnostic business logic layer.
// Both REST and gRPC handlers are thin adapters that delegate to this layer.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/namespace"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/reconciler"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// ErrInvalidArgument indicates a validation failure.
var ErrInvalidArgument = errors.New("invalid argument")

// PutResult is the response from any Put* operation.
type PutResult struct {
	Accepted   bool  `json:"accepted"`
	Generation int64 `json:"generation"`
}

// StoredItem is the response from Get/List operations.
type StoredItem struct {
	Kind       string          `json:"kind"`
	Name       string          `json:"name"`
	Namespace  string          `json:"namespace"`
	Generation int64           `json:"generation"`
	Spec       json.RawMessage `json:"spec"`
}

// DriftItem represents a single declared-vs-observed discrepancy.
type DriftItem struct {
	Kind   string `json:"kind"`
	DpuID  string `json:"dpu_id"`
	Op     string `json:"op"`
	Detail string `json:"detail,omitempty"`
}

// DpuStatus is the runtime view of a DPU.
type DpuStatus struct {
	ID       string                  `json:"id"`
	Endpoint string                  `json:"endpoint"`
	State    dashcenterv1.DpuState   `json:"state"`
	Labels   map[string]string       `json:"labels,omitempty"`
}

// ControlPlaneService defines the transport-agnostic API for all mutating
// and read operations on the dashcenter.v1 data model.
type ControlPlaneService interface {
	PutVnet(ctx context.Context, ns string, spec *dashcenterv1.VnetSpec) (*PutResult, error)
	PutEni(ctx context.Context, ns string, spec *dashcenterv1.EniSpec) (*PutResult, error)
	PutVnetMapping(ctx context.Context, ns string, spec *dashcenterv1.VnetMappingSpec) (*PutResult, error)
	PutAclPolicy(ctx context.Context, ns string, spec *dashcenterv1.AclPolicySpec) (*PutResult, error)
	PutRoutePolicy(ctx context.Context, ns string, spec *dashcenterv1.RoutePolicySpec) (*PutResult, error)
	PutHaSet(ctx context.Context, ns string, spec *dashcenterv1.HaSetSpec) (*PutResult, error)
	PutServiceTunnel(ctx context.Context, ns string, spec *dashcenterv1.ServiceTunnelSpec) (*PutResult, error)

	PutInventory(ctx context.Context, dpus []DpuInput) error
	GetInventory(ctx context.Context) ([]DpuStatus, error)

	Delete(ctx context.Context, ns, kind, name string) error
	Get(ctx context.Context, ns, kind, name string) (*StoredItem, error)
	List(ctx context.Context, ns, kind string) ([]*StoredItem, error)

	Reconcile(ctx context.Context) error
}

// DpuInput is the input for PutInventory.
type DpuInput struct {
	ID       string            `json:"id"`
	Endpoint string            `json:"endpoint"`
	Labels   map[string]string `json:"labels,omitempty"`
}

// controlPlaneService is the default implementation.
type controlPlaneService struct {
	store store.DesiredStore
	inv   *inventory.Inventory
	rec   *reconciler.Reconciler
	nsv   *namespace.Validator
}

// NewControlPlane creates a new ControlPlaneService.
func NewControlPlane(st store.DesiredStore, inv *inventory.Inventory, rec *reconciler.Reconciler) ControlPlaneService {
	return &controlPlaneService{store: st, inv: inv, rec: rec, nsv: namespace.NewValidator(st)}
}

// validKinds is the set of spec kinds the service layer recognizes.
var validKinds = map[string]bool{
	"vnet":           true,
	"eni":            true,
	"vnet_mapping":   true,
	"acl_policy":     true,
	"route_policy":   true,
	"ha_set":         true,
	"service_tunnel": true,
}

// specForKind returns an empty spec of the correct type for deserialization.
func specForKind(kind string) (any, bool) {
	switch kind {
	case "vnet":
		return &dashcenterv1.VnetSpec{}, true
	case "eni":
		return &dashcenterv1.EniSpec{}, true
	case "vnet_mapping":
		return &dashcenterv1.VnetMappingSpec{}, true
	case "acl_policy":
		return &dashcenterv1.AclPolicySpec{}, true
	case "route_policy":
		return &dashcenterv1.RoutePolicySpec{}, true
	case "ha_set":
		return &dashcenterv1.HaSetSpec{}, true
	case "service_tunnel":
		return &dashcenterv1.ServiceTunnelSpec{}, true
	default:
		return nil, false
	}
}

func resolveNS(ns string) string {
	if ns == "" {
		return store.DefaultNamespace
	}
	return ns
}

// --- Put operations ---

func (s *controlPlaneService) PutVnet(ctx context.Context, ns string, spec *dashcenterv1.VnetSpec) (*PutResult, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: spec is nil", ErrInvalidArgument)
	}
	name := spec.GetName()
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	ns = resolveNS(ns)
	if err := s.nsv.CheckVnet(ctx, ns, spec); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	gen, err := s.store.Put(ctx, store.ObjectKey{Namespace: ns, Kind: "vnet", Name: name}, spec, int64(spec.GetExpectedGeneration()))
	if err != nil {
		return nil, err
	}
	return &PutResult{Accepted: true, Generation: gen}, nil
}

func (s *controlPlaneService) PutEni(ctx context.Context, ns string, spec *dashcenterv1.EniSpec) (*PutResult, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: spec is nil", ErrInvalidArgument)
	}
	name := spec.GetName()
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	ns = resolveNS(ns)
	if err := s.nsv.CheckEni(ctx, ns, spec); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	gen, err := s.store.Put(ctx, store.ObjectKey{Namespace: ns, Kind: "eni", Name: name}, spec, int64(spec.GetExpectedGeneration()))
	if err != nil {
		return nil, err
	}
	return &PutResult{Accepted: true, Generation: gen}, nil
}

func (s *controlPlaneService) PutVnetMapping(ctx context.Context, ns string, spec *dashcenterv1.VnetMappingSpec) (*PutResult, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: spec is nil", ErrInvalidArgument)
	}
	// VnetMapping uses "vnet_name-ip_address" as the key if no separate name field.
	name := spec.GetVnetName()
	if name == "" {
		return nil, fmt.Errorf("%w: vnet_name is required", ErrInvalidArgument)
	}
	if spec.GetIpAddress() != "" {
		name = name + "-" + spec.GetIpAddress()
	}
	ns = resolveNS(ns)
	if err := s.nsv.CheckVnetMapping(ctx, ns, spec); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	gen, err := s.store.Put(ctx, store.ObjectKey{Namespace: ns, Kind: "vnet_mapping", Name: name}, spec, int64(spec.GetExpectedGeneration()))
	if err != nil {
		return nil, err
	}
	return &PutResult{Accepted: true, Generation: gen}, nil
}

func (s *controlPlaneService) PutAclPolicy(ctx context.Context, ns string, spec *dashcenterv1.AclPolicySpec) (*PutResult, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: spec is nil", ErrInvalidArgument)
	}
	name := spec.GetName()
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	ns = resolveNS(ns)
	if err := s.nsv.CheckAclPolicy(ctx, ns, spec); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	gen, err := s.store.Put(ctx, store.ObjectKey{Namespace: ns, Kind: "acl_policy", Name: name}, spec, int64(spec.GetExpectedGeneration()))
	if err != nil {
		return nil, err
	}
	return &PutResult{Accepted: true, Generation: gen}, nil
}

func (s *controlPlaneService) PutRoutePolicy(ctx context.Context, ns string, spec *dashcenterv1.RoutePolicySpec) (*PutResult, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: spec is nil", ErrInvalidArgument)
	}
	name := spec.GetName()
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	ns = resolveNS(ns)
	if err := s.nsv.CheckRoutePolicy(ctx, ns, spec); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	gen, err := s.store.Put(ctx, store.ObjectKey{Namespace: ns, Kind: "route_policy", Name: name}, spec, int64(spec.GetExpectedGeneration()))
	if err != nil {
		return nil, err
	}
	return &PutResult{Accepted: true, Generation: gen}, nil
}

func (s *controlPlaneService) PutHaSet(ctx context.Context, ns string, spec *dashcenterv1.HaSetSpec) (*PutResult, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: spec is nil", ErrInvalidArgument)
	}
	name := spec.GetName()
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	ns = resolveNS(ns)
	if err := s.nsv.CheckHaSet(ctx, ns, spec); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	gen, err := s.store.Put(ctx, store.ObjectKey{Namespace: ns, Kind: "ha_set", Name: name}, spec, int64(spec.GetExpectedGeneration()))
	if err != nil {
		return nil, err
	}
	return &PutResult{Accepted: true, Generation: gen}, nil
}

func (s *controlPlaneService) PutServiceTunnel(ctx context.Context, ns string, spec *dashcenterv1.ServiceTunnelSpec) (*PutResult, error) {
if spec == nil {
return nil, fmt.Errorf("%w: spec is nil", ErrInvalidArgument)
}
name := spec.Name
if name == "" {
return nil, fmt.Errorf("%w: name is required", ErrInvalidArgument)
}
ns = resolveNS(ns)
if err := s.nsv.CheckServiceTunnel(ctx, ns, spec); err != nil {
	return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
}
gen, err := s.store.Put(ctx, store.ObjectKey{Namespace: ns, Kind: "service_tunnel", Name: name}, spec, int64(spec.ExpectedGeneration))
	if err != nil {
		return nil, err
	}
	return &PutResult{Accepted: true, Generation: gen}, nil
}

// --- Inventory operations ---

func (s *controlPlaneService) PutInventory(ctx context.Context, dpus []DpuInput) error {
	for _, d := range dpus {
		if d.ID == "" {
			return fmt.Errorf("%w: dpu id is required", ErrInvalidArgument)
		}
		if d.Endpoint == "" {
			return fmt.Errorf("%w: dpu endpoint is required for %s", ErrInvalidArgument, d.ID)
		}
		if err := s.inv.Register(inventory.DpuEntry{
			ID: d.ID, Endpoint: d.Endpoint, Labels: d.Labels,
		}); err != nil {
			return fmt.Errorf("register %s: %w", d.ID, err)
		}
	}
	return nil
}

func (s *controlPlaneService) GetInventory(ctx context.Context) ([]DpuStatus, error) {
	entries := s.inv.List()
	result := make([]DpuStatus, len(entries))
	for i, e := range entries {
		result[i] = DpuStatus{
			ID:       e.ID,
			Endpoint: e.Endpoint,
			State:    e.State,
			Labels:   e.Labels,
		}
	}
	return result, nil
}

// --- CRUD operations ---

func (s *controlPlaneService) Delete(ctx context.Context, ns, kind, name string) error {
	if kind == "" {
		return fmt.Errorf("%w: kind is required", ErrInvalidArgument)
	}
	if name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	ns = resolveNS(ns)
	return s.store.Delete(ctx, store.ObjectKey{Namespace: ns, Kind: kind, Name: name})
}

func (s *controlPlaneService) Get(ctx context.Context, ns, kind, name string) (*StoredItem, error) {
	if kind == "" {
		return nil, fmt.Errorf("%w: kind is required", ErrInvalidArgument)
	}
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	ns = resolveNS(ns)
	sp, err := s.store.Get(ctx, store.ObjectKey{Namespace: ns, Kind: kind, Name: name})
	if err != nil {
		return nil, err
	}
	return &StoredItem{
		Kind:       sp.Key.Kind,
		Name:       sp.Key.Name,
		Namespace:  sp.Key.Namespace,
		Generation: sp.Generation,
		Spec:       json.RawMessage(sp.Data),
	}, nil
}

func (s *controlPlaneService) List(ctx context.Context, ns, kind string) ([]*StoredItem, error) {
	if kind == "" {
		return nil, fmt.Errorf("%w: kind is required", ErrInvalidArgument)
	}
	ns = resolveNS(ns)
	specs, err := s.store.List(ctx, ns, kind)
	if err != nil {
		return nil, err
	}
	items := make([]*StoredItem, len(specs))
	for i, sp := range specs {
		items[i] = &StoredItem{
			Kind:       sp.Key.Kind,
			Name:       sp.Key.Name,
			Namespace:  sp.Key.Namespace,
			Generation: sp.Generation,
			Spec:       json.RawMessage(sp.Data),
		}
	}
	return items, nil
}

// --- Reconcile ---

func (s *controlPlaneService) Reconcile(ctx context.Context) error {
	if s.rec != nil {
		s.rec.ForceReconcile()
	}
	return nil
}