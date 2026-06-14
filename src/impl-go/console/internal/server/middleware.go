// Package server implements the dashw BFF HTTP server, router, and
// middleware stack.
package server

import (
"log/slog"
"net/http"
"time"

"github.com/go-chi/chi/v5/middleware"
)

// requestIDMiddleware injects a unique X-Request-Id into every request
// context and response header. Uses chi's built-in implementation.
func requestIDMiddleware(next http.Handler) http.Handler {
return middleware.RequestID(next)
}

// realIPMiddleware extracts the real client IP from X-Real-IP or
// X-Forwarded-For headers. Uses chi's built-in implementation.
func realIPMiddleware(next http.Handler) http.Handler {
return middleware.RealIP(next)
}

// loggingMiddleware logs every request with structured fields:
// method, path, status, latency, request_id, remote_addr.
func loggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
return func(next http.Handler) http.Handler {
return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
start := time.Now()

// Wrap the response writer to capture status code
ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

next.ServeHTTP(ww, r)

latency := time.Since(start)
reqID := middleware.GetReqID(r.Context())

logger.Info("request",
"method", r.Method,
"path", r.URL.Path,
"status", ww.Status(),
"latency_ms", latency.Milliseconds(),
"bytes", ww.BytesWritten(),
"request_id", reqID,
"remote_addr", r.RemoteAddr,
)
})
}
}

// recoveryMiddleware catches panics in HTTP handlers, logs the stack
// trace, and returns 500 Internal Server Error. Prevents a single
// panicking handler from crashing the entire server.
func recoveryMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
return func(next http.Handler) http.Handler {
return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
defer func() {
if rec := recover(); rec != nil {
reqID := middleware.GetReqID(r.Context())
logger.Error("panic recovered",
"panic", rec,
"request_id", reqID,
"method", r.Method,
"path", r.URL.Path,
)
http.Error(w,
`{"error":"internal server error"}`,
http.StatusInternalServerError,
)
}
}()
next.ServeHTTP(w, r)
})
}
}