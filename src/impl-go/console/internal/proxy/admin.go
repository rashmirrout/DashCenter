package proxy

import (
"log/slog"
"net/http"
"net/http/httputil"
"net/url"
"strings"
"time"
)

// AdminProxy reverse-proxies /api/admin/* to dashd Admin :7443.
// Path rewrite: strips "/api" prefix.
//
// Example:
//
//Browser: GET /api/admin/health
//dashd:   GET /admin/health
type AdminProxy struct {
proxy  *httputil.ReverseProxy
logger *slog.Logger
}

// NewAdminProxy creates a reverse proxy targeting the dashd Admin API.
func NewAdminProxy(targetAddr string, timeout time.Duration, logger *slog.Logger) *AdminProxy {
target, err := url.Parse(targetAddr)
if err != nil {
logger.Error("invalid dashd Admin address", "addr", targetAddr, "error", err)
target, _ = url.Parse("http://localhost:7443")
}

rp := httputil.NewSingleHostReverseProxy(target)

originalDirector := rp.Director
rp.Director = func(req *http.Request) {
originalDirector(req)
req.URL.Path = strings.TrimPrefix(req.URL.Path, "/api")
req.URL.RawPath = strings.TrimPrefix(req.URL.RawPath, "/api")
req.Host = target.Host
}

rp.Transport = &http.Transport{
ResponseHeaderTimeout: timeout,
MaxIdleConns:          50,
MaxIdleConnsPerHost:   10,
IdleConnTimeout:       90 * time.Second,
}

rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
logger.Error("admin proxy error",
"path", r.URL.Path,
"method", r.Method,
"error", err,
)
w.Header().Set("Content-Type", "application/json")
http.Error(w,
`{"error":"dashd Admin unreachable","detail":"`+err.Error()+`"}`,
http.StatusBadGateway,
)
}

return &AdminProxy{proxy: rp, logger: logger}
}

// ServeHTTP implements http.Handler.
func (p *AdminProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
p.proxy.ServeHTTP(w, r)
}