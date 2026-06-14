// PD-G4 denial auditing: verifies that the DenyAuditor callback fires
// exactly once on the deny path for both HTTP and gRPC middleware, and
// that the allow path NEVER calls it.
package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"google.golang.org/grpc"
)

// denyAuthorizer always returns ErrPermissionDenied so we can assert
// the deny callback fires.
type denyAuthorizer struct{}

func (denyAuthorizer) Authorize(ctx context.Context, method string) (Subject, error) {
	return Anonymous, ErrPermissionDenied
}

// unauthAuthorizer always returns ErrUnauthenticated.
type unauthAuthorizer struct{}

func (unauthAuthorizer) Authorize(ctx context.Context, method string) (Subject, error) {
	return Anonymous, ErrUnauthenticated
}

type denyCall struct {
	method string
	code   int
	actor  string
	errMsg string
}

type denyRecorder struct {
	mu    sync.Mutex
	calls []denyCall
}

func (r *denyRecorder) cb() DenyAuditor {
	return func(method string, code int, actor string, err error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		r.calls = append(r.calls, denyCall{method, code, actor, msg})
	}
}

func TestHTTPMiddleware_DenyAuditor_403(t *testing.T) {
	rec := &denyRecorder{}
	mw := NewHTTPMiddleware(denyAuthorizer{}, WithDenyAuditor(rec.cb()))

	handlerCalled := false
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/vnets", nil)
	rr := httptest.NewRecorder()
	mw(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d; want 403", rr.Code)
	}
	if handlerCalled {
		t.Error("handler must not run on deny")
	}
	if len(rec.calls) != 1 {
		t.Fatalf("want 1 deny call, got %d (%+v)", len(rec.calls), rec.calls)
	}
	if got := rec.calls[0]; got.code != http.StatusForbidden || got.actor != Anonymous.Name || got.errMsg == "" {
		t.Errorf("call = %+v", got)
	}
}

func TestHTTPMiddleware_DenyAuditor_401(t *testing.T) {
	rec := &denyRecorder{}
	mw := NewHTTPMiddleware(unauthAuthorizer{}, WithDenyAuditor(rec.cb()))

	req := httptest.NewRequest(http.MethodGet, "/v1/vnets", nil)
	rr := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401", rr.Code)
	}
	if len(rec.calls) != 1 || rec.calls[0].code != http.StatusUnauthorized {
		t.Fatalf("want one 401 deny call, got %+v", rec.calls)
	}
}

func TestHTTPMiddleware_DenyAuditor_NotCalledOnAllow(t *testing.T) {
	rec := &denyRecorder{}
	mw := NewHTTPMiddleware(AllowAllAuthorizer{}, WithDenyAuditor(rec.cb()))

	req := httptest.NewRequest(http.MethodGet, "/v1/vnets", nil)
	rr := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rr.Code)
	}
	if len(rec.calls) != 0 {
		t.Errorf("allow path must not call deny auditor; got %+v", rec.calls)
	}
}

func TestUnaryServerInterceptor_DenyAuditor_PermissionDenied(t *testing.T) {
	rec := &denyRecorder{}
	interceptor := NewUnaryServerInterceptor(denyAuthorizer{}, WithDenyAuditor(rec.cb()))

	handlerCalled := false
	handler := func(ctx context.Context, req any) (any, error) {
		handlerCalled = true
		return nil, nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/dashcenter.v1.Test/Method"}
	_, err := interceptor(context.Background(), "req", info, handler)
	if err == nil {
		t.Fatal("want error on deny")
	}
	if handlerCalled {
		t.Error("handler must not run on deny")
	}
	if len(rec.calls) != 1 {
		t.Fatalf("want 1 deny call, got %d (%+v)", len(rec.calls), rec.calls)
	}
	if got := rec.calls[0]; got.code != http.StatusForbidden || got.method != info.FullMethod {
		t.Errorf("call = %+v", got)
	}
}

func TestUnaryServerInterceptor_DenyAuditor_Unauthenticated(t *testing.T) {
	rec := &denyRecorder{}
	interceptor := NewUnaryServerInterceptor(unauthAuthorizer{}, WithDenyAuditor(rec.cb()))
	info := &grpc.UnaryServerInfo{FullMethod: "/dashcenter.v1.Test/Method"}
	_, err := interceptor(context.Background(), "req", info,
		func(ctx context.Context, req any) (any, error) { return nil, nil })
	if err == nil {
		t.Fatal("want error on deny")
	}
	if len(rec.calls) != 1 || rec.calls[0].code != http.StatusUnauthorized {
		t.Fatalf("want one 401 deny call, got %+v", rec.calls)
	}
}

func TestUnaryServerInterceptor_DenyAuditor_NotCalledOnAllow(t *testing.T) {
	rec := &denyRecorder{}
	interceptor := NewUnaryServerInterceptor(AllowAllAuthorizer{}, WithDenyAuditor(rec.cb()))
	_, err := interceptor(context.Background(), "req",
		&grpc.UnaryServerInfo{FullMethod: "/x/Y"},
		func(ctx context.Context, req any) (any, error) { return "ok", nil })
	if err != nil {
		t.Fatalf("allow path errored: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("allow path must not call deny auditor; got %+v", rec.calls)
	}
}
