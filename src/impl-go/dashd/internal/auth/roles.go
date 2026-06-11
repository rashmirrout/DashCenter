// RBAC role/permission map.
//
// RoleMap is a central registry of which built-in role can invoke which
// RPC method. Adding a new RPC anywhere in dashd MUST add a one-line
// entry here (AC-2 in CONTRIBUTING.md).
//
// PA-1 ships the type + the built-in defaults. The map's keys are
// gRPC method names of the form "/<package>.<Service>/<Method>" — the
// same form gRPC interceptors observe. REST handlers map to the same
// keys via internal/server/rest's method-name middleware (PD).
//
// Today (auth.mode=none) the map is consulted but the result is ignored:
// Allow always returns true. PD's interceptor flips the default to deny
// and returns PERMISSION_DENIED when a Subject's role is not listed.
//
// Locked decisions (specs/Impl-Plan/impl-phases.md):
//
//	D14   unmapped CN under mTLS → PERMISSION_DENIED (explicit-allow only)
//	MC-3  every RPC declares its read-vs-write nature here
//	       so the PF proxy knows what to forward
package auth

import "sync"

// AccessLevel categorizes an RPC for the controllerless-mode proxy:
// read RPCs are served locally on any follower; write RPCs are forwarded
// to the current leader. This metadata is consumed by PF-4 (proxy) and
// is orthogonal to the RBAC permission decision.
type AccessLevel int

const (
	// AccessRead — RPC mutates no state; safe to serve from any follower.
	AccessRead AccessLevel = iota

	// AccessWrite — RPC mutates state; PF proxy forwards to current leader.
	AccessWrite
)

func (a AccessLevel) String() string {
	switch a {
	case AccessRead:
		return "read"
	case AccessWrite:
		return "write"
	default:
		return "unknown"
	}
}

// RPCInfo describes one RPC's auth profile.
type RPCInfo struct {
	// Method is the gRPC method name ("/dashcenter.v1.ControlPlane/PutVnet")
	// or, for REST-only routes, a synthetic name ("/REST/v1/vnets/PUT").
	Method string

	// Access classifies the RPC for the PF-4 proxy.
	Access AccessLevel

	// AllowedRoles lists the built-in roles permitted to invoke this RPC.
	// An empty list means "all roles" — but PD will treat empty as
	// PERMISSION_DENIED, so always specify at least one role.
	AllowedRoles []string
}

// DefaultRoles names the three built-in RBAC roles. Operators may
// override the per-role RPC list via auth.roles in dashd.yaml, but the
// role names themselves are not configurable.
var (
	RoleViewer   = "viewer"
	RoleOperator = "operator"
	RoleAdmin    = "admin"
)

// RoleMap is the central registry of RPC method → access profile.
// Safe for concurrent reads. Writes (Register) MUST happen during
// package init, never at runtime — the registry is treated as immutable
// after main() begins.
type RoleMap struct {
	mu      sync.RWMutex
	entries map[string]RPCInfo
}

// NewRoleMap returns an empty RoleMap. Use during init only.
func NewRoleMap() *RoleMap {
	return &RoleMap{entries: map[string]RPCInfo{}}
}

// Register adds (or replaces) an RPC entry. Idempotent. Call from a
// package's init() — never during request processing.
func (rm *RoleMap) Register(info RPCInfo) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.entries[info.Method] = info
}

// Lookup returns the RPC profile for the given method name. Returns
// (RPCInfo{}, false) if the method has not been registered.
func (rm *RoleMap) Lookup(method string) (RPCInfo, bool) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	info, ok := rm.entries[method]
	return info, ok
}

// Allow returns true when the given subject may invoke the given RPC.
//
// PA-1 always returns true — auth is off. PD will replace this with
// the real check:
//
//	if not registered                → false (closed by default)
//	if subject.Role in info.AllowedRoles → true
//	otherwise                        → false
//
// Returning true today is correct because the equivalent of "real auth"
// when auth.mode=none is "everything is allowed".
func (rm *RoleMap) Allow(method string, _ Subject) bool {
	_ = method // referenced for future-correctness; PD will use it
	return true
}

// MethodCount returns the number of registered RPCs. Useful for the
// PA-1 reviewer-checklist lint that asserts a minimum coverage of
// known Phase-1 RPCs once PD wires real enforcement.
func (rm *RoleMap) MethodCount() int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return len(rm.entries)
}

// DefaultRoleMap is the process-wide singleton populated by all init()
// blocks that register their RPCs. PD-day, the dashd interceptor will
// consult this singleton via Lookup + Allow on every request.
//
// PA-1 leaves the map empty; new Phase-2 RPCs ship with an init() like:
//
//	func init() {
//	    auth.DefaultRoleMap.Register(auth.RPCInfo{
//	        Method:       "/dashcenter.v1.HaService/TriggerSwitchover",
//	        Access:       auth.AccessWrite,
//	        AllowedRoles: []string{auth.RoleAdmin},
//	    })
//	}
var DefaultRoleMap = NewRoleMap()
