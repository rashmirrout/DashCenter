// PD-G4 audit interceptor — sits AFTER the auth interceptor in the
// chain so the Subject is already in ctx via auth.FromContext.
//
// Records one Entry per RPC. Read-only methods are skipped when
// cfg.IncludeReads is false (default), keeping the audit log focused
// on the mutating ops auditors actually care about.
package audit

import (
	"context"
	"net/http"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// InterceptorConfig tunes audit interception.
type InterceptorConfig struct {
	// Writer is the destination for entries. nil disables auditing
	// (interceptor becomes a pass-through).
	Writer *Writer

	// Roles is the central role map; consulted to decide read vs write.
	// nil falls back to auth.DefaultRoleMap.
	Roles *auth.RoleMap

	// IncludeReads, when true, audits read-only RPCs too. Default
	// false — production should keep this off to bound log volume.
	IncludeReads bool
}

// UnaryInterceptor returns a gRPC unary interceptor. Place it AFTER
// the auth interceptor in grpc.ChainUnaryInterceptor so the Subject
// is already in ctx.
func UnaryInterceptor(cfg InterceptorConfig) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		resp, err := handler(ctx, req)
		writeEntry(cfg, ctx, info.FullMethod, err)
		return resp, err
	}
}

// StreamInterceptor returns a gRPC stream interceptor. Records the
// entry when the stream terminates (so a long-lived watch produces
// one log row per session).
func StreamInterceptor(cfg InterceptorConfig) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		err := handler(srv, ss)
		writeEntry(cfg, ss.Context(), info.FullMethod, err)
		return err
	}
}

// HTTPMiddleware returns net/http middleware that emits one audit entry
// per request after the handler returns. Synthetic method name matches
// auth.NewHTTPMiddleware's format ("/REST" + path + "/" + verb) so a
// single role map covers both transports.
func HTTPMiddleware(cfg InterceptorConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			method := "/REST" + r.URL.Path + "/" + r.Method
			entryFromHTTP(cfg, r.Context(), method, rec.status)
		})
	}
}

// --- helpers ---------------------------------------------------------

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func writeEntry(cfg InterceptorConfig, ctx context.Context, method string, err error) {
	if cfg.Writer == nil {
		return
	}
	if !shouldAudit(cfg, method) {
		return
	}
	subj := auth.FromContext(ctx)
	e := Entry{
		Actor:  subj.Name,
		Role:   subj.Role,
		Method: method,
		OK:     err == nil,
	}
	if err != nil {
		st, _ := status.FromError(err)
		e.Code = st.Code().String()
		e.Error = truncate(err.Error(), 256)
	}
	// Best-effort: a failed audit append must never surface to the
	// caller. We deliberately swallow here so a broken audit log
	// cannot DOS dashd.
	_ = cfg.Writer.Append(e)
}

func entryFromHTTP(cfg InterceptorConfig, ctx context.Context, method string, status int) {
	if cfg.Writer == nil {
		return
	}
	if !shouldAudit(cfg, method) {
		return
	}
	subj := auth.FromContext(ctx)
	e := Entry{
		Actor:  subj.Name,
		Role:   subj.Role,
		Method: method,
		OK:     status >= 200 && status < 300,
		Code:   httpCodeString(status),
	}
	_ = cfg.Writer.Append(e)
}

func shouldAudit(cfg InterceptorConfig, method string) bool {
	if cfg.IncludeReads {
		return true
	}
	rm := cfg.Roles
	if rm == nil {
		rm = auth.DefaultRoleMap
	}
	info, ok := rm.Lookup(method)
	if !ok {
		// Unknown method — record it. An unmapped RPC is a security
		// signal in its own right.
		return true
	}
	return info.Access == auth.AccessWrite
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func httpCodeString(s int) string {
	switch {
	case s >= 200 && s < 300:
		return "OK"
	case s == 400:
		return "InvalidArgument"
	case s == 401:
		return "Unauthenticated"
	case s == 403:
		return "PermissionDenied"
	case s == 404:
		return "NotFound"
	case s == 409:
		return "Conflict"
	case s == 412:
		return "FailedPrecondition"
	case s == 429:
		return "ResourceExhausted"
	case s >= 500:
		return "Internal"
	}
	return "Unknown"
}

// DenyAuditor returns an auth.DenyAuditor closure bound to the given
// Writer. Use this to wire deny-side auditing into the auth middleware
// / interceptors:
//
//	authMW := auth.NewHTTPMiddleware(authz,
//	  auth.WithDenyAuditor(audit.DenyAuditor(auditWriter)))
//
// Each call emits one Entry with OK=false and a Code derived from the
// HTTP-equivalent status. nil writer returns a nil DenyAuditor so the
// auth middleware's nil-guard skips the call.
func DenyAuditor(w *Writer) auth.DenyAuditor {
	if w == nil {
		return nil
	}
	return func(method string, code int, actor string, err error) {
		role := ""
		// Best effort: bearer/cn prefix in actor — no Subject ctx yet
		// because deny happened before the ctx was enriched.
		e := Entry{
			Actor:  actor,
			Role:   role,
			Method: method,
			OK:     false,
			Code:   httpCodeString(code),
		}
		if err != nil {
			e.Error = truncate(err.Error(), 256)
		}
		_ = w.Append(e)
	}
}
