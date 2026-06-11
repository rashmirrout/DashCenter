// PD-G3 + PD-G1/G2: real Authorizers (token, mTLS) that replace
// AllowAllAuthorizer at runtime. Wiring sits in main.go: when
// cfg.Auth.Mode == "none" the interceptor chain still gets
// AllowAllAuthorizer (unchanged); when "token" it gets TokenAuthorizer
// reading the configured TokenEntry list; when "mtls" it gets
// MTLSAuthorizer + the role table.
//
// Locked decisions:
//
//   * Closed-default RBAC: an RPC that is NOT registered in
//     DefaultRoleMap returns PERMISSION_DENIED. PA-1's open-default
//     allowed every method; PD flips it. New RPCs must be added to
//     roles.go (AC-2 in CONTRIBUTING.md).
//
//   * Tokens are matched against the raw Authorization header
//     ("Bearer <token>"). Constant-time comparison via subtle.
//
//   * mTLS subject identity = the client certificate's CN. CNs are
//     resolved to roles via the auth.roles config table (CN -> role).
//     Unmapped CNs return PERMISSION_DENIED (D14 in impl-phases.md).
//
//   * Both Authorizers do RBAC enforcement inline: they parse the
//     subject, look up the method in RoleMap, and return either the
//     verified Subject or a gRPC status error. The interceptor wraps
//     the verified Subject into ctx via WithSubject.
package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// ErrUnauthenticated is returned by Authorize when no acceptable
// credentials are present. Interceptors map this to grpc UNAUTHENTICATED.
var ErrUnauthenticated = status.Error(codes.Unauthenticated, "auth: no acceptable credentials")

// ErrPermissionDenied is returned by Authorize when credentials are
// valid but the RBAC table does not permit the RPC. Interceptors map
// this to grpc PERMISSION_DENIED.
var ErrPermissionDenied = status.Error(codes.PermissionDenied, "auth: permission denied")

// TokenAuthorizer enforces bearer-token RBAC for gRPC + REST. Wire it
// from main.go when cfg.Auth.Mode == "token".
type TokenAuthorizer struct {
	// Tokens maps the raw bearer token string -> Subject. Built at
	// startup from cfg.Auth.Tokens.
	Tokens map[string]Subject

	// Roles is the RoleMap consulted for RBAC; pass DefaultRoleMap.
	Roles *RoleMap
}

// Authorize returns the Subject if the request carries a known bearer
// token AND the RoleMap permits the method for that Subject's role.
func (a *TokenAuthorizer) Authorize(ctx context.Context, method string) (Subject, error) {
	token := bearerFromContext(ctx)
	if token == "" {
		return Anonymous, ErrUnauthenticated
	}
	subj, ok := matchToken(a.Tokens, token)
	if !ok {
		return Anonymous, ErrUnauthenticated
	}
	if !a.Roles.AllowMethod(method, subj) {
		return subj, ErrPermissionDenied
	}
	return subj, nil
}

// MTLSAuthorizer enforces client-cert RBAC for gRPC + REST. Wire it
// from main.go when cfg.Auth.Mode == "mtls".
type MTLSAuthorizer struct {
	// CNRoles maps client-cert CommonName -> Subject. Built from
	// cfg.Auth.Tokens (where TokenEntry.Token is repurposed as the
	// CN string under mtls mode — keeps the YAML knob set small).
	CNRoles map[string]Subject

	// Roles is the RoleMap consulted for RBAC.
	Roles *RoleMap
}

// Authorize returns the Subject derived from the client cert CN when
// the RoleMap permits the method.
func (a *MTLSAuthorizer) Authorize(ctx context.Context, method string) (Subject, error) {
	cn := clientCNFromContext(ctx)
	if cn == "" {
		return Anonymous, ErrUnauthenticated
	}
	subj, ok := a.CNRoles[cn]
	if !ok {
		// D14: unmapped CN under mTLS -> PERMISSION_DENIED.
		return Anonymous, ErrPermissionDenied
	}
	if !a.Roles.AllowMethod(method, subj) {
		return subj, ErrPermissionDenied
	}
	return subj, nil
}

// --- helpers -----------------------------------------------------------

// bearerFromContext extracts the bearer token from either a gRPC
// metadata "authorization" header or (when wrapped by HTTPMiddleware)
// the REST-injected ctx value bearerHeaderKey{}. Empty string when no
// token is present.
func bearerFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(bearerHeaderKey{}).(string); ok && v != "" {
		return strings.TrimPrefix(v, "Bearer ")
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	vs := md.Get("authorization")
	if len(vs) == 0 {
		return ""
	}
	return strings.TrimPrefix(vs[0], "Bearer ")
}

// matchToken does a constant-time scan over the configured tokens. We
// can't use a single subtle.ConstantTimeCompare because the lookup is
// against an arbitrary set; the loop is constant-time per entry, which
// is the standard practice for small (<1000) token sets.
func matchToken(tokens map[string]Subject, presented string) (Subject, bool) {
	want := []byte(presented)
	for k, subj := range tokens {
		if subtle.ConstantTimeCompare([]byte(k), want) == 1 {
			return subj, true
		}
	}
	return Subject{}, false
}

// clientCNFromContext extracts the client certificate CN from either
// the gRPC peer info or (when wrapped by HTTPMiddleware) the
// REST-injected ctx value clientCNKey{}. Empty string when no cert is
// presented or when the connection is plaintext.
func clientCNFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(clientCNKey{}).(string); ok {
		return v
	}
	p, ok := peer.FromContext(ctx)
	if !ok || p == nil || p.AuthInfo == nil {
		return ""
	}
	return cnFromTLSInfo(p.AuthInfo)
}

// bearerHeaderKey + clientCNKey are unexported context keys used by
// NewHTTPMiddleware to pre-extract the credentials before they reach
// the Authorizer (the REST path doesn't have grpc metadata).
type bearerHeaderKey struct{}
type clientCNKey struct{}

// AllowMethod is the closed-default RBAC check used by the real
// Authorizers. Returns:
//   - true iff the method is registered AND subject.Role is in
//     RPCInfo.AllowedRoles (admin is implicitly allowed everywhere).
//   - false for unregistered methods (defence in depth).
//
// REST-fast-path: methods of the form "/REST/<path>/<HTTP-VERB>" are
// classified by the HTTP verb instead of an exact registry lookup —
// GET/HEAD/OPTIONS are read; everything else is write. This keeps the
// role map focused on the canonical gRPC method names while still
// giving REST clients meaningful RBAC. Operators who need finer
// per-route control can still register an exact REST entry; the exact
// match takes precedence.
//
// AllowMethod replaces the PA-1 Allow which always returned true.
// We keep Allow as a backward-compat alias so existing call sites in
// PA-1 continue to compile.
func (rm *RoleMap) AllowMethod(method string, subject Subject) bool {
	if rm == nil {
		return false
	}
	if subject.Role == RoleAdmin {
		return true
	}
	// Exact registry match wins.
	if info, ok := rm.Lookup(method); ok {
		for _, r := range info.AllowedRoles {
			if r == subject.Role {
				return true
			}
		}
		return false
	}
	// REST-fast-path: synthetic name format from NewHTTPMiddleware is
	// "/REST" + URL.Path + "/" + Method (e.g. "/REST/v1/vnets/v1/GET").
	// We classify by HTTP verb: read verbs allow viewer+operator+admin;
	// write verbs allow operator+admin.
	if strings.HasPrefix(method, "/REST/") {
		verb := lastSegment(method)
		readVerb := verb == "GET" || verb == "HEAD" || verb == "OPTIONS"
		switch subject.Role {
		case RoleViewer:
			return readVerb
		case RoleOperator:
			return true
		}
	}
	return false
}

// lastSegment returns the substring after the final "/" in s.
func lastSegment(s string) string {
	i := strings.LastIndex(s, "/")
	if i < 0 {
		return s
	}
	return s[i+1:]
}

// Sanity assertion: TokenAuthorizer + MTLSAuthorizer implement Authorizer.
var (
	_ Authorizer = (*TokenAuthorizer)(nil)
	_ Authorizer = (*MTLSAuthorizer)(nil)
)

// errNoMatch is an internal sentinel kept for symmetry with future
// expansions (e.g. JWT signing-key rotation). Not currently used by
// any caller; here so future PRs that need to distinguish "header
// missing" from "header malformed" have a stable name to reach for.
var errNoMatch = errors.New("auth: no matching credential")

var _ = errNoMatch
