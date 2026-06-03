// Package model is the in-memory DASH object store backing the simulator.
//
// One Store value holds every object kind. All mutations are serialized
// through a single sync.RWMutex; reads use RLock so List/Get scale across
// goroutines. Every mutation:
//
//  1. assigns/refreshes created_ts_ns / updated_ts_ns
//  2. publishes an *dashsimv1.Event onto the events.Bus
//
// The model also computes synthetic IDs for objects whose protobuf message
// doesn't carry a natural primary key (Routes, VnetMappings, AclRules).
package model

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	dashsimv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashsim/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/events"
)

// ErrNotFound is returned by Get/Delete/Update when an id is unknown.
var ErrNotFound = errors.New("not found")

// ErrAlreadyExists is returned by Create when an id is already present.
var ErrAlreadyExists = errors.New("already exists")

// Store is the simulator's authoritative in-memory state.
type Store struct {
	mu sync.RWMutex

	vnets        map[string]*dashsimv1.Vnet
	enis         map[string]*dashsimv1.Eni
	aclGroups    map[string]*dashsimv1.AclGroup
	aclRules     map[string]*dashsimv1.AclRule
	routes       map[string]*dashsimv1.Route
	vnetMappings map[string]*dashsimv1.VnetMapping

	bus    *events.Bus
	nextTx atomic.Uint64
}

// New constructs an empty store wired to the provided event bus.
func New(bus *events.Bus) *Store {
	return &Store{
		vnets:        make(map[string]*dashsimv1.Vnet),
		enis:         make(map[string]*dashsimv1.Eni),
		aclGroups:    make(map[string]*dashsimv1.AclGroup),
		aclRules:     make(map[string]*dashsimv1.AclRule),
		routes:       make(map[string]*dashsimv1.Route),
		vnetMappings: make(map[string]*dashsimv1.VnetMapping),
		bus:          bus,
	}
}

// TxID returns a fresh monotonic transaction id.
func (s *Store) TxID() string {
	return fmt.Sprintf("tx-%d-%d", time.Now().UnixNano(), s.nextTx.Add(1))
}

// Len returns the count per object kind. Useful for tests + /admin/health.
func (s *Store) Len() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]int{
		"vnets":         len(s.vnets),
		"enis":          len(s.enis),
		"acl_groups":    len(s.aclGroups),
		"acl_rules":     len(s.aclRules),
		"routes":        len(s.routes),
		"vnet_mappings": len(s.vnetMappings),
	}
}

// Reset wipes every object. Used by /admin/reset and scenario reload.
func (s *Store) Reset() {
	s.mu.Lock()
	s.vnets = make(map[string]*dashsimv1.Vnet)
	s.enis = make(map[string]*dashsimv1.Eni)
	s.aclGroups = make(map[string]*dashsimv1.AclGroup)
	s.aclRules = make(map[string]*dashsimv1.AclRule)
	s.routes = make(map[string]*dashsimv1.Route)
	s.vnetMappings = make(map[string]*dashsimv1.VnetMapping)
	s.mu.Unlock()
}

// Snapshot returns flat slices of every object, ordered by id. Used by the
// Subscribe stream when snapshot_first=true and by /admin/dump.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := Snapshot{
		Vnets:        sortedVnets(s.vnets),
		Enis:         sortedEnis(s.enis),
		AclGroups:    sortedAclGroups(s.aclGroups),
		AclRules:     sortedAclRules(s.aclRules),
		Routes:       sortedRoutes(s.routes),
		VnetMappings: sortedVnetMappings(s.vnetMappings),
	}
	return snap
}

// Snapshot is the result of Store.Snapshot.
type Snapshot struct {
	Vnets        []*dashsimv1.Vnet        `json:"vnets"`
	Enis         []*dashsimv1.Eni         `json:"enis"`
	AclGroups    []*dashsimv1.AclGroup    `json:"acl_groups"`
	AclRules     []*dashsimv1.AclRule     `json:"acl_rules"`
	Routes       []*dashsimv1.Route       `json:"routes"`
	VnetMappings []*dashsimv1.VnetMapping `json:"vnet_mappings"`
}

func nowNs() int64 { return time.Now().UnixNano() }

func (s *Store) publish(txID string, evType dashsimv1.EventType, kind dashsimv1.ObjectKind, id string, set func(e *dashsimv1.Event)) {
	ev := &dashsimv1.Event{
		TxnId:      txID,
		Type:       evType,
		Kind:       kind,
		Id:         id,
		ServerTsNs: nowNs(),
	}
	set(ev)
	s.bus.Publish(ev)
}

// -----------------------------------------------------------------------------
// VNETs
// -----------------------------------------------------------------------------

// CreateVnet inserts a new VNET. Returns ErrAlreadyExists if the id is taken.
func (s *Store) CreateVnet(in *dashsimv1.Vnet) (string, error) {
	if in.GetId() == "" {
		return "", errors.New("vnet: id is required")
	}
	tx := s.TxID()
	s.mu.Lock()
	if _, ok := s.vnets[in.Id]; ok {
		s.mu.Unlock()
		return tx, ErrAlreadyExists
	}
	v := cloneVnet(in)
	v.CreatedTsNs = nowNs()
	v.UpdatedTsNs = v.CreatedTsNs
	s.vnets[v.Id] = v
	s.mu.Unlock()
	s.publish(tx, dashsimv1.EventType_EVENT_TYPE_CREATED, dashsimv1.ObjectKind_OBJECT_KIND_VNET, v.Id,
		func(e *dashsimv1.Event) { e.Payload = &dashsimv1.Event_Vnet{Vnet: v} })
	return tx, nil
}

// DeleteVnet removes a VNET (and cascades to its ENIs/mappings/routes).
// Returns ErrNotFound if unknown.
func (s *Store) DeleteVnet(id string) (string, error) {
	tx := s.TxID()
	s.mu.Lock()
	v, ok := s.vnets[id]
	if !ok {
		s.mu.Unlock()
		return tx, ErrNotFound
	}
	delete(s.vnets, id)
	// Cascade
	cascadedEni := make([]string, 0)
	for eid, e := range s.enis {
		if e.VnetId == id {
			delete(s.enis, eid)
			cascadedEni = append(cascadedEni, eid)
		}
	}
	cascadedMap := make([]string, 0)
	for mid, m := range s.vnetMappings {
		if m.VnetId == id {
			delete(s.vnetMappings, mid)
			cascadedMap = append(cascadedMap, mid)
		}
	}
	cascadedRoutes := make([]string, 0)
	for rid, r := range s.routes {
		if r.Table == id || r.VnetId == id {
			delete(s.routes, rid)
			cascadedRoutes = append(cascadedRoutes, rid)
		}
	}
	s.mu.Unlock()

	s.publish(tx, dashsimv1.EventType_EVENT_TYPE_DELETED, dashsimv1.ObjectKind_OBJECT_KIND_VNET, id,
		func(e *dashsimv1.Event) { e.Payload = &dashsimv1.Event_Vnet{Vnet: v} })
	for _, eid := range cascadedEni {
		s.publish(tx, dashsimv1.EventType_EVENT_TYPE_DELETED, dashsimv1.ObjectKind_OBJECT_KIND_ENI, eid, func(*dashsimv1.Event) {})
	}
	for _, mid := range cascadedMap {
		s.publish(tx, dashsimv1.EventType_EVENT_TYPE_DELETED, dashsimv1.ObjectKind_OBJECT_KIND_VNET_MAPPING, mid, func(*dashsimv1.Event) {})
	}
	for _, rid := range cascadedRoutes {
		s.publish(tx, dashsimv1.EventType_EVENT_TYPE_DELETED, dashsimv1.ObjectKind_OBJECT_KIND_ROUTE, rid, func(*dashsimv1.Event) {})
	}
	return tx, nil
}

// GetVnet returns a deep copy or ErrNotFound.
func (s *Store) GetVnet(id string) (*dashsimv1.Vnet, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.vnets[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneVnet(v), nil
}

// ListVnets returns every VNET, ordered by id.
func (s *Store) ListVnets() []*dashsimv1.Vnet {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedVnets(s.vnets)
}

// -----------------------------------------------------------------------------
// ENIs
// -----------------------------------------------------------------------------

// CreateEni inserts a new ENI. The referenced VNET must exist.
func (s *Store) CreateEni(in *dashsimv1.Eni) (string, error) {
	if in.GetId() == "" {
		return "", errors.New("eni: id is required")
	}
	tx := s.TxID()
	s.mu.Lock()
	if _, ok := s.enis[in.Id]; ok {
		s.mu.Unlock()
		return tx, ErrAlreadyExists
	}
	if in.VnetId != "" {
		if _, ok := s.vnets[in.VnetId]; !ok {
			s.mu.Unlock()
			return tx, fmt.Errorf("eni: vnet %q does not exist", in.VnetId)
		}
	}
	e := cloneEni(in)
	e.CreatedTsNs = nowNs()
	e.UpdatedTsNs = e.CreatedTsNs
	if e.AdminState == "" {
		e.AdminState = "up"
	}
	s.enis[e.Id] = e
	s.mu.Unlock()
	s.publish(tx, dashsimv1.EventType_EVENT_TYPE_CREATED, dashsimv1.ObjectKind_OBJECT_KIND_ENI, e.Id,
		func(ev *dashsimv1.Event) { ev.Payload = &dashsimv1.Event_Eni{Eni: e} })
	return tx, nil
}

// UpdateEni replaces fields of an existing ENI (full-object update).
func (s *Store) UpdateEni(in *dashsimv1.Eni) (string, error) {
	if in.GetId() == "" {
		return "", errors.New("eni: id is required")
	}
	tx := s.TxID()
	s.mu.Lock()
	cur, ok := s.enis[in.Id]
	if !ok {
		s.mu.Unlock()
		return tx, ErrNotFound
	}
	updated := cloneEni(in)
	updated.CreatedTsNs = cur.CreatedTsNs
	updated.UpdatedTsNs = nowNs()
	if updated.AdminState == "" {
		updated.AdminState = cur.AdminState
	}
	s.enis[in.Id] = updated
	s.mu.Unlock()
	s.publish(tx, dashsimv1.EventType_EVENT_TYPE_UPDATED, dashsimv1.ObjectKind_OBJECT_KIND_ENI, in.Id,
		func(ev *dashsimv1.Event) { ev.Payload = &dashsimv1.Event_Eni{Eni: updated} })
	return tx, nil
}

// DeleteEni removes an ENI.
func (s *Store) DeleteEni(id string) (string, error) {
	tx := s.TxID()
	s.mu.Lock()
	e, ok := s.enis[id]
	if !ok {
		s.mu.Unlock()
		return tx, ErrNotFound
	}
	delete(s.enis, id)
	s.mu.Unlock()
	s.publish(tx, dashsimv1.EventType_EVENT_TYPE_DELETED, dashsimv1.ObjectKind_OBJECT_KIND_ENI, id,
		func(ev *dashsimv1.Event) { ev.Payload = &dashsimv1.Event_Eni{Eni: e} })
	return tx, nil
}

// GetEni returns a deep copy or ErrNotFound.
func (s *Store) GetEni(id string) (*dashsimv1.Eni, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.enis[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneEni(e), nil
}

// ListEnis returns every ENI, ordered by id.
func (s *Store) ListEnis() []*dashsimv1.Eni {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedEnis(s.enis)
}

// -----------------------------------------------------------------------------
// VNET mappings
// -----------------------------------------------------------------------------

func vnetMappingID(m *dashsimv1.VnetMapping) string {
	if m.GetId() != "" {
		return m.GetId()
	}
	return m.GetVnetId() + "/" + m.GetOverlayIp()
}

// AddVnetMapping inserts a mapping. The id is derived from vnet+overlay_ip if
// not supplied. Existing mapping with the same id is replaced.
func (s *Store) AddVnetMapping(in *dashsimv1.VnetMapping) (string, error) {
	if in.GetVnetId() == "" || in.GetOverlayIp() == "" {
		return "", errors.New("vnet_mapping: vnet_id and overlay_ip are required")
	}
	tx := s.TxID()
	in.Id = vnetMappingID(in)
	s.mu.Lock()
	if _, ok := s.vnets[in.VnetId]; !ok {
		s.mu.Unlock()
		return tx, fmt.Errorf("vnet_mapping: vnet %q does not exist", in.VnetId)
	}
	m := cloneVnetMapping(in)
	m.CreatedTsNs = nowNs()
	s.vnetMappings[m.Id] = m
	s.mu.Unlock()
	s.publish(tx, dashsimv1.EventType_EVENT_TYPE_CREATED, dashsimv1.ObjectKind_OBJECT_KIND_VNET_MAPPING, m.Id,
		func(ev *dashsimv1.Event) { ev.Payload = &dashsimv1.Event_VnetMapping{VnetMapping: m} })
	return tx, nil
}

// DeleteVnetMapping removes a mapping by id.
func (s *Store) DeleteVnetMapping(id string) (string, error) {
	tx := s.TxID()
	s.mu.Lock()
	m, ok := s.vnetMappings[id]
	if !ok {
		s.mu.Unlock()
		return tx, ErrNotFound
	}
	delete(s.vnetMappings, id)
	s.mu.Unlock()
	s.publish(tx, dashsimv1.EventType_EVENT_TYPE_DELETED, dashsimv1.ObjectKind_OBJECT_KIND_VNET_MAPPING, id,
		func(ev *dashsimv1.Event) { ev.Payload = &dashsimv1.Event_VnetMapping{VnetMapping: m} })
	return tx, nil
}

// ListVnetMappings returns every mapping, ordered by id.
func (s *Store) ListVnetMappings() []*dashsimv1.VnetMapping {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedVnetMappings(s.vnetMappings)
}

// -----------------------------------------------------------------------------
// Routes
// -----------------------------------------------------------------------------

func routeID(r *dashsimv1.Route) string {
	if r.GetId() != "" {
		return r.GetId()
	}
	return r.GetTable() + "/" + r.GetDstPrefix()
}

// AddRoute inserts or replaces a route.
func (s *Store) AddRoute(in *dashsimv1.Route) (string, error) {
	if in.GetTable() == "" || in.GetDstPrefix() == "" {
		return "", errors.New("route: table and dst_prefix are required")
	}
	tx := s.TxID()
	in.Id = routeID(in)
	s.mu.Lock()
	r := cloneRoute(in)
	r.CreatedTsNs = nowNs()
	s.routes[r.Id] = r
	s.mu.Unlock()
	s.publish(tx, dashsimv1.EventType_EVENT_TYPE_CREATED, dashsimv1.ObjectKind_OBJECT_KIND_ROUTE, r.Id,
		func(ev *dashsimv1.Event) { ev.Payload = &dashsimv1.Event_Route{Route: r} })
	return tx, nil
}

// DeleteRoute removes a route by id.
func (s *Store) DeleteRoute(id string) (string, error) {
	tx := s.TxID()
	s.mu.Lock()
	r, ok := s.routes[id]
	if !ok {
		s.mu.Unlock()
		return tx, ErrNotFound
	}
	delete(s.routes, id)
	s.mu.Unlock()
	s.publish(tx, dashsimv1.EventType_EVENT_TYPE_DELETED, dashsimv1.ObjectKind_OBJECT_KIND_ROUTE, id,
		func(ev *dashsimv1.Event) { ev.Payload = &dashsimv1.Event_Route{Route: r} })
	return tx, nil
}

// ListRoutes returns every route, ordered by id.
func (s *Store) ListRoutes() []*dashsimv1.Route {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedRoutes(s.routes)
}

// -----------------------------------------------------------------------------
// ACL groups + rules
// -----------------------------------------------------------------------------

// AddAclGroup inserts or replaces an ACL group.
func (s *Store) AddAclGroup(in *dashsimv1.AclGroup) (string, error) {
	if in.GetId() == "" {
		return "", errors.New("acl_group: id is required")
	}
	tx := s.TxID()
	s.mu.Lock()
	g := cloneAclGroup(in)
	g.CreatedTsNs = nowNs()
	s.aclGroups[g.Id] = g
	s.mu.Unlock()
	s.publish(tx, dashsimv1.EventType_EVENT_TYPE_CREATED, dashsimv1.ObjectKind_OBJECT_KIND_ACL_GROUP, g.Id,
		func(ev *dashsimv1.Event) { ev.Payload = &dashsimv1.Event_AclGroup{AclGroup: g} })
	return tx, nil
}

// DeleteAclGroup removes a group; cascades to its rules.
func (s *Store) DeleteAclGroup(id string) (string, error) {
	tx := s.TxID()
	s.mu.Lock()
	g, ok := s.aclGroups[id]
	if !ok {
		s.mu.Unlock()
		return tx, ErrNotFound
	}
	delete(s.aclGroups, id)
	cascaded := make([]string, 0)
	for rid, r := range s.aclRules {
		if r.GroupId == id {
			delete(s.aclRules, rid)
			cascaded = append(cascaded, rid)
		}
	}
	s.mu.Unlock()
	s.publish(tx, dashsimv1.EventType_EVENT_TYPE_DELETED, dashsimv1.ObjectKind_OBJECT_KIND_ACL_GROUP, id,
		func(ev *dashsimv1.Event) { ev.Payload = &dashsimv1.Event_AclGroup{AclGroup: g} })
	for _, rid := range cascaded {
		s.publish(tx, dashsimv1.EventType_EVENT_TYPE_DELETED, dashsimv1.ObjectKind_OBJECT_KIND_ACL_RULE, rid, func(*dashsimv1.Event) {})
	}
	return tx, nil
}

// ListAclGroups returns every group, ordered by id.
func (s *Store) ListAclGroups() []*dashsimv1.AclGroup {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedAclGroups(s.aclGroups)
}

func aclRuleID(r *dashsimv1.AclRule) string {
	if r.GetId() != "" {
		return r.GetId()
	}
	return fmt.Sprintf("%s/%d", r.GetGroupId(), r.GetNum())
}

// AddAclRule inserts a rule. The id is derived from group+num if unset.
func (s *Store) AddAclRule(in *dashsimv1.AclRule) (string, error) {
	if in.GetGroupId() == "" {
		return "", errors.New("acl_rule: group_id is required")
	}
	if in.GetNum() == 0 {
		return "", errors.New("acl_rule: num must be > 0")
	}
	tx := s.TxID()
	in.Id = aclRuleID(in)
	s.mu.Lock()
	if _, ok := s.aclGroups[in.GroupId]; !ok {
		s.mu.Unlock()
		return tx, fmt.Errorf("acl_rule: group %q does not exist", in.GroupId)
	}
	r := cloneAclRule(in)
	r.CreatedTsNs = nowNs()
	s.aclRules[r.Id] = r
	s.mu.Unlock()
	s.publish(tx, dashsimv1.EventType_EVENT_TYPE_CREATED, dashsimv1.ObjectKind_OBJECT_KIND_ACL_RULE, r.Id,
		func(ev *dashsimv1.Event) { ev.Payload = &dashsimv1.Event_AclRule{AclRule: r} })
	return tx, nil
}

// DeleteAclRule removes a rule by id.
func (s *Store) DeleteAclRule(id string) (string, error) {
	tx := s.TxID()
	s.mu.Lock()
	r, ok := s.aclRules[id]
	if !ok {
		s.mu.Unlock()
		return tx, ErrNotFound
	}
	delete(s.aclRules, id)
	s.mu.Unlock()
	s.publish(tx, dashsimv1.EventType_EVENT_TYPE_DELETED, dashsimv1.ObjectKind_OBJECT_KIND_ACL_RULE, id,
		func(ev *dashsimv1.Event) { ev.Payload = &dashsimv1.Event_AclRule{AclRule: r} })
	return tx, nil
}

// ListAclRules returns every rule, ordered by id.
func (s *Store) ListAclRules() []*dashsimv1.AclRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedAclRules(s.aclRules)
}

// -----------------------------------------------------------------------------
// Sorted accessors (used by Snapshot and List* methods)
// -----------------------------------------------------------------------------

func sortedVnets(m map[string]*dashsimv1.Vnet) []*dashsimv1.Vnet {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*dashsimv1.Vnet, 0, len(keys))
	for _, k := range keys {
		out = append(out, cloneVnet(m[k]))
	}
	return out
}

func sortedEnis(m map[string]*dashsimv1.Eni) []*dashsimv1.Eni {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*dashsimv1.Eni, 0, len(keys))
	for _, k := range keys {
		out = append(out, cloneEni(m[k]))
	}
	return out
}

func sortedVnetMappings(m map[string]*dashsimv1.VnetMapping) []*dashsimv1.VnetMapping {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*dashsimv1.VnetMapping, 0, len(keys))
	for _, k := range keys {
		out = append(out, cloneVnetMapping(m[k]))
	}
	return out
}

func sortedRoutes(m map[string]*dashsimv1.Route) []*dashsimv1.Route {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*dashsimv1.Route, 0, len(keys))
	for _, k := range keys {
		out = append(out, cloneRoute(m[k]))
	}
	return out
}

func sortedAclGroups(m map[string]*dashsimv1.AclGroup) []*dashsimv1.AclGroup {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*dashsimv1.AclGroup, 0, len(keys))
	for _, k := range keys {
		out = append(out, cloneAclGroup(m[k]))
	}
	return out
}

func sortedAclRules(m map[string]*dashsimv1.AclRule) []*dashsimv1.AclRule {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*dashsimv1.AclRule, 0, len(keys))
	for _, k := range keys {
		out = append(out, cloneAclRule(m[k]))
	}
	return out
}
