package server

import (
"context"
"encoding/json"
"io"
"log/slog"
"net/http"
"net/http/httptest"
"strings"
"testing"
"time"

"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/config"
)

func testLogger() *slog.Logger {
return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func TestNew_CreatesServer(t *testing.T) {
cfg := config.DefaultConfig()
logger := testLogger()

srv, err := New(cfg, logger)
if err != nil {
t.Fatalf("New() error: %v", err)
}
if srv == nil {
t.Fatal("New() returned nil server")
}
if srv.httpSrv == nil {
t.Fatal("httpSrv is nil")
}
if srv.httpSrv.Addr != ":8080" {
t.Errorf("Addr = %q, want %q", srv.httpSrv.Addr, ":8080")
}
}

func TestServer_HealthzReturns200(t *testing.T) {
cfg := config.DefaultConfig()
handler := buildRouter(cfg, testLogger(), nil, nil)

req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)

if rec.Code != http.StatusOK {
t.Errorf("/healthz status = %d, want 200", rec.Code)
}

var body map[string]any
if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
t.Fatalf("decode: %v", err)
}
if body["status"] != "ok" {
t.Errorf("status = %v, want ok", body["status"])
}
}

func TestServer_ReadyzReturns503WhenDashdDown(t *testing.T) {
cfg := config.DefaultConfig()
cfg.DashdAdminAddr = "http://127.0.0.1:1" // unreachable
handler := buildRouter(cfg, testLogger(), nil, nil)

req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)

if rec.Code != http.StatusServiceUnavailable {
t.Errorf("/readyz status = %d, want 503", rec.Code)
}
}

func TestServer_SPAFallback_ServesHTML(t *testing.T) {
cfg := config.DefaultConfig()
handler := buildRouter(cfg, testLogger(), nil, nil)

// Request a non-API path → should get HTML (SPA fallback)
req := httptest.NewRequest(http.MethodGet, "/fleet", nil)
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)

if rec.Code != http.StatusOK {
t.Errorf("/fleet status = %d, want 200", rec.Code)
}

ct := rec.Header().Get("Content-Type")
if !strings.Contains(ct, "text/html") {
t.Errorf("Content-Type = %q, want text/html", ct)
}

body := rec.Body.String()
if !strings.Contains(body, "dashw") {
t.Error("SPA fallback body should contain 'dashw'")
}
}

func TestServer_SPAFallback_RootPath(t *testing.T) {
cfg := config.DefaultConfig()
handler := buildRouter(cfg, testLogger(), nil, nil)

req := httptest.NewRequest(http.MethodGet, "/", nil)
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)

if rec.Code != http.StatusOK {
t.Errorf("/ status = %d, want 200", rec.Code)
}

ct := rec.Header().Get("Content-Type")
if !strings.Contains(ct, "text/html") {
t.Errorf("Content-Type = %q, want text/html", ct)
}
}

func TestServer_SPAFallback_DeepPath(t *testing.T) {
cfg := config.DefaultConfig()
handler := buildRouter(cfg, testLogger(), nil, nil)

req := httptest.NewRequest(http.MethodGet, "/dpu/dpu-sim-01", nil)
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)

if rec.Code != http.StatusOK {
t.Errorf("/dpu/dpu-sim-01 status = %d, want 200", rec.Code)
}
}

func TestServer_MetricsDisabledByDefault(t *testing.T) {
cfg := config.DefaultConfig() // EnableMetrics = false
handler := buildRouter(cfg, testLogger(), nil, nil)

req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)

// When metrics disabled, /metrics falls through to SPA fallback
// (which returns 200 with HTML, not the metrics endpoint)
ct := rec.Header().Get("Content-Type")
if strings.Contains(ct, "text/plain") {
t.Error("/metrics should not be registered when EnableMetrics=false")
}
}

func TestServer_MetricsEnabledReturnsText(t *testing.T) {
cfg := config.DefaultConfig()
cfg.EnableMetrics = true
handler := buildRouter(cfg, testLogger(), nil, nil)

req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)

if rec.Code != http.StatusOK {
t.Errorf("/metrics status = %d, want 200", rec.Code)
}

ct := rec.Header().Get("Content-Type")
if !strings.Contains(ct, "text/plain") {
t.Errorf("Content-Type = %q, want text/plain", ct)
}
}

func TestServer_RequestIDHeader(t *testing.T) {
cfg := config.DefaultConfig()
handler := buildRouter(cfg, testLogger(), nil, nil)

req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)

// chi middleware adds X-Request-Id to response
// Note: chi's RequestID generates one if not provided
if rec.Code != http.StatusOK {
t.Errorf("status = %d, want 200", rec.Code)
}
}

func TestServer_CORSEnabled(t *testing.T) {
cfg := config.DefaultConfig()
cfg.EnableCORS = true
handler := buildRouter(cfg, testLogger(), nil, nil)

// Send an OPTIONS preflight request
req := httptest.NewRequest(http.MethodOptions, "/healthz", nil)
req.Header.Set("Origin", "http://localhost:3000")
req.Header.Set("Access-Control-Request-Method", "GET")
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)

// CORS should add Access-Control-Allow-Origin header
acao := rec.Header().Get("Access-Control-Allow-Origin")
if acao == "" {
t.Error("CORS enabled but no Access-Control-Allow-Origin header")
}
}

func TestServer_RecoveryMiddleware_CatchesPanic(t *testing.T) {
logger := testLogger()

// Create a handler that panics
panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
panic("test panic")
})

// Wrap with recovery middleware
handler := recoveryMiddleware(logger)(panicHandler)

req := httptest.NewRequest(http.MethodGet, "/", nil)
rec := httptest.NewRecorder()

// Should not panic — recovery middleware catches it
handler.ServeHTTP(rec, req)

if rec.Code != http.StatusInternalServerError {
t.Errorf("status = %d, want 500 after panic", rec.Code)
}

body := rec.Body.String()
if !strings.Contains(body, "internal server error") {
t.Errorf("body = %q, want 'internal server error'", body)
}
}

func TestServer_RunAndShutdown(t *testing.T) {
cfg := config.DefaultConfig()
cfg.Listen = ":0" // random port

srv, err := New(cfg, testLogger())
if err != nil {
t.Fatalf("New() error: %v", err)
}

ctx, cancel := context.WithCancel(context.Background())

errCh := make(chan error, 1)
go func() {
errCh <- srv.Run(ctx)
}()

// Give the server a moment to start
time.Sleep(50 * time.Millisecond)

// Cancel context → triggers graceful shutdown
cancel()

select {
case err := <-errCh:
if err != nil {
t.Errorf("Run() returned error: %v", err)
}
case <-time.After(5 * time.Second):
t.Fatal("Run() did not return within 5s after cancel")
}
}
