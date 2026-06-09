package model

import (
"sort"
"sync"

dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime/kinds"
"google.golang.org/protobuf/proto"
)

// ObsCache is a thread-safe per-DPU observed-state cache.
type ObsCache struct {
mu   sync.RWMutex
data map[string]map[string]*dashapiv1.Object // dpuID → innerKey → obj
}

// NewObsCache creates an empty cache.
func NewObsCache() *ObsCache {
return &ObsCache{
data: make(map[string]map[string]*dashapiv1.Object),
}
}

// Set inserts or replaces an object in a DPU's cache.
func (c *ObsCache) Set(dpuID string, obj *dashapiv1.Object) {
c.mu.Lock()
defer c.mu.Unlock()

m := c.data[dpuID]
if m == nil {
m = make(map[string]*dashapiv1.Object)
c.data[dpuID] = m
}
ik := innerKey(obj.GetKind(), obj.GetKey())
m[ik] = obj
}

// Delete removes (kind, key) from a DPU's cache.
func (c *ObsCache) Delete(dpuID string, kind dashapiv1.ObjectKind, key []string) {
c.mu.Lock()
defer c.mu.Unlock()

m := c.data[dpuID]
if m == nil {
return
}
ik := innerKey(kind, key)
delete(m, ik)
}

// ClearDpu atomically replaces all entries for dpuID with an empty set.
// Called by subscribe/Pump on every reconnect (snapshot-first re-sync).
func (c *ObsCache) ClearDpu(dpuID string) {
c.mu.Lock()
defer c.mu.Unlock()
c.data[dpuID] = make(map[string]*dashapiv1.Object)
}

// GetDpu returns a defensive copy of a DPU's cache. Callers may mutate
// the returned map without affecting the cache.
func (c *ObsCache) GetDpu(dpuID string) map[string]*dashapiv1.Object {
c.mu.RLock()
defer c.mu.RUnlock()

m := c.data[dpuID]
out := make(map[string]*dashapiv1.Object, len(m))
for k, v := range m {
out[k] = v
}
return out
}

// Diff computes Add/Update/Remove for dpuID vs the given desired set.
// Equality: same (kind, key) AND payloads compare equal under proto.Equal.
// Generation is NOT compared.
// Output is stable-sorted by (kind, joined_key) for reproducible logs.
func (c *ObsCache) Diff(dpuID string, desired []*dashapiv1.Object) DiffResult {
c.mu.RLock()
observed := c.data[dpuID]
c.mu.RUnlock()

// Build desired lookup.
desiredMap := make(map[string]*dashapiv1.Object, len(desired))
for _, obj := range desired {
ik := innerKey(obj.GetKind(), obj.GetKey())
desiredMap[ik] = obj
}

var result DiffResult

// Add + Update: desired objects not in observed, or with different payload.
for ik, dObj := range desiredMap {
oObj, exists := observed[ik]
if !exists {
result.Add = append(result.Add, dObj)
} else if !payloadsEqual(dObj, oObj) {
result.Update = append(result.Update, dObj)
}
}

// Remove: observed objects not in desired.
for ik, oObj := range observed {
if _, exists := desiredMap[ik]; !exists {
result.Remove = append(result.Remove, oObj)
}
}

// Stable sort for reproducibility.
sortObjs(result.Add)
sortObjs(result.Update)
sortObjs(result.Remove)

return result
}

// payloadsEqual compares two Objects by their typed payloads using proto.Equal.
func payloadsEqual(a, b *dashapiv1.Object) bool {
ma, err1 := kinds.PayloadOf(a)
mb, err2 := kinds.PayloadOf(b)
if err1 != nil || err2 != nil {
return false
}
return proto.Equal(ma, mb)
}

// sortObjs sorts objects by (kind, joined_key) for stable output.
func sortObjs(objs []*dashapiv1.Object) {
sort.SliceStable(objs, func(i, j int) bool {
if objs[i].GetKind() != objs[j].GetKind() {
return objs[i].GetKind() < objs[j].GetKind()
}
return joinedKey(objs[i]) < joinedKey(objs[j])
})
}

func joinedKey(obj *dashapiv1.Object) string {
return innerKey(obj.GetKind(), obj.GetKey())
}