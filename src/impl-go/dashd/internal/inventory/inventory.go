// Package inventory manages the DPU registry, tracks DPU lifecycle state,
// and provides subscription for dynamic DPU add/remove events.
package inventory

import (
"errors"
"fmt"
"os"
"sort"
"sync"
"time"

dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"gopkg.in/yaml.v3"
)

// ErrNotFound is returned when a DPU is not in the registry.
var ErrNotFound = errors.New("inventory: dpu not found")

// DpuEntry represents a single DPU in the registry.
type DpuEntry struct {
ID           string
Endpoint     string
Labels       map[string]string
State        dashcenterv1.DpuState
Capabilities *dashcenterv1.DpuCapabilities
// Limits is the advertised DpuCapacityLimits for this DPU. Consumed by
// the capacity-admission tracker (internal/capacity, PB-1). nil means
// "limits not yet known" — the tracker treats this as "no admission
// check possible", allowing writes through with a log warning so a
// half-configured cluster does not silently reject every Put.
Limits       *dashcenterv1.DpuCapacityLimits
// Cordoned, when true, excludes this DPU from new ENI placements
// (PC-1). The capacity tracker still counts existing ENIs but the
// placement engine in capacity.placementForEni filters cordoned
// DPUs out of the fleet-wide fallback. A placement_hint that
// explicitly names a cordoned DPU is rejected at admission time by
// the operations layer (not silently dropped). Drain (PC-7) sets
// this true before migrating ENIs off the DPU.
Cordoned     bool
LastSeen     time.Time
ConsecErrors int
}

// Clone returns a deep copy of DpuEntry.
func (e DpuEntry) Clone() DpuEntry {
c := e
if e.Labels != nil {
c.Labels = make(map[string]string, len(e.Labels))
for k, v := range e.Labels {
c.Labels[k] = v
}
}
return c
}

// SubscribeFunc is called when a DPU is registered or deregistered.
type SubscribeFunc func(dpuID, endpoint string, removed bool)

// Inventory is a thread-safe registry of DPU entries.
type Inventory struct {
mu          sync.RWMutex
byID        map[string]*DpuEntry
subscribers []SubscribeFunc
}

// New creates an empty Inventory.
func New() *Inventory {
return &Inventory{
byID: make(map[string]*DpuEntry),
}
}

// Register adds or updates a DPU entry. New entries start in REGISTERING.
// Re-registering an existing DPU updates Endpoint/Labels but preserves State
// and Capabilities.
func (inv *Inventory) Register(e DpuEntry) error {
if e.ID == "" {
return errors.New("inventory: dpu id is required")
}
if e.Endpoint == "" {
return errors.New("inventory: dpu endpoint is required")
}

inv.mu.Lock()
defer inv.mu.Unlock()

existing, ok := inv.byID[e.ID]
if ok {
// Update endpoint and labels, preserve state.
existing.Endpoint = e.Endpoint
if e.Labels != nil {
existing.Labels = make(map[string]string, len(e.Labels))
for k, v := range e.Labels {
existing.Labels[k] = v
}
}
} else {
entry := e.Clone()
entry.State = dashcenterv1.DpuState_DPU_STATE_REGISTERING
entry.LastSeen = time.Now()
inv.byID[e.ID] = &entry
}

// Notify subscribers (outside lock would be cleaner but we keep it simple).
for _, fn := range inv.subscribers {
fn(e.ID, e.Endpoint, false)
}

return nil
}

// Deregister removes a DPU. Returns ErrNotFound if absent.
func (inv *Inventory) Deregister(id string) error {
inv.mu.Lock()
defer inv.mu.Unlock()

entry, ok := inv.byID[id]
if !ok {
return ErrNotFound
}
endpoint := entry.Endpoint
delete(inv.byID, id)

for _, fn := range inv.subscribers {
fn(id, endpoint, true)
}
return nil
}

// Get returns a clone of the DPU entry. Returns ErrNotFound if absent.
func (inv *Inventory) Get(id string) (DpuEntry, error) {
inv.mu.RLock()
defer inv.mu.RUnlock()

entry, ok := inv.byID[id]
if !ok {
return DpuEntry{}, ErrNotFound
}
return entry.Clone(), nil
}

// List returns clones of all DPU entries, sorted by ID.
func (inv *Inventory) List() []DpuEntry {
inv.mu.RLock()
defer inv.mu.RUnlock()

result := make([]DpuEntry, 0, len(inv.byID))
for _, e := range inv.byID {
result = append(result, e.Clone())
}
sort.Slice(result, func(i, j int) bool {
return result[i].ID < result[j].ID
})
return result
}

// SetState updates a DPU's state and sets LastSeen to now.
func (inv *Inventory) SetState(id string, state dashcenterv1.DpuState) error {
inv.mu.Lock()
defer inv.mu.Unlock()

entry, ok := inv.byID[id]
if !ok {
return ErrNotFound
}
entry.State = state
entry.LastSeen = time.Now()
return nil
}

// SetCapabilities stores capabilities advertised by the DPU.
func (inv *Inventory) SetCapabilities(id string, caps *dashcenterv1.DpuCapabilities) error {
inv.mu.Lock()
defer inv.mu.Unlock()

entry, ok := inv.byID[id]
if !ok {
return ErrNotFound
}
entry.Capabilities = caps
return nil
}

// SetLimits stores DpuCapacityLimits advertised by the DPU. Used by the
// capacity-admission tracker (internal/capacity, PB-1). Pass nil to
// clear (e.g. when a DPU has reset and is mid-handshake).
func (inv *Inventory) SetLimits(id string, limits *dashcenterv1.DpuCapacityLimits) error {
inv.mu.Lock()
defer inv.mu.Unlock()

entry, ok := inv.byID[id]
if !ok {
return ErrNotFound
}
entry.Limits = limits
return nil
}

// SetCordoned flips the placement-exclusion flag (PC-1). When true,
// the placement engine excludes this DPU from new ENI fan-out.
// Existing ENIs already placed on the DPU stay put until drained.
// Idempotent.
func (inv *Inventory) SetCordoned(id string, cordoned bool) error {
	inv.mu.Lock()
	defer inv.mu.Unlock()

	entry, ok := inv.byID[id]
	if !ok {
		return ErrNotFound
	}
	entry.Cordoned = cordoned
	return nil
}

// IncrementErrors atomically bumps ConsecErrors and returns new value.
func (inv *Inventory) IncrementErrors(id string) (int, error) {
inv.mu.Lock()
defer inv.mu.Unlock()

entry, ok := inv.byID[id]
if !ok {
return 0, ErrNotFound
}
entry.ConsecErrors++
return entry.ConsecErrors, nil
}

// ResetErrors zeros ConsecErrors.
func (inv *Inventory) ResetErrors(id string) error {
inv.mu.Lock()
defer inv.mu.Unlock()

entry, ok := inv.byID[id]
if !ok {
return ErrNotFound
}
entry.ConsecErrors = 0
return nil
}

// Subscribe registers a callback fired on Register/Deregister.
func (inv *Inventory) Subscribe(fn SubscribeFunc) {
inv.mu.Lock()
defer inv.mu.Unlock()
inv.subscribers = append(inv.subscribers, fn)
}

// inventoryYAML is the YAML schema for inventory files.
type inventoryYAML struct {
Dpus []dpuYAML `yaml:"dpus"`
}

type dpuYAML struct {
ID       string            `yaml:"id"`
Endpoint string            `yaml:"endpoint"`
Labels   map[string]string `yaml:"labels"`
}

// LoadFromFile reads a YAML inventory file and Register()s each DPU.
func LoadFromFile(path string, inv *Inventory) error {
data, err := os.ReadFile(path)
if err != nil {
return fmt.Errorf("inventory: read %s: %w", path, err)
}

var cfg inventoryYAML
if err := yaml.Unmarshal(data, &cfg); err != nil {
return fmt.Errorf("inventory: parse %s: %w", path, err)
}

for _, d := range cfg.Dpus {
if err := inv.Register(DpuEntry{
ID:       d.ID,
Endpoint: d.Endpoint,
Labels:   d.Labels,
}); err != nil {
return fmt.Errorf("inventory: register %s from %s: %w", d.ID, path, err)
}
}
return nil
}