package server

import (
"context"
"fmt"
"log/slog"
"net/http"
"time"

"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/config"
)

// Server is the dashw BFF HTTP server. It is stateless — no database,
// no session store. All fleet state comes from dashd; all client
// state lives in the browser.
type Server struct {
cfg     *config.Config
logger  *slog.Logger
httpSrv *http.Server
}

// New creates a Server wired to the given config. It builds the
// router, configures timeouts, and prepares the HTTP server. It does
// NOT start listening — call Run for that.
func New(cfg *config.Config, logger *slog.Logger) (*Server, error) {
s := &Server{
cfg:    cfg,
logger: logger,
}

handler := buildRouter(cfg, logger)

s.httpSrv = &http.Server{
Addr:         cfg.Listen,
Handler:      handler,
ReadTimeout:  30 * time.Second,
WriteTimeout: 60 * time.Second,
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

go func() {
s.logger.Info("dashw listening",
"addr", s.cfg.Listen,
"version", s.cfg.Version,
"cors", s.cfg.EnableCORS,
"metrics", s.cfg.EnableMetrics,
"dashd_rest", s.cfg.DashdRestAddr,
"dashd_admin", s.cfg.DashdAdminAddr,
"dashd_grpc", s.cfg.DashdGrpcAddr,
)
errCh <- s.httpSrv.ListenAndServe()
}()

select {
case <-ctx.Done():
s.logger.Info("shutting down dashw (signal received)")
return s.shutdown()
case err := <-errCh:
if err == http.ErrServerClosed {
return nil
}
return fmt.Errorf("http server: %w", err)
}
}

// shutdown performs graceful shutdown with a 15-second budget.
func (s *Server) shutdown() error {
shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()

// Future: close WebSocket connections, cancel gRPC stream contexts here.

if err := s.httpSrv.Shutdown(shutCtx); err != nil {
s.logger.Error("shutdown error", "error", err)
return fmt.Errorf("shutdown: %w", err)
}

s.logger.Info("graceful shutdown complete")
return nil
}