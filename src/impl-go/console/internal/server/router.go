package server

import (
"log/slog"
"net/http"

"github.com/go-chi/chi/v5"
"github.com/go-chi/cors"
"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/config"
"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/health"
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

// ── REST proxy routes (Phase A) ─────────────────────────────
// Placeholder: proxy routes are registered in A2.
// r.HandleFunc("/api/v1/*", restProxy.ServeHTTP)
// r.HandleFunc("/api/admin/*", adminProxy.ServeHTTP)

// ── Aggregation routes (Phase A3) ───────────────────────────
// Placeholder: aggregation routes are registered in A3.
// r.Get("/api/console/fleet/summary", agg.FleetSummary)
// r.Get("/api/console/dpu/{dpuId}/detail", agg.DpuDetail)
// r.Get("/api/console/topology", agg.Topology)
// r.Get("/api/console/vnet/{vnetName}/detail", agg.VnetDetail)
// r.Get("/api/console/vnet/{vnetName}/canvas", agg.VnetCanvas)
// r.Get("/api/console/stats/capacity", agg.CapacityStats)

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