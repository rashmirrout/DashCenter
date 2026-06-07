// Package model defines domain types for dashd's observed-state cache and
// diff computation. These types bridge the dashapi.v1 Object world with
// dashd's per-DPU reconciliation logic.
package model

import (
"fmt"
"strings"

dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
)

// ObjectKey uniquely identifies a dashapi object within a DPU's cache.
type ObjectKey struct {
DpuID string
Kind  dashapiv1.ObjectKind
Key   []string
}

// JoinedKey returns the key components joined with "/".
func (k ObjectKey) JoinedKey() string { return strings.Join(k.Key, "/") }

// String returns a human-readable representation.
func (k ObjectKey) String() string {
return fmt.Sprintf("%s/%s/%s", k.DpuID, k.Kind, k.JoinedKey())
}

// KeyOf creates an ObjectKey from a dpuID and Object, deep-copying the Key slice.
func KeyOf(dpuID string, obj *dashapiv1.Object) ObjectKey {
keyCopy := make([]string, len(obj.GetKey()))
copy(keyCopy, obj.GetKey())
return ObjectKey{
DpuID: dpuID,
Kind:  obj.GetKind(),
Key:   keyCopy,
}
}

// DiffResult captures what needs to change on a DPU.
type DiffResult struct {
Add    []*dashapiv1.Object // in desired, not in observed
Update []*dashapiv1.Object // in both, payload differs
Remove []*dashapiv1.Object // in observed, not in desired
}

// IsEmpty returns true when there's nothing to do.
func (d DiffResult) IsEmpty() bool {
return len(d.Add) == 0 && len(d.Update) == 0 && len(d.Remove) == 0
}

// Total returns the count of all changes.
func (d DiffResult) Total() int {
return len(d.Add) + len(d.Update) + len(d.Remove)
}

// innerKey produces a canonical cache key from kind+key components.
func innerKey(kind dashapiv1.ObjectKind, key []string) string {
return fmt.Sprintf("%d:%s", int(kind), strings.Join(key, "/"))
}