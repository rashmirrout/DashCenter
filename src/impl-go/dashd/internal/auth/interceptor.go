// gRPC and REST interceptors / middleware.
//
// AC-4 / AC-5 require every dashd RPC handler to be reached through the
// shared interceptor chains. PA-1 shipped pass-through implementations;
// PD activates real Authorizer dispatch. The function signatures and
// type names are unchanged so callers wired in PA/PB/PC keep working.
package auth

import (
	"context"
	"net/http"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Authorizer is the contract PD's real interceptor will implement.
// PA-1 ships AllowAllAuthorizer as the default — it lets every request
// through with the Anonymous Subject.
//
// The interface accepts a context (carries deadline + Subject), the gRPC
// method name (or REST synthetic name), and returns either the
// authenticated Subject + nil, or Anonymous + an error that the
// interceptor maps to UNAUTHENTICATED / PERMISSION_DENIED.
type Authorizer interface {
	Authorize(ctx context.Context, method string) (Subject, error)
}

// AllowAllAuthorizer is PA-1's no-op Authorizer. It always returns the
// Anonymous Subject and nil. PD replaces the singleton in main.go with a
// TokenAuthorizer or MTLSAuthorizer.
type AllowAllAuthorizer struct{}

// Authorize implements the Authorizer interface. PA-1 always succeeds.
func (AllowAllAuthorizer) Authorize(_ context.Context, _ string) (Subject, error) {
	return Anonymous, nil
}

// DenyAuditor is called once per Authorize-deny so the audit log can
// record 401/403 attempts even though the request never reached the
// handler (the existing audit middleware sits BEHIND auth, so denies
// short-circuit and skip auditing without this hook).
//
// `code` is the HTTP-equivalent status (401 / 403). For gRPC it's
// derived from the status code: codes.Unauthenticated → 401,
// codes.PermissionDenied → 403, anything else → 401 (treated as
// authentication failure for forensic clarity).
//
// `actor` is best-effort: the bearer-prefix or client-CN derived from
// the request context, or "anonymous" if neither is present.
//
// `err` carries the underlying ErrUnauthenticated / ErrPermissionDenied
// (or any custom error returned by the Authorizer). Callers may format
// it into Entry.Error.
//
// The callback runs synchronously on every deny — keep it cheap. A
// nil DenyAuditor is treated as the no-op default.
type DenyAuditor func(method string, code int, actor string, err error)

// MiddlewareOption configures NewHTTPMiddleware /
// NewUnaryServerInterceptor / NewStreamServerInterceptor. Functional
// options keep the existing single-argument constructors
// backward-compatible while letting PD wire deny-side auditing.
type MiddlewareOption func(*middlewareOpts)

type middlewareOpts struct {
	deny DenyAuditor
}

// WithDenyAuditor wires a DenyAuditor callback so the middleware emits
// one audit row per 401 / 403 short-circuit.
func WithDenyAuditor(fn DenyAuditor) MiddlewareOption {
	return func(o *middlewareOpts) { o.deny = fn }
}

func collectMiddlewareOpts(opts []MiddlewareOption) middlewareOpts {
	var mo middlewareOpts
	for _, o := range opts {
		if o != nil {
			o(&mo)
		}
	}
	return mo
}

// NewUnaryServerInterceptor returns a gRPC unary interceptor that calls
// the supplied Authorizer before delegating to the handler. PA-1 with
// AllowAllAuthorizer is a pass-through; PD swaps the Authorizer for a
// real one without touching this function.
//
// The interceptor injects the verified Subject into the context via
// WithSubject so handlers can recover it via FromContext (AC-1).
//
// Optional MiddlewareOptions wire deny-side auditing (WithDenyAuditor).
func NewUnaryServerInterceptor(a Authorizer, opts ...MiddlewareOption) grpc.UnaryServerInterceptor {
	if a == nil {
		a = AllowAllAuthorizer{}
	}
	mo := collectMiddlewareOpts(opts)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		subj, err := a.Authorize(ctx, info.FullMethod)
		if err != nil {
			if mo.deny != nil {
				mo.deny(info.FullMethod, grpcErrorToHTTPCode(err), actorFromContext(ctx), err)
			}
			return nil, err
		}
		return handler(WithSubject(ctx, subj), req)
	}
}

// NewStreamServerInterceptor returns a gRPC stream interceptor that
// calls the Authorizer once before opening the stream. Same swap-for-PD
// pattern as NewUnaryServerInterceptor.
func NewStreamServerInterceptor(a Authorizer, opts ...MiddlewareOption) grpc.StreamServerInterceptor {
	if a == nil {
		a = AllowAllAuthorizer{}
	}
	mo := collectMiddlewareOpts(opts)
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		subj, err := a.Authorize(ss.Context(), info.FullMethod)
		if err != nil {
			if mo.deny != nil {
				mo.deny(info.FullMethod, grpcErrorToHTTPCode(err), actorFromContext(ss.Context()), err)
			}
			return err
		}
		// Wrap the stream so handlers see a ctx carrying the Subject.
		wrapped := wrappedServerStream{ServerStream: ss, ctx: WithSubject(ss.Context(), subj)}
		return handler(srv, wrapped)
	}
}

// wrappedServerStream overrides Context() to return a ctx with the
// Subject injected.
type wrappedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w wrappedServerStream) Context() context.Context { return w.ctx }

// NewHTTPMiddleware returns net/http middleware that calls the
// Authorizer for each REST request. PA-1 with AllowAllAuthorizer is a
// pass-through. PD replaces it with the same shape.
//
// The middleware injects the Subject into the request's context so
// handlers can read it via FromContext.
//
// Optional MiddlewareOptions wire deny-side auditing
// (WithDenyAuditor) so 401/403 short-circuits still produce an audit
// row even though the downstream audit middleware never gets to run.
func NewHTTPMiddleware(a Authorizer, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	if a == nil {
		a = AllowAllAuthorizer{}
	}
	mo := collectMiddlewareOpts(opts)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Pre-extract credentials from the HTTP request so the
			// Authorizer's gRPC-oriented bearer/cn extractors find
			// something in ctx.
			ctx := r.Context()
			if hdr := r.Header.Get("Authorization"); hdr != "" {
				ctx = context.WithValue(ctx, bearerHeaderKey{}, hdr)
			}
			if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
				ctx = context.WithValue(ctx, clientCNKey{}, r.TLS.PeerCertificates[0].Subject.CommonName)
			}

			// Synthesise a method name from the REST verb + path so the
			// Authorizer can apply the same role table to both transports.
			method := "/REST" + r.URL.Path + "/" + r.Method
			subj, err := a.Authorize(ctx, method)
			if err != nil {
				code := http.StatusUnauthorized
				if err == ErrPermissionDenied {
					code = http.StatusForbidden
				}
				if mo.deny != nil {
					mo.deny(method, code, actorFromContext(ctx), err)
				}
				http.Error(w, err.Error(), code)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithSubject(ctx, subj)))
		})
	}
}

// cnFromTLSInfo isolates the credentials.TLSInfo type lookup from the
// rest of the auth package. Returns the client cert CommonName or "".
func cnFromTLSInfo(ai credentials.AuthInfo) string {
	tlsInfo, ok := ai.(credentials.TLSInfo)
	if !ok {
		return ""
	}
	if len(tlsInfo.State.PeerCertificates) == 0 {
		return ""
	}
	return tlsInfo.State.PeerCertificates[0].Subject.CommonName
}

// actorFromContext is the best-effort identity extractor used by the
// DenyAuditor. Returns the first non-empty of (mTLS CN, bearer prefix,
// "anonymous"). Never blocks, never errors.
func actorFromContext(ctx context.Context) string {
	if cn := clientCNFromContext(ctx); cn != "" {
		return "cn:" + cn
	}
	if tok := bearerFromContext(ctx); tok != "" {
		// Token-prefix only — never log the full token. 8 chars is
		// enough to correlate with the operator's own log.
		if len(tok) > 8 {
			return "bearer:" + tok[:8] + "\u2026"
		}
		return "bearer:" + tok
	}
	return Anonymous.Name
}

// grpcErrorToHTTPCode maps an Authorizer error to the HTTP-equivalent
// status integer the DenyAuditor uses. Mirrors REST's mapping so audit
// rows look identical across transports.
func grpcErrorToHTTPCode(err error) int {
	if err == ErrPermissionDenied {
		return http.StatusForbidden
	}
	return http.StatusUnauthorized
}
