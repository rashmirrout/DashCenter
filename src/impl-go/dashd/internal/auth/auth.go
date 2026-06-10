// Package auth is the dashd authentication + authorization subsystem.
//
// PA-1 ships only the package skeleton: every type is defined, every
// function returns a no-op result that matches today's plaintext
// behaviour. PD will replace the no-ops with real TLS termination
// (NewListener), real bearer/mTLS verification (NewInterceptor), and
// real RBAC enforcement (RoleMap.Allow).
//
// Until PD lands, contributors writing Phase-2 RPCs respect these forward-
// compatibility rules (full text in docs/CONTRIBUTING.md):
//
//	AC-1  handlers take ctx context.Context as the first parameter
//	AC-2  every new RPC is registered in roles.go's RoleMap
//	AC-3  listener creation goes through auth.NewListener (this package)
//	AC-4  gRPC interceptors go through the shared chain in server/grpc
//	AC-5  REST middleware goes through the shared chain in server/rest
//	AC-6  no plaintext credentials in env/yaml that PD won't be able to override
//	AC-7  integration tests run under auth.mode=none (the default)
//	AC-8  every mutating action logs slog with {actor,namespace,kind,name,op,result}
//	AC-9  this package's stubs exist; PRs add one-line roles.go entries
//	AC-10 PR checklist in docs/CONTRIBUTING.md
//
// Today, FromContext on any context returns the Anonymous subject. PD
// will inject a real Subject via WithSubject after verifying the bearer
// or client cert.
package auth

import (
	"context"
)

// Subject describes who is making a request — actor identity, role, and
// (when applicable) the namespace scope of the credential. PD-late
// audit-log entries copy these fields verbatim into JSONL.
//
// The zero value is intentionally meaningful: an empty Subject is
// "anonymous in namespace default with no role". Until PD wires real
// auth, every context yields the anonymous subject via FromContext.
type Subject struct {
	// Name identifies the caller — a username for token auth, the client
	// cert's CN for mTLS, or "anonymous" when auth is disabled.
	Name string

	// Role is one of "viewer", "operator", "admin", or "" for the
	// anonymous subject.
	Role string

	// Namespace, when non-empty, scopes the credential to one tenant.
	// Empty means "any namespace this dashd serves".
	Namespace string
}

// Anonymous is the subject returned by FromContext when no Subject has
// been injected. Today (auth.mode=none) every request runs as Anonymous.
// PD's interceptor replaces this with a verified Subject before the
// handler runs.
var Anonymous = Subject{Name: "anonymous"}

// subjectKey is the unexported context-key type used by WithSubject /
// FromContext. Using a private type prevents accidental key collisions
// across packages.
type subjectKey struct{}

// WithSubject returns a child context carrying the given Subject. PD's
// interceptor calls this once per RPC after verifying credentials.
// Handlers SHOULD NOT call WithSubject directly.
func WithSubject(ctx context.Context, s Subject) context.Context {
	return context.WithValue(ctx, subjectKey{}, s)
}

// FromContext returns the Subject attached to ctx, or Anonymous if none
// was injected. Every Phase-2 RPC handler that needs to know "who's
// calling me?" reads via FromContext — never from a global or a
// per-request HTTP header inspection.
//
// Today (PA-1) every ctx returns Anonymous since PD's interceptor has
// not yet been wired. This contract lets contributors write
// auth-aware code in PA/PB/PC/PE that becomes live the moment PD ships.
func FromContext(ctx context.Context) Subject {
	if s, ok := ctx.Value(subjectKey{}).(Subject); ok {
		return s
	}
	return Anonymous
}
