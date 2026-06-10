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
	// PA-1 contract: Allow returns true for every (method, subject) pair
	// because auth.mode=none means "auth off". PD will replace this; the
	// test guards against accidental early-enforcement.
	rm := NewRoleMap()
	rm.Register(RPCInfo{Method: "/test", Access: AccessRead, AllowedRoles: []string{RoleAdmin}})

	if !rm.Allow("/test", Anonymous) {
		t.Error("Allow(/test, Anonymous) = false; want true (PA-1 contract)")
	}
	if !rm.Allow("/not-registered", Anonymous) {
		t.Error("Allow(/not-registered, Anonymous) = false; want true (PA-1 contract)")
	}
	if !rm.Allow("/test", Subject{Role: "wrong-role"}) {
		t.Error("Allow returned false for wrong role; PA-1 must allow all")
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

func TestNewListener_TokenAndMTLSRejectedInPA1(t *testing.T) {
	for _, mode := range []string{"token", "mtls"} {
		_, err := NewListener("tcp", "127.0.0.1:0", ListenerConfig{Mode: mode})
		if err == nil {
			t.Errorf("NewListener(%q) should error in PA-1; config validator should prevent this code path", mode)
		}
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
