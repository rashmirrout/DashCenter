package service

import (
	"context"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// ObservabilityService defines read-only operations for fleet status and drift.
type ObservabilityService interface {
	GetDpuStatus(ctx context.Context, dpuIDs []string) ([]DpuStatus, error)
	GetDrift(ctx context.Context, dpuID string) ([]DriftItem, error)
	GetHealth(ctx context.Context) (*HealthStatus, error)
}

// HealthStatus is the admin health check response.
type HealthStatus struct {
	Status string                `json:"status"` // "ok" | "degraded"
	Dpus   map[string]string     `json:"dpus"`   // dpu_id → state
}

// observabilityService is the default implementation.
type observabilityService struct {
	inv   *inventory.Inventory
	store store.DesiredStore
	obs   *model.ObsCache
}

// NewObservability creates a new ObservabilityService.
func NewObservability(inv *inventory.Inventory, st store.DesiredStore, obs *model.ObsCache) ObservabilityService {
	return &observabilityService{inv: inv, store: st, obs: obs}
}

func (s *observabilityService) GetDpuStatus(ctx context.Context, dpuIDs []string) ([]DpuStatus, error) {
	entries := s.inv.List()
	// Filter by requested IDs if provided.
	filter := make(map[string]bool, len(dpuIDs))
	for _, id := range dpuIDs {
		filter[id] = true
	}

	result := make([]DpuStatus, 0, len(entries))
	for _, e := range entries {
		if len(filter) > 0 && !filter[e.ID] {
			continue
		}
		result = append(result, DpuStatus{
			ID:       e.ID,
			Endpoint: e.Endpoint,
			State:    e.State,
			Labels:   e.Labels,
		})
	}
	return result, nil
}

func (s *observabilityService) GetDrift(ctx context.Context, dpuID string) ([]DriftItem, error) {
if dpuID == "" {
return nil, nil
}

// Get observed objects for this DPU via ObsCache.GetDpu().
observedMap := s.obs.GetDpu(dpuID)

// Get all desired specs across all kinds.
kinds := []string{"vnet", "eni", "vnet_mapping", "acl_policy", "route_policy", "ha_set", "service_tunnel"}
desiredKeys := make(map[string]bool)
for _, kind := range kinds {
specs, err := s.store.List(ctx, store.DefaultNamespace, kind)
if err != nil {
continue // best-effort
}
for _, sp := range specs {
desiredKeys[sp.Key.Kind+"/"+sp.Key.Name] = true
}
}

var items []DriftItem

// Check what's observed but not desired.
observedKeys := make(map[string]bool, len(observedMap))
for key := range observedMap {
observedKeys[key] = true
if !desiredKeys[key] {
items = append(items, DriftItem{
Kind:   key,
DpuID:  dpuID,
Op:     "delete",
Detail: "observed on DPU but not declared",
})
}
}

// Check what's desired but not observed.
for key := range desiredKeys {
if !observedKeys[key] {
items = append(items, DriftItem{
Kind:  key,
DpuID: dpuID,
Op:    "apply",
Detail: "declared but not observed on DPU",
})
}
}

return items, nil
}

func (s *observabilityService) GetHealth(ctx context.Context) (*HealthStatus, error) {
	entries := s.inv.List()
	status := "ok"
	dpus := make(map[string]string, len(entries))

	for _, e := range entries {
		dpus[e.ID] = e.State.String()
		if e.State == 6 || e.State == 7 { // UNREACHABLE or FAILED
			status = "degraded"
		}
	}

	return &HealthStatus{Status: status, Dpus: dpus}, nil
}