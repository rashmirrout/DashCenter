package resilience

import (
"net/http"
"net/http/httptest"
"testing"
"time"
)

func TestRateLimiter_AllowWithinBurst(t *testing.T) {
rl := NewRateLimiter(5, 1.0, time.Minute)
defer rl.Stop()

// Should allow up to burst (5) requests
for i := 0; i < 5; i++ {
if !rl.Allow("10.0.0.1") {
t.Errorf("request %d should be allowed (within burst)", i)
}
}
}

func TestRateLimiter_RejectOverBurst(t *testing.T) {
rl := NewRateLimiter(3, 0.1, time.Minute) // 3 burst, very slow refill
defer rl.Stop()

// Exhaust burst
for i := 0; i < 3; i++ {
rl.Allow("10.0.0.1")
}

// Next should be rejected
if rl.Allow("10.0.0.1") {
t.Error("request should be rejected (over burst)")
}
}

func TestRateLimiter_PerIPIsolation(t *testing.T) {
rl := NewRateLimiter(2, 0.1, time.Minute)
defer rl.Stop()

// Exhaust IP-1's burst
rl.Allow("10.0.0.1")
rl.Allow("10.0.0.1")
if rl.Allow("10.0.0.1") {
t.Error("IP-1 should be rate-limited")
}

// IP-2 should still have its own budget
if !rl.Allow("10.0.0.2") {
t.Error("IP-2 should be allowed (separate limiter)")
}
}

func TestRateLimiter_Len(t *testing.T) {
rl := NewRateLimiter(5, 1.0, time.Minute)
defer rl.Stop()

if rl.Len() != 0 {
t.Errorf("Len = %d, want 0 initially", rl.Len())
}

rl.Allow("10.0.0.1")
rl.Allow("10.0.0.2")

if rl.Len() != 2 {
t.Errorf("Len = %d, want 2", rl.Len())
}
}

func TestRateLimiter_CleanupRemovesIdleIPs(t *testing.T) {
rl := NewRateLimiter(5, 1.0, 50*time.Millisecond)
defer rl.Stop()

rl.Allow("10.0.0.1")

// Wait for cleanup to run
time.Sleep(120 * time.Millisecond)

if rl.Len() != 0 {
t.Errorf("Len = %d, want 0 after cleanup (idle IP should be evicted)", rl.Len())
}
}

func TestRateLimiter_Middleware_GETPassesThrough(t *testing.T) {
rl := NewRateLimiter(1, 0.01, time.Minute) // very restrictive
defer rl.Stop()

handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
w.WriteHeader(http.StatusOK)
}))

// GET should always pass (not rate-limited)
for i := 0; i < 10; i++ {
req := httptest.NewRequest(http.MethodGet, "/api/v1/vnets", nil)
req.RemoteAddr = "10.0.0.1:1234"
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)

if rec.Code != http.StatusOK {
t.Errorf("GET request %d: status = %d, want 200", i, rec.Code)
}
}
}

func TestRateLimiter_Middleware_PUTRateLimited(t *testing.T) {
rl := NewRateLimiter(2, 0.1, time.Minute)
defer rl.Stop()

handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
w.WriteHeader(http.StatusOK)
}))

// First 2 PUTs should pass
for i := 0; i < 2; i++ {
req := httptest.NewRequest(http.MethodPut, "/api/v1/default/vnets/x", nil)
req.RemoteAddr = "10.0.0.1:1234"
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)

if rec.Code != http.StatusOK {
t.Errorf("PUT %d: status = %d, want 200", i, rec.Code)
}
}

// 3rd PUT should be rate-limited
req := httptest.NewRequest(http.MethodPut, "/api/v1/default/vnets/x", nil)
req.RemoteAddr = "10.0.0.1:1234"
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)

if rec.Code != http.StatusTooManyRequests {
t.Errorf("PUT 3: status = %d, want 429", rec.Code)
}

retryAfter := rec.Header().Get("Retry-After")
if retryAfter == "" {
t.Error("missing Retry-After header")
}
}

func TestRateLimiter_Middleware_POSTRateLimited(t *testing.T) {
rl := NewRateLimiter(1, 0.1, time.Minute)
defer rl.Stop()

handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
w.WriteHeader(http.StatusOK)
}))

req := httptest.NewRequest(http.MethodPost, "/api/v1/reconcile", nil)
req.RemoteAddr = "10.0.0.1:1234"
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)
// First passes
if rec.Code != http.StatusOK {
t.Errorf("POST 1: status = %d, want 200", rec.Code)
}

// Second rejected
req = httptest.NewRequest(http.MethodPost, "/api/v1/reconcile", nil)
req.RemoteAddr = "10.0.0.1:1234"
rec = httptest.NewRecorder()
handler.ServeHTTP(rec, req)
if rec.Code != http.StatusTooManyRequests {
t.Errorf("POST 2: status = %d, want 429", rec.Code)
}
}

func TestRateLimiter_Middleware_DELETERateLimited(t *testing.T) {
rl := NewRateLimiter(1, 0.1, time.Minute)
defer rl.Stop()

handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
w.WriteHeader(http.StatusOK)
}))

// First DELETE passes
req := httptest.NewRequest(http.MethodDelete, "/api/v1/default/vnets/x", nil)
req.RemoteAddr = "10.0.0.1:1234"
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)
if rec.Code != http.StatusOK {
t.Errorf("DELETE 1: status = %d, want 200", rec.Code)
}

// Second rejected
req = httptest.NewRequest(http.MethodDelete, "/api/v1/default/vnets/x", nil)
req.RemoteAddr = "10.0.0.1:1234"
rec = httptest.NewRecorder()
handler.ServeHTTP(rec, req)
if rec.Code != http.StatusTooManyRequests {
t.Errorf("DELETE 2: status = %d, want 429", rec.Code)
}
}

func TestExtractIP_XRealIP(t *testing.T) {
req := httptest.NewRequest(http.MethodGet, "/", nil)
req.Header.Set("X-Real-IP", "1.2.3.4")
req.RemoteAddr = "5.6.7.8:1234"

ip := extractIP(req)
if ip != "1.2.3.4" {
t.Errorf("extractIP = %q, want %q (from X-Real-IP)", ip, "1.2.3.4")
}
}

func TestExtractIP_XForwardedFor(t *testing.T) {
req := httptest.NewRequest(http.MethodGet, "/", nil)
req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8, 9.10.11.12")
req.RemoteAddr = "5.6.7.8:1234"

ip := extractIP(req)
if ip != "1.2.3.4" {
t.Errorf("extractIP = %q, want %q (first XFF entry)", ip, "1.2.3.4")
}
}

func TestExtractIP_XForwardedFor_Single(t *testing.T) {
req := httptest.NewRequest(http.MethodGet, "/", nil)
req.Header.Set("X-Forwarded-For", "1.2.3.4")
req.RemoteAddr = "5.6.7.8:1234"

ip := extractIP(req)
if ip != "1.2.3.4" {
t.Errorf("extractIP = %q, want %q", ip, "1.2.3.4")
}
}

func TestExtractIP_RemoteAddr(t *testing.T) {
req := httptest.NewRequest(http.MethodGet, "/", nil)
req.RemoteAddr = "192.168.1.1:5432"

ip := extractIP(req)
if ip != "192.168.1.1" {
t.Errorf("extractIP = %q, want %q (from RemoteAddr)", ip, "192.168.1.1")
}
}

func TestExtractIP_RemoteAddr_NoPort(t *testing.T) {
req := httptest.NewRequest(http.MethodGet, "/", nil)
req.RemoteAddr = "192.168.1.1"

ip := extractIP(req)
if ip != "192.168.1.1" {
t.Errorf("extractIP = %q, want %q", ip, "192.168.1.1")
}
}

func TestIsWriteMethod(t *testing.T) {
writes := []string{"PUT", "POST", "DELETE", "PATCH"}
reads := []string{"GET", "HEAD", "OPTIONS", "TRACE"}

for _, m := range writes {
if !isWriteMethod(m) {
t.Errorf("isWriteMethod(%q) = false, want true", m)
}
}
for _, m := range reads {
if isWriteMethod(m) {
t.Errorf("isWriteMethod(%q) = true, want false", m)
}
}
}