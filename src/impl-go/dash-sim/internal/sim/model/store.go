// Package model is the in-memory DASH object store backing the DashApi
// service. It is a generic (kind, key) -> proto.Message map; per-kind logic
// lives in package kinds.
//
// Every mutation:
//
//   1. validates the payload exists and matches the kind,
//   2. records created_ts_ns / updated_ts_ns at the store layer (timestamps
//      are stored OUT OF BAND — upstream proto types do not carry them, so we
//      tag each row in a sidecar map),
//   3. publishes an *dashapi.Event onto the events.Bus.
package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/events"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime/kinds"
	"google.golang.org/protobuf/proto"
)

// ErrNotFound is returned by Get/Delete when a (kind, key) is unknown.
var ErrNotFound = errors.New("not found")

// row is the stored value plus sidecar timestamps.
type row struct {
	val         proto.Message
	createdTsNs int64
	updatedTsNs int64
}

// Store is the simulator's authoritative in-memory state.
type Store struct {
	mu     sync.RWMutex
	tables map[dashapi.ObjectKind]map[string]*row
	bus    *events.Bus
	nextTx atomic.Uint64
}

// New constructs an empty store wired to the provided event bus.
func New(bus *events.Bus) *Store {
	return &Store{
		tables: make(map[dashapi.ObjectKind]map[string]*row),
		bus:    bus,
	}
}

// TxID returns a fresh monotonic transaction id.
func (s *Store) TxID() string {
	return fmt.Sprintf("tx-%d-%d", time.Now().UnixNano(), s.nextTx.Add(1))
}

// JoinKey is the canonical ":"-joined key (matches Redis APP_DB suffix).
func JoinKey(parts []string) string { return strings.Join(parts, ":") }

// Len returns count per object kind.
func (s *Store) Len() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]int, len(s.tables))
	for _, info := range kinds.All {
		out[info.Name] = len(s.tables[info.Kind])
	}
	return out
}

// Reset wipes every object.
func (s *Store) Reset() {
	s.mu.Lock()
	s.tables = make(map[dashapi.ObjectKind]map[string]*row)
	s.mu.Unlock()
}

func nowNs() int64 { return time.Now().UnixNano() }

// Apply creates or updates an object. Returns txn id and the event type the
// model would emit (CREATED or UPDATED).
func (s *Store) Apply(obj *dashapi.Object) (string, dashapi.EventType, error) {
	if obj == nil {
		return "", dashapi.EventType_EVENT_TYPE_UNSPECIFIED, errors.New("apply: nil object")
	}
	info, err := kinds.Lookup(obj.GetKind())
	if err != nil {
		return "", dashapi.EventType_EVENT_TYPE_UNSPECIFIED, err
	}
	payload, err := kinds.PayloadOf(obj)
	if err != nil {
		return "", dashapi.EventType_EVENT_TYPE_UNSPECIFIED, err
	}
	if len(obj.GetKey()) != len(info.KeyParts) {
		return "", dashapi.EventType_EVENT_TYPE_UNSPECIFIED,
			fmt.Errorf("apply: kind %s expects %d key parts %v, got %d",
				info.Name, len(info.KeyParts), info.KeyParts, len(obj.GetKey()))
	}
	for i, part := range obj.GetKey() {
		if part == "" {
			return "", dashapi.EventType_EVENT_TYPE_UNSPECIFIED,
				fmt.Errorf("apply: kind %s key part %q is empty", info.Name, info.KeyParts[i])
		}
	}

	key := JoinKey(obj.GetKey())
	tx := s.TxID()
	clone := proto.Clone(payload)

	s.mu.Lock()
	tbl, ok := s.tables[obj.GetKind()]
	if !ok {
		tbl = make(map[string]*row)
		s.tables[obj.GetKind()] = tbl
	}
	cur, exists := tbl[key]
	now := nowNs()
	var evType dashapi.EventType
	if exists {
		cur.val = clone
		cur.updatedTsNs = now
		evType = dashapi.EventType_EVENT_TYPE_UPDATED
	} else {
		tbl[key] = &row{val: clone, createdTsNs: now, updatedTsNs: now}
		evType = dashapi.EventType_EVENT_TYPE_CREATED
	}
	s.mu.Unlock()

	out, _ := kinds.WrapObject(obj.GetKind(), obj.GetKey(), proto.Clone(clone))
	s.bus.Publish(&dashapi.Event{
		TxnId:      tx,
		Type:       evType,
		Object:     out,
		ServerTsNs: now,
	})
	return tx, evType, nil
}

// Delete removes an object.
func (s *Store) Delete(kind dashapi.ObjectKind, keyParts []string) (string, error) {
	info, err := kinds.Lookup(kind)
	if err != nil {
		return "", err
	}
	if len(keyParts) != len(info.KeyParts) {
		return "", fmt.Errorf("delete: kind %s expects %d key parts, got %d",
			info.Name, len(info.KeyParts), len(keyParts))
	}
	key := JoinKey(keyParts)
	tx := s.TxID()

	s.mu.Lock()
	tbl, ok := s.tables[kind]
	if !ok {
		s.mu.Unlock()
		return tx, ErrNotFound
	}
	cur, exists := tbl[key]
	if !exists {
		s.mu.Unlock()
		return tx, ErrNotFound
	}
	delete(tbl, key)
	s.mu.Unlock()

	out, _ := kinds.WrapObject(kind, keyParts, proto.Clone(cur.val))
	s.bus.Publish(&dashapi.Event{
		TxnId:      tx,
		Type:       dashapi.EventType_EVENT_TYPE_DELETED,
		Object:     out,
		ServerTsNs: nowNs(),
	})
	return tx, nil
}

// Get returns a deep copy of the stored object, or ErrNotFound.
func (s *Store) Get(kind dashapi.ObjectKind, keyParts []string) (*dashapi.Object, error) {
	if _, err := kinds.Lookup(kind); err != nil {
		return nil, err
	}
	key := JoinKey(keyParts)
	s.mu.RLock()
	defer s.mu.RUnlock()
	tbl, ok := s.tables[kind]
	if !ok {
		return nil, ErrNotFound
	}
	cur, ok := tbl[key]
	if !ok {
		return nil, ErrNotFound
	}
	return kinds.WrapObject(kind, keyParts, proto.Clone(cur.val))
}

// List returns every object of kind whose joined key has the given prefix
// (empty matches all). Returned objects are deep copies, ordered by joined key.
func (s *Store) List(kind dashapi.ObjectKind, keyPrefix string) ([]*dashapi.Object, error) {
	if _, err := kinds.Lookup(kind); err != nil {
		return nil, err
	}
	s.mu.RLock()
	tbl := s.tables[kind]
	keys := make([]string, 0, len(tbl))
	for k := range tbl {
		if keyPrefix == "" || strings.HasPrefix(k, keyPrefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]*dashapi.Object, 0, len(keys))
	for _, k := range keys {
		cur := tbl[k]
		obj, _ := kinds.WrapObject(kind, strings.Split(k, ":"), proto.Clone(cur.val))
		out = append(out, obj)
	}
	s.mu.RUnlock()
	return out, nil
}

// SnapshotEvents emits SNAPSHOT events for every object whose kind matches
// the optional filter (empty == all kinds).
func (s *Store) SnapshotEvents(kindFilter []dashapi.ObjectKind) []*dashapi.Event {
	wanted := make(map[dashapi.ObjectKind]struct{}, len(kindFilter))
	for _, k := range kindFilter {
		wanted[k] = struct{}{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []*dashapi.Event
	now := nowNs()
	for _, info := range kinds.All {
		if len(wanted) > 0 {
			if _, ok := wanted[info.Kind]; !ok {
				continue
			}
		}
		tbl := s.tables[info.Kind]
		keys := make([]string, 0, len(tbl))
		for k := range tbl {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			cur := tbl[k]
			obj, _ := kinds.WrapObject(info.Kind, strings.Split(k, ":"), proto.Clone(cur.val))
			out = append(out, &dashapi.Event{
				Type:       dashapi.EventType_EVENT_TYPE_SNAPSHOT,
				Object:     obj,
				ServerTsNs: now,
			})
		}
	}
	return out
}

// AllKeys returns every (kind, joined-key) pair currently stored. Used by the
// counter tick loop.
func (s *Store) AllKeys() map[dashapi.ObjectKind][]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[dashapi.ObjectKind][]string, len(s.tables))
	for k, tbl := range s.tables {
		ks := make([]string, 0, len(tbl))
		for key := range tbl {
			ks = append(ks, key)
		}
		out[k] = ks
	}
	return out
}
