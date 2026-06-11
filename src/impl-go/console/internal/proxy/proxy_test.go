package proxy

import (
"encoding/json"
"io"
"log/slog"
"net/http"
"net/http/httptest"
"testing"
"time"
)

func testLogger() *slog.Logger {
return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// mockDashd creates a test server that records the received request
// path and method, then returns a JSON response.
func mockDashd(t *testing.T) *httptest.Server {
t.Helper()
return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(http.StatusOK)
_ = json.NewEncoder(w).Encode(map[string]string{
"received_path":   r.URL.Path,
"received_method": r.Method,
"backend":         "mock-dashd",
})
}))
}

func TestRestProxy_PathRewrite(t *testing.T) {
backend := mockDashd(t)
defer backend.Close()

proxy := NewRestProxy(backend.URL, 5*time.Second, testLogger())

tests := []struct {
name       string
inputPath  string
wantPath   string
method     string
}{
{"list vnets", "/api/v1/default/vnets", "/v1/default/vnets", "GET"},
{"put eni", "/api/v1/default/enis/eni-01", "/v1/default/enis/eni-01", "PUT"},
{"delete vnet", "/api/v1/default/vnets/vnet-prod", "/v1/default/vnets/vnet-prod", "DELETE"},
{"reconcile", "/api/v1/reconcile", "/v1/reconcile", "POST"},
{"simulate", "/api/v1/simulate", "/v1/simulate", "POST"},
{"inventory", "/api/v1/inventory", "/v1/inventory", "GET"},
}

for _, tt := range tests {
t.Run(tt.name, func(t *testing.T) {
req := httptest.NewRequest(tt.method, tt.inputPath, nil)
rec := httptest.NewRecorder()
proxy.ServeHTTP(rec, req)

if rec.Code != http.StatusOK {
t.Fatalf("status = %d, want 200", rec.Code)
}

var body map[string]string
if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
t.Fatalf("decode: %v", err)
}

if body["received_path"] != tt.wantPath {
t.Errorf("received_path = %q, want %q", body["received_path"], tt.wantPath)
}
if body["received_method"] != tt.method {
t.Errorf("received_method = %q, want %q", body["received_method"], tt.method)
}
})
}
}

func TestAdminProxy_PathRewrite(t *testing.T) {
backend := mockDashd(t)
defer backend.Close()

proxy := NewAdminProxy(backend.URL, 5*time.Second, testLogger())

tests := []struct {
name      string
inputPath string
wantPath  string
}{
{"health", "/api/admin/health", "/admin/health"},
{"leader", "/api/admin/leader", "/admin/leader"},
{"inventory", "/api/admin/inventory", "/admin/inventory"},
{"drift", "/api/admin/drift", "/admin/drift"},
{"observed", "/api/admin/observed", "/admin/observed"},
{"eni-placement", "/api/admin/eni-placement", "/admin/eni-placement"},
{"reconcile", "/api/admin/reconcile", "/admin/reconcile"},
}

for _, tt := range tests {
t.Run(tt.name, func(t *testing.T) {
req := httptest.NewRequest(http.MethodGet, tt.inputPath, nil)
rec := httptest.NewRecorder()
proxy.ServeHTTP(rec, req)

if rec.Code != http.StatusOK {
t.Fatalf("status = %d, want 200", rec.Code)
}

var body map[string]string
if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
t.Fatalf("decode: %v", err)
}

if body["received_path"] != tt.wantPath {
t.Errorf("received_path = %q, want %q", body["received_path"], tt.wantPath)
}
})
}
}

func TestRestProxy_BackendDown_Returns502(t *testing.T) {
// Point to a server that's not running
proxy := NewRestProxy("http://127.0.0.1:1", 1*time.Second, testLogger())

req := httptest.NewRequest(http.MethodGet, "/api/v1/default/vnets", nil)
rec := httptest.NewRecorder()
proxy.ServeHTTP(rec, req)

if rec.Code != http.StatusBadGateway {
t.Errorf("status = %d, want 502", rec.Code)
}

body := rec.Body.String()
if body == "" {
t.Error("expected error body")
}
}

func TestAdminProxy_BackendDown_Returns502(t *testing.T) {
proxy := NewAdminProxy("http://127.0.0.1:1", 1*time.Second, testLogger())

req := httptest.NewRequest(http.MethodGet, "/api/admin/health", nil)
rec := httptest.NewRecorder()
proxy.ServeHTTP(rec, req)

if rec.Code != http.StatusBadGateway {
t.Errorf("status = %d, want 502", rec.Code)
}
}

func TestRestProxy_InvalidURL_FallsBack(t *testing.T) {
// Invalid URL shouldn't panic — should fall back
proxy := NewRestProxy("://invalid", 1*time.Second, testLogger())
if proxy == nil {
t.Fatal("NewRestProxy returned nil for invalid URL")
}
}

func TestAdminProxy_InvalidURL_FallsBack(t *testing.T) {
proxy := NewAdminProxy("://invalid", 1*time.Second, testLogger())
if proxy == nil {
t.Fatal("NewAdminProxy returned nil for invalid URL")
}
}

func TestRestProxy_PreservesHeaders(t *testing.T) {
backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// Echo back a custom header
ct := r.Header.Get("Content-Type")
auth := r.Header.Get("Authorization")
w.Header().Set("Content-Type", "application/json")
_ = json.NewEncoder(w).Encode(map[string]string{
"content_type":  ct,
"authorization": auth,
})
}))
defer backend.Close()

proxy := NewRestProxy(backend.URL, 5*time.Second, testLogger())

req := httptest.NewRequest(http.MethodPut, "/api/v1/default/vnets/vnet-prod", nil)
req.Header.Set("Content-Type", "application/json")
req.Header.Set("Authorization", "Bearer test-token")
rec := httptest.NewRecorder()
proxy.ServeHTTP(rec, req)

var body map[string]string
_ = json.NewDecoder(rec.Body).Decode(&body)

if body["content_type"] != "application/json" {
t.Errorf("Content-Type not forwarded: %q", body["content_type"])
}
if body["authorization"] != "Bearer test-token" {
t.Errorf("Authorization not forwarded: %q", body["authorization"])
}
}

func TestRestProxy_PreservesStatusCode(t *testing.T) {
backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
w.WriteHeader(http.StatusNotFound)
_, _ = w.Write([]byte(`{"error":"not found"}`))
}))
defer backend.Close()

proxy := NewRestProxy(backend.URL, 5*time.Second, testLogger())

req := httptest.NewRequest(http.MethodGet, "/api/v1/default/vnets/nonexistent", nil)
rec := httptest.NewRecorder()
proxy.ServeHTTP(rec, req)

if rec.Code != http.StatusNotFound {
t.Errorf("status = %d, want 404 (should pass through dashd status)", rec.Code)
}
}