package server

import (
"log/slog"
"net/http"

"github.com/go-chi/chi/v5"
"github.com/go-chi/cors"
"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/config"
"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/health"
"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/proxy"
)

// buildRouter constructs the chi router with all middleware and routes.
// Routes are added in dependency order: middleware → health → proxy →
// aggregation → WS bridges → SPA fallback (must be last).
func buildRouter(cfg *config.Config, logger *slog.Logger) http.Handler {
r := chi.NewRouter()

// ── Global middleware (outermost → innermost) ───────────────
r.Use(requestIDMiddleware)
r.Use(realIPMiddleware)
r.Use(loggingMiddleware(logger))
r.Use(recoveryMiddleware(logger))

if cfg.EnableCORS {
r.Use(cors.Handler(cors.Options{
AllowedOrigins:   []string{"*"},
AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
AllowedHeaders:   []string{"*"},
AllowCredentials: true,
MaxAge:           300,
}))
}

// ── Health & readiness probes ───────────────────────────────
r.Get("/healthz", health.LivenessHandler())
r.Get("/readyz", health.ReadinessHandler(cfg))

	// ── REST proxy routes ───────────────────────────────────────
	restProxy := proxy.NewRestProxy(cfg.DashdRestAddr, cfg.ProxyTimeout, logger)
	adminProxy := proxy.NewAdminProxy(cfg.DashdAdminAddr, cfg.ProxyTimeout, logger)

	r.Route("/api", func(r chi.Router) {
		r.HandleFunc("/v1/*", restProxy.ServeHTTP)
		r.HandleFunc("/admin/*", adminProxy.ServeHTTP)

		if cfg.SimBaseAddr != "" {
			simProxy := proxy.NewSimProxy(cfg.SimBaseAddr, cfg.ProxyTimeout, logger)
			r.HandleFunc("/sim/{simId}/admin/*", simProxy.ServeHTTP)
		}
	})

	// ── Aggregation routes (Phase A3) ───────────────────────────
	// Placeholder: aggregation endpoints registered in A3.

// ── WebSocket bridges (Phase B) ─────────────────────────────
// Placeholder: WS routes are registered in B1.

// ── Optional metrics ────────────────────────────────────────
if cfg.EnableMetrics {
// Placeholder: Prometheus handler registered in v2-O1.
r.Get("/metrics", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "text/plain")
w.WriteHeader(http.StatusOK)
_, _ = w.Write([]byte("# metrics placeholder\n"))
}))
}

// ── SPA fallback (must be last) ─────────────────────────────
// Serves the embedded SPA for any path not matched above.
// This enables client-side routing (React Router).
r.NotFound(spaHandler())

return r
}