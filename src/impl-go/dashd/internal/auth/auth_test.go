// Tests for the PA-1 no-op auth stubs. These guard the contract PD will
// rely on: FromContext returns Anonymous when nothing is injected,
// WithSubject + FromContext round-trip, AllowAllAuthorizer accepts every
// request, and NewListener returns a real net.Listener under mode=none.
package auth

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc"
)

// --- Subject + context helpers ---

func TestFromContext_AnonymousByDefault(t *testing.T) {
	s := FromContext(context.Background())
	if s.Name != Anonymous.Name {
		t.Fatalf("Anonymous default: name = %q; want %q", s.Name, Anonymous.Name)
	}
	if s.Role != "" {
		t.Errorf("Anonymous Role = %q; want empty", s.Role)
	}
}

func TestWithSubject_RoundTrip(t *testing.T) {
	want := Subject{Name: "alice", Role: RoleAdmin, Namespace: "prod"}
	ctx := WithSubject(context.Background(), want)
	got := FromContext(ctx)
	if got != want {
		t.Fatalf("round-trip subject: got %+v; want %+v", got, want)
	}
}

func TestWithSubject_NestedOverride(t *testing.T) {
	ctx := WithSubject(context.Background(), Subject{Name: "outer"})
	ctx = WithSubject(ctx, Subject{Name: "inner"})
	if got := FromContext(ctx); got.Name != "inner" {
		t.Fatalf("nested WithSubject did not override: got %q; want inner", got.Name)
	}
}

// --- RoleMap ---

func TestRoleMap_RegisterLookup(t *testing.T) {
	rm := NewRoleMap()
	info := RPCInfo{
		Method:       "/dashcenter.v1.Test/Method",
		Access:       AccessWrite,
		AllowedRoles: []string{RoleAdmin},
	}
	rm.Register(info)

	if got, ok := rm.Lookup(info.Method); !ok || got.Access != AccessWrite {
		t.Fatalf("Lookup after Register: got=%+v ok=%v", got, ok)
	}
	if _, ok := rm.Lookup("/nonexistent"); ok {
		t.Error("Lookup of unregistered method returned ok=true")
	}
	if rm.MethodCount() != 1 {
		t.Errorf("MethodCount = %d; want 1", rm.MethodCount())
	}
}

func TestRoleMap_AllowAlwaysTrueInPA1(t *testing.T) {
	// PA-1 contract: Allow returns true (auth.mode=none). Kept as the
	// public Allow() signature; PD's closed-default lives on the new
	// AllowMethod() — see TestRoleMap_AllowMethod_ClosedDefault.
	rm := NewRoleMap()
	rm.Register(RPCInfo{Method: "/test", Access: AccessRead, AllowedRoles: []string{RoleAdmin}})
	if !rm.Allow("/test", Anonymous) {
		t.Error("Allow(/test, Anonymous) = false; want true (PA-1 compat)")
	}
}

// PD-G3: closed-default RBAC. AllowMethod returns false for unregistered
// methods and for subjects whose role is not in AllowedRoles. Admin is
// implicitly permitted everywhere.
func TestRoleMap_AllowMethod_ClosedDefault(t *testing.T) {
	rm := NewRoleMap()
	rm.Register(RPCInfo{Method: "/test", Access: AccessWrite, AllowedRoles: []string{RoleOperator}})

	if rm.AllowMethod("/not-registered", Subject{Role: RoleOperator}) {
		t.Error("AllowMethod for unregistered method must be false (closed-default)")
	}
	if rm.AllowMethod("/test", Subject{Role: RoleViewer}) {
		t.Error("AllowMethod /test with viewer must be false (not in AllowedRoles)")
	}
	if !rm.AllowMethod("/test", Subject{Role: RoleOperator}) {
		t.Error("AllowMethod /test with operator must be true")
	}
	if !rm.AllowMethod("/not-registered", Subject{Role: RoleAdmin}) {
		t.Error("AllowMethod admin must be allowed even on unregistered methods")
	}
}

func TestAccessLevel_String(t *testing.T) {
	if AccessRead.String() != "read" {
		t.Errorf("AccessRead.String() = %q; want read", AccessRead.String())
	}
	if AccessWrite.String() != "write" {
		t.Errorf("AccessWrite.String() = %q; want write", AccessWrite.String())
	}
	if AccessLevel(99).String() != "unknown" {
		t.Errorf("AccessLevel(99).String() = %q; want unknown", AccessLevel(99).String())
	}
}

// --- Authorizer ---

func TestAllowAllAuthorizer_AlwaysAnonymousNoError(t *testing.T) {
	subj, err := AllowAllAuthorizer{}.Authorize(context.Background(), "/anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subj.Name != Anonymous.Name {
		t.Errorf("got subject %q; want Anonymous", subj.Name)
	}
}

// --- NewListener ---

func TestNewListener_NoneReturnsPlainListener(t *testing.T) {
	ln, err := NewListener("tcp", "127.0.0.1:0", ListenerConfig{Mode: "none"})
	if err != nil {
		t.Fatalf("NewListener(none): %v", err)
	}
	defer ln.Close()
	if _, ok := ln.(*net.TCPListener); !ok {
		t.Errorf("expected *net.TCPListener for mode=none, got %T", ln)
	}
}

func TestNewListener_EmptyModeBehavesAsNone(t *testing.T) {
	ln, err := NewListener("tcp", "127.0.0.1:0", ListenerConfig{})
	if err != nil {
		t.Fatalf("NewListener(zero-config): %v", err)
	}
	defer ln.Close()
	if _, ok := ln.(*net.TCPListener); !ok {
		t.Errorf("expected *net.TCPListener for zero config, got %T", ln)
	}
}

func TestNewListener_TokenWithoutTLSMaterialIsPlain(t *testing.T) {
	// PD: token mode without cert/key falls through to plain net.Listen
	// so operators can run dashd behind an upstream TLS terminator.
	ln, err := NewListener("tcp", "127.0.0.1:0", ListenerConfig{Mode: "token"})
	if err != nil {
		t.Fatalf("NewListener(token, no TLS): %v", err)
	}
	defer ln.Close()
	if _, ok := ln.(*net.TCPListener); !ok {
		t.Errorf("expected *net.TCPListener for token-without-TLS; got %T", ln)
	}
}

func TestNewListener_MTLSWithoutTLSMaterialErrors(t *testing.T) {
	// PD: mTLS requires server cert/key + CA; missing material errors
	// loudly instead of silently downgrading.
	_, err := NewListener("tcp", "127.0.0.1:0", ListenerConfig{Mode: "mtls"})
	if err == nil {
		t.Error("NewListener(mtls) without material should error")
	}
}

func TestNewListener_RejectsTokenWithUnpairedKey(t *testing.T) {
	_, err := NewListener("tcp", "127.0.0.1:0", ListenerConfig{Mode: "token", CertFile: "/no/such/cert"})
	if err == nil {
		t.Error("NewListener(token) with unpaired cert should error")
	}
}

// --- gRPC + HTTP interceptors ---

func TestUnaryServerInterceptor_InjectsAnonymousSubject(t *testing.T) {
	interceptor := NewUnaryServerInterceptor(AllowAllAuthorizer{})

	called := false
	handler := func(ctx context.Context, req any) (any, error) {
		called = true
		s := FromContext(ctx)
		if s.Name != Anonymous.Name {
			t.Errorf("handler got subject %q; want Anonymous", s.Name)
		}
		return "ok", nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/dashcenter.v1.Test/Method"}
	resp, err := interceptor(context.Background(), "req", info, handler)
	if err != nil {
		t.Fatalf("interceptor err: %v", err)
	}
	if resp != "ok" {
		t.Errorf("got resp %v; want ok", resp)
	}
	if !called {
		t.Error("handler not invoked")
	}
}

func TestHTTPMiddleware_InjectsAnonymousSubject(t *testing.T) {
	mw := NewHTTPMiddleware(AllowAllAuthorizer{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := FromContext(r.Context())
		if s.Name != Anonymous.Name {
			t.Errorf("handler got subject %q; want Anonymous", s.Name)
		}
		_, _ = w.Write([]byte("ok"))
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/vnets", nil)
	rec := httptest.NewRecorder()
	mw(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("body = %q; want ok", rec.Body.String())
	}
}

func TestUnaryServerInterceptor_NilAuthorizerDefaultsToAllowAll(t *testing.T) {
	interceptor := NewUnaryServerInterceptor(nil) // PA-1: nil is OK

	resp, err := interceptor(context.Background(), "req",
		&grpc.UnaryServerInfo{FullMethod: "/x/Y"},
		func(ctx context.Context, req any) (any, error) { return "ok", nil })
	if err != nil {
		t.Fatalf("nil authorizer should default to AllowAll: %v", err)
	}
	if resp != "ok" {
		t.Errorf("got %v; want ok", resp)
	}
}
