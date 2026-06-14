package proxy

import (
"log/slog"
"net/http"
"net/http/httputil"
"net/url"
"strings"
"time"

"github.com/go-chi/chi/v5"
)

// SimProxy reverse-proxies /api/sim/{simId}/admin/* to a dash-sim instance.
// The simId is extracted from the URL and used to construct the target address
// by replacing "{id}" in the SimBaseAddr template.
//
// Example with SimBaseAddr = "http://dash-sim-{id}:8080":
//
//Browser: GET /api/sim/01/admin/dump
//sim:     GET /admin/dump  → http://dash-sim-01:8080/admin/dump
type SimProxy struct {
baseAddrTemplate string
timeout          time.Duration
logger           *slog.Logger
}

// NewSimProxy creates a dynamic reverse proxy for dash-sim instances.
// baseAddrTemplate should contain "{id}" which will be replaced with
// the simId from the URL path (e.g., "http://dash-sim-{id}:8080").
func NewSimProxy(baseAddrTemplate string, timeout time.Duration, logger *slog.Logger) *SimProxy {
return &SimProxy{
baseAddrTemplate: baseAddrTemplate,
timeout:          timeout,
logger:           logger,
}
}

// ServeHTTP implements http.Handler. Extracts simId from chi URL param,
// constructs the target URL, and proxies the request.
func (p *SimProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
simId := chi.URLParam(r, "simId")
if simId == "" {
w.Header().Set("Content-Type", "application/json")
http.Error(w, `{"error":"simId path parameter is required"}`, http.StatusBadRequest)
return
}

// Construct target address from template
targetAddr := strings.ReplaceAll(p.baseAddrTemplate, "{id}", simId)
target, err := url.Parse(targetAddr)
if err != nil {
p.logger.Error("invalid sim target", "simId", simId, "addr", targetAddr, "error", err)
w.Header().Set("Content-Type", "application/json")
http.Error(w, `{"error":"invalid sim address"}`, http.StatusBadGateway)
return
}

// Create a one-shot reverse proxy for this specific sim
rp := httputil.NewSingleHostReverseProxy(target)

rp.Director = func(req *http.Request) {
req.URL.Scheme = target.Scheme
req.URL.Host = target.Host
req.Host = target.Host

// Rewrite path: strip /api/sim/{simId} prefix
// Input:  /api/sim/01/admin/dump
// Output: /admin/dump
path := req.URL.Path
prefix := "/api/sim/" + simId
req.URL.Path = strings.TrimPrefix(path, prefix)
if req.URL.Path == "" {
req.URL.Path = "/"
}
}

rp.Transport = &http.Transport{
ResponseHeaderTimeout: p.timeout,
MaxIdleConns:          10,
MaxIdleConnsPerHost:   2,
IdleConnTimeout:       30 * time.Second,
}

rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
p.logger.Error("sim proxy error",
"simId", simId,
"path", r.URL.Path,
"error", err,
)
w.Header().Set("Content-Type", "application/json")
http.Error(w,
`{"error":"dash-sim unreachable","sim_id":"`+simId+`","detail":"`+err.Error()+`"}`,
http.StatusBadGateway,
)
}

rp.ServeHTTP(w, r)
}