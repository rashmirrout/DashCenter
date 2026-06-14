package resilience

import (
"net"
"net/http"
"sync"
"time"

"golang.org/x/time/rate"
)

// RateLimiter provides per-IP write rate limiting for the dashw BFF.
// Only write methods (PUT, POST, DELETE) are rate-limited; reads (GET)
// are handled by the in-process cache and don't need limiting.
type RateLimiter struct {
mu       sync.Mutex
limiters map[string]*limiterEntry
burst    int           // max burst (requests/sec)
ratePerS float64       // sustained rate (requests/sec)
cleanupD time.Duration // remove idle entries after this duration
stopCh   chan struct{}
}

type limiterEntry struct {
limiter  *rate.Limiter
lastSeen time.Time
}

// NewRateLimiter creates a per-IP rate limiter.
//
//   - burstPerSec: maximum burst (e.g., 10 mutations/sec)
//   - ratePerSec: sustained rate (e.g., 1.67 for 100/min)
//   - cleanupInterval: how often to remove idle IP entries
func NewRateLimiter(burstPerSec int, ratePerSec float64, cleanupInterval time.Duration) *RateLimiter {
rl := &RateLimiter{
limiters: make(map[string]*limiterEntry),
burst:    burstPerSec,
ratePerS: ratePerSec,
cleanupD: cleanupInterval,
stopCh:   make(chan struct{}),
}
go rl.cleanup()
return rl
}

// Allow checks if a request from the given IP is allowed.
// Returns true if the request should proceed, false if rate-limited.
func (rl *RateLimiter) Allow(ip string) bool {
rl.mu.Lock()
entry, ok := rl.limiters[ip]
if !ok {
entry = &limiterEntry{
limiter: rate.NewLimiter(rate.Limit(rl.ratePerS), rl.burst),
}
rl.limiters[ip] = entry
}
entry.lastSeen = time.Now()
rl.mu.Unlock()

return entry.limiter.Allow()
}

// Middleware returns an HTTP middleware that rate-limits write requests.
// Only PUT, POST, DELETE methods are checked. GET/OPTIONS/HEAD pass through.
// Returns 429 Too Many Requests with Retry-After header when limited.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// Only rate-limit writes
if !isWriteMethod(r.Method) {
next.ServeHTTP(w, r)
return
}

ip := extractIP(r)
if !rl.Allow(ip) {
w.Header().Set("Retry-After", "1")
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(http.StatusTooManyRequests)
_, _ = w.Write([]byte(`{"error":"rate limit exceeded","retry_after":"1s"}`))
return
}

next.ServeHTTP(w, r)
})
}

// Stop terminates the background cleanup goroutine.
func (rl *RateLimiter) Stop() {
close(rl.stopCh)
}

// Len returns the number of tracked IPs (for testing/monitoring).
func (rl *RateLimiter) Len() int {
rl.mu.Lock()
defer rl.mu.Unlock()
return len(rl.limiters)
}

// cleanup periodically removes idle IP entries to prevent unbounded growth.
func (rl *RateLimiter) cleanup() {
ticker := time.NewTicker(rl.cleanupD)
defer ticker.Stop()

for {
select {
case <-ticker.C:
rl.evictIdle()
case <-rl.stopCh:
return
}
}
}

// evictIdle removes entries not seen for more than cleanupD.
func (rl *RateLimiter) evictIdle() {
cutoff := time.Now().Add(-rl.cleanupD)

rl.mu.Lock()
defer rl.mu.Unlock()

for ip, entry := range rl.limiters {
if entry.lastSeen.Before(cutoff) {
delete(rl.limiters, ip)
}
}
}

// isWriteMethod returns true for HTTP methods that mutate state.
func isWriteMethod(method string) bool {
switch method {
case http.MethodPut, http.MethodPost, http.MethodDelete, http.MethodPatch:
return true
default:
return false
}
}

// extractIP extracts the client IP from the request.
// Prefers X-Real-IP (set by chi RealIP middleware), falls back to RemoteAddr.
func extractIP(r *http.Request) string {
// X-Real-IP is set by chi's RealIP middleware
if ip := r.Header.Get("X-Real-IP"); ip != "" {
return ip
}
// X-Forwarded-For first entry
if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
// Take only the first IP (client IP)
for i := 0; i < len(xff); i++ {
if xff[i] == ',' {
return xff[:i]
}
}
return xff
}
// Fall back to RemoteAddr (strip port)
ip, _, err := net.SplitHostPort(r.RemoteAddr)
if err != nil {
return r.RemoteAddr
}
return ip
}