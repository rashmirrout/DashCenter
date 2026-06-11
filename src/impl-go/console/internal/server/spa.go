package server

import (
"io/fs"
"net/http"
"os"
"path/filepath"
"strings"

"github.com/rashmirrout/DashCenter/src/impl-go/console/web"
)

// spaHandler returns an http.HandlerFunc that serves the embedded SPA.
//
// Behaviour:
//  1. If the requested path matches a real file in the embedded
//     dist/ directory (JS, CSS, images, fonts), serve it with
//     appropriate cache headers.
//  2. Otherwise, serve index.html — this enables client-side routing
//     (React Router). The browser handles /fleet, /dpu/dpu-1, etc.
//
// Cache policy:
//   - index.html: Cache-Control: no-cache (always revalidate)
//   - Hashed assets (*.js, *.css): Cache-Control: public, max-age=31536000, immutable
//   - Other static files: Cache-Control: public, max-age=3600
func spaHandler() http.HandlerFunc {
// Get the embedded filesystem. In production this is go:embed;
// during development it may fall back to the local filesystem.
distFS := web.DistFS()

fileServer := http.FileServer(http.FS(distFS))

return func(w http.ResponseWriter, r *http.Request) {
// Clean the path and strip leading slash for fs.Open
path := strings.TrimPrefix(r.URL.Path, "/")
if path == "" {
path = "."
}

// Check if the file exists in the embedded filesystem
f, err := distFS.Open(path)
if err != nil {
// File not found → serve index.html (SPA client-side routing)
serveIndex(w, r, distFS)
return
}
f.Close()

// Check if it's a directory (don't serve directory listings)
info, err := fs.Stat(distFS, path)
if err != nil || info.IsDir() {
serveIndex(w, r, distFS)
return
}

// Set cache headers based on file type
setCacheHeaders(w, path)

// Serve the actual file
fileServer.ServeHTTP(w, r)
}
}

// serveIndex serves the SPA's index.html with no-cache headers.
func serveIndex(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
w.Header().Set("Content-Type", "text/html; charset=utf-8")

data, err := fs.ReadFile(fsys, "index.html")
if err != nil {
// No index.html embedded — likely a build issue. Return a
// minimal HTML page so the operator sees something useful.
w.WriteHeader(http.StatusOK)
_, _ = w.Write([]byte(`<!DOCTYPE html>
<html><head><title>dashw</title></head>
<body style="background:#0A0E1A;color:#F9FAFB;font-family:sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0">
<div style="text-align:center">
<h1 style="color:#00D4FF">dashw</h1>
<p>DashCenter Web Console</p>
<p style="color:#9CA3AF">SPA not built yet. Run: <code style="color:#00FF88">cd src/impl-web/console && npm run build</code></p>
<p style="color:#9CA3AF">Then rebuild the Go binary to embed the dist/ directory.</p>
</div>
</body></html>`))
return
}

w.WriteHeader(http.StatusOK)
_, _ = w.Write(data)
}

// setCacheHeaders sets appropriate Cache-Control headers based on the
// file extension. Content-hashed assets get immutable caching;
// everything else gets short-lived caching.
func setCacheHeaders(w http.ResponseWriter, path string) {
ext := filepath.Ext(path)

switch ext {
case ".js", ".css":
// Vite produces content-hashed filenames (e.g., index-abc123.js)
// These are safe to cache forever.
w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
case ".woff2", ".woff", ".ttf", ".eot":
// Fonts change rarely
w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
case ".svg", ".png", ".ico", ".jpg", ".jpeg", ".webp":
// Images — long cache
w.Header().Set("Cache-Control", "public, max-age=86400")
default:
// Everything else — moderate cache
w.Header().Set("Cache-Control", "public, max-age=3600")
}
}

// devDistDir checks if a local dist directory exists for development.
// Not used in production (go:embed handles it).
func devDistDir() string {
candidates := []string{
"web/dist",
"../impl-web/console/dist",
}
for _, dir := range candidates {
if info, err := os.Stat(dir); err == nil && info.IsDir() {
return dir
}
}
return ""
}