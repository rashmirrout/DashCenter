package health

import (
"encoding/json"
"net/http"
"net/http/httptest"
"testing"

"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/config"
)

func TestLivenessHandler_AlwaysReturns200(t *testing.T) {
handler := LivenessHandler()

req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
rec := httptest.NewRecorder()

handler.ServeHTTP(rec, req)

if rec.Code != http.StatusOK {
t.Errorf("status = %d, want 200", rec.Code)
}

var resp Response
if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
t.Fatalf("decode: %v", err)
}

if resp.Status != "ok" {
t.Errorf("status = %q, want %q", resp.Status, "ok")
}

ct := rec.Header().Get("Content-Type")
if ct != "application/json" {
t.Errorf("Content-Type = %q, want application/json", ct)
}
}

func TestReadinessHandler_DashdUnreachable_Returns503(t *testing.T) {
// Point to a non-existent address — dashd is not running
cfg := &config.Config{
DashdAdminAddr: "http://127.0.0.1:1", // port 1 should always refuse
}

handler := ReadinessHandler(cfg)

req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
rec := httptest.NewRecorder()

handler.ServeHTTP(rec, req)

if rec.Code != http.StatusServiceUnavailable {
t.Errorf("status = %d, want 503", rec.Code)
}

var resp Response
if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
t.Fatalf("decode: %v", err)
}

if resp.Status != "not_ready" {
t.Errorf("status = %q, want %q", resp.Status, "not_ready")
}

if resp.Checks["dashd_admin"] != "unreachable" {
t.Errorf("checks.dashd_admin = %q, want %q", resp.Checks["dashd_admin"], "unreachable")
}
}

func TestReadinessHandler_DashdReachable_Returns200(t *testing.T) {
// Create a mock dashd admin server that returns 200
mockDashd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(http.StatusOK)
_, _ = w.Write([]byte(`{"status":"ok"}`))
}))
defer mockDashd.Close()

cfg := &config.Config{
DashdAdminAddr: mockDashd.URL,
}

handler := ReadinessHandler(cfg)

req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
rec := httptest.NewRecorder()

handler.ServeHTTP(rec, req)

if rec.Code != http.StatusOK {
t.Errorf("status = %d, want 200", rec.Code)
}

var resp Response
if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
t.Fatalf("decode: %v", err)
}

if resp.Status != "ready" {
t.Errorf("status = %q, want %q", resp.Status, "ready")
}

if resp.Checks["dashd_admin"] != "reachable" {
t.Errorf("checks.dashd_admin = %q, want %q", resp.Checks["dashd_admin"], "reachable")
}
}

func TestCheckDashd_InvalidURL(t *testing.T) {
// Completely invalid URL
ok := checkDashd("://invalid")
if ok {
t.Error("expected false for invalid URL")
}
}

func TestCheckDashd_ServerReturns500(t *testing.T) {
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
w.WriteHeader(http.StatusInternalServerError)
}))
defer srv.Close()

ok := checkDashd(srv.URL + "/admin/health")
if ok {
t.Error("expected false for 500 response")
}
}

func TestWriteJSON(t *testing.T) {
rec := httptest.NewRecorder()
writeJSON(rec, http.StatusCreated, map[string]string{"key": "value"})

if rec.Code != http.StatusCreated {
t.Errorf("status = %d, want 201", rec.Code)
}

ct := rec.Header().Get("Content-Type")
if ct != "application/json" {
t.Errorf("Content-Type = %q, want application/json", ct)
}

var body map[string]string
if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
t.Fatalf("decode: %v", err)
}
if body["key"] != "value" {
t.Errorf("body.key = %q, want %q", body["key"], "value")
}
}