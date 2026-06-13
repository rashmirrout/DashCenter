package server

import (
"context"
"errors"
"fmt"
"log/slog"
"net/http"
"time"

"google.golang.org/grpc"

dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/cluster"
"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/config"
)

// Server is the dashw BFF HTTP server. It is stateless — no database,
// no session store. All fleet state comes from dashd; all client
// state lives in the browser.
type Server struct {
cfg     *config.Config
logger  *slog.Logger
httpSrv *http.Server

// PE-G7: upstream gRPC conn + multiplexing hub. Closed in shutdown.
grpcConn *grpc.ClientConn
hub      *cluster.Hub
}

// New creates a Server wired to the given config. It builds the
// router, configures timeouts, and prepares the HTTP server. It does
// NOT start listening — call Run for that.
func New(cfg *config.Config, logger *slog.Logger) (*Server, error) {
s := &Server{
cfg:    cfg,
logger: logger,
}

// Best-effort: dial dashd gRPC + start the topology hub. If the
// dial fails (dashd not yet up), we degrade gracefully — the
// topology-v2 endpoints return 503 until the next process restart
// recovers them. dashw continues to serve every other endpoint.
if cfg.DashdGrpcAddr != "" {
conn, err := cluster.DialClusterService(
cfg.DashdGrpcAddr,
cfg.DashdInsecure,
cfg.GrpcDialTimeout,
cfg.DashdTLSCert, cfg.DashdTLSKey, cfg.DashdTLSCA,
cfg.DashdAuthToken,
)
if err != nil {
logger.Warn("dashw: dashd gRPC dial failed; topology-v2 endpoints will return 503",
"addr", cfg.DashdGrpcAddr, "error", err)
} else {
s.grpcConn = conn
cli := cluster.NewGRPCClient(dashcenterv1.NewClusterServiceClient(conn))
s.hub = cluster.NewHub(cli, cluster.HubConfig{
MaxWatchers:          cfg.TopoMaxWatchers,
MaxWatchersPerIP:     cfg.TopoMaxWatchersPerIP,
RingSize:             cfg.TopoRingSize,
SnapshotCacheTTL:     cfg.TopoSnapshotCacheTTL,
IdleTimeout:          cfg.TopoIdleTimeout,
UpstreamReconnectMin: cfg.TopoUpstreamReconnectMin,
UpstreamReconnectMax: cfg.TopoUpstreamReconnectMax,
}, logger)
}
}

handler := buildRouter(cfg, logger, s.hub)

s.httpSrv = &http.Server{
Addr:         cfg.Listen,
Handler:      handler,
ReadTimeout:  30 * time.Second,
WriteTimeout: 0, // SSE/WS streams need unbounded write deadline
IdleTimeout:  120 * time.Second,
}

return s, nil
}

// Run starts the HTTP server and blocks until the context is
// cancelled (e.g., SIGINT/SIGTERM). On cancellation it performs
// graceful shutdown: stops accepting new connections, drains
// in-flight requests (15s budget), and returns.
func (s *Server) Run(ctx context.Context) error {
errCh := make(chan error, 1)

// PE-G7: start the topology hub's upstream stream BEFORE the HTTP
// server begins accepting connections so cold-start watchers find a
// hot stream.
if s.hub != nil {
s.hub.Start(ctx)
}

go func() {
s.logger.Info("dashw listening",
"addr", s.cfg.Listen,
"version", s.cfg.Version,
"cors", s.cfg.EnableCORS,
"metrics", s.cfg.EnableMetrics,
"dashd_rest", s.cfg.DashdRestAddr,
"dashd_admin", s.cfg.DashdAdminAddr,
"dashd_grpc", s.cfg.DashdGrpcAddr,
"topology_hub", s.hub != nil,
)
errCh <- s.httpSrv.ListenAndServe()
}()

select {
case <-ctx.Done():
s.logger.Info("shutting down dashw (signal received)")
return s.shutdown()
case err := <-errCh:
if errors.Is(err, http.ErrServerClosed) {
return nil
}
return fmt.Errorf("http server: %w", err)
}
}

// shutdown performs graceful shutdown with a 15-second budget.
func (s *Server) shutdown() error {
shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()

if s.hub != nil {
s.hub.Stop()
}
if s.grpcConn != nil {
_ = s.grpcConn.Close()
}

if err := s.httpSrv.Shutdown(shutCtx); err != nil {
s.logger.Error("shutdown error", "error", err)
return fmt.Errorf("shutdown: %w", err)
}

s.logger.Info("graceful shutdown complete")
return nil
}