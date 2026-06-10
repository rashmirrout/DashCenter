// PD: central RPC registry feeding the RoleMap.
//
// Every dashcenter.v1 + REST method dashd accepts is enumerated here
// with its access level + permitted built-in roles. Closed-default
// RBAC (auth.RoleMap.AllowMethod) returns false for any method NOT in
// this list, so a Phase-2 PR that adds a new RPC MUST add a row
// here too (AC-2 in CONTRIBUTING.md).
//
// Convention:
//
//	read    -> viewer, operator, admin
//	write   -> operator, admin       (admin implicit anywhere)
//	admin-only -> admin
//	REST mirrors gRPC: REST entries cover the synthetic name format
//	used by NewHTTPMiddleware: "/REST/v1/<path>/<METHOD>"
//
// REST entries are deliberately path-globbed via short prefixes —
// AllowMethod does an exact-match lookup but main.go's HTTP middleware
// canonicalises the URL to its method-name form before invoking the
// authorizer (see middleware.RESTMethodOf). Until that canonicaliser
// ships in a follow-up, REST endpoints are matched at gRPC granularity
// only: PD installs Token/MTLS Authorizer on the gRPC chain by default,
// and REST runs under the same Authorizer when explicitly wired.
package auth

func init() {
	// --- ControlPlane (most CRUD) ---
	registerW("/dashcenter.v1.ControlPlane/PutVnet")
	registerW("/dashcenter.v1.ControlPlane/PutEni")
	registerW("/dashcenter.v1.ControlPlane/PutVnetMapping")
	registerW("/dashcenter.v1.ControlPlane/PutAclPolicy")
	registerW("/dashcenter.v1.ControlPlane/PutRoutePolicy")
	registerW("/dashcenter.v1.ControlPlane/PutHaSet")
	registerW("/dashcenter.v1.ControlPlane/PutServiceTunnel")
	registerW("/dashcenter.v1.ControlPlane/Delete")
	registerR("/dashcenter.v1.ControlPlane/Get")
	registerR("/dashcenter.v1.ControlPlane/SimulateApply")
	registerW("/dashcenter.v1.ControlPlane/Reconcile")
	registerA("/dashcenter.v1.ControlPlane/PutInventory")
	registerA("/dashcenter.v1.ControlPlane/RegisterDpu")
	registerW("/dashcenter.v1.ControlPlane/ApplyBatch")

	// --- Observability (read-only) ---
	registerR("/dashcenter.v1.ObservabilityService/GetDpuStatus")
	registerR("/dashcenter.v1.ObservabilityService/GetDrift")
	registerR("/dashcenter.v1.ObservabilityService/GetHealth")
	registerR("/dashcenter.v1.ObservabilityService/GetEniPlacement")
	registerR("/dashcenter.v1.ObservabilityService/GetCounters")
	registerR("/dashcenter.v1.ObservabilityService/WatchEvents")
	registerR("/dashcenter.v1.ObservabilityService/GetAuditLog")

	// --- HA (operator + admin for mutating) ---
	registerR("/dashcenter.v1.HaService/GetHaSetState")
	registerR("/dashcenter.v1.HaService/GetHaScopeState")
	registerR("/dashcenter.v1.HaService/WatchHaEvents")
	registerR("/dashcenter.v1.HaService/GetFlowSyncStats")
	registerW("/dashcenter.v1.HaService/TriggerSwitchover")
	registerA("/dashcenter.v1.HaService/TriggerFailover") // admin only — bigger blast radius

	// --- Migration (operator + admin) ---
	registerR("/dashcenter.v1.MigrationService/GetMigrationSession")
	registerR("/dashcenter.v1.MigrationService/ListMigrationSessions")
	registerR("/dashcenter.v1.MigrationService/StreamMigrationSession")
	registerW("/dashcenter.v1.MigrationService/CreateMigrationPlan")
	registerW("/dashcenter.v1.MigrationService/ValidateMigrationPlan")
	registerW("/dashcenter.v1.MigrationService/StartMigrationSession")
	registerW("/dashcenter.v1.MigrationService/AdvanceMigrationPhase")
	registerW("/dashcenter.v1.MigrationService/RollbackMigration")
	registerW("/dashcenter.v1.MigrationService/AbortMigration")
	registerW("/dashcenter.v1.MigrationService/CommitMigration")
	registerA("/dashcenter.v1.MigrationService/ExportMigrationBundle")
	registerA("/dashcenter.v1.MigrationService/ImportMigrationBundle")
}

// registerR registers a read-only RPC: viewer/operator/admin allowed.
func registerR(method string) {
	DefaultRoleMap.Register(RPCInfo{
		Method:       method,
		Access:       AccessRead,
		AllowedRoles: []string{RoleViewer, RoleOperator, RoleAdmin},
	})
}

// registerW registers a write RPC: operator/admin allowed. Viewers
// cannot perform writes.
func registerW(method string) {
	DefaultRoleMap.Register(RPCInfo{
		Method:       method,
		Access:       AccessWrite,
		AllowedRoles: []string{RoleOperator, RoleAdmin},
	})
}

// registerA registers an admin-only RPC. AllowMethod's admin-implicit
// path picks this up, but we list admin here too so the role table
// reports the truth via Lookup.
func registerA(method string) {
	DefaultRoleMap.Register(RPCInfo{
		Method:       method,
		Access:       AccessWrite,
		AllowedRoles: []string{RoleAdmin},
	})
}
