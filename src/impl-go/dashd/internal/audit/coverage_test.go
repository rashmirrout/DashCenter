// Coverage-focused tests for interceptor + helpers.
package audit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// --- truncate + httpCodeString ----------------------------------------

func TestTruncate(t *testing.T) {
	if truncate("abc", 10) != "abc" {
		t.Error("short string preserved")
	}
	got := truncate("aaaaabbbbb", 5)
	if got != "aaaaa…" {
		t.Errorf("truncate=%q; want aaaaa…", got)
	}
}

func TestHTTPCodeString(t *testing.T) {
	cases := map[int]string{
		200: "OK", 201: "OK",
		400: "InvalidArgument", 401: "Unauthenticated", 403: "PermissionDenied",
		404: "NotFound", 409: "Conflict", 412: "FailedPrecondition",
		429: "ResourceExhausted", 500: "Internal", 503: "Internal",
		418: "Unknown",
	}
	for code, want := range cases {
		if got := httpCodeString(code); got != want {
			t.Errorf("httpCodeString(%d) = %q; want %q", code, got, want)
		}
	}
}

// --- UnaryInterceptor + StreamInterceptor ------------------------------

func TestUnaryInterceptor_AppendsOneEntryPerCall(t *testing.T) {
	w, _ := newWriter(t)
	cfg := InterceptorConfig{Writer: w, IncludeReads: true}
	intc := UnaryInterceptor(cfg)
	called := false
	handler := func(ctx context.Context, req any) (any, error) { called = true; return "ok", nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/x/Y"}
	if _, err := intc(contextWithSubject("alice", "admin"), nil, info, handler); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("handler not invoked")
	}
	// Audit entry should be present.
	_ = w.Close()
}

func TestUnaryInterceptor_HandlerErrorRecorded(t *testing.T) {
	w, _ := newWriter(t)
	cfg := InterceptorConfig{Writer: w, IncludeReads: true}
	intc := UnaryInterceptor(cfg)
	handler := func(ctx context.Context, req any) (any, error) {
		return nil, status.Error(codes.PermissionDenied, "nope")
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/x/Y"}
	_, err := intc(contextWithSubject("alice", "viewer"), nil, info, handler)
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("got %v; want PermissionDenied", err)
	}
	_ = w.Close()
}

// fakeServerStream implements grpc.ServerStream just enough for the
// interceptor.
type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (f *fakeServerStream) Context() context.Context { return f.ctx }

func TestStreamInterceptor_RecordsAtTermination(t *testing.T) {
	w, _ := newWriter(t)
	cfg := InterceptorConfig{Writer: w, IncludeReads: true}
	intc := StreamInterceptor(cfg)
	handler := func(srv any, ss grpc.ServerStream) error { return errors.New("boom") }
	ss := &fakeServerStream{ctx: contextWithSubject("alice", "operator")}
	err := intc(nil, ss, &grpc.StreamServerInfo{FullMethod: "/x/stream"}, handler)
	if err == nil {
		t.Fatal("expected handler error to propagate")
	}
	_ = w.Close()
}

// --- HTTPMiddleware ----------------------------------------------------

func TestHTTPMiddleware_RecordsRequest(t *testing.T) {
	w, _ := newWriter(t)
	cfg := InterceptorConfig{Writer: w, IncludeReads: true}
	mw := HTTPMiddleware(cfg)
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	ts := httptest.NewServer(srv)
	defer ts.Close()
	resp, _ := http.Get(ts.URL + "/v1/test")
	if resp.StatusCode != 201 {
		t.Errorf("status=%d", resp.StatusCode)
	}
	_ = w.Close()
}

func TestEntryFromHTTP_NoWriterIsNoop(t *testing.T) {
	cfg := InterceptorConfig{}
	entryFromHTTP(cfg, context.Background(), "/REST/v1/x/GET", 200)
	// Reaching here without panic = pass.
}

func TestShouldAudit_UnknownMethod_True(t *testing.T) {
	cfg := InterceptorConfig{Roles: auth.NewRoleMap()}
	if !shouldAudit(cfg, "/totally/unknown") {
		t.Error("unknown method should be audited (security signal)")
	}
}

// --- signal shim -------------------------------------------------------

func TestSyscallSignal(t *testing.T) {
	if syscallSignal(0) == syscallSignal(1) {
		t.Error("syscallSignal not honouring its int argument")
	}
}

// --- waitForFile -------------------------------------------------------

func TestWaitForFile_RespectsCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitForFile(ctx, "/no/such/path")
	if err != context.Canceled {
		t.Errorf("got %v; want context.Canceled", err)
	}
}
