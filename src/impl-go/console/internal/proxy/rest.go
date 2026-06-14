// Package proxy implements reverse-proxy handlers for the dashw BFF.
// Each proxy transparently forwards requests to a dashd backend,
// rewriting paths as needed.
package proxy

import (
"log/slog"
"net/http"
"net/http/httputil"
"net/url"
"strings"
"time"
)

// RestProxy reverse-proxies /api/v1/* to dashd REST :8443.
// Path rewrite: strips "/api" prefix.
//
// Example:
//
//Browser: PUT /api/v1/default/enis/eni-01
//dashd:   PUT /v1/default/enis/eni-01
type RestProxy struct {
proxy  *httputil.ReverseProxy
logger *slog.Logger
}

// NewRestProxy creates a reverse proxy targeting the dashd REST API.
func NewRestProxy(targetAddr string, timeout time.Duration, logger *slog.Logger) *RestProxy {
target, err := url.Parse(targetAddr)
if err != nil {
logger.Error("invalid dashd REST address", "addr", targetAddr, "error", err)
// Fall back to localhost
target, _ = url.Parse("http://localhost:8443")
}

rp := httputil.NewSingleHostReverseProxy(target)

// Custom director: rewrite path by stripping "/api" prefix
originalDirector := rp.Director
rp.Director = func(req *http.Request) {
originalDirector(req)
req.URL.Path = strings.TrimPrefix(req.URL.Path, "/api")
req.URL.RawPath = strings.TrimPrefix(req.URL.RawPath, "/api")
req.Host = target.Host
}

// Transport with timeout and connection pooling
rp.Transport = &http.Transport{
ResponseHeaderTimeout: timeout,
MaxIdleConns:          100,
MaxIdleConnsPerHost:   20,
IdleConnTimeout:       90 * time.Second,
}

// Error handler: log and return 502
rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
logger.Error("rest proxy error",
"path", r.URL.Path,
"method", r.Method,
"error", err,
)
w.Header().Set("Content-Type", "application/json")
http.Error(w,
`{"error":"dashd REST unreachable","detail":"`+err.Error()+`"}`,
http.StatusBadGateway,
)
}

return &RestProxy{proxy: rp, logger: logger}
}

// ServeHTTP implements http.Handler.
func (p *RestProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
p.proxy.ServeHTTP(w, r)
}