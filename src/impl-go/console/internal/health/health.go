// Package health provides HTTP handlers for Kubernetes-style health
// probes: /healthz (liveness) and /readyz (readiness).
//
// Liveness: always returns 200 if the process is alive. K8s uses
// this to decide whether to restart the pod.
//
// Readiness: returns 200 only when dashd is reachable. K8s uses
// this to decide whether to route traffic to this instance.
package health

import (
"context"
"encoding/json"
"net/http"
"time"

"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/config"
)

// Response is the JSON shape returned by health endpoints.
type Response struct {
Status string            `json:"status"`
Checks map[string]string `json:"checks,omitempty"`
}

// LivenessHandler returns a handler for GET /healthz.
// It always returns 200 — if you can reach this handler, the process
// is alive.
func LivenessHandler() http.HandlerFunc {
return func(w http.ResponseWriter, r *http.Request) {
writeJSON(w, http.StatusOK, Response{Status: "ok"})
}
}

// ReadinessHandler returns a handler for GET /readyz.
// It returns 200 only when dashd is reachable (GET /admin/health
// returns 200 within 2 seconds). Otherwise returns 503.
//
// This prevents new instances from receiving traffic before they
// can serve data. In production with multiple replicas, K8s removes
// unready instances from the load balancer.
func ReadinessHandler(cfg *config.Config) http.HandlerFunc {
return func(w http.ResponseWriter, r *http.Request) {
checks := make(map[string]string)
healthy := true

// Check dashd REST reachability
dashdOK := checkDashd(cfg.DashdAdminAddr + "/admin/health")
if dashdOK {
checks["dashd_admin"] = "reachable"
} else {
checks["dashd_admin"] = "unreachable"
healthy = false
}

status := http.StatusOK
resp := Response{Status: "ready", Checks: checks}
if !healthy {
status = http.StatusServiceUnavailable
resp.Status = "not_ready"
}

writeJSON(w, status, resp)
}
}

// checkDashd performs a quick HTTP GET to verify dashd is reachable.
// Returns true if we get a 200 response within 2 seconds.
func checkDashd(url string) bool {
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
if err != nil {
return false
}

resp, err := http.DefaultClient.Do(req)
if err != nil {
return false
}
defer resp.Body.Close()

return resp.StatusCode == http.StatusOK
}

// writeJSON marshals v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(status)
_ = json.NewEncoder(w).Encode(v)
}