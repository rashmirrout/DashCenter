// Coverage-pushing tests for the PA-1 stubs. Targets the
// NewStreamServerInterceptor, HTTP middleware error path, and a couple
// of zero-value defaults that the core suite doesn't hit.
package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc"
)

// erroringAuthorizer always fails; used to drive the error path of
// interceptors/middleware that AllowAllAuthorizer cannot reach.
type erroringAuthorizer struct{}

func (erroringAuthorizer) Authorize(_ context.Context, _ string) (Subject, error) {
	return Anonymous, errors.New("forbidden")
}

// fakeStream is a minimal grpc.ServerStream that lets us observe the
// context the wrapped stream produces — that's the contract
// NewStreamServerInterceptor must preserve.
type fakeStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (f *fakeStream) Context() context.Context { return f.ctx }
func (f *fakeStream) SendMsg(_ any) error      { return nil }
func (f *fakeStream) RecvMsg(_ any) error      { return nil }

func TestStreamServerInterceptor_InjectsSubject(t *testing.T) {
	interceptor := NewStreamServerInterceptor(AllowAllAuthorizer{})

	stream := &fakeStream{ctx: context.Background()}
	called := false
	handler := func(_ any, ss grpc.ServerStream) error {
		called = true
		// The wrapped stream MUST carry the Anonymous subject.
		if s := FromContext(ss.Context()); s.Name != Anonymous.Name {
			t.Errorf("subject in stream ctx = %q; want %q", s.Name, Anonymous.Name)
		}
		return nil
	}

	info := &grpc.StreamServerInfo{FullMethod: "/dashcenter.v1.Test/StreamMethod"}
	if err := interceptor(nil, stream, info, handler); err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	if !called {
		t.Fatal("handler not invoked")
	}
}

func TestStreamServerInterceptor_NilAuthorizerDefaultsToAllowAll(t *testing.T) {
	interceptor := NewStreamServerInterceptor(nil)
	stream := &fakeStream{ctx: context.Background()}
	err := interceptor(nil, stream, &grpc.StreamServerInfo{FullMethod: "/x/Y"},
		func(_ any, _ grpc.ServerStream) error { return nil })
	if err != nil {
		t.Fatalf("nil authorizer should default to AllowAll: %v", err)
	}
}

func TestStreamServerInterceptor_AuthorizerError(t *testing.T) {
	interceptor := NewStreamServerInterceptor(erroringAuthorizer{})
	stream := &fakeStream{ctx: context.Background()}
	err := interceptor(nil, stream, &grpc.StreamServerInfo{FullMethod: "/x/Y"},
		func(_ any, _ grpc.ServerStream) error { return nil })
	if err == nil || err.Error() != "forbidden" {
		t.Fatalf("got err = %v; want 'forbidden'", err)
	}
}

func TestUnaryServerInterceptor_AuthorizerError(t *testing.T) {
	interceptor := NewUnaryServerInterceptor(erroringAuthorizer{})
	_, err := interceptor(context.Background(), "req",
		&grpc.UnaryServerInfo{FullMethod: "/x/Y"},
		func(ctx context.Context, req any) (any, error) { return "ok", nil })
	if err == nil || err.Error() != "forbidden" {
		t.Fatalf("got err = %v; want 'forbidden'", err)
	}
}

func TestHTTPMiddleware_AuthorizerError(t *testing.T) {
	mw := NewHTTPMiddleware(erroringAuthorizer{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not run when authorizer errors")
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/vnets", nil)
	rec := httptest.NewRecorder()
	mw(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401", rec.Code)
	}
}

func TestHTTPMiddleware_NilAuthorizerDefaultsToAllowAll(t *testing.T) {
	mw := NewHTTPMiddleware(nil)
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
	rec := httptest.NewRecorder()
	mw(handler).ServeHTTP(rec, req)
	if !called {
		t.Fatal("handler not invoked under nil authorizer")
	}
}

func TestNewListener_UnsupportedMode(t *testing.T) {
	_, err := NewListener("tcp", "127.0.0.1:0", ListenerConfig{Mode: "kerberos"})
	if err == nil {
		t.Fatal("expected error for unsupported mode")
	}
}
