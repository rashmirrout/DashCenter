// Package web provides the embedded SPA filesystem for the dashw
// console. In production builds, the dist/ directory is populated
// by `npm run build` in src/impl-web/console/ and then embedded
// via go:embed.
//
// If dist/ is empty (development without a built SPA), DistFS()
// returns a minimal filesystem with a placeholder index.html.
package web

import (
"embed"
"io/fs"
"testing/fstest"
)

//go:embed dist/*
var distEmbed embed.FS

// DistFS returns the embedded SPA filesystem. The returned fs.FS
// has dist/ as its root (files are accessed as "index.html", not
// "dist/index.html").
//
// If the embedded dist/ is empty (no SPA build), it returns a
// minimal in-memory filesystem with a placeholder page.
func DistFS() fs.FS {
sub, err := fs.Sub(distEmbed, "dist")
if err != nil {
// dist/ directory doesn't exist in the embed — return placeholder
return placeholderFS()
}

// Check if there's at least one file
entries, err := fs.ReadDir(sub, ".")
if err != nil || len(entries) == 0 {
return placeholderFS()
}

return sub
}

// placeholderFS returns a minimal in-memory filesystem with a
// placeholder index.html. Used when the SPA hasn't been built yet.
func placeholderFS() fs.FS {
return fstest.MapFS{
"index.html": &fstest.MapFile{
Data: []byte(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>dashw — DashCenter Web Console</title>
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body {
background: #0A0E1A;
color: #F9FAFB;
font-family: -apple-system, BlinkMacSystemFont, 'Inter', sans-serif;
display: flex;
align-items: center;
justify-content: center;
height: 100vh;
}
.container { text-align: center; max-width: 600px; padding: 2rem; }
h1 { color: #00D4FF; font-size: 2.5rem; margin-bottom: 1rem; }
.subtitle { color: #9CA3AF; font-size: 1.1rem; margin-bottom: 2rem; }
.status {
background: #111827;
border: 1px solid #374151;
border-radius: 12px;
padding: 1.5rem;
margin-bottom: 1.5rem;
}
.status h2 { color: #FFB800; font-size: 1rem; margin-bottom: 0.5rem; }
code {
color: #00FF88;
background: #1F2937;
padding: 0.2rem 0.5rem;
border-radius: 4px;
font-family: 'JetBrains Mono', monospace;
font-size: 0.85rem;
}
.endpoints { text-align: left; margin-top: 1rem; }
.endpoints li {
color: #9CA3AF;
margin: 0.5rem 0;
list-style: none;
}
.endpoints li::before { content: "→ "; color: #00D4FF; }
.endpoints a { color: #00D4FF; text-decoration: none; }
.endpoints a:hover { text-decoration: underline; }
</style>
</head>
<body>
<div class="container">
<h1>⬡ dashw</h1>
<p class="subtitle">DashCenter Web Console — Backend-for-Frontend is running</p>
<div class="status">
<h2>⚠ SPA Not Built</h2>
<p style="color:#9CA3AF">Build the frontend to see the full console:</p>
<p style="margin-top:0.5rem"><code>cd src/impl-web/console && npm install && npm run build</code></p>
<p style="color:#9CA3AF;margin-top:0.5rem">Then rebuild the Go binary:</p>
<p style="margin-top:0.5rem"><code>cd src/impl-go/console && go build -o dashw ./cmd/dashw</code></p>
</div>
<div class="status">
<h2>✓ BFF Endpoints Available</h2>
<ul class="endpoints">
<li><a href="/healthz">/healthz</a> — liveness probe</li>
<li><a href="/readyz">/readyz</a> — readiness probe</li>
<li><a href="/api/admin/health">/api/admin/health</a> — dashd health (proxy)</li>
<li><a href="/api/v1/default/vnets">/api/v1/default/vnets</a> — list vnets (proxy)</li>
</ul>
</div>
</div>
</body>
</html>`),
},
}
}