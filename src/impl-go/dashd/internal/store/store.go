// Package store defines the DesiredStore interface — the contract that every
// desired-state backend (file, etcd) must implement.
package store

import (
"context"
"errors"
"fmt"
"time"
)

// DefaultNamespace is used when the caller omits the namespace.
const DefaultNamespace = "default"

// ObjectKey uniquely identifies a stored spec.
type ObjectKey struct {
Namespace string // required; defaults to "default"; Validate() rejects ""
Kind      string // lowercase snake_case ("vnet", "eni", "vnet_mapping")
Name      string // operator-supplied resource name
}

// String returns the canonical "namespace/kind/name" representation.
func (k ObjectKey) String() string { return k.Namespace + "/" + k.Kind + "/" + k.Name }

// StoredSpec is the envelope persisted for each spec.
type StoredSpec struct {
Key          ObjectKey
Generation   int64     // per-key opaque monotonic token; starts at 1 for file backend.
EtcdRevision int64     // populated only by etcd backend; 0 for file backend.
Data         []byte    // protojson-encoded spec
UpdatedAt    time.Time
}

// EventType describes the kind of change.
type EventType int

const (
EventPut    EventType = 1
EventDelete EventType = 2
EventResync EventType = 3 // sentinel: subscriber missed events; must re-list
)

// DesiredEvent is emitted by Watch.
type DesiredEvent struct {
Type EventType
Key  ObjectKey   // zero-value for EventResync
Spec *StoredSpec // nil for EventDelete and EventResync
}

// DesiredStore is the contract for desired-state persistence.
type DesiredStore interface {
// Put creates or replaces. Returns the new generation.
// key.Namespace must be non-empty (caller defaults to DefaultNamespace).
// expectedGeneration > 0 and != current → ErrGenerationMismatch.
// expectedGeneration == 0 disables the check (last-write-wins).
Put(ctx context.Context, key ObjectKey, spec any, expectedGeneration int64) (int64, error)

// Delete removes the spec. Returns ErrNotFound if absent.
Delete(ctx context.Context, key ObjectKey) error

// Get returns the stored spec. Returns ErrNotFound if absent.
Get(ctx context.Context, key ObjectKey) (*StoredSpec, error)

// List returns all specs for (namespace, kind), sorted by Name.
// Empty slice if none.
List(ctx context.Context, namespace, kind string) ([]*StoredSpec, error)

// Watch returns a channel receiving a snapshot of current state
// followed by live mutations. The channel MAY be closed without warning
// (compaction, store restart, slow-subscriber drop, etc.); the caller
// MUST be prepared to re-Subscribe.
//
// Buffered (64). When a subscriber would be dropped due to back-pressure,
// the store sends EventResync before dropping, so the consumer knows it
// must re-list to restore consistency.
Watch(ctx context.Context) (<-chan DesiredEvent, error)

// Close releases resources. Idempotent.
Close() error
}

// Sentinel errors.
var (
ErrNotFound           = errors.New("store: not found")
ErrGenerationMismatch = errors.New("store: generation mismatch")
ErrClosed             = errors.New("store: closed")
)

// String returns the event type name.
func (e EventType) String() string {
switch e {
case EventPut:
return "PUT"
case EventDelete:
return "DELETE"
case EventResync:
return "RESYNC"
default:
return fmt.Sprintf("EventType(%d)", int(e))
}
}