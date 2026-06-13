package server

import (
"log/slog"
"net/http"

"github.com/go-chi/chi/v5"
"github.com/go-chi/cors"
"github.com/prometheus/client_golang/prometheus"
"github.com/prometheus/client_golang/prometheus/promhttp"

"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/aggregation"
"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/cluster"
"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/config"
"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/health"
"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/proxy"
)

// buildRouter constructs the chi router with all middleware and routes.
// Routes are added in dependency order: middleware → health → proxy →
// aggregation → WS bridges → SPA fallback (must be last).
//
// hub may be nil when the upstream gRPC dial failed at startup; in
// that case the /api/console/topology-v2* routes return 503.
func buildRouter(cfg *config.Config, logger *slog.Logger, hub *cluster.Hub) http.Handler {
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

// ── Aggregation routes ──────────────────────────────────────
agg := aggregation.New(cfg, logger)
r.Get("/api/console/fleet/summary", agg.FleetSummary)
r.Get("/api/console/dpu/{dpuId}/detail", agg.DpuDetail)
r.Get("/api/console/topology", agg.Topology)
r.Get("/api/console/vnet/{vnetName}/detail", agg.VnetDetail)
r.Get("/api/console/stats/capacity", agg.CapacityStats)
r.Get("/api/console/service-topology", agg.ServiceTopology)
// ── Topology v2 (PE-G7) ─ NEW ───────────────────────
// Live multiplexed stream over the dashd ClusterService gRPC.
// Browser MUST hit these endpoints — NEVER dashd directly.
if hub != nil {
h := cluster.NewHTTPHandler(hub)
r.Get("/api/console/topology-v2", h.GetSnapshot)
r.Get("/api/console/topology-v2/stream", h.SSE)
r.Get("/api/console/topology-v2/ws", h.WebSocket)
r.Get("/api/console/topology-v2/_stats", h.AdminStats)
} else {
stub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(http.StatusServiceUnavailable)
_, _ = w.Write([]byte(`{"error":"topology-v2 hub not configured (dashd gRPC dial failed at startup)"}`))
})
r.Get("/api/console/topology-v2", stub)
r.Get("/api/console/topology-v2/stream", stub)
r.Get("/api/console/topology-v2/ws", stub)
r.Get("/api/console/topology-v2/_stats", stub)
}
// ── WebSocket bridges (Phase B) ─────────────────────────────
// Placeholder: WS routes are registered in B1.

// ── Optional metrics ────────────────────────────────
if cfg.EnableMetrics {
r.Handle("/metrics", promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{}))
}

// ── SPA fallback (must be last) ─────────────────────────────
// Serves the embedded SPA for any path not matched above.
// This enables client-side routing (React Router).
r.NotFound(spaHandler())

return r
}